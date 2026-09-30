package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"k8s-hpa-manager/internal/config"
)

// PodExecHandler gerencia execução de comandos e debug em pods
type PodExecHandler struct {
	kubeManager *config.KubeConfigManager
	upgrader    websocket.Upgrader
}

// allowedOrigins lista as origens permitidas para WebSocket
var allowedOrigins = map[string]bool{
	"http://localhost:8080":  true,
	"https://localhost:8080": true,
	"http://localhost:5173":  true, // Vite dev server
	"https://localhost:5173": true,
	"http://127.0.0.1:8080":  true,
	"https://127.0.0.1:8080": true,
	"http://127.0.0.1:5173":  true,
	"https://127.0.0.1:5173": true,
}

// NewPodExecHandler cria um novo handler de exec
func NewPodExecHandler(km *config.KubeConfigManager) *PodExecHandler {
	return &PodExecHandler{
		kubeManager: km,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Se não tem Origin header (ex: conexão direta), permite
				if origin == "" {
					return true
				}
				// Verifica se a origem está na lista permitida
				if allowedOrigins[origin] {
					return true
				}
				// Em produção, permitir origens que começam com o mesmo host
				host := r.Host
				if strings.HasPrefix(origin, "http://"+host) || strings.HasPrefix(origin, "https://"+host) {
					return true
				}
				log.Printf("[SHELL] Rejected WebSocket connection from origin: %s", origin)
				return false
			},
			ReadBufferSize:  8192, // Aumentado de 1024 para 8KB
			WriteBufferSize: 8192, // Aumentado de 1024 para 8KB
		},
	}
}

// HandleShell gerencia conexão WebSocket para shell interativo
func (h *PodExecHandler) HandleShell(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	containerName := c.Query("container")
	shell := c.Query("shell")

	log.Printf("[SHELL] Connection request - cluster=%s, namespace=%s, pod=%s, container=%s, shell=%s",
		cluster, namespace, podName, containerName, shell)

	if shell == "" {
		shell = "/bin/bash"
	}

	if containerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "container parameter is required",
		})
		return
	}

	// Get Kubernetes client
	clientset, restConfig, err := h.getClientAndConfig(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get client: %v", err),
		})
		return
	}

	// Upgrade to WebSocket
	log.Printf("[SHELL] Attempting WebSocket upgrade...")
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[SHELL] Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("[SHELL] WebSocket upgrade successful")

	// Execute shell
	h.execInPod(c.Request.Context(), conn, clientset, restConfig, namespace, podName, containerName, shell, false, "", false)
}

// HandleDebug cria ephemeral debug container e conecta
func (h *PodExecHandler) HandleDebug(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	targetContainer := c.Query("container")
	shell := c.Query("shell")
	image := c.Query("image")

	log.Printf("[DEBUG] Connection request - cluster=%s, namespace=%s, pod=%s, container=%s, shell=%s, image=%s",
		cluster, namespace, podName, targetContainer, shell, image)

	if shell == "" {
		shell = "/bin/bash"
	}

	if image == "" {
		image = "nicolaka/netshoot"
	}

	if targetContainer == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "container parameter is required",
		})
		return
	}

	// Get Kubernetes client
	clientset, restConfig, err := h.getClientAndConfig(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get client: %v", err),
		})
		return
	}

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()

	// Step 1: Check for existing ephemeral debug container or create new
	debugContainerName, isReused, err := h.getOrCreateEphemeralContainer(ctx, clientset, namespace, podName, targetContainer, image, conn)
	if err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to get/create ephemeral container: %v", err))
		return
	}

	// Step 2: Wait for ephemeral container to be ready
	if !isReused {
		h.sendOutput(conn, "\x1b[1;36m⏳ Waiting for container to start...\x1b[0m\r\n")
		err = h.waitForEphemeralContainer(ctx, clientset, namespace, podName, debugContainerName, 30*time.Second)
		if err != nil {
			h.sendError(conn, fmt.Sprintf("Ephemeral container not ready: %v", err))
			return
		}
		h.sendOutput(conn, "\x1b[1;32m✓ Container ready!\x1b[0m\r\n\r\n")
	}

	// Step 3: Exec into ephemeral container (o heartbeat é tocado já na abertura da sessão)
	h.execInPod(ctx, conn, clientset, restConfig, namespace, podName, debugContainerName, shell, true, image, isReused)
}

