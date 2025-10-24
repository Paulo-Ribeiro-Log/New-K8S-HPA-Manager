// Custom React hook for API operations

import { useState, useEffect } from "react";
import { apiClient } from "@/lib/api/client";
import type {
  Cluster,
  Namespace,
  HPA,
  NodePool,
  CronJob,
  PrometheusResource,
} from "@/lib/api/types";
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';

export function useClusters() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchClusters = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getClusters();
      setClusters(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch clusters");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchClusters();
  }, []);

  return { clusters, loading, error, refetch: fetchClusters };
}

export function useNamespaces(cluster?: string) {
  const [namespaces, setNamespaces] = useState<Namespace[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchNamespaces = async () => {
    if (!cluster) {
      setNamespaces([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getNamespaces(cluster);
      setNamespaces(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch namespaces"
      );
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchNamespaces();
  }, [cluster]);

  return { namespaces, loading, error, refetch: fetchNamespaces };
}

export function useHPAs(cluster?: string, namespace?: string) {
  const [hpas, setHPAs] = useState<HPA[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchHPAs = async (bypassCache: boolean = false, overrideNamespace?: string | null) => {
    if (!cluster) {
      setHPAs([]);
      return;
    }

    // Se overrideNamespace for null, ignora o namespace (busca todos)
    // Se for undefined, usa o namespace da prop
    const nsToUse = overrideNamespace === null ? undefined : (overrideNamespace !== undefined ? overrideNamespace : namespace);
    
    console.log(`[useHPAs.fetchHPAs] Fetching - cluster: "${cluster}", namespace: "${nsToUse || 'all'}", bypassCache: ${bypassCache}`);

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getHPAs(cluster, nsToUse, bypassCache);
      console.log(`[useHPAs.fetchHPAs] Received ${data.length} HPAs`);
      setHPAs(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch HPAs");
    } finally {
      setLoading(false);
    }
  };

  const updateHPA = async (
    hpaCluster: string,
    hpaNamespace: string,
    hpaName: string,
    updates: Partial<HPA>
  ): Promise<HPA> => {
    try {
      await apiClient.updateHPA(hpaCluster, hpaNamespace, hpaName, updates);
      const fresh = await apiClient.getHPA(hpaCluster, hpaNamespace, hpaName, true);

      setHPAs((prev) => {
        const index = prev.findIndex(
          (item) =>
            item.cluster === fresh.cluster &&
            item.namespace === fresh.namespace &&
            item.name === fresh.name
        );

        if (index === -1) {
          return [...prev, fresh];
        }

        const next = [...prev];
        next[index] = fresh;
        return next;
      });

      // Disparar evento de rescan para recarregar a lista correta
      if (typeof window !== "undefined") {
        window.dispatchEvent(new CustomEvent("rescanHPAs", {
          detail: { cluster: hpaCluster, namespace: hpaNamespace }
        }));
      }

      return fresh;
    } catch (err) {
      throw err;
    }
  };

  useEffect(() => {
    fetchHPAs();
  }, [cluster, namespace]);

  useEffect(() => {
    const handleRescan = (event: Event) => {
      const customEvent = event as CustomEvent<{ cluster?: string; namespace?: string }>;
      const targetCluster = customEvent.detail?.cluster;

      console.log(`[useHPAs] Rescan event received - Event cluster: "${targetCluster}", Hook cluster: "${cluster}"`);

      // Se o evento não especificou cluster, recarrega todos
      // Se especificou, só recarrega se for o mesmo cluster
      if (targetCluster && targetCluster !== cluster) {
        console.log(`[useHPAs] Ignoring rescan - cluster mismatch`);
        return;
      }

      // No rescan, sempre buscar TODOS os HPAs do cluster (ignorar filtro de namespace)
      console.log(`[useHPAs] Rescanning ALL HPAs for cluster: ${cluster} (ignoring namespace filter)`);
      fetchHPAs(true, null).catch((err) => {
        console.error("[useHPAs] Error during rescan:", err);
      });
    };

    if (typeof window !== "undefined") {
      window.addEventListener("rescanHPAs", handleRescan as EventListener);
    }

    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener("rescanHPAs", handleRescan as EventListener);
      }
    };
  }, [cluster, namespace]);

  return { hpas, loading, error, refetch: fetchHPAs, updateHPA };
}

export function useNodePools(cluster?: string) {
  const [nodePools, setNodePools] = useState<NodePool[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchNodePools = async () => {
    if (!cluster) {
      console.log('[useNodePools] No cluster selected, clearing node pools');
      setNodePools([]);
      return;
    }

    console.log('[useNodePools] Fetching node pools for cluster:', cluster);
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getNodePools(cluster);
      console.log('[useNodePools] Received data:', data);
      setNodePools(data);
    } catch (err) {
      console.error('[useNodePools] Error fetching node pools:', err);
      setError(
        err instanceof Error ? err.message : "Failed to fetch node pools"
      );
    } finally {
      setLoading(false);
    }
  };

  const applySequential = async (pools: NodePool[]) => {
    try {
      return await apiClient.applyNodePoolsSequential(pools);
    } catch (err) {
      throw err;
    }
  };

  useEffect(() => {
    fetchNodePools();
  }, [cluster]);

  return {
    nodePools,
    loading,
    error,
    refetch: fetchNodePools,
    applySequential,
  };
}

// CronJobs hooks
export function useCronJobs(cluster?: string) {
  return useQuery({
    queryKey: ['cronjobs', cluster],
    queryFn: () => apiClient.getCronJobs(cluster),
    enabled: !!cluster,
    staleTime: 30000,
  });
}

export function useUpdateCronJob() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ cluster, namespace, name, data }: {
      cluster: string;
      namespace: string;
      name: string;
      data: { suspend: boolean };
    }) => apiClient.updateCronJob(cluster, namespace, name, data),
    onSuccess: (data, variables) => {
      // Invalidar cache dos CronJobs (query key: ['cronjobs', cluster])
      queryClient.invalidateQueries({
        queryKey: ['cronjobs', variables.cluster]
      });
    },
  });
}

