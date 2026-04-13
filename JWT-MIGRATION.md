# Migração RBAC → JWT

Checklist de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

**Contexto:** Substituir token estático (`K8S_HPA_WEB_TOKEN`) + RBAC via Azure CLI (`az account show` a cada request) por JWT assinado pelo backend. O Azure CLI ainda é chamado, mas apenas uma vez — no login. O `ProtectedAction` e `useUserPermissions` no frontend continuam funcionando sem mudança de interface.

---

## Fase 1 — Backend JWT core ✅ CONCLUÍDA

- [x] Verificar se `github.com/golang-jwt/jwt/v5` está no vendor — já presente (v5.3.0)
- [x] Criar `internal/auth/jwt.go`:
  - Struct `JWTClaims { Email, Name string; IsSRE bool; jwt.RegisteredClaims }`
  - Struct `JWTManager { secret []byte; ttl time.Duration; issuer string }`
  - `NewJWTManager(secret []byte, ttl time.Duration) *JWTManager`
  - `func (m *JWTManager) IsConfigured() bool` → `len(m.secret) >= 32`
  - `func (m *JWTManager) Generate(email, name string, isSRE bool) (string, error)` → HS256, exp = now+ttl
  - `func (m *JWTManager) Validate(tokenStr string) (*JWTClaims, error)` → valida assinatura + expiração
- [x] Criar `internal/web/handlers/auth.go`:
  - Struct `AuthHandler { jwtManager *auth.JWTManager; rbacManager *rbac.RBACManager; disableAD bool }`
  - `NewAuthHandler(jwtManager, rbacManager, disableAD) *AuthHandler`
  - `func (h *AuthHandler) Login(c *gin.Context)`:
    - Se `!jwtManager.IsConfigured()`: retorna 501 `{ error: "JWT não configurado", code: "JWT_NOT_CONFIGURED" }`
    - Se `disableAD=true`: gera JWT com `email="bypass@emergency.mode"`, `isSRE=true`
    - Caso normal: `GetCurrentUserEmail` → `GetUserPermissions` → `Generate` → retorna `{ token, email, is_sre, expires_at, ttl_hours }`
  - `func (h *AuthHandler) Logout(c *gin.Context)`: 200 stateless
  - `func (h *AuthHandler) RefreshToken(c *gin.Context)`: valida JWT atual → emite novo com mesmos claims
