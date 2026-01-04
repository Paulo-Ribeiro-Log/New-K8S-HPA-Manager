# Progresso: AI Diagnostics - Implementação

**Data de início**: 21/12/2025
**Status**: 🟢 Backend 100% | Frontend 100% | UI Integration 0%
**Última atualização**: 21/12/2025 20:30

---

## 📊 Resumo Executivo

Sistema de diagnósticos AI para recursos Kubernetes (Pods, Deployments, HPAs, Nodes) com **Gemini API** e **Ollama** como providers, sanitização de dados sensíveis e histórico persistente em SQLite.

**Objetivo**: Analisar problemas de recursos K8s usando IA, fornecendo sugestões de ação corretiva sem execução automática.

### 🎯 Fase 1 - Backend Integration: ✅ CONCLUÍDA (21/12/2025 19:00)

**Implementação realizada:**
- ✅ Flags CLI adicionadas (`--ai-provider`, `--ollama-url`, `--ollama-model`)
- ✅ AI system inicializado no `internal/web/server.go`
- ✅ 6 rotas REST registradas (`/api/v1/ai/*`)
- ✅ Endpoint `/api/v1/ai/status` testado com sucesso
- ✅ Detecção automática de `GEMINI_API_KEY` (variável de ambiente)
- ✅ Graceful degradation (AI desabilitado se inicialização falhar)

**Commits:**
- `a6a7686` - feat: implementa backend completo do AI Diagnostics (90%)
- `a108bdc` - feat: integra backend AI Diagnostics com servidor web
- `20cd444` - docs: atualiza status AI Diagnostics para Opção 1 concluída

### 🎯 Fase 2 - Frontend Base: ✅ CONCLUÍDA (21/12/2025 20:30)

**Implementação realizada:**
- ✅ Tipos TypeScript criados (`src/types/ai.ts`)
- ✅ 6 métodos AI adicionados ao API client
- ✅ Hook `useAIDiagnostics` com gerenciamento de estado completo
- ✅ Componente `AIAnalysisCard` com suporte Markdown e badges de prioridade
- ✅ Componente `AIHistoryPanel` com filtros e datas relativas
- ✅ Componente `AIDiagnosticsTab` (aba principal) com status e estatísticas
- ✅ Componente `AITriggerButton` reutilizável
- ✅ Integração no `Index.tsx` com nova aba "AI Diagnostics"
- ✅ Dependência `react-markdown` instalada
- ✅ Build do frontend bem-sucedido

**Commits:**
- `36026ee` - feat: implementa frontend completo AI Diagnostics (Fase 2)

---

## ✅ Implementado (Backend)

### 1. Módulo `internal/sanitizer/` ✅

**Arquivos criados:**
- `models.go` - Estruturas de configuração e resultados
- `patterns.go` - Regex patterns para IPs, emails, tokens, secrets
- `kubernetes.go` - Sanitização específica de recursos K8s
- `sanitizer.go` - Interface principal + implementação

**Funcionalidades:**
- ✅ Mascaramento de IPv4 (192.168.1.1 → X.X.X.X)
- ✅ Mascaramento de emails (user@example.com → user@REDACTED)
- ✅ Mascaramento de tokens JWT, Bearer, UUID, Base64
- ✅ Mascaramento de API Keys e Passwords (com contexto)
- ✅ Sanitização de Pods (env vars sensíveis)
- ✅ Sanitização de Secrets (mantém chaves, remove valores)
- ✅ Sanitização de ConfigMaps (chaves sensíveis)
- ✅ Sanitização de Events (mensagens)
- ✅ Sanitização de Logs (linha a linha)
- ✅ Sanitização recursiva de JSON/YAML

**Chaves sensíveis detectadas:**
```
password, passwd, pwd, secret, token, apikey, api_key,
authorization, auth, certificate, cert, key, private, credential
```

---

### 2. Módulo `internal/collectors/` ✅

**Arquivos criados:**
- `models.go` - Estruturas de contexto (DiagnosticContext, PodContext, etc)
- `pod_collector.go` - Coleta contexto de Pods
- `deployment_collector.go` - Coleta contexto de Deployments
- `hpa_collector.go` - Coleta contexto de HPAs
- `node_collector.go` - Coleta contexto de Nodes
- `context_builder.go` - Orquestrador de coleta

**Funcionalidades:**

#### PodCollector
- ✅ Manifesto completo do Pod
- ✅ Logs dos containers (últimas 500 linhas, configurável)
- ✅ Logs anteriores (se CrashLoopBackOff)
- ✅ Recursos relacionados (Deployment, ConfigMaps, Secrets)
- ✅ Informações do Node onde o pod está rodando

