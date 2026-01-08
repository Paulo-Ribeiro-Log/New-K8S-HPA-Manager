import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import type { AnalysisResult } from "@/types/ai";

// Formatar data para exibição
const formatDate = (dateStr: string | undefined): string => {
  if (!dateStr) return "Data não disponível";
  const date = new Date(dateStr);
  return date.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
};

// ============================================================================
// GERAÇÃO DE PDF - AI Diagnostics
// ============================================================================
export const generateAIDiagnosticsPDF = (analysis: AnalysisResult): void => {
  const doc = new jsPDF({
    orientation: "portrait",
    unit: "mm",
    format: "a4",
  });
  const pageWidth = doc.internal.pageSize.getWidth();
  let yPosition = 20;

  // Header box (azul profissional)
  doc.setFillColor(41, 128, 185);
  doc.rect(0, 0, pageWidth, 50, "F");

  // Título principal
  doc.setFontSize(24);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255);
  doc.text("ANALISE DE DIAGNOSTICO - AI", pageWidth / 2, 15, { align: "center" });

  // Subtítulo
  doc.setFontSize(12);
  doc.setFont("helvetica", "normal");
  const subtitle = `${analysis.resource_type}: ${analysis.namespace}/${analysis.resource_name}`;
  doc.text(subtitle, pageWidth / 2, 28, { align: "center" });

  // Data e hora
  doc.setFontSize(10);
  doc.setFont("helvetica", "italic");
  doc.text(`Analisado em: ${formatDate(analysis.analyzed_at)}`, pageWidth / 2, 40, { align: "center" });

  // Reset cor do texto
  doc.setTextColor(0, 0, 0);
  yPosition = 60;

  // ========================================================================
  // SEÇÃO 1: METADADOS DO RECURSO
  // ========================================================================
  if (analysis.resource_metadata) {
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setFillColor(240, 240, 240);
    doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
    doc.text("METADADOS DO RECURSO", 16, yPosition);
    yPosition += 10;

    // Grid de metadados usando autoTable
    const metadataRows: string[][] = [
      ["Nome", analysis.resource_metadata.name],
      ["Namespace", analysis.resource_metadata.namespace],
      ["Cluster", analysis.resource_metadata.cluster],
    ];

    if (analysis.resource_metadata.version) {
      metadataRows.push(["Versão", analysis.resource_metadata.version]);
    }

    metadataRows.push(
      ["Status", analysis.resource_metadata.status],
      ["Age", analysis.resource_metadata.age],
      ["Restart Count", analysis.resource_metadata.restart_count.toString()]
    );

    if (analysis.resource_metadata.node_name) {
      metadataRows.push(["Node", analysis.resource_metadata.node_name]);
    }

    autoTable(doc, {
      startY: yPosition,
      head: [["Campo", "Valor"]],
      body: metadataRows,
      theme: "grid",
      headStyles: {
        fillColor: [52, 152, 219],
        textColor: 255,
        fontStyle: "bold",
        fontSize: 10,
      },
      bodyStyles: {
        fontSize: 9,
      },
      columnStyles: {
        0: { fontStyle: "bold", cellWidth: 50 },
        1: { cellWidth: "auto" },
      },
      margin: { left: 14, right: 14 },
    });

    yPosition = (doc as any).lastAutoTable.finalY + 8;

    // Recursos (CPU/Memory)
    doc.setFontSize(11);
    doc.setFont("helvetica", "bold");
    doc.text("Recursos:", 16, yPosition);
    yPosition += 6;

    const resourcesRows: string[][] = [
      [
        "CPU",
        analysis.resource_metadata.resources.cpu.current || "N/A",
        analysis.resource_metadata.resources.cpu.request || "N/A",
        analysis.resource_metadata.resources.cpu.limit || "N/A",
      ],
      [
        "Memory",
        analysis.resource_metadata.resources.memory.current || "N/A",
        analysis.resource_metadata.resources.memory.request || "N/A",
        analysis.resource_metadata.resources.memory.limit || "N/A",
      ],
    ];

    autoTable(doc, {
      startY: yPosition,
      head: [["Tipo", "Current", "Request", "Limit"]],
      body: resourcesRows,
      theme: "striped",
      headStyles: {
        fillColor: [149, 165, 166],
        textColor: 255,
        fontStyle: "bold",
        fontSize: 9,
      },
      bodyStyles: {
        fontSize: 8,
      },
      columnStyles: {
        0: { fontStyle: "bold", cellWidth: 30 },
        1: { cellWidth: "auto" },
        2: { cellWidth: "auto" },
        3: { cellWidth: "auto" },
      },
      margin: { left: 14, right: 14 },
    });

    yPosition = (doc as any).lastAutoTable.finalY + 12;
  }

  // ========================================================================
  // SEÇÃO 2: SUMÁRIO EXECUTIVO
  // ========================================================================
  // Verificar se precisa de nova página
  if (yPosition > 200) {
    doc.addPage();
    yPosition = 20;
  }

  doc.setFontSize(14);
  doc.setFont("helvetica", "bold");
  doc.setFillColor(240, 240, 240);
  doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
  doc.text("SUMARIO EXECUTIVO", 16, yPosition);
  yPosition += 10;

  if (analysis.executive_summary?.quick_summary) {
    // Quick Summary (texto longo, usar splitTextToSize)
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    const summaryLines = doc.splitTextToSize(analysis.executive_summary.quick_summary, pageWidth - 28);
    doc.text(summaryLines, 16, yPosition);
    yPosition += summaryLines.length * 6 + 6;

    // Métricas do sumário (Severity, Status, Tempo Estimado)
    const summaryRows: string[][] = [
      ["Severity", analysis.executive_summary.severity.toUpperCase()],
      ["Status", analysis.executive_summary.status.toUpperCase()],
      ["Tempo Estimado", analysis.executive_summary.time_to_resolve],
    ];

    autoTable(doc, {
      startY: yPosition,
      head: [["Metrica", "Valor"]],
      body: summaryRows,
      theme: "grid",
      headStyles: {
        fillColor: [52, 152, 219],
        textColor: 255,
        fontStyle: "bold",
        fontSize: 10,
      },
      bodyStyles: {
        fontSize: 9,
      },
      columnStyles: {
        0: { fontStyle: "bold", cellWidth: 60 },
        1: { cellWidth: "auto" },
      },
      margin: { left: 14, right: 14 },
    });

    yPosition = (doc as any).lastAutoTable.finalY + 12;
  } else {
    // Fallback: análise legada (texto plano)
    doc.setFontSize(10);
    doc.setFont("helvetica", "italic");
    doc.text("Analise estruturada nao disponivel (formato legado)", 16, yPosition);
    yPosition += 12;
  }

  // ========================================================================
  // SEÇÃO 3: ANÁLISE DE CAUSA RAIZ
  // ========================================================================
  if (analysis.root_cause_analysis?.symptom) {
    // Verificar se precisa de nova página
    if (yPosition > 200) {
      doc.addPage();
      yPosition = 20;
    }

    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setFillColor(240, 240, 240);
    doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
    doc.text("ANALISE DE CAUSA RAIZ", 16, yPosition);
    yPosition += 10;

    // Sintoma
    doc.setFontSize(11);
    doc.setFont("helvetica", "bold");
    doc.text("Sintoma:", 16, yPosition);
    yPosition += 6;

    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    const symptomLines = doc.splitTextToSize(analysis.root_cause_analysis.symptom, pageWidth - 28);
    doc.text(symptomLines, 16, yPosition);
    yPosition += symptomLines.length * 6 + 6;

    // Causas Prováveis
    doc.setFontSize(11);
    doc.setFont("helvetica", "bold");
    doc.text("Causas Provaveis:", 16, yPosition);
    yPosition += 6;

    analysis.root_cause_analysis.probable_causes?.forEach((cause, index) => {
      // Verificar se precisa de nova página
      if (yPosition > 260) {
        doc.addPage();
        yPosition = 20;
      }

      doc.setFontSize(10);
      doc.setFont("helvetica", "normal");
      const causeLines = doc.splitTextToSize(`${index + 1}. ${cause}`, pageWidth - 32);
      doc.text(causeLines, 20, yPosition);
      yPosition += causeLines.length * 6 + 2;
    });

    yPosition += 4;

    // Evidências
    if (analysis.root_cause_analysis.evidence && analysis.root_cause_analysis.evidence.length > 0) {
      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text("Evidencias:", 16, yPosition);
      yPosition += 6;

      analysis.root_cause_analysis.evidence.forEach((evidence) => {
        // Verificar se precisa de nova página
        if (yPosition > 260) {
          doc.addPage();
          yPosition = 20;
        }

        doc.setFontSize(9);
        doc.setFont("helvetica", "italic");
        const evidenceLines = doc.splitTextToSize(`- ${evidence}`, pageWidth - 32);
        doc.text(evidenceLines, 20, yPosition);
        yPosition += evidenceLines.length * 5 + 2;
      });

      yPosition += 4;
    }

    // Confiança
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    doc.text(`Confianca: ${analysis.root_cause_analysis.confidence.toUpperCase()}`, 16, yPosition);
    yPosition += 12;
  }

  // ========================================================================
  // SEÇÃO 4: IMPACTO E SEVERIDADE
  // ========================================================================
  if (analysis.impact_assessment?.severity) {
    // Verificar se precisa de nova página
    if (yPosition > 200) {
      doc.addPage();
      yPosition = 20;
    }

    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setFillColor(240, 240, 240);
    doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
    doc.text("IMPACTO E SEVERIDADE", 16, yPosition);
    yPosition += 10;

    const impactRows: string[][] = [
      ["Usuarios Afetados", analysis.impact_assessment.affected_users],
      ["Downtime Estimado", analysis.impact_assessment.downtime_estimate],
      ["SLA Breach", analysis.impact_assessment.sla_breach ? "SIM" : "NAO"],
    ];

    autoTable(doc, {
      startY: yPosition,
      head: [["Campo", "Valor"]],
      body: impactRows,
      theme: "grid",
      headStyles: {
        fillColor: [52, 152, 219],
        textColor: 255,
        fontStyle: "bold",
        fontSize: 10,
      },
      bodyStyles: {
        fontSize: 9,
      },
      columnStyles: {
        0: { fontStyle: "bold", cellWidth: 60 },
        1: { cellWidth: "auto" },
      },
      margin: { left: 14, right: 14 },
    });

    yPosition = (doc as any).lastAutoTable.finalY + 8;

    if (analysis.impact_assessment.business_impact) {
      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text("Impacto no Negocio:", 16, yPosition);
      yPosition += 6;

      doc.setFontSize(10);
      doc.setFont("helvetica", "normal");
      const impactLines = doc.splitTextToSize(analysis.impact_assessment.business_impact, pageWidth - 28);
      doc.text(impactLines, 16, yPosition);
      yPosition += impactLines.length * 6 + 12;
    }
  }

  // ========================================================================
  // SEÇÃO 5: RECOMENDAÇÕES PRIORIZADAS
  // ========================================================================
  if (analysis.recommendations && analysis.recommendations.length > 0) {
    // Verificar se precisa de nova página
    if (yPosition > 180) {
      doc.addPage();
      yPosition = 20;
    }

    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setFillColor(240, 240, 240);
    doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
    doc.text("ACOES RECOMENDADAS", 16, yPosition);
    yPosition += 10;

    analysis.recommendations.forEach((rec, index) => {
      // Verificar se precisa de nova página
      if (yPosition > 220) {
        doc.addPage();
        yPosition = 20;
      }

      // Título da recomendação
      doc.setFontSize(12);
      doc.setFont("helvetica", "bold");
      doc.setFillColor(220, 220, 220);
      doc.rect(14, yPosition - 4, pageWidth - 28, 8, "F");
      doc.text(`${index + 1}. ${rec.title} (Prioridade: ${rec.priority})`, 16, yPosition);
      yPosition += 10;

      // Descrição
      doc.setFontSize(10);
      doc.setFont("helvetica", "normal");
      const descLines = doc.splitTextToSize(rec.description, pageWidth - 28);
      doc.text(descLines, 16, yPosition);
      yPosition += descLines.length * 6 + 4;

      // Comandos (se existir)
      if (rec.commands && rec.commands.length > 0) {
        doc.setFontSize(10);
        doc.setFont("helvetica", "bold");
        doc.text("Comandos:", 16, yPosition);
        yPosition += 6;

        doc.setFont("courier", "normal");
        doc.setFontSize(9);
        doc.setFillColor(245, 245, 245);

        rec.commands.forEach((cmd) => {
          // Verificar se precisa de nova página
          if (yPosition > 270) {
            doc.addPage();
            yPosition = 20;
          }

          const cmdLines = doc.splitTextToSize(`  $ ${cmd}`, pageWidth - 28);
          cmdLines.forEach((line: string) => {
            doc.rect(14, yPosition - 3, pageWidth - 28, 5, "F");
            doc.text(line, 16, yPosition);
            yPosition += 5;
          });
        });

        yPosition += 4;
      }

      // Metadata (Tempo Estimado, Risco, Impacto)
      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      const metaLine = `Tempo Estimado: ${rec.time_estimate}  |  Risco: ${rec.risk_level.toUpperCase()}  |  Impacto: ${rec.impact_level.toUpperCase()}`;
      doc.text(metaLine, 16, yPosition);
      yPosition += 12;
    });
  } else if (analysis.suggestions && analysis.suggestions.length > 0) {
    // Fallback: usar sugestões legadas
    // Verificar se precisa de nova página
    if (yPosition > 180) {
      doc.addPage();
      yPosition = 20;
    }

    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setFillColor(240, 240, 240);
    doc.rect(14, yPosition - 5, pageWidth - 28, 10, "F");
    doc.text("SUGESTOES", 16, yPosition);
    yPosition += 10;

    analysis.suggestions.forEach((sug, index) => {
      // Verificar se precisa de nova página
      if (yPosition > 240) {
        doc.addPage();
        yPosition = 20;
      }

      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      const sugLines = doc.splitTextToSize(`${index + 1}. ${sug.description}`, pageWidth - 28);
      doc.text(sugLines, 16, yPosition);
      yPosition += sugLines.length * 6 + 2;

      if (sug.command) {
        doc.setFont("courier", "normal");
        doc.setFontSize(9);
        doc.setFillColor(245, 245, 245);
        doc.rect(14, yPosition - 3, pageWidth - 28, 5, "F");
        doc.text(`  $ ${sug.command}`, 16, yPosition);
        yPosition += 5;
      }

      doc.setFontSize(9);
      doc.setFont("helvetica", "italic");
      doc.text(`Prioridade: ${sug.priority.toUpperCase()}`, 16, yPosition);
      yPosition += 10;
    });
  }

  // ========================================================================
  // FOOTER
  // ========================================================================
  const totalPages = doc.getNumberOfPages();
  for (let i = 1; i <= totalPages; i++) {
    doc.setPage(i);
    doc.setFontSize(8);
    doc.setFont("helvetica", "italic");
    doc.setTextColor(128, 128, 128);

    const footerText = `Gerado por: ${analysis.provider} (${analysis.model || "default"}) | Tokens: ${analysis.tokens_used || 0} | Tempo: ${analysis.response_time?.toFixed(2) || "N/A"}s`;
    doc.text(footerText, pageWidth / 2, 285, { align: "center" });

    // Número da página
    doc.text(`Pagina ${i} de ${totalPages}`, pageWidth - 20, 285, { align: "right" });
  }

  // Salvar PDF
  const filename = `diagnostico_${analysis.resource_name}_${new Date().toISOString().split("T")[0]}.pdf`;
  doc.save(filename);
};

