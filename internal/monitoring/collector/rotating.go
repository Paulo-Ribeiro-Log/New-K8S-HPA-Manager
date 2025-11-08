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

// ClusterTarget representa um cluster e seus HPAs monitorados
type ClusterTarget struct {
	Cluster    string
	Namespaces []string
	HPAs       []string
}

// RotatingCollector gerencia coleta rotativa de métricas
// Usa 6 portas (55551-55556) para rotacionar entre N clusters
type RotatingCollector struct {
	// Configuração
	clusters     []string      // Lista de clusters ativos
	targets      map[string]*ClusterTarget // Cluster -> Target mapping
	ports        []int         // [55551, 55552, 55553, 55554, 55555, 55556]
	slotDuration time.Duration // Calculado: 60s / totalSlots

	// Estado
	currentSlot int
	totalSlots  int // ceil(len(clusters) / len(ports))

	// Dependências
	persistence *storage.Persistence
	pfManager   *portforward.PortForwardManager
	kubeManager *config.KubeConfigManager

	// Controle
	running bool
	stopCh  chan struct{}
	mu      sync.RWMutex
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewRotatingCollector cria novo collector com 6 portas
func NewRotatingCollector(
	persistence *storage.Persistence,
	pfManager *portforward.PortForwardManager,
	kubeManager *config.KubeConfigManager,
) *RotatingCollector {
	ctx, cancel := context.WithCancel(context.Background())

	return &RotatingCollector{
		clusters:     make([]string, 0),
		targets:      make(map[string]*ClusterTarget),
		ports:        []int{55551, 55552, 55553, 55554, 55555, 55556},
		slotDuration: 10 * time.Second, // Padrão: 10s (será recalculado)
		currentSlot:  0,
		totalSlots:   1,
		persistence:  persistence,
		pfManager:    pfManager,
		kubeManager:  kubeManager,
		stopCh:       make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start inicia rotação
func (c *RotatingCollector) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("collector já está rodando")
	}
	c.running = true
	c.mu.Unlock()

	log.Info().
		Int("ports", len(c.ports)).
		Int("clusters", len(c.clusters)).
		Dur("slot_duration", c.slotDuration).
		Msg("🔄 RotatingCollector iniciado")

	c.wg.Add(1)
	go c.rotationLoop()

	return nil
}

// Stop para rotação (graceful)
func (c *RotatingCollector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	log.Info().Msg("Parando RotatingCollector...")

	// Sinaliza stop
	close(c.stopCh)
	c.cancel()

	// Aguarda goroutines terminarem
	c.wg.Wait()

	// Para todos os port-forwards
	if err := c.pfManager.StopAll(); err != nil {
		log.Warn().Err(err).Msg("Erro ao parar port-forwards")
	}

	log.Info().Msg("✅ RotatingCollector parado")
}

// AddTarget adiciona cluster/HPA à rotação
func (c *RotatingCollector) AddTarget(target scanner.ScanTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verifica se cluster já existe
	if existingTarget, exists := c.targets[target.Cluster]; exists {
		// Atualiza target existente
		existingTarget.Namespaces = target.Namespaces
		existingTarget.HPAs = target.HPAs
		log.Info().
			Str("cluster", target.Cluster).
			Int("hpas", len(target.HPAs)).
			Msg("Target atualizado no RotatingCollector")
	} else {
		// Novo cluster
		c.targets[target.Cluster] = &ClusterTarget{
			Cluster:    target.Cluster,
			Namespaces: target.Namespaces,
			HPAs:       target.HPAs,
		}
		c.clusters = append(c.clusters, target.Cluster)

		log.Info().
			Str("cluster", target.Cluster).
			Int("hpas", len(target.HPAs)).
			Msg("Novo cluster adicionado ao RotatingCollector")
	}

	// Recalcula slots e duração
	c.recalculateSlots()
}

// RemoveTarget remove cluster da rotação
func (c *RotatingCollector) RemoveTarget(cluster string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove do map
	delete(c.targets, cluster)

	// Remove da lista de clusters
	newClusters := make([]string, 0)
	for _, cl := range c.clusters {
		if cl != cluster {
			newClusters = append(newClusters, cl)
		}
	}
	c.clusters = newClusters

	log.Info().
		Str("cluster", cluster).
		Msg("Cluster removido do RotatingCollector")

	// Recalcula slots e duração
	c.recalculateSlots()

	// Para port-forward deste cluster
	if err := c.pfManager.Stop(cluster); err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Erro ao parar port-forward do cluster removido")
	}
}

