package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"k8s-hpa-manager/internal/monitoring/discovery"

	"github.com/rs/zerolog/log"
)

// PrometheusClient cliente HTTP para comunicação com Prometheus
type PrometheusClient struct {
	endpoint   *discovery.PrometheusEndpoint
	httpClient *http.Client
}

// QueryResult resultado de uma query Prometheus
type QueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"` // [timestamp, "value"]
		} `json:"result"`
	} `json:"data"`
}

// QueryRangeResult resultado de uma query_range Prometheus
type QueryRangeResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"` // [[timestamp, "value"], ...]
		} `json:"result"`
	} `json:"data"`
}

// formatBytes converte bytes para formato humanizado (Mi/Gi)
func formatBytes(bytes float64) string {
	const (
		Ki = 1024
		Mi = Ki * 1024
		Gi = Mi * 1024
	)

	switch {
	case bytes >= Gi:
		return fmt.Sprintf("%.0fGi", bytes/Gi)
	case bytes >= Mi:
		return fmt.Sprintf("%.0fMi", bytes/Mi)
	case bytes >= Ki:
		return fmt.Sprintf("%.0fKi", bytes/Ki)
	default:
		return fmt.Sprintf("%.0f", bytes)
	}
}

// NewPrometheusClient cria um novo cliente Prometheus
func NewPrometheusClient(cluster string) (*PrometheusClient, error) {
	endpoint, err := discovery.DiscoverEndpoint(cluster)
	if err != nil {
		return nil, fmt.Errorf("falha ao descobrir endpoint: %w", err)
	}

	if !endpoint.Available {
		return nil, fmt.Errorf("endpoint não disponível: %s", endpoint.URL)
	}

	client := &PrometheusClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // Certificado auto-assinado
				},
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}

	log.Info().
		Str("cluster", cluster).
		Str("url", endpoint.URL).
		Msg("Cliente Prometheus criado")

	return client, nil
}

// Query executa uma query instantânea no Prometheus
func (c *PrometheusClient) Query(ctx context.Context, query string) (*QueryResult, error) {
	queryURL := c.endpoint.URL + "api/v1/query"

	// Preparar URL com query
	params := url.Values{}
	params.Add("query", query)

	fullURL := fmt.Sprintf("%s?%s", queryURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status code %d: %s", resp.StatusCode, string(body))
	}

	var result QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query falhou: status=%s", result.Status)
	}

	log.Debug().
		Str("query", query).
		Int("results", len(result.Data.Result)).
		Msg("Query executada com sucesso")

	return &result, nil
}

// QueryRange executa uma query range no Prometheus (dados históricos)
func (c *PrometheusClient) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryRangeResult, error) {
	queryURL := c.endpoint.URL + "api/v1/query_range"

	// Preparar URL com query range
	params := url.Values{}
	params.Add("query", query)
	params.Add("start", fmt.Sprintf("%d", start.Unix()))
	params.Add("end", fmt.Sprintf("%d", end.Unix()))
	params.Add("step", step.String())

	fullURL := fmt.Sprintf("%s?%s", queryURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar query_range: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status code %d: %s", resp.StatusCode, string(body))
	}

	var result QueryRangeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query_range falhou: status=%s", result.Status)
	}

	log.Debug().
		Str("query", query).
		Int("results", len(result.Data.Result)).
		Time("start", start).
		Time("end", end).
		Dur("step", step).
		Msg("Query range executada com sucesso")

	return &result, nil
}

