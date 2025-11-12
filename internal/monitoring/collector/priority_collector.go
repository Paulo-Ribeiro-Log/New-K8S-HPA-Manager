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

// PriorityCollector - Sistema com prioridades
// PRIORIDADE 1: HPAs escolhidos pelo usuário (web interface)
//   - Port-forward dedicado persistente
//   - Scan a cada 30s
//   - Baseline de 3 dias carregada imediatamente
// PRIORIDADE 2: HPAs da configuração (arquivo)
//   - Port-forward sob demanda
//   - Scan menos frequente (ou ignorado)
type PriorityCollector struct {
	// HPAs prioritários (escolhidos pelo usuário)
	priorityHPAs map[string]*PriorityHPA // key: "cluster/namespace/hpa"
	priorityMu   sync.RWMutex

	// Port-forwards dedicados para HPAs prioritários
	portForwards map[string]int // cluster -> porta dedicada
	availPorts   []int          // Portas disponíveis: [55551, 55552, 55553, 55554, 55555, 55556]
	portMu       sync.Mutex

	// Dependências
	persistence *storage.Persistence
	pfManager   *portforward.PortForwardManager
	kubeManager *config.KubeConfigManager

	// Controle
	running  bool
	stopCh   chan struct{}
	mu       sync.RWMutex
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// PriorityHPA representa um HPA com prioridade máxima
type PriorityHPA struct {
	Cluster    string
	Namespace  string
	Name       string
	AddedAt    time.Time
	Port       int           // Porta dedicada para esse cluster
	LastScan   time.Time
	ScanCount  int
	BaselineReady bool      // Baseline de 3 dias já foi carregado
}

// NewPriorityCollector cria novo collector com sistema de prioridades
func NewPriorityCollector(
	persistence *storage.Persistence,
	pfManager *portforward.PortForwardManager,
	kubeManager *config.KubeConfigManager,
) *PriorityCollector {
	ctx, cancel := context.WithCancel(context.Background())

	return &PriorityCollector{
		priorityHPAs: make(map[string]*PriorityHPA),
		portForwards: make(map[string]int),
		availPorts:   []int{55551, 55552, 55553, 55554, 55555, 55556},
		persistence:  persistence,
		pfManager:    pfManager,
		kubeManager:  kubeManager,
		stopCh:       make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start inicia coleta
func (c *PriorityCollector) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("collector já está rodando")
	}
	c.running = true
	c.mu.Unlock()

	log.Info().
		Int("available_ports", len(c.availPorts)).
		Msg("🔄 PriorityCollector iniciado")

	// Inicia loop de scans prioritários
	c.wg.Add(1)
	go c.priorityScanLoop()

	return nil
}

// Stop para coleta (graceful)
func (c *PriorityCollector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	log.Info().Msg("Parando PriorityCollector...")

	// Sinaliza stop
	close(c.stopCh)
	c.cancel()

	// Aguarda goroutines terminarem
	c.wg.Wait()

	// Para todos os port-forwards
	if err := c.pfManager.StopAll(); err != nil {
		log.Warn().Err(err).Msg("Erro ao parar port-forwards")
	}

	log.Info().Msg("✅ PriorityCollector parado")
}

// AddPriorityHPA adiciona HPA com PRIORIDADE MÁXIMA (escolhido pelo usuário)
// Este HPA terá port-forward dedicado e scan a cada 30s
func (c *PriorityCollector) AddPriorityHPA(cluster, namespace, hpaName string) error {
	key := fmt.Sprintf("%s/%s/%s", cluster, namespace, hpaName)

	c.priorityMu.Lock()
	defer c.priorityMu.Unlock()

	// Verifica se já existe
	if _, exists := c.priorityHPAs[key]; exists {
		log.Info().
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("HPA prioritário já existe, atualizando...")
		return nil
	}

	// Aloca porta dedicada para o cluster (se ainda não tem)
	port, err := c.allocatePort(cluster)
	if err != nil {
		return fmt.Errorf("falha ao alocar porta: %w", err)
	}

	// Cria HPA prioritário
	priorityHPA := &PriorityHPA{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      hpaName,
		AddedAt:   time.Now(),
		Port:      port,
	}

	c.priorityHPAs[key] = priorityHPA

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Int("port", port).
		Msg("✅ HPA adicionado com PRIORIDADE MÁXIMA")

	// Inicia port-forward dedicado
	if err := c.startDedicatedPortForward(cluster, port); err != nil {
		log.Error().
			Err(err).
			Str("cluster", cluster).
			Int("port", port).
			Msg("Falha ao iniciar port-forward dedicado")
		return err
	}

	// Coleta baseline IMEDIATAMENTE (assíncrono)
	c.wg.Add(1)
	go c.collectBaselineImmediately(priorityHPA)

	return nil
}

// RemovePriorityHPA remove HPA prioritário
func (c *PriorityCollector) RemovePriorityHPA(cluster, namespace, hpaName string) {
	key := fmt.Sprintf("%s/%s/%s", cluster, namespace, hpaName)

	c.priorityMu.Lock()
	defer c.priorityMu.Unlock()

	delete(c.priorityHPAs, key)

	// Se não há mais HPAs deste cluster, libera porta
	hasOtherHPAs := false
	for _, hpa := range c.priorityHPAs {
		if hpa.Cluster == cluster {
			hasOtherHPAs = true
			break
		}
	}

	if !hasOtherHPAs {
		// Pega porta antes de liberar
		port := c.portForwards[cluster]
		c.releasePort(cluster)

		// Para port-forward dedicado
		if err := c.pfManager.StopWithPort(cluster, port); err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Int("port", port).
				Msg("Erro ao parar port-forward")
		}
	}

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("HPA prioritário removido")
}

