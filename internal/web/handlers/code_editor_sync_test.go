package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// Cenários reais do Sync (Pull → Push) do Code Editor, contra um remoto bare local.

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupSyncRepo cria origin.git (bare, branch main com 1 commit) e o clone em reposBase/<id>.
func setupSyncRepo(t *testing.T) (h *CodeEditorHandler, r *gin.Engine, origin, work string) {
	t.Helper()
	base := t.TempDir()
	// Isola do ~/.gitconfig do usuário (pull.rebase, credential helper…) e define identidade.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(base, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	t.Setenv("GITHUB_TOKEN", "")

	origin = filepath.Join(base, "origin.git")
	gitT(t, base, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(base, "seed")
	gitT(t, base, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "f"), []byte("1\n"), 0o644) //nolint:errcheck
	gitT(t, seed, "add", "f")
	gitT(t, seed, "commit", "-qm", "c1")
	gitT(t, seed, "push", "-q", "origin", "HEAD:main")

	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	h = NewCodeEditorHandler(nil, nil, nil, &logger)
	h.reposBase = filepath.Join(base, "repos")
	work = filepath.Join(h.reposBase, "repo")
	gitT(t, base, "clone", "-q", origin, work)

	r = gin.New()
	r.POST("/repos/:id/pull", h.Pull)
	r.POST("/repos/:id/push", h.Push)
	return h, r, origin, work
}

// callSSE devolve o corpo SSE e se terminou com erro.
func callSSE(t *testing.T, r *gin.Engine, path, body string) (string, bool) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	out := w.Body.String()
	return out, strings.Contains(out, `"done":true,"error"`)
}

func TestSync_BranchNaoPublicada(t *testing.T) {
	_, r, origin, work := setupSyncRepo(t)
	// Igual ao caso real: branch criada a partir de origin/main (upstream = origin/main).
	gitT(t, work, "checkout", "-q", "-b", "feat/nova", "origin/main")

	out, failed := callSSE(t, r, "/repos/repo/pull", "")
	if failed || !strings.Contains(out, "ainda não existe no remoto") {
		t.Fatalf("pull de branch não publicada deveria concluir sem erro:\n%s", out)
	}

	// Sync encadeia o push: publica e liga ao upstream correto. Token fictício força o caminho
	// "push para a URL" (o que não atualiza refs/remotes sozinho).
	out, failed = callSSE(t, r, "/repos/repo/push", `{"token":"dummy"}`)
	if failed {
		t.Fatalf("push falhou:\n%s", out)
	}
	if got := gitT(t, origin, "rev-parse", "refs/heads/feat/nova"); got != gitT(t, work, "rev-parse", "HEAD") {
		t.Fatalf("branch não foi publicada no remoto")
	}
	if up := gitT(t, work, "rev-parse", "--abbrev-ref", "@{u}"); up != "origin/feat/nova" {
		t.Fatalf("upstream = %q, esperado origin/feat/nova", up)
	}
	if n := gitT(t, work, "rev-list", "--count", "@{u}..HEAD"); n != "0" {
		t.Fatalf("ahead = %s após push, esperado 0", n)
	}
}

func TestSync_PullDivergenteComArquivoNaoCommitado(t *testing.T) {
	_, r, origin, work := setupSyncRepo(t)

	// Commit novo no remoto por outro clone.
	other := filepath.Join(filepath.Dir(origin), "other")
	gitT(t, filepath.Dir(origin), "clone", "-q", origin, other)
	os.WriteFile(filepath.Join(other, "remoto"), []byte("r\n"), 0o644) //nolint:errcheck
	gitT(t, other, "add", "remoto")
	gitT(t, other, "commit", "-qm", "remoto")
	gitT(t, other, "push", "-q", "origin", "HEAD:main")

	// Commit local (diverge) + arquivo modificado sem commit.
	os.WriteFile(filepath.Join(work, "local"), []byte("l\n"), 0o644) //nolint:errcheck
	gitT(t, work, "add", "local")
	gitT(t, work, "commit", "-qm", "local")
	os.WriteFile(filepath.Join(work, "f"), []byte("editado\n"), 0o644) //nolint:errcheck

	out, failed := callSSE(t, r, "/repos/repo/pull", "")
	if failed {
		t.Fatalf("pull divergente deveria fazer rebase:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(work, "remoto")); err != nil {
		t.Fatalf("commit remoto não foi integrado")
	}
	if got := gitT(t, work, "log", "-1", "--format=%s"); got != "local" {
		t.Fatalf("commit local deveria ficar por cima (rebase), topo = %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(work, "f")); string(b) != "editado\n" {
		t.Fatalf("autostash perdeu a modificação não commitada: %q", b)
	}
}
