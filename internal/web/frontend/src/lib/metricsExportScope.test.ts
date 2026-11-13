import { describe, expect, it } from "vitest";
import { allKeys, clusterState, describeScope, filterByScope, scopeKey, scopeSlug, scopeTree, toggleCluster, toggleNamespace } from "./metricsExportScope";
import { buildCSV, buildMarkdown, buildExportTable } from "./metricsReportExport";
import { fileSlug } from "./metricsEvents";
import type { ReportResult, ReportRow } from "./metricsReport";

const row = (cluster: string, namespace: string, workload: string): ReportRow =>
  ({
    cluster, namespace, workload,
    traffic: { source: "dynatrace", requests: 100, max_rps: 5, avg_ms: 50, p50_peak_ms: 1, p95_peak_ms: 2, p99_peak_ms: 3, err_4xx: 0, err_5xx: 0, services: [], peak_approx: false, resolution: "1m" },
    usage: null, flags: {},
  }) as ReportRow;

const result = (): ReportResult =>
  ({
    window: { start: "2026-11-10T13:22:30Z", end: "2026-11-11T05:00:00Z" }, percentiles: [99],
    plan: { dt_resolution: "1m", p99_resolution: "5m", peak_approx: false }, warnings: [], generated_at: "2026-11-11T12:23:00Z",
    rows: [row("aks-b", "ns2", "w3"), row("aks-a", "ns1", "w1"), row("aks-a", "ns1", "w2"), row("aks-a", "ns2", "w4"), row("arn:aws:eks:us-east-1:1:cluster/c", "x", "w5")],
  }) as ReportResult;

describe("árvore de escopo", () => {
  const tree = scopeTree(result().rows);
  it("clusters e namespaces em ordem, com a contagem de aplicações", () => {
    expect(tree.map((c) => [c.cluster, c.count])).toEqual([["aks-a", 3], ["aks-b", 1], ["arn:aws:eks:us-east-1:1:cluster/c", 1]]);
    expect(tree[0].namespaces).toEqual([{ namespace: "ns1", count: 2 }, { namespace: "ns2", count: 1 }]);
    expect(scopeTree([])).toEqual([]);
  });
  it("tudo marcado de partida, e o estado do cluster", () => {
    const all = allKeys(tree);
    expect(all.size).toBe(4);
    expect(clusterState(tree, all, "aks-a")).toBe("all");
    expect(clusterState(tree, new Set(), "aks-a")).toBe("none");
    expect(clusterState(tree, new Set([scopeKey("aks-a", "ns1")]), "aks-a")).toBe("some");
    expect(clusterState(tree, all, "não existe")).toBe("none");
  });
  it("marcar e desmarcar cluster e namespace devolve outro conjunto, sem mexer no original", () => {
    const all = allKeys(tree);
    const semA = toggleCluster(tree, all, "aks-a", false);
    expect(semA.size).toBe(2);
    expect(all.size).toBe(4);
    expect(clusterState(tree, semA, "aks-a")).toBe("none");
    expect(clusterState(tree, toggleCluster(tree, semA, "aks-a", true), "aks-a")).toBe("all");
    const so1 = toggleNamespace(toggleCluster(tree, all, "aks-a", false), "aks-a", "ns1", true);
    expect(clusterState(tree, so1, "aks-a")).toBe("some");
  });
  it("a chave não colide quando o nome tem separador", () => {
    expect(scopeKey("a|b", "c")).not.toBe(scopeKey("a", "b|c"));
  });
});

