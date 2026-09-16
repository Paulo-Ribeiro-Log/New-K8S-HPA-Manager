// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell,
  ComposedChart, Area, ReferenceLine,
} from "recharts";
import { AlertTriangle, Loader2, Activity, Cpu, Database } from "lucide-react";
import { fmtBRL, POOL_COLORS, KubectlBlock } from "@/lib/finopsFormat";
import type { FinOpsPool, FinOpsWorkload, TimelineReport } from "./types";

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
}

interface VMAlternativesResp {
  sku: string;
  cpu_cores: number;
  memory_gb: number;
  cpu_pct: number;
  mem_pct: number;
  alternatives: VMAlternativeItem[];
}

// ─── Componente: Alternativas de SKU por Pool ────────────────────────────────

function PoolSKUAlternatives({ pool, cpuPct, memPct }: {
  pool: FinOpsPool;
  cpuPct: number;
  memPct: number;
}) {
  const { data, isLoading } = useQuery<VMAlternativesResp>({
    queryKey: ["finops-vm-alternatives", pool.vm_size, cpuPct, memPct, pool.node_count],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/vm-alternatives?sku=${encodeURIComponent(pool.vm_size)}&cpu_pct=${cpuPct}&mem_pct=${memPct}&node_count=${pool.node_count}`,
        { headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` } }
      );
      if (!r.ok) throw new Error("vm-alternatives error");
      return r.json();
    },
    enabled: !!pool.vm_size && (cpuPct > 0 || memPct > 0),
    staleTime: 30 * 60 * 1000,
    retry: false,
  });

  if (isLoading) return (
    <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground py-1">
      <Loader2 className="h-3 w-3 animate-spin" />Buscando alternativas…
    </div>
  );
  if (!data?.alternatives?.length) return (
    <p className="text-[10px] text-muted-foreground py-1">Nenhuma alternativa identificada para o padrão atual.</p>
  );

  const verdictCfg = {
    recommended: { label: "Recomendado", cls: "text-green-600 dark:text-green-400 bg-green-100 dark:bg-green-900/30" },
    consider:    { label: "Considerar",  cls: "text-blue-600  dark:text-blue-400  bg-blue-100  dark:bg-blue-900/30"  },
    cheaper:     { label: "Mais barato", cls: "text-amber-600 dark:text-amber-400 bg-amber-100 dark:bg-amber-900/30" },
  };

  return (
    <div className="space-y-1.5">
      {data.alternatives.map(alt => {
        const cfg = verdictCfg[alt.verdict] ?? verdictCfg.consider;
        return (
          <div key={alt.vm_size} className="border rounded-lg p-2.5 space-y-1 bg-muted/20">
            <div className="flex items-center justify-between gap-2 flex-wrap">
              <span className="text-xs font-mono font-semibold">{alt.vm_size}</span>
              <div className="flex items-center gap-1.5">
                {alt.monthly_savings_brl > 10 && (
                  <span className="text-[11px] font-bold text-green-600 dark:text-green-400">
                    -{fmtBRL(alt.monthly_savings_brl)}/mês
                  </span>
                )}
                {alt.monthly_savings_brl < -10 && (
                  <span className="text-[11px] font-semibold text-orange-600">
                    +{fmtBRL(Math.abs(alt.monthly_savings_brl))}/mês
                  </span>
                )}
                <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${cfg.cls}`}>
                  {cfg.label}
                </span>
              </div>
            </div>
            <p className="text-[10px] text-foreground">
              {alt.cpu_cores} vCPU · {alt.memory_gb} GB RAM · <strong>{alt.mem_per_cpu_gb} GB/vCPU</strong>
              <span className="text-muted-foreground"> · ${alt.price_usd_hour.toFixed(3)}/hora</span>
              {alt.cost_delta_pct !== 0 && (
                <span className={`ml-1 font-medium ${alt.cost_delta_pct < 0 ? "text-green-600" : "text-red-500"}`}>
                  ({alt.cost_delta_pct > 0 ? "+" : ""}{alt.cost_delta_pct.toFixed(1)}%)
                </span>
              )}
            </p>
            <p className="text-[10px] text-muted-foreground italic">{alt.reason}</p>
          </div>
        );
      })}
    </div>
  );
}

// ─── Aba: Node Pools ──────────────────────────────────────────────────────────

export function NodePoolsTab({ pools, workloads, cluster }: {
  pools: FinOpsPool[];
  workloads: FinOpsWorkload[];
  cluster: string;
}) {
  const total = pools.reduce((s, p) => s + p.monthly_cost_brl, 0);
  const totalCPU = pools.reduce((s, p) => s + p.total_cpu_millicores, 0);
  const totalMem = pools.reduce((s, p) => s + p.total_memory_mi, 0);
  const totalNodes = pools.reduce((s, p) => s + p.node_count, 0);

  // ── Histórico de nodes (reutiliza cache do DashboardTab — mesma query key) ──
  const { data: tl } = useQuery<TimelineReport>({
    queryKey: ["finops-timeline-dashboard", cluster, 30],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=30`,
        { headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` } }
      );
      if (!r.ok) throw new Error("Timeline error");
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const nodesHistory = tl?.nodes ?? [];
  const minObservedNodes = nodesHistory.length > 0 ? Math.min(...nodesHistory.map(n => n.node_count)) : null;
  const maxObservedNodes = nodesHistory.length > 0 ? Math.max(...nodesHistory.map(n => n.node_count)) : null;
  const avgObservedNodes = nodesHistory.length > 0
    ? Math.round(nodesHistory.reduce((s, n) => s + n.node_count, 0) / nodesHistory.length)
    : null;
  const nodeChartData = nodesHistory.map(n => ({ date: n.date.slice(5), Nodes: n.node_count }));
  // Alerta: cluster chegou a ter mais nodes do que o atual → não sugere scale-down agressivo
  const clusterScaledUp = maxObservedNodes !== null && maxObservedNodes > totalNodes;

  // ── Rightsizing: utilização real vs capacidade ──────────────────────────────
  // cpu_request_millis = total de todos os pods do workload (soma acumulada no backend)
  const totalCPUReqMillis  = workloads.reduce((s, w) => s + w.cpu_request_millis, 0);
  const totalMemReqMi      = workloads.reduce((s, w) => s + w.mem_request_mi, 0);
  const totalCPUActualMillis = workloads.reduce((s, w) => s + (w.cpu_avg_millis ?? 0) * w.pods, 0);
  const totalMemActualMi     = workloads.reduce((s, w) => s + (w.mem_avg_mi ?? 0) * w.pods, 0);
  const hasPrometheus = totalCPUActualMillis > 0;

  const allocCPUPct  = totalCPU > 0 ? Math.round(totalCPUReqMillis / totalCPU * 100) : null;
  const allocMemPct  = totalMem > 0 ? Math.round(totalMemReqMi / totalMem * 100) : null;
  const actualCPUPct = hasPrometheus && totalCPU > 0 ? Math.round(totalCPUActualMillis / totalCPU * 100) : null;
  const actualMemPct = hasPrometheus && totalMem > 0 ? Math.round(totalMemActualMi / totalMem * 100) : null;

  // Recomendações de scale-down por pool (apenas User pools com node_count > 2)
  const utilizationPct = actualCPUPct ?? allocCPUPct ?? 0;
  // Se cluster escalou além do atual recentemente, o threshold cai para 40% (mais conservador)
  const scaleDownThreshold = clusterScaledUp ? 40 : 55;
  const rightsizingRecs = pools
    .filter(p => p.mode !== "System" && p.node_count > 2 && utilizationPct < scaleDownThreshold)
    .map(p => {
      const cpuPerNode = p.vm_cpu_cores * 1000;
      const memPerNode = p.vm_memory_gb * 1024;
      // Capacidade mínima segura: max(request, actual) × 1.4 (40% headroom)
      const safeCPUMillis = Math.max(totalCPUReqMillis, totalCPUActualMillis) * 1.4;
      const safeMemMi     = Math.max(totalMemReqMi, totalMemActualMi) * 1.4;
      // Quantos nodes deste pool seriam necessários apenas para este pool
      // (heurística: se é o único pool user, seria o total; se há vários, proporcional)
      const otherUserCPU = totalCPU - p.total_cpu_millicores;
      const cpuRemainingNeed = Math.max(0, safeCPUMillis - otherUserCPU);
      const memRemainingNeed = Math.max(0, safeMemMi - (totalMem - p.total_memory_mi));
      const minByCPU = cpuPerNode > 0 ? Math.ceil(cpuRemainingNeed / cpuPerNode) : p.node_count;
      const minByMem = memPerNode > 0 ? Math.ceil(memRemainingNeed / memPerNode) : p.node_count;
      const safeCount = Math.max(2, minByCPU, minByMem);
      if (safeCount >= p.node_count) return null;
      const removable = p.node_count - safeCount;
      const costPerNodeBRL = p.node_count > 0 ? p.monthly_cost_brl / p.node_count : 0;
      return {
        pool: p,
        safeCount,
        removable,
        savingBRL: Math.round(removable * costPerNodeBRL),
        cmd: `az aks nodepool scale --cluster-name <CLUSTER_NAME> --name ${p.name} --node-count ${safeCount} --resource-group <RESOURCE_GROUP>`,
      };
    })
    .filter((r): r is NonNullable<typeof r> => r !== null);

  // Chart de capacidade: vCPUs e RAM por pool
  const capData = pools.map((p, i) => ({
    name: p.name.length > 12 ? p.name.slice(0, 10) + "…" : p.name,
    vcpu: p.vm_cpu_cores * p.node_count,
    ram_gb: p.vm_memory_gb * p.node_count,
    color: POOL_COLORS[i % POOL_COLORS.length],
  }));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{pools.length} node pools • Total: <strong className="text-foreground">{fmtBRL(total)}/mês</strong></span>
        <span className="text-xs">Capacidade total: <strong className="text-foreground">{(totalCPU / 1000).toFixed(0)} vCPUs · {(totalMem / 1024).toFixed(0)} GB RAM</strong></span>
      </div>

      {/* ── Utilização do Cluster (Rightsizing 3a) ─────────────────────────── */}
      {(allocCPUPct !== null || allocMemPct !== null) && (
        <Card className={rightsizingRecs.length > 0 ? "border-orange-200 dark:border-orange-900/40" : ""}>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Cpu className="h-4 w-4 text-orange-500" />
              Utilização do Cluster
              {!hasPrometheus && (
                <span className="text-[10px] font-normal text-muted-foreground">(baseado em requests — ative Prometheus para uso real)</span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3 space-y-3">
            {/* Barras de utilização */}
            <div className="grid grid-cols-2 gap-4">
              {allocCPUPct !== null && (
                <div>
                  <div className="flex justify-between text-xs mb-1">
                    <span className="text-muted-foreground">CPU alocada</span>
                    <span className={`font-semibold ${allocCPUPct < 30 ? "text-red-500" : allocCPUPct < 60 ? "text-yellow-500" : "text-green-500"}`}>{allocCPUPct}%</span>
                  </div>
                  <div className="h-2 rounded-full bg-muted overflow-hidden">
                    <div className="h-full rounded-full transition-all" style={{ width: `${Math.min(100, allocCPUPct)}%`, background: allocCPUPct < 30 ? "#ef4444" : allocCPUPct < 60 ? "#f59e0b" : "#10b981" }} />
                  </div>
                  {actualCPUPct !== null && (
                    <div className="flex justify-between text-xs mt-1">
                      <span className="text-muted-foreground text-[10px]">CPU real (P95 avg)</span>
                      <span className={`text-[10px] font-semibold ${actualCPUPct < 20 ? "text-red-500" : "text-blue-500"}`}>{actualCPUPct}%</span>
                    </div>
                  )}
                </div>
              )}
              {allocMemPct !== null && (
                <div>
                  <div className="flex justify-between text-xs mb-1">
                    <span className="text-muted-foreground">Mem alocada</span>
                    <span className={`font-semibold ${allocMemPct < 30 ? "text-red-500" : allocMemPct < 60 ? "text-yellow-500" : "text-green-500"}`}>{allocMemPct}%</span>
                  </div>
                  <div className="h-2 rounded-full bg-muted overflow-hidden">
                    <div className="h-full rounded-full transition-all" style={{ width: `${Math.min(100, allocMemPct)}%`, background: allocMemPct < 30 ? "#ef4444" : allocMemPct < 60 ? "#f59e0b" : "#10b981" }} />
                  </div>
                  {actualMemPct !== null && (
                    <div className="flex justify-between text-xs mt-1">
                      <span className="text-muted-foreground text-[10px]">Mem real (avg)</span>
                      <span className={`text-[10px] font-semibold ${actualMemPct < 20 ? "text-red-500" : "text-blue-500"}`}>{actualMemPct}%</span>
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Recomendações de scale-down */}
            {rightsizingRecs.length > 0 && (
              <div className="space-y-2 pt-1 border-t">
                <p className="text-xs font-medium text-orange-600 dark:text-orange-400">
                  Oportunidades de rightsizing ({rightsizingRecs.length} pool{rightsizingRecs.length > 1 ? "s" : ""}):
                </p>
                {rightsizingRecs.map(r => (
                  <div key={r.pool.name} className="bg-orange-50/60 dark:bg-orange-950/20 rounded-lg p-3 space-y-1.5">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium">{r.pool.name}</span>
                      <span className="text-xs font-bold text-green-600">-{fmtBRL(r.savingBRL)}/mês</span>
                    </div>
                    <p className="text-[11px] text-muted-foreground">
                      Reduzir de <strong>{r.pool.node_count}</strong> → <strong>{r.safeCount}</strong> nodes
                      ({r.removable} node{r.removable > 1 ? "s" : ""} • {r.pool.vm_size})
                    </p>
                    <KubectlBlock cmd={r.cmd} />
                  </div>
                ))}
              </div>
            )}
            {/* Histórico de nodes (30d) */}
            {nodeChartData.length > 3 && (
              <div className="pt-1 border-t space-y-1.5">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
                    <Activity className="h-3 w-3" />
                    Histórico de nodes — 30d
                  </p>
                  {(minObservedNodes !== null || maxObservedNodes !== null || avgObservedNodes !== null) && (
                    <div className="flex gap-3 text-[10px] text-muted-foreground">
                      {minObservedNodes !== null && <span>mín <strong className="text-foreground">{minObservedNodes}</strong></span>}
                      {avgObservedNodes !== null && <span>avg <strong className="text-foreground">{avgObservedNodes}</strong></span>}
                      {maxObservedNodes !== null && <span>máx <strong className="text-foreground">{maxObservedNodes}</strong></span>}
                    </div>
                  )}
                </div>
                {clusterScaledUp && (
                  <div className="flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/30 rounded px-2 py-1">
                    <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                    Cluster chegou a {maxObservedNodes} nodes neste período (atual: {totalNodes}) — recomendação de scale-down conservadora ({scaleDownThreshold}% threshold)
                  </div>
                )}
                <ResponsiveContainer width="100%" height={70}>
                  <ComposedChart data={nodeChartData} margin={{ left: 4, right: 4, top: 2, bottom: 0 }}>
                    <XAxis dataKey="date" tick={{ fontSize: 9, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                    <YAxis tick={{ fontSize: 9 }} axisLine={false} tickLine={false} width={20} allowDecimals={false} />
                    <Tooltip content={({ active, payload, label }) => {
                      if (!active || !payload?.length) return null;
                      return (
                        <div style={{ background: "hsl(var(--card))", border: "1px solid var(--border)", borderRadius: 6, padding: "4px 8px", fontSize: 11 }}>
                          <p>{label}: <strong>{payload[0]?.value} nodes</strong></p>
                        </div>
                      );
                    }} />
                    <Area type="stepAfter" dataKey="Nodes" fill="#06b6d4" stroke="#06b6d4" fillOpacity={0.12} strokeWidth={1.5} dot={false} />
                    {totalNodes && <ReferenceLine y={totalNodes} stroke="#f59e0b" strokeDasharray="4 2" strokeOpacity={0.7}
                      label={{ value: `atual`, position: "right", fontSize: 9, fill: "#f59e0b" }} />}
                  </ComposedChart>
                </ResponsiveContainer>
              </div>
            )}

            {utilizationPct > 0 && rightsizingRecs.length === 0 && (
              <p className="text-[11px] text-muted-foreground">
                Utilização dentro do esperado — nenhum pool elegível para scale-down com margem segura de {scaleDownThreshold}%.
              </p>
            )}
          </CardContent>
        </Card>
      )}

      {/* Chart de capacidade */}
      <Card>
        <CardHeader className="pb-1 pt-3 px-4">
          <CardTitle className="text-sm">Capacidade por Pool</CardTitle>
        </CardHeader>
        <CardContent className="px-2 pb-3">
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={capData} margin={{ left: 4, right: 8, top: 4, bottom: 4 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.4} />
              <XAxis dataKey="name" tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis yAxisId="cpu" tick={{ fontSize: 10 }} axisLine={false} tickLine={false}
                label={{ value: "vCPU", angle: -90, position: "insideLeft", style: { fontSize: 9, fill: "#9ca3af" } }} />
              <YAxis yAxisId="ram" orientation="right" tick={{ fontSize: 10 }} axisLine={false} tickLine={false}
                label={{ value: "GB", angle: 90, position: "insideRight", style: { fontSize: 9, fill: "#9ca3af" } }} />
              <Tooltip
                cursor={{ fill: "rgba(100,100,100,0.1)" }}
                content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 4 }}>{label}</p>
                      {payload.map((p, i) => (
                        <p key={i} style={{ color: p.color }}>{p.name}: {p.value}</p>
                      ))}
                    </div>
                  );
                }}
              />
              <Bar yAxisId="cpu" dataKey="vcpu" name="vCPUs" radius={[3, 3, 0, 0]} maxBarSize={28}>
                {capData.map((e, i) => <Cell key={i} fill={e.color} />)}
              </Bar>
              <Bar yAxisId="ram" dataKey="ram_gb" name="RAM (GB)" radius={[3, 3, 0, 0]} maxBarSize={28} fillOpacity={0.4}>
                {capData.map((e, i) => <Cell key={i} fill={e.color} />)}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
          <div className="flex items-center gap-4 px-3 mt-1 flex-wrap">
            {pools.map((p, i) => (
              <span key={p.name} className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <span className="w-2 h-2 rounded-sm inline-block" style={{ background: POOL_COLORS[i % POOL_COLORS.length] }} />
                {p.name}
              </span>
            ))}
            <span className="ml-auto flex items-center gap-3 text-[10px] text-muted-foreground">
              <span className="flex items-center gap-1">
                <span className="inline-block w-3 h-3 rounded-sm opacity-100" style={{ background: "#6366f1" }} /> vCPUs (eixo esq.)
              </span>
              <span className="flex items-center gap-1">
                <span className="inline-block w-3 h-3 rounded-sm opacity-40" style={{ background: "#6366f1" }} /> RAM GB (eixo dir.)
              </span>
            </span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Pool</TableHead>
              <TableHead>VM Size</TableHead>
              <TableHead className="text-center">vCPU</TableHead>
              <TableHead className="text-center">RAM</TableHead>
              <TableHead className="text-center">Nodes</TableHead>
              <TableHead className="text-center">Modo</TableHead>
              <TableHead className="text-right">USD/hora</TableHead>
              <TableHead className="text-right">Compute R$/mês</TableHead>
              {pools.some(p => p.os_disk_tier) && (
                <>
                  <TableHead className="text-center">Disco OS</TableHead>
                  <TableHead className="text-right">OS R$/mês</TableHead>
                  <TableHead className="text-right">Total R$/mês</TableHead>
                </>
              )}
              <TableHead className="text-center">Fonte</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pools.map((p) => (
              <TableRow key={p.name}>
                <TableCell className="font-medium text-sm">{p.name}</TableCell>
                <TableCell className="font-mono text-xs">{p.vm_size}</TableCell>
                <TableCell className="text-center text-sm">{p.vm_cpu_cores}</TableCell>
                <TableCell className="text-center text-sm">{p.vm_memory_gb} GB</TableCell>
                <TableCell className="text-center text-sm">{p.node_count}</TableCell>
                <TableCell className="text-center">
                  <Badge variant={p.mode === "System" ? "outline" : "secondary"} className="text-[10px]">
                    {p.mode}
                  </Badge>
                </TableCell>
                <TableCell className="text-right font-mono text-xs">${p.vm_price_usd_hour.toFixed(3)}</TableCell>
                <TableCell className="text-right font-semibold text-sm">{fmtBRL(p.monthly_cost_brl)}</TableCell>
                {pools.some(q => q.os_disk_tier) && (
                  <>
                    <TableCell className="text-center text-xs">
                      {p.os_disk_tier ? (
                        <span className="font-mono">{p.os_disk_tier} · {p.os_disk_gb} GB</span>
                      ) : <span className="text-muted-foreground">—</span>}
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">
                      {p.os_disk_cost_brl ? fmtBRL(p.os_disk_cost_brl) : "—"}
                    </TableCell>
                    <TableCell className="text-right font-semibold text-sm text-blue-600">
                      {p.total_cost_brl ? fmtBRL(p.total_cost_brl) : fmtBRL(p.monthly_cost_brl)}
                    </TableCell>
                  </>
                )}
                <TableCell className="text-center">
                  <Badge variant={p.price_source === "api" ? "default" : "outline"} className="text-[10px]">
                    {p.price_source === "api" ? "Azure API" : "Fallback"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      {/* ── Sugestões de SKU por Pool ──────────────────────────────────────── */}
      {(actualCPUPct !== null || actualMemPct !== null) && pools.some(p => p.mode !== "System") && (
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Database className="h-4 w-4 text-indigo-500" />
              Sugestões de Família de SKU
              <span className="text-[10px] font-normal text-muted-foreground">
                {actualCPUPct !== null
                  ? `baseado em uso real — CPU: ${actualCPUPct}% · Mem: ${actualMemPct ?? 0}%`
                  : "ative Prometheus para sugestões baseadas em uso real"}
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-4 space-y-4">
            {pools.filter(p => p.mode !== "System").map(p => (
              <div key={p.name}>
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-xs font-medium">{p.name}</span>
                  <span className="text-[10px] text-muted-foreground font-mono">{p.vm_size}</span>
                  <span className="text-[10px] text-muted-foreground">
                    · {p.vm_cpu_cores} vCPU / {p.vm_memory_gb} GB · {p.vm_memory_gb / p.vm_cpu_cores} GB/vCPU · {p.node_count} nodes
                  </span>
                </div>
                <PoolSKUAlternatives
                  pool={p}
                  cpuPct={actualCPUPct ?? allocCPUPct ?? 0}
                  memPct={actualMemPct ?? allocMemPct ?? 0}
                />
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
