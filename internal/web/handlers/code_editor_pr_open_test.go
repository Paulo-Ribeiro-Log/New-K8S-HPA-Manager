package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/gin-gonic/gin"
)

func execGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

func openPRJSON(n int, head, headRepo, base string, draft bool) map[string]any {
	h := map[string]any{"ref": head}
	if headRepo != "" {
		h["repo"] = map[string]any{"full_name": headRepo}
	} else {
		h["repo"] = nil // repositório de origem apagado
	}
	return map[string]any{
		"number": n, "title": fmt.Sprintf("PR %d", n), "html_url": fmt.Sprintf("https://github.com/o/r/pull/%d", n),
		"draft": draft, "updated_at": "2026-11-09T10:00:00Z",
		"user": map[string]any{"login": "dev"}, "base": map[string]any{"ref": base}, "head": h,
	}
}

func TestListOpenPRsFiltraForkEPagina(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/o/r/pulls" || r.URL.Query().Get("state") != "open" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("requisição inesperada: %s?%s auth=%q", r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"))
		}
		var batch []map[string]any
		if r.URL.Query().Get("page") == "1" {
			for i := 1; i <= 98; i++ { // 98 do próprio repo
				batch = append(batch, openPRJSON(i, fmt.Sprintf("feat/b%d", i), "O/R", "main", false)) // full_name com outra caixa
			}
			batch = append(batch, openPRJSON(500, "main", "fulano/r", "main", false)) // fork: colide com "main"
			batch = append(batch, openPRJSON(501, "feat/orfa", "", "main", false))    // origem apagada
		} else {
			batch = append(batch, openPRJSON(600, "feat/rascunho", "o/r", "develop", true))
		}
		json.NewEncoder(w).Encode(batch) //nolint:errcheck
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()

	prs, err := listOpenPRs(context.Background(), "tok", "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("esperava 2 páginas (a 1ª veio com 100), veio %d chamadas", calls)
	}
	if len(prs) != 99 {
		t.Fatalf("esperava 99 PRs (98 + 1 rascunho; fork e origem apagada ficam de fora), veio %d", len(prs))
	}
	last := prs[len(prs)-1]
	if last.Branch != "feat/rascunho" || !last.Draft || last.Base != "develop" || last.Number != 600 || last.Author != "dev" {
		t.Errorf("último PR: %+v", last)
	}
	for _, p := range prs {
		if p.Branch == "main" || p.Branch == "feat/orfa" {
			t.Errorf("PR de fork/origem apagada não deveria entrar: %+v", p)
		}
	}
}

func TestValidPRBranch(t *testing.T) {
	for _, ok := range []string{"feat/x", "fix/volumes-disco_1.2", "main", "release/v1.3.49", "a"} {
		if !validPRBranch(ok) {
			t.Errorf("%q deveria valer", ok)
		}
	}
	for _, bad := range []string{"", "-rf", "../x", "a..b", "feat/", "x.lock", "a b", "a;b", "a$(b)", "feat/é", "a\nb"} {
		if validPRBranch(bad) {
			t.Errorf("%q não deveria valer", bad)
		}
	}
	if len("x") > 0 && validPRBranch(string(make([]byte, 201))) {
		t.Error("nome longo demais não deveria valer")
	}
}

// repoTemporario cria um repositório git com remote origin apontando para github.com/o/r.
func repoTemporario(t *testing.T) (base, id string) {
	t.Helper()
	base = t.TempDir()
	id = "meu-repo"
	dir := base + "/" + id
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "remote", "add", "origin", "https://github.com/o/r.git"}} {
		if out, err := execGit(args...); err != nil {
			t.Skipf("git indisponível: %v %s", err, out)
		}
	}
	return base, id
}

func TestRotasDePRAceitamBranchPedidaESemCheckout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GITHUB_TOKEN", "tok")
	base, id := repoTemporario(t)
	h := &CodeEditorHandler{reposBase: base}

	do := func(path string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		return c, w
	}

	// branch pedida: vale mesmo sem ser a branch atual (o repositório nem tem commits)
	c, _ := do("/x?branch=feat/outra")
	tg, ok := h.resolvePlanTarget(c)
	if !ok || tg.branch != "feat/outra" || tg.owner != "o" || tg.repo != "r" || tg.token != "tok" {
		t.Fatalf("alvo com ?branch: ok=%v %+v", ok, tg)
	}

	// nome inválido: 400, sem tocar no GitHub
	for _, bad := range []string{"..%2Fx", "a%20b", "x.lock"} {
		c, w := do("/x?branch=" + bad)
		if _, ok := h.resolvePlanTarget(c); ok || w.Code != http.StatusBadRequest {
			t.Errorf("?branch=%s deveria dar 400, deu ok=%v status=%d", bad, ok, w.Code)
		}
	}

	// ListOpenPRs de ponta a ponta contra um GitHub falso
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{openPRJSON(7, "feat/x", "o/r", "main", false)}) //nolint:errcheck
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()
	c, w := do("/x")
	h.ListOpenPRs(c)
	var got struct {
		PRs []OpenPR `json:"prs"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.PRs) != 1 || got.PRs[0].Branch != "feat/x" || got.PRs[0].Number != 7 {
		t.Errorf("ListOpenPRs: %d %s", w.Code, w.Body.String())
	}
}

func TestEnrichOpenPRsPreencheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Errorf("requisição inesperada: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"repository":{"pullRequests":{"nodes":[
		 {"number":1,"mergeable":"MERGEABLE","reviewDecision":"APPROVED","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},
		 {"number":2,"mergeable":"CONFLICTING","reviewDecision":"CHANGES_REQUESTED","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},
		 {"number":3,"mergeable":"UNKNOWN","reviewDecision":null,"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}]}}}}`)
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()

	prs := []OpenPR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 9}}
	enrichOpenPRs(context.Background(), "tok", "o", "r", prs)
	want := []OpenPR{
		{Number: 1, Checks: "success", Review: "approved", Mergeable: "ok"},
		{Number: 2, Checks: "failure", Review: "changes_requested", Mergeable: "conflict"},
		{Number: 3, Checks: "none", Review: "none", Mergeable: "unknown"},
		{Number: 9}, // fora da resposta: fica sem status
	}
	for i := range want {
		if prs[i] != want[i] {
			t.Errorf("PR %d: %+v, esperava %+v", want[i].Number, prs[i], want[i])
		}
	}
}

func TestEnrichOpenPRsFalhaNaoQuebra(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()
	prs := []OpenPR{{Number: 1}}
	enrichOpenPRs(context.Background(), "tok", "o", "r", prs)
	if prs[0].Checks != "" || prs[0].Review != "" {
		t.Errorf("sem acesso ao GraphQL o PR deveria ficar sem status: %+v", prs[0])
	}
}