#### DeploymentCollector
- ✅ Manifesto YAML completo
- ✅ Lista de ReplicaSets associados
- ✅ Lista de Pods gerenciados (com status detalhado)
- ✅ Status do rollout (Available, Updated, Ready)

#### HPACollector
- ✅ Manifesto completo do HPA
- ✅ Verificação se está "maxed out" (currentReplicas == maxReplicas)
- ✅ Métricas atuais (CPU, Memory, Pods)
- ✅ Nome do deployment alvo

#### NodeCollector
- ✅ Manifesto YAML completo
- ✅ Conditions (Ready, MemoryPressure, DiskPressure, etc)
- ✅ Taints aplicados
- ✅ Allocatable vs Capacity
- ✅ Lista de Pods rodando no node

#### ContextBuilder
- ✅ Orquestração de todos os collectors
- ✅ Coleta de eventos K8s (últimos 50)
- ✅ Suporte a kubectl describe (opcional)
- ✅ Placeholder para métricas Prometheus (TODO)

---

### 3. Módulo `internal/storage/` ✅

**Arquivos criados:**
- `models.go` - Estruturas de histórico (HistoryRecord, QueryFilters)
- `sqlite.go` - Cliente SQLite com WAL mode
- `migrations.go` - Schema DDL + índices
- `ai_history_store.go` - CRUD completo de histórico

**Funcionalidades:**
- ✅ SQLite com WAL mode (melhor concorrência)
- ✅ Schema com 5 índices otimizados
- ✅ CRUD completo (Save, GetByID, Query, Delete)
- ✅ Filtros avançados (cluster, namespace, resource, provider, user, data)
- ✅ Paginação (limit + offset)
- ✅ Estatísticas (total, por provider, por tipo, tempo médio)
- ✅ Busca textual em análises anteriores
- ✅ Cleanup (DeleteOlderThan)
- ✅ VACUUM para otimização

**Schema SQLite:**
```sql
CREATE TABLE ai_analysis_history (
    id TEXT PRIMARY KEY,
    resource_type TEXT NOT NULL,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT,
    analysis TEXT NOT NULL,
    suggestions JSON,
    tokens_used INTEGER,
    response_time REAL,
    analyzed_at DATETIME NOT NULL,
    user_email TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Índices: resource, analyzed_at, provider, user_email, resource_type
```

**Path do banco**: `./build/ai_diagnostics.db` (auto-criado)

---

### 4. Módulo `internal/ai/` ✅

**Arquivos criados:**
- `models.go` - Estruturas de análise (AnalysisResult, Suggestion)
- `config.go` - Configuração de providers
- `provider.go` - Interface base + factory
- `gemini_provider.go` - Implementação Gemini API
- `ollama_provider.go` - Implementação Ollama local
- `prompts.go` - Templates de prompts (Pod, Deployment, HPA, Node)
- `analyzer.go` - Orquestrador completo (Context → AI → Result)

**Funcionalidades:**

#### Config
- ✅ Suporte a Gemini e Ollama
- ✅ Lê `GEMINI_API_KEY` de env var automaticamente
- ✅ Defaults: Gemini (gemini-pro), Ollama (llama3.2)
- ✅ Timeout configurável (padrão: 120s)

#### Gemini Provider
- ✅ Integração com Gemini API REST
- ✅ Endpoint: `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`
- ✅ Parsing de resposta JSON (candidates[0].content.parts[0].text)
- ✅ IsAvailable() check com timeout de 5s

#### Ollama Provider
- ✅ Integração com Ollama local
- ✅ Endpoint: `http://localhost:11434/api/generate`
- ✅ Modo non-streaming para simplificar
- ✅ IsAvailable() check via `/api/tags`

#### Prompts
Templates especializados para cada tipo de recurso:
- ✅ **Pod Template**: Root Cause, Impact, Actions, kubectl commands
- ✅ **Deployment Template**: Rollout issues, replicas, pod failures
- ✅ **HPA Template**: Scaling issues, maxed out, flapping, metrics
- ✅ **Node Template**: NotReady, resource pressure, taints

**Formato de prompt:**
```
You are a Kubernetes expert specializing in {ResourceType} troubleshooting.

**Your response must include:**
1. Root Cause Analysis
2. Impact Assessment (severity + scope)
3. Recommended Actions (step-by-step)
4. kubectl Commands (prefix with $)

**Diagnostic Context (JSON):**
{sanitized_context_json}
```

