package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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
	cancelFuncs    sync.Map              // sessionID -> context.CancelFunc
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
	Prompt     string   `json:"prompt"`
	Cluster    string   `json:"cluster"`
	Namespace  string   `json:"namespace"`
	Clusters   []string `json:"clusters,omitempty"`   // todos os clusters selecionados
	Namespaces []string `json:"namespaces,omitempty"` // todos os namespaces selecionados
	AIEmail    string   `json:"ai_email"`
	CmdType    string   `json:"cmd_type"` // linguagem: kubectl | python | go | sh | bash
	Explain    bool     `json:"explain"`
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
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)
	go func() {
		h.runParallel(ctx, sessionID, req)
		h.cancelFuncs.Delete(sessionID)
	}()

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

// Cancel força a parada de uma execução em andamento.
// DELETE /api/v1/command-runner/session/:sessionId
func (h *CommandRunnerHandler) Cancel(c *gin.Context) {
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

	// Usar lista completa de clusters/namespaces se fornecida, senão fallback para campos simples
	clusters := req.Clusters
	if len(clusters) == 0 && req.Cluster != "" {
		clusters = []string{req.Cluster}
	}
	namespaces := req.Namespaces
	if len(namespaces) == 0 && req.Namespace != "" {
		namespaces = []string{req.Namespace}
	}

	var prompt string
	if req.Explain {
		prompt = buildAIChatPrompt(req.Prompt, clusters, namespaces, req.CmdType)
	} else {
		prompt = buildAIPrompt(req.Prompt, req.Cluster, req.Namespace, req.CmdType)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	rawResult, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gerar comando: " + err.Error()})
		return
	}

	if req.Explain {
		cmd, expl := parseAIExplainResponse(rawResult)
		c.JSON(http.StatusOK, gin.H{
			"command":     cmd,
			"type":        detectCommandType(cmd),
			"explanation": expl,
		})
	} else {
		result := cleanAICommandResponse(rawResult)
		c.JSON(http.StatusOK, gin.H{
			"command": result,
			"type":    detectCommandType(result),
		})
	}
}

// ─── execução paralela ────────────────────────────────────────────────────────

