# FinOps — Integração New Relic (EKS)

Checklist de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` + `FINOPS-DT-METRICS.md`.

**Contexto:** Clusters EKS não têm Dynatrace nem Prometheus, mas têm New Relic instalado.
O New Relic deve entrar como terceira fonte de métricas históricas no FinOps, completando
a cadeia: **DT (AKS primário) → NR (EKS) → Prometheus (fallback genérico)**.

**Fluxo completo após implementação:**
```
DT configurado E tem dados?   → enriquece workloads (fonte: dynatrace)
NR configurado E tem dados?   → enriquece workloads sem DT (fonte: newrelic)
Prometheus disponível?        → enriquece workloads restantes (fonte: prometheus)
Nenhum?                       → análise histórica omitida
```

**Evento K8s no New Relic:** `K8sContainerSample`
- `cpuUsedCores` × 1000 = millicores  
- `memoryWorkingSetBytes` = bytes  
- Dimensões: `namespaceName`, `deploymentName`, `daemonSetName`, `statefulSetName`, `clusterName`

**API:** NerdGraph (GraphQL) — `https://api.newrelic.com/graphql`  
**Auth:** header `Api-Key: <User API Key>`  
**Credenciais necessárias:** Account ID (número) + API Key (`NRAK-...`)

---

## Fase 1 — Pacote `internal/newrelic/`

**Arquivos a criar:**
- `internal/newrelic/client.go`
- `internal/newrelic/finops_metrics.go`

### `client.go`

```go
type Client struct {
    accountID  int
    apiKey     string
    httpClient *http.Client
    endpoint   string // "https://api.newrelic.com/graphql"
}

func NewClient(accountID int, apiKey string) *Client
func (c *Client) nrql(ctx context.Context, query string) ([]NRQLRow, error)
```

- POST para o endpoint NerdGraph com body GraphQL:
  ```graphql
  { actor { account(id: <accountID>) { nrql(query: "<NRQL>") { results } } } }
  ```
- Timeout: 45s (NR pode ser lento em queries longas)
- Sem cache próprio — o `Calculator` já controla se chama ou não

### `finops_metrics.go`

```go
// WorkloadMetrics — mesma struct do pacote dynatrace (replicada para evitar acoplamento)
type WorkloadMetrics struct {
    CPUAvgMillicores float64
    CPUP95Millicores float64
    MemAvgBytes      float64
    MemP95Bytes      float64
}

func (c *Client) GetAllWorkloadMetrics(ctx context.Context, clusterName string, windowDays int) (map[string]WorkloadMetrics, error)
```

**NRQL strategy — 4 queries paralelas:**

Cada query usa multi-FACET `namespaceName, deploymentName, daemonSetName, statefulSetName`.
Nos resultados, o workload é o primeiro campo não-vazio entre os três últimos.

```sql
-- CPU P95
SELECT percentile(cpuUsedCores * 1000, 95)
FROM K8sContainerSample
WHERE clusterName = '<cluster>' AND containerName != '' AND containerName != 'POD'
FACET namespaceName, deploymentName, daemonSetName, statefulSetName
SINCE <windowDays> days ago LIMIT MAX

-- CPU avg
SELECT average(cpuUsedCores * 1000)
FROM K8sContainerSample
WHERE clusterName = '<cluster>' AND containerName != '' AND containerName != 'POD'
FACET namespaceName, deploymentName, daemonSetName, statefulSetName
SINCE <windowDays> days ago LIMIT MAX

-- Mem P95
SELECT percentile(memoryWorkingSetBytes, 95)
FROM K8sContainerSample
WHERE clusterName = '<cluster>' AND containerName != '' AND containerName != 'POD'
FACET namespaceName, deploymentName, daemonSetName, statefulSetName
SINCE <windowDays> days ago LIMIT MAX

-- Mem avg
SELECT average(memoryWorkingSetBytes)
FROM K8sContainerSample
WHERE clusterName = '<cluster>' AND containerName != '' AND containerName != 'POD'
FACET namespaceName, deploymentName, daemonSetName, statefulSetName
SINCE <windowDays> days ago LIMIT MAX
```

