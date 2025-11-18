package monitor

import (
	"context"
	"fmt"
	"os"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/rs/zerolog/log"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// TestConfig configuração para testes focados
type TestConfig struct {
	// Target específico
	ClusterContext string // Context do kubeconfig
	Namespace      string
	HPAName        string // Opcional - se vazio, testa todos HPAs do namespace

	// Prometheus
	PrometheusNamespace string // Namespace onde Prometheus está (default: monitoring)
	PrometheusService   string // Nome do serviço Prometheus (default: prometheus-server)
	UsePortForward      bool   // Usar port-forward (default: true)
	LocalPort           int    // Porta local para port-forward (default: 55553)

	// Comportamento
	CollectMetrics bool // Coletar métricas do Prometheus
	ShowHistory    bool // Mostrar histórico de 5min
	Verbose        bool // Logs verbosos
}

// LoadTestConfigFromEnv carrega configuração de variáveis de ambiente
func LoadTestConfigFromEnv() *TestConfig {
	config := &TestConfig{
		ClusterContext:      getEnv("TEST_CLUSTER_CONTEXT", ""),
		Namespace:           getEnv("TEST_NAMESPACE", ""),
		HPAName:             getEnv("TEST_HPA_NAME", ""),
		PrometheusNamespace: getEnv("PROMETHEUS_NAMESPACE", "monitoring"),
		PrometheusService:   getEnv("PROMETHEUS_SERVICE", "prometheus-server"),
		UsePortForward:      getEnvBool("USE_PORT_FORWARD", true),
		LocalPort:           getEnvInt("LOCAL_PORT", 55553),
		CollectMetrics:      getEnvBool("COLLECT_METRICS", true),
		ShowHistory:         getEnvBool("SHOW_HISTORY", false),
		Verbose:             getEnvBool("VERBOSE", false),
	}

	return config
}

// RunTargetedTest executa teste focado em um cluster/namespace/HPA específico
func RunTargetedTest(config *TestConfig) error {
	ctx := context.Background()

	if config.Verbose {
		log.Info().
			Str("cluster", config.ClusterContext).
			Str("namespace", config.Namespace).
			Str("hpa", config.HPAName).
			Msg("Starting targeted test")
	}

	// 1. Cria cluster info
	cluster := &models.ClusterInfo{
		Name:    config.ClusterContext,
		Context: config.ClusterContext,
	}

	// 2. Cria K8s client
	k8sClient, err := NewK8sClient(cluster)
	if err != nil {
		return fmt.Errorf("failed to create K8s client: %w", err)
	}

	// 3. Testa conexão
	log.Info().Msg("Testing K8s connection...")
	if err := k8sClient.TestConnection(ctx); err != nil {
		return fmt.Errorf("K8s connection failed: %w", err)
	}
	log.Info().Msg("✅ K8s connection OK")

	// 4. Prometheus setup is now done at CLI level to avoid import cycle
	// The collector will handle prometheus integration separately

	// 5. Lista HPAs
	log.Info().
		Str("namespace", config.Namespace).
		Msg("Listing HPAs...")

	hpas, err := k8sClient.ListHPAs(ctx, config.Namespace)
	if err != nil {
		return fmt.Errorf("failed to list HPAs: %w", err)
	}

	if len(hpas) == 0 {
		log.Warn().
			Str("namespace", config.Namespace).
			Msg("No HPAs found in namespace")
		return nil
	}

	log.Info().
		Int("count", len(hpas)).
		Msg("HPAs found")

	// 6. Filtra HPA específico (se configurado)
	if config.HPAName != "" {
		var found bool
		var filteredHPAs []autoscalingv2.HorizontalPodAutoscaler
		for _, hpa := range hpas {
			if hpa.Name == config.HPAName {
				filteredHPAs = append(filteredHPAs, hpa)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("HPA %s not found in namespace %s", config.HPAName, config.Namespace)
		}
		hpas = filteredHPAs
	}

	// 7. Coleta dados de cada HPA
	for i, hpa := range hpas {
		log.Info().
			Int("index", i+1).
			Int("total", len(hpas)).
			Str("hpa", hpa.Name).
			Msg("Processing HPA...")

		// Coleta snapshot K8s
		snapshot, err := k8sClient.CollectHPASnapshot(ctx, &hpa)
		if err != nil {
			log.Error().
				Err(err).
				Str("hpa", hpa.Name).
				Msg("Failed to collect snapshot")
			continue
		}

		// Prometheus enrichment is handled at CLI level
		// to avoid import cycles

		// Exibe resultados
		printSnapshot(snapshot, config.ShowHistory)
		fmt.Println()
	}

	log.Info().Msg("✅ Test completed successfully")
	return nil
}

// printSnapshot exibe snapshot formatado
func printSnapshot(s *models.HPASnapshot, showHistory bool) {
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 HPA: %s/%s\n", s.Namespace, s.Name)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("\n")

	// Replicas
	fmt.Printf("🔢 Replicas:\n")
	fmt.Printf("   Min/Max:        %d / %d\n", s.MinReplicas, s.MaxReplicas)
	fmt.Printf("   Current:        %d\n", s.CurrentReplicas)
	fmt.Printf("   Desired:        %d\n", s.DesiredReplicas)
	if s.LastScaleTime != nil {
		fmt.Printf("   Last Scale:     %s\n", s.LastScaleTime.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("\n")

	// Targets
	fmt.Printf("🎯 Targets:\n")
	if s.CPUTarget > 0 {
		fmt.Printf("   CPU Target:     %d%%\n", s.CPUTarget)
	}
	if s.MemoryTarget > 0 {
		fmt.Printf("   Memory Target:  %d%%\n", s.MemoryTarget)
	}
	fmt.Printf("\n")

	// Current Metrics
	fmt.Printf("📈 Current Metrics (%s):\n", s.DataSource)
	if s.CPUCurrent > 0 {
		fmt.Printf("   CPU:            %.2f%%\n", s.CPUCurrent)
	}
	if s.MemoryCurrent > 0 {
		fmt.Printf("   Memory:         %.2f%%\n", s.MemoryCurrent)
	}
	fmt.Printf("\n")

	// Resources
	fmt.Printf("💾 Resources:\n")
	if s.CPURequest != "" {
		fmt.Printf("   CPU Request:    %s\n", s.CPURequest)
	}
	if s.CPULimit != "" {
		fmt.Printf("   CPU Limit:      %s\n", s.CPULimit)
	}
	if s.MemoryRequest != "" {
		fmt.Printf("   Memory Request: %s\n", s.MemoryRequest)
	}
	if s.MemoryLimit != "" {
		fmt.Printf("   Memory Limit:   %s\n", s.MemoryLimit)
	}
	fmt.Printf("\n")

	// Extended Metrics
	if s.RequestRate > 0 || s.ErrorRate > 0 || s.P95Latency > 0 {
		fmt.Printf("🌐 Application Metrics:\n")
		if s.RequestRate > 0 {
			fmt.Printf("   Request Rate:   %.2f req/s\n", s.RequestRate)
		}
		if s.ErrorRate > 0 {
			fmt.Printf("   Error Rate:     %.2f%%\n", s.ErrorRate)
		}
		if s.P95Latency > 0 {
			fmt.Printf("   P95 Latency:    %.2f ms\n", s.P95Latency)
		}
		fmt.Printf("\n")
	}

	// Status
	fmt.Printf("✅ Status:\n")
	fmt.Printf("   Ready:          %v\n", s.Ready)
	fmt.Printf("   Scaling Active: %v\n", s.ScalingActive)
	fmt.Printf("\n")

	// History (se habilitado)
	if showHistory {
		if len(s.CPUHistory) > 0 {
			fmt.Printf("📊 CPU History (last 5min):\n")
			for i, val := range s.CPUHistory {
				fmt.Printf("   T-%ds: %.2f%%\n", (len(s.CPUHistory)-i)*30, val)
			}
			fmt.Printf("\n")
		}

		if len(s.MemoryHistory) > 0 {
			fmt.Printf("📊 Memory History (last 5min):\n")
			for i, val := range s.MemoryHistory {
				fmt.Printf("   T-%ds: %.2f%%\n", (len(s.MemoryHistory)-i)*30, val)
			}
			fmt.Printf("\n")
		}

		if len(s.ReplicaHistory) > 0 {
			fmt.Printf("📊 Replica History (last 5min):\n")
			for i, val := range s.ReplicaHistory {
				fmt.Printf("   T-%ds: %d\n", (len(s.ReplicaHistory)-i)*30, val)
			}
			fmt.Printf("\n")
		}
	}
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var result int
	fmt.Sscanf(value, "%d", &result)
	return result
}
