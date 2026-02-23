import { useState } from "react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

// ── Análise (mutation) ────────────────────────────────────────────────────────

export function useAnalyzeNodePool() {
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<any>(null);

  const analyze = async (cluster: string, nodepool: string) => {
    setLoading(true);
    setResult(null);

    toast.info("Iniciando análise preditiva do node pool...", {
      description: "Coletando métricas e executando análise com IA",
      duration: 5000,
    });

    try {
      const data = await apiClient.analyzeNodePool(cluster, nodepool);
      setResult(data);
      toast.success("Análise preditiva concluída!", {
        description: `Health Score: ${data.health_score?.overall ?? "?"}/100`,
      });
      return data;
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Erro desconhecido";
      toast.error("Erro na análise preditiva", { description: msg });
      const errorResult = { error: msg };
      setResult(errorResult);
      return errorResult;
    } finally {
      setLoading(false);
    }
  };

  const reset = () => {
    setResult(null);
    setLoading(false);
  };

  return { analyze, loading, result, reset };
}

// ── Histórico (query) ─────────────────────────────────────────────────────────

export function useNodePoolPredictionHistory(filters?: {
  cluster?: string;
  nodepool?: string;
  limit?: number;
  offset?: number;
}) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);

  const fetch = async (overrideFilters?: typeof filters) => {
    setLoading(true);
    setError(null);
    try {
      const res = await apiClient.getNodePoolPredictionHistory(overrideFilters ?? filters);
      setData(res);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Erro ao buscar histórico";
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  return { fetch, loading, data, error };
}
