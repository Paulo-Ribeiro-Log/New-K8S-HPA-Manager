// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend, ComposedChart, Line, ReferenceLine,
} from "recharts";
import {
  TrendingDown, TrendingUp, AlertTriangle, Loader2, RefreshCw, Server,
  ArrowUpDown, Info, ChevronDown, ChevronUp, Activity, GitCompare, Database,
} from "lucide-react";
import { SummaryCard } from "@/lib/finopsFormat";
import type { HPADayPoint, HPATimeline, TimelineReport, TimelineCompareResponse, TimelineSnapshotMeta } from "./types";

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

export function HPAHistoryTab({
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

  const token = localStorage.getItem("auth_token");
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