// ============================================================================
// GERAÇÃO DE MARKDOWN
// ============================================================================
export const generateAIDiagnosticsMarkdown = (analysis: AnalysisResult): string => {
  let md = "";

  // Header
  md += "# ANALISE DE DIAGNOSTICO - AI\n\n";
  md += `**Recurso**: ${analysis.resource_type}/${analysis.namespace}/${analysis.resource_name}\n\n`;
  md += `**Cluster**: ${analysis.cluster}\n\n`;
  md += `**Analisado em**: ${formatDate(analysis.analyzed_at)}\n\n`;
  md += "---\n\n";

  // SEÇÃO 1: METADADOS DO RECURSO
  if (analysis.resource_metadata) {
    md += "## METADADOS DO RECURSO\n\n";
    md += "| Campo | Valor |\n";
    md += "|-------|-------|\n";
    md += `| **Nome** | ${analysis.resource_metadata.name} |\n`;
    md += `| **Namespace** | ${analysis.resource_metadata.namespace} |\n`;
    md += `| **Cluster** | ${analysis.resource_metadata.cluster} |\n`;

    if (analysis.resource_metadata.version) {
      md += `| **Versao** | ${analysis.resource_metadata.version} |\n`;
    }

    md += `| **Status** | ${analysis.resource_metadata.status} |\n`;
    md += `| **Age** | ${analysis.resource_metadata.age} |\n`;
    md += `| **Restart Count** | ${analysis.resource_metadata.restart_count} |\n`;

    if (analysis.resource_metadata.node_name) {
      md += `| **Node** | ${analysis.resource_metadata.node_name} |\n`;
    }

    md += "\n### Recursos\n\n";
    md += "| Tipo | Current | Request | Limit |\n";
    md += "|------|---------|---------|-------|\n";
    md += `| **CPU** | ${analysis.resource_metadata.resources.cpu.current || "N/A"} | ${analysis.resource_metadata.resources.cpu.request || "N/A"} | ${analysis.resource_metadata.resources.cpu.limit || "N/A"} |\n`;
    md += `| **Memory** | ${analysis.resource_metadata.resources.memory.current || "N/A"} | ${analysis.resource_metadata.resources.memory.request || "N/A"} | ${analysis.resource_metadata.resources.memory.limit || "N/A"} |\n\n`;
  }

  // SEÇÃO 2: SUMÁRIO EXECUTIVO
  md += "## SUMARIO EXECUTIVO\n\n";
  if (analysis.executive_summary?.quick_summary) {
    md += analysis.executive_summary.quick_summary + "\n\n";

    md += "| Campo | Valor |\n";
    md += "|-------|-------|\n";
    md += `| **Severity** | ${analysis.executive_summary.severity.toUpperCase()} |\n`;
    md += `| **Status** | ${analysis.executive_summary.status.toUpperCase()} |\n`;
    md += `| **Tempo Estimado** | ${analysis.executive_summary.time_to_resolve} |\n\n`;
  } else {
    md += analysis.analysis + "\n\n";
  }

  // SEÇÃO 3: ANÁLISE DE CAUSA RAIZ
  if (analysis.root_cause_analysis?.symptom) {
    md += "## ANALISE DE CAUSA RAIZ\n\n";

    md += "### Sintoma\n\n";
    md += analysis.root_cause_analysis.symptom + "\n\n";

    md += "### Causas Provaveis\n\n";
    analysis.root_cause_analysis.probable_causes?.forEach((cause, index) => {
      md += `${index + 1}. ${cause}\n`;
    });
    md += "\n";

    if (analysis.root_cause_analysis.evidence && analysis.root_cause_analysis.evidence.length > 0) {
      md += "### Evidencias\n\n";
      analysis.root_cause_analysis.evidence.forEach((evidence) => {
        md += `- ${evidence}\n`;
      });
      md += "\n";
    }

    md += `**Confianca**: ${analysis.root_cause_analysis.confidence.toUpperCase()}\n\n`;
  }

  // SEÇÃO 4: IMPACTO E SEVERIDADE
  if (analysis.impact_assessment?.severity) {
    md += "## IMPACTO E SEVERIDADE\n\n";

    md += "| Campo | Valor |\n";
    md += "|-------|-------|\n";
    md += `| **Usuarios Afetados** | ${analysis.impact_assessment.affected_users} |\n`;
    md += `| **Downtime Estimado** | ${analysis.impact_assessment.downtime_estimate} |\n`;
    md += `| **SLA Breach** | ${analysis.impact_assessment.sla_breach ? "SIM" : "NAO"} |\n\n`;

    if (analysis.impact_assessment.business_impact) {
      md += "### Impacto no Negocio\n\n";
      md += analysis.impact_assessment.business_impact + "\n\n";
    }
  }

  // SEÇÃO 5: RECOMENDAÇÕES PRIORIZADAS
  if (analysis.recommendations && analysis.recommendations.length > 0) {
    md += "## ACOES RECOMENDADAS\n\n";

    analysis.recommendations.forEach((rec, index) => {
      md += `### ${index + 1}. ${rec.title} (Prioridade: ${rec.priority})\n\n`;
      md += rec.description + "\n\n";

      if (rec.commands && rec.commands.length > 0) {
        md += "**Comandos**:\n\n";
        md += "```bash\n";
        rec.commands.forEach((cmd) => {
          md += cmd + "\n";
        });
        md += "```\n\n";
      }

      md += `- **Tempo Estimado**: ${rec.time_estimate}\n`;
      md += `- **Risco**: ${rec.risk_level.toUpperCase()}\n`;
      md += `- **Impacto**: ${rec.impact_level.toUpperCase()}\n\n`;
    });
  } else if (analysis.suggestions && analysis.suggestions.length > 0) {
    md += "## SUGESTOES\n\n";

    analysis.suggestions.forEach((sug, index) => {
      md += `### ${index + 1}. ${sug.description}\n\n`;

      if (sug.command) {
        md += "```bash\n";
        md += sug.command + "\n";
        md += "```\n\n";
      }

      md += `**Prioridade**: ${sug.priority.toUpperCase()}\n\n`;
    });
  }

  // Footer
  md += "---\n\n";
  md += `**Gerado por**: ${analysis.provider} (${analysis.model || "default"}) | **Tokens**: ${analysis.tokens_used || 0} | **Tempo**: ${analysis.response_time?.toFixed(2) || "N/A"}s\n`;

  return md;
};

