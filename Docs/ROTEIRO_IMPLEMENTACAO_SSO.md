# Roteiro de Implementação: SSO com Azure AD para Servidor Centralizado

**Data:** 11 de dezembro de 2025
**Projeto:** K8s HPA Manager
**Versão:** 1.0 - Análise Crítica e Roadmap Completo
**Status:** 📋 Planejamento

---

## 📋 Índice

1. [Contexto do Projeto](#contexto-do-projeto)
2. [Problema Crítico Identificado](#problema-crítico-identificado)
3. [Por Que SSO é Obrigatório](#por-que-sso-é-obrigatório)
4. [Arquitetura Proposta](#arquitetura-proposta)
5. [Roadmap de Implementação](#roadmap-de-implementação)
6. [Refatoração do RBAC](#refatoração-do-rbac)
7. [Sistema de Auditoria Centralizada](#sistema-de-auditoria-centralizada)
8. [Segurança e Compliance](#segurança-e-compliance)
9. [Checklist de Implementação](#checklist-de-implementação)
10. [Estimativa de Esforço](#estimativa-de-esforço)

---

## Contexto do Projeto

### Situação Atual

- ✅ **Interface TUI removida** - Aplicação 100% Web
- ✅ **RBAC implementado** - Grupo `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
- ✅ **Autenticação com token estático** - `K8S_HPA_WEB_TOKEN` ou `poc-token-123`
- ✅ **Sistema de histórico local** - `~/.k8s-hpa-manager/history/`

### Arquitetura Futura

- 🎯 **Servidor centralizado** - Toda empresa terá acesso
- 🎯 **DNS dedicado** - Ex: `https://k8s-hpa.empresa.com`
- 🎯 **Porta do servidor** - Configurada no DNS (não mais localhost:8080)
- 🎯 **Auditoria imutável** - Logs não podem ser apagados localmente

---

## Problema Crítico Identificado

### 🚨 RBAC Atual NÃO Funciona em Servidor Centralizado

#### Sistema RBAC Atual (Azure CLI)

```go
// internal/rbac/azure_ad.go (LINHA 46)
func (r *RBACManager) GetCurrentUserEmail(ctx context.Context) (string, error) {
    cmd := exec.CommandContext(ctx, "az", "account", "show", "--query", "user.name", "-o", "tsv")
    output, err := cmd.Output()
    // ...
    return strings.TrimSpace(string(output)), nil
}
```

**Problema:**
```
┌─────────────────────────────────────────────────────────────┐
│ Desenvolvimento Local (FUNCIONA)                            │
└─────────────────────────────────────────────────────────────┘
Desenvolvedor → az login no próprio PC → az account show
                                              ↓
                                    joao.silva@empresa.com ✅

┌─────────────────────────────────────────────────────────────┐
│ Servidor Centralizado (NÃO FUNCIONA)                        │
└─────────────────────────────────────────────────────────────┘
Usuário A (João) → https://k8s-hpa.empresa.com
Usuário B (Maria) → https://k8s-hpa.empresa.com
                              ↓
                    Servidor executa: az account show
                              ↓
                    service-account@empresa.com ❌ ERRADO!
                              ↓
                    Todos os usuários aparecem como "service-account"
                              ↓
                    RBAC não consegue diferenciar usuários
```

### Por Que Azure CLI Não Funciona

| Aspecto | Desenvolvimento Local | Servidor Centralizado |
|---------|----------------------|----------------------|
| **Azure CLI login** | Cada dev faz `az login` | Service account ou Managed Identity |
| **`az account show`** | Retorna email do dev | ❌ Retorna email do SERVIDOR |
| **Grupos do usuário** | Grupos do dev | ❌ Grupos do service account |
| **RBAC funciona?** | ✅ Sim | ❌ NÃO! |
| **Auditoria** | ✅ Identifica dev | ❌ Todos aparecem como "service-account" |

### Impacto

- ❌ **Impossível identificar usuários** individualmente
- ❌ **RBAC não funciona** (todos têm mesmas permissões do servidor)
- ❌ **Auditoria inútil** (não dá pra saber quem fez o quê)
- ❌ **Compliance comprometido** (não rastreável)

---

## Por Que SSO é Obrigatório

### Comparação: Token Estático vs SSO

| Requisito | Token Estático + RBAC Azure CLI | SSO OAuth 2.0/OIDC |
|-----------|--------------------------------|-------------------|
| **Servidor centralizado** | ❌ RBAC não funciona | ✅ Funciona perfeitamente |
| **Identificar usuário** | ❌ Todos são "service-account" | ✅ JWT contém email individual |
| **Verificar grupos Azure AD** | ❌ Grupos do servidor | ✅ Grupos do usuário (no JWT) |
| **MFA** | ⚠️ Só no `az login` do servidor | ✅ MFA para cada usuário |
| **Auditoria** | ❌ Impossível | ✅ Completa (email, nome, IP) |
| **Revogação de acesso** | ❌ Token compartilhado | ✅ Token individual revogável |
| **Expiração** | ❌ Token nunca expira | ✅ 1h com auto-refresh |
| **Compliance** | ❌ Não atende | ✅ Atende |

### Conclusão

**SSO NÃO É OPCIONAL - É OBRIGATÓRIO** para servidor centralizado porque:

1. ✅ RBAC atual **não funciona** em servidor (retorna service account)
2. ✅ Auditoria local **insegura** (pode ser apagada)
3. ✅ Compliance **obrigatório** para acesso corporativo
4. ✅ DNS resolve problema de callback URL
5. ✅ Sem TUI, foco 100% na Web (SSO funciona perfeitamente)

---

## Arquitetura Proposta

### Visão Geral

```
┌──────────────────────────────────────────────────────────────┐
│                    ARQUITETURA COMPLETA                       │
└──────────────────────────────────────────────────────────────┘

1. AUTENTICAÇÃO (Login)
   ↓
   Browser → https://k8s-hpa.empresa.com
               ↓
        Login Page (React)
               ↓
        "Login with Microsoft" button
               ↓
        Redirect: https://login.microsoftonline.com/...
               ↓
        Azure AD Login + MFA
               ↓
        Callback: /auth/callback?code=abc123
               ↓
        Backend (Go) troca código por tokens:
          • access_token (JWT, válido 1h)
          • refresh_token (válido 90 dias)
          • id_token (JWT com user info)
               ↓
        Extrai do JWT:
          • email: joao.silva@empresa.com
          • name: João Silva
          • groups: ["VV_CLOUD_SRE", "DevOps", ...]
               ↓
        ✅ Armazena em localStorage + Cookie seguro

2. AUTORIZAÇÃO (Verificar permissões - RBAC)
   ↓
   Usuário clica "Apply HPA"
          ↓
   Frontend: POST /api/v1/hpas/:cluster/:namespace/:name
             Header: Authorization: Bearer eyJhbG... (JWT)
          ↓
   Backend: Middleware RBAC
          ↓
       1. Valida JWT com Azure AD (verifica assinatura)
       2. Extrai claims (email, groups)
       3. Verifica se "VV_CLOUD_SRE" está em groups[]
          ↓
          ├── ✅ SIM → Libera ação + registra auditoria
          └── ❌ NÃO → HTTP 403 Forbidden

3. AUDITORIA (Registrar ação)
   ↓
   Ação aprovada (RBAC OK)
          ↓
   Extrai contexto do JWT (já validado):
     • user_email: joao.silva@empresa.com
     • user_name: João Silva
     • user_groups: ["VV_CLOUD_SRE"]
     • ip_address: 192.168.1.100 (X-Forwarded-For)
          ↓
   Cria AuditLog entry:
     {
       "timestamp": "2025-12-11T10:30:00Z",
       "user_email": "joao.silva@empresa.com",
       "user_name": "João Silva",
       "ip_address": "192.168.1.100",
       "action": "update_hpa",
       "resource": "production/api-backend",
       "cluster": "akspriv-prod",
       "before": { "min_replicas": 2 },
       "after": { "min_replicas": 5 },
       "status": "success"
     }
          ↓
   Persiste em 3 locais (redundância):
     1. PostgreSQL (consultas SQL + retenção longa)
     2. Azure Log Analytics (auditoria imutável cloud)
     3. Local JSON (backup + UI rápida - opcional)
```

### Componentes da Arquitetura

```
k8s-hpa-manager/
├── internal/
│   ├── web/
│   │   ├── auth/                    # ⭐ NOVO
│   │   │   ├── oidc.go              # Configuração Azure AD OIDC
│   │   │   ├── handlers.go          # /auth/login, /auth/callback, /auth/refresh
│   │   │   └── middleware.go        # JWTAuthMiddleware (valida tokens)
│   │   ├── handlers/
│   │   │   └── ... (existente)
│   │   └── middleware/
│   │       ├── auth.go              # ❌ DEPRECAR (token estático)
│   │       └── rbac.go              # ✅ MANTER (atualizar para JWT)
│   ├── rbac/
│   │   ├── azure_ad.go              # ⚠️ REFATORAR (ler JWT ao invés de Azure CLI)
│   │   └── azure_ad_test.go         # ⚠️ ATUALIZAR (testes com JWT mocks)
│   ├── audit/                       # ⭐ NOVO
│   │   ├── models.go                # AuditLog struct
│   │   ├── postgres.go              # PostgreSQL writer
│   │   ├── azure_logs.go            # Azure Log Analytics writer
│   │   └── writer.go                # Interface + async writes
│   └── history/
│       └── tracker.go               # ⚠️ ATUALIZAR (integrar com audit/)
└── frontend/
    ├── src/
    │   ├── pages/
    │   │   ├── Login.tsx            # ⚠️ REFATORAR (SSO flow)
    │   │   └── AuditViewer.tsx      # ⭐ NOVO
    │   ├── hooks/
    │   │   └── useTokenRefresh.ts   # ⭐ NOVO (auto-refresh JWT)
    │   └── lib/api/
    │       └── client.ts            # ⚠️ ATUALIZAR (JWT ao invés de token estático)
```

---

## Roadmap de Implementação

### Fase 1: SSO Backend (2 dias) 🔴 CRÍTICO

#### Dia 1: Implementar OAuth 2.0

**Criar módulo de autenticação:**

```go
// internal/web/auth/oidc.go
package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCProvider struct {
	Provider     *oidc.Provider
	OAuth2Config *oauth2.Config
	Verifier     *oidc.IDTokenVerifier
}

func NewOIDCProvider(ctx context.Context) (*OIDCProvider, error) {
	tenantID := os.Getenv("AZURE_TENANT_ID")
	clientID := os.Getenv("AZURE_CLIENT_ID")
	clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	redirectURL := os.Getenv("AZURE_REDIRECT_URL") // https://k8s-hpa.empresa.com/auth/callback

	if tenantID == "" || clientID == "" {
		return nil, fmt.Errorf("AZURE_TENANT_ID and AZURE_CLIENT_ID must be set")
	}

	issuerURL := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", tenantID)

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "User.Read"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return &OIDCProvider{
		Provider:     provider,
		OAuth2Config: oauth2Config,
		Verifier:     verifier,
	}, nil
}
```

**Implementar handlers de autenticação:**

```go
// internal/web/auth/handlers.go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	oidcProvider *OIDCProvider
}

func NewAuthHandler(oidcProvider *OIDCProvider) *AuthHandler {
	return &AuthHandler{
		oidcProvider: oidcProvider,
	}
}

// GET /auth/login - Redireciona para Azure AD
func (h *AuthHandler) Login(c *gin.Context) {
	// Gerar state para CSRF protection
	state, err := generateRandomState()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to generate state"})
		return
	}

	// Salvar state em cookie seguro
	c.SetCookie("oauth_state", state, 600, "/", "", true, true) // secure=true, httpOnly=true

	// Redirecionar para Azure AD
	authURL := h.oidcProvider.OAuth2Config.AuthCodeURL(state)
	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

// GET /auth/callback - Recebe código do Azure AD e troca por tokens
func (h *AuthHandler) Callback(c *gin.Context) {
	// Validar state (CSRF protection)
	state := c.Query("state")
	cookieState, err := c.Cookie("oauth_state")
	if err != nil || state != cookieState {
		c.JSON(400, gin.H{"error": "Invalid state parameter"})
		return
	}

	// Trocar código por tokens
	code := c.Query("code")
	oauth2Token, err := h.oidcProvider.OAuth2Config.Exchange(context.Background(), code)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to exchange token"})
		return
	}

	// Extrair ID Token (JWT)
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		c.JSON(500, gin.H{"error": "No id_token in response"})
		return
	}

	// Validar ID Token
	idToken, err := h.oidcProvider.Verifier.Verify(context.Background(), rawIDToken)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to verify ID token"})
		return
	}

	// Extrair claims (user info)
	var claims struct {
		Email  string   `json:"email"`
		Name   string   `json:"name"`
		Sub    string   `json:"sub"`
		Groups []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse claims"})
		return
	}

	// Redirecionar para frontend com tokens (via query params ou cookie seguro)
	// Opção 1: Cookie HttpOnly (mais seguro)
	c.SetCookie("access_token", oauth2Token.AccessToken, 3600, "/", "", true, true)
	c.SetCookie("refresh_token", oauth2Token.RefreshToken, 7776000, "/", "", true, true) // 90 dias

	// Redirecionar para dashboard
	c.Redirect(http.StatusTemporaryRedirect, "/")
}

// POST /auth/refresh - Renova access token usando refresh token
func (h *AuthHandler) Refresh(c *gin.Context) {
	// Opção 1: Ler refresh token do cookie
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		c.JSON(401, gin.H{"error": "No refresh token"})
		return
	}

	// Criar token source com refresh token
	tokenSource := h.oidcProvider.OAuth2Config.TokenSource(
		context.Background(),
		&oauth2.Token{RefreshToken: refreshToken},
	)

	// Obter novo access token
	newToken, err := tokenSource.Token()
	if err != nil {
		c.JSON(401, gin.H{"error": "Failed to refresh token"})
		return
	}

	// Atualizar cookie
	c.SetCookie("access_token", newToken.AccessToken, 3600, "/", "", true, true)

	c.JSON(200, gin.H{
		"access_token": newToken.AccessToken,
		"expires_in":   newToken.Expiry.Unix(),
	})
}

// POST /auth/logout - Logout
func (h *AuthHandler) Logout(c *gin.Context) {
	// Limpar cookies
	c.SetCookie("access_token", "", -1, "/", "", true, true)
	c.SetCookie("refresh_token", "", -1, "/", "", true, true)
	c.SetCookie("oauth_state", "", -1, "/", "", true, true)

	c.JSON(200, gin.H{"message": "Logged out successfully"})
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
```

**Implementar middleware JWT:**

```go
// internal/web/auth/middleware.go
package auth

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

func JWTAuthMiddleware(oidcProvider *OIDCProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Tentar ler token do header Authorization
		authHeader := c.GetHeader("Authorization")
		var token string

		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		// Fallback: ler do cookie (para navegação)
		if token == "" {
			token, _ = c.Cookie("access_token")
		}

		if token == "" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "UNAUTHORIZED",
					"message": "No authentication token provided",
				},
			})
			c.Abort()
			return
		}

		// Validar JWT com Azure AD
		idToken, err := oidcProvider.Verifier.Verify(context.Background(), token)
		if err != nil {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_TOKEN",
					"message": "Invalid or expired token",
				},
			})
			c.Abort()
			return
		}

		// Extrair claims
		var claims struct {
			Email  string   `json:"email"`
			Name   string   `json:"name"`
			Sub    string   `json:"sub"`
			Groups []string `json:"groups"`
		}
		if err := idToken.Claims(&claims); err != nil {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_CLAIMS",
					"message": "Invalid token claims",
				},
			})
			c.Abort()
			return
		}

		// Adicionar user info ao contexto (usado pelo RBAC)
		c.Set("user_email", claims.Email)
		c.Set("user_name", claims.Name)
		c.Set("user_sub", claims.Sub)
		c.Set("user_groups", claims.Groups)

		c.Next()
	}
}
```

**Tarefas Dia 1:**

- [ ] Instalar dependências:
  ```bash
  go get github.com/coreos/go-oidc/v3/oidc
  go get golang.org/x/oauth2
  ```
- [ ] Criar `internal/web/auth/oidc.go`
- [ ] Criar `internal/web/auth/handlers.go`
- [ ] Criar `internal/web/auth/middleware.go`
- [ ] Atualizar `internal/web/server.go` para registrar rotas de auth
- [ ] Testar login flow manualmente

#### Dia 2: Refatorar RBAC

**Atualizar RBAC para usar JWT:**

```go
// internal/rbac/azure_ad.go (REFATORADO)
package rbac

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ⚠️ MUDANÇA: Não usa mais Azure CLI
// Agora lê email e grupos do gin.Context (extraídos do JWT)

// GetCurrentUserEmail obtém email do JWT (via gin.Context)
func (r *RBACManager) GetCurrentUserEmail(c *gin.Context) (string, error) {
	email, exists := c.Get("user_email")
	if !exists {
		return "", fmt.Errorf("user_email not found in context")
	}
	return email.(string), nil
}

// GetUserGroups obtém grupos do JWT (via gin.Context)
func (r *RBACManager) GetUserGroups(c *gin.Context) ([]ADGroup, error) {
	groupNames, exists := c.Get("user_groups")
	if !exists {
		return nil, fmt.Errorf("user_groups not found in context")
	}

	// Converter []string para []ADGroup
	names := groupNames.([]string)
	groups := make([]ADGroup, len(names))
	for i, name := range names {
		groups[i] = ADGroup{
			DisplayName: name,
			ID:          "", // ID não está disponível no JWT, apenas nome
		}
	}

	return groups, nil
}

// CheckCurrentUserIsSRE verifica se usuário atual é SRE (via JWT)
func (r *RBACManager) CheckCurrentUserIsSRE(c *gin.Context) (bool, error) {
	// Se verificação AD estiver desabilitada, sempre retorna true (modo emergência)
	if r.disableADCheck {
		return true, nil
	}

	groups, err := r.GetUserGroups(c)
	if err != nil {
		return false, err
	}

	for _, group := range groups {
		if strings.EqualFold(group.DisplayName, "VV_CLOUD_SRE") {
			return true, nil
		}
	}
	return false, nil
}

// GetCurrentUserPermissions obtém permissões do usuário atual (via JWT)
func (r *RBACManager) GetCurrentUserPermissions(c *gin.Context) (*UserPermissions, error) {
	// Se verificação AD estiver desabilitada, retorna permissões bypass
	if r.disableADCheck {
		return &UserPermissions{
			Email:    "bypass@emergency.mode",
			IsSRE:    true,
			Groups:   []ADGroup{{ID: "bypass", DisplayName: "EMERGENCY_MODE"}},
			CachedAt: time.Now(),
		}, nil
	}

	email, err := r.GetCurrentUserEmail(c)
	if err != nil {
		return nil, err
	}

	groups, err := r.GetUserGroups(c)
	if err != nil {
		return nil, err
	}

	isSRE := r.checkSREGroup(groups)

	return &UserPermissions{
		Email:    email,
		IsSRE:    isSRE,
		Groups:   groups,
		CachedAt: time.Now(),
	}, nil
}

// ⚠️ REMOVER: Métodos antigos que usavam Azure CLI
// - Remover: GetCurrentUserEmail(ctx context.Context)
// - Remover: GetUserGroups(ctx context.Context, email string)
// - Manter apenas versões com gin.Context
```

**Atualizar middleware RBAC:**

```go
// internal/web/middleware/rbac.go (ATUALIZADO)
package middleware

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/rbac"
)

func RBACMiddleware(rbacManager *rbac.RBACManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ⚠️ MUDANÇA: Passa gin.Context ao invés de context.Context
		isSRE, err := rbacManager.CheckCurrentUserIsSRE(c)
		if err != nil {
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "RBAC_ERROR",
					"message": "Failed to check user permissions",
				},
			})
			c.Abort()
			return
		}

		if !isSRE {
			c.JSON(403, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "FORBIDDEN",
					"message": "Access denied. User is not member of VV_CLOUD_SRE group.",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
```

**Atualizar testes:**

```go
// internal/rbac/azure_ad_test.go (ATUALIZADO)
package rbac

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetCurrentUserEmailFromJWT(t *testing.T) {
	manager := NewRBACManager(false)

	// Mock gin.Context com JWT claims
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_email", "joao.silva@empresa.com")
	c.Set("user_name", "João Silva")
	c.Set("user_groups", []string{"VV_CLOUD_SRE", "DevOps"})

	email, err := manager.GetCurrentUserEmail(c)
	if err != nil {
		t.Fatalf("Failed to get user email: %v", err)
	}

	if email != "joao.silva@empresa.com" {
		t.Errorf("Expected joao.silva@empresa.com, got %s", email)
	}
}

func TestCheckCurrentUserIsSRE_WithJWT(t *testing.T) {
	manager := NewRBACManager(false)

	// Mock gin.Context com JWT claims (SRE user)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_email", "joao.silva@empresa.com")
	c.Set("user_groups", []string{"VV_CLOUD_SRE", "DevOps"})

	isSRE, err := manager.CheckCurrentUserIsSRE(c)
	if err != nil {
		t.Fatalf("Failed to check SRE status: %v", err)
	}

	if !isSRE {
		t.Error("Expected user to be SRE")
	}
}

func TestCheckCurrentUserIsSRE_NonSRE(t *testing.T) {
	manager := NewRBACManager(false)

	// Mock gin.Context com JWT claims (non-SRE user)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_email", "maria.santos@empresa.com")
	c.Set("user_groups", []string{"Developers", "QA"})

	isSRE, err := manager.CheckCurrentUserIsSRE(c)
	if err != nil {
		t.Fatalf("Failed to check SRE status: %v", err)
	}

	if isSRE {
		t.Error("Expected user NOT to be SRE")
	}
}
```

**Tarefas Dia 2:**

- [ ] Refatorar `internal/rbac/azure_ad.go` para usar `gin.Context`
- [ ] Remover dependência de Azure CLI (`exec.Command("az", ...)`)
- [ ] Atualizar `internal/web/middleware/rbac.go`
- [ ] Atualizar testes `internal/rbac/azure_ad_test.go`
- [ ] Testar RBAC com JWT mock
- [ ] Executar `go test ./internal/rbac -v`

---

### Fase 2: SSO Frontend (1 dia) 🔴 CRÍTICO

#### Dia 3: React/TypeScript

**Refatorar Login.tsx:**

```typescript
// internal/web/frontend/src/pages/Login.tsx (REFATORADO)
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Shield } from "lucide-react";

interface LoginProps {
  onLogin: () => void;
}

export const Login = ({ onLogin }: LoginProps) => {
  useEffect(() => {
    // Verificar se já está autenticado (cookie access_token)
    const checkAuth = async () => {
      try {
        const response = await fetch("/api/v1/clusters", {
          credentials: "include", // Inclui cookies
        });

        if (response.ok) {
          // Já está autenticado
          onLogin();
        }
      } catch (err) {
        // Não autenticado, mostrar tela de login
      }
    };

    checkAuth();
  }, [onLogin]);

  const handleLoginWithMicrosoft = () => {
    // Redirecionar para endpoint SSO (Azure AD)
    window.location.href = "/auth/login";
  };

  return (
    <div className="flex items-center justify-center min-h-screen bg-gradient-to-br from-background to-muted">
      <Card className="w-full max-w-md shadow-xl">
        <CardHeader className="space-y-4 text-center">
          <div className="mx-auto w-16 h-16 bg-primary/10 rounded-full flex items-center justify-center">
            <Shield className="w-8 h-8 text-primary" />
          </div>
          <CardTitle className="text-2xl">k8s HPA Manager</CardTitle>
          <CardDescription>
            Faça login com sua conta Microsoft corporativa
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            onClick={handleLoginWithMicrosoft}
            className="w-full"
          >
            Login com Microsoft
          </Button>
          <p className="text-xs text-muted-foreground mt-4 text-center">
            SSO via Azure AD - MFA obrigatório
          </p>
        </CardContent>
      </Card>
    </div>
  );
};
```

**Criar hook de auto-refresh:**

```typescript
// internal/web/frontend/src/hooks/useTokenRefresh.ts (NOVO)
import { useEffect } from "react";
import { apiClient } from "@/lib/api/client";

export const useTokenRefresh = () => {
  useEffect(() => {
    const refreshTokenBeforeExpiry = async () => {
      try {
        // Verificar se está autenticado
        const response = await fetch("/api/v1/clusters", {
          credentials: "include",
        });

        if (!response.ok) {
          // Token expirado ou inválido
          console.log("❌ Token inválido, redirecionando para login");
          window.location.href = "/login";
          return;
        }

        // Token ainda válido
        // Tentar renovar preventivamente (backend decide se precisa)
        await fetch("/auth/refresh", {
          method: "POST",
          credentials: "include",
        });

        console.log("✅ Token verificado/renovado");
      } catch (error) {
        console.error("❌ Erro ao verificar token:", error);
        // Em caso de erro, não fazer logout imediato
        // Aguardar próxima tentativa
      }
    };

    // Verificar a cada 5 minutos
    const interval = setInterval(refreshTokenBeforeExpiry, 5 * 60 * 1000);

    // Verificar imediatamente na montagem
    refreshTokenBeforeExpiry();

    return () => clearInterval(interval);
  }, []);
};
```

**Atualizar API client:**

```typescript
// internal/web/frontend/src/lib/api/client.ts (ATUALIZADO)
class APIClient {
  private baseURL: string;

  constructor() {
    this.baseURL = "/api/v1";
  }

  // ⚠️ REMOVER: setToken(), clearToken() não são mais necessários
  // Cookies são enviados automaticamente com credentials: "include"

  private async request<T>(
    endpoint: string,
    options?: RequestInit
  ): Promise<T> {
    const response = await fetch(`${this.baseURL}${endpoint}`, {
      ...options,
      credentials: "include", // ⭐ IMPORTANTE: Envia cookies automaticamente
      headers: {
        "Content-Type": "application/json",
        ...options?.headers,
      },
    });

    if (response.status === 401) {
      // Token expirado ou inválido
      window.location.href = "/login";
      throw new Error("Unauthorized");
    }

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }

    return response.json();
  }

  // ... resto dos métodos permanecem iguais
}

export const apiClient = new APIClient();
```

**Atualizar App.tsx:**

```typescript
// internal/web/frontend/src/App.tsx (ATUALIZADO)
import { useState, useEffect } from "react";
import { Login } from "./pages/Login";
import { useTokenRefresh } from "./hooks/useTokenRefresh";
// ... outros imports

const App = () => {
  const [isAuthenticated, setIsAuthenticated] = useState(false);

  // ⭐ Auto-refresh de tokens
  useTokenRefresh();

  useEffect(() => {
    // Verificar se já está autenticado ao carregar
    const checkAuth = async () => {
      try {
        const response = await fetch("/api/v1/clusters", {
          credentials: "include",
        });
        setIsAuthenticated(response.ok);
      } catch {
        setIsAuthenticated(false);
      }
    };

    checkAuth();
  }, []);

  if (!isAuthenticated) {
    return <Login onLogin={() => setIsAuthenticated(true)} />;
  }

  return (
    <div className="app">
      {/* Dashboard e resto da aplicação */}
    </div>
  );
};

export default App;
```

**Tarefas Dia 3:**

- [ ] Refatorar `src/pages/Login.tsx` com SSO flow
- [ ] Criar `src/hooks/useTokenRefresh.ts`
- [ ] Atualizar `src/lib/api/client.ts` (remover token estático)
- [ ] Atualizar `src/App.tsx` (adicionar useTokenRefresh)
- [ ] Testar login flow completo
- [ ] Testar auto-refresh (esperar 55min ou mock)

---

### Fase 3: Auditoria Centralizada (2 dias) 🟡 ALTO

#### Dia 4: Banco de Dados PostgreSQL

**Criar modelos de auditoria:**

```go
// internal/audit/models.go (NOVO)
package audit

import "time"

type AuditLog struct {
	ID           int64     `db:"id" json:"id"`
	Timestamp    time.Time `db:"timestamp" json:"timestamp"`
	UserEmail    string    `db:"user_email" json:"user_email"`
	UserName     string    `db:"user_name" json:"user_name"`
	UserGroups   string    `db:"user_groups" json:"user_groups"` // JSON array
	IPAddress    string    `db:"ip_address" json:"ip_address"`
	Action       string    `db:"action" json:"action"`
	Resource     string    `db:"resource" json:"resource"`
	Cluster      string    `db:"cluster" json:"cluster"`
	Namespace    string    `db:"namespace" json:"namespace"`
	BeforeState  string    `db:"before_state" json:"before_state"`   // JSON
	AfterState   string    `db:"after_state" json:"after_state"`     // JSON
	Status       string    `db:"status" json:"status"`               // success|failed
	ErrorMessage string    `db:"error_message" json:"error_message"` // Null se success
	DurationMs   int64     `db:"duration_ms" json:"duration_ms"`
}
```

**Criar schema PostgreSQL:**

```sql
-- internal/audit/schema.sql (NOVO)
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_email VARCHAR(255) NOT NULL,
    user_name VARCHAR(255) NOT NULL,
    user_groups JSONB NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource VARCHAR(500) NOT NULL,
    cluster VARCHAR(100) NOT NULL,
    namespace VARCHAR(100),
    before_state JSONB,
    after_state JSONB,
    status VARCHAR(20) NOT NULL,
    error_message TEXT,
    duration_ms BIGINT
);

-- Índices para queries rápidas
CREATE INDEX idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_logs_user_email ON audit_logs(user_email);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_cluster ON audit_logs(cluster);
CREATE INDEX idx_audit_logs_status ON audit_logs(status);

-- Retenção de 1 ano (opcional, via cron)
-- DELETE FROM audit_logs WHERE timestamp < NOW() - INTERVAL '1 year';
```

**Implementar writer PostgreSQL:**

```go
// internal/audit/postgres.go (NOVO)
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

type PostgresWriter struct {
	db *sql.DB
}

func NewPostgresWriter(dsn string) (*PostgresWriter, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresWriter{db: db}, nil
}

func (w *PostgresWriter) Write(ctx context.Context, log AuditLog) error {
	query := `
		INSERT INTO audit_logs (
			timestamp, user_email, user_name, user_groups, ip_address,
			action, resource, cluster, namespace, before_state, after_state,
			status, error_message, duration_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	userGroupsJSON, _ := json.Marshal(log.UserGroups)

	_, err := w.db.ExecContext(ctx, query,
		log.Timestamp,
		log.UserEmail,
		log.UserName,
		string(userGroupsJSON),
		log.IPAddress,
		log.Action,
		log.Resource,
		log.Cluster,
		log.Namespace,
		log.BeforeState,
		log.AfterState,
		log.Status,
		log.ErrorMessage,
		log.DurationMs,
	)

	return err
}

func (w *PostgresWriter) Query(ctx context.Context, filters AuditFilters) ([]AuditLog, error) {
	// Implementar queries com filtros
	// Ex: SELECT * FROM audit_logs WHERE user_email = ? AND timestamp > ?
	// ...
	return nil, nil
}

func (w *PostgresWriter) Close() error {
	return w.db.Close()
}
```

**Implementar writer assíncrono:**

```go
// internal/audit/writer.go (NOVO)
package audit

import (
	"context"
	"log"
	"sync"
)

type Writer interface {
	Write(ctx context.Context, log AuditLog) error
	Close() error
}

type AsyncWriter struct {
	writers []Writer
	queue   chan AuditLog
	wg      sync.WaitGroup
}

func NewAsyncWriter(writers ...Writer) *AsyncWriter {
	w := &AsyncWriter{
		writers: writers,
		queue:   make(chan AuditLog, 1000), // Buffer de 1000 logs
	}

	// Iniciar workers
	for i := 0; i < 3; i++ {
		w.wg.Add(1)
		go w.worker()
	}

	return w
}

func (w *AsyncWriter) Write(log AuditLog) {
	// Não bloqueia (assíncrono)
	select {
	case w.queue <- log:
		// Log enfileirado
	default:
		// Queue cheia, log warning
		log.Println("⚠️ Audit queue full, dropping log")
	}
}

func (w *AsyncWriter) worker() {
	defer w.wg.Done()

	for log := range w.queue {
		// Escrever em todos os writers em paralelo
		for _, writer := range w.writers {
			go func(wr Writer) {
				if err := wr.Write(context.Background(), log); err != nil {
					log.Printf("❌ Failed to write audit log: %v", err)
				}
			}(writer)
		}
	}
}

func (w *AsyncWriter) Close() error {
	close(w.queue)
	w.wg.Wait()

	for _, writer := range w.writers {
		writer.Close()
	}

	return nil
}
```

**Integrar com HistoryTracker:**

```go
// internal/history/tracker.go (ATUALIZADO)
package history

import (
	"k8s-hpa-manager/internal/audit"
	"github.com/gin-gonic/gin"
)

type HistoryTracker struct {
	// ... campos existentes
	auditWriter *audit.AsyncWriter // ⭐ NOVO
}

func (h *HistoryTracker) Track(c *gin.Context, action string, resource string, before, after interface{}) error {
	// 1. Extrair user info do JWT (já no contexto)
	userEmail, _ := c.Get("user_email")
	userName, _ := c.Get("user_name")
	userGroups, _ := c.Get("user_groups")
	ipAddress := c.ClientIP()

	// 2. Criar entrada de auditoria
	auditLog := audit.AuditLog{
		Timestamp:  time.Now(),
		UserEmail:  userEmail.(string),
		UserName:   userName.(string),
		UserGroups: fmt.Sprintf("%v", userGroups), // Converter para JSON
		IPAddress:  ipAddress,
		Action:     action,
		Resource:   resource,
		// ... resto dos campos
	}

	// 3. Escrever de forma assíncrona
	h.auditWriter.Write(auditLog)

	// 4. Também salvar localmente (para UI rápida - opcional)
	h.writeLocalHistory(auditLog)

	return nil
}
```

**Tarefas Dia 4:**

- [ ] Instalar dependência PostgreSQL: `go get github.com/lib/pq`
- [ ] Criar `internal/audit/models.go`
- [ ] Criar schema SQL `internal/audit/schema.sql`
- [ ] Implementar `internal/audit/postgres.go`
- [ ] Implementar `internal/audit/writer.go` (async)
- [ ] Integrar com `internal/history/tracker.go`
- [ ] Configurar PostgreSQL (Docker ou managed service)
- [ ] Testar escrita de logs

#### Dia 5: UI de Auditoria

**Criar página de visualização:**

```typescript
// internal/web/frontend/src/pages/AuditViewer.tsx (NOVO)
import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";

interface AuditLog {
  id: number;
  timestamp: string;
  user_email: string;
  user_name: string;
  ip_address: string;
  action: string;
  resource: string;
  cluster: string;
  status: "success" | "failed";
}

export const AuditViewer = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [filters, setFilters] = useState({
    user_email: "",
    action: "",
    cluster: "",
    start_date: "",
    end_date: "",
  });

  const fetchLogs = async () => {
    const params = new URLSearchParams(filters);
    const data = await apiClient.get<AuditLog[]>(`/audit?${params}`);
    setLogs(data);
  };

  useEffect(() => {
    fetchLogs();
  }, []);

  const exportCSV = () => {
    // Gerar CSV com user info
    const csv = [
      ["Timestamp", "User", "Email", "Action", "Resource", "Cluster", "Status"].join(","),
      ...logs.map((log) =>
        [
          log.timestamp,
          log.user_name,
          log.user_email,
          log.action,
          log.resource,
          log.cluster,
          log.status,
        ].join(",")
      ),
    ].join("\n");

    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `audit-logs-${new Date().toISOString()}.csv`;
    a.click();
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Audit Logs</CardTitle>
      </CardHeader>
      <CardContent>
        {/* Filtros */}
        <div className="grid grid-cols-3 gap-4 mb-4">
          <Input
            placeholder="Filtrar por usuário"
            value={filters.user_email}
            onChange={(e) => setFilters({ ...filters, user_email: e.target.value })}
          />
          <Input
            placeholder="Filtrar por ação"
            value={filters.action}
            onChange={(e) => setFilters({ ...filters, action: e.target.value })}
          />
          <Input
            placeholder="Filtrar por cluster"
            value={filters.cluster}
            onChange={(e) => setFilters({ ...filters, cluster: e.target.value })}
          />
        </div>

        <div className="flex gap-2 mb-4">
          <Button onClick={fetchLogs}>Buscar</Button>
          <Button onClick={exportCSV} variant="outline">
            Exportar CSV
          </Button>
        </div>

        {/* Tabela de logs */}
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left p-2">Timestamp</th>
                <th className="text-left p-2">Usuário</th>
                <th className="text-left p-2">Email</th>
                <th className="text-left p-2">IP</th>
                <th className="text-left p-2">Ação</th>
                <th className="text-left p-2">Recurso</th>
                <th className="text-left p-2">Cluster</th>
                <th className="text-left p-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log) => (
                <tr key={log.id} className="border-b hover:bg-muted/50">
                  <td className="p-2">{new Date(log.timestamp).toLocaleString()}</td>
                  <td className="p-2">{log.user_name}</td>
                  <td className="p-2">{log.user_email}</td>
                  <td className="p-2">{log.ip_address}</td>
                  <td className="p-2">
                    <code className="bg-muted px-1 py-0.5 rounded">{log.action}</code>
                  </td>
                  <td className="p-2">{log.resource}</td>
                  <td className="p-2">{log.cluster}</td>
                  <td className="p-2">
                    <span
                      className={`px-2 py-1 rounded text-xs ${
                        log.status === "success"
                          ? "bg-green-100 text-green-800"
                          : "bg-red-100 text-red-800"
                      }`}
                    >
                      {log.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
};
```

**Criar endpoint de auditoria:**

```go
// internal/web/handlers/audit.go (NOVO)
package handlers

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/audit"
)

type AuditHandler struct {
	postgresWriter *audit.PostgresWriter
}

func NewAuditHandler(postgresWriter *audit.PostgresWriter) *AuditHandler {
	return &AuditHandler{
		postgresWriter: postgresWriter,
	}
}

// GET /api/v1/audit - Listar logs de auditoria
func (h *AuditHandler) List(c *gin.Context) {
	filters := audit.AuditFilters{
		UserEmail: c.Query("user_email"),
		Action:    c.Query("action"),
		Cluster:   c.Query("cluster"),
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
	}

	logs, err := h.postgresWriter.Query(c.Request.Context(), filters)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to query audit logs"})
		return
	}

	c.JSON(200, logs)
}
```

**Tarefas Dia 5:**

- [ ] Criar `src/pages/AuditViewer.tsx`
- [ ] Criar `internal/web/handlers/audit.go`
- [ ] Adicionar rota `GET /api/v1/audit` no `server.go`
- [ ] Implementar filtros de query no PostgreSQL
- [ ] Testar visualização de logs
- [ ] Testar exportação CSV

---

### Fase 4: Deploy e Testes (1 dia) 🔴 CRÍTICO

#### Dia 6: Deployment em Produção

**Azure App Registration:**

1. Acessar **Azure Portal** → **Azure Active Directory** → **App registrations**
2. Criar novo app:
   - **Name:** k8s-hpa-manager
   - **Redirect URI:** `https://k8s-hpa.empresa.com/auth/callback`
3. Configurar permissões:
   - `openid`, `profile`, `email`, `User.Read`
4. Grant admin consent
5. Criar client secret (validade: 24 meses)
6. Copiar: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`

**Configuração do Servidor:**

```bash
# .env ou variáveis de ambiente
AZURE_TENANT_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
AZURE_CLIENT_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
AZURE_CLIENT_SECRET="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
AZURE_REDIRECT_URL="https://k8s-hpa.empresa.com/auth/callback"
DATABASE_URL="postgres://user:pass@host:5432/k8s_hpa_audit?sslmode=require"

# Opcional: Azure Log Analytics
AZURE_LOG_ANALYTICS_WORKSPACE_ID="..."
AZURE_LOG_ANALYTICS_KEY="..."
```

**Nginx/Reverse Proxy:**

```nginx
# /etc/nginx/sites-available/k8s-hpa.conf
server {
    listen 443 ssl http2;
    server_name k8s-hpa.empresa.com;

    ssl_certificate /etc/letsencrypt/live/k8s-hpa.empresa.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/k8s-hpa.empresa.com/privkey.pem;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support (se necessário)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}

# Redirecionar HTTP → HTTPS
server {
    listen 80;
    server_name k8s-hpa.empresa.com;
    return 301 https://$host$request_uri;
}
```

**Systemd Service:**

```ini
# /etc/systemd/system/k8s-hpa-manager.service
[Unit]
Description=K8s HPA Manager Web Server
After=network.target postgresql.service

[Service]
Type=simple
User=k8s-hpa
WorkingDirectory=/opt/k8s-hpa-manager
EnvironmentFile=/opt/k8s-hpa-manager/.env
ExecStart=/opt/k8s-hpa-manager/bin/new-k8s-hpa web --port 8080
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**Checklist de Deploy:**

- [ ] **Azure AD:**
  - [ ] App registration criado
  - [ ] Redirect URL configurado
  - [ ] Permissões concedidas
  - [ ] Client secret criado e salvo
- [ ] **Servidor:**
  - [ ] Variáveis de ambiente configuradas
  - [ ] PostgreSQL instalado e schema criado
  - [ ] SSL/TLS certificate configurado
  - [ ] Nginx/reverse proxy configurado
  - [ ] Systemd service configurado
  - [ ] Firewall configurado (portas 80, 443)
- [ ] **DNS:**
  - [ ] DNS apontando para servidor
  - [ ] Certificado válido (Let's Encrypt)
- [ ] **Build:**
  - [ ] `make build` executado
  - [ ] `make web-build` executado
  - [ ] Binário copiado para `/opt/k8s-hpa-manager/bin/`
  - [ ] Static files copiados para `/opt/k8s-hpa-manager/static/`

**Testes de Integração:**

- [ ] **Login:**
  - [ ] Acessar `https://k8s-hpa.empresa.com`
  - [ ] Clicar "Login com Microsoft"
  - [ ] Login com MFA funciona
  - [ ] Redireciona para dashboard
- [ ] **RBAC:**
  - [ ] Usuário SRE consegue criar/editar/deletar
  - [ ] Usuário não-SRE só visualiza (403 Forbidden)
  - [ ] Flag `--ad` de bypass funciona (emergências)
- [ ] **Auditoria:**
  - [ ] Logs aparecem no PostgreSQL
  - [ ] Logs contêm email, nome, IP do usuário
  - [ ] Filtros funcionam
  - [ ] Exportar CSV funciona
- [ ] **Auto-refresh:**
  - [ ] Deixar aberto por 1h+
  - [ ] Token renova automaticamente (sem logout)
- [ ] **Performance:**
  - [ ] Múltiplos usuários simultâneos (10+)
  - [ ] Latência aceitável (<500ms)

---

## Refatoração do RBAC

### Resumo das Mudanças

| Arquivo | Mudança | Motivo |
|---------|---------|--------|
| `internal/rbac/azure_ad.go` | **Refatorar** - Ler do `gin.Context` ao invés de Azure CLI | RBAC atual não funciona em servidor |
| `internal/rbac/azure_ad_test.go` | **Atualizar** - Testes com JWT mocks | Validar nova implementação |
| `internal/web/middleware/rbac.go` | **Atualizar** - Passar `gin.Context` | Compatibilidade com novo RBAC |
| `internal/web/middleware/auth.go` | **Deprecar** - Token estático | Substituído por JWT |

### Código Detalhado

Veja **Fase 1 - Dia 2** para código completo.

---

## Sistema de Auditoria Centralizada

### Opções de Armazenamento

#### Opção 1: PostgreSQL (Recomendado)

**Vantagens:**
- ✅ Queries SQL poderosas (filtros complexos)
- ✅ Backup automático (pg_dump)
- ✅ Retenção configurável (1 ano+)
- ✅ Custo zero (self-hosted) ou baixo (managed)

**Desvantagens:**
- ⚠️ Requer manutenção (self-hosted)

**Estimativa de Volume:**
```
Usuários: 50
Ações por dia por usuário: 20
Tamanho médio por log: 1 KB

Volume diário: 50 × 20 × 1 KB = 1 MB/dia
Volume anual: 1 MB × 365 = 365 MB/ano
```

**Conclusão:** Volume baixo, PostgreSQL handle com facilidade.

---

#### Opção 2: Azure Log Analytics (Cloud)

**Vantagens:**
- ✅ Totalmente gerenciado (zero manutenção)
- ✅ Integração nativa Azure AD
- ✅ Retenção longa (90+ dias padrão)
- ✅ Queries KQL (Kusto)
- ✅ Alertas automáticos

**Desvantagens:**
- ⚠️ Custo: ~$2.30/GB ingested + $0.10/GB retention
- ⚠️ Vendor lock-in (Azure)

**Estimativa de Custo:**
```
Volume anual: 365 MB = 0.365 GB
Custo ingest: 0.365 GB × $2.30 = $0.84/ano
Custo retention (1 ano): 0.365 GB × $0.10 × 12 = $0.44/ano
TOTAL: ~$1.30/ano
```

**Conclusão:** Custo irrisório para empresa, vale a pena pela praticidade.

---

#### Opção 3: Sistema Híbrido (Melhor dos 2 Mundos)

**Arquitetura:**
```
Aplicação
    ↓
AsyncWriter
    ├── PostgreSQL (local, UI rápida)
    └── Azure Log Analytics (cloud, auditoria imutável)
```

**Vantagens:**
- ✅ UI rápida (lê de PostgreSQL local)
- ✅ Auditoria segura (Azure imutável)
- ✅ Redundância (2 cópias)

**Recomendação Final:** **Sistema Híbrido** (PostgreSQL + Azure Log Analytics).

---

## Segurança e Compliance

### 1. Proteção de Secrets

```bash
# ❌ NUNCA:
export AZURE_CLIENT_SECRET="abc123..."

# ✅ Azure Key Vault:
az keyvault secret set --vault-name k8s-hpa-vault --name azure-client-secret --value "abc123..."
```

```go
// internal/config/secrets.go
import "github.com/Azure/azure-sdk-for-go/sdk/keyvault/azsecrets"

func GetSecret(name string) (string, error) {
    client, _ := azsecrets.NewClient(vaultURL, cred, nil)
    secret, _ := client.GetSecret(ctx, name, "", nil)
    return *secret.Value, nil
}
```

---

### 2. Rate Limiting

```go
import "github.com/ulule/limiter/v3"
import "github.com/ulule/limiter/v3/drivers/middleware/gin"

// Limite: 5 tentativas de login por minuto por IP
rate := limiter.Rate{Limit: 5, Period: 1 * time.Minute}
store := memory.NewStore()
middleware := ginlimiter.NewMiddleware(limiter.New(store, rate))

router.Use(middleware)
```

---

### 3. HTTPS Obrigatório

```go
func (s *Server) Start() error {
    // HTTP redirect
    go http.ListenAndServe(":80", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
    }))

    // HTTPS server
    return s.router.RunTLS(":443", "/etc/letsencrypt/live/k8s-hpa.empresa.com/fullchain.pem",
                                    "/etc/letsencrypt/live/k8s-hpa.empresa.com/privkey.pem")
}
```

---

### 4. Session Inactivity Timeout

```go
const INACTIVITY_TIMEOUT = 30 * time.Minute

func ActivityMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Ler last_activity do cookie ou session store
        lastActivity, _ := getLastActivity(c)

        if time.Since(lastActivity) > INACTIVITY_TIMEOUT {
            c.JSON(401, gin.H{"error": "Session expired due to inactivity"})
            c.Abort()
            return
        }

        // Atualizar last_activity
        setLastActivity(c, time.Now())
        c.Next()
    }
}
```

---

## Checklist de Implementação

### Backend

#### Fase 1: SSO (2 dias)

- [ ] **Dependências**
  - [ ] `go get github.com/coreos/go-oidc/v3/oidc`
  - [ ] `go get golang.org/x/oauth2`
- [ ] **Módulo Auth**
  - [ ] Criar `internal/web/auth/oidc.go`
  - [ ] Criar `internal/web/auth/handlers.go`
  - [ ] Criar `internal/web/auth/middleware.go`
  - [ ] Adicionar rotas no `server.go`
- [ ] **Refatorar RBAC**
  - [ ] Atualizar `internal/rbac/azure_ad.go` (usar JWT)
  - [ ] Atualizar `internal/rbac/azure_ad_test.go` (mocks)
  - [ ] Atualizar `internal/web/middleware/rbac.go`
  - [ ] Executar `go test ./internal/rbac -v`
- [ ] **Testes Manuais**
  - [ ] Login flow funciona
  - [ ] JWT validado corretamente
  - [ ] RBAC funciona com JWT

#### Fase 3: Auditoria (2 dias)

- [ ] **PostgreSQL**
  - [ ] `go get github.com/lib/pq`
  - [ ] Criar `internal/audit/models.go`
  - [ ] Criar `internal/audit/postgres.go`
  - [ ] Criar schema SQL
  - [ ] Testar conexão
- [ ] **Async Writer**
  - [ ] Criar `internal/audit/writer.go`
  - [ ] Integrar com `internal/history/tracker.go`
  - [ ] Testar escrita assíncrona
- [ ] **Handler**
  - [ ] Criar `internal/web/handlers/audit.go`
  - [ ] Adicionar rota `GET /api/v1/audit`

---

### Frontend

#### Fase 2: SSO (1 dia)

- [ ] **Login**
  - [ ] Refatorar `src/pages/Login.tsx`
  - [ ] Remover campo de token
  - [ ] Adicionar botão "Login com Microsoft"
- [ ] **Auto-Refresh**
  - [ ] Criar `src/hooks/useTokenRefresh.ts`
  - [ ] Integrar em `src/App.tsx`
- [ ] **API Client**
  - [ ] Atualizar `src/lib/api/client.ts`
  - [ ] Remover `setToken()`, `clearToken()`
  - [ ] Adicionar `credentials: "include"`
- [ ] **Testes Manuais**
  - [ ] Login flow funciona
  - [ ] Cookies salvos corretamente
  - [ ] Auto-refresh funciona (55min)

#### Fase 3: Auditoria UI (1 dia)

- [ ] **Audit Viewer**
  - [ ] Criar `src/pages/AuditViewer.tsx`
  - [ ] Implementar filtros
  - [ ] Implementar paginação
  - [ ] Implementar exportação CSV
- [ ] **Integração**
  - [ ] Adicionar rota `/audit` no menu
  - [ ] Testar filtros
  - [ ] Testar exportação

---

### Deploy

#### Fase 4: Produção (1 dia)

- [ ] **Azure AD**
  - [ ] Criar app registration
  - [ ] Configurar redirect URL
  - [ ] Conceder permissões
  - [ ] Criar client secret
- [ ] **Servidor**
  - [ ] Configurar variáveis de ambiente
  - [ ] Instalar PostgreSQL
  - [ ] Criar schema
  - [ ] Configurar Nginx/reverse proxy
  - [ ] Configurar SSL/TLS
  - [ ] Criar systemd service
- [ ] **DNS**
  - [ ] Apontar DNS para servidor
  - [ ] Verificar SSL válido
- [ ] **Build**
  - [ ] `make build`
  - [ ] `make web-build`
  - [ ] Copiar binário para servidor
  - [ ] Copiar static files
- [ ] **Testes**
  - [ ] Login funciona
  - [ ] RBAC funciona
  - [ ] Auditoria funciona
  - [ ] Auto-refresh funciona
  - [ ] Performance OK (10+ usuários)

---

## Estimativa de Esforço

### Por Fase

| Fase | Descrição | Esforço | Risco | Prioridade |
|------|-----------|---------|-------|------------|
| **1** | SSO Backend | 2 dias | Médio | 🔴 Crítico |
| **2** | SSO Frontend | 1 dia | Baixo | 🔴 Crítico |
| **3** | Auditoria DB | 2 dias | Médio | 🟡 Alto |
| **4** | Deploy/Testes | 1 dia | Alto | 🔴 Crítico |
| **TOTAL** | | **6 dias úteis** | | |

### Por Desenvolvedor

- **1 dev full-time:** 6 dias úteis (1.5 semanas)
- **2 devs (backend + frontend):** 3 dias úteis (paralelo)

### Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| Azure AD config errado | Média | Alto | Seguir checklist, testar em dev primeiro |
| RBAC quebra após refactor | Média | Alto | Testes unitários completos, testes manuais |
| PostgreSQL performance | Baixa | Médio | Índices corretos, queries otimizadas |
| SSL/TLS problemas | Baixa | Alto | Usar Let's Encrypt, testar antes do deploy |
| Usuários não conseguem login | Média | Crítico | Manter flag `--ad` bypass para emergências |

---

## Conclusão

### Por Que SSO é Obrigatório

1. ✅ **RBAC atual NÃO funciona** em servidor centralizado
2. ✅ **Auditoria local insegura** (pode ser apagada)
3. ✅ **Compliance corporativo** exige rastreabilidade
4. ✅ **Sem TUI**, foco 100% Web (SSO perfeito para isso)
5. ✅ **DNS resolve** problema de callback URL

### Benefícios Imediatos

- ✅ Login individual para cada usuário
- ✅ RBAC funciona corretamente (grupos do JWT)
- ✅ Auditoria completa e imutável
- ✅ MFA nativo do Azure AD
- ✅ Sessões longas (90 dias) com auto-refresh
- ✅ Revogação remota de acessos

### Próximos Passos

1. **Aprovação do roadmap** (este documento)
2. **Criar app registration no Azure AD** (15 minutos)
3. **Iniciar Fase 1** (SSO Backend - 2 dias)
4. **Continuar sequencialmente** até deploy

---

**Documento preparado em:** 11 de dezembro de 2025
**Versão:** 1.0
**Status:** 📋 Aguardando aprovação para implementação
**Esforço estimado:** 6 dias úteis (1.5 semanas)
**Prioridade:** 🔴 Crítico (bloqueador para servidor centralizado)
