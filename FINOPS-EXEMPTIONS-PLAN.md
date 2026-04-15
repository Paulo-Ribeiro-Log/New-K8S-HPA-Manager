# FinOps — Sistema de Isenções por Workload

Plano de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` + `FINOPS-DT-METRICS.md`.

**Contexto:** A aba FinOps analisa workloads K8s e sugere ações como "remover HPA" quando um deployment nunca
escala além de 1 réplica (max=1). Em ambientes HLG, muitas aplicações são intencionalmente configuradas com
HPA 1/1 para economizar recursos — é uma decisão de projeto, não desperdício. Isso gera falsos positivos.

**Conceito central — isenção condicional e automática:**
```
HPACurrent ≤ max_replicas_threshold  →  isento ativo   → sem WasteBRL, Verdict="ok"
HPACurrent > max_replicas_threshold  →  isento ignorado → analisado normalmente
Sem isenção cadastrada               →  comportamento atual (inalterado)
```
A isenção é computada on-the-fly a cada análise. Se o HPA ultrapassar o threshold (ex: alguém aumentou para
3 réplicas), a isenção é ignorada automaticamente sem intervenção manual.

---

## Fase 1 — Storage

**Arquivo:** `internal/storage/finops_exemptions_store.go` ← CRIAR

Schema da tabela SQLite:
```sql
CREATE TABLE IF NOT EXISTS finops_exemptions (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster                TEXT    NOT NULL,
    namespace              TEXT    NOT NULL,
    workload               TEXT    NOT NULL,
    max_replicas_threshold INTEGER NOT NULL DEFAULT 1,
    reason                 TEXT    NOT NULL DEFAULT '',
    created_by             TEXT,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cluster, namespace, workload)
);
CREATE INDEX IF NOT EXISTS idx_fe_cluster ON finops_exemptions(cluster);
CREATE INDEX IF NOT EXISTS idx_fe_ns      ON finops_exemptions(cluster, namespace);
```

Caminho do banco: `~/.k8s-hpa-manager/finops_exemptions.db`

Métodos necessários:
- `Upsert(e FinOpsExemption) error` — cria ou atualiza via `ON CONFLICT DO UPDATE SET`
- `GetAllAsMap(cluster string) (map[string]*FinOpsExemption, error)` — chave `"namespace/workload"`, usado no apply O(1)
- `ListByCluster(cluster string) ([]FinOpsExemption, error)` — para o endpoint GET
- `Delete(cluster, namespace, workload string) error`

- [ ] Criar `internal/storage/finops_exemptions_store.go`
- [ ] Inicializar store em `internal/web/server.go` (bloco dos stores existentes)

---

## Fase 2 — Modelos

**Arquivo:** `internal/finops/models.go` ← MODIFICAR

Adicionar struct:
```go
// FinOpsExemption representa a configuração de isenção de um workload.
// Persiste no banco; exempt_active é calculado no momento da análise.
type FinOpsExemption struct {
    ID                   int64     `json:"id"`
    Cluster              string    `json:"cluster"`
    Namespace            string    `json:"namespace"`
    Workload             string    `json:"workload"`
    MaxReplicasThreshold int       `json:"max_replicas_threshold"`
    Reason               string    `json:"reason"`
    CreatedBy            string    `json:"created_by,omitempty"`
    CreatedAt            time.Time `json:"created_at"`
    UpdatedAt            time.Time `json:"updated_at"`
}
```

Adicionar em `FinOpsWorkload` (campos `omitempty` — relatórios existentes sem isenção não são afetados):
```go
IsExempt     bool   `json:"is_exempt,omitempty"`     // workload tem isenção cadastrada
ExemptReason string `json:"exempt_reason,omitempty"` // razão da isenção
ExemptActive bool   `json:"exempt_active,omitempty"` // true = dentro do threshold (análise suprimida)
                                                      // false = threshold ultrapassado (ignorada)
