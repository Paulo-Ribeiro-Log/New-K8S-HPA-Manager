import { describe, expect, it } from "vitest";
import {
  normalizeLive, fmtRestartReasons, podsLevel, buildNetworkRows, hpaLevel, throttleLevel, netLevel, fmtBytesPerSec, summarizeStatus, worstLevel, seriesLevel, projectSeasonal, projectWithBaseline, DAY_MS, DEFAULT_LIVE_REFRESH, DEFAULT_LIVE_RANGE, buildChartData, errorLevel, fmtCores, fmtMemMB, fmtSpanLabel, fmtUsagePcts, parseLiveResources, replicasLevel, restartsLevel, usageLevel, latencyLevel, sparkHeights, groupLive, lastValue, liveRangeMs, liveWindow, minutesToThreshold,
  parseLiveRange, parseLiveRefresh, parseProjection, projectTrend, projectedPercentile, type LiveSeries,
} from "./metricsLive";

const MIN = 60_000;
const ts = (n: number, t0 = 0) => Array.from({ length: n }, (_, i) => t0 + i * MIN);

describe("faixa de tempo e atualização", () => {
  it("a janela termina agora e dura o que a faixa diz", () => {
    const now = Date.UTC(2026, 10, 12, 12, 0, 0);
    const w = liveWindow("3h", now);
    expect(Date.parse(w.end)).toBe(now);
    expect(Date.parse(w.end) - Date.parse(w.start)).toBe(3 * 3600e3);
    expect(liveRangeMs("5m")).toBe(5 * MIN);
    expect(liveRangeMs("7d")).toBe(7 * 24 * 3600e3);
  });
  it("lê a faixa gravada e cai no padrão se for inválida", () => {
    expect(parseLiveRange('"6h"')).toBe("6h");
    expect(parseLiveRange('"99h"')).toBe(DEFAULT_LIVE_RANGE);
    expect(parseLiveRange("lixo")).toBe(DEFAULT_LIVE_RANGE);
    expect(parseLiveRange(null)).toBe(DEFAULT_LIVE_RANGE);
  });
  it("só aceita atualização de 1 a 5 minutos (ou pausado)", () => {
    expect(parseLiveRefresh("120")).toBe(120);
    expect(parseLiveRefresh("300")).toBe(300);
    expect(parseLiveRefresh("0")).toBe(0);
    expect(parseLiveRefresh("30")).toBe(DEFAULT_LIVE_REFRESH); // abaixo de 1 min
    expect(parseLiveRefresh("600")).toBe(DEFAULT_LIVE_REFRESH); // acima de 5 min
    expect(parseLiveRefresh(null)).toBe(DEFAULT_LIVE_REFRESH);
  });
  it("lê o horizonte da projeção", () => {
    expect(parseProjection("0")).toBe(0);
    expect(parseProjection("60")).toBe(60);
    expect(parseProjection("45")).toBe(30);
  });
});

