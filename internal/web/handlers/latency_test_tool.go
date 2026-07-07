package handlers

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/web/sse"
)

const (
	// latencyTestPodImage precisa de ping+curl (netshoot é a imagem padrão de troubleshooting
	// de rede em K8s — traz ping/curl/dig/tcpdump). Tag FIXADA de propósito (nunca `latest`) —
	// validar se ainda é a mais recente/disponível no registry antes de confiar em produção; não
	// foi possível confirmar isso deste ambiente (sem acesso à internet/registry ao implementar).
	latencyTestPodImage = "nicolaka/netshoot:v0.12"
	// latencyTestPodContainer é o nome do container dentro do pod (usado nas chamadas de exec).
	latencyTestPodContainer = "netshoot"
	// latencyTestPodActiveDeadlineSec é o cinto de segurança: o K8s mata o pod sozinho mesmo se
	// o processo do servidor morrer no meio do teste, sem depender só do cleanup() explícito.
	latencyTestPodActiveDeadlineSec int64 = 300
	latencyTestPodReadyTimeout            = 60 * time.Second
	latencyTestPodPollInterval            = 500 * time.Millisecond

	// Guardrails — teto hardcoded, não confiar só na validação do frontend (Fase 4 do plano
	// ainda vai adicionar lock de "um teste por vez" + varredura de pods órfãos).
	latencyTestMaxRequests      = 200
	latencyTestDefaultRequests  = 20
	latencyTestMaxTimeoutMs     = 10000
	latencyTestDefaultTimeoutMs = 3000
)

// Protocolos suportados pelo probe (Fase 6.1). "http"/"https" usam curl; "icmp" usa ping.
const (
	latencyProtocolHTTP  = "http"
	latencyProtocolHTTPS = "https"
	latencyProtocolICMP  = "icmp"
)

var latencyValidProtocols = map[string]bool{
	latencyProtocolHTTP:  true,
	latencyProtocolHTTPS: true,
	latencyProtocolICMP:  true,
}

// LatencyTestStats agrega estatísticas sobre as amostras de latência coletadas num teste.
type LatencyTestStats struct {
	MinMs    float64 `json:"min_ms"`
	AvgMs    float64 `json:"avg_ms"`
	MedianMs float64 `json:"median_ms"`
	P95Ms    float64 `json:"p95_ms"`
	P99Ms    float64 `json:"p99_ms"`
	MaxMs    float64 `json:"max_ms"`
}

// LatencyTestResult é o resultado completo de uma execução de teste de latência.
type LatencyTestResult struct {
	Samples       []float64        `json:"samples"` // ms, na ordem em que as requisições rodaram
	Stats         LatencyTestStats `json:"stats"`
	ErrorCount    int              `json:"error_count"`
	TotalRequests int              `json:"total_requests"`
	// Historical é o contexto complementar via DT/Prometheus (Fase 5) — best-effort, nunca
	// bloqueia o teste ativo. MetricsSource vazio quando nenhuma fonte teve dado.
	Historical LatencyHistoricalContext `json:"historical"`
}

