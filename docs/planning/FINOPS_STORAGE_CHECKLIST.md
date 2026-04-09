# FinOps Storage — Checklist de Implementação

> Plano completo: [FINOPS_STORAGE_PLAN.md](FINOPS_STORAGE_PLAN.md)
> Branch de trabalho: `fix-service-now` (ou nova a partir de `main`)
> Última atualização: 2026-04-09

---

## Fase 7a — Pricing de discos + modelo de dados ✅ CONCLUÍDA

### `internal/finops/azure_disk_pricing.go` (arquivo novo)
- [x] Struct `DiskPricer` com `db *sql.DB` e cache SQLite (tabela `disk_pricing_cache`)
- [x] `NewDiskPricer(region string) (*DiskPricer, error)` — abre/cria a mesma DB de VM pricing
- [x] Tabela DDL: `CREATE TABLE IF NOT EXISTS disk_pricing_cache (disk_type, tier, region, price_usd, fetched_at)`
- [x] `GetDiskPrice(diskType, tier, region string) (float64, string, error)` — managed disks por tier (P10, E6, S10)
- [x] `GetFilesPricePerGB(filesType, region string) (float64, string, error)` — Azure Files Standard/Premium e Azure Blob
- [x] `MapStorageClassToAzureType(scName, provisioner, skuName string) (azureType, method string)` — tabela de mapeamento completa
- [x] `ResolveManagedDiskTier(azureType string, capacityGB float64) string` — arredonda GB para próximo tier (P4=32, P6=64, P10=128…)
- [x] Fallback hardcoded: Premium SSD P1–P50, Standard SSD E1–E50, Standard HDD S4–S50, Azure Files, Blob (brazilsouth)
- [x] Azure Pricing API: filter `serviceName eq 'Storage' and armRegionName eq 'brazilsouth'` + parse resposta

### `internal/finops/models.go` (extensões)
- [x] Adicionar struct `PVCCostItem` (Namespace, Name, BoundPV, StorageClass, CapacityGB, AzureDiskType, AzureDiskTier, PricePerMonth, MonthlyCostUSD, MonthlyCostBRL, Phase, ReclaimPolicy, WorkloadRef, IsOrphaned, PriceSource)
- [x] Adicionar struct `StorageClassBreakdown` (StorageClass, AzureType, PVCCount, TotalGB, MonthlyCostBRL)
- [x] Adicionar struct `StorageSummary` (totais, OSDiskCost, PVCCount, OrphanedPVCCount, OrphanedCostBRL, ByStorageClass, ByNamespace)
- [x] Estender `FinOpsPool`: campos OSDiskSKU, OSDiskTier, OSDiskGB, OSDiskCostUSD, OSDiskCostBRL, TotalCostUSD, TotalCostBRL
- [x] Estender `FinOpsWorkload`: campos StorageCostUSD, StorageCostBRL, PVCCount, PVCCapacityGB
- [x] Estender `FinOpsReport`: campos `PVCs []PVCCostItem` e `Storage StorageSummary`
- [x] Estender `FinOpsSummary`: campos StorageMonthlyCostBRL, StorageMonthlyCostUSD, OSDiskCostBRL, OrphanedStorageCostBRL, TotalWithStorageBRL

---

## Fase 7b — Storage Calculator ✅ CONCLUÍDA

### `internal/finops/storage_calculator.go` (arquivo novo)
- [x] Struct `StorageCalculator` com `diskPricer *DiskPricer`
- [x] `NewStorageCalculator(diskPricer)` construtor
- [x] `Calculate(ctx, client, rate) ([]PVCCostItem, StorageSummary, error)` — método principal
- [x] `listStorageClasses(ctx, client)` → `map[string]scInfo` (provisioner + skuName por SC)
- [x] `correlateToWorkloads(ctx, client)` → `map[pvcKey]workloadRef` (todos namespaces)
  - Listar Pods Running → `spec.volumes[].persistentVolumeClaim.claimName`
  - Resolver ownerRef: Pod → ReplicaSet → Deployment (ou StatefulSet/DaemonSet diretamente)
- [x] `OSDiskForNodePool(ctx, client, poolName string) (sizeGB int, source string)`
  - Listar nodes com label `kubernetes.azure.com/agentpool=<poolName>`
  - Tentar label `kubernetes.azure.com/os-disk-size-gb` → retorna "label"
  - Fallback: 128 GB → retorna "default"
- [ ] Testes unitários:
  - [ ] Teste de `MapStorageClassToAzureType` (managed-csi-premium → Premium SSD, azurefile → Azure Files Standard, etc.)
  - [ ] Teste de `ResolveManagedDiskTier` (100 GB Premium SSD → "P10", 33 GB → "P6")
  - [ ] Teste de cálculo de custo de um PVC (CapacityGB × tier price, ou CapacityGB × per_gb)
  - [ ] Teste de detecção de orfão

---

## Fase 7c — Integração no Calculator ✅ CONCLUÍDA

