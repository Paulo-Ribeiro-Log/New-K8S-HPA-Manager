import { describe, expect, it } from "vitest";
import {
  DEFAULT_PERCENTILES, allItems, fmtNodeUsage, fmtPools, fmtThrottle, parseNodeContext, groupRows, fmtInt, fmtLatency, fmtPct, fmtPctStats, fmtRps, isoToZonedLocal, newTarget, parseSets, parseTargets, pickKey, presetWindow,
  parsePercentiles, percentileValue, togglePercentile, setTargetCluster, setTargetNamespaces, setTargetPicked, targetItems, targetSummary, togglePicked, toggleTargetNamespace, upsertSet,
  zonedLocalToISO, type ReportRow, type ReportTraffic, type Target,
} from "./metricsReport";

const target = (cluster: string, namespaces: string[], picked: string[]): Target => ({ ...newTarget(cluster), namespaces, picked });

describe("alvos", () => {
  it("cada alvo tem id próprio e começa vazio", () => {
    const a = newTarget();
    const b = newTarget();
    expect(a.id).not.toBe(b.id);
    expect(a).toMatchObject({ cluster: "", namespaces: [], picked: [] });
    expect(targetItems(a)).toEqual([]);
  });

  it("só conta deployments de namespaces ainda escolhidos", () => {
    const t = target("c1", ["ns-a"], [pickKey("ns-a", "app"), pickKey("ns-b", "outro")]);
    expect(targetItems(t)).toEqual([{ cluster: "c1", namespace: "ns-a", workload: "app" }]);
    expect(targetItems({ ...t, cluster: "" })).toEqual([]);
  });

  it("trocar o cluster descarta namespaces e deployments do anterior", () => {
    const t = target("c1", ["ns-a"], [pickKey("ns-a", "app")]);
    expect(setTargetCluster(t, "c2")).toMatchObject({ cluster: "c2", namespaces: [], picked: [] });
    expect(setTargetCluster(t, "c1")).toBe(t); // mesmo cluster: nada muda
  });

  it("desmarcar um namespace tira os deployments dele e só os dele", () => {
    let t = target("c1", [], []);
    t = toggleTargetNamespace(toggleTargetNamespace(t, "ns-b", true), "ns-a", true);
    expect(t.namespaces).toEqual(["ns-a", "ns-b"]); // ordenado
    t = togglePicked(t, [pickKey("ns-a", "x"), pickKey("ns-b", "y")], true);
    t = toggleTargetNamespace(t, "ns-a", false);
    expect(t.namespaces).toEqual(["ns-b"]);
    expect(t.picked).toEqual(["ns-b/y"]);
    expect(toggleTargetNamespace(t, "ns-b", true)).toBe(t); // já marcado
  });

  it("marcar deployments ignora namespace não escolhido e não repete", () => {
    let t = target("c1", ["ns-a"], []);
    t = togglePicked(t, ["ns-a/x", "ns-z/y", "invalido", "ns-a/x"], true);
    expect(t.picked).toEqual(["ns-a/x"]);
    expect(togglePicked(t, ["ns-a/x"], false).picked).toEqual([]);
    expect(togglePicked(t, ["ns-a/nao-existe"], false).picked).toEqual(["ns-a/x"]);
  });

  it("aplicar a escolha de namespaces troca o conjunto e descarta os deployments dos que saíram", () => {
    const t = target("c1", ["ns-a", "ns-b"], ["ns-a/x", "ns-b/y"]);
    const r = setTargetNamespaces(t, ["ns-b", "ns-c", "ns-b"]);
    expect(r.namespaces).toEqual(["ns-b", "ns-c"]); // sem repetir, ordenado
    expect(r.picked).toEqual(["ns-b/y"]);
    expect(setTargetNamespaces(t, []).picked).toEqual([]);
    expect(t.namespaces).toEqual(["ns-a", "ns-b"]); // o alvo original não muda
  });

  it("aplicar a escolha de deployments substitui o conjunto e ignora o que não vale", () => {
    const t = target("c1", ["ns-a"], ["ns-a/velho"]);
    expect(setTargetPicked(t, ["ns-a/b", "ns-a/a", "ns-z/x", "sem-barra", "ns-a/b"]).picked).toEqual(["ns-a/a", "ns-a/b"]);
    expect(setTargetPicked(t, []).picked).toEqual([]);
  });

  it("junta os alvos sem repetir e ordena", () => {
    const a = target("c2", ["n"], ["n/z"]);
    const b = target("c1", ["n"], ["n/b", "n/a"]);
    const dup = target("c1", ["n"], ["n/a"]); // 2º alvo do mesmo cluster repete n/a
    expect(allItems([a, b, dup]).map((i) => `${i.cluster}/${i.namespace}/${i.workload}`)).toEqual(["c1/n/a", "c1/n/b", "c2/n/z"]);
  });

  it("resume o estado do alvo em uma linha", () => {
    expect(targetSummary(newTarget())).toBe("Escolha um cluster");
    expect(targetSummary(newTarget("c"))).toBe("Escolha os namespaces");
    expect(targetSummary(target("c", ["a"], []))).toBe("1 namespace, nenhum deployment marcado");
    expect(targetSummary(target("c", ["a"], ["a/x"]))).toBe("1 deployment em 1 namespace");
    expect(targetSummary(target("c", ["a", "b"], ["a/x", "b/y"]))).toBe("2 deployments em 2 namespaces");
  });

  it("lê os alvos gravados, ignora lixo e repara ids repetidos", () => {
    expect(parseTargets(null)).toEqual([]);
    expect(parseTargets("{nao json")).toEqual([]);
    expect(parseTargets('["c1|ns|app"]')).toEqual([]); // formato antigo
    const raw = JSON.stringify([
      { id: "x", cluster: "c1", namespaces: ["n"], picked: ["n/a"] },
      { id: "x", cluster: "c2", namespaces: [], picked: [] }, // id repetido
      { cluster: 1, namespaces: [], picked: [] },
      { cluster: "c3", namespaces: [1], picked: [] },
      null,
    ]);
    const out = parseTargets(raw);
    expect(out.map((t) => t.cluster)).toEqual(["c1", "c2"]);
    expect(new Set(out.map((t) => t.id)).size).toBe(2);
  });
});

