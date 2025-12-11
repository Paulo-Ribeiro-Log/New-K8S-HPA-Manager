# Implementação: user_email no History Tracker

**Data:** 11 de dezembro de 2025
**Versão:** v1.3.6
**Status:** ✅ Implementado

---

## 📋 Resumo

Adicionado suporte a `user_email` e `user_name` no History Tracker para rastreabilidade de usuários nas operações.

### Mudanças Implementadas

1. ✅ Campos `UserEmail` e `UserName` adicionados ao `HistoryEntry`
2. ✅ Filtro por `user_email` no endpoint de histórico
3. ✅ Função helper `GetCurrentUserInfo()` compatível com SSO futuro
4. ✅ Fallback para Azure CLI (RBAC atual) quando SSO não disponível

---

## 🔧 Arquivos Modificados

### 1. `internal/history/tracker.go`

**Adicionados campos ao HistoryEntry:**

```go
type HistoryEntry struct {
    ID          string                 `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    UserEmail   string                 `json:"user_email,omitempty"`   // ⭐ NOVO
    UserName    string                 `json:"user_name,omitempty"`    // ⭐ NOVO
    Action      string                 `json:"action"`
    Resource    string                 `json:"resource"`
    Cluster     string                 `json:"cluster"`
    Before      map[string]interface{} `json:"before"`
    After       map[string]interface{} `json:"after"`
    Status      string                 `json:"status"`
    ErrorMsg    string                 `json:"error_msg,omitempty"`
    Duration    int64                  `json:"duration_ms"`
    SessionName string                 `json:"session_name,omitempty"`
}
```

**Adicionado filtro por email:**

```go
type HistoryFilter struct {
    Action      string
    Cluster     string
    Resource    string
    Status      string
    UserEmail   string    // ⭐ NOVO
    StartDate   time.Time
    EndDate     time.Time
    SessionName string
}
```

### 2. `internal/history/userinfo.go` ⭐ NOVO

**Função compatível com SSO futuro + RBAC atual:**

```go
// GetCurrentUserInfo obtém informações do usuário atual
// Compatível com SSO futuro (JWT via gin.Context) e RBAC atual (Azure CLI)
func GetCurrentUserInfo(c *gin.Context) UserInfo {
    var userInfo UserInfo

    // 1. Tentar obter do contexto primeiro (SSO via JWT - preparado para futuro)
    if email, exists := c.Get("user_email"); exists {
        userInfo.Email = email.(string)
    }

    if name, exists := c.Get("user_name"); exists {
        userInfo.Name = name.(string)
    }

    // 2. Se não encontrou no contexto, usar Azure CLI (RBAC atual)
    if userInfo.Email == "" {
        userInfo.Email = getCurrentUserEmailFromAzureCLI()
    }

    // 3. Se ainda não tem nome, extrair do email
    if userInfo.Name == "" && userInfo.Email != "" {
        // Extrair nome do email (ex: joao.silva@empresa.com → João Silva)
        parts := strings.Split(userInfo.Email, "@")
        if len(parts) > 0 {
            nameParts := strings.Split(parts[0], ".")
            if len(nameParts) >= 2 {
                firstName := strings.Title(nameParts[0])
                lastName := strings.Title(nameParts[1])
                userInfo.Name = firstName + " " + lastName
            }
        }
    }

    return userInfo
}
```

### 3. `internal/web/handlers/helpers.go` ⭐ NOVO

**Helper para simplificar uso:**

```go
// GetUserInfoForHistory obtém informações do usuário para auditoria
func GetUserInfoForHistory(c *gin.Context) history.UserInfo {
    return history.GetCurrentUserInfo(c)
}
```

### 4. `internal/web/handlers/history.go`

**Adicionado suporte a filtro por email:**

```go
filter := history.HistoryFilter{
    Action:      c.Query("action"),
    Cluster:     c.Query("cluster"),
    Resource:    c.Query("resource"),
    Status:      c.Query("status"),
    UserEmail:   c.Query("user_email"),  // ⭐ NOVO
    SessionName: c.Query("session_name"),
}
```

### 5. `internal/web/handlers/hpas.go` (Exemplo)

**Como usar nos handlers:**

```go
// Antes (sem user info)
h.historyTracker.Log(history.HistoryEntry{
    Action:   history.ActionUpdateHPA,
    Resource: fmt.Sprintf("%s/%s", namespace, name),
    Cluster:  cluster,
    Status:   history.StatusSuccess,
    // ...
})

