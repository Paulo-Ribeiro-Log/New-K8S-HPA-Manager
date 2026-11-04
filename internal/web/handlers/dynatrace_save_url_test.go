package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func postDynatraceCfg(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/cfg", func(c *gin.Context) {
		c.Set("user_email", "ana@empresa.com")
		h(c)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cfg", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// A URL inválida é recusada antes de qualquer acesso ao store (por isso o handler pode ter
// tokensStore nil no teste): um nome solto nunca chega a ser gravado.
func TestDynatraceSaveConfig_RecusaURLSemEndereco(t *testing.T) {
	h := &DynatraceHandler{}
	w := postDynatraceCfg(h.SaveConfig, `{"dynatrace_url":"casas-bahia","dynatrace_token":"x"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "casas-bahia") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDynatraceSaveConfig_RecusaURLDoTenantHLGInvalida(t *testing.T) {
	h := &DynatraceHandler{}
	w := postDynatraceCfg(h.SaveConfig, `{"dynatrace_hlg_url":"hlg","dynatrace_hlg_token":"x"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "homologação") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAITokensSaveTokens_RecusaURLDynatraceSemEndereco(t *testing.T) {
	h := &AITokensHandler{}
	w := postDynatraceCfg(h.SaveTokens, `{"ai_email":"ana@empresa.com","dynatrace_url":"casas-bahia"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "não é um endereço") {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}
