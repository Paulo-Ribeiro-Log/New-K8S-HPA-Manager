import { Button } from "@/components/ui/button";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import type { ResourceType } from "@/types/ai";
import { Brain, Loader2 } from "lucide-react";
import { toast } from "sonner";

interface AITriggerButtonProps {
  resourceType: ResourceType;
  cluster: string;
  namespace: string;
  resourceName: string;
  variant?: "default" | "outline" | "ghost" | "secondary";
  size?: "default" | "sm" | "lg" | "icon";
  includeDescribe?: boolean;
  includeMetrics?: boolean;
  className?: string;
  children?: React.ReactNode;
}

export function AITriggerButton({
  resourceType,
  cluster,
  namespace,
  resourceName,
  variant = "outline",
  size = "sm",
  includeDescribe = true,
  includeMetrics = false,
  className,
  children,
}: AITriggerButtonProps) {
  const { analyzeResource, isAnalyzing } = useAIDiagnostics();

  const handleAnalyze = async () => {
    try {
      const analysis = await analyzeResource({
        resourceType,
        cluster,
        namespace,
        resourceName,
        includeDescribe,
        includeMetrics,
        includeLogs: true,
      });

      // Se análise foi bem-sucedida, abrir em nova aba (como faz na aba Pods)
      if (analysis) {
        // Salvar análise no sessionStorage para a nova página acessar
        sessionStorage.setItem(`ai-analysis-${analysis.id}`, JSON.stringify(analysis));

        // Abrir em nova aba do navegador
        const newWindow = window.open(`/ai-analysis/${analysis.id}`, '_blank');

        if (newWindow) {
          toast.success('Análise aberta em nova aba');
        } else {
          toast.error('Popup bloqueado! Permita popups para este site.');
        }
      }
    } catch (error) {
      console.error('❌ Erro ao analisar:', error);
    }
  };

  return (
    <Button
      variant={variant}
      size={size}
      onClick={handleAnalyze}
      disabled={isAnalyzing}
      className={className}
    >
      {isAnalyzing ? (
        <>
          <Loader2 className="h-4 w-4 mr-2 animate-spin" />
          Analisando...
        </>
      ) : (
        <>
          <Brain className="h-4 w-4 mr-2" />
          {children || "Analisar com AI"}
        </>
      )}
    </Button>
  );
}
