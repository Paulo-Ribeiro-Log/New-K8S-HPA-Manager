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
	"sync"
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

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	var roundTripper http.RoundTripper = transport
	if endpoint.RequiresGCPAuth {
		// GMP (Google Cloud Managed Service for Prometheus) — certificado válido do Google,
		// exige "Authorization: Bearer <token>" em toda requisição.
		roundTripper = discovery.GCPAuthTransport(transport)
	} else {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // Certificado auto-assinado (Prometheus self-hosted)
		}
	}

	client := &PrometheusClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: roundTripper,
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

	// Réplicas prontas ao longo do tempo (deployment com mesmo nome do HPA)
	readyReplicasQuery := fmt.Sprintf(
		`kube_deployment_status_replicas_ready{namespace="%s",deployment="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, readyReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["ready_replicas"] = result
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

	// Réplicas prontas ao longo do tempo (deployment com mesmo nome do HPA)
	readyReplicasQuery := fmt.Sprintf(
		`kube_deployment_status_replicas_ready{namespace="%s",deployment="%s"}`,
		namespace, hpaName,
	)

	result, err = c.QueryRange(ctx, readyReplicasQuery, start, end, step)
	if err == nil {
		historicalMetrics["ready_replicas"] = result
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

// GetDeploymentHistoricalMetrics busca métricas históricas de comportamento de um Deployment
// (réplicas desired/current/ready/updated/unavailable, CPU/memória %, restarts) — fonte primária
// do gráfico de comportamento no quick-view de pod (ver DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md).
//
// Diferente de GetHPAHistoricalMetrics: aquela depende de métricas indexadas pelo NOME DO HPA
// (kube_horizontalpodautoscaler_*) — não existem se o Deployment não tiver HPA, e reaproveitá-la
// passando o nome do deployment no lugar do HPA retornaria vazio sempre que os nomes divergirem
// (bug sutil). Esta função é independente de HPA — funciona pra qualquer Deployment.
func (c *PrometheusClient) GetDeploymentHistoricalMetrics(ctx context.Context, namespace, deployment string, duration, step time.Duration) (map[string]*QueryRangeResult, error) {
	end := time.Now()
	start := end.Add(-duration)
	return c.deploymentHistoricalMetricsRange(ctx, namespace, deployment, start, end, step)
}

// GetDeploymentHistoricalMetricsWithOffset busca a mesma janela de métricas de comportamento,
// deslocada no tempo — usado pela comparação D-1/D-2/D-3 (mesmo padrão já validado em produção
// no Conntrack Viewer e em GetHPAHistoricalMetricsWithOffset).
func (c *PrometheusClient) GetDeploymentHistoricalMetricsWithOffset(ctx context.Context, namespace, deployment string, duration, step, offset time.Duration) (map[string]*QueryRangeResult, error) {
	now := time.Now()
	end := now.Add(-offset)
	start := now.Add(-duration).Add(-offset)
	return c.deploymentHistoricalMetricsRange(ctx, namespace, deployment, start, end, step)
}

// deploymentHistoricalMetricsRange executa em paralelo (goroutines + WaitGroup — o handler que
// chama esta função tem timeout de 45s, mais queries que o Conntrack/HPA justificam paralelizar
// em vez de sequenciar) as queries range de comportamento de um Deployment no intervalo
// [start,end]. Cada query é best-effort: uma falha isolada só fica ausente do mapa resultado, não
// derruba as demais (mesmo espírito de GetHPAHistoricalMetrics).
//
// pod=~"%s-.*" (com hífen) é mais preciso que o pod=~"%s.*" usado em GetHPAHistoricalMetrics —
// evita match espúrio (ex: deployment "foo" casando pods de um deployment "foobar-xyz" diferente).
func (c *PrometheusClient) deploymentHistoricalMetricsRange(ctx context.Context, namespace, deployment string, start, end time.Time, step time.Duration) (map[string]*QueryRangeResult, error) {
	log.Info().
		Str("namespace", namespace).
		Str("deployment", deployment).
		Time("start", start).
		Time("end", end).
		Dur("step", step).
		Str("url", c.endpoint.URL).
		Msg("Iniciando busca de métricas históricas de comportamento do Deployment no Prometheus")

	queries := map[string]string{
		"replicas_desired":     fmt.Sprintf(`kube_deployment_spec_replicas{namespace="%s",deployment="%s"}`, namespace, deployment),
		"replicas_current":     fmt.Sprintf(`kube_deployment_status_replicas{namespace="%s",deployment="%s"}`, namespace, deployment),
		"replicas_ready":       fmt.Sprintf(`kube_deployment_status_replicas_ready{namespace="%s",deployment="%s"}`, namespace, deployment),
		"replicas_updated":     fmt.Sprintf(`kube_deployment_status_replicas_updated{namespace="%s",deployment="%s"}`, namespace, deployment),
		"replicas_unavailable": fmt.Sprintf(`kube_deployment_status_replicas_unavailable{namespace="%s",deployment="%s"}`, namespace, deployment),
		"cpu": fmt.Sprintf(
			`avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[1m]) / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s-.*",resource="cpu"}) * 100`,
			namespace, deployment, namespace, deployment,
		),
		"memory": fmt.Sprintf(
			`avg(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"} / on(pod,container) group_left() kube_pod_container_resource_requests{namespace="%s",pod=~"%s-.*",resource="memory"}) * 100`,
			namespace, deployment, namespace, deployment,
		),
		// increase() dá "restarts NESTE intervalo" (por ponto do gráfico), não o contador cru
		// acumulado desde sempre — é o que faz sentido plotar como barra por período. sum() é
		// obrigatório aqui: sem ele a query retorna 1 série POR pod+container (confirmado contra
		// um deployment real de produção com múltiplas réplicas — 2 séries numa deployment com 2
		// pods), e o merge de séries deste pacote (deployment_behavior.go) só considera a primeira
		// série de cada chave — sem sum(), restarts de todos os pods menos um seriam descartados
		// silenciosamente.
		"restarts": fmt.Sprintf(
			`sum(increase(kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s-.*"}[%s]))`,
			namespace, deployment, step.String(),
		),
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]*QueryRangeResult, len(queries))
	)
	for key, query := range queries {
		wg.Add(1)
		go func(key, query string) {
			defer wg.Done()
			result, err := c.QueryRange(ctx, query, start, end, step)
			if err != nil {
				log.Debug().
					Err(err).
					Str("key", key).
					Str("namespace", namespace).
					Str("deployment", deployment).
					Msg("Query de comportamento do Deployment falhou — ausente do resultado")
				return
			}
			mu.Lock()
			results[key] = result
			mu.Unlock()
		}(key, query)
	}
	wg.Wait()

	log.Info().
		Str("namespace", namespace).
		Str("deployment", deployment).
		Int("metrics_count", len(results)).
		Msg("Métricas de comportamento do Deployment coletadas do Prometheus")

	return results, nil
}

