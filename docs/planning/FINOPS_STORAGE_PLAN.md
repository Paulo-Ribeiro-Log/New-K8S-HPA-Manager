# FinOps — Plano de Expansão: Armazenamento (PVCs, Discos OS, Azure Files/Blob)

**Objetivo:** Incluir custo de armazenamento (PVCs, discos OS dos nodes, Azure Files e Blob) na análise FinOps, que hoje considera apenas custo de compute (VMs dos node pools).

**Branch:** `fix-service-now` (ou nova branch a partir de `main`)

---

## Estado Atual vs. Estado Desejado

| Categoria | Hoje | Após refatoração |
|-----------|------|-----------------|
| VMs (node pools) | ✅ Custo compute por pool, por workload | ✅ Mantido |
| Discos OS dos nodes | ❌ Ignorado | ✅ Custo por pool (nós × disco OS) |
| PVCs (discos persistentes) | ❌ Ignorado | ✅ Custo por PVC + por namespace + por workload |
| Azure Files / Azure Blob (CSI) | ❌ Ignorado | ✅ Custo por PVC (via storage class) |
| PVCs orfãos (não montados) | ❌ Ignorado | ✅ Detectados e destacados como desperdício |
| Total real do cluster | 🔶 Só compute | ✅ Compute + storage |

---

## Contexto Técnico

### Fontes de dados disponíveis (sem Azure Management API)

| Fonte | O que fornece | Como usar |
|-------|--------------|-----------|
| K8s `PersistentVolumeClaims` | namespace, name, storageClass, requests.storage, phase, volumeName | Enumerate via `CoreV1().PersistentVolumeClaims("").List()` |
| K8s `PersistentVolumes` | capacity, reclaimPolicy, storageClassName, volumeHandle (resource ID Azure) | `CoreV1().PersistentVolumes().List()` — ligado ao PVC pelo `volumeName` |
| K8s `StorageClasses` | provisioner, parameters.skuName | `StorageV1().StorageClasses().List()` — determina tipo Azure pelo provisioner + parâmetros |
| K8s `Pods` | spec.volumes[].persistentVolumeClaim.claimName + ownerReferences | Correlacionar PVC → Pod → Deployment |
| K8s `Nodes` | labels: `kubernetes.azure.com/os-disk-size-gb`, `beta.kubernetes.io/os` | Tamanho real do disco OS por node |
| Azure Pricing API | Preço por tier de disco (P10, E6, S10) e por GB/mês (Azure Files, Blob) | Mesmo mecanismo que `azure_pricing.go` |

> **Nota**: Azure Blob Storage avulso (Storage Accounts sem PVC) exige Azure Management API e não é viável sem autenticação. Foco apenas no que o K8s enxerga via PVCs/PVs.

---

## Modelo de Precificação de Armazenamento Azure

### 1. Managed Disks (Premium SSD, Standard SSD, Standard HDD)

Precificação **por disco/mês** (não por GB). O tamanho solicitado é arredondado para o próximo **tier** disponível:

| Tier | Premium SSD (P) | Standard SSD (E) | Standard HDD (S) | Preço aprox. USD/mês |
|------|----------------|-----------------|-----------------|---------------------|
| T4   | P4 – 32 GB     | E4 – 32 GB      | S4 – 32 GB      | P4≈$5.28 / E4≈$1.92 / S4≈$1.32 |
| T6   | P6 – 64 GB     | E6 – 64 GB      | S6 – 64 GB      | P6≈$10.22 / E6≈$3.84 |
| T10  | P10 – 128 GB   | E10 – 128 GB    | S10 – 128 GB    | P10≈$19.71 / E10≈$7.68 |
| T15  | P15 – 256 GB   | E15 – 256 GB    | S15 – 256 GB    | P15≈$38.09 |
| T20  | P20 – 512 GB   | E20 – 512 GB    | S20 – 512 GB    | P20≈$73.22 |
| T30  | P30 – 1024 GB  | E30 – 1024 GB   | S30 – 1024 GB   | P30≈$135.17 |