describe("projectTrend", () => {
  it("reta crescente: projeta pelo coeficiente e dá a variação em %", () => {
    // 100, 110, ..., 190 (10 por minuto); último = 190
    const v = Array.from({ length: 10 }, (_, i) => 100 + i * 10);
    const p = projectTrend(ts(10), v, 30)!;
    expect(p.fittedNow).toBeCloseTo(190, 6);
    expect(p.projected).toBeCloseTo(190 + 300, 6);
    expect(p.changePct).toBeCloseTo((300 / 190) * 100, 4);
    expect(p.slopePerHour).toBeCloseTo(600, 6);
    expect(p.r2).toBeCloseTo(1, 9);
    expect(p.reliable).toBe(true);
    expect(p.line).toEqual([{ t: 9 * MIN, v: p.fittedNow }, { t: 9 * MIN + 30 * MIN, v: p.projected }]);
  });
  it("série em queda nunca projeta valor negativo", () => {
    const v = Array.from({ length: 10 }, (_, i) => 100 - i * 10); // 100 → 10
    const p = projectTrend(ts(10), v, 60)!;
    expect(p.projected).toBe(0);
    expect(p.changePct).toBeLessThan(0);
  });
  it("série constante: variação 0% e r2 0 (não confiável)", () => {
    const p = projectTrend(ts(10), Array(10).fill(50), 30)!;
    expect(p.projected).toBeCloseTo(50, 9);
    expect(p.changePct).toBeCloseTo(0, 9);
    expect(p.reliable).toBe(false);
  });
  it("ruído sem tendência não é dado como confiável", () => {
    const v = [10, 90, 20, 80, 15, 85, 12, 88, 18, 82];
    expect(projectTrend(ts(10), v, 30)!.reliable).toBe(false);
  });
  it("poucos pontos não são confiáveis, mesmo numa reta perfeita", () => {
    const p = projectTrend(ts(5), [1, 2, 3, 4, 5], 30)!;
    expect(p.r2).toBeCloseTo(1, 9);
    expect(p.points).toBe(5);
    expect(p.reliable).toBe(false);
  });
  it("ignora nulos e usa o tempo real dos pontos, não o índice", () => {
    // pontos em 0, 2, 4, 6, 8, 10 min (lacunas viram nulo); valor = 5 * minuto
    const times = ts(11);
    const v: (number | null)[] = times.map((t, i) => (i % 2 === 0 ? (t / MIN) * 5 : null));
    const p = projectTrend(times, v, 10)!;
    expect(p.points).toBe(6);
    expect(p.slopePerHour).toBeCloseTo(300, 6); // 5 por minuto
    expect(p.projected).toBeCloseTo(50 + 50, 6);
  });
  it("sem pontos suficientes ou horizonte 0: nulo", () => {
    expect(projectTrend([0], [1], 30)).toBeNull();
    expect(projectTrend(ts(3), [null, null, null], 30)).toBeNull();
    expect(projectTrend(ts(10), Array(10).fill(1), 0)).toBeNull();
    expect(projectTrend([5, 5], [1, 2], 30)).toBeNull(); // mesmo instante: sem inclinação definida
  });
  it("base ~0 não dá percentual", () => {
    const p = projectTrend(ts(10), Array(10).fill(0), 30)!;
    expect(p.changePct).toBeNull();
  });
});

describe("minutesToThreshold", () => {
  const sobe = projectTrend(ts(10), Array.from({ length: 10 }, (_, i) => 100 + i * 10), 120)!; // 190, +10/min
  it("calcula em quantos minutos a reta cruza o limite", () => {
    expect(minutesToThreshold(sobe, 250, 120)).toBeCloseTo(6, 6);
  });
  it("não alerta se só cruzaria depois do horizonte", () => {
    expect(minutesToThreshold(sobe, 1000, 30)).toBeNull();
  });
  it("não alerta se já passou do limite", () => {
    expect(minutesToThreshold(sobe, 150, 120)).toBeNull();
  });
  it("não alerta se a reta não sobe", () => {
    const cai = projectTrend(ts(10), Array.from({ length: 10 }, (_, i) => 100 - i), 60)!;
    expect(minutesToThreshold(cai, 500, 60)).toBeNull();
  });
  it("não alerta com projeção não confiável", () => {
    const ruido = projectTrend(ts(10), [10, 90, 20, 80, 15, 85, 12, 88, 18, 120], 60)!;
    expect(ruido.reliable).toBe(false);
    expect(minutesToThreshold(ruido, 100, 60)).toBeNull();
    expect(minutesToThreshold(null, 100, 60)).toBeNull();
  });
});

function serie(n: number, rps: (i: number) => number | null, p99?: (i: number) => number | null): LiveSeries {
  const times = ts(n, 1_000_000);
  return {
    cluster: "c1", namespace: "ns", workload: "w", times,
    rps: times.map((_, i) => rps(i)), err_4xx_rps: times.map(() => null), err_5xx_rps: times.map(() => null), err_4xx_total: null, err_5xx_total: null,
    ...(p99 ? { p99_ms: times.map((_, i) => p99(i)) } : {}),
  };
}

