import type { NetDiscoveryHop, NetDiscoveryResult } from "./api/types";

// ─── "Descoberta de Rede" — Fase E do roadmap de maturidade profissional
// (IP-ROUTE-DISCOVERY-PLAN.md): exportar CSV/JSON (pra levar o dado bruto pra outra ferramenta) e
// gerar o Markdown usado pelo botão "Salvar como Nota" (reaproveita a feature de Notas já
// existente nesta app). 100% frontend, sem mudança de backend — o NetDiscoveryResult já tem tudo
// que estas funções precisam. Mesmo padrão de download via Blob+<a> já usado em outros exports CSV
// desta app (ex: HistoryViewer.tsx) — este projeto nunca centralizou esse helper num lib
// compartilhado, cada exportador mantém sua própria cópia pequena (mesma convenção já documentada
// pros geradores de PDF).

function downloadBlob(content: string, mimeType: string, filename: string): void {
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

function safeFileName(input: string): string {
  return input.replace(/[^a-zA-Z0-9.-]/g, "_");
}

function csvCell(value: string | number | undefined): string {
  const s = value === undefined ? "" : String(value);
  return `"${s.replace(/"/g, '""')}"`;
}

export function exportNetDiscoveryJSON(result: NetDiscoveryResult): void {
  const content = JSON.stringify(result, null, 2);
  const ts = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 16);
  downloadBlob(content, "application/json", `descoberta-rede_${safeFileName(result.target_input)}_${ts}.json`);
}

export function exportNetDiscoveryCSV(result: NetDiscoveryResult): void {
  const header = [
    "index", "ip", "rtt_min_ms", "rtt_avg_ms", "rtt_max_ms", "loss_pct",
    "reverse_dns", "asn", "asn_org", "cloud_match", "cloud_region", "k8s_resource",
  ];
  const rows = result.hops.map((h) => [
    csvCell(h.index),
    csvCell(h.timed_out ? "" : h.ip),
    csvCell(h.rtt_min_ms),
    csvCell(h.rtt_ms),
    csvCell(h.rtt_max_ms),
    csvCell(h.loss_pct),
    csvCell(h.reverse_dns),
    csvCell(h.asn),
    csvCell(h.asn_org),
    csvCell(h.cloud_match),
    csvCell(h.cloud_region),
    csvCell(h.internal_ref?.name),
  ].join(","));
  const content = [header.map(csvCell).join(","), ...rows].join("\n");
  const ts = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 16);
  downloadBlob(content, "text/csv", `descoberta-rede_${safeFileName(result.target_input)}_${ts}.csv`);
}

function osLabelMarkdown(guess?: string): string {
  if (guess === "linux") return "Linux provável";
  if (guess === "windows") return "Windows provável";
  return "SO não identificado";
}

function hopLatencySummary(h: NetDiscoveryHop): string {
  if (h.timed_out) return "* * *";
  const parts: string[] = [];
  if (h.rtt_ms != null) parts.push(`${h.rtt_ms.toFixed(1)} ms`);
  if (h.loss_pct) parts.push(`perda ${h.loss_pct.toFixed(0)}%`);
  return parts.length > 0 ? parts.join(", ") : "—";
}

// buildNoteMarkdown gera o conteúdo salvo pelo botão "Salvar como Nota" (NotesModal já existente
// nesta app) — resumo do alvo/fingerprint seguido de uma tabela Markdown com a rota, reaproveitando
// a mesma informação já mostrada na tela (nunca busca dado novo).
export function buildNoteMarkdown(result: NetDiscoveryResult, mode: "pod" | "local"): string {
  const lines: string[] = [];
  lines.push(`# Descoberta de Rede: ${result.target_input}${result.target_resolved ? ` (${result.target_ip})` : ""}`);
  lines.push("");
  lines.push(`- Modo: ${mode === "local" ? "Direto do servidor" : "A partir de um Cluster"}`);
  lines.push(`- Destino confirmado: ${result.reached ? "Sim" : "Não"}`);
  lines.push(`- Saltos coletados: ${result.hops.length}`);

  if (result.fingerprint) {
    const fp = result.fingerprint;
    lines.push(`- Identificação do destino: ${osLabelMarkdown(fp.os_guess)}${fp.os_confidence ? ` — ${fp.os_confidence}` : ""}`);
    if (fp.open_ports?.length) lines.push(`- Portas abertas: ${fp.open_ports.join(", ")}`);
    if (fp.service_versions?.length) {
      lines.push(`- Serviços detectados (nmap): ${fp.service_versions.map((sv) => `${sv.port}/${sv.service ?? "?"}${sv.version ? ` (${sv.version})` : ""}`).join(", ")}`);
    }
    if (fp.http_server) lines.push(`- Header Server: ${fp.http_server}`);
    if (fp.tls_subject) lines.push(`- Certificado TLS: ${fp.tls_subject}${fp.tls_issuer ? ` (emitido por ${fp.tls_issuer})` : ""}`);
  }

  lines.push("");
  lines.push("## Rota");
  lines.push("");
  lines.push("| # | IP | Latência/Perda | Contexto (DNS/ASN/nuvem) |");
  lines.push("|---|----|----|----|");
  for (const h of result.hops) {
    const ip = h.timed_out ? "* * *" : `${h.ip ?? "—"}${h.is_target ? " (destino)" : ""}`;
    const context = [
      h.reverse_dns,
      h.asn ? `AS${h.asn}${h.asn_org ? ` — ${h.asn_org}` : ""}` : undefined,
      h.cloud_match ? h.cloud_match.toUpperCase() : undefined,
      h.internal_ref ? `K8s: ${h.internal_ref.name}` : undefined,
    ].filter(Boolean).join(", ") || "—";
    lines.push(`| ${h.index} | ${ip} | ${hopLatencySummary(h)} | ${context} |`);
  }

  if (!result.reached) {
    lines.push("");
    lines.push("_Destino não confirmado dentro do limite de saltos — hops sem resposta são comuns em redes corporativas (firewall/NAT)._");
  }

  return lines.join("\n");
}
