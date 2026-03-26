import { useState, useEffect } from "react";
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
  PieChart, Pie, Legend, ComposedChart, Line,
  LabelList, ReferenceLine,
} from "recharts";
import {
  DollarSign, TrendingDown, AlertTriangle, CheckCircle2,
  Loader2, RefreshCw, Server, Layers, CircleDollarSign,
  ArrowUpDown, Info, ChevronDown, ChevronUp, Download, Brain, Activity
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

interface TimelineReport {
  cluster: string;
  start_date: string;
  end_date: string;
  hpas: HPATimeline[];
  nodes: { date: string; node_count: number }[];
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

// ─── Aba: Visão Geral ─────────────────────────────────────────────────────────

// Tooltip customizado com sombra e formatação elegante
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

function OverviewTab({ report }: { report: FinOpsReport }) {
  const { summary, namespaces, node_pools, workloads } = report;

  // Top 8 namespaces para chart horizontal
  const topNs = namespaces.slice(0, 8).map(ns => ({
    name: ns.namespace.length > 24 ? ns.namespace.slice(0, 22) + "…" : ns.namespace,
    value: ns.monthly_cost_brl,
    pct: summary.total_monthly_cost_brl > 0
      ? Math.round((ns.monthly_cost_brl / summary.total_monthly_cost_brl) * 100)
      : 0,
  }));

  // Node pools para chart
  const poolChart = node_pools.map((p, i) => ({
    name: p.name.length > 14 ? p.name.slice(0, 12) + "…" : p.name,
    custo: p.monthly_cost_brl,
    nos: p.node_count,
    color: POOL_COLORS[i % POOL_COLORS.length],
  }));

  // DonutChart — distribuição de saúde dos workloads
  const okCount = workloads.filter(w => w.verdict === "ok").length;
  const verdictDist = [
    { name: "Eficiente",    value: okCount, fill: "#10b981" },
    { name: "Desperdício",  value: summary.superprovisioned_count, fill: "#ef4444" },
    { name: "Risco OOM",    value: summary.oom_risk_count, fill: "#f59e0b" },
    { name: "Sem Request",  value: summary.no_request_count, fill: "#9ca3af" },
  ].filter(d => d.value > 0);

  // Top 10 workloads para chart de barras (custo + utilização HPA)
  const top10 = workloads.slice(0, 10).map(w => ({
    name: w.workload.length > 18 ? w.workload.slice(0, 16) + "…" : w.workload,
    custo: w.cost_share_brl,
    hpa_util: w.hpa_max > 0 ? Math.round((w.hpa_current / w.hpa_max) * 100) : null,
    color: verdictConfig[w.verdict]?.fill ?? "#6366f1",
  }));

  const verdictFills: Record<string, string> = {
    superprovisioned: "#ef4444",
    oom_risk: "#f59e0b",
    ok: "#6366f1",
    no_request: "#9ca3af",
  };

  return (
    <div className="space-y-4">
      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <SummaryCard
          icon={CircleDollarSign} label="Custo Total/mês" color="text-blue-600"
          value={fmtBRL(summary.total_monthly_cost_brl)}
          sub={fmtUSD(summary.total_monthly_cost_usd)}
        />
        {summary.potential_savings_brl > 0 ? (
          <SummaryCard
            icon={Activity} label="Desperdício Real (P95)" color="text-red-600"
            value={fmtBRL(summary.potential_savings_brl)}
            sub="baseado em uso P95 Prometheus"
          />
        ) : (
          <SummaryCard
            icon={TrendingDown} label="Economia HPA (se mín)" color="text-green-600"
            value={fmtBRL(summary.hpa_savings_if_min_brl)}
            sub={`${summary.superprovisioned_count} workloads`}
          />
        )}
        <SummaryCard
          icon={Layers} label="Workloads Analisados" color="text-purple-600"
          value={String(summary.workloads_analyzed)}
          sub={`${summary.no_request_count} sem request`}
        />
        <SummaryCard
          icon={DollarSign} label="Cotação USD/BRL" color="text-orange-600"
          value={`R$ ${report.exchange_rate.toFixed(4)}`}
          sub={report.exchange_date}
        />
      </div>

      {/* Row 2: DonutChart + Top Namespaces */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* DonutChart — Saúde dos Workloads */}
        <Card>
          <CardHeader className="pb-1 pt-4 px-4">
            <CardTitle className="text-sm">Saúde dos Workloads</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={220}>
              <PieChart>
                <Pie
                  data={verdictDist}
                  cx="42%"
                  cy="50%"
                  innerRadius={62}
                  outerRadius={88}
                  paddingAngle={3}
                  dataKey="value"
                  strokeWidth={0}
                >
                  {verdictDist.map((entry, i) => (
                    <Cell key={i} fill={entry.fill} />
                  ))}
                </Pie>
                <Tooltip
                  content={({ active, payload }) => {
                    if (!active || !payload?.length) return null;
                    const d = payload[0].payload;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs">
                        <p className="font-medium" style={{ color: d.fill }}>{d.name}</p>
                        <p className="text-foreground">{d.value} workloads ({Math.round((d.value / summary.workloads_analyzed) * 100)}%)</p>
                      </div>
                    );
                  }}
                />
                <Legend
                  layout="vertical"
                  align="right"
                  verticalAlign="middle"
                  iconType="circle"
                  iconSize={8}
                  formatter={(value, entry) => (
                    <span className="text-[11px] text-foreground">
                      {value} <span className="text-muted-foreground">({(entry as { payload?: { value?: number } }).payload?.value ?? 0})</span>
                    </span>
                  )}
                />
                {/* Centro do donut */}
                <text x="42%" y="46%" textAnchor="middle" dominantBaseline="middle"
                  className="fill-foreground" style={{ fontSize: 22, fontWeight: 700 }}>
                  {summary.workloads_analyzed}
                </text>
                <text x="42%" y="58%" textAnchor="middle" dominantBaseline="middle"
                  style={{ fontSize: 10, fill: "#9ca3af" }}>
                  workloads
                </text>
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Top Namespaces por Custo */}
        <Card>
          <CardHeader className="pb-1 pt-4 px-4">
            <CardTitle className="text-sm">Top Namespaces por Custo</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={topNs} layout="vertical" margin={{ left: 6, right: 52, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.4} />
                <XAxis type="number" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={120}
                  axisLine={false} tickLine={false} />
                <Tooltip content={<ChartTooltip formatter={(v, n) => `${n}: ${fmtBRL(v)}`} />} />
                <Bar dataKey="value" name="Custo/mês" radius={[0, 4, 4, 0]} maxBarSize={20}>
                  {topNs.map((_, i) => (
                    <Cell key={i} fill={POOL_COLORS[i % POOL_COLORS.length]} fillOpacity={0.85} />
                  ))}
                  <LabelList dataKey="pct" position="right"
                    formatter={(v: number) => `${v}%`}
                    style={{ fontSize: 10, fill: "#9ca3af" }} />
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Row 3: Top 10 Workloads + Node Pools */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Top 10 Workloads — Custo + Utilização HPA */}
        <Card>
          <CardHeader className="pb-1 pt-4 px-4">
            <CardTitle className="text-sm flex items-center justify-between">
              Top 10 Workloads por Custo
              <span className="text-[10px] text-muted-foreground font-normal">linha = % utiliz. HPA</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={220}>
              <ComposedChart data={top10} margin={{ left: 4, right: 40, top: 4, bottom: 28 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.4} />
                <XAxis dataKey="name" tick={{ fontSize: 9 }} angle={-35} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis yAxisId="left" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis yAxisId="right" orientation="right" unit="%" domain={[0, 100]}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={30} />
                <Tooltip
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs">
                        <p className="font-medium mb-1">{label}</p>
                        {payload.map((p, i) => (
                          <p key={i} style={{ color: p.color }}>
                            {p.name === "custo" ? `Custo: ${fmtBRL(p.value as number)}`
                              : `HPA util: ${p.value}%`}
                          </p>
                        ))}
                      </div>
                    );
                  }}
                />
                <Bar yAxisId="left" dataKey="custo" name="custo" radius={[3, 3, 0, 0]} maxBarSize={28}>
                  {top10.map((w, i) => (
                    <Cell key={i} fill={verdictFills[
                      workloads[i]?.verdict ?? "ok"
                    ] ?? "#6366f1"} fillOpacity={0.8} />
                  ))}
                </Bar>
                <Line yAxisId="right" dataKey="hpa_util" name="hpa_util" type="monotone"
                  stroke="#f59e0b" strokeWidth={2} dot={{ r: 3, fill: "#f59e0b" }}
                  connectNulls={false} />
                <ReferenceLine yAxisId="right" y={35} stroke="#ef4444" strokeDasharray="4 2"
                  strokeOpacity={0.5} />
              </ComposedChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Custo por Node Pool */}
        <Card>
          <CardHeader className="pb-1 pt-4 px-4">
            <CardTitle className="text-sm">Custo por Node Pool</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={poolChart} margin={{ left: 4, right: 12, top: 4, bottom: 20 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.4} />
                <XAxis dataKey="name" tick={{ fontSize: 10 }} angle={-25} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                <Tooltip
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    const pool = node_pools.find(p => p.name.startsWith(label?.slice(0, 8) ?? ""));
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium">{label}</p>
                        <p style={{ color: payload[0].color }}>{fmtBRL(payload[0].value as number)}/mês</p>
                        {pool && <p className="text-muted-foreground">{pool.node_count} nodes · {pool.vm_size}</p>}
                      </div>
                    );
                  }}
                />
                <Bar dataKey="custo" name="Custo/mês" radius={[4, 4, 0, 0]} maxBarSize={40}>
                  {poolChart.map((entry, i) => (
                    <Cell key={i} fill={entry.color} />
                  ))}
                  <LabelList dataKey="nos" position="top"
                    formatter={(v: number) => `${v}n`}
                    style={{ fontSize: 10, fill: "#9ca3af" }} />
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Alertas */}
      {summary.superprovisioned_count > 0 && (
        <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <TrendingDown className="h-4 w-4 text-red-600" />
          <AlertDescription className="text-sm">
            <strong>{summary.superprovisioned_count} workloads</strong> com HPA potencialmente superprovisionado.
            Economia estimada ajustando para mínimo de replicas:{" "}
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

// Cálculo de rightsizing: sugere novo min/max baseado no uso real histórico
function calcRightsizing(w: FinOpsWorkload) {
  const avgReplicas = w.hpa_avg_replicas ?? 0;
  const maxObserved = w.hpa_max_observed ?? w.hpa_current;
  const podCostBRL = w.pods > 0 ? w.cost_share_brl / w.pods : 0;

  // Novo mínimo: avg real + 10% de margem, pelo menos 1
  const suggestedMin = Math.max(1, Math.ceil(avgReplicas * 1.1));
  // Novo máximo: pico observado + 2 réplicas de buffer
  const suggestedMax = Math.max(suggestedMin + 1, maxObserved + 2);

  // Economia mensal se aplicar o novo mínimo
  const savingMin = Math.max(0, w.hpa_min - suggestedMin) * podCostBRL;
  // Custo após ajuste (usando avg real como baseline)
  const costAfterBRL = avgReplicas > 0 ? avgReplicas * podCostBRL : w.hpa_cost_current_brl;

  return { suggestedMin, suggestedMax, savingMin, costAfterBRL, podCostBRL };
}

function HPAHistoryTab({ workloads, windowDays, timeline }: {
  workloads: FinOpsWorkload[];
  windowDays: number;
  timeline?: TimelineReport;
}) {
  const allHPAs = workloads.filter(w => w.hpa_max > 0).sort((a, b) => b.cost_share_brl - a.cost_share_brl);
  const hasHistory = allHPAs.some(w => (w.hpa_avg_replicas ?? 0) > 0);

  if (allHPAs.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-muted-foreground gap-3">
        <Activity className="h-10 w-10 opacity-20" />
        <p className="font-medium">Nenhum HPA encontrado neste cluster</p>
      </div>
    );
  }

  if (!hasHistory) {
    return (
      <div className="space-y-4">
        <Alert className="border-blue-200 bg-blue-50 dark:bg-blue-950/20">
          <Activity className="h-4 w-4 text-blue-600" />
          <AlertDescription className="text-sm">
            <strong>Análise histórica não disponível.</strong> Marque{" "}
            <strong>"Análise histórica Prometheus"</strong> no topo e clique em{" "}
            <strong>Analisar</strong> para ver: quantas vezes cada HPA escalou nos últimos{" "}
            {windowDays} dias, uso médio real de réplicas, pico de scaling e sugestões de
            rightsizing para reduzir custo.
          </AlertDescription>
        </Alert>

        {/* Tabela de snapshot atual enquanto sem histórico */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">HPAs — snapshot atual (sem histórico)</CardTitle>
          </CardHeader>
          <ScrollArea className="h-[400px]">
            <Table>
              <TableHeader>
                <TableRow className="text-[11px]">
                  <TableHead>Namespace / Workload</TableHead>
                  <TableHead className="text-center">Min</TableHead>
                  <TableHead className="text-center">Atual</TableHead>
                  <TableHead className="text-center">Max</TableHead>
                  <TableHead className="text-center">% Util.</TableHead>
                  <TableHead className="text-right">R$ se Min</TableHead>
                  <TableHead className="text-right">R$ Atual</TableHead>
                  <TableHead className="text-right">R$ se Max</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {allHPAs.map((w, i) => {
                  const util = Math.round((w.hpa_current / w.hpa_max) * 100);
                  return (
                    <TableRow key={i} className="text-xs">
                      <TableCell>
                        <p className="text-muted-foreground text-[10px] truncate">{w.namespace}</p>
                        <p className="font-medium truncate">{w.workload}</p>
                      </TableCell>
                      <TableCell className="text-center font-mono">{w.hpa_min}</TableCell>
                      <TableCell className="text-center font-mono font-semibold">{w.hpa_current}</TableCell>
                      <TableCell className="text-center font-mono">{w.hpa_max}</TableCell>
                      <TableCell className="text-center">
                        <span className={util >= 90 ? "text-red-600 font-medium" : util <= 20 ? "text-purple-600" : "text-foreground"}>
                          {util}%
                        </span>
                      </TableCell>
                      <TableCell className="text-right text-green-600 font-mono">{fmtBRL(w.hpa_cost_min_brl)}</TableCell>
                      <TableCell className="text-right font-semibold font-mono">{fmtBRL(w.hpa_cost_current_brl)}</TableCell>
                      <TableCell className="text-right text-red-500/70 font-mono">{fmtBRL(w.hpa_cost_max_brl)}</TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </ScrollArea>
        </Card>
      </div>
    );
  }

  // ── Com dados históricos do Prometheus ──────────────────────────────────────

  const hpaWithHistory = allHPAs.filter(w => (w.hpa_avg_replicas ?? 0) > 0);
  const removable = hpaWithHistory.filter(w => w.hpa_never_scaled);
  const maxedOut = hpaWithHistory.filter(w => (w.hpa_max_observed ?? 0) >= w.hpa_max);

  // Totais financeiros
  const totalCurrentCost = hpaWithHistory.reduce((s, w) => s + w.hpa_cost_current_brl, 0);
  const totalRealCost = hpaWithHistory.reduce((s, w) => s + (w.avg_replicas_cost_brl ?? w.hpa_cost_current_brl), 0);
  const totalSavingRightsizing = hpaWithHistory.reduce((s, w) => s + calcRightsizing(w).savingMin, 0);
  const totalSavingRemovable = removable.reduce((s, w) => s + (w.hpa_cost_current_brl - (w.avg_replicas_cost_brl ?? w.hpa_cost_min_brl)), 0);

  // Chart: avg réplicas vs min configurado vs max observado (top 12)
  const replicasChart = hpaWithHistory.slice(0, 12).map(w => ({
    name: w.workload.length > 16 ? w.workload.slice(0, 14) + "…" : w.workload,
    min_config: w.hpa_min,
    avg_real: parseFloat((w.hpa_avg_replicas ?? 0).toFixed(1)),
    max_obs: w.hpa_max_observed ?? w.hpa_current,
    max_config: w.hpa_max,
  }));

  // Chart: escala de eventos + custo real vs configurado
  const costChart = hpaWithHistory.slice(0, 10).map(w => ({
    name: w.workload.length > 14 ? w.workload.slice(0, 12) + "…" : w.workload,
    custo_real: parseFloat((w.avg_replicas_cost_brl ?? w.hpa_cost_current_brl).toFixed(2)),
    custo_config: parseFloat(w.hpa_cost_current_brl.toFixed(2)),
    eventos: w.hpa_scale_events ?? 0,
  }));

  return (
    <div className="space-y-4">
      {/* Cards financeiros */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <SummaryCard icon={Activity} label={`Custo Real ${windowDays}d (avg réplicas)`} color="text-green-600"
          value={fmtBRL(totalRealCost)} sub={`Config: ${fmtBRL(totalCurrentCost)}`} />
        <SummaryCard icon={TrendingDown} label="Economia c/ Rightsizing" color="text-blue-600"
          value={fmtBRL(totalSavingRightsizing)} sub="ajustando minReplicas" />
        <SummaryCard icon={Info} label="HPAs Nunca Escalaram" color="text-purple-600"
          value={String(removable.length)}
          sub={removable.length > 0 ? `Economia: ${fmtBRL(totalSavingRemovable)}` : "nenhum"} />
        <SummaryCard icon={AlertTriangle} label="HPAs no Limite Máximo" color="text-red-600"
          value={String(maxedOut.length)}
          sub={maxedOut.length > 0 ? "risco de throttling" : "nenhum"} />
      </div>

      {removable.length > 0 && (
        <Alert className="border-purple-200 bg-purple-50 dark:bg-purple-950/20">
          <Info className="h-4 w-4 text-purple-600" />
          <AlertDescription className="text-sm">
            <strong>{removable.length} HPAs nunca escalaram</strong> além do mínimo configurado nos últimos {windowDays} dias.
            São candidatos à remoção do HPA (manter replicas fixas no mínimo atual).
            Economia estimada:{" "}
            <strong className="text-purple-700 dark:text-purple-400">{fmtBRL(totalSavingRemovable)}/mês</strong>
          </AlertDescription>
        </Alert>
      )}

      {maxedOut.length > 0 && (
        <Alert className="border-yellow-200 bg-yellow-50 dark:bg-yellow-950/20">
          <AlertTriangle className="h-4 w-4 text-yellow-600" />
          <AlertDescription className="text-sm">
            <strong>{maxedOut.length} HPAs atingiram o limite máximo</strong> nos últimos {windowDays} dias.
            Considere aumentar <code>maxReplicas</code> para evitar throttling em picos.
          </AlertDescription>
        </Alert>
      )}

      {/* Charts: réplicas reais vs configuração */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">
              Réplicas: configurado vs uso real {windowDays}d
              <span className="text-[10px] text-muted-foreground font-normal ml-2">
                min_cfg / avg_real / max_obs / max_cfg
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={240}>
              <BarChart data={replicasChart} margin={{ left: 4, right: 8, top: 4, bottom: 32 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.4} />
                <XAxis dataKey="name" tick={{ fontSize: 9 }} angle={-35} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false}
                  label={{ value: "réplicas", angle: -90, position: "insideLeft", style: { fontSize: 9, fill: "#9ca3af" } }} />
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
                <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 10 }} />
                <Bar dataKey="min_config" name="min config" fill="#9ca3af" fillOpacity={0.5} radius={[2, 2, 0, 0]} maxBarSize={14} />
                <Bar dataKey="avg_real" name="avg real" fill="#10b981" radius={[2, 2, 0, 0]} maxBarSize={14} />
                <Bar dataKey="max_obs" name="max observado" fill="#f59e0b" radius={[2, 2, 0, 0]} maxBarSize={14} />
                <Bar dataKey="max_config" name="max config" fill="#ef4444" fillOpacity={0.35} radius={[2, 2, 0, 0]} maxBarSize={14} />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">
              Custo real (avg {windowDays}d) vs configurado
              <span className="text-[10px] text-muted-foreground font-normal ml-2">verde = economia real</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={240}>
              <ComposedChart data={costChart} margin={{ left: 4, right: 36, top: 4, bottom: 32 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.4} />
                <XAxis dataKey="name" tick={{ fontSize: 9 }} angle={-35} textAnchor="end"
                  axisLine={false} tickLine={false} />
                <YAxis yAxisId="custo" tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                <YAxis yAxisId="eventos" orientation="right" tick={{ fontSize: 10 }}
                  axisLine={false} tickLine={false} width={28} />
                <Tooltip
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="bg-background/95 backdrop-blur border rounded-lg shadow-lg px-3 py-2 text-xs space-y-0.5">
                        <p className="font-medium">{label}</p>
                        {payload.map((p, i) => (
                          <p key={i} style={{ color: p.color }}>
                            {p.dataKey === "eventos" ? `${p.value} scale events` : `${p.name}: ${fmtBRL(p.value as number)}`}
                          </p>
                        ))}
                      </div>
                    );
                  }}
                />
                <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 10 }} />
                <Bar yAxisId="custo" dataKey="custo_config" name="Configurado" fill="#6366f1" fillOpacity={0.3} radius={[3, 3, 0, 0]} maxBarSize={22} />
                <Bar yAxisId="custo" dataKey="custo_real" name="Real (avg)" fill="#10b981" radius={[3, 3, 0, 0]} maxBarSize={22} />
                <Line yAxisId="eventos" dataKey="eventos" name="eventos" type="monotone"
                  stroke="#f59e0b" strokeWidth={2} dot={{ r: 3 }} connectNulls />
              </ComposedChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Tabela de rightsizing */}
      <Card>
        <CardHeader className="pb-1 pt-3 px-4">
          <CardTitle className="text-sm">
            Sugestões de Rightsizing — baseado em {windowDays} dias de uso real
          </CardTitle>
        </CardHeader>
        <ScrollArea className="h-[400px]">
          <Table>
            <TableHeader>
              <TableRow className="text-[11px]">
                <TableHead>Namespace / Workload</TableHead>
                <TableHead className="text-center">Atual (min/max)</TableHead>
                <TableHead className="text-center text-blue-500">Avg {windowDays}d</TableHead>
                <TableHead className="text-center text-yellow-500">Max Obs.</TableHead>
                <TableHead className="text-center text-muted-foreground">Eventos</TableHead>
                <TableHead className="text-center text-green-600">Sugerido (min/max)</TableHead>
                <TableHead className="text-right">Custo Real/mês</TableHead>
                <TableHead className="text-right text-green-600">Economia/mês</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {hpaWithHistory.map((w, i) => {
                const { suggestedMin, suggestedMax, savingMin, costAfterBRL } = calcRightsizing(w);
                const noChange = suggestedMin === w.hpa_min && suggestedMax === w.hpa_max;
                return (
                  <TableRow key={i} className="text-xs">
                    <TableCell className="max-w-[180px]">
                      <p className="text-muted-foreground text-[10px] truncate">{w.namespace}</p>
                      <p className="font-medium truncate">{w.workload}</p>
                    </TableCell>
                    <TableCell className="text-center font-mono">
                      {w.hpa_min}/{w.hpa_max}
                    </TableCell>
                    <TableCell className="text-center font-semibold text-blue-500">
                      {w.hpa_avg_replicas?.toFixed(1)}
                    </TableCell>
                    <TableCell className="text-center">
                      <span className={(w.hpa_max_observed ?? 0) >= w.hpa_max ? "text-red-600 font-medium" : "text-yellow-600"}>
                        {w.hpa_max_observed ?? "—"}
                      </span>
                    </TableCell>
                    <TableCell className="text-center">
                      <span className={
                        w.hpa_never_scaled ? "text-purple-600 font-medium" :
                        (w.hpa_scale_events ?? 0) < 5 ? "text-yellow-600" : "text-green-600"
                      }>
                        {w.hpa_scale_events ?? 0}×
                      </span>
                    </TableCell>
                    <TableCell className="text-center">
                      {noChange ? (
                        <span className="text-muted-foreground font-mono">{suggestedMin}/{suggestedMax}</span>
                      ) : (
                        <span className="font-mono font-semibold text-green-600">
                          {suggestedMin}/{suggestedMax}
                          {suggestedMin < w.hpa_min && (
                            <span className="text-[9px] text-green-500 ml-1">↓min</span>
                          )}
                          {suggestedMax > w.hpa_max && (
                            <span className="text-[9px] text-yellow-500 ml-1">↑max</span>
                          )}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono">
                      {fmtBRL(costAfterBRL)}
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      {savingMin > 0 ? (
                        <span className="text-green-600">{fmtBRL(savingMin)}</span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {w.hpa_never_scaled
                        ? <VerdictBadge verdict="hpa_removable" />
                        : (w.hpa_max_observed ?? 0) >= w.hpa_max
                          ? <span className="text-[10px] text-red-600 font-medium">No limite máx</span>
                          : noChange
                            ? <VerdictBadge verdict="ok" />
                            : <span className="text-[10px] text-blue-600 font-medium">Ajustar min</span>}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </ScrollArea>
      </Card>

      {/* Série temporal diária — disponível quando timeline foi carregado */}
      {timeline && timeline.hpas.length > 0 && (
        <HPATimelineChart timeline={timeline} windowDays={windowDays} />
      )}
    </div>
  );
}

// ─── Linha do tempo HPA (série diária via /finops/timeline) ──────────────────

function HPATimelineChart({ timeline, windowDays }: { timeline: TimelineReport; windowDays: number }) {
  const [selected, setSelected] = useState<string>(
    timeline.hpas[0] ? `${timeline.hpas[0].namespace}/${timeline.hpas[0].workload}` : ""
  );

  const hpa = timeline.hpas.find(h => `${h.namespace}/${h.workload}` === selected);

  // Nodes co-plotados para contexto de escala do cluster
  const nodeMap = Object.fromEntries(timeline.nodes.map(n => [n.date, n.node_count]));
  const seriesWithNodes = (hpa?.series ?? []).map(p => ({
    ...p,
    date: p.date.slice(5), // "MM-DD" para economizar espaço
    nodes: nodeMap[p.date] ?? null,
  }));

  return (
    <Card>
      <CardHeader className="pb-1 pt-3 px-4 flex flex-row items-center gap-3">
        <CardTitle className="text-sm flex-1">
          Série temporal diária — últimos {windowDays} dias
        </CardTitle>
        <Select value={selected} onValueChange={setSelected}>
          <SelectTrigger className="h-7 w-[260px] text-xs">
            <SelectValue placeholder="Selecionar HPA..." />
          </SelectTrigger>
          <SelectContent>
            {timeline.hpas.map(h => (
              <SelectItem key={`${h.namespace}/${h.workload}`} value={`${h.namespace}/${h.workload}`} className="text-xs">
                {h.namespace}/{h.workload}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent className="px-2 pb-3">
        {hpa ? (
          <ResponsiveContainer width="100%" height={220}>
            <ComposedChart data={seriesWithNodes} margin={{ left: 4, right: 36, top: 8, bottom: 24 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
              <XAxis dataKey="date" tick={{ fontSize: 9 }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
              <YAxis yAxisId="rep" tick={{ fontSize: 10 }} axisLine={false} tickLine={false}
                label={{ value: "réplicas", angle: -90, position: "insideLeft", style: { fontSize: 9, fill: "#9ca3af" } }} />
              <YAxis yAxisId="nodes" orientation="right" tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={28} />
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
              {/* Linhas de configuração (min/max HPA) */}
              <ReferenceLine yAxisId="rep" y={hpa.hpa_min} stroke="#9ca3af" strokeDasharray="4 2" label={{ value: `min:${hpa.hpa_min}`, position: "insideTopLeft", fontSize: 9, fill: "#9ca3af" }} />
              <ReferenceLine yAxisId="rep" y={hpa.hpa_max} stroke="#ef4444" strokeDasharray="4 2" label={{ value: `max:${hpa.hpa_max}`, position: "insideTopLeft", fontSize: 9, fill: "#ef4444" }} />
              {/* Área de réplicas */}
              <Bar yAxisId="rep" dataKey="max_replicas" name="max" fill="#f59e0b" fillOpacity={0.25} maxBarSize={8} />
              <Line yAxisId="rep" type="monotone" dataKey="avg_replicas" name="avg" stroke="#10b981" strokeWidth={2} dot={false} />
              <Line yAxisId="rep" type="monotone" dataKey="min_replicas" name="min" stroke="#9ca3af" strokeWidth={1} strokeDasharray="3 2" dot={false} />
              {/* Nodes (eixo direito) */}
              <Line yAxisId="nodes" type="step" dataKey="nodes" name="nodes" stroke="#8b5cf6" strokeWidth={1.5} dot={false} />
            </ComposedChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-xs text-muted-foreground text-center py-8">Selecione um HPA para ver a série temporal</p>
        )}
      </CardContent>
    </Card>
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
  const [timeline, setTimeline] = useState<TimelineReport | null>(null);
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

  // Buscar série temporal quando Prometheus está ativo e o relatório foi carregado
  useEffect(() => {
    if (!report || !withPrometheus || !cluster) {
      setTimeline(null);
      return;
    }
    const token = localStorage.getItem("token") || "poc-token-123";
    fetch(`/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=${windowDays}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then(r => r.ok ? r.json() : null)
      .then((data: TimelineReport | null) => setTimeline(data))
      .catch(() => setTimeline(null));
  }, [report, withPrometheus, cluster, windowDays]);

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

          <Tabs defaultValue="overview" className="flex-1 flex flex-col min-h-0">
            <TabsList className="w-fit">
              <TabsTrigger value="overview">Visão Geral</TabsTrigger>
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
              <TabsContent value="overview" className="mt-0 h-full">
                <OverviewTab report={report} />
              </TabsContent>
              <TabsContent value="nodepools" className="mt-0 h-full">
                <NodePoolsTab pools={report.node_pools} />
              </TabsContent>
              <TabsContent value="workloads" className="mt-0 h-full">
                <WorkloadsTab workloads={report.workloads} windowDays={report.window_days || windowDays} />
              </TabsContent>
              <TabsContent value="hpa-history" className="mt-0 h-full">
                <HPAHistoryTab workloads={report.workloads} windowDays={report.window_days || windowDays} timeline={timeline ?? undefined} />
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
