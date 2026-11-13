import { describe, expect, it } from "vitest";
import { DEFAULT_THRESHOLDS, changedThresholds, isDefaultThresholds, normalizeThresholds, validateThresholds } from "./metricsThresholds";

describe("normalizeThresholds", () => {
  it("sem nada devolve o padrão (os valores que eram fixos no código)", () => {
    for (const raw of [undefined, null, "", "não é json", 5, [], {}, "[]"]) expect(normalizeThresholds(raw)).toEqual(DEFAULT_THRESHOLDS);
    expect(DEFAULT_THRESHOLDS).toMatchObject({ latWarnMs: 500, latRedMs: 1000, usageWarn: 70, usageRed: 85, throttleWarn: 10, throttleRed: 25, netWarn: 0.5, err5Rate: 0, err4Rate: 0, restartsWarn: 0 });
  });
  it("aceita objeto, texto JSON e lista de um objeto (como o servidor guarda)", () => {
    const o = { latWarnMs: 300, latRedMs: 800 };
    expect(normalizeThresholds(o).latWarnMs).toBe(300);
    expect(normalizeThresholds(JSON.stringify(o)).latRedMs).toBe(800);
    expect(normalizeThresholds([o]).latWarnMs).toBe(300);
  });
  it("campo ausente, negativo, texto ou fora do máximo volta ao padrão sem derrubar os demais", () => {
    const t = normalizeThresholds({ latWarnMs: -1, latRedMs: "mil", usageWarn: 50, throttleRed: 500, netWarn: NaN, hpaCeiling: "sim", pods: false });
    expect(t.latWarnMs).toBe(500);
    expect(t.latRedMs).toBe(1000);
    expect(t.usageWarn).toBe(50);
    expect(t.throttleRed).toBe(25); // 500% passa do máximo (100)
    expect(t.netWarn).toBe(0.5);
    expect(t.hpaCeiling).toBe(true);
    expect(t.pods).toBe(false);
  });
  it("par com alerta acima do erro volta ao padrão do par inteiro", () => {
    const t = normalizeThresholds({ latWarnMs: 2000, latRedMs: 100, usageWarn: 60 });
    expect([t.latWarnMs, t.latRedMs]).toEqual([500, 1000]);
    expect(t.usageWarn).toBe(60);
  });
  it("não altera o padrão compartilhado", () => {
    const t = normalizeThresholds({ latWarnMs: 1 });
    t.latWarnMs = 999;
    expect(DEFAULT_THRESHOLDS.latWarnMs).toBe(500);
  });
});

describe("validateThresholds", () => {
  it("padrão é válido", () => expect(validateThresholds(DEFAULT_THRESHOLDS)).toEqual([]));
  it("barra negativo, não número e acima do máximo", () => {
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, netWarn: -1 })[0]).toContain("Rede");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, restartsWarn: NaN })[0]).toContain("Restarts");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, throttleRed: 150 })[0]).toContain("máximo");
  });
  it("barra alerta maior que erro, par a par", () => {
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, latWarnMs: 2000 })).toEqual(["Latência: o limite de alerta não pode ser maior que o de erro."]);
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, usageWarn: 90 })[0]).toContain("CPU e memória");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, throttleWarn: 30 })[0]).toContain("Throttling");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, latWarnMs: 1000 })).toEqual([]); // igual é permitido
  });
});

describe("isDefaultThresholds e changedThresholds", () => {
  it("reconhece o padrão e lista só o que mudou", () => {
    expect(isDefaultThresholds(DEFAULT_THRESHOLDS)).toBe(true);
    expect(changedThresholds(DEFAULT_THRESHOLDS)).toEqual([]);
    const t = { ...DEFAULT_THRESHOLDS, latWarnMs: 300, hpaCeiling: false };
    expect(isDefaultThresholds(t)).toBe(false);
    expect(changedThresholds(t)).toEqual(["Latência (alerta): 300", "Teto do HPA: ignorado"]);
  });
});
