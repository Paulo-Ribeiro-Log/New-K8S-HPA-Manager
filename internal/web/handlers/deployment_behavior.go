package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
	dtclient "k8s-hpa-manager/internal/dynatrace"
	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// ─── Tipos de resposta — ver DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md (Fase 1) ────────

// DeploymentBehaviorPoint é um ponto da série temporal de comportamento do Deployment.
// Timestamp em epoch MILISSEGUNDOS (não segundos) — mesma convenção de dtclient.MetricPoint.T,
// escolhida como padrão comum porque os dois caminhos (Prometheus e Dynatrace) convergem neste
// mesmo tipo de saída.
type DeploymentBehaviorPoint struct {
	Timestamp           int64   `json:"ts"`
	ReplicasDesired     float64 `json:"replicas_desired"`
	ReplicasCurrent     float64 `json:"replicas_current"`
	ReplicasReady       float64 `json:"replicas_ready"`
	ReplicasUpdated     float64 `json:"replicas_updated"`
	ReplicasUnavailable float64 `json:"replicas_unavailable"`
	CPUUsagePct         float64 `json:"cpu_usage_pct"`
	MemoryUsagePct      float64 `json:"memory_usage_pct"`
	Restarts            float64 `json:"restarts"`
}

// DeploymentScaleEvent marca uma mudança em ReplicasDesired ao longo da série — só disponível no
// caminho Prometheus (o Dynatrace não expõe réplicas desejadas via k8sWorkloadMetricDefs).
type DeploymentScaleEvent struct {
	Timestamp    int64   `json:"ts"`
	FromReplicas float64 `json:"from_replicas"`
	ToReplicas   float64 `json:"to_replicas"`
}

// DTProblemMarker — placeholder do overlay de problems (Fase 2, ainda não implementada). Definido
// já agora pra não precisar mexer no shape da resposta de novo quando a Fase 2 chegar.
type DTProblemMarker struct {
	ProblemID string `json:"problem_id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	StartTs   int64  `json:"start_ts"`
	EndTs     *int64 `json:"end_ts,omitempty"`
}

// DeploymentBehaviorResponse é a resposta de GET /deployments/:cluster/:namespace/:name/behavior.
type DeploymentBehaviorResponse struct {
	Cluster    string `json:"cluster"`
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	// WindowMinutes é o tamanho total da janela em minutos — substituiu um campo "Hours" (int)
	// porque a UI precisa de janelas menores que 1h (30min), que um inteiro de horas não representa.
	WindowMinutes int   `json:"window_minutes"`
	StepMinutes   int   `json:"step_minutes"`
	OffsetDays    []int `json:"offset_days,omitempty"`

	Points        []DeploymentBehaviorPoint         `json:"points"`
	ComparePoints map[int][]DeploymentBehaviorPoint `json:"compare_points,omitempty"`
	ScaleEvents   []DeploymentScaleEvent            `json:"scale_events"`

	HasHPA              bool `json:"has_hpa"`
	PrometheusAvailable bool `json:"prometheus_available"`

	// Source indica de onde vieram os dados: "prometheus" | "dynatrace" | "none". Mesma convenção
	// de cores já usada no FinOps (DT=azul, Prometheus=laranja) — ver finops/calculator.go.
	Source string `json:"source"`
	Error  string `json:"error,omitempty"`

	// Request/Limit atuais — só disponíveis no caminho Prometheus (instant Query(), não fazem
	// parte da série temporal). Zero quando indisponível (container sem limit definido, ou
	// caminho Dynatrace/none).
	CPURequestMillicores int64 `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores   int64 `json:"cpu_limit_millicores,omitempty"`
	MemoryRequestBytes   int64 `json:"memory_request_bytes,omitempty"`
	MemoryLimitBytes     int64 `json:"memory_limit_bytes,omitempty"`

	DynatraceProblems []DTProblemMarker `json:"dynatrace_problems,omitempty"` // Fase 2 — sempre vazio por ora
}

const (
	deploymentBehaviorDefaultMinutes     = 360 // 6h
	deploymentBehaviorDefaultStepMinutes = 5
	deploymentBehaviorTimeout            = 45 * time.Second
)

