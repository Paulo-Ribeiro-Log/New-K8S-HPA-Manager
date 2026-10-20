package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func profilingRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerProfilingRoutes(r.Group("/api/v1"))
	return r
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestProfilingDesligadoPorPadrao(t *testing.T) {
	t.Setenv("K8S_HPA_PPROF", "")
	r := profilingRouter()
	if w := get(r, "/api/v1/debug/runtime"); w.Code != http.StatusNotFound {
		t.Errorf("sem a variável não deve haver rota: %d", w.Code)
	}
	if w := get(r, "/api/v1/debug/pprof/heap"); w.Code != http.StatusNotFound {
		t.Errorf("sem a variável não deve haver rota: %d", w.Code)
	}
}

func TestProfilingLigadoPorVariavel(t *testing.T) {
	t.Setenv("K8S_HPA_PPROF", "1")
	r := profilingRouter()
	w := get(r, "/api/v1/debug/runtime")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"goroutines"`) {
		t.Errorf("runtime: %d %s", w.Code, w.Body.String())
	}
	if w := get(r, "/api/v1/debug/pprof/heap"); w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Errorf("heap: %d, %d bytes", w.Code, w.Body.Len())
	}
	if w := get(r, "/api/v1/debug/pprof/goroutine?debug=1"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "goroutine profile") {
		t.Errorf("goroutine: %d", w.Code)
	}
	if w := get(r, "/api/v1/debug/pprof/nao-existe"); w.Code != http.StatusNotFound {
		t.Errorf("perfil inexistente deveria dar 404: %d", w.Code)
	}
}
