package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSummarizeReviews(t *testing.T) {
	mk := func(user, state, commit string) ghReview {
		r := ghReview{State: state, CommitID: commit, SubmittedAt: "2026-11-09T10:00:00Z"}
		r.User.Login = user
		return r
	}
	got := summarizeReviews([]ghReview{
		mk("ana", "CHANGES_REQUESTED", "aaa"),
		mk("ana", "APPROVED", "bbb"),  // aprovou depois: vale a aprovação
		mk("ana", "COMMENTED", "bbb"), // comentar depois não desfaz
		mk("bia", "COMMENTED", "bbb"), // só comentou
		mk("caio", "APPROVED", "aaa"), // antiga: commit anterior
		mk("dedé", "PENDING", "bbb"),  // rascunho não conta
		mk("eva", "APPROVED", "bbb"),
		mk("eva", "DISMISSED", "bbb"),
	}, "bbb")
	want := map[string]struct {
		state string
		stale bool
	}{"ana": {"APPROVED", false}, "bia": {"COMMENTED", false}, "caio": {"APPROVED", true}, "eva": {"DISMISSED", false}}
	if len(got) != len(want) {
		t.Fatalf("revisores: %+v", got)
	}
	for _, r := range got {
		w, ok := want[r.User]
		if !ok || r.State != w.state || r.Stale != w.stale {
			t.Errorf("%s: %+v, esperado %+v", r.User, r, w)
		}
	}
	if got[0].User != "ana" || got[len(got)-1].User != "eva" {
		t.Errorf("deveria vir ordenado por usuário: %+v", got)
	}
}

func TestReviewPermissions(t *testing.T) {
	pr := ghPullDetail{State: "open"}
	pr.User.Login = "Autor"
	if c, a, _ := reviewPermissions(pr, "revisor", true); !c || !a {
		t.Error("revisor comum pode comentar e aprovar")
	}
	if c, a, why := reviewPermissions(pr, "autor", true); !c || a || !strings.Contains(why, "autor") {
		t.Errorf("autor só comenta (comparação sem caixa): %v %v %q", c, a, why)
	}
	if c, a, _ := reviewPermissions(pr, "", false); !c || !a {
		t.Error("sem saber quem é o viewer, deixa tentar (o GitHub valida)")
	}
	pr.State = "closed"
	if c, a, why := reviewPermissions(pr, "x", true); c || a || !strings.Contains(why, "fechado") {
		t.Errorf("fechado: %v %v %q", c, a, why)
	}
	pr.Merged = true
	if _, _, why := reviewPermissions(pr, "x", true); !strings.Contains(why, "mergeado") {
		t.Errorf("mergeado: %q", why)
	}
}

// fakeGH simula o GitHub para o PR 7 e registra o que foi enviado em POST.
type fakeGH struct {
	t        *testing.T
	pr       map[string]any
	viewer   string
	reviews  []map[string]any
	postCode int
	postMsg  string
	posted   []map[string]any
}

func (f *fakeGH) handler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		f.t.Errorf("sem o PAT: %s", r.URL.Path)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		json.NewEncoder(w).Encode(map[string]any{"login": f.viewer}) //nolint:errcheck
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
		json.NewEncoder(w).Encode(f.pr) //nolint:errcheck
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7/reviews":
		json.NewEncoder(w).Encode(f.reviews) //nolint:errcheck
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls":
		json.NewEncoder(w).Encode([]map[string]any{{"number": 7, "title": "T", "state": "open", "html_url": "u", "head": map[string]any{"sha": "sha1"}, "base": map[string]any{"ref": "main"}}}) //nolint:errcheck
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/pulls/7/reviews":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = append(f.posted, body)
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			json.NewEncoder(w).Encode(map[string]any{"message": f.postMsg}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 99, "state": "APPROVED", "html_url": "https://github.com/o/r/pull/7#pullrequestreview-99"}) //nolint:errcheck
	default:
		f.t.Errorf("rota inesperada: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}
}

func novoPR(state, author, sha string, merged bool) map[string]any {
	return map[string]any{"number": 7, "title": "T", "state": state, "merged": merged, "html_url": "u",
		"user": map[string]any{"login": author}, "base": map[string]any{"ref": "main"}, "head": map[string]any{"sha": sha}}
}

func montaRevisao(t *testing.T, f *fakeGH) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("GITHUB_TOKEN", "tok")
	base, id := repoTemporario(t)
	f.t = t
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	old := githubAPIBase
	githubAPIBase = srv.URL
	h := &CodeEditorHandler{reposBase: base}
	r := gin.New()
	r.POST("/repos/:id/pr/review", h.SubmitPRReview)
	r.GET("/repos/:id/pr/reviews", h.GetPRReviews)
	t.Cleanup(func() { githubAPIBase = old; srv.Close() })
	_ = id
	return r, func() {}
}

func enviaRevisao(r *gin.Engine, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/repos/"+id+"/pr/review?branch=feat/x", strings.NewReader(body)))
	return w
}

