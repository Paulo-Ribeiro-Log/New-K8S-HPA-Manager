# Plano de Implementação: GitHub Releases Compare

**Data**: 2026-01-11
**Versão**: 1.0
**Estimativa Total**: 18-26 horas

---

## 📋 Fases de Implementação

### **Fase 1: Base de Conhecimento + Backend Básico** ✅ **COMPLETA** (11/01/2026)

#### 1.1. Deployment Registry (SQLite) ✅
- ✅ Criar `internal/storage/deployment_registry.go` (348 linhas)
- ✅ Schema SQLite com tabelas `deployments` + `version_history`
- ✅ Campos adicionais: `squad`, `servicenow_task` (labels DevOps)
- ✅ Métodos: `UpsertDeployment()`, `SearchByAppName()`, `GetProductionVersion()`, `GetAllVersions()`
- ✅ Índices otimizados para busca rápida

#### 1.2. Integração com Health Checking ✅ **CORRIGIDO** (11/01/2026)
- ✅ Modificar `internal/healthcheck/deployment_checker.go`
- ✅ Auto-populate registry durante varredura de deployments
- ✅ Extrair versão de labels (`app.kubernetes.io/version`) + image tags
- ✅ **CORRIGIDO**: Extrair squad (`devops.k8s.io/component-squad`) - label correto
- ✅ Extrair ServiceNow task (`devops.k8s.io/servicenow-task-number`)
- ✅ Status de saúde automático (healthy/warning/critical)
- ℹ️ **NOTA**: Health Checking já filtra apenas Deployments (não inclui Pods soltos)

#### 1.3. GitHub Releases Handler ✅
- ✅ Criar `internal/web/handlers/github_releases.go` (389 linhas)
- ✅ Endpoint: `GET /api/v1/github/repos` (lista hierárquica)
- ✅ Endpoint: `GET /api/v1/github/repos/:owner/:repo/releases`
- ✅ Endpoint: `GET /api/v1/github/repos/:owner/:repo/compare/:basehead`
- ✅ Autenticação GitHub via `GITHUB_TOKEN` ou anônimo

#### 1.4. Deployment Search Endpoints ✅
- ✅ Endpoint: `GET /api/v1/github/deployments/search?app_name=X`
- ✅ Endpoint: `GET /api/v1/github/deployments/production?app_name=X`
- ✅ Endpoint: `GET /api/v1/github/deployments/all-versions?app_name=X`

#### 1.5. Config de Repositórios ✅
- ✅ Criar `github-repos.yaml.example` com estrutura hierárquica (117 linhas)
- ✅ Parser YAML no backend
- ✅ Documentação completa de labels esperados

#### 1.6. Rotas ✅
- ✅ Registrar rotas em `internal/web/server.go`
- ✅ Logger dedicado para GitHub handler

#### 1.7. Documentação ✅
- ✅ `ESTUDO_GITHUB_RELEASES.md` (891 linhas) - Análise técnica completa
- ✅ `PLANO_GITHUB_RELEASES.md` (283 linhas) - Roadmap de implementação

**Critérios de Aceite Fase 1**:
- ✅ Health Checking popula base SQLite automaticamente
- ✅ Endpoints de busca retornam deployments corretamente
- ✅ GitHub API retorna releases e comparisons
- ✅ Base persiste entre execuções
- ✅ Estrutura hierárquica (clusters → namespaces → deployments)
- ✅ Auto-detecção de versão em produção
- ✅ Compilação bem-sucedida

**Commit**: `e473cfca` - 173 arquivos, +62.963 linhas

---

### **Fase 2: Sistema de Token Individual** ✅ **COMPLETA** (11/01/2026)

#### 2.1. Token Storage ✅
- ✅ Criar `internal/storage/github_tokens.go` (281 linhas)
- ✅ Schema SQLite: tabela `github_tokens` (user_email PK, encrypted_token, created_at, updated_at)
- ✅ Criptografia AES-256-GCM com nonce aleatório (12 bytes)
- ✅ Métodos: `SaveToken()`, `GetToken()`, `DeleteToken()`, `HasToken()`
- ✅ Chave de criptografia em `~/.k8s-hpa-manager/.secret` (32 bytes, permissões 0600)
- ✅ Graceful degradation: sistema funciona mesmo se tokenStore falhar

