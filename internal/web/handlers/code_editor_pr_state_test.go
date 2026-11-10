package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestListClosedPRsMarcaMergeado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "closed" {
			t.Errorf("esperava state=closed: %s", r.URL.RawQuery)
		}
		a := openPRJSON(1, "feat/fechado", "o/r", "main", false)
		b := openPRJSON(2, "feat/mergeado", "o/r", "main", false)
		b["merged_at"] = "2026-11-09T10:00:00Z"
		c := openPRJSON(3, "main", "fulano/r", "main", false) // fork fica de fora
		json.NewEncoder(w).Encode([]map[string]any{a, b, c})  //nolint:errcheck
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()

	prs, err := listClosedPRs(context.Background(), "tok", "o", "r")
	if err != nil || len(prs) != 2 {
		t.Fatalf("prs=%+v err=%v", prs, err)
	}
	if prs[0].State != "closed" || prs[1].State != "merged" {
		t.Errorf("states: %+v", prs)
	}
}

func TestSetPRState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GITHUB_TOKEN", "tok")
	cur := map[string]any{"number": 7, "state": "open", "merged": false}
	var patched []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
			json.NewEncoder(w).Encode(cur) //nolint:errcheck
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/pulls/7":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			patched = append(patched, body)
			json.NewEncoder(w).Encode(map[string]any{"state": body["state"]}) //nolint:errcheck
		default:
			t.Errorf("rota inesperada: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()
	base, _ := repoTemporario(t)
	h := &CodeEditorHandler{reposBase: base}
	r := gin.New()
	r.POST("/repos/:id/pr/state", h.SetPRState)
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/repos/meu-repo/pr/state", strings.NewReader(body)))
		return w
	}

	// fechar um PR aberto
	if w := send(`{"pr":7,"state":"closed"}`); w.Code != 200 || len(patched) != 1 || patched[0]["state"] != "closed" {
		t.Fatalf("fechar: %d %s patched=%+v", w.Code, w.Body.String(), patched)
	}
	// já está como pedido: nada é enviado
	patched = nil
	if w := send(`{"pr":7,"state":"open"}`); w.Code != 200 || len(patched) != 0 {
		t.Errorf("já aberto: %d patched=%+v", w.Code, patched)
	}
	// reabrir um fechado
	cur["state"] = "closed"
	if w := send(`{"pr":7,"state":"open"}`); w.Code != 200 || len(patched) != 1 || patched[0]["state"] != "open" {
		t.Errorf("reabrir: %d %s patched=%+v", w.Code, w.Body.String(), patched)
	}
	// mergeado não reabre nem fecha
	patched = nil
	cur["merged"] = true
	if w := send(`{"pr":7,"state":"open"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "mergeado") || len(patched) != 0 {
		t.Errorf("mergeado: %d %s patched=%+v", w.Code, w.Body.String(), patched)
	}
	// validações
	for _, body := range []string{`{"pr":7,"state":"merged"}`, `{"state":"closed"}`, `{"pr":7}`} {
		if w := send(body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d", body, w.Code)
		}
	}
}
