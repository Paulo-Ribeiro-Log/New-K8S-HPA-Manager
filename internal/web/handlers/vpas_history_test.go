package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
)

// Excluir e aplicar VPA precisam deixar rastro no Change History (mesmo quando falham);
// dry-run não altera nada e não deve gravar.
func TestVPAHandlerRegistraNoHistorico(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ht, err := history.NewHistoryTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryTracker: %v", err)
	}
	t.Cleanup(func() { ht.Close() })

	h := NewVPAHandler(nil, ht)
	r := gin.New()
	r.DELETE("/vpas/:cluster/:namespace/:name", h.Delete)
	r.PUT("/vpas/:cluster/:namespace/:name", h.Apply)

	do := func(method, body string) {
		req := httptest.NewRequest(method, "/vpas/cluster-inexistente-teste/ns/meu-vpa", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	do(http.MethodDelete, "")
	do(http.MethodPut, `{"yaml":"kind: VerticalPodAutoscaler","dryRun":true}`)
	do(http.MethodPut, `{"yaml":"kind: VerticalPodAutoscaler","dryRun":false}`)

	entries, _, err := ht.Query(history.HistoryFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	got := map[string]int{}
	for _, e := range entries {
		got[e.Action]++
		if e.Resource != "ns/meu-vpa" {
			t.Errorf("resource = %q, quer ns/meu-vpa", e.Resource)
		}
	}
	if got["delete_vpa"] != 1 || got["apply_vpa"] != 1 || len(entries) != 2 {
		t.Fatalf("registros = %v; quer 1 delete_vpa e 1 apply_vpa (sem o dry-run)", got)
	}
}