### `internal/finops/calculator.go`
- [x] Adicionar campo `diskPricer *DiskPricer` na struct `Calculator`
- [x] Atualizar `NewCalculator(pricer, diskPricer, exchange)` — aceitar `DiskPricer`
- [x] Em `BuildReport`: após calcular workloads, chamar `StorageCalculator.Calculate` (erro não fatal — log.Warn)
- [x] Calcular disco OS por node pool via `OSDiskForNodePool` + preencher campos OSDisk em `FinOpsPool`
- [x] Enriquecer `FinOpsWorkload` com storage dos PVCs correlacionados (`groupPVCsByWorkload`)
- [x] Atualizar summary com totais de storage

### `internal/web/handlers/finops.go`
- [x] Adicionar `diskPricer *finops.DiskPricer` no struct `FinOpsHandler`
- [x] Atualizar `NewFinOpsHandler` para inicializar `DiskPricer` e passar ao `NewCalculator`
- [x] Adicionar handler `RefreshDiskPricing` (invalida tabela `disk_pricing_cache`)
- [x] `GetReport` retorna os novos campos `pvcs` e `storage` no JSON

### `internal/web/server.go`
- [x] `DiskPricer` injetado via `NewFinOpsHandler` (inicializado internamente)
- [x] Rota `POST /api/v1/finops/storage/refresh` registrada

### Build e testes
- [x] `go test -v ./internal/finops/...` — todos passando (commit c9ee9f4)
- [x] `make build` sem erros
- [ ] Teste manual: `GET /api/v1/finops/report?cluster=X` retorna `pvcs` e `storage` no JSON

---

## Fase 7d — Frontend ✅ CONCLUÍDA

### `FinOpsTab.tsx` — interfaces TypeScript
- [x] Adicionar interface `PVCCostItem` (espelha Go)
- [x] Adicionar interface `StorageClassBreakdown` e `StorageSummary`
- [x] Atualizar `FinOpsPool` com campos OS disk
- [x] Atualizar `FinOpsWorkload` com campos storage
- [x] Atualizar `FinOpsReport` com `pvcs` e `storage`
- [x] Atualizar `FinOpsSummary` com totais storage

### Nova aba "Armazenamento" (`StorageTab`)
- [x] 4 summary cards: Custo Total Storage, Nº PVCs, Disco OS R$/mês, Custo Orfãos
- [x] BarChart por tipo de storage (Premium SSD, Standard SSD, Azure Files, etc.)
- [x] Tabela de PVCs (colunas: Namespace, Nome, Tipo, GB, Tier, R$/mês, Workload, Status)
  - [x] Ordenável por custo / GB / nome
  - [x] Filtro por namespace e por tipo
  - [x] Badge "orfão" vermelho para PVCs sem pod montando
- [x] Seção "PVCs Orfãos" colapsável — lista com reclaimPolicy e kubectl delete sugerido
- [x] Alerta de banner quando há orfãos com custo
- [x] Aba só aparece quando `report.storage` presente (feature flag implícita)

### Aba "Visão Geral" — atualizações
- [x] Card "Custo/mês" exibe breakdown compute + storage quando dados disponíveis

### Aba "Node Pools" — atualizações
- [x] Novas colunas (condicionais): "Disco OS", "OS R$/mês", "Total R$/mês" (compute + OS disk)

### Aba "Workloads" — atualizações
- [x] Nova coluna "Storage R$/mês" com contagem de PVCs (visível apenas quando dados presentes)

### Prompt AI — atualização
- [x] `buildFinOpsPrompt` em `finops.go` com seção `=== ARMAZENAMENTO ===`
  - Total storage, breakdown por tipo, PVCs orfãos com reclaim=Retain destacado
  - Top 5 workloads por custo de storage

### Build final
- [x] `./rebuild-web.sh -b` sem erros
- [ ] Teste visual: aba Armazenamento renderiza com dados reais
- [ ] Teste visual: Node Pools mostra colunas OS disk
- [ ] Teste visual: Workloads mostra colunas storage

---

## Observações para retomada

- **Arquivo de referência para pricing VM**: `internal/finops/azure_pricing.go` — seguir exatamente o mesmo padrão de cache SQLite e fallback
- **Arquivo de referência para calculator**: `internal/finops/calculator.go:BuildReport()` — seguir o padrão de injeção e erro não-fatal
- **Correlação PVC→Workload**: mesmo padrão de `collectWorkloads()` em `calculator.go` (ownerRef chain)
- **Erro não fatal**: se `StorageCalculator.Calculate` falhar (cluster sem PVCs ou sem acesso), retornar relatório sem campos storage + `log.Warn` — não quebrar o relatório compute
- **Namespace kube-system**: incluir nos PVCs (etcd, prometheus têm custo real de disco)
- **Test de integração real**: usar `GET /api/v1/finops/report?cluster=<cluster-admin>` com VPN ativa
