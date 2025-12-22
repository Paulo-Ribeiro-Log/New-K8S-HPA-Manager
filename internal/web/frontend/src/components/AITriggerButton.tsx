import { Button } from "@/components/ui/button";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import type { ResourceType } from "@/types/ai";
import { Brain, Loader2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

interface AITriggerButtonProps {
  resourceType: ResourceType;
  cluster: string;
  namespace: string;
  resourceName: string;
  variant?: "default" | "outline" | "ghost" | "secondary";
  size?: "default" | "sm" | "lg" | "icon";
  includeDescribe?: boolean;
  includeMetrics?: boolean;
  navigateToAITab?: boolean; // Se true, navega para aba AI após análise
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
  navigateToAITab = true,
  className,
  children,
}: AITriggerButtonProps) {
  const { analyzeResource, isAnalyzing } = useAIDiagnostics();
  const navigate = useNavigate();

  const handleAnalyze = async () => {
    const analysis = await analyzeResource({
      resourceType,
      cluster,
      namespace,
      resourceName,
      includeDescribe,
      includeMetrics,
    });

    // Se análise foi bem-sucedida e deve navegar para aba AI
    if (analysis && navigateToAITab) {
      // Aguardar um pouco para garantir que o estado foi atualizado
      setTimeout(() => {
        navigate("/ai-diagnostics");
      }, 500);
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
