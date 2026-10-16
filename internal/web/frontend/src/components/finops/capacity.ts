// Capacidade × requests × uso por node pool e decomposição do custo do cluster — fonte única
// para as abas Node Pools e Relatório. Regras:
//  - cpu_request_millis/mem_request_mi são POR POD (total = × pods);
//  - capacidade = ALOCÁVEL dos nodes (status.allocatable), não a nominal da VM — numa F4s_v2 de
//    8 GB o AKS deixa ~5,6 GB para os pods. Relatório antigo sem o campo cai na nominal;
//  - um workload pode ter pods em vários pools (DaemonSet): conta-se pelos pool_pods;
//  - o pool está tão ocupado quanto o recurso mais ocupado (CPU ou memória), e ocupado é o maior
//    entre request e uso real — não a média CPU/memória: com CPU a 75% não sai node nenhum, por
//    mais memória que sobre.
import { fmtBRL } from "@/lib/finopsFormat";
import type { FinOpsPool, FinOpsReport, FinOpsWorkload } from "./types";
import type { PoolCoverage } from "./PricingCoverage";

// Folga sobre a necessidade ao dimensionar nodes (bin-packing e picos). Com a capacidade nominal
// (relatório antigo) a folga cobre também a reserva do sistema.
const HEADROOM_ALLOCATABLE = 1.15;
const HEADROOM_NOMINAL = 1.3;
// Pools de usuário nunca descem abaixo disto nas sugestões.
const MIN_USER_NODES = 2;

export type Resource = "cpu" | "mem";

// ── Reserva / Savings Plan (mesmas regras de internal/finops/coverage.go) ──────────────────────
// Os preços do relatório são de TABELA. Num pool coberto por reserva, tirar nodes não devolve esse
// valor: a reserva segue paga e só deixa de ser desperdício se outra VM do mesmo SKU, no escopo
// dela, a absorver. Por isso a economia de scale-down tem um melhor caso (reserva reaproveitada =
// tabela) e um pior caso (só a fatia sob demanda de hoje). Savings Plan, Spot e moeda que não é BRL:
// qualquer número seria chute — fica só o aviso.
const COVERAGE_SHARE = 0.2;
const HOURS_PER_MONTH = 730;

export interface ScaleDownEconomics {
  basis: "table" | "reservation" | "no_number";
  bestBRL: number;          // reserva reaproveitada / pool sob demanda = preço de tabela
  worstBRL: number | null;  // o que se realiza com certeza (null = não dá para afirmar)
  note: string;
}

// Custo efetivo mensal do pool (Cost Management), quando em BRL.
export function effectiveMonthlyBRL(cov?: PoolCoverage): number | null {
  if (!cov || cov.window_days <= 0 || cov.effective_cost <= 0) return null;
  if (cov.currency && cov.currency.toUpperCase() !== "BRL") return null;
  return cov.effective_cost * (HOURS_PER_MONTH / 24) / cov.window_days;
}

