// Exportação da comparação (atual × evento) em Markdown e CSV. Lógica pura (testada em
// metricsCompareExport.test.ts); o download fica com quem chama (downloadText).
import { fmtCompareValue, fmtDelta, verdict, type Comparison, type CompareMetric } from "./metricsCompare";
import { REPORT_TZ, type ReportResult } from "./metricsReport";
import { csvText, escapeMdCell, fmtWhen } from "./metricsReportExport";

const window = (r: ReportResult) => `${fmtWhen(r.window.start)} → ${fmtWhen(r.window.end)}`;

/** "35,8 / 40,1 (-11%, melhor)": atual / evento e a variação; o veredito só aparece quando há (pior ou melhor). */
function cell(m: CompareMetric): string {
  const v = verdict(m);
  const tag = v === "pior" || v === "melhor" ? `, ${v}` : "";
  return `${fmtCompareValue(m.kind, m.a)} / ${fmtCompareValue(m.kind, m.b)} (${fmtDelta(m.deltaPct)}${tag})`;
}

const PRESENCE: Record<string, string> = { ambos: "ambos", "so-atual": "só no atual", "so-evento": "só no evento" };

/** Markdown: cabeçalho com as duas janelas, avisos e uma tabela por cluster. `generatedAt` em ISO. */
export function buildComparisonMarkdown(cmp: Comparison, atual: ReportResult, evento: ReportResult, eventName: string, generatedAt: string): string {
  const lines: string[] = [
    `# Comparação: atual × ${eventName}`,
    "",
    `- **Atual:** ${window(atual)} (${REPORT_TZ})`,
    `- **Evento "${eventName}":** ${window(evento)}`,
    `- **Latência comparada:** P${cmp.percentile} (pico)`,
    `- **Gerado em:** ${fmtWhen(generatedAt)}`,
    "",
    "> Cada célula traz **atual / evento (variação)**. A variação é (atual − evento) / evento, e o evento é a base. Só taxas e picos são comparados, nunca os totais, porque os totais dependem do tamanho da janela. Variação até 5% conta como igual.",
    "",
  ];
  for (const n of cmp.notes) lines.push(`> ⚠ ${n}`, "");
  if (cmp.rows.length === 0) {
    lines.push("_Nenhuma aplicação foi comparada._", "");
    return lines.join("\n");
  }
  const labels = cmp.rows[0].metrics.map((m) => m.label);
  const byCluster = new Map<string, typeof cmp.rows>();
  for (const r of cmp.rows) byCluster.set(r.cluster, [...(byCluster.get(r.cluster) ?? []), r]);
  for (const [cluster, rows] of [...byCluster.entries()].sort(([a], [b]) => a.localeCompare(b, "pt-BR"))) {
    lines.push(`## ${cluster}`, "", `| Aplicação | Presença | ${labels.map(escapeMdCell).join(" | ")} |`, `| --- | --- | ${labels.map(() => "---").join(" | ")} |`);
    for (const r of rows) lines.push(`| ${escapeMdCell(`${r.workload} (ns: ${r.namespace})`)} | ${PRESENCE[r.presence] ?? r.presence} | ${r.metrics.map((m) => escapeMdCell(cell(m))).join(" | ")} |`);
    lines.push("");
  }
  return lines.join("\n");
}

const num = (v: number | null, decimals: number) => (v == null ? "" : String(Math.round(v * 10 ** decimals) / 10 ** decimals));
const DEC: Record<string, number> = { rps: 3, ms: 1, pct: 3 };
const KEYS: Record<string, string> = { avg_rps: "rps_medio", max_rps: "rps_pico", avg: "response_time_medio_ms", lat: "latencia_pico_ms", err5: "erros_5xx_pct", err4: "erros_4xx_pct", cpu: "cpu_req_max_pct", mem: "mem_req_max_pct" };

/**
 * CSV com um registro por aplicação: por métrica, o valor atual, o do evento e a variação em %. Números crus
 * (ponto decimal; ms para latência), sem dado = vazio, BOM UTF-8 para o Excel.
 */
export function buildComparisonCSV(cmp: Comparison, eventName: string, delimiter = ";"): string {
  const metrics = cmp.rows[0]?.metrics ?? [];
  const header = ["evento", "cluster", "namespace", "deployment", "presenca", ...metrics.flatMap((m) => [`${KEYS[m.key] ?? m.key}_atual`, `${KEYS[m.key] ?? m.key}_evento`, `${KEYS[m.key] ?? m.key}_variacao_pct`])];
  const lines = [header.map(csvText).join(delimiter)];
  const rows = [...cmp.rows].sort((a, b) => a.cluster.localeCompare(b.cluster, "pt-BR") || a.namespace.localeCompare(b.namespace, "pt-BR") || a.workload.localeCompare(b.workload, "pt-BR"));
  for (const r of rows) {
    lines.push([
      csvText(eventName), csvText(r.cluster), csvText(r.namespace), csvText(r.workload), csvText(PRESENCE[r.presence] ?? r.presence),
      ...r.metrics.flatMap((m) => [num(m.a, DEC[m.kind]), num(m.b, DEC[m.kind]), num(m.deltaPct, 1)]),
    ].join(delimiter));
  }
  return "\uFEFF" + lines.join("\r\n") + "\r\n";
}