// getPromClient retorna um PrometheusClient cacheado por cluster (mesmo padrão de
// NodePoolHandler.getPromClient, nodepools.go — cada handler mantém sua própria cópia pequena em
// vez de uma abstração compartilhada prematura).
func (h *DeploymentHandler) getPromClient(cluster string) (*promclient.PrometheusClient, error) {
	h.promClientsMu.RLock()
	if c, ok := h.promClients[cluster]; ok {
		h.promClientsMu.RUnlock()
		return c, nil
	}
	h.promClientsMu.RUnlock()

	h.promClientsMu.Lock()
	defer h.promClientsMu.Unlock()
	if c, ok := h.promClients[cluster]; ok {
		return c, nil
	}
	c, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		return nil, err
	}
	h.promClients[cluster] = c
	return c, nil
}

// dynatraceClientForBehavior resolve um cliente Dynatrace pro fallback de série temporal — mesma
// resolução de credenciais de DynatraceHandler.clientForUser/PodHandler.dynatraceClientForPods
// (tokens salvos do usuário, fallback env vars DT_API_URL/DT_API_TOKEN).
func (h *DeploymentHandler) dynatraceClientForBehavior(aiEmail string) (*dtclient.Client, error) {
	var dtURL, dtToken string
	if aiEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(aiEmail)
		if err == nil && tokens != nil {
			dtURL = tokens.DynatraceURL
			dtToken = tokens.DynatraceToken
		}
	}
	return dtclient.NewClient(dtURL, dtToken)
}

