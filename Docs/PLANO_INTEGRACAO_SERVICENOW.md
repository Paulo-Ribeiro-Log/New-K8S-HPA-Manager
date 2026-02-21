# Plano de Integração: ServiceNow + XL Release + Aba GitHub Releases

**Data**: 21 de janeiro de 2026
**Status**: Planejamento
**Prioridade**: Alta
**Estimativa**: 4-5 dias de desenvolvimento

---

## SUMÁRIO EXECUTIVO

Este plano cobre **duas integrações** na aba GitHub Releases:

| Integração | Função | Benefício |
|------------|--------|-----------|
| **ServiceNow** | Importar dados da CHG | Auto-preencher campos do formulário |
| **XL Release** | Aprovar deployment | Botão de aprovação no painel de visualização |

---

## 1. Visão Geral

### 1.1 Objetivo
Integrar a API do ServiceNow com a aba GitHub Releases para preencher automaticamente os campos de deployment a partir de uma Change Request (CHG).

### 1.2 Fluxo Proposto

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           FLUXO DE INTEGRAÇÃO                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Usuário cola URL do ServiceNow:                                         │
│     https://viavarejo.service-now.com/change_request.do?sys_id=c3b53c25...  │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  2. Backend extrai sys_id da URL:                                           │
│     sys_id = "c3b53c259722ba50ec54f3b6f053af33"                             │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  3. Backend chama API ServiceNow:                                           │
│     GET /api/now/table/change_request/{sys_id}                              │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  4. Backend extrai campos da CHG:                                           │
│     - "Motivo da mudança" (description ou u_motivo_mudanca)                 │
│     - "Versão" (u_versao ou campo customizado)                              │
│     - "Aplicação(ões)" (u_aplicacao ou cmdb_ci)                             │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  5. Backend faz parsing do texto para extrair:                              │
│     - Nome do Deployment → busca na base de conhecimento                    │
│     - Versão (tag nova) → regex para padrão semver (X.X.X-X)                │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  6. Frontend preenche automaticamente:                                      │
│     - Nome do Deployment ✅                                                 │
│     - Repositório GitHub (inferido do deployment)                           │
│     - Tag em Produção (via API existente)                                   │
│     - Tag da Nova Release (versão da CHG)                                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Análise de Viabilidade

### 2.1 Possível? SIM

A integração é **tecnicamente viável** com as seguintes considerações:

| Aspecto | Viabilidade | Observação |
|---------|-------------|------------|
| Acesso à API ServiceNow | **Sim** | Table API é padrão, requer autenticação |
| Leitura da CHG por sys_id | **Sim** | GET /api/now/table/change_request/{sys_id} |
| Campos personalizados | **Parcial** | Campos `u_*` precisam ser mapeados |
| Parsing de texto | **Parcial** | "Motivo da mudança" pode ter formato variável |
| Mapeamento automático | **Parcial** | Depende da consistência dos dados na CHG |

### 2.2 Pré-requisitos

1. **Credenciais ServiceNow**
   - Usuário técnico com permissão de leitura na tabela `change_request`
   - Alternativa: OAuth 2.0 com token de refresh

2. **Mapeamento de Campos**
   - Identificar nomes exatos dos campos customizados da ViaVarejo
   - Campos provavelmente usados:
     - `description` ou `u_motivo_mudanca` → "Motivo da mudança"
     - `u_versao` ou `u_version` → "Versão"
     - `u_aplicacao` ou `cmdb_ci` → "Aplicação(ões)"

3. **Formato dos Dados**
   - O campo "Versão" precisa conter a tag no formato: `2.5.8-1`, `v2.5.8`, etc.
   - O campo "Aplicação" precisa conter o nome do deployment ou aplicação

### 2.3 Riscos e Mitigações

| Risco | Impacto | Mitigação |
|-------|---------|-----------|
| Campos com nomes diferentes | Alto | Configuração via UI para mapear campos |
| Formato de versão inconsistente | Médio | Regex robusto + fallback manual |
| Limite de rate da API | Baixo | Cache de respostas (TTL 5min) |
| VPN/Firewall bloqueando | Médio | Documentar requisitos de rede |
| Timeout em CHGs grandes | Baixo | Timeout configurável (30s padrão) |

---

## 3. Arquitetura Técnica

### 3.1 Novos Componentes

```
internal/
├── servicenow/
│   ├── client.go              # Cliente HTTP para ServiceNow API
│   ├── models.go              # Structs para Change Request
│   ├── parser.go              # Parsing de campos e extração de dados
│   └── config.go              # Configuração (URL, credenciais)
├── web/
│   ├── handlers/
│   │   └── servicenow.go      # Endpoints REST para integração
│   └── frontend/src/
│       └── components/
│           └── ServiceNowImportModal.tsx  # Modal de importação
└── storage/
    └── servicenow_config.go   # Armazenamento de credenciais
```