// getOrCreateEphemeralContainer verifica se já existe um container de debug ou cria um novo
func (h *PodExecHandler) getOrCreateEphemeralContainer(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, podName, targetContainer, image string,
	conn *websocket.Conn,
) (string, bool, error) {
	// Get current pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", false, fmt.Errorf("failed to get pod: %w", err)
	}

	// Check for existing debug containers with the same image and target
	for _, ec := range pod.Spec.EphemeralContainers {
		if ec.Image == image && ec.TargetContainerName == targetContainer && isWatchdogDebugContainer(ec) {
			// Check if container is still running
			for _, status := range pod.Status.EphemeralContainerStatuses {
				if status.Name == ec.Name && status.State.Running != nil {
					h.sendOutput(conn, "\r\n\x1b[1;32m♻️  Reusing existing ephemeral debug container...\x1b[0m\r\n")
					h.sendOutput(conn, fmt.Sprintf("\x1b[1;33m🛠️  Container:\x1b[0m %s\r\n", ec.Name))
					h.sendOutput(conn, fmt.Sprintf("\x1b[1;33m🎯 Target:\x1b[0m %s\r\n\r\n", targetContainer))
					h.sendOutput(conn, debugIdleNotice)
					return ec.Name, true, nil
				}
			}
		}
	}

	// No suitable existing container found, create a new one
	debugContainerName := nextDebugContainerName(pod)

	h.sendOutput(conn, "\r\n\x1b[1;34m⚡ Creating ephemeral debug container...\x1b[0m\r\n")
	h.sendOutput(conn, fmt.Sprintf("\x1b[1;33m🛠️  Image:\x1b[0m %s\r\n", image))
	h.sendOutput(conn, fmt.Sprintf("\x1b[1;33m🎯 Target:\x1b[0m %s\r\n\r\n", targetContainer))
	h.sendOutput(conn, debugIdleNotice)

	err = h.createEphemeralContainer(ctx, clientset, namespace, podName, debugContainerName, targetContainer, image)
	if err != nil {
		return "", false, err
	}

	return debugContainerName, false, nil
}

// debugContainerBaseName identifica no pod os containers de debug criados por esta ferramenta.
const debugContainerBaseName = "k8s-hpa-test-debug"

// nextDebugContainerName devolve debugContainerBaseName ou, se já usado no pod (ephemeral
// containers encerrados continuam no spec e o nome precisa ser único), o primeiro sufixo -2, -3…
// livre entre todos os containers do pod.
func nextDebugContainerName(pod *corev1.Pod) string {
	used := map[string]bool{}
	for _, c := range pod.Spec.Containers {
		used[c.Name] = true
	}
	for _, c := range pod.Spec.InitContainers {
		used[c.Name] = true
	}
	for _, ec := range pod.Spec.EphemeralContainers {
		used[ec.Name] = true
	}
	name := debugContainerBaseName
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", debugContainerBaseName, i)
	}
	return name
}

