import { describe, expect, it } from "vitest";
import { buildComparison, deltaPct, verdict } from "./metricsCompare";
import type { ReportResult, ReportRow } from "./metricsReport";

const row = (workload: string, o: { requests?: number; max_rps?: number; p99?: number; e5?: number; e4?: number; cpu?: number } = {}): ReportRow =>
  ({
    cluster: "c1", namespace: "ns", workload,
    traffic: { source: "dynatrace", requests: o.requests ?? 3600, max_rps: o.max_rps ?? 10, p50_peak_ms: 1, p95_peak_ms: 2, p99_peak_ms: o.p99 ?? 200, err_4xx: o.e4 ?? 0, err_5xx: o.e5 ?? 0, services: [], peak_approx: false, resolution: "1m" },
    usage: { source: "prometheus", step: "1m", cpu_req_pct: { max: o.cpu ?? 50, min: 0, avg: 0 }, cpu_lim_pct: null, mem_req_pct: null, mem_lim_pct: null },
    flags: {},
  }) as ReportRow;
const res = (rows: ReportRow[], hours = 1, over: Partial<ReportResult> = {}): ReportResult =>
  ({ window: { start: "2026-11-11T17:00:00Z", end: new Date(Date.parse("2026-11-11T17:00:00Z") + hours * 3600e3).toISOString() }, percentiles: [99], plan: { dt_resolution: "1m", p99_resolution: "5m", peak_approx: false }, rows, generated_at: "", ...over }) as ReportResult;

describe("comparação entre períodos", () => {
  it("delta em % e sem base quando o evento é zero", () => {
    expect(deltaPct(150, 100)).toBe(50);
    expect(deltaPct(50, 100)).toBe(-50);
    expect(deltaPct(5, 0)).toBeNull();
    expect(deltaPct(null, 10)).toBeNull();
  });

  it("compara taxas e picos, não totais: 2 h com o dobro de requisições não é aumento de RPS", () => {
    const atual = res([row("hub", { requests: 7200, max_rps: 20, p99: 300, e5: 72 })], 2);
    const evento = res([row("hub", { requests: 3600, max_rps: 20, p99: 200, e5: 0 })], 1);
    const c = buildComparison(atual, evento);
    const m = Object.fromEntries(c.rows[0].metrics.map((x) => [x.key, x]));
    expect(m.avg_rps.a).toBe(1); // 7200 / 7200 s
    expect(m.avg_rps.b).toBe(1); // 3600 / 3600 s
    expect(m.avg_rps.deltaPct).toBe(0);
    expect(m.lat.deltaPct).toBe(50);
    expect(m.err5.a).toBe(1); // 72 / 7200 = 1%
    expect(m.err5.b).toBe(0);
    expect(m.err5.deltaPct).toBeNull();
    expect(c.notes.some((n) => n.includes("durações diferentes"))).toBe(true);
  });

  it("marca aplicações que existem só de um lado", () => {
    const c = buildComparison(res([row("hub"), row("so-hoje")]), res([row("hub"), row("so-evento")]));
    expect(Object.fromEntries(c.rows.map((r) => [r.workload, r.presence]))).toEqual({ hub: "ambos", "so-hoje": "so-atual", "so-evento": "so-evento" });
    const soHoje = c.rows.find((r) => r.workload === "so-hoje")!;
    expect(soHoje.metrics.every((m) => m.b === null && m.deltaPct === null)).toBe(true);
  });

  it("usa o maior percentil coletado nos dois", () => {
    const a = res([row("hub")], 1, { percentiles: [50, 95, 99] });
    const b = res([row("hub")], 1, { percentiles: [50, 95] });
    expect(buildComparison(a, b).percentile).toBe(95);
  });

  it("veredito: latência subindo é pior; tráfego é só informativo; ruído até 5% é igual", () => {
    const base = { key: "k", label: "l", kind: "ms" as const, a: 120, b: 100, deltaPct: 20, worseWhenUp: true };
    expect(verdict(base)).toBe("pior");
    expect(verdict({ ...base, a: 80, deltaPct: -20 })).toBe("melhor");
    expect(verdict({ ...base, a: 103, deltaPct: 3 })).toBe("igual");
    expect(verdict({ ...base, worseWhenUp: false })).toBe("igual");
    expect(verdict({ ...base, b: 0, a: 5, deltaPct: null })).toBe("sem-base");
  });
});

import { fmtCompareValue, fmtDelta } from "./metricsCompare";
describe("formatação da comparação", () => {
  it("valores com unidade e variação com sinal", () => {
    expect(fmtCompareValue("pct", null)).toBe("—");
    expect(fmtCompareValue("pct", 0.0123)).toBe("0,012%");
    expect(fmtCompareValue("pct", 12.34)).toBe("12,3%");
    expect(fmtCompareValue("ms", 1500)).toBe("1.500s");
    expect(fmtDelta(20.4)).toBe("+20%");
    expect(fmtDelta(-5.2)).toBe("-5%");
    expect(fmtDelta(0)).toBe("0%");
    expect(fmtDelta(null)).toBe("—");
  });
});