// ============================================================================
// GERAÇÃO DE CSV
// ============================================================================
export const generateAIDiagnosticsCSV = (analysis: AnalysisResult): string => {
  let csv = "";

  // Header geral
  csv += "ANALISE DE DIAGNOSTICO - AI\n";
  csv += `Recurso,${analysis.resource_type}/${analysis.namespace}/${analysis.resource_name}\n`;
  csv += `Cluster,${analysis.cluster}\n`;
  csv += `Analisado em,${formatDate(analysis.analyzed_at)}\n`;
  csv += "\n";

  // METADADOS DO RECURSO
  if (analysis.resource_metadata) {
    csv += "METADADOS DO RECURSO\n";
    csv += "Campo,Valor\n";
    csv += `Nome,${analysis.resource_metadata.name}\n`;
    csv += `Namespace,${analysis.resource_metadata.namespace}\n`;
    csv += `Cluster,${analysis.resource_metadata.cluster}\n`;

    if (analysis.resource_metadata.version) {
      csv += `Versao,${analysis.resource_metadata.version}\n`;
    }

    csv += `Status,${analysis.resource_metadata.status}\n`;
    csv += `Age,${analysis.resource_metadata.age}\n`;
    csv += `Restart Count,${analysis.resource_metadata.restart_count}\n`;

    if (analysis.resource_metadata.node_name) {
      csv += `Node,${analysis.resource_metadata.node_name}\n`;
    }

    csv += "\n";
    csv += "Recursos\n";
    csv += "Tipo,Current,Request,Limit\n";
    csv += `CPU,${analysis.resource_metadata.resources.cpu.current || "N/A"},${analysis.resource_metadata.resources.cpu.request || "N/A"},${analysis.resource_metadata.resources.cpu.limit || "N/A"}\n`;
    csv += `Memory,${analysis.resource_metadata.resources.memory.current || "N/A"},${analysis.resource_metadata.resources.memory.request || "N/A"},${analysis.resource_metadata.resources.memory.limit || "N/A"}\n`;
    csv += "\n";
  }

  // SUMÁRIO EXECUTIVO
  csv += "SUMARIO EXECUTIVO\n";
  if (analysis.executive_summary?.quick_summary) {
    csv += `Resumo,"${analysis.executive_summary.quick_summary.replace(/"/g, '""')}"\n`;
    csv += `Severity,${analysis.executive_summary.severity.toUpperCase()}\n`;
    csv += `Status,${analysis.executive_summary.status.toUpperCase()}\n`;
    csv += `Tempo Estimado,${analysis.executive_summary.time_to_resolve}\n`;
  } else {
    csv += `Analise,"${analysis.analysis.replace(/"/g, '""')}"\n`;
  }
  csv += "\n";

  // ANÁLISE DE CAUSA RAIZ
  if (analysis.root_cause_analysis?.symptom) {
    csv += "ANALISE DE CAUSA RAIZ\n";
    csv += `Sintoma,"${analysis.root_cause_analysis.symptom.replace(/"/g, '""')}"\n`;

    csv += "Causas Provaveis\n";
    analysis.root_cause_analysis.probable_causes?.forEach((cause, index) => {
      csv += `Causa ${index + 1},"${cause.replace(/"/g, '""')}"\n`;
    });

    if (analysis.root_cause_analysis.evidence && analysis.root_cause_analysis.evidence.length > 0) {
      csv += "Evidencias\n";
      analysis.root_cause_analysis.evidence.forEach((evidence, index) => {
        csv += `Evidencia ${index + 1},"${evidence.replace(/"/g, '""')}"\n`;
      });
    }

    csv += `Confianca,${analysis.root_cause_analysis.confidence.toUpperCase()}\n`;
    csv += "\n";
  }

  // IMPACTO E SEVERIDADE
  if (analysis.impact_assessment?.severity) {
    csv += "IMPACTO E SEVERIDADE\n";
    csv += `Usuarios Afetados,${analysis.impact_assessment.affected_users}\n`;
    csv += `Downtime Estimado,${analysis.impact_assessment.downtime_estimate}\n`;
    csv += `SLA Breach,${analysis.impact_assessment.sla_breach ? "SIM" : "NAO"}\n`;

    if (analysis.impact_assessment.business_impact) {
      csv += `Impacto no Negocio,"${analysis.impact_assessment.business_impact.replace(/"/g, '""')}"\n`;
    }
    csv += "\n";
  }

  // RECOMENDAÇÕES PRIORIZADAS
  if (analysis.recommendations && analysis.recommendations.length > 0) {
    csv += "ACOES RECOMENDADAS\n";
    csv += "Prioridade,Titulo,Descricao,Comandos,Tempo,Risco,Impacto\n";

    analysis.recommendations.forEach((rec) => {
      const commands = rec.commands ? rec.commands.join("; ") : "";
      csv += `${rec.priority},"${rec.title.replace(/"/g, '""')}","${rec.description.replace(/"/g, '""')}","${commands.replace(/"/g, '""')}",${rec.time_estimate},${rec.risk_level.toUpperCase()},${rec.impact_level.toUpperCase()}\n`;
    });
  } else if (analysis.suggestions && analysis.suggestions.length > 0) {
    csv += "SUGESTOES\n";
    csv += "Tipo,Descricao,Comando,Prioridade\n";

    analysis.suggestions.forEach((sug) => {
      csv += `${sug.type},"${sug.description.replace(/"/g, '""')}","${sug.command?.replace(/"/g, '""') || ""}",${sug.priority.toUpperCase()}\n`;
    });
  }

  csv += "\n";

  // Footer
  csv += `Provider,${analysis.provider}\n`;
  csv += `Model,${analysis.model || "default"}\n`;
  csv += `Tokens Used,${analysis.tokens_used || 0}\n`;
  csv += `Response Time,${analysis.response_time?.toFixed(2) || "N/A"}s\n`;

  return csv;
};
