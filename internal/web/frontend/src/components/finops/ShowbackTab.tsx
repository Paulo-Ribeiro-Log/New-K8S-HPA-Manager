// Showback do FinOps (Fase 3): custo por namespace ou dono, por mês, exportável em CSV
// (GET /finops/showback). Cada mês é a foto do último scan dele; compute no custo efetivo.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from "recharts";
import { Download, Loader2, Users, Wallet, PiggyBank, Info } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";
import { fmtBRL } from "@/lib/finopsFormat";
import type { ShowbackRow } from "./types";
import { AXIS_TICK, COST_COLORS, SectionTitle, StatTile, TooltipBox, TooltipRow } from "./charts";

const UNALLOCATED = "(não alocado)";
const shortCluster = (c: string) => c.replace(/-admin$/, "").replace(/^aks(priv|pub)?-/, "");
const monthLabel = (m: string) => {
  const t = new Date(`${m}-01T12:00:00`).toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
  return t.charAt(0).toUpperCase() + t.slice(1);
};
const pct = (a: number, b: number) => (b > 0 ? Math.round((a / b) * 100) : 0);

interface Line {
  key: string;
  label: string;
  sub: string;
  workloads: number;
  compute: number;
  storage: number;
  waste: number;
  total: number;
  unallocated: boolean;
}

