package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	NetworkInBytesSec   float64 `json:"network_in_bytes_sec"`  // bytes/s recebidos, somado entre réplicas+interfaces — só caminho Prometheus (Dynatrace não popula, mesmo caso de CPUUsagePct/MemoryUsagePct)
	NetworkOutBytesSec  float64 `json:"network_out_bytes_sec"` // bytes/s transmitidos, idem

	// CPUUsageMilliCores/MemoryUsageMB — valores ABSOLUTOS de uso (não percentual), só caminho
	// Dynatrace (builtin:kubernetes.workload.cpu_usage/memory_working_set, ver
	// k8sWorkloadMetricDefs em metrics.go). Existem porque o fallback Dynatrace não tem uma fonte
	// de request/limit pra normalizar em % (por isso CPUUsagePct/MemoryUsagePct ficam 0 nesse
	// caminho) — sem esses 2 campos, o dado real que a API do Dynatrace retorna simplesmente se
	// perdia (chegava em dynatraceSeriesToPointMap, nunca era escrito em nenhuma chave lida por
	// pointsFromSeriesMap). Omitempty: ficam ausentes no caminho Prometheus, onde não existem.
	CPUUsageMilliCores float64 `json:"cpu_usage_millicores,omitempty"`
	MemoryUsageMB      float64 `json:"memory_usage_mb,omitempty"`
}

// DeploymentScaleEvent marca uma mudança em ReplicasDesired ao longo da série — disponível nos
// dois caminhos (Prometheus e, desde a correção dos metricId de k8sWorkloadMetricDefs, também
// Dynatrace via pods_desired).
type DeploymentScaleEvent struct {
	Timestamp    int64   `json:"ts"`
	FromReplicas float64 `json:"from_replicas"`
	ToReplicas   float64 `json:"to_replicas"`
}

