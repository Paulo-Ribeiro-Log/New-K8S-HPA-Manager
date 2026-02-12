import { useState, useEffect } from "react";
import { apiClient } from "@/lib/api/client";
import type { NodeInfo, NodesListResponse, NodeDetailsResponse } from "@/lib/api/types";

export function useNodes(cluster: string, nodePoolName: string) {
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchNodes = async () => {
    if (!cluster || !nodePoolName) {
      setNodes([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const response: NodesListResponse = await apiClient.getNodesInNodePool(cluster, nodePoolName);
      setNodes(response.nodes || []);
    } catch (err) {
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
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const response: NodeDetailsResponse = await apiClient.getNodeDetails(cluster, nodePoolName, nodeName);
      setNodeDetails(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch node details");
      setNodeDetails(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchNodeDetails();
  }, [cluster, nodePoolName, nodeName]);

  return { nodeDetails, loading, error, refetch: fetchNodeDetails };
}