// recalculateSlots recalcula totalSlots e slotDuration
func (c *RotatingCollector) recalculateSlots() {
	numClusters := len(c.clusters)
	numPorts := len(c.ports)

	if numClusters == 0 {
		c.totalSlots = 1
		c.slotDuration = 60 * time.Second
		return
	}

	// Total de slots = ceil(numClusters / numPorts)
	c.totalSlots = (numClusters + numPorts - 1) / numPorts

	// Duração de cada slot = 60s / totalSlots
	c.slotDuration = 60 * time.Second / time.Duration(c.totalSlots)

	log.Info().
		Int("clusters", numClusters).
		Int("total_slots", c.totalSlots).
		Dur("slot_duration", c.slotDuration).
		Msg("Slots recalculados")
}

// rotationLoop loop principal de rotação
func (c *RotatingCollector) rotationLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.slotDuration)
	defer ticker.Stop()

	log.Info().Msg("🔄 Loop de rotação iniciado")

	for {
		select {
		case <-c.ctx.Done():
			log.Info().Msg("Loop de rotação encerrado (context cancelled)")
			return
		case <-c.stopCh:
			log.Info().Msg("Loop de rotação encerrado (stopCh)")
			return
		case <-ticker.C:
			// Executa coleta do slot atual
			c.collectSlot(c.currentSlot)

			// Avança para próximo slot
			c.mu.Lock()
			c.currentSlot = (c.currentSlot + 1) % c.totalSlots

			// Recria ticker se slotDuration mudou (clusters adicionados/removidos)
			ticker.Reset(c.slotDuration)
			c.mu.Unlock()
		}
	}
}

// collectSlot executa coleta de 1 slot (até 6 clusters em paralelo)
func (c *RotatingCollector) collectSlot(slotIndex int) {
	c.mu.RLock()
	numPorts := len(c.ports)
	numClusters := len(c.clusters)
	c.mu.RUnlock()

	if numClusters == 0 {
		log.Debug().Msg("Nenhum cluster para coletar")
		return
	}

	// Calcula quais clusters pertencem a este slot
	startIdx := slotIndex * numPorts
	endIdx := startIdx + numPorts
	if endIdx > numClusters {
		endIdx = numClusters
	}

	c.mu.RLock()
	clustersInSlot := c.clusters[startIdx:endIdx]
	c.mu.RUnlock()

	log.Debug().
		Int("slot_index", slotIndex).
		Int("clusters_count", len(clustersInSlot)).
		Strs("clusters", clustersInSlot).
		Msg("Coletando slot")

	// Coleta paralela dos clusters deste slot
	var wg sync.WaitGroup
	for i, cluster := range clustersInSlot {
		port := c.ports[i]
		wg.Add(1)

		go func(cl string, p int) {
			defer wg.Done()
			if err := c.collectCluster(cl, p); err != nil {
				log.Error().
					Err(err).
					Str("cluster", cl).
					Int("port", p).
					Msg("Erro ao coletar cluster")
			}
		}(cluster, port)
	}

	// Aguarda todas as coletas do slot terminarem
	wg.Wait()

	log.Debug().
		Int("slot_index", slotIndex).
		Msg("Slot coletado com sucesso")
}