// Prometheus hooks
export function usePrometheusResources(cluster?: string) {
  return useQuery({
    queryKey: ['prometheus', cluster],
    queryFn: () => apiClient.getPrometheusResources(cluster),
    enabled: !!cluster,
    staleTime: 30000,
  });
}

export function useUpdatePrometheusResource() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: ({ cluster, namespace, type, name, data }: {
      cluster: string;
      namespace: string;
      type: string;
      name: string;
      data: {
        cpu_request: string;
        memory_request: string;
        cpu_limit: string;
        memory_limit: string;
        replicas?: number;
      };
    }) => apiClient.updatePrometheusResource(cluster, namespace, type, name, data),
    onSuccess: (data, variables) => {
      // Invalidar cache dos recursos Prometheus
      queryClient.invalidateQueries({ 
        queryKey: ['prometheus', variables.cluster, variables.namespace] 
      });
    },
  });
}

export function useCronJobsOld(cluster?: string, namespace?: string) {
  const [cronJobs, setCronJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchCronJobs = async () => {
    if (!cluster) {
      setCronJobs([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getCronJobs(cluster);
      setCronJobs(data);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch cronjobs"
      );
    } finally {
      setLoading(false);
    }
  };

  const updateCronJob = async (
    jobCluster: string,
    jobNamespace: string,
    jobName: string,
    updates: Partial<CronJob>
  ) => {
    try {
      await apiClient.updateCronJob(jobCluster, jobNamespace, jobName, updates);
      await fetchCronJobs(); // Refresh list
    } catch (err) {
      throw err;
    }
  };

  useEffect(() => {
    fetchCronJobs();
  }, [cluster, namespace]);

  return { cronJobs, loading, error, refetch: fetchCronJobs, updateCronJob };
}

export function usePrometheusOld(cluster?: string, namespace?: string) {
  const [resources, setResources] = useState<PrometheusResource[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchResources = async () => {
    if (!cluster) {
      setResources([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getPrometheusResources(cluster);
      setResources(data);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Failed to fetch Prometheus resources"
      );
    } finally {
      setLoading(false);
    }
  };

  const updateResource = async (
    resCluster: string,
    resNamespace: string,
    resType: string,
    resName: string,
    updates: Partial<PrometheusResource>
  ) => {
    try {
      await apiClient.updatePrometheusResource(
        resCluster,
        resNamespace,
        resType,
        resName,
        updates
      );
      await fetchResources(); // Refresh list
    } catch (err) {
      throw err;
    }
  };

  useEffect(() => {
    fetchResources();
  }, [cluster, namespace]);

  return {
    resources,
    loading,
    error,
    refetch: fetchResources,
    updateResource,
  };
}
