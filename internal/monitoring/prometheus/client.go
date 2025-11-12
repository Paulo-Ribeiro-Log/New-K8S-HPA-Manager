package prometheus

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
)

// Client wrapper para Prometheus API
type Client struct {
	api       v1.API
	cluster   string
	endpoint  string
	timeout   time.Duration
	connected bool
}

// NewClient cria um novo client Prometheus (sem teste de conexão)
// FASE 4: Lazy connection - client inicia desconectado, primeira query testa
func NewClient(cluster, endpoint string) (*Client, error) {
	// Cria client da API Prometheus
	apiClient, err := api.NewClient(api.Config{
		Address: endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	client := &Client{
		api:       v1.NewAPI(apiClient),
		cluster:   cluster,
		endpoint:  endpoint,
		timeout:   10 * time.Second,
		connected: false, // Inicia desconectado, lazy connection na primeira query
	}

	log.Debug().
		Str("cluster", cluster).
		Str("endpoint", endpoint).
		Msg("Prometheus client created (lazy connection)")

	return client, nil
}

// TestConnection testa a conexão com Prometheus
func (c *Client) TestConnection(ctx context.Context) error {
	// Query simples para testar conectividade
	_, _, err := c.api.Query(ctx, "up", time.Now())
	if err != nil {
		c.connected = false
		return fmt.Errorf("connection test failed: %w", err)
	}

	c.connected = true
	log.Debug().
		Str("cluster", c.cluster).
		Str("endpoint", c.endpoint).
		Msg("Prometheus connection test successful")

	return nil
}

// Query executa uma query PromQL
func (c *Client) Query(ctx context.Context, query string) (model.Value, error) {
	if !c.connected {
		return nil, fmt.Errorf("prometheus client not connected")
	}

	result, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if len(warnings) > 0 {
		log.Warn().
			Str("cluster", c.cluster).
			Strs("warnings", warnings).
			Msg("Prometheus query returned warnings")
	}

	return result, nil
}

// QueryRange executa uma range query PromQL
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Value, error) {
	if !c.connected {
		return nil, fmt.Errorf("prometheus client not connected")
	}

	r := v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	}

	result, warnings, err := c.api.QueryRange(ctx, query, r)
	if err != nil {
		return nil, fmt.Errorf("range query failed: %w", err)
	}

	if len(warnings) > 0 {
		log.Warn().
			Str("cluster", c.cluster).
			Strs("warnings", warnings).
			Msg("Prometheus range query returned warnings")
	}

	return result, nil
}

