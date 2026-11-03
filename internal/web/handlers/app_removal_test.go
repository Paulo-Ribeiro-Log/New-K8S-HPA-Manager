package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/appremoval"
)

func execRequest(body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	h := NewAppRemovalHandler(nil, nil, nil)
	r := gin.New()
	r.POST("/app-removal/execute", h.Execute)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app-removal/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// A confirmação por nome é checada antes de qualquer acesso ao cluster.
func TestAppRemovalExecuteExigeConfirmacaoPeloNome(t *testing.T) {
	w := execRequest(`{"cluster":"c","namespace":"ns","name":"frete","confirm_name":"outro","items":[{"kind":"Service","name":"frete"}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CONFIRMATION_MISMATCH") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAppRemovalExecuteRejeitaCorpoInvalido(t *testing.T) {
	w := execRequest(`{"namespace":"ns"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func newAppRemovalRouter(h *AppRemovalHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app-removal/execute", h.Execute)
	r.POST("/app-removal/verify", h.Verify)
	return r
}

func post(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAppRemovalExecuteScanIDDesconhecido(t *testing.T) {
	r := newAppRemovalRouter(NewAppRemovalHandler(nil, nil, nil))
	w := post(r, "/app-removal/execute", `{"cluster":"c","namespace":"ns","name":"frete","dry_run":true,"scan_id":"nao-existe","items":[{"kind":"Service","name":"frete"}]}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "SCAN_EXPIRED") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAppRemovalExecuteScanDeOutraAplicacao(t *testing.T) {
	h := NewAppRemovalHandler(nil, nil, nil)
	id := h.store.Put("c", &appremoval.Result{Namespace: "ns", Name: "outra"})
	w := post(newAppRemovalRouter(h), "/app-removal/execute",
		`{"cluster":"c","namespace":"ns","name":"frete","dry_run":true,"scan_id":"`+id+`","items":[{"kind":"Service","name":"frete"}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "SCAN_MISMATCH") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAppRemovalExecuteSelecaoForaDoScan(t *testing.T) {
	h := NewAppRemovalHandler(nil, nil, nil)
	id := h.store.Put("c", &appremoval.Result{Namespace: "ns", Name: "frete",
		Items: []appremoval.Item{{Kind: "Service", Name: "frete"}}})
	w := post(newAppRemovalRouter(h), "/app-removal/execute",
		`{"cluster":"c","namespace":"ns","name":"frete","dry_run":true,"scan_id":"`+id+`","items":[{"kind":"Secret","name":"outro"}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_SELECTION") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAppRemovalVerifyScanIDDesconhecido(t *testing.T) {
	r := newAppRemovalRouter(NewAppRemovalHandler(nil, nil, nil))
	if w := post(r, "/app-removal/verify", `{"scan_id":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w := post(r, "/app-removal/verify", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("status=%d", w.Code)
	}
}
