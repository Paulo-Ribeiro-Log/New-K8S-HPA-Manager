package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
)

// resourceSafetyMargin e os limiares de "oom_risk"/"superprovisioned" replicam exatamente
// verdictFromPrometheus em internal/finops/prometheus_enricher.go — mesma lógica de negócio,
// reimplementada aqui (não importada do pacote finops) porque o domínio é diferente
// (DeploymentHealth do Health Check, não FinOpsWorkload) e o Health Check já tem os nomes de pod
// exatos disponíveis por deployment via label selector, então não precisa do mapa pod→workload
// global que o FinOps constrói para cobrir o cluster inteiro numa passada só.
const resourceSafetyMargin = 1.2

// ResourceEnricher compara uso real (P95 histórico via Prometheus) contra o request configurado de
// um deployment, para detectar risco de OOM/throttling ou superprovisionamento — contexto que o
// snapshot ao vivo do metrics-server (enrichWithMetrics) sozinho não cobre, por ser um único ponto
// no tempo.
type ResourceEnricher struct {
	api    v1.API
	window time.Duration
}

// NewResourceEnricher cria um enricher conectado ao endpoint Prometheus dado.
func NewResourceEnricher(prometheusURL string, window time.Duration) (*ResourceEnricher, error) {
	if window <= 0 {
		window = 48 * time.Hour
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		Timeout: 30 * time.Second,
	}
	apiClient, err := api.NewClient(api.Config{Address: prometheusURL, RoundTripper: httpClient.Transport})
	if err != nil {
		return nil, fmt.Errorf("falha ao criar cliente Prometheus: %w", err)
	}
	return &ResourceEnricher{api: v1.NewAPI(apiClient), window: window}, nil
}

// ResourceUsageP95 resume o uso histórico P95 de um deployment comparado ao request configurado.
type ResourceUsageP95 struct {
	CPUP95Millis float64
	MemP95Bytes  float64
	Verdict      string // "oom_risk" | "superprovisioned" | "ok"
}

// EnrichDeployment consulta CPU/memória P95 na janela configurada, escopado aos nomes de pod
// informados (já listados pelo deployment_checker via label selector — evita precisar de um join
// com kube_pod_labels ou heurística de nome pra casar pod↔deployment). Retorna ok=false quando não
// há dados suficientes (sem série no Prometheus, deployment sem request configurado etc.) — nesse
// caso o chamador simplesmente não anexa nada, sem erro.
func (e *ResourceEnricher) EnrichDeployment(ctx context.Context, namespace string, podNames []string, cpuRequestMilli, memRequestBytes int64) (ResourceUsageP95, bool) {
	if len(podNames) == 0 {
		return ResourceUsageP95{}, false
	}

	podRegex := strings.Join(podNames, "|")
	windowStr := fmt.Sprintf("%dh", int(e.window.Hours()))

	cpuP95 := e.queryMaxAcrossPods(ctx, fmt.Sprintf(
		`quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{namespace=%q,pod=~%q,container!="",container!="POD"}[5m])[%s:5m]) * 1000`,
		namespace, podRegex, windowStr,
	))
	memP95 := e.queryMaxAcrossPods(ctx, fmt.Sprintf(
		`quantile_over_time(0.95, container_memory_working_set_bytes{namespace=%q,pod=~%q,container!="",container!="POD"}[%s])`,
		namespace, podRegex, windowStr,
	))

	if cpuP95 == 0 && memP95 == 0 {
		return ResourceUsageP95{}, false
	}

	return ResourceUsageP95{
		CPUP95Millis: cpuP95,
		MemP95Bytes:  memP95,
		Verdict:      verdictFromP95(cpuRequestMilli, cpuP95, memRequestBytes, memP95),
	}, true
}

// queryMaxAcrossPods soma os containers de cada pod e retorna o maior valor entre pods — pior caso
// do deployment, mesmo critério do FinOps para agregação P95 (aggregatePodToWorkload, useMax=true).
func (e *ResourceEnricher) queryMaxAcrossPods(ctx context.Context, query string) float64 {
	qctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, _, err := e.api.Query(qctx, query, time.Now())
	if err != nil {
		log.Warn().Err(err).Msg("[HealthCheck] Falha na query Prometheus de recursos (ResourceEnricher)")
		return 0
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return 0
	}

	perPod := make(map[string]float64)
	for _, sample := range vec {
		pod := string(sample.Metric["pod"])
		if pod == "" {
			continue
		}
		perPod[pod] += float64(sample.Value)
	}

	var maxVal float64
	for _, v := range perPod {
		if v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

// verdictFromP95 aplica os mesmos limiares do FinOps (verdictFromPrometheus em
// internal/finops/prometheus_enricher.go): P95 >= 95% do request = risco de OOM/throttling;
// request > recomendado (P95 × 1.2) em mais de 15% = superprovisionado; senão, ok. Retorna string
// vazia quando não há request configurado (nada a comparar).
func verdictFromP95(cpuRequestMilli int64, cpuP95Milli float64, memRequestBytes int64, memP95Bytes float64) string {
	if cpuRequestMilli == 0 && memRequestBytes == 0 {
		return ""
	}

	cpuReq := float64(cpuRequestMilli)
	memReq := float64(memRequestBytes)

	cpuRisk := cpuP95Milli > 0 && cpuReq > 0 && cpuP95Milli >= 0.95*cpuReq
	memRisk := memP95Bytes > 0 && memReq > 0 && memP95Bytes >= 0.95*memReq
	if cpuRisk || memRisk {
		return "oom_risk"
	}

	cpuRecommended := cpuP95Milli * resourceSafetyMargin
	memRecommended := memP95Bytes * resourceSafetyMargin

	cpuWaste := cpuRecommended > 0 && cpuReq > 0 && (cpuReq-cpuRecommended) > cpuReq*0.15
	memWaste := memRecommended > 0 && memReq > 0 && (memReq-memRecommended) > memReq*0.15
	if cpuWaste || memWaste {
		return "superprovisioned"
	}

	return "ok"
}
