// Visão do Tempo real: modelo de linha (tabela e cartões), ordenação, filtro por situação e os números da
// faixa de resumo. Lógica pura (testada em metricsLiveView.test.ts); nenhuma chamada de rede nem DOM.
import { assessSeries, lastValue, projectedPercentile, type Level, type LiveSeries } from "./metricsLive";

export const LEVEL_RANK: Record<Level, number> = { info: 0, ok: 1, warn: 2, red: 3 };

export interface LiveRowModel {
  s: LiveSeries;
  key: string;
  level: Level;
  reasons: string[];
  rps: number | null;
  avg: number | null;
  /** Latência do maior percentil marcado (ms). */
  lat: number | null;
  latPercentile: 50 | 95 | 99;
  /** Cada percentil, no valor mais recente (ms); nulo = não coletado ou sem dado. */
  p50: number | null;
  p95: number | null;
  p99: number | null;
  e5Rate: number | null;
  e5Total: number | null;
  e4Rate: number | null;
  e4Total: number | null;
  repAvail: number | null;
  repDesired: number | null;
  hpaMax: number | null;
  /** Réplicas desejadas no teto do HPA. */
  atCeiling: boolean;
  cpuLim: number | null;
  memLim: number | null;
  restarts: number | null;
}

export const rowKey = (s: { cluster: string; namespace: string; workload: string }) => `${s.cluster}|${s.namespace}|${s.workload}`;

export function rowModel(s: LiveSeries, percentiles: number[]): LiveRowModel {
  const p = projectedPercentile(percentiles);
  const a = assessSeries(s, percentiles);
  const r = s.resources;
  return {
    s, key: rowKey(s), level: a.level, reasons: a.reasons,
    rps: lastValue(s.rps), avg: lastValue(s.avg_ms),
    lat: lastValue(p === 99 ? s.p99_ms : p === 95 ? s.p95_ms : s.p50_ms), latPercentile: p,
    p50: lastValue(s.p50_ms), p95: lastValue(s.p95_ms), p99: lastValue(s.p99_ms),
    e5Rate: lastValue(s.err_5xx_rps), e5Total: s.err_5xx_total ?? null,
    e4Rate: lastValue(s.err_4xx_rps), e4Total: s.err_4xx_total ?? null,
    repAvail: r?.replicas_available ?? null, repDesired: r?.replicas_desired ?? null, hpaMax: r?.hpa_max ?? null,
    atCeiling: r?.hpa_max != null && r?.replicas_desired != null && r.hpa_max > 0 && r.replicas_desired >= r.hpa_max,
    cpuLim: r?.cpu_lim_pct ?? null, memLim: r?.mem_lim_pct ?? null, restarts: r?.restarts_15m ?? null,
  };
}

// ── ordenação ─────────────────────────────────────────────────────────────────────────────────────

export const SORT_KEYS = ["gravidade", "nome", "rps", "avg", "lat", "p50", "p95", "p99", "err5", "err4", "replicas", "cpu", "mem"] as const;
export type SortKey = (typeof SORT_KEYS)[number];
export interface SortState {
  key: SortKey;
  desc: boolean;
}
export const DEFAULT_SORT: SortState = { key: "gravidade", desc: true };

const numericOf = (r: LiveRowModel, k: Exclude<SortKey, "gravidade" | "nome">): number | null =>
  ({ rps: r.rps, avg: r.avg, lat: r.lat, p50: r.p50, p95: r.p95, p99: r.p99, err5: r.e5Total, err4: r.e4Total, replicas: r.repAvail, cpu: r.cpuLim, mem: r.memLim })[k];

const byName = (a: LiveRowModel, b: LiveRowModel) =>
  a.s.workload.localeCompare(b.s.workload, "pt-BR") || a.s.namespace.localeCompare(b.s.namespace, "pt-BR");