// allocatePort aloca porta dedicada para um cluster
func (c *PriorityCollector) allocatePort(cluster string) (int, error) {
	c.portMu.Lock()
	defer c.portMu.Unlock()

	// Verifica se cluster já tem porta alocada
	if port, exists := c.portForwards[cluster]; exists {
		return port, nil
	}

	// Verifica se há portas disponíveis
	if len(c.availPorts) == 0 {
		return 0, fmt.Errorf("nenhuma porta disponível (máximo 6 clusters simultâneos)")
	}

	// Aloca primeira porta disponível
	port := c.availPorts[0]
	c.availPorts = c.availPorts[1:]
	c.portForwards[cluster] = port

	log.Debug().
		Str("cluster", cluster).
		Int("port", port).
		Int("remaining_ports", len(c.availPorts)).
		Msg("Porta alocada para cluster")

	return port, nil
}

// releasePort libera porta de um cluster
func (c *PriorityCollector) releasePort(cluster string) {
	c.portMu.Lock()
	defer c.portMu.Unlock()

	if port, exists := c.portForwards[cluster]; exists {
		delete(c.portForwards, cluster)
		c.availPorts = append(c.availPorts, port)

		log.Debug().
			Str("cluster", cluster).
			Int("port", port).
			Msg("Porta liberada")
	}
}

// startDedicatedPortForward inicia port-forward dedicado persistente
func (c *PriorityCollector) startDedicatedPortForward(cluster string, port int) error {
	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("Criando port-forward DEDICADO persistente...")

	if err := c.pfManager.StartWithPort(cluster, port); err != nil {
		return fmt.Errorf("falha ao criar port-forward: %w", err)
	}

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward DEDICADO criado e PERSISTENTE")

	return nil
}

