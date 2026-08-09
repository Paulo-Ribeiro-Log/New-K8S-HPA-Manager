/**
 * DynatraceMetricsPanel — Painel de métricas de performance por entidade.
 * Renderiza gráficos Recharts agrupados por tipo de entidade e família de métrica.
 */

import { useQuery } from "@tanstack/react-query";
import {
  ComposedChart, Area, Line, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer, ReferenceLine,
} from "recharts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RefreshCw, AlertCircle, Layers, Server, Activity, Cpu, BarChart3, Network } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { DTEntityMetrics, DTMetricSeries } from "@/types/healthcheck";

// ── Paleta de cores por chave de métrica ────────────────────────────────────────

const KEY_COLORS: Record<string, string> = {
  response_p50:    "#6366f1",
  response_p90:    "#a78bfa",
  response_p95:    "#f97316",
  response_p99:    "#ef4444",
  error_rate:      "#ef4444",
  error_count:     "#f97316",
  throughput:      "#22c55e",
  ext_calls:       "#8b5cf6",
  db_calls:        "#06b6d4",
  db_latency_p95:  "#f59e0b",
  pods_desired:    "#6366f1",
  cpu_milli:       "#f97316",
  cpu_throttle:    "#dc2626",
  memory_mb:       "#8b5cf6",
  node_cpu:        "#f97316",
  node_pods:       "#6366f1",
  host_cpu:        "#f97316",
  host_mem_avail:  "#6366f1",
  net_in:          "#22c55e",
  net_out:         "#ef4444",
  proc_cpu:        "#f97316",
  proc_mem:        "#8b5cf6",
  tcp_sockets:     "#06b6d4",
  app_response:    "#f97316",
  app_errors:      "#ef4444",
  app_sessions:    "#22c55e",
};

function colorFor(key: string, idx: number): string {
  return KEY_COLORS[key] ?? ["#6366f1","#22c55e","#f97316","#ef4444","#06b6d4"][idx % 5];
}

// ── Agrupamento de métricas por família ────────────────────────────────────────

interface ChartGroup {
  title: string;
  icon: React.ReactNode;
  keys: string[];
  // "area" = todas as séries como Area; "lines" = todas como Line; "mixed" = custom
  style: "area" | "lines" | "bar_plus_lines";
  dualAxis?: boolean; // segunda série usa yAxisId="right"
}

const SERVICE_GROUPS: ChartGroup[] = [
  {
    title: "Tempo de Resposta (P50 / P90 / P95 / P99)",
    icon: <Activity className="h-3.5 w-3.5" />,
    keys: ["response_p50", "response_p90", "response_p95", "response_p99"],
    style: "lines",
  },
  {
    title: "Taxa de Erro & Throughput",
    icon: <AlertCircle className="h-3.5 w-3.5" />,
    keys: ["error_rate", "throughput"],
    style: "lines",
    dualAxis: true,
  },
  {
    title: "Erros Absolutos",
    icon: <AlertCircle className="h-3.5 w-3.5" />,
    keys: ["error_count"],
    style: "area",
  },
  {
    title: "Calls Externos & Banco de Dados",
    icon: <Network className="h-3.5 w-3.5" />,
    keys: ["ext_calls", "db_calls"],
    style: "lines",
  },
  {
    title: "Latência de Banco (P95)",
    icon: <BarChart3 className="h-3.5 w-3.5" />,
    keys: ["db_latency_p95"],
    style: "area",
  },
];

const K8S_GROUPS: ChartGroup[] = [
  {
    // Bug real corrigido: "pods_running"/"pods_ready_pct"/"pod_restarts" usavam metricId sem
    // equivalente real pra CLOUD_APPLICATION nesta versão do Dynatrace (ver comentário detalhado em
    // k8sWorkloadMetricDefs, internal/dynatrace/metrics.go) — nunca tinham dado, então este grupo
    // inteiro sempre desaparecia (MetricChart retorna null com availableSeries vazio). Único
    // substituto real confirmado nesta família de métricas é "pods_desired" (contagem desejada, não
    // "rodando agora" — Dynatrace não expõe esse dado nesta dimensão).
    title: "Pods Desejados",
    icon: <Layers className="h-3.5 w-3.5" />,
    keys: ["pods_desired"],
    style: "area",
  },
  {
    title: "CPU — Uso & Throttling",
    icon: <Cpu className="h-3.5 w-3.5" />,
    keys: ["cpu_milli", "cpu_throttle"],
    style: "lines",
    dualAxis: true,
  },
  {
    title: "Memória (Working Set)",
    icon: <BarChart3 className="h-3.5 w-3.5" />,
    keys: ["memory_mb"],
    style: "area",
  },
];