#### 2.2. Endpoints de Token ✅
- ✅ Criar `internal/web/handlers/github_tokens.go` (236 linhas)
- ✅ `GET /api/v1/github/token/status` - Validar token atual (retorna username, email, rate limit)
- ✅ `POST /api/v1/github/token` - Salvar token (valida formato ghp_/gho_/etc antes de salvar)
- ✅ `DELETE /api/v1/github/token` - Remover token do usuário
- ✅ Middleware RBAC: `InjectUserEmail()` garante isolamento por usuário
- ✅ Validação com GitHub API antes de salvar (previne tokens inválidos)

#### 2.3. Integração com GitHub Releases Handler ✅
- ✅ Modificar `getGitHubClient()` para usar cadeia de prioridade de tokens:
  1. Token individual do usuário (via GitHubTokenStore)
  2. GITHUB_TOKEN global (variável de ambiente)
  3. Cliente não autenticado (60 req/h)
- ✅ Passar `tokenStore` para `NewGitHubReleasesHandler()` via DI
- ✅ Registrar rotas em `server.go` com RBAC middleware

#### 2.4. Frontend - Modal de Token
- [ ] Criar `TokenConfigModal.tsx`
- [ ] Input de token (type="password")
- [ ] Validação e display de status
- [ ] Instruções de como obter token

**Critérios de Aceite Fase 2 (Backend)**:
- ✅ Token criptografado no SQLite com AES-256-GCM
- ✅ Validação com GitHub API funciona (formato + live check)
- ✅ Display de username e rate limit (endpoint /status retorna tudo)
- ✅ Compilação bem-sucedida sem erros
- ✅ Integração com RBAC (user_email via middleware)
- ✅ Cadeia de fallback: user token → global token → unauthenticated

---

### **Fase 3: Frontend Básico** 🔄 **REVISADO** (11/01/2026 - 22:58)

**⚠️ DECISÃO DE ARQUITETURA IMPORTANTE (11/01/2026):**
- **GitHub Releases NÃO deve estar vinculada ao cluster selecionado**
- Releases são do GitHub, não específicas de cluster/namespace
- GitHubReleasesTab agora é independente (não recebe prop `cluster`)
- Instruções claras na interface sobre como popular a base via Health Checking
- Labels necessários documentados na UI: `devops.k8s.io/component-squad`, `devops.k8s.io/servicenow-task-number`

#### 3.1. Componente Principal ✅
- ✅ Criar `GitHubReleasesTab.tsx` (500+ linhas com SplitView)
- ✅ SplitView: painel esquerdo (config) + direito (resultados)
- ✅ Select de repositório (carregado do github-repos.yaml)
- ✅ **Campo de busca por deployment** (novo - não estava no plano original)
  - Input de busca com mínimo 3 caracteres
  - Integração com `/api/v1/github/deployments/search`
  - Cartões clicáveis com resultados (cluster, namespace, versão)
  - Auto-seleção do repositório ao clicar em resultado
- ✅ Botão "Detectar Versão em Produção" (usa endpoint `/production`)
- ✅ Select de base tag (com filtro de drafts)
- ✅ Select de head tag (com filtro de drafts)
- ✅ Botão "Comparar Releases" (desabilitado se base === head)

#### 3.2. API Client ✅
- ✅ Adicionar métodos em `lib/api/client.ts` (9 novos métodos)
- ✅ `getGitHubRepos()`, `getGitHubReleases()`, `compareGitHubReleases()`
- ✅ `searchDeployments()`, `getProductionDeployment()`, `getAllVersions()`
- ✅ `getGitHubTokenStatus()`, `saveGitHubToken()`, `deleteGitHubToken()`

#### 3.3. Hooks React Query ✅
- ✅ Criar `hooks/useGitHubReleases.ts` (8 hooks)
- ✅ `useGitHubRepos()`, `useGitHubReleases()`, `useGitHubComparison()`
- ✅ `useDeploymentSearch()` (com mínimo 3 caracteres)
- ✅ `useProductionDeployment()`, `useAllVersions()`
- ✅ `useGitHubTokenStatus()`, `useSaveGitHubToken()`, `useDeleteGitHubToken()`

#### 3.4. Tabs de Resultados ✅
- ✅ Tab "Commits": Cards com autor, data, SHA, link para GitHub
- ✅ Tab "Arquivos": Cards com status (added/modified/deleted), +/-, extension
- ✅ Tab "Release Notes": Grid 2 colunas (base vs head) com react-markdown + remark-gfm

#### 3.5. Token Config Modal ✅
- ✅ Criar `TokenConfigModal.tsx` (180+ linhas)
- ✅ Input de token (type="password" com toggle show/hide)
- ✅ Validação em tempo real com GitHub API
- ✅ Display de username, email, rate limit (remaining/limit)
- ✅ Instruções de como obter token (link para github.com/settings/tokens)
- ✅ Botões: Salvar, Remover, Cancelar
- ✅ Status colorido: verde (válido), vermelho (inválido), cinza (não configurado)