// GetDeploymentResourceLimits busca CPU/memória request+limit atuais do Deployment via query
// instant (não query_range — request/limit mudam raramente, evita 4 séries range desnecessárias
// por chamada). Retorna zero pros campos cuja métrica não existir (ex: container sem limit
// definido) — não é erro, é um estado válido do cluster.
func (c *PrometheusClient) GetDeploymentResourceLimits(ctx context.Context, namespace, deployment string) (cpuRequestCores, cpuLimitCores, memoryRequestBytes, memoryLimitBytes float64) {
	if v, err := c.QueryScalar(ctx, fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s-.*",resource="cpu"})`, namespace, deployment)); err == nil {
		cpuRequestCores = v
	}
	if v, err := c.QueryScalar(ctx, fmt.Sprintf(`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s-.*",resource="cpu"})`, namespace, deployment)); err == nil {
		cpuLimitCores = v
	}
	if v, err := c.QueryScalar(ctx, fmt.Sprintf(`max(kube_pod_container_resource_requests{namespace="%s",pod=~"%s-.*",resource="memory"})`, namespace, deployment)); err == nil {
		memoryRequestBytes = v
	}
	if v, err := c.QueryScalar(ctx, fmt.Sprintf(`max(kube_pod_container_resource_limits{namespace="%s",pod=~"%s-.*",resource="memory"})`, namespace, deployment)); err == nil {
		memoryLimitBytes = v
	}
	return
}

