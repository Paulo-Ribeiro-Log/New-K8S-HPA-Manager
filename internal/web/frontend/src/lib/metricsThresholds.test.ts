import { describe, expect, it } from "vitest";
import { DEFAULT_REPORT_THRESHOLDS, DEFAULT_THRESHOLDS, changedThresholds, latencyLimits, reportThresholds, isDefaultThresholds, normalizeThresholds, validateThresholds } from "./metricsThresholds";

describe("normalizeThresholds", () => {
  it("sem nada devolve o padrão (os valores que eram fixos no código)", () => {
    for (const raw of [undefined, null, "", "não é json", 5, [], {}, "[]"]) expect(normalizeThresholds(raw)).toEqual(DEFAULT_THRESHOLDS);
    expect(DEFAULT_THRESHOLDS).toMatchObject({ p50WarnMs: 200, p50RedMs: 500, p95WarnMs: 400, p95RedMs: 800, p99WarnMs: 500, p99RedMs: 1000, usageWarn: 70, usageRed: 85, throttleWarn: 10, throttleRed: 25, netWarn: 0.5, err5Rate: 0, err4Rate: 0, restartsWarn: 0 });
  });
  it("aceita objeto, texto JSON e lista de um objeto (como o servidor guarda)", () => {
    const o = { p99WarnMs: 300, p99RedMs: 800 };
    expect(normalizeThresholds(o).p99WarnMs).toBe(300);
    expect(normalizeThresholds(JSON.stringify(o)).p99RedMs).toBe(800);
    expect(normalizeThresholds([o]).p99WarnMs).toBe(300);
  });
  it("campo ausente, negativo, texto ou fora do máximo volta ao padrão sem derrubar os demais", () => {
    const t = normalizeThresholds({ p99WarnMs: -1, p99RedMs: "mil", usageWarn: 50, throttleRed: 500, netWarn: NaN, hpaCeiling: "sim", pods: false });
    expect(t.p99WarnMs).toBe(500);
    expect(t.p99RedMs).toBe(1000);
    expect(t.usageWarn).toBe(50);
    expect(t.throttleRed).toBe(25); // 500% passa do máximo (100)
    expect(t.netWarn).toBe(0.5);
    expect(t.hpaCeiling).toBe(true);
    expect(t.pods).toBe(false);
  });
  it("par com alerta acima do erro volta ao padrão do par inteiro", () => {
    const t = normalizeThresholds({ p99WarnMs: 2000, p99RedMs: 100, usageWarn: 60 });
    expect([t.p99WarnMs, t.p99RedMs]).toEqual([500, 1000]);
    expect(t.usageWarn).toBe(60);
  });
  it("não altera o padrão compartilhado", () => {
    const t = normalizeThresholds({ p99WarnMs: 1 });
    t.p99WarnMs = 999;
    expect(DEFAULT_THRESHOLDS.p99WarnMs).toBe(500);
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
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, p99WarnMs: 2000 })).toEqual(["Latência P99: o limite de alerta não pode ser maior que o de erro."]);
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, p50WarnMs: 900 })[0]).toContain("Latência P50");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, p95WarnMs: 900 })[0]).toContain("Latência P95");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, usageWarn: 90 })[0]).toContain("CPU e memória");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, throttleWarn: 30 })[0]).toContain("Throttling");
    expect(validateThresholds({ ...DEFAULT_THRESHOLDS, p99WarnMs: 1000 })).toEqual([]); // igual é permitido
  });
});

describe("isDefaultThresholds e changedThresholds", () => {
  it("reconhece o padrão e lista só o que mudou", () => {
    expect(isDefaultThresholds(DEFAULT_THRESHOLDS)).toBe(true);
    expect(changedThresholds(DEFAULT_THRESHOLDS)).toEqual([]);
    const t = { ...DEFAULT_THRESHOLDS, p99WarnMs: 300, hpaCeiling: false };
    expect(isDefaultThresholds(t)).toBe(false);
    expect(changedThresholds(t)).toEqual(["Latência P99 (alerta): 300", "Teto do HPA: ignorado"]);
  });
});

describe("escala de latência por percentil", () => {
  it("cada percentil tem a sua, com o P99 igual ao padrão de sempre", () => {
    expect(latencyLimits(DEFAULT_THRESHOLDS, 50)).toEqual({ warn: 200, red: 500 });
    expect(latencyLimits(DEFAULT_THRESHOLDS, 95)).toEqual({ warn: 400, red: 800 });
    expect(latencyLimits(DEFAULT_THRESHOLDS, 99)).toEqual({ warn: 500, red: 1000 });
  });
  it("limites salvos antes (um par só) viram os do P99 e o resto fica no padrão", () => {
    const t = normalizeThresholds({ latWarnMs: 300, latRedMs: 700, usageWarn: 60 });
    expect(latencyLimits(t, 99)).toEqual({ warn: 300, red: 700 });
    expect(latencyLimits(t, 50)).toEqual({ warn: 200, red: 500 });
    expect(latencyLimits(t, 95)).toEqual({ warn: 400, red: 800 });
    expect(t.usageWarn).toBe(60);
    expect("latWarnMs" in t).toBe(false); // o campo antigo não volta a ser gravado
  });
  it("se vierem o campo antigo e o novo, o novo vale", () => {
    const t = normalizeThresholds({ latWarnMs: 300, latRedMs: 700, p99WarnMs: 450, p99RedMs: 900 });
    expect(latencyLimits(t, 99)).toEqual({ warn: 450, red: 900 });
  });
  it("migração com par inválido volta ao padrão do P99", () => {
    expect(latencyLimits(normalizeThresholds({ latWarnMs: 900, latRedMs: 100 }), 99)).toEqual({ warn: 500, red: 1000 });
  });
});

describe("limites do Relatório a partir dos do Tempo real", () => {
  it("o padrão do Tempo real dá exatamente o padrão de sempre do Relatório", () => {
    expect(reportThresholds(DEFAULT_THRESHOLDS, 3600)).toEqual(DEFAULT_REPORT_THRESHOLDS);
    expect(reportThresholds(DEFAULT_THRESHOLDS, 86400)).toEqual(DEFAULT_REPORT_THRESHOLDS);
  });
  it("o P99 vermelho é o limite de erro do P99", () => {
    expect(reportThresholds({ ...DEFAULT_THRESHOLDS, p99RedMs: 800 }, 3600).p99_red_ms).toBe(800);
  });
  it("erros por segundo viram total da janela: taxa média acima do limite", () => {
    const t = { ...DEFAULT_THRESHOLDS, err5Rate: 0.5, err4Rate: 2 };
    expect(reportThresholds(t, 3600)).toMatchObject({ err_5xx: 1800, err_4xx: 7200 });
    expect(reportThresholds(t, 600)).toMatchObject({ err_5xx: 300, err_4xx: 1200 }); // janela menor, total menor
  });
  it("janela inválida não vira NaN nem negativo", () => {
    for (const w of [0, -5, NaN, Infinity]) {
      const r = reportThresholds({ ...DEFAULT_THRESHOLDS, err5Rate: 3 }, w);
      expect(Number.isFinite(r.err_5xx) && r.err_5xx >= 0).toBe(true);
    }
  });
});
