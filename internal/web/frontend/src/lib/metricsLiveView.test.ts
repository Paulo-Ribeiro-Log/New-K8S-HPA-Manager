import { describe, expect, it } from "vitest";
import { DEFAULT_SORT, TABLE_FROM, buildKpis, effectiveView, filterRows, groupRows, parseSort, parseViewMode, rowModel, sortRows, type LiveRowModel } from "./metricsLiveView";
import type { LiveSeries } from "./metricsLive";

const mk = (workload: string, o: Partial<LiveSeries> & { res?: object } = {}): LiveSeries => {
  const { res, ...rest } = o;
  return ({
    cluster: "c1", namespace: "ns", workload, times: [1], rps: [10], err_4xx_rps: [0], err_5xx_rps: [0], err_4xx_total: 0, err_5xx_total: 0, p99_ms: [100], avg_ms: [50],
    ...(res ? { resources: { replicas_available: 10, replicas_desired: 10, restarts_15m: 0, cpu_lim_pct: 20, mem_lim_pct: 30, pods: 10, ...res } } : {}), ...rest,
  }) as unknown as LiveSeries;
};
const rows = (...s: LiveSeries[]): LiveRowModel[] => s.map((x) => rowModel(x, [99]));

describe("rowModel", () => {
  it("extrai os valores mais recentes e marca o teto do HPA", () => {
    const r = rowModel(mk("a", { rps: [1, 20], p99_ms: [100, 300], res: { replicas_desired: 72, hpa_max: 72, replicas_available: 70 } }), [99]);
    expect(r.rps).toBe(20);
    expect(r.lat).toBe(300);
    expect(r.latPercentile).toBe(99);
    expect(r.atCeiling).toBe(true);
    expect(r.repAvail).toBe(70);
    expect(rowModel(mk("b"), [99]).atCeiling).toBe(false);
    expect(rowModel(mk("b"), [99]).repAvail).toBeNull();
  });
});

describe("sortRows", () => {
  const a = mk("a", { rps: [5] }), b = mk("b", { err_5xx_rps: [1], err_5xx_total: 10 }), c = mk("c", { p99_ms: [700] }), d = mk("d", { rps: [] as any, avg_ms: null });
  it("gravidade: o pior primeiro, empate por nome", () => {
    const r = sortRows(rows(a, c, b), DEFAULT_SORT).map((x) => x.s.workload);
    expect(r).toEqual(["b", "c", "a"]); // b=ERRO (5xx), c=ALERTA (P99 0,7 s), a=OK
    expect(sortRows(rows(a, c, b), { key: "gravidade", desc: false }).map((x) => x.s.workload)).toEqual(["a", "c", "b"]);
  });
  it("numérico: sem dado vai sempre para o fim, nos dois sentidos", () => {
    const rs = rows(a, mk("x", { rps: [50] }), d);
    expect(sortRows(rs, { key: "rps", desc: true }).map((x) => x.s.workload)).toEqual(["x", "a", "d"]);
    expect(sortRows(rs, { key: "rps", desc: false }).map((x) => x.s.workload)).toEqual(["a", "x", "d"]);
  });
  it("nome, nos dois sentidos, e não altera a lista original", () => {
    const rs = rows(mk("b"), mk("a"), mk("c"));
    expect(sortRows(rs, { key: "nome", desc: false }).map((x) => x.s.workload)).toEqual(["a", "b", "c"]);
    expect(sortRows(rs, { key: "nome", desc: true }).map((x) => x.s.workload)).toEqual(["c", "b", "a"]);
    expect(rs.map((x) => x.s.workload)).toEqual(["b", "a", "c"]);
  });
  it("parseSort aceita só valor válido e volta ao padrão", () => {
    expect(parseSort(JSON.stringify({ key: "rps", desc: false }))).toEqual({ key: "rps", desc: false });
    expect(parseSort(JSON.stringify({ key: "lixo", desc: true }))).toEqual(DEFAULT_SORT);
    expect(parseSort("não é json")).toEqual(DEFAULT_SORT);
    expect(parseSort(null)).toEqual(DEFAULT_SORT);
  });
});

