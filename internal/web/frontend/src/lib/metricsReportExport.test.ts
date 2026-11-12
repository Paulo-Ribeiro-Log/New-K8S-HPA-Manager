import { describe, expect, it, vi } from "vitest";
import { EXTRAS_COLUMNS } from "./metricsReport";
import { NODE_COLUMNS, buildCSV, buildReportPDF, buildExportTable, buildMarkdown, csvText, escapeMdCell, fileStamp, fmtWhen, pdfSafe } from "./metricsReportExport";
import type { ReportResult, ReportRow } from "./metricsReport";

const traffic = (o: Partial<NonNullable<ReportRow["traffic"]>> = {}): NonNullable<ReportRow["traffic"]> => ({
  source: "dynatrace", requests: 2087668.4, max_rps: 158.74, p50_peak_ms: 45.2, p95_peak_ms: 820, p99_peak_ms: 1501.3, p99_service: "FreteController",
  err_4xx: 181344, err_5xx: 9, services: ["CotacaoController", "FreteController"], peak_approx: false, resolution: "1m", ...o,
});
const usage = () => ({
  source: "prometheus", step: "1m", cpu_req_pct: { max: 3.3, min: 0.4, avg: 2.6 }, cpu_lim_pct: null,
  mem_req_pct: { max: 78.2, min: 77.7, avg: 78 }, mem_lim_pct: { max: 49.5, min: 49.2, avg: 49.4 },
});
const row = (cluster: string, namespace: string, workload: string, over: Partial<ReportRow> = {}): ReportRow => ({
  cluster, namespace, workload, traffic: traffic(), usage: usage(), flags: { p99: "red", err_4xx: "warn", err_5xx: "red" }, errors: [], ...over,
});

function fixture(over: Partial<ReportResult> = {}): ReportResult {
  return {
    window: { start: "2026-11-10T13:22:30Z", end: "2026-11-11T05:00:00Z" },
    percentiles: [50, 95, 99],
    plan: { dt_resolution: "1m", p99_resolution: "5m", peak_approx: false },
    warnings: [],
    generated_at: "2026-11-11T12:23:00Z",
    rows: [
      row("c2", "ns-b", "z-app", { flags: {}, traffic: traffic({ err_4xx: 0, err_5xx: 0, p99_peak_ms: 26 }) }),
      row("c1", "calculo-de-fretes-prd", "frete-hub"),
      row("c1", "calculo-de-fretes-prd", "frete-b2c", { traffic: null, usage: null, flags: {}, errors: ["Dynatrace: nenhum process group casa"] }),
    ],
    ...over,
  };
}

describe("formatação de datas", () => {
  it("usa o fuso do relatório", () => {
    expect(fmtWhen("2026-10-02T02:00:00Z")).toBe("01/10/2026 23:00");
    expect(fileStamp("2026-11-11T12:23:00Z")).toBe("2026-11-11_0923");
  });
});

