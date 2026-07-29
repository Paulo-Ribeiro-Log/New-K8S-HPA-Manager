import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Loader2, RefreshCw, AlertTriangle } from "lucide-react";
import { ComposedChart, Bar, Line, XAxis, YAxis, ReferenceLine, ReferenceArea } from "recharts";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { apiClient } from "@/lib/api/client";
import type { DeploymentBehaviorResponse, DeploymentBehaviorPoint } from "@/lib/api/types";
import { formatMillicores, formatBytes } from "@/lib/monitorUtils";
import { COMPARE_DAYS, COMPARE_COLORS, COMPARE_LABELS, compareColorForDataKey, compareLabelForDataKey, decimate } from "@/lib/chartHelpers";

interface Props {
  cluster: string;
  namespace: string;
  deployment: string;
}

type ChartRow = Record<string, number | string>;

// Janelas de tempo disponíveis no seletor — em minutos, pra caber as opções abaixo de 1h (30min).
const WINDOW_OPTIONS = [
  { label: "30m", minutes: 30 },
  { label: "1h", minutes: 60 },
  { label: "2h", minutes: 120 },
  { label: "3h", minutes: 180 },
  { label: "6h", minutes: 360 },
  { label: "12h", minutes: 720 },
  { label: "24h", minutes: 1440 },
];

// step (minutos por ponto) escalado com a janela — evita tanto pontos demais numa janela curta
// (1 ponto/min numa janela de 24h seria 1440 pontos) quanto poucos demais numa janela curta (5min
// de step numa janela de 30min daria só 6 pontos).
function stepForWindow(minutes: number): number {
  if (minutes <= 30) return 1;
  if (minutes <= 120) return 2;
  if (minutes <= 180) return 5;
  if (minutes <= 360) return 5;
  if (minutes <= 720) return 10;
  return 15;
}

const repChartConfig = {
  replicas_desired: { label: "Desejadas", color: "#6366f1" },
  replicas_current: { label: "Atuais", color: "#3b82f6" },
  replicas_ready: { label: "Prontas", color: "#22c55e" },
} satisfies ChartConfig;

const usageChartConfig = {
  cpu: { label: "CPU %", color: "#3b82f6" },
  memory: { label: "Memória %", color: "#a855f7" },
} satisfies ChartConfig;

const restartsChartConfig = {
  restarts: { label: "Restarts", color: "#ef4444" },
} satisfies ChartConfig;

function toTimeLabel(tsMs: number): string {
  return new Date(tsMs).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
}

function round1(n: number): number {
  return Math.round(n * 10) / 10;
}

// Cores por severidade — mesma paleta de severidade já usada em outras partes do app (Dynatrace
// SeverityLevel: AVAILABILITY | ERROR | PERFORMANCE | RESOURCE_CONTENTION | CUSTOM_ALERT).
// AVAILABILITY/ERROR são os mais graves (serviço fora do ar) → vermelho; PERFORMANCE/
// RESOURCE_CONTENTION → âmbar (degradação, não indisponibilidade); CUSTOM_ALERT/desconhecido →
// cinza-azulado neutro.
const DT_PROBLEM_SEVERITY_COLOR: Record<string, string> = {
  AVAILABILITY: "#ef4444",
  ERROR: "#f97316",
  PERFORMANCE: "#eab308",
  RESOURCE_CONTENTION: "#f59e0b",
  CUSTOM_ALERT: "#a855f7",
};

function severityColor(severity: string): string {
  return DT_PROBLEM_SEVERITY_COLOR[severity] ?? "#94a3b8";
}

