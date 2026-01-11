# Plano de Implementação: GitHub Releases Compare

**Data**: 2026-01-11
**Versão**: 1.0
**Estimativa Total**: 18-26 horas

---

## 📋 Fases de Implementação

### **Fase 1: Base de Conhecimento + Backend Básico** (3-4 horas) ✅ EM ANDAMENTO

#### 1.1. Deployment Registry (SQLite)
- [ ] Criar `internal/storage/deployment_registry.go`
- [ ] Schema SQLite com tabela `deployments`
- [ ] Métodos: `UpsertDeployment()`, `SearchByAppName()`, `GetProductionVersion()`
- [ ] Testes unitários básicos

#### 1.2. Integração com Health Checking
- [ ] Modificar `internal/healthcheck/deployment_checker.go`
- [ ] Auto-populate registry durante varredura de deployments
- [ ] Extrair versão de labels + image tags

#### 1.3. GitHub Releases Handler
- [ ] Criar `internal/web/handlers/github_releases.go`
- [ ] Endpoint: `GET /api/v1/github/repos` (listar repos do config)
- [ ] Endpoint: `GET /api/v1/github/repos/:owner/:repo/releases`
- [ ] Endpoint: `GET /api/v1/github/repos/:owner/:repo/compare/:base...:head`
- [ ] Reutilizar `internal/updater/github.go` (client já existe)

#### 1.4. Deployment Search Endpoints
- [ ] Endpoint: `GET /api/v1/github/deployments/search?app_name=X`
- [ ] Endpoint: `GET /api/v1/github/deployments/production?app_name=X`
- [ ] Endpoint: `GET /api/v1/github/deployments/all-versions?app_name=X`

#### 1.5. Config de Repositórios
- [ ] Criar `~/.k8s-hpa-manager/github-repos.yaml` com schema
- [ ] Parser YAML no backend

#### 1.6. Rotas
- [ ] Registrar rotas em `internal/web/server.go`

**Critérios de Aceite Fase 1**:
- ✅ Health Checking popula base SQLite automaticamente
- ✅ Endpoints de busca retornam deployments corretamente
- ✅ GitHub API retorna releases e comparisons
- ✅ Base persiste entre execuções

---

### **Fase 2: Sistema de Token Individual** (3-4 horas)

#### 2.1. Token Storage
- [ ] Criar `internal/storage/github_tokens.go`
- [ ] Schema SQLite: tabela `github_tokens`
- [ ] Criptografia AES-256-GCM
- [ ] Métodos: `SaveToken()`, `GetToken()`, `ValidateToken()`

#### 2.2. Endpoints de Token
- [ ] `GET /api/v1/github/token/status` - Validar token atual
- [ ] `POST /api/v1/github/token` - Salvar token
- [ ] Middleware para autenticação por usuário (Azure AD email)

#### 2.3. Frontend - Modal de Token
- [ ] Criar `TokenConfigModal.tsx`
- [ ] Input de token (type="password")
- [ ] Validação e display de status
- [ ] Instruções de como obter token

**Critérios de Aceite Fase 2**:
- ✅ Token criptografado no SQLite
- ✅ Validação com GitHub API funciona
- ✅ Display de username e rate limit

---

### **Fase 3: Frontend Básico** (3-4 horas)

#### 3.1. Componente Principal
- [ ] Criar `GitHubReleasesTab.tsx`
- [ ] SplitView: painel esquerdo (config) + direito (resultados)
- [ ] Select de repositório
- [ ] Botão "Detectar Versão em Produção"
- [ ] Select de base tag
- [ ] Select de head tag
- [ ] Botão "Comparar Releases"

#### 3.2. API Client
- [ ] Adicionar métodos em `lib/api/client.ts`
- [ ] `getGitHubRepos()`, `getGitHubReleases()`, `compareGitHubReleases()`
- [ ] `searchDeployments()`, `getProductionDeployment()`

#### 3.3. Hooks React Query
- [ ] Criar `hooks/useGitHubReleases.ts`
- [ ] `useGitHubRepos()`, `useGitHubReleases()`, `useGitHubComparison()`

#### 3.4. Tabs de Resultados
- [ ] Tab "Commits": `CommitCard` component
- [ ] Tab "Arquivos": Tabela básica
- [ ] Tab "Release Notes": Grid 2 colunas

#### 3.5. Registro da Tab
- [ ] Modificar `Index.tsx` para incluir nova tab

**Critérios de Aceite Fase 3**:
- ✅ Tab aparece no menu principal
- ✅ Auto-detect busca versão em produção
- ✅ Comparação exibe commits e arquivos
- ✅ Release notes renderizadas

---

### **Fase 4: Filtro de Extensões** (1-2 horas)

#### 4.1. Backend
- [ ] Adicionar campo `extension` ao struct `GitHubFile`
- [ ] Lógica de extração de extensão

#### 4.2. Frontend
- [ ] Select de filtro por extensão
- [ ] Opções: .yaml, .yml, Dockerfile, .go, .ts, .md
- [ ] Badge com contador "X de Y arquivos"
- [ ] Lógica de filtragem (useState + useMemo)

**Critérios de Aceite Fase 4**:
- ✅ Filtro funciona corretamente
- ✅ Badge atualiza dinamicamente
- ✅ Performance boa (até 1000 arquivos)

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
- ✅ Fase 1: Base de conhecimento + Backend básico
- ✅ Fase 2: Token individual
- ✅ Fase 3: Frontend básico

**Should Have**:
- ✅ Fase 4: Filtro de extensões
- ✅ Fase 5: Análise de IA

**Nice to Have**:
- 🔶 Fase 6: Mapa de versões
- 🔶 Fase 7: Polish extra

---

## 📊 Tracking de Progresso

| Fase | Status | Estimativa | Real | Notas |
|------|--------|------------|------|-------|
| Fase 1 | 🟡 Em Andamento | 3-4h | - | Deployment registry + endpoints |
| Fase 2 | ⚪ Pendente | 3-4h | - | Token storage + criptografia |
| Fase 3 | ⚪ Pendente | 3-4h | - | Frontend básico |
| Fase 4 | ⚪ Pendente | 1-2h | - | Filtro extensões |
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