#### Analyzer
- ✅ Orquestração completa: Collect → Sanitize → Prompt → AI → Extract Suggestions → Save
- ✅ Extração de sugestões de comandos kubectl (regex `^\$ ...`)
- ✅ Inferência de prioridade (critical, high, medium, low)
- ✅ Inferência de tipo (investigate, fix, scale, rollback, update, delete)
- ✅ Salvamento automático no histórico SQLite
- ✅ GetProviderStatus() para health checks

**Tipos de Sugestão:**
- `investigate` - Comandos de investigação (logs, describe, get)
- `fix` - Correções genéricas
- `scale` - Operações de scaling
- `rollback` - Rollback de deployments
- `update` - Updates/patches
- `delete` - Deleção de recursos

---

### 5. Handler HTTP `internal/web/handlers/ai_diagnostics.go` ✅

**Rotas implementadas:**
- `POST /api/v1/ai/analyze` - Executar análise AI
- `GET /api/v1/ai/history` - Listar histórico (com filtros)
- `GET /api/v1/ai/history/:id` - Obter análise específica
- `GET /api/v1/ai/status` - Status do provider (available/unavailable)
- `GET /api/v1/ai/stats` - Estatísticas do histórico
- `DELETE /api/v1/ai/history/:id` - Deletar análise

**Request Body (POST /analyze):**
```json
{
  "resource_type": "Pod",
  "cluster": "akspriv-prod",
  "namespace": "default",
  "resource_name": "my-app-xyz",
  "include_logs": true,
  "include_metrics": false,
  "include_describe": true
}
```

**Response (análise completa):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "resource_type": "Pod",
  "cluster": "akspriv-prod",
  "namespace": "default",
  "resource_name": "my-app-xyz",
  "provider": "gemini",
  "model": "gemini-pro",
  "analysis": "**Root Cause**:\nImagePullBackOff...",
  "suggestions": [
    {
      "type": "investigate",
      "description": "Check pod logs for errors",
      "command": "kubectl logs my-app-xyz",
      "priority": "high"
    }
  ],
  "response_time": 3.2,
  "analyzed_at": "2025-12-21T10:00:00Z"
}
```

**Filtros de histórico (GET /history):**
- `?cluster=...`
- `?namespace=...`
- `?resource_type=...`
- `?resource_name=...`
- `?provider=gemini|ollama`
- `?user_email=...`
- `?start_date=2025-12-20T00:00:00Z`
- `?end_date=2025-12-21T23:59:59Z`
- `?limit=50` (padrão)
- `?offset=0` (paginação)

---

### 6. Módulo `internal/kubernetes/manager.go` ✅

**Criado para compatibilidade** entre sistema existente (`config.KubeConfigManager`) e novo módulo AI.

**Interface:**
```go
type KubeManager struct {
    getClientFunc func(string) (kubernetes.Interface, error)
    executeKubectlDescribe func(...) (string, error)
}
```

Permite injetar funções do sistema existente sem criar dependência circular.

---

## ⏳ Pendente (Backend)

### 1. Integração no Servidor Web (cmd/web.go + internal/web/server.go)

**Flags CLI a adicionar:**
```bash
--ai-provider gemini|ollama     # Provider AI (padrão: gemini)
--ollama-url http://...         # URL do Ollama (padrão: localhost:11434)
--ollama-model llama3.2         # Modelo Ollama (padrão: llama3.2)
```

**Nota**: `--gemini-api-key` não é necessário (usa env var `GEMINI_API_KEY`)

**Inicialização no server.go:**
```go
// 1. Criar SQLite client
sqliteClient, _ := storage.NewSQLiteClient("./build/ai_diagnostics.db")
historyStore := storage.NewAIHistoryStore(sqliteClient)

// 2. Criar AI config
aiConfig := &ai.Config{
    Provider: flags.aiProvider,  // "gemini" ou "ollama"
    OllamaBaseURL: flags.ollamaURL,
    OllamaModel: flags.ollamaModel,
    Timeout: 120,
}

// 3. Criar AI provider
provider, _ := ai.NewProvider(aiConfig)

// 4. Criar KubeManager wrapper
kubeManager := kubernetes.NewKubeManager(
    kubeConfigManager.GetClient,
    // kubectl describe function
)

// 5. Criar AI Analyzer
analyzer := ai.NewAnalyzer(provider, kubeManager, historyStore)

// 6. Criar handler
aiHandler := handlers.NewAIDiagnosticsHandler(analyzer, historyStore)