### 3.2 API REST (Backend)

#### Novos Endpoints

```
POST /api/v1/servicenow/config
  - Salva configuração (URL base, credenciais)
  - Criptografa credenciais com AES-256-GCM

GET /api/v1/servicenow/config/status
  - Retorna status da configuração (configurado/não configurado)
  - Não retorna credenciais

POST /api/v1/servicenow/import
  - Body: { "url": "https://viavarejo.service-now.com/change_request.do?sys_id=..." }
  - Extrai sys_id da URL
  - Chama API ServiceNow
  - Retorna dados parseados para preencher campos

GET /api/v1/servicenow/fields
  - Lista campos disponíveis na tabela change_request
  - Usado para configuração de mapeamento
```

#### Exemplo de Resposta - POST /import

```json
{
  "success": true,
  "change_request": {
    "number": "CHG0012345",
    "short_description": "Deploy vv-geolocalizacao-api v2.5.8-1",
    "description": "Motivo da mudança: Correção de bug crítico...",
    "state": "Scheduled",
    "opened_at": "2026-01-20T10:00:00Z"
  },
  "extracted_data": {
    "deployment_name": "vv-geolocalizacao-api",
    "deployment_name_confidence": 0.95,
    "new_version": "2.5.8-1",
    "new_version_confidence": 0.98,
    "application": "vv-geolocalizacao-api",
    "github_repo_suggestion": "vv-retira-geolocalizacao"
  },
  "production_info": {
    "cluster": "akspriv-prod-admin",
    "namespace": "production",
    "current_version": "2.5.5-2",
    "found_in_registry": true
  }
}
```

### 3.3 ServiceNow API

#### Endpoint de Leitura

```http
GET https://viavarejo.service-now.com/api/now/table/change_request/{sys_id}
Authorization: Basic {base64(username:password)}
Content-Type: application/json
Accept: application/json

# Query params opcionais:
?sysparm_display_value=all  # Retorna valores e display values
&sysparm_fields=number,short_description,description,u_versao,u_aplicacao,cmdb_ci,state
```

#### Exemplo de Resposta ServiceNow

```json
{
  "result": {
    "sys_id": "c3b53c259722ba50ec54f3b6f053af33",
    "number": "CHG0012345",
    "short_description": "Deploy vv-geolocalizacao-api v2.5.8-1 em produção",
    "description": "Motivo da mudança:\n- Correção de bug crítico no endpoint /location\n- Melhoria de performance\n\nVersão: 2.5.8-1\nAplicação: vv-geolocalizacao-api",
    "state": "scheduled",
    "u_versao": "2.5.8-1",
    "u_aplicacao": {
      "display_value": "vv-geolocalizacao-api",
      "value": "sys_id_do_ci"
    },
    "cmdb_ci": {
      "display_value": "vv-geolocalizacao-api",
      "value": "sys_id_do_ci"
    }
  }
}
```

### 3.4 Parsing Inteligente

#### Extração de Versão

```go
// internal/servicenow/parser.go

// Padrões de versão suportados:
var versionPatterns = []string{
    `(?i)vers[aã]o[:\s]+([vV]?\d+\.\d+\.\d+(?:-\d+)?)`,  // Versão: 2.5.8-1
    `(?i)tag[:\s]+([vV]?\d+\.\d+\.\d+(?:-\d+)?)`,        // Tag: v2.5.8
    `([vV]?\d+\.\d+\.\d+(?:-\d+)?)`,                      // Qualquer semver
}

func ExtractVersion(text string) (string, float64) {
    for _, pattern := range versionPatterns {
        re := regexp.MustCompile(pattern)
        if match := re.FindStringSubmatch(text); len(match) > 1 {
            version := normalizeVersion(match[1])
            confidence := calculateConfidence(pattern)
            return version, confidence
        }
    }
    return "", 0.0
}
```

#### Extração de Aplicação