// DTProblemMarker — overlay de problems do Dynatrace (Fase 2) sobre o gráfico de comportamento.
// EndTs ausente (nil) indica problem ainda OPEN no fim da janela consultada.
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
	// Pod ecoa o parâmetro `pod` da requisição (vazio quando o escopo pedido foi "Deployment
	// inteiro"). PodScoped diz se a resposta DE FATO restringiu CPU/memória/restarts/rede a esse
	// pod — false tanto quando `pod` não foi pedido quanto quando foi pedido mas a fonte que
	// respondeu (Dynatrace, ver GetDeploymentBehavior) não suporta esse recorte, caso em que os
	// dados voltam a ser do Deployment inteiro mesmo com Pod preenchido — o frontend usa essa
	// combinação (Pod!="" && !PodScoped) pra avisar o usuário que caiu de volta pro agregado.
	Pod       string `json:"pod,omitempty"`
	PodScoped bool   `json:"pod_scoped"`
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

	// DynatraceProblems (Fase 2) — populado sempre que a entidade Dynatrace do workload resolve,
	// independente de qual fonte (Prometheus/Dynatrace) alimentou Points. omitempty cobre tanto
	// "sem Dynatrace configurado" quanto "sem problems na janela".
	DynatraceProblems []DTProblemMarker `json:"dynatrace_problems,omitempty"`

	// DynatraceUIBaseURL — base pra montar o link "Abrir no Dynatrace" de cada problem
	// (frontend monta {base}/ui/apps/dynatrace.davis.problems/problem/{problem_id}), mesmo padrão
	// já usado em DynatraceTab.tsx. Populado sempre que um client Dynatrace foi resolvido, mesmo
	// sem nenhum problem na janela — barato (só troca de sufixo na URL já em memória).
	DynatraceUIBaseURL string `json:"dynatrace_ui_base_url,omitempty"`
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
// GET /api/v1/deployments/:cluster/:namespace/:name/behavior?minutes=360&step=5&offset_days=1,2,3
func (h *DeploymentHandler) GetDeploymentBehavior(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	deployment := c.Param("name")
	// Identidade via InjectUserEmail() (JWT/RBAC) — não mais "ai_email" da query string. Usada só
	// pra resolver o token Dynatrace do fallback de série temporal, nada de resolução de provider AI
	// nesta função (não há entanglement com GetProviderForUser aqui).
	dtEmail := c.GetString("user_email")
	// pod (opcional): toggle "Este pod / Deployment inteiro" na aba Comportamento
	// (PodQuickViewModal.tsx) — quando presente, CPU/memória/restarts/rede ficam restritos a esse
	// pod exato em vez da média/soma de todos os pods do Deployment. Réplicas nunca mudam com
	// isso (ver comentário em deploymentHistoricalMetricsRange).
	podName := c.Query("pod")

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
		Pod:           podName,
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

		var rawSeries map[string]*promclient.QueryRangeResult
		var qerr error
		if podName != "" {
			rawSeries, qerr = promClient.GetPodScopedHistoricalMetrics(ctx, namespace, deployment, podName, duration, step)
		} else {
			rawSeries, qerr = promClient.GetDeploymentHistoricalMetrics(ctx, namespace, deployment, duration, step)
		}
		if qerr == nil && len(rawSeries) > 0 {
			resp.Source = "prometheus"
			resp.PodScoped = podName != ""
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

	end := time.Now()
	start := end.Add(-duration)

	// ─── 2. Resolve cliente/entidade Dynatrace uma única vez — reaproveitado tanto pelo fallback
	// de série (2a, só quando Prometheus não achou nada) quanto pelo overlay de problems (2b, Fase
	// 2 — aditivo, roda independente de qual fonte "venceu" a série, útil mesmo com Prometheus).
	// Sem gate de cloud provider: removido o "só AKS" que existia aqui — bug real já corrigido em
	// pods_dynatrace_status.go, mesma causa (cluster EKS asaplog-production roda Dynatrace de
	// verdade via Cloud Native Full Stack, suposição "EKS usa New Relic" não vale pra toda conta).
	// dtclient.NormalizeClusterName cobre o sufixo "-admin" (AKS) e o ARN completo de EKS sem
	// alias amigável configurado.
	var dtc *dtclient.Client
	var dtEntityID string
	var dtEntityFound bool
	if c, derr := h.dynatraceClientForBehavior(dtEmail); derr == nil {
		dtc = c
		// UI usa .apps.dynatrace.com (interface web), a API usa .live.dynatrace.com — mesma
		// transformação já usada em internal/web/handlers/dynatrace.go pro botão "Abrir no
		// Dynatrace" existente na aba Dynatrace.
		resp.DynatraceUIBaseURL = strings.ReplaceAll(dtc.BaseURL(), ".live.dynatrace.com", ".apps.dynatrace.com")
		if id, found, rerr := dtc.ResolveEntityForWorkload(ctx, dtclient.NormalizeClusterName(cluster), namespace, deployment); rerr == nil && found {
			dtEntityID = id
			dtEntityFound = true
		}
	}

	// ─── 2a. Fallback Dynatrace de série — só quando Prometheus não retornou nada ───────
	//
	// Escopo por pod (podName != ""): tenta PRIMEIRO a granularidade real de pod, via
	// CONTAINER_GROUP_INSTANCE (ResolveEntityForPod + GetPodBehaviorMetrics, pod_behavior_metrics.go)
	// — caminho DIFERENTE do agregado por workload abaixo (k8sWorkloadMetricDefs, que REJEITA
	// CLOUD_APPLICATION_INSTANCE como entidade primária: confirmado ao vivo contra o tenant real,
	// a API retorna "Entity type mismatch... Possible primary entity types: [CLOUD_APPLICATION]",
	// sempre 0 pontos — por isso um pod nunca tinha CPU/memória real vindas do Dynatrace antes desta
	// mudança). Só existe em clusters Cloud Native Full Stack (mesma instrumentação de
	// CLOUD_APPLICATION_INSTANCE) — se o pod não resolver ou não tiver containers correlacionados
	// (cluster classicFullStack, maioria da frota), cai pro agregado por workload como já fazia,
	// com PodScoped permanecendo false (fiel ao que a fonte realmente entregou).
	if resp.Source == "none" && podName != "" && dtc != nil {
		normalizedCluster := dtclient.NormalizeClusterName(cluster)
		if podEntityID, found, perr := dtc.ResolveEntityForPod(ctx, normalizedCluster, namespace, podName); perr == nil && found {
			if dtPodSeries, ok := dtc.GetPodBehaviorMetrics(ctx, podEntityID, start, end); ok {
				resp.Source = "dynatrace"
				resp.PodScoped = true
				resp.Points = pointsFromSeriesMap(dynatracePodSeriesToPointMap(dtPodSeries))
				// Sem pods_desired/restarts nesta granularidade (métricas de container não carregam
				// esse conceito) — scale events ficam vazios no caminho pod-scoped, diferente do
				// agregado por workload abaixo.
			}
		}
	}

	if resp.Source == "none" && dtEntityFound {
		dtSeries := dtc.GetDeploymentBehaviorMetrics(ctx, dtEntityID, start, end)
		if len(dtSeries) > 0 {
			resp.Source = "dynatrace"
			resp.Points = pointsFromSeriesMap(dynatraceSeriesToPointMap(dtSeries))
			// pods_desired (ver k8sWorkloadMetricDefs, metrics.go) alimenta ReplicasDesired desde a
			// correção dos metricId — scale events passam a existir também no caminho Dynatrace, não
			// só Prometheus (bug real: a chamada a deriveScaleEvents tinha ficado só no branch
			// Prometheus acima, apesar do comentário em dynatraceSeriesToPointMap já prometer isso).
			resp.ScaleEvents = deriveScaleEvents(resp.Points)
		}
	}

	// ─── 2c. Request/limit via API do K8s — fonte independente do Prometheus ───────────
	//
	// Só busca quando o Prometheus não já preencheu (evita uma chamada K8s redundante quando os 2
	// campos de request já vieram por outro caminho). Alimenta só a linha informativa "CPU
	// request/limit: X / Y" abaixo do gráfico — NÃO normaliza CPUUsageMilliCores/MemoryUsageMB em
	// CPUUsagePct/MemoryUsagePct.
	//
	// Tentativa revertida — achado real testando ao vivo contra um tenant real (nyr48864,
	// Deployment "cargas-web"/8 réplicas desejadas): dividir CPUUsageMilliCores/MemoryUsageMB (que
	// vêm de builtin:kubernetes.workload.cpu_usage/memory_working_set, escopo CLOUD_APPLICATION —
	// ver k8sWorkloadMetricDefs) pelo request de UM ÚNICO pod (o que este helper retorna, direto do
	// template do Deployment, sem multiplicar pela contagem de réplicas) deu 407% de uso de
	// memória — resultado implausível que expôs uma ambiguidade real: não há confirmação de que
	// esses 2 metricId agregam por SOMA entre as réplicas do workload (o que exigiria multiplicar
	// o request por ReplicasDesired antes de dividir) — uma segunda query ao vivo pra CPU (mesma
	// entidade, resolução "Inf") retornou uma magnitude bem diferente da série por hora já obtida,
	// sem uma explicação clara o suficiente pra confiar no cálculo. Preferível mostrar só o valor
	// absoluto (já correto e confirmado) do que arriscar uma % errada numa ferramenta usada em
	// troubleshooting real — reavaliar se/quando a semântica exata de agregação for confirmada
	// (ex: documentação oficial do Dynatrace ou teste controlado com workload de réplica única).
	if resp.CPURequestMillicores == 0 && resp.MemoryRequestBytes == 0 {
		cpuReq, cpuLim, memReq, memLim := h.getDeploymentResourceLimitsFromK8s(ctx, cluster, namespace, deployment)
		resp.CPURequestMillicores = cpuReq
		resp.CPULimitMillicores = cpuLim
		resp.MemoryRequestBytes = memReq
		resp.MemoryLimitBytes = memLim
	}

	// ─── 2b. Overlay de problems (Fase 2) — aditivo, não depende de qual fonte a série usou ───
	//
	// Bug real corrigido: usava só dtEntityID/dtEntityFound (CLOUD_APPLICATION, Cloud Native Full
	// Stack) — mas a MAIORIA da frota AKS roda OneAgent em modo classicFullStack, que nunca cria
	// CLOUD_APPLICATION nenhuma. Nesses clusters (o caso comum, não o exceção) dtEntityFound era
	// sempre false e o overlay nunca aparecia, mesmo com problems reais existindo no Dynatrace
	// pra aquele workload. ResolveEntityIDsForWorkload cobre os dois modos (CLOUD_APPLICATION
	// quando existe; senão PROCESS_GROUP_INSTANCE por réplica via HOST_GROUP — mesma correlação
	// já usada pelo indicador de monitoramento da aba Pods).
	if dtc != nil {
		if entityIDs, ierr := dtc.ResolveEntityIDsForWorkload(ctx, dtclient.NormalizeClusterName(cluster), namespace, deployment); ierr == nil && len(entityIDs) > 0 {
			if problems, perr := dtc.GetProblemsForEntitiesInWindow(ctx, entityIDs, start, end); perr == nil && len(problems) > 0 {
				resp.DynatraceProblems = dtProblemsToMarkers(problems)
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

// getDeploymentResourceLimitsFromK8s soma request/limit de CPU/memória de todos os containers do
// template do Deployment via API do K8s — fonte independente do Prometheus (o único caminho que
// já calculava isso, via kube_pod_container_resource_requests/_limits). Importa justamente porque
// o fallback Dynatrace de série (2a acima) entra em ação exatamente quando Prometheus NÃO está
// disponível — sem esta fonte alternativa, os valores absolutos que o Dynatrace retorna
// (CPUUsageMilliCores/MemoryUsageMB) nunca podiam ser normalizados em % (CPUUsagePct/
// MemoryUsagePct ficavam sempre 0 nesse caminho, mesmo com uso real chegando via Dynatrace).
// Soma por container (não por pod) — mesma convenção agregada da query Prometheus equivalente
// (kube_pod_container_resource_requests, soma implícita entre containers do mesmo pod via `sum`).
// Falha silenciosa (cluster inacessível, Deployment não encontrado) — campo informativo, não
// crítico pro resto da resposta, mesmo princípio de deploymentHasHPA acima.
func (h *DeploymentHandler) getDeploymentResourceLimitsFromK8s(ctx context.Context, cluster, namespace, deployment string) (cpuReqMilli, cpuLimMilli, memReqBytes, memLimBytes int64) {
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return 0, 0, 0, 0
	}
	dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, container := range dep.Spec.Template.Spec.Containers {
		if v, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuReqMilli += v.MilliValue()
		}
		if v, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuLimMilli += v.MilliValue()
		}
		if v, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			memReqBytes += v.Value()
		}
		if v, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
			memLimBytes += v.Value()
		}
	}
	return cpuReqMilli, cpuLimMilli, memReqBytes, memLimBytes
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
// remapeando a chave do Dynatrace (pods_desired) pra chave canônica usada por
// DeploymentBehaviorPoint (replicas_desired).
//
// Bug real corrigido — k8sWorkloadMetricDefs (metrics.go) usava 6 metricId que nunca retornaram
// dado nenhum pra CLOUD_APPLICATION neste tenant (404/entity-type-mismatch, ver comentário
// detalhado lá); corrigidos pros 4 selectors reais confirmados, mas 2 mudanças de cobertura ficam
// documentadas aqui porque afetam diretamente o que esta função consegue mapear:
//   - replicas_current/replicas_ready: SEM equivalente confirmado nesta família de métricas (só
//     "desejado" existe de fato, não "rodando agora"/"pronto agora") — ficam sempre 0 no caminho
//     Dynatrace, mesma limitação que replicas_desired tinha ANTES desta correção.
//   - restarts: idem, sem selector CLOUD_APPLICATION confirmado — removido daqui.
//   - replicas_desired: NOVO — antes impossível ("Sem réplicas desejadas no Dynatrace"), agora
//     preenchido via pods_desired — habilita os "scale events" (deriveScaleEvents) também no
//     caminho Dynatrace, não só Prometheus.
//
// LIMITAÇÃO CONHECIDA (documentada no plano — "cobertura quase 1:1", não total): cpu_milli/
// memory_mb do Dynatrace são valores ABSOLUTOS de uso, não percentuais relativos a request como
// no caminho Prometheus (que normaliza via kube_pod_container_resource_requests). Sem uma fonte
// de request/limit disponível neste fallback — Prometheus indisponível é justamente por que
// caímos aqui —, CPUUsagePct/MemoryUsagePct NÃO são preenchidos no caminho Dynatrace (ficam 0) —
// mas os valores absolutos em si (que a API do Dynatrace já retorna de verdade, desde a correção
// acima) não precisam ser jogados fora só porque não dá pra calcular %: mapeados pra
// cpu_absolute/memory_absolute, que pointsFromSeriesMap lê em CPUUsageMilliCores/MemoryUsageMB
// (campos NOVOS, dedicados — nunca confundir com CPUUsagePct/MemoryUsagePct, unidades diferentes).
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
	if desired, ok := raw["pods_desired"]; ok {
		out["replicas_desired"] = desired
	}
	if cpu, ok := raw["cpu_milli"]; ok {
		out["cpu_absolute"] = cpu
	}
	if mem, ok := raw["memory_mb"]; ok {
		out["memory_absolute"] = mem
	}
	return out
}

