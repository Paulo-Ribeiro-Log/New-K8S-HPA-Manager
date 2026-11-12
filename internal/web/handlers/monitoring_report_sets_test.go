package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

func setsRouter(t *testing.T, email string, store *storage.LocalSettingsStore) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewMonitoringSetsHandler(store)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_email", email) })
	r.GET("/sets", h.Get)
	r.PUT("/sets", h.Put)
	return r
}

func do(r http.Handler, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/sets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func newSetsStore(t *testing.T, dir string) *storage.LocalSettingsStore {
	t.Helper()
	cli, err := storage.NewSQLiteClient(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	st := storage.NewLocalSettingsStore(cli)
	if err := st.CreateTable(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestConjuntosSalvosSobrevivemAoReinicio(t *testing.T) {
	dir := t.TempDir()
	st := newSetsStore(t, dir)
	body := `{"sets":[{"name":"stress","targets":[{"id":"a"}]}]}`
	if w := do(setsRouter(t, "a@x.com", st), http.MethodPut, body); w.Code != 200 {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	// "reinício": novo cliente SQLite sobre o mesmo arquivo
	st2 := newSetsStore(t, dir)
	w := do(setsRouter(t, "a@x.com", st2), http.MethodGet, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"stress"`) {
		t.Fatalf("GET após reinício = %d %s", w.Code, w.Body)
	}
	// outro usuário não vê
	if w := do(setsRouter(t, "b@x.com", st2), http.MethodGet, ""); strings.Contains(w.Body.String(), "stress") {
		t.Fatalf("vazou entre usuários: %s", w.Body)
	}
}

func TestConjuntosSalvosVazioEInvalido(t *testing.T) {
	st := newSetsStore(t, t.TempDir())
	r := setsRouter(t, "a@x.com", st)
	if w := do(r, http.MethodGet, ""); w.Body.String() != `{"sets":[]}` {
		t.Fatalf("sem nada gravado: %s", w.Body)
	}
	if w := do(r, http.MethodPut, `{"x":1}`); w.Code != 400 {
		t.Fatalf("sem sets = %d", w.Code)
	}
	if w := do(r, http.MethodPut, `{"sets":[]}`); w.Code != 200 {
		t.Fatalf("lista vazia (apagar tudo) = %d", w.Code)
	}
	if w := do(setsRouter(t, "a@x.com", nil), http.MethodGet, ""); w.Code != 503 {
		t.Fatalf("sem store = %d", w.Code)
	}
}
