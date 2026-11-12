import { describe, expect, it } from "vitest";
import { buildSessionCSV, buildSessionMarkdown, newSession, recordSample } from "./metricsLiveSession";
import type { LiveSeries } from "./metricsLive";

const mk = (over: Partial<LiveSeries> = {}): LiveSeries =>
  ({ cluster: "c1", namespace: "ns", workload: "frete-hub", times: [1], rps: [10], err_4xx_rps: [0.5], err_5xx_rps: [0], p50_ms: null, p95_ms: null, p99_ms: [200], err_4xx_total: 5, err_5xx_total: 0, ...over }) as LiveSeries;
const res = (o: object) => ({ replicas_available: 70, replicas_desired: 72, restarts_15m: 0, cpu_lim_pct: 40, mem_lim_pct: 50, pods: 70, ...o }) as any;

describe("resumo da sessão do Tempo real", () => {
  it("agrega mín/méd/máx por aplicação sem guardar séries", () => {
    const s = newSession(Date.UTC(2026, 10, 12, 12, 0));
    recordSample(s, [mk({ rps: [10], p99_ms: [200] })], 99);
    recordSample(s, [mk({ rps: [30], p99_ms: [900] })], 99);
    recordSample(s, [mk({ rps: [20], p99_ms: [400] })], 99);
    const a = [...s.apps.values()][0];
    expect(s.updates).toBe(3);
    expect(a.samples).toBe(3);
    expect(a.rps).toEqual({ n: 3, min: 10, max: 30, sum: 60 });
    expect(a.lat!.max).toBe(900);
    expect(s.apps.size).toBe(1);
  });

  it("nulo não vira zero: sem dado não entra na média", () => {
    const s = newSession(0);
    recordSample(s, [mk({ rps: [null] as any })], 99);
    expect([...s.apps.values()][0].rps).toBeNull();
  });

  it("conta teto do HPA, restarts (máximo), razões e pods pendentes", () => {
    const s = newSession(0);
    recordSample(s, [mk({ resources: res({ hpa_max: 72, replicas_desired: 72, restarts_15m: 2, restart_reasons: { OOMKilled: 1 }, pods_pending: 3 }) })], 99);
    recordSample(s, [mk({ resources: res({ hpa_max: 72, replicas_desired: 60, restarts_15m: 1, restart_reasons: { OOMKilled: 2, Error: 1 }, pods_not_ready: 4 }) })], 99);
    const a = [...s.apps.values()][0];
    expect(a.atCeiling).toBe(1);
    expect(a.restartsMax).toBe(2);
    expect(a.restartReasons).toEqual({ OOMKilled: 2, Error: 1 });
    expect(a.pendingMax).toBe(3);
    expect(a.notReadyMax).toBe(4);
  });

  it("falha de coleta conta como amostra com falha, sem sujar os agregados", () => {
    const s = newSession(0);
    recordSample(s, [mk(), mk({ workload: "x", error: "timeout", rps: [], p99_ms: null })], 99);
    const x = s.apps.get("c1|ns|x")!;
    expect(x.errors).toBe(1);
    expect(x.rps).toBeNull();
  });

  it("Markdown traz o aviso de amostragem e os valores; CSV tem colunas consistentes", () => {
    const s = newSession(Date.UTC(2026, 10, 12, 12, 0));
    recordSample(s, [mk({ resources: res({ hpa_max: 72, replicas_desired: 72 }) })], 99);
    recordSample(s, [mk({ rps: [30], resources: res({}) })], 99);
    const md = buildSessionMarkdown(s, Date.UTC(2026, 10, 12, 12, 30));
    expect(md).toContain("amostrados a cada atualização");
    expect(md).toContain("### frete-hub (ns: ns)");
    expect(md).toContain("mín 10 · méd 20 · máx 30");
    expect(md).toContain("no teto do HPA em 1 de 2 amostras");
    expect(md).toContain("duração 30 min");
    const lines = buildSessionCSV(s).replace(/^﻿/, "").split("\r\n").filter(Boolean);
    expect(lines).toHaveLength(2);
    expect(lines[0].split(";").length).toBe(lines[1].split(";").length);
    expect(lines[0]).toContain("p99_ms_max");
  });

  it("sessão vazia diz que nada foi acompanhado", () => {
    expect(buildSessionMarkdown(newSession(0), 60000)).toContain("Nenhuma aplicação foi acompanhada");
  });
});