// GetCPUUsage obtém o uso atual de CPU de um HPA
func (c *Client) GetCPUUsage(ctx context.Context, namespace, hpaName string) (float64, error) {
	query := fmt.Sprintf(`
		sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// GetMemoryUsage obtém o uso atual de memória de um HPA
func (c *Client) GetMemoryUsage(ctx context.Context, namespace, hpaName string) (float64, error) {
	query := fmt.Sprintf(`
		sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// GetReplicaHistory obtém histórico de réplicas dos últimos 5 minutos
func (c *Client) GetReplicaHistory(ctx context.Context, namespace, hpaName string) ([]int32, error) {
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	query := fmt.Sprintf(`
		kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}
	`, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 30*time.Second)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesInt32(result)
}

// GetCPUHistory obtém histórico de CPU dos últimos 5 minutos
func (c *Client) GetCPUHistory(ctx context.Context, namespace, hpaName string) ([]float64, error) {
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	query := fmt.Sprintf(`
		sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 30*time.Second)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetMemoryHistory obtém histórico de memória dos últimos 5 minutos
func (c *Client) GetMemoryHistory(ctx context.Context, namespace, hpaName string) ([]float64, error) {
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	query := fmt.Sprintf(`
		sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 30*time.Second)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetRequestRate obtém taxa de requisições
func (c *Client) GetRequestRate(ctx context.Context, namespace, service string) (float64, error) {
	query := fmt.Sprintf(`
		sum(rate(http_requests_total{namespace="%s",service="%s"}[1m]))
	`, namespace, service)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// GetErrorRate obtém taxa de erros (%)
func (c *Client) GetErrorRate(ctx context.Context, namespace, service string) (float64, error) {
	query := fmt.Sprintf(`
		sum(rate(http_requests_total{namespace="%s",service="%s",status=~"5.."}[1m])) /
		sum(rate(http_requests_total{namespace="%s",service="%s"}[1m])) * 100
	`, namespace, service, namespace, service)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// GetP95Latency obtém P95 de latência (ms)
func (c *Client) GetP95Latency(ctx context.Context, namespace, service string) (float64, error) {
	query := fmt.Sprintf(`
		histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="%s",service="%s"}[5m])) by (le)) * 1000
	`, namespace, service)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// GetP99Latency obtém P99 de latência (ms)
func (c *Client) GetP99Latency(ctx context.Context, namespace, service string) (float64, error) {
	query := fmt.Sprintf(`
		histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="%s",service="%s"}[5m])) by (le)) * 1000
	`, namespace, service)

	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	return extractSingleValue(result)
}

// EnrichSnapshot enriquece um snapshot com métricas do Prometheus
// FASE 4: Lazy connection - tenta conectar se ainda não conectado
func (c *Client) EnrichSnapshot(ctx context.Context, snapshot *models.HPASnapshot) error {
	// Lazy connection: tenta conectar na primeira vez
	if !c.connected {
		log.Debug().
			Str("cluster", c.cluster).
			Msg("Attempting lazy connection to Prometheus...")

		if err := c.TestConnection(ctx); err != nil {
			log.Debug().
				Err(err).
				Str("cluster", c.cluster).
				Msg("Prometheus connection failed, skipping enrichment (will retry next scan)")
			return nil // Não é erro fatal, retenta no próximo scan
		}

		log.Info().
			Str("cluster", c.cluster).
			Str("endpoint", c.endpoint).
			Msg("✅ Prometheus lazy connection established")
	}

	// CPU atual
	if cpu, err := c.GetCPUUsage(ctx, snapshot.Namespace, snapshot.Name); err == nil {
		snapshot.CPUCurrent = cpu
		snapshot.DataSource = models.DataSourcePrometheus
	} else {
		log.Debug().
			Err(err).
			Str("namespace", snapshot.Namespace).
			Str("hpa", snapshot.Name).
			Msg("Failed to get CPU usage from Prometheus")
	}

	// Memory atual
	if mem, err := c.GetMemoryUsage(ctx, snapshot.Namespace, snapshot.Name); err == nil {
		snapshot.MemoryCurrent = mem
		snapshot.DataSource = models.DataSourcePrometheus
	}

	// Históricos
	if cpuHistory, err := c.GetCPUHistory(ctx, snapshot.Namespace, snapshot.Name); err == nil {
		snapshot.CPUHistory = cpuHistory
	}

	if memHistory, err := c.GetMemoryHistory(ctx, snapshot.Namespace, snapshot.Name); err == nil {
		snapshot.MemoryHistory = memHistory
	}

	if replicaHistory, err := c.GetReplicaHistory(ctx, snapshot.Namespace, snapshot.Name); err == nil {
		snapshot.ReplicaHistory = replicaHistory
	}

	// Extended metrics (se service name disponível)
	// Nota: assumimos que service = hpa name (comum em muitos casos)
	service := snapshot.Name

	if reqRate, err := c.GetRequestRate(ctx, snapshot.Namespace, service); err == nil {
		snapshot.RequestRate = reqRate
	}

	if errRate, err := c.GetErrorRate(ctx, snapshot.Namespace, service); err == nil {
		snapshot.ErrorRate = errRate
	}

	if latency, err := c.GetP95Latency(ctx, snapshot.Namespace, service); err == nil {
		snapshot.P95Latency = latency
	}

	if latency, err := c.GetP99Latency(ctx, snapshot.Namespace, service); err == nil {
		snapshot.P99Latency = latency
	}

	log.Debug().
		Str("cluster", c.cluster).
		Str("namespace", snapshot.Namespace).
		Str("hpa", snapshot.Name).
		Float64("cpu_current", snapshot.CPUCurrent).
		Float64("memory_current", snapshot.MemoryCurrent).
		Float64("p95_latency", snapshot.P95Latency).
		Float64("p99_latency", snapshot.P99Latency).
		Msg("Snapshot enriched with Prometheus metrics")

	return nil
}

// IsConnected retorna se o client está conectado
func (c *Client) IsConnected() bool {
	return c.connected
}

// GetCluster retorna o nome do cluster
func (c *Client) GetCluster() string {
	return c.cluster
}

// GetEndpoint retorna o endpoint
func (c *Client) GetEndpoint() string {
	return c.endpoint
}

// GetCPUHistoryRange obtém histórico de CPU com range customizável
func (c *Client) GetCPUHistoryRange(ctx context.Context, namespace, hpaName string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetMemoryHistoryRange obtém histórico de memória com range customizável
func (c *Client) GetMemoryHistoryRange(ctx context.Context, namespace, hpaName string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) /
		sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100
	`, namespace, hpaName, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetReplicaHistoryRange obtém histórico de réplicas com range customizável
func (c *Client) GetReplicaHistoryRange(ctx context.Context, namespace, hpaName string, start, end time.Time) ([]int32, error) {
	query := fmt.Sprintf(`
		kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}
	`, namespace, hpaName)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesInt32(result)
}

// GetRequestRateHistory obtém histórico de taxa de requisições
func (c *Client) GetRequestRateHistory(ctx context.Context, namespace, service string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		sum(rate(http_requests_total{namespace="%s",service="%s"}[1m]))
	`, namespace, service)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetErrorRateHistory obtém histórico de taxa de erros (%)
func (c *Client) GetErrorRateHistory(ctx context.Context, namespace, service string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		sum(rate(http_requests_total{namespace="%s",service="%s",status=~"5.."}[1m])) /
		sum(rate(http_requests_total{namespace="%s",service="%s"}[1m])) * 100
	`, namespace, service, namespace, service)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetLatencyP95History obtém histórico de latência P95 (ms)
func (c *Client) GetLatencyP95History(ctx context.Context, namespace, service string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="%s",service="%s"}[5m])) by (le)) * 1000
	`, namespace, service)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// GetLatencyP99History obtém histórico de latência P99 (ms)
func (c *Client) GetLatencyP99History(ctx context.Context, namespace, service string, start, end time.Time) ([]float64, error) {
	query := fmt.Sprintf(`
		histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="%s",service="%s"}[5m])) by (le)) * 1000
	`, namespace, service)

	result, err := c.QueryRange(ctx, query, start, end, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	return extractTimeSeriesFloat64(result)
}

// Helper functions

// extractSingleValue extrai um único valor float64 do resultado
func extractSingleValue(value model.Value) (float64, error) {
	switch v := value.(type) {
	case model.Vector:
		if len(v) == 0 {
			return 0, nil
		}
		return float64(v[0].Value), nil
	case *model.Scalar:
		return float64(v.Value), nil
	default:
		return 0, fmt.Errorf("unexpected value type: %T", value)
	}
}

// extractTimeSeriesFloat64 extrai série temporal como []float64
func extractTimeSeriesFloat64(value model.Value) ([]float64, error) {
	matrix, ok := value.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("expected matrix, got %T", value)
	}

	if len(matrix) == 0 {
		return []float64{}, nil
	}

	// Pega primeira série
	series := matrix[0]
	result := make([]float64, len(series.Values))

	for i, pair := range series.Values {
		result[i] = float64(pair.Value)
	}

	return result, nil
}

// extractTimeSeriesInt32 extrai série temporal como []int32
func extractTimeSeriesInt32(value model.Value) ([]int32, error) {
	matrix, ok := value.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("expected matrix, got %T", value)
	}

	if len(matrix) == 0 {
		return []int32{}, nil
	}

	// Pega primeira série
	series := matrix[0]
	result := make([]int32, len(series.Values))

	for i, pair := range series.Values {
		result[i] = int32(pair.Value)
	}

	return result, nil
}