describe("buildExportTable", () => {
  const t = buildExportTable(fixture());

  it("tem só as colunas dos percentis coletados, na ordem", () => {
    expect(t.columns).toEqual([
      "Aplicação", "Total Requests", "Max RPS", "Latência P50 (pico)", "Latência P95 (pico)", "Latência P99 (pico)", "Erros 4xx", "Erros 5xx",
      "CPU % do request (Máx/Mín/Méd)", "CPU % do limit (Máx/Mín/Méd)", "Mem % do request (Máx/Mín/Méd)", "Mem % do limit (Máx/Mín/Méd)",
    ]);
    const so99 = buildExportTable(fixture({ percentiles: [99] }));
    expect(so99.columns.filter((c) => c.startsWith("Latência"))).toEqual(["Latência P99 (pico)"]);
    const sem99 = buildExportTable(fixture({ percentiles: [50, 95] }));
    expect(sem99.columns.some((c) => c.includes("P99"))).toBe(false);
  });

  it("agrupa por cluster, ordenado, no formato do relatório de referência", () => {
    expect(t.groups.map((g) => g.cluster)).toEqual(["c1", "c2"]);
    expect(t.groups[0].rows.map((r) => r.cells[0])).toEqual(["frete-b2c (ns: calculo-de-fretes-prd)", "frete-hub (ns: calculo-de-fretes-prd)"]);
    const hub = t.groups[0].rows[1].cells;
    expect(hub.slice(0, 8)).toEqual(["frete-hub (ns: calculo-de-fretes-prd)", "2087668", "158.7", "0.045s", "0.820s", "1.501s", "181344", "9"]);
    expect(hub[8]).toBe("3.3% (Min: 0.4%, Avg: 2.6%)");
  });

  it("sem dado aparece escrito e nunca vira zero", () => {
    const semDado = t.groups[0].rows[0].cells;
    expect(semDado.slice(1)).toEqual(Array(11).fill("sem dado"));
    expect(t.groups[0].rows[1].cells[9]).toBe("sem dado"); // cpu % do limit nulo
  });

  it("os selos ficam na coluna certa", () => {
    const hub = t.groups[0].rows[1];
    expect(hub.flags[t.columns.indexOf("Latência P99 (pico)")]).toBe("red");
    expect(hub.flags[t.columns.indexOf("Erros 4xx")]).toBe("warn");
    expect(hub.flags[t.columns.indexOf("Erros 5xx")]).toBe("red");
    expect(t.groups[1].rows[0].flags).toEqual({});
  });

  it("junta os erros de coleta e descreve a janela e as fontes", () => {
    expect(t.issues).toEqual(["c1 / calculo-de-fretes-prd / frete-b2c: Dynatrace: nenhum process group casa"]);
    expect(t.meta.window).toContain("America/Sao_Paulo");
    expect(t.meta.sources).toBe("Dynatrace (tráfego e latência) + Prometheus (CPU e memória)");
    expect(t.meta.percentiles).toEqual([50, 95, 99]);
  });
});

describe("buildMarkdown", () => {
  const md = buildMarkdown(fixture());

  it("tem cabeçalho com janela, resolução, percentis e fontes", () => {
    expect(md).toContain("# Relatório de Métricas");
    expect(md).toContain("- **Janela:** 10/11/2026 10:22 → 11/11/2026 02:00 (America/Sao_Paulo)");
    expect(md).toContain("- **Resolução:** 1m (percentis 5m)");
    expect(md).toContain("- **Percentis coletados:** P50, P95, P99");
    expect(md).not.toContain("pico aproximado");
  });

  it("gera uma tabela por cluster, com separador e alinhamento", () => {
    expect(md).toContain("## c1");
    expect(md).toContain("## c2");
    const header = md.split("\n").find((l) => l.startsWith("| Aplicação"))!;
    const cols = header.split(" | ").length;
    const sep = md.split("\n").find((l) => l.startsWith("|---|"))!;
    expect(sep.split("|").length - 2).toBe(cols); // separador com o mesmo número de colunas
    expect(sep).toBe("|---|" + Array(cols - 1).fill("---:").join("|") + "|");
  });

  it("marca os selos com emoji e escreve sem dado", () => {
    const hubLine = md.split("\n").find((l) => l.startsWith("| frete-hub"))!;
    expect(hubLine).toContain("1.501s 🔴");
    expect(hubLine).toContain("181344 ⚠️");
    expect(hubLine).toContain("| 9 🔴 |");
    expect(md.split("\n").find((l) => l.startsWith("| frete-b2c"))).toContain("sem dado");
    expect(md).toContain("### Observações");
    expect(md).toContain("- c1 / calculo-de-fretes-prd / frete-b2c: Dynatrace: nenhum process group casa");
  });

  it("avisa quando o pico é aproximado e repete os avisos da coleta", () => {
    const m = buildMarkdown(fixture({ plan: { dt_resolution: "1h", p99_resolution: "1h", peak_approx: true }, warnings: ["fim cortado em agora"] }));
    expect(m).toContain("**pico aproximado**");
    expect(m).toContain("- **Aviso:** fim cortado em agora");
  });

  it("escapa o que quebraria a tabela", () => {
    expect(escapeMdCell("a|b\nc\\d")).toBe("a\\|b c\\\\d");
    const m = buildMarkdown(fixture({ rows: [row("c|x", "ns", "app|y")] }));
    expect(m).toContain("## c\\|x");
    expect(m).toContain("| app\\|y (ns: ns) |");
  });
});