func (h *CommandRunnerHandler) runParallel(sessionCtx context.Context, sessionID string, req ExecuteCommandRequest) {
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
			ok := h.runForTarget(sessionCtx, sessionID, t, req.Command, req.Type, req.TimeoutSec)

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
func (h *CommandRunnerHandler) runForTarget(sessionCtx context.Context, sessionID string, target CommandTarget, command, cmdType string, timeoutSec int) bool {
	// Substituir placeholders
	resolved := strings.ReplaceAll(command, "{{cluster}}", target.Cluster)
	if target.Namespace == "" {
		// Namespace vazio = todos os namespaces: substituir flags de namespace por -A
		resolved = strings.ReplaceAll(resolved, "-n {{namespace}}", "-A")
		resolved = strings.ReplaceAll(resolved, "--namespace={{namespace}}", "--all-namespaces")
		resolved = strings.ReplaceAll(resolved, "--namespace {{namespace}}", "--all-namespaces")
		resolved = strings.ReplaceAll(resolved, "{{namespace}}", "") // fallback
	} else {
		resolved = strings.ReplaceAll(resolved, "{{namespace}}", target.Namespace)
	}

	var script string
	var tmpPyFile string
	var tmpGoDir string
	switch {
	case cmdType == "python" || cmdType == "python3":
		// Escrever em arquivo temporário para evitar problemas de quoting com scripts
		// multi-line e caracteres especiais gerados pela AI
		f, err := os.CreateTemp("", "k8s-hpa-py-*.py")
		if err != nil {
			h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO] Falha ao criar arquivo temporário Python: %v", err), true)
			return false
		}
		tmpPyFile = f.Name()
		if _, err := f.WriteString(resolved); err != nil {
			f.Close()
			os.Remove(tmpPyFile)
			h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO] Falha ao escrever script Python: %v", err), true)
			return false
		}
		f.Close()
		script = buildPythonScript(tmpPyFile, target.Cluster, target.Namespace)

	case cmdType == "go":
		// Go precisa de um diretório temporário com go.mod para suportar imports externos
		dir, err := os.MkdirTemp("", "k8s-hpa-go-*")
		if err != nil {
			h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO] Falha ao criar diretório temporário Go: %v", err), true)
			return false
		}
		tmpGoDir = dir
		if err := os.WriteFile(dir+"/main.go", []byte(resolved), 0644); err != nil {
			os.RemoveAll(tmpGoDir)
			h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[ERRO] Falha ao escrever script Go: %v", err), true)
			return false
		}
		script = buildGoScript(tmpGoDir, target.Cluster, target.Namespace)

	default:
		script = buildShellScript(resolved, cmdType, target.Cluster, target.Namespace)
	}

	ctx, cancel := context.WithTimeout(sessionCtx, time.Duration(timeoutSec)*time.Second)
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
	if tmpPyFile != "" {
		os.Remove(tmpPyFile)
	}
	if tmpGoDir != "" {
		os.RemoveAll(tmpGoDir)
	}

	if ctx.Err() == context.DeadlineExceeded {
		h.sendLine(sessionID, target.Cluster, target.Namespace, fmt.Sprintf("[TIMEOUT] Limite de %ds excedido", timeoutSec), true)
		return false
	}
	if ctx.Err() == context.Canceled {
		h.sendLine(sessionID, target.Cluster, target.Namespace, "[CANCELADO] Execução interrompida pelo usuário", true)
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
		// Não usar aqui — Python é tratado via buildPythonScript com arquivo temporário
		return fmt.Sprintf(`export CLUSTER=%q; export NAMESPACE=%q; python3 -c %q`,
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

// buildPythonScript executa um script Python a partir de um arquivo temporário usando um venv
// isolado em ~/.k8s-hpa-manager/python-venv, criando-o automaticamente na primeira execução.
// Pacotes pré-instalados: kubernetes, requests, pyyaml, tabulate.
func buildPythonScript(scriptPath, cluster, namespace string) string {
	homeDir, _ := os.UserHomeDir()
	venvDir := homeDir + "/.k8s-hpa-manager/python-venv"
	return fmt.Sprintf(`
VENV_DIR=%q
if [ ! -f "$VENV_DIR/bin/python3" ]; then
    echo "[k8s-hpa] Preparando ambiente Python (primeira execução, aguarde ~30s)..."
    python3 -m venv "$VENV_DIR" 2>&1 || { echo "[ERRO] python3-venv não encontrado. Instale: sudo apt install python3-venv"; exit 1; }
    "$VENV_DIR/bin/pip" install --quiet --upgrade pip 2>&1
    "$VENV_DIR/bin/pip" install --quiet kubernetes requests pyyaml tabulate 2>&1
    echo "[k8s-hpa] Ambiente Python pronto."
fi
export CLUSTER=%q
export NAMESPACE=%q
"$VENV_DIR/bin/python3" %q
`, venvDir, cluster, namespace, scriptPath)
}

// buildGoScript executa um script Go a partir de um diretório temporário com go.mod.
// Suporta imports de qualquer pacote público via go mod tidy automático.
func buildGoScript(scriptDir, cluster, namespace string) string {
	return fmt.Sprintf(`
cd %q
if [ ! -f go.mod ]; then
    go mod init k8shpa_script 2>&1
fi
# Resolver dependências se houver imports externos (não-stdlib)
if grep -qE '^\s*"[a-z].*\.[a-z]' main.go 2>/dev/null; then
    echo "[k8s-hpa] Baixando dependências Go..."
    GOFLAGS=-mod=mod go mod tidy 2>&1
fi
export CLUSTER=%q
export NAMESPACE=%q
go run . 2>&1
`, scriptDir, cluster, namespace)
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
- Available packages: kubernetes, requests, pyyaml, tabulate, subprocess (stdlib)
- Read cluster/namespace from env: CLUSTER = os.environ.get('CLUSTER', '` + cluster + `'); NAMESPACE = os.environ.get('NAMESPACE', '` + namespace + `')
- To use the kubernetes SDK: from kubernetes import client, config; config.load_kube_config(context=CLUSTER)
- Do NOT include explanations, markdown, or code fences — return only the Python code
- Print output to stdout`
	case "go":
		langInstruction = `Generate a complete Go program (package main) that accomplishes this task.
Rules:
- Use os/exec to run kubectl commands (preferred for simplicity) or client-go SDK for advanced use
- Read cluster/namespace: cluster := os.Getenv("CLUSTER"); namespace := os.Getenv("NAMESPACE")
- External imports are supported (go mod tidy runs automatically)
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
- Do NOT include --context flag — it is automatically injected via a bash wrapper function
- Use -n {{namespace}} for namespace targeting ({{namespace}} is replaced at runtime)
- For piped commands the kubectl context wrapper handles all calls automatically
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

// codeBlockRe captura conteúdo de blocos de código markdown (```...```)
var codeBlockRe = regexp.MustCompile("(?s)```(?:[a-z0-9]*)?\n?(.*?)```")

// parseAIExplainResponse extrai comando (code block) e explicação do texto AI.
func parseAIExplainResponse(text string) (command, explanation string) {
	match := codeBlockRe.FindStringSubmatch(text)
	if len(match) > 1 {
		command = strings.TrimSpace(match[1])
		explanation = strings.TrimSpace(codeBlockRe.ReplaceAllString(text, ""))
		for strings.Contains(explanation, "\n\n\n") {
			explanation = strings.ReplaceAll(explanation, "\n\n\n", "\n\n")
		}
	} else {
		command = cleanAICommandResponse(text)
		explanation = ""
	}
	return
}

// buildAIChatPrompt cria prompt para o modo chat — solicita código + explicação.
// O AI deve gerar comandos específicos para a requisição do usuário, focados nos clusters/namespaces selecionados.
func buildAIChatPrompt(userRequest string, clusters, namespaces []string, cmdType string) string {
	if cmdType == "" {
		cmdType = "kubectl"
	}

	// Descreve o escopo de execução de forma natural
	allNs := len(namespaces) == 0 || (len(namespaces) == 1 && namespaces[0] == "*")

	var scopeDesc string
	switch {
	case len(clusters) == 0:
		scopeDesc = "no cluster selected"
	case len(clusters) == 1 && allNs:
		scopeDesc = fmt.Sprintf("cluster '%s' (all namespaces)", clusters[0])
	case len(clusters) == 1 && len(namespaces) == 1:
		scopeDesc = fmt.Sprintf("cluster '%s', namespace '%s'", clusters[0], namespaces[0])
	case len(clusters) == 1:
		scopeDesc = fmt.Sprintf("cluster '%s', namespaces: %s", clusters[0], strings.Join(namespaces, ", "))
	case allNs:
		scopeDesc = fmt.Sprintf("%d clusters (%s) — all namespaces", len(clusters), strings.Join(clusters, ", "))
	default:
		scopeDesc = fmt.Sprintf("%d clusters (%s), namespaces: %s",
			len(clusters), strings.Join(clusters, ", "), strings.Join(namespaces, ", "))
	}

	// Instrução de namespace para o comando
	var nsInstruction string
	if allNs {
		nsInstruction = "Use -A / --all-namespaces (all namespaces are targeted)."
	} else if len(namespaces) == 1 {
		nsInstruction = fmt.Sprintf("Use -n {{namespace}} as namespace placeholder (will be replaced with '%s' at runtime).", namespaces[0])
	} else {
		nsInstruction = fmt.Sprintf("Use -n {{namespace}} as namespace placeholder (replaced per target: %s).", strings.Join(namespaces, ", "))
	}

	// Nota sobre multi-cluster
	multiClusterNote := ""
	if len(clusters) > 1 {
		multiClusterNote = fmt.Sprintf("\nIMPORTANT: The command runs independently on each of the %d clusters. The --context flag is auto-injected per cluster — do NOT include it.", len(clusters))
	}

	langHint := "a kubectl command or pipeline"
	switch cmdType {
	case "python", "python3":
		langHint = "a Python 3 script"
	case "go":
		langHint = "a complete Go program (package main)"
	case "sh", "bash":
		langHint = "a " + cmdType + " shell script"
	}

	return fmt.Sprintf(`You are a Kubernetes and DevOps expert assistant integrated into a Command Runner tool.
IMPORTANT: Always respond in Brazilian Portuguese (pt-BR). Code comments and variable names can be in English, but all explanations must be in pt-BR.

USER REQUEST: "%s"

EXECUTION SCOPE: %s
LANGUAGE: %s%s

Generate %s that SPECIFICALLY addresses the user's request for the above scope.
- Be precise and targeted: if the user mentions an app name, resource type, error condition, or label — use it directly in the command.
- Do NOT generate generic templates. The command should be ready to run as-is for the described scope.
- For kubectl: NEVER include --context flag (auto-injected). %s
- For shell/python/go: $CLUSTER and $NAMESPACE env vars are pre-set at runtime.

Structure your response:
1. Complete code in a fenced code block
2. Brief explanation (2–4 sentences): what it does, key flags, and any caveats (destructive operations, required permissions, etc.)`,
		userRequest, scopeDesc, cmdType, multiClusterNote, langHint, nsInstruction)
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

