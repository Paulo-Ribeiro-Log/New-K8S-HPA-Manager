package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/storage"
)

// CodeEditorHandler gerencia operações de edição de código com Git.
type CodeEditorHandler struct {
	tokenStore     *storage.GitHubTokenStore
	logger         *zerolog.Logger
	reposBase      string // ~/.k8s-hpa-manager/repos
	maxReposPerUser int
}

// NewCodeEditorHandler cria o handler do editor de código.
func NewCodeEditorHandler(tokenStore *storage.GitHubTokenStore, logger *zerolog.Logger) *CodeEditorHandler {
	home, _ := os.UserHomeDir()
	return &CodeEditorHandler{
		tokenStore:     tokenStore,
		logger:         logger,
		reposBase:      filepath.Join(home, ".k8s-hpa-manager", "repos"),
		maxReposPerUser: 10,
	}
}

// RepoInfo descreve um repositório clonado.
type RepoInfo struct {
	ID            string    `json:"id"`             // owner-repo
	Owner         string    `json:"owner"`
	Repo          string    `json:"repo"`
	LocalPath     string    `json:"local_path"`
	CurrentBranch string    `json:"current_branch"`
	RemoteURL     string    `json:"remote_url"`
	ClonedAt      time.Time `json:"cloned_at"`
}

// FileNode representa nó na árvore de arquivos.
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"` // relativo à raiz do repo
	Type     string      `json:"type"` // "file" | "dir"
	Children []*FileNode `json:"children,omitempty"`
}

// GitStatusFile representa um arquivo no git status.
type GitStatusFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "M"odified, "A"dded, "D"eleted, "?"untracked, "R"enamed
}

// repoDir retorna o diretório de um repo por ID (owner-repo).
func (h *CodeEditorHandler) repoDir(id string) string {
	return filepath.Join(h.reposBase, id)
}

// repoID converte owner/repo para o ID local.
func repoID(owner, repo string) string {
	return owner + "-" + repo
}

// runGit executa um comando git no diretório do repo.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// getToken obtém o PAT do usuário; o email vem do contexto Gin (InjectUserEmail).
func (h *CodeEditorHandler) getToken(c *gin.Context) string {
	if h.tokenStore == nil {
		return os.Getenv("GITHUB_TOKEN")
	}
	email, _ := c.Get("user_email")
	if email == nil || email == "" {
		return os.Getenv("GITHUB_TOKEN")
	}
	tok, err := h.tokenStore.GetToken(fmt.Sprintf("%v", email))
	if err != nil {
		return os.Getenv("GITHUB_TOKEN")
	}
	return tok
}