describe("buildChartData", () => {
  it("liga o último ponto real à reta e estende até o fim do horizonte", () => {
    const s = serie(10, (i) => 100 + i * 10);
    const d = buildChartData(s, [99], 30);
    const last = d.rows[d.rows.length - 1];
    expect(last.t).toBe(s.times[9] + 30 * MIN);
    expect(last.rpsProj).toBeCloseTo(190 + 300, 6);
    expect(last.rps).toBeUndefined(); // o futuro não tem valor real
    const join = d.rows.find((r) => r.t === s.times[9])!;
    expect(join.rps).toBe(190);
    expect(join.rpsProj).toBeCloseTo(190, 6);
    expect(d.rows.filter((r) => r.rpsProj !== undefined)).toHaveLength(2); // só os dois pontos da reta
    expect(d.rows.map((r) => r.t)).toEqual([...d.rows.map((r) => r.t)].sort((a, b) => a - b));
  });
  it("sem projeção (horizonte 0) não acrescenta linhas", () => {
    const s = serie(10, (i) => i);
    const d = buildChartData(s, [99], 0);
    expect(d.rows).toHaveLength(10);
    expect(d.rpsProjection).toBeNull();
  });
  it("projeta a latência do maior percentil coletado", () => {
    expect(projectedPercentile([50])).toBe(50);
    expect(projectedPercentile([50, 95])).toBe(95);
    expect(projectedPercentile([50, 95, 99])).toBe(99);
    const s = serie(10, () => 1, (i) => 500 + i * 10);
    const d = buildChartData(s, [99], 30);
    expect(d.latPercentile).toBe(99);
    expect(d.latProjection!.projected).toBeCloseTo(590 + 300, 6);
    expect(d.hasLatency).toBe(true);
    expect(d.hasErrors).toBe(false);
  });
  it("nulo continua nulo (não vira zero)", () => {
    const s = serie(10, (i) => (i === 4 ? null : 10));
    expect(buildChartData(s, [99], 0).rows[4].rps).toBeNull();
  });
});

describe("auxiliares", () => {
  it("lastValue pula nulos do fim", () => {
    expect(lastValue([1, 2, null, null])).toBe(2);
    expect(lastValue([null, null])).toBeNull();
    expect(lastValue(undefined)).toBeNull();
    expect(lastValue([0])).toBe(0); // zero é valor
  });
  it("groupLive agrupa por cluster na ordem de aparição", () => {
    const mk = (cluster: string, workload: string): LiveSeries => ({ ...serie(1, () => 1), cluster, workload });
    const g = groupLive([mk("b", "x"), mk("a", "y"), mk("b", "z")]);
    expect(g.map((x) => [x.cluster, x.series.map((s) => s.workload)])).toEqual([["b", ["x", "z"]], ["a", ["y"]]]);
  });
});

describe("cartão compacto", () => {
  it("latência: acima de 1 s é erro, acima de 0,5 s é alerta, exatamente no limite não passa", () => {
    expect(latencyLevel(1000.1)).toBe("red");
    expect(latencyLevel(1000)).toBe("warn");
    expect(latencyLevel(500.1)).toBe("warn");
    expect(latencyLevel(500)).toBe("ok");
    expect(latencyLevel(0)).toBe("ok"); // zero é valor
    expect(latencyLevel(null)).toBe("info");
  });
  it("erros: 5xx é vermelho, 4xx é alerta, zero é ok, sem dado é info", () => {
    expect(errorLevel(0.001, "5xx")).toBe("red");
    expect(errorLevel(0.001, "4xx")).toBe("warn");
    expect(errorLevel(0, "5xx")).toBe("ok");
    expect(errorLevel(null, "5xx")).toBe("info");
    expect(errorLevel(undefined, "4xx")).toBe("info");
  });
  it("sparkHeights normaliza pelo maior valor e preserva lacunas", () => {
    expect(sparkHeights([10, 5, null, 20])).toEqual({ heights: [0.5, 0.25, null, 1], max: 20 });
    expect(sparkHeights([0, 0])).toEqual({ heights: [0, 0], max: 0 }); // tudo zero: barras vazias, sem divisão por 0
    expect(sparkHeights([null, null])).toEqual({ heights: [null, null], max: null });
    expect(sparkHeights([])).toEqual({ heights: [], max: null });
  });
  it("rótulo do período", () => {
    expect(fmtSpanLabel(5 * MIN)).toBe("5m");
    expect(fmtSpanLabel(30 * MIN)).toBe("30m");
    expect(fmtSpanLabel(60 * MIN)).toBe("1h");
    expect(fmtSpanLabel(24 * 3600e3)).toBe("1d");
    expect(fmtSpanLabel(48 * 3600e3)).toBe("2d");
    expect(fmtSpanLabel(36 * 3600e3)).toBe("36h");
    expect(fmtSpanLabel(90 * MIN)).toBe("1.5h");
  });
});