// GetHPAMetrics busca métricas de um HPA específico
func (c *PrometheusClient) GetHPAMetrics(ctx context.Context, namespace, hpaName string) (map[string]interface{}, error) {
	metrics := make(map[string]interface{})

	// Query para réplicas atuais
	currentReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err := c.Query(ctx, currentReplicasQuery)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar current_replicas: %w", err)
	}

	if len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["current_replicas"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para min replicas
	minReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_min_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, minReplicasQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["min_replicas"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para max replicas
	maxReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_max_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, maxReplicasQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["max_replicas"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para desired replicas
	desiredReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_desired_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, desiredReplicasQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["desired_replicas"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para CPU usage (%)
	cpuUsageQuery := fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[1m])) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.Query(ctx, cpuUsageQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["cpu_usage_percent"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para Memory usage (%)
	memoryUsageQuery := fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*"}) / sum(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.Query(ctx, memoryUsageQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			metrics["memory_usage_percent"] = result.Data.Result[0].Value[1]
		}
	}

	// Query para CPU Request POR POD (não soma de todos)
	cpuRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, cpuRequestQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			// Prometheus retorna em cores (ex: 0.384), converter para millicores com sufixo
			coresValue := result.Data.Result[0].Value[1]
			var coresFloat float64
			switch v := coresValue.(type) {
			case float64:
				coresFloat = v
			case string:
				var parseErr error
				coresFloat, parseErr = strconv.ParseFloat(v, 64)
				if parseErr != nil {
					log.Warn().Str("value", v).Msg("Falha ao parsear CPU request")
					return metrics, nil
				}
			default:
				log.Warn().Interface("value", coresValue).Msg("Tipo não suportado para CPU request")
				return metrics, nil
			}
			millicores := int(coresFloat * 1000)
			metrics["cpu_request"] = fmt.Sprintf("%dm", millicores)
			log.Info().
				Float64("cores", coresFloat).
				Int("millicores", millicores).
				Str("formatted", metrics["cpu_request"].(string)).
				Msg("CPU Request convertido")
		}
	}

	// Query para CPU Limit POR POD (não soma de todos)
	cpuLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, cpuLimitQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			// Prometheus retorna em cores (ex: 0.512), converter para millicores com sufixo
			coresValue := result.Data.Result[0].Value[1]
			var coresFloat float64
			switch v := coresValue.(type) {
			case float64:
				coresFloat = v
			case string:
				var parseErr error
				coresFloat, parseErr = strconv.ParseFloat(v, 64)
				if parseErr != nil {
					log.Warn().Str("value", v).Msg("Falha ao parsear CPU limit")
					return metrics, nil
				}
			default:
				log.Warn().Interface("value", coresValue).Msg("Tipo não suportado para CPU limit")
				return metrics, nil
			}
			millicores := int(coresFloat * 1000)
			metrics["cpu_limit"] = fmt.Sprintf("%dm", millicores)
		}
	}

	// Query para Memory Request (em bytes)
	memoryRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, memoryRequestQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			// Prometheus retorna em bytes, converter para Mi/Gi
			memValue := result.Data.Result[0].Value[1]
			var bytesFloat float64
			switch v := memValue.(type) {
			case float64:
				bytesFloat = v
			case string:
				var parseErr error
				bytesFloat, parseErr = strconv.ParseFloat(v, 64)
				if parseErr != nil {
					log.Warn().Str("value", v).Msg("Falha ao parsear Memory request")
					return metrics, nil
				}
			default:
				log.Warn().Interface("value", memValue).Msg("Tipo não suportado para Memory request")
				return metrics, nil
			}
			// Converter bytes para Mi/Gi
			metrics["memory_request"] = formatBytes(bytesFloat)
		}
	}

	// Query para Memory Limit (em bytes)
	memoryLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.Query(ctx, memoryLimitQuery)
	if err == nil && len(result.Data.Result) > 0 {
		if len(result.Data.Result[0].Value) > 1 {
			// Prometheus retorna em bytes, converter para Mi/Gi
			memValue := result.Data.Result[0].Value[1]
			var bytesFloat float64
			switch v := memValue.(type) {
			case float64:
				bytesFloat = v
			case string:
				var parseErr error
				bytesFloat, parseErr = strconv.ParseFloat(v, 64)
				if parseErr != nil {
					log.Warn().Str("value", v).Msg("Falha ao parsear Memory limit")
					return metrics, nil
				}
			default:
				log.Warn().Interface("value", memValue).Msg("Tipo não suportado para Memory limit")
				return metrics, nil
			}
			// Converter bytes para Mi/Gi
			metrics["memory_limit"] = formatBytes(bytesFloat)
		}
	}

	log.Debug().
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Int("metrics_count", len(metrics)).
		Msg("Métricas HPA coletadas")

	return metrics, nil
}

