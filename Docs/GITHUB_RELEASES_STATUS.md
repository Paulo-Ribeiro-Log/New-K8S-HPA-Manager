# Status: GitHub Releases Tab - 12/01/2026

## ✅ Implementado até agora

### Backend (Go)
1. **Scan Real de Clusters** (`internal/web/handlers/github_releases.go`)
   - ✅ Endpoint `POST /api/v1/github/deployments/scan?cluster=X`
   - ✅ Conecta ao cluster via kubeconfig
   - ✅ Lista todos namespaces e deployments
   - ✅ Extrai metadados: versão, squad, CHG, imagem
   - ✅ Popula base SQLite `~/.k8s-hpa-manager/deployment-registry.db`

2. **Endpoints Implementados**:
   - ✅ `GET /api/v1/github/repos` - Lista repositórios configurados
   - ✅ `GET /api/v1/github/deployments/registry` - Lista todos deployments da base
   - ✅ `GET /api/v1/github/deployments/production?app_name=X` - Busca versão em produção
   - ✅ `GET /api/v1/github/compare?release=X&new_tag=Y` - Compara releases no GitHub
   - ✅ `POST /api/v1/github/deployments/scan?cluster=X` - Escaneia cluster

### Frontend (React/TypeScript)
1. **GitHubReleasesTab.tsx**:
   - ✅ Campo de busca dinâmica (busca na base conforme digita)
   - ✅ Painel verde com informações da release em produção
   - ✅ Exibe: cluster, namespace, deployment, versão, squad, CHG, data, age
   - ✅ Campo para tag da nova release
   - ✅ Botão "Comparar com GitHub"
   - ✅ Painel de resultados com commits e arquivos alterados
   - ✅ Toggle para filtrar arquivos (.yml, .yaml, .md, Dockerfile)

2. **DeploymentScanModal.tsx**:
   - ✅ Modal com multi-select de clusters
   - ✅ **Filtro de produção**: Só exibe clusters com "-prd" no nome
   - ✅ Progress bar com status (pending, running, completed, error)
   - ✅ Contador de deployments encontrados/salvos

## ✅ Problema de Labels RESOLVIDO

### Labels sendo extraídos corretamente

**Status**: ✅ **FUNCIONANDO**
- Squad: "Integração WMS" ✅
- ServiceNow CHG: "CHG0430880" ✅
- Base de dados: 57 deployments com metadados completos ✅

**Correções aplicadas**:
1. ✅ Corrigido label de squad: `devops.k8s.io/component-squad` → `devops.k8s.io/squad`
2. ✅ Adicionada busca em **labels E annotations** (fallback)
3. ✅ Adicionado logging para debug (`squad` e `servicenow_task` nos logs)
4. ✅ Base de dados limpa e servidor reiniciado
5. ✅ Novo scan executado (57 deployments no cluster `akspriv-wms-prd-admin`)

**Arquivos modificados**:
- `internal/healthcheck/deployment_checker.go` (linha 516-527)
- `internal/web/handlers/github_releases.go` (linha 902-913, 865-879)

## ✅ Problema de Validação RESOLVIDO

### Validação de repositório e tags implementada

**Status**: ✅ **FUNCIONANDO**
- Token GitHub configurado ✅
- Rate limit: 5000 req/h ✅
- Validação de repositório antes de comparar ✅
- Listagem de tags do GitHub ✅
- Mensagens de erro claras e acionáveis ✅

**Implementação** (`internal/web/handlers/github_releases.go:621-696`):
```go
// 1. Buscar versão em produção (base de dados) ✅
prodDeployment, err := h.findProductionVersion(releaseName)

// 2. Listar tags do repositório GitHub ✅
tags, resp, err := githubClient.Repositories.ListTags(ctx, owner, repo, &github.ListOptions{PerPage: 100})

// 3. Validar se nova tag existe ✅
for _, tag := range tags {
    if tag.GetName() == headTag {
        tagExists = true
    }
}

// 4. Mensagem de erro amigável se não encontrar ✅
if !tagExists {
    return gin.H{
        "error": "Tag não encontrada",
        "suggested_tags": [...], // 10 tags mais recentes
    }
}
```

**Mensagens de erro melhoradas**:
- ❌ Repositório não encontrado: Sugere configurar mapeamento
- ❌ Tag não encontrada: Lista 10 tags mais recentes como sugestão
- ❌ Erro de permissão: Sugere verificar token

## ⚠️ Problema Atual: Mapeamento Deployment → Repositório

**Problema**: Mapeamento hardcoded assume `deployment name = repository name`

**Causa Raiz** (`internal/web/handlers/github_releases.go:600-602`):
```go
// Mapeamento hardcoded e incorreto
owner := "viavarejo-internal"
repo := releaseName  // wins-transformation ≠ nome real do repo
```

