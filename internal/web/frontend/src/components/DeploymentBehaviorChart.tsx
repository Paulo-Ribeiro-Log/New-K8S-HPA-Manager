import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Loader2, RefreshCw, AlertTriangle, ExternalLink } from "lucide-react";
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
  // podName (opcional): nome do pod que abriu esta aba (PodQuickViewModal) — habilita o toggle
  // "Este pod / Deployment inteiro". Sem isso (chamador não sabe/não tem um pod específico), o
  // toggle não aparece e o gráfico permanece só no agregado do Deployment, como sempre foi.
  podName?: string;
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

// usageAbsoluteChartConfig — caminho Dynatrace: sem fonte de request/limit pra normalizar em %
// (usageChartConfig acima), mas o valor ABSOLUTO de uso em si é real (builtin:kubernetes.workload.
// cpu_usage/memory_working_set, ver k8sWorkloadMetricDefs no backend) — melhor mostrar isso do que
// nada. Eixos separados (não dualAxis) de propósito: mCores e MB têm escalas tipicamente muito
// diferentes (ex: ~500m vs ~50000MB observado ao vivo) — um único eixo esmagaria a série menor.
const usageAbsoluteChartConfig = {
  cpu_absolute: { label: "CPU", color: "#3b82f6" },
  memory_absolute: { label: "Memória", color: "#a855f7" },
} satisfies ChartConfig;

const restartsChartConfig = {
  restarts: { label: "Restarts", color: "#ef4444" },
} satisfies ChartConfig;

const networkChartConfig = {
  network_in: { label: "Entrada (IN)", color: "#06b6d4" },
  network_out: { label: "Saída (OUT)", color: "#f97316" },
} satisfies ChartConfig;

// formatBytesPerSec formata bytes/s reaproveitando formatBytes (monitorUtils) — mesmo padrão de
// "nunca calcular/formatar inline" já seguido pelo resto do componente (formatMillicores acima).
function formatBytesPerSec(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}

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