// createTestPod cria um pod efêmero de curta duração no namespace alvo pra rodar o probe de
// latência via exec. Nasce no MESMO namespace do alvo — respeita NetworkPolicy de lá, já que a
// requisição parte de dentro, não de fora do cluster. `cleanup()` deve ser chamado sempre (defer),
// mas ActiveDeadlineSeconds garante que o K8s mata o pod sozinho mesmo se isso falhar.
func createTestPod(ctx context.Context, clientset kubernetes.Interface, namespace string) (podName string, cleanup func(), err error) {
	podName = fmt.Sprintf("latency-test-%d", rand.Int63())
	activeDeadline := latencyTestPodActiveDeadlineSec

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "latency-test-tool",
				"created-by": "k8s-hpa-manager",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &activeDeadline,
			Containers: []corev1.Container{
				{
					Name:    latencyTestPodContainer,
					Image:   latencyTestPodImage,
					Command: []string{"sleep", "300"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	if _, err = clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", nil, fmt.Errorf("falha ao criar pod de teste: %w", err)
	}

	cleanup = func() {
		delCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		gracePeriod := int64(0)
		_ = clientset.CoreV1().Pods(namespace).Delete(delCtx, podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
	}

	return podName, cleanup, nil
}

// waitPodRunning espera o pod ficar Running (poll simples) até o timeout, ou retorna erro se o
// pod terminar (Failed/Succeeded) antes disso — não deveria acontecer com `sleep 300`, mas cobre
// o caso de a imagem falhar o pull ou o namespace ter uma PodSecurityPolicy/admission bloqueando.
func waitPodRunning(ctx context.Context, clientset kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("falha ao consultar status do pod de teste: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("pod de teste terminou inesperadamente (fase: %s)", pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(latencyTestPodPollInterval):
		}
	}
	return fmt.Errorf("timeout esperando pod de teste ficar pronto (%s)", timeout)
}

// runLatencyProbe roda `count` amostras dentro do pod de teste num ÚNICO exec (não um exec por
// amostra — custaria uma chamada SPDY cada) e retorna as latências medidas em milissegundos.
// Protocol-aware (Fase 6.1): "http"/"https" usam curl (`target` é a URL completa com schema);
// "icmp" usa ping (`target` é um host puro, sem schema/path).
func runLatencyProbe(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, protocol, target string, count, timeoutMs int) (samples []float64, errorCount int, err error) {

	if protocol == latencyProtocolICMP {
		return runICMPProbe(ctx, clientset, restConfig, namespace, podName, target, count, timeoutMs)
	}
	return runHTTPProbe(ctx, clientset, restConfig, namespace, podName, target, count, timeoutMs)
}

// runHTTPProbe mede `count` requisições HTTP/HTTPS via curl. Linhas que o curl não conseguiu
// medir (timeout/erro de conexão) contam pra `errorCount` e não entram em `samples`.
func runHTTPProbe(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, url string, count, timeoutMs int) (samples []float64, errorCount int, err error) {

	timeoutSec := float64(timeoutMs) / 1000.0
	script := fmt.Sprintf(
		`for i in $(seq 1 %d); do curl -o /dev/null -s -w "%%{time_total}\n" --max-time %.3f %q || echo ERR; done`,
		count, timeoutSec, url,
	)

	stdout, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, latencyTestPodContainer, []string{"sh", "-c", script})
	if err != nil {
		return nil, 0, fmt.Errorf("falha ao executar probe de latência: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		seconds, parseErr := strconv.ParseFloat(line, 64)
		if parseErr != nil {
			errorCount++
			continue
		}
		samples = append(samples, seconds*1000.0)
	}

	return samples, errorCount, nil
}

// icmpTimeRegex extrai o valor de "time=X ms" (ou "time=X.Y ms") de cada linha de resposta do
// ping — formato comum a iputils e BusyBox ping.
var icmpTimeRegex = regexp.MustCompile(`time=([0-9.]+)\s*ms`)

// runICMPProbe mede `count` pacotes ICMP via ping — UMA única invocação (`-c count`), o próprio
// ping já dispara todos os pacotes sozinho, ao contrário do curl que precisa do for-loop do shell.
//
// `-i 0.2` (200ms entre pacotes) acelera o teste sem exigir privilégio — intervalos menores que
// 200ms são restritos a root em iputils/BusyBox ping, 200ms exatos não são.
//
// `; true` no fim força o shell a sempre sair 0: `ping` sozinho sai com código 1 quando TODOS os
// pacotes se perdem, o que faria o exec inteiro falhar e descartar o stdout — igual ao motivo do
// `|| echo ERR` no probe HTTP, aqui só descrito uma vez pra não repetir em outro protocolo depois.
//
// Risco conhecido, não contornável de forma genérica (ver LATENCY-METRICS-PLAN.md Fase 6.1):
// pod sem CAP_NET_RAW pode não conseguir abrir socket ICMP raw, dependendo do sysctl
// `net.ipv4.ping_group_range` do NÓ (não pedimos a capability aqui de propósito — clusters com
// Pod Security "restricted" rejeitam qualquer capability adicionada, e preferimos falhar com uma
// mensagem clara a arriscar quebrar a criação do pod). Se o ping falhar por permissão, o stderr
// real (capturado por `execCmdInPod`) aparece na mensagem de erro devolvida ao usuário.
func runICMPProbe(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, host string, count, timeoutMs int) (samples []float64, errorCount int, err error) {

	timeoutSec := int(math.Ceil(float64(timeoutMs) / 1000.0))
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	script := fmt.Sprintf(`ping -c %d -W %d -i 0.2 %q; true`, count, timeoutSec, host)

	stdout, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, latencyTestPodContainer, []string{"sh", "-c", script})
	if err != nil {
		return nil, 0, fmt.Errorf("falha ao executar ping: %w", err)
	}

	for _, m := range icmpTimeRegex.FindAllStringSubmatch(stdout, -1) {
		v, parseErr := strconv.ParseFloat(m[1], 64)
		if parseErr != nil {
			continue
		}
		samples = append(samples, v)
	}
	errorCount = count - len(samples)
	if errorCount < 0 {
		errorCount = 0
	}

	return samples, errorCount, nil
}

// normalizeICMPTarget aceita tanto um host puro quanto uma URL completa (ex: usuário trocou de
// HTTP pra ICMP sem limpar o campo) e devolve só o host, sem schema/path — defesa em
// profundidade, o frontend já deveria mandar limpo. Se não parsear como URL com schema, assume
// que já é um host puro e devolve como veio (só trim).
func normalizeICMPTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return u.Hostname()
}

