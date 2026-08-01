package finops

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/monitoring/discovery"
)

// PrometheusEnricher enriquece workloads com dados históricos reais do Prometheus.
// Usa queries batch (uma por métrica, todos os namespaces) para evitar N×queries por workload.
type PrometheusEnricher struct {
	api           v1.API
	window        int               // janela de histórico em dias
	podToWorkload map[string]string // "ns/pod" → "ns/workload" (preenchido pelo calculator)
}

// NewPrometheusEnricher cria um enricher conectado ao endpoint Prometheus dado.
//
// requiresGCPAuth: true quando prometheusURL é o Google Cloud Managed Service for Prometheus
// (GMP, ver internal/monitoring/discovery.RequiresGCPAuth) — nesse caso usa TLS real (não pula
// verificação) e injeta "Authorization: Bearer <token>" via discovery.GCPAuthTransport em vez de
// aceitar certificado auto-assinado sem auth.
func NewPrometheusEnricher(prometheusURL string, windowDays int, requiresGCPAuth bool) (*PrometheusEnricher, error) {
	if windowDays <= 0 {
		windowDays = 30
	}
	transport := &http.Transport{}
	var roundTripper http.RoundTripper = transport
	if requiresGCPAuth {
		roundTripper = discovery.GCPAuthTransport(transport)
	} else {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	httpClient := &http.Client{
		Transport: roundTripper,
		Timeout:   60 * time.Second,
	}
	apiClient, err := api.NewClient(api.Config{
		Address:      prometheusURL,
		RoundTripper: httpClient.Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao criar cliente Prometheus: %w", err)
	}
	return &PrometheusEnricher{
		api:    v1.NewAPI(apiClient),
		window: windowDays,
	}, nil
}

// SetPodMapping define o mapa "ns/pod" → "ns/workload" para agregação de métricas.
// Deve ser chamado pelo calculator antes de EnrichWorkloads.
func (e *PrometheusEnricher) SetPodMapping(m map[string]string) {
	e.podToWorkload = m
}

// EnrichWorkloads executa 8 queries batch e enriquece todos os workloads com:
//   - CPU/Mem P95 e avg dos últimos N dias
//   - Requests recomendados (P95 × 1.20)
//   - Histórico HPA: avg/max/min réplicas + scale events
//   - WasteBRL e verdict revisado
func (e *PrometheusEnricher) EnrichWorkloads(ctx context.Context, workloads []FinOpsWorkload) {
	w := e.window

	log.Info().Int("window_days", w).Int("workloads", len(workloads)).
		Msg("FinOps/Prom: iniciando enriquecimento batch")

	// ── 1. Métricas de container (por pod, depois agrega ao workload) ──────────
	cpuP95Map := e.queryContainerMetric(ctx,
		fmt.Sprintf(`quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])[%dd:5m]) * 1000`, w),
		"cpu_p95",
	)
	cpuAvgMap := e.queryContainerMetric(ctx,
		fmt.Sprintf(`avg_over_time(rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])[%dd:5m]) * 1000`, w),
		"cpu_avg",
	)
	memP95Map := e.queryContainerMetric(ctx,
		fmt.Sprintf(`quantile_over_time(0.95, container_memory_working_set_bytes{container!="",container!="POD"}[%dd]) / 1048576`, w),
		"mem_p95",
	)
	memAvgMap := e.queryContainerMetric(ctx,
		fmt.Sprintf(`avg_over_time(container_memory_working_set_bytes{container!="",container!="POD"}[%dd]) / 1048576`, w),
		"mem_avg",
	)

	// ── 2. Métricas HPA (por namespace/hpa-name) ──────────────────────────────
	hpaAvgMap := e.queryHPAMetric(ctx,
		fmt.Sprintf(`avg_over_time(kube_horizontalpodautoscaler_status_current_replicas[%dd])`, w),
		"hpa_avg",
	)
	hpaMaxMap := e.queryHPAMetric(ctx,
		fmt.Sprintf(`max_over_time(kube_horizontalpodautoscaler_status_current_replicas[%dd])`, w),
		"hpa_max",
	)
	hpaMinMap := e.queryHPAMetric(ctx,
		fmt.Sprintf(`min_over_time(kube_horizontalpodautoscaler_status_current_replicas[%dd])`, w),
		"hpa_min",
	)
	hpaEventsMap := e.queryHPAMetric(ctx,
		fmt.Sprintf(`changes(kube_horizontalpodautoscaler_status_current_replicas[%dd])`, w),
		"hpa_changes",
	)

	// ── 3. Agrega métricas de container: pod → workload ───────────────────────
	// P95: max entre pods (pior caso do workload)
	// avg: média entre pods (uso representativo)
	wlCPUP95 := e.aggregatePodToWorkload(cpuP95Map, true)
	wlCPUAvg := e.aggregatePodToWorkload(cpuAvgMap, false)
	wlMemP95 := e.aggregatePodToWorkload(memP95Map, true)
	wlMemAvg := e.aggregatePodToWorkload(memAvgMap, false)

	// ── 4. Enriquecer cada workload ───────────────────────────────────────────
	enriched := 0
	for i := range workloads {
		wl := &workloads[i]
		key := wl.Namespace + "/" + wl.Workload

		cpuP95 := wlCPUP95[key]
		cpuAvg := wlCPUAvg[key]
		memP95 := wlMemP95[key]
		memAvg := wlMemAvg[key]
		hasUsage := cpuP95 > 0 || memP95 > 0

		if hasUsage {
			wl.CPUP95Millis = round2(cpuP95)
			wl.CPUAvgMillis = round2(cpuAvg)
			wl.MemP95Mi = round2(memP95)
			wl.MemAvgMi = round2(memAvg)

			// Recomendação com margem de segurança SRE de 20%
			if cpuP95 > 0 {
				wl.CPURecommendedMillis = round2(cpuP95 * SafetyMargin)
			}
			if memP95 > 0 {
				wl.MemRecommendedMi = round2(memP95 * SafetyMargin)
			}

			// Desperdício = custo proporcional à fração de request além do recomendado
			wl.WasteBRL = calculateWaste(wl)

			// Verdict baseado em uso real
			wl.Verdict = verdictFromPrometheus(wl)
			enriched++
		}

		// Histórico HPA
		if hpaAvg, ok := hpaAvgMap[key]; ok {
			wl.HPAAvgReplicas = round2(hpaAvg)

			if v, ok := hpaMaxMap[key]; ok {
				wl.HPAMaxObserved = int(math.Round(v))
			}
			if v, ok := hpaMinMap[key]; ok {
				wl.HPAMinObserved = int(math.Round(v))
			}
			if v, ok := hpaEventsMap[key]; ok {
				wl.HPAScaleEvents = int(math.Round(v))
			}

			// Nunca escalou além do mínimo configurado
			if wl.HPAMax > 0 && wl.HPAMaxObserved > 0 {
				wl.HPANeverScaled = wl.HPAMaxObserved <= wl.HPAMin
			}
			if wl.HPANeverScaled && wl.Verdict != "oom_risk" {
				wl.Verdict = "hpa_removable"
			}

			// Custo real baseado em réplicas médias
			if wl.Pods > 0 {
				podCostBRL := wl.CostShareBRL / float64(wl.Pods)
				wl.AvgReplicasCostBRL = round2(podCostBRL * hpaAvg)
			}
		}
	}

	log.Info().Int("enriched", enriched).Int("window_days", w).
		Msg("FinOps/Prom: enriquecimento concluído")
}

// EnrichWorkloadsPartial é igual a EnrichWorkloads mas pula workloads já enriquecidos
// pelo Dynatrace (presentes em dtEnriched). Marca MetricsSource="prometheus" nos enriquecidos.
func (e *PrometheusEnricher) EnrichWorkloadsPartial(ctx context.Context, workloads []FinOpsWorkload, dtEnriched map[string]bool) {
	e.EnrichWorkloads(ctx, workloads)
	// Ajustar source: workloads não marcados pelo DT que agora têm dados = prometheus
	for i := range workloads {
		wl := &workloads[i]
		key := wl.Namespace + "/" + wl.Workload
		if !dtEnriched[key] && wl.MetricsSource == "" && (wl.CPUP95Millis > 0 || wl.MemP95Mi > 0) {
			wl.MetricsSource = "prometheus"
		}
	}
}

// queryContainerMetric executa uma query que retorna métricas por container
// e agrega por pod (soma containers do mesmo pod). Retorna map["ns/pod"] → valor.
func (e *PrometheusEnricher) queryContainerMetric(ctx context.Context, query, label string) map[string]float64 {
	qctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query falhou")
		return nil
	}

	vec, ok := result.(model.Vector)
	if !ok {
		return nil
	}

	m := make(map[string]float64)
	for _, sample := range vec {
		ns := string(sample.Metric["namespace"])
		pod := string(sample.Metric["pod"])
		if ns == "" || pod == "" {
			continue
		}
		// Soma containers do mesmo pod (label container distingue)
		m[ns+"/"+pod] += float64(sample.Value)
	}
	return m
}

// queryHPAMetric executa uma query de HPA e retorna map["ns/hpa-name"] → valor.
func (e *PrometheusEnricher) queryHPAMetric(ctx context.Context, query, label string) map[string]float64 {
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query HPA falhou")
		return nil
	}

	vec, ok := result.(model.Vector)
	if !ok {
		return nil
	}

	m := make(map[string]float64)
	for _, sample := range vec {
		ns := string(sample.Metric["namespace"])
		hpa := string(sample.Metric["horizontalpodautoscaler"])
		if ns == "" || hpa == "" {
			continue
		}
		m[ns+"/"+hpa] = float64(sample.Value)
	}
	return m
}

