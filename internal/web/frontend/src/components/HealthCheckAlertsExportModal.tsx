import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { AlertCircle, XCircle, Download, Server, FileText, FileSpreadsheet, Loader2 } from "lucide-react";
import type { HealthCheckProgress } from "@/types/healthcheck";
import { toast } from "sonner";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";

interface HealthCheckAlertsExportModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  alerts: HealthCheckProgress[];
}

export const HealthCheckAlertsExportModal = ({
  open,
  onOpenChange,
  cluster,
  alerts,
}: HealthCheckAlertsExportModalProps) => {
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportFormat, setExportFormat] = useState<"pdf" | "markdown" | "csv">("pdf");
  const [isExporting, setIsExporting] = useState(false);

  // Filtrar apenas warnings e criticals
  const filteredAlerts = alerts.filter(
    (event) => event.status === "warning" || event.status === "critical"
  );

  const warnings = filteredAlerts.filter((a) => a.status === "warning");
  const criticals = filteredAlerts.filter((a) => a.status === "critical");

  // Função para remover apenas emojis, mantendo todo o texto
  const removeEmojis = (text: string): string => {
    // Remove emojis comuns: ⚠️, ❌, ✅, 🔴, etc (mantém o texto)
    return text
      .replace(/[\u{1F300}-\u{1F9FF}]/gu, '') // Emojis gerais
      .replace(/[\u{2600}-\u{26FF}]/gu, '')   // Símbolos diversos (⚠️, ✅, ❌)
      .replace(/[\u{2700}-\u{27BF}]/gu, '')   // Dingbats
      .replace(/\uFE0F/g, '')                 // Variation selector (torna emoji colorido)
      .replace(/\s+/g, ' ')                   // Colapsar espaços múltiplos
      .trim();
  };

  // Gerar PDF usando jsPDF com autoTable
  const generatePDF = () => {
    const doc = new jsPDF({
      orientation: "portrait",
      unit: "mm",
      format: "a4",
    });

    const pageWidth = doc.internal.pageSize.getWidth();
    let yPosition = 20;

    // Header azul
    doc.setFillColor(41, 128, 185);
    doc.rect(0, 0, pageWidth, 35, "F");

    // Título
    doc.setFontSize(18);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(255, 255, 255);
    doc.text("RELATORIO DE ALERTAS", pageWidth / 2, 15, { align: "center" });

    // Subtítulo
    doc.setFontSize(12);
    doc.setFont("helvetica", "normal");
    doc.text("Health Check - Historico", pageWidth / 2, 22, { align: "center" });

    // Data
    doc.setFontSize(9);
    const timestamp = new Date().toLocaleString("pt-BR");
    doc.text(`Gerado em: ${timestamp}`, pageWidth / 2, 28, { align: "center" });

    // Reset cor do texto
    doc.setTextColor(0, 0, 0);
    yPosition = 45;

    // Sumário Executivo
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(41, 128, 185);
    doc.text("SUMARIO EXECUTIVO", 14, yPosition);
    yPosition += 8;

    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    doc.setTextColor(0, 0, 0);
    doc.text(`Cluster: ${cluster}`, 14, yPosition);
    yPosition += 6;
    doc.text(`Total de Warnings: ${warnings.length}`, 14, yPosition);
    yPosition += 6;
    doc.text(`Total de Criticals: ${criticals.length}`, 14, yPosition);
    yPosition += 12;

    // Título da seção de alertas
    doc.setFontSize(12);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(41, 128, 185);
    doc.text("Alertas Detectados", 14, yPosition);
    yPosition += 8;

    // Preparar dados para a tabela (REMOVER EMOJIS)
    const tableData = filteredAlerts.map((alert) => [
      alert.status.toUpperCase(),
      removeEmojis(alert.type || "N/A"),
      removeEmojis(alert.message),
    ]);

    // Gerar tabela com autoTable
    autoTable(doc, {
      startY: yPosition,
      head: [["Status", "Tipo", "Mensagem"]],
      body: tableData,
      theme: "grid",
      headStyles: {
        fillColor: [41, 128, 185],
        textColor: 255,
        fontStyle: "bold",
        fontSize: 9,
        halign: "center",
      },
      bodyStyles: {
        fontSize: 8,
        cellPadding: 3,
      },
      columnStyles: {
        0: {
          cellWidth: 25,
          halign: "center",
          fontStyle: "bold",
        },
        1: {
          cellWidth: 40,
        },
        2: {
          cellWidth: 117,
        },
      },
      styles: {
        overflow: "linebreak",
        cellPadding: 3,
        minCellHeight: 10,
        lineColor: [200, 200, 200],
        lineWidth: 0.5,
      },
      didParseCell: function (data) {
        if (data.section === "body" && data.column.index === 0) {
          const status = data.cell.raw as string;
          if (status === "CRITICAL") {
            data.cell.styles.fillColor = [239, 68, 68];
            data.cell.styles.textColor = [255, 255, 255];
          } else if (status === "WARNING") {
            data.cell.styles.fillColor = [251, 191, 36];
            data.cell.styles.textColor = [0, 0, 0];
          }
        }
      },
      margin: { left: 14, right: 14 },
    });

    // Salvar PDF
    doc.save(`health-check-alerts-${cluster}-${new Date().toISOString().split("T")[0]}.pdf`);
  };

  // Gerar Markdown
  const generateMarkdown = () => {
    const timestamp = new Date().toLocaleString("pt-BR");
    let md = `# RELATÓRIO DE ALERTAS - HEALTH CHECK\n\n`;
    md += `**Gerado em:** ${timestamp}\n\n`;
    md += `---\n\n`;

    md += `## SUMÁRIO EXECUTIVO\n\n`;
    md += `- **Cluster:** ${cluster}\n`;
    md += `- **Total de Warnings:** ${warnings.length}\n`;
    md += `- **Total de Criticals:** ${criticals.length}\n\n`;
    md += `---\n\n`;

    md += `## ALERTAS DETECTADOS\n\n`;

    md += `### 🖥️ Cluster: **${cluster}**\n\n`;
    md += `- **Warnings:** ${warnings.length}\n`;
    md += `- **Criticals:** ${criticals.length}\n\n`;

    md += `#### Detalhamento\n\n`;
    md += `| Status | Tipo | Mensagem |\n`;
    md += `|--------|------|----------|\n`;

    filteredAlerts.forEach((alert) => {
      const status = alert.status.toUpperCase();
      const type = removeEmojis(alert.type || "N/A");
      const message = removeEmojis(alert.message).replace(/\|/g, "\\|");
      md += `| **${status}** | ${type} | ${message} |\n`;
    });

    md += `\n---\n\n`;
    md += `*Relatório gerado automaticamente pelo K8s HPA Manager*\n`;

    const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `health-check-alerts-${cluster}-${new Date().toISOString().split("T")[0]}.md`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  // Gerar CSV
  const generateCSV = () => {
    const timestamp = new Date().toISOString().split("T")[0];
    const rows: string[] = [];

    // Header
    rows.push("Data_Geracao,Cluster,Status,Tipo,Mensagem");

    filteredAlerts.forEach((alert) => {
      const status = alert.status.toUpperCase();
      const type = removeEmojis(alert.type || "N/A");
      const message = `"${removeEmojis(alert.message).replace(/"/g, '""')}"`;
      rows.push(`${timestamp},${cluster},${status},${type},${message}`);
    });

    const csv = rows.join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `health-check-alerts-${cluster}-${timestamp}.csv`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  // Executar exportação
  const handleExport = async () => {
    setIsExporting(true);

    try {
      await new Promise((resolve) => setTimeout(resolve, 500));

      switch (exportFormat) {
        case "pdf":
          generatePDF();
          toast.success("Relatório PDF gerado com sucesso!");
          break;
        case "markdown":
          generateMarkdown();
          toast.success("Relatório Markdown gerado com sucesso!");
          break;
        case "csv":
          generateCSV();
          toast.success("Relatório CSV gerado com sucesso!");
          break;
      }

      setExportModalOpen(false);
      onOpenChange(false); // Fechar modal principal também
    } catch (error) {
      console.error("Failed to generate report:", error);
      toast.error("Erro ao gerar relatório");
    } finally {
      setIsExporting(false);
    }
  };

  const getFormatDescription = (fmt: string): string => {
    switch (fmt) {
      case "pdf":
        return "Documento formatado com tabelas profissionais. Ideal para apresentações e documentação.";
      case "markdown":
        return "Formato texto com marcação. Ideal para documentação técnica e versionamento (Git, Confluence).";
      case "csv":
        return "Planilha de valores separados por vírgula. Ideal para análise em Excel ou ferramentas de BI.";
      default:
        return "";
    }
  };

  return (
    <>
      {/* Modal Principal - Visualização dos Alertas */}
      <Dialog open={open && !exportModalOpen} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-4xl max-h-[90vh]">
          <DialogHeader>
            <div className="flex items-center justify-between">
              <div>
                <DialogTitle>Alertas Históricos - {cluster}</DialogTitle>
                <DialogDescription>
                  Warnings e Criticals detectados nesta análise
                </DialogDescription>
              </div>
              <Button
                onClick={() => setExportModalOpen(true)}
                variant="outline"
                size="sm"
                className="gap-2"
              >
                <Download className="h-4 w-4" />
                Exportar Relatório
              </Button>
            </div>
          </DialogHeader>

          {/* Resumo */}
          <div className="flex gap-4 pb-4 border-b">
            <div className="flex items-center gap-2">
              <Server className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">
                Cluster: {cluster}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <AlertCircle className="h-4 w-4 text-yellow-500" />
              <span className="text-sm font-medium">{warnings.length} warnings</span>
            </div>
            <div className="flex items-center gap-2">
              <XCircle className="h-4 w-4 text-red-500" />
              <span className="text-sm font-medium">{criticals.length} criticals</span>
            </div>
          </div>

          {/* Lista de alertas */}
          <ScrollArea className="h-[600px] pr-4">
            <div className="space-y-3">
              {filteredAlerts.map((alert, idx) => (
                <div
                  key={idx}
                  className={`flex items-start gap-3 p-3 rounded-lg border ${
                    alert.status === "critical"
                      ? "bg-red-50 border-red-200 dark:bg-red-950/30 dark:border-red-900"
                      : "bg-yellow-50 border-yellow-200 dark:bg-yellow-950/30 dark:border-yellow-900"
                  }`}
                >
                  {alert.status === "critical" ? (
                    <XCircle className="h-5 w-5 text-red-500 flex-shrink-0 mt-0.5" />
                  ) : (
                    <AlertCircle className="h-5 w-5 text-yellow-600 flex-shrink-0 mt-0.5" />
                  )}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <Badge
                        variant={alert.status === "critical" ? "destructive" : "default"}
                        className={
                          alert.status === "warning"
                            ? "bg-yellow-600 hover:bg-yellow-700"
                            : ""
                        }
                      >
                        {alert.status.toUpperCase()}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {removeEmojis(alert.type || '')}
                      </span>
                    </div>
                    <p className={`text-sm break-words whitespace-normal ${
                      alert.status === "critical"
                        ? "text-red-900 dark:text-red-100"
                        : "text-yellow-900 dark:text-yellow-100"
                    }`}>
                      {removeEmojis(alert.message)}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Modal de Exportação */}
      <Dialog open={exportModalOpen} onOpenChange={setExportModalOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Download className="h-5 w-5 text-blue-600" />
              Exportar Relatório de Alertas
            </DialogTitle>
            <DialogDescription>
              Escolha o formato de exportação para o relatório
            </DialogDescription>
          </DialogHeader>

          {/* Resumo dos dados */}
          <div className="bg-muted/50 p-3 rounded-lg text-sm space-y-1">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Cluster:</span>
              <span className="font-medium">{cluster}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Warnings:</span>
              <span className="font-medium text-yellow-600">{warnings.length}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Criticals:</span>
              <span className="font-medium text-red-600">{criticals.length}</span>
            </div>
          </div>

          {/* Seleção de formato */}
          <div className="space-y-4 py-4">
            <Label>Formato de Exportação</Label>
            <RadioGroup value={exportFormat} onValueChange={(value) => setExportFormat(value as any)}>
              <div className="flex items-center space-x-3 border rounded-lg p-3 hover:bg-accent cursor-pointer">
                <RadioGroupItem value="pdf" id="pdf" />
                <Label htmlFor="pdf" className="flex-1 cursor-pointer">
                  <div className="flex items-center gap-2">
                    <FileText className="h-4 w-4 text-red-600" />
                    <span className="font-medium">PDF</span>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {getFormatDescription("pdf")}
                  </p>
                </Label>
              </div>

              <div className="flex items-center space-x-3 border rounded-lg p-3 hover:bg-accent cursor-pointer">
                <RadioGroupItem value="markdown" id="markdown" />
                <Label htmlFor="markdown" className="flex-1 cursor-pointer">
                  <div className="flex items-center gap-2">
                    <FileText className="h-4 w-4 text-blue-600" />
                    <span className="font-medium">Markdown (.md)</span>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {getFormatDescription("markdown")}
                  </p>
                </Label>
              </div>

              <div className="flex items-center space-x-3 border rounded-lg p-3 hover:bg-accent cursor-pointer">
                <RadioGroupItem value="csv" id="csv" />
                <Label htmlFor="csv" className="flex-1 cursor-pointer">
                  <div className="flex items-center gap-2">
                    <FileSpreadsheet className="h-4 w-4 text-green-600" />
                    <span className="font-medium">CSV (.csv)</span>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {getFormatDescription("csv")}
                  </p>
                </Label>
              </div>
            </RadioGroup>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setExportModalOpen(false)} disabled={isExporting}>
              Cancelar
            </Button>
            <Button onClick={handleExport} disabled={isExporting}>
              {isExporting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {isExporting ? "Exportando..." : "Exportar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
