import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { ClusterInfo } from "@/lib/api/types";

/**
 * Hook para buscar informações do cluster
 * Inclui: contexto, versão K8s, métricas, etc
 */
export function useClusterInfo(cluster: string, enabled = true) {
  return useQuery<ClusterInfo>({
    queryKey: ["cluster-info", cluster],
    queryFn: async () => {
      return await apiClient.getClusterInfo(cluster);
    },
    enabled: enabled && !!cluster,
    staleTime: 30000, // 30 segundos
    refetchInterval: 60000, // Refresh a cada 1 minuto
  });
}

/**
 * Hook simplificado para buscar apenas a versão do Kubernetes
 */
export function useClusterVersion(cluster: string, enabled = true) {
  const { data, isLoading, error } = useClusterInfo(cluster, enabled);

  return {
    data: data
      ? {
          version: data.kubernetesVersion || "Unknown",
          context: data.context || cluster,
        }
      : null,
    isLoading,
    error,
  };
}
