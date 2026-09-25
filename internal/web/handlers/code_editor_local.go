package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// "Abrir pasta" do Code Editor: edita uma pasta qualquer da máquina do servidor (no WSL, inclusive
// as do Windows em /mnt/<drive>) sem clonar nada. A pasta vira um link simbólico
// reposBase/local-<nome> → pasta real; como todo o editor (árvore, arquivos, busca, Git, LSP,
// terminal, K8s) resolve o diretório como reposBase/<id>, nada mais precisa saber que é local.
// Fechar a pasta remove só o link (ver DeleteRepo), nunca os arquivos.

const localFolderPrefix = "local-"

// maxBrowseEntries limita a listagem de subpastas de um diretório no navegador de pastas.
const maxBrowseEntries = 2000

var (
	windowsDrivePathRe = regexp.MustCompile(`^([A-Za-z]):(?:[\\/](.*))?$`)
	// \\wsl$\Ubuntu\home\x ou \\wsl.localhost\Ubuntu\home\x → /home/x
	wslUNCPathRe  = regexp.MustCompile(`^(?i)[\\/]{2}wsl(?:\$|\.localhost)[\\/][^\\/]+(.*)$`)
	nonSlugCharRe = regexp.MustCompile(`[^a-z0-9._-]+`)
	driveRootRe   = regexp.MustCompile(`^/mnt/[a-z]$`)
)

