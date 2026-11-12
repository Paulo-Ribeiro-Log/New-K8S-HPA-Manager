// Tempo real do Relatório de Métricas: faixa de tempo (estilo Grafana), intervalo de atualização,
// dados dos gráficos e projeção de tendência. Lógica pura (testada em metricsLive.test.ts); nenhuma
// chamada de rede aqui.

import type { ReportItem } from "@/lib/metricsReport";

// ── tipos da API (espelham report.LiveResult) ──────────────────────────────────────────────────────

/** Itens que só existem no Prometheus (réplicas, restarts, CPU e memória). Nulo = sem dado. */
export interface LiveResources {
  replicas_available: number | null;
  replicas_desired: number | null;
  restarts_15m: number | null;
  cpu_cores: number | null;
  cpu_req_pct: number | null;
  cpu_lim_pct: number | null;
  mem_mb: number | null;
  mem_req_pct: number | null;
  mem_lim_pct: number | null;
  restart_reasons?: Record<string, number> | null;
  pods_pending?: number | null;
  pods_not_ready?: number | null;
  hpa_min?: number | null;
  hpa_max?: number | null;
  cpu_throttle_pct?: number | null;
  net_rx_bps?: number | null;
  net_tx_bps?: number | null;
  net_err_ps?: number | null;
  pods: number;
}

export interface LiveLink {
  name: string;
  url: string;
}

/** Rede de uma aplicação ao longo do tempo (gráfico do modal): bytes/s e descartes+erros/s. */
export interface NetworkSeries {
  step_seconds: number;
  times: number[];
  rx_bps: (number | null)[];
  tx_bps: (number | null)[];
  err_ps: (number | null)[];
  notes?: string[];
}

export interface NetworkRow {
  t: number;
  rx: number | null;
  tx: number | null;
  err: number | null;
}

/** Linhas do gráfico de rede; aceita listas nulas do Go. `hasErrors` diz se há algum descarte/erro a mostrar. */
export function buildNetworkRows(n: Partial<NetworkSeries> | null | undefined): { rows: NetworkRow[]; hasData: boolean; hasErrors: boolean } {
  const times = n?.times ?? [];
  const rows = times.map((t, i) => ({ t, rx: n?.rx_bps?.[i] ?? null, tx: n?.tx_bps?.[i] ?? null, err: n?.err_ps?.[i] ?? null }));
  return {
    rows,
    hasData: rows.some((r) => r.rx != null || r.tx != null),
    hasErrors: rows.some((r) => r.err != null && r.err > 0),
  };
}

export interface LiveSeries extends ReportItem {
  error?: string;
  times: number[]; // início de cada intervalo, epoch em ms
  rps: (number | null)[];
  err_4xx_rps: (number | null)[];
  err_5xx_rps: (number | null)[];
  /** Contagem de erros no período (inteiro, somada no servidor antes de reduzir os pontos). */
  err_4xx_total: number | null;
  err_5xx_total: number | null;
  p50_ms?: (number | null)[];
  p95_ms?: (number | null)[];
  p99_ms?: (number | null)[];
  resources?: LiveResources;
  services?: string[];
  /** Links diretos para os serviços no Dynatrace. */
  links?: LiveLink[];
  resolution?: string;
  notes?: string[];
}

export interface LiveResult {
  window: { start: string; end: string };
  resolution: string;
  /** Intervalo de cada ponto da resposta em segundos (resolução × agrupamento feito no servidor). */
  step_seconds: number;
  percentiles: number[];
  warnings?: string[];
  series: LiveSeries[];
  generated_at: string;
}

// ── faixa de tempo ─────────────────────────────────────────────────────────────────────────────────

const MIN = 60_000;
const HOUR = 60 * MIN;

/** Faixas relativas ("últimos X"), como o seletor do Grafana. A janela sempre termina agora. */
export const LIVE_RANGES = [
  { id: "5m", label: "Últimos 5 min", ms: 5 * MIN },
  { id: "15m", label: "Últimos 15 min", ms: 15 * MIN },
  { id: "30m", label: "Últimos 30 min", ms: 30 * MIN },
  { id: "1h", label: "Última 1 h", ms: HOUR },
  { id: "3h", label: "Últimas 3 h", ms: 3 * HOUR },
  { id: "6h", label: "Últimas 6 h", ms: 6 * HOUR },
  { id: "12h", label: "Últimas 12 h", ms: 12 * HOUR },
  { id: "24h", label: "Últimas 24 h", ms: 24 * HOUR },
  { id: "2d", label: "Últimos 2 dias", ms: 48 * HOUR },
  { id: "7d", label: "Últimos 7 dias", ms: 7 * 24 * HOUR },
] as const;

