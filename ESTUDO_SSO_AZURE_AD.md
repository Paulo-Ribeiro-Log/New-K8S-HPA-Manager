# Estudo de Implementação SSO com Azure AD

**Data:** 10 de dezembro de 2025  
**Projeto:** K8s HPA Manager  
**Objetivo:** Análise e proposta de implementação de Single Sign-On (SSO) via Azure AD

---

## 📋 Índice

1. [Visão Geral](#visão-geral)
2. [Situação Atual](#situação-atual)
3. [Proposta: SSO Básico com Azure AD](#proposta-sso-básico-com-azure-ad)
4. [Comparação Detalhada](#comparação-detalhada)
5. [Implementação Técnica](#implementação-técnica)
6. [Auditoria e Compliance](#auditoria-e-compliance)
7. [Compatibilidade com Provedores K8s](#compatibilidade-com-provedores-k8s)
8. [Roadmap de Implementação](#roadmap-de-implementação)

---

## Visão Geral

Este documento analisa a implementação de Single Sign-On (SSO) via Azure AD para a aplicação K8s HPA Manager, comparando o método atual de autenticação com token estático contra uma solução moderna baseada em OAuth 2.0 / OIDC.

### Principais Benefícios do SSO

- ✅ **Auto-renovação de tokens** (transparente para o usuário)
- ✅ **Identificação individual** de usuários
- ✅ **Auditoria completa** (quem, quando, de onde)
- ✅ **MFA nativo** do Azure AD
- ✅ **Revogação remota** de acessos
- ✅ **Sessões longas** (90 dias) com refresh automático

---

## Situação Atual

### 🔴 Autenticação com Token Estático

#### Configuração

```bash
# Definir token via variável de ambiente
export K8S_HPA_WEB_TOKEN="poc-token-123"

# Iniciar servidor
k8s-hpa-manager web --port 8080
```

#### Fluxo de Autenticação

```
┌─────────────────────────────────────────────────────────────┐
│ 1. CONFIGURAÇÃO INICIAL                                     │
└─────────────────────────────────────────────────────────────┘
$ export K8S_HPA_WEB_TOKEN="poc-token-123"
$ k8s-hpa-manager web --port 8080

┌─────────────────────────────────────────────────────────────┐
│ 2. PRIMEIRO ACESSO                                          │
└─────────────────────────────────────────────────────────────┘
Usuário:
  ↓ Abre http://localhost:8080/login
  ↓ Digita: "poc-token-123"
  ↓ Clica "Login"
  ↓
Frontend valida contra /api/v1/clusters
  ↓ Header: Authorization: Bearer poc-token-123
  ↓
Backend: middleware/auth.go
  ↓ Compara string: token == "poc-token-123" ✓
  ↓
✅ Acesso liberado → localStorage['auth_token'] = "poc-token-123"

┌─────────────────────────────────────────────────────────────┐
│ 3. USANDO A APLICAÇÃO                                       │
└─────────────────────────────────────────────────────────────┘
Toda requisição:
  GET /api/v1/clusters
  Header: Authorization: Bearer poc-token-123
           ↓
  Backend valida token (comparação string)
           ↓
  ✅ Retorna dados
```

#### Código Atual

**Frontend (Login.tsx):**
```typescript
export const Login = ({ onLogin }: LoginProps) => {
  const [token, setToken] = useState("poc-token-123"); // Default POC token
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      // Set token in API client
      apiClient.setToken(token);

      // Test token by fetching clusters
      const response = await fetch("/api/v1/clusters", {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        throw new Error("Invalid token");
      }

      onLogin();
    } catch (err) {
      setError("Authentication failed. Please check your token.");
      apiClient.clearToken();
    } finally {
      setLoading(false);
    }
  };
  // ...
};
```

**Backend (middleware/auth.go):**
```go
// AuthMiddleware valida o token Bearer no header Authorization
func AuthMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "UNAUTHORIZED",
					"message": "No authorization header provided",
				},
			})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_AUTH_FORMAT",
					"message": "Authorization header must be 'Bearer <token>'",
				},
			})
			c.Abort()
			return
		}

		if parts[1] != token {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_TOKEN",
					"message": "Invalid authentication token",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
```

#### Problemas Identificados

| Problema | Impacto | Severidade |
|----------|---------|------------|
| Token nunca expira | Risco de segurança | 🔴 Alto |
| Token compartilhado | Impossível identificar usuário | 🔴 Alto |
| Sem auditoria de usuário | Não compliance | 🔴 Alto |
| Sem MFA | Vulnerável a roubo de credenciais | 🟡 Médio |
| Renovação manual | Má experiência do usuário | 🟡 Médio |
| Revogação impossível | Token vazado = comprometimento | 🔴 Alto |

---

## Proposta: SSO Básico com Azure AD

### 🟢 Autenticação OAuth 2.0 / OIDC

#### Configuração

```bash
# Configurar Azure AD (uma vez)
export AZURE_CLIENT_ID="abc123..."
export AZURE_TENANT_ID="xyz789..."

# Iniciar servidor com SSO habilitado
k8s-hpa-manager web --port 8080 --sso-enabled
```

#### Fluxo de Autenticação

```
┌─────────────────────────────────────────────────────────────┐
│ 1. CONFIGURAÇÃO INICIAL (Uma vez)                           │
└─────────────────────────────────────────────────────────────┘
$ export AZURE_CLIENT_ID="abc123..."
$ export AZURE_TENANT_ID="xyz789..."
$ k8s-hpa-manager web --port 8080 --sso-enabled

Logs:
✅ SSO habilitado via Azure AD
✅ Callback URL: http://localhost:8080/auth/callback
✅ Login URL: http://localhost:8080/auth/login

┌─────────────────────────────────────────────────────────────┐
│ 2. PRIMEIRO ACESSO (Experiência do Usuário)                 │
└─────────────────────────────────────────────────────────────┘
Usuário:
  ↓ Abre http://localhost:8080
  ↓ Clica "Login with Microsoft"
  ↓
  ↓ Browser redireciona para:
  ↓ https://login.microsoftonline.com/...
  ↓
Azure AD:
  ↓ Digite seu email corporativo
  ↓ Digite sua senha
  ↓ [MFA se habilitado] - código do celular
  ↓
  ↓ Callback: http://localhost:8080/auth/callback?code=abc123...
  ↓
Backend (Go):
  ↓ Recebe código temporário
  ↓ Troca código por tokens no Azure AD:
       • access_token (válido 1h)
       • refresh_token (válido 90 dias)
       • id_token (JWT com user info)
  ↓
  ↓ Valida tokens
  ↓ Extrai informações: email, nome, grupos
  ↓
✅ Redireciona para dashboard
✅ localStorage['access_token'] = "eyJhbG..."  // JWT
✅ localStorage['refresh_token'] = "eyJhbG..."  // Refresh

┌─────────────────────────────────────────────────────────────┐
│ 3. USANDO A APLICAÇÃO (Transparente)                        │
└─────────────────────────────────────────────────────────────┘
08:00 - Você começa a trabalhar:
  GET /api/v1/clusters
  Header: Authorization: Bearer eyJhbG...  // JWT válido
           ↓
  Backend valida JWT:
    • Verifica assinatura com Azure AD
    • Verifica expiração (válido até 09:00)
    • Extrai claims (email, grupos, etc)
           ↓
  ✅ Retorna dados
  
08:55 - Token vai expirar em 5 minutos:
  Frontend detecta (token_expiry - now < 5min)
           ↓
  Automatic refresh em background:
    POST /auth/refresh
    Body: { refresh_token: "eyJhbG..." }
           ↓
  Backend valida refresh_token no Azure AD
           ↓
  Azure AD retorna novo access_token (válido até 09:55)
           ↓
  ✅ localStorage atualizado
  ✅ Você NEM PERCEBE - continua trabalhando

12:00 - Almoço (inativo por 1h):
  ✅ Token válido - auto-refresh às 12:55

18:00 - Fim do dia (fecha browser):
  ✅ Tokens salvos em localStorage

DIA SEGUINTE - 08:00:
  Você abre a aplicação:
    • Refresh token ainda válido (90 dias)
    • Auto-refresh do access token
  ✅ LOGIN AUTOMÁTICO - vai direto pro dashboard
```

#### Estrutura do Token JWT

```json
{
  "sub": "joao.silva@empresa.com",
  "name": "João Silva",
  "email": "joao.silva@empresa.com",
  "oid": "abc123-def456",
  "groups": ["DevOps", "SRE"],
  "roles": ["admin"],
  "iat": 1702123456,
  "exp": 1702127056
}
```

---

## Comparação Detalhada

### Tabela Comparativa

| Aspecto | 🔴 Token Estático (Atual) | 🟢 SSO Azure AD |
|---------|---------------------------|-----------------|
| **Login Inicial** | Digite token manualmente | Clique "Login with Microsoft" |
| **Duração da Sessão** | Infinita (ou manual) | 90 dias (auto-refresh) |
| **Expiração de Token** | ❌ Nunca ou manual | ✅ 1h (renova automaticamente) |
| **Renovação** | ❌ Manual (relogin) | ✅ Automática (transparente) |
| **Experiência** | Ruim (relogin frequente) | Excelente (invisível) |
| **Segurança** | ⚠️ Token fixo compartilhado | ✅ Token individual JWT |
| **MFA** | ❌ Não | ✅ Sim (Azure AD) |
| **Auditoria** | ❌ Impossível | ✅ Completa (Azure AD logs) |
| **Revogação** | ❌ Impossível | ✅ Sim (Azure AD) |
| **Múltiplos Usuários** | ⚠️ Mesmo token | ✅ Token individual |
| **Inatividade** | ❌ Sem controle | ✅ Logout automático configurável |
| **Complexidade Setup** | ✅ Simples (var env) | ⚠️ Requer App Registration Azure |
| **Compatibilidade K8s** | ✅ GKE/EKS/AKS (kubeconfig) | ✅ GKE/EKS/AKS (kubeconfig) |

### Impacto no Dia a Dia

#### Hoje (Token Estático)

```
06:00 - Chega no trabalho
        → Abre aplicação
        → Digite "poc-token-123"
        → Login

09:00 - Token ainda válido (nunca expira)
        ✅ Trabalhando normalmente

12:00 - Almoço
        → Aplicação aberta

15:00 - Reunião (fecha browser)

16:00 - Volta da reunião
        → Abre aplicação
        ❌ Precisa logar de novo (localStorage limpo)
        → Digite "poc-token-123" NOVAMENTE

19:00 - Fim do dia
```

#### Com SSO (Proposto)

```
06:00 - Chega no trabalho
        → Abre aplicação
        → Clica "Login with Microsoft"
        → [MFA] Código do celular
        ✅ Logado

09:00 - Token auto-refresh (você nem percebe)
        ✅ Trabalhando normalmente

12:00 - Almoço (inativo)
        ✅ Token válido

15:00 - Reunião (fecha browser)

16:00 - Volta da reunião
        → Abre aplicação
        ✅ JÁ ESTÁ LOGADO (refresh token válido)
        ✅ Vai direto pro dashboard

19:00 - Fim do dia

PRÓXIMOS 90 DIAS:
        → Abre aplicação
        ✅ LOGIN AUTOMÁTICO (enquanto refresh válido)
        → NEM PRECISA CLICAR NADA
```

---

## Implementação Técnica

### Backend (Go)

#### 1. Dependências

```bash
go get github.com/coreos/go-oidc/v3/oidc
go get golang.org/x/oauth2
```

#### 2. Estrutura de Arquivos

```
internal/
  web/
    auth/
      oidc.go          # Configuração OIDC
      middleware.go    # Middleware JWT validation
      handlers.go      # /auth/login, /auth/callback, /auth/refresh
```

#### 3. Código - OIDC Provider (auth/oidc.go)

```go
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
	redirectURL := os.Getenv("AZURE_REDIRECT_URL") // http://localhost:8080/auth/callback

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
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return &OIDCProvider{
		Provider:     provider,
		OAuth2Config: oauth2Config,
		Verifier:     verifier,
	}, nil
}
```

#### 4. Código - Auth Handlers (auth/handlers.go)

```go
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

	// Salvar state em sessão/cookie
	c.SetCookie("oauth_state", state, 600, "/", "", false, true)

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

	// Retornar tokens para o frontend
	c.JSON(200, gin.H{
		"access_token":  oauth2Token.AccessToken,
		"refresh_token": oauth2Token.RefreshToken,
		"id_token":      rawIDToken,
		"expires_in":    oauth2Token.Expiry.Unix(),
		"user": gin.H{
			"email":  claims.Email,
			"name":   claims.Name,
			"sub":    claims.Sub,
			"groups": claims.Groups,
		},
	})
}

// POST /auth/refresh - Renova access token usando refresh token
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	// Criar token source com refresh token
	tokenSource := h.oidcProvider.OAuth2Config.TokenSource(
		context.Background(),
		&oauth2.Token{RefreshToken: req.RefreshToken},
	)

	// Obter novo access token
	newToken, err := tokenSource.Token()
	if err != nil {
		c.JSON(401, gin.H{"error": "Failed to refresh token"})
		return
	}

	c.JSON(200, gin.H{
		"access_token": newToken.AccessToken,
		"expires_in":   newToken.Expiry.Unix(),
	})
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

#### 5. Código - JWT Middleware (auth/middleware.go)

```go
package auth

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

func JWTAuthMiddleware(oidcProvider *OIDCProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(401, gin.H{"error": "No authorization header"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{"error": "Invalid authorization format"})
			c.Abort()
			return
		}

		token := parts[1]

		// Validar JWT com Azure AD
		idToken, err := oidcProvider.Verifier.Verify(context.Background(), token)
		if err != nil {
			c.JSON(401, gin.H{"error": "Invalid token"})
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
			c.JSON(401, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		// Adicionar user info ao contexto
		c.Set("user_email", claims.Email)
		c.Set("user_name", claims.Name)
		c.Set("user_sub", claims.Sub)
		c.Set("user_groups", claims.Groups)

		c.Next()
	}
}
```

### Frontend (React/TypeScript)

#### 1. Login Component (pages/Login.tsx)

```typescript
import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Shield } from "lucide-react";
import { apiClient } from "@/lib/api/client";

interface LoginProps {
  onLogin: () => void;
}

export const Login = ({ onLogin }: LoginProps) => {
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Verificar se voltou do callback do Azure AD
    const params = new URLSearchParams(window.location.search);
    const accessToken = params.get("access_token");
    const refreshToken = params.get("refresh_token");

    if (accessToken && refreshToken) {
      // Salvar tokens
      localStorage.setItem("access_token", accessToken);
      localStorage.setItem("refresh_token", refreshToken);
      apiClient.setToken(accessToken);

      // Redirecionar para dashboard
      onLogin();
    }
  }, [onLogin]);

  const handleLoginWithMicrosoft = () => {
    setLoading(true);
    // Redirecionar para endpoint SSO
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
            Sign in with your Microsoft account
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button 
            onClick={handleLoginWithMicrosoft} 
            className="w-full"
            disabled={loading}
          >
            {loading ? "Redirecting..." : "Login with Microsoft"}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
};
```

#### 2. Auto-Refresh Hook (hooks/useTokenRefresh.ts)

```typescript
import { useEffect } from "react";
import { apiClient } from "@/lib/api/client";
import jwt_decode from "jwt-decode";

export const useTokenRefresh = () => {
  useEffect(() => {
    const refreshTokenBeforeExpiry = async () => {
      const token = localStorage.getItem("access_token");
      if (!token) return;

      try {
        const decoded: any = jwt_decode(token);
        const expiresIn = decoded.exp * 1000 - Date.now();

        // Renovar 5 minutos antes de expirar
        if (expiresIn < 5 * 60 * 1000) {
          console.log("🔄 Token expirando em breve, renovando...");

          const refreshToken = localStorage.getItem("refresh_token");
          if (!refreshToken) {
            console.log("❌ Sem refresh token, fazendo logout");
            apiClient.clearToken();
            window.location.href = "/login";
            return;
          }

          const response = await fetch("/auth/refresh", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
            },
            body: JSON.stringify({ refresh_token: refreshToken }),
          });

          if (!response.ok) {
            throw new Error("Failed to refresh token");
          }

          const data = await response.json();
          localStorage.setItem("access_token", data.access_token);
          apiClient.setToken(data.access_token);

          console.log("✅ Token renovado automaticamente");
        }
      } catch (error) {
        console.error("❌ Erro ao renovar token:", error);
        apiClient.clearToken();
        window.location.href = "/login";
      }
    };

    // Verificar a cada 1 minuto
    const interval = setInterval(refreshTokenBeforeExpiry, 60000);

    // Verificar imediatamente na montagem
    refreshTokenBeforeExpiry();

    return () => clearInterval(interval);
  }, []);
};
```

#### 3. Usar no App.tsx

```typescript
import { useTokenRefresh } from "./hooks/useTokenRefresh";

const App = () => {
  const [isAuthenticated, setIsAuthenticated] = useState(false);

  // Auto-refresh de tokens
  useTokenRefresh();

  // ... resto do código
};
```

---

## Auditoria e Compliance

### Situação Atual

#### Histórico Implementado (Local)

A aplicação já possui um sistema de auditoria local:

**Localização:**
- **Interface Web:** Header → Botão "History"
- **Disco:** `~/.k8s-hpa-manager/history/`
- **API:** `GET /api/v1/history`

**Exemplo de entrada:**

```json
{
  "id": "abc123-def456-ghi789",
  "timestamp": "2025-12-10T14:30:45Z",
  "action": "update_hpa",
  "resource": "production/api-backend",
  "cluster": "akspriv-prod",
  "before": {
    "min_replicas": 2,
    "max_replicas": 10
  },
  "after": {
    "min_replicas": 5,
    "max_replicas": 20
  },
  "status": "success",
  "duration_ms": 1234,
  "session_name": "scale-up-peak-hours"
}
```

**❌ Limitação:** Não identifica o usuário (todos usam mesmo token)

### Com SSO Azure AD

#### História Enriquecida com User Info

```json
{
  "id": "abc123-def456-ghi789",
  "timestamp": "2025-12-10T14:30:45Z",
  "action": "update_hpa",
  "resource": "production/api-backend",
  "cluster": "akspriv-prod",
  
  // ✅ NOVOS CAMPOS COM SSO
  "user_email": "joao.silva@empresa.com",
  "user_name": "João Silva",
  "user_id": "abc123-def456",
  "user_groups": ["DevOps", "SRE"],
  "ip_address": "192.168.1.100",
  
  "before": {
    "min_replicas": 2,
    "max_replicas": 10
  },
  "after": {
    "min_replicas": 5,
    "max_replicas": 20
  },
  "status": "success",
  "duration_ms": 1234,
  "session_name": "scale-up-peak-hours"
}
```

#### Azure AD Logs (Complementar)

**Azure Portal → Azure Active Directory → Sign-ins**

Você vê:
- ✅ Quem fez login (user@empresa.com)
- ✅ Quando (timestamp)
- ✅ De onde (IP, localização, device)
- ✅ Status (success/failed/MFA)
- ✅ Aplicação (k8s-hpa-manager)
- ✅ Duração da sessão
- ✅ MFA usado (SMS/App/Biometria)

**Retenção:** 30-90 dias (dependendo do plano Azure)

#### Comparação de Auditoria

| Aspecto | Atual | Com SSO |
|---------|-------|---------|
| **Ver histórico** | ✅ Sim (History Viewer) | ✅ Sim (History Viewer) |
| **Filtrar por ação** | ✅ Sim | ✅ Sim |
| **Filtrar por cluster** | ✅ Sim | ✅ Sim |
| **Filtrar por data** | ✅ Sim | ✅ Sim |
| **Exportar CSV** | ✅ Sim | ✅ Sim |
| **Ver before/after** | ✅ Sim | ✅ Sim |
| **Identificar usuário** | ❌ Não | ✅ Sim (email, nome, ID) |
| **Ver grupos do usuário** | ❌ Não | ✅ Sim (AD groups) |
| **Ver IP origem** | ❌ Não | ✅ Sim |
| **Logs de login** | ❌ Não | ✅ Sim (Azure AD) |
| **MFA tracking** | ❌ Não | ✅ Sim (Azure AD) |
| **Correlação login→ação** | ❌ Impossível | ✅ Possível |
| **Auditoria de acesso** | ❌ Não | ✅ Sim (Azure AD) |
| **Compliance ready** | ⚠️ Parcial | ✅ Completo |

---

## Compatibilidade com Provedores K8s

### Camadas de Autenticação

O SSO Azure AD funciona com **TODOS os provedores Kubernetes** porque existem duas camadas distintas:

```
┌─────────────────────────────────────────────────────────────┐
│ Camada 1: Autenticação da APLICAÇÃO WEB                     │
│ (Usuário → Frontend/Backend)                                │
│ → Azure AD, Google, AWS Cognito, etc.                       │
└─────────────────────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Camada 2: Autenticação com CLUSTERS K8S                     │
│ (Aplicação → GKE/EKS/AKS)                                   │
│ → kubeconfig, service accounts, OIDC                        │
└─────────────────────────────────────────────────────────────┘
```

### Compatibilidade por Provider

| Provider | SSO Web App | Kubeconfig (Atual) | OIDC com Azure AD | SSO Unificado |
|----------|-------------|-------------------|-------------------|---------------|
| **Google GKE** | ✅ Sim | ✅ Sim | ✅ Sim | ✅ Possível |
| **Amazon EKS** | ✅ Sim | ✅ Sim | ✅ Sim | ✅ Possível |
| **Azure AKS** | ✅ Sim | ✅ Sim | ✅ Nativo | ✅ Nativo |
| **On-Premise** | ✅ Sim | ✅ Sim | ✅ Sim | ✅ Possível |

### Opções de Integração

#### Opção 1: SSO Básico (Recomendado Inicialmente)

```
Usuário → Login Azure AD → Aplicação Web
                              ↓
                      Usa kubeconfig existente
                              ↓
                    GKE/EKS/AKS (como já faz hoje)
```

**Vantagens:**
- ✅ Implementação simples
- ✅ Funciona com **todos os clusters imediatamente**
- ✅ Não requer configuração nos clusters
- ✅ Sua app **JÁ ESTÁ PRONTA** para isso

**Desvantagens:**
- ⚠️ Ainda precisa gerenciar kubeconfig
- ⚠️ Dois sistemas de auth separados

#### Opção 2: SSO Completo com OIDC (Avançado)

```
Usuário → Login Azure AD → Token JWT
                              ↓
              ┌───────────────┴────────────────┐
              ↓                                ↓
      Aplicação Web                    Clusters K8s
      (autenticado)                    (OIDC → Azure AD)
```

**Vantagens:**
- ✅ **1 login = acesso a tudo**
- ✅ **RBAC centralizado no Azure AD**
- ✅ **Auditoria completa**
- ✅ **Tokens dinâmicos** (sem kubeconfig)
- ✅ **Funciona com GKE, EKS e AKS**

**Desvantagens:**
- ⚠️ Requer configuração OIDC em cada cluster
- ⚠️ Mais complexo de implementar

---

## Roadmap de Implementação

### Fase 1: SSO Básico com Azure AD (2-3 dias)

#### Dia 1: Backend
- [ ] Instalar dependências (`go-oidc`, `oauth2`)
- [ ] Criar package `internal/web/auth`
- [ ] Implementar `OIDCProvider`
- [ ] Implementar handlers (`/auth/login`, `/auth/callback`, `/auth/refresh`)
- [ ] Implementar `JWTAuthMiddleware`
- [ ] Atualizar `server.go` para usar novos endpoints

#### Dia 2: Frontend
- [ ] Atualizar `Login.tsx` com botão "Login with Microsoft"
- [ ] Criar `useTokenRefresh` hook
- [ ] Atualizar `apiClient` para usar JWT
- [ ] Implementar tratamento de callback do Azure AD
- [ ] Testar fluxo completo de login

#### Dia 3: Auditoria e Testes
- [ ] Atualizar `HistoryTracker` para incluir user info do JWT
- [ ] Adicionar campos `user_email`, `user_name`, `user_groups` ao histórico
- [ ] Criar filtros por usuário no `HistoryViewer`
- [ ] Testes de integração
- [ ] Documentação

### Fase 2: OIDC para Clusters (Opcional - 1-2 semanas)

- [ ] Configurar OIDC em cluster piloto (AKS)
- [ ] Implementar geração de kubeconfig dinâmico com JWT
- [ ] Testar RBAC baseado em grupos do Azure AD
- [ ] Expandir para outros clusters (GKE, EKS)
- [ ] Documentação avançada

---

## Configuração Azure AD

### 1. Criar App Registration

1. Acessar **Azure Portal** → **Azure Active Directory** → **App registrations**
2. Clicar em **New registration**
3. Preencher:
   - **Name:** k8s-hpa-manager
   - **Supported account types:** Accounts in this organizational directory only
   - **Redirect URI:** Web → `http://localhost:8080/auth/callback`
4. Clicar em **Register**

### 2. Configurar Client Secret

1. No app criado, ir em **Certificates & secrets**
2. Clicar em **New client secret**
3. Descrição: `k8s-hpa-manager-secret`
4. Expiração: **24 months**
5. Copiar o **Value** (só aparece uma vez!)

### 3. Configurar Permissões

1. Ir em **API permissions**
2. Clicar em **Add a permission**
3. Selecionar **Microsoft Graph**
4. **Delegated permissions:**
   - `openid`
   - `profile`
   - `email`
   - `User.Read`
5. Clicar em **Add permissions**
6. Clicar em **Grant admin consent** (requer admin)

### 4. Variáveis de Ambiente

```bash
# Copiar do Azure Portal
export AZURE_TENANT_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
export AZURE_CLIENT_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
export AZURE_CLIENT_SECRET="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
export AZURE_REDIRECT_URL="http://localhost:8080/auth/callback"
```

---

## Conclusão

A implementação de SSO com Azure AD traz benefícios significativos:

### Benefícios Imediatos
- ✅ **Experiência do usuário:** Login uma vez, válido por 90 dias
- ✅ **Segurança:** Tokens individuais, MFA, revogação remota
- ✅ **Auditoria:** Identificação completa de usuários
- ✅ **Manutenção:** Sem necessidade de gerenciar tokens manualmente

### Compatibilidade
- ✅ **GKE, EKS, AKS:** Funciona com todos via kubeconfig
- ✅ **OIDC nativo:** Possível expansão futura para SSO unificado

### Esforço de Implementação
- **Fase 1 (SSO Básico):** 2-3 dias
- **Fase 2 (OIDC Completo):** Opcional, 1-2 semanas

### Recomendação
Iniciar com **Fase 1 (SSO Básico)**, que resolve 90% dos problemas com mínimo esforço e sem necessidade de reconfigurar clusters Kubernetes.

---

**Documento preparado em:** 10 de dezembro de 2025  
**Versão:** 1.0  
**Autor:** Análise técnica realizada com Claude AI