```go
// Estratégia em cascata:
// 1. Campo u_aplicacao (display_value)
// 2. Campo cmdb_ci (display_value)
// 3. Busca no short_description
// 4. Busca no description (Aplicação: xxx)

func ExtractApplication(chg ChangeRequest, registry *DeploymentRegistry) (string, float64) {
    // 1. Campo dedicado
    if chg.UApplication.DisplayValue != "" {
        return chg.UApplication.DisplayValue, 0.99
    }

    // 2. CMDB CI
    if chg.CMDBCI.DisplayValue != "" {
        return chg.CMDBCI.DisplayValue, 0.95
    }

    // 3. Pattern no short_description
    // "Deploy vv-geolocalizacao-api v2.5.8-1"
    pattern := `(?i)deploy[:\s]+([a-z0-9-]+)`
    if match := regexp.MustCompile(pattern).FindStringSubmatch(chg.ShortDescription); len(match) > 1 {
        if registry.Exists(match[1]) {
            return match[1], 0.85
        }
    }

    // 4. Pattern no description
    pattern = `(?i)aplica[çc][aã]o[:\s]+([a-z0-9-]+)`
    if match := regexp.MustCompile(pattern).FindStringSubmatch(chg.Description); len(match) > 1 {
        return match[1], 0.75
    }

    return "", 0.0
}
```

### 3.5 Frontend - Modal de Importação

#### Componente: ServiceNowImportModal.tsx

```tsx
interface ServiceNowImportModalProps {
  open: boolean;
  onClose: () => void;
  onImportSuccess: (data: ImportedData) => void;
}

interface ImportedData {
  deploymentName: string;
  newVersion: string;
  githubRepoSuggestion?: string;
  productionVersion?: string;
  changeNumber: string;
  confidence: {
    deployment: number;
    version: number;
  };
}

// Layout do Modal:
// ┌─────────────────────────────────────────────────────┐
// │  Importar de ServiceNow                       [X]   │
// ├─────────────────────────────────────────────────────┤
// │                                                     │
// │  URL da Change Request:                             │
// │  ┌─────────────────────────────────────────────┐   │
// │  │ https://viavarejo.service-now.com/change... │   │
// │  └─────────────────────────────────────────────┘   │
// │                                                     │
// │  [Importar]                                         │
// │                                                     │
// │  ─────────── Dados Extraídos ───────────           │
// │                                                     │
// │  CHG: CHG0012345                                    │
// │  Deployment: vv-geolocalizacao-api (95%)           │
// │  Nova Versão: 2.5.8-1 (98%)                        │
// │  Versão Prod: 2.5.5-2                              │
// │  Repo Sugerido: vv-retira-geolocalizacao           │
// │                                                     │
// │  ⚠️ Revise os dados antes de confirmar             │
// │                                                     │
// │            [Cancelar]  [Usar Dados]                │
// │                                                     │
// └─────────────────────────────────────────────────────┘
```

### 3.6 Fluxo de Configuração

#### Modal de Configuração ServiceNow

```tsx
// Primeira vez: usuário precisa configurar credenciais
// ┌─────────────────────────────────────────────────────┐
// │  Configurar ServiceNow                        [X]   │
// ├─────────────────────────────────────────────────────┤
// │                                                     │
// │  URL Base:                                          │
// │  ┌─────────────────────────────────────────────┐   │
// │  │ https://viavarejo.service-now.com           │   │
// │  └─────────────────────────────────────────────┘   │
// │                                                     │
// │  Usuário:                                           │
// │  ┌─────────────────────────────────────────────┐   │
// │  │ usuario.tecnico                             │   │
// │  └─────────────────────────────────────────────┘   │
// │                                                     │
// │  Senha:                                             │
// │  ┌─────────────────────────────────────────────┐   │
// │  │ ••••••••••••                                │   │
// │  └─────────────────────────────────────────────┘   │
// │                                                     │
// │  [Testar Conexão]        [Salvar]                  │
// │                                                     │
// └─────────────────────────────────────────────────────┘
```

---

## 4. Mapeamento de Campos

### 4.1 ServiceNow → GitHub Releases

| Campo ServiceNow | Campo GitHub Releases | Extração |
|------------------|----------------------|----------|
| `u_aplicacao` ou `cmdb_ci` | Nome do Deployment | Direto ou parsing |
| `u_versao` ou parsing de `description` | Tag da Nova Release | Regex semver |
| Busca na BD por deployment | Tag em Produção | API existente |
| Convenção de nome ou config | Repositório GitHub | Inferência |

### 4.2 Convenções de Nomes (Repositórios)

Para inferir o repositório GitHub a partir do nome do deployment:

```go
// Mapeamento comum na ViaVarejo:
var repoMappings = map[string]string{
    "vv-geolocalizacao-api": "vv-retira-geolocalizacao",
    "vv-faturamento-api":    "vv-faturamento",
    // ... outros mapeamentos
}

// Fallback: usar mesmo nome do deployment
func InferGitHubRepo(deploymentName string) string {
    if repo, exists := repoMappings[deploymentName]; exists {
        return repo
    }
    // Tentar convenções comuns
    repo := strings.TrimPrefix(deploymentName, "vv-")
    repo = strings.TrimSuffix(repo, "-api")
    return "vv-" + repo
}
```

