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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
)

// CodeEditorHandler gerencia operações de edição de código com Git.
type CodeEditorHandler struct {
	tokenStore      *storage.GitHubTokenStore
	userTokensStore *storage.UserTokensStore
	historyTracker  *history.HistoryTracker
	logger          *zerolog.Logger
	reposBase       string // ~/.k8s-hpa-manager/repos
	maxReposPerUser int
}

// NewCodeEditorHandler cria o handler do editor de código.
func NewCodeEditorHandler(tokenStore *storage.GitHubTokenStore, userTokensStore *storage.UserTokensStore, ht *history.HistoryTracker, logger *zerolog.Logger) *CodeEditorHandler {
	home, _ := os.UserHomeDir()
	return &CodeEditorHandler{
		tokenStore:      tokenStore,
		userTokensStore: userTokensStore,
		historyTracker:  ht,
		logger:          logger,
		reposBase:       filepath.Join(home, ".k8s-hpa-manager", "repos"),
		maxReposPerUser: 10,
	}
}

// availableDiskMB retorna o espaço disponível em MB no diretório informado.
func availableDiskMB(dir string) int64 {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 9999
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 9999
	}
	return int64(stat.Bavail) * int64(stat.Bsize) / 1024 / 1024
}

// repoSize retorna o tamanho humanizado de um repo clonado via du -sh.
func repoSize(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "du", "-sh", dir).Output()
	if err != nil {
		return ""
	}
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
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
	Size          string    `json:"size,omitempty"` // ex: "42M"
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

// ReposBase expõe o caminho base dos repositórios (usado pelo LSP handler).
func (h *CodeEditorHandler) ReposBase() string {
	return h.reposBase
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

// gitCmdWithToken cria um exec.Cmd que injeta o token via GIT_ASKPASS.
// Quando token != "", desabilita o credential helper via -c credential.helper=
// para evitar que credenciais cacheadas no sistema sobreponham o token fornecido.
func gitCmdWithToken(ctx context.Context, dir, token string, args ...string) (*exec.Cmd, func()) {
	if token == "" {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		return cmd, func() {}
	}

	// Antepõe -c credential.helper= para desabilitar qualquer helper do sistema
	allArgs := append([]string{"-c", "credential.helper="}, args...)
	cmd := exec.CommandContext(ctx, "git", allArgs...)
	cmd.Dir = dir

	f, err := os.CreateTemp("", "git-askpass-*.sh")
	if err != nil {
		return cmd, func() {}
	}
	// Script POSIX: username=o próprio token (GitHub aceita token como username), password=token
	fmt.Fprintf(f, "#!/bin/sh\ncase \"$1\" in\n  *sername*) echo x-token-auth;;\n  *) echo %s;;\nesac\n", token)
	f.Close()
	os.Chmod(f.Name(), 0700) //nolint:errcheck
	cmd.Env = append(os.Environ(),
		"GIT_ASKPASS="+f.Name(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1", // ignora /etc/gitconfig do sistema
	)
	return cmd, func() { os.Remove(f.Name()) }
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
		owner, repo := ownerRepo(dir)
		if owner == "" {
			// fallback: split ID pelo primeiro hífen
			parts := strings.SplitN(id, "-", 2)
			if len(parts) == 2 {
				owner, repo = parts[0], parts[1]
			} else {
				owner = id
			}
		}
		repos = append(repos, RepoInfo{
			ID:            id,
			Owner:         owner,
			Repo:          repo,
			LocalPath:     dir,
			CurrentBranch: currentBranch(dir),
			RemoteURL:     remoteURL(dir),
			ClonedAt:      info.ModTime(),
			Size:          repoSize(dir),
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
		Token  string `json:"token"` // token explícito sobrepõe o armazenado
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Owner == "" || req.Repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner e repo são obrigatórios"})
		return
	}

	token := req.Token
	if token == "" {
		token = h.getToken(c)
	}
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

	// Quota de disco: exige pelo menos 500 MB livres
	if avail := availableDiskMB(h.reposBase); avail < 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("espaço em disco insuficiente: apenas %d MB disponíveis (mínimo 500 MB)", avail)})
		return
	}

	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", req.Owner, req.Repo)

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

	cmd, cleanup := gitCmdWithToken(ctx, "", token, args...)
	defer cleanup()
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
		sendEvent(scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
	} else {
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

	// Executar git diretamente sem TrimSpace global — o runGit padrão remove espaços
	// iniciais do output, corrompendo o XY code em linhas como " M arquivo.txt"
	// (o espaço inicial significa X=vazio, Y=modificado). Sem ele, line[3:] = "rquivo.txt".
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	rawOut, cmdErr := cmd.CombinedOutput()
	if cmdErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": string(rawOut)})
		return
	}

	files := []GitStatusFile{}
	for _, line := range strings.Split(string(rawOut), "\n") {
		if len(line) < 4 {
			continue
		}
		// XY é sempre 2 chars; preservar espaços (semanticamente importantes)
		xy := line[:2]
		path := strings.TrimSpace(line[3:])
		if strings.TrimSpace(xy) == "" || path == "" {
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

// injectTokenURL injeta o token diretamente na URL: https://TOKEN@github.com/...
// É o método mais confiável — ignora credential helper e GIT_ASKPASS.
func injectTokenURL(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rawURL, prefix) {
			// Remove qualquer token já embutido (https://OLD@github.com)
			rest := strings.TrimPrefix(rawURL, prefix)
			if at := strings.Index(rest, "@"); at >= 0 {
				rest = rest[at+1:]
			}
			return prefix + "x-token-auth:" + token + "@" + rest
		}
	}
	return rawURL
}

// cleanRemoteURL remove token da URL para não ficar exposto no git config.
func cleanRemoteURL(rawURL string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rawURL, prefix) {
			rest := strings.TrimPrefix(rawURL, prefix)
			if at := strings.Index(rest, "@"); at >= 0 {
				rest = rest[at+1:]
			}
			return prefix + rest
		}
	}
	return rawURL
}

