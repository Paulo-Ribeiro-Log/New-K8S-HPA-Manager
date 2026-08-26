import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { addLogoHeaderToPDF, addFooterToPDF } from "./logoUtils";
import type { NetDiscoveryResult } from "./api/types";

// ─── "Descoberta de Rede" — Fase 5, item P2 do roadmap de maturidade profissional
// (IP-ROUTE-DISCOVERY-PLAN.md, seção 10): exportar o resultado de uma descoberta em PDF, pra
// anexar num chamado/CHG (empresa inteira gira em torno de ServiceNow — hoje não dava pra anexar
// uma investigação de rede sem print manual). Reaproveita a MESMA infraestrutura de PDF já usada
// por FinOps/Health Check/Certificados/Node Pool Predictions (jsPDF+autoTable+logoUtils), nunca um
// mecanismo novo — autocontido no frontend, sem mudança de backend.

// removeEmojis — mesmo padrão local já usado em nodePoolPdfGenerator.ts (não centralizado num lib
// compartilhado neste projeto; cada gerador de PDF mantém sua própria cópia pequena). Necessário
// pro emoji do veredito de SO (🐧/🪟/❓, ver osLabel no NetDiscoveryTab.tsx) nunca entrar aqui —
// jsPDF com a fonte helvetica (WinAnsi) não renderiza emoji corretamente (lição documentada no
// CLAUDE.md pro export de PDF do Dynatrace).
const removeEmojis = (text: string): string =>
  text
    .replace(/[\u{1F300}-\u{1F9FF}]/gu, "")
    .replace(/[\u{2600}-\u{26FF}]/gu, "")
    .replace(/[\u{2700}-\u{27BF}]/gu, "")
    .replace(/️/g, "")
    .trim();

