import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import type { AnalysisResult } from "@/types/ai";
import {
  Brain,
  Copy,
  Check,
  AlertTriangle,
  AlertCircle,
  Info,
  TrendingUp,
  CheckCircle2,
  Clock,
  Server,
} from "lucide-react";
import { useState, useMemo } from "react";
import { useToast } from "@/components/ui/use-toast";

interface AIAnalysisModalProps {
  analysis: AnalysisResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Tipos para o JSON estruturado
interface ExecutiveSummary {
  severity: string;
  status: string;
  quick_summary: string;
  time_to_resolve: string;
}

interface RootCauseAnalysis {
  symptom: string;
  probable_causes: string[];
  evidence: string[];
  confidence: string;
}

interface ImpactAssessment {
  severity: string;
  affected_users: string;
  downtime_estimate: string;
  sla_breach: boolean;
  business_impact: string;
}

interface Recommendation {
  priority: number;
  title: string;
  description: string;
  commands: string[];
  time_estimate: string;
  risk_level: string;
  impact_level: string;
}

interface ParsedAnalysis {
  executive_summary?: ExecutiveSummary;
  root_cause_analysis?: RootCauseAnalysis;
  impact_assessment?: ImpactAssessment;
  recommendations?: Recommendation[];
}

// Helper para cor de severidade
const getSeverityColor = (severity: string) => {
  switch (severity?.toLowerCase()) {
    case "critical":
      return "bg-red-500 text-white";
    case "high":
      return "bg-orange-500 text-white";
    case "medium":
      return "bg-yellow-500 text-black";
    case "low":
      return "bg-blue-500 text-white";
    default:
      return "bg-gray-500 text-white";
  }
};

// Helper para cor de status
const getStatusColor = (status: string) => {
  switch (status?.toLowerCase()) {
    case "unhealthy":
      return "bg-red-500 text-white";
    case "degraded":
      return "bg-orange-500 text-white";
    case "healthy":
      return "bg-green-500 text-white";
    default:
      return "bg-gray-500 text-white";
  }
};

// Componente para copiar comando
const CommandCopy = ({ command }: { command: string }) => {
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      toast({ title: "Comando copiado!" });
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast({ title: "Erro ao copiar", variant: "destructive" });
    }
  };

  return (
    <div className="flex items-start gap-2 mt-2">
      <code className="flex-1 bg-muted p-2 rounded text-xs font-mono break-all">
        $ {command}
      </code>
      <Button size="sm" variant="outline" onClick={handleCopy} className="flex-shrink-0">
        {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
      </Button>
    </div>
  );
};