// currentBranch retorna o branch atual do repo.
func currentBranch(dir string) string {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// remoteURL retorna a URL remota (origin) sem o token.
func remoteURL(dir string) string {
	out, _ := runGit(dir, "remote", "get-url", "origin")
	// Remove token embedded na URL (https://TOKEN@github.com/...)
	if idx := strings.Index(out, "@github.com"); idx > 0 {
		return "https://github.com" + out[idx+len("@github.com"):]
	}
	return out
}

// ListRepos — GET /api/v1/code-editor/repos
func (h *CodeEditorHandler) ListRepos(c *gin.Context) {
	if err := os.MkdirAll(h.reposBase, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	entries, err := os.ReadDir(h.reposBase)
	if err != nil {
		c.JSON(http.StatusOK, []RepoInfo{})
		return
	}

	repos := make([]RepoInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		dir := filepath.Join(h.reposBase, id)
		// Verifica se é um repo git
		if _, err2 := os.Stat(filepath.Join(dir, ".git")); err2 != nil {
			continue
		}
		info, _ := e.Info()
		parts := strings.SplitN(id, "-", 2)
		owner, repo := id, ""
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}
		repos = append(repos, RepoInfo{
			ID:            id,
			Owner:         owner,
			Repo:          repo,
			LocalPath:     dir,
			CurrentBranch: currentBranch(dir),
			RemoteURL:     remoteURL(dir),
			ClonedAt:      info.ModTime(),
		})
	}
	c.JSON(http.StatusOK, repos)
}

// CloneRepo — POST /api/v1/code-editor/clone (SSE)
// Body: { "owner": "...", "repo": "...", "branch": "..." (optional) }
func (h *CodeEditorHandler) CloneRepo(c *gin.Context) {
	var req struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Owner == "" || req.Repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner e repo são obrigatórios"})
		return
	}

	token := h.getToken(c)
	id := repoID(req.Owner, req.Repo)
	dir := h.repoDir(id)

	// Já clonado?
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "repositório já clonado", "id": id})
		return
	}

	if err := os.MkdirAll(h.reposBase, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Limite de repositórios simultâneos
	if entries, _ := os.ReadDir(h.reposBase); len(entries) >= h.maxReposPerUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("limite de %d repositórios atingido — remova um antes de clonar", h.maxReposPerUser)})
		return
	}

	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", req.Owner, req.Repo)
	if token != "" {
		cloneURL = fmt.Sprintf("https://%s@github.com/%s/%s.git", token, req.Owner, req.Repo)
	}

	// SSE streaming
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, _ := c.Writer.(http.Flusher)

	sendEvent := func(line string) {
		data, _ := json.Marshal(map[string]string{"message": line})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendEvent(fmt.Sprintf("Clonando %s/%s...", req.Owner, req.Repo))

	args := []string{"clone", "--progress", cloneURL, dir}
	if req.Branch != "" {
		args = []string{"clone", "--progress", "-b", req.Branch, cloneURL, dir}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		sendEvent("Erro: " + err.Error())
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		// Filtra linhas com o token
		if token != "" {
			line = strings.ReplaceAll(line, token, "***")
		}
		sendEvent(line)
	}

	if err := cmd.Wait(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
	} else {
		// Configura identidade git local
		runGit(dir, "config", "user.email", "k8s-hpa-manager@local") //nolint:errcheck
		runGit(dir, "config", "user.name", "K8s HPA Manager")        //nolint:errcheck
		sendEvent("Clone concluído com sucesso.")
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"id\":%q}\n\n", id)
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// DeleteRepo — DELETE /api/v1/code-editor/repos/:id
func (h *CodeEditorHandler) DeleteRepo(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	if err := os.RemoveAll(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": id})
}

// GetFileTree — GET /api/v1/code-editor/repos/:id/tree
func (h *CodeEditorHandler) GetFileTree(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	root := &FileNode{Name: id, Path: "", Type: "dir"}
	err := buildTree(dir, dir, root, 0, 6)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, root.Children)
}

// Diretórios ignorados na árvore.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".idea": true, ".vscode": true, "vendor": true,
	"dist": true, "build": true, ".next": true, "target": true,
}

func buildTree(base, current string, node *FileNode, depth, maxDepth int) error {
	if depth >= maxDepth {
		return nil
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return err
	}
	// Dirs primeiro, depois arquivos
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" && name != ".gitignore" && name != ".env.example" {
			continue
		}
		if e.IsDir() && ignoredDirs[name] {
			continue
		}
		rel, _ := filepath.Rel(base, filepath.Join(current, name))
		child := &FileNode{Name: name, Path: rel}
		if e.IsDir() {
			child.Type = "dir"
			if err2 := buildTree(base, filepath.Join(current, name), child, depth+1, maxDepth); err2 != nil {
				return err2
			}
		} else {
			child.Type = "file"
		}
		node.Children = append(node.Children, child)
	}
	return nil
}

// ReadFile — GET /api/v1/code-editor/repos/:id/file?path=...
func (h *CodeEditorHandler) ReadFile(c *gin.Context) {
	id := c.Param("id")
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	fullPath := filepath.Join(h.repoDir(id), filepath.Clean(rel))
	// Sanitização de path traversal
	if !strings.HasPrefix(fullPath, h.repoDir(id)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo não encontrado"})
		return
	}
	if info.Size() > 5*1024*1024 { // 5MB limit
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "arquivo muito grande (máx 5MB)"})
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": string(data), "path": rel})
}

// WriteFile — POST /api/v1/code-editor/repos/:id/file
// Body: { "path": "...", "content": "..." }
func (h *CodeEditorHandler) WriteFile(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	fullPath := filepath.Join(h.repoDir(id), filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, h.repoDir(id)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"saved": req.Path})
}

