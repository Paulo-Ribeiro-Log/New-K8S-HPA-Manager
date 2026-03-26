import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell,
  PieChart, Pie, Legend, ComposedChart, Line, Area,
  LabelList, ReferenceLine,
} from "recharts";
import {
  DollarSign, TrendingDown, AlertTriangle, CheckCircle2,
  Loader2, RefreshCw, Server, Layers, CircleDollarSign,
  ArrowUpDown, Info, ChevronDown, ChevronUp, Download, Brain, Activity, Cpu, MemoryStick,
} from "lucide-react";
import { toast } from "sonner";
import { useClusters } from "@/hooks/useAPI";

// ─── Tipos ────────────────────────────────────────────────────────────────────

interface FinOpsPool {
  name: string;
  vm_size: string;
  vm_cpu_cores: number;
  vm_memory_gb: number;
  vm_price_usd_hour: number;
  price_source: string;
  node_count: number;
  mode: string;
  monthly_cost_usd: number;
  monthly_cost_brl: number;
  total_cpu_millicores: number;
  total_memory_mi: number;
}

interface FinOpsNamespace {
  namespace: string;
  workload_count: number;
  monthly_cost_usd: number;
  monthly_cost_brl: number;
}

interface FinOpsWorkload {
  namespace: string;
  workload: string;
  pods: number;
  cpu_request_millis: number;
  mem_request_mi: number;
  cpu_limit_millis?: number;
  mem_limit_mi?: number;
  cost_share_usd: number;
  cost_share_brl: number;
  hpa_min: number;
  hpa_max: number;
  hpa_current: number;
  hpa_cost_min_brl: number;
  hpa_cost_max_brl: number;
  hpa_cost_current_brl: number;
  verdict: "superprovisioned" | "ok" | "oom_risk" | "no_request" | "hpa_removable";
  // Prometheus — uso real CPU/Mem (últimos N dias)
  cpu_avg_millis?: number;
  cpu_p95_millis?: number;
  cpu_recommended_millis?: number;
  mem_avg_mi?: number;
  mem_p95_mi?: number;
  mem_recommended_mi?: number;
  // Prometheus — histórico HPA
  hpa_avg_replicas?: number;
  hpa_max_observed?: number;
  hpa_min_observed?: number;
  hpa_scale_events?: number;
  hpa_never_scaled?: boolean;
  avg_replicas_cost_brl?: number;
  waste_brl?: number;
}

interface FinOpsSummary {
  total_monthly_cost_brl: number;
  total_monthly_cost_usd: number;
  top_namespace: string;
  potential_savings_brl: number;
  hpa_savings_if_min_brl: number;
  workloads_analyzed: number;
  superprovisioned_count: number;
  oom_risk_count: number;
  no_request_count: number;
  hpa_removable_count: number;
}

interface FinOpsReport {
  cluster: string;
  generated_at: string;
  exchange_rate: number;
  exchange_date: string;
  window_days: number;
  node_pools: FinOpsPool[];
  namespaces: FinOpsNamespace[];
  workloads: FinOpsWorkload[];
  summary: FinOpsSummary;
}

// ─── Tipos: Timeline ──────────────────────────────────────────────────────────

interface HPADayPoint {
  date: string;         // "2026-02-24"
  max_replicas: number;
  avg_replicas: number;
  min_replicas: number;
}

interface HPATimeline {
  namespace: string;
  workload: string;
  hpa_min: number;
  hpa_max: number;
  series: HPADayPoint[];
}

interface CPUDayPoint {
  date: string;
  used_millis: number;
  req_millis: number;
}

interface MemDayPoint {
  date: string;
  used_mi: number;
  req_mi: number;
}