// collectCluster coleta métricas de 1 cluster
func (c *RotatingCollector) collectCluster(cluster string, port int) error {
	// Contexto com timeout de 30s por cluster
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	// Verifica se foi cancelado
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	log.Debug().
		Str("cluster", cluster).
		Int("port", port).
		Msg("Iniciando coleta de cluster")

	// 1. Inicia port-forward temporário
	if err := c.pfManager.Start(cluster); err != nil {
		return fmt.Errorf("falha ao iniciar port-forward: %w", err)
	}
	defer c.pfManager.Stop(cluster)

	// FASE 4: Aguarda port-forward estar realmente pronto (grace period)
	// O waitForReady() interno do PortForward já valida, mas damos mais 2s de margem
	// para garantir que Prometheus está respondendo antes de criar o client
	time.Sleep(2 * time.Second)

	// 2. Busca target deste cluster
	c.mu.RLock()
	target, exists := c.targets[cluster]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("target não encontrado para cluster %s", cluster)
	}

	// 3. FASE 3: Coletar métricas via Prometheus
	// FASE 4: Usar URL real do PortForwardManager (porta dinâmica 55553 ou 55554)
	promEndpoint := c.pfManager.GetURL(cluster)
	if promEndpoint == "" {
		log.Warn().
			Str("cluster", cluster).
			Msg("Port-forward não ativo, pulando coleta")
		return nil // Não é erro fatal, apenas pula este cluster
	}

	promClient, err := prometheus.NewClient(cluster, promEndpoint)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Falha ao criar cliente Prometheus, pulando coleta")
		return nil // Não é erro fatal, apenas pula este cluster
	}

	// 4. Cria K8s clientset para coletar dados do K8s API
	// Nota: clusters no kubeconfig têm sufixo "-admin"
	clusterContext := cluster
	if !strings.HasSuffix(cluster, "-admin") {
		clusterContext = cluster + "-admin"
	}

	clientset, err := c.kubeManager.GetClient(clusterContext)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Str("context", clusterContext).
			Msg("Failed to get K8s client, snapshots will have no Request/Limit data")
		// Não é erro fatal, continua sem dados do K8s
		clientset = nil
	}

	// 5. Coleta métricas de cada HPA
	snapshots := make([]*models.HPASnapshot, 0)

	for _, ns := range target.Namespaces {
		for _, hpaName := range target.HPAs {
			// Cria snapshot básico
			snapshot := &models.HPASnapshot{
				Cluster:   cluster,
				Namespace: ns,
				Name:      hpaName,
				Timestamp: time.Now(),
			}

			// Enriquece com dados do K8s API se clientset disponível
			if clientset != nil {
				c.enrichSnapshotWithK8sData(ctx, clientset, snapshot)
			}

			// Enriquece snapshot com métricas do Prometheus
			if err := promClient.EnrichSnapshot(ctx, snapshot); err != nil {
				log.Debug().
					Err(err).
					Str("cluster", cluster).
					Str("namespace", ns).
					Str("hpa", hpaName).
					Msg("Falha ao enriquecer snapshot com Prometheus (pode ser normal se não tem dados)")
			}

			snapshots = append(snapshots, snapshot)
		}
	}

	// Salva snapshots no SQLite (batch)
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
				Msg("📊 Métricas coletadas e salvas no SQLite")
		}
	}

	return nil
}

// GetClusters retorna lista de clusters ativos
func (c *RotatingCollector) GetClusters() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clusters := make([]string, len(c.clusters))
	copy(clusters, c.clusters)
	return clusters
}

// GetStatus retorna status do collector
func (c *RotatingCollector) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"running":       c.running,
		"clusters":      len(c.clusters),
		"total_slots":   c.totalSlots,
		"current_slot":  c.currentSlot,
		"slot_duration": c.slotDuration.String(),
		"ports":         c.ports,
	}
}

