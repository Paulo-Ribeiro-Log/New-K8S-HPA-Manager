package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMergeBlockReason(t *testing.T) {
	cases := []struct {
		name string
		st   PRMergeStatus
		want string // "" = pode; senão um trecho do motivo
	}{
		{"limpo", PRMergeStatus{State: "open", MergeableState: "clean"}, ""},
		{"com hooks", PRMergeStatus{State: "open", MergeableState: "has_hooks"}, ""},
		{"atrasado", PRMergeStatus{State: "open", MergeableState: "behind"}, ""},
		{"conflito", PRMergeStatus{State: "open", MergeableState: "dirty"}, "conflito"},
		{"checks falhando", PRMergeStatus{State: "open", MergeableState: "unstable"}, "checks falhando"},
		{"bloqueado", PRMergeStatus{State: "open", MergeableState: "blocked"}, "bloqueia"},
		{"rascunho", PRMergeStatus{State: "open", Draft: true, MergeableState: "clean"}, "rascunho"},
		{"calculando", PRMergeStatus{State: "open", MergeableState: "unknown"}, "calculando"},
		{"fechado", PRMergeStatus{State: "closed", MergeableState: "clean"}, "fechado"},
		{"mergeado", PRMergeStatus{State: "merged", MergeableState: "clean"}, "já foi mergeado"},
	}
	for _, c := range cases {
		got := mergeBlockReason(c.st)
		if (c.want == "") != (got == "") || (c.want != "" && !strings.Contains(got, c.want)) {
			t.Errorf("%s: motivo %q, esperava %q", c.name, got, c.want)
		}
	}
}

type fakeMergeGH struct {
	t       *testing.T
	pr      map[string]any
	puts    []map[string]any
	deletes []string
	putCode int
	putMsg  string
	delCode int
}

func (f *fakeMergeGH) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
		json.NewEncoder(w).Encode(f.pr) //nolint:errcheck
	case r.Method == http.MethodPut && r.URL.Path == "/repos/o/r/pulls/7/merge":
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.puts = append(f.puts, b)
		if f.putCode != 0 {
			w.WriteHeader(f.putCode)
			json.NewEncoder(w).Encode(map[string]any{"message": f.putMsg}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"sha": "merge123", "merged": true, "message": "ok"}) //nolint:errcheck
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/o/r/git/refs/heads/"):
		f.deletes = append(f.deletes, strings.TrimPrefix(r.URL.Path, "/repos/o/r/git/refs/heads/"))
		if f.delCode != 0 {
			w.WriteHeader(f.delCode)
			json.NewEncoder(w).Encode(map[string]any{"message": "Reference does not exist"}) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("rota inesperada: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}
}

func prParaMerge(mergeableState string) map[string]any {
	return map[string]any{
		"number": 7, "title": "T", "state": "open", "merged": false, "draft": false, "html_url": "u",
		"mergeable": true, "mergeable_state": mergeableState,
		"user": map[string]any{"login": "autor"}, "base": map[string]any{"ref": "main"},
		"head": map[string]any{"sha": "sha1", "ref": "feat/x", "repo": map[string]any{"full_name": "o/r"}},
	}
}

func montaMerge(t *testing.T, f *fakeMergeGH) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("GITHUB_TOKEN", "tok")
	base, _ := repoTemporario(t)
	f.t = t
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	old := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = old; srv.Close() })
	h := &CodeEditorHandler{reposBase: base}
	r := gin.New()
	r.POST("/repos/:id/pr/merge", h.MergePR)
	return r
}

func enviaMerge(r *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/repos/meu-repo/pr/merge", strings.NewReader(body)))
	return w
}

func TestMergePR(t *testing.T) {
	f := &fakeMergeGH{pr: prParaMerge("clean")}
	r := montaMerge(t, f)

	// merge simples (padrão: merge commit), sem apagar a branch
	w := enviaMerge(r, `{"pr":7,"head_sha":"sha1"}`)
	if w.Code != 200 || len(f.puts) != 1 || f.puts[0]["merge_method"] != "merge" || f.puts[0]["sha"] != "sha1" || len(f.deletes) != 0 {
		t.Fatalf("merge: %d %s puts=%+v deletes=%v", w.Code, w.Body.String(), f.puts, f.deletes)
	}

	// squash e apagar a branch depois
	f.puts = nil
	w = enviaMerge(r, `{"pr":7,"head_sha":"SHA1","method":"squash","delete_branch":true}`)
	var out struct {
		BranchDeleted bool `json:"branch_deleted"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || f.puts[0]["merge_method"] != "squash" || len(f.deletes) != 1 || f.deletes[0] != "feat/x" || !out.BranchDeleted {
		t.Errorf("squash+apagar: %d %s puts=%+v deletes=%v", w.Code, w.Body.String(), f.puts, f.deletes)
	}

	// falha ao apagar a branch não desfaz o merge
	f.deletes, f.delCode = nil, 422
	if w = enviaMerge(r, `{"pr":7,"head_sha":"sha1","delete_branch":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), "branch_error") {
		t.Errorf("apagar falhou: %d %s", w.Code, w.Body.String())
	}
}

func TestMergePRRecusas(t *testing.T) {
	f := &fakeMergeGH{pr: prParaMerge("dirty")}
	r := montaMerge(t, f)

	// conflito: 409, nada é enviado
	if w := enviaMerge(r, `{"pr":7,"head_sha":"sha1"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "conflito") || len(f.puts) != 0 {
		t.Errorf("conflito: %d %s puts=%d", w.Code, w.Body.String(), len(f.puts))
	}
	// commit novo desde o diff: 409
	f.pr = prParaMerge("clean")
	if w := enviaMerge(r, `{"pr":7,"head_sha":"antigo"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "novos commits") || len(f.puts) != 0 {
		t.Errorf("sha desatualizado: %d %s puts=%d", w.Code, w.Body.String(), len(f.puts))
	}
	// branch de fork nunca é apagada
	f.pr = prParaMerge("clean")
	f.pr["head"] = map[string]any{"sha": "sha1", "ref": "feat/x", "repo": map[string]any{"full_name": "fulano/r"}}
	w := enviaMerge(r, `{"pr":7,"head_sha":"sha1","delete_branch":true}`)
	if w.Code != 200 || len(f.deletes) != 0 || !strings.Contains(w.Body.String(), "branch_error") {
		t.Errorf("fork: %d %s deletes=%v", w.Code, w.Body.String(), f.deletes)
	}
	// erro do GitHub no merge (regra de proteção) volta com o status dele
	f.pr = prParaMerge("clean")
	f.putCode, f.putMsg = 405, "Required status check is expected"
	if w := enviaMerge(r, `{"pr":7,"head_sha":"sha1"}`); w.Code != 405 || !strings.Contains(w.Body.String(), "Required status check") {
		t.Errorf("405 do GitHub: %d %s", w.Code, w.Body.String())
	}
	// validações
	for _, body := range []string{`{"head_sha":"sha1"}`, `{"pr":7}`, `{"pr":7,"head_sha":"sha1","method":"fast-forward"}`} {
		if w := enviaMerge(r, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d", body, w.Code)
		}
	}
}
