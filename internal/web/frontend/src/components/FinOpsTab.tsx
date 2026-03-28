import { useState, useCallback } from "react";
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
  DollarSign, TrendingDown, TrendingUp, AlertTriangle, CheckCircle2,
  Loader2, RefreshCw, Server, Layers, CircleDollarSign,
  ArrowUpDown, Info, ChevronDown, ChevronUp, Download, Brain, Activity, Cpu, MemoryStick,
  GitCompare, Database, Copy, Check,
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

interface TimelineCompareResponse {
  cluster: string;
  days: number;
  current: TimelineReport;
  previous?: TimelineReport;
  has_previous: boolean;
  previous_saved_at?: string;
}

interface TimelineSnapshotMeta {
  id: string;
  cluster: string;
  start_date: string;
  end_date: string;
  days: number;
  saved_at: string;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  v.toLocaleString("pt-BR", { style: "currency", currency: "BRL", maximumFractionDigits: 0 });

const fmtUSD = (v: number) =>
  v.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

/** Formata millicores: 1500 → "1.5" (cores), 250 → "250m" */
const fmtMillis = (v: number) => v >= 1000 ? `${(v / 1000).toFixed(v % 1000 === 0 ? 0 : 1)}` : `${Math.round(v)}m`;

// ─── Recomendações concretas ──────────────────────────────────────────────────

interface Recommendation {
  lines: { text: string; highlight?: boolean }[];
  safeMin?: number;        // novo min de réplicas sugerido
  safeMax?: number;        // novo max de réplicas sugerido
  savingBRL: number;       // economia imediata estimada (réplicas atuais sendo reduzidas)
  exposureBRL: number;     // exposição de custo máximo (se escalar ao máximo configurado)
  needsPrometheus: boolean; // verdadeiro quando saving = 0 só por falta de dados
  kubectlList: string[];   // lista de comandos kubectl aplicáveis (pode ter HPA + resources)
}

function buildRecommendation(w: FinOpsWorkload, windowDays: number): Recommendation {
  const lines: { text: string; highlight?: boolean }[] = [];
  const podCostBRL = w.pods > 0 ? w.cost_share_brl / w.pods : 0;
  let safeMin: number | undefined;
  let safeMax: number | undefined;
  let savingBRL = 0;
  let needsPrometheus = false;

  // Exposição = custo se HPA escalar ao máximo configurado vs custo atual
  const exposureBRL = Math.max(0, w.hpa_cost_max_brl - w.hpa_cost_current_brl);

  // ── Caso 1: temos dados Prometheus de HPA ────────────────────────────────
  if ((w.hpa_avg_replicas ?? 0) > 0 && w.hpa_max > 0) {
    const avg    = w.hpa_avg_replicas!;
    const maxObs = w.hpa_max_observed ?? w.hpa_max;

    // Min sugerido: avg × 1.2, garantindo pelo menos 1
    const candidateMin = Math.max(1, Math.ceil(avg * 1.2));
    if (candidateMin < w.hpa_min) {
      safeMin  = candidateMin;
      savingBRL = (w.hpa_min - safeMin) * podCostBRL;
      lines.push({ text: `Reduzir min: ${w.hpa_min} → ${safeMin} réplicas`, highlight: true });
      lines.push({ text: `Média ${windowDays}d: ${avg.toFixed(1)} répl · pico real: ${maxObs}` });
      if (maxObs <= safeMin) {
        lines.push({ text: `Pico observado (${maxObs}) ≤ novo min (${safeMin}) — seguro para aplicar` });
      }
    } else if (w.verdict === "hpa_removable") {
      // Nunca escalou além do min — pode remover HPA
      safeMin = maxObs;
      lines.push({ text: `Remover HPA — fixar em ${safeMin} réplicas`, highlight: true });
      lines.push({ text: `Pico observado ${maxObs} ≤ min configurado ${w.hpa_min} em ${windowDays}d` });
      savingBRL = podCostBRL * 0.5; // overhead de gerenciamento HPA
    } else {
      // avg próxima do min — não reduzir min, mas verificar max
      lines.push({ text: `Min atual (${w.hpa_min}) já é adequado para a média observada (${avg.toFixed(1)})` });
    }

    // Max desnecessariamente alto vs pico real
    if (maxObs > 0 && maxObs < w.hpa_max * 0.6) {
      safeMax = Math.ceil(maxObs * 1.3);
      if (safeMax < w.hpa_max) {
        lines.push({ text: `Reduzir max: ${w.hpa_max} → ${safeMax} (pico foi ${maxObs}, +30% buffer)` });
      }
    }

  // ── Caso 2: sem Prometheus, workload JÁ está no mínimo (atual == min) ────
  } else if (w.hpa_max > 0 && w.hpa_min > 0 && w.hpa_current <= w.hpa_min) {
    needsPrometheus = true;
    const ratio = Math.round((w.hpa_current / w.hpa_max) * 100);
    lines.push({ text: `Rodando no mínimo configurado (${w.hpa_min} de ${w.hpa_max} max = ${ratio}% do teto)`, highlight: true });
    // Sugerir redução do max baseado em heurística 2× o atual
    const heuristicMax = Math.max(w.hpa_min + 1, w.hpa_current * 3);
    if (heuristicMax < w.hpa_max) {
      safeMax = heuristicMax;
      lines.push({ text: `Reduzir max de ${w.hpa_max} → ${heuristicMax} (3× o uso atual) para limitar exposição` });
    }
    lines.push({ text: `Ative "Usar Prometheus" para ver histórico real e recomendar min seguro` });

  // ── Caso 3: sem Prometheus, workload ACIMA do mínimo ─────────────────────
  } else if (w.hpa_max > 0 && w.hpa_current > w.hpa_min) {
    needsPrometheus = true;
    // Usar hpa_current como proxy — saving se reduzir min para atual
    if (w.hpa_min > w.hpa_current) {
      safeMin = w.hpa_current;
      savingBRL = (w.hpa_min - safeMin) * podCostBRL;
      lines.push({ text: `Reduzir min: ${w.hpa_min} → ${w.hpa_current} (já rodando com ${w.hpa_current})`, highlight: true });
    } else {
      const ratio = Math.round((w.hpa_current / w.hpa_max) * 100);
      lines.push({ text: `Rodando em ${ratio}% do max configurado (${w.hpa_current}/${w.hpa_max})`, highlight: true });
      lines.push({ text: `Ative "Usar Prometheus" para recomendar novo min baseado em histórico` });
    }
  }

  // ── CPU request muito acima do recomendado (Prometheus) ──────────────────
  if (w.cpu_recommended_millis && w.cpu_request_millis &&
      w.cpu_recommended_millis < w.cpu_request_millis * 0.85) {
    const pct = Math.round((1 - w.cpu_recommended_millis / w.cpu_request_millis) * 100);
    lines.push({
      text: `CPU request: ${fmtMillis(w.cpu_request_millis)} → ${fmtMillis(w.cpu_recommended_millis)} (-${pct}%, P95=${fmtMillis(w.cpu_p95_millis ?? 0)})`,
      highlight: !safeMin,
    });
  }

  // ── Mem request muito acima do recomendado (Prometheus) ──────────────────
  if (w.mem_recommended_mi && w.mem_request_mi &&
      w.mem_recommended_mi < w.mem_request_mi * 0.85) {
    const pct = Math.round((1 - w.mem_recommended_mi / w.mem_request_mi) * 100);
    lines.push({
      text: `Mem request: ${fmtMi(w.mem_request_mi)} → ${fmtMi(w.mem_recommended_mi)} (-${pct}%, P95=${fmtMi(w.mem_p95_mi ?? 0)})`,
      highlight: !safeMin && lines.length === 0,
    });
  }

  // ── OOM Risk ─────────────────────────────────────────────────────────────
  if (w.verdict === "oom_risk") {
    if (w.cpu_recommended_millis && w.cpu_request_millis)
      lines.push({ text: `CPU request: ${fmtMillis(w.cpu_request_millis)} → ${fmtMillis(w.cpu_recommended_millis)} (P95 ≥ 95% do request!)`, highlight: true });
    if (w.mem_recommended_mi && w.mem_request_mi)
      lines.push({ text: `Mem request: ${fmtMi(w.mem_request_mi)} → ${fmtMi(w.mem_recommended_mi)} (P95 ≥ 95% do request!)`, highlight: true });
    if (lines.length === 0)
      lines.push({ text: "Aumentar CPU/Mem request — risco real de throttling/OOM", highlight: true });
  }

  // ── Sem requests ─────────────────────────────────────────────────────────
  if (w.verdict === "no_request") {
    lines.push({ text: "Definir resource requests (CPU + Mem)", highlight: true });
    lines.push({ text: "Sem requests: scheduler não garante recursos — custo estimado por heurística" });
  }

  // ── kubectl: comandos HPA ─────────────────────────────────────────────────
  const kubectlList: string[] = [];

  if (safeMin !== undefined && safeMin < w.hpa_min && safeMax !== undefined && safeMax < w.hpa_max) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"minReplicas":${safeMin},"maxReplicas":${safeMax}}}'`
    );
  } else if (safeMin !== undefined && safeMin < w.hpa_min) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"minReplicas":${safeMin}}}'`
    );
  } else if (safeMax !== undefined && safeMax < w.hpa_max) {
    kubectlList.push(
      `kubectl patch hpa ${w.workload} -n ${w.namespace} -p '{"spec":{"maxReplicas":${safeMax}}}'`
    );
  }

  // ── kubectl: set resources (CPU e/ou Mem) ─────────────────────────────────
  const hasCpuRec = w.cpu_recommended_millis && w.cpu_request_millis &&
    Math.abs(w.cpu_recommended_millis - w.cpu_request_millis) / w.cpu_request_millis > 0.15;
  const hasMemRec = w.mem_recommended_mi && w.mem_request_mi &&
    Math.abs(w.mem_recommended_mi - w.mem_request_mi) / w.mem_request_mi > 0.15;

  if (hasCpuRec || hasMemRec) {
    const parts: string[] = [];
    if (hasCpuRec) parts.push(`cpu=${Math.round(w.cpu_recommended_millis!)}m`);
    if (hasMemRec) parts.push(`memory=${Math.round(w.mem_recommended_mi!)}Mi`);
    kubectlList.push(
      `kubectl set resources deployment ${w.workload} -n ${w.namespace} --requests=${parts.join(",")}`
    );
  }

  // waste_brl Prometheus é mais preciso que estimativa HPA
  if ((w.waste_brl ?? 0) > savingBRL) savingBRL = w.waste_brl!;

  return { lines, safeMin, safeMax, savingBRL, exposureBRL, needsPrometheus, kubectlList };
}

