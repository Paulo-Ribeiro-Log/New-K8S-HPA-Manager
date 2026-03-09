import { useState, useEffect } from "react";
import { apiClient } from "@/lib/api/client";
import type { NodeInfo, NodesListResponse, NodeDetailsResponse } from "@/lib/api/types";

export function useNodes(cluster: string, nodePoolName: string) {
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchNodes = async () => {
    if (!cluster || !nodePoolName) {
      console.log('[useNodes] Skipping fetch - missing params:', { cluster, nodePoolName });
      setNodes([]);
      return;
    }

    console.log('[useNodes] Fetching nodes for:', { cluster, nodePoolName });

    try {
      setLoading(true);
      setError(null);
      const response: NodesListResponse = await apiClient.getNodesInNodePool(cluster, nodePoolName);
      console.log('[useNodes] API Response:', response);
      console.log('[useNodes] Nodes count:', response.nodes?.length || 0);
      setNodes(response.nodes || []);
    } catch (err) {
      console.error('[useNodes] Error fetching nodes:', err);
      setError(err instanceof Error ? err.message : "Failed to fetch nodes");
      setNodes([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchNodes();
  }, [cluster, nodePoolName]);

  return { nodes, loading, error, refetch: fetchNodes };
}

export function useNodeDetails(cluster: string, nodePoolName: string, nodeName: string) {
  const [nodeDetails, setNodeDetails] = useState<NodeDetailsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchNodeDetails = async () => {
    if (!cluster || !nodePoolName || !nodeName) {
      setNodeDetails(null);
      setError(null);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      // Clear previous data before fetching new data to avoid state confusion
      setNodeDetails(null);
      const response: NodeDetailsResponse = await apiClient.getNodeDetails(cluster, nodePoolName, nodeName);
      console.log('📊 [useNodeDetails] Response received for node:', response.node.name);
      setNodeDetails(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch node details");
      setNodeDetails(null);
    } finally {
      setLoading(false);
    }
  };

  // Clear state immediately when parameters change
  useEffect(() => {
    setNodeDetails(null);
    setError(null);
    setLoading(false);
  }, [cluster, nodePoolName, nodeName]);

  // Fetch data after state is cleared
  useEffect(() => {
    const timer = setTimeout(() => {
      fetchNodeDetails();
    }, 50); // Small delay to ensure state is cleared

    return () => clearTimeout(timer);
  }, [cluster, nodePoolName, nodeName]);

  return { nodeDetails, loading, error, refetch: fetchNodeDetails };
}