export type LiveRangeId = (typeof LIVE_RANGES)[number]["id"];
export const DEFAULT_LIVE_RANGE: LiveRangeId = "1h";
export const LIVE_RANGE_KEY = "metrics_report_live_range";

export function liveRangeMs(id: LiveRangeId): number {
  return (LIVE_RANGES.find((r) => r.id === id) ?? LIVE_RANGES[3]).ms;
}

/** Janela "últimos X até agora" em RFC 3339 (UTC). */
export function liveWindow(id: LiveRangeId, now: number = Date.now()): { start: string; end: string } {
  return { start: new Date(now - liveRangeMs(id)).toISOString(), end: new Date(now).toISOString() };
}

export function parseLiveRange(raw: string | null): LiveRangeId {
  try {
    const v = raw ? JSON.parse(raw) : null;
    return LIVE_RANGES.some((r) => r.id === v) ? (v as LiveRangeId) : DEFAULT_LIVE_RANGE;
  } catch {
    return DEFAULT_LIVE_RANGE;
  }
}

// ── intervalo de atualização: 1 a 5 minutos (o Dynatrace entrega dado de 1 em 1 min) ──────────────

export const LIVE_REFRESH_CHOICES = [60, 120, 180, 240, 300] as const; // segundos
export const DEFAULT_LIVE_REFRESH = 60;
export const LIVE_REFRESH_KEY = "metrics_report_live_refresh";

/** Lê o intervalo gravado: 0 = pausado; só 1 a 5 min são aceitos, qualquer outro valor volta ao padrão. */
export function parseLiveRefresh(raw: string | null): number {
  try {
    const v = raw ? JSON.parse(raw) : null;
    if (v === 0) return 0;
    return (LIVE_REFRESH_CHOICES as readonly number[]).includes(v) ? v : DEFAULT_LIVE_REFRESH;
  } catch {
    return DEFAULT_LIVE_REFRESH;
  }
}

export function fmtRefresh(sec: number): string {
  return sec === 0 ? "Pausado" : `${sec / 60} min`;
}

// ── projeção de tendência ──────────────────────────────────────────────────────────────────────────

/** Horizontes da projeção (minutos); 0 = desligada. */
export const PROJECTION_CHOICES = [0, 15, 30, 60] as const;
export const DEFAULT_PROJECTION = 30;
export const PROJECTION_KEY = "metrics_report_live_projection";

export function parseProjection(raw: string | null): number {
  try {
    const v = raw ? JSON.parse(raw) : null;
    return (PROJECTION_CHOICES as readonly number[]).includes(v) ? v : DEFAULT_PROJECTION;
  } catch {
    return DEFAULT_PROJECTION;
  }
}

/** Mínimo de pontos válidos e de ajuste (R²) para a projeção ser dada como confiável. */
export const MIN_PROJECTION_POINTS = 6;
export const MIN_PROJECTION_R2 = 0.3;

export interface Projection {
  /** Valor da reta ajustada no último ponto real (base da comparação, menos ruidosa que o último ponto). */
  fittedNow: number;
  /** Valor projetado no fim do horizonte (nunca negativo). */
  projected: number;
  /** Variação projetada em relação ao ajuste atual, em %. Nulo quando o ajuste atual é ~0. */
  changePct: number | null;
  /** Inclinação, em unidades por hora. */
  slopePerHour: number;
  /** Quanto da variação a reta explica (0 a 1). */
  r2: number;
  points: number;
  /** false quando há poucos pontos ou a reta explica pouco: mostrar "sem tendência clara". */
  reliable: boolean;
  /** Os pontos da linha tracejada: do último instante real ao fim do horizonte (epoch ms). */
  line: { t: number; v: number }[];
  /** De onde veio: "ontem" = curva de ontem no mesmo horário, escalada pelo nível de hoje; "reta" = tendência linear do período. */
  basis?: "ontem" | "reta";
}

