package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClassifyAzureError(t *testing.T) {
	_, missing := exec.Command("az-inexistente-xyz").Output()
	if r, _ := classifyAzureError(missing); r != "cli_missing" {
		t.Fatalf("esperava cli_missing, veio %q", r)
	}
	fail := exec.Command("sh", "-c", "echo \"ERROR: Please run 'az login' to setup account.\" >&2; exit 1")
	_, err := fail.Output()
	if r, d := classifyAzureError(err); r != "login_required" || d == "" {
		t.Fatalf("esperava login_required com detalhe, veio %q/%q", r, d)
	}
	other := exec.Command("sh", "-c", "echo 'boom' >&2; exit 1")
	_, err = other.Output()
	if r, _ := classifyAzureError(err); r != "error" {
		t.Fatalf("esperava error, veio %q", r)
	}
}

func TestAzureSessionCacheERefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	h := &AzureSessionHandler{check: func(context.Context) AzureSessionStatus {
		calls++
		return AzureSessionStatus{OK: calls > 1}
	}}
	get := func(q string) AzureSessionStatus {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/azure/session"+q, nil)
		h.GetSession(c)
		var st AzureSessionStatus
		_ = json.Unmarshal(w.Body.Bytes(), &st)
		return st
	}
	if get("").OK || get("").OK || calls != 1 {
		t.Fatalf("cache não foi usado (calls=%d)", calls)
	}
	if !get("?refresh=1").OK || calls != 2 {
		t.Fatalf("refresh não refez a checagem (calls=%d)", calls)
	}
}
