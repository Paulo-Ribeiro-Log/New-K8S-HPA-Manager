package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// TestSREApprovalResolveApproverEmail_PrefersSSOProfileOverJWT — bug real corrigido, relatado pelo
// usuário logo depois de uma correção anterior desta mesma rotina: o e-mail de login da aplicação
// (JWT/Azure AD, ex: uma conta de nuvem secundária `.ca@via.com.br`) NÃO é o e-mail que o
// ServiceNow reconhece — quem loga lá é o e-mail cadastrado no Perfil SSO corporativo
// (`~/.k8s-hpa-manager/sso_profile.json`), que pode ser (e no caso relatado é) um e-mail
// corporativo diferente. `resolveApproverEmail` deve priorizar o Perfil SSO sobre o JWT.
func TestSREApprovalResolveApproverEmail_PrefersSSOProfileOverJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	tmpDir := t.TempDir()

	// Perfil SSO configurado com um e-mail — mesmo formato gravado por SaveProfile (sso_profile.go),
	// sem senha (o ponto central desta correção: ssoProfileEmail nunca deveria exigir senha válida
	// só pra devolver o e-mail já cadastrado).
	profile := ssoProfileFile{Email: "paulo.gribeiro@viaverjo.com.br"}
	raw, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(tmpDir, "sso_profile.json"), raw, 0600); err != nil {
		t.Fatalf("falha ao escrever perfil SSO de teste: %v", err)
	}

	h := NewSREApprovalHandler(&logger, tmpDir)

	router := gin.New()
	router.GET("/current-user", func(c *gin.Context) {
		// Simula o InjectUserEmail() real (server.go) — e-mail de login da aplicação, DIFERENTE
		// do e-mail cadastrado no Perfil SSO acima.
		c.Set("user_email", "4960023587.ca@via.com.br")
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
	if resp.Email != "paulo.gribeiro@viaverjo.com.br" {
		t.Errorf("email = %q, esperava o e-mail do Perfil SSO, não o de login da aplicação (JWT)", resp.Email)
	}
}

// TestSREApprovalResolveApproverEmail_FallsBackToJWTWhenNoSSOProfile — sem Perfil SSO configurado
// (arquivo inexistente), o e-mail do JWT ainda deve ser usado como fallback — melhor que nada, e
// preserva o comportamento já corrigido numa rodada anterior desta mesma correção.
func TestSREApprovalResolveApproverEmail_FallsBackToJWTWhenNoSSOProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	h := NewSREApprovalHandler(&logger, t.TempDir()) // diretório vazio, sem sso_profile.json

	router := gin.New()
	router.GET("/current-user", func(c *gin.Context) {
		c.Set("user_email", "aprovador.jwt@via.com.br")
		h.GetCurrentUser(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/current-user", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, esperava 200 — body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Email != "aprovador.jwt@via.com.br" {
		t.Errorf("email = %q, esperava o fallback do JWT quando não há Perfil SSO configurado", resp.Email)
	}
}

// TestSREApprovalApprove_DefaultsToResolvedApproverEmailWhenRequestOmitsIt — mesma correção,
// aplicada ao endpoint de aprovação em si: quando o request chega sem `approver_email` explícito,
// o handler deve preencher com o e-mail resolvido (Perfil SSO > JWT) ANTES de repassar pro client.
func TestSREApprovalApprove_DefaultsToResolvedApproverEmailWhenRequestOmitsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zerolog.Nop()
	tmpDir := t.TempDir()
	profile := ssoProfileFile{Email: "paulo.gribeiro@viaverjo.com.br"}
	raw, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(tmpDir, "sso_profile.json"), raw, 0600); err != nil {
		t.Fatalf("falha ao escrever perfil SSO de teste: %v", err)
	}
	h := NewSREApprovalHandler(&logger, tmpDir)

	// Reproduz só a parte do handler responsável pela prioridade de e-mail (sem chamar
	// h.client.Approve de verdade, que faria uma requisição HTTP real pro devstartcd) — mesmo
	// princípio de teste unitário focado já usado noutros handlers deste pacote quando o cliente
	// externo não é mockável via interface.
	router := gin.New()
	var gotApproverEmail string
	router.POST("/approve", func(c *gin.Context) {
		c.Set("user_email", "4960023587.ca@via.com.br")

		var req struct {
			ApprovalURL   string `json:"approval_url"`
			ApproverEmail string `json:"approver_email,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false})
			return
		}
		if req.ApproverEmail == "" {
			req.ApproverEmail = h.resolveApproverEmail(c)
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
	if gotApproverEmail != "paulo.gribeiro@viaverjo.com.br" {
		t.Errorf("approver_email = %q, esperava o e-mail do Perfil SSO preenchido automaticamente", gotApproverEmail)
	}
}