const fmtDate = (d: string | Date): string => {
  const date = typeof d === "string" ? new Date(d) : d;
  return date.toLocaleString("pt-BR", {
    day: "2-digit", month: "2-digit", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
};

const sectionHeader = (doc: jsPDF, title: string, y: number): number => {
  const pageWidth = doc.internal.pageSize.getWidth();
  doc.setFillColor(41, 128, 185);
  doc.rect(14, y - 5, pageWidth - 28, 9, "F");
  doc.setFontSize(11);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255);
  doc.text(title, 16, y);
  doc.setTextColor(0, 0, 0);
  return y + 10;
};

const checkNewPage = (doc: jsPDF, y: number, needed = 20): number => {
  if (y + needed > doc.internal.pageSize.getHeight() - 20) {
    doc.addPage();
    return 20;
  }
  return y;
};

const osLabelPDF = (guess?: string): string => {
  if (guess === "linux") return "Linux provavel";
  if (guess === "windows") return "Windows provavel";
  return "SO nao identificado";
};

interface NetDiscoveryExportContext {
  result: NetDiscoveryResult;
  mode: "pod" | "local";
  generatedAt?: string | Date; // default: agora (descoberta ao vivo). Passar created_at ao exportar do histórico.
}

// ============================================================================
// GERACAO DO PDF
// ============================================================================

export const exportNetDiscoveryPDF = async (ctx: NetDiscoveryExportContext): Promise<void> => {
  const { result, mode } = ctx;
  const generatedAt = ctx.generatedAt ?? new Date();
  const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
  const pageWidth = doc.internal.pageSize.getWidth();

  // ── Header ────────────────────────────────────────────────────────────────
  let y = await addLogoHeaderToPDF(
    doc,
    "DESCOBERTA DE REDE",
    `${result.target_input}${result.target_resolved ? `  (${result.target_ip})` : ""}`,
    45
  );

  doc.setFontSize(9);
  doc.setFont("helvetica", "italic");
  doc.setTextColor(100, 100, 100);
  doc.text(`Executado em: ${fmtDate(generatedAt)}`, pageWidth / 2, y - 5, { align: "center" });
  doc.setTextColor(0, 0, 0);
  y += 2;

  // ── Resumo ───────────────────────────────────────────────────────────────
  y = sectionHeader(doc, "RESUMO", y);
  doc.setFontSize(10);
  doc.setFont("helvetica", "normal");

  const summaryLines = [
    `Alvo digitado: ${result.target_input}`,
    `IP alcançado: ${result.target_ip}${result.target_resolved ? " (resolvido via DNS)" : ""}`,
    `Modo: ${mode === "local" ? "Direto do servidor" : "A partir de um Cluster"}`,
    `Saltos coletados: ${result.hops.length}`,
    `Destino confirmado: ${result.reached ? "Sim" : "Não (ver observação abaixo da tabela de saltos)"}`,
  ];
  summaryLines.forEach((line, i) => doc.text(line, 16, y + i * 6));
  y += summaryLines.length * 6 + 6;

  // ── Fingerprint do destino ──────────────────────────────────────────────
  if (result.fingerprint) {
    const fp = result.fingerprint;
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "IDENTIFICAÇÃO DO DESTINO (heurística — nunca certeza)", y);
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");

    const fpLines = [
      osLabelPDF(fp.os_guess),
      fp.ttl ? `TTL observado: ${fp.ttl}` : "TTL: sem resposta ao ping",
      fp.open_ports?.length ? `Portas abertas: ${fp.open_ports.join(", ")}` : "Portas abertas: nenhuma das ~18 verificadas respondeu",
      `Servidor web: ${fp.is_web_server ? "Sim" : "Não detectado"}`,
    ];
    if (fp.http_server) fpLines.push(`Header Server: ${fp.http_server}`);
    if (fp.tls_subject) fpLines.push(`Certificado TLS — Subject: ${fp.tls_subject}`);
    if (fp.tls_issuer) fpLines.push(`Certificado TLS — Issuer: ${fp.tls_issuer}`);
    fpLines.forEach((line, i) => doc.text(removeEmojis(line), 16, y + i * 6));
    y += fpLines.length * 6 + 2;

    // Achado real: um IP pode ter dezenas de PTR diferentes (ingress compartilhado) — sem esta
    // nota, o certificado/HTTP acima pareceria "do IP", escondendo que pode ser de um serviço
    // diferente do que o usuário pretendia investigar.
    if (fp.probed_host) {
      y = checkNewPage(doc, y, 14);
      doc.setFont("helvetica", "italic");
      doc.setFontSize(8.5);
      doc.setTextColor(150, 100, 0);
      const note = doc.splitTextToSize(
        `HTTP/certificado acima checados usando o hostname "${fp.probed_host}"` +
          (!result.target_resolved ? " (descoberto via DNS reverso, não digitado pelo usuário)" : "") +
          " — este IP pode responder de forma diferente pra outros hostnames que também apontam pra ele (comum em ingress compartilhado).",
        pageWidth - 32
      );
      doc.text(note, 16, y);
      doc.setTextColor(0, 0, 0);
      y += note.length * 5 + 4;
    }

    if (fp.os_confidence) {
      y = checkNewPage(doc, y, 14);
      doc.setFont("helvetica", "italic");
      doc.setFontSize(9);
      doc.setTextColor(100, 100, 100);
      const wrapped = doc.splitTextToSize(removeEmojis(fp.os_confidence), pageWidth - 32);
      doc.text(wrapped, 16, y);
      doc.setTextColor(0, 0, 0);
      y += wrapped.length * 5 + 6;
    }
  }

  // ── Tabela de saltos ─────────────────────────────────────────────────────
  y = checkNewPage(doc, y, 30);
  y = sectionHeader(doc, "ROTA (SALTOS)", y);

  // Fase A (múltiplas sondas por salto): coluna "Perda" — mesma amostragem que já alimenta a
  // tabela ao vivo/copyResult desta aba, exposta também no PDF pra quem só olha o documento
  // exportado (ex: anexado num CHG) sem nunca ter aberto a tela ao vivo.
  const hopRows = result.hops.map((h) => [
    String(h.index),
    h.timed_out ? "* * *" : `${h.ip ?? "—"}${h.is_target ? " (destino)" : ""}`,
    h.rtt_ms ? `${h.rtt_ms.toFixed(1)} ms` : "—",
    h.loss_pct ? `${h.loss_pct.toFixed(0)}%` : "—",
    h.reverse_dns || "—",
    h.asn ? `AS${h.asn}${h.asn_org ? ` - ${h.asn_org}` : ""}` : "—",
    h.cloud_match ? `${h.cloud_match.toUpperCase()}${h.cloud_region ? ` (${h.cloud_region})` : ""}` : (h.internal_ref ? `K8s: ${h.internal_ref.name}` : "—"),
  ]);

  autoTable(doc, {
    startY: y,
    head: [["#", "IP", "Latência", "Perda", "Hostname (DNS reverso)", "ASN / Organização", "Nuvem / K8s"]],
    body: hopRows,
    theme: "striped",
    headStyles: { fillColor: [59, 130, 246], fontStyle: "bold", fontSize: 8 },
    styles: { fontSize: 7.5, cellPadding: 1.5 },
    columnStyles: {
      0: { cellWidth: 8, halign: "center" },
      1: { cellWidth: 28 },
      2: { cellWidth: 18, halign: "center" },
      3: { cellWidth: 14, halign: "center" },
      4: { cellWidth: 44 },
      5: { cellWidth: 41 },
      6: { cellWidth: 29 },
    },
    margin: { left: 14, right: 14 },
  });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  y = (doc as any).lastAutoTable.finalY + 6;

  if (!result.reached) {
    y = checkNewPage(doc, y, 16);
    doc.setFontSize(8.5);
    doc.setFont("helvetica", "italic");
    doc.setTextColor(150, 100, 0);
    const note = doc.splitTextToSize(
      "Destino não confirmado dentro do limite de saltos. Saltos sem resposta são o normal em redes " +
        "corporativas (firewall/NAT ocultando topologia) — não é necessariamente uma falha de conectividade real.",
      pageWidth - 32
    );
    doc.text(note, 16, y);
    doc.setTextColor(0, 0, 0);
    y += note.length * 5;
  }

  // ── Footer em todas as páginas ───────────────────────────────────────────
  const totalPages = doc.getNumberOfPages();
  for (let i = 1; i <= totalPages; i++) {
    doc.setPage(i);
    addFooterToPDF(doc, i, totalPages, `Descoberta de Rede: ${result.target_input}`);
  }

  // ── Download ──────────────────────────────────────────────────────────────
  const safeName = result.target_input.replace(/[^a-zA-Z0-9.-]/g, "_");
  const ts = new Date(generatedAt).toISOString().replace(/[:.]/g, "-").slice(0, 16);
  doc.save(`descoberta-rede_${safeName}_${ts}.pdf`);
};
