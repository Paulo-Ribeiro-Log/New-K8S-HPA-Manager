import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { addLogoHeaderToPDF, addFooterToPDF } from "./logoUtils";

// ============================================================================
// Helpers internos
// ============================================================================

const fmtPct = (v: number | undefined | null) =>
  v != null ? `${v.toFixed(1)}%` : "—";

const fmtDate = (d: string | undefined) => {
  if (!d) return "—";
  return new Date(d).toLocaleString("pt-BR", {
    day: "2-digit", month: "2-digit", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
};

const removeEmojis = (text: string): string =>
  text
    .replace(/[\u{1F300}-\u{1F9FF}]/gu, "")
    .replace(/[\u{2600}-\u{26FF}]/gu, "")
    .replace(/[\u{2700}-\u{27BF}]/gu, "")
    .replace(/\uFE0F/g, "")
    .trim();

const sectionHeader = (
  doc: jsPDF,
  title: string,
  y: number,
  color: [number, number, number] = [41, 128, 185]
): number => {
  const pageWidth = doc.internal.pageSize.getWidth();
  doc.setFillColor(...color);
  doc.rect(14, y - 5, pageWidth - 28, 9, "F");
  doc.setFontSize(11);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255);
  doc.text(title, 16, y);
  doc.setTextColor(0, 0, 0);
  return y + 8;
};

const checkNewPage = (doc: jsPDF, y: number, needed = 20): number => {
  if (y + needed > doc.internal.pageSize.getHeight() - 20) {
    doc.addPage();
    return 20;
  }
  return y;
};

const riskColor = (level: string): [number, number, number] => {
  const map: Record<string, [number, number, number]> = {
    critical: [220, 53, 69],
    high: [255, 128, 0],
    medium: [255, 193, 7],
    low: [40, 167, 69],
  };
  return map[level?.toLowerCase()] ?? [108, 117, 125];
};

// ============================================================================
// GERACAO DO PDF
// ============================================================================

export const generateNodePoolPredictionPDF = async (result: any): Promise<void> => {
  const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
  const pageWidth = doc.internal.pageSize.getWidth();

  const nodepool = result.nodepool_name ?? "—";
  const cluster = result.raw_metrics?.azure_cluster || result.cluster || "—";

  // ── Header ────────────────────────────────────────────────────────────────
  let y = await addLogoHeaderToPDF(
    doc,
    "ANALISE PREDITIVA - NODE POOL",
    `${nodepool}  |  Cluster: ${cluster}`,
    45
  );

  doc.setFontSize(9);
  doc.setFont("helvetica", "italic");
  doc.setTextColor(100, 100, 100);
  doc.text(`Analisado em: ${fmtDate(result.analyzed_at)}`, pageWidth / 2, y - 5, { align: "center" });
  doc.setTextColor(0, 0, 0);
  y += 2;

  // ── Health Score + Sumário ────────────────────────────────────────────────
  y = sectionHeader(doc, "SUMARIO EXECUTIVO", y);
  y += 2;

  const hs = result.health_score;
  const es = result.executive_summary;
  const risk = es?.risk_level ?? "medium";
  const [rR, rG, rB] = riskColor(risk);

  // Health score badge
  doc.setFontSize(28);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(rR, rG, rB);
  doc.text(`${hs?.overall ?? "—"}`, 20, y + 10);
  doc.setFontSize(9);
  doc.setFont("helvetica", "normal");
  doc.setTextColor(80, 80, 80);
  doc.text("/100", 33, y + 10);
  doc.text("Health Score", 20, y + 15);

  // Risk badge
  doc.setFillColor(rR, rG, rB);
  doc.roundedRect(50, y, 35, 10, 2, 2, "F");
  doc.setFontSize(10);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255);
  doc.text(`Risco: ${risk.toUpperCase()}`, 67, y + 7, { align: "center" });
  doc.setTextColor(0, 0, 0);

  // Action required
  if (es?.action_required) {
    doc.setFillColor(220, 53, 69);
    doc.roundedRect(92, y, 40, 10, 2, 2, "F");
    doc.setFontSize(9);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(255, 255, 255);
    doc.text("ACAO NECESSARIA", 112, y + 7, { align: "center" });
    doc.setTextColor(0, 0, 0);
  }

  y += 20;

  // Current state
  if (es?.current_state) {
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    const lines = doc.splitTextToSize(removeEmojis(es.current_state), pageWidth - 30) as string[];
    doc.text(lines, 14, y);
    y += lines.length * 5 + 4;
  }

  // Health breakdown
  if (hs?.breakdown) {
    const bdRows = Object.entries(hs.breakdown).map(([k, v]) => [
      k.replace(/_/g, " ").toUpperCase(),
      `${v}/100`,
    ]);
    autoTable(doc, {
      startY: y,
      head: [["Componente", "Score"]],
      body: bdRows,
      theme: "grid",
      headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
      bodyStyles: { fontSize: 9 },
      columnStyles: { 0: { cellWidth: 80 }, 1: { cellWidth: 30, halign: "center" } },
      margin: { left: 14, right: 14 },
      tableWidth: "wrap",
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // Key findings
  if (es?.key_findings?.length) {
    y = checkNewPage(doc, y, 30);
    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.text("Principais achados:", 14, y);
    y += 5;
    doc.setFont("helvetica", "normal");
    doc.setFontSize(9);
    for (const f of es.key_findings.slice(0, 5)) {
      const lines = doc.splitTextToSize(`• ${removeEmojis(f)}`, pageWidth - 32) as string[];
      y = checkNewPage(doc, y, lines.length * 5 + 2);
      doc.text(lines, 18, y);
      y += lines.length * 5;
    }
    y += 4;
  }

  // ── Infraestrutura ────────────────────────────────────────────────────────
  y = checkNewPage(doc, y, 40);
  y = sectionHeader(doc, "INFRAESTRUTURA DO POOL", y);
  y += 2;

  const rm = result.raw_metrics;
  const infraRows = [
    ["Node Pool", nodepool],
    ["Cluster (Azure)", cluster],
    ["VM SKU", rm?.vm_size || "—"],
    ["Nodes atuais", String(rm?.current_nodes ?? "—")],
    ["Min / Max nodes", rm?.data_sources?.azure_api_available
      ? `${rm.min_nodes} / ${rm.max_nodes}`
      : "N/A (Azure API indisponível)"],
    ["Autoscaler", rm?.data_sources?.azure_api_available
      ? (rm.autoscaler_enabled ? "Habilitado" : "Desabilitado")
      : "N/A"],
    ["Prometheus", rm?.data_sources?.prometheus_available ? "Disponível" : "Indisponível"],
    ["Node Exporter", rm?.data_sources?.node_exporter_available ? "Disponível" : "Indisponível"],
  ];

  autoTable(doc, {
    startY: y,
    head: [["Campo", "Valor"]],
    body: infraRows,
    theme: "grid",
    headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
    bodyStyles: { fontSize: 9 },
    columnStyles: { 0: { cellWidth: 60, fontStyle: "bold" } },
    margin: { left: 14, right: 14 },
  });
  y = (doc as any).lastAutoTable.finalY + 6;

  // ── Estado dos Nodes ──────────────────────────────────────────────────────
  if (rm?.nodes_snapshot?.length) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "ESTADO ATUAL DOS NODES", y);
    y += 2;

    const nodeRows = rm.nodes_snapshot.map((n: any) => [
      n.node_name?.replace(/.*-vmss/, "vmss") ?? "—",
      fmtPct(n.cpu_usage_percent),
      fmtPct(n.cpu_requested_percent),
      fmtPct(n.mem_usage_percent),
      fmtPct(n.mem_requested_percent),
      String(n.pod_count ?? 0),
      n.status ?? "—",
    ]);

    autoTable(doc, {
      startY: y,
      head: [["Node", "CPU uso", "CPU req", "Mem uso", "Mem req", "Pods", "Status"]],
      body: nodeRows,
      theme: "grid",
      headStyles: { fillColor: [41, 128, 185], fontSize: 8 },
      bodyStyles: { fontSize: 8 },
      margin: { left: 14, right: 14 },
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // ── Tendências ────────────────────────────────────────────────────────────
  const trends = result.trends;
  if (trends) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "TENDENCIAS (por node, normalizadas)", y);
    y += 2;

    const trendRows = [
      ["CPU", String(trends.cpu_trend ?? "—"),
        trends.cpu_change_3d_percent != null ? `${trends.cpu_change_3d_percent > 0 ? "+" : ""}${trends.cpu_change_3d_percent.toFixed(1)}%` : "—",
        trends.cpu_change_7d_percent != null ? `${trends.cpu_change_7d_percent > 0 ? "+" : ""}${trends.cpu_change_7d_percent.toFixed(1)}%` : "—",
        trends.cpu_change_14d_percent != null ? `${trends.cpu_change_14d_percent > 0 ? "+" : ""}${trends.cpu_change_14d_percent.toFixed(1)}%` : "—"],
      ["Memoria", String(trends.mem_trend ?? "—"),
        "—",
        trends.mem_change_7d_percent != null ? `${trends.mem_change_7d_percent > 0 ? "+" : ""}${trends.mem_change_7d_percent.toFixed(1)}%` : "—",
        trends.mem_change_14d_percent != null ? `${trends.mem_change_14d_percent > 0 ? "+" : ""}${trends.mem_change_14d_percent.toFixed(1)}%` : "—"],
      ["Pods/node", String(trends.pods_trend ?? "—"),
        "—",
        trends.pods_change_7d_percent != null ? `${trends.pods_change_7d_percent > 0 ? "+" : ""}${trends.pods_change_7d_percent.toFixed(1)}%` : "—",
        "—"],
      ["conntrack", String(trends.conntrack_trend ?? "—"),
        "—",
        trends.conntrack_change_7d_percent ? `${trends.conntrack_change_7d_percent > 0 ? "+" : ""}${trends.conntrack_change_7d_percent.toFixed(1)}%` : "—",
        "—"],
    ];

    autoTable(doc, {
      startY: y,
      head: [["Metrica", "Tendencia", "D-3", "D-7", "D-14"]],
      body: trendRows,
      theme: "grid",
      headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
      bodyStyles: { fontSize: 9 },
      margin: { left: 14, right: 14 },
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // ── Previsões ─────────────────────────────────────────────────────────────
  const preds = result.predictions;
  if (preds) {
    const allPreds = [
      ...( preds.short_term ?? []).map((p: any) => ({ ...p, horizon: "Curto prazo" })),
      ...( preds.medium_term ?? []).map((p: any) => ({ ...p, horizon: "Medio prazo" })),
      ...( preds.long_term ?? []).map((p: any) => ({ ...p, horizon: "Longo prazo" })),
    ];

    if (allPreds.length > 0) {
      y = checkNewPage(doc, y, 40);
      y = sectionHeader(doc, "PREVISOES", y);
      y += 2;

      const predRows = allPreds.map((p: any) => [
        p.horizon,
        p.timeframe ?? "—",
        removeEmojis(p.event ?? ""),
        `${Math.round((p.probability ?? 0) * 100)}%`,
        p.severity?.toUpperCase() ?? "—",
      ]);

      autoTable(doc, {
        startY: y,
        head: [["Horizonte", "Prazo", "Evento", "Prob.", "Severidade"]],
        body: predRows,
        theme: "grid",
        headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
        bodyStyles: { fontSize: 9 },
        columnStyles: { 2: { cellWidth: 70 } },
        margin: { left: 14, right: 14 },
      });
      y = (doc as any).lastAutoTable.finalY + 6;
    }
  }

  // ── Recomendações ─────────────────────────────────────────────────────────
  if (result.recommendations?.length) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "RECOMENDACOES", y);
    y += 2;

    for (const rec of result.recommendations.slice(0, 8)) {
      y = checkNewPage(doc, y, 30);

      // Título da recomendação
      doc.setFontSize(10);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(41, 128, 185);
      doc.text(`${rec.priority}. ${removeEmojis(rec.title ?? "")}`, 14, y);
      doc.setTextColor(0, 0, 0);
      y += 5;

      // Badges: categoria, complexidade, risco
      doc.setFontSize(8);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(80, 80, 80);
      const meta = [
        `Categoria: ${rec.category ?? "—"}`,
        `Complexidade: ${rec.complexity ?? "—"}`,
        `Risco: ${rec.risk_level ?? "—"}`,
        `Tempo: ${rec.time_required ?? "—"}`,
      ].join("   |   ");
      doc.text(meta, 18, y);
      y += 4;

      // Descrição
      if (rec.description) {
        doc.setFontSize(9);
        const lines = doc.splitTextToSize(removeEmojis(rec.description), pageWidth - 32) as string[];
        doc.text(lines, 18, y);
        y += lines.length * 4 + 2;
      }

      // Ações
      if (rec.actions?.length) {
        doc.setFontSize(8);
        doc.setFont("helvetica", "italic");
        doc.setTextColor(60, 60, 60);
        for (const act of rec.actions.slice(0, 3)) {
          y = checkNewPage(doc, y, 8);
          const alines = doc.splitTextToSize(`• ${removeEmojis(act)}`, pageWidth - 38) as string[];
          doc.text(alines, 22, y);
          y += alines.length * 4;
        }
        doc.setFont("helvetica", "normal");
        doc.setTextColor(0, 0, 0);
      }
      y += 3;
    }
    y += 2;
  }

  // ── Bin Packing ───────────────────────────────────────────────────────────
  const bp = rm?.bin_packing;
  if (bp) {
    y = checkNewPage(doc, y, 35);
    y = sectionHeader(doc, "ANALISE DE BIN PACKING (FRAGMENTACAO)", y, [76, 175, 80]);
    y += 2;

    const bpRows = [
      ["Eficiencia CPU", fmtPct(bp.cpu_efficiency)],
      ["Eficiencia Memoria", fmtPct(bp.mem_efficiency)],
      ["Nivel de Fragmentacao", bp.fragmentation_level ?? "—"],
      ["Candidatos scale-in", String(bp.scale_in_candidates ?? 0)],
      ["Scale-in seguro", bp.scale_in_safe ? "Sim" : "Nao"],
    ];
    if (bp.wasted_resources) {
      bpRows.push(["Recursos desperdicados", bp.wasted_resources]);
    }

    autoTable(doc, {
      startY: y,
      head: [["Campo", "Valor"]],
      body: bpRows,
      theme: "grid",
      headStyles: { fillColor: [76, 175, 80], fontSize: 9 },
      bodyStyles: { fontSize: 9 },
      columnStyles: { 0: { cellWidth: 60, fontStyle: "bold" } },
      margin: { left: 14, right: 14 },
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // ── Timeline de Saturação ─────────────────────────────────────────────────
  const tl = result.saturation_timeline;
  if (tl?.forecasts?.length) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "TIMELINE DE SATURACAO", y, [244, 67, 54]);
    y += 2;

    if (tl.summary) {
      doc.setFontSize(9);
      doc.setFont("helvetica", "italic");
      doc.setTextColor(80, 80, 80);
      const sumLines = doc.splitTextToSize(tl.summary, pageWidth - 28) as string[];
      doc.text(sumLines, 14, y);
      y += sumLines.length * 4 + 3;
      doc.setFont("helvetica", "normal");
      doc.setTextColor(0, 0, 0);
    }

    const tlBody = tl.forecasts.map((f: any) => {
      const days = f.days_until_saturation != null ? `${Math.round(f.days_until_saturation)}d` : "—";
      const date = f.estimated_date ? new Date(f.estimated_date).toLocaleDateString("pt-BR") : "—";
      const node = f.affected_node ? ` (${f.affected_node})` : "";
      const growth = f.daily_growth_rate > 0 ? `+${f.daily_growth_rate.toFixed(2)}%/d` : "—";
      return [
        `${f.metric}${node}`,
        `${f.current_value.toFixed(1)}%`,
        `${f.threshold.toFixed(0)}%`,
        growth,
        days,
        date,
        f.urgency_badge ?? "—",
        f.confidence ?? "—",
      ];
    });

    const urgencyColor = (badge: string): [number, number, number] => {
      if (badge === "CRITICO") return [244, 67, 54];
      if (badge === "ATENCAO") return [255, 152, 0];
      return [76, 175, 80];
    };

    autoTable(doc, {
      startY: y,
      head: [["Metrica", "Atual", "Thresh.", "Cresc./dia", "Dias", "Data", "Urgencia", "Conf."]],
      body: tlBody,
      theme: "grid",
      headStyles: { fillColor: [244, 67, 54], fontSize: 8 },
      bodyStyles: { fontSize: 8 },
      columnStyles: {
        0: { cellWidth: 35 },
        1: { cellWidth: 16 },
        2: { cellWidth: 16 },
        3: { cellWidth: 22 },
        4: { cellWidth: 14 },
        5: { cellWidth: 22 },
        6: { cellWidth: 20 },
        7: { cellWidth: 14 },
      },
      margin: { left: 14, right: 14 },
      didParseCell: (data: any) => {
        if (data.section === "body" && data.column.index === 6) {
          const badge = String(data.cell.raw ?? "");
          data.cell.styles.textColor = urgencyColor(badge);
          data.cell.styles.fontStyle = "bold";
        }
      },
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // ── Custo ─────────────────────────────────────────────────────────────────
  const cost = result.cost_analysis;
  if (cost) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "ANALISE DE CUSTO", y, [255, 152, 0]);
    y += 2;

    const costRows = [
      ["Custo atual/mes (USD)", cost.monthly_cost_usd != null ? `$${cost.monthly_cost_usd.toFixed(2)}` : "—"],
      ["Custo atual/mes (BRL)", cost.monthly_cost_brl != null ? `R$ ${cost.monthly_cost_brl.toFixed(2)}` : "—"],
      ["Economia potencial/mes", cost.monthly_savings_usd != null ? `$${cost.monthly_savings_usd.toFixed(2)}` : "—"],
      ["VM Size", cost.vm_size ?? "—"],
      ["Custo por vCPU/hora", cost.cost_per_vcpu_hour != null ? `$${cost.cost_per_vcpu_hour.toFixed(4)}` : "—"],
    ];

    autoTable(doc, {
      startY: y,
      head: [["Metrica", "Valor"]],
      body: costRows,
      theme: "grid",
      headStyles: { fillColor: [255, 152, 0], fontSize: 9 },
      bodyStyles: { fontSize: 9 },
      columnStyles: { 0: { cellWidth: 70, fontStyle: "bold" } },
      margin: { left: 14, right: 14 },
    });
    y = (doc as any).lastAutoTable.finalY + 6;

    if (cost.recommendations?.length) {
      doc.setFontSize(10);
      doc.setFont("helvetica", "bold");
      doc.text("Recomendacoes de custo:", 14, y);
      y += 5;
      doc.setFont("helvetica", "normal");
      doc.setFontSize(9);
      for (const cr of cost.recommendations.slice(0, 4)) {
        y = checkNewPage(doc, y, 12);
        const txt = `• ${removeEmojis(cr.title ?? cr.description ?? "")}`;
        const lines = doc.splitTextToSize(txt, pageWidth - 32) as string[];
        doc.text(lines, 18, y);
        y += lines.length * 5;
      }
    }
  }

  // ── Root Cause ────────────────────────────────────────────────────────────
  const rc = result.root_cause;
  if (rc?.identified_causes?.length) {
    y = checkNewPage(doc, y, 40);
    y = sectionHeader(doc, "ANALISE DE CAUSA RAIZ", y, [156, 39, 176]);
    y += 4;

    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.text(`Fator principal: ${removeEmojis(rc.primary_factor ?? "—")}`, 14, y);
    y += 6;

    const rcRows = rc.identified_causes.slice(0, 5).map((c: any) => [
      removeEmojis(c.cause ?? ""),
      c.category ?? "—",
      `${Math.round((c.certainty ?? 0) * 100)}%`,
      removeEmojis(c.remediation ?? ""),
    ]);

    autoTable(doc, {
      startY: y,
      head: [["Causa", "Categoria", "Certeza", "Remediacao"]],
      body: rcRows,
      theme: "grid",
      headStyles: { fillColor: [156, 39, 176], fontSize: 9 },
      bodyStyles: { fontSize: 9 },
      columnStyles: { 0: { cellWidth: 55 }, 3: { cellWidth: 55 } },
      margin: { left: 14, right: 14 },
    });
    y = (doc as any).lastAutoTable.finalY + 6;
  }

  // ── Footer em todas as páginas ────────────────────────────────────────────
  const totalPages = doc.getNumberOfPages();
  for (let i = 1; i <= totalPages; i++) {
    doc.setPage(i);
    addFooterToPDF(doc, i, totalPages, `Node Pool: ${nodepool} | Cluster: ${cluster}`);
  }

  // ── Download ──────────────────────────────────────────────────────────────
  const ts = result.analyzed_at
    ? new Date(result.analyzed_at).toISOString().replace(/[:.]/g, "-").slice(0, 16)
    : "sem-data";
  doc.save(`nodepool_${nodepool}_${ts}.pdf`);
};