// createEphemeralContainer cria um ephemeral debug container
func (h *PodExecHandler) createEphemeralContainer(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, podName, debugName, targetContainer, image string,
) error {
	// Get current pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	// Check if debug container already exists
	for _, ec := range pod.Spec.EphemeralContainers {
		if ec.Name == debugName {
			return nil // Already exists
		}
	}

	// Define ephemeral container
	ephemeralContainer := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            debugName,
			Image:           image,
			Command:         debugWatchdogCommand,
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
		TargetContainerName: targetContainer,
	}

	// Prepare patch
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephemeralContainer)

	// Create JSON patch
	patchData, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"ephemeralContainers": []map[string]interface{}{
				{
					"name":                debugName,
					"image":               image,
					"targetContainerName": targetContainer,
					"command":             debugWatchdogCommand,
					"imagePullPolicy":     "IfNotPresent",
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	// Apply patch
	_, err = clientset.CoreV1().Pods(namespace).Patch(
		ctx,
		podName,
		types.StrategicMergePatchType,
		patchData,
		metav1.PatchOptions{},
		"ephemeralcontainers",
	)

	return err
}

// waitForEphemeralContainer aguarda o container estar pronto
func (h *PodExecHandler) waitForEphemeralContainer(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, podName, containerName string,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Check ephemeral container status
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name == containerName {
				if status.State.Running != nil {
					return nil // Container is running
				}
				if status.State.Terminated != nil {
					return fmt.Errorf("container terminated: %s", status.State.Terminated.Reason)
				}
				if status.State.Waiting != nil && status.State.Waiting.Reason != "ContainerCreating" {
					return fmt.Errorf("container waiting: %s", status.State.Waiting.Reason)
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for ephemeral container to start")
}

// execInPod executa shell no pod usando SPDY
func (h *PodExecHandler) execInPod(
	ctx context.Context,
	conn *websocket.Conn,
	clientset kubernetes.Interface,
	restConfig *rest.Config,
	namespace, podName, containerName, shell string,
	isEphemeral bool,
	image string,
	isReused bool,
) {
	// Create exec request with UTF-8 locale support
	// Use C.UTF-8 which is available in most containers (more universal than pt_BR)
	command := []string{
		"/bin/sh", "-c",
		fmt.Sprintf("export LANG=C.UTF-8 LC_ALL=C.UTF-8 TERM=xterm-256color && exec %s", shell),
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to create executor: %v", err))
		return
	}

	// Create terminal session wrapper
	session := &TerminalSession{
		conn:        conn,
		sizeCh:      make(chan remotecommand.TerminalSize, 5), // Buffer aumentado para evitar deadlock
		isEphemeral: isEphemeral,
		image:       image,
		isReused:    isReused,
	}

	if isEphemeral {
		runInDebug := func(script string) {
			cmdCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := execCmdInPod(cmdCtx, clientset, restConfig, namespace, podName, containerName, []string{"sh", "-c", script}); err != nil {
				log.Printf("[DEBUG] %s/%s %s: %q falhou: %v", namespace, podName, containerName, script, err)
			}
		}
		session.markActivity()
		go runInDebug(debugTouchHeartbeat)
		session.onHeartbeat = func() { go runInDebug(debugTouchHeartbeat) }
		session.onTerminate = func() {
			session.stopped.Store(true)
			session.writeNotice("\r\n\x1b[1;33m⏹  Encerrando o container de debug...\x1b[0m\r\n")
			runInDebug(debugStopUnconditional)
		}
		done := make(chan struct{})
		defer close(done)
		go session.watchIdle(done, func() {
			session.stopped.Store(true)
			runInDebug(debugStopIfNoOtherSession)
		})
	}

	// Send welcome message
	if isEphemeral && !isReused {
		session.sendWelcomeMessage()
	}

	// Start streaming
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             session,
		Stdout:            session,
		Stderr:            session,
		Tty:               true,
		TerminalSizeQueue: session,
	})

	if session.stopped.Load() {
		// Encerramento intencional (botão ou ociosidade): o fim do stream é esperado.
		session.writeNotice("\x1b[1;32m✓ Container de debug encerrado.\x1b[0m\r\n")
		return
	}
	if err != nil {
		log.Printf("Stream error: %v", err)
		session.writeNotice(fmt.Sprintf("\r\n\x1b[1;31m❌ Error: Stream error: %v\x1b[0m\r\n", err))
	}
}

// getClientAndConfig retorna clientset e rest config
func (h *PodExecHandler) getClientAndConfig(cluster string) (kubernetes.Interface, *rest.Config, error) {
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, nil, err
	}

	restConfig, err := h.kubeManager.GetRestConfig(cluster)
	if err != nil {
		return nil, nil, err
	}

	return clientset, restConfig, nil
}

// sendOutput envia output para o WebSocket
func (h *PodExecHandler) sendOutput(conn *websocket.Conn, text string) {
	msg := map[string]interface{}{
		"type": "output",
		"data": text,
	}
	conn.WriteJSON(msg)
}

// sendError envia erro para o WebSocket
func (h *PodExecHandler) sendError(conn *websocket.Conn, text string) {
	msg := map[string]interface{}{
		"type": "output",
		"data": fmt.Sprintf("\r\n\x1b[1;31m❌ Error: %s\x1b[0m\r\n", text),
	}
	conn.WriteJSON(msg)
}