function scaleDownEconomics(tableBRL: number, cov?: PoolCoverage): ScaleDownEconomics {
  const pctTxt = (v: number) => `${Math.round(v * 100)}%`;
  if (!cov) {
    return { basis: "table", bestBRL: tableBRL, worstBRL: tableBRL, note: "Preço de tabela: a cobertura de reserva/Savings Plan deste pool não foi consultada." };
  }
  if (cov.currency && cov.currency.toUpperCase() !== "BRL") {
    return { basis: "no_number", bestBRL: tableBRL, worstBRL: null, note: `O custo efetivo está em ${cov.currency}: sem número em reais.` };
  }
  if (cov.spot >= COVERAGE_SHARE) {
    return { basis: "no_number", bestBRL: tableBRL, worstBRL: null, note: `Spot cobre ~${pctTxt(cov.spot)} do custo: o preço de spot varia e é bem menor que o de tabela.` };
  }
  if (cov.savings_plan >= COVERAGE_SHARE) {
    return { basis: "no_number", bestBRL: tableBRL, worstBRL: null,
      note: `Savings Plan cobre ~${pctTxt(cov.savings_plan)} do custo: o valor comprometido por hora segue cobrado; só economiza se o plano continuar consumido por outros recursos.` };
  }
  if (cov.reservation >= COVERAGE_SHARE) {
    const eff = effectiveMonthlyBRL(cov) ?? 0;
    const odToday = cov.on_demand * eff;
    const worst = Math.round(Math.min(tableBRL, odToday));
    return { basis: "reservation", bestBRL: tableBRL, worstBRL: worst,
      note: `Reserva cobre ~${pctTxt(cov.reservation)} do custo efetivo. Certo mesmo é só a fatia sob demanda de hoje (até ${fmtBRL(odToday)}/mês). ` +
        `O valor de tabela só se realiza se a reserva liberada for usada por outras VMs do mesmo SKU no escopo dela — ou na renovação, comprando menos.` };
  }
  return { basis: "table", bestBRL: tableBRL, worstBRL: tableBRL, note: "Pool praticamente sob demanda: a economia de tabela se realiza." };
}

export interface PoolUsage {
  pool: FinOpsPool;
  costBRL: number;      // compute + disco do SO
  computeBRL: number;   // só compute (a parte ociosa é calculada sobre ele)
  allocatable: boolean; // capacidade = alocável dos nodes (false = nominal da VM)
  capCPU: number;       // millicores
  capMem: number;       // Mi
  reqCPU: number;       // Σ request × pods no pool
  reqMem: number;
  useCPU: number;       // Σ uso médio × pods (só workloads com métrica real)
  useMem: number;
  hasUsage: boolean;
  workloads: number;
  occupiedPct: number;  // 0..1, recurso mais ocupado (maior entre request e uso)
  binding: Resource;    // o recurso que limita o pool
  idleCostBRL: number;  // compute de TABELA × (1 − occupiedPct) — base do Relatório
  effectiveBRL: number | null;      // custo efetivo/mês (Cost Management), quando há
  idleEffectiveBRL: number | null;  // parte ociosa sobre o custo efetivo
  scaleDown: ScaleDownEconomics | null; // só com removable > 0
  headroom: number;
  safeNodes: number;    // nodes necessários com folga (0 = sem sugestão)
  removable: number;
  savingBRL: number;     // economia CERTA do scale-down (pior caso; 0 quando não dá para afirmar)
}

const ratio = (a: number, b: number) => (b > 0 ? a / b : 0);

export function poolCost(p: FinOpsPool): number {
  return p.total_cost_brl || p.monthly_cost_brl;
}

// Capacidade de um node do pool (alocável quando o relatório traz).
function perNode(pool: FinOpsPool): { cpu: number; mem: number; allocatable: boolean } {
  if (pool.allocatable_nodes && pool.allocatable_cpu_millicores && pool.allocatable_memory_mi) {
    return {
      cpu: pool.allocatable_cpu_millicores / pool.allocatable_nodes,
      mem: pool.allocatable_memory_mi / pool.allocatable_nodes,
      allocatable: true,
    };
  }
  return { cpu: pool.vm_cpu_cores * 1000, mem: pool.vm_memory_gb * 1024, allocatable: false };
}

// Pods do workload em cada pool: pool_pods quando há; senão todos no pool dominante.
function podsByPool(w: FinOpsWorkload): Record<string, number> {
  if (w.pool_pods && Object.keys(w.pool_pods).length > 0) return w.pool_pods;
  return w.node_pool ? { [w.node_pool]: w.pods } : {};
}