---

## 5. Cronograma de Implementação

### Fase 1: Backend ServiceNow (1 dia)

| Task | Descrição | Arquivos |
|------|-----------|----------|
| 1.1 | Criar cliente HTTP ServiceNow | `internal/servicenow/client.go` |
| 1.2 | Definir models (ChangeRequest) | `internal/servicenow/models.go` |
| 1.3 | Implementar parser de campos | `internal/servicenow/parser.go` |
| 1.4 | Criar storage de credenciais | `internal/storage/servicenow_config.go` |
| 1.5 | Implementar handlers REST | `internal/web/handlers/servicenow.go` |
| 1.6 | Registrar rotas | `internal/web/server.go` |

### Fase 2: Backend XL Release (1 dia)

| Task | Descrição | Arquivos |
|------|-----------|----------|
| 2.1 | Criar cliente HTTP XL Release | `internal/xlrelease/client.go` |
| 2.2 | Definir models (Release, Task) | `internal/xlrelease/models.go` |
| 2.3 | Implementar parser de URL | `internal/xlrelease/parser.go` |
| 2.4 | Criar storage de credenciais | `internal/storage/xlrelease_config.go` |
| 2.5 | Implementar handlers REST | `internal/web/handlers/xlrelease.go` |
| 2.6 | Registrar rotas | `internal/web/server.go` |

### Fase 3: Frontend - Importação ServiceNow (1 dia)

| Task | Descrição | Arquivos |
|------|-----------|----------|
| 3.1 | Criar modal de configuração ServiceNow | `ServiceNowConfigModal.tsx` |
| 3.2 | Criar modal de importação CHG | `ServiceNowImportModal.tsx` |
| 3.3 | Adicionar botão "Importar de CHG" | `GitHubReleasesTab.tsx` |
| 3.4 | Auto-preencher campos após import | `GitHubReleasesTab.tsx` |

### Fase 4: Frontend - Aprovação XL Release (1 dia)

| Task | Descrição | Arquivos |
|------|-----------|----------|
| 4.1 | Criar card de aprovação no painel direito | `GitHubReleasesTab.tsx` |
| 4.2 | Criar modal de configuração XL Release | `XLReleaseConfigModal.tsx` |
| 4.3 | Criar modal de confirmação de aprovação | `ApproveDeploymentModal.tsx` |
| 4.4 | Integrar com fluxo de comparação | `GitHubReleasesTab.tsx` |

### Fase 5: Testes e Refinamentos (0.5 dia)

| Task | Descrição |
|------|-----------|
| 5.1 | Testes unitários dos parsers |
| 5.2 | Testes de integração com mocks |
| 5.3 | Ajustes de UX baseados em feedback |
| 5.4 | Documentação |

### Resumo do Cronograma

| Fase | Descrição | Tempo |
|------|-----------|-------|
| 1 | Backend ServiceNow | 1 dia |
| 2 | Backend XL Release | 1 dia |
| 3 | Frontend ServiceNow | 1 dia |
| 4 | Frontend XL Release | 1 dia |
| 5 | Testes e Refinamentos | 0.5 dia |
| **Total** | | **4.5 dias** |

---

## 6. Requisitos de Infraestrutura

### 6.1 Rede
- Acesso HTTPS à instância ServiceNow (`viavarejo.service-now.com`)
- Porta 443 liberada no firewall (provavelmente já OK se VPN conectada)

### 6.2 Credenciais
- Usuário técnico do ServiceNow com:
  - `read` na tabela `change_request`
  - `read` na tabela `sys_user` (opcional, para referências)
  - `read` na tabela `cmdb_ci` (para aplicações)

### 6.3 Armazenamento
- SQLite existente (`~/.k8s-hpa-manager/`) para credenciais criptografadas
- Mesma estratégia do GitHubTokenStore (AES-256-GCM)

---

## 7. Informações Confirmadas (21/01/2026)

### 7.1 Campos do ServiceNow

| Pergunta | Resposta |
|----------|----------|
| Campo "Versão" | **Está dentro do campo `description`** (Motivo da mudança) |
| Campo "Aplicação" | **Está dentro do campo `description`** (Motivo da mudança) |
| Autenticação | **Azure AD SSO** |

### 7.2 Implicações

1. **Parsing de Texto**: Precisarei criar regex para extrair Versão e Aplicação do texto livre do `description`
2. **Azure AD SSO**: Podemos reutilizar a integração existente com Azure AD (módulo `internal/rbac/azure_ad.go`)
3. **OAuth 2.0**: ServiceNow com Azure AD geralmente usa OAuth 2.0 com token bearer

