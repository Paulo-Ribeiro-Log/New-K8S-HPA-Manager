package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