// computeLatencyStats calcula min/avg/mediana/p95/p99/max sobre as amostras (ms). Amostras vazias
// retornam stats zeradas — o chamador decide como exibir "sem dados" (não é papel desta função).
func computeLatencyStats(samples []float64) LatencyTestStats {
	if len(samples) == 0 {
		return LatencyTestStats{}
	}

	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	return LatencyTestStats{
		MinMs:    sorted[0],
		AvgMs:    sum / float64(len(sorted)),
		MedianMs: latencyPercentile(sorted, 50),
		P95Ms:    latencyPercentile(sorted, 95),
		P99Ms:    latencyPercentile(sorted, 99),
		MaxMs:    sorted[len(sorted)-1],
	}
}

// latencyPercentile assume `sorted` já ordenado ascendente — nearest-rank, mesmo método já usado
// em `percentile95` (internal/monitoring/nodepoolpredictions/cost_analyzer.go), só generalizado
// pra qualquer p (aquele é hardcoded em 95).
func latencyPercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ─── Handler: endpoint SSE + rotas (Fase 2) ───────────────────────────────────

// LatencyTestHandler orquestra o teste de latência sob demanda: cria o pod efêmero, roda o probe
// e reporta progresso via SSE, seguindo o mesmo padrão do Command Runner (start retorna
// session_id, cliente conecta no stream, cancel força parada).
type LatencyTestHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	dtTokenStore   *storage.UserTokensStore         // contexto histórico complementar (Fase 5) — pode ser nil
	testHistory    *storage.LatencyTestHistoryStore // fonte estruturada pro grafo de topologia (Fase 6.4) — pode ser nil
	cancelFuncs    sync.Map                         // sessionID -> context.CancelFunc
	runningUsers   sync.Map                         // userEmail -> struct{} — "um teste por vez por usuário"
	seenClusters   sync.Map                         // cluster -> struct{} — só varre órfãos onde este handler já rodou algo
}