// GetDeploymentBehavior monta o gráfico de comportamento de um Deployment (réplicas, CPU/memória
// %, restarts) numa janela de horas — Prometheus como fonte primária, Dynatrace como fallback real
// de série temporal (não só anotação) quando o cluster não tem Prometheus instalado. Nenhuma das
// duas fontes disponível → source="none", points vazio, sempre HTTP 200 (estado vazio explícito,
// nunca erro genérico — mesmo princípio de outras checagens best-effort do app).
//
// GET /api/v1/deployments/:cluster/:namespace/:name/behavior?minutes=360&step=5&offset_days=1,2,3&ai_email=
func (h *DeploymentHandler) GetDeploymentBehavior(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	deployment := c.Param("name")
	aiEmail := c.Query("ai_email")

	minutes := deploymentBehaviorDefaultMinutes
	if v, err := strconv.Atoi(c.Query("minutes")); err == nil && v > 0 {
		minutes = v
	}
	stepMinutes := deploymentBehaviorDefaultStepMinutes
	if v, err := strconv.Atoi(c.Query("step")); err == nil && v > 0 {
		stepMinutes = v
	}
	var offsetDays []int
	if raw := c.Query("offset_days"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if v, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && v > 0 {
				offsetDays = append(offsetDays, v)
			}
		}
	}

	duration := time.Duration(minutes) * time.Minute
	step := time.Duration(stepMinutes) * time.Minute

	resp := DeploymentBehaviorResponse{
		Cluster:       cluster,
		Namespace:     namespace,
		Deployment:    deployment,
		WindowMinutes: minutes,
		StepMinutes:   stepMinutes,
		OffsetDays:    offsetDays,
		Points:        []DeploymentBehaviorPoint{},
		ScaleEvents:   []DeploymentScaleEvent{},
		Source:        "none",
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), deploymentBehaviorTimeout)
	defer cancel()

	resp.HasHPA = h.deploymentHasHPA(ctx, cluster, namespace, deployment)

	// ─── 1. Tenta Prometheus primeiro ───────────────────────────────────────
	if promClient, err := h.getPromClient(cluster); err == nil {
		resp.PrometheusAvailable = true

		rawSeries, qerr := promClient.GetDeploymentHistoricalMetrics(ctx, namespace, deployment, duration, step)
		if qerr == nil && len(rawSeries) > 0 {
			resp.Source = "prometheus"
			resp.Points = pointsFromSeriesMap(prometheusSeriesToPointMap(rawSeries))
			resp.ScaleEvents = deriveScaleEvents(resp.Points)

			cpuReq, cpuLim, memReq, memLim := promClient.GetDeploymentResourceLimits(ctx, namespace, deployment)
			resp.CPURequestMillicores = int64(cpuReq * 1000)
			resp.CPULimitMillicores = int64(cpuLim * 1000)
			resp.MemoryRequestBytes = int64(memReq)
			resp.MemoryLimitBytes = int64(memLim)

			if len(offsetDays) > 0 {
				resp.ComparePoints = make(map[int][]DeploymentBehaviorPoint, len(offsetDays))
				for _, days := range offsetDays {
					offsetSeries, oerr := promClient.GetDeploymentHistoricalMetricsWithOffset(ctx, namespace, deployment, duration, step, time.Duration(days)*24*time.Hour)
					if oerr != nil || len(offsetSeries) == 0 {
						continue
					}
					resp.ComparePoints[days] = pointsFromSeriesMap(prometheusSeriesToPointMap(offsetSeries))
				}
			}
		}
	}

	// ─── 2. Fallback Dynatrace — só AKS + credenciais configuradas + entity resolvido ───────
	if resp.Source == "none" {
		serverURL := h.kubeManager.GetServerURL(cluster)
		if config.DetectCloudProvider(serverURL, cluster) == config.CloudProviderAKS {
			if dtc, err := h.dynatraceClientForBehavior(aiEmail); err == nil {
				// Nome real da entidade no Dynatrace nunca tem o sufixo "-admin" do context do
				// kubeconfig — mesmo bug corrigido em pods_dynatrace_status.go.
				entityID, found, rerr := dtc.ResolveEntityForWorkload(ctx, strings.TrimSuffix(cluster, "-admin"), namespace, deployment)
				if rerr == nil && found {
					end := time.Now()
					start := end.Add(-duration)
					dtSeries := dtc.GetDeploymentBehaviorMetrics(ctx, entityID, start, end)
					if len(dtSeries) > 0 {
						resp.Source = "dynatrace"
						resp.Points = pointsFromSeriesMap(dynatraceSeriesToPointMap(dtSeries))
						// Sem réplicas desejadas no Dynatrace (k8sWorkloadMetricDefs não cobre) —
						// não há o que diferenciar pra gerar scale events, fica vazio de propósito
						// (documentado no plano — não simular).
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// deploymentHasHPA detecta via API do K8s (não PromQL) se existe um HPA cujo scaleTargetRef
// aponta pra este Deployment. Falha ao listar (cluster inacessível, etc.) → false, sem propagar
// erro (campo informativo, não crítico pro resto da resposta).
func (h *DeploymentHandler) deploymentHasHPA(ctx context.Context, cluster, namespace, deployment string) bool {
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return false
	}
	hpas, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false
	}
	for _, hpa := range hpas.Items {
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" && hpa.Spec.ScaleTargetRef.Name == deployment {
			return true
		}
	}
	return false
}

// ─── Merge de séries — Prometheus e Dynatrace convergem no mesmo formato de saída ──────────────

// prometheusSeriesToPointMap converte o resultado bruto de deploymentHistoricalMetricsRange
// (map[key]*QueryRangeResult, timestamps em segundos) pro formato comum map[key]map[tsMs]valor.
// sum() é aplicado à query de restarts no cliente Prometheus (ver GetDeploymentHistoricalMetrics)
// — aqui assumimos no máximo 1 série por chave (Data.Result[0]); mais que isso indicaria uma
// query mal agregada e os labels extras seriam descartados silenciosamente.
func prometheusSeriesToPointMap(raw map[string]*promclient.QueryRangeResult) map[string]map[int64]float64 {
	out := make(map[string]map[int64]float64, len(raw))
	for key, result := range raw {
		if result == nil || len(result.Data.Result) == 0 {
			continue
		}
		values := result.Data.Result[0].Values
		m := make(map[int64]float64, len(values))
		for _, pair := range values {
			if len(pair) < 2 {
				continue
			}
			tsFloat, ok := pair[0].(float64)
			if !ok {
				continue
			}
			valStr, ok := pair[1].(string)
			if !ok {
				continue
			}
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				continue
			}
			m[int64(tsFloat)*1000] = val // segundos -> ms (mesma convenção epoch-ms do Dynatrace)
		}
		out[key] = m
	}
	return out
}

// dynatraceSeriesToPointMap converte []MetricSeriesData (k8sWorkloadMetricDefs) pro formato comum,
// remapeando as chaves do Dynatrace (pods_running/pods_ready_pct/pod_restarts/cpu_milli/memory_mb)
// pras chaves canônicas usadas por DeploymentBehaviorPoint (replicas_current/replicas_ready/
// restarts/cpu/memory).
//
// LIMITAÇÃO CONHECIDA (documentada no plano — "cobertura quase 1:1", não total): cpu_milli/
// memory_mb do Dynatrace são valores ABSOLUTOS de uso, não percentuais relativos a request como
// no caminho Prometheus (que normaliza via kube_pod_container_resource_requests). Sem uma fonte
// de request/limit disponível neste fallback — Prometheus indisponível é justamente por que
// caímos aqui —, CPUUsagePct/MemoryUsagePct NÃO são preenchidos no caminho Dynatrace (ficam 0).
// replicas_desired/updated/unavailable também não têm equivalente DT — mesma limitação.
func dynatraceSeriesToPointMap(series []dtclient.MetricSeriesData) map[string]map[int64]float64 {
	raw := make(map[string]map[int64]float64, len(series))
	for _, s := range series {
		m := make(map[int64]float64, len(s.Points))
		for _, p := range s.Points {
			m[p.T] = p.V
		}
		raw[s.Key] = m
	}

	out := make(map[string]map[int64]float64, 3)
	if running, ok := raw["pods_running"]; ok {
		out["replicas_current"] = running
		if readyPct, ok2 := raw["pods_ready_pct"]; ok2 {
			ready := make(map[int64]float64, len(running))
			for ts, r := range running {
				if pct, ok3 := readyPct[ts]; ok3 {
					ready[ts] = r * pct / 100
				}
			}
			out["replicas_ready"] = ready
		}
	}
	if restarts, ok := raw["pod_restarts"]; ok {
		out["restarts"] = restarts
	}
	return out
}

// pointsFromSeriesMap monta a timeline final ordenada combinando várias séries nomeadas — usado
// tanto pro caminho Prometheus quanto Dynatrace, que têm shapes de resposta bem diferentes mas
// convergem aqui pro mesmo DeploymentBehaviorPoint. Indexar um map ausente com [ts] em Go retorna
// o zero-value (0.0) com segurança — não precisa checar existência de cada série individualmente.
func pointsFromSeriesMap(series map[string]map[int64]float64) []DeploymentBehaviorPoint {
	tsSet := make(map[int64]struct{})
	for _, m := range series {
		for ts := range m {
			tsSet[ts] = struct{}{}
		}
	}
	timestamps := make([]int64, 0, len(tsSet))
	for ts := range tsSet {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	points := make([]DeploymentBehaviorPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		points = append(points, DeploymentBehaviorPoint{
			Timestamp:           ts,
			ReplicasDesired:     series["replicas_desired"][ts],
			ReplicasCurrent:     series["replicas_current"][ts],
			ReplicasReady:       series["replicas_ready"][ts],
			ReplicasUpdated:     series["replicas_updated"][ts],
			ReplicasUnavailable: series["replicas_unavailable"][ts],
			CPUUsagePct:         series["cpu"][ts],
			MemoryUsagePct:      series["memory"][ts],
			Restarts:            series["restarts"][ts],
		})
	}
	return points
}

// deriveScaleEvents percorre os pontos já ordenados e marca toda mudança em ReplicasDesired —
// feito no backend (zero queries extra) em vez de o frontend reprocessar a série toda vez.
func deriveScaleEvents(points []DeploymentBehaviorPoint) []DeploymentScaleEvent {
	events := make([]DeploymentScaleEvent, 0)
	if len(points) == 0 {
		return events
	}
	prev := points[0].ReplicasDesired
	for _, p := range points[1:] {
		if p.ReplicasDesired != prev {
			events = append(events, DeploymentScaleEvent{
				Timestamp:    p.Timestamp,
				FromReplicas: prev,
				ToReplicas:   p.ReplicasDesired,
			})
			prev = p.ReplicasDesired
		}
	}
	return events
}
