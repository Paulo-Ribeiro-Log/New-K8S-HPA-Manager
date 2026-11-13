import { memo, useCallback, useEffect, useRef, useMemo, useState, type ReactNode } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { BarChart3, ChevronDown, FileDown, SlidersHorizontal, ChevronRight, ExternalLink, Loader2, X, Play, RefreshCw, Square, TrendingDown, TrendingUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api/client";
import { PERCENTILE_CHOICES, REPORT_TZ, fmtInt, fmtLatency, fmtRps, togglePercentile, zonedLocalToISO, isoToZonedLocal, type ReportItem } from "@/lib/metricsReport";
import {
  DEFAULT_LIVE_RANGE, LATENCY_RED_MS, LEVEL_LABEL, LIVE_MAX_POINTS, LIVE_RANGES, LIVE_RESOURCES_KEY, LIVE_RANGE_KEY, LIVE_REFRESH_CHOICES, LIVE_REFRESH_KEY, PROJECTION_CHOICES, PROJECTION_KEY,
  buildChartData, errorLevel, fmtCores, fmtMemMB, fmtRefresh, fmtSpanLabel, fmtUsagePcts, parseLiveResources, replicasLevel, restartsLevel, hpaLevel, throttleLevel, podsLevel, fmtRestartReasons, netLevel, fmtBytesPerSec, usageLevel, lastValue, latencyLevel, liveRangeMs, liveWindow, minutesToThreshold, parseLiveRange, parseLiveRefresh, parseProjection, projectWithBaseline, DAY_MS, SEASONAL_LOOKBACK_MIN,
  normalizeLive, buildNetworkRows, projectedPercentile, assessSeries, summarizeStatus, worstLevel, type Level, type LiveChartData, type LiveRangeId, type LiveSeries, type Projection,
} from "@/lib/metricsLive";

// Tempo real do Relatório de Métricas: as mesmas métricas do relatório (Dynatrace), num cartão por
// aplicação no estilo do monitor de stress test (valor atual, barras de RPS do período, latências,
// erros e tendência). Atualiza sozinho a cada 1 a 5 minutos até o operador parar. Somente leitura.
//
// Memória: a resposta já vem reduzida pelo servidor (no máximo LIVE_MAX_POINTS pontos por série), o
// cartão desenha barras SVG leves (os gráficos completos só são montados quando o operador os abre), a
// consulta não fica em cache depois de substituída e a contagem regressiva é um componente à parte
// (não refaz a tela inteira a cada segundo).

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
import { MetricsLiveSummary } from "@/components/MetricsLiveSummary";
import { MetricsLiveTable } from "@/components/MetricsLiveTable";
import { COLORS, LEVEL_CLASS, LevelIcon, Spark, fmtClock } from "@/components/metricsLiveShared";
import { DEFAULT_SORT, SORT_KEY, VIEW_KEY, buildKpis, effectiveView, filterRows, groupRows, parseSort, parseViewMode, rowModel, sortRows, type SortKey, type SortState, type ViewMode } from "@/lib/metricsLiveView";
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

const COLLAPSED_KEY = "metrics_live_collapsed";
const ONLY_PROBLEMS_KEY = "metrics_live_only_problems";
const CARD_DETAILS_KEY = "metrics_live_card_details";
const SORT_LABEL: Record<SortKey, string> = { gravidade: "Gravidade", nome: "Nome", rps: "RPS", avg: "Response time", lat: "Latência (maior percentil)", p50: "P50", p95: "P95", p99: "P99", err5: "Erros 5xx", err4: "Erros 4xx", replicas: "Réplicas", cpu: "CPU", mem: "Memória" };
function parseCollapsed(raw: string | null): Set<string> {
  try {
    const v = raw ? JSON.parse(raw) : [];
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

const errText = (e: unknown) => (e instanceof Error ? e.message : "erro desconhecido");

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

  const pctsNow = data?.percentiles ?? percentiles;
  const status = useMemo(() => (data ? summarizeStatus(data.series, pctsNow) : null), [data, pctsNow]);

  // visão: tabela (padrão com muitas aplicações) ou cartões; ordenação; filtros. Tudo lembrado, menos o filtro por nível.
  const [viewMode, setViewMode] = useState<ViewMode>(() => loadPref(VIEW_KEY, parseViewMode));
  const [sort, setSort] = useState<SortState>(() => loadPref(SORT_KEY, parseSort));
  const [onlyProblems, setOnlyProblems] = useState<boolean>(() => loadPref(ONLY_PROBLEMS_KEY, (raw) => raw === "true"));
  const [cardDetails, setCardDetails] = useState<boolean>(() => loadPref(CARD_DETAILS_KEY, (raw) => raw !== "false"));
  const [levelFilter, setLevelFilter] = useState<Level | null>(null);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  useEffect(() => savePref(VIEW_KEY, viewMode), [viewMode]);
  useEffect(() => savePref(SORT_KEY, sort), [sort]);
  useEffect(() => savePref(ONLY_PROBLEMS_KEY, onlyProblems), [onlyProblems]);
  useEffect(() => savePref(CARD_DETAILS_KEY, cardDetails), [cardDetails]);
  const toggleRow = useCallback((key: string) => setExpandedRows((prev) => {
    const next = new Set(prev);
    if (!next.delete(key)) next.add(key);
    return next;
  }), []);
  const onSort = useCallback((key: SortKey) => setSort((prev) => (prev.key === key ? { key, desc: !prev.desc } : { key, desc: key !== "nome" })), []);

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
  const allRows = useMemo(() => (data ? data.series.map((s) => rowModel(s, pctsNow)) : []), [data, pctsNow]);
  const kpis = useMemo(() => buildKpis(allRows), [allRows]);
  const shown = useMemo(() => sortRows(filterRows(allRows, { onlyProblems, level: levelFilter }), sort), [allRows, onlyProblems, levelFilter, sort]);
  const groups = useMemo(() => groupRows(shown), [shown]);
  const view = effectiveView(viewMode, items.length);
  const showResources = allRows.some((r) => !!r.s.resources);
  const spanMs = range === "custom" && custom ? Date.parse(custom.end) - Date.parse(custom.start) : liveRangeMs(range === "custom" ? DEFAULT_LIVE_RANGE : range);
  const spanLabel = fmtSpanLabel(spanMs);
  const [detail, setDetail] = useState<LiveSeries | null>(null);

  return (
    <div className="space-y-3">
      {data && allRows.length > 0 && <MetricsLiveSummary kpis={kpis} status={status} level={levelFilter} onLevel={setLevelFilter} />}
      <section className="rounded-md border border-border p-3 space-y-2">
        {/* o essencial à vista; o que se mexe pouco fica em "Opções" */}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {running ? (
            <Button size="sm" variant="destructive" className="h-7 text-xs gap-1" onClick={() => setRunning(false)} title="Para as consultas ao Dynatrace; os dados ficam como estão">
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
          <label className="flex items-center gap-1.5" title="De quanto em quanto tempo os dados são atualizados (o Dynatrace entrega dado de 1 em 1 minuto)">
            Atualizar a cada
            <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={refresh} disabled={!relative} onChange={(e) => setRefresh(Number(e.target.value))}>
              {LIVE_REFRESH_CHOICES.map((s) => <option key={s} value={s}>{fmtRefresh(s)}</option>)}
              <option value={0}>{fmtRefresh(0)}</option>
            </select>
          </label>
          <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={items.length === 0 || customInvalid || q.isFetching} onClick={() => q.refetch()}>
            {q.isFetching ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}Atualizar agora
          </Button>
          <Countdown updatedAt={q.dataUpdatedAt} refreshSec={running && relative ? refresh : 0} fetching={q.isFetching} />
          <div className="ml-auto flex items-center gap-2">
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
            <Popover>
              <PopoverTrigger asChild>
                <Button size="sm" variant="outline" className="h-7 text-xs gap-1" title="Projeção, percentis de latência, itens do Prometheus e resumo da sessão">
                  <SlidersHorizontal className="h-3.5 w-3.5" />Opções
                </Button>
              </PopoverTrigger>
              <PopoverContent align="end" className="w-[26rem] space-y-3 p-3 text-xs">
                <div className="space-y-1">
                  <div className="font-medium">Projeção</div>
                  <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={horizon} disabled={!relative} onChange={(e) => setHorizon(Number(e.target.value))}>
                    {PROJECTION_CHOICES.map((m) => <option key={m} value={m}>{m === 0 ? "Desligada" : m === 60 ? "+1 h" : `+${m} min`}</option>)}
                  </select>
                  <div className="text-[10px] text-muted-foreground">Usa a curva de ontem no mesmo horário, escalada pelo nível de hoje (sem dado de ontem, uma reta). É extrapolação, não previsão. Só vale para períodos relativos.</div>
                </div>
                <div className="space-y-1">
                  <div className="font-medium">Latência</div>
                  <div className="flex items-center gap-3">
                    {PERCENTILE_CHOICES.map((p) => (
                      <label key={p} className="flex items-center gap-1.5 cursor-pointer">
                        <Checkbox checked={percentiles.includes(p)} onCheckedChange={(v) => onPercentiles(togglePercentile(percentiles, p, v === true))} />
                        P{p}
                      </label>
                    ))}
                  </div>
                  <div className="text-[10px] text-muted-foreground">Compartilhado com o relatório. A projeção e a situação usam o maior percentil marcado.</div>
                </div>
                <div className="space-y-1">
                  <div className="font-medium">Prometheus</div>
                  <label className="flex items-center gap-1.5 cursor-pointer" title="Réplicas, HPA, restarts, CPU, memória, throttling, rede e pods vêm do Prometheus do cluster. Tráfego, erros e latência vêm sempre do Dynatrace.">
                    <Checkbox checked={resources} onCheckedChange={(v) => setResources(v === true)} />
                    Réplicas, HPA, restarts, CPU, memória, throttling e rede
                  </label>
                  <div className="text-[10px] text-muted-foreground">Várias consultas ao Prometheus por cluster a cada atualização. Desligue se estiver lento; tráfego e latência continuam do Dynatrace.</div>
                </div>
                <div className="space-y-1 border-t border-border pt-2">
                  <div className="font-medium">Resumo da sessão</div>
                  <div className="text-muted-foreground" title="Agregados (mín/méd/máx) por aplicação, amostrados a cada atualização; a memória não cresce com o tempo">
                    {sessionUpdates} atualização(ões) desde {new Date(sessionRef.current.startedAt).toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ, hour: "2-digit", minute: "2-digit" })}
                  </div>
                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={sessionUpdates === 0} onClick={() => exportSession("md")} title="Baixa o resumo da sessão em Markdown"><FileDown className="h-3 w-3" />Resumo .md</Button>
                    <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={sessionUpdates === 0} onClick={() => exportSession("csv")} title="Baixa o resumo da sessão em CSV"><FileDown className="h-3 w-3" />CSV</Button>
                    <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={sessionUpdates === 0} onClick={() => { sessionRef.current = newSession(Date.now()); setSessionUpdates(0); }} title="Zera o resumo e começa uma sessão nova">Zerar</Button>
                  </div>
                </div>
              </PopoverContent>
            </Popover>
          </div>
        </div>
        {range === "custom" && (
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1.5">Início<Input type="datetime-local" className="h-7 w-48 text-xs" value={startLocal} onChange={(e) => setStartLocal(e.target.value)} /></label>
            <label className="flex items-center gap-1.5">Fim<Input type="datetime-local" className="h-7 w-48 text-xs" value={endLocal} onChange={(e) => setEndLocal(e.target.value)} /></label>
            <span className="text-muted-foreground">fuso {REPORT_TZ} · período passado: sem atualização automática nem projeção</span>
            {customInvalid && <span className="text-red-400">Data ou hora inválida.</span>}
          </div>
        )}
        {data && (
          <div className="text-[11px] text-muted-foreground">
            {items.length} aplicação(ões) · cada ponto = {fmtStep(data.step_seconds)} (Dynatrace {data.resolution}) · {new Date(data.window.start).toLocaleString("pt-BR", { timeZone: REPORT_TZ })} → {new Date(data.window.end).toLocaleString("pt-BR", { timeZone: REPORT_TZ })}
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

      {data && allRows.length > 0 && (
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <div role="group" aria-label="Visão" className="inline-flex overflow-hidden rounded-md border border-border">
            {(["table", "cards"] as const).map((v) => (
              <button key={v} type="button" aria-pressed={view === v} onClick={() => setViewMode(v)} className={`px-2.5 py-1 ${view === v ? "bg-primary text-primary-foreground" : "hover:bg-muted"}`} title={v === "table" ? "Uma linha por aplicação: melhor para muitas aplicações" : "Um cartão por aplicação: melhor para poucas"}>
                {v === "table" ? "Tabela" : "Cartões"}
              </button>
            ))}
          </div>
          <label className="flex items-center gap-1.5">
            Ordenar
            <select className="h-7 rounded border border-border bg-background px-1 text-xs" value={sort.key} onChange={(e) => setSort({ key: e.target.value as SortKey, desc: e.target.value !== "nome" })}>
              {(Object.keys(SORT_LABEL) as SortKey[]).filter((k) => k !== "p50" && k !== "p95" && k !== "p99" ? true : pctsNow.includes(Number(k.slice(1)))).map((k) => <option key={k} value={k}>{SORT_LABEL[k]}</option>)}
            </select>
            <button type="button" className="rounded border border-border px-1.5 py-0.5 hover:bg-muted" title="Inverter o sentido" onClick={() => setSort((p) => ({ ...p, desc: !p.desc }))}>{sort.desc ? "↓" : "↑"}</button>
          </label>
          <label className="flex items-center gap-1.5 cursor-pointer" title="Esconde as aplicações em OK ou sem dado e deixa só as em ERRO ou ALERTA">
            <Checkbox checked={onlyProblems} onCheckedChange={(v) => setOnlyProblems(v === true)} />
            Só problemas
          </label>
          {levelFilter && (
            <button type="button" className="inline-flex items-center gap-1 rounded border border-border px-1.5 py-0.5 hover:bg-muted" onClick={() => setLevelFilter(null)} title="Tirar o filtro">
              filtro: {LEVEL_LABEL[levelFilter]} <X className="h-3 w-3" />
            </button>
          )}
          <span className="text-muted-foreground">{shown.length} de {allRows.length} aplicação(ões){viewMode === "auto" ? " · visão automática" : ""}</span>
          {view === "cards" && (
            <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setCardDetails((v) => !v)} title="Mostra ou esconde as linhas de detalhe de todos os cartões">
              {cardDetails ? "esconder detalhes" : "mostrar detalhes"}
            </button>
          )}
        </div>
      )}
      {data && allRows.length > 0 && shown.length === 0 && <div className="text-xs text-muted-foreground">Nenhuma aplicação com este filtro.</div>}

      {view === "table" && shown.length > 0 && (
        <MetricsLiveTable
          groups={groups}
          sort={sort}
          onSort={onSort}
          collapsed={collapsed}
          onToggleCluster={toggleCollapsed}
          expanded={expandedRows}
          onToggleRow={toggleRow}
          renderExpanded={(s) => (
            <LiveCard s={s} base={baseMap.get(`${s.cluster}|${s.namespace}|${s.workload}`)} percentiles={pctsNow} horizon={effHorizon} spanLabel={spanLabel} onDetail={setDetail} onRemove={onRemove} detailsOpen onToggleDetails={() => {}} showToggle={false} />
          )}
          onDetail={setDetail}
          onRemove={onRemove}
          percentiles={pctsNow}
          showResources={showResources}
        />
      )}

      {view === "cards" && groups.map((g) => (
        <section key={g.cluster} className="space-y-2">
          <button className="flex items-center gap-1.5 text-sm font-medium text-amber-500 hover:text-amber-400" title={collapsed.has(g.cluster) ? "Expandir o cluster" : "Minimizar o cluster"} onClick={() => toggleCollapsed(g.cluster)}>
            {collapsed.has(g.cluster) ? <ChevronRight className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            {g.cluster}
            <span className="text-[11px] font-normal text-muted-foreground">({g.rows.length} aplicação(ões))</span>
            {collapsed.has(g.cluster) && (() => {
              const lv = worstLevel(g.rows.map((x) => x.level));
              return <span className={`inline-flex items-center gap-1 text-[11px] font-medium ${LEVEL_CLASS[lv]}`}><LevelIcon level={lv} className="h-3 w-3" />{LEVEL_LABEL[lv]}</span>;
            })()}
          </button>
          {!collapsed.has(g.cluster) && <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {g.rows.map((r) => (
              <LiveCard key={r.key} s={r.s} base={baseMap.get(r.key)} percentiles={pctsNow} horizon={effHorizon} spanLabel={spanLabel} onDetail={setDetail} onRemove={onRemove} detailsOpen={cardDetails} onToggleDetails={() => setCardDetails((v) => !v)} showToggle />
            ))}
          </div>}
        </section>
      ))}

      <div className="text-right"><BrowserMemory /></div>

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

// Número em destaque do cartão: rótulo pequeno, valor grande em tinta primária; o ícone (não a cor do
// número) sinaliza ALERTA ou ERRO.
function Hero({ label, value, unit, level }: { label: string; value: string; unit?: string; level?: Level }) {
  return (
    <div className="min-w-0 sm:px-3 sm:first:pl-0 sm:last:pr-0">
      <div className="flex items-center gap-1 text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
        {(level === "warn" || level === "red") && <LevelIcon level={level} className="h-3 w-3" />}
      </div>
      <div className="truncate text-lg font-semibold leading-tight tabular-nums text-foreground">{value}</div>
      {unit && <div className="truncate text-[10px] text-muted-foreground">{unit}</div>}
    </div>
  );
}

const LiveCard = memo(function LiveCard({
  s, base, percentiles, horizon, spanLabel, onDetail, onRemove, detailsOpen, onToggleDetails, showToggle,
}: {
  detailsOpen: boolean;
  onToggleDetails: () => void;
  showToggle: boolean;
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
  const assessed = useMemo(() => assessSeries(s, percentiles), [s, percentiles]);
  const latValue = lastValue(latP === 99 ? s.p99_ms : latP === 95 ? s.p95_ms : s.p50_ms);
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
        <span className="self-center" title={assessed.reasons.join("\n") || LEVEL_LABEL[assessed.level]}><LevelIcon level={assessed.level} /></span>
        <span className="text-sm font-semibold text-sky-400">{s.workload}</span>
        <span className="text-[10px] text-muted-foreground truncate" title={s.namespace}>ns: {s.namespace}</span>
        <button className="ml-auto text-muted-foreground hover:text-foreground" title="Abrir os gráficos completos" onClick={() => onDetail(s)}>
          <BarChart3 className="h-3.5 w-3.5" />
        </button>
        {removeBtn}
      </div>
      {/* o essencial na frente: quatro números grandes; o resto fica em "Detalhes" */}
      <div className="my-2 grid grid-cols-2 gap-x-2 gap-y-3 rounded-lg bg-muted/40 px-3 py-2.5 font-sans ring-1 ring-inset ring-border/60 sm:grid-cols-4 sm:gap-y-0 sm:divide-x sm:divide-border/50">
        <Hero label="RPS" value={s.error && !r ? "—" : fmtRps(rpsNow)} unit="req/s" />
        <Hero label={`P${latP}`} value={fmtLatency(latValue)} level={latencyLevel(latValue)} />
        <Hero label="Erros 5xx" value={fmtInt(s.err_5xx_total)} unit="no período" level={errorLevel(s.err_5xx_total, "5xx")} />
        <Hero label="Réplicas" value={r?.replicas_available != null ? `${r.replicas_available}/${r.replicas_desired ?? "?"}` : "—"} unit={hpaLevel(r?.replicas_desired, r?.hpa_max) === "warn" ? "no teto do HPA" : undefined} level={replicasLevel(r?.replicas_available, r?.replicas_desired) === "red" ? "red" : hpaLevel(r?.replicas_desired, r?.hpa_max) === "warn" ? "warn" : undefined} />
      </div>
      {showToggle && (
        <button type="button" className="mb-1 inline-flex items-center gap-1 font-sans text-[11px] text-muted-foreground hover:text-foreground" onClick={onToggleDetails}>
          {detailsOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}Detalhes
        </button>
      )}
      {detailsOpen && (
      <>
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
        <Row label="Response time" level="info">
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