As 4 queries são disparadas em paralelo via goroutines + `sync.WaitGroup`.  
Chave de retorno: `"namespace/workload"` — compatível com FinOps.

**Parsing do resultado NerdGraph:**
```json
{
  "data": {
    "actor": {
      "account": {
        "nrql": {
          "results": [
            {
              "namespaceName": "payments",
              "deploymentName": "payment-api",
              "daemonSetName": "",
              "statefulSetName": "",
              "percentile.cpuUsedCores * 1000": {"95": 312.4}
            }
          ]
        }
      }
    }
  }
}
```

Função auxiliar `pickWorkloadName(row)` — retorna o primeiro não-vazio entre `deploymentName`, `daemonSetName`, `statefulSetName`.

---

## Fase 2 — `internal/finops/newrelic_enricher.go`

**Mesmo padrão do `dynatrace_enricher.go`:**

```go
type NREnricher struct {
    client      *newrelic.Client
    clusterName string  // necessário para filtrar por cluster no NRQL
    windowDays  int
}

func NewNREnricher(client *newrelic.Client, clusterName string, windowDays int) *NREnricher

// EnrichWorkloads preenche workloads sem dados DT com métricas NR.
// Retorna set de chaves "namespace/workload" enriquecidas.
func (e *NREnricher) EnrichWorkloads(ctx context.Context, workloads []FinOpsWorkload) map[string]bool
```

Lógica idêntica ao DTEnricher: converte bytes→Mi, aplica `SafetyMargin` no P95,
calcula `WasteBRL`, chama `verdictFromPrometheus`, seta `MetricsSource = "newrelic"`.

**Atualizar `internal/finops/calculator.go` — `BuildReport`:**
```go
func (c *Calculator) BuildReport(
    ctx context.Context,
    cluster string,
    k8sClient kubernetes.Interface,
    pools []storage.NodePoolEntry,
    namespaces []string,
    dtEnricher *DTEnricher,    // existente
    nrEnricher *NREnricher,    // NOVO — entre DT e Prometheus
    enricher   *PrometheusEnricher,
) (*FinOpsReport, error)
```

Cadeia no `BuildReport`:
```go
var dtEnriched map[string]bool
if dtEnricher != nil {
    dtEnriched = dtEnricher.EnrichWorkloads(ctx, workloads)
}

var nrEnriched map[string]bool
if nrEnricher != nil {
    // Só enriquece workloads que DT não cobriu
    nrEnriched = nrEnricher.EnrichWorkloadsPartial(ctx, workloads, dtEnriched)
}

allEnriched := mergeMaps(dtEnriched, nrEnriched)

if enricher != nil {
    enricher.SetPodMapping(podToWorkload)
    enricher.EnrichWorkloadsPartial(ctx, workloads, allEnriched)
}
```

Adicionar método `EnrichWorkloadsPartial` no `NREnricher` (igual ao de Prometheus):
pula workloads já presentes em `alreadyEnriched`, seta `MetricsSource = "newrelic"` nos enriquecidos.

---

## Fase 3 — Credenciais no `UserTokensStore`

**Arquivo:** `internal/storage/user_tokens_store.go`

Adicionar campos na struct `UserTokens`:
```go
NewRelicAccountID int    `json:"new_relic_account_id,omitempty"`
NewRelicAPIKey    string `json:"new_relic_api_key,omitempty"`
```

Adicionar método:
```go
func (s *UserTokensStore) GetNewRelicConfig() (accountID int, apiKey string, ok bool)
```
Busca o primeiro usuário com `NewRelicAccountID > 0` e `NewRelicAPIKey != ""`.

**Atualizar interface `dtTokenReader` em `handlers/finops.go`:**  
Renomear para `observabilityTokenReader` (ou criar interface separada `nrTokenReader`).  
Adicionar método: `GetNewRelicConfig() (accountID int, apiKey string, ok bool)`

---

## Fase 4 — Wiring no Handler

**Arquivo:** `internal/web/handlers/finops.go`

Após criar o `dtEnricher`, criar o `nrEnricher`:
```go
var nrEnricher *finops.NREnricher
if h.dtTokenStore != nil {
    if accountID, apiKey, ok := h.dtTokenStore.GetNewRelicConfig(); ok {
        nrClient := newrelic.NewClient(accountID, apiKey)
        nrEnricher = finops.NewNREnricher(nrClient, cluster, windowDays)
        log.Info().Str("cluster", cluster).Int("account_id", accountID).
            Msg("FinOps: NR enricher ativado")
    }
}
```