describe("buildCSV", () => {
  const csv = buildCSV(fixture());
  const lines = csv.split("\r\n").filter(Boolean);

  it("começa com BOM UTF-8, usa ; por padrão e termina com quebra de linha", () => {
    expect(csv.charCodeAt(0)).toBe(0xfeff);
    expect(csv.endsWith("\r\n")).toBe(true);
    expect(buildCSV(fixture(), ",").split("\r\n")[0]).toContain('"cluster","namespace"');
    expect(lines[0]).toContain('"cluster";"namespace";"deployment";"requisicoes";"max_rps";"p50_pico_ms";"p95_pico_ms";"p99_pico_ms";"p99_servico"');
  });

  it("um registro por aplicação, ordenado, com número cru e ponto decimal", () => {
    expect(lines).toHaveLength(1 + 3);
    const hub = lines.find((l) => l.includes('"frete-hub"'))!.split(";");
    expect(hub.slice(0, 9)).toEqual(['"c1"', '"calculo-de-fretes-prd"', '"frete-hub"', "2087668", "158.7", "45.2", "820", "1501.3", '"FreteController"']);
    expect(hub[9]).toBe("181344");
  });

  it("sem dado é célula vazia, nunca zero", () => {
    const b2c = lines.find((l) => l.includes('"frete-b2c"'))!.split(";");
    expect(b2c.slice(3, 9)).toEqual(["", "", "", "", "", '""']);
    expect(b2c.slice(9, 12)).toEqual(["", "", ""]); // 4xx, 5xx, cpu req max
  });

  it("registra janela, alertas e erros de coleta", () => {
    const b2c = lines.find((l) => l.includes('"frete-b2c"'))!;
    expect(b2c).toContain('"Dynatrace: nenhum process group casa"');
    expect(b2c).toContain('"2026-11-10T13:22:30Z";"2026-11-11T05:00:00Z"');
    expect(lines.find((l) => l.includes('"frete-hub"'))).toContain('"vermelho";"aviso";"vermelho"');
  });

  it("protege contra fórmula e dobra as aspas", () => {
    expect(csvText("=HYPERLINK(\"x\")")).toBe('"\'=HYPERLINK(""x"")"');
    expect(csvText("+1")).toBe("\"'+1\"");
    expect(csvText("-2")).toBe("\"'-2\"");
    expect(csvText("@soma")).toBe("\"'@soma\"");
    expect(csvText("normal")).toBe('"normal"');
    expect(csvText("com;ponto")).toBe('"com;ponto"');
    const evil = buildCSV(fixture({ rows: [row("c", "ns", "=cmd|' /C calc'!A0")] }));
    expect(evil).toContain("\"'=cmd|' /C calc'!A0\"");
  });

  it("só tem a coluna do serviço do P99 quando o P99 foi coletado", () => {
    expect(buildCSV(fixture({ percentiles: [50] })).split("\r\n")[0]).not.toContain("p99_servico");
  });
});

describe("pdfSafe", () => {
  it("troca símbolos que a fonte do PDF não tem e descarta emoji", () => {
    expect(pdfSafe("a → b ≥ 1 … ≈ 2")).toBe("a -> b >= 1 ... ~ 2");
    expect(pdfSafe("🔴 alerta ⚠️")).toBe(" alerta ");
    expect(pdfSafe("Aplicação São João — ok")).toBe("Aplicação São João - ok"); // Latin-1 se mantém
  });
});

// O logoUtils carrega a imagem pelo DOM; no teste (node) ele é trocado por um cabeçalho simples.
vi.mock("./logoUtils", () => ({
  addLogoHeaderToPDF: async () => 40,
  addFooterToPDF: () => undefined,
}));

