// Exportação do relatório de métricas (CSV, Markdown e PDF), 100% no navegador a partir do resultado.
// Lógica pura e testada em metricsReportExport.test.ts; só `downloadText`/`exportReportPDF` tocam o DOM.
// O Markdown e o PDF partem da MESMA tabela (buildExportTable), então mostram os mesmos números.

import { EXTRAS_COLUMNS, REPORT_TZ, extrasCells, fmtLatency, fmtNodeUsage, fmtPct, fmtPctStats, fmtPools, fmtThrottle, percentileValue, type ReportPct, type ReportResult, type ReportRow } from "./metricsReport";

export const NO_DATA = "sem dado";

// ── formatação "de relatório": número cru, ponto decimal, sem separador de milhar (como o exemplo) ───

const int = (v: number | null | undefined) => (v == null ? NO_DATA : String(Math.round(v)));
const rps = (v: number | null | undefined) => (v == null ? NO_DATA : (Math.round(v * 10) / 10).toFixed(1));

/** Data e hora no fuso do relatório, ex.: "10/11/2026 10:22" (montada à mão: o toLocaleString varia entre navegadores). */
export function fmtWhen(iso: string, tz: string = REPORT_TZ): string {
  const parts = new Intl.DateTimeFormat("en-GB", { timeZone: tz, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).formatToParts(new Date(iso));
  const g = (t: string) => parts.find((p) => p.type === t)?.value ?? "00";
  return `${g("day")}/${g("month")}/${g("year")} ${g("hour")}:${g("minute")}`;
}

/** Sufixo de data para o nome do arquivo no fuso do relatório, ex.: "2026-11-11_0923". */
export function fileStamp(iso: string, tz: string = REPORT_TZ): string {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: tz, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).formatToParts(new Date(iso));
  const g = (t: string) => parts.find((p) => p.type === t)?.value ?? "00";
  return `${g("year")}-${g("month")}-${g("day")}_${g("hour")}${g("minute")}`;
}

// ── tabela comum ─────────────────────────────────────────────────────────────────────────────────────

export type ExportFlag = "red" | "warn";

export interface ExportRowModel {
  cells: string[];
  /** Selo por coluna (índice em `columns`). */
  flags: Record<number, ExportFlag>;
}

export interface ExportTable {
  columns: string[];
  groups: { cluster: string; rows: ExportRowModel[] }[];
  /** Linhas com erro de coleta, para a seção de observações. */
  issues: string[];
  meta: {
    window: string;
    resolution: string;
    percentiles: number[];
    generatedAt: string;
    warnings: string[];
    peakApprox: boolean;
    sources: string;
    nodeContext: boolean;
    extras: boolean;
  };
}

/** Colunas do contexto de node (só existem quando o relatório o coletou). */
export const NODE_COLUMNS = ["Node pool", "Node CPU % (pior/média)", "Node Mem % (pior/média)", "Throttling CPU % (máx/méd)"];

const flagOf = (f?: string): ExportFlag | undefined => (f === "red" || f === "warn" ? f : undefined);

function usedSources(rows: ReportRow[]): string {
  const s = new Set<string>();
  for (const r of rows) {
    if (r.traffic?.source) s.add(r.traffic.source);
    if (r.usage?.source) s.add(r.usage.source);
  }
  const label: Record<string, string> = { dynatrace: "Dynatrace (tráfego e latência)", prometheus: "Prometheus (CPU e memória)" };
  return [...s].map((x) => label[x] ?? x).join(" + ") || "nenhuma";
}