/**
 * Projeta por regressão linear (mínimos quadrados) sobre os pontos válidos da janela visível. É uma
 * extrapolação de tendência, não previsão: não conhece sazonalidade nem limite de capacidade. Devolve
 * nulo quando não há pontos suficientes para ajustar uma reta.
 */
export function projectTrend(times: number[], values: (number | null | undefined)[], horizonMin: number): Projection | null {
  const pts: { t: number; v: number }[] = [];
  for (let i = 0; i < times.length; i++) {
    const v = values[i];
    if (v != null && Number.isFinite(v)) pts.push({ t: times[i], v });
  }
  if (pts.length < 2 || horizonMin <= 0) return null;
  const last = pts[pts.length - 1].t;
  // x em minutos relativos ao último ponto (evita perda de precisão com epoch em ms)
  const xs = pts.map((p) => (p.t - last) / MIN);
  const n = pts.length;
  const mx = xs.reduce((a, b) => a + b, 0) / n;
  const my = pts.reduce((a, p) => a + p.v, 0) / n;
  let sxx = 0, sxy = 0, syy = 0;
  for (let i = 0; i < n; i++) {
    const dx = xs[i] - mx, dy = pts[i].v - my;
    sxx += dx * dx;
    sxy += dx * dy;
    syy += dy * dy;
  }
  if (sxx === 0) return null;
  const slope = sxy / sxx; // por minuto
  const intercept = my - slope * mx; // valor ajustado em x = 0 (último ponto)
  const r2 = syy === 0 ? 0 : (sxy * sxy) / (sxx * syy);
  const fittedNow = Math.max(0, intercept);
  const projected = Math.max(0, intercept + slope * horizonMin);
  return {
    fittedNow,
    projected,
    changePct: fittedNow > 1e-9 ? ((projected - fittedNow) / fittedNow) * 100 : null,
    slopePerHour: slope * 60,
    r2,
    points: n,
    reliable: n >= MIN_PROJECTION_POINTS && r2 >= MIN_PROJECTION_R2,
    line: [
      { t: last, v: fittedNow },
      { t: last + horizonMin * MIN, v: projected },
    ],
  };
}

/** Janela de comparação com ontem e mínimo de pares hoje×ontem para a projeção sazonal valer. */
export const SEASONAL_LOOKBACK_MIN = 30;
export const SEASONAL_MIN_PAIRS = 5;
export const DAY_MS = 24 * 60 * MIN;
/** Limites do fator hoje/ontem: fora disso o dia de ontem não serve de base. */
const SEASONAL_RATIO_MIN = 0.1;
const SEASONAL_RATIO_MAX = 10;

/** Valor de ontem num instante: média dos pontos da série-base a até 90 s do mesmo horário de ontem. */
function baseAt(base: { t: number; v: number }[], t: number): number | null {
  const target = t - DAY_MS;
  let sum = 0, n = 0;
  for (const b of base) {
    if (Math.abs(b.t - target) <= 90_000) {
      sum += b.v;
      n++;
    }
  }
  return n ? sum / n : null;
}

/**
 * Projeção sazonal: usa a curva de ontem no mesmo horário (que já traz o ciclo diário) e a escala pelo
 * fator hoje/ontem dos últimos 30 min. Nulo quando falta dado de ontem ou de hoje; quem chama cai na reta.
 */
