package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Login silencioso do Azure CLI: refaz o `az login` no tenant da subscription atual e volta a
// selecioná-la, sem o seletor interativo de subscription do `az login` no terminal.

const azureLoginTimeout = 3 * time.Minute

// azRun executa o `az` com args e env extras; devolve stdout (ou o stderr do az no erro).
type azRun func(ctx context.Context, env []string, args ...string) ([]byte, error)

func runAz(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

type azureAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
}

// AzureLoginResult é a resposta de POST /azure/login.
type AzureLoginResult struct {
	AzureSessionStatus
	Subscription string `json:"subscription,omitempty"`
}

// Login — POST /azure/login. Abre o login no navegador da máquina do servidor (a mesma do
// usuário), espera terminar (até 3 min) e reaplica a subscription que era a atual.
func (h *AzureSessionHandler) Login(c *gin.Context) {
	if !h.loginMu.TryLock() {
		c.JSON(http.StatusConflict, gin.H{"error": "Já existe um login do Azure em andamento"})
		return
	}
	defer h.loginMu.Unlock()

	// O `account show` lê o perfil local (sem rede), então funciona com a sessão expirada.
	showCtx, cancel := context.WithTimeout(c.Request.Context(), azureSessionTimeout)
	out, err := h.run(showCtx, nil, "account", "show", "-o", "json")
	cancel()
	var acc azureAccount
	if err != nil || json.Unmarshal(out, &acc) != nil || acc.ID == "" || acc.TenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Não há subscription atual no Azure CLI. Rode `az login` no terminal uma vez."})
		return
	}

	// login_experience_v2=off desliga o seletor de subscription do `az login`.
	loginCtx, cancel := context.WithTimeout(c.Request.Context(), azureLoginTimeout)
	defer cancel()
	if _, err := h.run(loginCtx, []string{"AZURE_CORE_LOGIN_EXPERIENCE_V2=off"}, "login", "--tenant", acc.TenantID, "-o", "none"); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "az login falhou: " + err.Error()})
		return
	}
	setCtx, cancel := context.WithTimeout(c.Request.Context(), azureSessionTimeout)
	defer cancel()
	if _, err := h.run(setCtx, nil, "account", "set", "--subscription", acc.ID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "login feito, mas não consegui voltar para a subscription " + acc.Name + ": " + err.Error()})
		return
	}

	h.mu.Lock()
	h.cached = h.check(c.Request.Context())
	h.expires = time.Now().Add(azureSessionCacheTTL)
	st := h.cached
	h.mu.Unlock()
	c.JSON(http.StatusOK, AzureLoginResult{AzureSessionStatus: st, Subscription: acc.Name})
}