// NewLatencyTestHandler cria o handler do teste de latência e inicia a varredura periódica de
// pods órfãos em background (ver sweepOrphanPods). `dtTokens` e `testHistory` podem ser nil —
// nesse caso o contexto histórico da Fase 5 cai direto pro fallback Prometheus (ou fica vazio) e
// os resultados simplesmente não são persistidos pro grafo da Fase 6.4, respectivamente.
func NewLatencyTestHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker, dtTokens *storage.UserTokensStore, testHistory *storage.LatencyTestHistoryStore) *LatencyTestHandler {
	h := &LatencyTestHandler{kubeManager: km, tracker: tracker, historyTracker: ht, dtTokenStore: dtTokens, testHistory: testHistory}
	go h.sweepOrphanPods()
	return h
}

const (
	// latencyTestSweepInterval é a frequência da varredura de pods órfãos.
	latencyTestSweepInterval = 5 * time.Minute
	// latencyTestOrphanAge é bem acima do ActiveDeadlineSeconds (5min) — se um pod de teste
	// ainda existe depois disso, o K8s deveria tê-lo matado sozinho e não matou (falha do
	// próprio K8s) ou o cleanup() explícito falhou por algum motivo; de qualquer forma, é
	// seguro forçar a remoção.
	latencyTestOrphanAge = 10 * time.Minute
)

// sweepOrphanPods varre periodicamente os clusters já usados por este handler (não a frota
// inteira — um cluster nunca usado nunca vai ter pod de teste) procurando pods
// `app=latency-test-tool` que sobreviveram além do esperado. Rede de segurança adicional pro
// caso do cleanup() explícito não ter rodado (ex: processo do servidor morreu no meio do teste,
// e como o processo caiu, o ActiveDeadlineSeconds do K8s é a defesa que realmente importa nesse
// cenário — esta varredura cobre o caso mais raro de o próprio K8s não ter aplicado o deadline).
func (h *LatencyTestHandler) sweepOrphanPods() {
	ticker := time.NewTicker(latencyTestSweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		h.seenClusters.Range(func(key, _ interface{}) bool {
			h.sweepClusterOrphans(key.(string))
			return true
		})
	}
}

// sweepClusterOrphans deleta, num único cluster, os pods de teste mais velhos que
// latencyTestOrphanAge. Best-effort: qualquer erro (cluster inacessível, etc.) só faz pular
// esse ciclo — tenta de novo no próximo tick.
func (h *LatencyTestHandler) sweepClusterOrphans(cluster string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=latency-test-tool",
	})
	if err != nil {
		return
	}

	for _, pod := range pods.Items {
		if time.Since(pod.CreationTimestamp.Time) < latencyTestOrphanAge {
			continue
		}
		delCtx, delCancel := context.WithTimeout(context.Background(), 10*time.Second)
		gracePeriod := int64(0)
		_ = clientset.CoreV1().Pods(pod.Namespace).Delete(delCtx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
		delCancel()
	}
}

// RunLatencyTestRequest é o body do POST /run.
type RunLatencyTestRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	URL       string `json:"url"`
	Requests  int    `json:"requests"`
	TimeoutMs int    `json:"timeout_ms"`
	// Protocol: "http" | "https" | "icmp" (default "http", Fase 6.1). Pra "icmp", URL deve ser um
	// host puro (sem schema/path) — normalizado em normalizeICMPTarget se vier com schema mesmo
	// assim (defesa em profundidade, o frontend já deveria enviar limpo).
	Protocol string `json:"protocol"`
}

