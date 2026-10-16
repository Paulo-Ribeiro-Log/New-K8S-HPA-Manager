// Visão multi-cluster do FinOps: ranking de custo e desperdício a partir do último relatório gravado
// de cada cluster (GET /finops/overview, sem novo scan). Mesmas regras da tela de um cluster: custo
// efetivo nos pools com cobertura, desperdício em pool sob reserva não é economia certa.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from "recharts";
import { Layers, Wallet, PiggyBank, ShieldAlert, Loader2, ArrowRight, Clock, Server } from "lucide-react";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";
import { fmtBRL } from "@/lib/finopsFormat";
import type { ClusterOverview } from "./types";
import { AXIS_TICK, COST_COLORS, SectionTitle, StatTile, TooltipBox, TooltipRow } from "./charts";

const shortBRL = (v: number) => (v >= 1000 ? `R$ ${(v / 1000).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} mil` : fmtBRL(v));
const pct = (a: number, b: number) => (b > 0 ? Math.round((a / b) * 100) : 0);
const shortName = (c: string) => c.replace(/-admin$/, "").replace(/^aks(priv|pub)?-/, "");
const STALE_DAYS = 7;

interface Group {
  key: string;
  label: string;
  sub: string;
  total: number;
  waste: number;
  idle: number;
  wasteCommitted: number;
  risks: number;
  nodes: number;
  oldestDays: number;
  clusters: ClusterOverview[];
}

const daysAgo = (iso: string) => Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000);

function group(rows: ClusterOverview[], by: "cluster" | "journey"): Group[] {
  const map = new Map<string, Group>();
  for (const r of rows) {
    const key = by === "cluster" ? r.cluster : (r.journey || "sem jornada");
    const g = map.get(key) ?? {
      key, label: by === "cluster" ? shortName(r.cluster) : key, sub: "",
      total: 0, waste: 0, idle: 0, wasteCommitted: 0, risks: 0, nodes: 0, oldestDays: 0, clusters: [],
    };
    g.total += r.total_brl;
    g.waste += r.waste_brl;
    g.idle += r.idle_brl ?? 0;
    g.wasteCommitted += r.waste_committed_brl;
    g.risks += r.oom_risk;
    g.nodes += r.nodes;
    g.oldestDays = Math.max(g.oldestDays, daysAgo(r.generated_at));
    g.clusters.push(r);
    map.set(key, g);
  }
  return [...map.values()]
    .map(g => ({
      ...g,
      sub: by === "cluster"
        ? [g.clusters[0].journey, g.clusters[0].environment?.toUpperCase()].filter(Boolean).join(" · ")
        : `${g.clusters.length} cluster(s) · ${[...new Set(g.clusters.map(c => c.environment?.toUpperCase()).filter(Boolean))].join(", ")}`,
    }))
    .sort((a, b) => b.total - a.total);
}

