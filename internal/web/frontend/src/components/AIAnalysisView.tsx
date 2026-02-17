import React, { useState } from "react";
import { AnalysisResult } from "@/types/ai";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  FileText,
  Clock,
  Server,
  Activity,
  Cpu,
  HardDrive,
  AlertCircle,
  TrendingUp,
  Info
} from "lucide-react";
import { useToast } from "@/components/ui/use-toast";
import ReactMarkdown from "react-markdown";
import {
  generateAIDiagnosticsPDF,
  generateAIDiagnosticsMarkdown,
  generateAIDiagnosticsCSV,
} from "@/lib/aiReportGenerator";

interface AIAnalysisViewProps {
  analysis: AnalysisResult;
  showExportButtons?: boolean;
}

export const AIAnalysisView: React.FC<AIAnalysisViewProps> = ({
  analysis: rawAnalysis,
  showExportButtons = true,
}) => {
  const { toast } = useToast();
  const [isExporting, setIsExporting] = useState(false);

  // Parse structured data from analysis field if it's a JSON string
  const analysis = React.useMemo(() => {
    // Se já tem executive_summary, está no formato novo
    if (rawAnalysis.executive_summary) {
      return rawAnalysis;
    }

    // Tenta fazer parse do campo analysis se for JSON estruturado
    if (rawAnalysis.analysis && typeof rawAnalysis.analysis === 'string') {
      try {
        // Verifica se parece ser JSON (começa com {)
        const trimmed = rawAnalysis.analysis.trim();
        if (trimmed.startsWith('{')) {
          console.log("[AIAnalysisView] Attempting JSON parse...");
          const parsed = JSON.parse(trimmed);
          // Se o parse funcionou e tem os campos estruturados, mescla com o original
          if (parsed.executive_summary || parsed.root_cause_analysis || parsed.recommendations) {
            console.log("[AIAnalysisView] JSON parse successful!");
            return {
              ...rawAnalysis,
              executive_summary: parsed.executive_summary,
              root_cause_analysis: parsed.root_cause_analysis,
              impact_assessment: parsed.impact_assessment,
              recommendations: parsed.recommendations,
              // Mantém o analysis original como markdown para fallback
              analysis: parsed.quick_summary || rawAnalysis.analysis,
            };
          }
        } else {
          console.log("[AIAnalysisView] Not JSON (doesn't start with '{'), using as markdown");
        }
      } catch (err) {
        // Não é JSON, mantém como markdown
        console.log("[AIAnalysisView] JSON parse failed, using as markdown:", err);
      }
    }

    console.log("[AIAnalysisView] Using fallback format (markdown)");
    return rawAnalysis;
  }, [rawAnalysis]);

  // Helper: Download blob as file
  const downloadBlob = (blob: Blob, filename: string) => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  // Export handlers (usando jsPDF no frontend - suporte UTF-8 nativo!)
  const handleExportPDF = async () => {
    setIsExporting(true);
    try {
      await generateAIDiagnosticsPDF(analysis);
      toast({
        title: "PDF exportado com sucesso",
        description: `Arquivo gerado com suporte completo a UTF-8`,
      });
    } catch (error) {
      toast({
        title: "Erro ao exportar PDF",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
    } finally {
      setIsExporting(false);
    }
  };

  const handleExportMarkdown = () => {
    setIsExporting(true);
    try {
      const content = generateAIDiagnosticsMarkdown(analysis);
      const filename = `diagnostico_${analysis.resource_name}_${new Date().toISOString().split('T')[0]}.md`;
      const blob = new Blob([content], { type: "text/markdown;charset=utf-8" });
      downloadBlob(blob, filename);
      toast({
        title: "Markdown exportado com sucesso",
        description: `Arquivo ${filename} baixado`,
      });
    } catch (error) {
      toast({
        title: "Erro ao exportar Markdown",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
    } finally {
      setIsExporting(false);
    }
  };

  const handleExportCSV = () => {
    setIsExporting(true);
    try {
      const content = generateAIDiagnosticsCSV(analysis);
      const filename = `diagnostico_${analysis.resource_name}_${new Date().toISOString().split('T')[0]}.csv`;
      const blob = new Blob([content], { type: "text/csv;charset=utf-8" });
      downloadBlob(blob, filename);
      toast({
        title: "CSV exportado com sucesso",
        description: `Arquivo ${filename} baixado`,
      });
    } catch (error) {
      toast({
        title: "Erro ao exportar CSV",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
    } finally {
      setIsExporting(false);
    }
  };

  // Helper: Get severity badge variant
  const getSeverityVariant = (severity: string): "destructive" | "default" | "secondary" | "outline" => {
    switch (severity.toLowerCase()) {
      case "critical":
        return "destructive";
      case "high":
        return "destructive";
      case "medium":
        return "default";
      case "low":
        return "secondary";
      default:
        return "outline";
    }
  };

  // Helper: Get status badge variant
  const getStatusVariant = (status: string): "destructive" | "default" | "secondary" => {
    switch (status.toLowerCase()) {
      case "unhealthy":
        return "destructive";
      case "degraded":
        return "default";
      case "healthy":
        return "secondary";
      default:
        return "outline";
    }
  };

  // Helper: Normalizar versão de x-x-x-x para x.x.x-x (formato semver)
  const normalizeVersion = (version: string): string => {
    if (!version) return version;
    // Remove prefixo "v" se existir
    let v = version.replace(/^v/, '');
    // Contar hífens
    const hyphens = (v.match(/-/g) || []).length;
    if (hyphens >= 3) {
      // Formato: x-x-x-x → x.x.x-x
      const parts = v.split('-');
      if (parts.length >= 4) {
        return `${parts[0]}.${parts[1]}.${parts[2]}-${parts.slice(3).join('-')}`;
      }
    } else if (hyphens === 2 && !v.includes('.')) {
      // Formato: x-x-x → x.x.x
      v = v.replace(/-/g, '.');
    }
    return v;
  };

  return (
    <div className="space-y-6">

      {/* Export Buttons */}
      {showExportButtons && (
        <div className="flex items-center justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleExportPDF}
            disabled={isExporting}
          >
            <Download className="h-4 w-4 mr-2" />
            PDF
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleExportMarkdown}
            disabled={isExporting}
          >
            <FileText className="h-4 w-4 mr-2" />
            Markdown
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleExportCSV}
            disabled={isExporting}
          >
            <Download className="h-4 w-4 mr-2" />
            CSV
          </Button>
        </div>
      )}

      {/* SECTION 1: RESOURCE METADATA (Header Profissional) */}
      {analysis.resource_metadata && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Server className="h-5 w-5" />
              Metadados do Recurso
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-6">
              {/* Left Column */}
              <div className="space-y-3">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Nome</div>
                  <div className="text-base font-semibold">{analysis.resource_metadata.name}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Namespace</div>
                  <div className="text-base">{analysis.resource_metadata.namespace}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Cluster</div>
                  <div className="text-base">{analysis.resource_metadata.cluster}</div>
                </div>
                {analysis.resource_metadata.version && (
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">Versao</div>
                    <Badge variant="outline">{normalizeVersion(analysis.resource_metadata.version)}</Badge>
                  </div>
                )}
              </div>

              {/* Right Column */}
              <div className="space-y-3">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Status</div>
                  <Badge variant={analysis.resource_metadata.status.includes("CrashLoop") ? "destructive" : "secondary"}>
                    {analysis.resource_metadata.status}
                  </Badge>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Age</div>
                  <div className="text-base flex items-center gap-2">
                    <Clock className="h-4 w-4" />
                    {analysis.resource_metadata.age}
                  </div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Restart Count</div>
                  <div className="text-base">
                    {analysis.resource_metadata.restart_count > 0 ? (
                      <Badge variant="destructive">{analysis.resource_metadata.restart_count}</Badge>
                    ) : (
                      <span>0</span>
                    )}
                  </div>
                </div>
                {analysis.resource_metadata.node_name && (
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">Node</div>
                    <div className="text-sm font-mono">{analysis.resource_metadata.node_name}</div>
                  </div>
                )}
              </div>
            </div>

            {/* Resources Section */}
            <div className="mt-6">
              <div className="text-sm font-semibold mb-3 flex items-center gap-2">
                <Activity className="h-4 w-4" />
                Recursos
              </div>
              <div className="grid grid-cols-2 gap-4">
                <Card className="p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <Cpu className="h-4 w-4 text-blue-500" />
                    <span className="font-medium">CPU</span>
                  </div>
                  <div className="space-y-1 text-sm">
                    <div>Current: <span className="font-semibold">{analysis.resource_metadata.resources.cpu.current || "N/A"}</span></div>
                    <div>Request: <span className="text-muted-foreground">{analysis.resource_metadata.resources.cpu.request || "N/A"}</span></div>
                    <div>Limit: <span className="text-muted-foreground">{analysis.resource_metadata.resources.cpu.limit || "N/A"}</span></div>
                  </div>
                </Card>
                <Card className="p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <HardDrive className="h-4 w-4 text-green-500" />
                    <span className="font-medium">Memory</span>
                  </div>
                  <div className="space-y-1 text-sm">
                    <div>Current: <span className="font-semibold">{analysis.resource_metadata.resources.memory.current || "N/A"}</span></div>
                    <div>Request: <span className="text-muted-foreground">{analysis.resource_metadata.resources.memory.request || "N/A"}</span></div>
                    <div>Limit: <span className="text-muted-foreground">{analysis.resource_metadata.resources.memory.limit || "N/A"}</span></div>
                  </div>
                </Card>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* SECTION 2: EXECUTIVE SUMMARY */}
      {analysis.executive_summary ? (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2">
                <Info className="h-5 w-5" />
                Sumario Executivo
              </CardTitle>
              <div className="flex gap-2">
                <Badge variant={getSeverityVariant(analysis.executive_summary.severity)}>
                  {analysis.executive_summary.severity.toUpperCase()}
                </Badge>
                <Badge variant={getStatusVariant(analysis.executive_summary.status)}>
                  {analysis.executive_summary.status.toUpperCase()}
                </Badge>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <p className="text-base mb-4">{analysis.executive_summary.quick_summary}</p>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <span className="font-medium">Tempo Estimado:</span>{" "}
                <span className="text-muted-foreground">{analysis.executive_summary.time_to_resolve}</span>
              </div>
              <div>
                <span className="font-medium">Provider:</span>{" "}
                <span className="text-muted-foreground">{analysis.provider} ({analysis.model || "N/A"})</span>
              </div>
            </div>
          </CardContent>
        </Card>
      ) : (
        /* Fallback: Legacy Analysis */
        <Card>
          <CardHeader>
            <CardTitle>Análise</CardTitle>
          </CardHeader>
          <CardContent>
            <ScrollArea className="h-96">
              <ReactMarkdown className="prose prose-sm max-w-none">
                {analysis.analysis}
              </ReactMarkdown>
            </ScrollArea>
          </CardContent>
        </Card>
      )}

      {/* SECTION 3: ROOT CAUSE ANALYSIS */}
      {analysis.root_cause_analysis && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <AlertCircle className="h-5 w-5" />
              Analise de Causa Raiz
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Accordion type="single" collapsible className="w-full">
              <AccordionItem value="symptom">
                <AccordionTrigger>Sintoma Identificado</AccordionTrigger>
                <AccordionContent>
                  <p className="text-base">{analysis.root_cause_analysis.symptom}</p>
                </AccordionContent>
              </AccordionItem>

              <AccordionItem value="causes">
                <AccordionTrigger>Causas Provaveis</AccordionTrigger>
                <AccordionContent>
                  <ol className="list-decimal list-inside space-y-2">
                    {(analysis.root_cause_analysis.probable_causes || []).map((cause, idx) => (
                      <li key={idx} className="text-base">{cause}</li>
                    ))}
                  </ol>
                </AccordionContent>
              </AccordionItem>

              {analysis.root_cause_analysis.evidence && analysis.root_cause_analysis.evidence.length > 0 && (
                <AccordionItem value="evidence">
                  <AccordionTrigger>Evidencias</AccordionTrigger>
                  <AccordionContent>
                    <ul className="list-disc list-inside space-y-1">
                      {(analysis.root_cause_analysis.evidence || []).map((ev, idx) => (
                        <li key={idx} className="text-sm font-mono bg-muted p-2 rounded">{ev}</li>
                      ))}
                    </ul>
                  </AccordionContent>
                </AccordionItem>
              )}
            </Accordion>

            <div className="mt-4">
              <span className="font-medium">Confianca:</span>{" "}
              <Badge variant={analysis.root_cause_analysis.confidence === "high" ? "secondary" : "outline"}>
                {analysis.root_cause_analysis.confidence.toUpperCase()}
              </Badge>
            </div>
          </CardContent>
        </Card>
      )}

      {/* SECTION 4: IMPACT ASSESSMENT */}
      {analysis.impact_assessment && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Impacto e Severidade
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Downtime Estimado - largura total para textos longos */}
            <Card className="p-4">
              <div className="flex items-center justify-between">
                <div className="text-xs text-muted-foreground uppercase font-medium">Downtime Estimado</div>
                <div className="text-base font-semibold">{analysis.impact_assessment.downtime_estimate}</div>
              </div>
            </Card>

            {/* SLA Breach e Severidade - lado a lado */}
            <div className="grid grid-cols-2 gap-4">
              <Card className="p-4">
                <div className="flex items-center justify-between">
                  <div className="text-xs text-muted-foreground uppercase font-medium">SLA Breach</div>
                  {analysis.impact_assessment.sla_breach ? (
                    <Badge variant="destructive">SIM</Badge>
                  ) : (
                    <Badge variant="secondary">NAO</Badge>
                  )}
                </div>
              </Card>
              <Card className="p-4">
                <div className="flex items-center justify-between">
                  <div className="text-xs text-muted-foreground uppercase font-medium">Severidade</div>
                  <Badge variant={getSeverityVariant(analysis.impact_assessment.severity)}>
                    {analysis.impact_assessment.severity.toUpperCase()}
                  </Badge>
                </div>
              </Card>
            </div>

            {/* Card de Usuários Afetados em largura total para textos longos */}
            <Card className="p-4">
              <div className="text-xs text-muted-foreground uppercase font-medium mb-2">Usuarios Afetados</div>
              <div className="text-sm break-words">{analysis.impact_assessment.affected_users}</div>
            </Card>

            {analysis.impact_assessment.business_impact && (
              <Card className="p-4">
                <div className="text-xs text-muted-foreground uppercase font-medium mb-2">Impacto no Negocio</div>
                <p className="text-sm break-words">{analysis.impact_assessment.business_impact}</p>
              </Card>
            )}
          </CardContent>
        </Card>
      )}

      {/* SECTION 5: ACTIONABLE RECOMMENDATIONS */}
      {analysis.recommendations && analysis.recommendations.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <CheckCircle2 className="h-5 w-5" />
              Acoes Recomendadas
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {(analysis.recommendations || [])
              .sort((a, b) => a.priority - b.priority) // Sort by priority (1 = highest)
              .map((rec, idx) => (
                <Card key={idx} className="border-l-4 border-l-blue-500">
                  <CardHeader>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Badge variant="outline">Prioridade {rec.priority}</Badge>
                        <span className="font-semibold text-base">{rec.title}</span>
                      </div>
                      <div className="flex gap-2">
                        <Badge variant={rec.risk_level === "high" ? "destructive" : "secondary"}>
                          Risco: {rec.risk_level.toUpperCase()}
                        </Badge>
                        <Badge variant={rec.impact_level === "high" ? "default" : "outline"}>
                          Impacto: {rec.impact_level.toUpperCase()}
                        </Badge>
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <p className="text-base mb-3">{rec.description}</p>

                    {rec.commands && rec.commands.length > 0 && (
                      <div className="bg-muted p-3 rounded font-mono text-xs space-y-1">
                        {(rec.commands || []).map((cmd, cmdIdx) => (
                          <div key={cmdIdx}>$ {cmd}</div>
                        ))}
                      </div>
                    )}

                    <div className="flex gap-4 text-sm text-muted-foreground mt-3">
                      <span>Tempo: {rec.time_estimate}</span>
                    </div>
                  </CardContent>
                </Card>
              ))}
          </CardContent>
        </Card>
      ) : (
        /* Fallback: Legacy Suggestions */
        analysis.suggestions && analysis.suggestions.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Sugestoes</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {analysis.suggestions.map((sug, idx) => (
                <Card key={idx} className="p-4">
                  <div className="flex items-start justify-between mb-2">
                    <span className="font-semibold">{sug.description}</span>
                    <Badge variant={getSeverityVariant(sug.priority)}>
                      {sug.priority.toUpperCase()}
                    </Badge>
                  </div>
                  {sug.command && (
                    <div className="bg-muted p-2 rounded font-mono text-xs">
                      $ {sug.command}
                    </div>
                  )}
                </Card>
              ))}
            </CardContent>
          </Card>
        )
      )}

      {/* Footer Metadata */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <div>
              Gerado por: <span className="font-semibold">{analysis.provider}</span> ({analysis.model || "N/A"})
            </div>
            <div className="flex gap-4">
              {analysis.tokens_used && <span>Tokens: {analysis.tokens_used}</span>}
              {analysis.response_time && <span>Tempo: {analysis.response_time.toFixed(2)}s</span>}
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};
