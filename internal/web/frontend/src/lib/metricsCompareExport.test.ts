import { describe, expect, it } from "vitest";
import { buildComparison } from "./metricsCompare";
import { buildComparisonCSV, buildComparisonMarkdown } from "./metricsCompareExport";
import type { ReportResult, ReportRow } from "./metricsReport";

const row = (cluster: string, workload: string, o: { requests?: number; max_rps?: number; p99?: number; avg?: number; e5?: number; cpu?: number } = {}): ReportRow =>
  ({
    cluster, namespace: "ns", workload,
    traffic: { source: "dynatrace", requests: o.requests ?? 3600, max_rps: o.max_rps ?? 10, avg_ms: o.avg ?? 100, p50_peak_ms: 1, p95_peak_ms: 2, p99_peak_ms: o.p99 ?? 200, err_4xx: 0, err_5xx: o.e5 ?? 0, services: [], peak_approx: false, resolution: "1m" },
    usage: { source: "prometheus", step: "1m", cpu_req_pct: { max: o.cpu ?? 50, min: 0, avg: 0 }, cpu_lim_pct: null, mem_req_pct: null, mem_lim_pct: null },
    flags: {},
  }) as ReportRow;
const res = (rows: ReportRow[], start: string, hours = 1): ReportResult =>
  ({ window: { start, end: new Date(Date.parse(start) + hours * 3600e3).toISOString() }, percentiles: [99], plan: { dt_resolution: "1m", p99_resolution: "5m", peak_approx: false }, rows, generated_at: "" }) as ReportResult;

const atual = res([row("c2", "z-api", { p99: 300, avg: 150, e5: 36 }), row("c1", "frete-hub", { p99: 600, max_rps: 20 }), row("c1", "so-hoje")], "2026-11-13T17:00:00Z");
const evento = res([row("c1", "frete-hub", { p99: 200, max_rps: 20 }), row("c2", "z-api", { p99: 300, avg: 100 }), row("c1", "so-evento")], "2026-11-11T17:00:00Z");
const cmp = buildComparison(atual, evento);

describe("exportar a comparação em Markdown", () => {
  const md = buildComparisonMarkdown(cmp, atual, evento, "Stress test 11/11", "2026-11-13T18:00:00Z");
  it("cabeçalho com as duas janelas, o evento e a latência comparada", () => {
    expect(md.split("\n")[0]).toBe("# Comparação: atual × Stress test 11/11");
    expect(md).toContain("- **Atual:** 13/11/2026 14:00 → 13/11/2026 15:00 (America/Sao_Paulo)");
    expect(md).toContain('- **Evento "Stress test 11/11":** 11/11/2026 14:00 → 11/11/2026 15:00');
    expect(md).toContain("- **Latência comparada:** P99 (pico)");
    expect(md).toContain("atual / evento (variação)");
  });
  it("uma tabela por cluster, em ordem, com a variação e o veredito", () => {
    expect(md.indexOf("## c1")).toBeLessThan(md.indexOf("## c2"));
    const hub = md.split("\n").find((l) => l.includes("frete-hub"))!;
    expect(hub).toContain("| ambos |");
    expect(hub).toContain("600 ms / 200 ms (+200%, pior)"); // a latência triplicou
    expect(hub).toContain("20,0 / 20,0 (0%)"); // pico de RPS igual, sem veredito
    const z = md.split("\n").find((l) => l.includes("z-api"))!;
    expect(z).toContain("150 ms / 100 ms (+50%, pior)"); // response time
  });
  it("marca o que existe só de um lado e usa — onde não há dado", () => {
    expect(md.split("\n").find((l) => l.includes("so-hoje"))).toContain("só no atual");
    expect(md.split("\n").find((l) => l.includes("so-evento"))).toContain("só no evento");
    expect(md.split("\n").find((l) => l.includes("so-hoje"))).toContain("— (—)"); // sem evento para comparar
  });
  it("sem aplicação comparada diz isso", () => {
    const vazio = buildComparisonMarkdown(buildComparison(res([], "2026-11-13T17:00:00Z"), res([], "2026-11-11T17:00:00Z")), atual, evento, "x", "2026-11-13T18:00:00Z");
    expect(vazio).toContain("_Nenhuma aplicação foi comparada._");
  });
  it("o nome da aplicação com barra vertical não quebra a tabela", () => {
    const c = buildComparison(res([row("c", "a|b")], "2026-11-13T17:00:00Z"), res([row("c", "a|b")], "2026-11-11T17:00:00Z"));
    expect(buildComparisonMarkdown(c, atual, evento, "x", "2026-11-13T18:00:00Z")).toContain("a\\|b");
  });
});

describe("exportar a comparação em CSV", () => {
  const csv = buildComparisonCSV(cmp, "Stress test 11/11");
  const lines = csv.replace(/^\uFEFF/, "").split("\r\n").filter(Boolean);
  it("começa com BOM, usa ; e termina com quebra de linha", () => {
    expect(csv.charCodeAt(0)).toBe(0xfeff);
    expect(csv.endsWith("\r\n")).toBe(true);
    expect(lines[0].startsWith('"evento";"cluster";"namespace";"deployment";"presenca";"rps_medio_atual";"rps_medio_evento";"rps_medio_variacao_pct"')).toBe(true);
    expect(lines[0]).toContain('"response_time_medio_ms_variacao_pct"');
  });
  it("um registro por aplicação, ordenado, todos com o mesmo número de colunas", () => {
    expect(lines).toHaveLength(1 + 4);
    expect(new Set(lines.map((l) => l.split(";").length)).size).toBe(1);
    expect(lines[1]).toContain('"c1";"ns";"frete-hub"');
    expect(lines[lines.length - 1]).toContain('"c2";"ns";"z-api"');
  });
  it("números crus (ponto decimal, ms), variação em % e vazio quando não há dado", () => {
    const hub = lines.find((l) => l.includes('"frete-hub"'))!.split(";");
    const i = lines[0].split(";").indexOf('"latencia_pico_ms_atual"');
    expect(hub.slice(i, i + 3)).toEqual(["600", "200", "200"]); // atual, evento, +200%
    const hoje = lines.find((l) => l.includes('"so-hoje"'))!.split(";");
    expect(hoje.slice(i, i + 3)).toEqual(["200", "", ""]); // sem evento: sem variação
  });
  it("protege o nome do evento contra fórmula", () => {
    expect(buildComparisonCSV(cmp, "=cmd()")).toContain("\"'=cmd()\"");
  });
});