export function AIAnalysisModal({ analysis, open, onOpenChange }: AIAnalysisModalProps) {
  if (!analysis) return null;

  // Parse do JSON estruturado no campo analysis
  const parsed = useMemo<ParsedAnalysis | null>(() => {
    if (!analysis.analysis) return null;

    try {
      const trimmed = analysis.analysis.trim();
      if (trimmed.startsWith('{')) {
        return JSON.parse(trimmed);
      }
    } catch {
      // Não é JSON válido
    }
    return null;
  }, [analysis.analysis]);

  // Formatar data
  const formatDate = () => {
    if (!analysis.analyzed_at) return "data desconhecida";
    const date = new Date(analysis.analyzed_at);
    if (isNaN(date.getTime())) return "data inválida";
    return date.toLocaleString("pt-BR");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl w-[95vw] max-h-[90vh] p-0 overflow-hidden flex flex-col">
        <DialogHeader className="px-6 pt-6 pb-4 border-b bg-gradient-to-r from-purple-50 to-blue-50 dark:from-purple-950/20 dark:to-blue-950/20 flex-shrink-0">
          <DialogTitle className="flex items-center gap-3 text-xl font-bold">
            <div className="p-2 bg-purple-100 dark:bg-purple-900/30 rounded-lg">
              <Brain className="h-5 w-5 text-purple-600 dark:text-purple-400" />
            </div>
            AI Diagnostics
            <Badge variant="outline">{analysis.provider || "Unknown"}</Badge>
          </DialogTitle>
          <DialogDescription className="text-sm font-mono">
            {analysis.resource_type}: {analysis.cluster}/{analysis.namespace}/{analysis.resource_name}
          </DialogDescription>
          <div className="flex items-center gap-4 text-xs text-muted-foreground mt-2">
            <span><Clock className="inline h-3 w-3 mr-1" />{formatDate()}</span>
            {analysis.response_time && <span>Tempo: {analysis.response_time.toFixed(2)}s</span>}
            {analysis.tokens_used && <span>Tokens: {analysis.tokens_used}</span>}
            {analysis.model && <span>Modelo: {analysis.model}</span>}
          </div>
        </DialogHeader>

        <ScrollArea className="flex-1 overflow-y-auto">
          <div className="px-6 py-4 space-y-4">
            {parsed ? (
              <>
                {/* SUMÁRIO EXECUTIVO */}
                {parsed.executive_summary && (
                  <Card>
                    <CardHeader className="pb-3">
                      <div className="flex items-center justify-between">
                        <CardTitle className="flex items-center gap-2 text-base">
                          <Info className="h-5 w-5" />
                          Sumário Executivo
                        </CardTitle>
                        <div className="flex gap-2">
                          <Badge className={getSeverityColor(parsed.executive_summary.severity)}>
                            {parsed.executive_summary.severity?.toUpperCase()}
                          </Badge>
                          <Badge className={getStatusColor(parsed.executive_summary.status)}>
                            {parsed.executive_summary.status?.toUpperCase()}
                          </Badge>
                        </div>
                      </div>
                    </CardHeader>
                    <CardContent>
                      <p className="text-sm mb-3">{parsed.executive_summary.quick_summary}</p>
                      <div className="text-xs text-muted-foreground">
                        <strong>Tempo estimado para resolver:</strong> {parsed.executive_summary.time_to_resolve}
                      </div>
                    </CardContent>
                  </Card>
                )}

                {/* ANÁLISE DE CAUSA RAIZ */}
                {parsed.root_cause_analysis && (
                  <Card>
                    <CardHeader className="pb-3">
                      <CardTitle className="flex items-center gap-2 text-base">
                        <AlertCircle className="h-5 w-5" />
                        Análise de Causa Raiz
                        <Badge variant="outline" className="ml-2">
                          Confiança: {parsed.root_cause_analysis.confidence?.toUpperCase()}
                        </Badge>
                      </CardTitle>
                    </CardHeader>
                    <CardContent>
                      <Accordion type="single" collapsible defaultValue="symptom" className="w-full">
                        <AccordionItem value="symptom">
                          <AccordionTrigger className="text-sm font-medium">Sintoma Identificado</AccordionTrigger>
                          <AccordionContent>
                            <p className="text-sm">{parsed.root_cause_analysis.symptom}</p>
                          </AccordionContent>
                        </AccordionItem>

                        <AccordionItem value="causes">
                          <AccordionTrigger className="text-sm font-medium">Causas Prováveis</AccordionTrigger>
                          <AccordionContent>
                            <ol className="list-decimal list-inside space-y-2 text-sm">
                              {parsed.root_cause_analysis.probable_causes?.map((cause, idx) => (
                                <li key={idx}>{cause}</li>
                              ))}
                            </ol>
                          </AccordionContent>
                        </AccordionItem>

                        {parsed.root_cause_analysis.evidence?.length > 0 && (
                          <AccordionItem value="evidence">
                            <AccordionTrigger className="text-sm font-medium">Evidências</AccordionTrigger>
                            <AccordionContent>
                              <ul className="space-y-1">
                                {parsed.root_cause_analysis.evidence?.map((ev, idx) => (
                                  <li key={idx} className="text-xs font-mono bg-muted p-2 rounded break-all">{ev}</li>
                                ))}
                              </ul>
                            </AccordionContent>
                          </AccordionItem>
                        )}
                      </Accordion>
                    </CardContent>
                  </Card>
                )}

                {/* AVALIAÇÃO DE IMPACTO */}
                {parsed.impact_assessment && (
                  <Card>
                    <CardHeader className="pb-3">
                      <CardTitle className="flex items-center gap-2 text-base">
                        <TrendingUp className="h-5 w-5" />
                        Avaliação de Impacto
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-3">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="p-3 bg-muted rounded">
                          <div className="text-xs text-muted-foreground mb-1">Severidade</div>
                          <Badge className={getSeverityColor(parsed.impact_assessment.severity)}>
                            {parsed.impact_assessment.severity?.toUpperCase()}
                          </Badge>
                        </div>
                        <div className="p-3 bg-muted rounded">
                          <div className="text-xs text-muted-foreground mb-1">Violação de SLA</div>
                          <Badge variant={parsed.impact_assessment.sla_breach ? "destructive" : "secondary"}>
                            {parsed.impact_assessment.sla_breach ? "SIM" : "NÃO"}
                          </Badge>
                        </div>
                      </div>
                      <div className="p-3 bg-muted rounded">
                        <div className="text-xs text-muted-foreground mb-1">Tempo de Indisponibilidade Estimado</div>
                        <p className="text-sm font-medium">{parsed.impact_assessment.downtime_estimate}</p>
                      </div>
                      <div className="p-3 bg-muted rounded">
                        <div className="text-xs text-muted-foreground mb-1">Usuários Afetados</div>
                        <p className="text-sm">{parsed.impact_assessment.affected_users}</p>
                      </div>
                      <div className="p-3 bg-muted rounded">
                        <div className="text-xs text-muted-foreground mb-1">Impacto no Negócio</div>
                        <p className="text-sm">{parsed.impact_assessment.business_impact}</p>
                      </div>
                    </CardContent>
                  </Card>
                )}

                {/* RECOMENDAÇÕES */}
                {parsed.recommendations && parsed.recommendations.length > 0 && (
                  <Card>
                    <CardHeader className="pb-3">
                      <CardTitle className="flex items-center gap-2 text-base">
                        <CheckCircle2 className="h-5 w-5" />
                        Recomendações ({parsed.recommendations.length})
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                      {parsed.recommendations
                        .sort((a, b) => a.priority - b.priority)
                        .map((rec, idx) => (
                          <div key={idx} className="border-l-4 border-blue-500 pl-4 py-2">
                            <div className="flex items-center gap-2 mb-2">
                              <Badge variant="outline">Prioridade {rec.priority}</Badge>
                              <span className="font-semibold text-sm">{rec.title}</span>
                            </div>
                            <p className="text-sm text-muted-foreground mb-2">{rec.description}</p>

                            {rec.commands && rec.commands.length > 0 && (
                              <div className="space-y-1">
                                {rec.commands.map((cmd, cmdIdx) => (
                                  <CommandCopy key={cmdIdx} command={cmd} />
                                ))}
                              </div>
                            )}

                            <div className="flex gap-4 mt-2 text-xs text-muted-foreground">
                              <span>Tempo: {rec.time_estimate}</span>
                              <span>Risco: {rec.risk_level}</span>
                              <span>Impacto: {rec.impact_level}</span>
                            </div>
                          </div>
                        ))}
                    </CardContent>
                  </Card>
                )}
              </>
            ) : (
              /* Fallback: mostrar análise raw se não for JSON */
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Server className="h-5 w-5" />
                    Análise
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <pre className="whitespace-pre-wrap text-sm font-mono bg-muted p-4 rounded overflow-auto">
                    {analysis.analysis}
                  </pre>
                </CardContent>
              </Card>
            )}
          </div>
        </ScrollArea>

        {/* Footer */}
        <div className="border-t px-6 py-4 flex justify-end flex-shrink-0">
          <Button onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