// GetHPAHistoricalMetrics busca métricas históricas de um HPA
func (c *PrometheusClient) GetHPAHistoricalMetrics(ctx context.Context, namespace, hpaName string, duration time.Duration, step time.Duration) (map[string]*QueryRangeResult, error) {
	end := time.Now()
	start := end.Add(-duration)

	log.Info().
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Time("start", start).
		Time("end", end).
		Dur("duration", duration).
		Dur("step", step).
		Str("url", c.endpoint.URL).
		Msg("Iniciando busca de métricas históricas no Prometheus")

	historicalMetrics := make(map[string]*QueryRangeResult)

	// Réplicas atuais ao longo do tempo
	replicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err := c.QueryRange(ctx, replicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["replicas"] = result
	}

	// Réplicas desejadas ao longo do tempo
	desiredReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_desired_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, desiredReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["desired_replicas"] = result
	}

	// Min replicas ao longo do tempo
	minReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_min_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, minReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["min_replicas"] = result
	}

	// Max replicas ao longo do tempo
	maxReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_max_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, maxReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["max_replicas"] = result
	}

	// CPU usage ao longo do tempo (usa avg() para evitar valores > 100%)
	cpuQuery := fmt.Sprintf(
		`avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*",container!="",container!="POD"}[1m]) / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu"] = result
	}

	// Memory usage ao longo do tempo (usa avg() para evitar valores > 100%)
	memoryQuery := fmt.Sprintf(
		`avg(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*",container!="",container!="POD"} / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory"] = result
	}

	// CPU Request ao longo do tempo (em cores - será convertido para millicores no processamento)
	cpuRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuRequestQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu_request"] = result
	}

	// CPU Limit ao longo do tempo (em cores - será convertido para millicores no processamento)
	cpuLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuLimitQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu_limit"] = result
	}

	// Memory Request ao longo do tempo (em bytes)
	memoryRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryRequestQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory_request"] = result
	}

	// Memory Limit ao longo do tempo (em bytes)
	memoryLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryLimitQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory_limit"] = result
	}

	// Log detalhado dos dados retornados
	logger := log.Info().
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Dur("duration", duration).
		Dur("step", step).
		Int("metrics_count", len(historicalMetrics))

	// Mostrar primeiro e último timestamp das réplicas (se disponível)
	if replicasResult, ok := historicalMetrics["replicas"]; ok && len(replicasResult.Data.Result) > 0 && len(replicasResult.Data.Result[0].Values) > 0 {
		firstTs := replicasResult.Data.Result[0].Values[0][0].(float64)
		lastTs := replicasResult.Data.Result[0].Values[len(replicasResult.Data.Result[0].Values)-1][0].(float64)
		logger = logger.
			Time("first_timestamp", time.Unix(int64(firstTs), 0)).
			Time("last_timestamp", time.Unix(int64(lastTs), 0)).
			Int("data_points", len(replicasResult.Data.Result[0].Values))
	}

	logger.Msg("Métricas históricas coletadas do Prometheus")

	return historicalMetrics, nil
}