export function computePoolUsage(pools: FinOpsPool[], workloads: FinOpsWorkload[]): { pools: PoolUsage[]; unassigned: number } {
  const names = new Set(pools.map(p => p.name));
  // pool → [workload, pods dele naquele pool]
  const byPool = new Map<string, [FinOpsWorkload, number][]>();
  let unassigned = 0;
  for (const w of workloads) {
    const entries = Object.entries(podsByPool(w)).filter(([pool]) => names.has(pool));
    if (entries.length === 0) { unassigned++; continue; }
    for (const [pool, pods] of entries) {
      const list = byPool.get(pool) ?? [];
      list.push([w, pods]);
      byPool.set(pool, list);
    }
  }

  const result = pools.map((pool): PoolUsage => {
    const wl = byPool.get(pool.name) ?? [];
    const sum = (f: (w: FinOpsWorkload) => number) => wl.reduce((s, [w, pods]) => s + f(w) * pods, 0);
    const node = perNode(pool);
    const capCPU = node.cpu * pool.node_count;
    const capMem = node.mem * pool.node_count;
    const reqCPU = sum(w => w.cpu_request_millis);
    const reqMem = sum(w => w.mem_request_mi);
    const useCPU = sum(w => w.cpu_avg_millis ?? 0);
    const useMem = sum(w => w.mem_avg_mi ?? 0);
    const costBRL = poolCost(pool);
    const computeBRL = pool.monthly_cost_brl;

    const occCPU = Math.min(1, ratio(Math.max(reqCPU, useCPU), capCPU));
    const occMem = Math.min(1, ratio(Math.max(reqMem, useMem), capMem));
    const occupiedPct = wl.length > 0 ? Math.max(occCPU, occMem) : 0;
    const binding: Resource = occMem > occCPU ? "mem" : "cpu";

    // Scale-down: só pools de usuário com workloads atribuídos. Necessidade por recurso = maior
    // entre request, uso médio e P95 × pods, com folga; vale o recurso que pede mais nodes.
    const headroom = node.allocatable ? HEADROOM_ALLOCATABLE : HEADROOM_NOMINAL;
    let safeNodes = 0, removable = 0, savingBRL = 0;
    let scaleDown: ScaleDownEconomics | null = null;
    if (pool.mode !== "System" && wl.length > 0 && pool.node_count > MIN_USER_NODES) {
      const needCPU = Math.max(reqCPU, useCPU, sum(w => w.cpu_p95_millis ?? 0)) * headroom;
      const needMem = Math.max(reqMem, useMem, sum(w => w.mem_p95_mi ?? 0)) * headroom;
      const need = Math.max(MIN_USER_NODES, Math.ceil(ratio(needCPU, node.cpu)), Math.ceil(ratio(needMem, node.mem)));
      if (need < pool.node_count) {
        safeNodes = need;
        removable = pool.node_count - need;
        scaleDown = scaleDownEconomics(Math.round(removable * ratio(costBRL, pool.node_count)), pool.coverage);
        savingBRL = scaleDown.worstBRL ?? 0;
      }
    }

    return {
      pool, costBRL, computeBRL, allocatable: node.allocatable, capCPU, capMem, reqCPU, reqMem, useCPU, useMem,
      hasUsage: useCPU > 0 || useMem > 0,
      workloads: wl.length,
      occupiedPct, binding,
      idleCostBRL: wl.length > 0 ? Math.round(computeBRL * (1 - occupiedPct)) : 0,
      effectiveBRL: effectiveMonthlyBRL(pool.coverage),
      idleEffectiveBRL: wl.length > 0 && effectiveMonthlyBRL(pool.coverage) !== null
        ? Math.round(effectiveMonthlyBRL(pool.coverage)! * (1 - occupiedPct)) : null,
      scaleDown,
      headroom, safeNodes, removable, savingBRL,
    };
  });
  return { pools: result, unassigned };
}

// Pool sob reserva / Savings Plan / Spot: economia nele só se realiza liberando nodes e
// reaproveitando o compromisso — nunca é "certa".
export function isCommittedPool(pool?: FinOpsPool): boolean {
  const c = pool?.coverage;
  return !!c && (c.reservation >= COVERAGE_SHARE || c.savings_plan >= COVERAGE_SHARE || c.spot >= COVERAGE_SHARE);
}

