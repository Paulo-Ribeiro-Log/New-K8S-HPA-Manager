package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestNormalizeLocalPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"":                 home,
		"~":                home,
		"~/scripts":        filepath.Join(home, "scripts"),
		`"/tmp/x/../y"`:    "/tmp/y",
		"  /home/p/proj  ": "/home/p/proj",
	}
	if runtime.GOOS == "linux" {
		cases[`C:\Users\paulo\scripts`] = "/mnt/c/Users/paulo/scripts"
		cases[`D:/dados`] = "/mnt/d/dados"
		cases[`C:\`] = "/mnt/c"
		cases[`\\wsl$\Ubuntu\home\paulo\scripts`] = "/home/paulo/scripts"
		cases[`\\wsl.localhost\Ubuntu-22.04\home\paulo`] = "/home/paulo"
	}
	for in, want := range cases {
		got, err := normalizeLocalPath(in)
		if err != nil || got != want {
			t.Errorf("normalizeLocalPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizeLocalPath("scripts/teste.sh"); err == nil {
		t.Error("caminho relativo deveria ser rejeitado")
	}
}

func newLocalFolderTestRouter(t *testing.T) (*gin.Engine, *CodeEditorHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	h := NewCodeEditorHandler(nil, nil, nil, &logger)
	h.reposBase = filepath.Join(t.TempDir(), "repos")
	r := gin.New()
	r.GET("/repos", h.ListRepos)
	r.POST("/open-folder", h.OpenFolder)
	r.DELETE("/repos/:id", h.DeleteRepo)
	r.GET("/repos/:id/tree", h.GetFileTree)
	r.GET("/repos/:id/file", h.ReadFile)
	return r, h
}

func doJSON(t *testing.T, r *gin.Engine, method, url string, body interface{}, out interface{}) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if out != nil {
		_ = json.Unmarshal(w.Body.Bytes(), out)
	}
	return w.Code
}

// Fluxo completo: abrir pasta → aparece na lista → lê arquivo pelo editor → fechar remove só o
// link, nunca os arquivos do usuário.
func TestOpenFolder_ListReadClose(t *testing.T) {
	r, h := newLocalFolderTestRouter(t)
	scripts := filepath.Join(t.TempDir(), "Meus Scripts")
	if err := os.MkdirAll(scripts, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "teste.sh"), []byte("echo oi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var opened RepoInfo
	if code := doJSON(t, r, "POST", "/open-folder", map[string]string{"path": scripts}, &opened); code != 200 {
		t.Fatalf("open-folder status %d", code)
	}
	if opened.ID != "local-meus-scripts" || !opened.IsLocal || opened.IsGit || opened.LocalPath != scripts {
		t.Fatalf("RepoInfo inesperado: %+v", opened)
	}

	// Idempotente: abrir de novo devolve o mesmo item.
	var again RepoInfo
	doJSON(t, r, "POST", "/open-folder", map[string]string{"path": scripts}, &again)
	if again.ID != opened.ID {
		t.Errorf("reabrir a mesma pasta criou outro item: %q", again.ID)
	}

	var repos []RepoInfo
	doJSON(t, r, "GET", "/repos", nil, &repos)
	if len(repos) != 1 || repos[0].ID != opened.ID || !repos[0].IsLocal {
		t.Fatalf("ListRepos = %+v", repos)
	}

	var file struct {
		Content string `json:"content"`
	}
	if code := doJSON(t, r, "GET", "/repos/"+opened.ID+"/file?path=teste.sh", nil, &file); code != 200 || file.Content != "echo oi\n" {
		t.Fatalf("ReadFile status %d content %q", code, file.Content)
	}

	if code := doJSON(t, r, "DELETE", "/repos/"+opened.ID, nil, nil); code != 200 {
		t.Fatalf("DeleteRepo status %d", code)
	}
	if _, err := os.Lstat(filepath.Join(h.reposBase, opened.ID)); !os.IsNotExist(err) {
		t.Error("link deveria ter sido removido")
	}
	if _, err := os.Stat(filepath.Join(scripts, "teste.sh")); err != nil {
		t.Fatalf("fechar a pasta NÃO pode apagar os arquivos do usuário: %v", err)
	}
}

func TestOpenFolder_Rejeicoes(t *testing.T) {
	r, h := newLocalFolderTestRouter(t)
	_ = os.MkdirAll(filepath.Join(h.reposBase, "owner-repo"), 0755)
	for _, p := range []string{"/", "relativo/x", "/nao/existe/mesmo", filepath.Join(h.reposBase, "owner-repo")} {
		if code := doJSON(t, r, "POST", "/open-folder", map[string]string{"path": p}, nil); code != http.StatusBadRequest {
			t.Errorf("open-folder(%q) status %d, want 400", p, code)
		}
	}
}

// Subpasta ilegível não derruba a árvore inteira (antes buildTree abortava no primeiro erro).
func TestGetFileTree_PulaSubpastaIlegivel(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignora permissões")
	}
	r, _ := newLocalFolderTestRouter(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ok.sh"), []byte("x"), 0644)
	locked := filepath.Join(dir, "bloqueada")
	_ = os.MkdirAll(locked, 0755)
	_ = os.Chmod(locked, 0)
	defer os.Chmod(locked, 0755)

	var opened RepoInfo
	doJSON(t, r, "POST", "/open-folder", map[string]string{"path": dir}, &opened)
	var tree []FileNode
	if code := doJSON(t, r, "GET", "/repos/"+opened.ID+"/tree", nil, &tree); code != 200 || len(tree) != 2 {
		t.Fatalf("tree status %d, nós %+v", code, tree)
	}
}
