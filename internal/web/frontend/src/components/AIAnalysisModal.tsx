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
import type { AnalysisResult } from "@/types/ai";
import {
  Brain,
  Clock,
  Cpu,
  CheckCircle2,
  ExternalLink,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { AIAnalysisView } from "./AIAnalysisView";

interface AIAnalysisModalProps {
  analysis: AnalysisResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

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
                <span className="truncate">Análise AI - {analysis.resource_type}</span>
              </DialogTitle>
              <DialogDescription className="flex items-center gap-2 flex-wrap text-xs sm:text-sm">
                <Badge variant="outline" className="font-mono text-[10px] sm:text-xs px-2 sm:px-3 py-0.5 sm:py-1">
                  <span className="truncate max-w-[200px] sm:max-w-[300px]">
                    {analysis.cluster}/{analysis.namespace}/{analysis.resource_name}
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
            {analysis.response_time && (
              <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
                <Clock className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.response_time.toFixed(1)}s</span>
              </div>
            )}
            {analysis.tokens_used && (
              <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
                <Cpu className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.tokens_used} tokens</span>
              </div>
            )}
            <div className="flex items-center gap-1 sm:gap-2 bg-white/50 dark:bg-black/20 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md">
              <CheckCircle2 className="h-3 w-3 sm:h-4 sm:w-4 flex-shrink-0" />
              <span className="font-medium whitespace-nowrap">{new Date(analysis.analyzed_at).toLocaleString("pt-BR")}</span>
            </div>
          </div>
        </DialogHeader>

        <ScrollArea className="flex-1 overflow-y-auto">
          <div className="px-4 sm:px-6 md:px-8 py-4 sm:py-6">
            <AIAnalysisView analysis={analysis} showExportButtons={true} />
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