describe("buildReportPDF", () => {
  it("gera um PDF de verdade, com título, janela, tabela por cluster e observações", async () => {
    const doc = await buildReportPDF(fixture());
    const raw = doc.output();
    expect(raw.startsWith("%PDF-")).toBe(true);
    expect(doc.getNumberOfPages()).toBeGreaterThanOrEqual(1);
    expect(doc.internal.pageSize.getWidth()).toBeGreaterThan(doc.internal.pageSize.getHeight()); // paisagem
    for (const trecho of ["frete-hub", "calculo-de-fretes-prd", "1.501s", "2087668", "sem dado", "Observacoes", "Dynatrace: nenhum process group casa"]) {
      expect(raw, trecho).toContain(trecho);
    }
    // o que o PDF não suporta (emoji, setas) não pode vazar para o arquivo
    expect(raw).not.toContain("🔴");
  });

  it("uma tabela só com os percentis coletados", async () => {
    const raw = (await buildReportPDF(fixture({ percentiles: [95] }))).output();
    // dentro do PDF os parênteses do texto saem escapados
    expect(raw).toContain("P95 \\(pico\\)");
    expect(raw).not.toContain("P99 \\(pico\\)");
    expect(raw).not.toContain("P50 \\(pico\\)");
  });
});

describe("contexto de node nas exportações", () => {
  const node = {
    source: "prometheus", nodes: 27, pools: [{ name: "calculofret2", nodes: 27, instance_type: "Standard_D4s_v5" }], pool_label: "label_agentpool",
    cpu: { nodes_with_data: 27, worst_pct: 76.4, avg_pct: 22.14 }, mem: { nodes_with_data: 27, worst_pct: 42.2, avg_pct: 36.8 },
    throttle_pct: { max: 0.00506, min: 0, avg: 0.0007 }, step: "1m", notes: ["uso de CPU de 9 dos 27 nodes"],
  };
  const comNode = () => {
    const f = fixture({ node_context: true });
    f.rows = [row("c1", "ns", "frete-hub", { node }), row("c1", "ns", "sem-node", { node: null })];
    return f;
  };

  it("sem o contexto coletado, nenhuma coluna de node aparece", () => {
    const t = buildExportTable(fixture());
    expect(t.columns.some((c) => c.startsWith("Node") || c.startsWith("Throttling"))).toBe(false);
    expect(buildCSV(fixture()).split("\r\n")[0]).not.toContain("node_pool");
    expect(buildMarkdown(fixture())).not.toContain("Contexto de node");
  });

  it("com o contexto, a tabela ganha as 4 colunas no fim, e 'sem dado' quando a linha não tem", () => {
    const t = buildExportTable(comNode());
    expect(t.columns.slice(-4)).toEqual(NODE_COLUMNS);
    const rows = t.groups[0].rows;
    const hub = rows.find((r) => r.cells[0].startsWith("frete-hub"))!;
    const semNode = rows.find((r) => r.cells[0].startsWith("sem-node"))!;
    expect(hub.cells.slice(-4)).toEqual(["calculofret2 (27 nodes, Standard_D4s_v5)", "76.4% / 22.1%", "42.2% / 36.8%", "0.01% / 0.00%"]);
    expect(semNode.cells.slice(-4)).toEqual(Array(4).fill("sem dado"));
    expect(t.meta.nodeContext).toBe(true);
    expect(t.issues).toContain("c1 / ns / frete-hub (node): uso de CPU de 9 dos 27 nodes");
  });

  it("o Markdown explica o que é pior e média e traz as colunas", () => {
    const md = buildMarkdown(comNode());
    expect(md).toContain("- **Contexto de node:** incluído");
    expect(md).toContain("Node CPU % (pior/média)");
    const linha = md.split("\n").find((l) => l.startsWith("| frete-hub"))!;
    expect(linha).toContain("calculofret2 (27 nodes, Standard_D4s_v5) | 76.4% / 22.1% | 42.2% / 36.8% | 0.01% / 0.00% |");
  });

  it("o CSV tem as colunas de node com número cru e vazio no que não tem dado", () => {
    const csv = buildCSV(comNode());
    const lines = csv.split("\r\n").filter(Boolean);
    expect(lines[0]).toContain('"node_pool";"nodes";"nodes_com_dado_cpu";"node_cpu_pior_pct";"node_cpu_med_pct";"node_mem_pior_pct";"node_mem_med_pct";"throttling_max_pct";"throttling_med_pct"');
    const header = lines[0].split(";");
    const hub = lines.find((l) => l.includes('"frete-hub"'))!.split(";");
    const at = (c: string) => hub[header.indexOf(`"${c}"`)];
    expect([at("node_pool"), at("nodes"), at("nodes_com_dado_cpu"), at("node_cpu_pior_pct"), at("node_cpu_med_pct"), at("throttling_max_pct")]).toEqual(['"calculofret2"', "27", "27", "76.4", "22.1", "0.005"]);
    const sem = lines.find((l) => l.includes('"sem-node"'))!.split(";");
    for (const c of ["nodes", "node_cpu_pior_pct", "node_mem_med_pct", "throttling_max_pct"]) expect(sem[header.indexOf(`"${c}"`)], c).toBe(""); // vazio, nunca 0
    expect(sem[header.indexOf('"node_pool"')]).toBe('""');
    // todas as linhas têm o mesmo número de colunas do cabeçalho
    for (const l of lines) expect(l.split(";").length).toBe(header.length);
  });

  it("o PDF também sai com o contexto de node", async () => {
    const raw = (await buildReportPDF(comNode())).output();
    expect(raw).toContain("calculofret2");
    expect(raw).toContain("Contexto de node incluido");
    expect(raw).toContain("Throttling CPU");
  });
});

