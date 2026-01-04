# Plano de Implementação: AI-Powered Diagnostics para K8s HPA Manager

## 📋 Resumo Executivo

Implementar sistema completo de diagnósticos inteligentes com IA para recursos Kubernetes (Pods, Deployments, HPAs, Nodes), utilizando **Gemini API** ou **Ollama local** como providers, com sanitização de dados sensíveis e histórico persistente em SQLite.

**Escopo MVP**: Análise completa de Pods, Deployments, HPAs e Nodes com sugestões de ações corretivas (sem automação).

---

## 🎯 Requisitos Validados com Usuário

✅ **AI Providers**: Gemini API + Ollama local (seleção via CLI: `--gemini` ou `--ollama`)
✅ **Escopo**: Pods, Deployments, HPAs, Nodes (MVP completo)
✅ **UI**: Nova aba "AI Diagnostics" + botões contextuais nas abas existentes
✅ **Automação**: Apenas sugestões (sem execução automática)
✅ **Sanitização**: Mascarar IPs, tokens, secrets, emails antes de enviar para IA
✅ **Histórico**: SQLite para armazenar análises anteriores

---

## 🏗️ Arquitetura de Alto Nível

```
┌─────────────────────────────────────────────────────────────┐
│                    FRONTEND (React/TS)                       │
│  • AIDiagnosticsTab (aba principal)                         │
│  • AITriggerButton (botões contextuais em Pods/HPAs/etc)    │
│  • AIAnalysisCard (exibição de resultados)                  │
│  • AIHistoryPanel (histórico de análises)                   │
└─────────────────────────────────────────────────────────────┘
                            ↓ HTTP POST
┌─────────────────────────────────────────────────────────────┐
│                    BACKEND (Go)                              │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Handler: ai_diagnostics.go                         │   │
│  │  POST /api/v1/ai/analyze                            │   │
│  │  GET  /api/v1/ai/history                            │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Context Builder (collectors/)                      │   │
│  │  • PodCollector    → Logs, Events, Describe         │   │
│  │  • DeploymentCollector → Rollout, ReplicaSets       │   │
│  │  • HPACollector    → Métricas, Scaling History      │   │
│  │  • NodeCollector   → Conditions, Resources          │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Sanitizer (sanitizer/)                             │   │
│  │  • Mascarar IPs, Tokens, Secrets, Emails           │   │
│  │  • Manter estrutura para análise AI                │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  AI Analyzer (ai/)                                  │   │
│  │  • PromptBuilder → Templates por tipo de recurso    │   │
│  │  • Provider Interface                               │   │
│  │    - GeminiProvider (API)                           │   │
│  │    - OllamaProvider (Local)                         │   │
│  └─────────────────────────────────────────────────────┘   │
│                            ↓                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Storage (storage/)                                 │   │
│  │  • SQLite: ai_diagnostics.db                        │   │
│  │  • CRUD de histórico de análises                   │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## 📁 Estrutura de Diretórios e Arquivos

### Backend (Go) - NOVOS MÓDULOS

```
internal/
├── ai/                                    ⭐ NOVO MÓDULO
│   ├── config.go                          # Configuração de providers
│   ├── provider.go                        # Interface base
│   ├── gemini_provider.go                 # Implementação Gemini
│   ├── ollama_provider.go                 # Implementação Ollama
│   ├── analyzer.go                        # Orquestrador de análises
│   ├── prompts.go                         # Templates de prompts
│   └── models.go                          # AnalysisResult, Suggestion
│
├── collectors/                            ⭐ NOVO MÓDULO
│   ├── context_builder.go                 # Orquestrador de coleta
│   ├── pod_collector.go                   # Coleta contexto de Pods
│   ├── deployment_collector.go            # Coleta contexto de Deployments
│   ├── hpa_collector.go                   # Coleta contexto de HPAs
│   ├── node_collector.go                  # Coleta contexto de Nodes
│   └── models.go                          # DiagnosticContext, PodContext, etc
│
├── sanitizer/                             ⭐ NOVO MÓDULO
│   ├── sanitizer.go                       # Interface + lógica principal
│   ├── patterns.go                        # Regex patterns (IPs, emails, tokens)
│   ├── kubernetes.go                      # Sanitização K8s (secrets, env vars)
│   └── models.go                          # SanitizationConfig
│
├── storage/                               ⭐ NOVO MÓDULO
│   ├── sqlite.go                          # Cliente SQLite base
│   ├── ai_history_store.go                # CRUD de histórico
│   ├── migrations.go                      # Schema DDL
│   └── models.go                          # HistoryRecord, QueryFilters
│
└── web/
    └── handlers/
        └── ai_diagnostics.go              ⭐ NOVO HANDLER