```

- [ ] Adicionar struct `FinOpsExemption` em `models.go`
- [ ] Adicionar campos `IsExempt`, `ExemptReason`, `ExemptActive` em `FinOpsWorkload`

---

## Fase 3 — Lógica de Aplicação

**Arquivo:** `internal/finops/exemptions.go` ← CRIAR

Função pura (sem I/O), chamada pelo handler após `BuildReport`:
```go
// ApplyExemptions aplica isenções ao slice de workloads in-place.
// exemptions: mapa "namespace/workload" → FinOpsExemption (retornado pelo store.GetAllAsMap).
// Workloads sem isenção não são modificados.
func ApplyExemptions(workloads []FinOpsWorkload, exemptions map[string]*storage.FinOpsExemption) {
    for i := range workloads {
        wl := &workloads[i]
        ex, ok := exemptions[wl.Namespace+"/"+wl.Name]
        if !ok {
            continue
        }
        wl.IsExempt = true
        wl.ExemptReason = ex.Reason

        // Usa HPACurrent como proxy de réplicas ativas; fallback para Pods
        current := int(wl.CurrentReplicas)
        if current == 0 {
            current = wl.Pods
        }
        if current <= ex.MaxReplicasThreshold {
            // Isenção ativa: suprimir análise de desperdício
            wl.ExemptActive = true
            wl.WasteBRL = 0
            wl.Verdict = "ok"
        }
        // Se current > threshold: ExemptActive=false, workload analisado normalmente
    }
}
```

Após `ApplyExemptions`, o handler deve recalcular o `FinOpsSummary` para que contadores como
`HPARemovableCount` e `PotentialSavingsBRL` não incluam workloads com isenção ativa.
Extrair `BuildSummary` do `calculator.go` como função exportada para reutilizar.

- [ ] Criar `internal/finops/exemptions.go` com `ApplyExemptions`
- [ ] Exportar `BuildSummary` em `calculator.go`
- [ ] Integrar no handler: `ApplyExemptions` + recalcular summary após `BuildReport`

---

## Fase 4 — Endpoints HTTP

**Arquivo:** `internal/web/handlers/finops.go` ← MODIFICAR

Adicionar `exemptionsStore *storage.FinOpsExemptionsStore` ao `FinOpsHandler`.

| Método | Path | Descrição |
|--------|------|-----------|
| `GET`  | `/api/v1/finops/exemptions?cluster=X` | Lista todas as isenções do cluster |
| `POST` | `/api/v1/finops/exemptions` | Cria ou atualiza isenção (upsert) |
| `DELETE` | `/api/v1/finops/exemptions?cluster=X&namespace=Y&workload=Z` | Remove isenção |

Body do `POST`:
```json
{
  "cluster": "meu-cluster-hlg",
  "namespace": "payments",
  "workload": "legacydata-api",
  "max_replicas_threshold": 1,
  "reason": "Ambiente HLG"
}
```

Validações: `cluster`, `namespace`, `workload` obrigatórios; `max_replicas_threshold >= 1`; `reason` máx 200 chars.
`created_by` preenchido automaticamente via contexto Gin (email do usuário autenticado).

- [ ] Adicionar `exemptionsStore` ao `FinOpsHandler`
- [ ] Implementar `ListExemptions`, `UpsertExemption`, `DeleteExemption`
- [ ] Registrar rotas em `server.go`
- [ ] Injetar store no handler em `NewServer`

---

## Fase 5 — Frontend

**Arquivo:** `internal/web/frontend/src/components/FinOpsTab.tsx` ← MODIFICAR

### 5a. Tipos TypeScript

```typescript
interface FinOpsExemption {
  id: number;
  cluster: string;
  namespace: string;
  workload: string;
  max_replicas_threshold: number;
  reason: string;
  created_at: string;
}