describe("extras (réplicas, HPA, restarts e rede) nas exportações", () => {
  const extras = {
    source: "prometheus", step: "1m", replicas: { max: 72, min: 10, avg: 40 }, replicas_desired: { max: 72, min: 10, avg: 40 }, hpa_min: 10, hpa_max: 72, restarts: 3,
    net_rx_bps: { max: 3 * 1024 * 1024, min: 0, avg: 2 * 1024 * 1024 }, net_tx_bps: { max: 1024 * 1024, min: 0, avg: 512 * 1024 }, net_err_ps: { max: 1.5, min: 0, avg: 0.2 }, notes: ["hpa_max: sem série"],
  };
  const comExtras = () => {
    const f = fixture({ extras: true });
    f.rows = [row("c1", "ns", "frete-hub", { extras }), row("c1", "ns", "sem-extras", { extras: null })];
    return f;
  };

  it("sem os extras coletados, nenhuma coluna deles aparece", () => {
    expect(buildExportTable(fixture()).columns).not.toContain(EXTRAS_COLUMNS[0]);
    expect(buildCSV(fixture()).split("\r\n")[0]).not.toContain("replicas_max");
  });

  it("com os extras, a tabela ganha as colunas e 'sem dado' quando a linha não tem", () => {
    const t = buildExportTable(comExtras());
    expect(t.columns.slice(-EXTRAS_COLUMNS.length)).toEqual(EXTRAS_COLUMNS);
    const rows = t.groups[0].rows;
    const hub = rows.find((r) => r.cells[0].startsWith("frete-hub"))!;
    const sem = rows.find((r) => r.cells[0].startsWith("sem-extras"))!;
    expect(hub.cells.length).toBe(t.columns.length);
    expect(hub.cells.slice(-EXTRAS_COLUMNS.length)).toEqual(["72 (Mín: 10, Méd: 40)", "10-72", "3", "3,0 MB/s / 2,0 MB/s", "1,0 MB/s / 512,0 KB/s", "1,5/s"]);
    expect(sem.cells.slice(-EXTRAS_COLUMNS.length)).toEqual(Array(EXTRAS_COLUMNS.length).fill("sem dado"));
    expect(t.meta.extras).toBe(true);
    expect(t.issues).toContain("c1 / ns / frete-hub (extras): hpa_max: sem série");
    expect(buildMarkdown(comExtras())).toContain("Réplicas, HPA, restarts e rede");
  });

  it("no CSV, cabeçalho e linhas têm o mesmo número de colunas e o sem dado é vazio", () => {
    const lines = buildCSV(comExtras()).replace(/^﻿/, "").split("\r\n").filter(Boolean);
    const n = lines[0].split(";").length;
    expect(lines.slice(1).every((l) => l.split(";").length === n)).toBe(true);
    expect(lines[0]).toContain("hpa_max");
  });
});