const HOST_GROUPS: ChartGroup[] = [
  {
    title: "CPU do Host",
    icon: <Cpu className="h-3.5 w-3.5" />,
    keys: ["host_cpu", "node_cpu"],
    style: "area",
  },
  {
    title: "Memória Disponível",
    icon: <BarChart3 className="h-3.5 w-3.5" />,
    keys: ["host_mem_avail"],
    style: "area",
  },
  {
    title: "Tráfego de Rede (Entrada / Saída)",
    icon: <Network className="h-3.5 w-3.5" />,
    keys: ["net_in", "net_out"],
    style: "lines",
  },
  {
    title: "Pods no Nó",
    icon: <Layers className="h-3.5 w-3.5" />,
    keys: ["node_pods"],
    style: "lines",
  },
];

const PROCESS_GROUPS: ChartGroup[] = [
  {
    title: "CPU & Memória do Processo",
    icon: <Cpu className="h-3.5 w-3.5" />,
    keys: ["proc_cpu", "proc_mem"],
    style: "lines",
    dualAxis: true,
  },
  {
    title: "Sockets TCP Abertos",
    icon: <Network className="h-3.5 w-3.5" />,
    keys: ["tcp_sockets"],
    style: "lines",
  },
];

const APP_GROUPS: ChartGroup[] = [
  {
    title: "Duração de Ação (P95) & Erros",
    icon: <Activity className="h-3.5 w-3.5" />,
    keys: ["app_response", "app_errors"],
    style: "lines",
    dualAxis: true,
  },
  {
    title: "Sessões",
    icon: <BarChart3 className="h-3.5 w-3.5" />,
    keys: ["app_sessions"],
    style: "area",
  },
];

function groupsForType(entityType: string): ChartGroup[] {
  switch (entityType) {
    case "SERVICE":                                   return SERVICE_GROUPS;
    case "CLOUD_APPLICATION":
    case "CLOUD_APPLICATION_INSTANCE":               return K8S_GROUPS;
    case "KUBERNETES_NODE":                           return [...HOST_GROUPS, ...K8S_GROUPS];
    case "HOST":                                      return HOST_GROUPS;
    case "PROCESS_GROUP":
    case "PROCESS_GROUP_INSTANCE":                   return PROCESS_GROUPS;
    case "APPLICATION":                              return APP_GROUPS;
    default:                                         return SERVICE_GROUPS;
  }
}

// ── Helpers de formatação ──────────────────────────────────────────────────────

function fmtTime(ts: number): string {
  return new Date(ts).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
}

/** Formatação compacta para ticks do eixo Y — evita labels longas que se sobrepõem */
function fmtTick(v: number, unit: string): string {
  const abs = Math.abs(v);
  let num: string;
  if (abs >= 1_000_000) num = `${(v / 1_000_000).toFixed(1)}M`;
  else if (abs >= 1_000) num = `${(v / 1_000).toFixed(1)}k`;
  else if (abs >= 100)  num = v.toFixed(0);
  else if (abs >= 10)   num = v.toFixed(1);
  else                  num = v.toFixed(2);

  // Unidade abreviada para o tick
  const u: Record<string, string> = {
    "ms": "ms", "%": "%", "MB": "MB", "GB": "GB",
    "mCPU": "m", "KB/s": "K/s", "req/min": "rpm",
    "calls/min": "cpm", "pods": "p", "count": "",
    "sessions": "s",
  };
  const suffix = u[unit] ?? unit;
  return suffix ? `${num}${suffix}` : num;
}

function fmtValue(v: number, unit: string): string {
  if (unit === "ms")        return `${v.toFixed(1)}ms`;
  if (unit === "%")         return `${v.toFixed(1)}%`;
  if (unit === "MB")        return `${v.toFixed(0)}MB`;
  if (unit === "GB")        return `${v.toFixed(2)}GB`;
  if (unit === "mCPU")      return `${v.toFixed(0)}m`;
  if (unit === "KB/s")      return `${v.toFixed(1)}KB/s`;
  if (unit === "req/min")   return `${v.toFixed(0)} req/min`;
  if (unit === "calls/min") return `${v.toFixed(0)} calls/min`;
  if (unit === "pods")      return `${v.toFixed(0)} pods`;
  if (unit === "count")     return `${v.toFixed(0)}`;
  if (unit === "sessions")  return `${v.toFixed(0)} sess`;
  return `${v.toFixed(1)} ${unit}`.trim();
}