// Ciclo de vida do ephemeral container de debug. A API do Kubernetes não permite remover um
// ephemeral container do pod (a entrada fica no spec até o pod ser recriado); o que dá pra fazer é
// ENCERRAR o processo principal dele — o container vai pra Terminated e para de consumir recursos.
// Antes o processo principal era um `/bin/sh` esperando stdin pra sempre, então o container ficava
// Running indefinidamente depois que o terminal fechava.
//
// `kill 1` NÃO serve: com targetContainerName o container compartilha o namespace de processos
// do alvo, e o PID 1 ali é a própria aplicação. Por isso o processo principal virou um laço de
// vigia que sai sozinho quando /tmp/.stop existe (botão "Encerrar container" ou ociosidade) ou
// quando /tmp/.hb (heartbeat) fica debugHeartbeatMaxAgeSec sem ser tocado — backstop que funciona
// mesmo com o browser fechado ou o servidor desligado. /tmp é do filesystem do próprio container
// de debug, nunca do app. `stat -c %Y`/`date +%s` existem no BusyBox e no coreutils.
const (
	debugIdleTimeout        = 10 * time.Minute
	debugIdleWarnBefore     = 1 * time.Minute
	debugHeartbeatThrottle  = 60 * time.Second
	debugHeartbeatMaxAgeSec = 660 // 11 min: margem sobre o timeout do servidor (heartbeat é throttled)
	debugHeartbeatFile      = "/tmp/.hb"
	debugStopFile           = "/tmp/.stop"
	debugHeartbeatAgeExpr   = `$(( $(date +%s) - $(stat -c %Y ` + debugHeartbeatFile + ` 2>/dev/null || echo 0) ))`
)

var (
	debugWatchdogCommand = []string{"/bin/sh", "-c",
		"touch " + debugHeartbeatFile + "; rm -f " + debugStopFile + "; " +
			"while [ ! -e " + debugStopFile + " ] && [ " + debugHeartbeatAgeExpr + " -lt " + fmt.Sprint(debugHeartbeatMaxAgeSec) + " ]; do sleep 15; done"}
	debugTouchHeartbeat    = "touch " + debugHeartbeatFile
	debugStopUnconditional = "touch " + debugStopFile
	// Ociosidade de UMA sessão só encerra o container se nenhuma outra sessão (reuso) tocou o
	// heartbeat nos últimos 2 min — sessões ativas tocam a cada debugHeartbeatThrottle.
	debugStopIfNoOtherSession = "[ " + debugHeartbeatAgeExpr + " -ge 120 ] && touch " + debugStopFile + "; true"
	debugIdleNotice           = "\x1b[1;36mℹ️  O container de debug é encerrado após 10 min sem atividade, ou pelo botão \"Encerrar container\".\x1b[0m\r\n\r\n"
)

// isWatchdogDebugContainer — só reaproveita containers criados com o laço de vigia; os antigos
// (processo principal `/bin/sh`) nunca se encerram e não respondem ao botão.
func isWatchdogDebugContainer(ec corev1.EphemeralContainer) bool {
	return len(ec.Command) == len(debugWatchdogCommand) && strings.Join(ec.Command, "\x00") == strings.Join(debugWatchdogCommand, "\x00")
}

// TerminalSession implementa io.Reader, io.Writer e remotecommand.TerminalSizeQueue
type TerminalSession struct {
	conn        *websocket.Conn
	sizeCh      chan remotecommand.TerminalSize
	isEphemeral bool
	image       string
	isReused    bool

	// Só no ephemeral debug: atividade (entrada ou saída) mantém o heartbeat; o monitor de
	// ociosidade escreve avisos em paralelo ao stream, daí o mutex nas escritas no WebSocket.
	writeMu       sync.Mutex
	activityMu    sync.Mutex
	lastActivity  time.Time
	lastHeartbeat time.Time
	onHeartbeat   func()
	onTerminate   func()
	stopped       atomic.Bool
}

// markActivity registra atividade e dispara o heartbeat no container, no máximo 1x por minuto.
func (t *TerminalSession) markActivity() {
	t.activityMu.Lock()
	now := time.Now()
	t.lastActivity = now
	fire := t.onHeartbeat != nil && now.Sub(t.lastHeartbeat) >= debugHeartbeatThrottle
	if fire {
		t.lastHeartbeat = now
	}
	t.activityMu.Unlock()
	if fire {
		t.onHeartbeat()
	}
}

