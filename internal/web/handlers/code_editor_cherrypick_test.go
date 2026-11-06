package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func gitCP(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func TestCherryPickFluxo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	gitCP(t, dir, "init", "-b", "main")
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) } //nolint:errcheck
	write("a.txt", "base\n")
	gitCP(t, dir, "add", ".")
	gitCP(t, dir, "commit", "-m", "base")
	gitCP(t, dir, "checkout", "-b", "feat")
	write("b.txt", "novo\n")
	gitCP(t, dir, "add", ".")
	gitCP(t, dir, "commit", "-m", "limpo")
	limpo := gitCP(t, dir, "rev-parse", "HEAD")
	write("a.txt", "feat\n")
	gitCP(t, dir, "commit", "-am", "conflita")
	conflita := gitCP(t, dir, "rev-parse", "HEAD")
	gitCP(t, dir, "checkout", "main")
	write("a.txt", "main\n")
	gitCP(t, dir, "commit", "-am", "main muda a")

	h := &CodeEditorHandler{reposBase: filepath.Dir(dir)}
	id := filepath.Base(dir)
	call := func(body string) (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		h.CherryPick(c)
		return w.Code, w.Body.String()
	}

	if code, body := call(`{"hash":"` + limpo + `"}`); code != 200 || strings.Contains(body, "has_conflicts") {
		t.Fatalf("pick limpo: %d %s", code, body)
	}
	if code, body := call(`{"hash":"--abort"}`); code != 400 {
		t.Fatalf("hash inválido deveria dar 400: %d %s", code, body)
	}
	code, body := call(`{"hash":"` + conflita + `"}`)
	if code != 200 || !strings.Contains(body, `"has_conflicts":true`) {
		t.Fatalf("pick com conflito: %d %s", code, body)
	}
	if code, _ := call(`{"hash":"` + limpo + `"}`); code != 400 {
		t.Fatalf("pick durante cherry-pick em andamento deveria dar 400")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: id}}
	h.AbortMerge(c)
	if w.Code != 200 || gitPathExists(dir, "CHERRY_PICK_HEAD") {
		t.Fatalf("abort: %d %s", w.Code, w.Body.String())
	}
}
