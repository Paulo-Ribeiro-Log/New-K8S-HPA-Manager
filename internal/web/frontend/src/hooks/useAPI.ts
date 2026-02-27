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
  ConfigMapSummary,
  SecretSummary,
  DeploymentSummary,
  DaemonSetSummary,
  StatefulSetSummary,
  IngressSummary,
  PodSummary,
  ServiceSummary,
  VPASummary,
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

export function useHPAs(cluster?: string, namespace?: string, showSystem: boolean = false) {
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

    console.log(`[useHPAs.fetchHPAs] Fetching - cluster: "${cluster}", namespace: "${nsToUse || 'all'}", bypassCache: ${bypassCache}, showSystem: ${showSystem}`);

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getHPAs(cluster, nsToUse, bypassCache, showSystem);
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
  }, [cluster, namespace, showSystem]);

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

  // Listen for rescan events
  useEffect(() => {
    const handleRescan = (event: Event) => {
      const customEvent = event as CustomEvent<{ cluster?: string }>;
      const targetCluster = customEvent.detail?.cluster;

      console.log(`[useNodePools] Rescan event received - Event cluster: "${targetCluster}", Hook cluster: "${cluster}"`);

      // Se o evento não especificou cluster, recarrega todos
      // Se especificou, só recarrega se for o mesmo cluster
      if (targetCluster && targetCluster !== cluster) {
        console.log(`[useNodePools] Ignoring rescan - cluster mismatch`);
        return;
      }

      console.log(`[useNodePools] Rescanning node pools for cluster: ${cluster}`);
      fetchNodePools().catch((err) => {
        console.error("[useNodePools] Error during rescan:", err);
      });
    };

    if (typeof window !== "undefined") {
      window.addEventListener("rescanNodePools", handleRescan as EventListener);
    }

    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener("rescanNodePools", handleRescan as EventListener);
      }
    };
  }, [cluster]);

  return {
    nodePools,
    loading,
    error,
    refetch: fetchNodePools,
    applySequential,
  };
}

export function useConfigMaps(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [configMaps, setConfigMaps] = useState<ConfigMapSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchConfigMaps = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setConfigMaps([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getConfigMaps(cluster, namespaces, undefined, showSystem, bypassCache);
      setConfigMaps(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch configmaps");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchConfigMaps();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchConfigMaps(true);

  return { configMaps, loading, error, refetch };
}

export function useSecrets(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [secrets, setSecrets] = useState<SecretSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchSecrets = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setSecrets([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getSecrets(cluster, namespaces, showSystem, bypassCache);
      setSecrets(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch secrets");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSecrets();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchSecrets(true);

  return { secrets, loading, error, refetch };
}

export function useDeployments(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [deployments, setDeployments] = useState<DeploymentSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchDeployments = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setDeployments([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getDeployments(cluster, namespaces, undefined, showSystem, bypassCache);
      setDeployments(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch deployments");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDeployments();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchDeployments(true);

  return { deployments, loading, error, refetch };
}

export function useDaemonSets(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [daemonsets, setDaemonSets] = useState<DaemonSetSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchDaemonSets = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setDaemonSets([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getDaemonSets(cluster, namespaces, undefined, showSystem, bypassCache);
      setDaemonSets(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch daemonsets");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDaemonSets();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchDaemonSets(true);

  return { daemonsets, loading, error, refetch };
}

export function useStatefulSets(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [statefulsets, setStatefulSets] = useState<StatefulSetSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchStatefulSets = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setStatefulSets([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getStatefulSets(cluster, namespaces, undefined, showSystem, bypassCache);
      setStatefulSets(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch statefulsets");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStatefulSets();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchStatefulSets(true);

  return { statefulsets, loading, error, refetch };
}

export function useIngresses(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [ingresses, setIngresses] = useState<IngressSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchIngresses = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setIngresses([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getIngresses(cluster, namespaces, undefined, showSystem, bypassCache);
      setIngresses(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch ingresses");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchIngresses();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchIngresses(true);

  return { ingresses, loading, error, refetch };
}

export function usePods(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [pods, setPods] = useState<PodSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchPods = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setPods([]);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getPods(cluster, namespaces, undefined, showSystem, bypassCache);
      setPods(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch pods");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchPods();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchPods(true);

  return { pods, loading, error, refetch };
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
      data: { suspend?: boolean; schedule?: string };
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

export function useServices(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [services, setServices] = useState<ServiceSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchServices = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setServices([]);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.getServices(cluster, namespaces || [], showSystem);
      void bypassCache;
      setServices(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch services");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchServices();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchServices(true);

  return { services, loading, error, refetch };
}

export function useVPAs(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [vpas, setVPAs] = useState<VPASummary[]>([]);
  const [crdNotInstalled, setCrdNotInstalled] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchVPAs = async () => {
    if (!cluster) {
      setVPAs([]);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.getVPAs(cluster, namespaces, showSystem);
      setVPAs(result.data);
      setCrdNotInstalled(result.crdNotInstalled || false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch VPAs");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchVPAs();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchVPAs();

  return { vpas, crdNotInstalled, loading, error, refetch };
}