export function projectSeasonal(
  times: number[],
  values: (number | null | undefined)[],
  baseTimes: number[],
  baseValues: (number | null | undefined)[],
  horizonMin: number,
): Projection | null {
  if (horizonMin <= 0) return null;
  const today: { t: number; v: number }[] = [];
  for (let i = 0; i < times.length; i++) {
    const v = values[i];
    if (v != null && Number.isFinite(v)) today.push({ t: times[i], v });
  }
  const base: { t: number; v: number }[] = [];
  for (let i = 0; i < baseTimes.length; i++) {
    const v = baseValues[i];
    if (v != null && Number.isFinite(v)) base.push({ t: baseTimes[i], v });
  }
  if (!today.length || !base.length) return null;
  const last = today[today.length - 1].t;

  let sumToday = 0, sumBase = 0, pairs = 0;
  for (const p of today) {
    if (p.t < last - SEASONAL_LOOKBACK_MIN * MIN) continue;
    const b = baseAt(base, p.t);
    if (b == null) continue;
    sumToday += p.v;
    sumBase += b;
    pairs++;
  }
  if (pairs < SEASONAL_MIN_PAIRS || sumBase <= 1e-9) return null;
  const ratio = sumToday / sumBase;
  if (ratio < SEASONAL_RATIO_MIN || ratio > SEASONAL_RATIO_MAX) return null;

  // "agora" suavizado: ontem perto do último instante, na escala de hoje
  const nowBase = baseAt(base, last);
  const endBase = baseAt(base, last + horizonMin * MIN);
  if (nowBase == null || endBase == null) return null;
  const fittedNow = ratio * nowBase;
  const projected = ratio * endBase;
  const line: { t: number; v: number }[] = [{ t: last, v: fittedNow }];
  for (let m = 5; m < horizonMin; m += 5) {
    const b = baseAt(base, last + m * MIN);
    if (b != null) line.push({ t: last + m * MIN, v: ratio * b });
  }
  line.push({ t: last + horizonMin * MIN, v: projected });
  return {
    fittedNow,
    projected,
    changePct: fittedNow > 1e-9 ? ((projected - fittedNow) / fittedNow) * 100 : null,
    slopePerHour: ((projected - fittedNow) / horizonMin) * 60,
    r2: 0,
    points: pairs,
    reliable: true,
    line,
    basis: "ontem",
  };
}

/** Sazonal quando há base de ontem utilizável; senão a reta do período. */
export function projectWithBaseline(
  times: number[],
  values: (number | null | undefined)[],
  base: { times: number[]; values: (number | null | undefined)[] } | null | undefined,
  horizonMin: number,
): Projection | null {
  if (base) {
    const sp = projectSeasonal(times, values, base.times, base.values, horizonMin);
    if (sp) return sp;
  }
  const lin = projectTrend(times, values, horizonMin);
  return lin ? { ...lin, basis: "reta" } : null;
}

/**
 * Em quantos minutos a reta cruza o limite, se estiver subindo e abaixo dele agora. Nulo quando a
 * projeção não é confiável, a reta não sobe, já passou do limite ou só cruzaria depois do horizonte.
 */
export function minutesToThreshold(p: Projection | null, threshold: number, horizonMin: number): number | null {
  if (!p || !p.reliable || p.slopePerHour <= 0 || p.fittedNow >= threshold) return null;
  const m = ((threshold - p.fittedNow) / p.slopePerHour) * 60;
  return m <= horizonMin ? m : null;
}

// ── dados dos gráficos ─────────────────────────────────────────────────────────────────────────────

export interface LiveRow {
  t: number;
  rps?: number | null;
  e4?: number | null;
  e5?: number | null;
  p50?: number | null;
  p95?: number | null;
  p99?: number | null;
  /** Reta tracejada de projeção (só nas linhas de ligação e do futuro). */
  rpsProj?: number;
  latProj?: number;
}

/** Percentil usado na projeção de latência: o mais alto entre os coletados (o que mais importa para o limite). */
export function projectedPercentile(percentiles: number[]): 50 | 95 | 99 {
  return percentiles.includes(99) ? 99 : percentiles.includes(95) ? 95 : 50;
}

function latSeries(s: LiveSeries, p: 50 | 95 | 99) {
  return p === 99 ? s.p99_ms : p === 95 ? s.p95_ms : s.p50_ms;
}

export interface LiveChartData {
  rows: LiveRow[];
  rpsProjection: Projection | null;
  latProjection: Projection | null;
  latPercentile: 50 | 95 | 99;
  /** true quando o servidor devolveu pelo menos um ponto de cada tipo (decide quais gráficos desenhar). */
  hasLatency: boolean;
  hasErrors: boolean;
}

const hasAny = (a?: (number | null)[]) => !!a && a.some((v) => v != null);

