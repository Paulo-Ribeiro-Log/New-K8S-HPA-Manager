package monitor

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"github.com/rs/zerolog/log"
)

// BaselineCollector coleta baseline antes do stress test
type BaselineCollector struct {
	promClient *prometheus.Client
	k8sClient  *K8sClient
}

// NewBaselineCollector cria novo baseline collector
func NewBaselineCollector(promClient *prometheus.Client, k8sClient *K8sClient) *BaselineCollector {
	return &BaselineCollector{
		promClient: promClient,
		k8sClient:  k8sClient,
	}
}

// CaptureBaseline captura baseline de todos os HPAs em um cluster
func (bc *BaselineCollector) CaptureBaseline(ctx context.Context, duration time.Duration) (*models.BaselineSnapshot, error) {
	log.Info().
		Dur("duration", duration).
		Msg("Capturing baseline snapshot")

	snapshot := &models.BaselineSnapshot{
		Timestamp:    time.Now(),
		Duration:     duration,
		HPABaselines: make(map[string]*models.HPABaseline),
	}

	// Busca todos os namespaces (sem excluir nenhum)
	namespaces, err := bc.k8sClient.ListNamespaces(ctx, []string{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Busca todos os HPAs de todos os namespaces
	var hpas []*models.HPASnapshot
	for _, ns := range namespaces {
		// Lista HPAs do namespace
		hpaList, err := bc.k8sClient.ListHPAs(ctx, ns)
		if err != nil {
			log.Warn().
				Err(err).
				Str("namespace", ns).
				Msg("Failed to list HPAs from namespace, skipping")
			continue
		}

		// Converte cada HPA em snapshot
		for i := range hpaList {
			snapshot, err := bc.k8sClient.CollectHPASnapshot(ctx, &hpaList[i])
			if err != nil {
				log.Warn().
					Err(err).
					Str("namespace", ns).
					Str("hpa", hpaList[i].Name).
					Msg("Failed to collect HPA snapshot, skipping")
				continue
			}
			hpas = append(hpas, snapshot)
		}
	}

	snapshot.TotalHPAs = len(hpas)

	// Coleta baseline para cada HPA
	var totalReplicas int32
	var cpuValues, memoryValues, replicaValues []float64

	for _, hpa := range hpas {
		hpaBaseline, err := bc.captureHPABaseline(ctx, hpa, duration)
		if err != nil {
			log.Warn().
				Err(err).
				Str("namespace", hpa.Namespace).
				Str("hpa", hpa.Name).
				Msg("Failed to capture HPA baseline, skipping")
			continue
		}

		key := fmt.Sprintf("%s/%s/%s", bc.promClient.GetCluster(), hpa.Namespace, hpa.Name)
		snapshot.HPABaselines[key] = hpaBaseline

		// Acumula para estatísticas globais
		totalReplicas += hpaBaseline.CurrentReplicas
		cpuValues = append(cpuValues, hpaBaseline.CPUAvg)
		memoryValues = append(memoryValues, hpaBaseline.MemoryAvg)
		replicaValues = append(replicaValues, float64(hpaBaseline.CurrentReplicas))
	}

	snapshot.TotalReplicas = int(totalReplicas)

	// Calcula estatísticas globais
	if len(cpuValues) > 0 {
		snapshot.CPUAvg = calculateAvg(cpuValues)
		snapshot.CPUMax = calculateMax(cpuValues)
		snapshot.CPUMin = calculateMin(cpuValues)
		snapshot.CPUP95 = calculateP95(cpuValues)
	}

	if len(memoryValues) > 0 {
		snapshot.MemoryAvg = calculateAvg(memoryValues)
		snapshot.MemoryMax = calculateMax(memoryValues)
		snapshot.MemoryMin = calculateMin(memoryValues)
		snapshot.MemoryP95 = calculateP95(memoryValues)
	}

	if len(replicaValues) > 0 {
		snapshot.ReplicasAvg = calculateAvg(replicaValues)
		snapshot.ReplicasMax = int32(calculateMax(replicaValues))
		snapshot.ReplicasMin = int32(calculateMin(replicaValues))
	}

	log.Info().
		Int("total_hpas", snapshot.TotalHPAs).
		Int("total_replicas", snapshot.TotalReplicas).
		Float64("cpu_avg", snapshot.CPUAvg).
		Float64("memory_avg", snapshot.MemoryAvg).
		Msg("Baseline captured successfully")

	return snapshot, nil
}

// captureHPABaseline captura baseline de um HPA específico
func (bc *BaselineCollector) captureHPABaseline(ctx context.Context, hpa *models.HPASnapshot, duration time.Duration) (*models.HPABaseline, error) {
	baseline := &models.HPABaseline{
		Cluster:         bc.promClient.GetCluster(),
		Namespace:       hpa.Namespace,
		Name:            hpa.Name,
		MinReplicas:     hpa.MinReplicas,
		MaxReplicas:     hpa.MaxReplicas,
		TargetCPU:       hpa.CPUTarget,
		CurrentReplicas: hpa.CurrentReplicas,
		Timestamp:       time.Now(),
	}

	// Calcula período de observação
	end := time.Now()
	start := end.Add(-duration)

	// Busca histórico de CPU do Prometheus
	cpuHistory, err := bc.promClient.GetCPUHistoryRange(ctx, hpa.Namespace, hpa.Name, start, end)
	if err != nil {
		log.Debug().
			Err(err).
			Str("hpa", hpa.Name).
			Msg("Failed to get CPU history, using current value")
		cpuHistory = []float64{hpa.CPUCurrent}
	}

	if len(cpuHistory) > 0 {
		baseline.CPUAvg = calculateAvg(cpuHistory)
		baseline.CPUMax = calculateMax(cpuHistory)
		baseline.CPUMin = calculateMin(cpuHistory)
	}

	// Busca histórico de Memória
	memoryHistory, err := bc.promClient.GetMemoryHistoryRange(ctx, hpa.Namespace, hpa.Name, start, end)
	if err != nil {
		log.Debug().
			Err(err).
			Str("hpa", hpa.Name).
			Msg("Failed to get memory history, using current value")
		memoryHistory = []float64{hpa.MemoryCurrent}
	}

	if len(memoryHistory) > 0 {
		baseline.MemoryAvg = calculateAvg(memoryHistory)
		baseline.MemoryMax = calculateMax(memoryHistory)
		baseline.MemoryMin = calculateMin(memoryHistory)
	}

	// Busca histórico de Réplicas
	replicaHistory, err := bc.promClient.GetReplicaHistoryRange(ctx, hpa.Namespace, hpa.Name, start, end)
	if err != nil {
		log.Debug().
			Err(err).
			Str("hpa", hpa.Name).
			Msg("Failed to get replica history, using current value")
		replicaHistory = []int32{hpa.CurrentReplicas}
	}

	if len(replicaHistory) > 0 {
		replicaFloat := make([]float64, len(replicaHistory))
		for i, r := range replicaHistory {
			replicaFloat[i] = float64(r)
		}
		baseline.ReplicasAvg = calculateAvg(replicaFloat)
		baseline.ReplicasMax = int32(calculateMax(replicaFloat))
		baseline.ReplicasMin = int32(calculateMin(replicaFloat))
		baseline.ReplicasStdDev = calculateStdDev(replicaFloat, baseline.ReplicasAvg)
	}

	// Tenta buscar métricas de aplicação (request rate, error rate, latency)
	// Nota: Isso pode falhar se métricas não estiverem disponíveis
	if svc := findServiceForHPA(hpa); svc != "" {
		// Request rate
		if reqRate, err := bc.promClient.GetRequestRateHistory(ctx, hpa.Namespace, svc, start, end); err == nil && len(reqRate) > 0 {
			baseline.RequestRateAvg = calculateAvg(reqRate)
		}

		// Error rate
		if errRate, err := bc.promClient.GetErrorRateHistory(ctx, hpa.Namespace, svc, start, end); err == nil && len(errRate) > 0 {
			baseline.ErrorRateAvg = calculateAvg(errRate)
		}

		// Latency P95
		if latency, err := bc.promClient.GetLatencyP95History(ctx, hpa.Namespace, svc, start, end); err == nil && len(latency) > 0 {
			baseline.LatencyP95Avg = calculateAvg(latency)
		}
	}

	// Avalia se HPA estava saudável no baseline
	baseline.Healthy = bc.evaluateHPAHealth(baseline, hpa)

	return baseline, nil
}

// evaluateHPAHealth avalia se HPA estava saudável no período de baseline
func (bc *BaselineCollector) evaluateHPAHealth(baseline *models.HPABaseline, hpa *models.HPASnapshot) bool {
	// HPA não saudável se:
	// 1. Estava no limite (maxReplicas) com CPU alta
	if baseline.CurrentReplicas >= baseline.MaxReplicas && baseline.CPUAvg > 80 {
		baseline.Notes = "HPA estava no limite com CPU alta durante baseline"
		return false
	}

	// 2. CPU consistentemente muito alta
	if baseline.CPUAvg > 85 {
		baseline.Notes = "CPU muito alta durante baseline"
		return false
	}

	// 3. Oscilação excessiva de réplicas
	if baseline.ReplicasStdDev > 2.0 {
		baseline.Notes = "Oscilação excessiva de réplicas durante baseline"
		return false
	}

	// 4. Taxa de erros alta
	if baseline.ErrorRateAvg > 1.0 { // > 1%
		baseline.Notes = "Taxa de erros alta durante baseline"
		return false
	}

	return true
}

// findServiceForHPA tenta encontrar o nome do serviço associado ao HPA
// Nota: Implementação simplificada - pode ser melhorada
func findServiceForHPA(hpa *models.HPASnapshot) string {
	// Normalmente o serviço tem o mesmo nome do HPA ou deployment
	// Isso é uma convenção comum, mas pode variar
	return hpa.Name
}

// Funções auxiliares de cálculo estatístico

func calculateAvg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func calculateMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
	}
	return min
}

func calculateP95(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Ordena valores
	sorted := make([]float64, len(values))
	copy(sorted, values)

	// Bubble sort simples (ok para datasets pequenos)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Retorna valor no percentil 95
	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func calculateStdDev(values []float64, avg float64) float64 {
	if len(values) == 0 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		diff := v - avg
		variance += diff * diff
	}
	variance /= float64(len(values))

	// Raiz quadrada da variância
	return sqrt(variance)
}

// sqrt implementação simples de raiz quadrada (método de Newton)
func sqrt(x float64) float64 {
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}
