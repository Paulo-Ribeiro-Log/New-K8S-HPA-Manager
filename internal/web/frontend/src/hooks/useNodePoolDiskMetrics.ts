import { useState, useEffect, useCallback } from "react";
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

  const fetchMetrics = useCallback(async () => {
    if (!cluster || !nodePoolName) {
      setMetrics(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const response = await apiClient.getNodePoolDiskMetrics(cluster, nodePoolName);
      if (response.success && response.data && response.data.length > 0) {
        setMetrics(response.data[0]);
      } else {
        setMetrics(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch disk metrics");
      setMetrics(null);
    } finally {
      setLoading(false);
    }
  }, [cluster, nodePoolName]);

  useEffect(() => {
    fetchMetrics();
  }, [fetchMetrics]);

  return { metrics, loading, error, refetch: fetchMetrics };
}
