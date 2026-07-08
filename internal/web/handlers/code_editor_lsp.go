package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// ─── LSP tipos ───────────────────────────────────────────────────────────────

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"` // 1=Error 2=Warning 3=Info 4=Hint
	Message  string   `json:"message"`
	Source   string   `json:"source"`
}

type lspCompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

type lspHoverResult struct {
	Contents string    `json:"contents"`
	Range    *lspRange `json:"range,omitempty"`
}

// ─── Sessão por repositório ───────────────────────────────────────────────────

type lspSession struct {
	lang     string
	repoDir  string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan json.RawMessage
	diags    map[string][]lspDiagnostic // uri → diagnósticos
	diagsMu  sync.RWMutex
	lastUsed time.Time
	initDone bool
}

func (s *lspSession) send(method string, params interface{}) (int64, error) {
	id := atomic.AddInt64(&s.nextID, 1)
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
	s.mu.Lock()
	_, err = fmt.Fprint(s.stdin, msg)
	s.mu.Unlock()
	return id, err
}

func (s *lspSession) notify(method string, params interface{}) error {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(body)
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
	s.mu.Lock()
	_, err := fmt.Fprint(s.stdin, msg)
	s.mu.Unlock()
	return err
}

func (s *lspSession) call(method string, params interface{}) (json.RawMessage, error) {
	id, err := s.send(method, params)
	if err != nil {
		return nil, err
	}
	ch := make(chan json.RawMessage, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(10 * time.Second):
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("timeout")
	}
}

// readLoop lê mensagens do processo e despacha para pending ou armazena diagnósticos
func (s *lspSession) readLoop() {
	for {
		// Lê header Content-Length
		var contentLen int
		for {
			line, err := s.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
				contentLen = n
			}
		}
		if contentLen == 0 {
			continue
		}
		buf := make([]byte, contentLen)
		if _, err := io.ReadFull(s.stdout, buf); err != nil {
			return
		}

		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(buf, &msg); err != nil {
			continue
		}

		// Notificação: diagnósticos
		if msg.Method == "textDocument/publishDiagnostics" {
			var p struct {
				URI         string          `json:"uri"`
				Diagnostics []lspDiagnostic `json:"diagnostics"`
			}
			if json.Unmarshal(msg.Params, &p) == nil {
				s.diagsMu.Lock()
				s.diags[p.URI] = p.Diagnostics
				s.diagsMu.Unlock()
			}
			continue
		}

		// Resposta a uma requisição
		if msg.ID != nil {
			s.mu.Lock()
			ch, ok := s.pending[*msg.ID]
			if ok {
				delete(s.pending, *msg.ID)
			}
			s.mu.Unlock()
			if ok {
				ch <- msg.Result
			}
		}
	}
}

func (s *lspSession) shutdown() {
	_ = s.notify("shutdown", nil)
	_ = s.notify("exit", nil)
	time.Sleep(300 * time.Millisecond)
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// ─── Manager global de sessões ────────────────────────────────────────────────

var (
	lspSessions    sync.Map // key: "repoId/lang" → *lspSession
	lspCleanupOnce sync.Once
)

func startLSPCleanup() {
	lspCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				lspSessions.Range(func(k, v interface{}) bool {
					s := v.(*lspSession)
					s.mu.Lock()
					idle := time.Since(s.lastUsed)
					s.mu.Unlock()
					if idle > 10*time.Minute {
						s.shutdown()
						lspSessions.Delete(k)
					}
					return true
				})
			}
		}()
	})
}

func lspBinaryPath(lang string) string {
	switch lang {
	case "go":
		// Tenta GOPATH/bin/gopls antes do PATH
		if p := filepath.Join(os.Getenv("HOME"), "go", "bin", "gopls"); func() bool { _, e := os.Stat(p); return e == nil }() {
			return p
		}
		if p, err := exec.LookPath("gopls"); err == nil {
			return p
		}
	case "python":
		if p, err := exec.LookPath("pyright"); err == nil {
			return p
		}
		if p, err := exec.LookPath("pylsp"); err == nil {
			return p
		}
	}
	return ""
}

