package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/monitoring/storage"
)

// HistoricalCollector coleta dados históricos de 3 dias do Prometheus
type HistoricalCollector struct {
	promClient v1.API
	storage    *storage.Persistence
}

// NewHistoricalCollector cria novo collector histórico
func NewHistoricalCollector(promEndpoint string, storage *storage.Persistence) (*HistoricalCollector, error) {
	client, err := api.NewClient(api.Config{
		Address: promEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao criar client Prometheus: %w", err)
	}

	return &HistoricalCollector{
		promClient: v1.NewAPI(client),
		storage:    storage,
	}, nil
}

// CollectBaselineParams parâmetros para coleta de baseline
type CollectBaselineParams struct {
	Cluster     string
	Namespace   string
	HPAName     string
	ServiceName string // Opcional - para queries de aplicação (P95/P99)
}

// CollectBaseline coleta 3 dias de histórico usando TODAS as queries disponíveis
func (hc *HistoricalCollector) CollectBaseline(ctx context.Context, params CollectBaselineParams) error {
	log.Info().
		Str("cluster", params.Cluster).
		Str("namespace", params.Namespace).
		Str("hpa", params.HPAName).
		Str("service", params.ServiceName).
		Msg("Iniciando coleta de baseline histórico (3 dias) - TODAS as métricas")

	// Define range: últimos 3 dias
	end := time.Now()
	start := end.Add(-3 * 24 * time.Hour)

	// Step de 5 minutos = ~864 snapshots em 3 dias
	step := 5 * time.Minute

	log.Info().
		Time("start", start).
		Time("end", end).
		Dur("step", step).
		Int("expected_points", int(3*24*60/5)).
		Msg("Range de coleta histórica")

	// Busca TODOS os templates de queries disponíveis
	templates := prometheus.GetAllTemplates()

	log.Info().
		Int("total_templates", len(templates)).
		Msg("Templates de queries disponíveis")

	// Coleta métricas em paralelo para performance
	allMetrics := make(map[string]map[time.Time]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	successCount := 0
	failureCount := 0

	for _, template := range templates {
		// Verifica se query é aplicável a este HPA
		if !hc.isApplicable(template, params.ServiceName) {
			log.Debug().
				Str("query", template.Name).
				Msg("Query não aplicável, pulando")
			continue
		}

		wg.Add(1)
		go func(tmpl prometheus.QueryTemplate) {
			defer wg.Done()

			// Build query usando QueryBuilder
			query, err := hc.buildQuery(tmpl, params)
			if err != nil {
				log.Warn().
					Err(err).
					Str("query", tmpl.Name).
					Msg("Erro ao construir query, pulando")
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			// Context com timeout de 1 minuto para cada query
			queryCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
			defer cancel()

			// Coleta range data
			data, err := hc.queryRange(queryCtx, query, tmpl.Name, start, end, step)
			if err != nil {
				log.Error().
					Err(err).
					Str("query", tmpl.Name).
					Msg("Query falhou")
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			// Adiciona ao map thread-safe
			mu.Lock()
			allMetrics[tmpl.Name] = data
			successCount++
			mu.Unlock()

			log.Info().
				Str("query", tmpl.Name).
				Int("points", len(data)).
				Msg("Query executada com sucesso")
		}(template)
	}

	// Aguarda todas as queries completarem
	wg.Wait()

	log.Info().
		Int("success", successCount).
		Int("failed", failureCount).
		Int("total", successCount+failureCount).
		Msg("Coleta de métricas concluída")

	if successCount == 0 {
		return fmt.Errorf("nenhuma métrica coletada com sucesso")
	}

	// Mescla TODAS as métricas em snapshots
	snapshots := hc.mergeAllMetrics(params.Cluster, params.Namespace, params.HPAName, allMetrics)

	// Valida cobertura de dados (mínimo 70%)
	expectedPoints := int(3 * 24 * 60 / 5) // 864 snapshots esperados
	minPoints := int(float64(expectedPoints) * 0.7)

	if len(snapshots) < minPoints {
		log.Warn().
			Int("collected", len(snapshots)).
			Int("minimum", minPoints).
			Float64("coverage", float64(len(snapshots))/float64(expectedPoints)*100).
			Msg("Cobertura abaixo de 70%, baseline pode ser incompleto")
	}

	log.Info().
		Int("snapshots", len(snapshots)).
		Int("expected", expectedPoints).
		Float64("coverage", float64(len(snapshots))/float64(expectedPoints)*100).
		Int("metrics_per_snapshot", successCount).
		Msg("Snapshots históricos gerados")

	// Salva no SQLite como baseline
	saved, err := hc.storage.SaveHistoricalBaseline(snapshots)
	if err != nil {
		return fmt.Errorf("falha ao salvar baseline no SQLite: %w", err)
	}

	log.Info().
		Str("cluster", params.Cluster).
		Str("namespace", params.Namespace).
		Str("hpa", params.HPAName).
		Int("saved", saved).
		Int("total", len(snapshots)).
		Float64("success_rate", float64(saved)/float64(len(snapshots))*100).
		Msg("Baseline histórico salvo com sucesso")

	return nil
}

// isApplicable verifica se query template é aplicável a este HPA
func (hc *HistoricalCollector) isApplicable(template prometheus.QueryTemplate, serviceName string) bool {
	// Queries que requerem service name
	applicationQueries := map[string]bool{
		"request_rate": true,
		"error_rate":   true,
		"p95_latency":  true,
		"p99_latency":  true,
	}

	// Se query precisa de service mas não foi fornecido, skip
	if applicationQueries[template.Name] && serviceName == "" {
		return false
	}

	return true
}

// buildQuery constrói query usando QueryBuilder
func (hc *HistoricalCollector) buildQuery(template prometheus.QueryTemplate, params CollectBaselineParams) (string, error) {
	builder := prometheus.NewQueryBuilder(template).
		WithNamespace(params.Namespace).
		WithHPAName(params.HPAName)

	// Adiciona service se disponível
	if params.ServiceName != "" {
		builder.WithService(params.ServiceName)
	}

	query, err := builder.Build()
	if err != nil {
		return "", fmt.Errorf("erro ao construir query %s: %w", template.Name, err)
	}

	return query, nil
}

// queryRange executa query range no Prometheus
func (hc *HistoricalCollector) queryRange(
	ctx context.Context,
	query, metricName string,
	start, end time.Time,
	step time.Duration,
) (map[time.Time]float64, error) {

	log.Debug().
		Str("metric", metricName).
		Str("query", query).
		Msg("Executando query range")

	// Executa query range
	result, warnings, err := hc.promClient.QueryRange(ctx, query, v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})

	if err != nil {
		return nil, fmt.Errorf("erro ao executar query: %w", err)
	}

	if len(warnings) > 0 {
		log.Warn().
			Strs("warnings", warnings).
			Str("metric", metricName).
			Msg("Warnings da query Prometheus")
	}

	// Parse resultado
	data := make(map[time.Time]float64)

	if matrix, ok := result.(model.Matrix); ok {
		for _, series := range matrix {
			for _, sample := range series.Values {
				timestamp := sample.Timestamp.Time()
				value := float64(sample.Value)
				data[timestamp] = value
			}
		}
	}

	log.Debug().
		Str("metric", metricName).
		Int("points", len(data)).
		Msg("Query range concluída")

	return data, nil
}

// mergeAllMetrics mescla TODAS as métricas coletadas em snapshots
func (hc *HistoricalCollector) mergeAllMetrics(
	cluster, namespace, hpaName string,
	allMetrics map[string]map[time.Time]float64,
) []*models.HPASnapshot {

	// Cria map de timestamps únicos
	timestamps := make(map[time.Time]bool)
	for _, metricData := range allMetrics {
		for t := range metricData {
			timestamps[t] = true
		}
	}

	log.Info().
		Int("unique_timestamps", len(timestamps)).
		Int("metrics_collected", len(allMetrics)).
		Msg("Mesclando métricas em snapshots")

	// Cria snapshots
	snapshots := make([]*models.HPASnapshot, 0, len(timestamps))

	for timestamp := range timestamps {
		snapshot := &models.HPASnapshot{
			Cluster:   cluster,
			Namespace: namespace,
			Name:      hpaName,
			Timestamp: timestamp,

			// Métricas core (campos principais)
			CPUCurrent:      allMetrics["cpu_usage"][timestamp],
			MemoryCurrent:   allMetrics["memory_usage"][timestamp],
			CurrentReplicas: int32(allMetrics["hpa_current_replicas"][timestamp]),
			DesiredReplicas: int32(allMetrics["hpa_desired_replicas"][timestamp]),

			// Targets (podem ser zero se não configurados)
			CPUTarget:    0, // Será preenchido pelo analyzer
			MemoryTarget: 0,

			// Métricas adicionais em JSON
			AdditionalMetrics: hc.buildAdditionalMetrics(allMetrics, timestamp),
		}

		snapshots = append(snapshots, snapshot)
	}

	log.Info().
		Int("snapshots_created", len(snapshots)).
		Msg("Snapshots mesclados com sucesso")

	return snapshots
}

// buildAdditionalMetrics cria map de métricas adicionais para JSON
func (hc *HistoricalCollector) buildAdditionalMetrics(
	allMetrics map[string]map[time.Time]float64,
	timestamp time.Time,
) map[string]interface{} {

	additional := make(map[string]interface{})

	// Lista de métricas adicionais (não core)
	additionalMetricNames := []string{
		"cpu_usage_raw",
		"cpu_throttling",
		"memory_usage_raw",
		"memory_oom",
		"hpa_replica_delta",
		"network_rx_bytes",
		"network_tx_bytes",
		"request_rate",
		"error_rate",
		"p95_latency",
		"p99_latency",
		"pod_restart_count",
		"pod_ready_count",
	}

	for _, metricName := range additionalMetricNames {
		if metricData, exists := allMetrics[metricName]; exists {
			if value, hasTimestamp := metricData[timestamp]; hasTimestamp {
				additional[metricName] = value
			}
		}
	}

	return additional
}

// GetMetricsJSON serializa métricas adicionais em JSON para persistência
func GetMetricsJSON(metrics map[string]interface{}) (string, error) {
	if len(metrics) == 0 {
		return "{}", nil
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
