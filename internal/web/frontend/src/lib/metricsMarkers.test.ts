import { describe, expect, it } from "vitest";
import { MARKER_MAX_AGE_MS, MARKER_NAME_MAX, MAX_MARKERS, addMarker, markerTime, markersInRange, mergeMarkers, parseMarkers, removeMarker, shortName } from "./metricsMarkers";

const NOW = Date.UTC(2026, 10, 13, 17, 30);
const m = (id: string, name: string, minsAgo: number) => ({ id, name, at: new Date(NOW - minsAgo * 60000).toISOString() });

describe("marcadores pontuais", () => {
  it("parse aceita só marcadores válidos e recentes, do mais novo ao mais antigo", () => {
    const velho = { id: "v", name: "velho", at: new Date(NOW - MARKER_MAX_AGE_MS - 1000).toISOString() };
    const r = parseMarkers([m("a", "a", 30), m("b", "b", 5), velho, { id: "", name: "x", at: new Date(NOW).toISOString() }, { id: "c", name: " ", at: new Date(NOW).toISOString() }, { id: "d", name: "d", at: "lixo" }, null, 7], NOW);
    expect(r.map((x) => x.id)).toEqual(["b", "a"]);
    expect(parseMarkers("não é json", NOW)).toEqual([]);
    expect(parseMarkers({}, NOW)).toEqual([]);
    expect(parseMarkers(JSON.stringify([m("a", "a", 1)]), NOW)).toHaveLength(1);
  });

  it("add marca o instante dado, apara e limita o nome, e recusa nome vazio", () => {
    const l = addMarker([], "  Subi o limite do HPA  ", NOW);
    expect(l).toHaveLength(1);
    expect(l[0].name).toBe("Subi o limite do HPA");
    expect(l[0].at).toBe(new Date(NOW).toISOString());
    expect(addMarker(l, "   ", NOW)).toBe(l);
    expect(addMarker([], "x".repeat(200), NOW)[0].name).toHaveLength(MARKER_NAME_MAX);
  });

  it("permite o mesmo nome várias vezes, com ids diferentes, e o mais novo vem primeiro", () => {
    let l = addMarker([], "HPA escalou", NOW - 600000);
    l = addMarker(l, "HPA escalou", NOW);
    expect(l).toHaveLength(2);
    expect(new Set(l.map((x) => x.id)).size).toBe(2);
    expect(Date.parse(l[0].at)).toBeGreaterThan(Date.parse(l[1].at));
  });

  it("não passa do limite", () => {
    let l = parseMarkers([], NOW);
    for (let i = 0; i < MAX_MARKERS + 5; i++) l = addMarker(l, `m${i}`, NOW + i * 1000);
    expect(l).toHaveLength(MAX_MARKERS);
    expect(l[0].name).toBe(`m${MAX_MARKERS + 4}`); // ficam os mais novos
  });

  it("remove pelo id e junta servidor e navegador sem perder nenhum", () => {
    expect(removeMarker([m("a", "a", 1), m("b", "b", 2)], "a").map((x) => x.id)).toEqual(["b"]);
    const merged = mergeMarkers([m("a", "a", 10)], [m("a", "a-local", 10), m("b", "b", 5)]);
    expect(merged.map((x) => x.id)).toEqual(["b", "a"]);
    expect(merged.find((x) => x.id === "a")!.name).toBe("a"); // o do servidor vale no mesmo id
  });

  it("markersInRange devolve os do intervalo, em ordem cronológica, inclusive nas pontas", () => {
    const l = [m("a", "a", 60), m("b", "b", 30), m("c", "c", 10), m("d", "d", 1)];
    const r = markersInRange(l, NOW - 30 * 60000, NOW - 10 * 60000);
    expect(r.map((x) => x.id)).toEqual(["b", "c"]);
    expect(r[0].t).toBe(NOW - 30 * 60000);
    expect(markersInRange(l, NOW, NOW)).toEqual([]);
  });

  it("hora no fuso do relatório, com data quando não é de hoje", () => {
    expect(markerTime("2026-11-13T17:12:00Z", NOW)).toBe("14:12"); // UTC-3
    expect(markerTime("2026-11-12T17:12:00Z", NOW)).toBe("12/11 14:12");
  });

  it("nome curto de rótulo", () => {
    expect(shortName("curto")).toBe("curto");
    expect(shortName("a".repeat(30))).toBe(`${"a".repeat(23)}…`);
  });
});