export function ClustersOverviewTab({ currentCluster, onOpen }: { currentCluster: string; onOpen: (cluster: string) => void }) {
  const [by, setBy] = useState<"cluster" | "journey">("cluster");
  const [env, setEnv] = useState("all");
  const { data, isLoading, error } = useQuery({
    queryKey: ["finops-overview"],
    queryFn: () => apiClient.get<{ clusters: ClusterOverview[] }>("/finops/overview"),
    staleTime: 60_000,
  });

  if (isLoading) {
    return <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" /> Lendo os relatórios gravados…</p>;
  }
  if (error) return <p className="text-sm text-rose-600">{(error as Error).message}</p>;

  const all = data?.clusters ?? [];
  const envs = [...new Set(all.map(c => c.environment).filter(Boolean))].sort() as string[];
  const rows = env === "all" ? all : all.filter(c => c.environment === env);
  if (rows.length === 0) {
    return (
      <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">
        Nenhum cluster analisado ainda. Cada <strong>Analisar</strong> grava o relatório do cluster, e ele passa a aparecer aqui.
      </div>
    );
  }

  const total = rows.reduce((s, r) => s + r.total_brl, 0);
  const waste = rows.reduce((s, r) => s + r.waste_brl, 0);
  const idle = rows.reduce((s, r) => s + (r.idle_brl ?? 0), 0);
  const wasteCommitted = rows.reduce((s, r) => s + r.waste_committed_brl, 0);
  const risks = rows.reduce((s, r) => s + r.oom_risk, 0);
  const stale = rows.filter(r => daysAgo(r.generated_at) > STALE_DAYS).length;
  const groups = group(rows, by);
  const max = Math.max(1, ...groups.map(g => g.total));
  const chart = groups.slice(0, 12).map(g => ({ name: g.label, desperdicio: Math.round(g.waste), ociosa: Math.round(g.idle), restante: Math.round(Math.max(0, g.total - g.waste - g.idle)), total: g.total }));

  return (
    <div className="space-y-4 pb-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          Último relatório gravado de cada cluster, sem novo scan. Custo efetivo nos pools com cobertura de reserva/Savings Plan; isenções já aplicadas.
        </p>
        <div className="flex items-center gap-2">
          <select value={env} onChange={e => setEnv(e.target.value)} className="h-8 rounded border bg-background px-2 text-xs">
            <option value="all">Todos os ambientes</option>
            {envs.map(e => <option key={e} value={e}>{e.toUpperCase()}</option>)}
          </select>
          <div className="flex rounded-md border p-0.5 text-xs">
            {(["cluster", "journey"] as const).map(v => (
              <button key={v} onClick={() => setBy(v)}
                className={`rounded px-2.5 py-1 ${by === v ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}>
                {v === "cluster" ? "Por cluster" : "Por jornada"}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <StatTile icon={Layers} label="Clusters analisados" value={rows.length}
          sub={stale > 0 ? `${stale} com relatório de mais de ${STALE_DAYS} dias` : "todos analisados na última semana"} tone={stale > 0 ? "warn" : "default"} />
        <StatTile icon={Wallet} label="Custo mensal" value={shortBRL(total)} sub={`${shortBRL(total * 12)}/ano no ritmo atual`} />
        <StatTile icon={PiggyBank} label="Request acima do uso" value={shortBRL(waste)}
          sub={wasteCommitted > 0 ? `${shortBRL(waste - wasteCommitted)} sob demanda · ${shortBRL(wasteCommitted)} sob reserva` : `${pct(waste, total)}% do custo`} tone={waste > 0 ? "warn" : "good"} />
        <StatTile icon={Server} label="Capacidade ociosa" value={shortBRL(idle)} sub={`${pct(idle, total)}% do custo · nodes que nem request nem uso ocupam`} tone={idle > total * 0.2 ? "bad" : idle > total * 0.1 ? "warn" : "good"} />
        <StatTile icon={ShieldAlert} label="Risco de OOM" value={risks} sub="workloads com uso encostado no request" tone={risks > 0 ? "bad" : "good"} />
      </div>

      <div className="rounded-xl border bg-card p-5 shadow-sm">
        <SectionTitle icon={Wallet} title={by === "cluster" ? "Custo por cluster" : "Custo por jornada"} sub="Mensal; vermelho = capacidade ociosa, âmbar = request acima do uso" />
        <ResponsiveContainer width="100%" height={Math.max(180, chart.length * 30)}>
          <BarChart data={chart} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
            <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.35} />
            <XAxis type="number" tick={AXIS_TICK} axisLine={false} tickLine={false} tickFormatter={v => (v >= 1000 ? `${Math.round(v / 1000)}k` : String(v))} />
            <YAxis type="category" dataKey="name" tick={AXIS_TICK} width={150} axisLine={false} tickLine={false} />
            <Tooltip cursor={{ fill: "hsl(var(--muted) / 0.4)" }} content={({ active, payload, label }) => active && payload?.length ? (
              <TooltipBox title={label}>
                <TooltipRow label="custo/mês" value={fmtBRL((payload[0].payload as { total: number }).total)} />
                <TooltipRow color={COST_COLORS.idle} label="capacidade ociosa" value={fmtBRL(payload.find(p => p.dataKey === "ociosa")?.value as number)} />
                <TooltipRow color={COST_COLORS.waste} label="request acima do uso" value={fmtBRL(payload.find(p => p.dataKey === "desperdicio")?.value as number)} />
              </TooltipBox>) : null} />
            <Bar dataKey="restante" stackId="a" fill="#6366f1" fillOpacity={0.8} maxBarSize={18} />
            <Bar dataKey="ociosa" stackId="a" fill={COST_COLORS.idle} fillOpacity={0.85} maxBarSize={18} />
            <Bar dataKey="desperdicio" stackId="a" fill={COST_COLORS.waste} radius={[0, 4, 4, 0]} maxBarSize={18} />
          </BarChart>
        </ResponsiveContainer>
      </div>

      <div className="rounded-xl border bg-card shadow-sm overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-[11px] uppercase tracking-wide text-muted-foreground">
              <tr>
                <th className="px-4 py-2 text-left font-medium">{by === "cluster" ? "Cluster" : "Jornada"}</th>
                <th className="px-3 py-2 text-right font-medium">Custo/mês</th>
                <th className="px-3 py-2 text-left font-medium w-48">Participação</th>
                <th className="px-3 py-2 text-right font-medium">Ociosa</th>
                <th className="px-3 py-2 text-right font-medium">Request acima do uso</th>
                <th className="px-3 py-2 text-right font-medium">Risco OOM</th>
                <th className="px-3 py-2 text-right font-medium">Nodes</th>
                <th className="px-3 py-2 text-right font-medium">Relatório</th>
                {by === "cluster" && <th className="px-3 py-2" />}
              </tr>
            </thead>
            <tbody className="divide-y">
              {groups.map(g => {
                const c = g.clusters[0];
                const isCurrent = by === "cluster" && c.cluster === currentCluster;
                return (
                  <tr key={g.key} className={isCurrent ? "bg-primary/5" : "hover:bg-muted/30"}>
                    <td className="px-4 py-2.5">
                      <p className="font-medium">{g.label}{isCurrent && <span className="ml-1.5 text-[10px] text-primary">atual</span>}</p>
                      <p className="text-[11px] text-muted-foreground">{g.sub}</p>
                    </td>
                    <td className="px-3 py-2.5 text-right tabular-nums font-semibold">
                      {fmtBRL(g.total)}
                      {by === "cluster" && c.covered_pools > 0 && (
                        <p className="text-[10px] font-normal text-muted-foreground" title={`Tabela: ${fmtBRL(c.compute_table_brl)} de compute`}>efetivo em {c.covered_pools} de {c.pools} pools</p>
                      )}
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="h-2 rounded-full bg-muted overflow-hidden">
                        <div className="h-full rounded-full bg-gradient-to-r from-indigo-500 to-violet-500" style={{ width: `${(g.total / max) * 100}%` }} />
                      </div>
                      <p className="mt-0.5 text-[10px] text-muted-foreground">{pct(g.total, total)}% do total</p>
                    </td>
                    <td className="px-3 py-2.5 text-right tabular-nums">
                      {g.idle >= 1 ? <span className="text-rose-600 dark:text-rose-400 font-medium">{fmtBRL(g.idle)}</span> : "—"}
                      {g.idle >= 1 && <p className="text-[10px] text-muted-foreground">{pct(g.idle, g.total)}% do custo</p>}
                    </td>
                    <td className="px-3 py-2.5 text-right tabular-nums">
                      {g.waste >= 1 ? <span className="text-amber-600 dark:text-amber-400 font-medium">{fmtBRL(g.waste)}</span> : "—"}
                      {g.waste >= 1 && <p className="text-[10px] text-muted-foreground">{pct(g.waste, g.total)}% do custo{g.wasteCommitted >= 1 ? ` · ${pct(g.wasteCommitted, g.waste)}% sob reserva` : ""}</p>}
                    </td>
                    <td className={`px-3 py-2.5 text-right tabular-nums ${g.risks > 0 ? "text-rose-600 font-medium" : "text-muted-foreground"}`}>{g.risks || "—"}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-muted-foreground">{g.nodes}</td>
                    <td className={`px-3 py-2.5 text-right text-[11px] ${g.oldestDays > STALE_DAYS ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"}`}>
                      <span className="inline-flex items-center gap-1" title={by === "cluster" ? new Date(c.generated_at).toLocaleString("pt-BR") : "relatório mais antigo do grupo"}>
                        <Clock className="h-3 w-3" />{g.oldestDays === 0 ? "hoje" : `há ${g.oldestDays} d`}
                      </span>
                    </td>
                    {by === "cluster" && (
                      <td className="px-3 py-2.5 text-right">
                        <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => onOpen(c.cluster)}>
                          Abrir <ArrowRight className="h-3 w-3" />
                        </Button>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-[11px] text-muted-foreground">
        Ociosa pela mesma conta do Node Pools (recurso mais cheio de cada pool, maior entre request e uso, sobre a capacidade alocável). Relatório com mais de {STALE_DAYS} dias aparece em âmbar: rode Analisar de novo para atualizar.
      </p>
    </div>
  );
}