// Depois (com user info) ✅
userInfo := GetUserInfoForHistory(c)
h.historyTracker.Log(history.HistoryEntry{
    UserEmail: userInfo.Email,  // ⭐ NOVO
    UserName:  userInfo.Name,   // ⭐ NOVO
    Action:    history.ActionUpdateHPA,
    Resource:  fmt.Sprintf("%s/%s", namespace, name),
    Cluster:   cluster,
    Status:    history.StatusSuccess,
    // ...
})
```

---

## 🔄 Como Funciona

### Fluxo de Obtenção de User Info

```
Handler recebe requisição
    ↓
GetUserInfoForHistory(c)
    ↓
1. Tenta obter do gin.Context (SSO - futuro)
    ├── Se encontrar → Retorna email + nome do JWT ✅
    └── Se não encontrar → Passo 2
    ↓
2. Executa Azure CLI: az account show
    ├── Retorna email do usuário logado ✅
    └── Extrai nome do email (joao.silva → João Silva)
    ↓
3. Retorna UserInfo{Email, Name}
    ↓
HistoryEntry salvo com user info ✅
```

### Exemplo de Entrada no Histórico

**Antes:**
```json
{
  "id": "abc-123",
  "timestamp": "2025-12-11T10:30:00Z",
  "action": "update_hpa",
  "resource": "production/api-backend",
  "cluster": "akspriv-prod",
  "status": "success"
}
```

**Depois:**
```json
{
  "id": "abc-123",
  "timestamp": "2025-12-11T10:30:00Z",
  "user_email": "paulo.gribeiro@viavarejo.com.br",  ⭐ NOVO
  "user_name": "Paulo Gribeiro",                     ⭐ NOVO
  "action": "update_hpa",
  "resource": "production/api-backend",
  "cluster": "akspriv-prod",
  "status": "success"
}
```

---

## 🌐 API Endpoint Atualizado

### GET /api/v1/history

**Novos parâmetros de query:**

```bash
# Filtrar por email do usuário
GET /api/v1/history?user_email=paulo.gribeiro@viavarejo.com.br

# Combinar com outros filtros
GET /api/v1/history?user_email=paulo.gribeiro@viavarejo.com.br&action=update_hpa&cluster=akspriv-prod
```

**Resposta:**

```json
{
  "entries": [
    {
      "id": "abc-123",
      "timestamp": "2025-12-11T10:30:00Z",
      "user_email": "paulo.gribeiro@viavarejo.com.br",
      "user_name": "Paulo Gribeiro",
      "action": "update_hpa",
      "resource": "production/api-backend",
      "cluster": "akspriv-prod",
      "status": "success",
      "duration_ms": 1234
    }
  ],
  "count": 1
}
```

---

## 📝 Handlers a Atualizar

### ✅ Já Atualizado
- `hpas.go` - Update HPA

### ⚠️ Pendente (Mesmo Padrão)

Todos seguem o mesmo padrão. Basta adicionar essas 2 linhas antes de criar `HistoryEntry`:

```go
userInfo := GetUserInfoForHistory(c)