// 7. Registrar rotas
apiV1 := router.Group("/api/v1")
aiHandler.RegisterRoutes(apiV1, kubeManager)
```

---

## ⏳ Pendente (Frontend)

### Componentes React a criar

**1. `src/types/ai.ts`** - Tipos TypeScript
```typescript
export interface AnalysisResult {
  id: string;
  resource_type: string;
  cluster: string;
  namespace: string;
  resource_name: string;
  provider: string;
  model: string;
  analysis: string;
  suggestions: Suggestion[];
  response_time: number;
  analyzed_at: string;
}

export interface Suggestion {
  type: 'investigate' | 'fix' | 'scale' | 'rollback' | 'update' | 'delete';
  description: string;
  command?: string;
  priority: 'low' | 'medium' | 'high' | 'critical';
}
```

**2. `src/lib/api/client.ts`** - Métodos API
```typescript
async analyzeResource(req: AnalyzeRequest): Promise<AnalysisResult>
async getAIHistory(filters: HistoryFilters): Promise<HistoryResponse>
async getAnalysisById(id: string): Promise<AnalysisResult>
async getAIProviderStatus(): Promise<ProviderStatus>
```

**3. `src/hooks/useAIDiagnostics.ts`** - React Query hook
```typescript
export const useAIDiagnostics = () => {
  const analyzeMutation = useMutation(...)
  const { data: history } = useQuery('ai-history', ...)
  const { data: status } = useQuery('ai-status', ...)
  return { analyzeMutation, history, status }
}
```

**4. `src/components/AIAnalysisCard.tsx`** - Card de resultado
- Exibe análise markdown
- Lista de sugestões com badges de prioridade
- Botão "Copy" para comandos kubectl
- Indicador de tempo de resposta

**5. `src/components/AIHistoryPanel.tsx`** - Lista de histórico
- Timeline de análises anteriores
- Filtros (cluster, namespace, resource, data)
- Paginação
- Click para expandir análise completa

**6. `src/components/AIDiagnosticsTab.tsx`** - Aba principal
- Seletor de cluster/namespace/resource
- Botão "Analisar com AI"
- Progress indicator durante análise
- Exibe AIAnalysisCard com resultado
- Exibe AIHistoryPanel com histórico

**7. `src/components/AITriggerButton.tsx`** - Botão contextual reutilizável
```tsx
<AITriggerButton
  resourceType="Pod"
  cluster={cluster}
  namespace={namespace}
  resourceName={podName}
  disabled={analyzing}
/>
```

### Abas a modificar (adicionar botão AI)

- **PodsPanel.tsx** - Botão em pods com problemas (CrashLoopBackOff, Error, etc)
- **DeploymentsTab.tsx** - Botão em deployments com falhas
- **HPAPanel.tsx** - Botão em HPAs maxed out
- **NodesPanel.tsx** - Botão em nodes com problemas (NotReady, Pressure, etc)

---

## 📝 Arquivos Criados (20 arquivos)

### Backend (Go)
```
internal/
├── sanitizer/
│   ├── models.go              ✅ 75 linhas
│   ├── patterns.go            ✅ 90 linhas
│   ├── kubernetes.go          ✅ 165 linhas
│   └── sanitizer.go           ✅ 155 linhas
│
├── collectors/
│   ├── models.go              ✅ 155 linhas
│   ├── pod_collector.go       ✅ 200 linhas
│   ├── deployment_collector.go ✅ 100 linhas
│   ├── hpa_collector.go       ✅ 55 linhas
│   ├── node_collector.go      ✅ 90 linhas
│   └── context_builder.go     ✅ 140 linhas
│
├── storage/
│   ├── models.go              ✅ 35 linhas
│   ├── sqlite.go              ✅ 90 linhas
│   ├── migrations.go          ✅ 40 linhas
│   └── ai_history_store.go    ✅ 290 linhas
│
├── ai/
│   ├── models.go              ✅ 70 linhas
│   ├── config.go              ✅ 85 linhas
│   ├── provider.go            ✅ 25 linhas
│   ├── gemini_provider.go     ✅ 115 linhas
│   ├── ollama_provider.go     ✅ 105 linhas
│   ├── prompts.go             ✅ 140 linhas
│   └── analyzer.go            ✅ 270 linhas
│
├── kubernetes/
│   └── manager.go             ✅ 35 linhas (wrapper)
│
└── web/handlers/
    └── ai_diagnostics.go      ✅ 180 linhas
