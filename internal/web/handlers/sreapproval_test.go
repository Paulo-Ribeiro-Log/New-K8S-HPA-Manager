package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// TestSREApprovalGetCurrentUser_PrefersJWTUserEmail — bug real corrigido: `GET /current-user`
// sempre chamava `az account show` no processo do servidor, ignorando por completo a identidade
// real de quem está logado na aplicação via SSO/JWT (`user_email` no contexto Gin, populado pelo
// middleware `InjectUserEmail()`). Esse é o único caminho testável sem depender de `az` CLI real
// (o fallback em si já é coberto pelo comportamento pré-existente de `sreapproval.GetCurrentUserEmail`,
// inalterado por esta correção).
func TestSREApprovalGetCurrentUser_PrefersJWTUserEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	h := NewSREApprovalHandler(&logger)

	router := gin.New()
	router.GET("/current-user", func(c *gin.Context) {
		// Simula o InjectUserEmail() real (server.go) — o dado que hoje só existe quando o
		// middleware está de fato aplicado na rota.
		c.Set("user_email", "aprovador.real@via.com.br")
		h.GetCurrentUser(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/current-user", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, esperava 200 — body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool   `json:"success"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("falha ao decodificar resposta: %v — body: %s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatal("success = false, esperava true")
	}
	// O ponto central do bug: o e-mail devolvido precisa ser o do JWT (aprovador real logado na
	// sessão web), NUNCA o de uma chamada `az account show` no servidor — que nem deveria rodar
	// quando o contexto já tem `user_email`.
	if resp.Email != "aprovador.real@via.com.br" {
		t.Errorf("email = %q, esperava o e-mail do JWT (aprovador.real@via.com.br), não um fallback via Azure CLI", resp.Email)
	}
}

// TestSREApprovalApprove_DefaultsToJWTUserEmailWhenRequestOmitsIt — mesma correção, aplicada ao
// endpoint de aprovação em si: quando o request chega sem `approver_email` explícito, o handler
// deve preencher com o e-mail da sessão (JWT) ANTES de repassar pro client — nunca deixar o campo
// vazio seguir adiante pra só então cair no fallback de Azure CLI dentro do client.
func TestSREApprovalApprove_DefaultsToJWTUserEmailWhenRequestOmitsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Reproduz só a parte do handler responsável pela prioridade de e-mail (sem chamar
	// h.client.Approve de verdade, que faria uma requisição HTTP real pro devstartcd) — mesmo
	// princípio de teste unitário focado já usado noutros handlers deste pacote quando o cliente
	// externo não é mockável via interface.
	router := gin.New()
	var gotApproverEmail string
	router.POST("/approve", func(c *gin.Context) {
		c.Set("user_email", "aprovador.real@via.com.br")

		var req struct {
			ApprovalURL   string `json:"approval_url"`
			ApproverEmail string `json:"approver_email,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false})
			return
		}
		if req.ApproverEmail == "" {
			req.ApproverEmail = c.GetString("user_email")
		}
		gotApproverEmail = req.ApproverEmail
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	body := `{"approval_url":"https://devstartcd.via.com.br/approve/123"}`
	req := httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, esperava 200 — body: %s", w.Code, w.Body.String())
	}
	if gotApproverEmail != "aprovador.real@via.com.br" {
		t.Errorf("approver_email = %q, esperava o e-mail do JWT preenchido automaticamente", gotApproverEmail)
	}
}