describe("conjuntos salvos", () => {
  const x = target("c", ["n"], ["n/x"]);
  it("grava, substitui pelo nome e ignora nome ou alvos sem deployment", () => {
    let s = upsertSet([], "Frete", [x]);
    s = upsertSet(s, "Estoque", [target("c", ["n"], ["n/e"])]);
    s = upsertSet(s, "Frete", [target("c", ["n"], ["n/z"])]);
    expect(s.map((v) => [v.name, v.targets[0].picked[0]])).toEqual([["Estoque", "n/e"], ["Frete", "n/z"]]);
    expect(upsertSet(s, "  ", [x])).toBe(s);
    expect(upsertSet(s, "Novo", [newTarget("c")])).toBe(s); // alvo sem deployment não vale
  });
  it("lê só o que tem o formato certo", () => {
    const raw = JSON.stringify([{ name: "ok", targets: [x] }, { name: "vazio", targets: [newTarget("c")] }, { name: 1, targets: [x] }, { name: "x" }]);
    expect(parseSets(raw).map((s) => s.name)).toEqual(["ok"]);
    expect(parseSets("lixo")).toEqual([]);
    expect(parseSets('[{"name":"antigo","items":[{"cluster":"c","namespace":"n","workload":"w"}]}]')).toEqual([]); // formato antigo
  });
});

