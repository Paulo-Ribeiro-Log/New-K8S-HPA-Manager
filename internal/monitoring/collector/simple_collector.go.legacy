package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/portforward"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/monitoring/scanner"
	"k8s-hpa-manager/internal/monitoring/storage"
)

// SimpleCollector - Sistema simplificado de coleta
// - 1 porta por cluster para scans normais (port-forward criado durante scan, destruído após)
// - 1 porta dedicada (55557) para baseline (criada sob demanda, destruída após coleta)
type SimpleCollector struct {
	// Configuração
	targets      map[string]*SimpleTarget // Cluster -> Target mapping
	scanPorts    []int                    // [55551-55556] para scans normais
	baselinePort int                      // 55557 para baseline

	// Dependências
	persistence *storage.Persistence
	pfManager   *portforward.PortForwardManager
	kubeManager *config.KubeConfigManager

	// Controle
	running       bool
	scanInterval  time.Duration
	stopCh        chan struct{}
	baselineQueue chan BaselineRequest
	mu            sync.RWMutex
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

// SimpleTarget representa um cluster e seus HPAs monitorados (simple collector)
type SimpleTarget struct {
	Cluster    string
	Namespaces []string
	HPAs       []string
}

// BaselineRequest requisição de coleta de baseline
type BaselineRequest struct {
	Cluster   string
	Namespace string
	HPAName   string
}

// NewSimpleCollector cria novo collector simplificado
func NewSimpleCollector(
	persistence *storage.Persistence,
	pfManager *portforward.PortForwardManager,
	kubeManager *config.KubeConfigManager,
) *SimpleCollector {
	ctx, cancel := context.WithCancel(context.Background())

	return &SimpleCollector{
		targets:       make(map[string]*SimpleTarget),
		scanPorts:     []int{55551, 55552, 55553, 55554, 55555, 55556},
		baselinePort:  55557,
		persistence:   persistence,
		pfManager:     pfManager,
		kubeManager:   kubeManager,
		scanInterval:  30 * time.Second, // Scan a cada 30s
		stopCh:        make(chan struct{}),
		baselineQueue: make(chan BaselineRequest, 100), // Fila de baseline
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start inicia coleta
func (c *SimpleCollector) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("collector já está rodando")
	}
	c.running = true
	c.mu.Unlock()

	log.Info().
		Int("scan_ports", len(c.scanPorts)).
		Int("baseline_port", c.baselinePort).
		Dur("scan_interval", c.scanInterval).
		Msg("🔄 SimpleCollector iniciado")

	// Inicia loop de scans
	c.wg.Add(1)
	go c.scanLoop()

	// Inicia worker de baseline
	c.wg.Add(1)
	go c.baselineWorker()

	return nil
}

// Stop para coleta (graceful)
func (c *SimpleCollector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	log.Info().Msg("Parando SimpleCollector...")

	// Sinaliza stop
	close(c.stopCh)
	c.cancel()

	// Aguarda goroutines terminarem
	c.wg.Wait()

	// Para todos os port-forwards
	if err := c.pfManager.StopAll(); err != nil {
		log.Warn().Err(err).Msg("Erro ao parar port-forwards")
	}

	log.Info().Msg("✅ SimpleCollector parado")
}

// AddTarget adiciona cluster/HPA à coleta
func (c *SimpleCollector) AddTarget(target scanner.ScanTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verifica se cluster já existe
	if existingTarget, exists := c.targets[target.Cluster]; exists {
		// Atualiza target existente (mescla HPAs)
		hpaMap := make(map[string]bool)
		for _, hpa := range existingTarget.HPAs {
			hpaMap[hpa] = true
		}
		for _, hpa := range target.HPAs {
			hpaMap[hpa] = true
		}
		existingTarget.HPAs = make([]string, 0, len(hpaMap))
		for hpa := range hpaMap {
			existingTarget.HPAs = append(existingTarget.HPAs, hpa)
		}
		existingTarget.Namespaces = target.Namespaces

		log.Info().
			Str("cluster", target.Cluster).
			Int("total_hpas", len(existingTarget.HPAs)).
			Msg("Target atualizado no SimpleCollector")
	} else {
		// Novo cluster
		c.targets[target.Cluster] = &SimpleTarget{
			Cluster:    target.Cluster,
			Namespaces: target.Namespaces,
			HPAs:       target.HPAs,
		}

		log.Info().
			Str("cluster", target.Cluster).
			Int("hpas", len(target.HPAs)).
			Msg("Novo cluster adicionado ao SimpleCollector")
	}

	// Adiciona HPAs à fila de baseline (se necessário)
	for _, ns := range target.Namespaces {
		for _, hpaName := range target.HPAs {
			c.requestBaselineIfNeeded(target.Cluster, ns, hpaName)
		}
	}
}

// RemoveTarget remove cluster da coleta
func (c *SimpleCollector) RemoveTarget(cluster string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.targets, cluster)

	log.Info().
		Str("cluster", cluster).
		Msg("Cluster removido do SimpleCollector")
}