// dtProblemUrl monta o link "Abrir no Dynatrace" de um problem — mesmo padrão já usado no botão
// equivalente de DynatraceTab.tsx. baseUrl vem de DeploymentBehaviorResponse.dynatrace_ui_base_url
// (já normalizado pro domínio .apps.dynatrace.com da UI, não .live.dynatrace.com da API).
function dtProblemUrl(baseUrl: string | undefined, problemId: string): string | null {
  if (!baseUrl) return null;
  return `${baseUrl}/ui/apps/dynatrace.davis.problems/problem/${problemId}`;
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
export function DeploymentBehaviorChart({ cluster, namespace, deployment, podName }: Props) {
  const [windowMinutes, setWindowMinutes] = useState(360);
  const [compareOffsets, setCompareOffsets] = useState<number[]>([]);
  const [data, setData] = useState<DeploymentBehaviorResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Toggle "Este pod / Deployment inteiro" — ver overview de design em
  // DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md ("Comportamento" mede o Deployment por padrão, não o pod
  // selecionado — ephemeral pods teriam histórico quase vazio se fosse o padrão). Começa em
  // "deployment" mesmo quando podName está disponível: preserva o comportamento de sempre por
  // padrão, o usuário opta conscientemente por "Este pod" quando quiser esse recorte.
  const [scope, setScope] = useState<"deployment" | "pod">("deployment");
  const effectivePod = scope === "pod" && podName ? podName : undefined;

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
        pod: effectivePod,
      });
      setData(resp);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Erro ao carregar comportamento do Deployment.");
    } finally {
      setLoading(false);
    }
  }, [cluster, namespace, deployment, windowMinutes, compareOffsets, effectivePod]);

  useEffect(() => {
    fetchBehavior();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, namespace, deployment, windowMinutes, compareOffsets, effectivePod]);

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
        cpu_absolute: p.cpu_usage_millicores ?? 0,
        memory_absolute: p.memory_usage_mb ?? 0,
        restarts: p.restarts,
        network_in: p.network_in_bytes_sec,
        network_out: p.network_out_bytes_sec,
      };
      compareSeries.forEach(({ offset, pts }) => {
        const cp = pts[idx];
        if (cp) row[`replicasD${offset}`] = cp.replicas_current;
      });
      return row;
    });
    return { chartData: rows, decimatedPoints: mainPts };
  }, [data, compareOffsets]);

  // hasAbsoluteUsage — só o caminho Dynatrace popula cpu_usage_millicores/memory_usage_mb; some
  // pontos podem legitimamente vir zerados (workload ocioso), então checa QUALQUER ponto > 0 em
  // vez de assumir presença só pela fonte ser "dynatrace" — mais fiel ao dado real recebido.
  const hasAbsoluteUsage = useMemo(
    () => (data?.points ?? []).some((p) => (p.cpu_usage_millicores ?? 0) > 0 || (p.memory_usage_mb ?? 0) > 0),
    [data]
  );

  // hasPctUsage — backend agora calcula cpu_usage_pct/memory_usage_pct também no caminho
  // Dynatrace (via request/limit buscado direto na API do K8s, independente do Prometheus — ver
  // getDeploymentResourceLimitsFromK8s), então essa checagem NÃO depende mais de data.source: %
  // pode vir populado nos dois caminhos, ou em nenhum (Deployment sem request configurado).
  const hasPctUsage = useMemo(
    () => (data?.points ?? []).some((p) => (p.cpu_usage_pct ?? 0) > 0 || (p.memory_usage_pct ?? 0) > 0),
    [data]
  );

  // hasCurrentReplicas/hasReadyReplicas — no caminho Dynatrace, só "desejadas" tem cobertura real
  // (nenhum metricId CLOUD_APPLICATION confirmado pra "rodando agora"/"pronto agora" — ver
  // k8sWorkloadMetricDefs). Sem essa checagem, o painel de Réplicas desenhava uma linha "Atuais: 0"
  // constante — visualmente indistinguível de "0 réplicas rodando" (alarmante e falso), quando na
  // verdade é só "não temos esse dado nesse caminho".
  const hasCurrentReplicas = useMemo(() => (data?.points ?? []).some((p) => p.replicas_current > 0), [data]);
  const hasReadyReplicas = useMemo(() => (data?.points ?? []).some((p) => p.replicas_ready > 0), [data]);

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

  // Bug real corrigido: o Toolbar (seletor de janela, badges, refresh) antes só era renderizado
  // dentro do "caminho feliz" — qualquer early-return (loading inicial, erro, ou sem fonte de
  // dados) escondia o seletor de janela junto, deixando o usuário sem como trocar pra uma janela
  // com dados nem como fechar/atualizar: ao escolher uma janela sem nenhum evento, a tela ficava
  // presa sem nenhum controle visível. Toolbar agora é sempre renderizado; só o conteúdo abaixo
  // dele varia por estado (loading/erro/vazio/gráfico).
  //
  // Segundo bug real corrigido (mesma rodada): o antigo early-return de "sem dados" disparava
  // sempre que source==="none", mesmo quando dynatrace_problems vinha populado — tornando o
  // overlay da Fase 2 inalcançável em qualquer cluster sem série de métricas (ex: Prometheus
  // indisponível + métricas k8sWorkloadMetricDefs vazias no Dynatrace, caso real de clusters cuja
  // telemetria de runtime vem via OpenTelemetry em vez do caminho padrão do OneAgent). Confirmado
  // contra um cluster real: source="none"/points=0 mas dynatrace_problems com 1 problem real.
  // Agora só cai no estado vazio total quando NÃO há nem série nem problems.
  const showEmptyState = !loading && (!data || (data.source === "none" && !data.dynatrace_problems?.length));

  // replicaCountLabel — última contagem conhecida de réplicas, só pra dar contexto no rótulo de
  // escopo abaixo ("Deployment inteiro (N réplicas)"). Prefere replicas_current; cai pra
  // replicas_desired se o caminho atual não populou "atuais" (ver hasCurrentReplicas acima).
  const lastPoint = data?.points?.[data.points.length - 1];
  const replicaCountLabel = lastPoint
    ? (lastPoint.replicas_current || lastPoint.replicas_desired || undefined)
    : undefined;

  return (
    <div className="space-y-4 p-1">
      {/* Rótulo de escopo — sempre visível, evita a confusão de "por que isso não bate com o pod
          que eu abri" (achado real reportado pelo usuário: sem isso, nada no gráfico deixava
          claro que os dados são do Deployment inteiro, não do pod selecionado). */}
      <div className="text-xs text-muted-foreground">
        {effectivePod ? (
          <>Comportamento do pod <span className="font-mono text-foreground">{effectivePod}</span></>
        ) : (
          <>
            Comportamento do Deployment <span className="font-medium text-foreground">{deployment}</span>
            {replicaCountLabel ? ` (${replicaCountLabel} réplica${replicaCountLabel === 1 ? "" : "s"})` : ""}
            {" — dados agregados de todos os pods, não só o selecionado"}
          </>
        )}
      </div>

      {data?.pod && !data.pod_scoped && (
        <div className="text-[11px] text-amber-700 dark:text-amber-400 bg-amber-500/10 border border-amber-500/30 rounded px-2 py-1">
          {data.source === "dynatrace"
            ? "Não foi possível isolar este pod no Dynatrace — mostrando o Deployment inteiro. Granularidade por pod exige Cloud Native Full Stack (a maioria dos clusters usa classicFullStack) e dados recentes o bastante do pod (métricas de container somem pouco depois de ele terminar)."
            : `A fonte atual (${data.source}) não suporta granularidade por pod — mostrando o Deployment inteiro mesmo assim.`}
        </div>
      )}

      {/* Toolbar — sempre visível, mesmo em loading/erro/estado vazio */}
      <div className="flex flex-wrap items-center gap-2">
        {podName && (
          <div className="flex items-center rounded-md border border-border overflow-hidden">
            <button
              onClick={() => setScope("pod")}
              className={`h-7 px-2 text-[11px] font-medium transition-colors ${
                scope === "pod" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted"
              }`}
            >
              Este pod
            </button>
            <button
              onClick={() => setScope("deployment")}
              className={`h-7 px-2 text-[11px] font-medium transition-colors ${
                scope === "deployment" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted"
              }`}
            >
              Deployment inteiro
            </button>
          </div>
        )}

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

        {data && data.source !== "none" && (
          <span
            className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${
              data.source === "dynatrace"
                ? "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300"
                : "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300"
            }`}
          >
            {data.source === "dynatrace" ? "Dynatrace" : "Prometheus"}
          </span>
        )}

        {data?.has_hpa && (
          <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
            HPA ativo
          </span>
        )}

        <div className="flex-1" />

        {data?.source === "prometheus" && (
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

      {loading && !data ? (
        <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
          <Loader2 className="w-4 h-4 animate-spin" /> Carregando comportamento...
        </div>
      ) : error ? (
        <div className="flex flex-col items-center gap-2 py-10 text-center">
          <AlertTriangle className="w-5 h-5 text-destructive" />
          <p className="text-sm text-destructive">{error}</p>
          <Button size="sm" variant="outline" onClick={fetchBehavior}>Tentar de novo</Button>
        </div>
      ) : showEmptyState ? (
        <div className="flex flex-col items-center justify-center gap-2 py-10 text-center text-sm text-muted-foreground">
          <AlertTriangle className="w-5 h-5 text-amber-500" />
          <p>Nenhuma fonte de métricas históricas disponível para este cluster.</p>
          <p className="text-xs">Requer Prometheus instalado no cluster, ou Dynatrace configurado com o workload resolvível como CLOUD_APPLICATION.</p>
        </div>
      ) : !data ? null : chartData.length === 0 ? (
        <div className="py-6 space-y-3">
          <p className="text-sm text-muted-foreground text-center">Sem série de métricas na janela selecionada.</p>
          {/* Sem pontos não há timeline pra ancorar o ReferenceArea (dtProblemMarkers exige
              decimatedPoints não-vazio) — lista simples com datas reais no lugar do overlay. */}
          {data.dynatrace_problems && data.dynatrace_problems.length > 0 && (
            <div className="space-y-1.5 max-w-md mx-auto">
              <p className="text-[10px] text-muted-foreground uppercase tracking-wide text-center">
                {data.dynatrace_problems.length} problema{data.dynatrace_problems.length !== 1 ? "s" : ""} Dynatrace na janela
              </p>
              {data.dynatrace_problems.map((p) => {
                const url = dtProblemUrl(data.dynatrace_ui_base_url, p.problem_id);
                return (
                  <div
                    key={p.problem_id}
                    className="flex items-center gap-2 text-xs px-2 py-1.5 rounded border"
                    style={{ borderColor: severityColor(p.severity) }}
                  >
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: severityColor(p.severity) }} />
                    <span className="flex-1 truncate">{p.title}</span>
                    <span className="text-muted-foreground shrink-0">
                      {new Date(p.start_ts).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}
                      {!p.end_ts && " · em aberto"}
                    </span>
                    {url && (
                      <a href={url} target="_blank" rel="noopener noreferrer" title="Abrir no Dynatrace" className="shrink-0 text-muted-foreground hover:text-foreground">
                        <ExternalLink className="w-3 h-3" />
                      </a>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
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
              {/* top:20 (não 8) — o label "de→para" dos eventos de escala (abaixo) precisa de
                  espaço acima da linha do gráfico; com margem de 8px o texto ficava cortado, fora
                  da área visível do ComposedChart. */}
              <ComposedChart data={chartData} margin={{ top: 20, right: 8, left: -18, bottom: 0 }}>
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
                {/* label explícito — antes a linha tracejada não tinha nenhuma indicação do que
                    representava (fácil de confundir com o overlay de problems Dynatrace, cor
                    parecida); agora mostra "de→para" réplicas diretamente no gráfico. */}
                {scaleEventMarkers.map((m, i) => (
                  <ReferenceLine
                    key={i}
                    x={m.time}
                    stroke="#f59e0b"
                    strokeDasharray="3 3"
                    strokeWidth={1}
                    label={{ value: `${m.event.from_replicas}→${m.event.to_replicas}`, position: "top", fontSize: 9, fill: "#f59e0b" }}
                  />
                ))}
                <Line type="stepAfter" dataKey="replicas_desired" stroke={repChartConfig.replicas_desired.color} strokeWidth={1.5} dot={false} />
                {hasCurrentReplicas && (
                  <Line type="monotone" dataKey="replicas_current" stroke={repChartConfig.replicas_current.color} strokeWidth={1.5} dot={false} />
                )}
                {hasReadyReplicas && (
                  <Line type="monotone" dataKey="replicas_ready" stroke={repChartConfig.replicas_ready.color} strokeWidth={1.5} dot={false} />
                )}
                {compareOffsets.map((offset) => (
                  <Line key={offset} type="monotone" dataKey={`replicasD${offset}`} stroke={COMPARE_COLORS[offset]}
                    strokeWidth={1.5} strokeDasharray="5 3" dot={false} connectNulls />
                ))}
              </ComposedChart>
            </ChartContainer>
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground justify-end flex-wrap mt-0.5">
              <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_desired.color }} />Desejadas</span>
              {hasCurrentReplicas && (
                <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_current.color }} />Atuais</span>
              )}
              {hasReadyReplicas && (
                <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: repChartConfig.replicas_ready.color }} />Prontas</span>
              )}
              {scaleEventMarkers.length > 0 && (
                <span className="flex items-center gap-1">
                  <span className="inline-block w-3 h-0.5 border-t border-dashed" style={{ borderColor: "#f59e0b" }} />
                  Mudança de escala
                </span>
              )}
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
                {data.dynatrace_problems.map((p) => {
                  const url = dtProblemUrl(data.dynatrace_ui_base_url, p.problem_id);
                  const title = `${p.title} (${p.severity}${p.end_ts ? "" : " · em aberto"})${url ? " — clique pra abrir no Dynatrace" : ""}`;
                  const className = "inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border";
                  const style = { borderColor: severityColor(p.severity), color: severityColor(p.severity) };
                  const inner = (
                    <>
                      <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: severityColor(p.severity) }} />
                      <span className="truncate max-w-[160px]">{p.title}</span>
                      {url && <ExternalLink className="w-2.5 h-2.5 shrink-0" />}
                    </>
                  );
                  return url ? (
                    <a key={p.problem_id} href={url} target="_blank" rel="noopener noreferrer" title={title} className={`${className} hover:bg-muted/50`} style={style}>
                      {inner}
                    </a>
                  ) : (
                    <span key={p.problem_id} title={title} className={`${className} cursor-help`} style={style}>
                      {inner}
                    </span>
                  );
                })}
              </div>
            )}
          </div>

          {/* Painel 2 — CPU / Memória.
              Prioriza % (hasPctUsage) sobre valor absoluto — NÃO depende mais de data.source: o
              backend agora calcula cpu_usage_pct/memory_usage_pct também no caminho Dynatrace, via
              request/limit buscado direto na API do K8s (getDeploymentResourceLimitsFromK8s,
              independente do Prometheus). Só cai pro valor absoluto quando o Deployment realmente
              não tem request configurado (não dá pra calcular % de jeito nenhum, nem com Prometheus
              nem com K8s). */}
          <div>
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">
              CPU / Memória {hasPctUsage ? "(% do request)" : ""}
            </p>
            {!hasPctUsage && hasAbsoluteUsage ? (
              <>
                <p className="text-[10px] text-muted-foreground mb-1">
                  Uso absoluto (Dynatrace) — Deployment sem request de CPU/memória configurado, não dá pra calcular %.
                </p>
                {/* Empilhados (não lado a lado) — cada um com a largura cheia pro eixo de tempo não
                    ficar espremido; domain=['auto','auto'] em vez do default [0, auto] — sem isso,
                    memória (ex: variando 58000-59700MB) fica achatada perto do topo do gráfico
                    contra uma escala que começa em 0. */}
                <div className="space-y-2">
                  <ChartContainer config={usageAbsoluteChartConfig} className="h-[110px] w-full">
                    <ComposedChart data={chartData} margin={{ top: 4, right: 4, left: -12, bottom: 0 }}>
                      <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                      <YAxis domain={["auto", "auto"]} tick={{ fontSize: 9 }} tickLine={false} axisLine={false} tickFormatter={(v) => formatMillicores(Number(v))} width={40} />
                      <ChartTooltip content={<ChartTooltipContent labelFormatter={(l) => `Horário: ${l}`} formatter={(v) => formatMillicores(Number(v))} />} />
                      <Line type="monotone" dataKey="cpu_absolute" stroke={usageAbsoluteChartConfig.cpu_absolute.color} strokeWidth={1.5} dot={false} name={usageAbsoluteChartConfig.cpu_absolute.label} />
                    </ComposedChart>
                  </ChartContainer>
                  <ChartContainer config={usageAbsoluteChartConfig} className="h-[110px] w-full">
                    <ComposedChart data={chartData} margin={{ top: 4, right: 4, left: -12, bottom: 0 }}>
                      <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                      <YAxis domain={["auto", "auto"]} tick={{ fontSize: 9 }} tickLine={false} axisLine={false} tickFormatter={(v) => formatBytes(Number(v) * 1024 * 1024)} width={48} />
                      <ChartTooltip content={<ChartTooltipContent labelFormatter={(l) => `Horário: ${l}`} formatter={(v) => formatBytes(Number(v) * 1024 * 1024)} />} />
                      <Line type="monotone" dataKey="memory_absolute" stroke={usageAbsoluteChartConfig.memory_absolute.color} strokeWidth={1.5} dot={false} name={usageAbsoluteChartConfig.memory_absolute.label} />
                    </ComposedChart>
                  </ChartContainer>
                </div>
                <p className="text-[9px] text-muted-foreground mt-0.5 flex gap-3">
                  <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: usageAbsoluteChartConfig.cpu_absolute.color }} />CPU</span>
                  <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5" style={{ backgroundColor: usageAbsoluteChartConfig.memory_absolute.color }} />Memória</span>
                </p>
              </>
            ) : !hasPctUsage && !hasAbsoluteUsage ? (
              <p className="text-xs text-muted-foreground py-3">
                Uso de CPU/memória indisponível — sem métrica de uso (Prometheus/Dynatrace) nem
                request configurado no Deployment pra normalizar em %.
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

          {/* Painel — Rede (IN/OUT) — mesmo chart pros dois sentidos, pedido explícito do usuário
              em vez de dois painéis separados (facilita comparar entrada vs. saída no mesmo
              instante). Só caminho Prometheus — container_network_*_bytes_total não tem
              equivalente no fallback Dynatrace desta integração (mesma limitação de CPU/Memória
              acima). */}
          <div>
            <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Rede (IN/OUT)</p>
            {data.source === "dynatrace" ? (
              <p className="text-xs text-muted-foreground py-3">
                Tráfego de rede indisponível no fallback Dynatrace — sem métrica equivalente nesse caminho.
              </p>
            ) : (
              <ChartContainer config={networkChartConfig} className="h-[140px] w-full">
                <ComposedChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                  <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
                  <YAxis tick={{ fontSize: 9 }} tickLine={false} axisLine={false} tickFormatter={(v) => formatBytesPerSec(Number(v))} width={56} />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        labelFormatter={(l) => `Horário: ${l}`}
                        formatter={(value, _name, item) => {
                          const key = String(item.dataKey);
                          const color = networkChartConfig[key as keyof typeof networkChartConfig]?.color ?? "#94a3b8";
                          const label = networkChartConfig[key as keyof typeof networkChartConfig]?.label ?? key;
                          return (
                            <>
                              <span className="h-2.5 w-2.5 rounded-[2px] shrink-0" style={{ backgroundColor: color }} />
                              <div className="flex flex-1 justify-between items-center leading-none gap-3">
                                <span className="text-muted-foreground">{label}</span>
                                <span className="font-mono font-medium tabular-nums text-foreground">{formatBytesPerSec(Number(value))}</span>
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
                  <Line type="monotone" dataKey="network_in" stroke={networkChartConfig.network_in.color} strokeWidth={1.5} dot={false} />
                  <Line type="monotone" dataKey="network_out" stroke={networkChartConfig.network_out.color} strokeWidth={1.5} dot={false} />
                </ComposedChart>
              </ChartContainer>
            )}
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