// ── Merge de timestamps para Recharts ──────────────────────────────────────────

function mergeSeries(series: DTMetricSeries[]): Record<string, number>[] {
  const map = new Map<number, Record<string, number>>();
  for (const s of series) {
    for (const pt of (s.points ?? [])) {
      if (!map.has(pt.t)) map.set(pt.t, { t: pt.t });
      map.get(pt.t)![s.key] = pt.v;
    }
  }
  return Array.from(map.values()).sort((a, b) => a.t - b.t);
}

// ── Tooltip customizado ────────────────────────────────────────────────────────

function ChartTooltip({ active, payload, label, seriesMap }: {
  active?: boolean;
  payload?: any[];
  label?: number;
  seriesMap: Record<string, DTMetricSeries>;
}) {
  if (!active || !payload?.length || label === undefined) return null;
  return (
    <div className="rounded border bg-background/98 p-2 text-xs shadow-xl backdrop-blur-sm">
      <p className="font-semibold mb-1.5 text-muted-foreground">{fmtTime(label)}</p>
      {payload.map((p: any, i: number) => {
        const s = seriesMap[p.dataKey];
        if (p.value === undefined || p.value === null) return null;
        return (
          <p key={i} className="flex items-center gap-1.5" style={{ color: p.color }}>
            <span className="inline-block w-2 h-2 rounded-full shrink-0" style={{ background: p.color }} />
            <span>{s?.label ?? p.dataKey}:</span>
            <strong>{fmtValue(p.value, s?.unit ?? "")}</strong>
          </p>
        );
      })}
    </div>
  );
}

// ── MetricChart (gráfico individual) ───────────────────────────────────────────

interface MetricChartProps {
  group: ChartGroup;
  availableSeries: DTMetricSeries[];
  problemStartMs: number;
  height?: number;
}