func getOrCreateSession(repoId, lang, repoDir string) (*lspSession, error) {
	startLSPCleanup()
	key := repoId + "/" + lang
	if v, ok := lspSessions.Load(key); ok {
		s := v.(*lspSession)
		s.mu.Lock()
		s.lastUsed = time.Now()
		s.mu.Unlock()
		return s, nil
	}

	bin := lspBinaryPath(lang)
	if bin == "" {
		return nil, fmt.Errorf("language server '%s' não encontrado no PATH", lang)
	}

	ctx := context.Background()
	var cmd *exec.Cmd
	switch lang {
	case "go":
		cmd = exec.CommandContext(ctx, bin, "serve")
	case "python":
		cmd = exec.CommandContext(ctx, bin, "--stdio")
	default:
		cmd = exec.CommandContext(ctx, bin)
	}
	cmd.Dir = repoDir
	env := append(os.Environ(), "GOPATH="+filepath.Join(os.Getenv("HOME"), "go"))
	if lang == "python" {
		// pyright precisa do PATH para encontrar o interpretador python
		env = append(env, "PYTHONPATH="+repoDir)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil // descartar stderr do language server

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("falha ao iniciar %s: %w", bin, err)
	}

	s := &lspSession{
		lang:     lang,
		repoDir:  repoDir,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReaderSize(stdoutPipe, 64*1024),
		pending:  make(map[int64]chan json.RawMessage),
		diags:    make(map[string][]lspDiagnostic),
		lastUsed: time.Now(),
	}
	go s.readLoop()

	// Initialize LSP
	rootURI := "file://" + repoDir
	_, err = s.call("initialize", map[string]interface{}{
		"processId":             os.Getpid(),
		"rootUri":               rootURI,
		"capabilities":          lspClientCapabilities(),
		"initializationOptions": map[string]interface{}{},
	})
	if err != nil {
		s.shutdown()
		return nil, fmt.Errorf("initialize falhou: %w", err)
	}
	_ = s.notify("initialized", map[string]interface{}{})
	s.initDone = true

	lspSessions.Store(key, s)
	return s, nil
}

func lspClientCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"textDocument": map[string]interface{}{
			"completion": map[string]interface{}{
				"completionItem": map[string]interface{}{
					"documentationFormat": []string{"plaintext"},
					"snippetSupport":      false,
				},
			},
			"hover": map[string]interface{}{
				"contentFormat": []string{"plaintext", "markdown"},
			},
			"publishDiagnostics": map[string]interface{}{
				"relatedInformation": false,
			},
			"definition": map[string]interface{}{
				"linkSupport": false,
			},
		},
		"workspace": map[string]interface{}{
			"applyEdit": false,
		},
	}
}

// ─── Handler HTTP ─────────────────────────────────────────────────────────────

type CodeEditorLSPHandler struct {
	reposBase string
}

func NewCodeEditorLSPHandler(reposBase string) *CodeEditorLSPHandler {
	return &CodeEditorLSPHandler{reposBase: reposBase}
}

func (h *CodeEditorLSPHandler) repoDir(id string) string {
	return filepath.Join(h.reposBase, id)
}

func (h *CodeEditorLSPHandler) fileURI(repoDir, relPath string) string {
	return "file://" + filepath.Join(repoDir, relPath)
}

// POST /repos/:id/lsp/open  {"lang":"go","path":"main.go","content":"..."}
func (h *CodeEditorLSPHandler) Open(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)

	var req struct {
		Lang    string `json:"lang"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Lang == "" || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lang e path obrigatórios"})
		return
	}
	if !isSupportedLang(req.Lang) {
		c.JSON(http.StatusOK, gin.H{"started": false, "reason": "linguagem não suportada"})
		return
	}

	s, err := getOrCreateSession(id, req.Lang, repoDir)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	uri := h.fileURI(repoDir, req.Path)
	_ = s.notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": req.Lang,
			"version":    1,
			"text":       req.Content,
		},
	})
	c.JSON(http.StatusOK, gin.H{"started": true})
}

// POST /repos/:id/lsp/change  {"lang":"go","path":"main.go","content":"...","version":2}
func (h *CodeEditorLSPHandler) Change(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)

	var req struct {
		Lang    string `json:"lang"`
		Path    string `json:"path"`
		Content string `json:"content"`
		Version int    `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inválido"})
		return
	}

	key := id + "/" + req.Lang
	v, ok := lspSessions.Load(key)
	if !ok {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	s := v.(*lspSession)
	uri := h.fileURI(repoDir, req.Path)
	_ = s.notify("textDocument/didChange", map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": uri, "version": req.Version},
		"contentChanges": []map[string]interface{}{{"text": req.Content}},
	})
	c.JSON(http.StatusOK, gin.H{})
}

