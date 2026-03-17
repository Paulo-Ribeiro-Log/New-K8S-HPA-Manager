/**
 * DynatraceContextPanel — Evidências Davis AI (com mini charts), Topologia, Traces, Eventos.
 * Carrega contexto + métricas (compartilhadas via React Query cache com DynatraceMetricsPanel).
 */

import { useQuery } from "@tanstack/react-query";
import {
  ComposedChart, Area, ReferenceLine,
  XAxis, YAxis, ResponsiveContainer, Tooltip,
} from "recharts";
import { apiClient } from "@/lib/api/client";
import {
  DTEvidenceItem,
  DTTopoRelation,
  DTTraceEntry,
  DTContextEventEntry,
  DTProblemContext,
  DTProblemMetrics,
  DTMetricSeries,
} from "@/types/healthcheck";
import {
  AlertCircle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  Clock,
  GitMerge,
  Info,
  ListChecks,
  Network,
  Route,
  Zap,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";

// ─── Helpers ──────────────────────────────────────────────────────────────────

function fmtTime(iso: string) {
  return new Date(iso).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function fmtMs(ms: number) {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`;
  return `${ms.toFixed(0)}ms`;
}

function statusColor(code?: number) {
  if (!code) return "text-gray-400";
  if (code < 300) return "text-green-400";
  if (code < 400) return "text-yellow-400";
  return "text-red-400";
}

function entityTypeShort(t: string) {
  const map: Record<string, string> = {
    SERVICE: "SVC", CLOUD_APPLICATION: "K8s App", CLOUD_APPLICATION_INSTANCE: "Pod",
    KUBERNETES_NODE: "Node", HOST: "Host", PROCESS_GROUP: "PG", APPLICATION: "App",
  };
  return map[t] ?? t;
}

function evidenceTypeLabel(t: string) {
  const map: Record<string, string> = {
    EVENT: "Evento", METRIC: "Métrica", TRANSACTIONAL: "Transação",
    LOG: "Log", MAINTENANCE_WINDOW: "Manutenção",
  };
  return map[t] ?? t;
}

// ─── Sparkline para evidência ─────────────────────────────────────────────────

// Prioridade de métricas para exibir por tipo de entidade
const PRIMARY_METRICS: Record<string, string[]> = {
  SERVICE:                    ["response_p99", "response_p95", "error_rate", "throughput", "response_p50"],
  CLOUD_APPLICATION:          ["cpu_throttle", "cpu_milli", "pod_restarts", "memory_mb"],
  CLOUD_APPLICATION_INSTANCE: ["cpu_throttle", "cpu_milli", "pod_restarts", "memory_mb"],
  KUBERNETES_NODE:            ["node_cpu", "cpu_milli"],
  HOST:                       ["host_cpu", "host_mem_avail"],
  PROCESS_GROUP:              ["proc_cpu", "proc_mem"],
  APPLICATION:                ["app_response", "app_errors"],
};

const SERIES_COLORS: Record<string, string> = {
  response_p99: "#ef4444", response_p95: "#f97316", response_p50: "#6366f1",
  error_rate: "#ef4444", throughput: "#22c55e", cpu_throttle: "#dc2626",
  cpu_milli: "#f97316", pod_restarts: "#ef4444", memory_mb: "#8b5cf6",
  node_cpu: "#f97316", host_cpu: "#f97316", proc_cpu: "#f97316",
  app_response: "#f97316", app_errors: "#ef4444",
};

function pickPrimarySeriesForEntity(
  entityId: string,
  entityType: string,
  metricsData: DTProblemMetrics | undefined,
): { series: DTMetricSeries; color: string } | null {
  if (!metricsData) return null;
  const entityMetrics = metricsData.entities.find(e => e.entityId === entityId);
  if (!entityMetrics) return null;

  const metrics = entityMetrics.metrics ?? [];
  const byKey = Object.fromEntries(metrics.map(m => [m.key, m]));
  const priority = PRIMARY_METRICS[entityType] ?? PRIMARY_METRICS.SERVICE;

  for (const key of priority) {
    const s = byKey[key];
    if (s && (s.points ?? []).length > 1) {
      return { series: s, color: SERIES_COLORS[key] ?? "#6366f1" };
    }
  }

  // fallback: primeira série com dados
  const first = metrics.find(m => (m.points ?? []).length > 1);
  if (first) return { series: first, color: "#6366f1" };
  return null;
}

interface SparklineProps {
  series: DTMetricSeries;
  color: string;
  problemStartMs: number;
  height?: number;
}

function EvidenceSparkline({ series, color, problemStartMs, height = 64 }: SparklineProps) {
  const data = (series.points ?? []).map(p => ({ t: p.t, v: p.v }));
  if (data.length < 2) return null;

  const hasAnomaly = data.some(d => d.t >= problemStartMs);

  return (
    <div className="mt-2 rounded bg-black/20 border border-white/5 px-1 pt-1 pb-0.5">
      <div className="flex items-center justify-between px-1 mb-1">
        <span className="text-[9px] text-gray-500 uppercase tracking-wider">{series.label}</span>
        {hasAnomaly && (
          <span className="text-[9px] text-red-400 flex items-center gap-0.5">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse" />
            Anomalia
          </span>
        )}
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <ComposedChart data={data} margin={{ top: 2, right: 4, bottom: 0, left: 0 }}>
          <XAxis dataKey="t" hide />
          <YAxis hide domain={["auto", "auto"]} />
          <Tooltip
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null;
              const v = payload[0]?.value as number;
              return (
                <div className="bg-background border rounded px-2 py-1 text-[10px] shadow">
                  <span className="text-gray-400">{new Date(label).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" })}</span>
                  <span className="ml-2 font-semibold" style={{ color }}>
                    {v?.toFixed(series.unit === "ms" ? 1 : 2)} {series.unit}
                  </span>
                </div>
              );
            }}
            cursor={{ stroke: "rgba(255,255,255,0.1)" }}
          />
          <ReferenceLine
            x={problemStartMs}
            stroke="#ef4444"
            strokeDasharray="3 2"
            strokeWidth={1}
          />
          <Area
            type="monotone"
            dataKey="v"
            stroke={color}
            fill={color + "20"}
            strokeWidth={1.5}
            dot={false}
            connectNulls
          />
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}

// ─── Evidências Davis AI ──────────────────────────────────────────────────────

function EvidenceSection({
  items,
  metricsData,
  problemStartMs,
}: {
  items: DTEvidenceItem[];
  metricsData: DTProblemMetrics | undefined;
  problemStartMs: number;
}) {
  if (!items.length) return <EmptyState icon={<Zap size={16} />} msg="Nenhuma evidência Davis AI" />;

  const rootCauses = items.filter(e => e.rootCause);
  const others = items.filter(e => !e.rootCause);

  return (
    <div className="space-y-2">
      {rootCauses.length > 0 && (
        <div className="mb-3">
          <p className="text-[10px] font-semibold uppercase tracking-wider text-red-400 mb-1.5">Causa Raiz</p>
          <div className="space-y-3">
            {rootCauses.map(e => (
              <EvidenceRow key={e.entityId + e.evidenceType} item={e} isRoot
                metricsData={metricsData} problemStartMs={problemStartMs} />
            ))}
          </div>
        </div>
      )}
      {others.length > 0 && (
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-wider text-gray-400 mb-1.5">Contribuintes</p>
          <div className="space-y-3">
            {others.map(e => (
              <EvidenceRow key={e.entityId + e.evidenceType} item={e} isRoot={false}
                metricsData={metricsData} problemStartMs={problemStartMs} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function EvidenceRow({
  item, isRoot, metricsData, problemStartMs,
}: {
  item: DTEvidenceItem;
  isRoot: boolean;
  metricsData: DTProblemMetrics | undefined;
  problemStartMs: number;
}) {
  const spark = pickPrimarySeriesForEntity(item.entityId, item.entityType, metricsData);

  return (
    <div className={`rounded-lg border p-2.5 ${isRoot ? "bg-red-500/8 border-red-500/25" : "bg-white/5 border-white/10"}`}>
      {/* Cabeçalho da evidência */}
      <div className="flex items-start gap-2">
        <span className={`mt-0.5 shrink-0 ${isRoot ? "text-red-400" : "text-yellow-400"}`}>
          {isRoot ? <AlertCircle size={13} /> : <CheckCircle2 size={13} />}
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="font-medium text-xs text-white truncate">{item.entityName || item.entityId}</span>
            <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 border-gray-600 text-gray-400 shrink-0">
              {entityTypeShort(item.entityType)}
            </Badge>
          </div>
          <div className="text-[10px] text-gray-400 mt-0.5 flex items-center gap-1.5 flex-wrap">
            <span className="text-gray-300">{item.displayName}</span>
            <span className="text-gray-600">·</span>
            <span className="bg-gray-800 text-gray-400 px-1 rounded text-[9px]">{evidenceTypeLabel(item.evidenceType)}</span>
            <span className="text-gray-600">·</span>
            <span className="text-gray-500 flex items-center gap-0.5"><Clock size={9} /> {fmtTime(item.startTime)}</span>
          </div>
        </div>
      </div>

      {/* Mini gráfico da métrica anômala */}
      {spark && (
        <EvidenceSparkline
          series={spark.series}
          color={spark.color}
          problemStartMs={problemStartMs}
        />
      )}
    </div>
  );
}

// ─── Topologia ────────────────────────────────────────────────────────────────

function TopologySection({ items }: { items: DTTopoRelation[] }) {
  if (!items.length) return <EmptyState icon={<Network size={16} />} msg="Sem relações de topologia" />;

  const outgoing = items.filter(t => t.direction === "outgoing");
  const incoming = items.filter(t => t.direction === "incoming");

  return (
    <div className="space-y-3">
      {incoming.length > 0 && (
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-wider text-blue-400 mb-1 flex items-center gap-1">
            <ArrowDown size={11} /> Upstream — chama este serviço ({incoming.length})
          </p>
          <TopoTable items={incoming} />
        </div>
      )}
      {outgoing.length > 0 && (
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-wider text-purple-400 mb-1 flex items-center gap-1">
            <ArrowUp size={11} /> Downstream — chamado por este serviço ({outgoing.length})
          </p>
          <TopoTable items={outgoing} />
        </div>
      )}
    </div>
  );
}

function TopoTable({ items }: { items: DTTopoRelation[] }) {
  return (
    <table className="w-full text-xs border-collapse">
      <thead>
        <tr className="text-gray-500 border-b border-white/10">
          <th className="text-left py-1 pr-2 font-normal">Serviço / Entidade</th>
          <th className="text-left py-1 font-normal w-20">Tipo</th>
        </tr>
      </thead>
      <tbody>
        {items.map(r => (
          <tr key={r.entityId} className="border-b border-white/5 hover:bg-white/5">
            <td className="py-1 pr-2 text-white">
              <span className="block truncate max-w-[280px]">{r.entityName || r.entityId}</span>
            </td>
            <td className="py-1 text-gray-400 shrink-0">{entityTypeShort(r.entityType)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// ─── Distributed Traces ───────────────────────────────────────────────────────

function TracesSection({ items, tracesError }: { items: DTTraceEntry[]; tracesError?: string }) {
  if (tracesError) {
    const is403 = tracesError.includes("403");
    const is404 = tracesError.includes("404");
    const msg = is403
      ? "Token sem permissão para traces (requer escopo DataExport ou CaptureRequestData no token Dynatrace)"
      : is404
      ? "Endpoint de traces não disponível nesta instância Dynatrace"
      : `Erro ao buscar traces: ${tracesError}`;
    return (
      <div className="text-xs px-2 py-3 space-y-1">
        <p className="text-yellow-400 flex items-center gap-1.5">
          <AlertCircle size={12} className="shrink-0" /> {msg}
        </p>
        {is403 && (
          <p className="text-gray-500 pl-4">
            Adicione o escopo <code className="bg-gray-800 px-1 rounded">DataExport</code> ao token em{" "}
            <span className="text-gray-400">Dynatrace → Access Tokens → Edit</span>
          </p>
        )}
      </div>
    );
  }

  if (!items.length)
    return <EmptyState icon={<Route size={16} />} msg="Nenhum trace encontrado no período do problem" />;

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs border-collapse min-w-[480px]">
        <thead>
          <tr className="text-gray-500 border-b border-white/10">
            <th className="text-left py-1 pr-2 font-normal">Endpoint</th>
            <th className="text-left py-1 pr-2 font-normal w-14">Método</th>
            <th className="text-left py-1 pr-2 font-normal w-14">Status</th>
            <th className="text-right py-1 pr-2 font-normal w-20">Duração</th>
            <th className="text-left py-1 font-normal w-20">Início</th>
          </tr>
        </thead>
        <tbody>
          {items.map(t => (
            <tr key={t.traceId} className={`border-b border-white/5 hover:bg-white/5 ${t.hasError ? "bg-red-500/5" : ""}`}>
              <td className="py-1 pr-2 text-white">
                <span className="block truncate max-w-[260px]">{t.name}</span>
              </td>
              <td className="py-1 pr-2 text-gray-300">{t.httpMethod ?? "—"}</td>
              <td className={`py-1 pr-2 font-mono ${statusColor(t.httpStatus)}`}>{t.httpStatus ?? "—"}</td>
              <td className={`py-1 pr-2 text-right font-mono ${t.durationMs > 1000 ? "text-red-400" : "text-gray-300"}`}>
                {fmtMs(t.durationMs)}
              </td>
              <td className="py-1 text-gray-400">{fmtTime(t.startTime)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ─── Eventos ──────────────────────────────────────────────────────────────────

function EventsSection({ items, problemStart }: { items: DTContextEventEntry[]; problemStart: string }) {
  if (!items.length) return <EmptyState icon={<ListChecks size={16} />} msg="Sem eventos no período" />;

  const pStart = new Date(problemStart).getTime();

  return (
    <div className="space-y-0.5">
      {items.map(ev => {
        const evTime = new Date(ev.startTime).getTime();
        const isAfterStart = evTime >= pStart;
        return (
          <div key={ev.eventId} className={`flex items-start gap-2 px-2 py-1.5 rounded text-xs ${isAfterStart ? "bg-white/5" : "bg-white/[0.02]"}`}>
            <span className={`mt-0.5 shrink-0 ${isAfterStart ? "text-orange-400" : "text-gray-600"}`}>
              <Clock size={12} />
            </span>
            <div className="flex-1 min-w-0">
              <div className="flex items-baseline gap-1.5 flex-wrap">
                <span className="text-gray-400 font-mono text-[10px] shrink-0">{fmtTime(ev.startTime)}</span>
                <span className="text-white truncate">{ev.title}</span>
              </div>
              <div className="text-gray-500 text-[10px] mt-0.5 flex items-center gap-1">
                <span className="bg-gray-700 px-1 py-0 rounded text-[9px]">{ev.eventType}</span>
                <span className="truncate">{ev.entityName || ev.entityId}</span>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Empty / Section helpers ──────────────────────────────────────────────────

function EmptyState({ icon, msg }: { icon: React.ReactNode; msg: string }) {
  return (
    <div className="flex items-center gap-2 text-gray-500 text-xs py-4 px-2">
      <span className="text-gray-600">{icon}</span>
      <span>{msg}</span>
    </div>
  );
}

function Section({
  icon, title, count, children,
}: {
  icon: React.ReactNode;
  title: string;
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <div className="border border-white/10 rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 bg-white/5 border-b border-white/10">
        <span className="text-gray-400">{icon}</span>
        <span className="text-xs font-semibold text-gray-200">{title}</span>
        {count !== undefined && (
          <span className="ml-auto text-[10px] text-gray-500 bg-white/5 px-1.5 py-0.5 rounded-full">{count}</span>
        )}
      </div>
      <div className="p-3">{children}</div>
    </div>
  );
}

// ─── DynatraceContextPanel ────────────────────────────────────────────────────

interface Props {
  problemId: string;
  aiEmail: string;
}

export function DynatraceContextPanel({ problemId, aiEmail }: Props) {
  // Contexto: evidências, topologia, traces, eventos
  const { data, isLoading, isError, error } = useQuery<DTProblemContext>({
    queryKey: ["dt-context", problemId, aiEmail],
    queryFn: () => apiClient.getDynatraceProblemContext(problemId, aiEmail),
    staleTime: 2 * 60_000,
    retry: 1,
  });

  // Métricas: compartilhadas via cache com DynatraceMetricsPanel (sem double-fetch)
  const { data: metricsData } = useQuery<DTProblemMetrics>({
    queryKey: ["dt-metrics", problemId, aiEmail],
    queryFn: () => apiClient.getDynatraceProblemMetrics(problemId, aiEmail),
    staleTime: 2 * 60_000,
    retry: 1,
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-400 text-sm py-6 justify-center">
        <div className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin" />
        Coletando evidências, topologia e traces…
      </div>
    );
  }

  if (isError) {
    return (
      <div className="text-red-400 text-xs px-3 py-4">
        Erro ao carregar contexto: {(error as Error)?.message ?? "Desconhecido"}
      </div>
    );
  }

  if (!data) return null;

  const problemStartMs = new Date(data.problemStart).getTime();

  return (
    <div className="space-y-3">
      <Section icon={<Zap size={14} />} title="Evidências Davis AI" count={data.evidence?.length ?? 0}>
        <EvidenceSection
          items={data.evidence ?? []}
          metricsData={metricsData}
          problemStartMs={problemStartMs}
        />
      </Section>

      <Section icon={<GitMerge size={14} />} title="Topologia" count={data.topology?.length ?? 0}>
        <TopologySection items={data.topology ?? []} />
      </Section>

      {/* Traces: ocultar se 404 (endpoint não disponível nesta instância) */}
      {!data.tracesError?.includes("404") && (
        <Section
          icon={data.tracesError ? <Info size={14} className="text-yellow-400" /> : <Route size={14} />}
          title="Traces Distribuídos"
          count={data.tracesError ? undefined : (data.traces?.length ?? 0)}
        >
          <TracesSection items={data.traces ?? []} tracesError={data.tracesError} />
        </Section>
      )}

      <Section icon={<ListChecks size={14} />} title="Eventos" count={data.events?.length ?? 0}>
        <EventsSection items={data.events ?? []} problemStart={data.problemStart} />
      </Section>
    </div>
  );
}