```

### Backend (Go) - MODIFICAÇÕES

```
cmd/
└── web.go                                 ✏️ MODIFICAR
    # Adicionar flags: --ai-provider, --gemini-api-key, --ollama-url, --ollama-model
    # Inicializar AI provider e passar para server
```

### Frontend (React/TS) - NOVOS COMPONENTES

```
internal/web/frontend/src/
├── components/
│   ├── AIDiagnosticsTab.tsx               ⭐ NOVO (aba principal)
│   ├── AIAnalysisCard.tsx                 ⭐ NOVO (card de resultado)
│   ├── AIHistoryPanel.tsx                 ⭐ NOVO (lista de histórico)
│   ├── AITriggerButton.tsx                ⭐ NOVO (botão contextual)
│   ├── PodsPanel.tsx                      ✏️ MODIFICAR (adicionar botão AI)
│   ├── DeploymentsTab.tsx                 ✏️ MODIFICAR (adicionar botão AI)
│   ├── HPAPanel.tsx                       ✏️ MODIFICAR (adicionar botão AI)
│   └── NodesPanel.tsx                     ✏️ MODIFICAR (adicionar botão AI)
│
├── hooks/
│   ├── useAIDiagnostics.ts                ⭐ NOVO
│   └── useAIHistory.ts                    ⭐ NOVO
│
├── lib/api/
│   └── client.ts                          ✏️ MODIFICAR (adicionar métodos AI)
│
└── types/
    └── ai.ts                              ⭐ NOVO (tipos TypeScript)
```

### Configuração

```
configs/
└── ai_prompts.yaml                        ⭐ NOVO (templates de prompts)