// Run inicia o teste de latência e retorna um session_id para streaming SSE.
// POST /api/v1/latency-test/run
func (h *LatencyTestHandler) Run(c *gin.Context) {
	var req RunLatencyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	req.Cluster = strings.TrimSpace(req.Cluster)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.URL = strings.TrimSpace(req.URL)
	if req.Cluster == "" || req.Namespace == "" || req.URL == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e url são obrigatórios"))
		return
	}

	req.Protocol = strings.ToLower(strings.TrimSpace(req.Protocol))
	if req.Protocol == "" {
		req.Protocol = latencyProtocolHTTP
	}
	if !latencyValidProtocols[req.Protocol] {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_PROTOCOL", "protocol deve ser 'http', 'https' ou 'icmp'"))
		return
	}
	if req.Protocol == latencyProtocolICMP {
		req.URL = normalizeICMPTarget(req.URL)
	}

	if req.Requests <= 0 {
		req.Requests = latencyTestDefaultRequests
	}
	if req.Requests > latencyTestMaxRequests {
		req.Requests = latencyTestMaxRequests
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = latencyTestDefaultTimeoutMs
	}
	if req.TimeoutMs > latencyTestMaxTimeoutMs {
		req.TimeoutMs = latencyTestMaxTimeoutMs
	}

	userInfo := GetUserInfoForHistory(c)

	// "Um teste por vez por usuário" — evita empilhar pods de teste se o mesmo analista disparar
	// múltiplos testes em abas/janelas diferentes.
	lockKey := userInfo.Email
	if lockKey == "" {
		lockKey = "unknown"
	}
	if _, alreadyRunning := h.runningUsers.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("TEST_ALREADY_RUNNING",
			"você já tem um teste de latência em andamento — aguarde terminar ou cancele antes de iniciar outro"))
		return
	}

	h.seenClusters.Store(req.Cluster, struct{}{})

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(sessionID)
		defer h.runningUsers.Delete(lockKey)
		h.runTest(ctx, sessionID, req, userInfo)
	}()

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// Stream conecta o cliente ao fluxo SSE de um teste em andamento.
// GET /api/v1/latency-test/stream/:sessionId
func (h *LatencyTestHandler) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(event)); err != nil {
				return
			}
			c.Writer.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

// Cancel força a parada de um teste em andamento — o cleanup do pod roda de qualquer forma
// (context.Background() próprio dentro de createTestPod), então cancelar não deixa pod órfão.
// POST /api/v1/latency-test/cancel/:sessionId
func (h *LatencyTestHandler) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	if val, ok := h.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
		h.cancelFuncs.Delete(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}

