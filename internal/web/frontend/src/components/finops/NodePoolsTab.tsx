// Aba Node Pools do FinOps: capacidade × requests × uso real POR POOL (capacity.ts), custo da
// capacidade ociosa (pelo recurso mais ocupado), sugestão de scale-down por pool, histórico de nodes
// e alternativas de SKU.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, ReferenceLine } from "recharts";
import { AlertTriangle, Loader2, Activity, Boxes, CircleDollarSign, Cpu, MemoryStick, Server, TrendingDown, Info, Database, Microscope } from "lucide-react";
import { fmtBRL, POOL_COLORS, KubectlBlock } from "@/lib/finopsFormat";
import { apiClient } from "@/lib/api/client";
import type { FinOpsPool, FinOpsWorkload } from "./types";
import { computePoolUsage, environmentOf, type PoolUsage } from "./capacity";
import { CoverageLine, CoverageWarningBox, ReservedHintChip, type CoverageWarning, type ReservedHint } from "./PricingCoverage";
import { DeepAnalysisModal } from "./DeepAnalysisModal";
import { OffHoursCard } from "./OffHoursCard";
import { BaseReservationCard } from "./BaseReservationCard";
import { AXIS_TICK, COST_COLORS, CapacityBar, SectionTitle, StatTile, TooltipBox, TooltipRow } from "./charts";

interface VMAlternativeItem {
  vm_size: string;
  cpu_cores: number;
  memory_gb: number;
  mem_per_cpu_gb: number;
  price_usd_hour: number;
  price_source: string;
  cost_delta_pct: number;
  monthly_savings_brl: number;
  reason: string;
  verdict: "recommended" | "consider" | "cheaper";
  coverage_warning?: CoverageWarning;
  reserved_hint?: ReservedHint;
}

interface VMAlternativesResp {
  alternatives: VMAlternativeItem[];
}

const fmtCPU = (m: number) => `${(m / 1000).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} vCPU`;
const fmtMem = (mi: number) => `${(mi / 1024).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} GB`;
const pctOf = (a: number, b: number) => (b > 0 ? Math.round((a / b) * 100) : 0);

// ─── Alternativas de SKU (preço de tabela) ───────────────────────────────────

