package handlers

import (
	"errors"
	"fmt"
	"testing"

	"k8s-hpa-manager/internal/azlogin"
)

// Login do Azure expirado sai como AZ_LOGIN_REQUIRED (a UI mostra o aviso com o comando).
func TestCloudAuthErrorCode(t *testing.T) {
	if got := cloudAuthErrorCode(fmt.Errorf("%w\nAADSTS70043", azlogin.ErrLoginRequired)); got != azlogin.Code {
		t.Errorf("login expirado: %q, quer %q", got, azlogin.Code)
	}
	if got := cloudAuthErrorCode(errors.New("credenciais AWS incompletas")); got != "CLOUD_AUTH_FAILED" {
		t.Errorf("outro erro: %q, quer CLOUD_AUTH_FAILED", got)
	}
}