// Cópia do relatório com os valores em reais dos workloads, namespaces e do resumo no custo
// EFETIVO do pool de cada workload (Dashboard, Oportunidades). Sem cobertura em nenhum pool,
// devolve o próprio relatório. Não passe a cópia para capacity.ts (buildActionPlan,
// computeCostBreakdown...): eles aplicam o fator sobre o relatório original.
export function toEffectiveReport(report: FinOpsReport): FinOpsReport {
  const cost = computeCostBreakdown(report);
  if (cost.coveredPools === 0) return report;
  const factorOf = poolCostFactors(report);
  const pools = new Map((report.node_pools ?? []).map(p => [p.name, p]));
  const workloads = (report.workloads ?? []).map(w => {
    const f = factorOf(w.node_pool);
    const x = (v?: number) => (v === undefined ? v : v * f);
    return {
      ...w,
      cost_share_brl: w.cost_share_brl * f,
      cost_share_usd: w.cost_share_usd * f,
      hpa_cost_min_brl: w.hpa_cost_min_brl * f,
      hpa_cost_max_brl: w.hpa_cost_max_brl * f,
      hpa_cost_current_brl: w.hpa_cost_current_brl * f,
      avg_replicas_cost_brl: x(w.avg_replicas_cost_brl),
      waste_brl: x(w.waste_brl),
      cost_basis: f !== 1 ? "effective" as const : "table" as const,
      committed: isCommittedPool(pools.get(w.node_pool ?? "")),
    };
  });
  const nsCost = new Map<string, number>();
  workloads.forEach(w => nsCost.set(w.namespace, (nsCost.get(w.namespace) ?? 0) + w.cost_share_brl));
  const usdRatio = cost.tableComputeBRL > 0 ? cost.computeBRL / cost.tableComputeBRL : 1;
  return {
    ...report,
    workloads,
    namespaces: (report.namespaces ?? []).map(n => ({
      ...n,
      monthly_cost_brl: nsCost.get(n.namespace) ?? n.monthly_cost_brl * usdRatio,
      monthly_cost_usd: n.monthly_cost_usd * usdRatio,
    })),
    summary: {
      ...report.summary,
      total_monthly_cost_brl: cost.computeBRL,
      total_monthly_cost_usd: report.summary.total_monthly_cost_usd * usdRatio,
      total_with_storage_brl: cost.totalBRL,
      potential_savings_brl: workloads.reduce((s, w) => s + (w.waste_brl ?? 0), 0),
      hpa_savings_if_min_brl: report.summary.hpa_savings_if_min_brl * usdRatio,
    },
  };
}

// Fator custo efetivo ÷ tabela de cada pool (1 sem cobertura). Para levar custo/desperdício de um
// workload (calculados em preço de tabela pelo backend) ao custo efetivo do pool dele.
export function poolCostFactors(report: FinOpsReport): (pool?: string) => number {
  const usage = computePoolUsage(report.node_pools ?? [], report.workloads ?? []).pools;
  const factor = new Map(usage.map(u => [u.pool.name, u.effectiveBRL !== null && u.computeBRL > 0 ? u.effectiveBRL / u.computeBRL : 1]));
  return pool => factor.get(pool ?? "") ?? 1;
}