/** Junta a série e a projeção numa lista só, ordenada por instante, para o gráfico. */
export function buildChartData(s: LiveSeries, percentiles: number[], horizonMin: number, base?: LiveSeries | null): LiveChartData {
  const rows: LiveRow[] = s.times.map((t, i) => ({
    t,
    rps: s.rps[i] ?? null,
    e4: s.err_4xx_rps[i] ?? null,
    e5: s.err_5xx_rps[i] ?? null,
    p50: s.p50_ms?.[i] ?? null,
    p95: s.p95_ms?.[i] ?? null,
    p99: s.p99_ms?.[i] ?? null,
  }));
  const latPercentile = projectedPercentile(percentiles);
  const rpsProjection = projectWithBaseline(s.times, s.rps, base ? { times: base.times, values: base.rps } : null, horizonMin);
  const baseLat = base ? latSeries(base, latPercentile) : null;
  const latProjection = projectWithBaseline(s.times, latSeries(s, latPercentile) ?? [], base && baseLat ? { times: base.times, values: baseLat } : null, horizonMin);

  const at = new Map(rows.map((r) => [r.t, r]));
  const put = (proj: Projection | null, key: "rpsProj" | "latProj") => {
    if (!proj) return;
    for (const pt of proj.line) {
      const row = at.get(pt.t) ?? { t: pt.t };
      row[key] = pt.v;
      at.set(pt.t, row);
    }
  };
  put(rpsProjection, "rpsProj");
  put(latProjection, "latProj");
  return {
    rows: [...at.values()].sort((a, b) => a.t - b.t),
    rpsProjection,
    latProjection,
    latPercentile,
    hasLatency: hasAny(s.p50_ms) || hasAny(s.p95_ms) || hasAny(s.p99_ms),
    hasErrors: hasAny(s.err_4xx_rps) || hasAny(s.err_5xx_rps),
  };
}

/** Último valor não nulo de uma série (o "valor atual" do cartão). */
export function lastValue(a: (number | null | undefined)[] | undefined): number | null {
  if (!a) return null;
  for (let i = a.length - 1; i >= 0; i--) {
    const v = a[i];
    if (v != null) return v;
  }
  return null;
}

/** Agrupa as séries por cluster, mantendo a ordem em que aparecem. */
export function groupLive(series: LiveSeries[]): { cluster: string; series: LiveSeries[] }[] {
  const out: { cluster: string; series: LiveSeries[] }[] = [];
  for (const s of series) {
    let g = out.find((x) => x.cluster === s.cluster);
    if (!g) {
      g = { cluster: s.cluster, series: [] };
      out.push(g);
    }
    g.series.push(s);
  }
  return out;
}

// ── cartão compacto: níveis, barras e rótulos ─────────────────────────────────────────────────────

/** Pontos por série pedidos ao servidor: o cartão não desenha mais barras que isso, e a resposta fica pequena. */
export const LIVE_MAX_POINTS = 120;

export type Level = "ok" | "warn" | "red" | "info";
export const LEVEL_LABEL: Record<Level, string> = { ok: "OK", warn: "ALERTA", red: "ERRO", info: "INFO" };

/** Limites de latência: os do selo do relatório (P99 acima de 1 s é vermelho); metade disso já chama atenção. */
export const LATENCY_WARN_MS = 500;
export const LATENCY_RED_MS = 1000;

export function latencyLevel(ms: number | null | undefined): Level {
  if (ms == null) return "info";
  return ms > LATENCY_RED_MS ? "red" : ms > LATENCY_WARN_MS ? "warn" : "ok";
}

/** Erros por segundo: qualquer 5xx é vermelho e qualquer 4xx é alerta (mesma regra do relatório). Sem dado = info. */
export function errorLevel(rate: number | null | undefined, kind: "4xx" | "5xx"): Level {
  if (rate == null) return "info";
  return rate > 0 ? (kind === "5xx" ? "red" : "warn") : "ok";
}

const LEVEL_RANK: Record<Level, number> = { info: 0, ok: 1, warn: 2, red: 3 };

/** O pior nível de uma lista (vermelho > alerta > ok > info); lista vazia = info. */
export function worstLevel(levels: Level[]): Level {
  return levels.reduce<Level>((w, l) => (LEVEL_RANK[l] > LEVEL_RANK[w] ? l : w), "info");
}

/** Nível resumido de uma aplicação (cartão minimizado e cabeçalho do cluster): latência do maior percentil marcado, 5xx e 4xx. */
export function seriesLevel(s: LiveSeries, percentiles: number[]): Level {
  const p = projectedPercentile(percentiles);
  return worstLevel([
    latencyLevel(lastValue(p === 99 ? s.p99_ms : p === 95 ? s.p95_ms : s.p50_ms)),
    errorLevel(lastValue(s.err_5xx_rps), "5xx"),
    errorLevel(lastValue(s.err_4xx_rps), "4xx"),
  ]);
}

