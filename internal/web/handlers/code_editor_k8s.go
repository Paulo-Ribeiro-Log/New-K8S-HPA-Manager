package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// CodeEditorK8sHandler expõe ações kubectl integradas ao editor de código.
type CodeEditorK8sHandler struct {
	reposBase string
	logger    *zerolog.Logger
}

// NewCodeEditorK8sHandler cria o handler de integração K8s para o editor.
func NewCodeEditorK8sHandler(reposBase string, logger *zerolog.Logger) *CodeEditorK8sHandler {
	return &CodeEditorK8sHandler{reposBase: reposBase, logger: logger}
}

// ListContexts retorna a lista de contexts do kubeconfig local.
// GET /api/v1/code-editor/k8s/contexts
func (h *CodeEditorK8sHandler) ListContexts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "kubectl", "config", "get-contexts", "-o=name").Output()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kubectl não disponível: " + err.Error()})
		return
	}

	var contexts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			contexts = append(contexts, line)
		}
	}
	c.JSON(http.StatusOK, gin.H{"contexts": contexts})
}

// k8sRequest é o body compartilhado para as operações kubectl que recebem YAML via stdin.
type k8sRequest struct {
	Cluster string `json:"cluster" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// Diff executa `kubectl diff -f -` via SSE.
// POST /api/v1/code-editor/repos/:id/k8s/diff
func (h *CodeEditorK8sHandler) Diff(c *gin.Context) {
	var req k8sRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.streamKubectl(c, req.Cluster, req.Content, 30*time.Second, "diff", "-f", "-")
}

// DryRun executa `kubectl apply --dry-run=server -f -` via SSE.
// POST /api/v1/code-editor/repos/:id/k8s/dry-run
func (h *CodeEditorK8sHandler) DryRun(c *gin.Context) {
	var req k8sRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.streamKubectl(c, req.Cluster, req.Content, 30*time.Second, "apply", "--dry-run=server", "-f", "-")
}

// Apply executa `kubectl apply -f -` via SSE.
// POST /api/v1/code-editor/repos/:id/k8s/apply
func (h *CodeEditorK8sHandler) Apply(c *gin.Context) {
	var req k8sRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.streamKubectl(c, req.Cluster, req.Content, 120*time.Second, "apply", "-f", "-")
}

// GetResource executa `kubectl get <kind> <name> -n <ns> -o yaml` de forma síncrona.
// GET /api/v1/code-editor/repos/:id/k8s/resource?cluster=&kind=&name=&namespace=
func (h *CodeEditorK8sHandler) GetResource(c *gin.Context) {
	cluster := c.Query("cluster")
	kind := c.Query("kind")
	name := c.Query("name")
	namespace := c.Query("namespace")

	if cluster == "" || kind == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, kind e name são obrigatórios"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"--context", cluster, "get", kind, name, "-o", "yaml"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(out)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": string(out)})
}

// streamKubectl executa um comando kubectl com stdin e transmite a saída via SSE.
// Linhas de stderr e stdout são mescladas em ordem para refletir o output real do kubectl.
func (h *CodeEditorK8sHandler) streamKubectl(c *gin.Context, cluster, stdin string, timeout time.Duration, args ...string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fullArgs := append([]string{"--context", cluster}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
	cmd.Stdin = strings.NewReader(stdin)

	// Combina stdout e stderr para capturar o output completo
	cmd.Env = os.Environ()

	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(c.Writer, "data: {\"line\":\"erro interno: %s\",\"done\":true,\"error\":true}\n\n", err.Error())
		c.Writer.Flush()
		return
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		fmt.Fprintf(c.Writer, "data: {\"line\":\"kubectl não encontrado: %s\",\"done\":true,\"error\":true}\n\n", err.Error())
		c.Writer.Flush()
		return
	}

	// Lê linhas em goroutine
	doneCh := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			escaped := strings.ReplaceAll(line, `"`, `\"`)
			fmt.Fprintf(c.Writer, "data: {\"line\":\"%s\"}\n\n", escaped)
			c.Writer.Flush()
		}
		close(doneCh)
	}()

	cmdErr := cmd.Wait()
	pw.Close()
	<-doneCh
	pr.Close()

	if cmdErr != nil {
		// kubectl diff retorna exit code 1 quando há diferenças — não é erro
		if ctx.Err() != nil {
			fmt.Fprintf(c.Writer, "data: {\"line\":\"timeout: operação cancelada após %s\",\"done\":true,\"error\":true}\n\n", timeout)
		} else {
			fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
		}
	} else {
		fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	}
	c.Writer.Flush()
}
