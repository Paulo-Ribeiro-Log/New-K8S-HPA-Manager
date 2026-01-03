import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import type { HealthCheckResult } from "@/types/healthcheck";

export interface ReportData {
  results: Record<string, HealthCheckResult>;
  timestamp: string;
}

// Formatar data para exibição
const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr);
  return date.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
};

// Formatar duração
const formatDuration = (ms: number): string => {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}m ${remainingSeconds}s`;
};

// Extrair mensagens de eventos (apenas warnings e críticos)
interface EventMessage {
  type: "warning" | "critical";
  message: string;
  phase?: string;
}

const extractIssues = (result: HealthCheckResult): EventMessage[] => {
  const issues: EventMessage[] = [];

  // Procurar por problemas em deployment_issues
  if (result.deployment_issues && result.deployment_issues.length > 0) {
    result.deployment_issues.forEach((issue) => {
      issues.push({
        type: issue.severity === "critical" ? "critical" : "warning",
        message: issue.message,
        phase: "Deployments",
      });
    });
  }

  // Procurar por problemas em service_issues
  if (result.service_issues && result.service_issues.length > 0) {
    result.service_issues.forEach((issue) => {
      issues.push({
        type: issue.severity === "critical" ? "critical" : "warning",
        message: issue.message,
        phase: "Services",
      });
    });
  }

  // Procurar por problemas em config_issues
  if (result.config_issues && result.config_issues.length > 0) {
    result.config_issues.forEach((issue) => {
      issues.push({
        type: issue.severity === "critical" ? "critical" : "warning",
        message: issue.message,
        phase: "Configs",
      });
    });
  }

  return issues;
};

// ============================================================================
// GERAÇÃO DE PDF
// ============================================================================
export const generatePDFReport = (data: ReportData): void => {
  const doc = new jsPDF({
    orientation: "portrait",
    unit: "mm",
    format: "a4",
  });
  const pageWidth = doc.internal.pageSize.getWidth();
  let yPosition = 20;

  // Header box
  doc.setFillColor(41, 128, 185); // Azul profissional
  doc.rect(0, 0, pageWidth, 35, "F");

  // Título
  doc.setFontSize(20);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255); // Branco
  doc.text("RELATORIO DE HEALTH CHECKING", pageWidth / 2, 15, { align: "center" });

  // Data do relatório
  doc.setFontSize(10);
  doc.setFont("helvetica", "normal");
  doc.text(`Data de Geracao: ${formatDate(data.timestamp)}`, pageWidth / 2, 25, { align: "center" });

  // Reset cor do texto
  doc.setTextColor(0, 0, 0);
  yPosition = 45;

  // Resumo geral
  const clusters = Object.keys(data.results);
  const totalHealthy = clusters.reduce((sum, c) => sum + data.results[c].healthy_count, 0);
  const totalWarnings = clusters.reduce((sum, c) => sum + data.results[c].warning_count, 0);
  const totalCritical = clusters.reduce((sum, c) => sum + data.results[c].critical_count, 0);

  // Seção de sumário executivo
  doc.setFontSize(14);
  doc.setFont("helvetica", "bold");
  doc.text("SUMARIO EXECUTIVO", 14, yPosition);
  yPosition += 8;

  // Tabela de resumo usando autoTable
  autoTable(doc, {
    startY: yPosition,
    head: [['Metrica', 'Valor']],
    body: [
      ['Clusters Analisados', clusters.length.toString()],
      ['Checks Saudaveis', totalHealthy.toString()],
      ['Avisos (Warnings)', totalWarnings.toString()],
      ['Criticos', totalCritical.toString()],
    ],
    theme: 'grid',
    headStyles: {
      fillColor: [52, 152, 219],
      textColor: 255,
      fontStyle: 'bold',
      fontSize: 10,
    },
    bodyStyles: {
      fontSize: 9,
    },
    columnStyles: {
      0: { fontStyle: 'bold', cellWidth: 80 },
      1: { halign: 'right', cellWidth: 'auto' },
    },
    margin: { left: 14, right: 14 },
  });

  yPosition = (doc as any).lastAutoTable.finalY + 15;

  // Título da seção de análise detalhada
  doc.setFontSize(14);
  doc.setFont("helvetica", "bold");
  doc.text("ANALISE DETALHADA POR CLUSTER", 14, yPosition);
  yPosition += 10;

  // Detalhes por cluster
  clusters.forEach((cluster, index) => {
    const result = data.results[cluster];
    const issues = extractIssues(result);
    const warnings = issues.filter((i) => i.type === "warning");
    const criticals = issues.filter((i) => i.type === "critical");

    // Verificar se precisa de nova página
    if (yPosition > 240) {
      doc.addPage();
      yPosition = 20;
    }

    // Cabeçalho do cluster com número
    doc.setFontSize(12);
    doc.setFont("helvetica", "bold");
    doc.text(`${index + 1}. Cluster: ${cluster}`, 14, yPosition);
    yPosition += 8;

    // Tabela de métricas do cluster
    autoTable(doc, {
      startY: yPosition,
      head: [['Parametro', 'Valor']],
      body: [
        ['Data da Analise', formatDate(result.started_at)],
        ['Status Geral', result.overall_status.toUpperCase()],
        ['Duracao da Analise', formatDuration(result.duration_ms)],
        ['Total de Checks', result.total_checks.toString()],
        ['Checks Saudaveis', result.healthy_count.toString()],
        ['Avisos', result.warning_count.toString()],
        ['Criticos', result.critical_count.toString()],
      ],
      theme: 'striped',
      headStyles: {
        fillColor: [149, 165, 166],
        textColor: 255,
        fontStyle: 'bold',
        fontSize: 9,
      },
      bodyStyles: {
        fontSize: 8,
      },
      columnStyles: {
        0: { fontStyle: 'bold', cellWidth: 80 },
        1: { halign: 'right', cellWidth: 'auto' },
      },
      margin: { left: 20, right: 14 },
    });

    yPosition = (doc as any).lastAutoTable.finalY + 8;

    // Avisos (Warnings)
    if (warnings.length > 0) {
      if (yPosition > 240) {
        doc.addPage();
        yPosition = 20;
      }

      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text(`Avisos Identificados (${warnings.length})`, 20, yPosition);
      yPosition += 6;

      const warningData = warnings.map((warning, idx) => [
        (idx + 1).toString(),
        warning.phase || 'N/A',
        warning.message,
      ]);

      autoTable(doc, {
        startY: yPosition,
        head: [['#', 'Fase', 'Descricao']],
        body: warningData,
        theme: 'grid',
        headStyles: {
          fillColor: [243, 156, 18],
          textColor: 255,
          fontStyle: 'bold',
          fontSize: 9,
        },
        bodyStyles: {
          fontSize: 8,
        },
        columnStyles: {
          0: { cellWidth: 10, halign: 'center' },
          1: { cellWidth: 28, fontStyle: 'bold' },
          2: { cellWidth: 134, overflow: 'linebreak' },
        },
        styles: {
          overflow: 'linebreak',
          cellPadding: 2,
          minCellHeight: 8,
        },
        margin: { left: 24, right: 14 },
        tableWidth: 'auto',
      });

      yPosition = (doc as any).lastAutoTable.finalY + 8;
    }

    // Críticos
    if (criticals.length > 0) {
      if (yPosition > 240) {
        doc.addPage();
        yPosition = 20;
      }

      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text(`Problemas Criticos (${criticals.length})`, 20, yPosition);
      yPosition += 6;

      const criticalData = criticals.map((critical, idx) => [
        (idx + 1).toString(),
        critical.phase || 'N/A',
        critical.message,
      ]);

      autoTable(doc, {
        startY: yPosition,
        head: [['#', 'Fase', 'Descricao']],
        body: criticalData,
        theme: 'grid',
        headStyles: {
          fillColor: [231, 76, 60],
          textColor: 255,
          fontStyle: 'bold',
          fontSize: 9,
        },
        bodyStyles: {
          fontSize: 8,
        },
        columnStyles: {
          0: { cellWidth: 10, halign: 'center' },
          1: { cellWidth: 28, fontStyle: 'bold' },
          2: { cellWidth: 134, overflow: 'linebreak' },
        },
        styles: {
          overflow: 'linebreak',
          cellPadding: 2,
          minCellHeight: 8,
        },
        margin: { left: 24, right: 14 },
        tableWidth: 'auto',
      });

      yPosition = (doc as any).lastAutoTable.finalY + 8;
    }

    // Se não há problemas
    if (warnings.length === 0 && criticals.length === 0) {
      doc.setFillColor(46, 204, 113); // Verde
      doc.setDrawColor(39, 174, 96);
      doc.setLineWidth(0.5);
      doc.roundedRect(20, yPosition - 3, pageWidth - 34, 10, 2, 2, 'FD');

      doc.setFontSize(9);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(255, 255, 255);
      doc.text("Status: Nenhum problema identificado - Todos os checks estao saudaveis", 24, yPosition + 3);
      doc.setTextColor(0, 0, 0);
      yPosition += 12;
    }

    yPosition += 10;
  });

  // Salvar PDF (usar data da análise ao invés de data atual)
  const reportDate = new Date(data.timestamp).toISOString().split("T")[0];
  const filename = `health-check-report-${reportDate}.pdf`;
  doc.save(filename);
};

// ============================================================================
// GERAÇÃO DE MARKDOWN
// ============================================================================
export const generateMarkdownReport = (data: ReportData): void => {
  const clusters = Object.keys(data.results);
  const totalHealthy = clusters.reduce((sum, c) => sum + data.results[c].healthy_count, 0);
  const totalWarnings = clusters.reduce((sum, c) => sum + data.results[c].warning_count, 0);
  const totalCritical = clusters.reduce((sum, c) => sum + data.results[c].critical_count, 0);

  let markdown = "";

  // Cabeçalho do documento
  markdown += `# RELATORIO DE HEALTH CHECKING\n\n`;
  markdown += `**Data de Geracao:** ${formatDate(data.timestamp)}\n\n`;
  markdown += `---\n\n`;

  // Sumário executivo
  markdown += `## SUMARIO EXECUTIVO\n\n`;
  markdown += `| Metrica | Valor |\n`;
  markdown += `|---------|-------|\n`;
  markdown += `| Clusters Analisados | ${clusters.length} |\n`;
  markdown += `| Checks Saudaveis | ${totalHealthy} |\n`;
  markdown += `| Avisos (Warnings) | ${totalWarnings} |\n`;
  markdown += `| Criticos | ${totalCritical} |\n\n`;
  markdown += `---\n\n`;

  // Detalhes por cluster
  markdown += `## ANALISE DETALHADA POR CLUSTER\n\n`;

  clusters.forEach((cluster, clusterIdx) => {
    const result = data.results[cluster];
    const issues = extractIssues(result);
    const warnings = issues.filter((i) => i.type === "warning");
    const criticals = issues.filter((i) => i.type === "critical");

    markdown += `### ${clusterIdx + 1}. Cluster: ${cluster}\n\n`;

    // Tabela de métricas do cluster
    markdown += `| Parametro | Valor |\n`;
    markdown += `|-----------|-------|\n`;
    markdown += `| Data da Analise | ${formatDate(result.started_at)} |\n`;
    markdown += `| Status Geral | **${result.overall_status.toUpperCase()}** |\n`;
    markdown += `| Duracao da Analise | ${formatDuration(result.duration_ms)} |\n`;
    markdown += `| Total de Checks | ${result.total_checks} |\n`;
    markdown += `| Checks Saudaveis | ${result.healthy_count} |\n`;
    markdown += `| Avisos | ${result.warning_count} |\n`;
    markdown += `| Criticos | ${result.critical_count} |\n\n`;

    // Avisos
    if (warnings.length > 0) {
      markdown += `#### Avisos Identificados (${warnings.length})\n\n`;
      markdown += `| # | Fase | Descricao |\n`;
      markdown += `|---|------|--------|\n`;
      warnings.forEach((warning, idx) => {
        markdown += `| ${idx + 1} | ${warning.phase} | ${warning.message} |\n`;
      });
      markdown += `\n`;
    }

    // Críticos
    if (criticals.length > 0) {
      markdown += `#### Problemas Criticos (${criticals.length})\n\n`;
      markdown += `| # | Fase | Descricao |\n`;
      markdown += `|---|------|--------|\n`;
      criticals.forEach((critical, idx) => {
        markdown += `| ${idx + 1} | ${critical.phase} | ${critical.message} |\n`;
      });
      markdown += `\n`;
    }

    // Se não há problemas
    if (warnings.length === 0 && criticals.length === 0) {
      markdown += `> **Status:** Nenhum problema identificado. Todos os checks estao em estado saudavel.\n\n`;
    }

    markdown += `---\n\n`;
  });

  // Download do arquivo (usar data da análise ao invés de data atual)
  const reportDate = new Date(data.timestamp).toISOString().split("T")[0];
  const blob = new Blob([markdown], { type: "text/markdown" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `health-check-report-${reportDate}.md`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};

// ============================================================================
// GERAÇÃO DE CSV
// ============================================================================
export const generateCSVReport = (data: ReportData): void => {
  const clusters = Object.keys(data.results);
  let csv = "";

  // Cabeçalho
  csv += "Data_Analise,Cluster,Status,Duracao_ms,Total_Checks,Healthy,Warnings,Critical,Tipo_Alerta,Fase,Mensagem\n";

  // Linhas de dados
  clusters.forEach((cluster) => {
    const result = data.results[cluster];
    const issues = extractIssues(result);
    const analysisDate = formatDate(result.started_at);

    if (issues.length === 0) {
      // Se não há problemas, adicionar uma linha com resumo
      csv += `"${analysisDate}","${cluster}","${result.overall_status}",${result.duration_ms},${result.total_checks},${result.healthy_count},${result.warning_count},${result.critical_count},"N/A","N/A","Nenhum problema encontrado"\n`;
    } else {
      // Adicionar uma linha para cada problema
      issues.forEach((issue) => {
        const message = issue.message.replace(/"/g, '""'); // Escape aspas
        csv += `"${analysisDate}","${cluster}","${result.overall_status}",${result.duration_ms},${result.total_checks},${result.healthy_count},${result.warning_count},${result.critical_count},"${issue.type}","${issue.phase}","${message}"\n`;
      });
    }
  });

  // Download do arquivo (usar data da análise ao invés de data atual)
  const reportDate = new Date(data.timestamp).toISOString().split("T")[0];
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `health-check-report-${reportDate}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};
