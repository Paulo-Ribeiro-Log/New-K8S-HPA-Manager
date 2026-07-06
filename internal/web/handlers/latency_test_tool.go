package handlers

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
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
	"k8s-hpa-manager/internal/web/sse"
)

const (
	// latencyTestPodImage é pequena e já traz curl — evita instalar nada no pod efêmero.
	latencyTestPodImage = "curlimages/curl:8.10.1"
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
					Name:    "curl",
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

// runLatencyProbe roda `count` requisições HTTP via curl dentro do pod de teste num ÚNICO exec
// (não um exec por requisição — custaria uma chamada SPDY por amostra) e retorna as latências
// medidas em milissegundos, na ordem em que completaram. Linhas que o curl não conseguiu medir
// (timeout/erro de conexão) contam pra `errorCount` e não entram em `samples`.
func runLatencyProbe(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, url string, count, timeoutMs int) (samples []float64, errorCount int, err error) {

	timeoutSec := float64(timeoutMs) / 1000.0
	script := fmt.Sprintf(
		`for i in $(seq 1 %d); do curl -o /dev/null -s -w "%%{time_total}\n" --max-time %.3f %q || echo ERR; done`,
		count, timeoutSec, url,
	)

	stdout, err := execCmdInPod(ctx, clientset, restConfig, namespace, podName, "curl", []string{"sh", "-c", script})
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
	cancelFuncs    sync.Map // sessionID -> context.CancelFunc
}

// NewLatencyTestHandler cria o handler do teste de latência.
func NewLatencyTestHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *LatencyTestHandler {
	return &LatencyTestHandler{kubeManager: km, tracker: tracker, historyTracker: ht}
}

// RunLatencyTestRequest é o body do POST /run.
type RunLatencyTestRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	URL       string `json:"url"`
	Requests  int    `json:"requests"`
	TimeoutMs int    `json:"timeout_ms"`
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

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(sessionID)
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

	send("probe_run", "in_progress", fmt.Sprintf("Executando %d requisições...", req.Requests), 0.5)
	samples, errorCount, err := runLatencyProbe(ctx, clientset, restConfig, req.Namespace, podName, req.URL, req.Requests, req.TimeoutMs)
	if err != nil {
		fail("falha ao executar probe de latência", err)
		return
	}

	result := LatencyTestResult{
		Samples:       samples,
		Stats:         computeLatencyStats(samples),
		ErrorCount:    errorCount,
		TotalRequests: req.Requests,
	}

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "complete",
		Phase:     "completed",
		Message:   fmt.Sprintf("Teste concluído: %d/%d requisições bem-sucedidas", len(samples), req.Requests),
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
	if h.historyTracker == nil {
		return
	}

	status := "success"
	errMsg := ""
	if opErr != nil {
		status = "failed"
		errMsg = opErr.Error()
	}

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