// scanLoop loop principal de scans
func (c *SimpleCollector) scanLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.scanInterval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", c.scanInterval).
		Msg("🔄 Loop de scans iniciado")

	// Executa scan imediato ao iniciar
	c.executeScan()

	for {
		select {
		case <-c.ctx.Done():
			log.Info().Msg("Loop de scans encerrado (context cancelled)")
			return
		case <-c.stopCh:
			log.Info().Msg("Loop de scans encerrado (stopCh)")
			return
		case <-ticker.C:
			c.executeScan()
		}
	}
}

// executeScan executa 1 scan de todos os clusters (1 por vez)
func (c *SimpleCollector) executeScan() {
	c.mu.RLock()
	clusters := make([]string, 0, len(c.targets))
	for cluster := range c.targets {
		clusters = append(clusters, cluster)
	}
	c.mu.RUnlock()

	if len(clusters) == 0 {
		log.Debug().Msg("Nenhum cluster para escanear")
		return
	}

	log.Debug().
		Int("clusters", len(clusters)).
		Msg("Iniciando scan de clusters...")

	startTime := time.Now()

	// Scan sequencial de cada cluster
	for _, cluster := range clusters {
		if err := c.scanCluster(cluster); err != nil {
			log.Error().
				Err(err).
				Str("cluster", cluster).
				Msg("Erro ao escanear cluster")
		}
	}

	elapsed := time.Since(startTime)
	log.Info().
		Int("clusters", len(clusters)).
		Dur("elapsed", elapsed).
		Msg("✅ Scan completo")
}

// scanCluster escaneia 1 cluster
func (c *SimpleCollector) scanCluster(cluster string) error {
	// Contexto com timeout de 30s por cluster
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	log.Debug().
		Str("cluster", cluster).
		Msg("Escaneando cluster...")

	// 1. Criar port-forward temporário
	if err := c.pfManager.Start(cluster); err != nil {
		return fmt.Errorf("falha ao criar port-forward: %w", err)
	}
	defer c.pfManager.Stop(cluster) // Destrói após scan

	// Aguarda port-forward estar pronto
	time.Sleep(2 * time.Second)

	// 2. Busca target deste cluster
	c.mu.RLock()
	target, exists := c.targets[cluster]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("target não encontrado para cluster %s", cluster)
	}

	// 3. Criar cliente Prometheus
	promEndpoint := c.pfManager.GetURL(cluster)
	if promEndpoint == "" {
		log.Warn().
			Str("cluster", cluster).
			Msg("Port-forward não ativo, pulando scan")
		return nil
	}

	promClient, err := prometheus.NewClient(cluster, promEndpoint)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Falha ao criar cliente Prometheus, pulando scan")
		return nil
	}

	// 4. Criar K8s clientset
	clusterContext := cluster
	if !strings.HasSuffix(cluster, "-admin") {
		clusterContext = cluster + "-admin"
	}

	clientset, err := c.kubeManager.GetClient(clusterContext)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Failed to get K8s client")
		clientset = nil
	}

	// 5. Coletar métricas de cada HPA
	snapshots := make([]*models.HPASnapshot, 0)

	for _, ns := range target.Namespaces {
		for _, hpaName := range target.HPAs {
			snapshot := &models.HPASnapshot{
				Cluster:   cluster,
				Namespace: ns,
				Name:      hpaName,
				Timestamp: time.Now(),
			}

			// Enriquece com K8s API se disponível
			if clientset != nil {
				// TODO: enrichSnapshotWithK8sData(ctx, clientset, snapshot)
			}

			// Enriquece com Prometheus
			if err := promClient.EnrichSnapshot(ctx, snapshot); err != nil {
				log.Debug().
					Err(err).
					Str("cluster", cluster).
					Str("namespace", ns).
					Str("hpa", hpaName).
					Msg("Falha ao enriquecer snapshot com Prometheus")
			}

			snapshots = append(snapshots, snapshot)
		}
	}

	// 6. Salva snapshots no SQLite (batch)
	if len(snapshots) > 0 && c.persistence != nil {
		if err := c.persistence.SaveSnapshots(snapshots); err != nil {
			log.Error().
				Err(err).
				Str("cluster", cluster).
				Int("snapshots", len(snapshots)).
				Msg("Erro ao salvar snapshots no SQLite")
		} else {
			log.Debug().
				Str("cluster", cluster).
				Int("snapshots", len(snapshots)).
				Msg("📊 Métricas coletadas e salvas")
		}
	}

	return nil
}