export function ShowbackTab({ cluster }: { cluster: string }) {
  const [month, setMonth] = useState(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
  });
  const [scope, setScope] = useState<"cluster" | "all">("cluster");
  const [by, setBy] = useState<"namespace" | "owner">("namespace");
  const { data, isLoading, error } = useQuery({
    queryKey: ["finops-showback", month, scope === "all" ? "" : cluster],
    queryFn: () => {
      const params = new URLSearchParams({ month });
      if (scope === "cluster") params.set("cluster", cluster);
      return apiClient.get<{ month: string; months: string[]; rows: ShowbackRow[]; report_at: Record<string, string> }>(`/finops/showback?${params}`);
    },
    staleTime: 60_000,
  });

  const rows = data?.rows ?? [];
  const map = new Map<string, Line>();
  for (const r of rows) {
    const unallocated = r.namespace === UNALLOCATED;
    const ownerKey = unallocated ? UNALLOCATED : r.owner || "(sem dono)";
    const key = by === "namespace"
      ? `${scope === "all" ? r.cluster + "/" : ""}${r.namespace}`
      : `${ownerKey}`;
    const l = map.get(key) ?? {
      key,
      label: by === "namespace" ? r.namespace : ownerKey,
      sub: "",
      workloads: 0, compute: 0, storage: 0, waste: 0, total: 0, unallocated,
    };
    l.workloads += r.workloads;
    l.compute += r.compute_brl;
    l.storage += r.storage_brl;
    l.waste += r.waste_brl;
    l.total += r.total_brl;
    if (by === "namespace") {
      l.sub = [scope === "all" ? shortCluster(r.cluster) : "", r.owner ? `dono: ${r.owner}` : ""].filter(Boolean).join(" · ");
    } else {
      const nss = new Set(l.sub ? l.sub.split(", ") : []);
      if (!unallocated) nss.add(scope === "all" ? `${shortCluster(r.cluster)}/${r.namespace}` : r.namespace);
      l.sub = [...nss].slice(0, 6).join(", ");
    }
    map.set(key, l);
  }
  const lines = [...map.values()].sort((a, b) => (a.unallocated === b.unallocated ? b.total - a.total : a.unallocated ? 1 : -1));
  const total = lines.reduce((s, l) => s + l.total, 0);
  const allocated = lines.filter(l => !l.unallocated).reduce((s, l) => s + l.total, 0);
  const waste = lines.reduce((s, l) => s + l.waste, 0);
  const withOwner = rows.filter(r => r.namespace !== UNALLOCATED && r.owner).length;
  const nsCount = rows.filter(r => r.namespace !== UNALLOCATED).length;
  const chart = lines.filter(l => !l.unallocated).slice(0, 12).map(l => ({ name: l.label.length > 22 ? l.label.slice(0, 21) + "…" : l.label, compute: Math.round(l.compute), storage: Math.round(l.storage), total: l.total }));

  const exportCSV = () => {
    const esc = (v: string | number) => {
      const s = String(v);
      return /[;"\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
    };
    const num = (v: number) => v.toFixed(2).replace(".", ",");
    const header = ["Mês", "Cluster", "Namespace", "Dono", "Label do dono", "Workloads", "Compute (R$)", "Storage (R$)", "Total (R$)", "Request acima do uso (R$)"];
    const body = rows.map(r => [month, r.cluster, r.namespace, r.owner ?? "", r.owner_label ?? "", r.workloads, num(r.compute_brl), num(r.storage_brl), num(r.total_brl), num(r.waste_brl)].map(esc).join(";"));
    const blob = new Blob(["﻿" + [header.join(";"), ...body].join("\n")], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `finops-showback-${scope === "all" ? "todos" : shortCluster(cluster)}-${month}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
    toast.success("CSV exportado");
  };

  return (
    <div className="space-y-4 pb-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          Custo mensal por namespace ou dono (label do namespace: <code>owner</code>, <code>team</code>, <code>squad</code>…). Cada mês é a foto do último Analisar dele; compute no custo efetivo.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <select value={month} onChange={e => setMonth(e.target.value)} className="h-8 rounded border bg-background px-2 text-xs">
            {(data?.months ?? [month]).map(m => <option key={m} value={m}>{monthLabel(m)}</option>)}
          </select>
          <select value={scope} onChange={e => setScope(e.target.value as "cluster" | "all")} className="h-8 rounded border bg-background px-2 text-xs">
            <option value="cluster">Este cluster</option>
            <option value="all">Todos os clusters</option>
          </select>
          <div className="flex rounded-md border p-0.5 text-xs">
            {(["namespace", "owner"] as const).map(v => (
              <button key={v} onClick={() => setBy(v)}
                className={`rounded px-2.5 py-1 ${by === v ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}>
                {v === "namespace" ? "Por namespace" : "Por dono"}
              </button>
            ))}
          </div>
          <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={exportCSV} disabled={rows.length === 0}>
            <Download className="h-3.5 w-3.5" /> CSV
          </Button>
        </div>
      </div>

      {isLoading ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" /> Carregando…</p>
      ) : error ? (
        <p className="text-sm text-rose-600">{(error as Error).message}</p>
      ) : rows.length === 0 ? (
        <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">
          Sem showback para {monthLabel(month)}{scope === "cluster" ? " neste cluster" : ""}. Cada <strong>Analisar</strong> grava a foto do mês.
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile icon={Wallet} label="Custo do mês" value={fmtBRL(total)} sub={scope === "all" ? `${Object.keys(data?.report_at ?? {}).length} cluster(s)` : "cluster inteiro"} />
            <StatTile icon={Users} label="Alocado a namespaces" value={`${pct(allocated, total)}%`} sub={`${fmtBRL(total - allocated)} não alocado (ociosa e sistema)`} />
            <StatTile icon={Users} label="Namespaces com dono" value={`${withOwner} de ${nsCount}`}
              sub={withOwner < nsCount ? "coloque o label owner/team no namespace" : "todos identificados"} tone={withOwner < nsCount ? "warn" : "good"} />
            <StatTile icon={PiggyBank} label="Request acima do uso" value={fmtBRL(waste)} sub={`${pct(waste, allocated)}% do alocado`} tone={waste > 0 ? "warn" : "good"} />
          </div>

          <div className="rounded-xl border bg-card p-5 shadow-sm">
            <SectionTitle icon={Wallet} title={by === "namespace" ? "Maiores namespaces" : "Custo por dono"} sub={monthLabel(month)} />
            <ResponsiveContainer width="100%" height={Math.max(180, chart.length * 28)}>
              <BarChart data={chart} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.35} />
                <XAxis type="number" tick={AXIS_TICK} axisLine={false} tickLine={false} tickFormatter={v => (v >= 1000 ? `${Math.round(v / 1000)}k` : String(v))} />
                <YAxis type="category" dataKey="name" tick={AXIS_TICK} width={160} axisLine={false} tickLine={false} />
                <Tooltip cursor={{ fill: "hsl(var(--muted) / 0.4)" }} content={({ active, payload, label }) => active && payload?.length ? (
                  <TooltipBox title={label}>
                    <TooltipRow color="#6366f1" label="compute" value={fmtBRL(payload[0].payload.compute)} />
                    <TooltipRow color={COST_COLORS.pvc} label="storage" value={fmtBRL(payload[0].payload.storage)} />
                    <TooltipRow label="total" value={fmtBRL(payload[0].payload.total)} />
                  </TooltipBox>) : null} />
                <Bar dataKey="compute" stackId="a" fill="#6366f1" fillOpacity={0.85} maxBarSize={16} />
                <Bar dataKey="storage" stackId="a" fill={COST_COLORS.pvc} radius={[0, 4, 4, 0]} maxBarSize={16} />
              </BarChart>
            </ResponsiveContainer>
          </div>

          <div className="overflow-x-auto rounded-xl border bg-card shadow-sm">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-[11px] uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 text-left font-medium">{by === "namespace" ? "Namespace" : "Dono"}</th>
                  <th className="px-3 py-2 text-right font-medium">Workloads</th>
                  <th className="px-3 py-2 text-right font-medium">Compute</th>
                  <th className="px-3 py-2 text-right font-medium">Storage</th>
                  <th className="px-3 py-2 text-right font-medium">Total/mês</th>
                  <th className="px-3 py-2 text-left font-medium w-40">Participação</th>
                  <th className="px-3 py-2 text-right font-medium">Request acima do uso</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {lines.map(l => (
                  <tr key={l.key} className={l.unallocated ? "bg-muted/30 text-muted-foreground" : "hover:bg-muted/30"}>
                    <td className="px-4 py-2">
                      <p className={l.unallocated ? "italic" : "font-medium"}>{l.label}</p>
                      {l.unallocated
                        ? <p className="text-[11px] flex items-center gap-1"><Info className="h-3 w-3" /> capacidade dos nodes que nenhum workload ocupa e disco do SO</p>
                        : l.sub && <p className="text-[11px] text-muted-foreground truncate max-w-md">{l.sub}</p>}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{l.unallocated ? "—" : l.workloads}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{fmtBRL(l.compute)}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{l.storage >= 1 ? fmtBRL(l.storage) : "—"}</td>
                    <td className="px-3 py-2 text-right tabular-nums font-semibold">{fmtBRL(l.total)}</td>
                    <td className="px-3 py-2">
                      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
                        <div className={`h-full rounded-full ${l.unallocated ? "bg-slate-400" : "bg-gradient-to-r from-indigo-500 to-violet-500"}`} style={{ width: `${pct(l.total, total)}%` }} />
                      </div>
                      <p className="mt-0.5 text-[10px] text-muted-foreground">{pct(l.total, total)}%</p>
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{l.waste >= 1 ? <span className="text-amber-600 dark:text-amber-400">{fmtBRL(l.waste)}</span> : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