// Decomposição do custo mensal do cluster. Pools com cobertura do Cost Management entram pelo custo
// EFETIVO (com reserva/Savings Plan) — fator efetivo ÷ tabela do pool, aplicado ao compute, à parte
// ociosa e ao desperdício dos workloads do pool; os demais, por preço de tabela.
//  - capacidade ociosa = Σ da parte ociosa de cada pool (recurso mais ocupado, ver acima);
//  - request acima do uso = Σ waste_brl (o mesmo das abas Workloads), no fator do pool do workload;
//  - em uso = o resto do compute.
export interface CostBreakdown {
  computeBRL: number;
  productiveBRL: number;   // compute ocupado e necessário
  wasteBRL: number;        // request acima do necessário
  idleBRL: number;         // capacidade dos nodes que nem request nem uso ocupam
  osDiskBRL: number;
  pvcBRL: number;          // PVCs em uso
  orphanBRL: number;       // PVCs sem pod
  totalBRL: number;
  coveredPools: number;    // pools pelo custo efetivo
  tableComputeBRL: number; // o compute em preço de tabela, para comparação
  diskEffective: boolean;  // discos pelo custo efetivo (Cost Management)
}

export function computeCostBreakdown(report: FinOpsReport): CostBreakdown {
  const { summary, storage } = report;
  const workloads = report.workloads ?? [];
  const usage = computePoolUsage(report.node_pools ?? [], workloads).pools;
  const factor = new Map(usage.map(u => [u.pool.name, u.effectiveBRL !== null && u.computeBRL > 0 ? u.effectiveBRL / u.computeBRL : 1]));
  const coveredPools = usage.filter(u => u.effectiveBRL !== null).length;

  const tableComputeBRL = summary.total_monthly_cost_brl;
  const computeBRL = coveredPools > 0 ? usage.reduce((s, u) => s + u.computeBRL * (factor.get(u.pool.name) ?? 1), 0) : tableComputeBRL;
  const idleBRL = Math.min(computeBRL, usage.reduce((s, u) => s + u.idleCostBRL * (factor.get(u.pool.name) ?? 1), 0));
  const wasteRaw = workloads.reduce((s, w) => s + (w.waste_brl ?? 0) * (factor.get(w.node_pool ?? "") ?? 1), 0);
  const wasteBRL = Math.min(computeBRL - idleBRL, wasteRaw);
  // Discos: custo efetivo do Cost Management quando gravado (AttachDiskCost), senão tabela.
  const pools = report.node_pools ?? [];
  const pvcs = report.pvcs ?? [];
  const osEffective = pools.some(p => (p.os_disk_effective_brl ?? 0) > 0);
  const pvcEffective = pvcs.some(p => (p.effective_cost_brl ?? 0) > 0);
  const osDiskBRL = osEffective
    ? pools.reduce((s, p) => s + ((p.os_disk_effective_brl ?? 0) > 0 ? p.os_disk_effective_brl! : (p.os_disk_cost_brl ?? 0)), 0)
    : storage?.os_disk_cost_brl ?? 0;
  const pvcCost = (p: (typeof pvcs)[number]) => ((p.effective_cost_brl ?? 0) > 0 ? p.effective_cost_brl! : p.monthly_cost_brl);
  const orphanBRL = pvcEffective ? pvcs.filter(p => p.is_orphaned).reduce((s, p) => s + pvcCost(p), 0) : storage?.orphaned_cost_brl ?? 0;
  const pvcBRL = pvcEffective
    ? pvcs.filter(p => !p.is_orphaned).reduce((s, p) => s + pvcCost(p), 0)
    : Math.max(0, (storage?.total_monthly_cost_brl ?? 0) - orphanBRL);
  const totalBRL = coveredPools > 0 || osEffective || pvcEffective
    ? computeBRL + osDiskBRL + pvcBRL + orphanBRL
    : summary.total_with_storage_brl ?? (computeBRL + osDiskBRL + pvcBRL + orphanBRL);
  return {
    computeBRL,
    productiveBRL: computeBRL - idleBRL - wasteBRL,
    wasteBRL,
    idleBRL,
    osDiskBRL, pvcBRL, orphanBRL, totalBRL,
    coveredPools, tableComputeBRL,
    diskEffective: osEffective || pvcEffective,
  };
}

// Eficiência do cluster: uso médio ÷ request (workloads com métrica) e request ÷ capacidade alocável.
export interface Efficiency {
  cpuUse: number | null;
  memUse: number | null;
  cpuAlloc: number | null;
  memAlloc: number | null;
}