describe("itens do Prometheus", () => {
  it("réplicas: menos disponíveis que desejadas é erro", () => {
    expect(replicasLevel(70, 70)).toBe("ok");
    expect(replicasLevel(71, 70)).toBe("ok"); // durante um rollout pode passar
    expect(replicasLevel(69, 70)).toBe("red");
    expect(replicasLevel(0, 0)).toBe("ok"); // escalado a zero de propósito
    expect(replicasLevel(null, 70)).toBe("info");
    expect(replicasLevel(70, null)).toBe("info");
  });
  it("restarts: qualquer um é alerta, sem dado é info", () => {
    expect(restartsLevel(0)).toBe("ok");
    expect(restartsLevel(1)).toBe("warn");
    expect(restartsLevel(null)).toBe("info");
  });
  it("uso em % do limit: 70 e 85 são os cortes; sem limit não há veredito", () => {
    expect(usageLevel(85.1)).toBe("red");
    expect(usageLevel(85)).toBe("warn");
    expect(usageLevel(70.1)).toBe("warn");
    expect(usageLevel(70)).toBe("ok");
    expect(usageLevel(0)).toBe("ok");
    expect(usageLevel(null)).toBe("info");
  });
  it("formatação", () => {
    expect(fmtCores(0.8123)).toBe("0.81 cores");
    expect(fmtCores(0)).toBe("0.00 cores"); // zero é valor
    expect(fmtCores(null)).toBe("sem dado");
    expect(fmtMemMB(141.17)).toBe("141 MB");
    expect(fmtMemMB(1023.4)).toBe("1023 MB");
    expect(fmtMemMB(1024)).toBe("1.0 GB"); // exatamente 1 GB já é GB
    expect(fmtMemMB(64392.13)).toBe("62.9 GB");
    expect(fmtMemMB(null)).toBe("sem dado");
    expect(fmtUsagePcts(0.94, 5.12)).toBe("(0.9% do limit · 5.1% do request)");
    expect(fmtUsagePcts(null, 5.12)).toBe("(5.1% do request)");
    expect(fmtUsagePcts(null, null)).toBe("");
  });
  it("a escolha gravada vem ligada por padrão", () => {
    expect(parseLiveResources(null)).toBe(true);
    expect(parseLiveResources("true")).toBe(true);
    expect(parseLiveResources("false")).toBe(false);
  });
});

describe("normalizeLive", () => {
  it("troca listas nulas do Go por listas vazias (a tela chama .length)", () => {
    const r = normalizeLive({ series: [{ item: { cluster: "c", namespace: "n", workload: "w" }, times: null, rps: null, err_4xx_rps: null, err_5xx_rps: null, notes: null }], warnings: null } as any);
    expect(r.series[0].times).toEqual([]);
    expect(r.series[0].rps).toEqual([]);
    expect(r.series[0].notes).toEqual([]);
    expect(r.warnings).toEqual([]);
  });
});