```

**Total Backend**: **20 arquivos** | **~2,500 linhas de código Go**

---

## 🔍 Próximas Ações (Ordem de Prioridade)

### Fase 1: Backend Integration (1 hora)
1. ✅ Módulos criados (sanitizer, collectors, storage, ai, handlers)
2. ⏳ Adicionar flags CLI ao `cmd/web.go`
3. ⏳ Inicializar AI system no `internal/web/server.go`
4. ⏳ Registrar rotas do handler
5. ⏳ Testar endpoint `/api/v1/ai/status`
6. ⏳ Testar análise manual via `curl`

### Fase 2: Frontend Base (2 dias)
7. ⏳ Criar tipos TypeScript (`src/types/ai.ts`)
8. ⏳ Adicionar métodos API (`src/lib/api/client.ts`)
9. ⏳ Criar hook `useAIDiagnostics`
10. ⏳ Criar `AIAnalysisCard.tsx`
11. ⏳ Criar `AIHistoryPanel.tsx`
12. ⏳ Criar `AIDiagnosticsTab.tsx`

### Fase 3: UI Integration (1 dia)
13. ⏳ Criar `AITriggerButton.tsx`
14. ⏳ Adicionar botão AI em `PodsPanel.tsx`
15. ⏳ Adicionar botão AI em `DeploymentsTab.tsx`
16. ⏳ Adicionar botão AI em `HPAPanel.tsx`
17. ⏳ Adicionar botão AI em `NodesPanel.tsx`

### Fase 4: Testing & Polish (1 dia)
18. ⏳ Testar análise de Pod com CrashLoopBackOff
19. ⏳ Testar análise de HPA maxed out
20. ⏳ Testar histórico e filtros
21. ⏳ Ajustes finais de UX

---

## 🧪 Testes Manuais Planejados

### Backend
```bash
# 1. Verificar status do provider
curl http://localhost:8080/api/v1/ai/status

# 2. Analisar um pod problemático
curl -X POST http://localhost:8080/api/v1/ai/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "resource_type": "Pod",
    "cluster": "akspriv-prod",
    "namespace": "default",
    "resource_name": "problematic-pod-xyz",
    "include_logs": true,
    "include_describe": true
  }'

# 3. Listar histórico
curl http://localhost:8080/api/v1/ai/history?limit=10

# 4. Obter estatísticas
curl http://localhost:8080/api/v1/ai/stats
```

### Frontend
1. Abrir aba "AI Diagnostics"
2. Selecionar cluster/namespace
3. Clicar em "Analisar com AI" em um pod problemático
4. Verificar análise retornada
5. Copiar comando kubectl sugerido
6. Verificar histórico de análises

---

## 📊 Estatísticas de Implementação

- **Linhas de código**: ~2,500 (Go) + ~0 (TypeScript pendente)
- **Arquivos criados**: 20 (Backend) + 0 (Frontend pendente)
- **Módulos novos**: 5 (sanitizer, collectors, storage, ai, kubernetes/manager)
- **Endpoints API**: 6
- **Tempo estimado restante**: 4 dias (backend integration + frontend completo)
- **Progresso geral**: 50% (backend quase completo, frontend não iniciado)

---

## 🎯 Critérios de Aceitação

- [ ] Backend compila sem erros
- [ ] Servidor web inicia com flags AI
- [ ] Endpoint `/api/v1/ai/status` retorna status do provider
- [ ] Análise de Pod retorna resultado válido com sugestões
- [ ] Histórico SQLite armazena análises corretamente
- [ ] Dados sensíveis são sanitizados (IPs, tokens, secrets)
- [ ] Frontend exibe análise com markdown renderizado
- [ ] Botões contextuais aparecem em recursos problemáticos
- [ ] Comandos kubectl são copiáveis com 1 clique

---

## 📌 Notas Importantes

1. **Gemini API Key**: Já configurado em `~/zsh` via `GEMINI_API_KEY`
2. **Provider padrão**: Gemini (não precisa de flag `--ai-provider` se usar Gemini)
3. **Ollama**: Requer Ollama rodando em `http://localhost:11434` (ou custom via flag)
4. **SQLite**: Banco criado automaticamente em `./build/ai_diagnostics.db`
5. **Timeout**: 120 segundos para análises AI (configurável)
6. **Sanitização**: SEMPRE aplicada antes de enviar para IA (segurança crítica)
7. **Automação**: NÃO implementada - apenas sugestões (conforme requisito)

---

## 🔗 Referências

- **Plano original**: `/PLANO_AI_DIAGNOSTICS.md`
- **Gemini API Docs**: https://ai.google.dev/docs
- **Ollama API Docs**: https://github.com/ollama/ollama/blob/main/docs/api.md
- **Projeto**: New K8s HPA Manager v1.3.5+

---

**Última atualização**: 21/12/2025 - Backend 90% completo, aguardando integração no servidor web