const verdictConfig: Record<string, { label: string; color: string; fill: string; icon: typeof CheckCircle2 }> = {
  superprovisioned: { label: "Desperdício",    fill: "#ef4444", color: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",       icon: TrendingDown },
  oom_risk:         { label: "Risco OOM",      fill: "#f59e0b", color: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400", icon: AlertTriangle },
  ok:               { label: "Eficiente",      fill: "#10b981", color: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",   icon: CheckCircle2 },
  no_request:       { label: "Sem Request",    fill: "#9ca3af", color: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",       icon: Info },
  hpa_removable:    { label: "Remover HPA",    fill: "#8b5cf6", color: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400", icon: Info },
};

const POOL_COLORS = ["#6366f1", "#8b5cf6", "#06b6d4", "#10b981", "#f59e0b", "#ef4444", "#ec4899"];

// ─── Componentes Auxiliares ───────────────────────────────────────────────────

/** Exibe um comando kubectl com botão de copiar */
function KubectlBlock({ cmd }: { cmd: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(cmd).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [cmd]);
  return (
    <div className="flex items-center gap-1.5">
      <code className="flex-1 text-[10px] font-mono bg-background border rounded px-2 py-1 break-all">
        {cmd}
      </code>
      <button
        onClick={handleCopy}
        title={copied ? "Copiado!" : "Copiar"}
        className="shrink-0 p-1 rounded hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
      >
        {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}

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


const fmtMi = (v: number) => v >= 1024 ? `${(v / 1024).toFixed(1)}Gi` : `${Math.round(v)}Mi`;

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

  // ── Cost per node per day ────────────────────────────────────────────────────
  const totalNodes = node_pools.reduce((s, p) => s + p.node_count, 0);
  const costPerNodePerDay = totalNodes > 0 ? summary.total_monthly_cost_brl / 30 / totalNodes : 0;

  // ── Global efficiency metrics ────────────────────────────────────────────────
  const promWorkloads = workloads.filter(w => w.cpu_avg_millis && (w.cpu_request_millis ?? 0) > 0);
  const sumCpuUsed = promWorkloads.reduce((s, w) => s + (w.cpu_avg_millis ?? 0) * w.pods, 0);
  const sumCpuReq  = promWorkloads.reduce((s, w) => s + w.cpu_request_millis * w.pods, 0);
  const cpuEffGlobal = sumCpuReq > 0 ? Math.round(sumCpuUsed / sumCpuReq * 100) : null;

  const promMemWorkloads = workloads.filter(w => w.mem_avg_mi && (w.mem_request_mi ?? 0) > 0);
  const sumMemUsed = promMemWorkloads.reduce((s, w) => s + (w.mem_avg_mi ?? 0) * w.pods, 0);
  const sumMemReq  = promMemWorkloads.reduce((s, w) => s + w.mem_request_mi * w.pods, 0);
  const memEffGlobal = sumMemReq > 0 ? Math.round(sumMemUsed / sumMemReq * 100) : null;

  const hpaWkl = workloads.filter(w => w.hpa_max > 0);
  const hpaEff = hpaWkl.length > 0
    ? Math.round(hpaWkl.reduce((s, w) => s + w.hpa_current, 0) / hpaWkl.reduce((s, w) => s + w.hpa_max, 0) * 100)
    : null;

  const totalSavings = summary.potential_savings_brl > 0
    ? summary.potential_savings_brl
    : summary.hpa_savings_if_min_brl;

  // ── Main timeline: custo diário + eficiência % ───────────────────────────────
  type MainPoint = { date: string; custo: number | null; cpuEff: number | null; memEff: number | null; nodes: number | null };
  const mainByDate = new Map<string, MainPoint>();

  (tl?.nodes ?? []).forEach(n => {
    const date = n.date.slice(5);
    mainByDate.set(date, {
      date,
      custo: Math.round(n.node_count * costPerNodePerDay),
      nodes: n.node_count,
      cpuEff: null,
      memEff: null,
    });
  });
  (tl?.cpu ?? []).forEach(p => {
    const date = p.date.slice(5);
    const existing = mainByDate.get(date) ?? { date, custo: null, nodes: null, cpuEff: null, memEff: null };
    existing.cpuEff = p.req_millis > 0 ? Math.round(p.used_millis / p.req_millis * 100) : null;
    mainByDate.set(date, existing);
  });
  (tl?.mem ?? []).forEach(p => {
    const date = p.date.slice(5);
    const existing = mainByDate.get(date) ?? { date, custo: null, nodes: null, cpuEff: null, memEff: null };
    existing.memEff = p.req_mi > 0 ? Math.round(p.used_mi / p.req_mi * 100) : null;
    mainByDate.set(date, existing);
  });
  const mainTimeline = [...mainByDate.values()].sort((a, b) => a.date.localeCompare(b.date));
  const hasEfficiency = mainTimeline.some(p => p.cpuEff !== null);

  // ── Namespace breakdown (stacked by verdict) ─────────────────────────────────
  // IMPORTANTE: total e wastePct ficam fora do objeto de dados do Recharts para não
  // expandir o domínio do eixo X além da soma das barras empilhadas (causaria barra branca vazia)
  const nsBreakdownRaw = namespaces.slice(0, 9).map(ns => {
    const nswl = workloads.filter(w => w.namespace === ns.namespace);
    let eficiente = 0, desperdicio = 0, risco = 0, sem_req = 0;
    nswl.forEach(w => {
      if      (w.verdict === "ok" || w.verdict === "hpa_removable") eficiente  += w.cost_share_brl;
      else if (w.verdict === "superprovisioned")                    desperdicio += w.cost_share_brl;
      else if (w.verdict === "oom_risk")                            risco       += w.cost_share_brl;
      else                                                          sem_req     += w.cost_share_brl;
    });
    const total = ns.monthly_cost_brl;
    const wastePct = total > 0 ? Math.round((desperdicio + risco) / total * 100) : 0;
    return { ns: ns.namespace, eficiente, desperdicio, risco, sem_req, total, wastePct };
  }).sort((a, b) => (b.desperdicio + b.risco) - (a.desperdicio + a.risco));

  // Dados para o Recharts — sem campos numéricos extras
  const nsBreakdown = nsBreakdownRaw.map(({ eficiente, desperdicio, risco, sem_req, ns }) => ({
    name: ns.length > 22 ? ns.slice(0, 20) + "…" : ns,
    eficiente, desperdicio, risco, sem_req,
  }));
  // Mapa auxiliar para o tooltip (total e wastePct)
  const nsExtraMap = new Map(nsBreakdownRaw.map(r => [
    r.ns.length > 22 ? r.ns.slice(0, 20) + "…" : r.ns,
    { total: r.total, wastePct: r.wastePct },
  ]));

  // ── Opportunities table ───────────────────────────────────────────────────────
  const windowDays = report.window_days || 30;
  const opportunities = workloads
    .map(w => {
      const rec = buildRecommendation(w, windowDays);
      // Prioridade: waste_brl (Prometheus) > estimativa HPA > fallback
      const saving = rec.savingBRL > 0
        ? rec.savingBRL
        : w.verdict === "superprovisioned" ? w.hpa_cost_current_brl - w.hpa_cost_min_brl
        : w.verdict === "hpa_removable"    ? w.hpa_cost_current_brl - w.hpa_cost_min_brl
        : 0;
      return { ...w, saving, rec };
    })
    .filter(w => w.saving > 5 || (w.verdict !== "ok" && w.cost_share_brl > 50))
    .sort((a, b) => b.saving - a.saving)
    .slice(0, 12);

  const opTotalSaving = opportunities.reduce((s, o) => s + o.saving, 0);

  // ── HPA top-8 for chart ───────────────────────────────────────────────────────
  const top8HPAs = (tl?.hpas ?? [])
    .map(h => ({ ...h, maxObs: Math.max(...h.series.map(p => p.max_replicas), 0) }))
    .sort((a, b) => b.maxObs - a.maxObs).slice(0, 8);

  const hpaAllDates = [...new Set((tl?.hpas ?? []).flatMap(h => h.series.map(p => p.date)))].sort();
  const hpaChartData = hpaAllDates.map(date => {
    const entry: Record<string, number | string> = { date: date.slice(5) };
    top8HPAs.forEach((h, i) => {
      const pt = h.series.find(p => p.date === date);
      entry[`h${i}`] = pt ? Math.round(pt.avg_replicas) : 0;
    });
    return entry;
  });

  // ── Nodes ─────────────────────────────────────────────────────────────────────
  const nodesData = (tl?.nodes ?? []).map(n => ({
    date: n.date.slice(5),
    Nodes: n.node_count,
    custo: Math.round(n.node_count * costPerNodePerDay),
  }));

  return (
    <div className="space-y-4">
      {/* ── 1. KPI cards (6) ──────────────────────────────────────────────── */}
      <div className="grid grid-cols-3 md:grid-cols-6 gap-2">
        {/* Custo/mês */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo/mês</p>
            <p className="text-lg font-bold text-blue-600 leading-tight">{fmtBRL(summary.total_monthly_cost_brl)}</p>
            <p className="text-[10px] text-muted-foreground">{fmtUSD(summary.total_monthly_cost_usd)}</p>
          </CardContent>
        </Card>
        {/* Custo/dia */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo/dia (médio)</p>
            <p className="text-lg font-bold text-blue-400 leading-tight">{fmtBRL(summary.total_monthly_cost_brl / 30)}</p>
            <p className="text-[10px] text-muted-foreground">{totalNodes} nodes · {node_pools.length} pools</p>
          </CardContent>
        </Card>
        {/* Eficiência CPU */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Eficiência CPU</p>
            {cpuEffGlobal !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${cpuEffGlobal < 35 ? "text-red-500" : cpuEffGlobal < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {cpuEffGlobal}%
                </p>
                <p className="text-[10px] text-muted-foreground">uso real / request</p>
              </>
            ) : hpaEff !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${hpaEff < 35 ? "text-red-500" : hpaEff < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {hpaEff}%
                </p>
                <p className="text-[10px] text-muted-foreground">HPA cur/max (proxy)</p>
              </>
            ) : (
              <p className="text-lg font-bold leading-tight text-muted-foreground">—</p>
            )}
          </CardContent>
        </Card>
        {/* Eficiência Mem */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Eficiência Mem</p>
            {memEffGlobal !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${memEffGlobal < 35 ? "text-red-500" : memEffGlobal < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {memEffGlobal}%
                </p>
                <p className="text-[10px] text-muted-foreground">uso real / request</p>
              </>
            ) : (
              <p className="text-lg font-bold leading-tight text-muted-foreground">—</p>
            )}
          </CardContent>
        </Card>
        {/* Desperdício */}
        <Card className="col-span-1 border-red-200 dark:border-red-900/40">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Desperdício/mês</p>
            <p className="text-lg font-bold text-red-500 leading-tight">{fmtBRL(totalSavings)}</p>
            <p className="text-[10px] text-muted-foreground">
              {summary.total_monthly_cost_brl > 0 ? Math.round(totalSavings / summary.total_monthly_cost_brl * 100) : 0}% do custo total
            </p>
          </CardContent>
        </Card>
        {/* Câmbio */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">USD/BRL</p>
            <p className="text-lg font-bold text-orange-500 leading-tight">R$ {report.exchange_rate.toFixed(2)}</p>
            <p className="text-[10px] text-muted-foreground">{report.exchange_date}</p>
          </CardContent>
        </Card>
      </div>

      {/* ── 2. Window selector ──────────────────────────────────────────────── */}
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
            {!hasEfficiency && <span className="text-yellow-600 ml-2">⚠ Ative Prometheus para eficiência real</span>}
          </span>
        )}
      </div>

      {/* ── 3. Chart central: Custo Diário + Eficiência % ────────────────────── */}
      <Card>
        <CardHeader className="pb-1 pt-3 px-4">
          <CardTitle className="text-sm flex items-center gap-2">
            <CircleDollarSign className="h-4 w-4 text-amber-500" />
            Custo Diário × Eficiência de Uso
            <span className="text-[10px] font-normal text-muted-foreground">
              {hasEfficiency ? "barras = custo R$/dia · linhas = % do request realmente utilizado" : "barras = custo estimado R$/dia (baseado em contagem de nodes)"}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent className="px-2 pb-3">
          {mainTimeline.length === 0 ? (
            <div className="flex items-center justify-center h-[210px] text-xs text-muted-foreground gap-2">
              {tlLoading ? <><Loader2 className="h-4 w-4 animate-spin" />Consultando Prometheus…</> : <><Activity className="h-4 w-4 opacity-30" />Sem dados de timeline (Prometheus inacessível)</>}
            </div>
          ) : (
            <>
            {hasEfficiency && (
              <div className="flex items-center gap-4 px-3 mb-1 flex-wrap">
                <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <span className="inline-block w-5 h-0.5 bg-indigo-500 rounded" />CPU utilizado % (real/request)
                </span>
                <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <span className="inline-block w-5 h-0.5 bg-emerald-500 rounded" />Mem utilizado % (real/request)
                </span>
                <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <span className="inline-block w-5 border-t-2 border-dashed border-red-400" />35% — alerta desperdício
                </span>
                <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <span className="inline-block w-5 border-t-2 border-dashed border-green-500 opacity-60" />70% — uso saudável
                </span>
                <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <span className="inline-block w-5 border-t-2 border-amber-400" />100% — limite do request (acima = risco OOM)
                </span>
              </div>
            )}
            <ResponsiveContainer width="100%" height={hasEfficiency ? 190 : 170}>
              <ComposedChart data={mainTimeline} margin={{ left: 8, right: hasEfficiency ? 44 : 8, top: 4, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                <YAxis yAxisId="cost" tickFormatter={v => `R$${v >= 1000 ? (v/1000).toFixed(1)+"k" : v}`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={60} />
                {hasEfficiency && (
                  <YAxis yAxisId="eff" orientation="right" unit="%" allowDataOverflow={false}
                    tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} width={40} />
                )}
                <Tooltip content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  const custo = payload.find(p => p.name === "custo");
                  const cpuE  = payload.find(p => p.name === "cpuEff");
                  const memE  = payload.find(p => p.name === "memEff");
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 4 }}>{label}</p>
                      {custo && <p style={{ color: "#f59e0b" }}>Custo estimado: {fmtBRL(custo.value as number)}</p>}
                      {cpuE  && <p style={{ color: "#6366f1" }}>CPU utilizado: {cpuE.value}% do request{(cpuE.value as number) > 100 ? " ⚠ over-request!" : ""}</p>}
                      {memE  && <p style={{ color: "#10b981" }}>Mem utilizada: {memE.value}% do request{(memE.value as number) > 100 ? " ⚠ risco OOM!" : ""}</p>}
                      {cpuE && (cpuE.value as number) < 35 && <p style={{ color: "#f87171", fontWeight: 600 }}>⚠ CPU abaixo de 35% — alto desperdício</p>}
                    </div>
                  );
                }} />
                <Bar yAxisId="cost" dataKey="custo" name="custo" fill="#f59e0b" fillOpacity={0.65} radius={[2,2,0,0]} maxBarSize={16} />
                {hasEfficiency && <>
                  <Line yAxisId="eff" dataKey="cpuEff" name="cpuEff" type="monotone"
                    stroke="#6366f1" strokeWidth={2} dot={false} connectNulls legendType="none" />
                  <Line yAxisId="eff" dataKey="memEff" name="memEff" type="monotone"
                    stroke="#10b981" strokeWidth={2} dot={false} connectNulls legendType="none" />
                  <ReferenceLine yAxisId="eff" y={35} stroke="#ef4444" strokeDasharray="5 3" strokeOpacity={0.6}
                    label={{ value: "35%", position: "insideTopLeft", fontSize: 9, fill: "#ef4444" }} />
                  <ReferenceLine yAxisId="eff" y={70} stroke="#22c55e" strokeDasharray="5 3" strokeOpacity={0.5}
                    label={{ value: "70%", position: "insideTopLeft", fontSize: 9, fill: "#22c55e" }} />
                  <ReferenceLine yAxisId="eff" y={100} stroke="#f59e0b" strokeOpacity={0.7}
                    label={{ value: "100%", position: "insideTopLeft", fontSize: 9, fill: "#f59e0b" }} />
                </>}
              </ComposedChart>
            </ResponsiveContainer>
            </>
          )}
        </CardContent>
      </Card>

      {/* ── 4. Namespace breakdown | Opportunities ──────────────────────────── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Namespace: custo com breakdown de veredicto */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Layers className="h-4 w-4 text-purple-500" />
              Custo por Namespace — distribuição de saúde
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={Math.max(160, nsBreakdown.length * 28 + 20)}>
              <BarChart data={nsBreakdown} layout="vertical" margin={{ left: 6, right: 70, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.3} />
                <XAxis type="number"
                  tickFormatter={v => v === 0 ? "R$0" : v >= 1000 ? `R$${(v/1000).toFixed(0)}k` : `R$${Math.round(v)}`}
                  tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} />
                <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={130} axisLine={false} tickLine={false} />
                <Tooltip cursor={{ fill: "rgba(100,100,100,0.1)" }} content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  const extra = nsExtraMap.get(label as string);
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 4 }}>{label}</p>
                      <p style={{ color: "var(--foreground)" }}>Total: {extra ? fmtBRL(extra.total) : "—"}/mês</p>
                      {payload.map((p, i) => (p.value as number) > 0 && (
                        <p key={i} style={{ color: p.color }}>{p.name}: {fmtBRL(p.value as number)}</p>
                      ))}
                      {extra && extra.wastePct > 0 && (
                        <p style={{ color: "#f87171", fontWeight: 600, marginTop: 4 }}>{extra.wastePct}% potencialmente desperdiçado</p>
                      )}
                    </div>
                  );
                }} />
                <Bar dataKey="eficiente"   name="Eficiente"   stackId="a" fill="#10b981" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="desperdicio" name="Desperdício"  stackId="a" fill="#ef4444" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="risco"       name="Risco OOM"   stackId="a" fill="#f59e0b" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="sem_req"     name="Sem Request"  stackId="a" fill="#9ca3af" fillOpacity={0.8} maxBarSize={20} radius={[0,3,3,0]} />

              </BarChart>
            </ResponsiveContainer>
            <div className="flex gap-3 mt-1 px-2 flex-wrap">
              {[["#10b981","Eficiente"],["#ef4444","Desperdício"],["#f59e0b","Risco OOM"],["#9ca3af","Sem Request"]].map(([c, l]) => (
                <span key={l} className="flex items-center gap-1 text-[10px] text-muted-foreground">
                  <span className="w-2 h-2 rounded-sm inline-block" style={{background: c}} />{l}
                </span>
              ))}
            </div>
          </CardContent>
        </Card>

        {/* Tabela de Oportunidades */}
        <Card className="border-amber-200 dark:border-amber-900/40">
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center justify-between">
              <span className="flex items-center gap-2">
                <TrendingDown className="h-4 w-4 text-red-500" />
                Oportunidades de Economia
                {windowDays > 0 && (
                  <span className="text-[10px] font-normal text-muted-foreground">({windowDays}d de histórico Prometheus)</span>
                )}
              </span>
              {opTotalSaving > 0 && (
                <span className="text-[10px] font-semibold text-green-600 dark:text-green-400">
                  Potencial total: {fmtBRL(opTotalSaving)}/mês
                </span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            {opportunities.length === 0 ? (
              <div className="flex items-center justify-center py-8 text-xs text-muted-foreground gap-2">
                <CheckCircle2 className="h-4 w-4 text-green-500" />
                Nenhuma oportunidade significativa identificada
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow className="text-[10px] text-muted-foreground">
                    <TableHead className="pl-4">Workload</TableHead>
                    <TableHead>Recomendação concreta</TableHead>
                    <TableHead className="text-right">Custo atual</TableHead>
                    <TableHead className="text-right text-green-600">Economia/mês</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {opportunities.map((w, i) => (
                    <TableRow key={i} className={`text-xs align-top ${
                      w.verdict === "superprovisioned" ? "bg-red-50/40 dark:bg-red-950/10" :
                      w.verdict === "oom_risk"         ? "bg-yellow-50/40 dark:bg-yellow-950/10" :
                      w.verdict === "hpa_removable"    ? "bg-purple-50/40 dark:bg-purple-950/10" : ""
                    }`}>
                      <TableCell className="pl-4 py-2 min-w-[130px]">
                        <p className="font-medium truncate max-w-[160px]">{w.workload}</p>
                        <p className="text-[10px] text-muted-foreground truncate max-w-[160px]">{w.namespace}</p>
                        <div className="mt-1">
                          <VerdictBadge verdict={w.verdict} />
                        </div>
                        {/* HPA config atual */}
                        {w.hpa_max > 0 && (
                          <p className="text-[10px] text-muted-foreground mt-1 font-mono">
                            HPA: {w.hpa_min}↔{w.hpa_current}↔{w.hpa_max}
                            {w.hpa_avg_replicas ? ` · avg ${w.hpa_avg_replicas.toFixed(1)}` : ""}
                          </p>
                        )}
                      </TableCell>
                      <TableCell className="py-2 max-w-[340px]">
                        <div className="space-y-0.5">
                          {w.rec.lines.map((line, li) => (
                            <p key={li} className={`text-[11px] leading-snug ${
                              line.highlight
                                ? "font-semibold text-foreground"
                                : "text-muted-foreground"
                            }`}>
                              {line.highlight && (
                                <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-500 mr-1.5 mb-px" />
                              )}
                              {line.text}
                            </p>
                          ))}
                          {(w.rec.kubectlList ?? []).length > 0 && (
                            <p className="text-[10px] font-mono bg-muted/50 rounded px-1.5 py-0.5 mt-1 truncate text-muted-foreground" title={w.rec.kubectlList[0]}>
                              $ {w.rec.kubectlList[0]}
                            </p>
                          )}
                          {w.rec.safeMin !== undefined && w.hpa_min > 0 && (
                            <p className="text-[10px] text-green-600 dark:text-green-400 font-medium mt-0.5">
                              Economia estimada: {fmtBRL((w.hpa_min - w.rec.safeMin) * (w.pods > 0 ? w.cost_share_brl / w.pods : 0))}/mês por réplica removida
                            </p>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="text-right font-mono text-[11px] py-2 whitespace-nowrap">
                        {fmtBRL(w.cost_share_brl)}
                        {w.hpa_cost_min_brl > 0 && w.hpa_cost_min_brl < w.cost_share_brl && (
                          <p className="text-[10px] text-muted-foreground">
                            mín: {fmtBRL(w.hpa_cost_min_brl)}
                          </p>
                        )}
                      </TableCell>
                      <TableCell className="text-right py-2 whitespace-nowrap">
                        {w.saving > 0
                          ? (
                            <div>
                              <span className="text-green-600 font-bold text-sm">-{fmtBRL(w.saving)}</span>
                              <p className="text-[10px] text-muted-foreground">/ano: -{fmtBRL(w.saving * 12)}</p>
                            </div>
                          )
                          : <span className="text-muted-foreground font-mono text-[11px]">—</span>}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {opTotalSaving > 0 && (
              <div className="px-4 py-2 border-t bg-green-50/50 dark:bg-green-950/10 flex justify-between items-center">
                <span className="text-[10px] text-muted-foreground">Aplicando todas as recomendações acima:</span>
                <span className="text-xs font-bold text-green-600">
                  -{fmtBRL(opTotalSaving)}/mês · -{fmtBRL(opTotalSaving * 12)}/ano
                </span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* ── 5. HPA | Nodes ──────────────────────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
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
              <div className="flex items-center justify-center h-[170px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem HPAs com histórico (Prometheus)"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={170}>
                <ComposedChart data={hpaChartData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                        <p className="font-medium mb-1">{label}</p>
                        {payload.filter(p => (p.value as number) > 0).map((p, i) => {
                          const idx = parseInt((p.dataKey as string).replace("h",""));
                          return <p key={i} style={{ color: p.color }}>{top8HPAs[idx]?.workload}: {p.value} répl.</p>;
                        })}
                      </div>
                    );
                  }} />
                  <Legend iconType="plainline" iconSize={12} wrapperStyle={{ fontSize: 9, paddingTop: 4 }}
                    formatter={(_v: unknown, entry: { dataKey?: unknown }) => {
                      const idx = parseInt(((entry.dataKey as string) ?? "").replace("h",""));
                      return top8HPAs[idx]?.workload ?? String(entry.dataKey ?? "");
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

        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Server className="h-4 w-4 text-cyan-500" />
              Nodes Ready
              <span className="text-[10px] font-normal text-muted-foreground">custo estimado no tooltip</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {nodesData.length === 0 ? (
              <div className="flex items-center justify-center h-[170px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Server className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem dados Prometheus"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={170}>
                <ComposedChart data={nodesData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    const nodes = payload[0]?.value as number;
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                        <p className="font-medium">{label}</p>
                        <p className="text-cyan-500">{nodes} nodes</p>
                        <p className="text-amber-500">≈ {fmtBRL(nodes * costPerNodePerDay)}/dia</p>
                        <p className="text-muted-foreground">≈ {fmtBRL(nodes * costPerNodePerDay * 30)}/mês projetado</p>
                      </div>
                    );
                  }} />
                  <Area type="stepAfter" dataKey="Nodes" fill="#06b6d4" stroke="#06b6d4"
                    fillOpacity={0.15} strokeWidth={2} dot={false} />
                  <ReferenceLine y={totalNodes} stroke="#f59e0b" strokeDasharray="4 2" strokeOpacity={0.7}
                    label={{ value: `atual: ${totalNodes}`, position: "right", fontSize: 9, fill: "#f59e0b" }} />
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>
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
            <BarChart data={nsChart} layout="vertical" margin={{ left: 4, right: 52, top: 0, bottom: 0 }}>
              <XAxis type="number" hide />
              <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={120} axisLine={false} tickLine={false} />
              <Tooltip
                cursor={{ fill: "rgba(100,100,100,0.1)" }}
                content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 2 }}>{label}</p>
                      <p style={{ color: (payload[0]?.payload as { color?: string })?.color }}>Custo/mês: {fmtBRL(payload[0]?.value as number)}</p>
                    </div>
                  );
                }}
              />
              <Bar dataKey="value" radius={[0, 4, 4, 0]} maxBarSize={14}>
                {nsChart.map((e, i) => <Cell key={i} fill={e.color} />)}
                <LabelList dataKey="value" position="right"
                  formatter={(v: number) => v >= 1000 ? `R$${(v / 1000).toFixed(0)}k` : `R$${Math.round(v)}`}
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

interface HPARec {
  label: string;
  detail: string;   // detalhe contextual com valores concretos
  color: string;
  bg: string;
}

function hpaRecommendation(
  avgUtil: number,
  daysAtMax: number,
  daysAtMin: number,
  totalDays: number,
  hpaMin: number,
  hpaMax: number,
  histMax: number,   // pico real observado nas séries
  histAvg: number,   // média real de réplicas
): HPARec {
  // HPA estático: min=max → nunca escala, não faz sentido analisar padrões de scaling
  if (hpaMin === hpaMax) {
    return {
      label: "HPA estático (min=max)",
      detail: `min e max fixos em ${hpaMin} réplica${hpaMin !== 1 ? "s" : ""} — considere remover o HPA`,
      color: "text-yellow-600",
      bg: "bg-yellow-50 dark:bg-yellow-950/20",
    };
  }
  if (daysAtMax >= Math.ceil(totalDays * 0.1)) {
    // Hit o limite em >= 10% dos dias: sugere novo max = hpaMax + 50% (arredondado para cima)
    const suggested = hpaMax + Math.max(1, Math.ceil(hpaMax * 0.5));
    return {
      label: "Aumentar maxReplicas",
      detail: `Atingiu max (${hpaMax}) em ${daysAtMax}d → sugerido: max=${suggested}`,
      color: "text-red-500",
      bg: "bg-red-50 dark:bg-red-950/20",
    };
  }
  if (daysAtMin >= Math.ceil(totalDays * 0.8)) {
    // Ficou no mínimo >= 80% dos dias: candidato a remover HPA
    return {
      label: "Remover HPA / fixar min",
      detail: `${daysAtMin}d no mínimo (${hpaMin}) — pico observado: ${histMax} rep.`,
      color: "text-purple-500",
      bg: "bg-purple-50 dark:bg-purple-950/20",
    };
  }
  if (avgUtil < 25 && hpaMin > 1) {
    // Utilização média baixa: sugere novo min = pico observado + 10% de margem, mín. 1
    const suggested = Math.max(1, Math.ceil(histMax * 1.1));
    const saving = hpaMin - suggested;
    // Só recomenda redução se o valor sugerido for de fato menor que o mínimo atual
    if (saving > 0) {
      return {
        label: "Reduzir minReplicas",
        detail: `Pico: ${histMax} rep. | avg: ${histAvg.toFixed(1)} → min atual ${hpaMin} → sugerido: ${suggested} (−${saving} réplicas)`,
        color: "text-blue-500",
        bg: "bg-blue-50 dark:bg-blue-950/20",
      };
    }
  }
  return {
    label: "Normal",
    detail: `Util. avg ${avgUtil.toFixed(0)}% | pico: ${histMax} de ${hpaMax} réplicas`,
    color: "text-green-500",
    bg: "",
  };
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
                <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
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

// ─── Painel de comparação de períodos HPA ─────────────────────────────────────

function HPAComparePanel({
  compareData,
  loading,
  days,
}: {
  compareData: (TimelineCompareResponse & { current_period?: string; previous_period?: string; current_saved_at?: string }) | null;
  loading: boolean;
  days: number;
}) {
  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-10 gap-3 text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
          <span className="text-sm">Carregando comparação de períodos…</span>
        </CardContent>
      </Card>
    );
  }

  if (!compareData) {
    return (
      <Card className="border-dashed">
        <CardContent className="flex flex-col items-center justify-center py-10 gap-2 text-muted-foreground">
          <GitCompare className="h-8 w-8 opacity-20" />
          <p className="text-sm">Clique em <strong>Comparar períodos</strong> para carregar</p>
          <p className="text-xs opacity-70">O período anterior vem do banco de snapshots (salvo automaticamente a cada busca)</p>
        </CardContent>
      </Card>
    );
  }

  if (!compareData.has_previous) {
    return (
      <Card className="border-yellow-200 bg-yellow-50 dark:bg-yellow-950/20">
        <CardContent className="flex items-center gap-3 py-4 px-4">
          <Info className="h-4 w-4 text-yellow-600 shrink-0" />
          <div>
            <p className="text-sm font-medium text-yellow-800 dark:text-yellow-300">Sem período anterior salvo</p>
            <p className="text-xs text-yellow-700 dark:text-yellow-400">
              O período atual ({compareData.current.start_date} → {compareData.current.end_date}) foi salvo.
              Faça outra busca após {days} dias para comparar evolução.
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  const current = compareData.current;
  const previous = compareData.previous!;

  // Indexar HPAs do período anterior por workload key
  const prevMap = new Map<string, HPATimeline>();
  for (const h of previous.hpas) prevMap.set(`${h.namespace}/${h.workload}`, h);

  // Calcular avg replicas por período
  const avgReplicas = (hpa: HPATimeline) => {
    if (!hpa.series.length) return 0;
    return hpa.series.reduce((s, p) => s + p.avg_replicas, 0) / hpa.series.length;
  };

  type CompareRow = {
    key: string;
    namespace: string;
    workload: string;
    currentAvg: number;
    previousAvg: number;
    delta: number;
    currentMin: number;
    currentMax: number;
    prevMin: number;
    prevMax: number;
  };

  const rows: CompareRow[] = current.hpas.map(h => {
    const key = `${h.namespace}/${h.workload}`;
    const prev = prevMap.get(key);
    const currentAvg = avgReplicas(h);
    const previousAvg = prev ? avgReplicas(prev) : 0;
    return {
      key,
      namespace: h.namespace,
      workload: h.workload,
      currentAvg,
      previousAvg,
      delta: prev ? currentAvg - previousAvg : 0,
      currentMin: h.hpa_min,
      currentMax: h.hpa_max,
      prevMin: prev?.hpa_min ?? 0,
      prevMax: prev?.hpa_max ?? 0,
    };
  }).sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta));

  const prevDate = previous.start_date.slice(5) + " → " + previous.end_date.slice(5);
  const currDate = current.start_date.slice(5) + " → " + current.end_date.slice(5);

  return (
    <Card>
      <CardHeader className="pb-2 pt-4 px-4">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <CardTitle className="text-sm flex items-center gap-2">
            <GitCompare className="h-4 w-4 text-indigo-500" />
            Comparação de Períodos
          </CardTitle>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <span className="flex items-center gap-1">
              <span className="inline-block w-3 h-0.5 bg-muted-foreground/50 rounded" />
              Anterior: {prevDate}
            </span>
            <span className="flex items-center gap-1">
              <span className="inline-block w-3 h-0.5 bg-indigo-500 rounded" />
              Atual: {currDate}
            </span>
          </div>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow className="text-[11px] text-muted-foreground">
              <TableHead className="pl-4 w-[240px]">Workload / Namespace</TableHead>
              <TableHead className="text-center">Config anterior</TableHead>
              <TableHead className="text-center">Avg anterior</TableHead>
              <TableHead className="text-center">Avg atual</TableHead>
              <TableHead className="text-center">Config atual</TableHead>
              <TableHead className="text-center">Delta</TableHead>
              <TableHead className="text-center">Tendência</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(r => {
              const hasPrev = prevMap.has(r.key);
              const isNew = !hasPrev;
              const absD = Math.abs(r.delta);
              const trend =
                !hasPrev ? "novo"
                : absD < 0.3 ? "estável"
                : r.delta > 0 ? "aumentando"
                : "reduzindo";

              const trendBadge: Record<string, string> = {
                novo:       "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
                estável:    "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
                aumentando: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
                reduzindo:  "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
              };

              const configChanged =
                hasPrev && (r.currentMin !== r.prevMin || r.currentMax !== r.prevMax);

              return (
                <TableRow key={r.key} className="text-xs">
                  <TableCell className="pl-4 py-2">
                    <p className="font-semibold truncate max-w-[200px]">{r.workload}</p>
                    <p className="text-[10px] text-muted-foreground truncate max-w-[200px]">{r.namespace}</p>
                  </TableCell>
                  <TableCell className="text-center font-mono text-muted-foreground">
                    {hasPrev ? `${r.prevMin}→${r.prevMax}` : "—"}
                  </TableCell>
                  <TableCell className="text-center font-mono text-muted-foreground">
                    {hasPrev ? r.previousAvg.toFixed(1) : "—"}
                  </TableCell>
                  <TableCell className="text-center font-mono font-semibold">
                    {r.currentAvg.toFixed(1)}
                  </TableCell>
                  <TableCell className="text-center font-mono">
                    <span className={configChanged ? "text-yellow-600 font-semibold" : ""}>
                      {r.currentMin}→{r.currentMax}
                    </span>
                  </TableCell>
                  <TableCell className="text-center">
                    {isNew ? (
                      <span className="text-blue-500 text-[10px]">novo</span>
                    ) : (
                      <span className={`font-semibold ${r.delta > 0.3 ? "text-red-500" : r.delta < -0.3 ? "text-green-500" : "text-muted-foreground"}`}>
                        {r.delta > 0 ? "+" : ""}{r.delta.toFixed(1)}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-center">
                    <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium ${trendBadge[trend]}`}>
                      {trend === "aumentando" && <TrendingUp className="h-2.5 w-2.5" />}
                      {trend === "reduzindo"  && <TrendingDown className="h-2.5 w-2.5" />}
                      {trend}
                    </span>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        <div className="text-[10px] text-muted-foreground px-4 py-2 border-t flex items-center gap-4 flex-wrap">
          {compareData.previous_saved_at && (
            <span>Anterior salvo em: {new Date(compareData.previous_saved_at).toLocaleString("pt-BR")}</span>
          )}
          {compareData.current_saved_at && (
            <span>Atual salvo em: {new Date(compareData.current_saved_at).toLocaleString("pt-BR")}</span>
          )}
          {compareData.current_period && (
            <span className="text-indigo-500 font-medium">Atual: {compareData.current_period}</span>
          )}
          {compareData.previous_period && (
            <span className="text-muted-foreground">Anterior: {compareData.previous_period}</span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function HPAHistoryTab({
  cluster,
  days,
  setDays,
}: {
  cluster: string;
  days: number;
  setDays: (d: number) => void;
}) {
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [sortField, setSortField] = useState<SortField>("daysAtMax");
  const [sortAsc, setSortAsc] = useState(false);
  const [showCompare, setShowCompare] = useState(false);
  const [compareMode, setCompareMode] = useState<"auto" | "snapshot" | "saved">("auto");
  const [selectedSnap1, setSelectedSnap1] = useState<string>("");
  const [selectedSnap2, setSelectedSnap2] = useState<string>("");

  const token = localStorage.getItem("token") || "poc-token-123";
  const authHeaders = { Authorization: `Bearer ${token}` };

  // ── Timeline principal (useQuery preserva dados ao trocar de aba) ──────────
  const {
    data: timeline,
    isFetching: loading,
    error: queryError,
    refetch: fetchTimeline,
  } = useQuery<TimelineReport>({
    queryKey: ["finops-hpa-history", cluster, days],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=${days}`,
        { headers: authHeaders }
      );
      if (!r.ok) {
        const e = await r.json().catch(() => ({}));
        throw new Error((e as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: false,       // só busca ao clicar no botão
    staleTime: 10 * 60 * 1000, // dados sobrevivem 10 min ao trocar de aba
    retry: false,
  });

  const error = queryError ? (queryError as Error).message : null;

  // ── Comparação de períodos ─────────────────────────────────────────────────
  const {
    data: compareData,
    isFetching: compareLoading,
    refetch: fetchCompare,
  } = useQuery<TimelineCompareResponse>({
    queryKey: ["finops-hpa-compare", cluster, days],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline/compare?cluster=${encodeURIComponent(cluster)}&days=${days}`,
        { headers: authHeaders }
      );
      if (!r.ok) {
        const e = await r.json().catch(() => ({}));
        throw new Error((e as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: false,
    staleTime: 10 * 60 * 1000,
    retry: false,
  });

  // ── Snapshots salvos (metadados — também usado para o seletor de comparação) ──
  const { data: savedData } = useQuery<{ count: number; snapshots: TimelineSnapshotMeta[] }>({
    queryKey: ["finops-hpa-saved", cluster],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline/saved?cluster=${encodeURIComponent(cluster)}`,
        { headers: authHeaders }
      );
      if (!r.ok) return { count: 0, snapshots: [] };
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 60 * 1000,
    retry: false,
  });

  // ── Comparação com snapshot específico (Prometheus atual vs salvo) ─────────
  const {
    data: snapCompareData,
    isFetching: snapCompareLoading,
    refetch: fetchSnapCompare,
  } = useQuery<TimelineCompareResponse>({
    queryKey: ["finops-hpa-compare-snapshot", cluster, days, selectedSnap1],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline/compare-snapshot?cluster=${encodeURIComponent(cluster)}&days=${days}&snapshot_id=${encodeURIComponent(selectedSnap1)}`,
        { headers: authHeaders }
      );
      if (!r.ok) {
        const e = await r.json().catch(() => ({}));
        throw new Error((e as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: false,
    staleTime: 10 * 60 * 1000,
    retry: false,
  });

  // ── Comparação entre dois snapshots salvos (sem Prometheus) ───────────────
  const {
    data: savedCompareData,
    isFetching: savedCompareLoading,
    refetch: fetchSavedCompare,
  } = useQuery<TimelineCompareResponse & { current_period?: string; previous_period?: string; current_saved_at?: string }>({
    queryKey: ["finops-hpa-compare-saved", cluster, selectedSnap1, selectedSnap2],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline/compare-saved?cluster=${encodeURIComponent(cluster)}&snap1=${encodeURIComponent(selectedSnap1)}&snap2=${encodeURIComponent(selectedSnap2)}`,
        { headers: authHeaders }
      );
      if (!r.ok) {
        const e = await r.json().catch(() => ({}));
        throw new Error((e as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: false,
    staleTime: 10 * 60 * 1000,
    retry: false,
  });

  const activeCompareData = compareMode === "auto"
    ? compareData
    : compareMode === "snapshot"
    ? snapCompareData ?? null
    : savedCompareData ?? null;

  const activeCompareLoading = compareMode === "auto"
    ? compareLoading
    : compareMode === "snapshot"
    ? snapCompareLoading
    : savedCompareLoading;

  const handleCompareExecute = () => {
    if (compareMode === "auto") fetchCompare();
    else if (compareMode === "snapshot" && selectedSnap1) fetchSnapCompare();
    else if (compareMode === "saved" && selectedSnap1 && selectedSnap2) fetchSavedCompare();
  };

  // Agrupar snapshots por mês para facilitar seleção
  const snapshotsByMonth = (() => {
    const snaps = savedData?.snapshots ?? [];
    const map = new Map<string, TimelineSnapshotMeta[]>();
    for (const s of snaps) {
      const month = s.end_date.slice(0, 7); // "YYYY-MM"
      if (!map.has(month)) map.set(month, []);
      map.get(month)!.push(s);
    }
    return map;
  })();

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
    const histMax = h.series.reduce((m, p) => Math.max(m, p.max_replicas), 0);
    const histAvg = h.series.reduce((s, p) => s + p.avg_replicas, 0) / n;
    return { ...h, avgUtil, daysAtMax, daysAtMin, histMax, histAvg };
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
      {/* Controles — linha 1: período + busca */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1">
          {([7, 15, 30] as const).map(d => (
            <Button key={d} variant={days === d ? "default" : "outline"} size="sm"
              className="h-7 text-xs px-3" onClick={() => setDays(d)}>
              {d}d
            </Button>
          ))}
        </div>
        <Button size="sm" className="h-7 text-xs gap-1.5" onClick={() => fetchTimeline()} disabled={loading}>
          {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          Buscar histórico
        </Button>
        {(savedData?.count ?? 0) > 0 && (
          <span className="flex items-center gap-1 text-xs text-muted-foreground">
            <Database className="h-3 w-3" />
            {savedData!.count} snapshot{savedData!.count !== 1 ? "s" : ""} salvos
          </span>
        )}
        {timeline && (
          <span className="text-xs text-muted-foreground">
            {timeline.start_date} → {timeline.end_date} · {timeline.hpas.length} HPAs · {timeline.nodes.length} dias
          </span>
        )}
      </div>

      {/* Controles — linha 2: comparação */}
      <div className="flex items-center gap-2 flex-wrap p-3 rounded-lg border bg-muted/30">
        <GitCompare className="h-4 w-4 text-indigo-500 shrink-0" />
        <span className="text-xs font-medium text-muted-foreground">Comparar:</span>

        {/* Modo de comparação */}
        <div className="flex items-center gap-1">
          {(["auto", "snapshot", "saved"] as const).map(mode => (
            <Button
              key={mode}
              size="sm"
              variant={compareMode === mode ? "default" : "outline"}
              className="h-6 text-[11px] px-2"
              onClick={() => { setCompareMode(mode); setShowCompare(false); }}
            >
              {mode === "auto" ? "Automático" : mode === "snapshot" ? "Atual vs Salvo" : "Dois Salvos"}
            </Button>
          ))}
        </div>

        {/* Seletores por modo */}
        {compareMode === "snapshot" && (
          <select
            className="h-7 text-xs px-2 rounded border bg-background"
            value={selectedSnap1}
            onChange={e => setSelectedSnap1(e.target.value)}
          >
            <option value="">— Período anterior —</option>
            {[...snapshotsByMonth.entries()].map(([month, items]) => (
              <optgroup key={month} label={month}>
                {items.map(s => (
                  <option key={s.id} value={s.id}>
                    {s.start_date.slice(5)} → {s.end_date.slice(5)} ({s.days}d) · {new Date(s.saved_at).toLocaleDateString("pt-BR")}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        )}

        {compareMode === "saved" && (
          <>
            <select
              className="h-7 text-xs px-2 rounded border bg-background"
              value={selectedSnap1}
              onChange={e => setSelectedSnap1(e.target.value)}
            >
              <option value="">— Período atual (mais recente) —</option>
              {[...snapshotsByMonth.entries()].map(([month, items]) => (
                <optgroup key={month} label={month}>
                  {items.map(s => (
                    <option key={s.id} value={s.id}>
                      {s.start_date.slice(5)} → {s.end_date.slice(5)} ({s.days}d)
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
            <span className="text-xs text-muted-foreground">vs</span>
            <select
              className="h-7 text-xs px-2 rounded border bg-background"
              value={selectedSnap2}
              onChange={e => setSelectedSnap2(e.target.value)}
            >
              <option value="">— Período anterior —</option>
              {[...snapshotsByMonth.entries()].map(([month, items]) => (
                <optgroup key={month} label={month}>
                  {items.map(s => (
                    <option key={s.id} value={s.id}>
                      {s.start_date.slice(5)} → {s.end_date.slice(5)} ({s.days}d)
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </>
        )}

        <Button
          size="sm"
          variant={showCompare ? "default" : "outline"}
          className="h-7 text-xs gap-1.5"
          onClick={() => {
            const canRun =
              compareMode === "auto" ||
              (compareMode === "snapshot" && !!selectedSnap1) ||
              (compareMode === "saved" && !!selectedSnap1 && !!selectedSnap2);
            if (!canRun) return;
            if (!showCompare) {
              setShowCompare(true);
              handleCompareExecute();
            } else {
              setShowCompare(false);
            }
          }}
          disabled={activeCompareLoading ||
            (compareMode === "snapshot" && !selectedSnap1) ||
            (compareMode === "saved" && (!selectedSnap1 || !selectedSnap2))}
        >
          {activeCompareLoading
            ? <Loader2 className="h-3 w-3 animate-spin" />
            : <GitCompare className="h-3 w-3" />}
          {showCompare ? "Ocultar" : "Comparar"}
        </Button>
      </div>

      {error && (
        <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <AlertTriangle className="h-4 w-4 text-red-600" />
          <AlertDescription className="text-sm text-red-700 dark:text-red-400">{error}</AlertDescription>
        </Alert>
      )}

      {/* Painel de comparação */}
      {showCompare && (
        <HPAComparePanel
          compareData={activeCompareData}
          loading={activeCompareLoading}
          days={days}
        />
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
                    const rec = hpaRecommendation(h.avgUtil, h.daysAtMax, h.daysAtMin, days, h.hpa_min, h.hpa_max, h.histMax, h.histAvg);
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
                          <TableCell className="max-w-[300px]">
                            <p className={`text-[11px] font-semibold ${rec.color}`}>{rec.label}</p>
                            <p className="text-[10px] text-muted-foreground leading-tight mt-0.5">{rec.detail}</p>
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

function OpportunitiesTab({ workloads, windowDays }: {
  workloads: FinOpsWorkload[];
  summary?: FinOpsSummary;
  windowDays: number;
}) {
  const hasPrometheus = workloads.some(w => (w.waste_brl ?? 0) > 0 || (w.hpa_avg_replicas ?? 0) > 0);

  const opportunities = workloads
    .filter(w => w.verdict === "superprovisioned" || w.verdict === "hpa_removable" || w.verdict === "no_request" || (w.waste_brl ?? 0) > 0)
    .map(w => {
      const rec = buildRecommendation(w, windowDays);
      const saving = rec.savingBRL > 0
        ? rec.savingBRL
        : Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl);
      return { ...w, rec, saving };
    })
    .sort((a, b) => {
      // Prioridade: saving > exposureBRL (precisa Prometheus) > custo
      if (b.saving !== a.saving) return b.saving - a.saving;
      return b.rec.exposureBRL - a.rec.exposureBRL;
    });

  const totalSaving   = opportunities.reduce((s, o) => s + o.saving, 0);
  const totalExposure = opportunities.reduce((s, o) => s + o.rec.exposureBRL, 0);
  const totalWaste    = workloads.reduce((s, w) => s + (w.waste_brl ?? 0), 0);
  const needsPrometheusCount = opportunities.filter(o => o.rec.needsPrometheus).length;

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
          {/* Banner de resumo */}
          <Alert className={totalWaste > 0 ? "border-red-200 bg-red-50 dark:bg-red-950/20" : totalSaving > 0 ? "border-green-200 bg-green-50 dark:bg-green-950/20" : "border-amber-200 bg-amber-50 dark:bg-amber-950/20"}>
            <TrendingDown className={`h-4 w-4 ${totalWaste > 0 ? "text-red-600" : totalSaving > 0 ? "text-green-600" : "text-amber-600"}`} />
            <AlertDescription className="text-sm space-y-1">
              <span>
                <strong>{opportunities.length} oportunidades</strong> identificadas
                {needsPrometheusCount > 0 && (
                  <span className="text-muted-foreground text-[11px]"> ({needsPrometheusCount} precisam de Prometheus para quantificar)</span>
                )}
                {"."}
              </span>
              {totalWaste > 0 && (
                <span className="block">
                  Desperdício real (P95):{" "}
                  <strong className="text-red-600">{fmtBRL(totalWaste)}/mês = {fmtBRL(totalWaste * 12)}/ano</strong>
                </span>
              )}
              {totalSaving > 0 && (
                <span className="block">
                  Economia imediata estimada:{" "}
                  <strong className="text-green-600">{fmtBRL(totalSaving)}/mês = {fmtBRL(totalSaving * 12)}/ano</strong>
                </span>
              )}
              {!hasPrometheus && totalExposure > 0 && (
                <span className="block text-amber-700 dark:text-amber-400">
                  Exposição máxima (custo se HPAs escalarem ao teto):{" "}
                  <strong>{fmtBRL(totalExposure)}/mês</strong>
                  {" "}— ative Prometheus para ver histórico real e recomendar valores seguros
                </span>
              )}
            </AlertDescription>
          </Alert>

          {/* Chart de oportunidades */}
          {opportunities.length > 1 && (() => {
            const chartData = opportunities.slice(0, 10).map(w => ({
              name: w.workload.length > 16 ? w.workload.slice(0, 14) + "…" : w.workload,
              atual: w.cost_share_brl,
              saving: w.saving,
            })).filter(d => d.saving > 0);
            if (!chartData.length) return null;
            return (
              <Card>
                <CardHeader className="pb-1 pt-3 px-4">
                  <CardTitle className="text-sm">Potencial de Economia por Workload (top {chartData.length})</CardTitle>
                </CardHeader>
                <CardContent className="px-2 pb-3">
                  <ResponsiveContainer width="100%" height={Math.max(160, chartData.length * 26)}>
                    <BarChart data={chartData} layout="vertical"
                      margin={{ left: 8, right: 70, top: 4, bottom: 4 }}>
                      <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.4} />
                      <XAxis type="number"
                        tickFormatter={v => v === 0 ? "R$0" : v >= 1000 ? `R$${(v/1000).toFixed(0)}k` : `R$${Math.round(v)}`}
                        tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                      <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={110}
                        axisLine={false} tickLine={false} />
                      <Tooltip cursor={{ fill: "rgba(100,100,100,0.1)" }}
                        content={({ active, payload, label }) => {
                          if (!active || !payload?.length) return null;
                          return (
                            <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                              <p className="font-medium">{label}</p>
                              {payload.map((p, i) => (
                                <p key={i} style={{ color: p.color }}>
                                  {p.name === "atual" ? `Custo atual: ${fmtBRL(p.value as number)}`
                                    : `Economia estimada: ${fmtBRL(p.value as number)}`}
                                </p>
                              ))}
                            </div>
                          );
                        }}
                      />
                      <Bar dataKey="atual" name="atual" stackId="a" fill="#6366f1" fillOpacity={0.25}
                        radius={[0, 0, 0, 0]} maxBarSize={18} />
                      <Bar dataKey="saving" name="saving" stackId="a" fill="#ef4444" fillOpacity={0.85}
                        radius={[0, 4, 4, 0]} maxBarSize={18}>
                        <LabelList dataKey="saving" position="right"
                          formatter={(v: number) => v > 0 ? `-${fmtBRL(v)}` : ""}
                          style={{ fontSize: 9, fill: "#16a34a" }} />
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                </CardContent>
              </Card>
            );
          })()}

          {/* Lista de oportunidades com recomendações concretas */}
          <div className="space-y-2">
            {opportunities.map((w, i) => {
              const borderColor =
                w.verdict === "oom_risk"         ? "#f59e0b"
                : w.verdict === "superprovisioned" ? "#ef4444"
                : w.verdict === "hpa_removable"    ? "#8b5cf6"
                : "#9ca3af";

              return (
                <Card key={i} className="border-l-4" style={{ borderLeftColor: borderColor }}>
                  <CardContent className="p-3">
                    {/* Cabeçalho: workload + saving */}
                    <div className="flex items-start justify-between gap-3 mb-2">
                      <div className="flex items-center gap-2 flex-wrap min-w-0">
                        <span className="font-semibold text-sm">{w.workload}</span>
                        <span className="text-xs text-muted-foreground">{w.namespace}</span>
                        <VerdictBadge verdict={w.verdict} />
                      </div>
                      <div className="text-right flex-shrink-0 space-y-0.5">
                        <p className="text-[10px] text-muted-foreground">custo atual</p>
                        <p className="text-sm font-semibold">{fmtBRL(w.cost_share_brl)}/mês</p>
                        {w.saving > 0 ? (
                          <>
                            <p className="text-[10px] text-muted-foreground mt-1">economia estimada</p>
                            <p className="text-base font-bold text-green-600">-{fmtBRL(w.saving)}/mês</p>
                            <p className="text-[10px] text-muted-foreground">/ano: -{fmtBRL(w.saving * 12)}</p>
                          </>
                        ) : w.rec.exposureBRL > 0 ? (
                          <>
                            <p className="text-[10px] text-amber-600 dark:text-amber-400 mt-1">exposição máxima</p>
                            <p className="text-sm font-bold text-amber-600">+{fmtBRL(w.rec.exposureBRL)}/mês</p>
                            <p className="text-[10px] text-muted-foreground">(se escalar ao max={w.hpa_max})</p>
                          </>
                        ) : null}
                      </div>
                    </div>

                    {/* Estado atual */}
                    <div className="text-xs text-muted-foreground mb-2 space-y-0.5">
                      {w.hpa_max > 0 && (
                        <p>
                          HPA configurado: <span className="font-mono">{w.hpa_min} (min) / {w.hpa_current} (atual) / {w.hpa_max} (max)</span>
                          {w.hpa_max > 0 && (
                            <span className="ml-1">— atual em <strong>{Math.round((w.hpa_current / w.hpa_max) * 100)}% do máximo</strong></span>
                          )}
                          {w.hpa_avg_replicas != null && (
                            <span className="ml-1">· média {windowDays}d: <strong>{w.hpa_avg_replicas.toFixed(1)}</strong> répl.</span>
                          )}
                          {w.hpa_max_observed != null && w.hpa_max_observed > 0 && (
                            <span className="ml-1">· pico observado: <strong>{w.hpa_max_observed}</strong></span>
                          )}
                        </p>
                      )}
                      {(w.cpu_p95_millis ?? 0) > 0 && (
                        <p>
                          CPU — request: <span className="font-mono">{fmtMillis(w.cpu_request_millis)}</span>
                          {" · "}P95: <span className="font-mono">{fmtMillis(w.cpu_p95_millis!)}</span>
                          {w.cpu_recommended_millis ? <span> · recomendado: <span className="font-mono text-amber-600">{fmtMillis(w.cpu_recommended_millis)}</span></span> : null}
                        </p>
                      )}
                      {(w.mem_p95_mi ?? 0) > 0 && (
                        <p>
                          Mem — request: <span className="font-mono">{fmtMi(w.mem_request_mi)}</span>
                          {" · "}P95: <span className="font-mono">{fmtMi(w.mem_p95_mi!)}</span>
                          {w.mem_recommended_mi ? <span> · recomendado: <span className="font-mono text-amber-600">{fmtMi(w.mem_recommended_mi)}</span></span> : null}
                        </p>
                      )}
                      {w.verdict === "no_request" && (
                        <p>Sem resource requests definidos — custo alocado por heurística, não por consumo real</p>
                      )}
                    </div>

                    {/* Recomendação concreta */}
                    <div className="border rounded-md bg-muted/30 px-3 py-2 space-y-1">
                      <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground mb-1">Ação recomendada</p>
                      {w.rec.lines.map((line, li) => (
                        <p key={li} className={`text-xs leading-snug flex items-start gap-1.5 ${
                          line.highlight ? "font-semibold text-foreground" : "text-muted-foreground"
                        }`}>
                          {line.highlight
                            ? <span className="mt-1 shrink-0 w-2 h-2 rounded-full bg-green-500 inline-block" />
                            : <span className="mt-1 shrink-0 w-2 h-2 rounded-full bg-muted-foreground/30 inline-block" />
                          }
                          {line.text}
                        </p>
                      ))}
                      {(w.rec.kubectlList ?? []).length > 0 && (
                        <div className="mt-1.5 space-y-1.5">
                          {w.rec.kubectlList.map((cmd, ci) => (
                            <KubectlBlock key={ci} cmd={cmd} />
                          ))}
                        </div>
                      )}
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>

          {/* Rodapé com total */}
          {totalSaving > 0 && (
            <Card className="border-green-200 bg-green-50/50 dark:bg-green-950/10">
              <CardContent className="p-3 flex justify-between items-center">
                <span className="text-xs text-muted-foreground">Aplicando todas as recomendações acima:</span>
                <div className="text-right">
                  <span className="text-sm font-bold text-green-600">-{fmtBRL(totalSaving)}/mês</span>
                  <p className="text-[10px] text-muted-foreground">/ano: -{fmtBRL(totalSaving * 12)}</p>
                </div>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  );
}

// ─── Componente Principal ─────────────────────────────────────────────────────

export const FinOpsTab = ({ selectedCluster }: { selectedCluster?: string }) => {
  const { clusters } = useClusters();

  // Usar o contexto real do kubeconfig (campo `context`) — cada máquina tem seu próprio
  // padrão de nomenclatura (com ou sem sufixo -admin). Não forçar sufixo no frontend.
  const clusterOptions = (clusters ?? [])
    .map(c => c.context)
    .filter((v, i, a) => !!v && a.indexOf(v) === i);

  const defaultCluster = selectedCluster
    ? ((clusters ?? []).find(c => c.context === selectedCluster || c.name === selectedCluster.replace(/-admin$/, ""))?.context ?? selectedCluster)
    : clusterOptions[0] ?? "";

  const [cluster, setCluster] = useState(defaultCluster);
  const [triggerKey, setTriggerKey] = useState(0);
  const [withPrometheus, setWithPrometheus] = useState(false);
  const [windowDays, setWindowDays] = useState(30);
  const [aiAnalysis, setAiAnalysis] = useState<string | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiExpanded, setAiExpanded] = useState(true);

  // Estado persistente da aba HPA Histórico (sobrevive a troca de tabs)
  const [hpaHistoryDays, setHpaHistoryDays] = useState(30);

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
          <div className="flex items-center gap-2 text-xs text-muted-foreground -mb-1 flex-wrap">
            <Server className="h-3.5 w-3.5 shrink-0" />
            <span>
              {report.cluster.replace("-admin", "")} · gerado em {new Date(report.generated_at).toLocaleTimeString("pt-BR")}
              {" · "}câmbio USD/BRL: <strong>R$ {report.exchange_rate.toFixed(4)}</strong> ({report.exchange_date})
              {" · "}{report.node_pools.length} node pools · {report.summary.workloads_analyzed} workloads
            </span>
            {!withPrometheus && report.window_days === 0 && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400 border border-amber-300 dark:border-amber-700">
                <AlertTriangle className="h-3 w-3" />
                Sem Prometheus — saving estimado por HPA config apenas
              </span>
            )}
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
                <HPAHistoryTab cluster={cluster} days={hpaHistoryDays} setDays={setHpaHistoryDays} />
              </TabsContent>
              <TabsContent value="opportunities" className="mt-0 h-full">
                <OpportunitiesTab workloads={report.workloads} summary={report.summary} windowDays={report.window_days || windowDays} />
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