function MetricChart({ group, availableSeries, problemStartMs, height = 150 }: MetricChartProps) {
  if (availableSeries.length === 0) return null;

  const data = mergeSeries(availableSeries);
  if (data.length === 0) return null;

  const seriesMap = Object.fromEntries(availableSeries.map(s => [s.key, s]));

  // Para dual axis: primeira série → esquerda, demais → direita
  const leftUnit = availableSeries[0]?.unit ?? "";
  const rightUnit = availableSeries[1]?.unit ?? leftUnit;

  return (
    <div className="space-y-1">
      <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1.5">
        {group.icon}{group.title}
      </p>
      <ResponsiveContainer width="100%" height={height}>
        <ComposedChart data={data} margin={{ top: 4, right: group.dualAxis ? 60 : 12, bottom: 0, left: 6 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" />

          <XAxis
            dataKey="t"
            tickFormatter={fmtTime}
            tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
            tickLine={false}
            axisLine={false}
            minTickGap={50}
          />

          {/* Y esquerdo — largura suficiente para rótulos como "2.5KB/s" */}
          <YAxis
            yAxisId="left"
            tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
            tickLine={false}
            axisLine={false}
            width={52}
            tickFormatter={(v) => fmtTick(v, leftUnit)}
          />

          {/* Y direito (dual axis) */}
          {group.dualAxis && (
            <YAxis
              yAxisId="right"
              orientation="right"
              tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
              tickLine={false}
              axisLine={false}
              width={56}
              tickFormatter={(v) => fmtTick(v, rightUnit)}
            />
          )}

          <Tooltip
            content={<ChartTooltip seriesMap={seriesMap} />}
            cursor={{ stroke: "rgba(255,255,255,0.1)", strokeWidth: 1 }}
          />

          {/* Linha vertical: início do problem */}
          <ReferenceLine
            x={problemStartMs}
            yAxisId="left"
            stroke="#ef4444"
            strokeDasharray="4 2"
            strokeWidth={1.5}
            label={{ value: "Início", fill: "#ef4444", fontSize: 9, position: "insideTopRight" }}
          />

          {availableSeries.map((s, i) => {
            const color = colorFor(s.key, i);
            const axisId = group.dualAxis && i > 0 ? "right" : "left";

            if (group.style === "area" || (group.style === "lines" && availableSeries.length === 1)) {
              return (
                <Area
                  key={s.key}
                  type="monotone"
                  dataKey={s.key}
                  yAxisId={axisId}
                  stroke={color}
                  fill={color + "20"}
                  strokeWidth={1.5}
                  dot={false}
                  connectNulls
                  name={s.label}
                />
              );
            }

            if (group.style === "bar_plus_lines" && s.key === "pod_restarts") {
              return (
                <Bar
                  key={s.key}
                  dataKey={s.key}
                  yAxisId={axisId}
                  fill={color + "80"}
                  stroke={color}
                  strokeWidth={1}
                  name={s.label}
                />
              );
            }

            return (
              <Line
                key={s.key}
                type="monotone"
                dataKey={s.key}
                yAxisId={axisId}
                stroke={color}
                strokeWidth={i === 0 ? 2 : 1.5}
                dot={false}
                connectNulls
                name={s.label}
              />
            );
          })}

          {availableSeries.length > 1 && (
            <Legend
              wrapperStyle={{ fontSize: 9, paddingTop: 4 }}
              formatter={(value, entry: any) => {
                const s = seriesMap[entry.dataKey];
                return s?.label ?? value;
              }}
            />
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}

// ── EntityMetricsSection ────────────────────────────────────────────────────────

const ENTITY_TYPE_ICON: Record<string, React.ReactNode> = {
  SERVICE:                    <Activity className="h-3.5 w-3.5 text-blue-400" />,
  CLOUD_APPLICATION:          <Layers className="h-3.5 w-3.5 text-indigo-400" />,
  CLOUD_APPLICATION_INSTANCE: <Layers className="h-3.5 w-3.5 text-indigo-400" />,
  KUBERNETES_NODE:            <Server className="h-3.5 w-3.5 text-green-400" />,
  HOST:                       <Server className="h-3.5 w-3.5 text-green-400" />,
  PROCESS_GROUP:              <Cpu className="h-3.5 w-3.5 text-orange-400" />,
  PROCESS_GROUP_INSTANCE:     <Cpu className="h-3.5 w-3.5 text-orange-400" />,
  APPLICATION:                <BarChart3 className="h-3.5 w-3.5 text-purple-400" />,
};

const ENTITY_TYPE_LABEL: Record<string, string> = {
  SERVICE:                    "Serviço",
  CLOUD_APPLICATION:          "Workload K8s",
  CLOUD_APPLICATION_INSTANCE: "Pod",
  KUBERNETES_NODE:            "Nó K8s",
  HOST:                       "Host",
  PROCESS_GROUP:              "Grupo de Processos",
  PROCESS_GROUP_INSTANCE:     "Processo",
  APPLICATION:                "Aplicação",
};

export function EntityMetricsSection({ entity, problemStartMs, columns = 1 }: {
  entity: DTEntityMetrics;
  problemStartMs: number;
  /** 1 = layout vertical (padrão), 2 = grid 2 colunas (Diagnóstico) */
  columns?: 1 | 2;
}) {
  const byKey = Object.fromEntries(entity.metrics.map(m => [m.key, m]));
  const groups = groupsForType(entity.entityType);

  // Filtrar grupos que têm ao menos uma série com dados E pelo menos 2 pontos
  const visibleGroups = groups.map(g => ({
    group: g,
    series: g.keys.map(k => byKey[k]).filter(
      s => !!s && (s.points ?? []).filter(p => !isNaN(p.v)).length >= 2
    ) as DTMetricSeries[],
  })).filter(({ series }) => series.length > 0);

  // Fallback: métricas que chegaram mas não pertencem a nenhum grupo predefinido
  const coveredKeys = new Set(visibleGroups.flatMap(g => g.group.keys));
  const ungrouped = entity.metrics.filter(
    m => !coveredKeys.has(m.key) && (m.points ?? []).filter(p => !isNaN(p.v)).length >= 2,
  );

  // Criar grupos sintéticos para cada métrica não coberta (cada uma em chart individual)
  const fallbackGroups: { group: ChartGroup; series: DTMetricSeries[] }[] = ungrouped.map(m => ({
    group: { title: m.label, icon: <Activity className="h-3.5 w-3.5" />, keys: [m.key], style: "area" },
    series: [m],
  }));

  const allGroups = [...visibleGroups, ...fallbackGroups];

  if (allGroups.length === 0) {
    return (
      <div className="text-xs text-muted-foreground italic py-2">
        Nenhuma métrica disponível para esta entidade neste período.
      </div>
    );
  }

  const typeIcon = ENTITY_TYPE_ICON[entity.entityType] ?? <Server className="h-3.5 w-3.5" />;
  const typeLabel = ENTITY_TYPE_LABEL[entity.entityType] ?? entity.entityType;

  return (
    <div className="space-y-4">
      {/* Cabeçalho da entidade */}
      <div className="flex items-center gap-2 flex-wrap">
        {typeIcon}
        <span className="text-sm font-semibold">{entity.entityName}</span>
        <Badge variant="outline" className="text-[10px] font-mono">{typeLabel}</Badge>
        {entity.isRootCause && (
          <Badge className="text-[10px] bg-orange-500/20 text-orange-400 border-orange-500/30">
            Causa Raiz
          </Badge>
        )}
        <span className="text-[10px] text-muted-foreground font-mono ml-auto">{entity.entityId}</span>
      </div>

      {/* Gráficos por grupo */}
      <div className={columns === 2 ? "grid grid-cols-2 gap-4" : "space-y-5"}>
        {allGroups.map(({ group, series }) => (
          <div key={group.title} className={columns === 2 ? "rounded-lg border border-border/50 bg-muted/10 px-3 pt-2 pb-1" : undefined}>
            <MetricChart
              group={group}
              availableSeries={series}
              problemStartMs={problemStartMs}
              height={series.length > 2 ? 160 : 140}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

// ── DynatraceMetricsPanel (componente público) ─────────────────────────────────

interface DynatraceMetricsPanelProps {
  problemId: string;
  aiEmail: string;
  problemTitle: string;
}

export function DynatraceMetricsPanel({ problemId, aiEmail, problemTitle }: DynatraceMetricsPanelProps) {
  const { data, isLoading, isError, error, refetch, isRefetching } = useQuery({
    queryKey: ["dt-metrics", problemId, aiEmail],
    queryFn: () => apiClient.getDynatraceProblemMetrics(problemId, aiEmail),
    staleTime: 2 * 60_000,
    retry: 1,
  });

  if (isLoading || isRefetching) {
    return (
      <div className="flex flex-col items-center gap-3 py-10 text-center">
        <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
        <div className="space-y-1">
          <p className="text-sm font-medium">Coletando métricas do Dynatrace</p>
          <p className="text-xs text-muted-foreground">
            Buscando dados de todas as entidades afetadas em paralelo...
          </p>
        </div>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <AlertCircle className="h-6 w-6 text-red-400" />
        <p className="text-sm text-muted-foreground">
          {(error as any)?.message ?? "Erro ao carregar métricas"}
        </p>
        <Button size="sm" variant="outline" onClick={() => refetch()}>
          <RefreshCw className="h-3 w-3 mr-1.5" />Tentar novamente
        </Button>
      </div>
    );
  }

  if (!data) return null;

  const problemStartMs = new Date(data.problemStart).getTime();
  const timeFromStr = new Date(data.timeFrom).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
  const timeToStr = new Date(data.timeTo).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });

  const entitiesWithData = data.entities.filter(e => e.metrics.length > 0);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="space-y-0.5">
          <p className="text-xs text-muted-foreground">
            Janela: <strong>{timeFromStr}</strong> → <strong>{timeToStr}</strong>
            {" · "}linha vermelha = início do problem
          </p>
          <p className="text-[10px] text-muted-foreground">
            {entitiesWithData.length} entidade(s) com dados · {data.entities.reduce((s, e) => s + e.metrics.length, 0)} séries coletadas
          </p>
        </div>
        <Button size="sm" variant="ghost" className="h-7 w-7 p-0" onClick={() => refetch()} title="Atualizar métricas">
          <RefreshCw className="h-3 w-3" />
        </Button>
      </div>

      {entitiesWithData.length === 0 ? (
        <div className="text-center py-8 text-sm text-muted-foreground">
          <AlertCircle className="h-8 w-8 mx-auto mb-2 opacity-30" />
          Nenhuma métrica retornada. Verifique se o token tem escopo <code>metrics.read</code>.
        </div>
      ) : (
        <div className="space-y-8">
          {data.entities.map((entity) => (
            entity.metrics.length > 0 && (
              <div key={entity.entityId} className="space-y-4">
                <EntityMetricsSection entity={entity} problemStartMs={problemStartMs} />
                <div className="border-t border-dashed border-border/40" />
              </div>
            )
          ))}
        </div>
      )}
    </div>
  );
}
