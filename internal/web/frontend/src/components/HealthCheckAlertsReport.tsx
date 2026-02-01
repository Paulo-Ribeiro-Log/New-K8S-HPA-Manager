import { useState, useEffect } from "react";
import { DateRange } from "react-day-picker";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Calendar } from "@/components/ui/calendar";
import { AlertCircle, XCircle, Download, Server, CheckCircle2, FileText, FileSpreadsheet, Loader2, CalendarIcon, Filter } from "lucide-react";
import type { HealthCheckProgress } from "@/types/healthcheck";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { cn } from "@/lib/utils";
import { addLogoHeaderToPDF, getMarkdownHeader } from "@/lib/logoUtils";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";

interface HealthCheckAlertsReportProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventsPerCluster: Record<string, HealthCheckProgress[]>;
  clusters: string[];
  clusterSessions: Record<string, string>; // ✅ Map cluster -> sessionId para buscar do banco
}

export const HealthCheckAlertsReport = ({
  open,
  onOpenChange,
  eventsPerCluster,
  clusters,
  clusterSessions,
}: HealthCheckAlertsReportProps) => {
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportFormat, setExportFormat] = useState<"pdf" | "markdown" | "csv">("pdf");
  const [isExporting, setIsExporting] = useState(false);
  const [isLoadingFromDB, setIsLoadingFromDB] = useState(false);
  const [eventsFromDB, setEventsFromDB] = useState<Record<string, HealthCheckProgress[]>>({});

  // ✅ Date Range Picker (Option 2)
  const [dateRange, setDateRange] = useState<DateRange | undefined>(undefined);
  const [dateFilterMode, setDateFilterMode] = useState<"current" | "today" | "week" | "month" | "custom">("current");

  // ✅ Buscar eventos persistidos do banco ao abrir modal
  useEffect(() => {
    if (!open) return;

    const loadEventsFromDatabase = async () => {
      setIsLoadingFromDB(true);
      const loadedEvents: Record<string, HealthCheckProgress[]> = {};

      try {
        // Modo "current": busca eventos da sessão atual
        if (dateFilterMode === "current" && clusterSessions) {
          for (const cluster of clusters) {
            const sessionId = clusterSessions[cluster];
            if (!sessionId) continue;

            console.log(`[HealthCheckAlertsReport] Buscando eventos da sessão atual para cluster: ${cluster}, sessionId: ${sessionId}`);

            try {
              const response = await apiClient.getHealthCheckEvents(sessionId);
              if (response.success && response.events) {
                console.log(`[HealthCheckAlertsReport] Eventos carregados do banco para ${cluster}:`, response.events.length);
                loadedEvents[cluster] = response.events;
              }
            } catch (error) {
              console.error(`[HealthCheckAlertsReport] Erro ao buscar eventos do cluster ${cluster}:`, error);
            }
          }
        }
        // Modo "today" | "week" | "month" | "custom": busca histórico completo e filtra
        else {
          console.log(`[HealthCheckAlertsReport] Buscando histórico completo para filtro de período: ${dateFilterMode}`);

          try {
            const historyResponse = await apiClient.getHealthCheckHistory();
            if (!historyResponse.success || !historyResponse.data) {
              toast.error("Erro ao carregar histórico de análises");
              setEventsFromDB({});
              return;
            }

            const now = new Date();
            const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate());

            // Filtrar histórico por data
            let filteredHistory = historyResponse.data.filter((h: any) => {
              const date = new Date(h.started_at);
              switch (dateFilterMode) {
                case "today":
                  return date >= startOfDay;
                case "week":
                  const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
                  return date >= weekAgo;
                case "month":
                  const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
                  return date >= monthAgo;
                case "custom":
                  if (!dateRange) return true;
                  if (dateRange.from && !dateRange.to) {
                    const fromDate = new Date(dateRange.from);
                    fromDate.setHours(0, 0, 0, 0);
                    return date >= fromDate;
                  }
                  if (dateRange.from && dateRange.to) {
                    const fromDate = new Date(dateRange.from);
                    fromDate.setHours(0, 0, 0, 0);
                    const toDate = new Date(dateRange.to);
                    toDate.setHours(23, 59, 59, 999);
                    return date >= fromDate && date <= toDate;
                  }
                  return true;
                default:
                  return true;
              }
            });

            // Buscar eventos de cada sessão filtrada
            for (const result of filteredHistory) {
              try {
                const response = await apiClient.getHealthCheckEvents(result.id);
                if (response.success && response.events) {
                  const cluster = result.cluster;
                  if (!loadedEvents[cluster]) {
                    loadedEvents[cluster] = [];
                  }
                  // Agregar eventos de múltiplas sessões do mesmo cluster
                  loadedEvents[cluster].push(...response.events);
                }
              } catch (error) {
                console.error(`[HealthCheckAlertsReport] Erro ao buscar eventos da sessão ${result.id}:`, error);
              }
            }

            console.log(`[HealthCheckAlertsReport] Total de eventos agregados por período:`, Object.keys(loadedEvents).length);
          } catch (error) {
            console.error('[HealthCheckAlertsReport] Erro ao carregar histórico:', error);
            toast.error("Erro ao buscar alertas por período");
          }
        }

        setEventsFromDB(loadedEvents);
      } catch (error) {
        console.error('[HealthCheckAlertsReport] Erro ao carregar eventos do banco:', error);
      } finally {
        setIsLoadingFromDB(false);
      }
    };

    loadEventsFromDatabase();
  }, [open, clusterSessions, clusters, dateFilterMode, dateRange]);

  // Extrair apenas warnings e criticals de cada cluster
  // ✅ Priorizar eventos do banco, fallback para eventos em memória
  const getAlertsForCluster = (cluster: string) => {
    const events = eventsFromDB[cluster] || eventsPerCluster[cluster] || [];
    return events.filter(
      (event) => event.status === "warning" || event.status === "critical"
    );
  };

  // Agrupar alertas por cluster
  const clusterAlerts = clusters.map((cluster) => ({
    cluster,
    alerts: getAlertsForCluster(cluster),
  }));

  // Contar totais
  const totalWarnings = clusterAlerts.reduce(
    (sum, ca) => sum + ca.alerts.filter((a) => a.status === "warning").length,
    0
  );
  const totalCriticals = clusterAlerts.reduce(
    (sum, ca) => sum + ca.alerts.filter((a) => a.status === "critical").length,
    0
  );

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
  const generatePDF = async () => {
    const doc = new jsPDF({
      orientation: "portrait",
      unit: "mm",
      format: "a4",
    });

    // Header com logo
    let yPosition = await addLogoHeaderToPDF(
      doc,
      "RELATORIO DE ALERTAS",
      "Health Check - Warnings e Criticals"
    );

    // Data abaixo do header
    const pageWidth = doc.internal.pageSize.getWidth();
    doc.setFontSize(9);
    doc.setTextColor(100, 100, 100);
    const timestamp = new Date().toLocaleString("pt-BR");
    doc.text(`Gerado em: ${timestamp}`, pageWidth / 2, yPosition - 5, { align: "center" });
    doc.setTextColor(0, 0, 0);
    yPosition += 5;

    // Sumário Executivo
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(41, 128, 185);
    doc.text("SUMARIO EXECUTIVO", 14, yPosition);
    yPosition += 8;

    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    doc.setTextColor(0, 0, 0);
    doc.text(`Total de Clusters Analisados: ${clusters.length}`, 14, yPosition);
    yPosition += 6;
    doc.text(`Total de Warnings: ${totalWarnings}`, 14, yPosition);
    yPosition += 6;
    doc.text(`Total de Criticals: ${totalCriticals}`, 14, yPosition);
    yPosition += 12;

    // Processar cada cluster
    clusterAlerts.forEach(({ cluster, alerts }, clusterIndex) => {
      if (alerts.length === 0) return;

      const warnings = alerts.filter((a) => a.status === "warning");
      const criticals = alerts.filter((a) => a.status === "critical");

      // Nova página para cada cluster (exceto o primeiro)
      if (clusterIndex > 0) {
        doc.addPage();
        yPosition = 20;
      }

      // Título do cluster
      doc.setFontSize(12);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(41, 128, 185);
      doc.text(`Cluster: ${cluster}`, 14, yPosition);
      yPosition += 5;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(100, 100, 100);
      doc.text(`Warnings: ${warnings.length} | Criticals: ${criticals.length}`, 14, yPosition);
      yPosition += 8;

      // Preparar dados para a tabela (REMOVER EMOJIS)
      const tableData = alerts.map((alert) => [
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

      // Atualizar yPosition para próximo cluster
      yPosition = (doc as any).lastAutoTable.finalY + 15;
    });

    // Salvar PDF
    doc.save(`health-check-alerts-${new Date().toISOString().split("T")[0]}.pdf`);
  };

  // Gerar Markdown
  const generateMarkdown = () => {
    const timestamp = new Date().toLocaleString("pt-BR");

    // Header com branding
    let md = getMarkdownHeader(
      "RELATORIO DE ALERTAS - HEALTH CHECK",
      `Gerado em: ${timestamp}`
    );

    md += `## SUMÁRIO EXECUTIVO\n\n`;
    md += `- **Total de Clusters:** ${clusters.length}\n`;
    md += `- **Total de Warnings:** ${totalWarnings}\n`;
    md += `- **Total de Criticals:** ${totalCriticals}\n\n`;
    md += `---\n\n`;

    md += `## ANÁLISE DETALHADA POR CLUSTER\n\n`;

    clusterAlerts.forEach(({ cluster, alerts }) => {
      if (alerts.length === 0) return;

      const warnings = alerts.filter((a) => a.status === "warning");
      const criticals = alerts.filter((a) => a.status === "critical");

      md += `### 🖥️ Cluster: **${cluster}**\n\n`;
      md += `- **Warnings:** ${warnings.length}\n`;
      md += `- **Criticals:** ${criticals.length}\n\n`;

      md += `#### Alertas Detectados\n\n`;
      md += `| Status | Tipo | Mensagem |\n`;
      md += `|--------|------|----------|\n`;

      alerts.forEach((alert) => {
        const status = alert.status.toUpperCase();
        const type = removeEmojis(alert.type || "N/A");
        const message = removeEmojis(alert.message).replace(/\|/g, "\\|");
        md += `| **${status}** | ${type} | ${message} |\n`;
      });

      md += `\n---\n\n`;
    });

    md += `\n---\n\n`;
    md += `*Relatório gerado automaticamente pelo K8s HPA Manager*\n`;

    const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `health-check-alerts-${new Date().toISOString().split("T")[0]}.md`;
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

    clusterAlerts.forEach(({ cluster, alerts }) => {
      alerts.forEach((alert) => {
        const status = alert.status.toUpperCase();
        const type = removeEmojis(alert.type || "N/A");
        const message = `"${removeEmojis(alert.message).replace(/"/g, '""')}"`;
        rows.push(`${timestamp},${cluster},${status},${type},${message}`);
      });
    });

    const csv = rows.join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `health-check-alerts-${timestamp}.csv`;
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
          await generatePDF();
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh]">
        <DialogHeader>
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle>Relatório de Alertas</DialogTitle>
              <DialogDescription>
                Warnings e Criticals detectados durante o Health Check
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

        {/* ✅ Filtros de Período (Option 2) */}
        <div className="flex flex-wrap items-center gap-2 pb-4 border-b">
          <div className="flex items-center gap-2">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <Label className="text-sm text-muted-foreground">Período:</Label>
          </div>

          {/* Botões rápidos */}
          <Button
            variant={dateFilterMode === "current" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setDateFilterMode("current");
              setDateRange(undefined);
            }}
          >
            Análise Atual
          </Button>

          <Button
            variant={dateFilterMode === "today" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setDateFilterMode("today");
              setDateRange(undefined);
            }}
          >
            Hoje
          </Button>

          <Button
            variant={dateFilterMode === "week" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setDateFilterMode("week");
              setDateRange(undefined);
            }}
          >
            Última Semana
          </Button>

          <Button
            variant={dateFilterMode === "month" ? "default" : "outline"}
            size="sm"
            onClick={() => {
              setDateFilterMode("month");
              setDateRange(undefined);
            }}
          >
            Último Mês
          </Button>

          {/* Date Picker customizado */}
          <Popover>
            <PopoverTrigger asChild>
              <Button
                variant={dateFilterMode === "custom" ? "default" : "outline"}
                size="sm"
                className={cn(
                  "justify-start text-left font-normal",
                  !dateRange && "text-muted-foreground"
                )}
              >
                <CalendarIcon className="mr-2 h-4 w-4" />
                {dateRange?.from ? (
                  dateRange.to ? (
                    <>
                      {format(dateRange.from, "dd MMM", { locale: ptBR })} -{" "}
                      {format(dateRange.to, "dd MMM yyyy", { locale: ptBR })}
                    </>
                  ) : (
                    format(dateRange.from, "dd MMM yyyy", { locale: ptBR })
                  )
                ) : (
                  <span>Personalizado</span>
                )}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="start">
              <Calendar
                initialFocus
                mode="range"
                defaultMonth={dateRange?.from}
                selected={dateRange}
                onSelect={(range) => {
                  setDateRange(range);
                  if (range?.from) {
                    setDateFilterMode("custom");
                  }
                }}
                numberOfMonths={2}
                disabled={(date) => date > new Date()}
                locale={ptBR}
              />
            </PopoverContent>
          </Popover>
        </div>

        {/* Resumo */}
        <div className="flex gap-4 pb-4 border-b">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">
              {clusters.length} clusters
            </span>
          </div>
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-yellow-500" />
            <span className="text-sm font-medium">{totalWarnings} warnings</span>
          </div>
          <div className="flex items-center gap-2">
            <XCircle className="h-4 w-4 text-red-500" />
            <span className="text-sm font-medium">{totalCriticals} criticals</span>
          </div>
        </div>

        {/* Loading indicator */}
        {isLoadingFromDB && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
            <span className="ml-3 text-sm text-muted-foreground">
              Carregando alertas do banco de dados...
            </span>
          </div>
        )}

        {/* Lista de alertas por cluster */}
        <ScrollArea className="h-[600px] pr-4">
          {clusterAlerts.map(({ cluster, alerts }) => {
            if (alerts.length === 0) return null;

            const warnings = alerts.filter((a) => a.status === "warning");
            const criticals = alerts.filter((a) => a.status === "critical");

            return (
              <div key={cluster} className="mb-6 pb-6 border-b last:border-0">
                {/* Header do cluster */}
                <div className="flex items-center gap-3 mb-3">
                  <Server className="h-5 w-5 text-blue-500" />
                  <h3 className="font-semibold text-lg">{cluster}</h3>
                  <div className="flex gap-2 ml-auto">
                    {warnings.length > 0 && (
                      <Badge variant="outline" className="bg-yellow-50 text-yellow-700 border-yellow-300">
                        {warnings.length} warnings
                      </Badge>
                    )}
                    {criticals.length > 0 && (
                      <Badge variant="outline" className="bg-red-50 text-red-700 border-red-300">
                        {criticals.length} criticals
                      </Badge>
                    )}
                  </div>
                </div>

                {/* Alertas */}
                <div className="space-y-3 pl-8">
                  {alerts.map((alert, idx) => (
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
              </div>
            );
          })}

          {/* Mensagem quando não há alertas */}
          {clusterAlerts.every((ca) => ca.alerts.length === 0) && (
            <div className="flex flex-col items-center justify-center h-[400px] text-center">
              <CheckCircle2 className="h-16 w-16 text-green-500 mb-4" />
              <h3 className="text-lg font-semibold mb-2">
                Nenhum alerta encontrado!
              </h3>
              <p className="text-sm text-muted-foreground max-w-md">
                Todos os clusters estão saudáveis. Não foram detectados warnings
                ou criticals durante o health check.
              </p>
            </div>
          )}
        </ScrollArea>
      </DialogContent>

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
              <span className="text-muted-foreground">Clusters:</span>
              <span className="font-medium">{clusters.length}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Warnings:</span>
              <span className="font-medium text-yellow-600">{totalWarnings}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Criticals:</span>
              <span className="font-medium text-red-600">{totalCriticals}</span>
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
    </Dialog>
  );
};