#### 3.6. Registro da Tab ✅
- ✅ Modificar `Index.tsx` para incluir nova tab (import + tabs array + switch case)
- ✅ Ícone: `GitCompareArrows` (lucide-react)

#### 3.7. Dependências ✅
- ✅ Instalar `react-markdown` + `remark-gfm` (18 novos pacotes)
- ✅ Compilação frontend bem-sucedida (4253 módulos, 13s)
- ✅ Compilação backend bem-sucedida (sem erros)

**Critérios de Aceite Fase 3**:
- ✅ Tab aparece no menu principal (11ª tab, após AI Diagnostics)
- ✅ Auto-detect busca versão em produção (endpoint `/production`)
- ✅ Comparação exibe commits e arquivos (3 tabs funcionais)
- ✅ Release notes renderizadas (markdown com syntax highlighting)
- ✅ **Campo de busca funcional** (busca por app_name, mínimo 3 chars)
- ✅ **Token modal integrado** (botão Settings no header esquerdo)
- ✅ Compilação completa (frontend + backend) sem erros

---

### **Fase 4: Filtro de Extensões** ✅ **COMPLETA** (11/01/2026)

#### 4.1. Backend ✅
- ✅ Campo `extension` já existe no struct `GitHubFile` (criado na Fase 1)
- ✅ Lógica de extração de extensão já implementada em `github_releases.go:286-289`

#### 4.2. Frontend ✅
- ✅ **Implementação diferente do plano original**: Toggle button ao invés de select dropdown
  - **Por padrão**: Exibe apenas arquivos de infraestrutura (.yaml, .yml, Dockerfile)
  - **Botão toggle**: "Mostrar Todos" / "Apenas Infra" (Eye/EyeOff icons)
  - **Justificativa**: Para análise de infra K8s, arquivos de código (.java, .go, .ts) não são relevantes
- ✅ Badge com contador "X de Y arquivos" (duplo):
  - Badge inline no TabsTrigger: `Arquivos (15)` com badge `15/47`
  - Badge no header da tab: "15 de 47 arquivos"
- ✅ Lógica de filtragem usando `useMemo` (filtra `.yaml`, `.yml`, `Dockerfile`)
- ✅ Alert informativo quando filtro ativo
- ✅ Mensagem quando nenhum arquivo de infra encontrado (sugere "Mostrar Todos")

**Implementação Técnica**:
- Estado: `showAllFiles` (boolean, default: false)
- useMemo: `filteredFiles` com lógica de filtragem por extensão
- Botão toggle com ícones Eye/EyeOff
- Alert com contagem de arquivos ocultos
- Mensagem de lista vazia quando filtro não encontra nada

**Critérios de Aceite Fase 4**:
- ✅ Filtro funciona corretamente (apenas .yaml, .yml, Dockerfile por padrão)
- ✅ Badge atualiza dinamicamente (inline no TabsTrigger + header da tab)
- ✅ Performance boa (useMemo garante cálculo eficiente)
- ✅ Toggle button intuitivo (Eye/EyeOff)
- ✅ UX melhorada com alerts informativos

---

### **Fase 5: Análise de IA** (4-6 horas)

#### 5.1. Backend - AI Analyzer
- [ ] Criar `internal/ai/github_analyzer.go`
- [ ] Método `AnalyzeFileChange()` - analisa diff individual
- [ ] Método `AnalyzeComparison()` - analisa todos os arquivos
- [ ] Categorização: infra, k8s, app
- [ ] Classificação de risco: low, medium, high

#### 5.2. Endpoint de Análise
- [ ] `POST /api/v1/github/repos/:owner/:repo/analyze`
- [ ] Processamento paralelo de arquivos
- [ ] Agregação de resultados

#### 5.3. Prompt Engineering
- [ ] Prompts especializados por categoria
- [ ] Regras explícitas de classificação de risco
- [ ] Template JSON para resposta estruturada

#### 5.4. Frontend - Tab Análise IA
- [ ] Cards de sumário (Alto/Médio/Baixo Risco)
- [ ] Accordion por categoria
- [ ] Alert de recomendação geral
- [ ] Loading states

**Critérios de Aceite Fase 5**:
- ✅ IA classifica riscos corretamente
- ✅ Análise completa em < 2 minutos (50 arquivos)
- ✅ Recomendações são úteis e acionáveis

---