### 7.3 Exemplo Real do Texto (Confirmado 21/01/2026)

O campo "Motivo da mudança" segue um **template padronizado da esteira de CD**:

```
Opções de Janelas disponíveis para implementação em Produção:
    - Padrão: 00:00:00 - 08:00:00

Objetivo/Motivo da Mudança:
    * Issues do Jira:
        * PDE-3215: Baixa: Envio no Notfis identificação de pedido Meli
    * Mensagens de commits sem a issue do Jira referenciadas:
        * update plugin sonar
        * Update PedidoItemReservaAcl.java

Informações úteis sobre o repositório/projeto e da esteira de CD:
    * Squad(s): Planejamento.
    * Produto: tms-sync-1p-order-management-acl.
    * Projeto: tms-sync-1p-order-management-acl.
    * Aplicação(ões): tms-sync-1p-order-management-acl.
    * Branch no GitHub: release/0.0.6.
    * Versão: 0.0.6-2.
    * Repositório: github.com/viavarejo-internal/tms-sync-1p-order-management-acl.git.
    * Titulo da release no XL-Release: [Planejamento] tms-sync-1p-order-management-acl - 0.0.6-2.
    * Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/...
    * Severidade: 1
    * Matriz de impacto: NÃO
    * Aplicação crítica: NÃO
```

### 7.4 Padrões de Extração (Regex)

```go
// internal/servicenow/parser.go

// Padrões para extração do template da esteira de CD
var extractionPatterns = map[string]*regexp.Regexp{
    // Aplicação(ões): tms-sync-1p-order-management-acl.
    "application": regexp.MustCompile(`\* Aplicação\(ões\):\s*(.+?)\.`),

    // Versão: 0.0.6-2.
    "version": regexp.MustCompile(`\* Versão:\s*(.+?)\.`),

    // Repositório: github.com/viavarejo-internal/tms-sync-1p-order-management-acl.git.
    "repository": regexp.MustCompile(`\* Repositório:\s*github\.com/viavarejo-internal/(.+?)\.git`),

    // Squad(s): Planejamento.
    "squad": regexp.MustCompile(`\* Squad\(s\):\s*(.+?)\.`),

    // Branch no GitHub: release/0.0.6.
    "branch": regexp.MustCompile(`\* Branch no GitHub:\s*(.+?)\.`),

    // Issues do Jira (múltiplas)
    "jira_issues": regexp.MustCompile(`([A-Z]+-\d+)`),
}

// Função de extração
func ExtractFromDescription(description string) (*ExtractedData, error) {
    data := &ExtractedData{}

    // Aplicação
    if match := extractionPatterns["application"].FindStringSubmatch(description); len(match) > 1 {
        data.Application = strings.TrimSpace(match[1])
        data.ApplicationConfidence = 0.99  // Template padronizado = alta confiança
    }

    // Versão
    if match := extractionPatterns["version"].FindStringSubmatch(description); len(match) > 1 {
        data.Version = strings.TrimSpace(match[1])
        data.VersionConfidence = 0.99
    }

    // Repositório GitHub (BÔNUS - já vem no texto!)
    if match := extractionPatterns["repository"].FindStringSubmatch(description); len(match) > 1 {
        data.GitHubRepo = strings.TrimSpace(match[1])
        data.GitHubRepoConfidence = 0.99
    }

    // Squad (BÔNUS)
    if match := extractionPatterns["squad"].FindStringSubmatch(description); len(match) > 1 {
        data.Squad = strings.TrimSpace(match[1])
    }

    return data, nil
}
```

### 7.5 Mapeamento Atualizado

| Campo ServiceNow | Regex | Campo GitHub Releases | Confiança |
|------------------|-------|----------------------|-----------|
| `* Aplicação(ões): X.` | `Aplicação\(ões\):\s*(.+?)\.` | Nome do Deployment | 99% |
| `* Versão: X.` | `Versão:\s*(.+?)\.` | Tag da Nova Release | 99% |
| `* Repositório: .../{repo}.git` | `viavarejo-internal/(.+?)\.git` | Repositório GitHub | 99% |
| Busca na BD | API existente | Tag em Produção | 95% |

**BÔNUS**: O template já inclui o repositório GitHub diretamente, não preciso inferir!

### 7.4 Autenticação Azure AD SSO - Abordagem