**Exemplo real**:
- Deployment: `wins-transformation`
- Repositório real: Pode ser `viavarejo-internal/wins-api` ou outro

**Problemas identificados**:
1. 📛 **Mapeamento incorreto**: Assume `deployment name = repository name`
2. ⚙️ **Sem configuração**: Não há como usuário configurar owner/repo corretos
3. 🗄️ **Sem persistência**: Mapeamentos não são salvos

## 🔍 Próximos Passos - Sistema de Mapeamento

### ✅ Fase 1: Token GitHub (COMPLETO)

**Status**: ✅ **IMPLEMENTADO E FUNCIONANDO**
- ✅ Frontend: Modal `TokenConfigModal.tsx` completo
- ✅ Backend: Persistência criptografada em SQLite
- ✅ Middleware RBAC: Token por usuário (user_email)
- ✅ Validação: Token testado com GitHub API antes de salvar
- ✅ Rate limit: 5000 req/h (autenticado)

### ✅ Fase 2: Validação de Repositório e Tags (COMPLETO)

**Status**: ✅ **IMPLEMENTADO E FUNCIONANDO**
- ✅ Validação de repositório antes de comparar
- ✅ Listagem de tags do GitHub (últimas 100)
- ✅ Verificação se nova tag existe
- ✅ Mensagens de erro claras com sugestões
- ✅ Logs detalhados para debug

### ⏸️ Fase 3: Sistema de Mapeamento Deployment → Repositório (PRÓXIMO)

**Objetivo**: Permitir usuário configurar mapeamento correto entre deployment e repositório GitHub.

**Implementação**:

#### 3.1. Backend - Adicionar colunas na tabela `deployments`

```sql
ALTER TABLE deployments ADD COLUMN github_owner TEXT;
ALTER TABLE deployments ADD COLUMN github_repo TEXT;
```

**Arquivo**: `internal/storage/deployment_registry.go`

#### 3.2. Backend - Endpoint para salvar mapeamento

```go
// POST /api/v1/github/deployments/mapping
type MappingRequest struct {
    DeploymentName string `json:"deployment_name"`
    Cluster        string `json:"cluster"`
    Namespace      string `json:"namespace"`
    GitHubOwner    string `json:"github_owner"`
    GitHubRepo     string `json:"github_repo"`
}
```

**Arquivo**: `internal/web/handlers/github_releases.go`

#### 3.3. Backend - Modificar `CompareReleasesWithRegistry()` para usar mapeamento

```go
// Prioridade de mapeamento:
// 1. Mapeamento configurado pelo usuário (prodDeployment.GitHubOwner/GitHubRepo)
// 2. Fallback: owner=viavarejo-internal, repo=deployment_name
owner := prodDeployment.GitHubOwner
if owner == "" {
    owner = "viavarejo-internal" // Fallback
}

repo := prodDeployment.GitHubRepo
if repo == "" {
    repo = releaseName // Fallback
}
```

#### 3.4. Frontend - Modal de Configuração

**Componente**: `RepoMappingModal.tsx` (novo)

**Funcionalidades**:
- Buscar deployment por nome (autocomplete)
- Campos: GitHub Owner, GitHub Repo
- Validar repositório existe antes de salvar (botão "Testar Acesso")
- Salvar mapeamento na base de dados

**Integração**: Botão "Configurar Repositório" no painel verde de detalhes de produção

#### 3.5. Frontend - Atualizar `GitHubReleasesTab` para exibir mapeamento

- Badge mostrando repositório configurado: `viavarejo-internal/wins-api` ✅
- Link para repositório no GitHub
- Botão "Editar" para abrir modal de configuração

### Fase 4: Testar End-to-End

1. ✅ Configurar token do GitHub
2. ⏸️ Configurar mapeamento de 1 deployment real
3. ⏸️ Buscar release em produção
4. ⏸️ Comparar com nova tag
5. ⏸️ Verificar commits e arquivos alterados

### Fase 5: Re-scan Automático (Futuro)

**Requisito do usuário**: "a base de dados deve ser atualizável pq sempre terão seus deployments e versões atualizadas alem de novos deployments serão criados e postos em produção"

**Opções**:
1. **Botão Manual "Atualizar Base"**: No header da aba GitHub Releases
2. **Auto-refresh periódico**: A cada X horas (configurável)
3. **Webhook do GitHub**: Notificação quando nova release é criada
4. **Integração com Health Checking**: Aproveitar scan de saúde para atualizar base

## 📁 Arquivos Principais

### Backend
- `internal/web/handlers/github_releases.go` - Handlers REST API
- `internal/healthcheck/deployment_checker.go` - Extração de metadados
- `internal/storage/deployment_registry.go` - SQLite operations
- `internal/web/server.go` - Rotas (linha 658)