export function computeEfficiency(pools: FinOpsPool[], workloads: FinOpsWorkload[]): Efficiency {
  // Mesma capacidade da aba Node Pools (alocável quando há).
  const capCPU = pools.reduce((s, p) => s + perNode(p).cpu * p.node_count, 0);
  const capMem = pools.reduce((s, p) => s + perNode(p).mem * p.node_count, 0);
  const tot = (list: FinOpsWorkload[], f: (w: FinOpsWorkload) => number) => list.reduce((s, w) => s + f(w) * w.pods, 0);
  const withCPU = workloads.filter(w => (w.cpu_avg_millis ?? 0) > 0 && w.cpu_request_millis > 0);
  const withMem = workloads.filter(w => (w.mem_avg_mi ?? 0) > 0 && w.mem_request_mi > 0);
  const pct = (a: number, b: number) => (b > 0 ? Math.round(a / b * 100) : null);
  return {
    cpuUse: withCPU.length ? pct(tot(withCPU, w => w.cpu_avg_millis ?? 0), tot(withCPU, w => w.cpu_request_millis)) : null,
    memUse: withMem.length ? pct(tot(withMem, w => w.mem_avg_mi ?? 0), tot(withMem, w => w.mem_request_mi)) : null,
    cpuAlloc: pct(tot(workloads, w => w.cpu_request_millis), capCPU),
    memAlloc: pct(tot(workloads, w => w.mem_request_mi), capMem),
  };
}

// Plano de ação: as economias do relatório, sem dupla contagem (request acima do uso, nodes sem
// request e volumes órfãos são partes disjuntas do custo). É o número de "economia possível" de
// todas as abas — não some outra coisa por fora.
export type Priority = "critical" | "high" | "medium";

export interface Action {
  priority: Priority;
  title: string;
  detail: string;
  savingBRL: number;
  where: string;
}

