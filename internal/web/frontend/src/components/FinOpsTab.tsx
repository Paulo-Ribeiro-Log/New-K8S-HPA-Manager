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
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell
} from "recharts";
import {
  DollarSign, TrendingDown, AlertTriangle, CheckCircle2,
  Loader2, RefreshCw, Server, Layers, CircleDollarSign,
  ArrowUpDown, Info, ChevronDown, ChevronUp, Download, Brain
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
  cost_share_usd: number;
  cost_share_brl: number;
  hpa_min: number;
  hpa_max: number;
  hpa_current: number;
  hpa_cost_min_brl: number;
  hpa_cost_max_brl: number;
  hpa_cost_current_brl: number;
  verdict: "superprovisioned" | "ok" | "oom_risk" | "no_request";
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
}

interface FinOpsReport {
  cluster: string;
  generated_at: string;
  exchange_rate: number;
  exchange_date: string;
  node_pools: FinOpsPool[];
  namespaces: FinOpsNamespace[];
  workloads: FinOpsWorkload[];
  summary: FinOpsSummary;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  v.toLocaleString("pt-BR", { style: "currency", currency: "BRL", maximumFractionDigits: 0 });

const fmtUSD = (v: number) =>
  v.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

const verdictConfig: Record<string, { label: string; color: string; icon: typeof CheckCircle2 }> = {
  superprovisioned: { label: "Desperdício",  color: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",    icon: TrendingDown },
  oom_risk:         { label: "Risco OOM",    color: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400", icon: AlertTriangle },
  ok:               { label: "Eficiente",    color: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",   icon: CheckCircle2 },
  no_request:       { label: "Sem Request",  color: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",    icon: Info },
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

function OverviewTab({ report }: { report: FinOpsReport }) {
  const { summary, namespaces, node_pools } = report;

  const topNs = namespaces.slice(0, 10).map(ns => ({
    name: ns.namespace.length > 22 ? ns.namespace.slice(0, 20) + "…" : ns.namespace,
    value: ns.monthly_cost_brl,
  }));

  const poolChart = node_pools.map((p, i) => ({
    name: p.name.length > 14 ? p.name.slice(0, 12) + "…" : p.name,
    value: p.monthly_cost_brl,
    color: POOL_COLORS[i % POOL_COLORS.length],
  }));

  return (
    <div className="space-y-4">
      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <SummaryCard
          icon={CircleDollarSign} label="Custo Total/mês" color="text-blue-600"
          value={fmtBRL(summary.total_monthly_cost_brl)}
          sub={fmtUSD(summary.total_monthly_cost_usd)}
        />
        <SummaryCard
          icon={TrendingDown} label="Economia HPA (se mín)" color="text-green-600"
          value={fmtBRL(summary.hpa_savings_if_min_brl)}
          sub={`${summary.superprovisioned_count} workloads`}
        />
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

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Top namespaces */}
        <Card>
          <CardHeader className="pb-2 pt-4 px-4">
            <CardTitle className="text-sm">Top Namespaces por Custo</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-4">
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={topNs} layout="vertical" margin={{ left: 8, right: 16, top: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" tickFormatter={v => `R$${(v/1000).toFixed(0)}k`} tick={{ fontSize: 10 }} />
                <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={130} />
                <Tooltip formatter={(v: number) => [fmtBRL(v), "Custo/mês"]} />
                <Bar dataKey="value" fill="#6366f1" radius={[0, 3, 3, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Node pools */}
        <Card>
          <CardHeader className="pb-2 pt-4 px-4">
            <CardTitle className="text-sm">Custo por Node Pool</CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-4">
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={poolChart} margin={{ left: 8, right: 16, top: 0, bottom: 20 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} />
                <XAxis dataKey="name" tick={{ fontSize: 10 }} angle={-30} textAnchor="end" />
                <YAxis tickFormatter={v => `R$${(v/1000).toFixed(0)}k`} tick={{ fontSize: 10 }} />
                <Tooltip formatter={(v: number) => [fmtBRL(v), "Custo/mês"]} />
                <Bar dataKey="value" radius={[3, 3, 0, 0]}>
                  {poolChart.map((entry, i) => <Cell key={i} fill={entry.color} />)}
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

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{pools.length} node pools • Total: <strong className="text-foreground">{fmtBRL(total)}/mês</strong></span>
      </div>
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

type SortKey = "cost_share_brl" | "cpu_request_millis" | "mem_request_mi" | "hpa_cost_min_brl";

function WorkloadsTab({ workloads }: { workloads: FinOpsWorkload[] }) {
  const [sortKey, setSortKey] = useState<SortKey>("cost_share_brl");
  const [sortAsc, setSortAsc] = useState(false);
  const [filterVerdict, setFilterVerdict] = useState<string>("all");

  const filtered = workloads
    .filter(w => filterVerdict === "all" || w.verdict === filterVerdict)
    .sort((a, b) => {
      const diff = a[sortKey] - b[sortKey];
      return sortAsc ? diff : -diff;
    });

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) setSortAsc(!sortAsc);
    else { setSortKey(key); setSortAsc(false); }
  };

  const SortIcon = ({ k }: { k: SortKey }) =>
    sortKey === k
      ? (sortAsc ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />)
      : <ArrowUpDown className="h-3 w-3 opacity-30" />;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 flex-wrap">
        <span className="text-sm text-muted-foreground">{filtered.length} workloads</span>
        <div className="flex gap-1">
          {["all", "superprovisioned", "ok", "no_request"].map(v => (
            <Button key={v} size="sm" variant={filterVerdict === v ? "default" : "outline"}
              className="h-6 text-[10px] px-2"
              onClick={() => setFilterVerdict(v)}>
              {v === "all" ? "Todos" : verdictConfig[v]?.label ?? v}
            </Button>
          ))}
        </div>
      </div>
      <Card>
        <ScrollArea className="h-[480px]">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Namespace</TableHead>
                <TableHead>Workload</TableHead>
                <TableHead className="text-center">Pods</TableHead>
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("cpu_request_millis")}>
                  <span className="flex items-center justify-end gap-1">CPU Req <SortIcon k="cpu_request_millis" /></span>
                </TableHead>
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("mem_request_mi")}>
                  <span className="flex items-center justify-end gap-1">Mem Req <SortIcon k="mem_request_mi" /></span>
                </TableHead>
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("cost_share_brl")}>
                  <span className="flex items-center justify-end gap-1">R$/mês <SortIcon k="cost_share_brl" /></span>
                </TableHead>
                <TableHead className="text-center">HPA min/cur/max</TableHead>
                <TableHead className="text-right cursor-pointer select-none" onClick={() => toggleSort("hpa_cost_min_brl")}>
                  <span className="flex items-center justify-end gap-1">R$ no mín <SortIcon k="hpa_cost_min_brl" /></span>
                </TableHead>
                <TableHead>Veredicto</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((w, i) => (
                <TableRow key={i} className="text-xs">
                  <TableCell className="max-w-[120px] truncate text-muted-foreground">{w.namespace}</TableCell>
                  <TableCell className="font-medium max-w-[160px] truncate">{w.workload}</TableCell>
                  <TableCell className="text-center">{w.pods}</TableCell>
                  <TableCell className="text-right font-mono">
                    {w.cpu_request_millis > 0 ? `${Math.round(w.cpu_request_millis)}m` : "—"}
                  </TableCell>
                  <TableCell className="text-right font-mono">
                    {w.mem_request_mi > 0 ? `${Math.round(w.mem_request_mi)}Mi` : "—"}
                  </TableCell>
                  <TableCell className="text-right font-semibold">{fmtBRL(w.cost_share_brl)}</TableCell>
                  <TableCell className="text-center font-mono">
                    {w.hpa_max > 0
                      ? <span>{w.hpa_min}/{w.hpa_current}/{w.hpa_max}</span>
                      : <span className="text-muted-foreground">—</span>}
                  </TableCell>
                  <TableCell className="text-right">
                    {w.hpa_min > 0 && w.hpa_min < w.hpa_current
                      ? <span className="text-green-600 font-medium">{fmtBRL(w.hpa_cost_min_brl)}</span>
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

// ─── Aba: Oportunidades ───────────────────────────────────────────────────────

function OpportunitiesTab({ workloads, summary }: { workloads: FinOpsWorkload[]; summary: FinOpsSummary }) {
  const opportunities = workloads
    .filter(w => w.verdict === "superprovisioned" || w.verdict === "no_request")
    .sort((a, b) => (b.hpa_cost_current_brl - b.hpa_cost_min_brl) - (a.hpa_cost_current_brl - a.hpa_cost_min_brl));

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
          <Alert className="border-green-200 bg-green-50 dark:bg-green-950/20">
            <TrendingDown className="h-4 w-4 text-green-600" />
            <AlertDescription className="text-sm">
              <strong>{opportunities.length} oportunidades</strong> identificadas.
              Economia potencial ajustando HPAs para mínimo:{" "}
              <strong className="text-green-700 dark:text-green-400">{fmtBRL(summary.hpa_savings_if_min_brl)}/mês</strong>
              {" "}= <strong>{fmtBRL(summary.hpa_savings_if_min_brl * 12)}/ano</strong>
            </AlertDescription>
          </Alert>

          <div className="space-y-2">
            {opportunities.map((w, i) => {
              const saving = w.hpa_cost_current_brl - w.hpa_cost_min_brl;
              return (
                <Card key={i} className="border-l-4" style={{ borderLeftColor: w.verdict === "superprovisioned" ? "#ef4444" : "#9ca3af" }}>
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
                        {w.verdict === "no_request" && (
                          <p className="text-xs text-muted-foreground mt-1">
                            Sem CPU/Mem request definido — impossível calcular custo real
                          </p>
                        )}
                      </div>
                      {saving > 0 && (
                        <div className="text-right flex-shrink-0">
                          <p className="text-xs text-muted-foreground">Economia/mês</p>
                          <p className="text-base font-bold text-green-600">{fmtBRL(saving)}</p>
                          <p className="text-xs text-muted-foreground">{fmtBRL(w.hpa_cost_current_brl)} → {fmtBRL(w.hpa_cost_min_brl)}</p>
                        </div>
                      )}
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
  const [aiAnalysis, setAiAnalysis] = useState<string | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiExpanded, setAiExpanded] = useState(true);

  const { data: report, isLoading, error, refetch } = useQuery<FinOpsReport>({
    queryKey: ["finops-report", cluster, triggerKey],
    queryFn: async () => {
      const r = await fetch(`/api/v1/finops/report?cluster=${encodeURIComponent(cluster)}`, {
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
    const header = "Namespace,Workload,Pods,CPU Request (m),Mem Request (Mi),Custo R$/mês,HPA Min,HPA Atual,HPA Max,Custo HPA Min R$,Custo HPA Max R$,Veredicto";
    const rows = report.workloads.map(w =>
      [w.namespace, w.workload, w.pods,
       Math.round(w.cpu_request_millis), Math.round(w.mem_request_mi),
       w.cost_share_brl.toFixed(2),
       w.hpa_min, w.hpa_current, w.hpa_max,
       w.hpa_cost_min_brl.toFixed(2), w.hpa_cost_max_brl.toFixed(2),
       w.verdict
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
        <div className="flex items-center gap-2">
          <Select value={cluster} onValueChange={setCluster}>
            <SelectTrigger className="w-72 h-8 text-sm">
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
              <TabsTrigger value="opportunities">
                Oportunidades
                {report.summary.superprovisioned_count > 0 && (
                  <Badge variant="destructive" className="ml-1 text-[10px]">{report.summary.superprovisioned_count}</Badge>
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
                <WorkloadsTab workloads={report.workloads} />
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
