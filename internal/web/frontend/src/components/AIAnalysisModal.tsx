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
import type { AnalysisResult } from "@/types/ai";
import ReactMarkdown from "react-markdown";
import {
  Brain,
  Clock,
  Cpu,
  Sparkles,
  AlertCircle,
  CheckCircle2,
  TrendingUp,
  Wrench,
  ExternalLink,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

interface AIAnalysisModalProps {
  analysis: AnalysisResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const suggestionIcons = {
  investigate: AlertCircle,
  fix: Wrench,
  optimize: TrendingUp,
  scale: Sparkles,
};

const priorityColors = {
  critical: "border-red-500 bg-red-50 dark:bg-red-950/20",
  high: "border-orange-500 bg-orange-50 dark:bg-orange-950/20",
  medium: "border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20",
  low: "border-blue-500 bg-blue-50 dark:bg-blue-950/20",
};

export function AIAnalysisModal({ analysis, open, onOpenChange }: AIAnalysisModalProps) {
  const navigate = useNavigate();

  if (!analysis) return null;

  const handleViewInAIDiagnostics = () => {
    onOpenChange(false);
    navigate("/ai-diagnostics");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-7xl w-[95vw] h-[95vh] p-0 overflow-hidden flex flex-col">
        <DialogHeader className="px-4 sm:px-6 md:px-8 pt-4 sm:pt-6 pb-3 sm:pb-4 border-b bg-gradient-to-r from-purple-50 to-blue-50 dark:from-purple-950/20 dark:to-blue-950/20 flex-shrink-0">
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-2 flex-1 min-w-0">
              <DialogTitle className="flex items-center gap-2 sm:gap-3 text-lg sm:text-xl md:text-2xl font-bold">
                <div className="p-1.5 sm:p-2 bg-purple-100 dark:bg-purple-900/30 rounded-lg flex-shrink-0">
                  <Brain className="h-4 w-4 sm:h-5 sm:w-5 md:h-6 md:w-6 text-purple-600 dark:text-purple-400" />
                </div>
                <span className="truncate">Análise AI - {analysis.resourceType}</span>
              </DialogTitle>
              <DialogDescription className="flex items-center gap-2 flex-wrap text-xs sm:text-sm">
                <Badge variant="outline" className="font-mono text-[10px] sm:text-xs px-2 sm:px-3 py-0.5 sm:py-1">
                  <span className="truncate max-w-[200px] sm:max-w-[300px]">
                    {analysis.cluster}/{analysis.namespace}/{analysis.resourceName}
                  </span>
                </Badge>
                <Badge variant="secondary" className="text-[10px] sm:text-xs px-2 sm:px-3 py-0.5 sm:py-1">
                  {analysis.provider} • {analysis.model || "default"}
                </Badge>
              </DialogDescription>
            </div>
          </div>

          {/* Metadados */}
          <div className="flex items-center gap-2 sm:gap-4 md:gap-6 flex-wrap text-[10px] sm:text-xs text-muted-foreground mt-3 sm:mt-4 pt-2 sm:pt-3 border-t border-purple-200/50 dark:border-purple-800/50">
            {analysis.responseTime && (
              <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
                <Clock className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.responseTime.toFixed(1)}s</span>
              </div>
            )}
            {analysis.tokensUsed && (
              <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
                <Cpu className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.tokensUsed} tokens</span>
              </div>
            )}
            <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
              <CheckCircle2 className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
              <span className="font-medium whitespace-nowrap">{new Date(analysis.analyzedAt).toLocaleString("pt-BR")}</span>
            </div>
          </div>
        </DialogHeader>

        <ScrollArea className="flex-1 overflow-y-auto">
          <div className="px-4 sm:px-6 md:px-8 py-4 sm:py-6 space-y-4 sm:space-y-6">
            {/* Análise Principal */}
            <Card className="border-2 shadow-sm">
              <CardHeader className="bg-gradient-to-r from-purple-50 to-transparent dark:from-purple-950/20 dark:to-transparent border-b p-3 sm:p-4 md:p-6">
                <CardTitle className="text-base sm:text-lg flex items-center gap-2 font-semibold">
                  <Sparkles className="h-4 w-4 sm:h-5 sm:w-5 text-purple-600 flex-shrink-0" />
                  Análise Detalhada
                </CardTitle>
              </CardHeader>
              <CardContent className="pt-4 sm:pt-6 p-3 sm:p-4 md:p-6">
                <div className="prose prose-sm dark:prose-invert max-w-none break-words overflow-wrap-anywhere">
                  <ReactMarkdown
                    components={{
                      // Garantir quebra de linha em códigos e pre
                      code: ({ node, className, ...props }) => {
                        const isInline = !className?.includes('language-');
                        return isInline ? (
                          <code className="break-all whitespace-pre-wrap" {...props} />
                        ) : (
                          <code className="block overflow-x-auto whitespace-pre-wrap break-words" {...props} />
                        );
                      },
                      pre: ({ node, ...props }) => (
                        <pre className="overflow-x-auto whitespace-pre-wrap break-words bg-muted p-4 rounded-lg" {...props} />
                      ),
                      // Garantir quebra de linha em parágrafos
                      p: ({ node, ...props }) => (
                        <p className="break-words whitespace-pre-wrap leading-relaxed my-4" {...props} />
                      ),
                      // Listas com melhor espaçamento
                      ul: ({ node, ...props }) => (
                        <ul className="space-y-2 my-4" {...props} />
                      ),
                      ol: ({ node, ...props }) => (
                        <ol className="space-y-2 my-4" {...props} />
                      ),
                      li: ({ node, ...props }) => (
                        <li className="break-words ml-4" {...props} />
                      ),
                      // Headers com melhor separação
                      h1: ({ node, ...props }) => (
                        <h1 className="text-2xl font-bold mt-8 mb-4 pb-2 border-b" {...props} />
                      ),
                      h2: ({ node, ...props }) => (
                        <h2 className="text-xl font-bold mt-6 mb-3 pb-2 border-b border-muted" {...props} />
                      ),
                      h3: ({ node, ...props }) => (
                        <h3 className="text-lg font-semibold mt-4 mb-2" {...props} />
                      ),
                    }}
                  >
                    {analysis.analysis}
                  </ReactMarkdown>
                </div>
              </CardContent>
            </Card>

            {/* Sugestões */}
            {analysis.suggestions && analysis.suggestions.length > 0 && (
              <Card className="border-2 shadow-sm">
                <CardHeader className="bg-gradient-to-r from-orange-50 to-transparent dark:from-orange-950/20 dark:to-transparent border-b p-3 sm:p-4 md:p-6">
                  <CardTitle className="text-base sm:text-lg flex items-center gap-2 font-semibold">
                    <AlertCircle className="h-4 w-4 sm:h-5 sm:w-5 text-orange-600 flex-shrink-0" />
                    Sugestões de Ação ({analysis.suggestions.length})
                  </CardTitle>
                </CardHeader>
                <CardContent className="pt-4 sm:pt-6 p-3 sm:p-4 md:p-6">
                  <div className="space-y-3 sm:space-y-4">
                    {analysis.suggestions.map((suggestion, idx) => {
                      const Icon = suggestionIcons[suggestion.type as keyof typeof suggestionIcons] || AlertCircle;
                      const priorityColor = priorityColors[suggestion.priority as keyof typeof priorityColors] || priorityColors.low;

                      return (
                        <div
                          key={idx}
                          className={`p-3 sm:p-4 rounded-lg border-l-4 shadow-sm ${priorityColor} transition-all hover:shadow-md`}
                        >
                          <div className="flex items-start gap-2 sm:gap-3 md:gap-4">
                            <div className="p-1.5 sm:p-2 bg-white dark:bg-black/20 rounded-lg flex-shrink-0">
                              <Icon className="h-4 w-4 sm:h-5 sm:w-5" />
                            </div>
                            <div className="flex-1 min-w-0 space-y-2">
                              <div className="flex items-center gap-1.5 sm:gap-2 flex-wrap">
                                <Badge variant="outline" className="text-[10px] sm:text-xs font-semibold">
                                  {suggestion.type}
                                </Badge>
                                <Badge 
                                  variant="secondary" 
                                  className={`text-[10px] sm:text-xs font-semibold ${
                                    suggestion.priority === 'critical' ? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-200' :
                                    suggestion.priority === 'high' ? 'bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-200' :
                                    suggestion.priority === 'medium' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-200' :
                                    'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-200'
                                  }`}
                                >
                                  {suggestion.priority}
                                </Badge>
                              </div>
                              <p className="text-xs sm:text-sm leading-relaxed break-words whitespace-pre-wrap">
                                {suggestion.description}
                              </p>
                              {suggestion.command && (
                                <div className="mt-2 sm:mt-3 bg-muted/50 rounded-lg p-2 sm:p-3 border">
                                  <code className="text-[10px] sm:text-xs font-mono block break-all whitespace-pre-wrap leading-relaxed">
                                    {suggestion.command}
                                  </code>
                                </div>
                              )}
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </CardContent>
              </Card>
            )}
          </div>
        </ScrollArea>

        {/* Footer Actions */}
        <div className="border-t bg-gradient-to-r from-muted/30 to-muted/10 px-4 sm:px-6 md:px-8 py-3 sm:py-4 flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-2 sm:gap-4 flex-shrink-0">
          <Button
            variant="outline"
            size="default"
            onClick={handleViewInAIDiagnostics}
            className="gap-2 font-medium w-full sm:w-auto"
          >
            <ExternalLink className="h-4 w-4" />
            <span className="hidden sm:inline">Ver no AI Diagnostics</span>
            <span className="sm:hidden">AI Diagnostics</span>
          </Button>
          <Button 
            variant="default" 
            size="default" 
            onClick={() => onOpenChange(false)}
            className="font-medium w-full sm:w-auto"
          >
            Fechar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