// GetGitStatus — GET /api/v1/code-editor/repos/:id/status
func (h *CodeEditorHandler) GetGitStatus(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	out, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": out})
		return
	}

	files := []GitStatusFile{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		xy := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if xy == "" || path == "" {
			continue
		}
		// Renomeado: "old -> new"
		if strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			path = parts[1]
		}
		files = append(files, GitStatusFile{Path: path, Status: xy})
	}

	branch := currentBranch(dir)
	// Contar commits à frente/atrás do upstream
	ahead, behind := "", ""
	if aheadOut, err2 := runGit(dir, "rev-list", "--count", "@{u}..HEAD"); err2 == nil {
		ahead = aheadOut
	}
	if behindOut, err2 := runGit(dir, "rev-list", "--count", "HEAD..@{u}"); err2 == nil {
		behind = behindOut
	}

	c.JSON(http.StatusOK, gin.H{
		"files":  files,
		"branch": branch,
		"ahead":  ahead,
		"behind": behind,
	})
}

// ListBranches — GET /api/v1/code-editor/repos/:id/branches
func (h *CodeEditorHandler) ListBranches(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	// Fetch para atualizar refs remotas
	runGit(dir, "fetch", "--prune", "--quiet") //nolint:errcheck

	local, _ := runGit(dir, "branch", "--format=%(refname:short)")
	remote, _ := runGit(dir, "branch", "-r", "--format=%(refname:short)")
	current := currentBranch(dir)

	localList := splitLines(local)
	remoteList := []string{}
	for _, r := range splitLines(remote) {
		// Remove "origin/HEAD -> origin/main"
		if strings.Contains(r, "->") {
			continue
		}
		remoteList = append(remoteList, r)
	}

	c.JSON(http.StatusOK, gin.H{
		"current": current,
		"local":   localList,
		"remote":  remoteList,
	})
}

// CreateBranch — POST /api/v1/code-editor/repos/:id/branch
// Body: { "name": "...", "from": "..." (optional) }
func (h *CodeEditorHandler) CreateBranch(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Name string `json:"name"`
		From string `json:"from"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}

	args := []string{"checkout", "-b", req.Name}
	if req.From != "" {
		args = append(args, req.From)
	}
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"branch": req.Name, "message": out})
}

// CheckoutBranch — POST /api/v1/code-editor/repos/:id/checkout
// Body: { "branch": "..." }
func (h *CodeEditorHandler) CheckoutBranch(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Branch string `json:"branch"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Branch == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "branch é obrigatório"})
		return
	}

	branch := req.Branch
	// Remove prefixo "origin/" se for branch remoto
	if strings.HasPrefix(branch, "origin/") {
		branch = strings.TrimPrefix(branch, "origin/")
		out, err := runGit(dir, "checkout", "-b", branch, req.Branch)
		if err != nil {
			// Pode já existir localmente
			out, err = runGit(dir, "checkout", branch)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": out})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"branch": branch, "message": out})
		return
	}

	out, err := runGit(dir, "checkout", branch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"branch": branch, "message": out})
}