describe("janela em fuso", () => {
  it("converte a hora local de Brasília para RFC 3339 com offset", () => {
    expect(zonedLocalToISO("2026-10-01T23:00")).toBe("2026-10-01T23:00:00-03:00");
    expect(zonedLocalToISO("2026-10-02T04:30")).toBe("2026-10-02T04:30:00-03:00");
    // o instante é o certo: 23:00 em -03:00 = 02:00Z do dia seguinte
    expect(Date.parse(zonedLocalToISO("2026-10-01T23:00")!)).toBe(Date.UTC(2026, 9, 2, 2, 0));
  });
  it("respeita horário de verão de outro fuso (o offset depende da data)", () => {
    expect(zonedLocalToISO("2026-07-01T12:00", "America/New_York")).toBe("2026-07-01T12:00:00-04:00");
    expect(zonedLocalToISO("2026-01-15T12:00", "America/New_York")).toBe("2026-01-15T12:00:00-05:00");
  });
  it("recusa data inexistente e formato errado", () => {
    expect(zonedLocalToISO("2026-02-30T10:00")).toBeNull();
    expect(zonedLocalToISO("2026-13-01T10:00")).toBeNull();
    expect(zonedLocalToISO("2026-10-01 23:00")).toBeNull();
    expect(zonedLocalToISO("")).toBeNull();
  });
  it("volta do instante para a hora local do campo", () => {
    expect(isoToZonedLocal("2026-10-02T02:00:00Z")).toBe("2026-10-01T23:00");
    expect(isoToZonedLocal(Date.UTC(2026, 9, 2, 7, 30))).toBe("2026-10-02T04:30");
  });
  it("atalhos terminam no minuto cheio e voltam N horas", () => {
    const now = Date.UTC(2026, 10, 11, 12, 34, 56);
    const w = presetWindow("6h", now);
    expect(w.end).toBe("2026-11-11T12:34:00.000Z");
    expect(Date.parse(w.end) - Date.parse(w.start)).toBe(6 * 3600e3);
    expect(Date.parse(presetWindow("7d", now).end) - Date.parse(presetWindow("7d", now).start)).toBe(7 * 24 * 3600e3);
  });
});

describe("formatação", () => {
  it("sem dado nunca vira zero", () => {
    for (const f of [fmtInt, fmtRps, fmtLatency, fmtPct]) {
      expect(f(null)).toBe("sem dado");
      expect(f(undefined)).toBe("sem dado");
    }
    expect(fmtPctStats(null)).toBe("sem dado");
    expect(fmtInt(0)).toBe("0");
    expect(fmtLatency(0)).toBe("0.000s");
  });
  it("segue o formato do relatório de referência", () => {
    expect(fmtLatency(1769)).toBe("1.769s");
    expect(fmtLatency(26)).toBe("0.026s");
    expect(fmtRps(740)).toBe("740,0");
    expect(fmtPct(9.04)).toBe("9.0%");
    expect(fmtPctStats({ max: 9, min: 1.9, avg: 6.7 })).toBe("9.0% (Min: 1.9%, Avg: 6.7%)");
    expect(fmtInt(1170869)).toBe((1170869).toLocaleString("pt-BR"));
  });
});

describe("groupRows", () => {
  const row = (cluster: string, namespace: string, workload: string, flags: ReportRow["flags"] = {}, errors?: string[]): ReportRow => ({
    cluster, namespace, workload, traffic: null, usage: null, flags, errors,
  });
  const rows = [
    row("c2", "ns-a", "z"),
    row("c1", "ns-b", "b", { p99: "red", err_4xx: "warn" }),
    row("c1", "ns-a", "y", { err_4xx: "warn" }),
    row("c1", "ns-a", "x", {}, ["Dynatrace: sem process group"]),
    row("c1", "ns-b", "a", { err_5xx: "red" }),
  ];

  it("agrupa cluster → namespace → deployment, tudo ordenado", () => {
    const g = groupRows(rows);
    expect(g.map((c) => c.cluster)).toEqual(["c1", "c2"]);
    expect(g[0].namespaces.map((n) => n.namespace)).toEqual(["ns-a", "ns-b"]);
    expect(g[0].namespaces[0].rows.map((r) => r.workload)).toEqual(["x", "y"]);
    expect(g[0].namespaces[1].rows.map((r) => r.workload)).toEqual(["a", "b"]);
  });

  it("conta alertas por nível: vermelho vence aviso, e erro de coleta é contado à parte", () => {
    const g = groupRows(rows);
    expect(g[0].alerts).toEqual({ total: 4, red: 2, warn: 1, failed: 1 });
    expect(g[0].namespaces[0].alerts).toEqual({ total: 2, red: 0, warn: 1, failed: 1 });
    expect(g[0].namespaces[1].alerts).toEqual({ total: 2, red: 2, warn: 0, failed: 0 }); // b tem red e warn: conta só como red
    expect(g[1].alerts).toEqual({ total: 1, red: 0, warn: 0, failed: 0 });
  });

  it("sem linhas, sem grupos", () => {
    expect(groupRows([])).toEqual([]);
  });
});