interface TimelineReport {
  cluster: string;
  start_date: string;
  end_date: string;
  hpas: HPATimeline[];
  nodes: { date: string; node_count: number }[];
  cpu: CPUDayPoint[];
  mem: MemDayPoint[];
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  v.toLocaleString("pt-BR", { style: "currency", currency: "BRL", maximumFractionDigits: 0 });

const fmtUSD = (v: number) =>
  v.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

const verdictConfig: Record<string, { label: string; color: string; fill: string; icon: typeof CheckCircle2 }> = {
  superprovisioned: { label: "Desperdício",    fill: "#ef4444", color: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",       icon: TrendingDown },
  oom_risk:         { label: "Risco OOM",      fill: "#f59e0b", color: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400", icon: AlertTriangle },
  ok:               { label: "Eficiente",      fill: "#10b981", color: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",   icon: CheckCircle2 },
  no_request:       { label: "Sem Request",    fill: "#9ca3af", color: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",       icon: Info },
  hpa_removable:    { label: "Remover HPA",    fill: "#8b5cf6", color: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400", icon: Info },
};

const POOL_COLORS = ["#6366f1", "#8b5cf6", "#06b6d4", "#10b981", "#f59e0b", "#ef4444", "#ec4899"];

// ─── Componentes Auxiliares ───────────────────────────────────────────────────

function SummaryCard({ icon: Icon, label, value, sub, color }: {
  icon: typeof DollarSign; label: string; value: string; sub?: string; color: string;
}) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-start justify-between gap-2">
          <div>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className={`text-xl font-bold ${color}`}>{value}</p>
            {sub && <p className="text-xs text-muted-foreground mt-0.5">{sub}</p>}
          </div>
          <Icon className={`h-5 w-5 mt-0.5 ${color}`} />
        </div>
      </CardContent>
    </Card>
  );
}

function VerdictBadge({ verdict }: { verdict: FinOpsWorkload["verdict"] }) {
  const cfg = verdictConfig[verdict] ?? verdictConfig.ok;
  const Icon = cfg.icon;
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium ${cfg.color}`}>
      <Icon className="h-3 w-3" />
      {cfg.label}
    </span>
  );
}

// ─── Helpers de charts ────────────────────────────────────────────────────────

function ChartTooltip({ active, payload, label, formatter }: {
  active?: boolean; payload?: { name: string; value: number; color: string }[]; label?: string;
  formatter?: (v: number, name: string) => string;
}) {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs">
      {label && <p className="font-medium text-foreground mb-1">{label}</p>}
      {payload.map((p, i) => (
        <p key={i} style={{ color: p.color }} className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full inline-block flex-shrink-0" style={{ background: p.color }} />
          {formatter ? formatter(p.value, p.name) : `${p.name}: ${fmtBRL(p.value)}`}
        </p>
      ))}
    </div>
  );
}

const fmtMi = (v: number) => v >= 1024 ? `${(v / 1024).toFixed(1)}Gi` : `${Math.round(v)}Mi`;
const fmtM  = (v: number) => v >= 1000 ? `${(v / 1000).toFixed(1)}` : `${Math.round(v)}`;

const LINE_COLORS = ["#6366f1","#10b981","#f59e0b","#ef4444","#8b5cf6","#06b6d4","#ec4899","#84cc16","#f97316","#14b8a6"];

// ─── Aba: Dashboard ───────────────────────────────────────────────────────────

function DashboardTab({ cluster, report }: { cluster: string; report: FinOpsReport }) {
  const { summary, namespaces, node_pools, workloads } = report;
  const [days, setDays] = useState(30);

  // ── Fetch timeline ──────────────────────────────────────────────────────────
  const { data: tl, isLoading: tlLoading } = useQuery<TimelineReport>({
    queryKey: ["finops-timeline-dashboard", cluster, days],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=${days}`,
        { headers: { Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}` } }
      );
      if (!r.ok) throw new Error(`Timeline: ${r.status}`);
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  // ── Custo diário estimado ────────────────────────────────────────────────────
  const totalNodes = node_pools.reduce((s, p) => s + p.node_count, 0);
  const costPerNodePerDay = totalNodes > 0
    ? summary.total_monthly_cost_brl / 30 / totalNodes
    : 0;

  const dailyCostData = (tl?.nodes ?? []).map(n => ({
    date: n.date.slice(5),
    custo: parseFloat((n.node_count * costPerNodePerDay).toFixed(0)),
    nodes: n.node_count,
  }));

  // ── CPU série temporal ───────────────────────────────────────────────────────
  const cpuData = (tl?.cpu ?? []).map(p => ({
    date: p.date.slice(5),
    Uso: Math.round(p.used_millis),
    Request: Math.round(p.req_millis),
  }));

  // ── Mem série temporal ───────────────────────────────────────────────────────
  const memData = (tl?.mem ?? []).map(p => ({
    date: p.date.slice(5),
    Uso: Math.round(p.used_mi),
    Request: Math.round(p.req_mi),
  }));

  // ── HPA top-8 (por max replicas) — multi-line ────────────────────────────────
  const top8HPAs = (tl?.hpas ?? [])
    .map(h => ({ ...h, maxObs: Math.max(...h.series.map(p => p.max_replicas), 0) }))
    .sort((a, b) => b.maxObs - a.maxObs)
    .slice(0, 8);

  const hpaAllDates = [...new Set((tl?.hpas ?? []).flatMap(h => h.series.map(p => p.date)))].sort();
  const hpaChartData = hpaAllDates.map(date => {
    const entry: Record<string, number | string> = { date: date.slice(5) };
    top8HPAs.forEach((h, i) => {
      const pt = h.series.find(p => p.date === date);
      entry[`h${i}`] = pt ? Math.round(pt.avg_replicas) : 0;
    });
    return entry;
  });

  // ── Nodes série temporal ─────────────────────────────────────────────────────
  const nodesData = (tl?.nodes ?? []).map(n => ({
    date: n.date.slice(5),
    Nodes: n.node_count,
  }));

  // ── Charts auxiliares (snapshot do report) ───────────────────────────────────
  const topNs = namespaces.slice(0, 8).map(ns => ({
    name: ns.namespace.length > 22 ? ns.namespace.slice(0, 20) + "…" : ns.namespace,
    value: ns.monthly_cost_brl,
    pct: summary.total_monthly_cost_brl > 0
      ? Math.round((ns.monthly_cost_brl / summary.total_monthly_cost_brl) * 100) : 0,
  }));

  const okCount = workloads.filter(w => w.verdict === "ok").length;
  const verdictDist = [
    { name: "Eficiente",   value: okCount,                       fill: "#10b981" },
    { name: "Desperdício", value: summary.superprovisioned_count, fill: "#ef4444" },
    { name: "Risco OOM",   value: summary.oom_risk_count,         fill: "#f59e0b" },
    { name: "Sem Request", value: summary.no_request_count,       fill: "#9ca3af" },
  ].filter(d => d.value > 0);

  const poolChart = node_pools.map((p, i) => ({
    name: p.name.length > 14 ? p.name.slice(0, 12) + "…" : p.name,
    custo: p.monthly_cost_brl,
    nos: p.node_count,
    color: POOL_COLORS[i % POOL_COLORS.length],
  }));

  // ── helpers ──────────────────────────────────────────────────────────────────
  const xTick = { fontSize: 10, fill: "#9ca3af" };
  const yTick = { fontSize: 10 };
  const gridProps = { strokeDasharray: "3 3", vertical: false, opacity: 0.35 };
  const tlEmpty = !tlLoading && (!tl || (tl.nodes.length === 0 && tl.cpu.length === 0));

  return (
    <div className="space-y-4">
      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <SummaryCard icon={CircleDollarSign} label="Custo Total/mês" color="text-blue-600"
          value={fmtBRL(summary.total_monthly_cost_brl)} sub={fmtUSD(summary.total_monthly_cost_usd)} />
        {summary.potential_savings_brl > 0 ? (
          <SummaryCard icon={Activity} label="Desperdício Real (P95)" color="text-red-600"
            value={fmtBRL(summary.potential_savings_brl)} sub="baseado em uso P95 Prometheus" />
        ) : (
          <SummaryCard icon={TrendingDown} label="Economia HPA (se mín)" color="text-green-600"
            value={fmtBRL(summary.hpa_savings_if_min_brl)} sub={`${summary.superprovisioned_count} workloads`} />
        )}
        <SummaryCard icon={Layers} label="Workloads Analisados" color="text-purple-600"
          value={String(summary.workloads_analyzed)} sub={`${summary.no_request_count} sem request`} />
        <SummaryCard icon={DollarSign} label="Cotação USD/BRL" color="text-orange-600"
          value={`R$ ${report.exchange_rate.toFixed(4)}`} sub={report.exchange_date} />
      </div>

      {/* Seletor de janela temporal */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">Série temporal:</span>
        {([7, 15, 30] as const).map(d => (
          <Button key={d} variant={days === d ? "default" : "outline"} size="sm"
            className="h-7 text-xs px-3" onClick={() => setDays(d)}>
            {d}d
          </Button>
        ))}
        {tlLoading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground ml-1" />}
        {tl && !tlLoading && (
          <span className="text-[10px] text-muted-foreground ml-1">
            {tl.start_date} → {tl.end_date} · {tl.hpas.length} HPAs · {tl.nodes.length} dias
          </span>
        )}
        {tlEmpty && (
          <span className="text-[10px] text-yellow-600 ml-1">Prometheus sem dados (URL auto-descoberta)</span>
        )}
      </div>

      {/* ── Chart 1: Custo Estimado Diário ─────────────────────────────────── */}
      {dailyCostData.length > 0 && (
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <CircleDollarSign className="h-4 w-4 text-amber-500" />
              Custo Estimado Diário (R$)
              <span className="text-[10px] font-normal text-muted-foreground ml-1">
                baseado em contagem de nodes × custo médio por node
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={160}>
              <ComposedChart data={dailyCostData} margin={{ left: 8, right: 8, top: 4, bottom: 0 }}>
                <CartesianGrid {...gridProps} />
                <XAxis dataKey="date" tick={xTick} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                <YAxis yAxisId="cost" tickFormatter={v => `R$${(v).toFixed(0)}`} tick={yTick}
                  axisLine={false} tickLine={false} width={64} />
                <YAxis yAxisId="nodes" orientation="right" tick={xTick} axisLine={false} tickLine={false} width={28} />
                <Tooltip content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  return (
                    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                      <p className="font-medium">{label}</p>
                      <p className="text-amber-500">Custo: {fmtBRL(payload[0]?.value as number)}</p>
                      <p className="text-cyan-500">Nodes: {payload[1]?.value}</p>
                    </div>
                  );
                }} />
                <Bar yAxisId="cost" dataKey="custo" name="Custo R$" fill="#f59e0b" fillOpacity={0.75}
                  radius={[2, 2, 0, 0]} maxBarSize={18} />
                <Line yAxisId="nodes" dataKey="nodes" name="Nodes" type="monotone"
                  stroke="#06b6d4" strokeWidth={2} dot={false} />
              </ComposedChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      {/* ── Charts 2+3: CPU e Memória ────────────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* CPU */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Cpu className="h-4 w-4 text-indigo-500" />
              CPU — Uso vs Request (cluster)
              <span className="text-[10px] font-normal text-muted-foreground">millicores</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {cpuData.length === 0 ? (
              <div className="flex items-center justify-center h-[160px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem dados Prometheus"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={160}>
                <ComposedChart data={cpuData} margin={{ left: 8, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid {...gridProps} />
                  <XAxis dataKey="date" tick={xTick} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tickFormatter={v => `${fmtM(v)}m`} tick={yTick} axisLine={false} tickLine={false} width={52} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium">{label}</p>
                        {payload.map((p, i) => (
                          <p key={i} style={{ color: p.color }}>{p.name}: {Math.round(p.value as number)}m</p>
                        ))}
                      </div>
                    );
                  }} />
                  <Area type="monotone" dataKey="Uso" fill="#6366f1" stroke="#6366f1"
                    fillOpacity={0.25} strokeWidth={1.5} dot={false} />
                  <Line type="monotone" dataKey="Request" stroke="#a5b4fc"
                    strokeWidth={1.5} strokeDasharray="4 2" dot={false} />
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        {/* Memória */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <MemoryStick className="h-4 w-4 text-emerald-500" />
              Memória — Uso vs Request (cluster)
              <span className="text-[10px] font-normal text-muted-foreground">MiB / GiB</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {memData.length === 0 ? (
              <div className="flex items-center justify-center h-[160px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem dados Prometheus"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={160}>
                <ComposedChart data={memData} margin={{ left: 8, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid {...gridProps} />
                  <XAxis dataKey="date" tick={xTick} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tickFormatter={fmtMi} tick={yTick} axisLine={false} tickLine={false} width={56} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium">{label}</p>
                        {payload.map((p, i) => (
                          <p key={i} style={{ color: p.color }}>{p.name}: {fmtMi(p.value as number)}</p>
                        ))}
                      </div>
                    );
                  }} />
                  <Area type="monotone" dataKey="Uso" fill="#10b981" stroke="#10b981"
                    fillOpacity={0.25} strokeWidth={1.5} dot={false} />
                  <Line type="monotone" dataKey="Request" stroke="#6ee7b7"
                    strokeWidth={1.5} strokeDasharray="4 2" dot={false} />
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      {/* ── Charts 4+5: HPA Réplicas e Nodes ────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* HPA Réplicas — top 8 */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Activity className="h-4 w-4 text-violet-500" />
              HPA Réplicas — top {top8HPAs.length}
              <span className="text-[10px] font-normal text-muted-foreground">(avg diário)</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {hpaChartData.length === 0 ? (
              <div className="flex items-center justify-center h-[180px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem HPAs com histórico"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={180}>
                <ComposedChart data={hpaChartData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid {...gridProps} />
                  <XAxis dataKey="date" tick={xTick} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={yTick} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium mb-1">{label}</p>
                        {payload.filter(p => (p.value as number) > 0).map((p, i) => (
                          <p key={i} style={{ color: p.color }}>{p.name}: {p.value} répl.</p>
                        ))}
                      </div>
                    );
                  }} />
                  <Legend iconType="plainline" iconSize={12} wrapperStyle={{ fontSize: 9, paddingTop: 4 }}
                    formatter={(_v, entry) => {
                      const idx = parseInt((entry.dataKey as string).replace("h", ""));
                      return top8HPAs[idx]?.workload ?? entry.dataKey;
                    }} />
                  {top8HPAs.map((_, i) => (
                    <Line key={i} type="monotone" dataKey={`h${i}`}
                      stroke={LINE_COLORS[i % LINE_COLORS.length]}
                      strokeWidth={1.5} dot={false} connectNulls />
                  ))}
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        {/* Nodes */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Server className="h-4 w-4 text-cyan-500" />
              Nodes Ready
              <span className="text-[10px] font-normal text-muted-foreground">(máximo diário)</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {nodesData.length === 0 ? (
              <div className="flex items-center justify-center h-[180px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Server className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem dados Prometheus"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={180}>
                <ComposedChart data={nodesData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid {...gridProps} />
                  <XAxis dataKey="date" tick={xTick} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={yTick} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    const nodes = payload[0]?.value as number;
                    const dayCost = nodes * costPerNodePerDay;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium">{label}</p>
                        <p className="text-cyan-500">{nodes} nodes</p>
                        <p className="text-amber-500">≈ {fmtBRL(dayCost)}/dia</p>
                      </div>
                    );
                  }} />
                  <Area type="stepAfter" dataKey="Nodes" fill="#06b6d4" stroke="#06b6d4"
                    fillOpacity={0.2} strokeWidth={2} dot={false} />
                  <ReferenceLine y={totalNodes} stroke="#f59e0b" strokeDasharray="4 2"
                    strokeOpacity={0.6} label={{ value: "atual", position: "right", fontSize: 9, fill: "#f59e0b" }} />
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      {/* ── Distribuição financeira (snapshot) ──────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Saúde dos Workloads</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={200}>
              <PieChart>
                <Pie data={verdictDist} cx="42%" cy="50%"
                  innerRadius={56} outerRadius={80} paddingAngle={3} dataKey="value" strokeWidth={0}>
                  {verdictDist.map((e, i) => <Cell key={i} fill={e.fill} />)}
                </Pie>
                <Tooltip content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const d = payload[0].payload;
                  return (
                    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs">
                      <p className="font-medium" style={{ color: d.fill }}>{d.name}</p>
                      <p>{d.value} workloads ({Math.round((d.value / summary.workloads_analyzed) * 100)}%)</p>
                    </div>
                  );
                }} />
                <Legend layout="vertical" align="right" verticalAlign="middle" iconType="circle" iconSize={8}
                  formatter={(value, entry) => (
                    <span className="text-[11px] text-foreground">
                      {value} <span className="text-muted-foreground">
                        ({(entry as { payload?: { value?: number } }).payload?.value ?? 0})
                      </span>
                    </span>
                  )} />
                <text x="42%" y="46%" textAnchor="middle" dominantBaseline="middle"
                  className="fill-foreground" style={{ fontSize: 20, fontWeight: 700 }}>
                  {summary.workloads_analyzed}
                </text>
                <text x="42%" y="58%" textAnchor="middle" dominantBaseline="middle"
                  style={{ fontSize: 10, fill: "#9ca3af" }}>workloads</text>
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Top Namespaces por Custo</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={topNs} layout="vertical" margin={{ left: 6, right: 52, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.35} />
                <XAxis type="number" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={xTick} axisLine={false} tickLine={false} />
                <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={120}
                  axisLine={false} tickLine={false} />
                <Tooltip content={<ChartTooltip formatter={(v) => fmtBRL(v)} />} />
                <Bar dataKey="value" name="Custo/mês" radius={[0, 4, 4, 0]} maxBarSize={18}>
                  {topNs.map((_, i) => <Cell key={i} fill={POOL_COLORS[i % POOL_COLORS.length]} fillOpacity={0.85} />)}
                  <LabelList dataKey="pct" position="right" formatter={(v: number) => `${v}%`}
                    style={{ fontSize: 10, fill: "#9ca3af" }} />
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Custo por Node Pool */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Custo por Node Pool</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={poolChart} margin={{ left: 4, right: 12, top: 4, bottom: 20 }}>
                <CartesianGrid {...gridProps} />
                <XAxis dataKey="name" tick={{ fontSize: 10 }} angle={-25} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={yTick} axisLine={false} tickLine={false} />
                <Tooltip content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  const pool = node_pools.find(p => p.name.startsWith(label?.slice(0, 8) ?? ""));
                  return (
                    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                      <p className="font-medium">{label}</p>
                      <p style={{ color: payload[0].color }}>{fmtBRL(payload[0].value as number)}/mês</p>
                      {pool && <p className="text-muted-foreground">{pool.node_count} nodes · {pool.vm_size}</p>}
                    </div>
                  );
                }} />
                <Bar dataKey="custo" name="Custo/mês" radius={[4, 4, 0, 0]} maxBarSize={40}>
                  {poolChart.map((e, i) => <Cell key={i} fill={e.color} />)}
                  <LabelList dataKey="nos" position="top" formatter={(v: number) => `${v}n`}
                    style={{ fontSize: 10, fill: "#9ca3af" }} />
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Top 10 Workloads — custo + HPA util */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center justify-between">
              Top 10 Workloads por Custo
              <span className="text-[10px] text-muted-foreground font-normal">linha = % utiliz. HPA</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={200}>
              <ComposedChart
                data={workloads.slice(0, 10).map(w => ({
                  name: w.workload.length > 16 ? w.workload.slice(0, 14) + "…" : w.workload,
                  custo: w.cost_share_brl,
                  hpa_util: w.hpa_max > 0 ? Math.round((w.hpa_current / w.hpa_max) * 100) : null,
                  fill: verdictConfig[w.verdict]?.fill ?? "#6366f1",
                }))}
                margin={{ left: 4, right: 36, top: 4, bottom: 28 }}>
                <CartesianGrid {...gridProps} />
                <XAxis dataKey="name" tick={{ fontSize: 9 }} angle={-35} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis yAxisId="left" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={yTick} axisLine={false} tickLine={false} />
                <YAxis yAxisId="right" orientation="right" unit="%" domain={[0, 100]}
                  tick={xTick} axisLine={false} tickLine={false} width={28} />
                <Tooltip content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  return (
                    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs">
                      <p className="font-medium mb-1">{label}</p>
                      {payload.map((p, i) => (
                        <p key={i} style={{ color: p.color }}>
                          {p.name === "custo" ? `Custo: ${fmtBRL(p.value as number)}` : `HPA util: ${p.value}%`}
                        </p>
                      ))}
                    </div>
                  );
                }} />
                <Bar yAxisId="left" dataKey="custo" name="custo" radius={[3, 3, 0, 0]} maxBarSize={24}>
                  {workloads.slice(0, 10).map((w, i) => (
                    <Cell key={i} fill={verdictConfig[w.verdict]?.fill ?? "#6366f1"} fillOpacity={0.8} />
                  ))}
                </Bar>
                <Line yAxisId="right" dataKey="hpa_util" name="hpa_util" type="monotone"
                  stroke="#f59e0b" strokeWidth={2} dot={{ r: 3, fill: "#f59e0b" }} connectNulls={false} />
                <ReferenceLine yAxisId="right" y={35} stroke="#ef4444" strokeDasharray="4 2" strokeOpacity={0.5} />
              </ComposedChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Alerta de economia */}
      {summary.superprovisioned_count > 0 && (
        <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <TrendingDown className="h-4 w-4 text-red-600" />
          <AlertDescription className="text-sm">
            <strong>{summary.superprovisioned_count} workloads</strong> com HPA superprovisionado.
            Economia estimada ajustando para mínimo:{" "}
            <strong className="text-red-700 dark:text-red-400">{fmtBRL(summary.hpa_savings_if_min_brl)}/mês</strong>
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

// ─── Aba: Node Pools ──────────────────────────────────────────────────────────

function NodePoolsTab({ pools }: { pools: FinOpsPool[] }) {
  const total = pools.reduce((s, p) => s + p.monthly_cost_brl, 0);
  const totalCPU = pools.reduce((s, p) => s + p.total_cpu_millicores, 0);
  const totalMem = pools.reduce((s, p) => s + p.total_memory_mi, 0);

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
                content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  return (
                    <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                      <p className="font-medium">{label}</p>
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
              <TableHead className="text-right">R$/mês</TableHead>
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
    </div>
  );
}

// ─── Aba: Workloads ───────────────────────────────────────────────────────────

type SortKey = "cost_share_brl" | "cpu_request_millis" | "mem_request_mi" | "hpa_cost_min_brl" | "waste_brl";

function WorkloadsTab({ workloads, windowDays }: { workloads: FinOpsWorkload[]; windowDays: number }) {
  const [sortKey, setSortKey] = useState<SortKey>("cost_share_brl");
  const [sortAsc, setSortAsc] = useState(false);
  const [filterVerdict, setFilterVerdict] = useState<string>("all");

  const hasPrometheus = workloads.some(w => (w.cpu_p95_millis ?? 0) > 0 || (w.mem_p95_mi ?? 0) > 0);

  const filtered = workloads
    .filter(w => filterVerdict === "all" || w.verdict === filterVerdict)
    .sort((a, b) => {
      const va = sortKey === "waste_brl" ? (a.waste_brl ?? 0) : a[sortKey as keyof FinOpsWorkload] as number;
      const vb = sortKey === "waste_brl" ? (b.waste_brl ?? 0) : b[sortKey as keyof FinOpsWorkload] as number;
      return sortAsc ? va - vb : vb - va;
    });

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) setSortAsc(!sortAsc);
    else { setSortKey(key); setSortAsc(false); }
  };

  const SortIcon = ({ k }: { k: SortKey }) =>
    sortKey === k
      ? (sortAsc ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />)
      : <ArrowUpDown className="h-3 w-3 opacity-30" />;

  const filterButtons = ["all", "superprovisioned", "oom_risk", "hpa_removable", "ok", "no_request"];

  // Mini chart: custo por namespace (top 6)
  const nsCostMap = workloads.reduce<Record<string, number>>((acc, w) => {
    acc[w.namespace] = (acc[w.namespace] ?? 0) + w.cost_share_brl;
    return acc;
  }, {});
  const nsChart = Object.entries(nsCostMap)
    .sort((a, b) => b[1] - a[1]).slice(0, 6)
    .map(([ns, v], i) => ({ name: ns.length > 18 ? ns.slice(0, 16) + "…" : ns, value: v, color: POOL_COLORS[i % POOL_COLORS.length] }));

  return (
    <div className="space-y-3">
      {/* Mini chart custo por namespace */}
      <Card>
        <CardContent className="p-3">
          <ResponsiveContainer width="100%" height={90}>
            <BarChart data={nsChart} layout="vertical" margin={{ left: 4, right: 48, top: 0, bottom: 0 }}>
              <XAxis type="number" hide />
              <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={120} axisLine={false} tickLine={false} />
              <Tooltip formatter={(v: number) => [fmtBRL(v), "Custo/mês"]} />
              <Bar dataKey="value" radius={[0, 4, 4, 0]} maxBarSize={14}>
                {nsChart.map((e, i) => <Cell key={i} fill={e.color} />)}
                <LabelList dataKey="value" position="right"
                  formatter={(v: number) => `R$${(v / 1000).toFixed(0)}k`}
                  style={{ fontSize: 9, fill: "#9ca3af" }} />
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </CardContent>
      </Card>

      <div className="flex items-center gap-3 flex-wrap">
        <span className="text-sm text-muted-foreground">{filtered.length} workloads</span>
        <div className="flex gap-1 flex-wrap">
          {filterButtons.map(v => (
            <Button key={v} size="sm" variant={filterVerdict === v ? "default" : "outline"}
              className="h-6 text-[10px] px-2"
              onClick={() => setFilterVerdict(v)}>
              {v === "all" ? "Todos" : verdictConfig[v]?.label ?? v}
            </Button>
          ))}
        </div>
        {hasPrometheus && (
          <span className="flex items-center gap-1 text-[10px] text-blue-500">
            <Activity className="h-3 w-3" />
            Análise {windowDays}d Prometheus
          </span>
        )}
      </div>

      <Card>
        <ScrollArea className="h-[420px]">
          <Table>
            <TableHeader>
              <TableRow className="text-[11px]">
                <TableHead>Namespace / Workload</TableHead>
                <TableHead className="text-center">Pods</TableHead>
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("cpu_request_millis")}>
                  <span className="flex items-center justify-end gap-1">CPU Req <SortIcon k="cpu_request_millis" /></span>
                </TableHead>
                {hasPrometheus && (
                  <TableHead className="text-right text-blue-500">
                    <span className="flex flex-col items-end leading-tight">
                      <span>avg / P95</span>
                      <span className="text-[9px] text-muted-foreground">→ recomendado</span>
                    </span>
                  </TableHead>
                )}
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("mem_request_mi")}>
                  <span className="flex items-center justify-end gap-1">Mem Req <SortIcon k="mem_request_mi" /></span>
                </TableHead>
                {hasPrometheus && (
                  <TableHead className="text-right text-blue-500">
                    <span className="flex flex-col items-end leading-tight">
                      <span>avg / P95</span>
                      <span className="text-[9px] text-muted-foreground">→ recomendado</span>
                    </span>
                  </TableHead>
                )}
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("cost_share_brl")}>
                  <span className="flex items-center justify-end gap-1">R$/mês <SortIcon k="cost_share_brl" /></span>
                </TableHead>
                {hasPrometheus && (
                  <TableHead className="text-right cursor-pointer select-none text-red-500" onClick={() => toggleSort("waste_brl")}>
                    <span className="flex items-center justify-end gap-1">Desperdício <SortIcon k="waste_brl" /></span>
                  </TableHead>
                )}
                <TableHead className="text-center">HPA m/a/M</TableHead>
                <TableHead>Veredicto</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((w, i) => (
                <TableRow key={i} className="text-xs">
                  <TableCell className="max-w-[180px]">
                    <p className="text-muted-foreground text-[10px] truncate">{w.namespace}</p>
                    <p className="font-medium truncate">{w.workload}</p>
                  </TableCell>
                  <TableCell className="text-center">{w.pods}</TableCell>
                  <TableCell className="text-right font-mono">
                    {w.cpu_request_millis > 0 ? `${Math.round(w.cpu_request_millis)}m` : "—"}
                  </TableCell>
                  {hasPrometheus && (
                    <TableCell className="text-right font-mono text-[11px]">
                      {(w.cpu_p95_millis ?? 0) > 0 ? (
                        <span className="flex flex-col items-end leading-tight">
                          <span className="text-muted-foreground">{Math.round(w.cpu_avg_millis ?? 0)}m / {Math.round(w.cpu_p95_millis!)}m</span>
                          <span className="text-blue-500 font-semibold">→ {Math.round(w.cpu_recommended_millis ?? 0)}m</span>
                        </span>
                      ) : "—"}
                    </TableCell>
                  )}
                  <TableCell className="text-right font-mono">
                    {w.mem_request_mi > 0 ? `${Math.round(w.mem_request_mi)}Mi` : "—"}
                  </TableCell>
                  {hasPrometheus && (
                    <TableCell className="text-right font-mono text-[11px]">
                      {(w.mem_p95_mi ?? 0) > 0 ? (
                        <span className="flex flex-col items-end leading-tight">
                          <span className="text-muted-foreground">{Math.round(w.mem_avg_mi ?? 0)}Mi / {Math.round(w.mem_p95_mi!)}Mi</span>
                          <span className="text-blue-500 font-semibold">→ {Math.round(w.mem_recommended_mi ?? 0)}Mi</span>
                        </span>
                      ) : "—"}
                    </TableCell>
                  )}
                  <TableCell className="text-right font-semibold">{fmtBRL(w.cost_share_brl)}</TableCell>
                  {hasPrometheus && (
                    <TableCell className="text-right font-semibold">
                      {(w.waste_brl ?? 0) > 0
                        ? <span className="text-red-500">{fmtBRL(w.waste_brl!)}</span>
                        : <span className="text-muted-foreground">—</span>}
                    </TableCell>
                  )}
                  <TableCell className="text-center font-mono text-[11px]">
                    {w.hpa_max > 0
                      ? <span>{w.hpa_min}/{w.hpa_current}/{w.hpa_max}</span>
                      : <span className="text-muted-foreground">—</span>}
                  </TableCell>
                  <TableCell><VerdictBadge verdict={w.verdict} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
      </Card>
    </div>
  );
}

// ─── Aba: HPA Histórico ────────────────────────────────────────────────────────

type SortField = "workload" | "util" | "daysAtMax" | "daysAtMin";

function hpaRecommendation(avgUtil: number, daysAtMax: number, daysAtMin: number, totalDays: number) {
  if (daysAtMax >= Math.ceil(totalDays * 0.1))
    return { label: "Aumentar maxReplicas", color: "text-red-600", bg: "bg-red-50 dark:bg-red-950/20" };
  if (daysAtMin >= Math.ceil(totalDays * 0.8))
    return { label: "Remover HPA / fixar min", color: "text-purple-600", bg: "bg-purple-50 dark:bg-purple-950/20" };
  if (avgUtil < 25)
    return { label: "Reduzir minReplicas", color: "text-blue-600", bg: "bg-blue-50 dark:bg-blue-950/20" };
  return { label: "Normal", color: "text-green-600", bg: "" };
}

function HPASparkline({ series, hpaMax, days }: { series: HPADayPoint[]; hpaMax: number; days: number }) {
  const pts = series.slice(-days);
  return (
    <div className="flex items-end gap-px" style={{ width: 72, height: 28 }}>
      {pts.map((p, i) => {
        const pct = hpaMax > 0 ? p.max_replicas / hpaMax : 0;
        const h = Math.max(2, Math.round(pct * 26));
        const color = pct >= 1 ? "#ef4444" : pct >= 0.7 ? "#f59e0b" : "#10b981";
        return (
          <div key={i}
            title={`${p.date.slice(5)}: max ${p.max_replicas}, avg ${p.avg_replicas.toFixed(1)}`}
            style={{ height: h, background: color, opacity: 0.85, flex: 1, minWidth: 1 }}
            className="rounded-sm" />
        );
      })}
    </div>
  );
}

function HPADetailChart({ hpa, nodeMap }: { hpa: HPATimeline; nodeMap: Record<string, number> }) {
  const series = hpa.series.map(p => ({
    date: p.date.slice(5),
    max: p.max_replicas,
    avg: parseFloat(p.avg_replicas.toFixed(1)),
    min: p.min_replicas,
    nodes: nodeMap[p.date.slice(5)] ?? null,
  }));

  return (
    <div className="border-t bg-muted/20 px-4 pt-3 pb-4">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-xs text-muted-foreground">{hpa.namespace} /</span>
        <span className="text-xs font-semibold">{hpa.workload}</span>
        <span className="text-[10px] text-muted-foreground ml-1">
          config: min={hpa.hpa_min} max={hpa.hpa_max} · {hpa.series.length} dias
        </span>
      </div>
      <ResponsiveContainer width="100%" height={200}>
        <ComposedChart data={series} margin={{ left: 4, right: 32, top: 6, bottom: 16 }}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
          <XAxis dataKey="date" tick={{ fontSize: 9 }} axisLine={false} tickLine={false}
            interval={Math.max(0, Math.floor(series.length / 8))} />
          <YAxis yAxisId="rep" tick={{ fontSize: 9 }} axisLine={false} tickLine={false} width={24}
            label={{ value: "réplicas", angle: -90, position: "insideLeft", style: { fontSize: 9, fill: "#9ca3af" } }} />
          <YAxis yAxisId="nodes" orientation="right" tick={{ fontSize: 9 }}
            axisLine={false} tickLine={false} width={24} />
          <Tooltip
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null;
              return (
                <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                  <p className="font-medium">{label}</p>
                  {payload.map((p, i) => (
                    <p key={i} style={{ color: p.color as string }}>
                      {p.name}: {typeof p.value === "number" ? p.value.toFixed(p.name === "avg" ? 1 : 0) : p.value}
                    </p>
                  ))}
                </div>
              );
            }}
          />
          <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 10 }} />
          <ReferenceLine yAxisId="rep" y={hpa.hpa_min} stroke="#9ca3af" strokeDasharray="4 2"
            label={{ value: `min:${hpa.hpa_min}`, position: "insideTopLeft", fontSize: 9, fill: "#9ca3af" }} />
          <ReferenceLine yAxisId="rep" y={hpa.hpa_max} stroke="#ef4444" strokeDasharray="4 2"
            label={{ value: `max:${hpa.hpa_max}`, position: "insideTopLeft", fontSize: 9, fill: "#ef4444" }} />
          <Bar yAxisId="rep" dataKey="max" name="max/dia" fill="#f59e0b" fillOpacity={0.25} maxBarSize={10} radius={[2, 2, 0, 0]} />
          <Line yAxisId="rep" type="monotone" dataKey="avg" name="avg" stroke="#10b981" strokeWidth={2} dot={false} />
          <Line yAxisId="rep" type="monotone" dataKey="min" name="min" stroke="#9ca3af" strokeWidth={1} strokeDasharray="3 2" dot={false} />
          <Line yAxisId="nodes" type="step" dataKey="nodes" name="nodes" stroke="#8b5cf6" strokeWidth={1.5} dot={false} />
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}

function HPAHistoryTab({ cluster }: { cluster: string }) {
  const [days, setDays] = useState(30);
  const [timeline, setTimeline] = useState<TimelineReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [sortField, setSortField] = useState<SortField>("daysAtMax");
  const [sortAsc, setSortAsc] = useState(false);

  const fetchTimeline = async () => {
    setLoading(true);
    setError(null);
    setTimeline(null);
    setExpandedKey(null);
    try {
      const token = localStorage.getItem("token") || "poc-token-123";
      const r = await fetch(`/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=${days}`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!r.ok) {
        const e = await r.json().catch(() => ({}));
        throw new Error((e as { error?: string }).error ?? `Erro ${r.status}`);
      }
      setTimeline(await r.json());
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  const toggleSort = (field: SortField) => {
    if (sortField === field) setSortAsc(a => !a);
    else { setSortField(field); setSortAsc(false); }
  };

  const nodeMap = Object.fromEntries((timeline?.nodes ?? []).map(n => [n.date.slice(5), n.node_count]));

  const hpaRows = (timeline?.hpas ?? []).map(h => {
    const n = h.series.length || 1;
    const avgUtil = h.series.reduce((s, p) => s + (h.hpa_max > 0 ? p.avg_replicas / h.hpa_max : 0), 0) / n * 100;
    const daysAtMax = h.series.filter(p => p.max_replicas >= h.hpa_max).length;
    const daysAtMin = h.series.filter(p => p.max_replicas <= h.hpa_min).length;
    return { ...h, avgUtil, daysAtMax, daysAtMin };
  }).sort((a, b) => {
    let diff = 0;
    if (sortField === "workload") diff = a.workload.localeCompare(b.workload);
    else if (sortField === "util")      diff = a.avgUtil - b.avgUtil;
    else if (sortField === "daysAtMax") diff = a.daysAtMax - b.daysAtMax;
    else if (sortField === "daysAtMin") diff = a.daysAtMin - b.daysAtMin;
    return sortAsc ? diff : -diff;
  });

  // Summary cards
  const neverScaled  = hpaRows.filter(h => h.daysAtMin >= Math.ceil(days * 0.8)).length;
  const atMaxRisk    = hpaRows.filter(h => h.daysAtMax >= Math.ceil(days * 0.1)).length;
  const avgUtilAll   = hpaRows.length > 0 ? hpaRows.reduce((s, h) => s + h.avgUtil, 0) / hpaRows.length : 0;
  const currentNodes = timeline?.nodes.at(-1)?.node_count ?? 0;

  const SortBtn = ({ field, label }: { field: SortField; label: string }) => (
    <button className="flex items-center gap-1 hover:text-foreground transition-colors"
      onClick={() => toggleSort(field)}>
      {label}
      <ArrowUpDown className={`h-3 w-3 ${sortField === field ? "opacity-100" : "opacity-30"}`} />
    </button>
  );

  return (
    <div className="space-y-4">
      {/* Controles */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1">
          {([7, 15, 30] as const).map(d => (
            <Button key={d} variant={days === d ? "default" : "outline"} size="sm"
              className="h-7 text-xs px-3" onClick={() => setDays(d)}>
              {d}d
            </Button>
          ))}
        </div>
        <Button size="sm" className="h-7 text-xs gap-1.5" onClick={fetchTimeline} disabled={loading}>
          {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          Buscar histórico
        </Button>
        {timeline && (
          <span className="text-xs text-muted-foreground">
            {timeline.start_date} → {timeline.end_date} · {timeline.hpas.length} HPAs · {timeline.nodes.length} dias
          </span>
        )}
      </div>

      {error && (
        <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <AlertTriangle className="h-4 w-4 text-red-600" />
          <AlertDescription className="text-sm text-red-700 dark:text-red-400">{error}</AlertDescription>
        </Alert>
      )}

      {!timeline && !loading && !error && (
        <div className="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3">
          <Activity className="h-10 w-10 opacity-20" />
          <p className="text-sm">Selecione o período e clique em <strong>Buscar histórico</strong></p>
          <p className="text-xs opacity-70">Requer Prometheus acessível pelo servidor</p>
        </div>
      )}

      {loading && (
        <div className="flex items-center justify-center py-20 gap-3 text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
          <span className="text-sm">Consultando Prometheus ({days} dias)…</span>
        </div>
      )}

      {timeline && (
        <>
          {/* Summary cards */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <SummaryCard icon={Activity} label="HPAs monitorados" color="text-indigo-600"
              value={String(hpaRows.length)} sub={`${days} dias de histórico`} />
            <SummaryCard icon={TrendingDown} label="Nunca escalaram (≥80% no mín)" color="text-purple-600"
              value={String(neverScaled)} sub={neverScaled > 0 ? "candidatos a remover HPA" : "nenhum"} />
            <SummaryCard icon={AlertTriangle} label="No limite máximo (≥10% dos dias)" color="text-red-600"
              value={String(atMaxRisk)} sub={atMaxRisk > 0 ? "risco de throttling" : "nenhum"} />
            <SummaryCard icon={Server} label="Nodes ativos / Util. média" color="text-cyan-600"
              value={String(currentNodes)} sub={`util. avg HPAs: ${avgUtilAll.toFixed(0)}%`} />
          </div>

          {/* Tabela */}
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow className="text-[11px] text-muted-foreground">
                    <TableHead className="pl-4 w-[260px]">
                      <SortBtn field="workload" label="Workload / Namespace" />
                    </TableHead>
                    <TableHead className="text-center w-[80px]">Min → Max</TableHead>
                    <TableHead className="text-center w-[90px]">
                      <SortBtn field="util" label="Util avg %" />
                    </TableHead>
                    <TableHead className="w-[88px]">Scaling ({days}d)</TableHead>
                    <TableHead className="text-center w-[80px]">
                      <SortBtn field="daysAtMax" label="Dias@Max" />
                    </TableHead>
                    <TableHead className="text-center w-[80px]">
                      <SortBtn field="daysAtMin" label="Dias@Min" />
                    </TableHead>
                    <TableHead>Recomendação</TableHead>
                    <TableHead className="w-8" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {hpaRows.map(h => {
                    const key = `${h.namespace}/${h.workload}`;
                    const isOpen = expandedKey === key;
                    const rec = hpaRecommendation(h.avgUtil, h.daysAtMax, h.daysAtMin, days);
                    const utilColor = h.avgUtil >= 80 ? "text-red-500" : h.avgUtil >= 50 ? "text-yellow-500" : "text-green-500";
                    return (
                      <>
                        <TableRow key={key}
                          className={`text-xs cursor-pointer hover:bg-muted/40 transition-colors ${isOpen ? "bg-muted/30" : ""}`}
                          onClick={() => setExpandedKey(isOpen ? null : key)}>
                          <TableCell className="pl-4 py-2">
                            <p className="font-semibold truncate max-w-[220px]">{h.workload}</p>
                            <p className="text-[10px] text-muted-foreground truncate max-w-[220px]">{h.namespace}</p>
                          </TableCell>
                          <TableCell className="text-center font-mono">
                            <span className="text-muted-foreground">{h.hpa_min}</span>
                            <span className="text-muted-foreground mx-1">→</span>
                            <span>{h.hpa_max}</span>
                          </TableCell>
                          <TableCell className="text-center">
                            <span className={`font-bold ${utilColor}`}>{h.avgUtil.toFixed(0)}%</span>
                          </TableCell>
                          <TableCell>
                            <HPASparkline series={h.series} hpaMax={h.hpa_max} days={days} />
                          </TableCell>
                          <TableCell className="text-center">
                            {h.daysAtMax > 0
                              ? <span className="text-red-500 font-semibold">{h.daysAtMax}d</span>
                              : <span className="text-muted-foreground">—</span>}
                          </TableCell>
                          <TableCell className="text-center">
                            {h.daysAtMin > 0
                              ? <span className="text-purple-500 font-semibold">{h.daysAtMin}d</span>
                              : <span className="text-muted-foreground">—</span>}
                          </TableCell>
                          <TableCell>
                            <span className={`text-[11px] font-medium ${rec.color}`}>{rec.label}</span>
                          </TableCell>
                          <TableCell className="pr-3">
                            {isOpen
                              ? <ChevronUp className="h-3.5 w-3.5 text-muted-foreground" />
                              : <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />}
                          </TableCell>
                        </TableRow>
                        {isOpen && (
                          <TableRow key={`${key}-detail`} className="hover:bg-transparent">
                            <TableCell colSpan={8} className="p-0">
                              <HPADetailChart hpa={h} nodeMap={nodeMap} />
                            </TableCell>
                          </TableRow>
                        )}
                      </>
                    );
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

// ─── Aba: Oportunidades ───────────────────────────────────────────────────────

function OpportunitiesTab({ workloads, summary }: { workloads: FinOpsWorkload[]; summary: FinOpsSummary }) {
  const hasPrometheus = workloads.some(w => (w.waste_brl ?? 0) > 0);

  const opportunities = workloads
    .filter(w => w.verdict === "superprovisioned" || w.verdict === "no_request" || (w.waste_brl ?? 0) > 0)
    .sort((a, b) => {
      // Ordenar por desperdício Prometheus se disponível, senão por economia HPA
      const wa = hasPrometheus ? (a.waste_brl ?? 0) : (a.hpa_cost_current_brl - a.hpa_cost_min_brl);
      const wb = hasPrometheus ? (b.waste_brl ?? 0) : (b.hpa_cost_current_brl - b.hpa_cost_min_brl);
      return wb - wa;
    });

  const totalWaste = workloads.reduce((s, w) => s + (w.waste_brl ?? 0), 0);

  return (
    <div className="space-y-3">
      {opportunities.length === 0 ? (
        <Card>
          <CardContent className="p-8 text-center text-muted-foreground">
            <CheckCircle2 className="h-10 w-10 mx-auto mb-2 text-green-500" />
            <p className="font-medium">Nenhuma oportunidade de saving identificada</p>
            <p className="text-sm mt-1">Todos os workloads estão classificados como eficientes.</p>
          </CardContent>
        </Card>
      ) : (
        <>
          {hasPrometheus ? (
            <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
              <Activity className="h-4 w-4 text-red-600" />
              <AlertDescription className="text-sm">
                <strong>Desperdício real baseado em P95 Prometheus:</strong>{" "}
                <strong className="text-red-700 dark:text-red-400">{fmtBRL(totalWaste)}/mês</strong>
                {" "}= <strong>{fmtBRL(totalWaste * 12)}/ano</strong>
                {summary.hpa_savings_if_min_brl > 0 && (
                  <span className="text-muted-foreground ml-2">
                    · Economia HPA adicional: {fmtBRL(summary.hpa_savings_if_min_brl)}/mês
                  </span>
                )}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950/20">
              <TrendingDown className="h-4 w-4 text-green-600" />
              <AlertDescription className="text-sm">
                <strong>{opportunities.length} oportunidades</strong> identificadas.
                Economia potencial ajustando HPAs para mínimo:{" "}
                <strong className="text-green-700 dark:text-green-400">{fmtBRL(summary.hpa_savings_if_min_brl)}/mês</strong>
                {" "}= <strong>{fmtBRL(summary.hpa_savings_if_min_brl * 12)}/ano</strong>
              </AlertDescription>
            </Alert>
          )}

          {/* Chart de oportunidades */}
          {opportunities.length > 1 && (() => {
            const chartData = opportunities.slice(0, 10).map(w => ({
              name: w.workload.length > 16 ? w.workload.slice(0, 14) + "…" : w.workload,
              atual: hasPrometheus
                ? w.cost_share_brl
                : w.hpa_cost_current_brl,
              saving: hasPrometheus
                ? (w.waste_brl ?? 0)
                : Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl),
            })).filter(d => d.saving > 0);
            if (!chartData.length) return null;
            return (
              <Card>
                <CardHeader className="pb-1 pt-3 px-4">
                  <CardTitle className="text-sm">Potencial de Economia por Workload</CardTitle>
                </CardHeader>
                <CardContent className="px-2 pb-3">
                  <ResponsiveContainer width="100%" height={180}>
                    <BarChart data={chartData} layout="vertical"
                      margin={{ left: 8, right: 60, top: 4, bottom: 4 }}>
                      <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.4} />
                      <XAxis type="number" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                        tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                      <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={110}
                        axisLine={false} tickLine={false} />
                      <Tooltip
                        content={({ active, payload, label }) => {
                          if (!active || !payload?.length) return null;
                          return (
                            <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                              <p className="font-medium">{label}</p>
                              {payload.map((p, i) => (
                                <p key={i} style={{ color: p.color }}>
                                  {p.name === "atual" ? `Custo atual: ${fmtBRL(p.value as number)}`
                                    : `Economia: ${fmtBRL(p.value as number)}`}
                                </p>
                              ))}
                            </div>
                          );
                        }}
                      />
                      <Bar dataKey="atual" name="atual" stackId="a" fill="#6366f1" fillOpacity={0.3}
                        radius={[0, 0, 0, 0]} maxBarSize={18} />
                      <Bar dataKey="saving" name="saving" stackId="a" fill="#ef4444" fillOpacity={0.85}
                        radius={[0, 4, 4, 0]} maxBarSize={18}>
                        <LabelList dataKey="saving" position="right"
                          formatter={(v: number) => v > 0 ? `R$${(v / 1000).toFixed(1)}k` : ""}
                          style={{ fontSize: 9, fill: "#ef4444" }} />
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                </CardContent>
              </Card>
            );
          })()}

          <div className="space-y-2">
            {opportunities.map((w, i) => {
              const hpaSaving = w.hpa_cost_current_brl - w.hpa_cost_min_brl;
              const waste = w.waste_brl ?? 0;
              const borderColor = w.verdict === "oom_risk" ? "#f59e0b"
                : w.verdict === "superprovisioned" ? "#ef4444"
                : waste > 0 ? "#f97316"
                : "#9ca3af";
              return (
                <Card key={i} className="border-l-4" style={{ borderLeftColor: borderColor }}>
                  <CardContent className="p-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-medium text-sm truncate">{w.workload}</span>
                          <span className="text-xs text-muted-foreground">{w.namespace}</span>
                          <VerdictBadge verdict={w.verdict} />
                        </div>
                        {w.verdict === "superprovisioned" && w.hpa_max > 0 && (
                          <p className="text-xs text-muted-foreground mt-1">
                            HPA: min={w.hpa_min} / atual={w.hpa_current} / max={w.hpa_max}
                            {" · "}Rodando em {Math.round((w.hpa_current / w.hpa_max) * 100)}% do máximo
                          </p>
                        )}
                        {(w.cpu_p95_millis ?? 0) > 0 && (
                          <p className="text-xs text-muted-foreground mt-1">
                            P95: CPU {Math.round(w.cpu_p95_millis!)}m / {Math.round(w.cpu_request_millis)}m req
                            {(w.mem_p95_mi ?? 0) > 0 && ` · Mem ${Math.round(w.mem_p95_mi!)}Mi / ${Math.round(w.mem_request_mi)}Mi req`}
                          </p>
                        )}
                        {w.verdict === "no_request" && (
                          <p className="text-xs text-muted-foreground mt-1">
                            Sem CPU/Mem request definido — impossível calcular custo real
                          </p>
                        )}
                      </div>
                      <div className="text-right flex-shrink-0 space-y-0.5">
                        {waste > 0 && (
                          <>
                            <p className="text-[10px] text-muted-foreground">Desperdício/mês</p>
                            <p className="text-base font-bold text-red-600">{fmtBRL(waste)}</p>
                          </>
                        )}
                        {!hasPrometheus && hpaSaving > 0 && (
                          <>
                            <p className="text-[10px] text-muted-foreground">Economia HPA/mês</p>
                            <p className="text-base font-bold text-green-600">{fmtBRL(hpaSaving)}</p>
                            <p className="text-[10px] text-muted-foreground">{fmtBRL(w.hpa_cost_current_brl)} → {fmtBRL(w.hpa_cost_min_brl)}</p>
                          </>
                        )}
                      </div>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

// ─── Componente Principal ─────────────────────────────────────────────────────

export const FinOpsTab = ({ selectedCluster }: { selectedCluster?: string }) => {
  const { clusters } = useClusters();

  // Clusters que têm node pools no registry usam sufixo -admin
  const clusterOptions = (clusters ?? []).map(c =>
    c.name.endsWith("-admin") ? c.name : c.name + "-admin"
  ).filter((v, i, a) => a.indexOf(v) === i);

  const defaultCluster = selectedCluster
    ? (selectedCluster.endsWith("-admin") ? selectedCluster : selectedCluster + "-admin")
    : clusterOptions[0] ?? "";

  const [cluster, setCluster] = useState(defaultCluster);
  const [triggerKey, setTriggerKey] = useState(0);
  const [withPrometheus, setWithPrometheus] = useState(false);
  const [windowDays, setWindowDays] = useState(30);
  const [aiAnalysis, setAiAnalysis] = useState<string | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiExpanded, setAiExpanded] = useState(true);

  const { data: report, isLoading, error, refetch } = useQuery<FinOpsReport>({
    queryKey: ["finops-report", cluster, triggerKey],
    queryFn: async () => {
      let url = `/api/v1/finops/report?cluster=${encodeURIComponent(cluster)}`;
      if (withPrometheus) {
        url += `&with_prometheus=true&window_days=${windowDays}`;
      }
      const r = await fetch(url, {
        headers: { Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}` },
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const exportCSV = () => {
    if (!report) return;
    const hasP95 = report.workloads.some(w => (w.cpu_p95_millis ?? 0) > 0);
    const header = [
      "Namespace", "Workload", "Pods",
      "CPU Request (m)", "Mem Request (Mi)",
      ...(hasP95 ? ["CPU P95 (m)", "Mem P95 (Mi)", "Desperdício R$/mês"] : []),
      "Custo R$/mês", "HPA Min", "HPA Atual", "HPA Max",
      "Custo HPA Min R$", "Custo HPA Max R$", "Veredicto",
    ].join(",");
    const rows = report.workloads.map(w =>
      [w.namespace, w.workload, w.pods,
       Math.round(w.cpu_request_millis), Math.round(w.mem_request_mi),
       ...(hasP95 ? [Math.round(w.cpu_p95_millis ?? 0), Math.round(w.mem_p95_mi ?? 0), (w.waste_brl ?? 0).toFixed(2)] : []),
       w.cost_share_brl.toFixed(2),
       w.hpa_min, w.hpa_current, w.hpa_max,
       w.hpa_cost_min_brl.toFixed(2), w.hpa_cost_max_brl.toFixed(2),
       w.verdict,
      ].join(",")
    );
    const csv = [header, ...rows].join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `finops-${report.cluster.replace("-admin", "")}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("CSV exportado com sucesso");
  };

  const analyzeWithAI = async () => {
    if (!report) return;
    const aiEmail = localStorage.getItem("ai_email") ?? "";
    if (!aiEmail) {
      toast.error("Configure seu e-mail de AI em Configurações → AI Settings");
      return;
    }
    setAiLoading(true);
    setAiAnalysis(null);
    try {
      const r = await fetch("/api/v1/finops/analyze", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}`,
        },
        body: JSON.stringify({ ai_email: aiEmail, report }),
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      const data = await r.json();
      setAiAnalysis(data.analysis);
      setAiExpanded(true);
      toast.success("Análise AI concluída");
    } catch (err) {
      toast.error("Falha na análise AI: " + (err as Error).message);
    } finally {
      setAiLoading(false);
    }
  };

  return (
    <div className="flex flex-col h-full p-4 gap-4 overflow-auto">
      {/* Header */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-lg font-semibold flex items-center gap-2">
            <CircleDollarSign className="h-5 w-5 text-blue-500" />
            FinOps — Análise de Custo AKS
          </h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Custo real baseado na Azure Pricing API (pay-as-you-go, Brasil Sul)
          </p>
        </div>
        <div className="flex flex-col gap-2 items-end">
          <div className="flex items-center gap-2 flex-wrap justify-end">
            <Select value={cluster} onValueChange={setCluster}>
              <SelectTrigger className="w-64 h-8 text-sm">
                <SelectValue placeholder="Selecionar cluster..." />
              </SelectTrigger>
              <SelectContent>
                {clusterOptions.map(c => (
                  <SelectItem key={c} value={c}>{c.replace("-admin", "")}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size="sm" variant="outline" className="h-8 gap-1"
              onClick={() => { setTriggerKey(k => k + 1); refetch(); setAiAnalysis(null); }}
              disabled={isLoading}>
              <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
              Analisar
            </Button>
            {report && (
              <>
                <Button size="sm" variant="outline" className="h-8 gap-1" onClick={exportCSV}>
                  <Download className="h-3.5 w-3.5" />
                  CSV
                </Button>
                <Button size="sm" variant="outline" className="h-8 gap-1"
                  onClick={analyzeWithAI} disabled={aiLoading}>
                  {aiLoading
                    ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    : <Brain className="h-3.5 w-3.5" />}
                  {aiLoading ? "Analisando..." : "Analisar com AI"}
                </Button>
              </>
            )}
          </div>
          {/* Toggle análise histórica Prometheus */}
          <div className="flex items-center gap-2 flex-wrap justify-end">
            <label className="flex items-center gap-1.5 cursor-pointer select-none text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={withPrometheus}
                onChange={e => setWithPrometheus(e.target.checked)}
                className="h-3.5 w-3.5 cursor-pointer"
              />
              <Activity className="h-3 w-3" />
              Análise histórica Prometheus
            </label>
            {withPrometheus && (
              <Select value={String(windowDays)} onValueChange={v => setWindowDays(Number(v))}>
                <SelectTrigger className="h-7 w-24 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[7, 14, 30].map(d => (
                    <SelectItem key={d} value={String(d)}>{d} dias</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
        </div>
      </div>

      {/* Estados */}
      {isLoading && (
        <div className="flex-1 flex items-center justify-center gap-3 text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
          <span>Coletando dados do cluster e preços Azure...</span>
        </div>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>
            {(error as Error).message}
            {(error as Error).message.includes("scan") && (
              <span className="block mt-1 text-xs">
                Acesse a aba <strong>Dynatrace → Node Pools</strong> e clique em "Escanear Clusters" para popular o registry.
              </span>
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* Relatório */}
      {report && !isLoading && (
        <>
          <div className="flex items-center gap-2 text-xs text-muted-foreground -mb-1">
            <Server className="h-3.5 w-3.5" />
            <span>
              {report.cluster.replace("-admin", "")} · gerado em {new Date(report.generated_at).toLocaleTimeString("pt-BR")}
              {" · "}câmbio USD/BRL: <strong>R$ {report.exchange_rate.toFixed(4)}</strong> ({report.exchange_date})
              {" · "}{report.node_pools.length} node pools · {report.summary.workloads_analyzed} workloads
            </span>
          </div>

          {/* Resultado AI */}
          {aiAnalysis && (
            <Card className="border-blue-200 dark:border-blue-800">
              <CardHeader className="py-2 px-4 cursor-pointer" onClick={() => setAiExpanded(e => !e)}>
                <CardTitle className="text-sm flex items-center justify-between">
                  <span className="flex items-center gap-2">
                    <Brain className="h-4 w-4 text-blue-500" />
                    Análise AI — Recomendações FinOps
                  </span>
                  {aiExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                </CardTitle>
              </CardHeader>
              {aiExpanded && (
                <CardContent className="px-4 pb-4">
                  <pre className="text-xs whitespace-pre-wrap font-sans leading-relaxed text-foreground">
                    {aiAnalysis}
                  </pre>
                </CardContent>
              )}
            </Card>
          )}

          <Tabs defaultValue="dashboard" className="flex-1 flex flex-col min-h-0">
            <TabsList className="w-fit">
              <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
              <TabsTrigger value="nodepools">
                Node Pools
                <Badge variant="secondary" className="ml-1 text-[10px]">{report.node_pools.length}</Badge>
              </TabsTrigger>
              <TabsTrigger value="workloads">
                Workloads
                <Badge variant="secondary" className="ml-1 text-[10px]">{report.summary.workloads_analyzed}</Badge>
              </TabsTrigger>
              <TabsTrigger value="hpa-history">
                HPA {report.window_days > 0 ? `${report.window_days}d` : "Histórico"}
                {report.summary.hpa_removable_count > 0 && (
                  <Badge className="ml-1 text-[10px] bg-purple-600">{report.summary.hpa_removable_count}</Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="opportunities">
                Oportunidades
                {(report.summary.superprovisioned_count + report.summary.oom_risk_count) > 0 && (
                  <Badge variant="destructive" className="ml-1 text-[10px]">
                    {report.summary.superprovisioned_count + report.summary.oom_risk_count}
                  </Badge>
                )}
              </TabsTrigger>
            </TabsList>

            <div className="flex-1 overflow-auto mt-3">
              <TabsContent value="dashboard" className="mt-0 h-full">
                <DashboardTab cluster={cluster} report={report} />
              </TabsContent>
              <TabsContent value="nodepools" className="mt-0 h-full">
                <NodePoolsTab pools={report.node_pools} />
              </TabsContent>
              <TabsContent value="workloads" className="mt-0 h-full">
                <WorkloadsTab workloads={report.workloads} windowDays={report.window_days || windowDays} />
              </TabsContent>
              <TabsContent value="hpa-history" className="mt-0 h-full">
                <HPAHistoryTab cluster={cluster} />
              </TabsContent>
              <TabsContent value="opportunities" className="mt-0 h-full">
                <OpportunitiesTab workloads={report.workloads} summary={report.summary} />
              </TabsContent>
            </div>
          </Tabs>
        </>
      )}

      {!report && !isLoading && !error && (
        <div className="flex-1 flex items-center justify-center flex-col gap-3 text-muted-foreground">
          <CircleDollarSign className="h-12 w-12 opacity-20" />
          <p>Selecione um cluster e clique em <strong>Analisar</strong></p>
        </div>
      )}
    </div>
  );
};
