package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/sanitizer"
	"k8s-hpa-manager/internal/web/sse"
)

// CommandRunnerHandler gerencia execução de comandos em lote em múltiplos clusters/namespaces.
type CommandRunnerHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	aiHandler      *AIDiagnosticsHandler // pode ser nil
}

// CommandTarget é um par cluster+namespace alvo de execução.
type CommandTarget struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}

// ExecuteCommandRequest é o body do POST /execute.
type ExecuteCommandRequest struct {
	Targets    []CommandTarget `json:"targets"`
	Command    string          `json:"command"`
	Type       string          `json:"type"`        // "kubectl" | "sh" | "bash" | "python" | "go"
	TimeoutSec int             `json:"timeout_sec"` // default 300, max 1800
}

// GenerateCommandRequest é o body do POST /generate.
type GenerateCommandRequest struct {
	Prompt    string `json:"prompt"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	AIEmail   string `json:"ai_email"`
	CmdType   string `json:"cmd_type"` // linguagem: kubectl | python | go | sh | bash
}

// allowedCmdTypes define os tipos de comando aceitos.
var allowedCmdTypes = map[string]bool{
	"kubectl": true,
	"sh":      true,
	"bash":    true,
	"python":  true,
	"python3": true,
	"go":      true,
}

// NewCommandRunnerHandler cria o handler.
func NewCommandRunnerHandler(
	km *config.KubeConfigManager,
	tracker *sse.ProgressTracker,
	ht *history.HistoryTracker,
	aiHandler *AIDiagnosticsHandler,
) *CommandRunnerHandler {
	return &CommandRunnerHandler{
		kubeManager:    km,
		tracker:        tracker,
		historyTracker: ht,
		aiHandler:      aiHandler,
	}
}

// Execute inicia a execução em lote e retorna um session_id para streaming SSE.
// POST /api/v1/command-runner/execute
func (h *CommandRunnerHandler) Execute(c *gin.Context) {
	var req ExecuteCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	if len(req.Targets) == 0 {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_TARGETS", "Selecione pelo menos um cluster/namespace"))
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_COMMAND", "Comando não pode ser vazio"))
		return
	}
	if !allowedCmdTypes[req.Type] {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_TYPE", "Tipo inválido: use kubectl, sh, bash, python ou go"))
		return
	}
	if req.TimeoutSec <= 0 || req.TimeoutSec > 1800 {
		req.TimeoutSec = 300
	}

	sessionID := uuid.New().String()
	go h.runParallel(sessionID, req)

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// Stream conecta o cliente ao fluxo SSE de uma execução em andamento.
// GET /api/v1/command-runner/stream/:sessionId
func (h *CommandRunnerHandler) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Replay de eventos perdidos (reconexão)
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

// GenerateCommand usa AI para gerar um comando a partir de uma descrição em linguagem natural.
// POST /api/v1/command-runner/generate
func (h *CommandRunnerHandler) GenerateCommand(c *gin.Context) {
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, errorResponse("AI_UNAVAILABLE", "AI não configurado neste servidor"))
		return
	}

	var req GenerateCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PROMPT", "Prompt não pode ser vazio"))
		return
	}

	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	prompt := buildAIPrompt(req.Prompt, req.Cluster, req.Namespace, req.CmdType)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	result, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gerar comando: " + err.Error()})
		return
	}

	result = cleanAICommandResponse(result)

	c.JSON(http.StatusOK, gin.H{
		"command": result,
		"type":    detectCommandType(result),
	})
}

// ─── execução paralela ────────────────────────────────────────────────────────

func (h *CommandRunnerHandler) runParallel(sessionID string, req ExecuteCommandRequest) {
	total := len(req.Targets)

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "init",
		Phase:     "started",
		Message:   fmt.Sprintf("Iniciando execução em %d target(s)...", total),
		Progress:  0.0,
		Timestamp: time.Now(),
	})

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		completed int
		hasError  bool
	)

	for _, target := range req.Targets {
		wg.Add(1)
		go func(t CommandTarget) {
			defer wg.Done()
			ok := h.runForTarget(sessionID, t, req.Command, req.Type, req.TimeoutSec)

			mu.Lock()
			completed++
			if !ok {
				hasError = true
			}
			progress := float64(completed) / float64(total)
			mu.Unlock()

			// Marcador de fim por cluster
			h.tracker.SendToClient(sessionID, sse.ProgressEvent{
				ID:        sessionID,
				Type:      "cluster_done",
				Phase:     "in_progress",
				Cluster:   t.Cluster,
				Message:   fmt.Sprintf("[%s/%s] Concluído", t.Cluster, t.Namespace),
				Progress:  progress,
				Timestamp: time.Now(),
			})
		}(target)
	}

	wg.Wait()

	finalType := "complete"
	finalMsg := fmt.Sprintf("Execução finalizada em %d target(s)", total)
	if hasError {
		finalMsg += " (com erros)"
	}

	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      finalType,
		Phase:     "completed",
		Message:   finalMsg,
		Progress:  1.0,
		Timestamp: time.Now(),
	})

	// Audit log
	if h.historyTracker != nil {
		clusterNames := make([]string, 0, len(req.Targets))
		for _, t := range req.Targets {
			clusterNames = append(clusterNames, t.Cluster)
		}
		h.historyTracker.Log(history.HistoryEntry{
			Action:  "command_runner",
			Resource: req.Type + ": " + truncate(req.Command, 60),
			Cluster: strings.Join(clusterNames, ","),
			Before:  map[string]interface{}{"targets": req.Targets},
			After:   map[string]interface{}{"success": !hasError},
			Status:  map[bool]string{false: "success", true: "failed"}[hasError],
		})
	}
}

// runForTarget executa o comando num único cluster/namespace e envia o output via SSE linha a linha.
// Retorna true se o comando terminou com exit code 0.
func (h *CommandRunnerHandler) runForTarget(sessionID string, target CommandTarget, command, cmdType string, timeoutSec int) bool {
	// Substituir placeholders
	resolved := strings.ReplaceAll(command, "{{cluster}}", target.Cluster)
	resolved = strings.ReplaceAll(resolved, "{{namespace}}", target.Namespace)

	script := buildShellScript(resolved, cmdType, target.Cluster, target.Namespace)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", script)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO interno] %v", err), true)
		return false
	}
	cmd.Stderr = cmd.Stdout // merge stderr → stdout

	if err := cmd.Start(); err != nil {
		h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO ao iniciar] %v", err), true)
		return false
	}

	san := sanitizer.New()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		clean := san.SanitizeText(line)
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      "output",
			Phase:     "in_progress",
			Message:   clean,
			Cluster:   target.Cluster,
			Details:   target.Namespace,
			Timestamp: time.Now(),
		})
	}

	err = cmd.Wait()

	if ctx.Err() == context.DeadlineExceeded {
		h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[TIMEOUT] Limite de %ds excedido", timeoutSec), true)
		return false
	}
	if err != nil {
		h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[EXIT %v]", err), true)
		return false
	}
	return true
}

// sendLine envia uma linha de texto simples via SSE.
func (h *CommandRunnerHandler) sendLine(sessionID, cluster, namespace, msg string, isError bool) {
	t := "output"
	if isError {
		t = "output_error"
	}
	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      t,
		Phase:     "in_progress",
		Message:   msg,
		Cluster:   cluster,
		Details:   namespace,
		Timestamp: time.Now(),
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildShellScript monta o script bash que será executado.
// Para kubectl: injeta uma função wrapper que força --context=CLUSTER em todos os calls,
// cobrindo casos de pipes com xargs kubectl.
func buildShellScript(command, cmdType, cluster, namespace string) string {
	switch cmdType {
	case "kubectl":
		// Wrapper de função: toda chamada a `kubectl` no script usará --context=CLUSTER
		// Funciona para pipes como: kubectl get ... | xargs kubectl delete ...
		return fmt.Sprintf(
			`kubectl() { command kubectl --context='%s' "$@"; }; export -f kubectl 2>/dev/null || true; %s`,
			cluster, command,
		)
	case "python", "python3":
		return fmt.Sprintf(`export CLUSTER='%s'; export NAMESPACE='%s'; python3 -c %q`,
			cluster, namespace, command)
	case "go":
		// go run a partir de stdin via processo temporário
		return fmt.Sprintf(`export CLUSTER='%s'; export NAMESPACE='%s'; echo %q | go run /dev/stdin 2>&1`,
			cluster, namespace, command)
	default: // sh, bash
		return fmt.Sprintf(`export CLUSTER='%s'; export NAMESPACE='%s'; %s`,
			cluster, namespace, command)
	}
}

// buildAIPrompt cria o prompt para o provider de AI respeitando a linguagem escolhida.
func buildAIPrompt(userRequest, cluster, namespace, cmdType string) string {
	if cmdType == "" {
		cmdType = "kubectl"
	}

	var langInstruction string
	switch cmdType {
	case "python", "python3":
		langInstruction = `Generate a complete Python 3 script that accomplishes this task.
Rules:
- Use the 'kubernetes' (k8s) Python SDK or 'subprocess' to run kubectl commands
- Export cluster context via: import os; CLUSTER = os.environ.get('CLUSTER', '` + cluster + `'); NAMESPACE = os.environ.get('NAMESPACE', '` + namespace + `')
- Do NOT include explanations, markdown, or code fences — return only the Python code
- Print output to stdout`
	case "go":
		langInstruction = `Generate a complete Go program (package main) that accomplishes this task.
Rules:
- Use os/exec to run kubectl commands or the client-go SDK
- Export cluster/namespace via environment variables CLUSTER and NAMESPACE
- Do NOT include explanations, markdown, or code fences — return only the Go code`
	case "sh", "bash":
		langInstruction = `Generate a shell script (` + cmdType + `) that accomplishes this task.
Rules:
- Do NOT include explanations, markdown, code blocks, or backticks
- CLUSTER and NAMESPACE env vars are pre-set; use them if needed
- Use kubectl --context=$CLUSTER and -n $NAMESPACE`
	default: // kubectl
		langInstruction = `Generate a kubectl command (or pipeline) that accomplishes this task.
Rules:
- Do NOT include explanations, markdown, code blocks, or backticks
- Use --context={{cluster}} placeholder — it will be auto-injected as a bash function wrapper
- Use -n {{namespace}} or --namespace={{namespace}} for namespace targeting
- For piped commands (kubectl ... | xargs kubectl ...) the kubectl wrapper handles context automatically
- If multiple steps are needed, join with && or newlines`
	}

	return fmt.Sprintf(`You are a Kubernetes and DevOps expert.

The user wants to perform the following action:
"%s"

Target cluster: %s
Target namespace: %s

Language/type requested: %s

%s`, userRequest, cluster, namespace, cmdType, langInstruction)
}

// cleanAICommandResponse remove markdown artifacts que algumas AIs insistem em incluir.
func cleanAICommandResponse(s string) string {
	s = strings.TrimSpace(s)
	// Remove blocos de código markdown
	for _, fence := range []string{"```bash", "```shell", "```sh", "```kubectl", "```"} {
		s = strings.ReplaceAll(s, fence, "")
	}
	s = strings.TrimSpace(s)
	// Remove backticks isolados
	s = strings.Trim(s, "`")
	return strings.TrimSpace(s)
}

// detectCommandType infere o tipo de comando a partir do texto.
func detectCommandType(command string) string {
	cmd := strings.TrimSpace(command)
	switch {
	case strings.HasPrefix(cmd, "kubectl"):
		return "kubectl"
	case strings.HasPrefix(cmd, "python") || strings.HasPrefix(cmd, "python3"):
		return "python"
	case strings.HasPrefix(cmd, "go "):
		return "go"
	default:
		return "sh"
	}
}

// truncate corta uma string no máximo n caracteres.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

