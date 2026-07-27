package handlers

import (
	"bufio"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/web/sse"
)

// PodLogsStreamHandler transmite ao vivo (Follow=true, mesmo `kubectl logs -f`) os logs de vários
// pods simultaneamente, um por goroutine, todos publicando no mesmo sessionID via o broker SSE já
// usado por outras operações de longa duração (Cordon/Drain, Health Check, Command Runner) —
// mesmo padrão de streaming linha-a-linha já usado em command_runner.go (bufio.Scanner sobre
// cmd.StdoutPipe(), aqui trocado por bufio.Scanner sobre GetLogs(...).Stream(ctx)).
//
// Diferente do endpoint de leitura única GetLogs (pods.go) — que faz uma chamada DoRaw() e
// devolve um snapshot —, aqui a conexão fica aberta indefinidamente até o cliente cancelar
// (fechar o modal) ou o contexto ser cancelado por qualquer outro motivo.
type PodLogsStreamHandler struct {
	kubeManager *config.KubeConfigManager
	tracker     *sse.ProgressTracker
	cancelFuncs sync.Map // sessionID -> context.CancelFunc
}

func NewPodLogsStreamHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker) *PodLogsStreamHandler {
	return &PodLogsStreamHandler{kubeManager: km, tracker: tracker}
}

type streamPodRef struct {
	Namespace string `json:"namespace" binding:"required"`
	Name      string `json:"name" binding:"required"`
	// Container é opcional — o frontend já resolve o primeiro container "normal" de cada pod
	// (mesma heurística de PodLogsPanel.tsx/ContainersTab.tsx) antes de montar essa lista, pra
	// não duplicar essa lógica no backend.
	Container string `json:"container"`
}

type streamAllPodLogsRequest struct {
	Cluster string         `json:"cluster" binding:"required"`
	Pods    []streamPodRef `json:"pods" binding:"required"`
	// TailLines é opcional — combinado com Follow=true, o K8s devolve as últimas N linhas e
	// depois continua streamando ao vivo (mesmo `kubectl logs --tail=N -f`), evitando tela em
	// branco enquanto espera a primeira linha nova depois de abrir o modal.
	TailLines int64 `json:"tail_lines"`
}

// StreamAll inicia o streaming ao vivo dos pods informados e devolve um session_id pra o
// frontend conectar via GET /stream-all/:sessionId (EventSource/SSE). Não bloqueia — as
// goroutines de streaming rodam em background, publicando eventos conforme as linhas chegam.
//
// POST /api/v1/pods/logs/stream-all
func (h *PodLogsStreamHandler) StreamAll(c *gin.Context) {
	var req streamAllPodLogsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if len(req.Pods) == 0 {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PODS", "pods não pode ser vazio"))
		return
	}

	// NÃO usar h.kubeManager.GetClient() aqui — o restConfig cacheado por trás dele tem
	// Timeout=30s (kubeconfig.go, GetRestConfig), e o http.Client.Timeout do Go cobre a leitura
	// do corpo INTEIRO da resposta, não só o handshake — mata qualquer streaming Follow=true
	// depois de ~30s, mesmo com dados chegando o tempo todo (confirmado empiricamente: sessão de
	// streaming morria sozinha aos ~28s mesmo sem nenhum cliente GET conectado). Construímos um
	// clientset próprio, com Timeout=0 (sem limite — o fim é controlado só pelo ctx cancelável
	// abaixo, via Cancel() ou o próprio wg.Wait()), a partir do mesmo restConfig.
	restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	streamConfig := rest.CopyConfig(restConfig)
	streamConfig.Timeout = 0
	clientset, err := kubernetes.NewForConfig(streamConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go func() {
		var wg sync.WaitGroup
		for _, pod := range req.Pods {
			wg.Add(1)
			go h.streamOnePod(ctx, &wg, clientset, sessionID, pod, req.TailLines)
		}
		wg.Wait()
		// Todas as goroutines terminaram (cancelado, ou todos os pods sumiram) — sinaliza fim
		// pro handler de Stream fechar a conexão HTTP em vez de ficar pendurado.
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID: sessionID, Type: "complete", Phase: "completed", Timestamp: time.Now(),
		})
		h.cancelFuncs.Delete(sessionID)
	}()

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// streamOnePod faz o `Follow: true` de um único pod/container e publica cada linha recebida no
// broker SSE compartilhado da sessão. Retorna quando o contexto é cancelado ou o stream do
// próprio K8s encerra (pod removido, container reiniciado, erro de rede etc.) — uma falha aqui
// não afeta as goroutines dos outros pods (cada uma é independente).
func (h *PodLogsStreamHandler) streamOnePod(ctx context.Context, wg *sync.WaitGroup, clientset kubernetes.Interface, sessionID string, pod streamPodRef, tailLines int64) {
	defer wg.Done()

	opts := &corev1.PodLogOptions{Follow: true, Timestamps: true}
	if pod.Container != "" {
		opts.Container = pod.Container
	}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}

	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return // cancelado antes mesmo de abrir o stream — não é um erro real a reportar
		}
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID: sessionID, Type: "log", Phase: "in_progress", Timestamp: time.Now(),
			Result: gin.H{"pod": pod.Name, "error": err.Error()},
		})
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	// Buffer maior que o default (64KB) — uma linha de log muito longa (stack trace, JSON grande
	// numa linha só) não deve ser silenciosamente truncada pelo scanner.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID: sessionID, Type: "log", Phase: "in_progress", Timestamp: time.Now(),
			Result: gin.H{"pod": pod.Name, "line": scanner.Text()},
		})
	}
}

// Stream conecta o cliente ao fluxo SSE de um streaming de logs em andamento — mesmo formato do
// Stream de db_test_tool.go/kafka_test_tool.go, exceto que aqui o loop NUNCA para sozinho em
// eventos "log" (só em "complete"/"error"), já que o streaming é contínuo por natureza.
//
// GET /api/v1/pods/logs/stream-all/:sessionId
func (h *PodLogsStreamHandler) Stream(c *gin.Context) {
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

// Cancel para o streaming de todos os pods de uma sessão — chamado quando o modal fecha ou o
// usuário desliga o toggle "Auto". Cancela o contexto compartilhado por todas as goroutines
// streamOnePod daquela sessão, que desbloqueia sozinho o Stream(ctx) de cada uma.
//
// POST /api/v1/pods/logs/stream-all/:sessionId/cancel
func (h *PodLogsStreamHandler) Cancel(c *gin.Context) {
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
