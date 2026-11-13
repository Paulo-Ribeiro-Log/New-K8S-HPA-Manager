import { describe, expect, it } from "vitest";
import { eventLabel, mergeEvents, parseEvents, removeEvent, upsertEvent } from "./metricsEvents";

const A = { name: "Stress test", start: "2026-11-11T17:00:00Z", end: "2026-11-11T19:00:00Z" };
const B = { name: "Black Friday", start: "2026-11-27T12:00:00Z", end: "2026-11-27T15:30:00Z" };

describe("eventos nomeados", () => {
  it("parse aceita só eventos válidos e ordena do mais recente", () => {
    const r = parseEvents([A, B, { name: "", start: A.start, end: A.end }, { name: "x", start: "lixo", end: A.end }, { name: "volta", start: A.end, end: A.start }, null, 5]);
    expect(r.map((e) => e.name)).toEqual(["Black Friday", "Stress test"]);
    expect(parseEvents("não é json")).toEqual([]);
    expect(parseEvents(JSON.stringify([A]))).toHaveLength(1);
    expect(parseEvents({})).toEqual([]);
  });
  it("upsert troca pelo nome, apara o nome e recusa janela inválida", () => {
    const l = upsertEvent([A], "  Stress test ", "2026-11-12T10:00:00Z", "2026-11-12T11:00:00Z");
    expect(l).toHaveLength(1);
    expect(l[0].start).toBe("2026-11-12T10:00:00Z");
    expect(upsertEvent([A], "", A.start, A.end)).toEqual([A]);
    expect(upsertEvent([A], "x", A.end, A.start)).toEqual([A]);
  });
  it("remove e junta servidor e navegador sem perder nenhum, com o servidor valendo no mesmo nome", () => {
    expect(removeEvent([A, B], "Stress test")).toEqual([B]);
    const m = mergeEvents([A], [{ ...A, end: "2026-11-11T20:00:00Z" }, B]);
    expect(m.map((e) => e.name)).toEqual(["Black Friday", "Stress test"]);
    expect(m.find((e) => e.name === "Stress test")!.end).toBe(A.end);
  });
  it("o rótulo mostra nome, intervalo no fuso do relatório e duração", () => {
    expect(eventLabel(A)).toBe("Stress test · 11/11 14:00 → 16:00 (2 h)");
    expect(eventLabel(B)).toContain("(3 h 30 min)");
    expect(eventLabel({ name: "noite", start: "2026-11-12T01:00:00Z", end: "2026-11-12T05:00:00Z" })).toContain("11/11 22:00 → 12/11 02:00");
  });
});

import { eventAtWindow, fileSlug } from "./metricsEvents";
describe("evento da janela atual e nome de arquivo", () => {
  const ev = { name: "Stress test 11/11", start: "2026-11-11T17:00:00Z", end: "2026-11-11T19:00:00Z" };
  it("acha o evento pela janela, no minuto, e ignora segundos e formato", () => {
    expect(eventAtWindow([ev], "2026-11-11T17:00:00Z", "2026-11-11T19:00:00Z")).toEqual(ev);
    expect(eventAtWindow([ev], "2026-11-11T17:00:00.000Z", "2026-11-11T19:00:30Z")).toEqual(ev);
    expect(eventAtWindow([ev], "2026-11-11T17:01:00Z", "2026-11-11T19:00:00Z")).toBeNull();
    expect(eventAtWindow([ev], null, "2026-11-11T19:00:00Z")).toBeNull();
    expect(eventAtWindow([ev], "lixo", "lixo")).toBeNull();
    expect(eventAtWindow([], ev.start, ev.end)).toBeNull();
  });
  it("slug sem acento, sem barra e sem hífen sobrando", () => {
    expect(fileSlug("Stress test 11/11")).toBe("stress-test-11-11");
    expect(fileSlug("  Black Friday — Cálculo de Fretes!  ")).toBe("black-friday-calculo-de-fretes");
    expect(fileSlug("///")).toBe("");
    expect(fileSlug("a".repeat(60)).length).toBe(40);
    expect(fileSlug("x".repeat(39) + "-y")).toBe("x".repeat(39));
  });
});
