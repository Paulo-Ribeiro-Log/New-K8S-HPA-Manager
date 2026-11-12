// Comparação entre dois relatórios do mesmo conjunto de aplicações (atual × um evento salvo). Compara só o
// que NÃO depende do tamanho da janela: RPS médio, pico de RPS, pico de latência, % de erros sobre as
// requisições e pico de CPU/memória. Totais brutos (requisições, erros) ficam de fora de propósito: uma
// janela de 2 h tem o dobro das requisições de uma de 1 h sem que nada tenha piorado.
import { fmtLatency, fmtRps, percentileValue, type ReportResult, type ReportRow } from "./metricsReport";

export type MetricKind = "rps" | "ms" | "pct";

export interface CompareMetric {
  key: string;
  label: string;
  kind: MetricKind;
  a: number | null; // atual
  b: number | null; // evento (base da comparação)
  /** (a − b) / b × 100; nulo quando b é zero ou falta dado. */
  deltaPct: number | null;
  /** Subir é pior (latência, erros, CPU, memória) ou só informativo (tráfego). */
  worseWhenUp: boolean;
}

export interface CompareRow {
  cluster: string;
  namespace: string;
  workload: string;
  /** "ambos" ou só em um dos relatórios (o evento pode ter cobrido outras aplicações). */
  presence: "ambos" | "so-atual" | "so-evento";
  metrics: CompareMetric[];
}

export interface Comparison {
  rows: CompareRow[];
  /** Latência comparada: o maior percentil coletado nos dois relatórios. */
  percentile: number;
  notes: string[];
}

const seconds = (r: ReportResult) => Math.max(1, (Date.parse(r.window.end) - Date.parse(r.window.start)) / 1000);
const key = (r: { cluster: string; namespace: string; workload: string }) => `${r.cluster}|${r.namespace}|${r.workload}`;

/** Variação em %; nulo sem base (b zero/ausente) ou sem valor atual. */
export function deltaPct(a: number | null, b: number | null): number | null {
  if (a == null || b == null || b === 0) return null;
  return ((a - b) / b) * 100;
}

const ratio = (part: number | null | undefined, total: number | null | undefined): number | null =>
  part == null || total == null || total <= 0 ? null : (part / total) * 100;

function metricsOf(row: ReportRow | undefined, secs: number, pct: number) {
  const t = row?.traffic;
  const u = row?.usage;
  return {
    avg_rps: t?.requests == null ? null : t.requests / secs,
    max_rps: t?.max_rps ?? null,
    avg: row ? (t?.avg_ms ?? null) : null,
    lat: row ? percentileValue(t ?? null, pct) : null,
    err5: ratio(t?.err_5xx, t?.requests),
    err4: ratio(t?.err_4xx, t?.requests),
    cpu: u?.cpu_req_pct?.max ?? null,
    mem: u?.mem_req_pct?.max ?? null,
  };
}

/** Compara A (atual) com B (evento). Aplicações só de um lado aparecem marcadas, com o outro lado vazio. */
export function buildComparison(a: ReportResult, b: ReportResult): Comparison {
  const common = (a.percentiles ?? []).filter((p) => (b.percentiles ?? []).includes(p));
  const pct = common.length ? Math.max(...common) : 99;
  const notes: string[] = [];
  const sa = seconds(a), sb = seconds(b);
  if (Math.abs(sa - sb) / Math.max(sa, sb) > 0.2) notes.push("As janelas têm durações diferentes: por isso só taxas e picos são comparados, não os totais de requisições e erros.");
  if (a.plan?.peak_approx || b.plan?.peak_approx) notes.push("Pelo menos uma janela é antiga: o pico de RPS e de latência é aproximado (resolução menor).");

  const ra = new Map(a.rows.map((r) => [key(r), r]));
  const rb = new Map(b.rows.map((r) => [key(r), r]));
  const keys = [...new Set([...ra.keys(), ...rb.keys()])].sort((x, y) => x.localeCompare(y, "pt-BR"));
  const rows = keys.map((k): CompareRow => {
    const x = ra.get(k), y = rb.get(k);
    const ref = (x ?? y)!;
    const ma = metricsOf(x, sa, pct), mb = metricsOf(y, sb, pct);
    const m = (key: string, label: string, kind: MetricKind, va: number | null, vb: number | null, worseWhenUp: boolean): CompareMetric => ({ key, label, kind, a: va, b: vb, deltaPct: deltaPct(va, vb), worseWhenUp });
    return {
      cluster: ref.cluster, namespace: ref.namespace, workload: ref.workload,
      presence: x && y ? "ambos" : x ? "so-atual" : "so-evento",
      metrics: [
        m("avg_rps", "RPS médio", "rps", ma.avg_rps, mb.avg_rps, false),
        m("max_rps", "Pico de RPS", "rps", ma.max_rps, mb.max_rps, false),
        m("avg", "Resposta média", "ms", ma.avg, mb.avg, true),
        m("lat", `Latência P${pct} (pico)`, "ms", ma.lat, mb.lat, true),
        m("err5", "Erros 5xx (% das requisições)", "pct", ma.err5, mb.err5, true),
        m("err4", "Erros 4xx (% das requisições)", "pct", ma.err4, mb.err4, true),
        m("cpu", "CPU % do request (máx)", "pct", ma.cpu, mb.cpu, true),
        m("mem", "Mem % do request (máx)", "pct", ma.mem, mb.mem, true),
      ],
    };
  });
  return { rows, percentile: pct, notes };
}

/** Valor de uma métrica da comparação, com a unidade; sem dado = "—". */
export function fmtCompareValue(kind: MetricKind, v: number | null): string {
  if (v == null) return "—";
  if (kind === "rps") return fmtRps(v);
  if (kind === "ms") return fmtLatency(v);
  return `${v.toLocaleString("pt-BR", { maximumFractionDigits: v < 1 ? 3 : 1 })}%`;
}

/** "+20%" / "-5%" / "—". */
export function fmtDelta(d: number | null): string {
  if (d == null) return "—";
  const r = Math.round(d);
  return `${r > 0 ? "+" : ""}${r}%`;
}

/** "pior", "melhor" ou "igual" para a variação de uma métrica (limite de 5% para não chamar ruído de mudança). */
export function verdict(m: CompareMetric, tolerancePct = 5): "pior" | "melhor" | "igual" | "sem-base" {
  if (m.deltaPct == null) return m.a === 0 && m.b === 0 ? "igual" : "sem-base";
  if (Math.abs(m.deltaPct) <= tolerancePct) return "igual";
  const up = m.deltaPct > 0;
  if (!m.worseWhenUp) return "igual"; // tráfego: informativo, sem veredito
  return up ? "pior" : "melhor";
}
