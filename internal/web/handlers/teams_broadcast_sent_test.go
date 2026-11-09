package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/storage"
)

func TestSentPreview(t *testing.T) {
	cases := map[string]string{
		"# **Aviso** importante\n\ncorpo": "Aviso importante",
		"\n\n  > Manutenção hoje  ":       "Manutenção hoje",
		"":                                "(sem texto)",
		strings.Repeat("a", 100):          strings.Repeat("a", 80) + "…",
	}
	for in, want := range cases {
		if got := sentPreview(in); got != want {
			t.Errorf("sentPreview(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestListGetForgetSent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewTeamsSentStore(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lg := zerolog.Nop()
	h := &TeamsBroadcastHandler{logger: &lg}
	h.SetSentStore(st)
	_ = st.SaveBatch(storage.TeamsSentBatch{ID: "b1", SentAt: time.Now(), Preview: "Aviso", Total: 1, OK: 1},
		[]storage.TeamsSentMessage{{ThreadID: "t1", DisplayName: "Ana", MessageID: "m1"}})
	h.markSent(storage.TeamsSentStateDeleted, [][2]string{{"t1", "m1"}})

	r := gin.New()
	r.GET("/sent", h.ListSent)
	r.GET("/sent/:batchId", h.GetSent)
	r.DELETE("/sent/:batchId", h.ForgetSent)
	do := func(method, path string) (int, string) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code, w.Body.String()
	}
	if code, body := do(http.MethodGet, "/sent"); code != 200 || !strings.Contains(body, `"deleted":1`) || !strings.Contains(body, `"active":0`) {
		t.Errorf("list: %d %s", code, body)
	}
	if code, body := do(http.MethodGet, "/sent/b1"); code != 200 || !strings.Contains(body, `"state":"deleted"`) || !strings.Contains(body, "Ana") {
		t.Errorf("get: %d %s", code, body)
	}
	if code, _ := do(http.MethodDelete, "/sent/b1"); code != 200 {
		t.Errorf("forget: %d", code)
	}
	if _, body := do(http.MethodGet, "/sent"); !strings.Contains(body, `"batches":[]`) {
		t.Errorf("lista deveria estar vazia: %s", body)
	}
}
