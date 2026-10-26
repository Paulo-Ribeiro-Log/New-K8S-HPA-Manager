package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
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

func TestAzureLogin_UsaSubscriptionAtualSemSeletor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls []string
	var loginEnv []string
	h := &AzureSessionHandler{
		check: func(context.Context) AzureSessionStatus { return AzureSessionStatus{OK: true} },
		run: func(_ context.Context, env []string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "account" && args[1] == "show" {
				return []byte(`{"id":"sub-1","name":"Prod","tenantId":"ten-1"}`), nil
			}
			if args[0] == "login" {
				loginEnv = env
			}
			return nil, nil
		},
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/azure/login", nil)
	h.Login(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	want := []string{"account show -o json", "login --tenant ten-1 -o none", "account set --subscription sub-1"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("chamadas = %v, quer %v", calls, want)
	}
	if len(loginEnv) != 1 || loginEnv[0] != "AZURE_CORE_LOGIN_EXPERIENCE_V2=off" {
		t.Fatalf("env do login = %v", loginEnv)
	}
	var res AzureLoginResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if !res.OK || res.Subscription != "Prod" {
		t.Fatalf("resposta = %+v", res)
	}
}

func TestAzureLogin_SemSubscriptionAtual(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AzureSessionHandler{run: func(context.Context, []string, ...string) ([]byte, error) {
		return nil, errors.New("Please run 'az login'")
	}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/azure/login", nil)
	h.Login(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, veio %d", w.Code)
	}
}
