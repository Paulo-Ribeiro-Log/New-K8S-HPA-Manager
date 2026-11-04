package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Suporte ao painel Git lateral do Code Editor: lista de remotes, fetch e busca no histórico.

const (
	logFieldSep     = "\x1f" // separador de campos do `git log`: o assunto do commit pode ter "|"
	logSearchWindow = 500    // commits examinados quando há busca (revisão/mensagem/autor)
	logMaxLimit     = 200
)

type commitLogEntry struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	When    string `json:"when"`
}

type gitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// parseLogEntries lê a saída de `git log --pretty=format:%H%x1f%s%x1f%an%x1f%ar`.
func parseLogEntries(out string) []commitLogEntry {
	entries := []commitLogEntry{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, logFieldSep, 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, commitLogEntry{Hash: parts[0], Message: parts[1], Author: parts[2], When: parts[3]})
	}
	return entries
}

// filterLogEntries mantém os commits cujo hash começa com `q` ou cuja mensagem/autor contém `q`
// (sem diferenciar maiúsculas) e devolve no máximo `limit`. `q` vazio só aplica o limite.
func filterLogEntries(entries []commitLogEntry, q string, limit int) []commitLogEntry {
	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]commitLogEntry, 0, limit)
	for _, e := range entries {
		if q != "" &&
			!strings.HasPrefix(strings.ToLower(e.Hash), q) &&
			!strings.Contains(strings.ToLower(e.Message), q) &&
			!strings.Contains(strings.ToLower(e.Author), q) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out
}

// parseLogLimit lê ?limit= (padrão 20, entre 1 e logMaxLimit).
func parseLogLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 20
	}
	if n > logMaxLimit {
		return logMaxLimit
	}
	return n
}

// parseGitRemotes lê `git remote -v` ("origin\thttps://... (fetch)") e devolve um item por remote
// (a URL de fetch), sem credenciais embutidas na URL.
func parseGitRemotes(out string) []gitRemote {
	remotes := []gitRemote{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name, rest, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !strings.HasSuffix(rest, "(fetch)") || seen[name] {
			continue
		}
		seen[name] = true
		url := cleanRemoteURL(strings.TrimSpace(strings.TrimSuffix(rest, "(fetch)")))
		remotes = append(remotes, gitRemote{Name: name, URL: url})
	}
	return remotes
}

// GetCommitLog — GET /api/v1/code-editor/repos/:id/log?limit=20&q=texto
// Sem `q`, os últimos `limit` commits. Com `q`, procura nos últimos logSearchWindow commits por
// prefixo de hash, trecho da mensagem ou do autor.
func (h *CodeEditorHandler) GetCommitLog(c *gin.Context) {
	dir := h.repoDir(c.Param("id"))
	limit := parseLogLimit(c.DefaultQuery("limit", "20"))
	q := strings.TrimSpace(c.Query("q"))

	window := limit
	if q != "" {
		window = logSearchWindow
	}
	out, err := runGit(dir, "log", "-"+strconv.Itoa(window), "--pretty=format:%H"+logFieldSep+"%s"+logFieldSep+"%an"+logFieldSep+"%ar")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, filterLogEntries(parseLogEntries(out), q, limit))
}

// Remotes — GET /api/v1/code-editor/repos/:id/remotes
func (h *CodeEditorHandler) Remotes(c *gin.Context) {
	dir := h.repoDir(c.Param("id"))
	out, err := runGit(dir, "remote", "-v")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, parseGitRemotes(out))
}

// Fetch — POST /api/v1/code-editor/repos/:id/fetch (SSE, mesmo contrato do Pull/Push)
// Body: { "token": "", "profile_id": "" }. `git fetch --all --prune`: atualiza as refs de todos os
// remotes sem mexer na branch atual nem no working tree.
func (h *CodeEditorHandler) Fetch(c *gin.Context) {
	dir := h.repoDir(c.Param("id"))
	var req struct {
		Token     string `json:"token"`
		ProfileID string `json:"profile_id"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, _ := c.Writer.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	sendSSE := func(line string) {
		data, _ := json.Marshal(map[string]string{"message": line})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flush()
	}
	done := func(err error) {
		if err != nil {
			fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		} else {
			fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
		}
		flush()
	}

	sendSSE("Executando git fetch...")

	token := req.Token
	if token == "" {
		token = h.resolveProfileToken(c, req.ProfileID)
	}
	if token == "" {
		token = h.getToken(c)
	}

	// Mesma limpeza do Pull: token antigo embutido na URL do origin ignoraria o GIT_ASKPASS.
	if originURL, err := runGit(dir, "remote", "get-url", "origin"); err == nil {
		if cleaned := cleanRemoteURL(originURL); cleaned != originURL {
			runGit(dir, "remote", "set-url", "origin", cleaned) //nolint:errcheck
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd, cleanup := gitCmdWithToken(ctx, dir, token, "fetch", "--all", "--prune", "--progress")
	defer cleanup()

	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		done(err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { recover() }() //nolint:errcheck
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			if token != "" {
				line = strings.ReplaceAll(line, token, "***")
			}
			sendSSE(line)
		}
	}()

	fetchErr := cmd.Wait()
	wg.Wait() // toda a saída precisa sair antes do "done"
	if fetchErr != nil {
		done(fetchErr)
		return
	}
	sendSSE("Fetch concluído com sucesso.")
	done(nil)
}
