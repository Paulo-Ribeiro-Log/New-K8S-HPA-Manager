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

  console.log('✅ AITriggerButton renderizado:', { resourceType, cluster, namespace, resourceName });

  const handleAnalyze = async () => {
    console.log('🚀 handleAnalyze chamado!', { resourceType, cluster, namespace, resourceName });
    
    try {
      console.log('📡 Chamando analyzeResource...');
      const analysis = await analyzeResource({
        resourceType,
        cluster,
        namespace,
        resourceName,
        includeDescribe,
        includeMetrics,
        includeLogs: true,
      });

      console.log('✅ Análise retornada:', analysis);

      // Se análise foi bem-sucedida e deve navegar para aba AI
      if (analysis && navigateToAITab) {
        console.log('🔄 Navegando para /ai-diagnostics...');
        // Aguardar um pouco para garantir que o estado foi atualizado
        setTimeout(() => {
          navigate("/ai-diagnostics");
        }, 500);
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