API filter: `serviceName eq 'Storage' and productName eq 'Premium SSD Managed Disks' and skuName eq 'P10 LRS'`

### 2. Azure Files Standard / Premium

Precificação **por GB/mês**:
- Azure Files Standard: ≈ $0.060/GB/mês (LRS, Brasil Sul)
- Azure Files Premium: ≈ $0.130/GB/mês (LRS, Brasil Sul)

### 3. Azure Blob Storage (via CSI blob driver)

Precificação **por GB/mês**:
- Hot tier: ≈ $0.023/GB/mês (LRS, Brasil Sul)

### Mapeamento StorageClass → Tipo Azure

```
managed-csi            → Standard SSD (E-series)
managed-csi-premium    → Premium SSD (P-series)
managed                → Standard HDD (S-series)
default                → Standard SSD (E-series)   ← AKS padrão
azurefile              → Azure Files Standard
azurefile-csi          → Azure Files Standard
azurefile-csi-premium  → Azure Files Premium
blob / blob-csi        → Azure Blob Hot
```

> Se a StorageClass tiver `parameters.skuName` explícito (ex: `Premium_LRS`, `Standard_LRS`), usar esse valor — é mais confiável que o nome da SC.

---

## Arquitetura da Refatoração

### Fluxo de dados expandido

```
K8s API (PVCs, PVs, StorageClasses, Pods, Nodes)
        │
        ▼
storage_calculator.go
  ├── listar PVCs + PVs → determinar tipo + GB
  ├── resolver StorageClass → Azure type + SKU
  ├── correlacionar PVC → Pod → Workload
  └── detectar orfãos (PVCs sem pod montando)
        │
        ▼
azure_disk_pricing.go
  ├── GetDiskPrice(type, tier, region) → USD/mês  [managed disk]
  ├── GetFilesPricePerGB(type, region) → USD/GB/mês  [files/blob]
  └── cache SQLite (mesma DB de VM, nova tabela disk_pricing_cache)
        │
        ▼
calculator.go (integrado)
  ├── Custo compute (node pools) — atual
  ├── Custo disco OS por node pool
  └── Custo storage (PVCs agregados por workload/namespace)
        │
        ▼
FinOpsReport expandido:
  ├── NodePools  → + OSDiskCostUSD/BRL, TotalCostUSD/BRL
  ├── Workloads  → + StorageCostBRL, PVCCount, PVCCapacityGB
  ├── PVCs       → nova lista PVCCostItem[]
  ├── Storage    → nova StorageSummary (totais, por SC, por namespace)
  └── Summary    → + StorageMonthlyCostBRL, OSDiskCostBRL, TotalWithStorageBRL, OrphanedCostBRL
```

---

## Novos Tipos em `models.go`