// collectBaselineImmediately coleta baseline de 3 dias IMEDIATAMENTE
func (c *PriorityCollector) collectBaselineImmediately(hpa *PriorityHPA) {
	defer c.wg.Done()

	log.Info().
		Str("cluster", hpa.Cluster).
		Str("namespace", hpa.Namespace).
		Str("hpa", hpa.Name).
		Msg("📊 Coletando baseline de 3 dias IMEDIATAMENTE...")

	startTime := time.Now()

	// Contexto com timeout de 10 minutos
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
	defer cancel()

	// Verifica se baseline já existe no SQLite
	if c.persistence != nil {
		ready, err := c.persistence.IsBaselineReady(hpa.Cluster, hpa.Namespace, hpa.Name)
		if err == nil && ready {
			log.Info().
				Str("cluster", hpa.Cluster).
				Str("hpa", hpa.Name).
				Msg("✅ Baseline já existe no SQLite (carregado)")

			c.priorityMu.Lock()
			hpa.BaselineReady = true
			c.priorityMu.Unlock()
			return
		}
	}

	// Criar port-forward TEMPORÁRIO para baseline (portas 55557 ou 55558)
	baselinePort, err := c.pfManager.StartBaselinePort(hpa.Cluster)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Msg("❌ Falha ao criar port-forward de baseline")
		return
	}
	defer func() {
		// Destroi port-forward de baseline ao finalizar
		if err := c.pfManager.StopBaselinePort(hpa.Cluster, baselinePort); err != nil {
			log.Warn().
				Err(err).
				Str("cluster", hpa.Cluster).
				Int("port", baselinePort).
				Msg("Erro ao parar port-forward de baseline")
		}
	}()

	// Aguarda port-forward estar pronto
	time.Sleep(3 * time.Second)

	// Criar cliente Prometheus (usa porta de baseline)
	promEndpoint := fmt.Sprintf("http://localhost:%d", baselinePort)
	promClient, err := prometheus.NewClient(hpa.Cluster, promEndpoint)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Msg("❌ Falha ao criar cliente Prometheus para baseline")
		return
	}

	// Testa conexão antes de coletar histórico
	if err := promClient.TestConnection(ctx); err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Int("port", baselinePort).
			Msg("❌ Prometheus não está acessível na porta de baseline")
		return
	}

	log.Info().
		Str("cluster", hpa.Cluster).
		Int("port", baselinePort).
		Msg("✅ Prometheus conectado com sucesso")

	// Coletar histórico de 3 dias via Prometheus
	snapshots, err := c.collectHistoricalData(ctx, promClient, hpa)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("❌ Erro ao coletar dados históricos")
		return
	}

	if len(snapshots) == 0 {
		log.Warn().
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("⚠️ Nenhum dado histórico encontrado (HPA pode ser novo)")
		// Marca como ready mesmo sem dados (evita tentar novamente)
		c.priorityMu.Lock()
		hpa.BaselineReady = true
		c.priorityMu.Unlock()
		return
	}

	// Salvar no SQLite
	if err := c.persistence.SaveSnapshots(snapshots); err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Int("snapshots", len(snapshots)).
			Msg("❌ Erro ao salvar baseline no SQLite")
		return
	}

	// Marcar baseline como pronto
	if err := c.persistence.MarkBaselineReady(hpa.Cluster, hpa.Namespace, hpa.Name); err != nil {
		log.Error().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("❌ Erro ao marcar baseline como pronto")
		return
	}

	c.priorityMu.Lock()
	hpa.BaselineReady = true
	c.priorityMu.Unlock()

	elapsed := time.Since(startTime)
	log.Info().
		Str("cluster", hpa.Cluster).
		Str("namespace", hpa.Namespace).
		Str("hpa", hpa.Name).
		Int("snapshots", len(snapshots)).
		Dur("elapsed", elapsed).
		Msg("✅ Baseline de 3 dias coletado e pronto para exibição!")
}

// collectHistoricalData coleta 3 dias de histórico via Prometheus
func (c *PriorityCollector) collectHistoricalData(ctx context.Context, promClient *prometheus.Client, hpa *PriorityHPA) ([]*models.HPASnapshot, error) {
	// Range de 3 dias
	end := time.Now()
	start := end.Add(-72 * time.Hour) // 3 dias
	step := 1 * time.Minute            // Coleta a cada 1 minuto

	log.Debug().
		Str("cluster", hpa.Cluster).
		Str("hpa", hpa.Name).
		Time("start", start).
		Time("end", end).
		Msg("Coletando histórico de 3 dias...")

	// Query para histórico de réplicas
	replicasQuery := fmt.Sprintf(`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`, hpa.Namespace, hpa.Name)
	replicasResult, err := promClient.QueryRange(ctx, replicasQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("Falha ao coletar histórico de réplicas")
	}

	// Query para histórico de CPU
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`, hpa.Namespace, hpa.Name, hpa.Namespace, hpa.Name)
	cpuResult, err := promClient.QueryRange(ctx, cpuQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("Falha ao coletar histórico de CPU")
	}

	// Query para histórico de memória
	memoryQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`, hpa.Namespace, hpa.Name, hpa.Namespace, hpa.Name)
	memoryResult, err := promClient.QueryRange(ctx, memoryQuery, start, end, step)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
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
					Cluster:   hpa.Cluster,
					Namespace: hpa.Namespace,
					Name:      hpa.Name,
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
		Str("cluster", hpa.Cluster).
		Str("hpa", hpa.Name).
		Int("snapshots", len(snapshots)).
		Msg("Histórico coletado")

	return snapshots, nil
}