build/
└── ai_diagnostics.db                      ⭐ NOVO (SQLite - auto-criado)
```

---

## 🔑 Componentes Críticos (Ordem de Implementação)

### Fase 1: Backend Base (2 dias)

1. **`internal/sanitizer/`** (CRÍTICO - fundação)
   - `sanitizer.go` - Interface e implementação
   - `patterns.go` - Regex patterns (IPs, tokens, emails, secrets)
   - `kubernetes.go` - Sanitização de Pods, Secrets, ConfigMaps

2. **`internal/collectors/`** (CRÍTICO - coleta de dados)
   - `models.go` - Estruturas de dados (DiagnosticContext, PodContext, etc)
   - `context_builder.go` - Orquestrador
   - `pod_collector.go` - Logs + Events + Describe + Related Resources
   - `deployment_collector.go` - Rollout + ReplicaSets + Pods
   - `hpa_collector.go` - Métricas + Scaling History
   - `node_collector.go` - Conditions + Taints + Resources

3. **`internal/storage/`** (CRÍTICO - persistência)
   - `sqlite.go` - Cliente SQLite
   - `migrations.go` - Schema DDL
   - `ai_history_store.go` - CRUD de histórico

### Fase 2: AI Integration (1.5 dias)

4. **`internal/ai/`** (CRÍTICO - IA)
   - `config.go` - Configuração de providers
   - `provider.go` - Interface base
   - `gemini_provider.go` - Implementação Gemini API
   - `ollama_provider.go` - Implementação Ollama
   - `prompts.go` - Templates de prompts (Pod, Deployment, HPA, Node)
   - `analyzer.go` - Orquestrador (Context → Prompt → AI → Result)
   - `models.go` - AnalysisResult, Suggestion, Priority

5. **`configs/ai_prompts.yaml`** (templates de prompts)

### Fase 3: HTTP Handler (0.5 dias)

6. **`internal/web/handlers/ai_diagnostics.go`** (CRÍTICO - API)
   - `POST /api/v1/ai/analyze` - Executar análise
   - `GET /api/v1/ai/history` - Listar histórico
   - `GET /api/v1/ai/history/:id` - Obter análise específica
   - `GET /api/v1/ai/status` - Status do provider

7. **`cmd/web.go`** (CRÍTICO - CLI)
   - Adicionar flags: `--ai-provider`, `--gemini-api-key`, `--ollama-url`, `--ollama-model`
   - Inicializar AI provider
   - Passar para `web.StartServer()`

### Fase 4: Frontend Base (2 dias)

8. **`src/types/ai.ts`** (tipos TypeScript)

9. **`src/lib/api/client.ts`** (CRÍTICO - API client)
   - `analyzeResource()`
   - `getAIHistory()`
   - `getAnalysisById()`
   - `getAIProviderStatus()`

10. **`src/hooks/useAIDiagnostics.ts`** (CRÍTICO - estado)

11. **`src/components/AIAnalysisCard.tsx`** (exibição de resultado)

12. **`src/components/AIHistoryPanel.tsx`** (lista de histórico)

13. **`src/components/AIDiagnosticsTab.tsx`** (CRÍTICO - aba principal)

14. **`src/components/AITriggerButton.tsx`** (botão reutilizável)

### Fase 5: UI Integration (1 dia)

15. **Modificar abas existentes**:
    - `PodsPanel.tsx` - Adicionar botão AI em pods problemáticos
    - `DeploymentsTab.tsx` - Adicionar botão AI em deployments
    - `HPAPanel.tsx` - Adicionar botão AI em HPAs maxed out
    - `NodesPanel.tsx` - Adicionar botão AI em nodes com problemas

---

## 🔐 Segurança - Sanitização de Dados

### Patterns Mascarados (Regex)

```go
// internal/sanitizer/patterns.go

IPv4:       \b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b         → X.X.X.X
Email:      [a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}          → user@REDACTED
JWT Token:  eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\..*       → eyJ***REDACTED***
Bearer:     Bearer\s+[a-zA-Z0-9_-]+                        → Bearer ***REDACTED***
UUID:       [0-9a-f]{8}-[0-9a-f]{4}-...                    → XXXXXXXX-XXXX-...

// Chaves sensíveis K8s (ENV vars, annotations)
password, passwd, pwd, secret, token, apikey, api_key, authorization, auth, certificate, cert, key
→ ***REDACTED***
```

### Campos Kubernetes Sanitizados

- **Pod.Spec.Containers[].Env** - Env vars com chaves sensíveis
- **Secret.Data** - Todos os valores (manter apenas chaves)
- **ConfigMap.Data** - Chaves sensíveis apenas
- **Annotations** - Chaves sensíveis

---

## 📊 API Endpoints

| Método | Path | Descrição | RBAC | Body/Query |
|--------|------|-----------|------|------------|
| POST | `/api/v1/ai/analyze` | Executar análise AI | Opcional | `{"resourceType": "Pod", "cluster": "...", "namespace": "...", "resourceName": "..."}` |
| GET | `/api/v1/ai/history` | Listar histórico | Público | `?cluster=...&namespace=...&resourceType=...&limit=50` |
| GET | `/api/v1/ai/history/:id` | Obter análise específica | Público | - |
| GET | `/api/v1/ai/status` | Status do provider AI | Público | - |

---

## 🗄️ SQLite Schema

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

CREATE INDEX idx_resource ON ai_analysis_history(cluster, namespace, resource_name);
CREATE INDEX idx_analyzed_at ON ai_analysis_history(analyzed_at DESC);
CREATE INDEX idx_provider ON ai_analysis_history(provider);
```

**Path**: `./build/ai_diagnostics.db` (auto-criado)

---

## 🎨 Prompt Templates (Exemplo - Pod)

