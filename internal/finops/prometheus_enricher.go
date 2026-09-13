package finops

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net/http"
	"strings"
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

// promPeakSample é um valor de pico + o instante em que ocorreu — usado por qualquer "top"
// (CPU/Mem de workload ou de node) que precise dizer QUANDO o pico aconteceu, não só QUANTO foi.
// Necessário pra recursos efêmeros (nodes spot, pods de rollouts recentes): um pico sem
// timestamp é inútil pra julgar se ainda é relevante ou se já ficou obsoleto.
type promPeakSample struct {
	Value float64
	At    time.Time
}

// rangeStepForWindow escolhe o step de uma QueryRange (não-subquery) proporcional à janela, pra
// manter o volume de pontos por série num teto baixo (~120-170) mesmo em janelas de 30d —
// resolução de meia-hora/hora é mais que suficiente pra um humano julgar "o pico foi recente ou
// antigo", que é o único propósito do timestamp aqui (não é uma série pra plotar num gráfico
// detalhado). Reduzido de um teto de ~500-700 pontos/série pra ~120-170: essa QueryRange devolve
// UMA série POR POD (ou por node) — num cluster com centenas/milhares de pods, o volume total
// transferido (pontos × séries) é a causa mais provável de "top sempre zerado" relatada pelo
// usuário (falha silenciosa: a query estoura algum limite do Prometheus — query.max-samples,
// timeout, tamanho de resposta — e o código só loga um Warn e segue com mapa vazio). Por isso
// esta query agora é só BEST-EFFORT pro timestamp — o VALOR do pico vem de uma query instant com
// subquery (queryPodMetricPeakValue/queryInstanceMetricPeakValue), mesmo padrão já comprovado
// confiável usado pelo P95 (um único ponto por série, nunca a série inteira).
func rangeStepForWindow(windowDays int) time.Duration {
	switch {
	case windowDays <= 3:
		return 30 * time.Minute
	case windowDays <= 7:
		return time.Hour
	case windowDays <= 14:
		return 2 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// peakOf varre os pontos de uma série (já ordenados por tempo pelo Prometheus) e retorna o de
// maior valor + seu timestamp. len(values) > 0 é responsabilidade do chamador.
func peakOf(values []model.SamplePair) promPeakSample {
	best := values[0]
	for _, v := range values[1:] {
		if v.Value > best.Value {
			best = v
		}
	}
	return promPeakSample{Value: float64(best.Value), At: best.Timestamp.Time()}
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
	// Pico real de CPU/Mem — CPU só exibido como "top" na UI (nunca usado na recomendação de CPU
	// Limit, que segue baseada na proporção limit/request, ver recommendedLimits); Mem também
	// alimenta o Mem Limit recomendado (ver recommendedLimits). Duas queries por métrica,
	// deliberadamente separadas (bug real corrigido — "top sempre zerado em todas as análises"):
	//   1) *MaxValueMap: query INSTANT com subquery (mesmo padrão/custo do P95 acima — Prometheus
	//      devolve UM único ponto por série, nunca a série inteira) — fonte PRIMÁRIA e confiável
	//      do VALOR do pico.
	//   2) *MaxRange: QueryRange de resolução completa (uma série POR POD, todos os pontos) — só
	//      existe pra saber QUANDO o pico ocorreu (pedido explícito do usuário: um "top" sem
	//      data/hora não diz se é recente ou de semanas atrás). Num cluster com muitos pods essa
	//      query transfere ordens de magnitude mais dado que a #1 e é a candidata natural a
	//      estourar algum limite do Prometheus (query.max-samples, timeout, tamanho de resposta)
	//      — falhando SILENCIOSAMENTE (só um log.Warn) e zerando o "top" inteiro quando o valor
	//      dependia só dela. Por isso o valor NUNCA depende dela ter sucesso — o timestamp é
	//      best-effort, anexado só quando essa query de fato retorna algo pro pod/workload.
	cpuMaxValueMap := e.queryPodMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])))[%dd:5m]) * 1000`, w),
		"cpu_max_value",
	)
	memMaxValueMap := e.queryPodMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"}))[%dd:5m]) / 1048576`, w),
		"mem_max_value",
	)
	cpuMaxRange := e.queryPodMetricRangeMax(ctx,
		`sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])) * 1000`,
		"cpu_max_range",
	)
	memMaxRange := e.queryPodMetricRangeMax(ctx,
		`sum by (namespace, pod) (container_memory_working_set_bytes{container!="",container!="POD"}) / 1048576`,
		"mem_max_range",
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
	// Valor confiável do pico (reaproveita aggregatePodToWorkload, useMax=true — mesma redução
	// "pior caso entre pods" já usada pro P95, só que sobre a query instant/barata acima).
	wlCPUMaxValue := e.aggregatePodToWorkload(cpuMaxValueMap, true)
	wlMemMaxValue := e.aggregatePodToWorkload(memMaxValueMap, true)
	// Timestamp best-effort (pior caso entre pods, com o "quando" preservado) — ver comentário
	// acima sobre por que essa query pode falhar silenciosamente em clusters grandes.
	wlCPUMaxPeak := e.aggregatePodToWorkloadPeak(cpuMaxRange)
	wlMemMaxPeak := e.aggregatePodToWorkloadPeak(memMaxRange)

	// ── 4. Enriquecer cada workload ───────────────────────────────────────────
	enriched := 0
	for i := range workloads {
		wl := &workloads[i]
		key := wl.Namespace + "/" + wl.Workload

		cpuP95 := wlCPUP95[key]
		cpuAvg := wlCPUAvg[key]
		memP95 := wlMemP95[key]
		memAvg := wlMemAvg[key]
		cpuMaxValue, hasCPUMaxValue := wlCPUMaxValue[key]
		memMaxValue, hasMemMaxValue := wlMemMaxValue[key]
		cpuMaxPeak, hasCPUMaxAt := wlCPUMaxPeak[key]
		memMaxPeak, hasMemMaxAt := wlMemMaxPeak[key]
		hasUsage := cpuP95 > 0 || memP95 > 0

		if hasUsage {
			wl.CPUP95Millis = round2(cpuP95)
			wl.CPUAvgMillis = round2(cpuAvg)
			wl.MemP95Mi = round2(memP95)
			wl.MemAvgMi = round2(memAvg)

			// Valor do pico: sempre da query confiável (instant/subquery) quando disponível;
			// só cai pro valor da QueryRange (mais pesada) se a query barata não tiver retornado
			// nada pra este workload — nunca o contrário, pra não regredir pra um valor menos
			// confiável quando o barato já funcionou. Timestamp: sempre best-effort, da
			// QueryRange, anexado independente de qual das duas deu o valor.
			switch {
			case hasCPUMaxValue:
				wl.CPUMaxMillis = round2(cpuMaxValue)
			case hasCPUMaxAt:
				wl.CPUMaxMillis = round2(cpuMaxPeak.Value)
			}
			if hasCPUMaxAt {
				at := cpuMaxPeak.At
				wl.CPUMaxAt = &at
			}
			switch {
			case hasMemMaxValue:
				wl.MemMaxMi = round2(memMaxValue)
			case hasMemMaxAt:
				wl.MemMaxMi = round2(memMaxPeak.Value)
			}
			if hasMemMaxAt {
				at := memMaxPeak.At
				wl.MemMaxAt = &at
			}

			// Recomendação com margem de segurança SRE de 20%
			if cpuP95 > 0 {
				wl.CPURecommendedMillis = round2(cpuP95 * SafetyMargin)
			}
			if memP95 > 0 {
				wl.MemRecommendedMi = round2(memP95 * SafetyMargin)
			}

			wl.CPULimitRecommendedMillis, wl.MemLimitRecommendedMi = recommendedLimits(
				wl.CPURecommendedMillis, wl.MemRecommendedMi, wl.MemP95Mi, wl.MemMaxMi,
				wl.CPULimitMillis, wl.CPURequestMillis, wl.MemLimitMi,
			)

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

// queryPodMetricPeakValue executa uma query INSTANT com subquery (mesmo padrão/custo de
// quantile_over_time usado pro P95 em EnrichWorkloads — Prometheus reduz tudo server-side e
// devolve um ÚNICO ponto por série, nunca a série inteira) pra obter o valor real do pico de
// CPU/Mem por pod, já agregado via "sum by (namespace, pod)" dentro da própria query. Fonte
// PRIMÁRIA e confiável do VALOR do pico — ver queryPodMetricRangeMax pro porquê de existir uma
// query separada, bem mais pesada, só pro timestamp.
func (e *PrometheusEnricher) queryPodMetricPeakValue(ctx context.Context, query, label string) map[string]float64 {
	qctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query de pico (valor) falhou")
		return nil
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return nil
	}
	m := make(map[string]float64, len(vec))
	for _, sample := range vec {
		ns := string(sample.Metric["namespace"])
		pod := string(sample.Metric["pod"])
		if ns == "" || pod == "" {
			continue
		}
		m[ns+"/"+pod] = float64(sample.Value)
	}
	return m
}

// queryPodMetricRangeMax executa QueryRange com a métrica JÁ agregada por (namespace,pod) via
// "sum by" na própria query — diferente de queryContainerMetric (que soma containers em Go após
// uma agregação temporal já feita pelo Prometheus), aqui a soma de containers acontece em cada
// ponto no tempo ANTES de achar o pico, o que é mais correto pra "top": o pico real do POD é o
// maior valor da série já somada, não a soma dos picos independentes de cada container (que
// podem nunca ter ocorrido no mesmo instante). Retorna valor + timestamp exato do pico.
func (e *PrometheusEnricher) queryPodMetricRangeMax(ctx context.Context, query, label string) map[string]promPeakSample {
	qctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	end := time.Now()
	start := end.Add(-time.Duration(e.window) * 24 * time.Hour)
	r := v1.Range{Start: start, End: end, Step: rangeStepForWindow(e.window)}

	result, _, err := e.api.QueryRange(qctx, query, r)
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query range (com timestamp) falhou")
		return nil
	}
	mat, ok := result.(model.Matrix)
	if !ok {
		return nil
	}

	m := make(map[string]promPeakSample)
	for _, series := range mat {
		ns := string(series.Metric["namespace"])
		pod := string(series.Metric["pod"])
		if ns == "" || pod == "" || len(series.Values) == 0 {
			continue
		}
		m[ns+"/"+pod] = peakOf(series.Values)
	}
	return m
}

// aggregatePodToWorkloadPeak reduz picos por pod (com timestamp) pro pico do workload — mantém o
// timestamp do PARTICULAR pod que teve o maior valor entre todos os pods do workload (pior caso,
// mesmo critério de aggregatePodToWorkload com useMax=true, só que preservando o "quando").
func (e *PrometheusEnricher) aggregatePodToWorkloadPeak(podPeaks map[string]promPeakSample) map[string]promPeakSample {
	if len(podPeaks) == 0 || len(e.podToWorkload) == 0 {
		return nil
	}
	result := make(map[string]promPeakSample)
	for podKey, peak := range podPeaks {
		wlKey, ok := e.podToWorkload[podKey]
		if !ok {
			continue
		}
		if existing, ok := result[wlKey]; !ok || peak.Value > existing.Value {
			result[wlKey] = peak
		}
	}
	return result
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

// nodeTopUsage retorna, por node, o pico histórico (janela e.window) de CPU (millicores) e Mem
// (MiB) — "top" de verdade, não uma média — E a data/hora em que esse pico ocorreu (promPeakSample.At).
// Timestamp é crítico pra nodes efêmeros (spot/preemptible, recriados a cada eviction): sem ele
// não dá pra saber se o "top" reflete agora ou um momento qualquer dos últimos N dias, nem
// explicar por que um node muito jovem tem pouco (ou nenhum) histórico de pico. Valores brutos
// (não %) — o chamador (ComputeNodeUsage, live_metrics.go) calcula o percentual usando a
// capacidade real do node via API K8s. Best-effort: node ausente no resultado = sem dado, nunca
// erro.
//
// Correlação Prometheus↔node K8s em 2 estratégias (bug real corrigido — "pico" vinha
// consistentemente 0% em TODOS os nodes de um cluster real, mesmo com uso "agora" — via
// metrics-server, nunca depende do que segue — mostrando valores reais 6-25% CPU/46-68% Mem):
//  1. (preferida) join com node_uname_info, correlacionando por "nodename" EXATO — coletor
//     default do node-exporter (raramente desabilitado), cujo label "nodename" é o hostname do
//     SO e coincide com Node.metadata.name na esmagadora maioria dos clusters (AKS/EKS/GKE
//     inclusos, mesmo em nodes com nome tipo "aks-pool-xxx-vmssNNNNNN").
//  2. (fallback) substring no label "instance" contra o nome do node — mesmo padrão já usado em
//     internal/monitoring/predictions/collector.go, mas que SÓ funciona quando o scrape usa o
//     hostname como "instance"; falha (sem erro, só resultado vazio) quando "instance" é o
//     IP:porta do scrape target — exatamente o cenário real que motivou esta correção, já que
//     "aks-...-vmssNNNNNN" nunca é substring de um IP.
func (e *PrometheusEnricher) nodeTopUsage(ctx context.Context, nodeNames []string) (cpu, mem map[string]promPeakSample) {
	if len(nodeNames) == 0 {
		return make(map[string]promPeakSample), make(map[string]promPeakSample)
	}

	// Valor confiável (instant/subquery, mesmo custo já comprovado do P95 de workload) — fonte
	// PRIMÁRIA do valor do pico, nunca depende de nenhuma QueryRange (mais pesada) ter sucesso.
	cpuValueByNodename := e.queryNodenameMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((sum by (nodename) (rate(node_cpu_seconds_total{mode!="idle"}[5m]) * on(instance) group_left(nodename) node_uname_info))[%dd:5m]) * 1000`, e.window),
		"node_cpu_top_value_by_nodename",
	)
	memValueByNodenameRaw := e.queryNodenameMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((sum by (nodename) ((node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) * on(instance) group_left(nodename) node_uname_info))[%dd:5m])`, e.window),
		"node_mem_top_value_by_nodename",
	)
	memValueByNodename := make(map[string]float64, len(memValueByNodenameRaw))
	for k, v := range memValueByNodenameRaw {
		memValueByNodename[k] = v / 1048576
	}
	cpuValueByInstance := e.queryInstanceMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((sum by (instance) (rate(node_cpu_seconds_total{mode!="idle"}[5m])))[%dd:5m]) * 1000`, e.window),
		"node_cpu_top_value",
	)
	memValueByInstanceRaw := e.queryInstanceMetricPeakValue(ctx,
		fmt.Sprintf(`max_over_time((node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes)[%dd:5m])`, e.window),
		"node_mem_top_value",
	)
	memValueByInstance := make(map[string]float64, len(memValueByInstanceRaw))
	for k, v := range memValueByInstanceRaw {
		memValueByInstance[k] = v / 1048576
	}

	// Timestamp best-effort (QueryRange, resolução completa — uma série por node; ver
	// rangeStepForWindow/queryPodMetricRangeMax pro porquê disso poder falhar silenciosamente em
	// clusters com muitos nodes e nunca zerar o valor por causa disso).
	cpuRangeByNodename := e.queryNodenameMetricRangeMax(ctx,
		`sum by (nodename) (rate(node_cpu_seconds_total{mode!="idle"}[5m]) * on(instance) group_left(nodename) node_uname_info) * 1000`,
		"node_cpu_top_range_by_nodename",
	)
	memRangeByNodenameRaw := e.queryNodenameMetricRangeMax(ctx,
		`sum by (nodename) ((node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) * on(instance) group_left(nodename) node_uname_info)`,
		"node_mem_top_range_by_nodename",
	)
	memRangeByNodename := make(map[string]promPeakSample, len(memRangeByNodenameRaw))
	for k, v := range memRangeByNodenameRaw {
		v.Value = v.Value / 1048576
		memRangeByNodename[k] = v
	}
	cpuByInstance := e.queryInstanceMetricRangeMax(ctx,
		`sum by (instance) (rate(node_cpu_seconds_total{mode!="idle"}[5m])) * 1000`,
		"node_cpu_top",
	)
	memByInstanceRaw := e.queryInstanceMetricRangeMax(ctx,
		`node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes`,
		"node_mem_top",
	)
	memByInstance := make(map[string]promPeakSample, len(memByInstanceRaw))
	for k, v := range memByInstanceRaw {
		v.Value = v.Value / 1048576
		memByInstance[k] = v
	}

	return mergeNodeTopPeaks(nodeNames, cpuValueByNodename, cpuRangeByNodename, cpuValueByInstance, cpuByInstance),
		mergeNodeTopPeaks(nodeNames, memValueByNodename, memRangeByNodename, memValueByInstance, memByInstance)
}

// mergeInstancePeak combina o valor confiável (subquery instant) com o timestamp best-effort
// (QueryRange) por node, correlacionando por substring no label "instance" (node-exporter)
// contra o nome literal do node K8s — estratégia de FALLBACK (ver nodeTopUsage), usada quando o
// join por "nodename" (mergeNodeTopPeaks) não encontrou nada pra esse node. O valor NUNCA
// depende da query de timestamp ter tido sucesso; o timestamp é anexado só quando essa query de
// fato retorna algo pra esse node.
func mergeInstancePeak(nodeNames []string, valueByInstance map[string]float64, rangeByInstance map[string]promPeakSample) map[string]promPeakSample {
	out := make(map[string]promPeakSample, len(nodeNames))
	for _, name := range nodeNames {
		var sample promPeakSample
		var found bool
		for instance, v := range valueByInstance {
			if strings.Contains(instance, name) {
				sample.Value = v
				found = true
				break
			}
		}
		for instance, v := range rangeByInstance {
			if strings.Contains(instance, name) {
				if !found {
					sample.Value = v.Value
					found = true
				}
				sample.At = v.At
				break
			}
		}
		if found {
			out[name] = sample
		}
	}
	return out
}

// mergeNodeTopPeaks combina as 2 estratégias de correlação documentadas em nodeTopUsage: roda o
// fallback (substring de "instance", via mergeInstancePeak) primeiro, preenchendo o que
// conseguir; depois a estratégia preferida (join exato por "nodename") SOBRESCREVE cada node em
// que encontrou dado — nunca o contrário, pra sempre priorizar a correlação mais confiável
// quando ela está disponível.
func mergeNodeTopPeaks(
	nodeNames []string,
	valueByNodename map[string]float64, rangeByNodename map[string]promPeakSample,
	valueByInstance map[string]float64, rangeByInstance map[string]promPeakSample,
) map[string]promPeakSample {
	out := mergeInstancePeak(nodeNames, valueByInstance, rangeByInstance)
	for _, name := range nodeNames {
		v, hasValue := valueByNodename[name]
		r, hasRange := rangeByNodename[name]
		if !hasValue && !hasRange {
			continue
		}
		sample := out[name] // zero-value se o fallback não tinha achado nada pra este node
		switch {
		case hasValue:
			sample.Value = v
		case hasRange:
			sample.Value = r.Value
		}
		if hasRange {
			sample.At = r.At
		}
		out[name] = sample
	}
	return out
}

// queryInstanceMetricPeakValue é o equivalente pra node de queryPodMetricPeakValue — query
// INSTANT com subquery (um único ponto por série via node-exporter, nunca a série inteira),
// fonte confiável do VALOR do pico por "instance". Ver queryInstanceMetricRangeMax pro porquê
// de existir uma query bem mais pesada, separada, só pro timestamp.
func (e *PrometheusEnricher) queryInstanceMetricPeakValue(ctx context.Context, query, label string) map[string]float64 {
	qctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query de pico de node (valor) falhou")
		return nil
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return nil
	}
	m := make(map[string]float64, len(vec))
	for _, sample := range vec {
		instance := string(sample.Metric["instance"])
		if instance == "" {
			continue
		}
		m[instance] = float64(sample.Value)
	}
	return m
}

// queryInstanceMetricRangeMax executa QueryRange (não subquery instant) agrupado pelo label
// "instance" (node-exporter) e retorna, por instance, o pico (valor + timestamp exato) dentro da
// janela e.window. Step escolhido por rangeStepForWindow — mesmo trade-off resolução/volume já
// usado por queryContainerMetricRangeMax (o "quando" de um pico não precisa de resolução de
// segundos, minutos já bastam pra um humano julgar recência).
func (e *PrometheusEnricher) queryInstanceMetricRangeMax(ctx context.Context, query, label string) map[string]promPeakSample {
	qctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	end := time.Now()
	start := end.Add(-time.Duration(e.window) * 24 * time.Hour)
	r := v1.Range{Start: start, End: end, Step: rangeStepForWindow(e.window)}

	result, _, err := e.api.QueryRange(qctx, query, r)
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query range de node (com timestamp) falhou")
		return nil
	}
	mat, ok := result.(model.Matrix)
	if !ok {
		return nil
	}

	m := make(map[string]promPeakSample)
	for _, series := range mat {
		instance := string(series.Metric["instance"])
		if instance == "" || len(series.Values) == 0 {
			continue
		}
		m[instance] = peakOf(series.Values)
	}
	return m
}

// queryNodenameMetricPeakValue é o equivalente de queryInstanceMetricPeakValue, mas keyed pelo
// label "nodename" (via join com node_uname_info na própria query — ver nodeTopUsage) em vez de
// "instance" — resultado já vem correlacionável por IGUALDADE EXATA contra o nome do node K8s,
// sem precisar do fallback de substring que mergeInstancePeak usa.
func (e *PrometheusEnricher) queryNodenameMetricPeakValue(ctx context.Context, query, label string) map[string]float64 {
	qctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query de pico de node por nodename (valor) falhou")
		return nil
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return nil
	}
	m := make(map[string]float64, len(vec))
	for _, sample := range vec {
		nodename := string(sample.Metric["nodename"])
		if nodename == "" {
			continue
		}
		m[nodename] = float64(sample.Value)
	}
	return m
}

// queryNodenameMetricRangeMax é o equivalente de queryInstanceMetricRangeMax, mas keyed por
// "nodename" — ver queryNodenameMetricPeakValue e nodeTopUsage pro porquê dessa correlação ser
// preferível à de "instance" (substring, mais frágil).
func (e *PrometheusEnricher) queryNodenameMetricRangeMax(ctx context.Context, query, label string) map[string]promPeakSample {
	qctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	end := time.Now()
	start := end.Add(-time.Duration(e.window) * 24 * time.Hour)
	r := v1.Range{Start: start, End: end, Step: rangeStepForWindow(e.window)}

	result, _, err := e.api.QueryRange(qctx, query, r)
	if err != nil {
		log.Warn().Err(err).Str("metric", label).Msg("FinOps/Prom: query range de node por nodename (com timestamp) falhou")
		return nil
	}
	mat, ok := result.(model.Matrix)
	if !ok {
		return nil
	}

	m := make(map[string]promPeakSample)
	for _, series := range mat {
		nodename := string(series.Metric["nodename"])
		if nodename == "" || len(series.Values) == 0 {
			continue
		}
		m[nodename] = peakOf(series.Values)
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