// priorityScanLoop loop de scans para HPAs prioritários (30s interval)
func (c *PriorityCollector) priorityScanLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Info().Msg("🔄 Loop de scans prioritários iniciado (30s interval)")

	// Executa scan imediato ao iniciar
	c.executePriorityScans()

	for {
		select {
		case <-c.ctx.Done():
			log.Info().Msg("Loop de scans prioritários encerrado (context cancelled)")
			return
		case <-c.stopCh:
			log.Info().Msg("Loop de scans prioritários encerrado (stopCh)")
			return
		case <-ticker.C:
			c.executePriorityScans()
		}
	}
}

// ensurePortForward garante que port-forward está ativo (reconciliação KISS)
// Testa conexão Prometheus, recria port-forward se falhar
func (c *PriorityCollector) ensurePortForward(ctx context.Context, hpa *PriorityHPA) error {
	// Criar cliente temporário para testar conexão
	promEndpoint := fmt.Sprintf("http://localhost:%d", hpa.Port)
	testClient, err := prometheus.NewClient(hpa.Cluster, promEndpoint)
	if err != nil {
		return fmt.Errorf("falha ao criar cliente de teste: %w", err)
	}

	// Testar conexão com timeout curto (3s)
	testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := testClient.TestConnection(testCtx); err == nil {
		// Port-forward ativo, tudo OK
		return nil
	}

	// Port-forward caiu, recriar
	log.Warn().
		Str("cluster", hpa.Cluster).
		Int("port", hpa.Port).
		Msg("⚠️ Port-forward caiu, recriando...")

	// Para port-forward antigo (ignora erro se já estiver parado)
	_ = c.pfManager.StopWithPort(hpa.Cluster, hpa.Port)

	// Aguarda 1s antes de recriar (limpeza de recursos)
	time.Sleep(1 * time.Second)

	// Recria port-forward
	if err := c.pfManager.StartWithPort(hpa.Cluster, hpa.Port); err != nil {
		return fmt.Errorf("falha ao recriar port-forward: %w", err)
	}

	// Aguarda port-forward estar pronto
	time.Sleep(2 * time.Second)

	// Testa novamente
	if err := testClient.TestConnection(ctx); err != nil {
		return fmt.Errorf("port-forward recriado mas Prometheus ainda inacessível: %w", err)
	}

	log.Info().
		Str("cluster", hpa.Cluster).
		Int("port", hpa.Port).
		Msg("✅ Port-forward recriado com sucesso")

	return nil
}