```yaml
# configs/ai_prompts.yaml

prompts:
  pod:
    system: "You are a Kubernetes expert specializing in Pod troubleshooting."
    template: |
      Analyze the following Pod diagnostic context and provide:

      1. **Root Cause Analysis**: Identify the primary issue(s)
      2. **Impact Assessment**: Severity and scope
      3. **Recommended Actions**: Step-by-step remediation
      4. **kubectl Commands**: Specific commands (prefix with $)

      Context:
      {{CONTEXT}}

      Format: Clear sections, concise but thorough.
```

Templates similares para `deployment`, `hpa`, `node`.

---

## 🔄 Fluxo de Execução Completo

### Cenário: Usuário analisa Pod com CrashLoopBackOff

1. **Frontend (PodsPanel.tsx)**:
   - Usuário vê pod `my-app-xyz` com status `CrashLoopBackOff`
   - Clica em botão "Analisar com AI"

2. **Frontend (useAIDiagnostics hook)**:
   ```typescript
   POST /api/v1/ai/analyze
   Body: {
     "resourceType": "Pod",
     "cluster": "akspriv-prod",
     "namespace": "default",
     "resourceName": "my-app-xyz"
   }
   ```

3. **Backend (ai_diagnostics.go → Analyze())**:
   - Valida request
   - Cria `ContextRequest`

4. **Backend (ContextBuilder.BuildContext())**:
   - `PodCollector.Collect()`:
     - Obtém Pod manifest
     - Coleta logs (últimas 500 linhas)
     - Executa `kubectl describe pod`
     - Busca recursos relacionados (Deployment, ConfigMaps, Secrets)
   - Coleta eventos K8s (últimos 50)
   - Coleta alertas Prometheus

5. **Backend (Sanitizer)**:
   ```
   IPs: 10.0.1.5 → X.X.X.X
   Tokens: Bearer abc123... → Bearer ***REDACTED***
   Env vars: PASSWORD=secret → PASSWORD=***REDACTED***
   ```

6. **Backend (AI Analyzer)**:
   - `PromptBuilder.BuildPrompt()` usa template de Pod
   - Injeta contexto sanitizado JSON
   - Chama `provider.Analyze(prompt)`

7. **AI Provider (Gemini ou Ollama)**:
   - Gemini: `POST https://generativelanguage.googleapis.com/.../generateContent`
   - Ollama: `POST http://localhost:11434/api/generate`
   - Retorna análise textual

8. **Backend (Analyzer)**:
   - Cria `AnalysisResult`
   - Extrai sugestões (comandos kubectl com `$`)
   - Gera UUID

9. **Backend (HistoryStore.Save())**:
   - Obtém `user_email` via RBAC
   - `INSERT INTO ai_analysis_history`

10. **Backend → Frontend (JSON)**:
    ```json
    {
      "id": "550e8400-...",
      "resourceType": "Pod",
      "analysis": "**Root Cause**:\nImagePullBackOff...",
      "suggestions": [
        {
          "type": "investigate",
          "command": "kubectl logs my-app-xyz",
          "priority": "high"
        }
      ],
      "provider": "Gemini",
      "responseTime": 3.2
    }
    ```

11. **Frontend (AIAnalysisCard)**:
    - Renderiza análise
    - Exibe sugestões com badges de prioridade
    - Botão "Copy" para comandos kubectl

12. **Frontend (AIHistoryPanel)**:
    - Auto-refresh
    - Nova análise aparece no topo

---

## ⚙️ Configuração CLI

### Comandos

```bash
# Gemini API (requer API key)
new-k8s-hpa web --ai-provider gemini --gemini-api-key "AIza..."

# Gemini via env var (recomendado)
export GEMINI_API_KEY="AIza..."
new-k8s-hpa web --ai-provider gemini

# Ollama local (padrão: http://localhost:11434)
new-k8s-hpa web --ai-provider ollama

# Ollama custom
new-k8s-hpa web --ai-provider ollama \
  --ollama-url "http://192.168.1.100:11434" \
  --ollama-model "llama3.2"
```

### Flags Adicionadas