// aggregatePodToWorkload agrega métricas de nível pod para nível workload.
//   - useMax=true  → toma o maior valor entre os pods (para P95: pior caso)
//   - useMax=false → toma a média entre os pods (para avg: uso representativo)
func (e *PrometheusEnricher) aggregatePodToWorkload(podMetrics map[string]float64, useMax bool) map[string]float64 {
	if len(podMetrics) == 0 || len(e.podToWorkload) == 0 {
		return nil
	}

	sums := make(map[string]float64)
	counts := make(map[string]int)

	for podKey, value := range podMetrics {
		wlKey, ok := e.podToWorkload[podKey]
		if !ok {
			continue
		}
		if useMax {
			if value > sums[wlKey] {
				sums[wlKey] = value
			}
		} else {
			sums[wlKey] += value
			counts[wlKey]++
		}
	}

	if useMax {
		return sums
	}

	result := make(map[string]float64, len(sums))
	for k, sum := range sums {
		if n := counts[k]; n > 0 {
			result[k] = sum / float64(n)
		}
	}
	return result
}

// calculateWaste calcula o desperdício financeiro mensal com base em P95 × 1.20 vs request atual.
func calculateWaste(wl *FinOpsWorkload) float64 {
	if wl.CostShareBRL == 0 {
		return 0
	}
	var cpuFrac, memFrac float64
	if wl.CPURequestMillis > 0 && wl.CPURecommendedMillis > 0 {
		if excess := wl.CPURequestMillis - wl.CPURecommendedMillis; excess > 0 {
			cpuFrac = excess / wl.CPURequestMillis
		}
	}
	if wl.MemRequestMi > 0 && wl.MemRecommendedMi > 0 {
		if excess := wl.MemRequestMi - wl.MemRecommendedMi; excess > 0 {
			memFrac = excess / wl.MemRequestMi
		}
	}
	if cpuFrac == 0 && memFrac == 0 {
		return 0
	}
	return round2((cpuFrac + memFrac) / 2 * wl.CostShareBRL)
}