export interface StatusEntry {
  /** "cluster · aplicação" */
  name: string;
  reasons: string[];
}
export type StatusSummary = Record<Level, StatusEntry[]>;

const secs = (ms: number) => `${(ms / 1000).toFixed(3)}s`;
const perSec = (v: number) => `${v.toLocaleString("pt-BR", { maximumFractionDigits: 1 })}/s`;

/** Resumo para os selos do topo: aplicações agrupadas pelo nível, com o motivo de cada uma. Falha de coleta conta como ERRO. */
export function summarizeStatus(series: LiveSeries[], percentiles: number[]): StatusSummary {
  const out: StatusSummary = { red: [], warn: [], ok: [], info: [] };
  const p = projectedPercentile(percentiles);
  for (const s of series) {
    const name = `${s.cluster} · ${s.workload}`;
    if (s.error && !s.resources) {
      out.red.push({ name, reasons: [`falha na coleta: ${s.error}`] });
      continue;
    }
    const lat = lastValue(p === 99 ? s.p99_ms : p === 95 ? s.p95_ms : s.p50_ms);
    const e5 = lastValue(s.err_5xx_rps);
    const e4 = lastValue(s.err_4xx_rps);
    const level = worstLevel([latencyLevel(lat), errorLevel(e5, "5xx"), errorLevel(e4, "4xx")]);
    const reasons: string[] = [];
    if (latencyLevel(lat) === "red" || latencyLevel(lat) === "warn") reasons.push(`P${p} ${secs(lat as number)}`);
    if (errorLevel(e5, "5xx") === "red") reasons.push(`5xx ${perSec(e5 as number)}${s.err_5xx_total != null ? ` (${Math.round(s.err_5xx_total).toLocaleString("pt-BR")} no período)` : ""}`);
    if (errorLevel(e4, "4xx") === "warn") reasons.push(`4xx ${perSec(e4 as number)}${s.err_4xx_total != null ? ` (${Math.round(s.err_4xx_total).toLocaleString("pt-BR")} no período)` : ""}`);
    if (level === "info") reasons.push("sem dados no período");
    out[level].push({ name, reasons });
  }
  return out;
}

/** Alturas (0 a 1) das barras da sparkline, relativas ao maior valor; nulo continua nulo (lacuna, nunca zero). */
export function sparkHeights(values: (number | null | undefined)[]): { heights: (number | null)[]; max: number | null } {
  let max: number | null = null;
  for (const v of values) if (v != null && (max === null || v > max)) max = v;
  return { heights: values.map((v) => (v == null ? null : max && max > 0 ? v / max : 0)), max };
}

/** "30m", "1h", "24h", "7d": o tamanho do período nos rótulos ("RPS 30m"). */
export function fmtSpanLabel(ms: number): string {
  const min = Math.round(ms / MIN);
  if (min < 60) return `${min}m`;
  if (min % (24 * 60) === 0) return `${min / (24 * 60)}d`;
  if (min % 60 === 0) return `${min / 60}h`;
  return `${(min / 60).toFixed(1)}h`;
}

// ── itens do Prometheus (réplicas, restarts, CPU, memória) ────────────────────────────────────────

export const LIVE_RESOURCES_KEY = "metrics_report_live_resources";

/** Lê a escolha gravada: ligado por padrão; só "false" desliga. */
export function parseLiveResources(raw: string | null): boolean {
  return raw !== "false";
}

/** Réplicas: menos disponíveis que desejadas é ERRO (rollout, falta de capacidade ou pods caindo). */
export function replicasLevel(avail: number | null | undefined, desired: number | null | undefined): Level {
  if (avail == null || desired == null) return "info";
  return avail >= desired ? "ok" : "red";
}

/** Restarts nos últimos 15 min: qualquer um é alerta. */
export function restartsLevel(n: number | null | undefined): Level {
  if (n == null) return "info";
  return n > 0 ? "warn" : "ok";
}