- [x] Registrar endpoints no `internal/web/server.go` **fora** do grupo autenticado:
  - `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `POST /api/v1/auth/refresh`
- [x] Ler `K8S_HPA_JWT_SECRET` e `K8S_HPA_JWT_TTL` em `NewServer`; `jwtManager` adicionado ao struct `Server`
- [x] Testado: `POST /auth/login` com `--ad` → JWT válido; sem `K8S_HPA_JWT_SECRET` → `IsConfigured()=false` (501)

---

## Fase 2 — Middleware dual-mode

- [ ] Adicionar em `internal/web/middleware/auth.go`:
  - `func JWTAuthMiddleware(jwtManager *auth.JWTManager, staticToken string) gin.HandlerFunc`:
    - Se `!jwtManager.IsConfigured()`: delegar para lógica atual de token estático (backward compat)
    - Se configurado: `jwtManager.Validate(token)` → 401 se inválido/expirado; injetar `c.Set("jwt_claims", claims)` se OK
  - `func WebSocketJWTAuthMiddleware(jwtManager *auth.JWTManager, staticToken string) gin.HandlerFunc`: mesmo dual-mode aceitando query param `?token=`
- [ ] Adaptar `internal/web/middleware/rbac.go` — todos os handlers verificam `c.Get("jwt_claims")` primeiro:
  - `RequireSREGroup()`: se claims presente → `claims.IsSRE` diretamente; senão → comportamento atual (az CLI)
  - `OptionalSRECheck()`: idem
  - `GetUserPermissions()`: se claims presente → montar resposta `{ email, isSRE, groups:[] }` do JWT; senão → chamar az CLI
  - `InjectUserEmail()`: se claims presente → `c.Set("user_email", claims.Email)`; senão → az CLI
- [ ] Em `internal/web/server.go`:
  - Substituir `api.Use(middleware.AuthMiddleware(s.token))` → `api.Use(middleware.JWTAuthMiddleware(s.jwtManager, s.token))`
  - Substituir `wsShell.Use(middleware.WebSocketAuthMiddleware(s.token))` → `wsShell.Use(middleware.WebSocketJWTAuthMiddleware(s.jwtManager, s.token))`
- [ ] `make build` + testar com token estático (deve continuar funcionando quando `K8S_HPA_JWT_SECRET` ausente)
- [ ] Testar com `K8S_HPA_JWT_SECRET` configurado + JWT obtido na Fase 1

---

## Fase 3 — Frontend

- [ ] Adicionar em `internal/web/frontend/src/lib/api/client.ts`:
  - `async login(): Promise<{ token: string; email: string; isSRE: boolean; expiresAt: string }>`
    → `POST /auth/login`, chama `this.setToken(data.token)` no sucesso
  - `async logout(): Promise<void>` → `POST /auth/logout`, chama `this.clearToken()`
  - `isTokenExpired(): boolean` → decodifica payload do JWT (`atob(token.split('.')[1])`), compara `exp` com `Date.now()/1000`; retorna `false` se token não for JWT (backward compat)
  - `getTokenClaims(): { email?: string; isSRE?: boolean } | null` → decodifica claims localmente sem verificar assinatura
- [ ] Modificar `internal/web/frontend/src/hooks/useUserPermissions.ts`:
  - No `queryFn`: se `localStorage["auth_token"]` tiver formato JWT (3 partes separadas por `.`), decodificar localmente e retornar claims sem chamar `/permissions`; senão, manter chamada atual
- [ ] Modificar página/componente de login (localizar com `grep -r "auth_token\|setToken\|handleLogin" src/`):
  - Tentar `apiClient.login()` no submit; se backend retornar 501 (JWT não configurado), exibir campo de input para token estático (comportamento atual)
  - No carregamento inicial da app (`App.tsx` ou similar): se `apiClient.isTokenExpired()` → `apiClient.clearToken()` → redirecionar para login
- [ ] `./rebuild-web.sh -b` + testar login end-to-end no navegador
- [ ] Verificar que `ProtectedAction` ainda funciona corretamente (lê `isSRE` via `useUserPermissions` que agora vem do JWT local)

---

## Fase 4 — Fluxo real de produção (Azure CLI no login)

- [ ] Validar que `AuthHandler.Login` no modo não-bypass chama corretamente `GetCurrentUserEmail` + `GetUserPermissions` e emite JWT com `isSRE` real do grupo Azure AD `VV_CLOUD_SRE`
- [ ] Testar com ambiente VPN + Azure CLI autenticado no servidor
- [ ] Implementar auto-refresh: frontend chama `POST /auth/refresh` quando `isTokenExpired()` for verdadeiro (no interceptor do `apiClient.request()` ou via `useEffect` periódico)

---

## Fase 5 — Limpeza (após estabilização em produção)

- [ ] Remover `AuthMiddleware` e `WebSocketAuthMiddleware` originais de `middleware/auth.go`
- [ ] Remover fallbacks `|| "poc-token-123"` espalhados no frontend (buscar com `grep -r "poc-token-123" src/`)
- [ ] Remover campo de input de token estático do Login se não for mais necessário
- [ ] Atualizar `CLAUDE.md`: remover menção ao `K8S_HPA_WEB_TOKEN` como mecanismo principal; documentar `K8S_HPA_JWT_SECRET` e `K8S_HPA_JWT_TTL`
- [ ] Atualizar `docs/guides/DEVELOPMENT_COMMANDS.md` e `docs/guides/QUICK_START.md`

---

## Variáveis de ambiente novas

| Variável | Padrão | Descrição |
|---|---|---|
| `K8S_HPA_JWT_SECRET` | `""` (sem JWT) | Secret para assinar JWT (mín. 32 bytes) |
| `K8S_HPA_JWT_TTL` | `8h` | Tempo de vida do token |

Variáveis existentes que continuam funcionando:

| Variável | Comportamento |
|---|---|
| `K8S_HPA_WEB_TOKEN` | Aceito como fallback quando `K8S_HPA_JWT_SECRET` não configurado |
| `K8S_HPA_WINDOWS_BROWSER` | Sem mudança |

---

## Arquivos principais da implementação

```
internal/auth/jwt.go                          ← CRIAR
internal/web/handlers/auth.go                  ← CRIAR
internal/web/middleware/auth.go                ← MODIFICAR (adicionar JWTAuthMiddleware)
internal/web/middleware/rbac.go                ← MODIFICAR (dual-mode: claims JWT ou az CLI)
internal/web/server.go                         ← MODIFICAR (wiring + novos endpoints)
cmd/web.go                                     ← MODIFICAR (ler K8S_HPA_JWT_SECRET)
src/lib/api/client.ts                          ← MODIFICAR (login, logout, isTokenExpired)
src/hooks/useUserPermissions.ts                ← MODIFICAR (decodificar JWT local)
src/pages/Login.tsx (ou componente equivalente) ← MODIFICAR (tentar login JWT, fallback estático)
```