```go
// PVCCostItem representa um PVC com seu custo mensal calculado
type PVCCostItem struct {
    Namespace      string   `json:"namespace"`
    Name           string   `json:"name"`
    BoundPV        string   `json:"bound_pv"`        // nome do PV vinculado
    StorageClass   string   `json:"storage_class"`
    CapacityGB     float64  `json:"capacity_gb"`
    AzureDiskType  string   `json:"azure_disk_type"`  // "Premium SSD", "Azure Files Standard", etc.
    AzureDiskTier  string   `json:"azure_disk_tier"`  // "P10", "E6" — vazio para Files/Blob
    PricePerMonth  float64  `json:"price_usd_month"`  // preço base (por disco ou por GB)
    MonthlyCostUSD float64  `json:"monthly_cost_usd"`
    MonthlyCostBRL float64  `json:"monthly_cost_brl"`
    Phase          string   `json:"phase"`            // Bound | Pending | Lost
    ReclaimPolicy  string   `json:"reclaim_policy"`   // Delete | Retain
    WorkloadRef    string   `json:"workload_ref"`      // "namespace/deployment" se detectável
    IsOrphaned     bool     `json:"is_orphaned"`       // true se nenhum pod está montando
    PriceSource    string   `json:"price_source"`      // "api" | "fallback"
}

// StorageClassBreakdown agrupa custo por tipo de storage class
type StorageClassBreakdown struct {
    StorageClass   string  `json:"storage_class"`
    AzureType      string  `json:"azure_type"`
    PVCCount       int     `json:"pvc_count"`
    TotalGB        float64 `json:"total_gb"`
    MonthlyCostBRL float64 `json:"monthly_cost_brl"`
}

// StorageSummary consolida os números de armazenamento do relatório
type StorageSummary struct {
    TotalMonthlyCostUSD    float64                  `json:"total_monthly_cost_usd"`
    TotalMonthlyCostBRL    float64                  `json:"total_monthly_cost_brl"`
    OSDiskCostUSD          float64                  `json:"os_disk_cost_usd"`
    OSDiskCostBRL          float64                  `json:"os_disk_cost_brl"`
    PVCCount               int                      `json:"pvc_count"`
    BoundPVCCount          int                      `json:"bound_pvc_count"`
    OrphanedPVCCount       int                      `json:"orphaned_pvc_count"`
    OrphanedCostBRL        float64                  `json:"orphaned_cost_brl"`
    TotalCapacityGB        float64                  `json:"total_capacity_gb"`
    ByStorageClass         []StorageClassBreakdown  `json:"by_storage_class"`
    ByNamespace            map[string]float64       `json:"by_namespace"`
}
```

### Extensões em tipos existentes

```go
// FinOpsPool — adicionar custo de disco OS
type FinOpsPool struct {
    // ...campos existentes...
    OSDiskSKU      string  `json:"os_disk_sku"`       // "Premium SSD"
    OSDiskTier     string  `json:"os_disk_tier"`      // "P10"
    OSDiskGB       int     `json:"os_disk_gb"`        // 128
    OSDiskCostUSD  float64 `json:"os_disk_cost_usd"`  // por pool (× node_count)
    OSDiskCostBRL  float64 `json:"os_disk_cost_brl"`
    TotalCostUSD   float64 `json:"total_cost_usd"`    // compute + os_disk
    TotalCostBRL   float64 `json:"total_cost_brl"`
}

// FinOpsWorkload — adicionar custo de storage
type FinOpsWorkload struct {
    // ...campos existentes...
    StorageCostUSD float64 `json:"storage_cost_usd,omitempty"`
    StorageCostBRL float64 `json:"storage_cost_brl,omitempty"`
    PVCCount       int     `json:"pvc_count,omitempty"`
    PVCCapacityGB  float64 `json:"pvc_capacity_gb,omitempty"`
}

// FinOpsReport — adicionar lista de PVCs e resumo storage
type FinOpsReport struct {
    // ...campos existentes...
    PVCs    []PVCCostItem  `json:"pvcs"`
    Storage StorageSummary `json:"storage"`
}

// FinOpsSummary — adicionar totais de storage
type FinOpsSummary struct {
    // ...campos existentes...
    StorageMonthlyCostBRL  float64 `json:"storage_monthly_cost_brl"`
    StorageMonthlyCostUSD  float64 `json:"storage_monthly_cost_usd"`
    OSDiskCostBRL          float64 `json:"os_disk_cost_brl"`
    OrphanedStorageCostBRL float64 `json:"orphaned_storage_cost_brl"`
    TotalWithStorageBRL    float64 `json:"total_with_storage_brl"`
}
```

---

## Novos Arquivos Go

### `internal/finops/azure_disk_pricing.go`

Responsável por buscar e cachear preços de storage na Azure Pricing API.