### Frontend
- `internal/web/frontend/src/components/GitHubReleasesTab.tsx` - Componente principal
- `internal/web/frontend/src/components/DeploymentScanModal.tsx` - Modal de scan
- `internal/web/frontend/src/components/TokenConfigModal.tsx` - Config de token GitHub

### Base de Dados
- `~/.k8s-hpa-manager/deployment-registry.db` - SQLite (atualmente limpa)
- Schema: `deployments` table com 14 colunas

## 🧪 Comandos de Teste

```bash
# Ver total de deployments na base
curl -s -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/github/deployments/registry" | jq '.total'

# Buscar versão em produção
curl -s -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/github/deployments/production?app_name=wins-transformation" | jq '.'

# Fazer scan de cluster
curl -X POST -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/github/deployments/scan?cluster=akspriv-wms-prd-admin"

# Comparar releases
curl -s -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/github/compare?release=wins-transformation&new_tag=0.1.3-1" | jq '.'
```

## 📊 Progresso Geral

| Fase | Status | Descrição |
|------|--------|-----------|
| Fase 1 | ✅ 100% | Backend - Scan real + Endpoints REST |
| Fase 2 | ✅ 100% | Frontend - Busca dinâmica + Painel de info |
| Fase 3 | ✅ 100% | Extração de metadados (squad/CHG funcionando) |
| **Fase 4a** | ✅ 100% | **Token GitHub (5000 req/h, repos privados)** |
| **Fase 4b** | ✅ 100% | **Validação de repositório e tags** |
| **Fase 4c** | ⏸️ 0% | **Mapeamento deployment → repositório** |
| **Fase 4d** | ⏸️ 0% | **Testar comparação GitHub end-to-end** |
| Fase 5 | ⏸️ 0% | Re-scan automático/manual |

**Progresso Total**: 5/8 fases completas (62.5%)

## 🔄 Para Continuar - Ordem de Execução

### ✅ 1. Token GitHub (COMPLETO)
```bash
# ✅ Backend: SQLite com criptografia AES
# ✅ Endpoint POST /api/v1/github/token
# ✅ Validação com GitHub API antes de salvar
# ✅ Rate limit: 5000 req/h
```

### ✅ 2. Validação (COMPLETO)
```bash
# ✅ Listagem de tags do repositório
# ✅ Verificação se nova tag existe
# ✅ Mensagens de erro amigáveis com sugestões
```

### ⏸️ 3. Sistema de Mapeamento (PRÓXIMO - AGORA)
```bash
# Backend (Go):
# 1. Adicionar colunas github_owner, github_repo na tabela deployments
# 2. Criar endpoint POST /api/v1/github/deployments/mapping
# 3. Modificar CompareReleasesWithRegistry() para usar mapeamento

# Frontend (React/TypeScript):
# 4. Criar componente RepoMappingModal.tsx
# 5. Integrar botão "Configurar Repositório" no painel verde
# 6. Exibir badge com repositório configurado
```

### ⏸️ 4. Teste End-to-End (FINAL)
```bash
# 1. ✅ Configurar token GitHub
# 2. ⏸️ Configurar mapeamento de 1 deployment real
# 3. ⏸️ Testar comparação completa
# 4. ⏸️ Verificar commits e arquivos alterados exibidos corretamente
```

## 🎯 Prioridade Máxima

**PRÓXIMA TAREFA**: Implementar sistema de mapeamento deployment → repositório

**Passo 1 - Backend (30min)**:
1. Adicionar colunas `github_owner`, `github_repo` na tabela `deployments`
2. Método `UpdateGitHubMapping()` em `deployment_registry.go`
3. Endpoint `POST /api/v1/github/deployments/mapping` em `github_releases.go`
4. Modificar `CompareReleasesWithRegistry()` para ler mapeamento

**Passo 2 - Frontend (30min)**:
1. Criar `RepoMappingModal.tsx`
2. Hook `useRepoMapping()` com React Query
3. Integrar botão no painel verde de produção
4. Badge com repositório configurado

---

**Última atualização**: 12/01/2026 10:15 BRT
**Status do servidor**: ✅ Rodando (PID: 1231195)
**Base de dados**: 57 deployments (cluster akspriv-wms-prd-admin)
**Versão**: v1.3.1-144-g659c6d7-dirty
**Token GitHub**: ✅ Configurado (Paulo-Ribeiro-Log, 4981/5000 req/h)
**Labels extraídos**: ✅ Squad e CHG funcionando
**Validação**: ✅ Repositório e tags validados antes de comparar
**Próximo passo**: Sistema de mapeamento deployment → repositório