```go
// cmd/web.go
webCmd.Flags().StringVar(&aiProvider, "ai-provider", "gemini", "AI provider (gemini|ollama)")
webCmd.Flags().StringVar(&geminiAPIKey, "gemini-api-key", "", "Gemini API key (or GEMINI_API_KEY env)")
webCmd.Flags().StringVar(&ollamaBaseURL, "ollama-url", "http://localhost:11434", "Ollama base URL")
webCmd.Flags().StringVar(&ollamaModel, "ollama-model", "llama3.2", "Ollama model name")
```

---

## 📦 Dependências Go Adicionais

```bash
go get github.com/google/uuid      # UUID generation
go get github.com/mattn/go-sqlite3 # SQLite driver (já existe)
go mod tidy
```

---

## 🧪 Testes Recomendados

### Unit Tests (Go)

- `sanitizer_test.go` - Validar mascaramento de patterns
- `context_builder_test.go` - Validar coleta de contexto
- `gemini_provider_test.go` - Mock de API calls
- `ollama_provider_test.go` - Mock de API calls

### Integration Tests

- Análise de pod real com CrashLoopBackOff
- Análise de HPA maxed out
- Histórico SQLite (CRUD completo)

### Manual Tests

```bash
# 1. Iniciar com Gemini
export GEMINI_API_KEY="sua-key"
./build/new-k8s-hpa web --ai-provider gemini

# 2. Acessar http://localhost:8080
# 3. Navegar para aba Pods
# 4. Encontrar pod problemático
# 5. Clicar "Analisar com AI"
# 6. Verificar resultado em AI Diagnostics

# 7. Testar Ollama
./build/new-k8s-hpa web --ai-provider ollama
```

---

## 📝 Notas de Implementação

### ⚠️ Pontos de Atenção

1. **Timeout de Análise**: Context timeout de 120s (AI pode demorar)
2. **Rate Limiting**: Gemini tem limites de quota (adicionar middleware se necessário)
3. **Sanitização Completa**: NUNCA enviar IPs reais, tokens ou secrets para IA
4. **Ollama Performance**: Modelo local pode ser lento sem GPU
5. **SQLite Concurrency**: Usar WAL mode para melhor performance

### ✅ Validações Necessárias

- Cluster existe e está acessível
- Resource existe (Pod/Deployment/HPA/Node)
- Provider AI está disponível (`IsAvailable()`)
- API key válida (Gemini)

### 🔧 Otimizações Futuras (Pós-MVP)

- Cache de análises recentes (evitar re-análise)
- Stream de resposta AI (SSE para feedback em tempo real)
- Suporte a mais providers (OpenAI, Claude, etc)
- Análise batch (múltiplos recursos de uma vez)
- Export de análises (PDF/Markdown)

---

## 📅 Estimativa de Tempo

| Fase | Componente | Tempo Estimado |
|------|------------|----------------|
| 1 | Backend Base (Sanitizer, Collectors, Storage) | 2 dias |
| 2 | AI Integration (Providers, Analyzer, Prompts) | 1.5 dias |
| 3 | HTTP Handler + CLI | 0.5 dias |
| 4 | Frontend Base (Componentes, Hooks) | 2 dias |
| 5 | UI Integration (Modificar abas) | 1 dia |
| 6 | Testes + Ajustes | 1 dia |
| **TOTAL** | **MVP Completo** | **8 dias** |

---

## ✅ Critérios de Aceitação

- [ ] Usuário pode analisar Pods, Deployments, HPAs e Nodes via botão "Analisar com AI"
- [ ] Análise retorna causa raiz, impacto e sugestões de ações
- [ ] Sugestões incluem comandos kubectl copiáveis
- [ ] Dados sensíveis são mascarados (IPs, tokens, secrets)
- [ ] Histórico de análises é armazenado em SQLite
- [ ] Usuário pode alternar entre Gemini e Ollama via CLI flag
- [ ] Aba "AI Diagnostics" exibe análises completas + histórico
- [ ] Botões contextuais aparecem em pods/deployments problemáticos
- [ ] Provider status é exibido (disponível/indisponível)

---

**Fim do Plano de Implementação**