// DeploymentBehaviorChart — gráfico de comportamento do Deployment (réplicas, CPU/memória %,
// restarts) ao longo do tempo. Ver DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md (Fases 1-2). Prometheus é a
// fonte primária; Dynatrace entra como fallback real de série temporal quando o cluster não tem
// Prometheus — funciona em qualquer cloud provider onde o workload tenha uma entidade Dynatrace
// CLOUD_APPLICATION resolvível (Cloud Native Full Stack), não só AKS (confirmado com um cluster
// EKS real que usa Dynatrace). Sem nenhuma das duas fontes disponível, mostra estado vazio
// explícito (não erro genérico).
//
// Overlay de problems Dynatrace (Fase 2, dtProblemMarkers/ReferenceArea abaixo) é aditivo e
// independente de qual fonte alimentou a série — aparece mesmo com source="prometheus", desde
// que a entidade do workload resolva no Dynatrace.
//
// Comparação D-1/D-2/D-3 (opt-in, nunca automático) é aplicada só ao painel de Réplicas
// (replicas_current) — decisão de escopo: aplicar a mesma comparação aos 3 painéis triplicaria a
// complexidade do componente sem ganho proporcional pro caso de uso principal (troubleshooting de
// "por que este pod reiniciou" se apoia mais em quando as réplicas mudaram do que em CPU/mem
// histórico ponto a ponto).
export function DeploymentBehaviorChart({ cluster, namespace, deployment }: Props) {
  const [windowMinutes, setWindowMinutes] = useState(360);
  const [compareOffsets, setCompareOffsets] = useState<number[]>([]);
  const [data, setData] = useState<DeploymentBehaviorResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchBehavior = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // Bug real corrigido: aiEmail nunca era enviado aqui, mesmo o client.ts já suportando o
      // parâmetro — sem ele, o backend nunca acha o token do Dynatrace salvo do usuário
      // (dynatraceClientForBehavior falha) e o fallback/overlay de problems (Fases 1-2) nunca
      // tinham chance de funcionar pela UI de verdade, só nos testes manuais via curl com
      // ai_email explícito. Mesma fonte já usada por PodsPanel/DeploymentsTab/DaemonSetsTab.
      const aiEmail = localStorage.getItem("ai_email") ?? undefined;
      const resp = await apiClient.getDeploymentBehavior(cluster, namespace, deployment, {
        minutes: windowMinutes,
        step: stepForWindow(windowMinutes),
        offsetDays: compareOffsets.length > 0 ? compareOffsets : undefined,
        aiEmail,
      });
      setData(resp);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Erro ao carregar comportamento do Deployment.");
    } finally {
      setLoading(false);
    }
  }, [cluster, namespace, deployment, windowMinutes, compareOffsets]);

  useEffect(() => {
    fetchBehavior();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, namespace, deployment, windowMinutes, compareOffsets]);

  const { chartData, decimatedPoints } = useMemo(() => {
    if (!data?.points?.length) return { chartData: [] as ChartRow[], decimatedPoints: [] as DeploymentBehaviorPoint[] };
    const mainPts = decimate(data.points);
    const compareSeries = compareOffsets.map((offset) => ({
      offset,
      pts: decimate(data.compare_points?.[offset] ?? []),
    }));
    const rows: ChartRow[] = mainPts.map((p, idx) => {
      const row: ChartRow = {
        time: toTimeLabel(p.ts),
        replicas_desired: p.replicas_desired,
        replicas_current: p.replicas_current,
        replicas_ready: p.replicas_ready,
        cpu: round1(p.cpu_usage_pct),
        memory: round1(p.memory_usage_pct),
        restarts: p.restarts,
      };
      compareSeries.forEach(({ offset, pts }) => {
        const cp = pts[idx];
        if (cp) row[`replicasD${offset}`] = cp.replicas_current;
      });
      return row;
    });
    return { chartData: rows, decimatedPoints: mainPts };
  }, [data, compareOffsets]);

  // Mapeia cada scale event pro rótulo de tempo do ponto decimado mais próximo — os eventos vêm
  // com o timestamp exato, mas o eixo X do gráfico usa strings decimadas (~48 amostras), então
  // precisamos achar a categoria mais próxima pra plotar a ReferenceLine vertical.
  const scaleEventMarkers = useMemo(() => {
    if (!data?.scale_events?.length || decimatedPoints.length === 0) return [];
    return data.scale_events.map((ev) => {
      let bestIdx = 0;
      let bestDiff = Infinity;
      decimatedPoints.forEach((p, idx) => {
        const diff = Math.abs(p.ts - ev.ts);
        if (diff < bestDiff) {
          bestDiff = diff;
          bestIdx = idx;
        }
      });
      return { time: String(chartData[bestIdx]?.time ?? ""), event: ev };
    }).filter((m) => m.time !== "");
  }, [data, decimatedPoints, chartData]);

  // Overlay de problems Dynatrace (Fase 2) — mesma técnica de scaleEventMarkers: mapeia o
  // timestamp real de início/fim pro rótulo de tempo do ponto decimado mais próximo, já que o
  // eixo X usa strings decimadas, não os timestamps originais. EndTs ausente (problem ainda OPEN)
  // usa o último ponto da janela como fim da área sombreada.
  const dtProblemMarkers = useMemo(() => {
    if (!data?.dynatrace_problems?.length || decimatedPoints.length === 0) return [];
    const nearestLabel = (ts: number) => {
      let bestIdx = 0;
      let bestDiff = Infinity;
      decimatedPoints.forEach((p, idx) => {
        const diff = Math.abs(p.ts - ts);
        if (diff < bestDiff) {
          bestDiff = diff;
          bestIdx = idx;
        }
      });
      return String(chartData[bestIdx]?.time ?? "");
    };
    return data.dynatrace_problems
      .map((p) => ({
        problemId: p.problem_id,
        title: p.title,
        severity: p.severity,
        x1: nearestLabel(p.start_ts),
        x2: p.end_ts != null ? nearestLabel(p.end_ts) : String(chartData[chartData.length - 1]?.time ?? ""),
      }))
      .filter((m) => m.x1 !== "" && m.x2 !== "");
  }, [data, decimatedPoints, chartData]);

  const xInterval = Math.max(0, Math.floor(chartData.length / 6) - 1);

  const toggleCompare = (offset: number) => {
    setCompareOffsets((prev) => (prev.includes(offset) ? prev.filter((o) => o !== offset) : [...prev, offset].sort((a, b) => a - b)));
  };

  if (loading && !data) {
    return (
      <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
        <Loader2 className="w-4 h-4 animate-spin" /> Carregando comportamento...
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-center">
        <AlertTriangle className="w-5 h-5 text-destructive" />
        <p className="text-sm text-destructive">{error}</p>
        <Button size="sm" variant="outline" onClick={fetchBehavior}>Tentar de novo</Button>
      </div>
    );
  }

  if (!data || data.source === "none") {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-10 text-center text-sm text-muted-foreground">
        <AlertTriangle className="w-5 h-5 text-amber-500" />
        <p>Nenhuma fonte de métricas históricas disponível para este cluster.</p>
        <p className="text-xs">Requer Prometheus instalado no cluster, ou Dynatrace configurado com o workload resolvível como CLOUD_APPLICATION.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4 p-1">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={String(windowMinutes)} onValueChange={(v) => setWindowMinutes(Number(v))}>
          <SelectTrigger className="h-7 text-xs w-24">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {WINDOW_OPTIONS.map((opt) => (
              <SelectItem key={opt.minutes} value={String(opt.minutes)} className="text-xs">{opt.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <span
          className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${
            data.source === "dynatrace"
              ? "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300"
              : "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300"
          }`}
        >
          {data.source === "dynatrace" ? "Dynatrace" : "Prometheus"}
        </span>

        {data.has_hpa && (
          <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
            HPA ativo
          </span>
        )}

        <div className="flex-1" />

        {data.source === "prometheus" && (
          <div className="flex items-center gap-1">
            <span className="text-[10px] text-muted-foreground">Comparar:</span>
            {COMPARE_DAYS.map((d) => {
              const active = compareOffsets.includes(d);
              return (
                <button
                  key={d}
                  onClick={() => toggleCompare(d)}
                  className="h-6 px-2 rounded text-[10px] font-mono font-medium border transition-colors"
                  style={active ? { backgroundColor: `${COMPARE_COLORS[d]}22`, borderColor: COMPARE_COLORS[d], color: COMPARE_COLORS[d] } : undefined}
                >
                  {COMPARE_LABELS[d]}
                </button>
              );
            })}
          </div>
        )}

        <Button size="sm" variant="outline" className="h-7 text-xs" onClick={fetchBehavior} disabled={loading}>
          {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
        </Button>
      </div>

      {chartData.length === 0 ? (
        <p className="text-sm text-muted-foreground text-center py-6">Sem pontos na janela selecionada.</p>
      ) : (
        <>
          {/* Painel 1 — Réplicas */}
          <div>
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">
              Réplicas
              {scaleEventMarkers.length > 0 && ` · ${scaleEventMarkers.length} mudança${scaleEventMarkers.length !== 1 ? "s" : ""} de escala`}
              {dtProblemMarkers.length > 0 && ` · ${dtProblemMarkers.length} problema${dtProblemMarkers.length !== 1 ? "s" : ""} Dynatrace`}
            </p>
            <ChartContainer config={repChartConfig} className="h-[140px] w-full">
              <ComposedChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                <YAxis tick={{ fontSize: 9 }} tickLine={false} axisLine={false} allowDecimals={false} />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={(l) => `Horário: ${l}`}
                      formatter={(value, _name, item) => {
                        const key = String(item.dataKey);
                        const isCompare = /D\d+$/.test(key);
                        const color = isCompare ? compareColorForDataKey(key) : (repChartConfig[key as keyof typeof repChartConfig]?.color ?? "#94a3b8");
                        const label = isCompare ? compareLabelForDataKey(key) : (repChartConfig[key as keyof typeof repChartConfig]?.label ?? key);
                        return (
                          <>
                            <span className="h-2.5 w-2.5 rounded-[2px] shrink-0" style={{ backgroundColor: color }} />
                            <div className="flex flex-1 justify-between items-center leading-none gap-3">
                              <span className="text-muted-foreground">{label}</span>
                              <span className="font-mono font-medium tabular-nums text-foreground">{Number(value)}</span>
                            </div>
                          </>
                        );
                      }}
                    />
                  }
                />
                {dtProblemMarkers.map((m) => (
                  <ReferenceArea key={m.problemId} x1={m.x1} x2={m.x2} fill={severityColor(m.severity)} fillOpacity={0.15} stroke={severityColor(m.severity)} strokeOpacity={0.5} strokeWidth={1} ifOverflow="extendDomain" />
                ))}
                {scaleEventMarkers.map((m, i) => (
                  <ReferenceLine key={i} x={m.time} stroke="#f59e0b" strokeDasharray="3 3" strokeWidth={1} />
                ))}
                <Line type="stepAfter" dataKey="replicas_desired" stroke={repChartConfig.replicas_desired.color} strokeWidth={1.5} dot={false} />
                <Line type="monotone" dataKey="replicas_current" stroke={repChartConfig.replicas_current.color} strokeWidth={1.5} dot={false} />
                <Line type="monotone" dataKey="replicas_ready" stroke={repChartConfig.replicas_ready.color} strokeWidth={1.5} dot={false} />
                {compareOffsets.map((offset) => (
                  <Line key={offset} type="monotone" dataKey={`replicasD${offset}`} stroke={COMPARE_COLORS[offset]}
                    strokeWidth={1.5} strokeDasharray="5 3" dot={false} connectNulls />
                ))}
              </ComposedChart>
            </ChartContainer>
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground justify-end flex-wrap mt-0.5">
              <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_desired.color }} />Desejadas</span>
              <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_current.color }} />Atuais</span>
              <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_ready.color }} />Prontas</span>
              {compareOffsets.map((offset) => (
                <span key={offset} className="flex items-center gap-1">
                  <span className="inline-block w-3 h-0.5" style={{ backgroundColor: COMPARE_COLORS[offset] }} />
                  {COMPARE_LABELS[offset]}
                </span>
              ))}
            </div>
            {/* Lista dos problems Dynatrace na janela — ReferenceArea não tem tooltip nativo no
                Recharts, então o título/severidade só ficam acessíveis aqui (title="..." no hover). */}
            {data.dynatrace_problems && data.dynatrace_problems.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-1">
                {data.dynatrace_problems.map((p) => (
                  <span
                    key={p.problem_id}
                    title={`${p.title} (${p.severity}${p.end_ts ? "" : " · em aberto"})`}
                    className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border cursor-help"
                    style={{ borderColor: severityColor(p.severity), color: severityColor(p.severity) }}
                  >
                    <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: severityColor(p.severity) }} />
                    <span className="truncate max-w-[160px]">{p.title}</span>
                  </span>
                ))}
              </div>
            )}
          </div>

          {/* Painel 2 — CPU / Memória */}
          <div>
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">
              CPU / Memória {data.source === "prometheus" ? "(% do request)" : ""}
            </p>
            {data.source === "dynatrace" ? (
              <p className="text-xs text-muted-foreground py-3">
                Uso de CPU/memória indisponível no fallback Dynatrace — sem uma fonte de request/limit
                nesse caminho pra normalizar em %.
              </p>
            ) : (
              <ChartContainer config={usageChartConfig} className="h-[140px] w-full">
                <ComposedChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                  <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                  <YAxis domain={[0, 100]} unit="%" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} />
                  <ChartTooltip content={<ChartTooltipContent labelFormatter={(l) => `Horário: ${l}`} />} />
                  {dtProblemMarkers.map((m) => (
                    <ReferenceArea key={m.problemId} x1={m.x1} x2={m.x2} fill={severityColor(m.severity)} fillOpacity={0.15} stroke={severityColor(m.severity)} strokeOpacity={0.5} strokeWidth={1} ifOverflow="extendDomain" />
                  ))}
                  <ReferenceLine y={80} stroke="#eab308" strokeDasharray="4 3" strokeWidth={1} />
                  <ReferenceLine y={95} stroke="#ef4444" strokeDasharray="4 3" strokeWidth={1} />
                  <Line type="monotone" dataKey="cpu" stroke={usageChartConfig.cpu.color} strokeWidth={1.5} dot={false} />
                  <Line type="monotone" dataKey="memory" stroke={usageChartConfig.memory.color} strokeWidth={1.5} dot={false} />
                </ComposedChart>
              </ChartContainer>
            )}
            {(data.cpu_request_millicores || data.cpu_limit_millicores || data.memory_request_bytes || data.memory_limit_bytes) ? (
              <p className="text-[10px] text-muted-foreground mt-0.5">
                CPU request/limit: {formatMillicores(data.cpu_request_millicores ?? 0)} / {formatMillicores(data.cpu_limit_millicores ?? 0)}
                {" · "}
                Memória request/limit: {formatBytes(data.memory_request_bytes ?? 0)} / {formatBytes(data.memory_limit_bytes ?? 0)}
              </p>
            ) : null}
          </div>

          {/* Painel 3 — Restarts */}
          <div>
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Restarts por intervalo</p>
            <ChartContainer config={restartsChartConfig} className="h-[90px] w-full">
              <ComposedChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                <YAxis tick={{ fontSize: 9 }} tickLine={false} axisLine={false} allowDecimals={false} />
                <ChartTooltip content={<ChartTooltipContent labelFormatter={(l) => `Horário: ${l}`} />} />
                <Bar dataKey="restarts" fill={restartsChartConfig.restarts.color} radius={[2, 2, 0, 0]} />
              </ComposedChart>
            </ChartContainer>
          </div>
        </>
      )}
    </div>
  );
}