// runTest executa o fluxo completo (criar pod → aguardar ready → rodar probe → resultado),
// reportando progresso via SSE a cada etapa. Roda em goroutine própria (disparada por Run).
func (h *LatencyTestHandler) runTest(ctx context.Context, sessionID string, req RunLatencyTestRequest, userInfo history.UserInfo) {
	start := time.Now()

	send := func(evtType, phase, message string, progress float64) {
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      evtType,
			Phase:     phase,
			Message:   message,
			Progress:  progress,
			Timestamp: time.Now(),
			Cluster:   req.Cluster,
		})
	}

	fail := func(stage string, err error) {
		send("error", "failed", fmt.Sprintf("%s: %v", stage, err), 1.0)
		h.logHistory(req, userInfo, start, nil, fmt.Errorf("%s: %w", stage, err))
	}

	send("init", "started", "Iniciando teste de latência...", 0.05)

	// Contexto histórico (Fase 5) roda em paralelo com o teste ativo inteiro — nunca deve
	// atrasar o resultado principal. Contexto/timeout PRÓPRIOS (não o `ctx` do teste): mesmo se
	// o usuário cancelar o teste ativo, deixamos essa busca best-effort terminar sozinha até o
	// timeout dela (ou simplesmente descartamos o resultado, se o `runTest` já tiver retornado).
	histCh := make(chan LatencyHistoricalContext, 1)
	go func() {
		histCtx, histCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer histCancel()
		histCh <- h.fetchHistoricalLatencyContext(histCtx, req.Cluster, req.Namespace, req.URL)
	}()

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		fail("falha ao conectar no cluster", err)
		return
	}
	restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
	if err != nil {
		fail("falha ao obter configuração do cluster", err)
		return
	}

	send("pod_create", "in_progress", "Criando pod de teste...", 0.15)
	podName, cleanup, err := createTestPod(ctx, clientset, req.Namespace)
	if err != nil {
		fail("falha ao criar pod de teste", err)
		return
	}
	defer cleanup()

	send("pod_wait", "in_progress", "Aguardando pod ficar pronto...", 0.3)
	if err := waitPodRunning(ctx, clientset, req.Namespace, podName, latencyTestPodReadyTimeout); err != nil {
		fail("pod de teste não ficou pronto", err)
		return
	}

	probeNoun := "requisições"
	if req.Protocol == latencyProtocolICMP {
		probeNoun = "pacotes ICMP"
	}
	send("probe_run", "in_progress", fmt.Sprintf("Executando %d %s...", req.Requests, probeNoun), 0.5)
	samples, errorCount, err := runLatencyProbe(ctx, clientset, restConfig, req.Namespace, podName, req.Protocol, req.URL, req.Requests, req.TimeoutMs)
	if err != nil {
		fail("falha ao executar probe de latência", err)
		return
	}

	// Espera o contexto histórico só até um teto curto — se ainda não voltou, segue sem ele
	// (best-effort de verdade: o resultado do teste ativo nunca fica refém disso).
	var historical LatencyHistoricalContext
	select {
	case historical = <-histCh:
	case <-time.After(2 * time.Second):
	}

	result := LatencyTestResult{
		Samples:       samples,
		Stats:         computeLatencyStats(samples),
		ErrorCount:    errorCount,
		Historical:    historical,
		TotalRequests: req.Requests,
	}

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "complete",
		Phase:     "completed",
		Message:   fmt.Sprintf("Teste concluído: %d de %d %s com sucesso", len(samples), req.Requests, probeNoun),
		Progress:  1.0,
		Timestamp: time.Now(),
		Cluster:   req.Cluster,
		Result:    result,
	})
	h.logHistory(req, userInfo, start, &result, nil)
}

// logHistory registra a execução no HistoryTracker — gera carga real no cluster alvo, vale
// trilha de auditoria como outras operações sensíveis.
func (h *LatencyTestHandler) logHistory(req RunLatencyTestRequest, userInfo history.UserInfo, start time.Time, result *LatencyTestResult, opErr error) {
	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}

	if h.historyTracker != nil {
		after := map[string]interface{}{
			"namespace": req.Namespace,
			"url":       req.URL,
			"requests":  req.Requests,
		}
		if result != nil {
			after["stats"] = result.Stats
			after["error_count"] = result.ErrorCount
		}

		h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "latency_test",
			Resource:  req.URL,
			Cluster:   req.Cluster,
			Status:    status,
			After:     after,
			Duration:  time.Since(start).Milliseconds(),
			ErrorMsg:  errMsg,
		})
	}

	// Dual-write (Fase 6.4): a fonte estruturada só recebe testes que de fato rodaram até o fim
	// (result != nil) — um teste que falhou antes de gerar amostra nenhuma não tem P95/P99
	// significativo pra registrar aqui (o HistoryTracker acima já cobre a falha como auditoria).
	// Best-effort: erro de persistência aqui nunca deve afetar a resposta já enviada ao usuário.
	if h.testHistory != nil && result != nil {
		_ = h.testHistory.Save(storage.LatencyTestRecord{
			Cluster:       req.Cluster,
			Namespace:     req.Namespace,
			Target:        req.URL,
			Protocol:      req.Protocol,
			P95Ms:         result.Stats.P95Ms,
			P99Ms:         result.Stats.P99Ms,
			ErrorCount:    result.ErrorCount,
			TotalRequests: result.TotalRequests,
			TestedAt:      time.Now(),
			TestedBy:      userInfo.Email,
		})
	}
}
