import { describe, expect, it } from "vitest";
import { MAX_SAVED_NAME, defaultReportName, describeSaved, fmtSize, nameProblem, savedAt, type SavedReportMeta } from "./metricsSavedReports";

const meta = (o: Partial<SavedReportMeta> = {}): SavedReportMeta => ({
  id: 1, name: "x", created_at: "2026-11-13T17:20:00Z", window_start: "2026-11-11T17:00:00Z", window_end: "2026-11-11T19:00:00Z", apps: 13, clusters: 2, size_bytes: 46080, ...o,
});

describe("relatórios salvos: textos", () => {
  it("nome sugerido pela janela, no fuso do relatório", () => {
    expect(defaultReportName({ window: { start: "2026-11-11T17:00:00Z", end: "2026-11-11T19:00:00Z" } })).toBe("Relatório 11/11 14:00 → 16:00");
    expect(defaultReportName({ window: { start: "2026-11-11T22:00:00Z", end: "2026-11-12T04:00:00Z" } })).toBe("Relatório 11/11 19:00 → 12/11 01:00");
  });
  it("tamanho legível", () => {
    expect(fmtSize(512)).toBe("512 B");
    expect(fmtSize(46080)).toBe("45 KB");
    expect(fmtSize(1.5 * 1024 * 1024)).toBe("1,5 MB");
    expect(fmtSize(NaN)).toBe("—");
    expect(fmtSize(-1)).toBe("—");
  });
  it("resumo da lista, com singular", () => {
    expect(describeSaved(meta())).toBe("13 aplicações · 2 clusters · 11/11 14:00 → 16:00 · 45 KB");
    expect(describeSaved(meta({ apps: 1, clusters: 1, size_bytes: 900 }))).toBe("1 aplicação · 1 cluster · 11/11 14:00 → 16:00 · 900 B");
    expect(describeSaved(meta({ window_end: "2026-11-12T04:00:00Z" }))).toContain("11/11 14:00 → 12/11 01:00");
  });
  it("quando foi salvo", () => {
    expect(savedAt(meta())).toBe("salvo em 13/11 14:20");
  });
  it("nome: vazio e grande demais são recusados; o limite conta caracteres", () => {
    expect(nameProblem("")).toBe("Informe um nome.");
    expect(nameProblem("   ")).toBe("Informe um nome.");
    expect(nameProblem("a".repeat(MAX_SAVED_NAME + 1))).toContain("80");
    expect(nameProblem("ã".repeat(MAX_SAVED_NAME))).toBe("");
    expect(nameProblem("Stress test 11/11")).toBe("");
  });
});
