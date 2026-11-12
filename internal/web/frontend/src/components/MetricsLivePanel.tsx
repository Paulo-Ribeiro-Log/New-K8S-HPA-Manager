import { memo, useCallback, useEffect, useRef, useMemo, useState, type ReactNode } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { BarChart3, ChevronDown, FileDown, ChevronRight, ExternalLink, Loader2, X, Play, RefreshCw, Square, TrendingDown, TrendingUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api/client";
import { PERCENTILE_CHOICES, REPORT_TZ, fmtInt, fmtLatency, fmtRps, togglePercentile, zonedLocalToISO, isoToZonedLocal, type ReportItem } from "@/lib/metricsReport";
import {
  DEFAULT_LIVE_RANGE, LATENCY_RED_MS, LEVEL_LABEL, LIVE_MAX_POINTS, LIVE_RANGES, LIVE_RESOURCES_KEY, LIVE_RANGE_KEY, LIVE_REFRESH_CHOICES, LIVE_REFRESH_KEY, PROJECTION_CHOICES, PROJECTION_KEY,
  buildChartData, errorLevel, fmtCores, fmtMemMB, fmtRefresh, fmtSpanLabel, fmtUsagePcts, parseLiveResources, replicasLevel, restartsLevel, hpaLevel, throttleLevel, podsLevel, fmtRestartReasons, netLevel, fmtBytesPerSec, usageLevel, groupLive, lastValue, latencyLevel, liveRangeMs, liveWindow, minutesToThreshold, parseLiveRange, parseLiveRefresh, parseProjection, projectWithBaseline, DAY_MS, SEASONAL_LOOKBACK_MIN,
  normalizeLive, buildNetworkRows, projectedPercentile, sparkHeights, seriesLevel, summarizeStatus, worstLevel, type Level, type LiveChartData, type LiveRangeId, type LiveSeries, type Projection,
} from "@/lib/metricsLive";

// Tempo real do Relatório de Métricas: as mesmas métricas do relatório (Dynatrace), num cartão por
// aplicação no estilo do monitor de stress test (valor atual, barras de RPS do período, latências,
// erros e tendência). Atualiza sozinho a cada 1 a 5 minutos até o operador parar. Somente leitura.
//
// Memória: a resposta já vem reduzida pelo servidor (no máximo LIVE_MAX_POINTS pontos por série), o
// cartão desenha barras SVG leves (os gráficos completos só são montados quando o operador os abre), a
// consulta não fica em cache depois de substituída e a contagem regressiva é um componente à parte
// (não refaz a tela inteira a cada segundo).

const COLORS = { rps: "#38bdf8", p50: "#34d399", p95: "#fbbf24", p99: "#f87171", e4: "#fbbf24", e5: "#f87171", proj: "#a78bfa", avg: "#2dd4bf", grid: "#374151", axis: "#9ca3af", spark: "#d946ef" };
const LEVEL_CLASS: Record<Level, string> = { ok: "text-emerald-400", warn: "text-amber-400", red: "text-red-400", info: "text-sky-400" };

function loadPref<T>(key: string, parse: (raw: string | null) => T): T {
  try {
    return parse(localStorage.getItem(key));
  } catch {
    return parse(null);
  }
}
function savePref(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* sem armazenamento: segue sem lembrar */
  }
}

import { MetricsEventPicker } from "@/components/MetricsEventPicker";
import type { SavedEvent } from "@/lib/metricsEvents";
import { buildSessionCSV, buildSessionMarkdown, newSession, recordSample, type Session } from "@/lib/metricsLiveSession";
import { downloadText, fileStamp } from "@/lib/metricsReportExport";

// Memória JS desta aba (só Chromium: performance.memory). O número inclui lixo ainda não coletado, então
// oscila em serra; o que importa é se o pico sobe com o tempo.
function BrowserMemory() {
  const [m, setM] = useState<{ used: number; peak: number } | null>(null);
  useEffect(() => {
    const perf = performance as unknown as { memory?: { usedJSHeapSize: number } };
    if (!perf.memory) return;
    let peak = 0;
    const tick = () => {
      const used = (perf.memory?.usedJSHeapSize ?? 0) / 1048576;
      peak = Math.max(peak, used);
      setM({ used, peak });
    };
    tick();
    const id = setInterval(tick, 10_000);
    return () => clearInterval(id);
  }, []);
  if (!m) return null;
  return (
    <span className="text-[11px] text-muted-foreground cursor-help" title="Heap JavaScript desta aba (Chromium), lido a cada 10 s. Inclui lixo ainda não coletado: oscila em serra. Se o pico só sobe com o tempo, há vazamento.">
      memória JS da aba: <b>{m.used.toFixed(0)} MB</b> (pico {m.peak.toFixed(0)} MB)
    </span>
  );
}