// normalizeLocalPath converte o caminho digitado pelo usuário: vazio → home, "~" → home, caminho
// do Windows (C:\Users\x) → /mnt/c/Users/x e \\wsl$\<distro>\x → /x quando o servidor roda no
// WSL. Não acessa o disco.
func normalizeLocalPath(raw string) (string, error) {
	p := strings.Trim(strings.TrimSpace(raw), `"'`)
	home, _ := os.UserHomeDir()

	switch {
	case p == "" || p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	case runtime.GOOS == "linux" && wslUNCPathRe.MatchString(p):
		p = strings.ReplaceAll(wslUNCPathRe.FindStringSubmatch(p)[1], `\`, "/")
	case runtime.GOOS == "linux" && windowsDrivePathRe.MatchString(p):
		m := windowsDrivePathRe.FindStringSubmatch(p)
		p = "/mnt/" + strings.ToLower(m[1]) + "/" + strings.ReplaceAll(m[2], `\`, "/")
	}

	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("caminho precisa ser absoluto: %q", raw)
	}
	return filepath.Clean(p), nil
}

// resolveLocalPath = normalizeLocalPath + exige pasta existente.
func resolveLocalPath(raw string) (string, error) {
	p, err := normalizeLocalPath(raw)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("pasta não encontrada: %s", p)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("não é uma pasta: %s", p)
	}
	return p, nil
}

func isGitDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

type browseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsGit bool   `json:"is_git"`
}

type browseShortcut struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// BrowseFolders — GET /api/v1/code-editor/browse?path=...&hidden=1
// Lista as subpastas de um diretório do servidor (navegador do "Abrir pasta").
func (h *CodeEditorHandler) BrowseFolders(c *gin.Context) {
	dir, err := resolveLocalPath(c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	showHidden := c.Query("hidden") == "1"

	entries, err := os.ReadDir(dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("sem permissão para ler %s: %v", dir, err)})
		return
	}

	dirs := make([]browseEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		fi, err := os.Stat(full) // segue links: link para pasta também é navegável
		if err != nil || !fi.IsDir() {
			continue
		}
		dirs = append(dirs, browseEntry{Name: name, Path: full, IsGit: isGitDir(full)})
		if len(dirs) >= maxBrowseEntries {
			break
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })

	parent := ""
	if dir != "/" {
		parent = filepath.Dir(dir)
	}

	c.JSON(http.StatusOK, gin.H{
		"path":      dir,
		"parent":    parent,
		"is_git":    isGitDir(dir),
		"dirs":      dirs,
		"shortcuts": browseShortcuts(),
	})
}

// browseShortcuts: home do servidor + discos do Windows montados pelo WSL (/mnt/c, /mnt/d...).
func browseShortcuts() []browseShortcut {
	var out []browseShortcut
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, browseShortcut{Label: "Home", Path: home})
	}
	if runtime.GOOS == "linux" {
		drives, _ := filepath.Glob("/mnt/[a-z]")
		for _, d := range drives {
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				label := strings.ToUpper(filepath.Base(d)) + ":"
				if _, err := os.Stat(filepath.Join(d, "Users")); err == nil {
					out = append(out, browseShortcut{Label: label + `\Users`, Path: filepath.Join(d, "Users")})
				} else {
					out = append(out, browseShortcut{Label: label, Path: d})
				}
			}
		}
	}
	return out
}

// OpenFolder — POST /api/v1/code-editor/open-folder  Body: { "path": "..." }
// Registra a pasta como item do editor (link reposBase/local-<nome>). Idempotente: a mesma
// pasta aberta de novo devolve o item existente.
func (h *CodeEditorHandler) OpenFolder(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body inválido"})
		return
	}
	target, err := resolveLocalPath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Raiz do sistema ou de um disco do Windows: árvore gigantesca, sem sentido como workspace.
	if target == "/" || driveRootRe.MatchString(target) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "escolha uma pasta mais específica que a raiz do disco"})
		return
	}
	if target == h.reposBase || strings.HasPrefix(target+string(os.PathSeparator), h.reposBase+string(os.PathSeparator)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "essa pasta já é gerenciada pelo editor (repositórios clonados)"})
		return
	}
	if err := os.MkdirAll(h.reposBase, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Já aberta? Devolve o item existente.
	entries, _ := os.ReadDir(h.reposBase)
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if dest, err := os.Readlink(filepath.Join(h.reposBase, e.Name())); err == nil && dest == target {
			c.JSON(http.StatusOK, h.localRepoInfo(e.Name()))
			return
		}
	}

	slug := strings.Trim(nonSlugCharRe.ReplaceAllString(strings.ToLower(filepath.Base(target)), "-"), "-")
	if slug == "" {
		slug = "pasta"
	}
	id := localFolderPrefix + slug
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(h.reposBase, id)); os.IsNotExist(err) {
			break
		}
		if n > 99 {
			c.JSON(http.StatusConflict, gin.H{"error": "muitas pastas abertas com o mesmo nome"})
			return
		}
		id = fmt.Sprintf("%s%s-%d", localFolderPrefix, slug, n)
	}

	if err := os.Symlink(target, filepath.Join(h.reposBase, id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao registrar a pasta: %v", err)})
		return
	}
	h.logger.Info().Str("id", id).Str("path", target).Msg("Code Editor: pasta local aberta")
	c.JSON(http.StatusOK, h.localRepoInfo(id))
}

// isLocalFolder informa se o item id é uma pasta local (link), sem seguir o link.
func (h *CodeEditorHandler) isLocalFolder(id string) bool {
	fi, err := os.Lstat(h.repoDir(id))
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// localRepoInfo monta o RepoInfo de uma pasta local. Sem Size: `du` numa pasta arbitrária
// (ex: /mnt/c/Users/x) pode levar muito tempo.
func (h *CodeEditorHandler) localRepoInfo(id string) RepoInfo {
	link := h.repoDir(id)
	target, _ := os.Readlink(link)
	info := RepoInfo{
		ID:        id,
		Repo:      filepath.Base(target),
		LocalPath: target,
		IsLocal:   true,
		IsGit:     isGitDir(link),
	}
	if fi, err := os.Lstat(link); err == nil {
		info.ClonedAt = fi.ModTime()
	}
	if info.IsGit {
		info.CurrentBranch = currentBranch(link)
		info.RemoteURL = remoteURL(link)
		// owner/repo do remote (usados pelo "Criar PR"); o frontend exibe o nome da pasta
		// (local_path) para itens locais.
		if owner, repo := ownerRepo(link); owner != "" {
			info.Owner, info.Repo = owner, repo
		}
	}
	return info
}