// requestBaselineIfNeeded verifica se baseline é necessário e adiciona à fila
func (c *SimpleCollector) requestBaselineIfNeeded(cluster, namespace, hpaName string) {
	if c.persistence == nil {
		return
	}

	// Verifica se baseline já existe e está atualizado
	ready, err := c.persistence.IsBaselineReady(cluster, namespace, hpaName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Str("hpa", hpaName).
			Msg("Erro ao verificar baseline, adicionando à fila")
		// Adiciona à fila por segurança
		c.addToBaselineQueue(cluster, namespace, hpaName)
		return
	}

	if ready {
		log.Debug().
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("Baseline já existe e está atualizado")
		return
	}

	// Baseline não existe ou está desatualizado, adiciona à fila
	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("Baseline necessário, adicionando à fila")

	c.addToBaselineQueue(cluster, namespace, hpaName)
}

// addToBaselineQueue adiciona requisição à fila de baseline
func (c *SimpleCollector) addToBaselineQueue(cluster, namespace, hpaName string) {
	req := BaselineRequest{
		Cluster:   cluster,
		Namespace: namespace,
		HPAName:   hpaName,
	}

	select {
	case c.baselineQueue <- req:
		log.Debug().
			Str("cluster", cluster).
			Str("hpa", hpaName).
			Msg("HPA adicionado à fila de baseline")
	case <-time.After(2 * time.Second):
		log.Warn().
			Str("cluster", cluster).
			Str("hpa", hpaName).
			Msg("Timeout ao adicionar à fila de baseline (fila cheia)")
	}
}

// baselineWorker processa fila de baselines
func (c *SimpleCollector) baselineWorker() {
	defer c.wg.Done()

	log.Info().
		Int("port", c.baselinePort).
		Msg("🔄 Worker de baseline iniciado")

	for {
		select {
		case <-c.ctx.Done():
			log.Info().Msg("Worker de baseline encerrado (context cancelled)")
			return
		case <-c.stopCh:
			log.Info().Msg("Worker de baseline encerrado (stopCh)")
			return
		case req := <-c.baselineQueue:
			c.collectBaseline(req)
		}
	}
}

// collectBaseline coleta baseline de 3 dias para um HPA
func (c *SimpleCollector) collectBaseline(req BaselineRequest) {
	log.Info().
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("hpa", req.HPAName).
		Int("port", c.baselinePort).
		Msg("📊 Iniciando coleta de baseline histórico (3 dias)...")

	startTime := time.Now()

	// Contexto com timeout de 10 minutos
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
	defer cancel()

	// 1. Criar port-forward dedicado na porta de baseline
	log.Info().
		Int("port", c.baselinePort).
		Str("cluster", req.Cluster).
		Msg("Criando port-forward dedicado para baseline...")

	if err := c.pfManager.Start(req.Cluster); err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Msg("❌ Falha ao criar port-forward para baseline")
		return
	}

	// IMPORTANTE: Destruir port-forward ao final
	defer func() {
		if err := c.pfManager.Stop(req.Cluster); err != nil {
			log.Warn().
				Err(err).
				Str("cluster", req.Cluster).
				Msg("Erro ao destruir port-forward após baseline")
		} else {
			log.Info().
				Str("cluster", req.Cluster).
				Msg("🗑️ Port-forward destruído após baseline")
		}
	}()

	// Aguarda port-forward estar pronto
	time.Sleep(3 * time.Second)

	// 2. Criar cliente Prometheus (porta 55557 dedicada)
	promEndpoint := fmt.Sprintf("http://localhost:%d", c.baselinePort)
	promClient, err := prometheus.NewClient(req.Cluster, promEndpoint)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Msg("❌ Falha ao criar cliente Prometheus para baseline")
		return
	}

	// 3. Coletar histórico de 3 dias via Prometheus QueryRange
	snapshots, err := c.collectHistoricalData(ctx, promClient, req)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("❌ Erro ao coletar dados históricos")
		return
	}

	if len(snapshots) == 0 {
		log.Warn().
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("❌ Nenhum dado histórico encontrado (HPA pode ser novo)")
		return
	}

	// 4. Salvar no SQLite
	if err := c.persistence.SaveSnapshots(snapshots); err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Int("snapshots", len(snapshots)).
			Msg("❌ Erro ao salvar baseline no SQLite")
		return
	}

	// 5. Marcar baseline como pronto
	if err := c.persistence.MarkBaselineReady(req.Cluster, req.Namespace, req.HPAName); err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("❌ Erro ao marcar baseline como pronto")
		return
	}

	elapsed := time.Since(startTime)
	log.Info().
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("hpa", req.HPAName).
		Int("snapshots", len(snapshots)).
		Dur("elapsed", elapsed).
		Msg("✅ Baseline histórico coletado e salvo")
}