export function buildActionPlan(report: FinOpsReport): { actions: Action[]; totalSaving: number } {
  const workloads = report.workloads ?? [];
  const cost = computeCostBreakdown(report);
  const { pools: poolUsage } = computePoolUsage(report.node_pools ?? [], workloads);
  const hasMetrics = workloads.some(w => (w.cpu_p95_millis ?? 0) > 0 || (w.mem_p95_mi ?? 0) > 0);
  const actions: Action[] = [];

  // Request acima do uso, no custo efetivo do pool do workload. Em pool sob reserva/Savings
  // Plan/Spot não é economia certa: só vira dinheiro se os nodes liberados saírem e a reserva for
  // reaproveitada — fica fora do total e aparece no detalhe.
  const byName = new Map(poolUsage.map(u => [u.pool.name, u]));
  const committed = (u?: PoolUsage) => isCommittedPool(u?.pool);
  const effWaste = (w: FinOpsWorkload) => {
    const u = byName.get(w.node_pool ?? "");
    const f = u && u.effectiveBRL !== null && u.computeBRL > 0 ? u.effectiveBRL / u.computeBRL : 1;
    return (w.waste_brl ?? 0) * f;
  };
  const wasteful = workloads.filter(w => (w.waste_brl ?? 0) > 0).sort((a, b) => effWaste(b) - effWaste(a));
  if (wasteful.length > 0) {
    const certain = wasteful.filter(w => !committed(byName.get(w.node_pool ?? ""))).reduce((s, w) => s + effWaste(w), 0);
    const conditional = wasteful.filter(w => committed(byName.get(w.node_pool ?? ""))).reduce((s, w) => s + effWaste(w), 0);
    const top = wasteful.slice(0, 3).map(w => `${w.workload} (${fmtBRL(effWaste(w))})`).join(" · ");
    actions.push({
      priority: "critical",
      title: `Ajustar requests de ${wasteful.length} workload(s) ao uso real`,
      detail: conditional >= 1
        ? `${top} · ${fmtBRL(conditional)}/mês em pools sob reserva/Savings Plan só viram economia ao liberar nodes e reaproveitar a reserva`
        : top,
      savingBRL: Math.round(certain),
      where: "Rightsizing / Oportunidades",
    });
  }
  // Sem métrica de uso, a única estimativa é o HPA rodando acima do mínimo.
  const superHPA = workloads.filter(w => w.verdict === "superprovisioned");
  if (!hasMetrics && superHPA.length > 0) {
    actions.push({
      priority: "high",
      title: `Reduzir réplicas de ${superHPA.length} HPA(s) superdimensionado(s)`,
      detail: superHPA.slice(0, 3).map(w => `${w.workload}: ${w.hpa_current}/${w.hpa_max}`).join(" · "),
      savingBRL: superHPA.reduce((s, w) => s + Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl), 0),
      where: "HPA Histórico / Oportunidades",
    });
  }
  const scaleDown = poolUsage.filter(u => u.removable > 0);
  if (scaleDown.length > 0) {
    actions.push({
      priority: "high",
      title: `Reduzir nodes em ${scaleDown.length} pool(s)`,
      detail: scaleDown.map(u => {
        const sd = u.scaleDown;
        const extra = sd?.basis === "reservation" ? ` (sob reserva: até ${fmtBRL(sd.bestBRL)} se a reserva for reaproveitada)`
          : sd?.basis === "no_number" ? " (Savings Plan/Spot: sem número)" : "";
        return `${u.pool.name}: ${u.pool.node_count} para ${u.safeNodes}${extra}`;
      }).join(" · "),
      savingBRL: scaleDown.reduce((s, u) => s + u.savingBRL, 0),
      where: "Node Pools",
    });
  }
  if (cost.orphanBRL > 0) {
    actions.push({
      priority: "medium",
      title: `Remover ${report.storage?.orphaned_pvc_count ?? 0} volume(s) sem pod`,
      detail: (report.pvcs ?? []).filter(p => p.is_orphaned).slice(0, 3).map(p => `${p.namespace}/${p.name}`).join(" · "),
      savingBRL: cost.orphanBRL,
      where: "Armazenamento / Recursos Órfãos",
    });
  }
  // Prioridade pelo peso da economia no custo total.
  for (const a of actions) {
    const share = cost.totalBRL > 0 ? a.savingBRL / cost.totalBRL : 0;
    a.priority = share >= 0.05 ? "critical" : share >= 0.01 ? "high" : "medium";
  }
  actions.sort((a, b) => b.savingBRL - a.savingBRL);
  return { actions, totalSaving: actions.reduce((s, a) => s + a.savingBRL, 0) };
}

// ─── Ambientes não produtivos fora do horário (Fase 3) ────────────────────────

/** Ambiente pelo nome do cluster (mesmos apelidos do backend, finops/environment.go). */
export function environmentOf(cluster: string): string {
  const aliases: Record<string, string> = { prd: "prd", prod: "prd", hlg: "hlg", hml: "hlg", stg: "hlg", uat: "hlg", qa: "qa", dev: "dev", sit: "hlg" };
  let env = "";
  for (const tok of cluster.toLowerCase().split(/[-_./: ]+/)) if (aliases[tok]) env = aliases[tok];
  return env;
}

export interface OffHoursSchedule {
  offStart: number;     // hora em que desliga (0–23)
  offEnd: number;       // hora em que liga
  weekends: boolean;    // desligado sábado e domingo inteiros
}

/** Fração da semana com o ambiente desligado. */
export function offFraction(s: OffHoursSchedule): number {
  const night = (s.offEnd - s.offStart + 24) % 24; // horas desligado por dia útil
  const hours = s.weekends ? 5 * night + 48 : 7 * night; // sem fim de semana: só as noites
  return Math.min(1, hours / 168);
}