function PoolSKUAlternatives({ cluster, usage }: { cluster: string; usage: PoolUsage }) {
  const { pool } = usage;
  // Percentual do pool: uso real quando há métrica, senão requests.
  const cpuPct = pctOf(usage.hasUsage ? usage.useCPU : usage.reqCPU, usage.capCPU);
  const memPct = pctOf(usage.hasUsage ? usage.useMem : usage.reqMem, usage.capMem);
  const { data, isLoading, error } = useQuery<VMAlternativesResp>({
    queryKey: ["finops-vm-alternatives", cluster, pool.name, pool.vm_size, cpuPct, memPct, pool.node_count],
    queryFn: () => apiClient.getFinOpsVMAlternatives<VMAlternativesResp>(cluster, pool.name, pool.vm_size, cpuPct, memPct, pool.node_count),
    enabled: !!pool.vm_size && (cpuPct > 0 || memPct > 0),
    staleTime: 30 * 60 * 1000,
    retry: false,
  });

  if (isLoading) return (
    <p className="flex items-center gap-1.5 text-xs text-muted-foreground"><Loader2 className="h-3 w-3 animate-spin" />Buscando alternativas…</p>
  );
  if (error) return <p className="text-xs text-rose-600">Não foi possível buscar alternativas: {(error as Error).message}</p>;
  if (!data?.alternatives?.length) return <p className="text-xs text-muted-foreground">Nenhuma alternativa melhor para o perfil de uso atual ({cpuPct}% CPU · {memPct}% memória).</p>;

  const verdictCfg = {
    recommended: { label: "Recomendado", cls: "text-emerald-700 dark:text-emerald-300 bg-emerald-500/10" },
    consider:    { label: "Considerar",  cls: "text-sky-700 dark:text-sky-300 bg-sky-500/10" },
    cheaper:     { label: "Mais barato", cls: "text-amber-700 dark:text-amber-300 bg-amber-500/10" },
  };
  return (
    <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
      {data.alternatives.map(alt => {
        const cfg = verdictCfg[alt.verdict] ?? verdictCfg.consider;
        return (
          <div key={alt.vm_size} className="rounded-lg border p-3 space-y-1">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-mono font-semibold">{alt.vm_size}</span>
              <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${cfg.cls}`}>{cfg.label}</span>
            </div>
            <p className="text-[11px] text-muted-foreground">
              {alt.cpu_cores} vCPU · {alt.memory_gb} GB · {alt.mem_per_cpu_gb} GB/vCPU · ${alt.price_usd_hour.toFixed(3)}/h
            </p>
            {Math.abs(alt.monthly_savings_brl) > 10 && (
              <p className={`text-xs font-semibold ${alt.monthly_savings_brl > 0 ? "text-emerald-600" : "text-rose-600"}`}>
                {alt.monthly_savings_brl > 0 ? "−" : "+"}{fmtBRL(Math.abs(alt.monthly_savings_brl))}/mês ({alt.cost_delta_pct > 0 ? "+" : ""}{alt.cost_delta_pct.toFixed(0)}%)
              </p>
            )}
            <p className="text-[11px] text-muted-foreground italic">{alt.reason}</p>
            <ReservedHintChip hint={alt.reserved_hint} />
            <CoverageWarningBox warning={alt.coverage_warning} />
          </div>
        );
      })}
    </div>
  );
}

// ─── Card de um pool ─────────────────────────────────────────────────────────

function PoolCard({ usage, color, provider, clusterScaledUp, onDeepAnalysis }: {
  usage: PoolUsage;
  color: string;
  provider?: string;
  clusterScaledUp: boolean;
  onDeepAnalysis: () => void;
}) {
  const { pool } = usage;
  const idlePct = Math.round((1 - usage.occupiedPct) * 100);
  const capNote = usage.allocatable ? "alocável" : "nominal da VM";
  return (
    <Card className="overflow-hidden">
      <div className="h-1" style={{ background: color }} />
      <CardContent className="p-4 space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="flex items-center gap-2 font-semibold">
              {pool.name}
              <Badge variant={pool.mode === "System" ? "outline" : "secondary"} className="text-[10px]">{pool.mode || "—"}</Badge>
            </p>
            <p className="text-xs text-muted-foreground font-mono truncate">
              {pool.node_count} × {pool.vm_size} · {pool.vm_cpu_cores} vCPU / {pool.vm_memory_gb} GB
            </p>
          </div>
          <div className="text-right shrink-0">
            {usage.effectiveBRL !== null ? (
              <>
                <p className="text-lg font-semibold tabular-nums" title="Custo efetivo (amortizado) no Cost Management, com reserva/Savings Plan">
                  {fmtBRL(usage.effectiveBRL)} <span className="text-[11px] font-normal text-muted-foreground">efetivo</span>
                </p>
                <p className="text-[11px] text-muted-foreground">tabela {fmtBRL(usage.costBRL)}/mês · ${pool.vm_price_usd_hour.toFixed(3)}/h por node</p>
              </>
            ) : (
              <>
                <p className="text-lg font-semibold tabular-nums">{fmtBRL(usage.costBRL)}</p>
                <p className="text-[11px] text-muted-foreground">
                  por mês, preço de tabela · ${pool.vm_price_usd_hour.toFixed(3)}/h por node
                  {pool.price_source !== "api" && <span className="text-amber-600"> · preço estimado</span>}
                </p>
              </>
            )}
          </div>
        </div>
        {provider === "aks" && <CoverageLine coverage={pool.coverage} />}

        {usage.workloads > 0 ? (
          <div className="space-y-3">
            <CapacityBar label="CPU" cap={usage.capCPU} req={usage.reqCPU} use={usage.useCPU} hasUsage={usage.hasUsage} format={fmtCPU}
              capLabel={capNote} limiting={usage.binding === "cpu"} />
            <CapacityBar label="Memória" cap={usage.capMem} req={usage.reqMem} use={usage.useMem} hasUsage={usage.hasUsage} format={fmtMem}
              capLabel={capNote} limiting={usage.binding === "mem"} />
          </div>
        ) : (
          <p className="text-xs text-muted-foreground flex items-center gap-1.5">
            <Info className="h-3.5 w-3.5" /> Nenhum workload deste relatório foi associado a este pool.
          </p>
        )}

        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t pt-3 text-xs">
          <span className="text-muted-foreground">{usage.workloads} workloads</span>
          {pool.os_disk_sku === "Efêmero" ? (
            <span className="text-muted-foreground" title="Disco do SO no cache/disco temporário da VM: já está no preço da VM, sem custo à parte. Tamanho visto pelo node (sistema de arquivos).">
              Disco OS efêmero · ~{pool.os_disk_gb} GB · sem custo à parte
            </span>
          ) : pool.os_disk_tier && (
            <span className="text-muted-foreground">Disco OS {pool.os_disk_tier} ({pool.os_disk_sku}) · {pool.os_disk_gb} GB{pool.os_disk_cost_brl ? ` · ${fmtBRL(pool.os_disk_cost_brl)}/mês` : ""}</span>
          )}
          {usage.workloads > 0 && (
            <span className="font-medium" style={{ color: idlePct >= 40 ? COST_COLORS.idle : undefined }}
              title={`O pool está ${100 - idlePct}% ocupado pelo recurso mais cheio (${usage.binding === "cpu" ? "CPU" : "memória"}), contando o maior entre request e uso. ` +
                (usage.idleEffectiveBRL !== null
                  ? `Sobre o custo efetivo; em preço de tabela seriam ${fmtBRL(usage.idleCostBRL)}/mês.`
                  : `Sobre o compute em preço de tabela (${fmtBRL(usage.computeBRL)}/mês).`)}>
              Ociosa: {fmtBRL(usage.idleEffectiveBRL ?? usage.idleCostBRL)}/mês ({idlePct}%{usage.idleEffectiveBRL !== null ? ", efetivo" : " do compute, tabela"})
            </span>
          )}
          <Button size="sm" variant="outline" className="ml-auto h-7 gap-1" onClick={onDeepAnalysis}
            title="Gargalo de agendamento, custo dos DaemonSets, interação com HPA, throttling e nodes por SKU">
            <Microscope className="h-3.5 w-3.5" /> Deep Analysis
          </Button>
        </div>

        {usage.removable > 0 && usage.scaleDown && (
          <div className={`rounded-lg border p-3 space-y-2 ${usage.scaleDown.basis === "table" ? "bg-emerald-500/10 border-emerald-500/30" : "bg-amber-500/10 border-amber-500/30"}`}>
            <div className="text-sm flex items-start justify-between gap-2">
              <span>
                Dá para reduzir de <strong>{pool.node_count}</strong> para <strong>{usage.safeNodes}</strong> nodes
              </span>
              <span className="text-right">
                {usage.scaleDown.worstBRL === null ? (
                  <span className="block font-semibold text-amber-600">sem número</span>
                ) : usage.scaleDown.worstBRL > 0 ? (
                  <span className="block font-semibold text-emerald-600 dark:text-emerald-400">
                    −{fmtBRL(usage.scaleDown.worstBRL)}/mês{usage.scaleDown.basis !== "table" && " certos"}
                  </span>
                ) : (
                  <span className="block font-semibold text-amber-600">nenhuma economia certa</span>
                )}
                {usage.scaleDown.basis !== "table" && (
                  <span className="block text-[11px] text-muted-foreground">até −{fmtBRL(usage.scaleDown.bestBRL)} em preço de tabela</span>
                )}
              </span>
            </div>
            <p className="text-[11px] text-muted-foreground">{usage.scaleDown.note}</p>
            <p className="text-[11px] text-muted-foreground">
              Conta: maior entre requests, uso médio e P95 × pods, sobre a capacidade {capNote}, com {Math.round((usage.headroom - 1) * 100)}% de folga.
              {clusterScaledUp && <span className="text-amber-600"> O cluster já teve mais nodes nos últimos 30 dias — confira o pico antes.</span>}
            </p>
            {provider === "aks" ? (
              <KubectlBlock cmd={`az aks nodepool scale --resource-group <RESOURCE_GROUP> --cluster-name <CLUSTER> --name ${pool.name} --node-count ${usage.safeNodes}`} />
            ) : (
              <p className="text-[11px] text-muted-foreground">Ajuste o tamanho do node group no console/CLI do provedor.</p>
            )}
            <p className="text-[11px] text-muted-foreground">Com autoscaler ligado, reduza o mínimo do pool em vez do número de nodes.</p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ─── Aba ─────────────────────────────────────────────────────────────────────

export function NodePoolsTab({ pools, workloads, cluster, provider, reportAt }: {
  pools: FinOpsPool[];
  workloads: FinOpsWorkload[];
  cluster: string;
  provider?: string;
  reportAt?: string; // generated_at do relatório (chave do cache das sugestões de reserva)
}) {
  const { pools: usage, unassigned } = computePoolUsage(pools, workloads);
  const [deepPool, setDeepPool] = useState<string | null>(null);
  const totalCost = usage.reduce((s, u) => s + u.costBRL, 0);
  const totalNodes = pools.reduce((s, p) => s + p.node_count, 0);
  const capCPU = usage.reduce((s, u) => s + u.capCPU, 0);
  const capMem = usage.reduce((s, u) => s + u.capMem, 0);
  const reqCPU = usage.reduce((s, u) => s + u.reqCPU, 0);
  const reqMem = usage.reduce((s, u) => s + u.reqMem, 0);
  const useCPU = usage.reduce((s, u) => s + u.useCPU, 0);
  const useMem = usage.reduce((s, u) => s + u.useMem, 0);
  const hasUsage = usage.some(u => u.hasUsage);
  // Ociosa e custo: efetivo (Cost Management) nos pools com cobertura, tabela nos demais.
  const idleCost = usage.reduce((s, u) => s + (u.idleEffectiveBRL ?? u.idleCostBRL), 0);
  const covered = usage.filter(u => u.effectiveBRL !== null);
  const effectiveTotal = usage.reduce((s, u) => s + (u.effectiveBRL ?? u.costBRL), 0);
  const uncertainSaving = usage.some(u => u.scaleDown && u.scaleDown.basis !== "table");
  const suggested = usage.filter(u => u.removable > 0).length;
  const totalSaving = usage.reduce((s, u) => s + u.savingBRL, 0);
  const idleOf = (u: PoolUsage) => u.idleEffectiveBRL ?? u.idleCostBRL;
  const topIdle = [...usage].sort((a, b) => idleOf(b) - idleOf(a)).find(u => idleOf(u) > 0);

  // Mesma query key do Dashboard: reaproveita o cache.
  const { data: tl } = useQuery({
    queryKey: ["finops-timeline-dashboard", cluster, 30],
    queryFn: () => apiClient.getFinOpsTimeline(cluster, 30),
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  const history = (tl?.nodes ?? []).map(n => ({ date: n.date.slice(5), nodes: n.node_count }));
  const counts = history.map(h => h.nodes);
  const minNodes = counts.length ? Math.min(...counts) : null;
  const maxNodes = counts.length ? Math.max(...counts) : null;
  const clusterScaledUp = maxNodes !== null && maxNodes > totalNodes;

  return (
    <div className="space-y-4">
      {/* Indicadores */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {covered.length > 0 ? (
          <StatTile icon={CircleDollarSign} label="Custo dos pools" value={fmtBRL(effectiveTotal)}
            sub={`efetivo em ${covered.length} de ${pools.length} pools · tabela ${fmtBRL(totalCost)} · ${totalNodes} nodes`} />
        ) : (
          <StatTile icon={CircleDollarSign} label="Custo dos pools" value={fmtBRL(totalCost)}
            sub={`preço de tabela · ${pools.length} pools · ${totalNodes} nodes`} />
        )}
        <StatTile icon={Cpu} label="CPU requisitada" value={`${pctOf(reqCPU, capCPU)}%`}
          sub={hasUsage ? `${pctOf(useCPU, capCPU)}% em uso · ${fmtCPU(capCPU)}` : fmtCPU(capCPU)} />
        <StatTile icon={MemoryStick} label="Memória requisitada" value={`${pctOf(reqMem, capMem)}%`}
          sub={hasUsage ? `${pctOf(useMem, capMem)}% em uso · ${fmtMem(capMem)}` : fmtMem(capMem)} />
        <StatTile icon={TrendingDown} label="Ociosa (todos os pools)" value={fmtBRL(idleCost)}
          tone={idleCost > effectiveTotal * 0.3 ? "bad" : idleCost > effectiveTotal * 0.15 ? "warn" : "default"}
          sub={<>
            {covered.length > 0 && <>efetivo onde há cobertura · </>}
            {topIdle && <>maior: {topIdle.pool.name} {fmtBRL(topIdle.idleEffectiveBRL ?? topIdle.idleCostBRL)} · </>}
            {suggested === 0 ? "nenhum scale-down sugerido"
              : `scale-down em ${suggested} pool(s): −${fmtBRL(totalSaving)}/mês certos${uncertainSaving ? " (reserva/Savings Plan: veja cada card)" : ""}`}
          </>} />
      </div>

      {/* Produção: reserva/Savings Plan para os nodes que nunca saem */}
      {provider === "aks" && !["hlg", "dev", "qa"].includes(environmentOf(cluster)) && reportAt && <BaseReservationCard cluster={cluster} reportAt={reportAt} />}

      {/* Ambiente não produtivo: quanto economiza desligando fora do horário */}
      {["hlg", "dev", "qa"].includes(environmentOf(cluster)) && <OffHoursCard pools={pools} cluster={cluster} provider={provider} />}

      {unassigned > 0 && (
        <p className="text-xs text-muted-foreground flex items-center gap-1.5">
          <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
          {unassigned} workload(s) sem pool identificado (node sem label reconhecida) ficaram fora da conta por pool.
        </p>
      )}

      {/* Pools */}
      <div>
        <SectionTitle icon={Boxes} title="Capacidade por pool"
          sub={<>Trilho = capacidade dos nodes · <span className="text-indigo-500">faixa clara</span> = requisitado pelos pods{hasUsage && <> · <span className="text-emerald-500">faixa cheia</span> = uso médio real</>}</>} />
        <div className="grid gap-3 xl:grid-cols-2">
          {usage.map((u, i) => (
            <PoolCard key={u.pool.name} usage={u} color={POOL_COLORS[i % POOL_COLORS.length]} provider={provider}
              clusterScaledUp={clusterScaledUp} onDeepAnalysis={() => setDeepPool(u.pool.name)} />
          ))}
        </div>
      </div>

      {deepPool && <DeepAnalysisModal cluster={cluster} pool={deepPool} onClose={() => setDeepPool(null)} />}

      {/* Histórico de nodes */}
      {history.length > 3 && (
        <Card>
          <CardContent className="p-4">
            <SectionTitle icon={Activity} title="Nodes nos últimos 30 dias"
              right={<span className="text-xs text-muted-foreground tabular-nums">mín {minNodes} · máx {maxNodes} · atual {totalNodes}</span>} />
            <ResponsiveContainer width="100%" height={150}>
              <AreaChart data={history} margin={{ left: -16, right: 8, top: 8, bottom: 0 }}>
                <defs>
                  <linearGradient id="np-nodes" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#6366f1" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} stroke="hsl(var(--border))" strokeDasharray="3 3" />
                <XAxis dataKey="date" tick={AXIS_TICK} axisLine={false} tickLine={false} interval="preserveStartEnd" minTickGap={24} />
                <YAxis tick={AXIS_TICK} axisLine={false} tickLine={false} allowDecimals={false} domain={[0, "auto"]} />
                <Tooltip cursor={{ stroke: "hsl(var(--border))" }} content={({ active, payload, label }) =>
                  active && payload?.length ? <TooltipBox title={label}><TooltipRow color="#6366f1" label="Nodes" value={payload[0].value} /></TooltipBox> : null} />
                <ReferenceLine y={totalNodes} stroke="hsl(var(--muted-foreground))" strokeDasharray="4 4"
                  label={{ value: "atual", position: "insideTopRight", fontSize: 10, fill: "hsl(var(--muted-foreground))" }} />
                <Area type="stepAfter" dataKey="nodes" stroke="#6366f1" strokeWidth={2} fill="url(#np-nodes)" dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      {/* Alternativas de SKU */}
      {usage.some(u => u.pool.mode !== "System" && u.workloads > 0) && (
        <Card>
          <CardContent className="p-4 space-y-4">
            <SectionTitle icon={Database} title="Alternativas de SKU"
              sub={'Economia em preço de tabela (pay-as-you-go). Com a cobertura consultada ("Atualizar cobertura" no Rightsizing), o pool em reserva mostra o efeito real da troca.'} />
            {usage.filter(u => u.pool.mode !== "System" && u.workloads > 0).map(u => (
              <div key={u.pool.name} className="space-y-2">
                <p className="text-xs flex items-center gap-2">
                  <Server className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="font-medium">{u.pool.name}</span>
                  <span className="font-mono text-muted-foreground">{u.pool.vm_size}</span>
                </p>
                <PoolSKUAlternatives cluster={cluster} usage={u} />
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
