import { useState, useCallback } from "react";
import { apiClient } from "@/lib/api/client";
import type {
  HealthCheckRequest,
  HealthCheckResult,
} from "@/types/healthcheck";
import { useToast } from "@/components/ui/use-toast";

export function useHealthChecking() {
  const { toast } = useToast();

  const [isRunning, setIsRunning] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [clusterSessions, setClusterSessions] = useState<Record<string, string>>({});
  const [results, setResults] = useState<HealthCheckResult[]>([]);
  const [history, setHistory] = useState<HealthCheckResult[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);

  /**
   * Run health check
   */
  const runHealthCheck = useCallback(
    async (request: HealthCheckRequest) => {
      console.log("[useHealthChecking] runHealthCheck called with request:", request);
      setIsRunning(true);
      try {
        console.log("[useHealthChecking] Calling apiClient.runHealthCheck...");
        const response = await apiClient.runHealthCheck(request);
        console.log("[useHealthChecking] Response received:", response);

        if (response.success) {
          setSessionId(response.session_id);
          setClusterSessions(response.cluster_sessions);
          console.log("[useHealthChecking] Session ID set:", response.session_id);
          console.log("[useHealthChecking] Cluster sessions:", response.cluster_sessions);
          toast({
            title: "✅ Health Check Iniciado",
            description: response.message,
          });
          return response.session_id;
        } else {
          console.error("[useHealthChecking] Response not successful:", response);
          toast({
            title: "❌ Erro ao iniciar health check",
            description: "Falha ao iniciar health check",
            variant: "destructive",
          });
          return null;
        }
      } catch (error) {
        console.error("[useHealthChecking] Exception caught:", error);
        toast({
          title: "❌ Erro ao executar health check",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        setIsRunning(false);
        return null;
      }
    },
    [toast]
  );

  /**
   * Fetch health check history
   */
  const fetchHistory = useCallback(
    async (cluster?: string, namespace?: string) => {
      setIsLoadingHistory(true);
      try {
        const response = await apiClient.getHealthCheckHistory(cluster, namespace);

        if (response.success) {
          setHistory(response.data);
          return response.data;
        } else {
          toast({
            title: "❌ Erro ao carregar histórico",
            description: "Falha ao buscar histórico de health checks",
            variant: "destructive",
          });
          return [];
        }
      } catch (error) {
        console.error("Failed to fetch health check history:", error);
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
   * Fetch health check statistics
   */
  const fetchStats = useCallback(
    async (cluster?: string, days?: number) => {
      try {
        const response = await apiClient.getHealthCheckStats(cluster, days);

        if (response.success) {
          return response.data;
        } else {
          toast({
            title: "❌ Erro ao carregar estatísticas",
            description: "Falha ao buscar estatísticas de health checks",
            variant: "destructive",
          });
          return null;
        }
      } catch (error) {
        console.error("Failed to fetch health check stats:", error);
        toast({
          title: "❌ Erro ao carregar estatísticas",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return null;
      }
    },
    [toast]
  );

  /**
   * Get specific health check result by ID
   */
  const getResult = useCallback(
    async (id: string) => {
      try {
        const response = await apiClient.getHealthCheckResult(id);

        if (response.success) {
          return response.data;
        } else {
          toast({
            title: "❌ Resultado não encontrado",
            description: "Health check result não foi encontrado",
            variant: "destructive",
          });
          return null;
        }
      } catch (error) {
        console.error("Failed to get health check result:", error);
        toast({
          title: "❌ Erro ao buscar resultado",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return null;
      }
    },
    [toast]
  );

  /**
   * Delete health check result
   */
  const deleteResult = useCallback(
    async (id: string) => {
      try {
        const response = await apiClient.deleteHealthCheckResult(id);

        if (response.success) {
          toast({
            title: "✅ Resultado deletado",
            description: response.message,
          });

          // Remove from history
          setHistory((prev) => prev.filter((r) => r.id !== id));

          return true;
        } else {
          toast({
            title: "❌ Erro ao deletar resultado",
            description: "Falha ao deletar health check result",
            variant: "destructive",
          });
          return false;
        }
      } catch (error) {
        console.error("Failed to delete health check result:", error);
        toast({
          title: "❌ Erro ao deletar resultado",
          description: error instanceof Error ? error.message : "Erro desconhecido",
          variant: "destructive",
        });
        return false;
      }
    },
    [toast]
  );

  /**
   * Reset state (useful after completion)
   */
  const reset = useCallback(() => {
    setIsRunning(false);
    setSessionId(null);
    setClusterSessions({});
    setResults([]);
  }, []);

  /**
   * Mark health check as completed
   */
  const markCompleted = useCallback((result: HealthCheckResult) => {
    setIsRunning(false);
    setResults((prev) => [...prev, result]);
  }, []);

  return {
    // State
    isRunning,
    sessionId,
    clusterSessions,
    results,
    history,
    isLoadingHistory,

    // Methods
    runHealthCheck,
    fetchHistory,
    fetchStats,
    getResult,
    deleteResult,
    reset,
    markCompleted,
  };
}