describe("filtrar o relatório pelo escopo", () => {
  it("tudo marcado devolve o mesmo relatório, sem aviso de parcial", () => {
    const r = result();
    const out = filterByScope(r, allKeys(scopeTree(r.rows)));
    expect(out).toBe(r);
    expect(out.scope_note).toBeUndefined();
  });
  it("só um cluster: as linhas dele e a nota de parcial; o original não muda", () => {
    const r = result();
    const tree = scopeTree(r.rows);
    const out = filterByScope(r, toggleCluster(tree, new Set(), "aks-a", true));
    expect(out.rows.map((x) => x.workload).sort()).toEqual(["w1", "w2", "w4"]);
    expect(out.scope_note).toBe("1 de 3 clusters, 3 de 5 aplicações: aks-a (todos os namespaces)");
    expect(r.rows).toHaveLength(5);
  });
  it("só alguns namespaces de um cluster", () => {
    const r = result();
    const tree = scopeTree(r.rows);
    const sel = toggleNamespace(new Set(), "aks-a", "ns1", true);
    const out = filterByScope(r, sel);
    expect(out.rows.map((x) => x.workload).sort()).toEqual(["w1", "w2"]);
    expect(out.scope_note).toContain("aks-a (ns1)");
    expect(describeScope(tree, sel)).toMatchObject({ partial: true, clusters: 1, apps: 2, totalApps: 5, totalClusters: 3 });
  });
  it("dois clusters, cada um inteiro ou em parte", () => {
    const r = result();
    const tree = scopeTree(r.rows);
    let sel = toggleCluster(tree, new Set(), "aks-b", true);
    sel = toggleNamespace(sel, "aks-a", "ns2", true);
    expect(describeScope(tree, sel).note).toBe("2 de 3 clusters, 2 de 5 aplicações: aks-a (ns2); aks-b (todos os namespaces)");
  });
  it("nada marcado: zero aplicações", () => {
    const r = result();
    const out = filterByScope(r, new Set());
    expect(out.rows).toEqual([]);
    expect(out.scope_note).toContain("0 de 3 clusters, 0 de 5 aplicações");
  });
});

describe("os documentos refletem o escopo", () => {
  const r = result();
  const tree = scopeTree(r.rows);
  const parcial = filterByScope(r, toggleCluster(tree, new Set(), "aks-a", true));
  it("Markdown: só o cluster escolhido e o aviso de exportação parcial", () => {
    const md = buildMarkdown(parcial);
    expect(md).toContain("- **Escopo:** exportação **parcial** — 1 de 3 clusters, 3 de 5 aplicações: aks-a (todos os namespaces)");
    expect(md).toContain("## aks-a");
    expect(md).not.toContain("## aks-b");
    expect(md).not.toContain("w3");
  });
  it("Markdown completo não tem a linha de escopo", () => {
    expect(buildMarkdown(r)).not.toContain("**Escopo:**");
    expect(buildMarkdown(r)).toContain("## aks-b");
  });
  it("CSV: só as linhas do escopo", () => {
    const lines = buildCSV(parcial).replace(/^\uFEFF/, "").split("\r\n").filter(Boolean);
    expect(lines).toHaveLength(1 + 3);
    expect(lines.slice(1).every((l) => l.includes('"aks-a"'))).toBe(true);
  });
  it("a tabela de exportação leva o escopo no meta", () => {
    expect(buildExportTable(parcial).meta.scope).toContain("aks-a");
    expect(buildExportTable(r).meta.scope).toBe("");
  });
});

describe("nome do arquivo", () => {
  const tree = scopeTree(result().rows);
  it("vazio com tudo marcado; cluster inteiro; cluster em parte; vários", () => {
    expect(scopeSlug(tree, allKeys(tree), fileSlug)).toBe("");
    expect(scopeSlug(tree, toggleCluster(tree, new Set(), "aks-a", true), fileSlug)).toBe("aks-a");
    expect(scopeSlug(tree, toggleNamespace(new Set(), "aks-a", "ns1", true), fileSlug)).toBe("aks-a-parcial");
    expect(scopeSlug(tree, toggleCluster(tree, toggleCluster(tree, new Set(), "aks-a", true), "aks-b", true), fileSlug)).toBe("parcial");
  });
  it("cluster de EKS (ARN) vira um nome de arquivo seguro", () => {
    const s = scopeSlug(tree, toggleCluster(tree, new Set(), "arn:aws:eks:us-east-1:1:cluster/c", true), fileSlug);
    expect(s).toBe("arn-aws-eks-us-east-1-1-cluster-c");
    expect(s).toMatch(/^[a-z0-9-]+$/);
  });
});
