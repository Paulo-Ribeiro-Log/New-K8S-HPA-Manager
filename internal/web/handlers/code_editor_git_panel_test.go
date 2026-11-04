package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func entries(msgs ...string) []commitLogEntry {
	var out []commitLogEntry
	for i, m := range msgs {
		out = append(out, commitLogEntry{Hash: "abc" + string(rune('0'+i)) + "def0", Message: m, Author: "Ana", When: "1 day ago"})
	}
	return out
}

func TestFilterLogEntries(t *testing.T) {
	es := entries("fix: subnet", "feat: nova aba", "Merge pull request #28", "refactor")
	if got := filterLogEntries(es, "", 2); len(got) != 2 || got[0].Message != "fix: subnet" {
		t.Errorf("sem busca só aplica o limite: %+v", got)
	}
	if got := filterLogEntries(es, "MERGE", 10); len(got) != 1 || got[0].Message != "Merge pull request #28" {
		t.Errorf("mensagem sem diferenciar maiúsculas: %+v", got)
	}
	if got := filterLogEntries(es, "abc1", 10); len(got) != 1 || got[0].Message != "feat: nova aba" {
		t.Errorf("prefixo do hash: %+v", got)
	}
	if got := filterLogEntries(es, "ana", 10); len(got) != 4 {
		t.Errorf("autor: %+v", got)
	}
	if got := filterLogEntries(es, "def0", 10); len(got) != 0 {
		t.Errorf("hash só casa por prefixo, não por trecho: %+v", got)
	}
	if got := filterLogEntries(es, "inexistente", 10); len(got) != 0 {
		t.Errorf("sem resultado: %+v", got)
	}
}

func TestParseLogLimit(t *testing.T) {
	for in, want := range map[string]int{"": 20, "abc": 20, "0": 20, "-3": 20, "5": 5, "50": 50, "9999": logMaxLimit} {
		if got := parseLogLimit(in); got != want {
			t.Errorf("parseLogLimit(%q) = %d, quer %d", in, got, want)
		}
	}
}

func TestParseGitRemotes(t *testing.T) {
	out := "origin\thttps://user:ghp_SECRET@github.com/o/r.git (fetch)\n" +
		"origin\thttps://user:ghp_SECRET@github.com/o/r.git (push)\n" +
		"upstream\tgit@github.com:o/outro.git (fetch)\n" +
		"upstream\tgit@github.com:o/outro.git (push)"
	got := parseGitRemotes(out)
	if len(got) != 2 {
		t.Fatalf("um item por remote: %+v", got)
	}
	if got[0].Name != "origin" || got[0].URL != "https://github.com/o/r.git" {
		t.Errorf("origin deve sair sem credenciais: %+v", got[0])
	}
	if strings.Contains(got[0].URL, "SECRET") {
		t.Error("token vazou na URL do remote")
	}
	if got[1].Name != "upstream" || got[1].URL != "git@github.com:o/outro.git" {
		t.Errorf("upstream: %+v", got[1])
	}
	if len(parseGitRemotes("")) != 0 {
		t.Error("sem remotes, lista vazia")
	}
}

// Fluxo completo com um repositório git de verdade: assunto com "|", busca e remote com credencial.
func TestGitPanelEndpoints_RepoReal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "meu-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@x.io"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	for i, msg := range []string{"fix: a | b", "feat: login", "chore: deps"} {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", msg)
	}
	git("remote", "add", "origin", "https://user:ghp_SECRET@github.com/o/r.git")

	h := &CodeEditorHandler{reposBase: base}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/repos/:id/log", h.GetCommitLog)
	r.GET("/repos/:id/remotes", h.Remotes)
	get := func(path string, out any) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	var log []commitLogEntry
	get("/repos/meu-repo/log?limit=2", &log)
	if len(log) != 2 || log[0].Message != "chore: deps" {
		t.Errorf("limite: %+v", log)
	}
	get("/repos/meu-repo/log?limit=10&q=login", &log)
	if len(log) != 1 || log[0].Message != "feat: login" || log[0].Author != "Ana" {
		t.Errorf("busca por mensagem: %+v", log)
	}
	// o "|" do assunto não pode desalinhar autor/quando
	get("/repos/meu-repo/log?limit=10&q="+url.QueryEscape("a | b"), &log)
	if len(log) != 1 || log[0].Message != "fix: a | b" || log[0].Author != "Ana" || log[0].When == "" {
		t.Errorf("assunto com |: %+v", log)
	}
	hash := log[0].Hash
	get("/repos/meu-repo/log?q="+hash[:7], &log)
	if len(log) != 1 || log[0].Hash != hash {
		t.Errorf("busca por prefixo do hash: %+v", log)
	}

	var remotes []gitRemote
	get("/repos/meu-repo/remotes", &remotes)
	if len(remotes) != 1 || remotes[0].URL != "https://github.com/o/r.git" || strings.Contains(remotes[0].URL, "SECRET") {
		t.Errorf("remotes: %+v", remotes)
	}
}
