import { useState, useCallback, useEffect, useRef } from "react";
import { apiClient } from "@/lib/api/client";
import type {
  AnalyzeRequest,
  AnalysisResult,
  ProviderStatus,
  AIStats,
  HistoryFilter,
} from "@/types/ai";
import { useToast } from "@/components/ui/use-toast";

export function useAIDiagnostics() {
  const { toast } = useToast();

  const [isAnalyzing, setIsAnalyzing] = useState(false);
  const [currentAnalysis, setCurrentAnalysis] = useState<AnalysisResult | null>(null);
  const [history, setHistory] = useState<AnalysisResult[]>([]);
  const [providerStatus, setProviderStatus] = useState<ProviderStatus | null>(null);
  const [stats, setStats] = useState<AIStats | null>(null);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [isLoadingStatus, setIsLoadingStatus] = useState(false);

  // AbortController para cancelar análise em andamento
  const abortControllerRef = useRef<AbortController | null>(null);

  /**
   * Fetch AI provider status
   */
  const fetchProviderStatus = useCallback(async () => {
    setIsLoadingStatus(true);
    try {
      // Obter ai_email do localStorage
      const aiEmail = localStorage.getItem("ai_email") || "";
      const status = await apiClient.getAIProviderStatus(aiEmail);
      setProviderStatus(status);

      if (!status.available) {
        toast({
          title: "⚠️ AI Provider Indisponível",
          description: status.error || "O provider AI não está acessível no momento.",
          variant: "destructive",
        });
      }

      return status;
    } catch (error) {
      console.error("Failed to fetch AI provider status:", error);
      toast({
        title: "❌ Erro ao verificar status do AI",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
      return null;
    } finally {
      setIsLoadingStatus(false);
    }
  }, [toast]);

  /**
   * Fetch analysis history with optional filters
   */
  const fetchHistory = useCallback(
    async (filters?: HistoryFilter) => {
      setIsLoadingHistory(true);
      try {
        const historyData = await apiClient.getAIHistory(filters);
        setHistory(historyData);
        return historyData;
      } catch (error) {
        console.error("Failed to fetch AI history:", error);
        toast({
          title: "❌ Erro ao carregar histórico",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return [];
      } finally {
        setIsLoadingHistory(false);
      }
    },
    [toast]
  );

  /**
   * Analyze a Kubernetes resource with AI
   */
  const analyzeResource = useCallback(
    async (request: AnalyzeRequest): Promise<AnalysisResult | null> => {
      // Cancelar análise anterior se existir
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }

      // Criar novo AbortController
      abortControllerRef.current = new AbortController();

      setIsAnalyzing(true);
      setCurrentAnalysis(null);

      try {
        toast({
          title: "Analisando com AI...",
          description: `Analisando ${request.resourceType}: ${request.resourceName}`,
        });

        // Obter ai_email do localStorage e adicionar ao request
        const aiEmail = localStorage.getItem("ai_email") || "";
        const enrichedRequest = { ...request, aiEmail };

        const result = await apiClient.analyzeResource(enrichedRequest, abortControllerRef.current.signal);
        setCurrentAnalysis(result);

        toast({
          title: "Análise concluída",
          description: `${request.resourceType} analisado com sucesso`,
        });

        // Refresh history after new analysis
        await fetchHistory();

        return result;
      } catch (error) {
        // Ignorar erro se foi cancelamento
        if (error instanceof Error && error.name === "AbortError") {
          console.log("Analysis was cancelled by user");
          toast({
            title: "Análise cancelada",
            description: "A análise foi cancelada pelo usuário",
          });
          return null;
        }

        console.error("Failed to analyze resource:", error);
        toast({
          title: "Erro na análise AI",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return null;
      } finally {
        setIsAnalyzing(false);
        abortControllerRef.current = null;
      }
    },
    [toast, fetchHistory]
  );

  /**
   * Cancel ongoing analysis
   */
  const cancelAnalysis = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
      setIsAnalyzing(false);
    }
  }, []);

  /**
   * Get a specific analysis by ID
   */
  const getAnalysisById = useCallback(
    async (id: string): Promise<AnalysisResult | null> => {
      try {
        const analysis = await apiClient.getAnalysisById(id);
        setCurrentAnalysis(analysis);
        return analysis;
      } catch (error) {
        console.error("Failed to fetch analysis:", error);
        toast({
          title: "❌ Erro ao carregar análise",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return null;
      }
    },
    [toast]
  );

  /**
   * Fetch AI statistics
   */
  const fetchStats = useCallback(async () => {
    try {
      const statsData = await apiClient.getAIStats();
      setStats(statsData);
      return statsData;
    } catch (error) {
      console.error("Failed to fetch AI stats:", error);
      toast({
        title: "❌ Erro ao carregar estatísticas",
        description: error instanceof Error ? error.message : "Erro desconhecido",
        variant: "destructive",
      });
      return null;
    }
  }, [toast]);

  /**
   * Delete an analysis from history
   */
  const deleteAnalysis = useCallback(
    async (id: string): Promise<boolean> => {
      try {
        await apiClient.deleteAnalysis(id);

        // Remove from local state
        setHistory((prev) => prev.filter((item) => item.id !== id));

        // Clear current analysis if it was deleted
        if (currentAnalysis?.id === id) {
          setCurrentAnalysis(null);
        }

        toast({
          title: "✅ Análise deletada",
          description: "A análise foi removida do histórico",
        });

        return true;
      } catch (error) {
        console.error("Failed to delete analysis:", error);
        toast({
          title: "❌ Erro ao deletar análise",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return false;
      }
    },
    [currentAnalysis, toast]
  );

  /**
   * Clear current analysis
   */
  const clearCurrentAnalysis = useCallback(() => {
    setCurrentAnalysis(null);
  }, []);

  /**
   * REMOVIDO: Auto-refresh stats a cada 60 segundos
   *
   * MOTIVO: Com componentes sempre montados (Pods, ConfigMaps, etc), este hook
   * fica ativo em background mesmo quando usuário não está vendo a aba.
   * Isso causava chamadas desnecessárias ao backend, esgotando quota do Gemini.
   *
   * SOLUÇÃO: Stats devem ser carregados APENAS quando usuário explicitamente
   * acessar a aba de AI Diagnostics, via chamada manual a fetchStats().
   */
  // useEffect(() => {
  //   const interval = setInterval(() => {
  //     fetchStats();
  //   }, 60000);
  //   return () => clearInterval(interval);
  // }, [fetchStats]);

  return {
    // State
    isAnalyzing,
    currentAnalysis,
    history,
    providerStatus,
    stats,
    isLoadingHistory,
    isLoadingStatus,

    // Actions
    analyzeResource,
    cancelAnalysis,
    fetchHistory,
    getAnalysisById,
    fetchProviderStatus,
    fetchStats,
    deleteAnalysis,
    clearCurrentAnalysis,
  };
}