const BADGE_CLASS: Record<Level, string> = {
  red: "border-red-500/50 bg-red-500/10 text-red-400",
  warn: "border-amber-500/50 bg-amber-500/10 text-amber-400",
  ok: "border-emerald-500/50 bg-emerald-500/10 text-emerald-400",
  info: "border-sky-500/50 bg-sky-500/10 text-sky-400",
};
const COLLAPSED_KEY = "metrics_live_collapsed";
function parseCollapsed(raw: string | null): Set<string> {
  try {
    const v = raw ? JSON.parse(raw) : [];
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

const errText = (e: unknown) => (e instanceof Error ? e.message : "erro desconhecido");

const fmtClock = (ms: number, withDate: boolean) =>
  new Date(ms).toLocaleString("pt-BR", { timeZone: REPORT_TZ, ...(withDate ? { day: "2-digit", month: "2-digit" } : {}), hour: "2-digit", minute: "2-digit" });

type CustomRange = { start: string; end: string };

// Contagem regressiva isolada: o relógio de 1 s só refaz este pedaço da tela.
function Countdown({ updatedAt, refreshSec, fetching }: { updatedAt: number; refreshSec: number; fetching: boolean }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (refreshSec <= 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [refreshSec]);
  const next = refreshSec > 0 && updatedAt ? Math.max(0, Math.round((updatedAt + refreshSec * 1000 - now) / 1000)) : null;
  return (
    <span className="text-muted-foreground">
      {updatedAt > 0 && `atualizado às ${new Date(updatedAt).toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ })}`}
      {next != null && !fetching && ` · próxima em ${next}s`}
    </span>
  );
}

export function MetricsLivePanel({
  items, percentiles, onPercentiles, onRemove, events, onSaveEvent, onRemoveEvent,
}: {
  events: SavedEvent[];
  onSaveEvent: (name: string, start: string, end: string) => boolean;
  onRemoveEvent: (name: string) => void;
  onRemove: (item: ReportItem) => void;
  items: ReportItem[];
  percentiles: number[];
  onPercentiles: (next: number[]) => void;
}) {
  const [range, setRange] = useState<LiveRangeId | "custom">(() => loadPref(LIVE_RANGE_KEY, parseLiveRange));
  const [refresh, setRefresh] = useState<number>(() => loadPref(LIVE_REFRESH_KEY, parseLiveRefresh));
  const [horizon, setHorizon] = useState<number>(() => loadPref(PROJECTION_KEY, parseProjection));
  const [startLocal, setStartLocal] = useState(() => isoToZonedLocal(Date.now() - 3600e3));
  const [endLocal, setEndLocal] = useState(() => isoToZonedLocal(Date.now()));
  // rodando = consulta e atualiza sozinho; parado = nada é consultado (os cartões ficam como estão)
  const [running, setRunning] = useState(true);
  // réplicas, restarts, CPU e memória (Prometheus): custam consultas a mais, então dá para desligar
  const [resources, setResources] = useState<boolean>(() => loadPref(LIVE_RESOURCES_KEY, parseLiveResources));
  useEffect(() => savePref(LIVE_RESOURCES_KEY, resources), [resources]);
  useEffect(() => { if (range !== "custom") savePref(LIVE_RANGE_KEY, range); }, [range]);
  useEffect(() => savePref(LIVE_REFRESH_KEY, refresh), [refresh]);
  useEffect(() => savePref(PROJECTION_KEY, horizon), [horizon]);

  const custom: CustomRange | null = useMemo(() => {
    if (range !== "custom") return null;
    const start = zonedLocalToISO(startLocal), end = zonedLocalToISO(endLocal);
    return start && end ? { start, end } : null;
  }, [range, startLocal, endLocal]);
  const relative = range !== "custom";
  const customInvalid = range === "custom" && !custom;

  // a faixa personalizada é um período já passado: sem atualização automática e sem projeção
  const effHorizon = relative ? horizon : 0;
  const itemsKey = useMemo(() => items.map((i) => `${i.cluster}/${i.namespace}/${i.workload}`).join(","), [items]);
  const pctKey = percentiles.join(",");

  const q = useQuery({
    queryKey: ["metrics-live", itemsKey, range, custom?.start, custom?.end, pctKey, resources],
    queryFn: () => {
      const w = range === "custom" ? custom! : liveWindow(range);
      return apiClient.metricsLive({ items, start: w.start, end: w.end, percentiles, max_points: LIVE_MAX_POINTS, resources }).then(normalizeLive);
    },
    enabled: running && items.length > 0 && !customInvalid,
    retry: false,
    placeholderData: keepPreviousData, // ao trocar a faixa, mantém os cartões até chegar o novo dado
    gcTime: 0, // resultado substituído não fica em cache (cada atualização traz um novo)
    structuralSharing: false, // sempre há dado novo: comparar o antigo ponto a ponto só gasta CPU e memória
    refetchInterval: running && relative && refresh > 0 ? refresh * 1000 : false, // pausa sozinho com o navegador em segundo plano
  });

  const data = q.data;

  const status = useMemo(() => (data ? summarizeStatus(data.series, data.percentiles ?? percentiles) : null), [data, percentiles]);

  // resumo da sessão: só agregados por aplicação, a cada atualização (a memória não cresce com o tempo)
  const sessionRef = useRef<Session>(newSession(Date.now()));
  const [sessionUpdates, setSessionUpdates] = useState(0);
  useEffect(() => {
    if (!q.data) return;
    recordSample(sessionRef.current, q.data.series, projectedPercentile(q.data.percentiles ?? percentiles));
    setSessionUpdates(sessionRef.current.updates);
    // só quando chega dado novo
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.dataUpdatedAt]);
  const exportSession = (kind: "md" | "csv") => {
    const now = Date.now();
    const stamp = fileStamp(new Date(now).toISOString());
    if (kind === "md") downloadText(buildSessionMarkdown(sessionRef.current, now), "text/markdown", `tempo-real-resumo_${stamp}.md`);
    else downloadText(buildSessionCSV(sessionRef.current), "text/csv", `tempo-real-resumo_${stamp}.csv`);
  };

  // minimizados (cluster inteiro ou cartão), lembrados no navegador
  const [collapsed, setCollapsed] = useState<Set<string>>(() => loadPref(COLLAPSED_KEY, parseCollapsed));
  const toggleCollapsed = useCallback((key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      savePref(COLLAPSED_KEY, [...next]);
      return next;
    });
  }, []);

  // base da projeção sazonal: ontem no mesmo horário (≈1,5 h de dados), buscada a cada 5 min e não a cada atualização
  const baseOn = running && relative && effHorizon > 0 && items.length > 0;
  const bq = useQuery({
    queryKey: ["metrics-live-base", itemsKey, projectedPercentile(percentiles), effHorizon],
    queryFn: () => {
      const now = Date.now();
      const start = new Date(now - DAY_MS - SEASONAL_LOOKBACK_MIN * 60_000 - 5 * 60_000).toISOString();
      const end = new Date(now - DAY_MS + (effHorizon + 5) * 60_000).toISOString();
      return apiClient.metricsLive({ items, start, end, percentiles: [projectedPercentile(percentiles)], max_points: LIVE_MAX_POINTS, resources: false }).then(normalizeLive);
    },
    enabled: baseOn,
    retry: false,
    gcTime: 0,
    structuralSharing: false,
    placeholderData: keepPreviousData,
    refetchInterval: baseOn ? 5 * 60_000 : false,
  });
  const baseMap = useMemo(() => {
    const m = new Map<string, LiveSeries>();
    for (const b of bq.data?.series ?? []) m.set(`${b.cluster}|${b.namespace}|${b.workload}`, b);
    return m;
  }, [bq.data]);
  const groups = useMemo(() => (data ? groupLive(data.series) : []), [data]);
  const spanMs = range === "custom" && custom ? Date.parse(custom.end) - Date.parse(custom.start) : liveRangeMs(range === "custom" ? DEFAULT_LIVE_RANGE : range);
  const spanLabel = fmtSpanLabel(spanMs);
  const [detail, setDetail] = useState<LiveSeries | null>(null);

  return (
    <div className="space-y-3">
      <section className="rounded-md border border-border p-3 space-y-2">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {running ? (
            <Button size="sm" variant="destructive" className="h-7 text-xs gap-1" onClick={() => setRunning(false)} title="Para as consultas ao Dynatrace; os cartões ficam como estão">
              <Square className="h-3 w-3" />Parar
            </Button>
          ) : (
            <Button size="sm" className="h-7 text-xs gap-1" disabled={items.length === 0 || customInvalid} onClick={() => setRunning(true)}>
              <Play className="h-3 w-3" />Iniciar
            </Button>
          )}
          <span className={running ? "text-emerald-400" : "text-muted-foreground"}>{running ? (relative && refresh > 0 ? "● ao vivo" : "● consultado") : "■ parado"}</span>
          <label className="flex items-center gap-1.5">
            Período
            <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={range} onChange={(e) => setRange(e.target.value as LiveRangeId | "custom")}>
              {LIVE_RANGES.map((r) => <option key={r.id} value={r.id}>{r.label}</option>)}
              <option value="custom">Personalizado…</option>
            </select>
          </label>
          <label className="flex items-center gap-1.5" title="De quanto em quanto tempo os cartões são atualizados (o Dynatrace entrega dado de 1 em 1 minuto)">
            Atualizar a cada
            <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={refresh} disabled={!relative} onChange={(e) => setRefresh(Number(e.target.value))}>
              {LIVE_REFRESH_CHOICES.map((s) => <option key={s} value={s}>{fmtRefresh(s)}</option>)}
              <option value={0}>{fmtRefresh(0)}</option>
            </select>
          </label>
          <label className="flex items-center gap-1.5" title="Estende uma reta ajustada aos dados do período (tendência linear, não previsão): não conhece sazonalidade nem limite de capacidade">
            Projeção
            <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={horizon} disabled={!relative} onChange={(e) => setHorizon(Number(e.target.value))}>
              {PROJECTION_CHOICES.map((m) => <option key={m} value={m}>{m === 0 ? "Desligada" : m === 60 ? "+1 h" : `+${m} min`}</option>)}
            </select>
          </label>
          <MetricsEventPicker
            events={events}
            current={(() => {
              const s = zonedLocalToISO(startLocal), e = zonedLocalToISO(endLocal);
              return s && e ? { start: s, end: e } : null;
            })()}
            onApply={(s, e) => {
              setRange("custom");
              setStartLocal(isoToZonedLocal(Date.parse(s)));
              setEndLocal(isoToZonedLocal(Date.parse(e)));
            }}
            onSave={onSaveEvent}
            onRemove={onRemoveEvent}
          />
          <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={items.length === 0 || customInvalid || q.isFetching} onClick={() => q.refetch()}>
            {q.isFetching ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}Atualizar agora
          </Button>
          <Countdown updatedAt={q.dataUpdatedAt} refreshSec={running && relative ? refresh : 0} fetching={q.isFetching} />
        </div>
        {range === "custom" && (
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1.5">Início<Input type="datetime-local" className="h-7 w-48 text-xs" value={startLocal} onChange={(e) => setStartLocal(e.target.value)} /></label>
            <label className="flex items-center gap-1.5">Fim<Input type="datetime-local" className="h-7 w-48 text-xs" value={endLocal} onChange={(e) => setEndLocal(e.target.value)} /></label>
            <span className="text-muted-foreground">fuso {REPORT_TZ} · período passado: sem atualização automática nem projeção</span>
            {customInvalid && <span className="text-red-400">Data ou hora inválida.</span>}
          </div>
        )}
        {status && (
          <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
            {(["red", "warn", "ok", "info"] as const).map((lv) => status[lv].length > 0 && (
              <span
                key={lv}
                className={`cursor-help rounded border px-1.5 py-0.5 font-medium ${BADGE_CLASS[lv]}`}
                title={status[lv].map((e) => `${e.name}${e.reasons.length ? ` — ${e.reasons.join("; ")}` : ""}`).join("\n")}
              >
                {LEVEL_LABEL[lv]} {status[lv].length}
              </span>
            ))}
            <span className="text-muted-foreground">passe o mouse para ver quais</span>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <span className="text-muted-foreground" title="Agregados (mín/méd/máx) por aplicação, amostrados a cada atualização; a memória não cresce com o tempo">
            sessão: {sessionUpdates} atualização(ões) desde {new Date(sessionRef.current.startedAt).toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ, hour: "2-digit", minute: "2-digit" })}
          </span>
          <Button size="sm" variant="outline" className="h-6 text-[11px] gap-1" disabled={sessionUpdates === 0} onClick={() => exportSession("md")} title="Baixa o resumo da sessão em Markdown"><FileDown className="h-3 w-3" />Resumo .md</Button>
          <Button size="sm" variant="outline" className="h-6 text-[11px] gap-1" disabled={sessionUpdates === 0} onClick={() => exportSession("csv")} title="Baixa o resumo da sessão em CSV"><FileDown className="h-3 w-3" />CSV</Button>
          <Button size="sm" variant="ghost" className="h-6 text-[11px]" disabled={sessionUpdates === 0} onClick={() => { sessionRef.current = newSession(Date.now()); setSessionUpdates(0); }} title="Zera o resumo e começa uma sessão nova">Zerar</Button>
          <BrowserMemory />
        </div>
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <span className="text-muted-foreground">Latência:</span>
          {PERCENTILE_CHOICES.map((p) => (
            <label key={p} className="flex items-center gap-1.5 cursor-pointer">
              <Checkbox checked={percentiles.includes(p)} onCheckedChange={(v) => onPercentiles(togglePercentile(percentiles, p, v === true))} />
              P{p}
            </label>
          ))}
          <span className="text-[10px] text-muted-foreground">compartilhado com o relatório · a projeção de latência usa o maior percentil marcado</span>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <label className="flex items-center gap-1.5 cursor-pointer" title="Réplicas, restarts, CPU e memória vêm do Prometheus do cluster. Tráfego, erros e P50/P95/P99 vêm sempre do Dynatrace.">
            <Checkbox checked={resources} onCheckedChange={(v) => setResources(v === true)} />
            Réplicas, restarts, CPU e memória
          </label>
          <span className="text-[10px] text-muted-foreground">Prometheus · 9 consultas por cluster a cada atualização · latência e tráfego continuam do Dynatrace</span>
        </div>
        {data && (
          <div className="text-xs text-muted-foreground">
            {items.length} aplicação(ões) · cada barra = {fmtStep(data.step_seconds)} (Dynatrace {data.resolution}) · {new Date(data.window.start).toLocaleString("pt-BR", { timeZone: REPORT_TZ })} → {new Date(data.window.end).toLocaleString("pt-BR", { timeZone: REPORT_TZ })}
            {data.warnings?.map((w) => <span key={w} className="ml-2 text-amber-400">{w}</span>)}
          </div>
        )}
        {!running && items.length > 0 && <div className="text-[11px] text-muted-foreground">Parado: mudanças de período, latência e seleção só valem ao iniciar ou ao clicar em Atualizar agora.</div>}
      </section>

      {items.length === 0 && <div className="text-xs text-muted-foreground">Marque ao menos uma aplicação no painel da esquerda.</div>}
      {q.isError && <div className="text-xs text-red-400">{errText(q.error)}{data ? " (mostrando a última atualização que deu certo)" : ""}</div>}
      {q.isLoading && items.length > 0 && !customInvalid && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />Consultando o Dynatrace…</div>
      )}

      {groups.map((g) => (
        <section key={g.cluster} className="space-y-2">
          <button className="flex items-center gap-1.5 text-sm font-medium text-amber-500 hover:text-amber-400" title={collapsed.has(g.cluster) ? "Expandir o cluster" : "Minimizar o cluster"} onClick={() => toggleCollapsed(g.cluster)}>
            {collapsed.has(g.cluster) ? <ChevronRight className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            {g.cluster}
            <span className="text-[11px] font-normal text-muted-foreground">({g.series.length} aplicação(ões))</span>
            {collapsed.has(g.cluster) && (() => {
              const lv = worstLevel(g.series.map((x) => seriesLevel(x, data?.percentiles ?? percentiles)));
              return <span className={`text-[11px] font-medium ${LEVEL_CLASS[lv]}`}>{LEVEL_LABEL[lv]}</span>;
            })()}
          </button>
          {!collapsed.has(g.cluster) && <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {g.series.map((s) => (
              <LiveCard key={`${s.cluster}|${s.namespace}|${s.workload}`} s={s} base={baseMap.get(`${s.cluster}|${s.namespace}|${s.workload}`)} percentiles={data?.percentiles ?? percentiles} horizon={effHorizon} spanLabel={spanLabel} onDetail={setDetail} onRemove={onRemove} />
            ))}
          </div>}
        </section>
      ))}

      {/* gráficos completos: só existem enquanto o modal está aberto */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          {detail && <DetailCharts s={detail} base={baseMap.get(`${detail.cluster}|${detail.namespace}|${detail.workload}`)} percentiles={data?.percentiles ?? percentiles} horizon={effHorizon} spanMs={spanMs} win={data?.window} />}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function fmtStep(sec: number): string {
  return sec < 60 ? `${sec}s` : sec % 3600 === 0 ? `${sec / 3600} h` : sec % 60 === 0 ? `${sec / 60} min` : `${(sec / 60).toFixed(1)} min`;
}

// Uma linha do cartão: rótulo, valor e situação (como o monitor de stress test).
function Row({ label, level, source = "Dynatrace", children }: { label: string; level?: Level; source?: "Dynatrace" | "Prometheus"; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[88px_1fr_auto] items-baseline gap-2 leading-5">
      <span className="text-cyan-400" title={`Fonte: ${source}`}>{label}</span>
      <span className="min-w-0">{children}</span>
      {level && <span className={`text-right font-medium ${LEVEL_CLASS[level]}`}>{LEVEL_LABEL[level]}</span>}
    </div>
  );
}

// Barras do RPS no período (SVG leve; lacuna = sem barra). Cada barra leva a hora e o valor na dica.
function Spark({ times, values }: { times: number[]; values: (number | null)[] }) {
  const { heights, max } = useMemo(() => sparkHeights(values), [values]);
  const n = heights.length;
  if (n === 0) return <span className="text-muted-foreground">sem dados</span>;
  const H = 16;
  return (
    <span className="inline-flex items-center gap-2">
      <svg viewBox={`0 0 ${n * 3} ${H}`} preserveAspectRatio="none" className="h-4 w-48 max-w-full" role="img" aria-label="RPS no período">
        {heights.map((h, i) =>
          h == null ? null : (
            <rect key={i} x={i * 3} y={H - Math.max(1, h * H)} width={2} height={Math.max(1, h * H)} fill={COLORS.spark}>
              <title>{`${fmtClock(times[i], true)}: ${fmtRps(values[i])} req/s`}</title>
            </rect>
          ),
        )}
      </svg>
      <span className="text-fuchsia-400">(Máx: {fmtRps(max)})</span>
    </span>
  );
}

const LiveCard = memo(function LiveCard({
  s, base, percentiles, horizon, spanLabel, onDetail, onRemove,
}: {
  onRemove: (item: ReportItem) => void;
  s: LiveSeries;
  base?: LiveSeries;
  percentiles: number[];
  horizon: number;
  spanLabel: string;
  onDetail: (s: LiveSeries) => void;
}) {
  const rpsNow = lastValue(s.rps);
  const e5 = lastValue(s.err_5xx_rps);
  const e4 = lastValue(s.err_4xx_rps);
  const lat = [50, 95, 99].filter((p) => percentiles.includes(p)).map((p) => ({ p, v: lastValue(p === 99 ? s.p99_ms : p === 95 ? s.p95_ms : s.p50_ms) }));
  const hasLatency = lat.some((l) => l.v != null);
  const avgNow = lastValue(s.avg_ms);
  const hasAvg = avgNow != null;
  const latP = projectedPercentile(percentiles);
  const rpsProj = useMemo(() => (horizon > 0 ? projectWithBaseline(s.times, s.rps, base ? { times: base.times, values: base.rps } : null, horizon) : null), [s, base, horizon]);
  const latProj = useMemo(() => (horizon > 0 ? projectWithBaseline(s.times, (latP === 99 ? s.p99_ms : latP === 95 ? s.p95_ms : s.p50_ms) ?? [], base ? { times: base.times, values: (latP === 99 ? base.p99_ms : latP === 95 ? base.p95_ms : base.p50_ms) ?? [] } : null, horizon) : null), [s, base, horizon, latP]);
  const toLimit = latP === 99 ? minutesToThreshold(latProj, LATENCY_RED_MS, horizon) : null;

  const r = s.resources;
  if (s.error && !r) {
    return (
      <div className="rounded-md border border-red-500/40 p-3 text-xs">
        <div className="text-sm font-medium">{s.workload} <span className="text-[10px] font-normal text-muted-foreground">ns: {s.namespace}</span></div>
        <div className="mt-1 text-red-400 break-words">{s.error}</div>
      </div>
    );
  }
  const removeBtn = (
    <button className="text-muted-foreground hover:text-red-400" title="Remover do acompanhamento (também desmarca da seleção do painel)" onClick={() => onRemove({ cluster: s.cluster, namespace: s.namespace, workload: s.workload })}>
      <X className="h-3.5 w-3.5" />
    </button>
  );
  return (
    <div className="rounded-md border border-border p-3 text-xs font-mono">
      <div className="mb-1 flex items-baseline gap-2">
        <span className="text-sm font-semibold text-sky-400">{s.workload}</span>
        <span className="text-[10px] text-muted-foreground truncate" title={s.namespace}>ns: {s.namespace}</span>
        <button className="ml-auto text-muted-foreground hover:text-foreground" title="Abrir os gráficos completos" onClick={() => onDetail(s)}>
          <BarChart3 className="h-3.5 w-3.5" />
        </button>
        {removeBtn}
      </div>
      {r && (
        <>
          <Row label="Réplicas" level={replicasLevel(r.replicas_available, r.replicas_desired)} source="Prometheus">
            {r.replicas_available != null && r.replicas_desired != null ? <><b>{r.replicas_available}</b> / {r.replicas_desired} <span className="text-muted-foreground">(disp / des)</span></> : "sem dado"}
          </Row>
          <Row label="Restarts 15m" level={restartsLevel(r.restarts_15m)} source="Prometheus">
            {r.restarts_15m == null ? "sem dado" : <b>{r.restarts_15m}</b>}
            {fmtRestartReasons(r.restart_reasons) && <span className={`ml-1 ${fmtRestartReasons(r.restart_reasons).includes("OOMKilled") ? "text-red-400" : "text-muted-foreground"}`}>({fmtRestartReasons(r.restart_reasons)})</span>}
          </Row>
          {((r.pods_pending ?? 0) > 0 || (r.pods_not_ready ?? 0) > 0) && (
            <Row label="Pods" level={podsLevel(r.pods_pending, r.pods_not_ready)} source="Prometheus">
              <b>{r.pods_pending ?? 0}</b> pendente(s) · <b>{r.pods_not_ready ?? 0}</b> não pronto(s)
            </Row>
          )}
          {r.hpa_max != null && (
            <Row label="HPA" level={hpaLevel(r.replicas_desired, r.hpa_max)} source="Prometheus">
              <b>{r.hpa_min ?? "?"}–{r.hpa_max}</b> <span className="text-muted-foreground">réplicas (mín–máx){hpaLevel(r.replicas_desired, r.hpa_max) === "warn" ? " · no teto: não escala mais" : ""}</span>
            </Row>
          )}
          <Row label="CPU" level={usageLevel(r.cpu_lim_pct)} source="Prometheus">
            <b>{fmtCores(r.cpu_cores)}</b> <span className="text-muted-foreground">{fmtUsagePcts(r.cpu_lim_pct, r.cpu_req_pct)}</span>
          </Row>
          {r.cpu_throttle_pct != null && (
            <Row label="Throttling" level={throttleLevel(r.cpu_throttle_pct)} source="Prometheus">
              <b>{r.cpu_throttle_pct.toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%</b> <span className="text-muted-foreground">dos períodos de CPU limitados</span>
            </Row>
          )}
          <Row label="Memória" level={usageLevel(r.mem_lim_pct)} source="Prometheus">
            <b>{fmtMemMB(r.mem_mb)}</b> <span className="text-muted-foreground">{fmtUsagePcts(r.mem_lim_pct, r.mem_req_pct)}</span>
          </Row>
          {(r.net_rx_bps != null || r.net_tx_bps != null) && (
            <Row label="Rede" level={netLevel(r.net_err_ps)} source="Prometheus">
              <b>↓ {fmtBytesPerSec(r.net_rx_bps)}</b> <b>↑ {fmtBytesPerSec(r.net_tx_bps)}</b>
              {r.net_err_ps != null && r.net_err_ps > 0 && <span className="text-muted-foreground"> · {r.net_err_ps.toLocaleString("pt-BR", { maximumFractionDigits: 2 })}/s descartes/erros</span>}
            </Row>
          )}
        </>
      )}
      {s.error ? (
        <div className="my-1 font-sans text-[11px] text-red-400 break-words">{s.error}</div>
      ) : (
        <>
      <Row label="RPS" level="info"><b>{fmtRps(rpsNow)}</b> req/s</Row>
      <Row label={`RPS ${spanLabel}`} level="info"><Spark times={s.times} values={s.rps} /></Row>
      <Row label="Erros 5xx" level={errorLevel(s.err_5xx_total, "5xx")}>
        <b>{fmtInt(s.err_5xx_total)}</b> <span className="text-muted-foreground">no período · {fmtRps(e5)}/s agora</span>
      </Row>
      <Row label="Erros 4xx" level={errorLevel(s.err_4xx_total, "4xx")}>
        <b>{fmtInt(s.err_4xx_total)}</b> <span className="text-muted-foreground">no período · {fmtRps(e4)}/s agora</span>
      </Row>
      {hasAvg && (
        <Row label="Resp. média" level="info">
          <b>{fmtLatency(avgNow)}</b> <span className="text-muted-foreground">média ponderada pelas requisições</span>
        </Row>
      )}
      {hasLatency && (
        <Row label="Latências" level={latencyLevel(lat.find((l) => l.p === latP)?.v)}>
          {lat.map((l, i) => (
            <span key={l.p}>
              {i > 0 && <span className="text-muted-foreground"> | </span>}P{l.p}: <b className={LEVEL_CLASS[latencyLevel(l.v)]}>{fmtLatency(l.v)}</b>
            </span>
          ))}
        </Row>
      )}
      {horizon > 0 && (
        <Row label="Tendência">
          <div className="flex flex-col font-sans text-[11px]">
            <Trend label="RPS" p={rpsProj} horizon={horizon} fmt={fmtRps} />
            {hasLatency && <Trend label={`P${latP}`} p={latProj} horizon={horizon} fmt={fmtLatency} />}
            {toLimit != null && <span className="text-red-400">P99 deve passar de {fmtLatency(LATENCY_RED_MS)} em ~{Math.max(1, Math.round(toLimit))} min</span>}
          </div>
        </Row>
      )}
        </>
      )}
      {s.links && s.links.length > 0 && <DynatraceLinks links={s.links} className="mt-1" />}
      {s.notes && s.notes.length > 0 && <div className="mt-1 font-sans text-[10px] text-muted-foreground">{s.notes.join(" · ")}</div>}
    </div>
  );
});

// Gráficos completos de uma aplicação (RPS, latência e erros com a projeção): montados só no modal.
function DetailCharts({ s, base, percentiles, horizon, spanMs, win }: { s: LiveSeries; base?: LiveSeries; percentiles: number[]; horizon: number; spanMs: number; win?: { start: string; end: string } }) {
  // a rede só é pedida quando o modal abre (uma consulta por abertura e por atualização), não em todo cartão
  const netQ = useQuery({
    queryKey: ["metrics-live-net", s.cluster, s.namespace, s.workload, win?.start, win?.end],
    queryFn: () => apiClient.metricsLiveNetwork({ item: { cluster: s.cluster, namespace: s.namespace, workload: s.workload }, start: win!.start, end: win!.end }),
    enabled: !!win,
    retry: false,
    gcTime: 0,
    structuralSharing: false,
    placeholderData: keepPreviousData,
  });
  const net = useMemo(() => buildNetworkRows(netQ.data), [netQ.data]);
  const d = useMemo(() => buildChartData(s, percentiles, horizon, base), [s, base, percentiles, horizon]);
  const withDate = spanMs > 20 * 3600e3;
  return (
    <div className="space-y-3">
      <DialogHeader>
        <DialogTitle>{s.workload}</DialogTitle>
        <DialogDescription>ns: {s.namespace} · {s.cluster}{s.services?.length ? ` · ${s.services.length} serviço(s) de entrada` : ""}</DialogDescription>
        {s.links && s.links.length > 0 && <DynatraceLinks links={s.links} />}
      </DialogHeader>
      <MiniChart title="Requisições por segundo" data={d} spanWithDate={withDate} height={160} yFmt={(v) => fmtRps(v)}
        lines={[{ key: "rps", color: COLORS.rps, name: "RPS" }, { key: "rpsProj", color: COLORS.proj, name: "projeção", dashed: true }]} />
      {(d.hasLatency || d.hasAvg) && (
        <MiniChart title={d.hasAvg ? "Latência (média e percentis)" : "Latência"} data={d} spanWithDate={withDate} height={160} yFmt={(v) => fmtLatency(v)} limit={percentiles.includes(99) ? LATENCY_RED_MS : undefined}
          lines={[
            ...(d.hasAvg ? [{ key: "avg", color: COLORS.avg, name: "Média" }] : []),
            ...(percentiles.includes(50) ? [{ key: "p50", color: COLORS.p50, name: "P50" }] : []),
            ...(percentiles.includes(95) ? [{ key: "p95", color: COLORS.p95, name: "P95" }] : []),
            ...(percentiles.includes(99) ? [{ key: "p99", color: COLORS.p99, name: "P99" }] : []),
            { key: "latProj", color: COLORS.proj, name: `projeção P${d.latPercentile}`, dashed: true },
          ]} />
      )}
      {d.hasErrors && (
        <MiniChart title="Erros por segundo" data={d} spanWithDate={withDate} height={120} yFmt={(v) => fmtRps(v)}
          lines={[{ key: "e4", color: COLORS.e4, name: "4xx" }, { key: "e5", color: COLORS.e5, name: "5xx" }]} />
      )}
      {netQ.isLoading && <div className="text-[11px] text-muted-foreground">Carregando a rede (Prometheus)…</div>}
      {netQ.isError && <div className="text-[11px] text-red-400">Rede (Prometheus): {errText(netQ.error)}</div>}
      {net.hasData && (
        <MiniChart title="Rede (Prometheus): ↓ recebido · ↑ enviado, somando os pods" data={net} spanWithDate={withDate} height={140} yFmt={(v) => fmtBytesPerSec(v)}
          lines={[{ key: "rx", color: COLORS.rps, name: "↓ recebido" }, { key: "tx", color: COLORS.p50, name: "↑ enviado" }]} />
      )}
      {net.hasErrors && (
        <MiniChart title="Pacotes descartados e erros de rede por segundo" data={net} spanWithDate={withDate} height={100} yFmt={(v) => v.toLocaleString("pt-BR", { maximumFractionDigits: 2 })}
          lines={[{ key: "err", color: COLORS.e5, name: "descartes + erros" }]} />
      )}
    </div>
  );
}

// Links para os serviços da aplicação no Dynatrace (abre em outra aba).
function DynatraceLinks({ links, className }: { links: { name: string; url: string }[]; className?: string }) {
  return (
    <div className={`flex flex-wrap items-center gap-x-3 gap-y-0.5 font-sans text-[11px] ${className ?? ""}`}>
      <span className="text-muted-foreground">Abrir no Dynatrace:</span>
      {links.map((l) => (
        <a key={l.url} href={l.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sky-400 hover:underline" title={`Abrir o serviço ${l.name} no Dynatrace`}>
          <ExternalLink className="h-3 w-3" />
          <span className="max-w-[18rem] truncate">{l.name}</span>
        </a>
      ))}
    </div>
  );
}

function Trend({ label, p, horizon, fmt }: { label: string; p: Projection | null; horizon: number; fmt: (v: number | null) => string }) {
  if (!p) return <span className="text-muted-foreground">{label}: poucos dados para projetar</span>;
  if (!p.reliable) return <span className="text-muted-foreground" title={`${p.points} pontos, ajuste ${(p.r2 * 100).toFixed(0)}%`}>{label}: sem tendência clara</span>;
  const up = p.slopePerHour > 0;
  const Icon = up ? TrendingUp : TrendingDown;
  return (
    <span title={p.basis === "ontem" ? `curva de ontem no mesmo horário, escalada pelo nível de hoje (${p.points} pontos comparados)` : `reta ajustada em ${p.points} pontos (ajuste ${(p.r2 * 100).toFixed(0)}%); sem dado de ontem utilizável`} className={up ? "text-amber-400" : "text-emerald-400"}>
      <Icon className="inline h-3 w-3 mr-1" />
      {label}: {fmt(p.fittedNow)} → ~{fmt(p.projected)} em {horizon === 60 ? "1 h" : `${horizon} min`}
      {p.changePct != null && ` (${p.changePct >= 0 ? "+" : ""}${p.changePct.toFixed(0)}%)`}
      <span className="ml-1 text-[10px] text-muted-foreground">{p.basis === "ontem" ? "base: ontem" : "reta"}</span>
    </span>
  );
}

function MiniChart({
  title, data, lines, yFmt, limit, height = 120, spanWithDate,
}: {
  title: string;
  data: { rows: object[] };
  lines: { key: string; color: string; name: string; dashed?: boolean }[];
  yFmt: (v: number) => string;
  limit?: number;
  height?: number;
  spanWithDate: boolean;
}) {
  return (
    <div>
      <div className="text-[10px] text-muted-foreground mb-0.5">{title}</div>
      <div style={{ height }}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={data.rows} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
            <CartesianGrid stroke={COLORS.grid} strokeDasharray="3 3" vertical={false} />
            <XAxis dataKey="t" type="number" scale="time" domain={["dataMin", "dataMax"]} tick={{ fontSize: 9, fill: COLORS.axis }} tickFormatter={(t) => fmtClock(t, spanWithDate)} minTickGap={40} />
            <YAxis width={54} tick={{ fontSize: 9, fill: COLORS.axis }} tickFormatter={(v) => yFmt(v)} domain={[0, "auto"]} />
            <Tooltip
              contentStyle={{ background: "#111827", border: "1px solid #374151", fontSize: 11 }}
              labelFormatter={(t) => fmtClock(Number(t), true)}
              formatter={(v, name) => [typeof v === "number" ? yFmt(v) : "—", name]}
            />
            {limit != null && <ReferenceLine y={limit} stroke={COLORS.p99} strokeDasharray="2 4" strokeOpacity={0.6} />}
            {lines.map((l) => (
              <Line key={l.key} dataKey={l.key} name={l.name} type="monotone" stroke={l.color} strokeWidth={l.dashed ? 1.5 : 1.8} strokeDasharray={l.dashed ? "5 4" : undefined} dot={false} connectNulls={false} isAnimationActive={false} />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