h.historyTracker.Log(history.HistoryEntry{
    UserEmail: userInfo.Email,  // ⭐ Adicionar
    UserName:  userInfo.Name,   // ⭐ Adicionar
    // ... resto dos campos
})
```

**Lista de handlers:**
- [ ] `configmaps.go` (2 usos)
- [ ] `nodepools.go` (5 usos)
- [ ] `deployments.go` (1 uso)
- [ ] `ingress.go` (1 uso)
- [ ] `cronjobs.go` (1 uso)
- [ ] `prometheus.go` (2 usos)
- [ ] `namespaces.go` (3 usos)
- [ ] `pods.go` (2 usos)
- [ ] `secrets.go` (2 usos)
- [ ] `internal/kubernetes/client.go` (rollouts - 6 usos)

---

## ✅ Compatibilidade

### Retrocompatibilidade
- ✅ Campos opcionais (`omitempty`) - não quebra logs antigos
- ✅ Filtros opcionais - histórico existente continua funcionando
- ✅ JSON backward-compatible

### Compatibilidade Futura (SSO)
- ✅ Preparado para JWT (`c.Get("user_email")`)
- ✅ Fallback automático para Azure CLI
- ✅ Zero mudanças necessárias quando SSO for implementado

---

## 🧪 Como Testar

### 1. Testar Localmente (Azure CLI)

```bash
# 1. Fazer login no Azure
az login

# 2. Verificar usuário logado
az account show --query user.name -o tsv
# Output: seu.email@empresa.com

# 3. Executar aplicação
./build/new-k8s-hpa web

# 4. Fazer uma operação (ex: editar HPA)
# Via frontend ou API

# 5. Verificar histórico
curl http://localhost:8080/api/v1/history | jq '.entries[0].user_email'
# Output: "seu.email@empresa.com"
```

### 2. Filtrar por Usuário

```bash
# Histórico de um usuário específico
curl "http://localhost:8080/api/v1/history?user_email=paulo.gribeiro@viavarejo.com.br" | jq

# Combinar filtros
curl "http://localhost:8080/api/v1/history?user_email=paulo.gribeiro@viavarejo.com.br&action=update_hpa" | jq
```

### 3. Verificar Logs em Disco

```bash
# Ver entrada mais recente
cat ~/.k8s-hpa-manager/history/$(date +%Y-%m)/*.json | jq -s 'sort_by(.timestamp) | last'

# Deve conter:
# {
#   "user_email": "seu.email@empresa.com",
#   "user_name": "Seu Nome",
#   ...
# }
```

---

## 🚀 Próximos Passos

### Curto Prazo (Opcional)
1. Atualizar todos os handlers restantes (19 locais)
2. Adicionar campo `user_email` no frontend (HistoryViewer)
3. Exportação CSV com user info

### Médio Prazo (PostgreSQL Externo)
1. Criar banco PostgreSQL externo
2. Implementar writer assíncrono
3. Auditoria centralizada e imutável

### Longo Prazo (SSO)
1. Implementar SSO com Azure AD
2. JWT automático no `gin.Context`
3. Zero mudanças no código (já preparado!)

---

## 📊 Benefícios Imediatos

✅ **Rastreabilidade:** Saber quem fez cada ação
✅ **Auditoria:** Histórico completo por usuário
✅ **Filtros:** Buscar operações de usuários específicos
✅ **Compliance:** Preparado para auditoria corporativa
✅ **Futuro:** Compatível com SSO (sem refactoring)

---

## 🎯 Conclusão

A implementação de `user_email` no History Tracker é uma **solução incremental** que:

1. ✅ Funciona **hoje** com RBAC atual (Azure CLI)
2. ✅ Preparada para **SSO futuro** (zero mudanças)
3. ✅ Base para **PostgreSQL externo** (próximo passo)
4. ✅ **Compatível** com código existente

**Status:** Pronto para uso em desenvolvimento local. Aguardando decisão sobre:
- Atualizar handlers restantes (20min)
- PostgreSQL externo (1 dia)
- SSO completo (6 dias - conforme ROTEIRO_IMPLEMENTACAO_SSO.md)

---

**Documento preparado em:** 11 de dezembro de 2025
**Autor:** Análise técnica realizada com Claude AI
