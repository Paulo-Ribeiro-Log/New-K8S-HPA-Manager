package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/displayenv"
)

func TestSystemDisplayCacheEForcaAtualizacao(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	h := &SystemDisplayHandler{detect: func(context.Context) displayenv.Report {
		calls++
		return displayenv.Report{OS: "linux", IsWSL: true, Mode: displayenv.ModeWindowsBrowser, Capabilities: []displayenv.Capability{}}
	}}
	r := gin.New()
	r.GET("/d", h.GetDisplay)
	get := func(q string) map[string]any {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/d"+q, nil))
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	out := get("")
	data, _ := out["data"].(map[string]any)
	if out["success"] != true || data["mode"] != "windows-browser" || data["is_wsl"] != true {
		t.Errorf("resposta: %+v", out)
	}
	get("")
	if calls != 1 {
		t.Errorf("a 2ª chamada deveria vir do cache: %d detecções", calls)
	}
	get("?refresh=true")
	if calls != 2 {
		t.Errorf("?refresh=true deveria detectar de novo: %d", calls)
	}
}