describe("percentis de latência", () => {
  it("o padrão é só P99 e o que está gravado é validado", () => {
    expect(DEFAULT_PERCENTILES).toEqual([99]);
    expect(parsePercentiles(null)).toEqual([99]);
    expect(parsePercentiles("lixo")).toEqual([99]);
    expect(parsePercentiles("[]")).toEqual([99]);
    expect(parsePercentiles("[90,100]")).toEqual([99]);
    expect(parsePercentiles("[99,50,50,\"x\"]")).toEqual([50, 99]);
    expect(parsePercentiles("[95]")).toEqual([95]);
  });

  it("marcar e desmarcar mantém a ordem, sem repetir, e nunca zera a escolha", () => {
    expect(togglePercentile([99], 50, true)).toEqual([50, 99]);
    expect(togglePercentile([50, 99], 95, true)).toEqual([50, 95, 99]);
    expect(togglePercentile([50, 95, 99], 95, false)).toEqual([50, 99]);
    expect(togglePercentile([99], 99, false)).toEqual([99]); // o último não sai
    expect(togglePercentile([50, 99], 50, true)).toEqual([50, 99]);
  });

  it("lê o valor do percentil e devolve null quando não foi coletado", () => {
    const t = { p50_peak_ms: 120, p95_peak_ms: null, p99_peak_ms: 1500 } as ReportTraffic;
    expect(percentileValue(t, 50)).toBe(120);
    expect(percentileValue(t, 95)).toBeNull();
    expect(percentileValue(t, 99)).toBe(1500);
    expect(percentileValue(t, 90)).toBeNull();
    expect(percentileValue(null, 99)).toBeNull();
    expect(percentileValue({ p50_peak_ms: 0, p95_peak_ms: 0, p99_peak_ms: 0 } as ReportTraffic, 50)).toBe(0); // zero real continua zero
  });
});

describe("contexto de node", () => {
  it("é opt-in: só a escolha gravada 'true' liga", () => {
    expect(parseNodeContext(null)).toBe(false);
    expect(parseNodeContext("true")).toBe(true);
    expect(parseNodeContext("false")).toBe(false);
    expect(parseNodeContext("1")).toBe(false);
    expect(parseNodeContext("lixo")).toBe(false);
  });

  it("descreve os pools, com plural e tipo de instância", () => {
    expect(fmtPools(null)).toBe("sem dado");
    expect(fmtPools([])).toBe("sem dado");
    expect(fmtPools([{ name: "calculofret2", nodes: 27, instance_type: "Standard_D4s_v5" }])).toBe("calculofret2 (27 nodes, Standard_D4s_v5)");
    expect(fmtPools([{ name: "a", nodes: 1 }, { name: "b", nodes: 2, instance_type: "m5.large" }])).toBe("a (1 node); b (2 nodes, m5.large)");
  });

  it("mostra pior node e média, e a cobertura quando é parcial", () => {
    const u = { nodes_with_data: 27, worst_pct: 76.4, avg_pct: 22.14 };
    expect(fmtNodeUsage(u, 27)).toBe("76.4% / 22.1%");
    expect(fmtNodeUsage(u)).toBe("76.4% / 22.1%");
    expect(fmtNodeUsage({ ...u, nodes_with_data: 9 }, 27)).toBe("76.4% / 22.1% (9 de 27 nodes)");
    expect(fmtNodeUsage(null, 27)).toBe("sem dado");
    expect(fmtNodeUsage({ nodes_with_data: 0, worst_pct: null, avg_pct: null })).toBe("sem dado");
    expect(fmtNodeUsage({ nodes_with_data: 1, worst_pct: 0, avg_pct: 0 })).toBe("0.0% / 0.0%"); // zero real continua zero
  });

  it("throttling usa duas casas (são frações de por cento)", () => {
    expect(fmtThrottle({ max: 0.00506, min: 0, avg: 0.0007 })).toBe("0.01% / 0.00%");
    expect(fmtThrottle({ max: 12.345, min: 1, avg: 3.2 })).toBe("12.35% / 3.20%");
    expect(fmtThrottle(null)).toBe("sem dado");
  });
});