/** Monta a tabela agrupada por cluster, com as colunas do relatório de referência e só os percentis coletados. */
export function buildExportTable(result: ReportResult): ExportTable {
  const pcts = result.percentiles?.length ? result.percentiles : [99];
  const columns = [
    "Aplicação", "Total Requests", "Max RPS", "Resposta média", ...pcts.map((p) => `Latência P${p} (pico)`), "Erros 4xx", "Erros 5xx",
    "CPU % do request (Máx/Mín/Méd)", "CPU % do limit (Máx/Mín/Méd)", "Mem % do request (Máx/Mín/Méd)", "Mem % do limit (Máx/Mín/Méd)",
    ...(result.node_context ? NODE_COLUMNS : []),
    ...(result.extras ? EXTRAS_COLUMNS : []),
  ];
  const p99col = columns.indexOf("Latência P99 (pico)");
  const col4 = columns.indexOf("Erros 4xx");
  const col5 = columns.indexOf("Erros 5xx");

  const byCluster = new Map<string, ReportRow[]>();
  for (const r of result.rows) byCluster.set(r.cluster, [...(byCluster.get(r.cluster) ?? []), r]);
  const cmp = (a: string, b: string) => a.localeCompare(b, "pt-BR");

  const issues: string[] = [];
  const groups = [...byCluster.entries()]
    .sort(([a], [b]) => cmp(a, b))
    .map(([cluster, rows]) => ({
      cluster,
      rows: [...rows]
        .sort((a, b) => cmp(a.namespace, b.namespace) || cmp(a.workload, b.workload))
        .map((r): ExportRowModel => {
          const t = r.traffic;
          const u = r.usage;
          const flags: Record<number, ExportFlag> = {};
          const f99 = flagOf(r.flags?.p99);
          if (f99 && p99col >= 0) flags[p99col] = f99;
          const f4 = flagOf(r.flags?.err_4xx);
          if (f4) flags[col4] = f4;
          const f5 = flagOf(r.flags?.err_5xx);
          if (f5) flags[col5] = f5;
          for (const n of r.node?.notes ?? []) issues.push(`${r.cluster} / ${r.namespace} / ${r.workload} (node): ${n}`);
          for (const n of r.extras?.notes ?? []) issues.push(`${r.cluster} / ${r.namespace} / ${r.workload} (extras): ${n}`);
          for (const e of r.errors ?? []) issues.push(`${r.cluster} / ${r.namespace} / ${r.workload}: ${e.length > 300 ? `${e.slice(0, 300)}…` : e}`);
          return {
            cells: [
              `${r.workload} (ns: ${r.namespace})`, int(t?.requests), rps(t?.max_rps), fmtLatency(t?.avg_ms),
              ...pcts.map((p) => fmtLatency(percentileValue(t, p))),
              int(t?.err_4xx), int(t?.err_5xx),
              fmtPctStats(u?.cpu_req_pct), fmtPctStats(u?.cpu_lim_pct), fmtPctStats(u?.mem_req_pct), fmtPctStats(u?.mem_lim_pct),
              ...(result.node_context ? [fmtPools(r.node?.pools), fmtNodeUsage(r.node?.cpu, r.node?.nodes), fmtNodeUsage(r.node?.mem, r.node?.nodes), fmtThrottle(r.node?.throttle_pct)] : []),
              ...(result.extras ? extrasCells(r.extras) : []),
            ],
            flags,
          };
        }),
    }));

  return {
    columns,
    groups,
    issues,
    meta: {
      window: `${fmtWhen(result.window.start)} → ${fmtWhen(result.window.end)} (${REPORT_TZ})`,
      resolution: `${result.plan.dt_resolution} (percentis ${result.plan.p99_resolution})`,
      percentiles: pcts,
      generatedAt: fmtWhen(result.generated_at),
      warnings: result.warnings ?? [],
      peakApprox: !!result.plan.peak_approx,
      sources: usedSources(result.rows),
      nodeContext: !!result.node_context,
      extras: !!result.extras,
    },
  };
}

// ── Markdown ─────────────────────────────────────────────────────────────────────────────────────────