describe("projeção sazonal (ontem no mesmo horário)", () => {
  const MINUTE = 60_000;
  const T0 = Date.UTC(2026, 10, 12, 12, 0, 0);
  // ontem: sobe 100 → 200 em 90 min (1 ponto por minuto, de T0-DAY-30min a T0-DAY+60min)
  const baseTimes = Array.from({ length: 91 }, (_, i) => T0 - DAY_MS - 30 * MINUTE + i * MINUTE);
  const baseVals = baseTimes.map((_, i) => 100 + (100 * i) / 90);
  // hoje: últimos 30 min, com o dobro de ontem
  const times = Array.from({ length: 30 }, (_, i) => T0 - 29 * MINUTE + i * MINUTE);
  const today = times.map((t) => 2 * (100 + (100 * ((t - (T0 - 30 * MINUTE)) / MINUTE)) / 90));

  it("escala a curva de ontem pelo fator de hoje", () => {
    const p = projectSeasonal(times, today, baseTimes, baseVals, 30)!;
    expect(p.basis).toBe("ontem");
    expect(p.reliable).toBe(true);
    // ontem em T0+30 ≈ 100 + 100*60/90 = 166,7; hoje é 2× → ≈ 333
    expect(p.projected).toBeGreaterThan(320);
    expect(p.projected).toBeLessThan(345);
    expect(p.changePct!).toBeGreaterThan(0);
  });

  it("sem dado de ontem cai na reta do período", () => {
    expect(projectSeasonal(times, today, [], [], 30)).toBeNull();
    const p = projectWithBaseline(times, today, { times: [], values: [] }, 30)!;
    expect(p.basis).toBe("reta");
  });

  it("ontem zerado ou fator absurdo não serve de base", () => {
    expect(projectSeasonal(times, today, baseTimes, baseVals.map(() => 0), 30)).toBeNull();
    expect(projectSeasonal(times, today.map((v) => v * 1000), baseTimes, baseVals, 30)).toBeNull();
  });

  it("poucos pares hoje×ontem não bastam", () => {
    expect(projectSeasonal(times.slice(-3), today.slice(-3), baseTimes, baseVals, 30)).toBeNull();
  });
});

describe("nível resumido (minimizar)", () => {
  it("pega o pior nível: vermelho > alerta > ok > info", () => {
    expect(worstLevel([])).toBe("info");
    expect(worstLevel(["ok", "warn", "info"])).toBe("warn");
    expect(worstLevel(["ok", "red", "warn"])).toBe("red");
    expect(worstLevel(["info", "ok"])).toBe("ok");
  });
  it("série com 5xx é vermelha; só 4xx é alerta; latência alta pesa", () => {
    const base = { item: { cluster: "c", namespace: "n", workload: "w" }, times: [1], rps: [1], err_4xx_rps: [0], err_5xx_rps: [0], p99_ms: [100] } as any;
    expect(seriesLevel(base, [99])).toBe("ok");
    expect(seriesLevel({ ...base, err_5xx_rps: [0.5] }, [99])).toBe("red");
    expect(seriesLevel({ ...base, err_4xx_rps: [1] }, [99])).toBe("warn");
    expect(seriesLevel({ ...base, p99_ms: [1500] }, [99])).toBe("red");
  });
});

describe("summarizeStatus (selos do topo)", () => {
  const mk = (workload: string, over: any = {}) => ({ cluster: "c", namespace: "n", workload, times: [1], rps: [1], err_4xx_rps: [0], err_5xx_rps: [0], p99_ms: [100], ...over }) as any;
  it("agrupa por nível e explica o motivo", () => {
    const r = summarizeStatus([mk("a"), mk("b", { err_5xx_rps: [2], err_5xx_total: 1234 }), mk("c", { err_4xx_rps: [1] }), mk("d", { error: "timeout", p99_ms: null, err_4xx_rps: null, err_5xx_rps: null })], [99]);
    expect(r.ok.map((e) => e.name)).toEqual(["c · a"]);
    expect(r.red.map((e) => e.name)).toEqual(["c · b", "c · d"]);
    expect(r.red[0].reasons[0]).toContain("5xx");
    expect(r.red[0].reasons[0]).toContain("1.234");
    expect(r.red[1].reasons[0]).toContain("falha na coleta");
    expect(r.warn.map((e) => e.name)).toEqual(["c · c"]);
  });
});

describe("HPA, throttling e rede", () => {
  it("HPA no teto é alerta; sem HPA é info", () => {
    expect(hpaLevel(72, 72)).toBe("warn");
    expect(hpaLevel(80, 72)).toBe("warn");
    expect(hpaLevel(40, 72)).toBe("ok");
    expect(hpaLevel(40, null)).toBe("info");
    expect(hpaLevel(null, 72)).toBe("info");
    expect(hpaLevel(0, 0)).toBe("ok");
  });
  it("throttling: >10% alerta, >25% erro", () => {
    expect(throttleLevel(null)).toBe("info");
    expect(throttleLevel(10)).toBe("ok");
    expect(throttleLevel(10.1)).toBe("warn");
    expect(throttleLevel(25.1)).toBe("red");
  });
  it("rede: descartes/erros >0,5/s é alerta", () => {
    expect(netLevel(null)).toBe("info");
    expect(netLevel(0.5)).toBe("ok");
    expect(netLevel(0.6)).toBe("warn");
  });
  it("bytes por segundo na unidade certa", () => {
    expect(fmtBytesPerSec(null)).toBe("sem dado");
    expect(fmtBytesPerSec(512)).toBe("512 B/s");
    expect(fmtBytesPerSec(1536)).toBe("1,5 KB/s");
    expect(fmtBytesPerSec(5 * 1024 * 1024)).toBe("5,0 MB/s");
    expect(fmtBytesPerSec(-3)).toBe("0 B/s");
  });
});