// NamespaceMetrics representa métricas agregadas de um namespace
type NamespaceMetrics struct {
	Namespace              string  `json:"namespace"`
	CPURequestMillis       int64   `json:"cpu_request_millis"`
	CPUUsageMillis         int64   `json:"cpu_usage_millis"`
	CPUPercentOfCluster    float64 `json:"cpu_percent_of_cluster"`
	MemoryRequestGB        float64 `json:"memory_request_gb"`
	MemoryUsageGB          float64 `json:"memory_usage_gb"`
	MemoryPercentOfCluster float64 `json:"memory_percent_of_cluster"`
	PodCount               int     `json:"pod_count"`
	PodPercentOfCluster    float64 `json:"pod_percent_of_cluster"`
}

// GetNamespaceMetrics busca métricas agregadas por namespace
func (c *PrometheusClient) GetNamespaceMetrics(ctx context.Context) ([]NamespaceMetrics, error) {
	log.Info().Msg("Buscando métricas agregadas por namespace")

	metrics := make([]NamespaceMetrics, 0)

	// Query para CPU Request por namespace (em cores, converter para millicores)
	cpuRequestQuery := `sum by (namespace) (kube_pod_container_resource_requests{resource="cpu"})`
	cpuRequestResult, err := c.Query(ctx, cpuRequestQuery)
	if err != nil {
		log.Error().Err(err).Msg("Erro ao buscar CPU request por namespace")
		return nil, fmt.Errorf("erro ao buscar CPU request: %w", err)
	}

	// Query para CPU Usage por namespace (em cores/s, converter para millicores)
	cpuUsageQuery := `sum by (namespace) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`
	cpuUsageResult, err := c.Query(ctx, cpuUsageQuery)
	if err != nil {
		log.Warn().Err(err).Msg("Erro ao buscar CPU usage por namespace (continuando)")
	}

	// Query para Memory Request por namespace (em bytes, converter para GB)
	memoryRequestQuery := `sum by (namespace) (kube_pod_container_resource_requests{resource="memory"})`
	memoryRequestResult, err := c.Query(ctx, memoryRequestQuery)
	if err != nil {
		log.Error().Err(err).Msg("Erro ao buscar Memory request por namespace")
		return nil, fmt.Errorf("erro ao buscar Memory request: %w", err)
	}

	// Query para Memory Usage por namespace (em bytes, converter para GB)
	memoryUsageQuery := `sum by (namespace) (container_memory_working_set_bytes{container!="",container!="POD"})`
	memoryUsageResult, err := c.Query(ctx, memoryUsageQuery)
	if err != nil {
		log.Warn().Err(err).Msg("Erro ao buscar Memory usage por namespace (continuando)")
	}

	// Query para contagem de Pods por namespace
	podCountQuery := `count by (namespace) (kube_pod_info)`
	podCountResult, err := c.Query(ctx, podCountQuery)
	if err != nil {
		log.Error().Err(err).Msg("Erro ao buscar Pod count por namespace")
		return nil, fmt.Errorf("erro ao buscar Pod count: %w", err)
	}

	// Calcular totais do cluster para percentuais
	var totalCPURequest, totalCPUUsage, totalMemoryRequest, totalMemoryUsage float64
	var totalPods int

	// Processar CPU Request
	namespaceData := make(map[string]*NamespaceMetrics)

	for _, result := range cpuRequestResult.Data.Result {
		namespace := result.Metric["namespace"]
		if namespace == "" {
			continue
		}

		value := parseFloat(fmt.Sprintf("%v", result.Value[1]))
		totalCPURequest += value

		if _, exists := namespaceData[namespace]; !exists {
			namespaceData[namespace] = &NamespaceMetrics{Namespace: namespace}
		}
		namespaceData[namespace].CPURequestMillis = int64(value * 1000) // Converter cores para millicores
	}

	// Processar CPU Usage
	if cpuUsageResult != nil {
		for _, result := range cpuUsageResult.Data.Result {
			namespace := result.Metric["namespace"]
			if namespace == "" {
				continue
			}

			value := parseFloat(fmt.Sprintf("%v", result.Value[1]))
			totalCPUUsage += value

			if _, exists := namespaceData[namespace]; !exists {
				namespaceData[namespace] = &NamespaceMetrics{Namespace: namespace}
			}
			namespaceData[namespace].CPUUsageMillis = int64(value * 1000)
		}
	}

	// Processar Memory Request
	for _, result := range memoryRequestResult.Data.Result {
		namespace := result.Metric["namespace"]
		if namespace == "" {
			continue
		}

		value := parseFloat(fmt.Sprintf("%v", result.Value[1]))
		totalMemoryRequest += value

		if _, exists := namespaceData[namespace]; !exists {
			namespaceData[namespace] = &NamespaceMetrics{Namespace: namespace}
		}
		namespaceData[namespace].MemoryRequestGB = value / 1024 / 1024 / 1024 // Converter bytes para GB
	}

	// Processar Memory Usage
	if memoryUsageResult != nil {
		for _, result := range memoryUsageResult.Data.Result {
			namespace := result.Metric["namespace"]
			if namespace == "" {
				continue
			}

			value := parseFloat(fmt.Sprintf("%v", result.Value[1]))
			totalMemoryUsage += value

			if _, exists := namespaceData[namespace]; !exists {
				namespaceData[namespace] = &NamespaceMetrics{Namespace: namespace}
			}
			namespaceData[namespace].MemoryUsageGB = value / 1024 / 1024 / 1024
		}
	}

	// Processar Pod Count
	for _, result := range podCountResult.Data.Result {
		namespace := result.Metric["namespace"]
		if namespace == "" {
			continue
		}

		value := int(parseFloat(fmt.Sprintf("%v", result.Value[1])))
		totalPods += value

		if _, exists := namespaceData[namespace]; !exists {
			namespaceData[namespace] = &NamespaceMetrics{Namespace: namespace}
		}
		namespaceData[namespace].PodCount = value
	}

	// Calcular percentuais
	for _, data := range namespaceData {
		if totalCPURequest > 0 {
			data.CPUPercentOfCluster = (float64(data.CPURequestMillis) / 1000 / totalCPURequest) * 100
		}
		if totalMemoryRequest > 0 {
			data.MemoryPercentOfCluster = (data.MemoryRequestGB * 1024 * 1024 * 1024 / totalMemoryRequest) * 100
		}
		if totalPods > 0 {
			data.PodPercentOfCluster = (float64(data.PodCount) / float64(totalPods)) * 100
		}

		metrics = append(metrics, *data)
	}

	log.Info().
		Int("namespace_count", len(metrics)).
		Float64("total_cpu_cores", totalCPURequest).
		Float64("total_memory_gb", totalMemoryRequest/1024/1024/1024).
		Int("total_pods", totalPods).
		Msg("Métricas de namespace coletadas")

	return metrics, nil
}