// collectHistoricalData coleta 3 dias de histórico via Prometheus
func (c *SimpleCollector) collectHistoricalData(ctx context.Context, promClient *prometheus.Client, req BaselineRequest) ([]*models.HPASnapshot, error) {
	// Range de 3 dias
	end := time.Now()
	start := end.Add(-72 * time.Hour) // 3 dias
	step := 1 * time.Minute            // Coleta a cada 1 minuto

	log.Debug().
		Str("cluster", req.Cluster).
		Str("hpa", req.HPAName).
		Time("start", start).
		Time("end", end).
		Msg("Coletando histórico de 3 dias...")

	// Query para histórico de réplicas
	replicasQuery := fmt.Sprintf(`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`, req.Namespace, req.HPAName)
	replicasResult, err := promClient.QueryRange(ctx, replicasQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("Falha ao coletar histórico de réplicas")
	}

	// Query para histórico de CPU
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`, req.Namespace, req.HPAName, req.Namespace, req.HPAName)
	cpuResult, err := promClient.QueryRange(ctx, cpuQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("Falha ao coletar histórico de CPU")
	}

	// Query para histórico de memória
	memoryQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`, req.Namespace, req.HPAName, req.Namespace, req.HPAName)
	memoryResult, err := promClient.QueryRange(ctx, memoryQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", req.Cluster).
			Str("hpa", req.HPAName).
			Msg("Falha ao coletar histórico de memória")
	}

	// Converter resultados para snapshots
	snapshots := make([]*models.HPASnapshot, 0)

	// Usa timestamp de réplicas como base (métrica mais confiável)
	if matrix, ok := replicasResult.(model.Matrix); ok {
		for _, sample := range matrix {
			for _, value := range sample.Values {
				timestamp := time.Unix(int64(value.Timestamp)/1000, 0)

				snapshot := &models.HPASnapshot{
					Cluster:   req.Cluster,
					Namespace: req.Namespace,
					Name:      req.HPAName,
					Timestamp: timestamp,
				}

				// Preencher réplicas
				snapshot.CurrentReplicas = int32(float64(value.Value))

				// Buscar CPU correspondente
				if cpuMatrix, ok := cpuResult.(model.Matrix); ok {
					if len(cpuMatrix) > 0 {
						for _, cpuSample := range cpuMatrix[0].Values {
							cpuTimestamp := time.Unix(int64(cpuSample.Timestamp)/1000, 0)
							if cpuTimestamp.Equal(timestamp) || cpuTimestamp.Sub(timestamp).Abs() < 30*time.Second {
								snapshot.CPUCurrent = float64(cpuSample.Value)
								break
							}
						}
					}
				}

				// Buscar memória correspondente
				if memMatrix, ok := memoryResult.(model.Matrix); ok {
					if len(memMatrix) > 0 {
						for _, memSample := range memMatrix[0].Values {
							memTimestamp := time.Unix(int64(memSample.Timestamp)/1000, 0)
							if memTimestamp.Equal(timestamp) || memTimestamp.Sub(timestamp).Abs() < 30*time.Second {
								snapshot.MemoryCurrent = float64(memSample.Value)
								break
							}
						}
					}
				}

				snapshots = append(snapshots, snapshot)
			}
		}
	}

	log.Debug().
		Str("cluster", req.Cluster).
		Str("hpa", req.HPAName).
		Int("snapshots", len(snapshots)).
		Msg("Histórico coletado")

	return snapshots, nil
}

// GetClusters retorna lista de clusters ativos
func (c *SimpleCollector) GetClusters() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clusters := make([]string, 0, len(c.targets))
	for cluster := range c.targets {
		clusters = append(clusters, cluster)
	}
	return clusters
}

// GetStatus retorna status do collector
func (c *SimpleCollector) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"running":         c.running,
		"clusters":        len(c.targets),
		"scan_interval":   c.scanInterval.String(),
		"baseline_queue":  len(c.baselineQueue),
		"scan_ports":      c.scanPorts,
		"baseline_port":   c.baselinePort,
	}
}