/** Hora da semana (0 = segunda 00h) está dentro do horário desligado? */
export function isOffHour(h: number, s: OffHoursSchedule): boolean {
  const wd = Math.floor(h / 24), hr = h % 24;
  if (s.weekends && wd >= 5) return true;
  if (s.offStart === s.offEnd) return false;
  return s.offStart > s.offEnd ? hr >= s.offStart || hr < s.offEnd : hr >= s.offStart && hr < s.offEnd;
}

export interface OffHoursPool {
  pool: FinOpsPool;
  measured: boolean;         // a fração veio das horas-node reais (Prometheus), não de "ligado 24 h"
  share: number;             // fração das horas-node do pool que cai no horário desligado
  offNodes: number | null;   // média de nodes fora do horário hoje (medido)
  onNodes: number | null;    // média de nodes no horário hoje (medido)
  monthlyBRL: number;        // custo mensal na base usada (efetivo quando há cobertura)
  savingBRL: number;         // economia mensal com o pool desligado fora do horário
  committed: boolean;        // sob reserva/Savings Plan: a economia não é certa
  stays: boolean;            // pool System (no "escalar a 0" fica ligado)
}

export interface OffHoursEstimate {
  fraction: number;
  stopCluster: { certainBRL: number; committedBRL: number };   // az aks stop: todos os pools
  scaleUserPools: { certainBRL: number; committedBRL: number }; // pools User a 0, System fica
  pools: OffHoursPool[];
}

/** Economia de desligar fora do horário. Reserva/Savings Plan ≥ 20% → a economia não é certa (o
 *  compromisso é pago de qualquer jeito); Spot e sob demanda economizam de verdade. */
export function offHoursEstimate(nodePools: FinOpsPool[], s: OffHoursSchedule, nodeHours?: Record<string, number[]>): OffHoursEstimate {
  const fraction = offFraction(s);
  const pools = nodePools.map(p => {
    // Com o perfil medido: economia = parte das horas-node que hoje roda fora do horário (se o
    // pool já desce à noite, sobra menos para economizar). Sem perfil: supõe ligado 24 h.
    const prof = nodeHours?.[p.name.toLowerCase()] ?? nodeHours?.[p.name];
    let share = fraction, offNodes: number | null = null, onNodes: number | null = null, measured = false;
    if (prof && prof.length === 168) {
      // -1 = hora sem amostra: fica fora da conta. Precisa de dado nas duas faixas para valer.
      let off = 0, on = 0, nOff = 0, nOn = 0;
      prof.forEach((v, h) => {
        if (v < 0) return;
        if (isOffHour(h, s)) { off += v; nOff++; } else { on += v; nOn++; }
      });
      if (nOff >= 12 && nOn >= 12) {
        offNodes = off / nOff;
        onNodes = on / nOn;
        // Horas-node fora do horário na semana inteira, pela média de cada faixa.
        const offH = Math.round(fraction * 168);
        const total = offNodes * offH + onNodes * (168 - offH);
        share = total > 0 ? (offNodes * offH) / total : 0;
        measured = true;
      }
    }
    const eff = effectiveMonthlyBRL(p.coverage);
    const monthlyBRL = eff ?? p.monthly_cost_brl;
    const c = p.coverage;
    const committed = !!c && (c.reservation >= COVERAGE_SHARE || c.savings_plan >= COVERAGE_SHARE);
    return { pool: p, monthlyBRL, savingBRL: monthlyBRL * share, committed, stays: p.mode?.toLowerCase() === "system", measured, share, offNodes, onNodes };
  });
  const sum = (list: OffHoursPool[]) => ({
    certainBRL: list.filter(p => !p.committed).reduce((a, p) => a + p.savingBRL, 0),
    committedBRL: list.filter(p => p.committed).reduce((a, p) => a + p.savingBRL, 0),
  });
  return { fraction, stopCluster: sum(pools), scaleUserPools: sum(pools.filter(p => !p.stays)), pools };
}