```
┌─────────────────────────────────────────────────────────────────┐
│                    FLUXO OAUTH 2.0 + AZURE AD                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. App registrado no Azure AD (existente ou novo)              │
│                                                                 │
│  2. ServiceNow configurado como "Enterprise Application"        │
│                                                                 │
│  3. Fluxo OAuth 2.0:                                            │
│     a) GET /oauth2/v2.0/authorize → redirect com code           │
│     b) POST /oauth2/v2.0/token → access_token                   │
│     c) GET ServiceNow API com Bearer {access_token}             │
│                                                                 │
│  Alternativa: On-Behalf-Of (OBO) flow                           │
│     - Reutiliza token Azure AD do usuário logado                │
│     - Troca por token ServiceNow via OBO                        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Vantagem**: O projeto já autentica com Azure AD para RBAC, podemos estender para ServiceNow.

---

## 8. Integração XL Release (Digital.ai Release)

### 8.1 Objetivo

Adicionar botão **"Aprovar Deployment"** no painel de visualização da aba GitHub Releases que, ao ser clicado, aprova a task de gate no XL Release.

### 8.2 Fluxo Proposto

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     FLUXO DE APROVAÇÃO XL RELEASE                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. URL extraída do ServiceNow (Motivo da mudança):                         │
│     http://release.viavarejo.com.br/#/releases/Folder...Release...          │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  2. Backend extrai releaseId da URL:                                        │
│     releaseId = "Folder9fbbec04...-Releaseb83edcbe..."                      │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  3. Backend consulta release para obter tasks:                              │
│     GET /api/v1/releases/{releaseId}                                        │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  4. Frontend exibe botão "Aprovar Deployment" no painel                     │
│     (habilitado apenas se gate task estiver aguardando aprovação)           │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  5. Usuário clica no botão → Backend chama:                                 │
│     POST /api/v1/tasks/{taskId}/complete                                    │
│                                                                             │
│                              ▼                                              │
│                                                                             │
│  6. Task aprovada! Deployment prossegue no XL Release                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.3 API REST do XL Release

**Base URL**: `http://release.viavarejo.com.br`

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| `GET` | `/api/v1/releases/{releaseId}` | Obter detalhes da release (inclui phases e tasks) |
| `GET` | `/api/v1/tasks/{taskId}` | Obter detalhes de uma task específica |
| `POST` | `/api/v1/tasks/{taskId}/complete` | Completar/aprovar uma task |