// dynatracePodSeriesToPointMap converte []MetricSeriesData (k8sContainerMetricDefs — ver
// pod_behavior_metrics.go) pro formato comum, mesmas chaves de saída (cpu_absolute/memory_absolute)
// que dynatraceSeriesToPointMap já usa pro caminho agregado — pointsFromSeriesMap não precisa saber
// qual dos dois caminhos alimentou a série. Diferente do caminho agregado, não há replicas_desired
// aqui (métricas de container não carregam esse conceito).
func dynatracePodSeriesToPointMap(series []dtclient.MetricSeriesData) map[string]map[int64]float64 {
	raw := make(map[string]map[int64]float64, len(series))
	for _, s := range series {
		m := make(map[int64]float64, len(s.Points))
		for _, p := range s.Points {
			m[p.T] = p.V
		}
		raw[s.Key] = m
	}

	out := make(map[string]map[int64]float64, 2)
	if cpu, ok := raw["cpu_milli"]; ok {
		out["cpu_absolute"] = cpu
	}
	if mem, ok := raw["memory_mb"]; ok {
		out["memory_absolute"] = mem
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
			NetworkInBytesSec:   series["network_in"][ts],
			NetworkOutBytesSec:  series["network_out"][ts],
			CPUUsageMilliCores:  series["cpu_absolute"][ts],
			MemoryUsageMB:       series["memory_absolute"][ts],
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

// dtProblemsToMarkers converte []dtclient.Problem em []DTProblemMarker pro overlay do gráfico de
// comportamento (Fase 2) — extraído em função própria pra ser testável sem precisar de um cliente
// Dynatrace real. EndTs fica nil quando o problem ainda está OPEN (EndTime == nil na origem).
func dtProblemsToMarkers(problems []dtclient.Problem) []DTProblemMarker {
	markers := make([]DTProblemMarker, 0, len(problems))
	for _, p := range problems {
		marker := DTProblemMarker{
			ProblemID: p.ProblemID,
			Title:     p.Title,
			Severity:  p.SeverityLevel,
			StartTs:   p.StartTime.UnixMilli(),
		}
		if p.EndTime != nil {
			endTs := p.EndTime.UnixMilli()
			marker.EndTs = &endTs
		}
		markers = append(markers, marker)
	}
	return markers
}
