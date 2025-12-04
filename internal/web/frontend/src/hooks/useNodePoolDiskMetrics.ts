import { useState, useEffect } from "react";
import { apiClient } from "@/lib/api/client";

export interface NodeDiskMetrics {
  node_name: string;
  node_pool_name: string;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  usage_percent: number;
  is_ephemeral: boolean;
  disk_type: string;
}

export interface NodePoolDiskMetrics {
  node_pool_name: string;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  usage_percent: number;
  node_count: number;
  nodes: NodeDiskMetrics[];
}

export function useNodePoolDiskMetrics(cluster: string, nodePoolName?: string) {
  const [metrics, setMetrics] = useState<NodePoolDiskMetrics | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    console.log('[useNodePoolDiskMetrics] Effect triggered - cluster:', cluster, 'nodePoolName:', nodePoolName);

    if (!cluster || !nodePoolName) {
      console.log('[useNodePoolDiskMetrics] Missing cluster or nodePoolName, skipping fetch');
      setMetrics(null);
      return;
    }

    const fetchMetrics = async () => {
      console.log('[useNodePoolDiskMetrics] Fetching metrics for:', cluster, nodePoolName);
      setLoading(true);
      setError(null);

      try {
        const response = await apiClient.getNodePoolDiskMetrics(cluster, nodePoolName);
        console.log('[useNodePoolDiskMetrics] Response:', response);

        if (response.success && response.data && response.data.length > 0) {
          // Retornar métricas do primeiro node pool (já filtrado no backend)
          console.log('[useNodePoolDiskMetrics] Setting metrics:', response.data[0]);
          setMetrics(response.data[0]);
        } else {
          console.log('[useNodePoolDiskMetrics] No metrics found in response');
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