describe("buildNetworkRows (gráfico de rede)", () => {
  it("monta as linhas e diz se há dado e se há descartes/erros", () => {
    const r = buildNetworkRows({ times: [1000, 2000], rx_bps: [100, null], tx_bps: [50, 60], err_ps: [0, 0] });
    expect(r.rows).toEqual([{ t: 1000, rx: 100, tx: 50, err: 0 }, { t: 2000, rx: null, tx: 60, err: 0 }]);
    expect(r.hasData).toBe(true);
    expect(r.hasErrors).toBe(false);
    expect(buildNetworkRows({ times: [1], rx_bps: [1], tx_bps: [1], err_ps: [0.2] }).hasErrors).toBe(true);
  });
  it("aceita listas nulas do Go e ausência de resposta", () => {
    expect(buildNetworkRows({ times: null as any, rx_bps: null as any })).toEqual({ rows: [], hasData: false, hasErrors: false });
    expect(buildNetworkRows(undefined).hasData).toBe(false);
  });
  it("normalizeLive troca links nulos por lista vazia", () => {
    const r = normalizeLive({ series: [{ item: {}, times: null, rps: null, err_4xx_rps: null, err_5xx_rps: null, links: null }], warnings: null } as any);
    expect(r.series[0].links).toEqual([]);
  });
});

describe("motivo dos restarts e pods pendentes", () => {
  it("lista as razões da mais frequente para a menos", () => {
    expect(fmtRestartReasons({ Error: 1, OOMKilled: 2 })).toBe("OOMKilled ×2, Error ×1");
    expect(fmtRestartReasons({ B: 1, A: 1 })).toBe("A ×1, B ×1");
    expect(fmtRestartReasons(null)).toBe("");
    expect(fmtRestartReasons({})).toBe("");
  });
  it("pods pendentes ou não prontos dão alerta", () => {
    expect(podsLevel(null, null)).toBe("info");
    expect(podsLevel(0, 0)).toBe("ok");
    expect(podsLevel(1, 0)).toBe("warn");
    expect(podsLevel(0, 3)).toBe("warn");
    expect(podsLevel(null, 2)).toBe("warn");
  });
});

describe("média do tempo de resposta no gráfico", () => {
  const s = { cluster: "c", namespace: "n", workload: "w", times: [1000, 2000], rps: [1, 2], err_4xx_rps: [0, 0], err_5xx_rps: [0, 0], avg_ms: [120, null], p99_ms: null } as any;
  it("entra nas linhas e liga o gráfico de latência mesmo sem percentis", () => {
    const d = buildChartData(s, [99], 0);
    expect(d.rows.map((r) => r.avg)).toEqual([120, null]);
    expect(d.hasAvg).toBe(true);
    expect(d.hasLatency).toBe(false);
    expect(buildChartData({ ...s, avg_ms: null }, [99], 0).hasAvg).toBe(false);
  });
  it("normalizeLive troca avg_ms nulo por ausente e preserva lista", () => {
    const r = normalizeLive({ series: [{ item: {}, times: null, rps: null, err_4xx_rps: null, err_5xx_rps: null, avg_ms: null }], warnings: null } as any);
    expect(r.series[0].avg_ms).toBeNull();
    const r2 = normalizeLive({ series: [{ item: {}, times: [1], rps: [1], err_4xx_rps: [0], err_5xx_rps: [0], avg_ms: [5] }], warnings: null } as any);
    expect(r2.series[0].avg_ms).toEqual([5]);
  });
});