/** Uso em % do limit: acima de 85% é ERRO, acima de 70% é ALERTA. Sem limit definido não há veredito (info). */
export function usageLevel(limPct: number | null | undefined): Level {
  if (limPct == null) return "info";
  return limPct > 85 ? "red" : limPct > 70 ? "warn" : "ok";
}

/** "OOMKilled ×2, Error ×1": as razões dos restarts, da mais frequente para a menos (empate por nome). */
export function fmtRestartReasons(m: Record<string, number> | null | undefined): string {
  const e = Object.entries(m ?? {}).filter(([, n]) => n > 0);
  e.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  return e.map(([r, n]) => `${r} ×${n}`).join(", ");
}

/** Pods pendentes ou não prontos: qualquer um é alerta (rollout, scale-out em andamento ou falta de capacidade). */
export function podsLevel(pending: number | null | undefined, notReady: number | null | undefined): Level {
  if (pending == null && notReady == null) return "info";
  return (pending ?? 0) > 0 || (notReady ?? 0) > 0 ? "warn" : "ok";
}

/** HPA no teto (réplicas desejadas >= máximo): a aplicação não escala mais e a latência tende a subir. Sem HPA = info. */
export function hpaLevel(desired: number | null | undefined, max: number | null | undefined): Level {
  if (max == null || desired == null) return "info";
  return max > 0 && desired >= max ? "warn" : "ok";
}

/** Throttling de CPU (% dos períodos limitados): acima de 25% é ERRO, acima de 10% é ALERTA. */
export function throttleLevel(pct: number | null | undefined): Level {
  if (pct == null) return "info";
  return pct > 25 ? "red" : pct > 10 ? "warn" : "ok";
}

/** Rede: pacotes descartados + erros acima de 0,5/s é ALERTA. O volume em si não tem veredito. */
export function netLevel(errPs: number | null | undefined): Level {
  if (errPs == null) return "info";
  return errPs > 0.5 ? "warn" : "ok";
}

/** Bytes por segundo em B, KB, MB ou GB (base 1024), com uma casa. */
export function fmtBytesPerSec(v: number | null | undefined): string {
  if (v == null) return "sem dado";
  const units = ["B/s", "KB/s", "MB/s", "GB/s"];
  let x = Math.max(0, v);
  let i = 0;
  while (x >= 1024 && i < units.length - 1) {
    x /= 1024;
    i++;
  }
  return `${i === 0 ? Math.round(x) : x.toLocaleString("pt-BR", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} ${units[i]}`;
}

export function fmtCores(v: number | null | undefined): string {
  return v == null ? "sem dado" : `${v.toFixed(2)} cores`;
}

/** MB até 1 GB, depois GB com uma casa ("64392 MB" vira "62.9 GB"). */
export function fmtMemMB(mb: number | null | undefined): string {
  if (mb == null) return "sem dado";
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${Math.round(mb)} MB`;
}

/** "(0.9% do limit · 5.1% do request)": só o que existe. */
export function fmtUsagePcts(limPct: number | null | undefined, reqPct: number | null | undefined): string {
  const parts: string[] = [];
  if (limPct != null) parts.push(`${limPct.toFixed(1)}% do limit`);
  if (reqPct != null) parts.push(`${reqPct.toFixed(1)}% do request`);
  return parts.length ? `(${parts.join(" · ")})` : "";
}

/** O Go serializa slice vazio/nil como `null`: a tela assume listas. Normaliza ao receber a resposta. */
export function normalizeLive(r: LiveResult): LiveResult {
  const arr = <T,>(a: T[] | null | undefined): T[] => (Array.isArray(a) ? a : []);
  return {
    ...r,
    warnings: arr(r.warnings),
    series: arr(r.series).map((s) => ({
      ...s,
      times: arr(s.times),
      rps: arr(s.rps),
      err_4xx_rps: arr(s.err_4xx_rps),
      err_5xx_rps: arr(s.err_5xx_rps),
      p50_ms: s.p50_ms == null ? s.p50_ms : arr(s.p50_ms),
      p95_ms: s.p95_ms == null ? s.p95_ms : arr(s.p95_ms),
      p99_ms: s.p99_ms == null ? s.p99_ms : arr(s.p99_ms),
      notes: arr(s.notes),
      services: arr(s.services),
      links: arr(s.links),
    })),
  };
}
