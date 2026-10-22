# FinOps Tab — Plano de Melhorias

> Criado em 2026-03-28. Continuar este plano em novos chats lendo este arquivo primeiro.
> Arquivo principal: `internal/web/frontend/src/components/FinOpsTab.tsx`
> Backend: `internal/web/handlers/finops.go`

---

## Checklist Geral

### Fase 1 — Quick wins (baixo esforço, alto impacto) ✅ CONCLUÍDA
- [x] **1a** Botão "Copiar" inline para kubectl command nos cards de Oportunidades
- [x] **1b** Gerar `kubectl set resources` para OOM Risk e CPU/Mem overprovisioned
- [x] **1c** Indicador de staleness — badge "⚠ Prometheus não incluído" quando report foi gerado sem Prometheus

### Fase 2 — Filtros e interatividade (médio esforço) ✅ CONCLUÍDA
- [x] **2a** Filtros na aba Oportunidades: por namespace, tipo de ação, saving mínimo, "só com kubectl"
- [x] **2b** Checkbox por card + total de saving dinâmico (simular "se aplicar essas 5...")
- [x] **2c** Agrupamento de Oportunidades por tipo de ação (seções colapsáveis: HPA, CPU, Mem, Sem Request)

### Fase 3 — Infraestrutura e projeções (alto esforço) ✅ CONCLUÍDA
- [x] **3a** Recomendação de Node Pool rightsizing (cruzar CPUEff% com node count → sugerir escalar down)
- [x] **3b** Projeção de custo futuro (linha de tendência 30d baseada na série temporal)
- [x] **3c** Novo veredicto `fixed_high_cost` para workloads caros sem HPA (candidatos a adicionar HPA)

---

## Detalhes por item

### 1a — Botão "Copiar" kubectl
**Arquivo:** `FinOpsTab.tsx` — componente `OpportunitiesTab`, bloco do `w.rec.kubectl`
**Implementação:** `navigator.clipboard.writeText(w.rec.kubectl)` + ícone `Copy` do lucide-react
**Onde renderizar:** ao lado do `<code>` que já exibe o comando

### 1b — kubectl para CPU/Mem
**Arquivo:** `FinOpsTab.tsx` — função `buildRecommendation()`
**Comando a gerar:**
```bash
# Para superprovisioned com CPU:
kubectl set resources deployment <workload> -n <namespace> \
  --requests=cpu=<recommended>m,memory=<recommended>Mi \
  --limits=cpu=<limit>m,memory=<limit>Mi

# Para OOM risk (aumentar):
kubectl set resources deployment <workload> -n <namespace> \
  --requests=cpu=<recommended>m,memory=<recommended>Mi
```
**Lógica:** se `cpu_recommended_millis` existe E difere >15% do request → gerar comando
**Múltiplos comandos:** `buildRecommendation` deve retornar `kubectlList: string[]` (array)

### 1c — Staleness indicator
**Arquivo:** `FinOpsTab.tsx` — linha do metadado do report (linha ~2543)
**Lógica:** se `withPrometheus === false` E `report.window_days === 0`, mostrar badge âmbar
**Badge:** `⚠ Sem Prometheus — saving estimado apenas por HPA config` com link para reativar

### 2a — Filtros em Oportunidades
**Estado:** `filterNs`, `filterType`, `minSaving`, `onlyWithKubectl`
**Tipos de ação:** "HPA min", "HPA max", "CPU request", "Mem request", "Remover HPA", "Adicionar request"

### 2b — Checkbox + saving selecionado
**Estado:** `selectedIds: Set<string>` (key = `${namespace}/${workload}`)
**UI:** checkbox no canto do card + barra sticky no rodapé: "X selecionados → economia: R$Y/mês"

### 2c — Agrupamento por tipo
**Tipos de grupo:**
1. `hpa_min` — Reduzir minReplicas (maior economia imediata)
2. `hpa_max` — Reduzir maxReplicas (limitar exposição)
3. `cpu_request` — Reduzir CPU request
4. `mem_request` — Reduzir Mem request
5. `remove_hpa` — Remover HPA (fixar réplicas)
6. `add_request` — Definir resource requests

### 3a — Node Pool rightsizing
**Backend:** `internal/web/handlers/finops.go` — novo campo `pool_cpu_utilization_pct` no `FinOpsPool`
**Lógica:** cruzar `total_cpu_millicores` do pool com `sum(cpu_avg_millis × pods)` dos workloads naquele pool
**Frontend:** novo card em `NodePoolsTab` + recomendação em Oportunidades com veredicto `pool_oversized`
**Comando sugerido:** `az aks nodepool scale --cluster-name X --name Y --node-count Z`

### 3b — Projeção de custo
**Dados:** série `nodes × costPerNodePerDay` dos últimos 30d → regressão linear → extrapolar 90d
**UI:** em `DashboardTab`, linha tracejada além do último ponto do gráfico "Custo Diário"
**Recharts:** `<Area>` adicional com dados calculados no frontend (sem backend)

### 3c — Fixed high cost sem HPA
**Backend:** `internal/web/handlers/finops.go` — na função de veredicto, adicionar:
- Se `hpa_max == 0` E `cost_share_brl > threshold` (ex: > média × 2) E `pods > 3` → veredicto `fixed_high_cost`
**Frontend:** novo item em `verdictConfig` + lógica em `buildRecommendation`

---

## Status de conclusão

| Fase | Item | Status | Commit |
|------|------|--------|--------|
| 1    | 1a   | ✅ concluído | — |
| 1    | 1b   | ✅ concluído | — |
| 1    | 1c   | ✅ concluído | — |
| 2    | 2a   | ✅ concluído | — |
| 2    | 2b   | ✅ concluído | — |
| 2    | 2c   | ✅ concluído | — |
| 3    | 3a   | ✅ concluído | 262ab88 |
| 3    | 3b   | ✅ concluído | 262ab88 |
| 3    | 3c   | ✅ concluído | 262ab88 |
