import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { FileText, FileSpreadsheet, Download, Loader2 } from "lucide-react";
import type { HealthCheckResult } from "@/types/healthcheck";
import {
  generatePDFReport,
  generateMarkdownReport,
  generateCSVReport,
  type ReportData,
} from "@/lib/reportGenerator";
import { toast } from "sonner";

interface ExportReportModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  results: Record<string, HealthCheckResult>;
}

export const ExportReportModal = ({
  open,
  onOpenChange,
  results,
}: ExportReportModalProps) => {
  const [format, setFormat] = useState<"pdf" | "markdown" | "csv">("pdf");
  const [isExporting, setIsExporting] = useState(false);

  const handleExport = async () => {
    setIsExporting(true);

    try {
      // Usar a data da análise mais recente ao invés da data atual
      const timestamps = Object.values(results).map(r => new Date(r.started_at).getTime());
      const mostRecentTimestamp = Math.max(...timestamps);
      const analysisDate = new Date(mostRecentTimestamp).toISOString();

      const reportData: ReportData = {
        results,
        timestamp: analysisDate,
      };

      // Delay para feedback visual
      await new Promise((resolve) => setTimeout(resolve, 500));

      switch (format) {
        case "pdf":
          generatePDFReport(reportData);
          toast.success("Relatório PDF gerado com sucesso!");
          break;
        case "markdown":
          generateMarkdownReport(reportData);
          toast.success("Relatório Markdown gerado com sucesso!");
          break;
        case "csv":
          generateCSVReport(reportData);
          toast.success("Relatório CSV gerado com sucesso!");
          break;
      }

      onOpenChange(false);
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
        return "Documento formatado com tabelas e seções organizadas. Ideal para apresentações e impressão.";
      case "markdown":
        return "Formato texto com marcação. Ideal para documentação e versionamento (Git, Confluence, etc).";
      case "csv":
        return "Planilha de valores separados por vírgula. Ideal para análise em Excel ou ferramentas de BI.";
      default:
        return "";
    }
  };

  const clusterCount = Object.keys(results).length;
  const totalWarnings = Object.values(results).reduce((sum, r) => sum + r.warning_count, 0);
  const totalCritical = Object.values(results).reduce((sum, r) => sum + r.critical_count, 0);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="h-5 w-5 text-blue-600" />
            Exportar Relatório
          </DialogTitle>
          <DialogDescription>
            Escolha o formato para exportar o relatório de Health Checking
          </DialogDescription>
        </DialogHeader>

        {/* Resumo dos resultados */}
        <div className="bg-muted/50 rounded-lg p-3 space-y-1 text-sm">
          <div className="font-medium">Resumo da análise:</div>
          <div className="text-muted-foreground">
            • {clusterCount} cluster{clusterCount > 1 ? "s" : ""} analisado{clusterCount > 1 ? "s" : ""}
          </div>
          <div className="text-muted-foreground">
            • {totalWarnings} aviso{totalWarnings !== 1 ? "s" : ""} (warnings)
          </div>
          <div className="text-muted-foreground">
            • {totalCritical} problema{totalCritical !== 1 ? "s" : ""} crítico{totalCritical !== 1 ? "s" : ""}
          </div>
        </div>

        {/* Seleção de formato */}
        <div className="space-y-3">
          <Label>Formato de exportação:</Label>
          <RadioGroup value={format} onValueChange={(v) => setFormat(v as typeof format)}>
            {/* PDF */}
            <div className="flex items-start space-x-3 space-y-0">
              <RadioGroupItem value="pdf" id="pdf" />
              <div className="flex-1 space-y-1">
                <Label htmlFor="pdf" className="flex items-center gap-2 font-medium cursor-pointer">
                  <FileText className="h-4 w-4 text-red-600" />
                  PDF (Portable Document Format)
                </Label>
                <p className="text-xs text-muted-foreground">
                  {getFormatDescription("pdf")}
                </p>
              </div>
            </div>

            {/* Markdown */}
            <div className="flex items-start space-x-3 space-y-0">
              <RadioGroupItem value="markdown" id="markdown" />
              <div className="flex-1 space-y-1">
                <Label htmlFor="markdown" className="flex items-center gap-2 font-medium cursor-pointer">
                  <FileText className="h-4 w-4 text-blue-600" />
                  Markdown (.md)
                </Label>
                <p className="text-xs text-muted-foreground">
                  {getFormatDescription("markdown")}
                </p>
              </div>
            </div>

            {/* CSV */}
            <div className="flex items-start space-x-3 space-y-0">
              <RadioGroupItem value="csv" id="csv" />
              <div className="flex-1 space-y-1">
                <Label htmlFor="csv" className="flex items-center gap-2 font-medium cursor-pointer">
                  <FileSpreadsheet className="h-4 w-4 text-green-600" />
                  CSV (Comma-Separated Values)
                </Label>
                <p className="text-xs text-muted-foreground">
                  {getFormatDescription("csv")}
                </p>
              </div>
            </div>
          </RadioGroup>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isExporting}>
            Cancelar
          </Button>
          <Button onClick={handleExport} disabled={isExporting}>
            {isExporting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Gerando...
              </>
            ) : (
              <>
                <Download className="mr-2 h-4 w-4" />
                Exportar
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