func (t *TerminalSession) idleFor() time.Duration {
	t.activityMu.Lock()
	defer t.activityMu.Unlock()
	return time.Since(t.lastActivity)
}

// writeNotice escreve uma mensagem do próprio servidor no terminal SEM contar como atividade.
func (t *TerminalSession) writeNotice(text string) {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.conn.WriteJSON(map[string]interface{}{"type": "output", "data": text})
}

// watchIdle avisa 1 min antes e, aos 10 min sem atividade, encerra o container (se nenhuma outra
// sessão o estiver usando) e fecha o WebSocket — o que termina o stream do exec.
func (t *TerminalSession) watchIdle(done <-chan struct{}, stop func()) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	warned := false
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			idle := t.idleFor()
			switch {
			case idle >= debugIdleTimeout:
				t.writeNotice("\r\n\x1b[1;33m⏹  10 min sem atividade — encerrando o container de debug.\x1b[0m\r\n")
				stop()
				t.conn.Close()
				return
			case idle >= debugIdleTimeout-debugIdleWarnBefore && !warned:
				warned = true
				t.writeNotice("\r\n\x1b[1;33m⚠  Sem atividade há 9 min — o container de debug será encerrado em 1 min.\x1b[0m\r\n")
			case idle < debugIdleTimeout-debugIdleWarnBefore:
				warned = false
			}
		}
	}
}

// Read lê input do WebSocket
func (t *TerminalSession) Read(p []byte) (int, error) {
	var msg struct {
		Type string `json:"type"`
		Data string `json:"data"`
		Size *struct {
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		} `json:"size,omitempty"`
	}

	err := t.conn.ReadJSON(&msg)
	if err != nil {
		return 0, err
	}

	switch msg.Type {
	case "terminate":
		if t.onTerminate != nil {
			t.onTerminate()
		}
		return 0, nil
	case "input":
		if t.isEphemeral {
			t.markActivity()
		}
		data := []byte(msg.Data)
		// FIX: Prevenir buffer overflow - truncar se necessário
		if len(data) > len(p) {
			log.Printf("[SHELL] Warning: Input truncated from %d to %d bytes", len(data), len(p))
			data = data[:len(p)]
		}
		n := copy(p, data)
		return n, nil
	case "resize":
		if msg.Size != nil {
			select {
			case t.sizeCh <- remotecommand.TerminalSize{
				Width:  msg.Size.Cols,
				Height: msg.Size.Rows,
			}:
			default:
			}
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// Write escreve output para o WebSocket
func (t *TerminalSession) Write(p []byte) (int, error) {
	// FIX: Validar UTF-8 para evitar JSON corrompido
	data := string(p)
	if !utf8.ValidString(data) {
		// Substituir bytes inválidos por caractere de substituição Unicode
		data = strings.ToValidUTF8(data, "�")
	}

	if t.isEphemeral {
		t.markActivity()
	}

	msg := map[string]interface{}{
		"type": "output",
		"data": data,
	}
	t.writeMu.Lock()
	err := t.conn.WriteJSON(msg)
	t.writeMu.Unlock()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Next retorna o próximo tamanho do terminal
func (t *TerminalSession) Next() *remotecommand.TerminalSize {
	size := <-t.sizeCh
	return &size
}

// sendWelcomeMessage envia mensagem de boas vindas para ephemeral debug
func (t *TerminalSession) sendWelcomeMessage() {
	welcome := fmt.Sprintf(
		"\r\n\x1b[1;32m╔═══════════════════════════════════════════════════════════════╗\x1b[0m\r\n"+
			"\x1b[1;32m║\x1b[0m  \x1b[1;33m🛠️  Ephemeral Debug Container\x1b[0m                             \x1b[1;32m║\x1b[0m\r\n"+
			"\x1b[1;32m║\x1b[0m  \x1b[1;36mImage:\x1b[0m %-48s \x1b[1;32m║\x1b[0m\r\n"+
			"\x1b[1;32m╚═══════════════════════════════════════════════════════════════╝\x1b[0m\r\n\r\n",
		t.image,
	)
	t.Write([]byte(welcome))
}
