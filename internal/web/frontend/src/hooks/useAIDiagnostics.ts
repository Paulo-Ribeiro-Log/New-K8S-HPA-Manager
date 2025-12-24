import { useState, useCallback, useEffect } from "react";
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

  /**
   * Fetch AI provider status
   */
  const fetchProviderStatus = useCallback(async () => {
    setIsLoadingStatus(true);
    try {
      const status = await apiClient.getAIProviderStatus();
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
      setIsAnalyzing(true);
      setCurrentAnalysis(null);

      try {
        toast({
          title: "🤖 Analisando com AI...",
          description: `Analisando ${request.resourceType}: ${request.resourceName}`,
        });

        const result = await apiClient.analyzeResource(request);
        setCurrentAnalysis(result);

        toast({
          title: "✅ Análise concluída",
          description: `${request.resourceType} analisado com sucesso`,
        });

        // Refresh history after new analysis
        await fetchHistory();

        return result;
      } catch (error) {
        console.error("Failed to analyze resource:", error);
        toast({
          title: "❌ Erro na análise AI",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return null;
      } finally {
        setIsAnalyzing(false);
      }
    },
    [toast, fetchHistory]
  );

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
   * Auto-refresh stats every 60 seconds
   */
  useEffect(() => {
    const interval = setInterval(() => {
      fetchStats();
    }, 60000); // 60 seconds

    return () => clearInterval(interval);
  }, [fetchStats]);

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
    fetchHistory,
    getAnalysisById,
    fetchProviderStatus,
    fetchStats,
    deleteAnalysis,
    clearCurrentAnalysis,
  };
}