```
Funções:
  GetDiskPrice(diskType, tier, region) (float64, string, error)
    → ex: GetDiskPrice("Premium SSD", "P10", "brazilsouth") → ($19.71, "api", nil)
    → cacheia na tabela "disk_pricing_cache" do mesmo SQLite de VMs

  GetFilesPricePerGB(filesType, region) (float64, string, error)
    → ex: GetFilesPricePerGB("Azure Files Standard", "brazilsouth") → ($0.060, "api", nil)

  MapStorageClassToAzureType(scName, provisioner, skuName string) (azureType, method string)
    → azureType: "Premium SSD" | "Standard SSD" | "Standard HDD" | "Azure Files Standard" |
                 "Azure Files Premium" | "Azure Blob Hot"
    → method:    "sku_param" | "provisioner" | "name_match" | "default"

  ResolveMangedDiskTier(azureType, requestedGB float64) string
    → arredonda GB para o próximo tier: 100GB Premium SSD → "P10" (128GB)

Tabela SQLite nova (mesma DB finops_pricing_cache.db):
  CREATE TABLE disk_pricing_cache (
      disk_type  TEXT NOT NULL,   -- "Premium SSD"
      tier       TEXT NOT NULL,   -- "P10" ou "per_gb"
      region     TEXT NOT NULL,
      price_usd  REAL NOT NULL,
      fetched_at DATETIME NOT NULL,
      PRIMARY KEY (disk_type, tier, region)
  );

Fallback (hardcoded, brazilsouth):
  Premium SSD:   P4=$5.28, P6=$10.22, P10=$19.71, P15=$38.09, P20=$73.22, P30=$135.17
  Standard SSD:  E4=$1.92, E6=$3.84,  E10=$7.68,  E15=$14.84, E20=$28.56, E30=$52.49
  Standard HDD:  S4=$1.32, S6=$2.64,  S10=$5.28,  S15=$10.21, S20=$19.71, S30=$36.62
  Azure Files Standard: $0.060/GB/mês
  Azure Files Premium:  $0.130/GB/mês
  Azure Blob Hot:       $0.023/GB/mês
```

### `internal/finops/storage_calculator.go`

Responsável por coletar PVCs do cluster e calcular custos.

```
Funções:
  StorageCalculator.Calculate(ctx, cluster, client, rate) → ([]PVCCostItem, StorageSummary, error)
    → passo 1: listar todos PVCs (todos namespaces)
    → passo 2: listar PVs correspondentes (para obter capacidade real se PVC não tem)
    → passo 3: listar StorageClasses → mapa name → provisioner + skuName
    → passo 4: listar Pods Running → mapa PVC → workloadRef
    → passo 5: para cada PVC: resolver tipo Azure + tier + preço + workloadRef + orphan
    → passo 6: agregar StorageSummary (por SC, por namespace, orfãos)

  listStorageClasses(ctx, client) → map[string]scInfo
    scInfo: { provisioner, skuName, reclaimPolicy }

  correlateToWorkloads(ctx, client, namespace) → map[pvcName]workloadRef
    → lista Pods Running no namespace
    → para cada pod: spec.volumes[].persistentVolumeClaim.claimName
    → resolve pod → owner (ReplicaSet/StatefulSet/DaemonSet) → owner (Deployment)

  OSDiskForNodePool(ctx, client, poolName) (sizeGB int, label string)
    → lista nodes com label node.kubernetes.io/instance-type do pool
    → tenta label kubernetes.azure.com/os-disk-size-gb (valor real)
    → fallback: 128GB (padrão AKS para a maioria dos SKUs)
    → retorna sizeGB e método usado ("label" | "default")
```

---

## Modificações em Arquivos Existentes

### `internal/finops/calculator.go`

Adicionar ao método `BuildReport`:

```go
// Após buildSummary(), antes de return:

// 5. Calcular custo de armazenamento (PVCs + discos OS)
storageCalc := NewStorageCalculator(c.diskPricer, c.exchange)
pvcs, storageSummary, err := storageCalc.Calculate(ctx, cluster, client, rate)
if err != nil {
    log.Warn().Err(err).Msg("finops: erro ao calcular storage (não fatal)")
    // continua sem storage — não bloqueia o relatório
}

// 5a. Custo disco OS por node pool
for i := range finOpsPools {
    sizeGB, _ := storageCalc.OSDiskForNodePool(ctx, client, finOpsPools[i].Name)
    tier := resolveDiskTier("Premium SSD", float64(sizeGB)) // AKS padrão = Premium SSD
    priceUSD, _, _ := c.diskPricer.GetDiskPrice("Premium SSD", tier, defaultPricingRegion)
    finOpsPools[i].OSDiskSKU = "Premium SSD"
    finOpsPools[i].OSDiskTier = tier
    finOpsPools[i].OSDiskGB = sizeGB
    finOpsPools[i].OSDiskCostUSD = priceUSD * float64(finOpsPools[i].NodeCount)
    finOpsPools[i].OSDiskCostBRL = finOpsPools[i].OSDiskCostUSD * rate
    finOpsPools[i].TotalCostUSD = finOpsPools[i].MonthlyCostUSD + finOpsPools[i].OSDiskCostUSD
    finOpsPools[i].TotalCostBRL = finOpsPools[i].MonthlyCostBRL + finOpsPools[i].OSDiskCostBRL
}

// 5b. Enriquecer workloads com custo de storage
pvcByWorkload := groupPVCsByWorkload(pvcs)
for i := range workloads {
    key := workloads[i].Namespace + "/" + workloads[i].Workload
    if items, ok := pvcByWorkload[key]; ok {
        for _, pvc := range items {
            workloads[i].StorageCostUSD += pvc.MonthlyCostUSD
            workloads[i].StorageCostBRL += pvc.MonthlyCostBRL
            workloads[i].PVCCount++
            workloads[i].PVCCapacityGB += pvc.CapacityGB
        }
    }
}

// 5c. Atualizar summary com totais de storage
summary.StorageMonthlyCostBRL = storageSummary.TotalMonthlyCostBRL
summary.StorageMonthlyCostUSD = storageSummary.TotalMonthlyCostUSD
// ...
```

### `internal/web/handlers/finops.go`

- `GetReport` já chama `BuildReport` — apenas passar o `diskPricer` no construtor do `Calculator`
- Registrar `DiskPricer` no `FinOpsHandler` (junto com `pricer` e `exchange` já existentes)
- Adicionar endpoint `POST /finops/storage/refresh` para invalidar cache de disco

### `internal/web/server.go`

Adicionar rota:
```go
finOpsGroup.POST("/storage/refresh", finOpsHandler.RefreshDiskPricing)
```

---

## Frontend: `FinOpsTab.tsx`

### Nova aba "Armazenamento"

Posicionada entre "Node Pools" e "Workloads" (4 abas passam a ser 5).

```
┌─────────────────────────────────────────────────────────────┐
│ Armazenamento                                               │
│                                                             │
│  [R$ 4.280/mês]  [38 PVCs]  [2.4 TB total]  [R$ 320 orfão]│
│                                                             │
│  Por tipo de storage:                                       │
│  ▓▓▓▓▓▓▓▓ Premium SSD    R$ 2.100  12 PVCs   850 GB        │
│  ▓▓▓▓▓▓   Standard SSD   R$ 1.350  18 PVCs   1.2 TB        │
│  ▓▓▓      Azure Files     R$   830   8 PVCs   400 GB        │
│                                                             │
│  Tabela PVCs (ordenável):                                   │
│  Namespace | Nome | Tipo | GB | Tier | R$/mês | Workload | Orfão │
│  ─────────────────────────────────────────────────────────  │
│  ⚠️  PVCs orfãos (não montados por nenhum pod):             │
│  [badge vermelho] 5 PVCs · R$ 320/mês desperdiçados         │
└─────────────────────────────────────────────────────────────┘
```