// POST /repos/:id/lsp/complete  {"lang":"go","path":"...","content":"...","line":0,"character":0}
func (h *CodeEditorLSPHandler) Complete(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)

	var req struct {
		Lang      string `json:"lang"`
		Path      string `json:"path"`
		Content   string `json:"content"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
		Version   int    `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inválido"})
		return
	}

	s, err := getOrCreateSession(id, req.Lang, repoDir)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	uri := h.fileURI(repoDir, req.Path)
	// Sincroniza conteúdo antes de completar
	_ = s.notify("textDocument/didChange", map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": uri, "version": req.Version},
		"contentChanges": []map[string]interface{}{{"text": req.Content}},
	})

	res, err := s.call("textDocument/completion", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": req.Line, "character": req.Character},
		"context":      map[string]interface{}{"triggerKind": 1},
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"items": []interface{}{}})
		return
	}

	// gopls retorna {items:[...]} ou lista direta
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	var items []json.RawMessage
	if json.Unmarshal(res, &list) == nil && list.Items != nil {
		items = list.Items
	} else {
		_ = json.Unmarshal(res, &items)
	}

	out := make([]lspCompletionItem, 0, len(items))
	for _, raw := range items {
		var it struct {
			Label         string          `json:"label"`
			Kind          int             `json:"kind"`
			Detail        string          `json:"detail"`
			Documentation json.RawMessage `json:"documentation"`
			InsertText    string          `json:"insertText"`
		}
		if json.Unmarshal(raw, &it) != nil {
			continue
		}
		doc := ""
		var docStr string
		if json.Unmarshal(it.Documentation, &docStr) == nil {
			doc = docStr
		} else {
			var docObj struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(it.Documentation, &docObj) == nil {
				doc = docObj.Value
			}
		}
		ins := it.InsertText
		if ins == "" {
			ins = it.Label
		}
		out = append(out, lspCompletionItem{
			Label: it.Label, Kind: it.Kind, Detail: it.Detail,
			Documentation: doc, InsertText: ins,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// POST /repos/:id/lsp/hover  {"lang":"go","path":"...","content":"...","line":0,"character":0}
func (h *CodeEditorLSPHandler) Hover(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)

	var req struct {
		Lang      string `json:"lang"`
		Path      string `json:"path"`
		Content   string `json:"content"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
		Version   int    `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inválido"})
		return
	}

	key := id + "/" + req.Lang
	v, ok := lspSessions.Load(key)
	if !ok {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	s := v.(*lspSession)
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	uri := h.fileURI(repoDir, req.Path)
	_ = s.notify("textDocument/didChange", map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": uri, "version": req.Version},
		"contentChanges": []map[string]interface{}{{"text": req.Content}},
	})

	res, err := s.call("textDocument/hover", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": req.Line, "character": req.Character},
	})
	if err != nil || string(res) == "null" {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	var hover struct {
		Contents json.RawMessage `json:"contents"`
		Range    *lspRange       `json:"range"`
	}
	if json.Unmarshal(res, &hover) != nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	text := extractHoverText(hover.Contents)
	c.JSON(http.StatusOK, lspHoverResult{Contents: text, Range: hover.Range})
}

// POST /repos/:id/lsp/definition
func (h *CodeEditorLSPHandler) Definition(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)

	var req struct {
		Lang      string `json:"lang"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inválido"})
		return
	}

	key := id + "/" + req.Lang
	v, ok := lspSessions.Load(key)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"locations": []interface{}{}})
		return
	}
	s := v.(*lspSession)
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	uri := h.fileURI(repoDir, req.Path)
	res, err := s.call("textDocument/definition", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": req.Line, "character": req.Character},
	})
	if err != nil || string(res) == "null" {
		c.JSON(http.StatusOK, gin.H{"locations": []interface{}{}})
		return
	}

	// Pode ser Location ou []Location
	var locs []lspLocation
	if json.Unmarshal(res, &locs) != nil {
		var loc lspLocation
		if json.Unmarshal(res, &loc) == nil {
			locs = []lspLocation{loc}
		}
	}

	// Converte URI file:// → caminho relativo ao repo
	prefix := "file://" + repoDir + "/"
	type outLoc struct {
		Path  string   `json:"path"`
		Range lspRange `json:"range"`
	}
	out := make([]outLoc, 0, len(locs))
	for _, l := range locs {
		rel := strings.TrimPrefix(l.URI, prefix)
		if rel != "" {
			out = append(out, outLoc{Path: rel, Range: l.Range})
		}
	}
	c.JSON(http.StatusOK, gin.H{"locations": out})
}

// GET /repos/:id/lsp/diagnostics?lang=go&path=main.go
func (h *CodeEditorLSPHandler) Diagnostics(c *gin.Context) {
	id := c.Param("id")
	repoDir := h.repoDir(id)
	lang := c.Query("lang")
	path := c.Query("path")

	key := id + "/" + lang
	v, ok := lspSessions.Load(key)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"diagnostics": []interface{}{}})
		return
	}
	s := v.(*lspSession)
	uri := h.fileURI(repoDir, path)
	s.diagsMu.RLock()
	diags := s.diags[uri]
	s.diagsMu.RUnlock()
	c.JSON(http.StatusOK, gin.H{"diagnostics": diags})
}

// DELETE /repos/:id/lsp?lang=go
func (h *CodeEditorLSPHandler) Shutdown(c *gin.Context) {
	id := c.Param("id")
	lang := c.Query("lang")
	key := id + "/" + lang
	if v, ok := lspSessions.Load(key); ok {
		v.(*lspSession).shutdown()
		lspSessions.Delete(key)
	}
	c.JSON(http.StatusOK, gin.H{})
}

// GET /repos/:id/lsp/status?lang=go
func (h *CodeEditorLSPHandler) Status(c *gin.Context) {
	id := c.Param("id")
	lang := c.Query("lang")
	available := lspBinaryPath(lang) != ""
	running := false
	if _, ok := lspSessions.Load(id + "/" + lang); ok {
		running = true
	}
	c.JSON(http.StatusOK, gin.H{
		"available": available,
		"running":   running,
		"lang":      lang,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func isSupportedLang(lang string) bool {
	return lang == "go" || lang == "python"
}

func extractHoverText(raw json.RawMessage) string {
	// string simples
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// {kind, value}
	var obj struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Value
	}
	// array de strings ou objetos
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, item := range arr {
			parts = append(parts, extractHoverText(item))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