/** Cópia ordenada. Sem dado vai sempre para o fim, qualquer que seja o sentido; empate cai no nome. */
export function sortRows(rows: LiveRowModel[], sort: SortState): LiveRowModel[] {
  const dir = sort.desc ? -1 : 1;
  const out = [...rows];
  if (sort.key === "nome") return out.sort((a, b) => dir * byName(a, b));
  if (sort.key === "gravidade") return out.sort((a, b) => dir * (LEVEL_RANK[a.level] - LEVEL_RANK[b.level]) || byName(a, b));
  const k = sort.key;
  return out.sort((a, b) => {
    const x = numericOf(a, k), y = numericOf(b, k);
    if (x == null && y == null) return byName(a, b);
    if (x == null) return 1;
    if (y == null) return -1;
    return dir * (x - y) || byName(a, b);
  });
}

export function parseSort(raw: string | null): SortState {
  try {
    const v = raw ? JSON.parse(raw) : null;
    if (v && (SORT_KEYS as readonly string[]).includes(v.key) && typeof v.desc === "boolean") return { key: v.key, desc: v.desc };
  } catch {
    /* valor gravado inválido: volta ao padrão */
  }
  return DEFAULT_SORT;
}

// ── filtro ────────────────────────────────────────────────────────────────────────────────────────

export interface ViewFilter {
  /** Só ERRO e ALERTA. */
  onlyProblems: boolean;
  /** Um nível específico (clique num número da faixa de resumo). */
  level: Level | null;
}

export function filterRows(rows: LiveRowModel[], f: ViewFilter): LiveRowModel[] {
  return rows.filter((r) => (!f.level || r.level === f.level) && (!f.onlyProblems || r.level === "red" || r.level === "warn"));
}

// ── agrupamento por cluster (mantém a ordem recebida dentro de cada cluster) ─────────────────────────

export function groupRows(rows: LiveRowModel[]): { cluster: string; rows: LiveRowModel[] }[] {
  const map = new Map<string, LiveRowModel[]>();
  for (const r of rows) map.set(r.s.cluster, [...(map.get(r.s.cluster) ?? []), r]);
  return [...map.entries()].sort(([a], [b]) => a.localeCompare(b, "pt-BR")).map(([cluster, rs]) => ({ cluster, rows: rs }));
}

// ── faixa de resumo ───────────────────────────────────────────────────────────────────────────────

export interface Kpis {
  counts: Record<Level, number>;
  total: number;
  /** Soma do RPS mais recente das aplicações (nulo sem nenhum dado). */
  rps: number | null;
  /** A aplicação com a pior latência (do maior percentil marcado). */
  worst: { name: string; ms: number; percentile: 50 | 95 | 99 } | null;
  e5Rate: number | null;
  e5Total: number | null;
}

export function buildKpis(rows: LiveRowModel[]): Kpis {
  const counts: Record<Level, number> = { red: 0, warn: 0, ok: 0, info: 0 };
  let rps: number | null = null, e5Rate: number | null = null, e5Total: number | null = null;
  let worst: Kpis["worst"] = null;
  for (const r of rows) {
    counts[r.level] += 1;
    if (r.rps != null) rps = (rps ?? 0) + r.rps;
    if (r.e5Rate != null) e5Rate = (e5Rate ?? 0) + r.e5Rate;
    if (r.e5Total != null) e5Total = (e5Total ?? 0) + r.e5Total;
    if (r.lat != null && (!worst || r.lat > worst.ms)) worst = { name: r.s.workload, ms: r.lat, percentile: r.latPercentile };
  }
  return { counts, total: rows.length, rps, worst, e5Rate, e5Total };
}

// ── visão: tabela ou cartões ──────────────────────────────────────────────────────────────────────

export type ViewMode = "auto" | "table" | "cards";
export const VIEW_KEY = "metrics_live_view";
export const SORT_KEY = "metrics_live_sort";
/** A partir de quantas aplicações a tabela é o padrão: acima disso os cartões obrigam a rolar demais. */
export const TABLE_FROM = 7;

export function parseViewMode(raw: string | null): ViewMode {
  try {
    const v = raw ? JSON.parse(raw) : null;
    return v === "table" || v === "cards" ? v : "auto";
  } catch {
    return "auto";
  }
}

export function effectiveView(mode: ViewMode, count: number): "table" | "cards" {
  return mode === "auto" ? (count >= TABLE_FROM ? "table" : "cards") : mode;
}