// QueryScalar executa uma query e retorna um único valor escalar (float64)
// Útil para métricas agregadas como avg(), sum(), etc
func (c *PrometheusClient) QueryScalar(ctx context.Context, query string) (float64, error) {
	result, err := c.Query(ctx, query)
	if err != nil {
		return 0, err
	}

	// Verifica se há resultados
	if len(result.Data.Result) == 0 {
		return 0, fmt.Errorf("query returned no results")
	}

	// Pega o primeiro resultado (queries agregadas retornam 1 valor)
	firstResult := result.Data.Result[0]
	if len(firstResult.Value) < 2 {
		return 0, fmt.Errorf("invalid value format")
	}

	// Valor está na posição [1] do array [timestamp, "value"]
	valueStr, ok := firstResult.Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("value is not a string: %T", firstResult.Value[1])
	}

	// Converter string para float64
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse value: %w", err)
	}

	log.Debug().
		Str("query", query).
		Float64("value", value).
		Msg("Query scalar executada com sucesso")

	return value, nil
}

// parseFloat helper para converter interface{} para float64
func parseFloat(value string) float64 {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		log.Warn().Str("value", value).Msg("Erro ao parsear float")
		return 0
	}
	return f
}

// Close fecha o cliente (cleanup de recursos)
func (c *PrometheusClient) Close() error {
	c.httpClient.CloseIdleConnections()
	log.Debug().
		Str("cluster", c.endpoint.Cluster).
		Msg("Cliente Prometheus fechado")
	return nil
}
