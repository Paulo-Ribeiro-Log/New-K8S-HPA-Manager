package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// repo clonado de um origin bare local, com a branch "feat" publicada.
func setupBranchRepo(t *testing.T) (h *CodeEditorHandler, id, dir string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "teste")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "teste@example.com")
	}
	t.Setenv("GITHUB_TOKEN", "")
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	dir = filepath.Join(base, "repo")
	g := func(d string, args ...string) {
		if out, err := runGit(d, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(origin, 0o755) //nolint:errcheck
	g(origin, "init", "--bare", "-b", "main")
	os.MkdirAll(dir, 0o755) //nolint:errcheck
	g(dir, "init", "-b", "main")
	g(dir, "remote", "add", "origin", origin)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644) //nolint:errcheck
	g(dir, "add", ".")
	g(dir, "commit", "-m", "base")
	g(dir, "push", "-u", "origin", "main")
	g(dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644) //nolint:errcheck
	g(dir, "add", ".")
	g(dir, "commit", "-m", "feat")
	g(dir, "push", "-u", "origin", "feat")
	// commit só local: a branch deixa de estar mergeada em qualquer lugar
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644) //nolint:errcheck
	g(dir, "add", ".")
	g(dir, "commit", "-m", "so local")
	g(dir, "checkout", "main")
	return &CodeEditorHandler{reposBase: base}, "repo", dir
}

func postBranch(h *CodeEditorHandler, id, route, body string) (int, map[string]any) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if route == "rename" {
		h.RenameBranch(c)
	} else {
		h.DeleteBranch(c)
	}
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m) //nolint:errcheck
	return w.Code, m
}

func TestRenameBranchLocalERemoto(t *testing.T) {
	h, id, dir := setupBranchRepo(t)
	if code, m := postBranch(h, id, "rename", `{"old":"feat","new":"feat2","rename_remote":true}`); code != 200 || m["remote_renamed"] != true {
		t.Fatalf("rename: %d %v", code, m)
	}
	if branchExists(dir, "refs/heads/feat") || !branchExists(dir, "refs/heads/feat2") {
		t.Fatal("renomeação local não aconteceu")
	}
	if out, _ := runGit(dir, "ls-remote", "--heads", "origin"); strings.Contains(out, "refs/heads/feat\n") || !strings.Contains(out, "refs/heads/feat2") {
		t.Fatalf("remoto: %s", out)
	}
	if code, _ := postBranch(h, id, "rename", `{"old":"feat2","new":"main"}`); code != 409 {
		t.Fatalf("destino existente deveria dar 409, deu %d", code)
	}
	if code, _ := postBranch(h, id, "rename", `{"old":"feat2","new":"-x"}`); code != 400 {
		t.Fatalf("nome inválido deveria dar 400, deu %d", code)
	}
	if code, _ := postBranch(h, id, "rename", `{"old":"main","new":"principal","rename_remote":true}`); code != 403 {
		t.Fatalf("main no remoto deveria dar 403, deu %d", code)
	}
}

func TestDeleteBranch(t *testing.T) {
	h, id, dir := setupBranchRepo(t)
	if code, _ := postBranch(h, id, "delete", `{"name":"main","scope":"local"}`); code != 409 {
		t.Fatalf("branch atual deveria dar 409, deu %d", code)
	}
	if code, _ := postBranch(h, id, "delete", `{"name":"main","scope":"both"}`); code != 409 && code != 403 {
		t.Fatalf("main deveria ser recusada, deu %d", code)
	}
	if code, _ := postBranch(h, id, "delete", `{"name":"main","scope":"remote"}`); code != 403 {
		t.Fatalf("main remoto deveria dar 403, deu %d", code)
	}
	// feat não foi mergeada em main → -d recusa; force apaga
	code, m := postBranch(h, id, "delete", `{"name":"feat","scope":"local"}`)
	if code != 409 || m["error_type"] != "not_merged" {
		t.Fatalf("não mergeada: %d %v", code, m)
	}
	if code, m := postBranch(h, id, "delete", `{"name":"feat","scope":"both","force":true}`); code != 200 {
		t.Fatalf("both+force: %d %v", code, m)
	}
	if branchExists(dir, "refs/heads/feat") {
		t.Fatal("branch local ainda existe")
	}
	if out, _ := runGit(dir, "ls-remote", "--heads", "origin"); strings.Contains(out, "refs/heads/feat") {
		t.Fatalf("remoto ainda tem feat: %s", out)
	}
}
