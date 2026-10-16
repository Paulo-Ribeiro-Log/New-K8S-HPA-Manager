# Plano: revisão das abas do FinOps (consolidação + funções que faltam)

**Status**: 🟡 em andamento. Fases 0, 1 e 2 concluídas (branch `feat/finops-relatorio-nodepools`); Fase 3 em andamento (custo faturado feito).

Para continuar em outro chat, leia este arquivo e o `CLAUDE.md`, e marque cada item `[x]` ao terminar. Faça um PR por fase, seguindo o padrão do repo: commit do código + commit `chore(web)` com os assets.

## Contexto (análise de outubro de 2026)

A aba FinOps tem 12 sub-abas (`components/FinOpsTab.tsx`). Várias mostram o mesmo dado de formas diferentes, e algumas ficaram para trás quando abas mais novas passaram a fazer o mesmo trabalho melhor:

| Aba | Decisão |
|---|---|
| Dashboard | manter, mais enxuto (tirar o card "Oportunidades de Economia" e o gráfico de réplicas dos HPAs, que repetem outras abas) |
| Node Pools | manter (refeita na Fase 0) |
| Workloads | **remover**: o Rightsizing mostra o mesmo e mais (limits, pico, histórico, uso atual) |
| HPA Histórico | manter: é a única que olha o comportamento de escala |
| Armazenamento | **juntar com Recursos Órfãos**: um PVC sem pod é um disco desatachado, então o mesmo disco aparece nas duas |
| Oportunidades | **juntar com Rightsizing** como filtro "com oportunidade" |
| Relatório | manter (refeito na Fase 0) |
| Rightsizing | manter: vira a aba principal de workloads |
| Deep Analysis | **remover a aba**: o botão "Analisar pool" vai para cada card do Node Pools |
| Recursos de Dados | manter, mas só para AKS (depende da convenção `rg-<nome>-data-<env>`) |
| Recursos Órfãos | manter, absorvendo os PVCs órfãos do Armazenamento |

Estrutura-alvo (feita na Fase 2): **Dashboard · Relatório · Workloads (Rightsizing + Oportunidades) · Node Pools (+ Deep Analysis) · HPA · Armazenamento e Órfãos · Recursos de Dados**.

### Regras para não repetir os erros encontrados

- `cpu_request_millis`/`mem_request_mi` do `FinOpsWorkload` são **por pod** (`internal/finops/models.go`). Total = × `pods`.
- Conta de capacidade, custo ocioso e scale-down: sempre via `components/finops/capacity.ts` (`computePoolUsage`, `computeCostBreakdown`, `computeEfficiency`). Não refaça a conta em outra aba.
- Capacidade = **alocável** dos nodes (`allocatable_*` do pool; relatório antigo cai na nominal). Um pool está tão ocupado quanto o recurso mais cheio (CPU ou memória), usando o maior entre request e uso. Nada de média CPU/memória: com a CPU a 75%, não sai node nenhum. Pods por pool vêm do `pool_pods` (DaemonSet).
- Decomposição do custo: capacidade ociosa = Σ da parte ociosa dos pools; request acima do uso = Σ `waste_brl`; em uso = o resto do compute.
- Visual: `components/finops/charts.tsx` (`StatTile`, `CapacityBar`, `RingGauge`, `TooltipBox`, `COST_COLORS`).
- HTTP sempre por `lib/api/client.ts` (`getFinOpsTimeline`, `getFinOpsVMAlternatives`, …).

## Fase 0 — Relatório e Node Pools ✅

- [x] `capacity.ts`: capacidade × requests × uso **por pool** (pelo `node_pool` do workload), custo sem request, scale-down por pool (requests ou P95 × pods, +30% de folga, mínimo de 2 nodes, nunca no pool System).
- [x] Node Pools refeita:
  - cards por pool com barras de capacidade;
  - indicadores do cluster;
  - histórico de nodes;
  - comando `az aks nodepool scale` só para AKS, com aviso de autoscaler.