// executePriorityScans executa scan de TODOS os HPAs prioritários
func (c *PriorityCollector) executePriorityScans() {
	c.priorityMu.RLock()
	hpas := make([]*PriorityHPA, 0, len(c.priorityHPAs))
	for _, hpa := range c.priorityHPAs {
		hpas = append(hpas, hpa)
	}
	c.priorityMu.RUnlock()

	if len(hpas) == 0 {
		log.Debug().Msg("Nenhum HPA prioritário para escanear")
		return
	}

	log.Debug().
		Int("hpas", len(hpas)).
		Msg("Escaneando HPAs prioritários...")

	startTime := time.Now()

	// Scan paralelo de todos os HPAs prioritários
	var wg sync.WaitGroup
	for _, hpa := range hpas {
		wg.Add(1)
		go func(h *PriorityHPA) {
			defer wg.Done()
			if err := c.scanPriorityHPA(h); err != nil {
				log.Error().
					Err(err).
					Str("cluster", h.Cluster).
					Str("hpa", h.Name).
					Msg("Erro ao escanear HPA prioritário")
			}
		}(hpa)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	log.Info().
		Int("hpas", len(hpas)).
		Dur("elapsed", elapsed).
		Msg("✅ Scan prioritário completo")
}

// scanPriorityHPA escaneia 1 HPA prioritário
func (c *PriorityCollector) scanPriorityHPA(hpa *PriorityHPA) error {
	// Contexto com timeout de 30s
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	log.Debug().
		Str("cluster", hpa.Cluster).
		Str("namespace", hpa.Namespace).
		Str("hpa", hpa.Name).
		Msg("Escaneando HPA prioritário...")

	// RECONCILIAÇÃO: Verifica se port-forward está ativo, recria se necessário
	if err := c.ensurePortForward(ctx, hpa); err != nil {
		return fmt.Errorf("falha ao garantir port-forward: %w", err)
	}

	// Criar cliente Prometheus (port-forward já está ativo)
	promEndpoint := fmt.Sprintf("http://localhost:%d", hpa.Port)
	promClient, err := prometheus.NewClient(hpa.Cluster, promEndpoint)
	if err != nil {
		return fmt.Errorf("falha ao criar cliente Prometheus: %w", err)
	}

	// Criar snapshot
	snapshot := &models.HPASnapshot{
		Cluster:   hpa.Cluster,
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
		Timestamp: time.Now(),
	}

	// Enriquecer com dados do K8s API (HPA config + Deployment resources)
	// CRÍTICO: Preenche MinReplicas, MaxReplicas, CPUTarget, MemoryTarget,
	// CPURequest, CPULimit, MemoryRequest, MemoryLimit
	clusterContext := hpa.Cluster
	if !strings.HasSuffix(clusterContext, "-admin") {
		clusterContext += "-admin"
	}
	client, err := c.kubeManager.GetClient(clusterContext)
	if err == nil {
		c.enrichSnapshotWithK8sData(ctx, client, snapshot)
	} else {
		log.Debug().
			Err(err).
			Str("cluster", hpa.Cluster).
			Msg("Falha ao obter client para enriquecer com K8s data")
	}

	// Enriquecer com Prometheus
	if err := promClient.EnrichSnapshot(ctx, snapshot); err != nil {
		log.Debug().
			Err(err).
			Str("cluster", hpa.Cluster).
			Str("hpa", hpa.Name).
			Msg("Falha ao enriquecer snapshot com Prometheus")
	}

	// Salvar no SQLite
	if c.persistence != nil {
		if err := c.persistence.SaveSnapshots([]*models.HPASnapshot{snapshot}); err != nil {
			return fmt.Errorf("erro ao salvar snapshot: %w", err)
		}
	}

	// Atualizar estatísticas
	c.priorityMu.Lock()
	hpa.LastScan = time.Now()
	hpa.ScanCount++
	c.priorityMu.Unlock()

	log.Debug().
		Str("cluster", hpa.Cluster).
		Str("hpa", hpa.Name).
		Int("scan_count", hpa.ScanCount).
		Msg("📊 HPA prioritário escaneado")

	return nil
}

// GetPriorityHPAs retorna lista de HPAs prioritários
func (c *PriorityCollector) GetPriorityHPAs() []*PriorityHPA {
	c.priorityMu.RLock()
	defer c.priorityMu.RUnlock()

	hpas := make([]*PriorityHPA, 0, len(c.priorityHPAs))
	for _, hpa := range c.priorityHPAs {
		hpas = append(hpas, hpa)
	}
	return hpas
}

// GetStatus retorna status do collector
func (c *PriorityCollector) GetStatus() map[string]interface{} {
	c.priorityMu.RLock()
	priorityCount := len(c.priorityHPAs)
	c.priorityMu.RUnlock()

	c.portMu.Lock()
	usedPorts := len(c.portForwards)
	availPorts := len(c.availPorts)
	c.portMu.Unlock()

	return map[string]interface{}{
		"running":        c.running,
		"priority_hpas":  priorityCount,
		"used_ports":     usedPorts,
		"available_ports": availPorts,
	}
}

// AddTarget adiciona target genérico (compatibilidade - IGNORADO)
func (c *PriorityCollector) AddTarget(target scanner.ScanTarget) {
	log.Debug().
		Str("cluster", target.Cluster).
		Msg("AddTarget chamado mas IGNORADO (use AddPriorityHPA para HPAs prioritários)")
}

// RemoveTarget remove target genérico (compatibilidade)
func (c *PriorityCollector) RemoveTarget(cluster string) {
	// Remove todos os HPAs deste cluster
	c.priorityMu.Lock()
	defer c.priorityMu.Unlock()

	keysToRemove := make([]string, 0)
	for key, hpa := range c.priorityHPAs {
		if hpa.Cluster == cluster {
			keysToRemove = append(keysToRemove, key)
		}
	}

	for _, key := range keysToRemove {
		delete(c.priorityHPAs, key)
	}

	// Libera porta e para port-forward
	if port, exists := c.portForwards[cluster]; exists {
		c.releasePort(cluster)
		if err := c.pfManager.StopWithPort(cluster, port); err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Int("port", port).
				Msg("Erro ao parar port-forward")
		}
	}
}

// GetClusters retorna lista de clusters ativos
func (c *PriorityCollector) GetClusters() []string {
	c.priorityMu.RLock()
	defer c.priorityMu.RUnlock()

	clustersMap := make(map[string]bool)
	for _, hpa := range c.priorityHPAs {
		clustersMap[hpa.Cluster] = true
	}

	clusters := make([]string, 0, len(clustersMap))
	for cluster := range clustersMap {
		clusters = append(clusters, cluster)
	}
	return clusters
}