### **Fase 6: Visualização Avançada** (2-3 horas)

#### 6.1. Tab Arquivos Completa
- [ ] Integrar filtro + análise IA
- [ ] Coluna "Análise IA" na tabela
- [ ] Badges coloridos de risco

#### 6.2. Tab Release Notes
- [ ] Renderização Markdown com `react-markdown`
- [ ] Grid responsivo 2 colunas
- [ ] Syntax highlighting para código

#### 6.3. Tab Commits
- [ ] Links para GitHub funcionais
- [ ] Avatar do autor (se disponível)
- [ ] Formatação de data relativa

#### 6.4. Mapa de Versões (Bonus)
- [ ] Nova tab "Mapa de Versões"
- [ ] Agrupamento por versão
- [ ] Cards de deployment por versão
- [ ] Indicador de produção

**Critérios de Aceite Fase 6**:
- ✅ Todas as tabs funcionam perfeitamente
- ✅ UI polida e profissional
- ✅ Links externos funcionam

---

### **Fase 7: Polish e Testes** (2-3 horas)

#### 7.1. Loading States
- [ ] Skeleton loaders em todos os selects
- [ ] Loading spinner durante comparação
- [ ] Progress bar na análise de IA

#### 7.2. Error Handling
- [ ] Mensagens amigáveis para rate limit
- [ ] Alerta quando token inválido
- [ ] Fallback quando deployment não encontrado

#### 7.3. Testes
- [ ] Testar com repositórios reais
- [ ] Testar análise IA com diferentes arquivos
- [ ] Testar auto-detect em múltiplos ambientes
- [ ] Validar performance

#### 7.4. Documentação
- [ ] Atualizar CLAUDE.md
- [ ] Adicionar seção no README (se aplicável)
- [ ] Comentários inline no código

**Critérios de Aceite Fase 7**:
- ✅ Zero erros console
- ✅ Performance aceitável (< 2s para comparação)
- ✅ Documentação completa

---

## 🎯 Priorização

**Must Have (MVP)**:
- ✅ Fase 1: Base de conhecimento + Backend básico ✅ **COMPLETA**
- ✅ Fase 2: Token individual ✅ **COMPLETA**
- ✅ Fase 3: Frontend básico ✅ **COMPLETA**

**Should Have**:
- ✅ Fase 4: Filtro de extensões ✅ **COMPLETA**
- ⚪ Fase 5: Análise de IA

**Nice to Have**:
- 🔶 Fase 6: Mapa de versões
- 🔶 Fase 7: Polish extra

---

## 📊 Tracking de Progresso

| Fase | Status | Estimativa | Real | Notas |
|------|--------|------------|------|-------|
| Fase 1 | 🟢 Completo | 3-4h | ~4h | Deployment registry + endpoints + Health Check integration |
| Fase 2 | 🟢 Completo | 3-4h | ~2h | Token storage + criptografia + endpoints + RBAC |
| Fase 3 | 🟢 Completo | 3-4h | ~3h | Frontend básico + Token modal + Campo de busca (extra) |
| Fase 4 | 🟢 Completo | 1-2h | ~0.5h | Filtro inteligente (toggle button) - Mais simples que plano original |
| Fase 5 | ⚪ Pendente | 4-6h | - | AI analysis |
| Fase 6 | ⚪ Pendente | 2-3h | - | Visualização |
| Fase 7 | ⚪ Pendente | 2-3h | - | Polish |

**Legenda**:
- 🟢 Completo
- 🟡 Em Andamento
- ⚪ Pendente
- 🔴 Bloqueado

---

## 🔗 Arquivos Chave

### Backend
- `internal/storage/deployment_registry.go` - Base de conhecimento SQLite
- `internal/healthcheck/deployment_checker.go` - Auto-populate
- `internal/web/handlers/github_releases.go` - REST API
- `internal/storage/github_tokens.go` - Token storage
- `internal/ai/github_analyzer.go` - AI analysis

### Frontend
- `internal/web/frontend/src/components/GitHubReleasesTab.tsx` - Componente principal
- `internal/web/frontend/src/hooks/useGitHubReleases.ts` - React Query hooks
- `internal/web/frontend/src/lib/api/client.ts` - API client

### Config
- `~/.k8s-hpa-manager/github-repos.yaml` - Repositórios configurados
- `~/.k8s-hpa-manager/deployment-registry.db` - Base SQLite
- `~/.k8s-hpa-manager/github-tokens.db` - Tokens criptografados

---

**Última Atualização**: 2026-01-11
**Próxima Revisão**: Após completar Fase 1
