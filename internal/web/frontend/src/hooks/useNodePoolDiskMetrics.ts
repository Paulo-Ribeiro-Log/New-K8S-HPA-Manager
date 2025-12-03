import { useState, useEffect } from "react";
import { apiClient } from "@/lib/api/client";

export interface NodePoolDiskMetrics {
  node_pool_name: string;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  usage_percent: number;
  node_count: number;
}

export function useNodePoolDiskMetrics(cluster: string, nodePoolName?: string) {
  const [metrics, setMetrics] = useState<NodePoolDiskMetrics | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!cluster || !nodePoolName) {
      setMetrics(null);
      return;
    }

    const fetchMetrics = async () => {
      setLoading(true);
      setError(null);

      try {
        const response = await apiClient.getNodePoolDiskMetrics(cluster, nodePoolName);

        if (response.success && response.data && response.data.length > 0) {
          // Retornar métricas do primeiro node pool (já filtrado no backend)
          setMetrics(response.data[0]);
        } else {
          setMetrics(null);
        }
      } catch (err) {
        console.error("[useNodePoolDiskMetrics] Error fetching metrics:", err);
        setError(err instanceof Error ? err.message : "Failed to fetch disk metrics");
        setMetrics(null);
      } finally {
        setLoading(false);
      }
    };

    fetchMetrics();
  }, [cluster, nodePoolName]);

  return { metrics, loading, error };
}