// Pull — POST /api/v1/code-editor/repos/:id/pull (SSE)
func (h *CodeEditorHandler) Pull(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	token := h.getToken(c)
	if token != "" {
		// Atualiza URL remota com o token
		owner, repo := ownerRepo(dir)
		newURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", token, owner, repo)
		runGit(dir, "remote", "set-url", "origin", newURL) //nolint:errcheck
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, _ := c.Writer.(http.Flusher)

	sendSSE := func(line string) {
		data, _ := json.Marshal(map[string]string{"message": line})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendSSE("Executando git pull...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "pull", "--progress")
	cmd.Dir = dir
	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	go func() {
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			if token != "" {
				line = strings.ReplaceAll(line, token, "***")
			}
			sendSSE(line)
		}
	}()

	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
	} else {
		sendSSE("Pull concluído com sucesso.")
		fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// Commit — POST /api/v1/code-editor/repos/:id/commit
// Body: { "message": "...", "files": ["path1", ...] (optional), "amend": false }
func (h *CodeEditorHandler) Commit(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
		Amend   bool     `json:"amend"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido"})
		return
	}
	if !req.Amend && req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message é obrigatório"})
		return
	}

	// Stage arquivos (somente se não for amend sem mudanças ou se há arquivos indicados)
	if !req.Amend || len(req.Files) > 0 {
		if len(req.Files) == 0 {
			if out, err := runGit(dir, "add", "."); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": out})
				return
			}
		} else {
			for _, f := range req.Files {
				if out, err := runGit(dir, "add", f); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": out, "file": f})
					return
				}
			}
		}
	}

	var args []string
	if req.Amend {
		if req.Message != "" {
			args = []string{"commit", "--amend", "-m", req.Message}
		} else {
			args = []string{"commit", "--amend", "--no-edit"}
		}
	} else {
		args = []string{"commit", "-m", req.Message}
	}

	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// Push — POST /api/v1/code-editor/repos/:id/push (SSE)
// Body: { "branch": "..." (optional) }
func (h *CodeEditorHandler) Push(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Branch string `json:"branch"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck

	token := h.getToken(c)
	if token != "" {
		owner, repo := ownerRepo(dir)
		newURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", token, owner, repo)
		runGit(dir, "remote", "set-url", "origin", newURL) //nolint:errcheck
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, _ := c.Writer.(http.Flusher)

	sendSSE := func(line string) {
		data, _ := json.Marshal(map[string]string{"message": line})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendSSE("Executando git push...")

	branch := req.Branch
	if branch == "" {
		branch = currentBranch(dir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "push", "--progress", "origin", branch)
	cmd.Dir = dir
	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	go func() {
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			if token != "" {
				line = strings.ReplaceAll(line, token, "***")
			}
			sendSSE(line)
		}
	}()

	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
	} else {
		// Remove token da URL remota após push bem-sucedido (segurança)
		if token != "" {
			owner, repo := ownerRepo(dir)
			cleanURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
			runGit(dir, "remote", "set-url", "origin", cleanURL) //nolint:errcheck
		}
		sendSSE("Push concluído com sucesso.")
		fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// GetCommitLog — GET /api/v1/code-editor/repos/:id/log?limit=20
func (h *CodeEditorHandler) GetCommitLog(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	limit := c.DefaultQuery("limit", "20")
	out, err := runGit(dir, "log", "--oneline", "-"+limit, "--pretty=format:%H|%s|%an|%ar")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": out})
		return
	}
	type LogEntry struct {
		Hash    string `json:"hash"`
		Message string `json:"message"`
		Author  string `json:"author"`
		When    string `json:"when"`
	}
	entries := []LogEntry{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, LogEntry{
			Hash:    parts[0],
			Message: parts[1],
			Author:  parts[2],
			When:    parts[3],
		})
	}
	c.JSON(http.StatusOK, entries)
}

// GetFileDiff — GET /api/v1/code-editor/repos/:id/diff?path=...
func (h *CodeEditorHandler) GetFileDiff(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	path := c.Query("path")
	args := []string{"diff", "HEAD", "--"}
	if path != "" {
		args = append(args, path)
	}
	out, _ := runGit(dir, args...)
	c.JSON(http.StatusOK, gin.H{"diff": out})
}

// WalkDir — GET /api/v1/code-editor/repos/:id/search?q=...
func (h *CodeEditorHandler) SearchFiles(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	q := strings.ToLower(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q é obrigatório"})
		return
	}

	var matches []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.Contains(name, q) {
			rel, _ := filepath.Rel(dir, path)
			matches = append(matches, rel)
		}
		return nil
	})

	c.JSON(http.StatusOK, gin.H{"matches": matches})
}

// ownerRepo extrai owner/repo da URL remota do git.
func ownerRepo(dir string) (string, string) {
	url := remoteURL(dir)
	// https://github.com/owner/repo.git ou git@github.com:owner/repo.git
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	return "", ""
}

func splitLines(s string) []string {
	result := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// ─── Fase 2: novos handlers ────────────────────────────────────────────────

// GrepFiles — GET /api/v1/code-editor/repos/:id/grep?q=&ext=
// Busca conteúdo de arquivos com git grep.
func (h *CodeEditorHandler) GrepFiles(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	q := c.Query("q")
	ext := c.Query("ext") // ex: "go", "ts"
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q é obrigatório"})
		return
	}

	args := []string{"grep", "-n", "--ignore-case", "-m", "5"}
	if ext != "" {
		args = append(args, q, "--", "*."+strings.TrimPrefix(ext, "."))
	} else {
		args = append(args, q)
	}

	// Exit code 1 = sem matches; ignoramos o erro
	out, _ := runGit(dir, args...)

	type GrepMatch struct {
		File    string `json:"file"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	matches := []GrepMatch{}
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 {
			continue
		}
		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)
		matches = append(matches, GrepMatch{
			File:    parts[0],
			Line:    lineNum,
			Content: strings.TrimSpace(parts[2]),
		})
		if len(matches) >= 200 {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"matches": matches, "query": q})
}

// GetOriginalContent — GET /api/v1/code-editor/repos/:id/original?path=
// Retorna o conteúdo do arquivo no HEAD (para diff view).
func (h *CodeEditorHandler) GetOriginalContent(c *gin.Context) {
	id := c.Param("id")
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	dir := h.repoDir(id)
	fullPath := filepath.Join(dir, filepath.Clean(rel))
	if !strings.HasPrefix(fullPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	// git show HEAD:<path> — exit 128 se arquivo novo (untracked)
	out, err := runGit(dir, "show", "HEAD:"+rel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"content": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": out})
}

// DeleteFile — DELETE /api/v1/code-editor/repos/:id/file?path=
func (h *CodeEditorHandler) DeleteFile(c *gin.Context) {
	id := c.Param("id")
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	dir := h.repoDir(id)
	fullPath := filepath.Join(dir, filepath.Clean(rel))
	if !strings.HasPrefix(fullPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	if err := os.Remove(fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": rel})
}

// CreateFile — POST /api/v1/code-editor/repos/:id/file/create
// Body: { "path": "...", "content": "" }
func (h *CodeEditorHandler) CreateFile(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	dir := h.repoDir(id)
	fullPath := filepath.Join(dir, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	if _, err := os.Stat(fullPath); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "arquivo já existe"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": req.Path})
}

// CreateDir — POST /api/v1/code-editor/repos/:id/mkdir
// Body: { "path": "..." }
func (h *CodeEditorHandler) CreateDir(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	dir := h.repoDir(id)
	fullPath := filepath.Join(dir, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": req.Path})
}

// RenameFile — POST /api/v1/code-editor/repos/:id/rename
// Body: { "from": "...", "to": "..." }
func (h *CodeEditorHandler) RenameFile(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.From == "" || req.To == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from e to são obrigatórios"})
		return
	}
	dir := h.repoDir(id)
	fromPath := filepath.Join(dir, filepath.Clean(req.From))
	toPath := filepath.Join(dir, filepath.Clean(req.To))
	if !strings.HasPrefix(fromPath, dir) || !strings.HasPrefix(toPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(toPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.Rename(fromPath, toPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": req.From, "to": req.To})
}

// ResetFile — POST /api/v1/code-editor/repos/:id/reset-file
// Body: { "path": "..." }
// Descarta mudanças em um arquivo (git checkout HEAD -- <path>).
func (h *CodeEditorHandler) ResetFile(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path é obrigatório"})
		return
	}
	out, err := runGit(dir, "checkout", "HEAD", "--", req.Path)
	if err != nil {
		// Pode ser arquivo novo (untracked) — tenta git clean
		out2, err2 := runGit(dir, "clean", "-f", "--", req.Path)
		if err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": out + "\n" + out2})
			return
		}
		out = out2
	}
	c.JSON(http.StatusOK, gin.H{"message": out, "path": req.Path})
}

// Stash — POST /api/v1/code-editor/repos/:id/stash
// Body: { "message": "..." (opcional) }
func (h *CodeEditorHandler) Stash(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Message string `json:"message"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck

	args := []string{"stash", "push", "--include-untracked"}
	if req.Message != "" {
		args = append(args, "-m", req.Message)
	}
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// StashPop — POST /api/v1/code-editor/repos/:id/stash/pop
func (h *CodeEditorHandler) StashPop(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	out, err := runGit(dir, "stash", "pop")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// MergeBranch — POST /api/v1/code-editor/repos/:id/merge
// Body: { "branch": "...", "no_ff": false }
func (h *CodeEditorHandler) MergeBranch(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Branch string `json:"branch"`
		NoFF   bool   `json:"no_ff"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Branch == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "branch é obrigatório"})
		return
	}
	args := []string{"merge"}
	if req.NoFF {
		args = append(args, "--no-ff")
	}
	args = append(args, req.Branch)
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// FormatFile — POST /api/v1/code-editor/repos/:id/format
// Body: { "path": "...", "content": "..." }
// Executa o formatter adequado para a extensão do arquivo via stdin.
func (h *CodeEditorHandler) FormatFile(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(req.Path))

	var formatter string
	var args []string
	switch ext {
	case ".tf", ".tfvars":
		formatter = "terraform"
		args = []string{"fmt", "-"}
	case ".go":
		formatter = "gofmt"
	case ".json":
		formatter = "jq"
		args = []string{"."}
	case ".yaml", ".yml":
		formatter = "yq"
		args = []string{"eval", "."}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "sem formatter disponível para ." + strings.TrimPrefix(ext, ".")})
		return
	}

	// Verifica se o formatter está disponível no PATH
	if _, err := exec.LookPath(formatter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": formatter + " não encontrado no PATH do servidor"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, formatter, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(req.Content)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := errors.As(err, &exitErr); ok && len(exitErr.Stderr) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": string(exitErr.Stderr)})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": string(out)})
}
// CherryPick — POST /api/v1/code-editor/repos/:id/cherry-pick
// Body: { "hash": "abc1234" }
func (h *CodeEditorHandler) CherryPick(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Hash string `json:"hash"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Hash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hash é obrigatório"})
		return
	}
	out, err := runGit(dir, "cherry-pick", req.Hash)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// ListTags — GET /api/v1/code-editor/repos/:id/tags
func (h *CodeEditorHandler) ListTags(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	// --sort=-creatordate: mais recente primeiro; %(*objectname) pega o commit apontado por tags anotadas
	out, err := runGit(dir, "tag", "-l", "--sort=-creatordate", "--format=%(refname:short)|%(creatordate:short)|%(*objectname)%(objectname)")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": out})
		return
	}
	type TagEntry struct {
		Name   string `json:"name"`
		Date   string `json:"date"`
		Commit string `json:"commit"`
	}
	var tags []TagEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		entry := TagEntry{Name: parts[0]}
		if len(parts) > 1 {
			entry.Date = parts[1]
		}
		if len(parts) > 2 {
			entry.Commit = parts[2]
			if len(entry.Commit) > 7 {
				entry.Commit = entry.Commit[:7]
			}
		}
		tags = append(tags, entry)
	}
	if tags == nil {
		tags = []TagEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// CreateTag — POST /api/v1/code-editor/repos/:id/tags
// Body: { "name": "v1.0.0", "hash": "abc1234", "message": "..." }
func (h *CodeEditorHandler) CreateTag(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Name    string `json:"name"`
		Hash    string `json:"hash"`
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	args := []string{"tag"}
	if req.Message != "" {
		args = append(args, "-a", req.Name, "-m", req.Message)
	} else {
		args = append(args, req.Name)
	}
	if req.Hash != "" {
		args = append(args, req.Hash)
	}
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Tag " + req.Name + " criada", "output": out})
}

// DeleteTag — DELETE /api/v1/code-editor/repos/:id/tags/:name
func (h *CodeEditorHandler) DeleteTag(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	out, err := runGit(dir, "tag", "-d", name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// ListFonts — GET /api/v1/code-editor/fonts
// Retorna fontes monoespaçadas instaladas no sistema via fc-list.
// O browser acessa as fontes diretamente pelo CSS font-family (servidor e browser na mesma máquina).
func (h *CodeEditorHandler) ListFonts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "fc-list", ":spacing=mono", "family").Output()
	if err != nil {
		// fc-list não disponível — retorna lista vazia (frontend usa fallback hardcoded)
		c.JSON(http.StatusOK, gin.H{"fonts": []string{}})
		return
	}

	seen := map[string]bool{}
	var fonts []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "Fira Code,Fira Code Light" → pega o primeiro nome (família base)
		name := strings.TrimSpace(strings.SplitN(line, ",", 2)[0])
		if name != "" && !seen[name] {
			seen[name] = true
			fonts = append(fonts, name)
		}
	}
	sort.Strings(fonts)
	c.JSON(http.StatusOK, gin.H{"fonts": fonts})
}