/** Texto seguro dentro de uma célula de tabela Markdown: sem `|` solto e sem quebra de linha. */
export function escapeMdCell(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

const MD_ICON: Record<ExportFlag, string> = { red: " 🔴", warn: " ⚠️" };

/** Markdown pronto para colar em um relatório: cabeçalho, uma tabela por cluster, legenda e observações. */
export function buildMarkdown(result: ReportResult): string {
  const t = buildExportTable(result);
  const out: string[] = [];
  out.push("# Relatório de Métricas", "");
  out.push(`- **Janela:** ${t.meta.window}`);
  out.push(`- **Resolução:** ${t.meta.resolution}${t.meta.peakApprox ? " — **pico aproximado**: a resolução disponível suaviza picos curtos" : ""}`);
  out.push(`- **Percentis coletados:** ${t.meta.percentiles.map((p) => `P${p}`).join(", ")}`);
  out.push(`- **Fontes:** ${t.meta.sources}`);
  if (t.meta.extras) out.push("- **Réplicas, HPA, restarts e rede:** incluídos (Prometheus, ~14 dias de retenção). Réplicas = disponíveis na janela; HPA = limites do HPA do Deployment (vazio se não houver HPA com esse alvo ou nome); restarts = soma na janela; rede = bytes/s somando os pods (pico / média). São contexto, não veredito.");
  if (t.meta.nodeContext) out.push("- **Contexto de node:** incluído (Prometheus, ~14 dias de retenção). \"Pior\" = maior pico entre os nodes que hospedaram a aplicação; \"média\" = média das médias de cada node. É contexto, não veredito.");
  out.push(`- **Gerado em:** ${t.meta.generatedAt}`);
  for (const w of t.meta.warnings) out.push(`- **Aviso:** ${escapeMdCell(w)}`);
  out.push("");

  for (const g of t.groups) {
    out.push(`## ${escapeMdCell(g.cluster)}`, "");
    out.push(`| ${t.columns.map(escapeMdCell).join(" | ")} |`);
    out.push(`|${t.columns.map((_, i) => (i === 0 ? "---" : "---:")).join("|")}|`);
    for (const r of g.rows) {
      const cells = r.cells.map((c, i) => escapeMdCell(c) + (r.flags[i] ? MD_ICON[r.flags[i]] : ""));
      out.push(`| ${cells.join(" | ")} |`);
    }
    out.push("");
  }

  out.push('> 🔴 P99 acima de 1 s ou qualquer 5xx · ⚠️ qualquer 4xx · "sem dado" = a fonte não devolveu o valor (não é zero).', "");
  if (t.issues.length > 0) {
    out.push("### Observações", "");
    for (const i of t.issues) out.push(`- ${escapeMdCell(i)}`);
    out.push("");
  }
  return out.join("\n");
}

// ── CSV ──────────────────────────────────────────────────────────────────────────────────────────────

export type CsvDelimiter = ";" | ",";

/**
 * Célula de texto do CSV: sempre entre aspas e com a defesa contra injeção de fórmula (uma célula que
 * começa com = + - @ ou tabulação/retorno é interpretada como fórmula pelo Excel): ganha um apóstrofo.
 */
export function csvText(s: string): string {
  const safe = /^[=+\-@\t\r]/.test(s) ? `'${s}` : s;
  return `"${safe.replace(/"/g, '""')}"`;
}

const csvNum = (v: number | null | undefined, decimals = 0) => (v == null ? "" : String(Math.round(v * 10 ** decimals) / 10 ** decimals));
const csvPct = (p: ReportPct | null | undefined): string[] => (p == null ? ["", "", ""] : [csvNum(p.max, 1), csvNum(p.min, 1), csvNum(p.avg, 1)]);
const csvFlag = (f?: string) => (f === "red" ? "vermelho" : f === "warn" ? "aviso" : "");

/**
 * CSV com um registro por aplicação. Números crus (ponto decimal, sem milhar; sem dado = vazio, nunca
 * zero) e BOM UTF-8 para o Excel acertar os acentos. O padrão de separador é ";" (Excel em português).
 */
export function buildCSV(result: ReportResult, delimiter: CsvDelimiter = ";"): string {
  const pcts = result.percentiles?.length ? result.percentiles : [99];
  const header = [
    "cluster", "namespace", "deployment", "requisicoes", "max_rps", "resposta_media_ms", ...pcts.map((p) => `p${p}_pico_ms`), ...(pcts.includes(99) ? ["p99_servico"] : []),
    "erros_4xx", "erros_5xx",
    "cpu_req_max_pct", "cpu_req_min_pct", "cpu_req_med_pct", "cpu_lim_max_pct", "cpu_lim_min_pct", "cpu_lim_med_pct",
    "mem_req_max_pct", "mem_req_min_pct", "mem_req_med_pct", "mem_lim_max_pct", "mem_lim_min_pct", "mem_lim_med_pct",
    ...(result.node_context
      ? ["node_pool", "nodes", "nodes_com_dado_cpu", "node_cpu_pior_pct", "node_cpu_med_pct", "node_mem_pior_pct", "node_mem_med_pct", "throttling_max_pct", "throttling_med_pct"]
      : []),
    ...(result.extras
      ? ["replicas_max", "replicas_min", "replicas_med", "replicas_desejadas_max", "hpa_min", "hpa_max", "restarts_janela", "rede_rx_pico_bps", "rede_rx_med_bps", "rede_tx_pico_bps", "rede_tx_med_bps", "rede_descartes_erros_pico_ps"]
      : []),
    "alerta_p99", "alerta_4xx", "alerta_5xx", "servicos", "fontes", "resolucao", "pico_aproximado", "erros_de_coleta", "janela_inicio", "janela_fim",
  ];
  const lines = [header.map(csvText).join(delimiter)];
  const cmp = (a: string, b: string) => a.localeCompare(b, "pt-BR");
  const rows = [...result.rows].sort((a, b) => cmp(a.cluster, b.cluster) || cmp(a.namespace, b.namespace) || cmp(a.workload, b.workload));
  for (const r of rows) {
    const t = r.traffic;
    const u = r.usage;
    const cells = [
      csvText(r.cluster), csvText(r.namespace), csvText(r.workload),
      csvNum(t?.requests), csvNum(t?.max_rps, 1), csvNum(t?.avg_ms, 1),
      ...pcts.map((p) => csvNum(percentileValue(t, p), 1)),
      ...(pcts.includes(99) ? [csvText(t?.p99_service ?? "")] : []),
      csvNum(t?.err_4xx), csvNum(t?.err_5xx),
      ...csvPct(u?.cpu_req_pct), ...csvPct(u?.cpu_lim_pct), ...csvPct(u?.mem_req_pct), ...csvPct(u?.mem_lim_pct),
      ...(result.node_context
        ? [
            csvText((r.node?.pools ?? []).map((p) => p.name).join(", ")), csvNum(r.node?.nodes), csvNum(r.node?.cpu?.nodes_with_data),
            csvNum(r.node?.cpu?.worst_pct, 1), csvNum(r.node?.cpu?.avg_pct, 1), csvNum(r.node?.mem?.worst_pct, 1), csvNum(r.node?.mem?.avg_pct, 1),
            csvNum(r.node?.throttle_pct?.max, 3), csvNum(r.node?.throttle_pct?.avg, 3),
          ]
        : []),
      ...(result.extras
        ? [
            csvNum(r.extras?.replicas?.max, 0), csvNum(r.extras?.replicas?.min, 0), csvNum(r.extras?.replicas?.avg, 1), csvNum(r.extras?.replicas_desired?.max, 0),
            csvNum(r.extras?.hpa_min, 0), csvNum(r.extras?.hpa_max, 0), csvNum(r.extras?.restarts, 0),
            csvNum(r.extras?.net_rx_bps?.max, 0), csvNum(r.extras?.net_rx_bps?.avg, 0), csvNum(r.extras?.net_tx_bps?.max, 0), csvNum(r.extras?.net_tx_bps?.avg, 0),
            csvNum(r.extras?.net_err_ps?.max, 2),
          ]
        : []),
      csvText(csvFlag(r.flags?.p99)), csvText(csvFlag(r.flags?.err_4xx)), csvText(csvFlag(r.flags?.err_5xx)),
      csvText((t?.services ?? []).join(", ")),
      csvText([t?.source, u?.source].filter(Boolean).join(" + ")),
      csvText(t?.resolution ?? ""),
      csvText(t?.peak_approx ? "sim" : t ? "nao" : ""),
      csvText((r.errors ?? []).join(" | ")),
      csvText(result.window.start), csvText(result.window.end),
    ];
    lines.push(cells.join(delimiter));
  }
  return `\uFEFF${lines.join("\r\n")}\r\n`;
}

// ── PDF (só texto e cor: as fontes padrão do PDF não têm emoji nem setas) ────────────────────────────

/** Troca símbolos fora do Latin-1 por equivalentes simples e descarta o resto, para o texto sair no PDF. */
export function pdfSafe(s: string): string {
  return s
    .replace(/→/g, "->").replace(/≥/g, ">=").replace(/≤/g, "<=").replace(/≈/g, "~").replace(/…/g, "...").replace(/[–—]/g, "-")
    .replace(/[‘’]/g, "'").replace(/[“”]/g, '"')
    // eslint-disable-next-line no-control-regex
    .replace(/[^\x09\x0A\x0D\x20-\x7E\xA0-\xFF]/g, "");
}

export const PDF_FLAG_FILL: Record<ExportFlag, [number, number, number]> = { red: [254, 202, 202], warn: [254, 240, 138] };

/** Monta o PDF (A4 paisagem) sem baixar. O jsPDF é carregado só aqui, para não pesar no pacote principal. */
export async function buildReportPDF(result: ReportResult) {
  const { default: jsPDF } = await import("jspdf");
  const { default: autoTable } = await import("jspdf-autotable");
  const { addLogoHeaderToPDF, addFooterToPDF } = await import("./logoUtils");
  const { lastTableY } = await import("./pdfAutoTable");

  const t = buildExportTable(result);
  const doc = new jsPDF({ orientation: "landscape", unit: "mm", format: "a4" });
  const M = 10;
  const pageH = doc.internal.pageSize.getHeight();

  let y = await addLogoHeaderToPDF(doc, "RELATORIO DE METRICAS", pdfSafe(t.meta.window));
  doc.setFontSize(8);
  doc.setFont("helvetica", "normal");
  doc.setTextColor(60, 60, 60);
  const info = [
    `Resolucao: ${pdfSafe(t.meta.resolution)}${t.meta.peakApprox ? " - PICO APROXIMADO (a resolucao disponivel suaviza picos curtos)" : ""}`,
    `Percentis: ${t.meta.percentiles.map((p) => `P${p}`).join(", ")}   |   Fontes: ${pdfSafe(t.meta.sources)}   |   Gerado em: ${t.meta.generatedAt}`,
    ...(t.meta.extras ? ["Replicas, HPA, restarts e rede incluidos (Prometheus, ~14 dias). Rede = bytes/s somando os pods (pico / media). E contexto, nao veredito."] : []),
    ...(t.meta.nodeContext ? ["Contexto de node incluido (Prometheus, ~14 dias). Pior = maior pico entre os nodes da aplicacao; media = media das medias. E contexto, nao veredito."] : []),
    ...t.meta.warnings.map((w) => `Aviso: ${pdfSafe(w)}`),
  ];
  y += 6;
  for (const line of info) {
    doc.text(line, M, y);
    y += 4;
  }
  y += 2;

  for (const g of t.groups) {
    if (y > pageH - 40) {
      doc.addPage();
      y = M + 4;
    }
    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(15, 23, 42);
    doc.text(pdfSafe(g.cluster), M, y);
    autoTable(doc, {
      startY: y + 2,
      head: [t.columns.map(pdfSafe)],
      body: g.rows.map((r) => r.cells.map(pdfSafe)),
      margin: { left: M, right: M },
      styles: { fontSize: t.columns.length > 14 ? 5.5 : 6.5, cellPadding: 1, overflow: "linebreak" },
      headStyles: { fillColor: [41, 128, 185], textColor: 255, fontSize: t.columns.length > 14 ? 5.5 : 6.5 },
      columnStyles: { 0: { cellWidth: 42 } },
      didParseCell: (data) => {
        if (data.section !== "body") return;
        const flag = g.rows[data.row.index]?.flags[data.column.index];
        if (flag) data.cell.styles.fillColor = PDF_FLAG_FILL[flag];
      },
    });
    y = lastTableY(doc) + 8;
  }

  if (y > pageH - 30) {
    doc.addPage();
    y = M + 4;
  }
  doc.setFontSize(7);
  doc.setFont("helvetica", "italic");
  doc.setTextColor(90, 90, 90);
  doc.text('Fundo vermelho: P99 acima de 1 s ou qualquer 5xx  |  Fundo amarelo: qualquer 4xx  |  "sem dado": a fonte nao devolveu o valor (nao e zero).', M, y);
  y += 5;
  if (t.issues.length > 0) {
    doc.setFont("helvetica", "bold");
    doc.text("Observacoes", M, y);
    y += 4;
    doc.setFont("helvetica", "normal");
    for (const issue of t.issues) {
      const lines = doc.splitTextToSize(pdfSafe(`- ${issue}`), doc.internal.pageSize.getWidth() - M * 2) as string[];
      for (const l of lines) {
        if (y > pageH - 14) {
          doc.addPage();
          y = M + 4;
        }
        doc.text(l, M, y);
        y += 3.5;
      }
    }
  }

  const pages = doc.getNumberOfPages();
  for (let i = 1; i <= pages; i++) {
    doc.setPage(i);
    addFooterToPDF(doc, i, pages);
  }
  return doc;
}

/** Gera e baixa o PDF do relatório. */
export async function exportReportPDF(result: ReportResult): Promise<void> {
  const doc = await buildReportPDF(result);
  doc.save(`relatorio-metricas_${fileStamp(result.generated_at)}.pdf`);
}

// ── download de texto (CSV, Markdown) ────────────────────────────────────────────────────────────────

export function downloadText(content: string, mimeType: string, filename: string): void {
  const blob = new Blob([content], { type: `${mimeType};charset=utf-8;` });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.setAttribute("href", url);
  link.setAttribute("download", filename);
  link.style.visibility = "hidden";
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}