// verdictFromPrometheus atualiza o veredicto usando dados reais de uso P95.
// Mesmas regras do script Python original:
//   - RISCO OOM/THR: P95 > 95% do request (risco de throttling ou OOM)
//   - DESPERDÍCIO: (request - recommended) > 15% do request
//   - EFICIENTE: demais casos
func verdictFromPrometheus(wl *FinOpsWorkload) string {
	if wl.CPURequestMillis == 0 && wl.MemRequestMi == 0 {
		return "no_request"
	}

	// OOM/Throttling: P95 >= 95% do request em CPU ou Mem
	cpuRisk := wl.CPUP95Millis > 0 && wl.CPURequestMillis > 0 && wl.CPUP95Millis >= 0.95*wl.CPURequestMillis
	memRisk := wl.MemP95Mi > 0 && wl.MemRequestMi > 0 && wl.MemP95Mi >= 0.95*wl.MemRequestMi
	if cpuRisk || memRisk {
		return "oom_risk"
	}

	// Desperdício: request > recomendado em mais de 15% (pelo menos em um recurso com dados)
	cpuWaste := wl.CPURecommendedMillis > 0 && wl.CPURequestMillis > 0 &&
		(wl.CPURequestMillis-wl.CPURecommendedMillis) > wl.CPURequestMillis*0.15
	memWaste := wl.MemRecommendedMi > 0 && wl.MemRequestMi > 0 &&
		(wl.MemRequestMi-wl.MemRecommendedMi) > wl.MemRequestMi*0.15
	if cpuWaste || memWaste {
		return "superprovisioned"
	}

	return "ok"
}