// Adicionar em FinOpsWorkload existente:
// is_exempt?: boolean;
// exempt_reason?: string;
// exempt_active?: boolean;
```

### 5b. Query de isenções (React Query)

```typescript
const { data: exemptions = [], refetch: refetchExemptions } = useQuery<FinOpsExemption[]>({
  queryKey: ["finops-exemptions", cluster],
  queryFn: () => apiClient.get(`/api/v1/finops/exemptions?cluster=${cluster}`),
  enabled: !!cluster,
  staleTime: 60_000,
});
```

### 5c. Badge visual por workload

Nas tabelas de Workloads e Oportunidades, ao lado do `VerdictBadge`:

| Estado | Visual |
|--------|--------|
| `is_exempt=true, exempt_active=true` | Badge cinza + ícone escudo + tooltip com razão — "Isento: Ambiente HLG" |
| `is_exempt=true, exempt_active=false` | Badge âmbar — "Isenção ignorada (threshold ultrapassado)" |

### 5d. `ExemptionDialog` (modal por workload)

Componente `ExemptionDialog` aberto por botão de contexto (ícone de escudo ou `...`) em cada linha:

- Input numérico "Threshold de réplicas" — default: valor atual de `hpa_max`
- Select de razões predefinidas + campo livre:
  - "Ambiente HLG"
  - "Controle de custos"
  - "Definição de projeto"
  - "Política de capacidade"
  - "Outro" (campo de texto livre)
- Se já existe isenção: mostra dados atuais + botão "Remover isenção" → DELETE
- Após salvar/remover: `refetchExemptions()` + `queryClient.invalidateQueries(["finops-report", ...])`

### 5e. Filtro "Ocultar isentos ativos"

Checkbox na barra de filtros das abas Workloads e Oportunidades:
```typescript
const filtered = hideExempt
  ? workloads.filter(w => !w.is_exempt || !w.exempt_active)
  : workloads;
```
Ligado por padrão — evita falsos positivos na lista principal.

- [ ] Adicionar tipos `FinOpsExemption` e campos em `FinOpsWorkload`
- [ ] Adicionar query de isenções
- [ ] Criar `ExemptionDialog`
- [ ] Adicionar badge de isenção nas tabelas (Workloads + Oportunidades)
- [ ] Adicionar botão de contexto por linha
- [ ] Adicionar filtro "Ocultar isentos ativos"
- [ ] Passar `cluster` para `WorkloadsTab` (ajuste de props)

---

## Fase 6 — Polimento (pode ser posterior)

- [ ] Contagem de isenções ativas no badge da aba "Workloads" (ex: "Workloads (3 isentos)")
- [ ] Excluir isentos ativos do `PotentialSavingsBRL` no `FinOpsSummary`
- [ ] Seção "Isenções ativas" no relatório PDF (lista compacta ao final)

---

## Decisões de Design

**Por que aplicar isenções no handler e não no `Calculator`?**
O `Calculator` é puro — sem I/O de storage — e testável de forma isolada. Injetar storage nele quebraria
essa pureza e o tornaria mais difícil de testar. Isenção é regra de negócio da apresentação.

**Por que banco separado (`finops_exemptions.db`)?**
Padrão dos outros stores do projeto (nodepool_registry, finops_timeline). Isolamento de falhas e
backup trivial por arquivo.

**Por que `Verdict="ok"` para isentos ativos e não um novo enum?**
Menos disruptivo — não quebra o frontend nem os contadores de summary existentes. A distinção visual
fica nos campos `is_exempt`/`exempt_active`.

**Por que threshold computado on-the-fly e não campo `active` editável?**
Evita estado desincronizado: se o HPA escalar além do threshold, a isenção desaparece automaticamente.
Apenas `max_replicas_threshold` é persistido; `exempt_active` é sempre derivado na análise.

---

## Arquivos a criar/modificar

```
internal/storage/finops_exemptions_store.go     ← CRIAR (Fase 1)
internal/finops/models.go                       ← MODIFICAR (Fase 2)
internal/finops/exemptions.go                   ← CRIAR (Fase 3)
internal/finops/calculator.go                   ← MODIFICAR (Fase 3 — exportar BuildSummary)
internal/web/handlers/finops.go                 ← MODIFICAR (Fase 4)
internal/web/server.go                          ← MODIFICAR (Fase 1 + 4)
internal/web/frontend/src/components/FinOpsTab.tsx ← MODIFICAR (Fase 5)
```