// Pull — POST /api/v1/code-editor/repos/:id/pull (SSE)
func (h *CodeEditorHandler) Pull(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	var req struct {
		Token string `json:"token"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck

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

	token := req.Token
	if token == "" {
		token = h.getToken(c)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Usa ASKPASS (mesmo mecanismo do clone) para autenticar.
	// git pull --progress origin <branch> é explícito sobre qual branch puxar,
	// evitando o comportamento errado de "git pull <url>" que mescla FETCH_HEAD
	// (branch padrão do remoto) em vez do branch rastreado atual.
	branch := currentBranch(dir)
	pullArgs := []string{"pull", "--progress", "origin"}
	if branch != "" {
		pullArgs = append(pullArgs, branch)
	}

	cmd, cleanup := gitCmdWithToken(ctx, dir, token, pullArgs...)
	defer cleanup()

	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	var wgPull sync.WaitGroup
	wgPull.Add(1)
	go func() {
		defer wgPull.Done()
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

	pullErr := cmd.Wait()
	wgPull.Wait() // garante que toda saída foi enviada antes do "done"

	if pullErr != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", pullErr.Error())
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

	// Audit log
	if h.historyTracker != nil {
		branch := currentBranch(dir)
		remote := remoteURL(dir)
		msg := req.Message
		if req.Amend {
			msg = "[amend] " + msg
		}
		userInfo := GetUserInfoForHistory(c)
		h.historyTracker.Log(history.HistoryEntry{ //nolint:errcheck
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "code_editor_commit",
			Resource:  id,
			Cluster:   remote,
			Before:    map[string]interface{}{"branch": branch},
			After:     map[string]interface{}{"message": msg, "output": strings.TrimSpace(out)},
			Status:    history.StatusSuccess,
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": out})
}

// Push — POST /api/v1/code-editor/repos/:id/push (SSE)
// Body: { "branch": "..." (optional), "token": "..." (optional) }
func (h *CodeEditorHandler) Push(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Branch string `json:"branch"`
		Token  string `json:"token"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck

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

	token := req.Token
	if token == "" {
		token = h.getToken(c)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	if token != "" {
		// Injeta token diretamente na URL do push — ignora qualquer credential helper
		originURL, _ := runGit(dir, "remote", "get-url", "origin")
		authedURL := injectTokenURL(cleanRemoteURL(originURL), token)
		gitArgs := []string{"-c", "credential.helper=", "push", "--progress", authedURL, "HEAD:" + branch}
		cmd = exec.CommandContext(ctx, "git", gitArgs...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	} else {
		cmd = exec.CommandContext(ctx, "git", "push", "--progress", "origin", branch)
		cmd.Dir = dir
	}

	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	var wgPush sync.WaitGroup
	var rejected bool
	wgPush.Add(1)
	go func() {
		defer wgPush.Done()
		defer func() { recover() }() //nolint:errcheck
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			if token != "" {
				line = strings.ReplaceAll(line, token, "***")
			}
			if strings.Contains(line, "[rejected]") || strings.Contains(line, "rejected)") ||
				(strings.Contains(line, "rejected") && strings.Contains(line, "fetch first")) {
				rejected = true
			}
			sendSSE(line)
		}
	}()

	pushErr := cmd.Wait()
	wgPush.Wait()

	// Se o push foi rejeitado por divergência, faz pull --rebase e tenta novamente.
	if pushErr != nil && rejected {
		sendSSE("")
		sendSSE("⚠️  Push rejeitado — o remote tem commits novos.")
		sendSSE("🔄 Fazendo git pull --rebase automaticamente...")

		pullCtx, pullCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		pullCmd, pullCleanup := gitCmdWithToken(pullCtx, dir, token, "pull", "--rebase", "--progress", "origin", branch)
		defer pullCancel()
		defer pullCleanup()

		pullStderr, _ := pullCmd.StderrPipe()
		pullStdout, _ := pullCmd.StdoutPipe()
		var pullWg sync.WaitGroup
		pullWg.Add(1)
		go func() {
			defer pullWg.Done()
			scanner := bufio.NewScanner(io.MultiReader(pullStdout, pullStderr))
			for scanner.Scan() {
				line := scanner.Text()
				if token != "" {
					line = strings.ReplaceAll(line, token, "***")
				}
				sendSSE(line)
			}
		}()
		pullErr := pullCmd.Start()
		if pullErr == nil {
			pullErr = pullCmd.Wait()
		}
		pullWg.Wait()

		if pullErr != nil {
			sendSSE("❌ Pull --rebase falhou. Resolva os conflitos e tente novamente.")
			fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", pullErr.Error())
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		// Retry push após pull bem-sucedido
		sendSSE("")
		sendSSE("✅ Pull concluído. Repetindo git push...")

		retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer retryCancel()
		var retryCmd *exec.Cmd
		if token != "" {
			originURL, _ := runGit(dir, "remote", "get-url", "origin")
			authedURL := injectTokenURL(cleanRemoteURL(originURL), token)
			args := []string{"-c", "credential.helper=", "push", "--progress", authedURL, "HEAD:" + branch}
			retryCmd = exec.CommandContext(retryCtx, "git", args...)
			retryCmd.Dir = dir
			retryCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		} else {
			retryCmd = exec.CommandContext(retryCtx, "git", "push", "--progress", "origin", branch)
			retryCmd.Dir = dir
		}
		retryStderr, _ := retryCmd.StderrPipe()
		retryStdout, _ := retryCmd.StdoutPipe()
		var retryWg sync.WaitGroup
		retryWg.Add(1)
		go func() {
			defer retryWg.Done()
			scanner := bufio.NewScanner(io.MultiReader(retryStdout, retryStderr))
			for scanner.Scan() {
				line := scanner.Text()
				if token != "" {
					line = strings.ReplaceAll(line, token, "***")
				}
				sendSSE(line)
			}
		}()
		retryErr := retryCmd.Start()
		if retryErr == nil {
			retryErr = retryCmd.Wait()
		}
		retryWg.Wait()

		if retryErr != nil {
			fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", retryErr.Error())
		} else {
			sendSSE("✅ Push concluído com sucesso.")
			fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	if pushErr != nil {
		fmt.Fprintf(c.Writer, "data: {\"done\":true,\"error\":%q}\n\n", pushErr.Error())
	} else {
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

// CopyFile — POST /api/v1/code-editor/repos/:id/copy
// Body: { "from": "...", "to": "..." }
func (h *CodeEditorHandler) CopyFile(c *gin.Context) {
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
	src, err := os.Open(fromPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	dst, err := os.Create(toPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
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

// Stage — POST /api/v1/code-editor/repos/:id/stage
// Body: { "files": ["path1", "path2"] } — vazio = git add .
func (h *CodeEditorHandler) Stage(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Files []string `json:"files"`
	}
	c.ShouldBindJSON(&req) //nolint:errcheck
	var args []string
	if len(req.Files) == 0 {
		args = []string{"add", "."}
	} else {
		args = append([]string{"add", "--"}, req.Files...)
	}
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// Unstage — POST /api/v1/code-editor/repos/:id/unstage
// Body: { "files": ["path1", "path2"] }
func (h *CodeEditorHandler) Unstage(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	var req struct {
		Files []string `json:"files"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "files é obrigatório"})
		return
	}
	args := append([]string{"restore", "--staged", "--"}, req.Files...)
	out, err := runGit(dir, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
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
		// Conflitos de merge: git retorna exit 1 mas o merge é iniciado
		if strings.Contains(out, "CONFLICT") {
			c.JSON(http.StatusOK, gin.H{"message": out, "has_conflicts": true})
			return
		}
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

// ─── Fase 4: Git Blame ──────────────────────────────────────────────────────

// BlameLineInfo representa uma linha do git blame.
type BlameLineInfo struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
	Line    int    `json:"line"`
}

func isHexStr(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func parseBlame(raw string) []BlameLineInfo {
	cache := map[string]BlameLineInfo{}
	var result []BlameLineInfo
	var cur BlameLineInfo

	for _, line := range strings.Split(raw, "\n") {
		if len(line) == 0 {
			continue
		}
		if line[0] == '\t' {
			if cur.Line > 0 {
				if cached, ok := cache[cur.Hash]; ok {
					cur.Author = cached.Author
					cur.Date = cached.Date
					cur.Summary = cached.Summary
				}
				result = append(result, cur)
				cur = BlameLineInfo{}
			}
			continue
		}
		if len(line) >= 40 && isHexStr(line[:40]) {
			parts := strings.Fields(line)
			hash := parts[0]
			cur.Hash = hash
			cur.Short = hash[:7]
			if len(parts) >= 3 {
				if n, err := strconv.Atoi(parts[2]); err == nil {
					cur.Line = n
				}
			}
			continue
		}
		if strings.HasPrefix(line, "author ") && !strings.HasPrefix(line, "author-") {
			cur.Author = strings.TrimPrefix(line, "author ")
		} else if strings.HasPrefix(line, "author-time ") {
			if ts, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				cur.Date = time.Unix(ts, 0).Format("2006-01-02")
			}
		} else if strings.HasPrefix(line, "summary ") {
			cur.Summary = strings.TrimPrefix(line, "summary ")
		}
		if cur.Hash != "" && cur.Author != "" {
			cache[cur.Hash] = cur
		}
	}
	return result
}

// GetBlame retorna anotações de blame para um arquivo.
func (h *CodeEditorHandler) GetBlame(c *gin.Context) {
	id := c.Param("id")
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path obrigatório"})
		return
	}
	dir := h.repoDir(id)
	if _, err := os.Stat(dir); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo não encontrado"})
		return
	}
	out, err := runGit(dir, "blame", "--porcelain", path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	lines := parseBlame(out)
	c.JSON(http.StatusOK, gin.H{"lines": lines})
}

// ─── Fase 4: Histórico de arquivo ──────────────────────────────────────────

// FileLogEntry representa uma entrada no histórico de um arquivo.
type FileLogEntry struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// GetFileLog retorna o histórico de commits de um arquivo específico.
func (h *CodeEditorHandler) GetFileLog(c *gin.Context) {
	id := c.Param("id")
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path obrigatório"})
		return
	}
	dir := h.repoDir(id)
	if _, err := os.Stat(dir); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo não encontrado"})
		return
	}
	out, err := runGit(dir, "log", "--follow", "--pretty=format:%H|%an|%ad|%s", "--date=short", "--", path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	var entries []FileLogEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, FileLogEntry{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Message: parts[3],
		})
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
}

// GetFileAtCommit retorna o conteúdo de um arquivo em um commit específico.
func (h *CodeEditorHandler) GetFileAtCommit(c *gin.Context) {
	id := c.Param("id")
	hash := c.Query("hash")
	path := c.Query("path")
	if hash == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hash e path obrigatórios"})
		return
	}
	dir := h.repoDir(id)
	if _, err := os.Stat(dir); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo não encontrado"})
		return
	}
	out, err := runGit(dir, "show", hash+":"+path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": out})
}

// ─── Fase 4: Upload de arquivos ────────────────────────────────────────────

// UploadFiles aceita múltiplos arquivos via multipart form e os salva no repo.
func (h *CodeEditorHandler) UploadFiles(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	if _, err := os.Stat(dir); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo não encontrado"})
		return
	}

	targetDir := c.PostForm("dir")
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "form inválido: " + err.Error()})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nenhum arquivo enviado"})
		return
	}

	var created []string
	for _, fh := range files {
		rel := filepath.Join(targetDir, filepath.Base(fh.Filename))
		full := filepath.Join(dir, rel)
		// Validar path traversal
		if !strings.HasPrefix(filepath.Clean(full)+string(os.PathSeparator), filepath.Clean(dir)+string(os.PathSeparator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			continue
		}
		if err := c.SaveUploadedFile(fh, full); err != nil {
			continue
		}
		created = append(created, rel)
	}
	c.JSON(http.StatusOK, gin.H{"created": created})
}

// ─── Fase 4: Find & Replace global ────────────────────────────────────────

// ReplaceMatch representa um match de substituição.
type ReplaceMatch struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ReplaceRequest é o body do endpoint de replace.
type ReplaceRequest struct {
	Query       string `json:"query"`
	Replacement string `json:"replacement"`
	IsRegex     bool   `json:"is_regex"`
	Glob        string `json:"glob"`    // ex: "*.go" — filtra por extensão
	DryRun      bool   `json:"dry_run"` // se true, apenas pré-visualiza
}

var ignoredReplDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "build": true, "dist": true,
}

// ReplaceInFiles realiza find & replace em todos os arquivos do repo.
func (h *CodeEditorHandler) ReplaceInFiles(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	if _, err := os.Stat(dir); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo não encontrado"})
		return
	}

	var req ReplaceRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query obrigatória"})
		return
	}

	// Construir regexp
	var re *regexp.Regexp
	if req.IsRegex {
		var err error
		re, err = regexp.Compile(req.Query)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "regex inválida: " + err.Error()})
			return
		}
	}

	// Calcular extensão-alvo do glob (ex: "*.go" → ".go")
	var extFilter string
	if req.Glob != "" {
		extFilter = strings.TrimPrefix(req.Glob, "*")
	}

	var matches []ReplaceMatch
	modifiedFiles := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		// Ignorar diretórios proibidos
		parts := strings.SplitN(rel, string(os.PathSeparator), 2)
		if d.IsDir() {
			if ignoredReplDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Filtro de extensão
		if extFilter != "" && !strings.HasSuffix(d.Name(), extFilter) {
			return nil
		}
		_ = parts

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		// Ignorar binários (heurística: byte nulo nos primeiros 512 bytes)
		check := data
		if len(check) > 512 {
			check = check[:512]
		}
		for _, b := range check {
			if b == 0 {
				return nil
			}
		}

		lines := strings.Split(string(data), "\n")
		changed := false
		for i, line := range lines {
			var newLine string
			if req.IsRegex {
				newLine = re.ReplaceAllString(line, req.Replacement)
			} else {
				newLine = strings.ReplaceAll(line, req.Query, req.Replacement)
			}
			if newLine != line {
				matches = append(matches, ReplaceMatch{
					File:   rel,
					Line:   i + 1,
					Before: line,
					After:  newLine,
				})
				lines[i] = newLine
				changed = true
			}
		}
		if changed && !req.DryRun {
			newContent := strings.Join(lines, "\n")
			if err := os.WriteFile(path, []byte(newContent), 0644); err == nil {
				modifiedFiles++
			}
		} else if changed {
			modifiedFiles++
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"matches":        matches,
		"modified_files": modifiedFiles,
		"applied":        !req.DryRun,
	})
}

// ── Fase 5: Resolução de conflitos ──────────────────────────────────────────

// GetConflicts — GET /api/v1/code-editor/repos/:id/conflicts
// Retorna se há merge em andamento e quais arquivos têm conflitos.
func (h *CodeEditorHandler) GetConflicts(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
		c.JSON(http.StatusOK, gin.H{"in_merge": false, "files": []string{}})
		return
	}

	out, _ := runGit(dir, "diff", "--name-only", "--diff-filter=U")
	files := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	c.JSON(http.StatusOK, gin.H{"in_merge": true, "files": files})
}

// ResolveConflict — POST /api/v1/code-editor/repos/:id/resolve-conflict
// Body: { "path": "...", "content": "..." }
// Grava o conteúdo resolvido e faz git add.
func (h *CodeEditorHandler) ResolveConflict(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path obrigatório"})
		return
	}

	fullPath := filepath.Join(dir, filepath.Clean(req.Path))
	if !strings.HasPrefix(fullPath, dir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "caminho inválido"})
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	out, err := runGit(dir, "add", req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"staged": req.Path})
}

// AbortMerge — POST /api/v1/code-editor/repos/:id/merge/abort
func (h *CodeEditorHandler) AbortMerge(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	out, err := runGit(dir, "merge", "--abort")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// CommitMerge — POST /api/v1/code-editor/repos/:id/merge/commit
// Finaliza o merge após resolver todos os conflitos (git commit --no-edit).
func (h *CodeEditorHandler) CommitMerge(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	out, err := runGit(dir, "commit", "--no-edit")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": out})
}

// ── Fase 5: Diff entre branches ─────────────────────────────────────────────

// GetBranchDiff — GET /api/v1/code-editor/repos/:id/branch-diff?from=&to=
// Retorna diff unificado entre dois refs (branches, tags ou commits).
func (h *CodeEditorHandler) GetBranchDiff(c *gin.Context) {
	id := c.Param("id")
	dir := h.repoDir(id)
	from := c.Query("from")
	to := c.Query("to")
	if from == "" || to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from e to são obrigatórios"})
		return
	}

	// Lista de arquivos alterados (formato: status<TAB>path)
	filesOut, _ := runGit(dir, "diff", "--name-status", from+".."+to)

	// Diff unificado (limite 500 KB)
	diffOut, _ := runGit(dir, "diff", "--unified=3", from+".."+to)
	const maxDiff = 500 * 1024
	if len(diffOut) > maxDiff {
		diffOut = diffOut[:maxDiff] + "\n\n... (diff truncado — muito grande para exibir)"
	}

	c.JSON(http.StatusOK, gin.H{
		"diff":  diffOut,
		"files": filesOut,
		"from":  from,
		"to":    to,
	})
}

func profileEmail(c *gin.Context) string {
	email, _ := c.Get("user_email")
	s := fmt.Sprintf("%v", email)
	if s == "" || s == "<nil>" {
		return "default"
	}
	return s
}

// GetGitHubProfiles — GET /api/v1/code-editor/github-profiles
func (h *CodeEditorHandler) GetGitHubProfiles(c *gin.Context) {
	if h.userTokensStore == nil {
		c.JSON(http.StatusOK, gin.H{"profiles": []storage.GitHubEditorProfile{}})
		return
	}
	profiles, err := h.userTokensStore.GetGitHubEditorProfiles(profileEmail(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

// SaveGitHubProfiles — PUT /api/v1/code-editor/github-profiles
func (h *CodeEditorHandler) SaveGitHubProfiles(c *gin.Context) {
	var req struct {
		Profiles []storage.GitHubEditorProfile `json:"profiles"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido"})
		return
	}
	if h.userTokensStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	if err := h.userTokensStore.SaveGitHubEditorProfiles(profileEmail(c), req.Profiles); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// CreatePR — POST /api/v1/code-editor/repos/:id/pr/create
// Cria um Pull Request via GitHub API usando o PAT do usuário.
// Body: { "title", "body", "head", "base" }
func (h *CodeEditorHandler) CreatePR(c *gin.Context) {
	id := c.Param("id")
	dir := filepath.Join(h.reposBase, id)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repositório não encontrado"})
		return
	}

	var req struct {
		Title string `json:"title" binding:"required"`
		Body  string `json:"body"`
		Head  string `json:"head" binding:"required"`
		Base  string `json:"base" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	owner, repo := ownerRepo(dir)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "não foi possível determinar owner/repo do repositório"})
		return
	}

	token := h.getToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "PAT GitHub não configurado — configure em GitHub Releases → perfil"})
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"title": req.Title,
		"body":  req.Body,
		"head":  req.Head,
		"base":  req.Base,
	})

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, apiURL, strings.NewReader(string(payload)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "falha ao contatar GitHub API: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		var ghErr struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(respBody, &ghErr)
		msg := ghErr.Message
		if len(ghErr.Errors) > 0 {
			msg += ": " + ghErr.Errors[0].Message
		}
		if msg == "" {
			msg = fmt.Sprintf("GitHub API retornou %d", resp.StatusCode)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	var pr struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
		State   string `json:"state"`
	}
	_ = json.Unmarshal(respBody, &pr)
	c.JSON(http.StatusCreated, gin.H{
		"number": pr.Number,
		"url":    pr.HTMLURL,
		"title":  pr.Title,
	})
}