### Atualização da aba "Visão Geral"

- Summary cards: `Total (Compute + Storage)` substitui só-compute
- Novo summary card: `Armazenamento R$/mês` (subcategoria)
- Gráfico de pizza: compute vs. storage (compute total vs. PVC storage vs. OS disks)
- Alert de orfãos: se há PVCs orfãos, mostrar banner amarelo com saving potencial

### Atualização da aba "Node Pools"

Novas colunas:
- `Disco OS` — ex: `P10 · 128 GB`
- `Custo OS R$/mês` — custo do disco OS × nodes

### Atualização da aba "Workloads"

Novas colunas (opcionais, mostradas quando há dados):
- `PVCs` — número de PVCs vinculados
- `Storage R$/mês` — custo mensal de storage do workload

### Atualização da aba "Oportunidades"

Nova seção: **PVCs Orfãos**
- Lista PVCs não montados por nenhum pod
- Se `reclaimPolicy=Retain`, alerta que o disco persiste mesmo após delete do PVC
- Botão "Copiar kubectl" para deletar o PVC

---

## Estratégia de Correlação PVC → Workload

```
Para cada namespace com PVCs:
  Listar Pods Running no namespace
  Para cada Pod:
    spec.volumes[] → pesquisar volumes com persistentVolumeClaim != nil
    Para cada PVC encontrado:
      Resolver owner: pod → ownerRef[ReplicaSet|StatefulSet|DaemonSet] → ownerRef[Deployment]
      Registrar: pvcName → "namespace/DeploymentName"

Resultado:
  pvcWorkloadMap["mynamespace/my-pvc"] = "mynamespace/my-deployment"

PVCs não presentes no mapa → IsOrphaned = true
```

Limitações conhecidas:
- PVCs montados por Jobs/CronJobs não têm Deployment como owner — serão atribuídos ao Job/CronJob
- PVCs com `phase=Pending` não têm PV vinculado — tamanho extraído de `spec.resources.requests.storage`
- PVCs em namespaces `kube-system` são incluídos (discos de componentes do sistema têm custo real)

---

## Checklist de Implementação

### Fase 7a — Pricing de discos + modelo de dados
- [ ] Criar `internal/finops/azure_disk_pricing.go`
  - [ ] Estrutura `DiskPricer` com cache SQLite (tabela `disk_pricing_cache`)
  - [ ] `GetDiskPrice(type, tier, region)` — managed disks por tier
  - [ ] `GetFilesPricePerGB(type, region)` — Azure Files e Blob por GB
  - [ ] `MapStorageClassToAzureType(scName, provisioner, skuName)` — tabela de mapeamento
  - [ ] `ResolveManagedDiskTier(azureType, capacityGB)` — arredonda para próximo tier
  - [ ] Fallback com preços hardcoded (brazilsouth)
- [ ] Atualizar `internal/finops/models.go`
  - [ ] Adicionar `PVCCostItem`, `StorageClassBreakdown`, `StorageSummary`
  - [ ] Estender `FinOpsPool` com campos OS disk
  - [ ] Estender `FinOpsWorkload` com campos de storage
  - [ ] Estender `FinOpsReport` com `PVCs` e `Storage`
  - [ ] Estender `FinOpsSummary` com totais de storage

### Fase 7b — Storage Calculator
- [ ] Criar `internal/finops/storage_calculator.go`
  - [ ] `StorageCalculator.Calculate(ctx, client, rate)` → `([]PVCCostItem, StorageSummary, error)`
  - [ ] `listStorageClasses(ctx, client)` → mapa sc → provisioner + skuName
  - [ ] `correlateToWorkloads(ctx, client)` → mapa pvcKey → workloadRef (todos namespaces)
  - [ ] `OSDiskForNodePool(ctx, client, poolName)` → sizeGB + source
  - [ ] Testes: pelo menos 1 teste de mapeamento de SC e 1 de cálculo de custo