- [x] Corrigido: o Node Pools somava o request sem × pods, com "CPU alocada" muito baixa e scale-down falso.
- [x] Corrigido: o scale-down usava a utilização do cluster inteiro para todos os pools.
- [x] Corrigido: as "Alternativas de SKU" nunca funcionaram (chamada sem `cluster` → 400 engolido). Agora usam o percentual do próprio pool e avisam que o preço é de tabela.
- [x] Corrigido: o gráfico "Capacidade por Pool" (vCPU e GB em dois eixos, mesma cor, só opacidade diferente) foi trocado pelas barras de capacidade.
- [x] Relatório refeito:
  - capa com os indicadores;
  - "Para onde vai o dinheiro" (rosca);
  - medidores de eficiência;
  - tendência de 30 dias;
  - treemap de namespaces colorido pelo desperdício;
  - maiores custos empilhados;
  - plano de ação com economia coerente;
  - riscos;
  - qualidade dos dados;
  - PDF novo.

## Fase 1 — Correções que sobraram (sem mudar a estrutura) ✅

- [x] `helpers.ts::financeProviderInfo`:
  - recebe o `cloud_provider` do cluster, em vez do prefixo do nome;
  - fonte certa por provider (Azure Retail Prices / AWS Price List / GCP Billing Catalog).
- [x] Cabeçalho: "Custo estimado pela … — reservas e Savings Plan não descontados", em vez de "Custo real".
- [x] Dashboard:
  - o custo diário ficou rotulado como estimativa (nodes × custo médio por node), porque a timeline só tem a contagem total de nodes;
  - a projeção de 30 dias usa a data completa da série, sem o ano fixo em 2026.
- [x] Economia unificada: `buildActionPlan` (`capacity.ts`) é a fonte do "Economia possível" do Dashboard e do Relatório.
- [x] Todos os `fetch` diretos do FinOps passam pelo `apiClient`. O `get`/`post` genéricos ganharam `init` opcional para o `signal`. Arquivos migrados:
  - FinOpsTab, Dashboard, HPA Histórico, Rightsizing, Recursos de Dados, PerfBenchmark e PricingCoverage;
  - os hooks `useRightsizingReport` e `useUnattachedDisks`.
- [x] `GET /finops/vm-alternatives?pool=…` anexa `CoverageWarning`/`ReservedHint` com a mesma regra do rightsizing (`annotateAlternativesCoverage`, testado). O Node Pools mostra o aviso.
- [x] Comandos com o tipo certo. O backend agora devolve `kind` (`ResolveWorkloadKind`, testado; coluna `kind` no SQLite do rightsizing, por migração). Os comandos ficaram assim:
  - `kubectl set resources`/`autoscale` usam deployment, statefulset ou daemonset;
  - para Job, ReplicaSet solto e Pod avulso, a orientação vem em texto;
  - a nota de Helm/Argo/Flux aparece sob os comandos (Oportunidades e Rightsizing).

As sobras desta fase foram resolvidas na Fase 2.

## Fase 2 — Consolidação das abas ✅

Estrutura final, de 12 para 7 abas: **Dashboard · Relatório · Workloads · Node Pools · HPA · Armazenamento e Órfãos · Recursos de Dados** (esta só para AKS). Sem relatório, só aparecem Armazenamento e Órfãos e, no AKS, Recursos de Dados.

- [x] Aba Workloads antiga removida (`WorkloadsTab.tsx` apagado). O CSV do cabeçalho continua.
- [x] Nova aba **Workloads** (`WorkloadsHubTab.tsx`), com duas visões em abas manuais:
  - "Request e limit": o Rightsizing inteiro, com scan, histórico e tier de VM;
  - "Oportunidades": a antiga aba, com filtros, agrupamento por ação e seleção com soma.

  O badge conta os workloads com oportunidade (`isOpportunity`, em `helpers.ts`, o mesmo filtro da visão).
