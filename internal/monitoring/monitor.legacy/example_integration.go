package monitor

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/rs/zerolog/log"
)

// MonitoringSession representa uma sessão de monitoramento ativa
type MonitoringSession struct {
	k8sClients     map[string]*K8sClient // cluster -> client
	portForwardMgr *PortForwardManager
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewMonitoringSession cria uma nova sessão de monitoramento
func NewMonitoringSession(clusters []*models.ClusterInfo) (*MonitoringSession, error) {
	ctx, cancel := context.WithCancel(context.Background())

	session := &MonitoringSession{
		k8sClients:     make(map[string]*K8sClient),
		portForwardMgr: NewPortForwardManager(DefaultLocalPort),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Inicializa clients para cada cluster
	for _, cluster := range clusters {
		client, err := NewK8sClient(cluster)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster.Name).
				Msg("Failed to create K8s client for cluster, skipping")
			continue
		}

		// Testa conexão
		testCtx, testCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := client.TestConnection(testCtx); err != nil {
			testCancel()
			log.Warn().
				Err(err).
				Str("cluster", cluster.Name).
				Msg("Failed to connect to cluster, skipping")
			continue
		}
		testCancel()

		session.k8sClients[cluster.Name] = client
	}

	if len(session.k8sClients) == 0 {
		session.Shutdown()
		return nil, fmt.Errorf("no clusters available")
	}

	log.Info().
		Int("clusters", len(session.k8sClients)).
		Msg("Monitoring session initialized")

	// Inicia heartbeat em background
	go session.heartbeatLoop()

	return session, nil
}

// heartbeatLoop envia heartbeats periódicos para o port-forward manager
func (s *MonitoringSession) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Debug().Msg("Heartbeat loop stopping")
			return

		case <-ticker.C:
			s.portForwardMgr.Heartbeat()
		}
	}
}

// CollectAllHPAs coleta snapshots de todos os HPAs de todos os clusters
func (s *MonitoringSession) CollectAllHPAs() ([]*models.HPASnapshot, error) {
	var allSnapshots []*models.HPASnapshot

	for clusterName, client := range s.k8sClients {
		log.Debug().
			Str("cluster", clusterName).
			Msg("Collecting HPAs from cluster")

		// Lista namespaces
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		namespaces, err := client.ListNamespaces(ctx, []string{})
		cancel()

		if err != nil {
			log.Error().
				Err(err).
				Str("cluster", clusterName).
				Msg("Failed to list namespaces")
			continue
		}

		// Para cada namespace, lista HPAs
		for _, namespace := range namespaces {
			ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
			hpas, err := client.ListHPAs(ctx, namespace)
			cancel()

			if err != nil {
				log.Warn().
					Err(err).
					Str("cluster", clusterName).
					Str("namespace", namespace).
					Msg("Failed to list HPAs")
				continue
			}

			// Coleta snapshot de cada HPA
			for _, hpa := range hpas {
				ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
				snapshot, err := client.CollectHPASnapshot(ctx, &hpa)
				cancel()

				if err != nil {
					log.Warn().
						Err(err).
						Str("cluster", clusterName).
						Str("namespace", namespace).
						Str("hpa", hpa.Name).
						Msg("Failed to collect HPA snapshot")
					continue
				}

				allSnapshots = append(allSnapshots, snapshot)
			}
		}
	}

	log.Info().
		Int("total_snapshots", len(allSnapshots)).
		Msg("HPA collection complete")

	return allSnapshots, nil
}

// SetupPrometheusPortForward configura port-forward para Prometheus em um cluster
func (s *MonitoringSession) SetupPrometheusPortForward(clusterName, namespace string, service string) (string, error) {
	// Verifica se cluster existe
	if _, exists := s.k8sClients[clusterName]; !exists {
		return "", fmt.Errorf("cluster %s not found", clusterName)
	}

	// Inicia port-forward
	endpoint, err := s.portForwardMgr.GetLocalEndpoint(clusterName, namespace, service, 9090)
	if err != nil {
		return "", fmt.Errorf("failed to setup port-forward: %w", err)
	}

	log.Info().
		Str("cluster", clusterName).
		Str("endpoint", endpoint).
		Msg("Prometheus port-forward established")

	return endpoint, nil
}

// GetPortForwardStatus retorna status de todos os port-forwards ativos
func (s *MonitoringSession) GetPortForwardStatus() map[string]interface{} {
	return s.portForwardMgr.GetStatus()
}

// Shutdown encerra a sessão de monitoramento
func (s *MonitoringSession) Shutdown() {
	log.Info().Msg("Shutting down monitoring session")

	// Para heartbeat loop
	s.cancel()

	// Shutdown port-forward manager
	if s.portForwardMgr != nil {
		s.portForwardMgr.Shutdown()
	}

	log.Info().Msg("Monitoring session shutdown complete")
}

// Example usage (commented out para não executar):
/*
func ExampleUsage() {
	// Descobre clusters
	clusters, _ := config.DiscoverClusters(&models.WatchdogConfig{
		AutoDiscoverClusters: true,
	})

	// Cria sessão de monitoramento
	session, err := NewMonitoringSession(clusters)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create monitoring session")
	}
	defer session.Shutdown()

	// Setup port-forward para Prometheus
	prometheusEndpoint, err := session.SetupPrometheusPortForward(
		"production-cluster",
		"monitoring",
		"prometheus-server",
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to setup Prometheus port-forward")
	} else {
		log.Info().Str("endpoint", prometheusEndpoint).Msg("Prometheus ready")
	}

	// Loop de coleta
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			snapshots, err := session.CollectAllHPAs()
			if err != nil {
				log.Error().Err(err).Msg("Failed to collect HPAs")
				continue
			}

			log.Info().
				Int("count", len(snapshots)).
				Msg("HPAs collected")

			// Processar snapshots...
			for _, snapshot := range snapshots {
				fmt.Printf("HPA: %s/%s - Current: %d, Desired: %d\n",
					snapshot.Namespace,
					snapshot.Name,
					snapshot.CurrentReplicas,
					snapshot.DesiredReplicas,
				)
			}
		}
	}
}
*/