Passar `nrEnricher` para `calc.BuildReport(...)`.

Atualizar log de conclusão:
```go
Bool("newrelic", nrEnricher != nil).
```

---

## Fase 5 — Frontend

### 5a. Badge NR em `FinOpsTab.tsx`

Na aba **Oportunidades** (coluna Desperdício) e **HPA Histórico** (P95, avg replicas):

```tsx
{w.metrics_source === "newrelic" && (
  <span className="text-[9px] font-medium px-1 py-0.5 rounded bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">NR</span>
)}
```

Adicionar após os badges DT e Prom existentes. Cor: âmbar (distinct de DT=azul e Prom=laranja).

### 5b. Configuração de credenciais NR na UI

**Arquivo:** `internal/web/frontend/src/components/profile/AISettingsModal.tsx` (ou equivalente)

Adicionar seção "New Relic" com:
- Campo `Account ID` (número)
- Campo `API Key` (senha, tipo `NRAK-...`)
- Botão "Testar" → chama `GET /api/v1/newrelic/test?cluster=<cluster>`

### 5c. Endpoint de teste NR (opcional mas recomendado)

`GET /api/v1/newrelic/test` — faz uma NRQL simples `SELECT count(*) FROM K8sContainerSample SINCE 1 hour ago` para validar credenciais.

---

## Ordem de implementação sugerida

1. [ ] **Fase 1** — Pacote `internal/newrelic/` (client + NRQL)
2. [ ] **Fase 2** — `newrelic_enricher.go` + atualizar `BuildReport`
3. [ ] **Fase 3** — Credenciais no `UserTokensStore`
4. [ ] **Fase 4** — Wiring no handler
5. [ ] **Fase 5a** — Badge NR no frontend
6. [ ] **Fase 5b/5c** — UI de configuração + endpoint de teste (pode ser posterior)

---

## Arquivos a criar/modificar

```
internal/newrelic/client.go                       ← CRIAR (Fase 1)
internal/newrelic/finops_metrics.go               ← CRIAR (Fase 1)
internal/finops/newrelic_enricher.go              ← CRIAR (Fase 2)
internal/finops/calculator.go                     ← MODIFICAR (Fase 2)
internal/storage/user_tokens_store.go             ← MODIFICAR (Fase 3)
internal/web/handlers/finops.go                   ← MODIFICAR (Fase 4)
internal/web/frontend/src/components/FinOpsTab.tsx ← MODIFICAR (Fase 5a)
internal/web/frontend/src/components/profile/AISettingsModal.tsx ← MODIFICAR (Fase 5b)
```

---

## Dependências externas

Nenhuma biblioteca Go nova necessária — usar apenas `net/http` + `encoding/json` para o cliente NerdGraph.  
A API NerdGraph é REST sobre GraphQL com JSON puro — sem SDK obrigatório.

---

## Observações

- **clusterName no NR**: o nome do cluster EKS no NR é o mesmo do kubeconfig context ou pode ter sufixo. Se necessário, o usuário pode configurar o "cluster name NR" separadamente do context K8s.
- **LIMIT MAX**: NR retorna até 2000 linhas por default; `LIMIT MAX` eleva para 5000 (máximo sem paginação). Para clusters grandes com muitos workloads, verificar se basta.
- **Sem cache no client**: o `Calculator` é instanciado por request — o cache de Azure Pricing usa SQLite, mas métricas NR não precisam de cache (são buscadas a cada análise FinOps, igual ao Prometheus).
- **Região da API**: usar `https://api.eu.newrelic.com/graphql` para contas EU. Detectar pela API key prefix (`NRAK-EU-...`) ou deixar configurável.
- **HPA metrics**: NR não tem equivalente direto de `kube_horizontalpodautoscaler_status_current_replicas`. HPA histórico permanece exclusivo do Prometheus (para clusters EKS sem Prometheus, os campos HPAAvgReplicas/HPAScaleEvents ficarão zerados).