func TestSubmitPRReview(t *testing.T) {
	f := &fakeGH{viewer: "revisor", pr: novoPR("open", "autor", "sha1", false)}
	r, _ := montaRevisao(t, f)
	id := "meu-repo"

	// aprovar o commit que foi visto
	w := enviaRevisao(r, id, `{"pr":7,"event":"approve","head_sha":"sha1"}`)
	if w.Code != 200 || len(f.posted) != 1 {
		t.Fatalf("aprovar: %d %s posted=%d", w.Code, w.Body.String(), len(f.posted))
	}
	p := f.posted[0]
	if p["event"] != "APPROVE" || p["commit_id"] != "sha1" || p["body"] != nil {
		t.Errorf("payload da aprovação: %+v", p)
	}

	// pedir alterações com texto
	f.posted = nil
	if w = enviaRevisao(r, id, `{"pr":7,"event":"REQUEST_CHANGES","head_sha":"sha1","body":"falta o teste"}`); w.Code != 200 || f.posted[0]["body"] != "falta o teste" || f.posted[0]["event"] != "REQUEST_CHANGES" {
		t.Errorf("pedir alterações: %d %s %+v", w.Code, w.Body.String(), f.posted)
	}

	// commit desatualizado: o PR recebeu commits depois que o usuário abriu o diff -> 409, nada enviado
	f.posted = nil
	if w = enviaRevisao(r, id, `{"pr":7,"event":"APPROVE","head_sha":"sha0"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "novos commits") || len(f.posted) != 0 {
		t.Errorf("commit desatualizado: %d %s posted=%d", w.Code, w.Body.String(), len(f.posted))
	}

	// validações (400), sem tocar no GitHub
	for name, body := range map[string]string{
		"evento inválido":      `{"pr":7,"event":"MERGE","head_sha":"sha1"}`,
		"sem head_sha":         `{"pr":7,"event":"APPROVE"}`,
		"sem pr":               `{"event":"APPROVE","head_sha":"sha1"}`,
		"alterações sem texto": `{"pr":7,"event":"REQUEST_CHANGES","head_sha":"sha1"}`,
		"comentário sem texto": `{"pr":7,"event":"COMMENT","head_sha":"sha1","body":"  "}`,
		"comentário gigante":   `{"pr":7,"event":"COMMENT","head_sha":"sha1","body":"` + strings.Repeat("a", reviewBodyMax+1) + `"}`,
	} {
		if w = enviaRevisao(r, id, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d %s", name, w.Code, w.Body.String())
		}
	}
	if len(f.posted) != 0 {
		t.Errorf("validação não pode enviar nada: %+v", f.posted)
	}
}

func TestSubmitPRReviewPRProprioEFechado(t *testing.T) {
	f := &fakeGH{viewer: "Autor", pr: novoPR("open", "autor", "sha1", false)}
	r, _ := montaRevisao(t, f)
	id := "meu-repo"

	// o autor não aprova o próprio PR (422, sem enviar), mas pode comentar
	if w := enviaRevisao(r, id, `{"pr":7,"event":"APPROVE","head_sha":"sha1"}`); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "autor") || len(f.posted) != 0 {
		t.Errorf("aprovar o próprio PR: %d %s posted=%d", w.Code, w.Body.String(), len(f.posted))
	}
	if w := enviaRevisao(r, id, `{"pr":7,"event":"COMMENT","head_sha":"sha1","body":"nota"}`); w.Code != 200 || len(f.posted) != 1 {
		t.Errorf("comentar o próprio PR: %d %s", w.Code, w.Body.String())
	}

	// PR fechado/mergeado não recebe revisão
	f.viewer, f.posted = "revisor", nil
	f.pr = novoPR("closed", "autor", "sha1", true)
	if w := enviaRevisao(r, id, `{"pr":7,"event":"APPROVE","head_sha":"sha1"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "mergeado") || len(f.posted) != 0 {
		t.Errorf("PR mergeado: %d %s", w.Code, w.Body.String())
	}
}

func TestSubmitPRReviewErroDePermissaoDoPAT(t *testing.T) {
	f := &fakeGH{viewer: "revisor", pr: novoPR("open", "autor", "sha1", false), postCode: 403, postMsg: "Resource not accessible by personal access token"}
	r, _ := montaRevisao(t, f)
	w := enviaRevisao(r, "meu-repo", `{"pr":7,"event":"APPROVE","head_sha":"sha1"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Pull requests: Read and write") {
		t.Errorf("erro de permissão deveria orientar o usuário: %d %s", w.Code, w.Body.String())
	}
}

func TestGetPRReviews(t *testing.T) {
	f := &fakeGH{viewer: "autor", pr: novoPR("open", "autor", "sha1", false), reviews: []map[string]any{
		{"state": "APPROVED", "commit_id": "sha1", "submitted_at": "2026-11-09T10:00:00Z", "user": map[string]any{"login": "ana"}},
		{"state": "CHANGES_REQUESTED", "commit_id": "sha0", "submitted_at": "2026-11-08T10:00:00Z", "user": map[string]any{"login": "bia"}},
	}}
	r, _ := montaRevisao(t, f)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/repos/meu-repo/pr/reviews?branch=feat/x", nil))
	var got struct {
		PR struct {
			HeadSHA string `json:"head_sha"`
			Author  string
		} `json:"pr"`
		Viewer  string          `json:"viewer"`
		Reviews []ReviewSummary `json:"reviews"`
		CanApp  bool            `json:"can_approve"`
		CanCom  bool            `json:"can_comment"`
		Reason  string          `json:"approve_block_reason"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got.PR.HeadSHA != "sha1" || got.Viewer != "autor" || got.CanApp || !got.CanCom || got.Reason == "" {
		t.Errorf("permissões do autor: %+v", got)
	}
	if len(got.Reviews) != 2 || got.Reviews[0].User != "ana" || got.Reviews[0].Stale || !got.Reviews[1].Stale || got.Reviews[1].State != "CHANGES_REQUESTED" {
		t.Errorf("revisões: %+v", got.Reviews)
	}
}