### Fase 7c — Integração no Calculator
- [ ] Atualizar `internal/finops/calculator.go`
  - [ ] Injetar `DiskPricer` no `Calculator` (construtor `NewCalculator`)
  - [ ] Chamar `StorageCalculator.Calculate` em `BuildReport`
  - [ ] Calcular disco OS por node pool (label K8s ou default 128GB)
  - [ ] Enriquecer `FinOpsWorkload` com storage de PVCs correlacionados
  - [ ] Atualizar `FinOpsSummary` com totais de storage
- [ ] Atualizar `internal/web/handlers/finops.go`
  - [ ] Adicionar `DiskPricer` no `FinOpsHandler`
  - [ ] Adicionar endpoint `POST /finops/storage/refresh`
- [ ] `make build` sem erros

### Fase 7d — Frontend
- [ ] Atualizar TypeScript interfaces em `FinOpsTab.tsx` (novos campos)
- [ ] Adicionar aba "Armazenamento" com tabela PVCs + breakdown por tipo
- [ ] Atualizar aba "Visão Geral" (total com storage, pie chart, alert orfãos)
- [ ] Atualizar aba "Node Pools" (colunas disco OS + custo OS)
- [ ] Atualizar aba "Workloads" (colunas PVC count + storage cost)
- [ ] Adicionar seção "PVCs Orfãos" em "Oportunidades" (com kubectl commands)
- [ ] `./rebuild-web.sh -b` sem erros

---

## Decisões de Design

| Decisão | Escolha | Motivo |
|---------|---------|--------|
| Escopo de storage | Apenas PVCs/PVs visíveis via K8s API | Azure Blob avulso exige Azure Management API (auth adicional) |
| Disco OS dos nodes | Premium SSD P10 (128GB) como default | Padrão AKS; sobrescrito por label `kubernetes.azure.com/os-disk-size-gb` se presente |
| Falha no storage | Não fatal — retorna relatório sem dados de storage + warning | Clusters sem PVCs ou sem acesso não quebram o relatório |
| Cache disco pricing | Mesma SQLite DB das VMs, tabela separada `disk_pricing_cache` | Reutiliza infraestrutura existente |
| Correlação orfão | PVC não presente em `spec.volumes` de nenhum Pod Running | Job/CronJob Pods podem criar falsos positivos — aceito como limitação |
| Azure Files preço | Per-GB (não per-tier) | Azure Files não tem tiers de capacidade como managed disks |
| Namespace kube-system | Incluído nos PVCs | Componentes do sistema (etcd, prometheus) têm custo real de storage |

---

## Impacto no Relatório de AI

O prompt FinOps enviado para AI (`buildFinOpsPrompt` em `finops.go`) deve ser atualizado para incluir:

```
=== ARMAZENAMENTO ===
Total storage: R$ X.XXX/mês (Y% do custo total)
  - Managed Disks: R$ X.XXX (N PVCs, X TB)
  - Azure Files: R$ XXX (N PVCs, X GB)
  - Discos OS nodes: R$ XXX (N nodes × P10)
  - PVCs orfãos: R$ XXX/mês desperdiçados (N PVCs, reclaim=Retain → disco não deletado!)

Top 5 workloads por storage:
  1. namespace/workload: R$ XXX/mês (3 PVCs, 500 GB)
  ...
```

---

## Referências

- Azure Pricing API (Storage): `https://prices.azure.com/api/retail/prices?$filter=serviceName eq 'Storage' and armRegionName eq 'brazilsouth'`
- AKS OS disk types: `https://learn.microsoft.com/en-us/azure/aks/cluster-configuration#os-disk-type`
- AKS Storage Classes padrão: `https://learn.microsoft.com/en-us/azure/aks/concepts-storage#storage-classes`
- K8s PVC API: `CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})`
- Código de referência (VM pricing): `internal/finops/azure_pricing.go`
- Código de referência (workload calc): `internal/finops/calculator.go`