**Referência**: [Digital.ai Release REST API](https://apidocs.digital.ai/xl-release/22.1.x/rest-docs/)

### 8.4 Extração do Release ID da URL

```go
// internal/xlrelease/parser.go

import "regexp"

// URL exemplo: http://release.viavarejo.com.br/#/releases/Folder9fbbec040721402eaf0acc000e0eae5b-Releaseb83edcbea463481787fc4259836585ad
var releaseURLPattern = regexp.MustCompile(`/releases/((?:Folder[a-f0-9]+-)*Release[a-f0-9]+)`)

func ExtractReleaseID(url string) (string, error) {
    match := releaseURLPattern.FindStringSubmatch(url)
    if len(match) < 2 {
        return "", fmt.Errorf("release ID não encontrado na URL")
    }
    // Converte para formato API: Applications/Folder...-Release...
    return "Applications/" + match[1], nil
}

// Exemplo:
// Input:  http://release.viavarejo.com.br/#/releases/Folder9fbbec040721402eaf0acc000e0eae5b-Releaseb83edcbea463481787fc4259836585ad
// Output: Applications/Folder9fbbec040721402eaf0acc000e0eae5b-Releaseb83edcbea463481787fc4259836585ad
```

### 8.5 Encontrar Task de Aprovação

```go
// internal/xlrelease/client.go

type Release struct {
    ID     string  `json:"id"`
    Title  string  `json:"title"`
    Status string  `json:"status"`
    Phases []Phase `json:"phases"`
}

type Phase struct {
    ID     string `json:"id"`
    Title  string `json:"title"`
    Status string `json:"status"`
    Tasks  []Task `json:"tasks"`
}

type Task struct {
    ID     string `json:"id"`
    Title  string `json:"title"`
    Type   string `json:"type"`
    Status string `json:"status"`
    // Gate tasks têm condições
    Conditions []Condition `json:"conditions,omitempty"`
}

// Encontra a task de gate aguardando aprovação
func (c *Client) FindPendingGateTask(releaseID string) (*Task, error) {
    release, err := c.GetRelease(releaseID)
    if err != nil {
        return nil, err
    }

    for _, phase := range release.Phases {
        for _, task := range phase.Tasks {
            // Procura por gate task com status IN_PROGRESS ou PENDING
            if isGateTask(task.Type) && isPendingApproval(task.Status) {
                return &task, nil
            }
        }
    }
    return nil, fmt.Errorf("nenhuma task de aprovação pendente encontrada")
}

func isGateTask(taskType string) bool {
    return taskType == "xlrelease.GateTask" ||
           taskType == "xlrelease.UserInputTask" ||
           strings.Contains(taskType, "Gate")
}

func isPendingApproval(status string) bool {
    return status == "IN_PROGRESS" || status == "PENDING_INPUT"
}
```

### 8.6 Aprovar Task

```go
// internal/xlrelease/client.go

func (c *Client) ApproveTask(taskID string) error {
    url := fmt.Sprintf("%s/api/v1/tasks/%s/complete", c.baseURL, taskID)

    req, err := http.NewRequest("POST", url, nil)
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", "application/json")
    req.SetBasicAuth(c.username, c.password) // ou Bearer token

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
        return fmt.Errorf("falha ao aprovar task: status %d", resp.StatusCode)
    }

    return nil
}
```

### 8.7 API REST Backend (Novos Endpoints)

```
POST /api/v1/xlrelease/config
  - Salva configuração (URL base, credenciais)
  - Criptografa credenciais

GET /api/v1/xlrelease/config/status
  - Retorna se está configurado

GET /api/v1/xlrelease/release?url={xlReleaseUrl}
  - Obtém detalhes da release
  - Retorna tasks pendentes de aprovação

POST /api/v1/xlrelease/approve
  - Body: { "release_url": "http://...", "task_id": "..." }
  - Aprova a task no XL Release
```

### 8.8 Frontend - Botão de Aprovação

#### No Painel de Visualização (ComparisonResult)

```tsx
// GitHubReleasesTab.tsx - Painel direito

{/* Seção de Aprovação XL Release */}
{xlReleaseUrl && (
  <Card className="mt-4">
    <CardHeader className="pb-2">
      <CardTitle className="text-sm font-medium flex items-center gap-2">
        <Rocket className="h-4 w-4" />
        Aprovar no XL Release
      </CardTitle>
    </CardHeader>
    <CardContent>
      <div className="space-y-3">
        {/* Info da Release */}
        <div className="text-sm text-muted-foreground">
          <p>Release: <span className="font-mono">{releaseTitle}</span></p>
          <p>Task: <span className="font-mono">{pendingTask?.title}</span></p>
          <p>Status: <Badge variant={pendingTask?.status === 'IN_PROGRESS' ? 'default' : 'secondary'}>
            {pendingTask?.status}
          </Badge></p>
        </div>

        {/* Botões */}
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => window.open(xlReleaseUrl, '_blank')}
          >
            <ExternalLink className="h-4 w-4 mr-2" />
            Abrir no XL Release
          </Button>

          <Button
            variant="default"
            size="sm"
            onClick={handleApproveDeployment}
            disabled={!pendingTask || isApproving}
            className="bg-green-600 hover:bg-green-700"
          >
            {isApproving ? (
              <Loader2 className="h-4 w-4 mr-2 animate-spin" />
            ) : (
              <CheckCircle2 className="h-4 w-4 mr-2" />
            )}
            Aprovar Deployment
          </Button>
        </div>

        {/* Confirmação após aprovação */}
        {approvalSuccess && (
          <Alert className="bg-green-50 border-green-200">
            <CheckCircle2 className="h-4 w-4 text-green-600" />
            <AlertDescription className="text-green-800">
              Deployment aprovado com sucesso! A esteira de CD continuará automaticamente.
            </AlertDescription>
          </Alert>
        )}
      </div>
    </CardContent>
  </Card>
)}
```

#### Modal de Confirmação

```tsx
// ApproveDeploymentModal.tsx

<Dialog open={showApproveModal} onOpenChange={setShowApproveModal}>
  <DialogContent>
    <DialogHeader>
      <DialogTitle className="flex items-center gap-2">
        <ShieldAlert className="h-5 w-5 text-amber-500" />
        Confirmar Aprovação
      </DialogTitle>
    </DialogHeader>

    <div className="space-y-4">
      <Alert variant="warning">
        <AlertCircle className="h-4 w-4" />
        <AlertDescription>
          Você está prestes a aprovar o deployment. Esta ação irá:
          <ul className="list-disc ml-4 mt-2">
            <li>Completar a task de gate no XL Release</li>
            <li>Permitir que o deployment prossiga para produção</li>
            <li>Esta ação <strong>não pode ser desfeita</strong></li>
          </ul>
        </AlertDescription>
      </Alert>

      <div className="bg-muted p-3 rounded-md text-sm">
        <p><strong>Release:</strong> {releaseTitle}</p>
        <p><strong>Aplicação:</strong> {applicationName}</p>
        <p><strong>Versão:</strong> {newVersion}</p>
        <p><strong>CHG:</strong> {changeNumber}</p>
      </div>

      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={() => setShowApproveModal(false)}>
          Cancelar
        </Button>
        <Button
          variant="default"
          className="bg-green-600 hover:bg-green-700"
          onClick={confirmApproval}
        >
          Confirmar Aprovação
        </Button>
      </div>
    </div>
  </DialogContent>
</Dialog>
```

### 8.9 Fluxo Completo Integrado

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     FLUXO COMPLETO: ServiceNow + XL Release                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Usuário cola URL da CHG do ServiceNow                                   │
│                                                                             │
│  2. Sistema extrai do "Motivo da mudança":                                  │
│     ├─ Aplicação: tms-sync-1p-order-management-acl                          │
│     ├─ Versão: 0.0.6-2                                                      │
│     ├─ Repositório: tms-sync-1p-order-management-acl                        │
│     └─ Link XL Release: http://release.viavarejo.com.br/...                 │
│                                                                             │
│  3. Campos são auto-preenchidos no formulário                               │
│                                                                             │
│  4. Usuário clica "Comparar Agora" → Exibe diff de commits                  │
│                                                                             │
│  5. Painel de visualização mostra:                                          │
│     ├─ Commits e arquivos alterados                                         │
│     ├─ Botão "Abrir no XL Release"                                          │
│     └─ Botão "Aprovar Deployment" (se task pendente)                        │
│                                                                             │
│  6. Usuário revisa as mudanças e clica "Aprovar Deployment"                 │
│                                                                             │
│  7. Modal de confirmação → Usuário confirma                                 │
│                                                                             │
│  8. Task aprovada no XL Release → Deploy prossegue                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.10 Extração do Link XL Release do ServiceNow

Adicionar ao parser do ServiceNow:

```go
// internal/servicenow/parser.go

var extractionPatterns = map[string]*regexp.Regexp{
    // ... padrões existentes ...

    // Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/...
    "xlrelease_url": regexp.MustCompile(`\* Link da release no XL-Release:\s*(.+)`),
}

// ExtractedData atualizado
type ExtractedData struct {
    Application             string
    ApplicationConfidence   float64
    Version                 string
    VersionConfidence       float64
    GitHubRepo              string
    GitHubRepoConfidence    float64
    Squad                   string
    XLReleaseURL            string  // NOVO
    XLReleaseURLConfidence  float64 // NOVO
    JiraIssues              []string
}
```

---

## 9. Alternativas Consideradas

### 8.1 Alternativa: Scraping da Página

**Descartada** porque:
- Frágil (quebra com mudanças de UI)
- Requer autenticação via browser (cookies)
- Violaria termos de uso do ServiceNow

### 8.2 Alternativa: Webhook do ServiceNow

**Possível para futuro** mas:
- Requer configuração no ServiceNow (Business Rule)
- Mais complexo de implementar
- Útil se quiser automação completa (CHG aprovada → auto-deploy)

### 8.3 Alternativa: Plugin ServiceNow

**Não aplicável**:
- Exigiria instalação de plugin na instância ServiceNow
- Dependeria de aprovação da equipe ServiceNow

---

## 9. Conclusão

### 9.1 Resumo

| Aspecto | Status |
|---------|--------|
| **Viabilidade Técnica** | SIM - API Table padrão do ServiceNow |
| **Complexidade** | Média - 3-4 dias de trabalho |
| **Dependências Externas** | Credenciais ServiceNow + nomes dos campos |
| **Risco Principal** | Campos customizados com nomes diferentes |
| **Benefício Principal** | Automação do preenchimento, reduz erros manuais |

### 9.2 Próximos Passos

1. **Confirmar nomes dos campos** no ServiceNow da ViaVarejo
2. **Obter credenciais** de usuário técnico para API
3. **Aprovar plano** com usuário
4. **Iniciar implementação** Fase 1 (Backend)

---

## 10. Referências

- [ServiceNow Table API](https://www.servicenow.com/docs/bundle/xanadu-api-reference/page/integrate/inbound-rest/concept/c_TableAPI.html)
- [ServiceNow REST API Explorer](https://www.servicenow.com/community/developer-forum/use-rest-api-to-get-details-of-a-change-request/m-p/1614200)
- [Change Management API](https://www.servicenow.com/docs/bundle/washingtondc-api-reference/page/integrate/inbound-rest/concept/change-management-api.html)
- Código atual: `internal/web/handlers/github_releases.go`
- Código atual: `internal/web/frontend/src/components/GitHubReleasesTab.tsx`
