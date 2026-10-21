package handlers

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Estado da sessão do Azure CLI do processo do servidor. Só leitura: nunca dispara `az login`
// (o login é interativo e o usuário o faz no terminal dele).

const (
	azureSessionCacheTTL = 60 * time.Second
	azureSessionTimeout  = 15 * time.Second
)

// AzureSessionStatus é a resposta de GET /azure/session.
type AzureSessionStatus struct {
	OK        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`   // login_required | cli_missing | error
	Resource  string `json:"resource,omitempty"` // recurso cujo token falhou (arm | graph)
	Detail    string `json:"detail,omitempty"`   // stderr do az, resumido
	CheckedAt string `json:"checkedAt"`
}

var azureSessionResources = []struct{ name, url string }{
	{"arm", "https://management.azure.com"},
	{"graph", "https://graph.microsoft.com"},
}

type AzureSessionHandler struct {
	mu      sync.Mutex
	cached  AzureSessionStatus
	expires time.Time
	check   func(ctx context.Context) AzureSessionStatus
}

func NewAzureSessionHandler() *AzureSessionHandler {
	return &AzureSessionHandler{check: checkAzureSession}
}

// GetSession devolve o estado da sessão do `az` (cache de 60 s; ?refresh=1 força nova checagem).
func (h *AzureSessionHandler) GetSession(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Query("refresh") != "1" && time.Now().Before(h.expires) {
		c.JSON(http.StatusOK, h.cached)
		return
	}
	h.cached = h.check(c.Request.Context())
	h.expires = time.Now().Add(azureSessionCacheTTL)
	c.JSON(http.StatusOK, h.cached)
}

// checkAzureSession pede um token de cada recurso usado pela aplicação (ARM e Graph). O token
// do Graph pode falhar mesmo com o ARM válido (CAE, consentimento), por isso os dois.
func checkAzureSession(ctx context.Context) AzureSessionStatus {
	st := AzureSessionStatus{OK: true, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, r := range azureSessionResources {
		cctx, cancel := context.WithTimeout(ctx, azureSessionTimeout)
		_, err := exec.CommandContext(cctx, "az", "account", "get-access-token",
			"--resource", r.url, "--query", "expiresOn", "-o", "tsv").Output()
		cancel()
		if err == nil {
			continue
		}
		st.OK = false
		st.Resource = r.name
		st.Reason, st.Detail = classifyAzureError(err)
		return st
	}
	return st
}

// classifyAzureError separa "precisa de az login" de "az ausente" e de qualquer outra falha.
func classifyAzureError(err error) (reason, detail string) {
	var nf *exec.Error
	if errors.As(err, &nf) {
		return "cli_missing", "Azure CLI (az) não encontrado no PATH do servidor"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		detail = strings.TrimSpace(string(ee.Stderr))
	}
	if len(detail) > 400 {
		detail = detail[:400] + "…"
	}
	low := strings.ToLower(detail)
	for _, m := range []string{"az login", "aadsts70043", "aadsts700082", "aadsts50173", "aadsts50076", "aadsts50079", "interaction_required", "refresh token", "expired"} {
		if strings.Contains(low, m) {
			return "login_required", detail
		}
	}
	return "error", detail
}