- [x] Deep Analysis virou o botão "Deep Analysis" em cada card do Node Pools, que abre o `DeepAnalysisModal`. `DeepAnalysisTab.tsx` foi apagado.
- [x] **Armazenamento e Órfãos** (`StorageOrphansTab.tsx`): seção "Volumes do cluster" (PVCs, com o relatório) e seção "Recursos órfãos na nuvem" (discos e, no AKS, demais órfãos). O disco de um PVC sem pod ganha a etiqueta "PVC sem pod — já em Volumes do cluster".
- [x] Dashboard sem o card "Oportunidades de Economia" (que trazia o número divergente) e sem o gráfico de réplicas dos HPAs. Os KPIs, custo diário, namespaces e nodes ficaram.
- [x] Recursos de Dados, como aba e como resumo no Dashboard, só aparecem para AKS.
- [x] Badges:
  - Relatório = nº de ações do plano;
  - Workloads = workloads com oportunidade;
  - HPA = HPAs que nunca escalaram;
  - Armazenamento = PVCs órfãos.

  O `RightsizingTabBadge` foi removido.
- [x] Sobras da Fase 1:
  - `CoverageRefreshButton` recebe o `provider`, sem prefixo de nome;
  - o relatório traz o `hpa_name` (nome real do HPA) e o `kubectl patch hpa` o usa.

## Correção pós-Fase 2 — capacidade ociosa e reserva ✅

- [x] Ociosa pelo recurso mais ocupado (maior entre request e uso), sobre a capacidade alocável (`allocatable_*`), com pods por pool (`pool_pods`).
- [x] Cobertura de reserva/Savings Plan no relatório (`coverage` por pool). Node Pools e Relatório usam o custo efetivo onde há cobertura, e a economia de scale-down e de requests separa o certo (sob demanda) do condicional (reserva reaproveitada).
- [x] Dashboard, Workloads (Rightsizing/Oportunidades), CSV e cabeçalho pelo custo efetivo:
  - `toEffectiveReport` faz a cópia do relatório para exibição (nunca passe a cópia para as contas do `capacity.ts`, que aplicam o fator sobre o original);
  - o Rightsizing recebe `costFactor`/`committedPool`;
  - economia em pool sob reserva aparece como condicional ("só liberando nodes");
  - o HPA não mostra valores em reais;
  - as alternativas de SKU seguem em preço de tabela, com o aviso de cobertura.
- [ ] O custo efetivo cobre só compute (`Virtual Machines`); discos do SO e PVCs seguem em preço de tabela.

## Fase 3 — Funções que faltam (por prioridade)

- [x] **Custo faturado de verdade.** Painel "Fatura do Azure × estimativa" no Relatório e valor no cabeçalho: custo amortizado de 30 dias do RG do AKS + RG de nodes, por serviço (`GET /finops/billed-cost`, `BilledCostPanel.tsx`). Só AKS.
  - [ ] Sobra: RG de dados (pode estar em outra subscription, ver `resolveDataRG`) e EKS/GKE (Cost Explorer / BigQuery billing export) ficam de fora.
- [ ] **Isenções por workload.** Implementar o `FINOPS-EXEMPTIONS-PLAN.md` (o plano está escrito, mas o store não existe). Sem isso, os HPAs 1/1 de HLG continuam como desperdício.
- [ ] **Visão por jornada ou multi-cluster.** Ranking de custo e desperdício a partir do `finops-report-cache.db`, sem novo scan.
- [ ] **Aplicar e acompanhar.**
  - Mandar a recomendação para o Staging (HPA/resources) ou gerar o patch de values.
  - Registrar no Change History.
  - Medir a economia realizada no próximo scan.
- [ ] **Scan agendado com alerta no Teams** quando o custo ou o desperdício passa de um limite.
- [ ] **Showback.** Custo por namespace ou label de dono, exportável por mês.
- [ ] **Ambientes não produtivos fora do horário.** Estimar quanto se economiza parando o AKS ou escalando os pools de HLG à noite e no fim de semana.
- [ ] **Reserva para a carga base.** Sugerir reserva ou Savings Plan pelo mínimo estável de nodes de cada pool, cruzando com a cobertura atual.

## Verificação (a cada item)

- `npx tsc --noEmit -p tsconfig.app.json` (baseline: 0 erros) e `npx eslint` nos arquivos mexidos.
- Teste visual em headless: servidor temporário na porta 8098 com `HOME` de teste e go-rod `HijackRequests` devolvendo um relatório falso em `/api/v1/finops/report/last`, `/timeline` e `/clusters`. Clique em Tools → FinOps → sub-aba e tire a screenshot.
- Mudou frontend: `make web-build` e commit `chore(web)` separado.