// GetHPAHistoricalMetricsWithOffset busca métricas históricas de um HPA com offset temporal (ex: D-1)
// offset: deslocamento temporal (ex: 24*time.Hour para dados de ontem)
func (c *PrometheusClient) GetHPAHistoricalMetricsWithOffset(ctx context.Context, namespace, hpaName string, duration time.Duration, step time.Duration, offset time.Duration) (map[string]*QueryRangeResult, error) {
	// Calcular janela temporal deslocada
	// Para D-1: pegar a mesma janela de tempo, mas deslocada 24h no passado
	now := time.Now()
	end := now.Add(-offset)
	start := now.Add(-duration).Add(-offset)

	log.Info().
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Time("start", start).
		Time("end", end).
		Dur("duration", duration).
		Dur("step", step).
		Dur("offset", offset).
		Str("url", c.endpoint.URL).
		Msg("Iniciando busca de métricas históricas com offset no Prometheus")

	historicalMetrics := make(map[string]*QueryRangeResult)

	// Réplicas atuais ao longo do tempo
	replicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_current_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err := c.QueryRange(ctx, replicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["replicas"] = result
	}

	// Réplicas desejadas ao longo do tempo
	desiredReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_status_desired_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, desiredReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["desired_replicas"] = result
	}

	// Min replicas ao longo do tempo
	minReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_min_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, minReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["min_replicas"] = result
	}

	// Max replicas ao longo do tempo
	maxReplicasQuery := fmt.Sprintf(
		`kube_horizontalpodautoscaler_spec_max_replicas{namespace="%s",horizontalpodautoscaler="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, maxReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["max_replicas"] = result
	}

	// CPU usage ao longo do tempo (usa avg() para evitar valores > 100%)
	cpuQuery := fmt.Sprintf(
		`avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*",container!="",container!="POD"}[1m]) / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu"] = result
	}

	// Memory usage ao longo do tempo (usa avg() para evitar valores > 100%)
	memoryQuery := fmt.Sprintf(
		`avg(container_memory_working_set_bytes{namespace="%s",pod=~"%s.*",container!="",container!="POD"} / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"}) * 100`,
		namespace, hpaName, namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory"] = result
	}

	// CPU Request ao longo do tempo (em cores - será convertido para millicores no processamento)
	cpuRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuRequestQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu_request"] = result
	}

	// CPU Limit ao longo do tempo (em cores - será convertido para millicores no processamento)
	cpuLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="cpu"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, cpuLimitQuery, start, end, step)
	if err == nil {
		historicalMetrics["cpu_limit"] = result
	}

	// Memory Request ao longo do tempo (em bytes)
	memoryRequestQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryRequestQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory_request"] = result
	}

	// Memory Limit ao longo do tempo (em bytes)
	memoryLimitQuery := fmt.Sprintf(
		`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s.*",resource="memory"})`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, memoryLimitQuery, start, end, step)
	if err == nil {
		historicalMetrics["memory_limit"] = result
	}

	// Log detalhado dos dados retornados
	logger := log.Info().
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Dur("duration", duration).
		Dur("step", step).
		Dur("offset", offset).
		Int("metrics_count", len(historicalMetrics))

	// Mostrar primeiro e último timestamp das réplicas (se disponível)
	if replicasResult, ok := historicalMetrics["replicas"]; ok && len(replicasResult.Data.Result) > 0 && len(replicasResult.Data.Result[0].Values) > 0 {
		firstTs := replicasResult.Data.Result[0].Values[0][0].(float64)
		lastTs := replicasResult.Data.Result[0].Values[len(replicasResult.Data.Result[0].Values)-1][0].(float64)
		logger = logger.
			Time("first_timestamp", time.Unix(int64(firstTs), 0)).
			Time("last_timestamp", time.Unix(int64(lastTs), 0)).
			Int("data_points", len(replicasResult.Data.Result[0].Values))
	}

	logger.Msg("Métricas históricas com offset coletadas do Prometheus")

	return historicalMetrics, nil
}

// Close fecha o cliente (cleanup de recursos)
func (c *PrometheusClient) Close() error {
	c.httpClient.CloseIdleConnections()
	log.Debug().
		Str("cluster", c.endpoint.Cluster).
		Msg("Cliente Prometheus fechado")
	return nil
}