describe("filterRows e groupRows", () => {
  const rs = rows(mk("ok"), mk("alerta", { p99_ms: [700] }), mk("erro", { err_5xx_rps: [2] }), mk("vazio", { rps: [], err_4xx_rps: [], err_5xx_rps: [], p99_ms: null, avg_ms: null }));
  it("só problemas = ERRO e ALERTA", () => {
    expect(filterRows(rs, { onlyProblems: true, level: null }).map((r) => r.s.workload).sort()).toEqual(["alerta", "erro"]);
    expect(filterRows(rs, { onlyProblems: false, level: null })).toHaveLength(4);
  });
  it("nível específico, e os dois filtros juntos", () => {
    expect(filterRows(rs, { onlyProblems: false, level: "ok" }).map((r) => r.s.workload)).toEqual(["ok"]);
    expect(filterRows(rs, { onlyProblems: false, level: "info" }).map((r) => r.s.workload)).toEqual(["vazio"]);
    expect(filterRows(rs, { onlyProblems: true, level: "ok" })).toEqual([]);
  });
  it("agrupa por cluster em ordem alfabética, mantendo a ordem de dentro", () => {
    const g = groupRows(rows(mk("z"), mk("a", { cluster: "b" } as any), mk("m", { cluster: "a" } as any), mk("y")));
    expect(g.map((x) => x.cluster)).toEqual(["a", "b", "c1"]);
    expect(g[2].rows.map((r) => r.s.workload)).toEqual(["z", "y"]);
  });
});

describe("buildKpis", () => {
  it("conta por nível, soma RPS e 5xx e acha a pior latência", () => {
    const k = buildKpis(rows(mk("a", { rps: [10], p99_ms: [200] }), mk("b", { rps: [30], p99_ms: [900], err_5xx_rps: [1], err_5xx_total: 60 }), mk("c", { rps: [] as any, p99_ms: null, err_4xx_rps: [], err_5xx_rps: [] })));
    expect(k.counts).toEqual({ red: 1, warn: 0, ok: 1, info: 1 }); // b=ERRO (5xx), a=OK (200 ms), c=sem dado
    expect(k.total).toBe(3);
    expect(k.rps).toBe(40);
    expect(k.worst).toEqual({ name: "b", ms: 900, percentile: 99 });
    expect(k.e5Rate).toBe(1);
    expect(k.e5Total).toBe(60);
  });
  it("sem nenhum dado, os totais são nulos (nunca zero)", () => {
    const k = buildKpis(rows(mk("c", { rps: [] as any, p99_ms: null, err_4xx_rps: [], err_5xx_rps: [], err_5xx_total: null as any })));
    expect(k.rps).toBeNull();
    expect(k.worst).toBeNull();
    expect(k.e5Rate).toBeNull();
    expect(k.e5Total).toBeNull();
    expect(buildKpis([]).total).toBe(0);
  });
});

describe("visão tabela ou cartões", () => {
  it("automático: tabela a partir de 7 aplicações; a escolha explícita vale sempre", () => {
    expect(effectiveView("auto", TABLE_FROM - 1)).toBe("cards");
    expect(effectiveView("auto", TABLE_FROM)).toBe("table");
    expect(effectiveView("cards", 50)).toBe("cards");
    expect(effectiveView("table", 1)).toBe("table");
  });
  it("parse devolve auto para qualquer valor inválido", () => {
    expect(parseViewMode(JSON.stringify("table"))).toBe("table");
    expect(parseViewMode(JSON.stringify("cards"))).toBe("cards");
    expect(parseViewMode(JSON.stringify("x"))).toBe("auto");
    expect(parseViewMode("lixo")).toBe("auto");
    expect(parseViewMode(null)).toBe("auto");
  });
});

describe("um valor por percentil (tabela com colunas P50, P95 e P99)", () => {
  it("rowModel guarda cada percentil e o de ordenação do maior marcado", () => {
    const r = rowModel(mk("a", { p50_ms: [10], p95_ms: [120], p99_ms: [900] } as any), [50, 95, 99]);
    expect([r.p50, r.p95, r.p99]).toEqual([10, 120, 900]);
    expect(r.lat).toBe(900);
    const so95 = rowModel(mk("b", { p50_ms: null, p95_ms: [120], p99_ms: null } as any), [95]);
    expect([so95.p50, so95.p95, so95.p99]).toEqual([null, 120, null]);
    expect(so95.lat).toBe(120);
  });
  it("ordena por cada percentil, com sem dado no fim", () => {
    const rs = [mk("a", { p95_ms: [300] } as any), mk("b", { p95_ms: [100] } as any), mk("c", { p95_ms: null } as any)].map((x) => rowModel(x, [95]));
    expect(sortRows(rs, { key: "p95", desc: true }).map((r) => r.s.workload)).toEqual(["a", "b", "c"]);
    expect(sortRows(rs, { key: "p95", desc: false }).map((r) => r.s.workload)).toEqual(["b", "a", "c"]);
    expect(parseSort(JSON.stringify({ key: "p50", desc: true }))).toEqual({ key: "p50", desc: true });
  });
});