// CollectBaseline coleta histórico de 3 dias para um HPA (assíncrono)
// FASE 3: Implementação de baseline inteligente
func (c *RotatingCollector) CollectBaseline(cluster, namespace, hpaName string) {
	// Executa em goroutine para não bloquear
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		log.Info().
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("📊 Iniciando coleta de baseline histórico (3 dias)...")

		// Contexto com timeout de 5 minutos para toda a operação
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Minute)
		defer cancel()

		// 1. Criar port-forward temporário
		if err := c.pfManager.Start(cluster); err != nil {
			log.Error().
				Err(err).
				Str("cluster", cluster).
				Msg("Falha ao criar port-forward para baseline")
			return
		}
		defer c.pfManager.Stop(cluster)

		// Aguarda port-forward estar pronto
		time.Sleep(2 * time.Second)

		// 2. Criar cliente Prometheus
		// Usa primeira porta disponível (55551) - port-forward manager gerencia
		promEndpoint := "http://localhost:55551"
		promClient, err := prometheus.NewClient(cluster, promEndpoint)
		if err != nil {
			log.Error().
				Err(err).
				Str("cluster", cluster).
				Msg("Falha ao criar cliente Prometheus para baseline")
			return
		}

		// 3. Definir range de 3 dias
		end := time.Now()
		start := end.Add(-72 * time.Hour) // 3 dias
		step := 1 * time.Minute            // Coleta a cada 1 minuto

		// Total esperado: 72h * 60min = 4320 pontos
		totalPoints := int(72 * 60)
		log.Info().
			Str("cluster", cluster).
			Str("hpa", hpaName).
			Time("start", start).
			Time("end", end).
			Int("expected_points", totalPoints).
			Msg("Coletando baseline de 3 dias...")

		// 4. Coletar métricas históricas via QueryRange
		snapshots := make([]*models.HPASnapshot, 0, totalPoints)

		// Query para histórico de réplicas
		replicasQuery := fmt.Sprintf(`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`, namespace, hpaName)
		replicasResult, err := promClient.QueryRange(ctx, replicasQuery, start, end, step)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Str("hpa", hpaName).
				Msg("Falha ao coletar histórico de réplicas (pode ser normal se HPA é novo)")
		}

		// Query para histórico de CPU
		cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`, namespace, hpaName, namespace, hpaName)
		cpuResult, err := promClient.QueryRange(ctx, cpuQuery, start, end, step)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Str("hpa", hpaName).
				Msg("Falha ao coletar histórico de CPU")
		}

		// Query para histórico de memória
		memoryQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`, namespace, hpaName, namespace, hpaName)
		memoryResult, err := promClient.QueryRange(ctx, memoryQuery, start, end, step)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Str("hpa", hpaName).
				Msg("Falha ao coletar histórico de memória")
		}

		// Converter resultados para snapshots
		// Usa timestamp de réplicas como base (métrica mais confiável)
		if matrix, ok := replicasResult.(model.Matrix); ok {
			for _, sample := range matrix {
				for _, value := range sample.Values {
					timestamp := time.Unix(int64(value.Timestamp)/1000, 0)

					snapshot := &models.HPASnapshot{
						Cluster:   cluster,
						Namespace: namespace,
						Name:      hpaName,
						Timestamp: timestamp,
					}

					// Preencher réplicas
					snapshot.CurrentReplicas = int32(float64(value.Value))

					// Buscar CPU correspondente
					if cpuMatrix, ok := cpuResult.(model.Matrix); ok {
						if len(cpuMatrix) > 0 {
							// Encontra valor mais próximo do timestamp
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

		if len(snapshots) == 0 {
			log.Warn().
				Str("cluster", cluster).
				Str("hpa", hpaName).
				Msg("Nenhum dado histórico encontrado (HPA pode ser novo)")
			return
		}

		// 5. Batch insert no SQLite
		if c.persistence != nil {
			if err := c.persistence.SaveSnapshots(snapshots); err != nil {
				log.Error().
					Err(err).
					Str("cluster", cluster).
					Str("hpa", hpaName).
					Int("snapshots", len(snapshots)).
					Msg("Erro ao salvar baseline no SQLite")
				return
			}

			// Marcar baseline como pronto
			if err := c.persistence.MarkBaselineReady(cluster, namespace, hpaName); err != nil {
				log.Error().
					Err(err).
					Str("cluster", cluster).
					Str("hpa", hpaName).
					Msg("Erro ao marcar baseline como pronto")
				return
			}

			log.Info().
				Str("cluster", cluster).
				Str("namespace", namespace).
				Str("hpa", hpaName).
				Int("snapshots", len(snapshots)).
				Dur("period", end.Sub(start)).
				Msg("✅ Baseline histórico coletado e salvo")
		}
	}()
}
