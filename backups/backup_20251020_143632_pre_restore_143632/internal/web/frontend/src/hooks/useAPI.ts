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
  const [lastValidCluster, setLastValidCluster] = useState<string>('');

  const fetchHPAs = async (forceRefresh = false) => {
    // 🔧 FIX: Só preservar dados se não for um refresh forçado
    if (!cluster && !forceRefresh) {
      console.log('[useHPAs] Cluster not provided, skipping fetch (preserving existing data)');
      return;
    }
    
    if (!cluster && forceRefresh) {
      console.log('[useHPAs] Force refresh requested but no cluster, skipping');
      return;
    }

    try {
      setLoading(true);
      setError(null);
      console.log(`[useHPAs] Fetching HPAs for cluster: ${cluster}, namespace: ${namespace || 'all'}, forceRefresh: ${forceRefresh}`);
      const data = await apiClient.getHPAs(cluster, namespace);
      setHPAs(data);
      setLastValidCluster(cluster); // 🔧 Armazenar último cluster válido
      console.log(`[useHPAs] Fetched ${data.length} HPAs successfully`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch HPAs");
      console.error('[useHPAs] Error fetching HPAs:', err);
    } finally {
      setLoading(false);
    }
  };

  const updateHPA = async (
    hpaCluster: string,
    hpaNamespace: string,
    hpaName: string,
    updates: Partial<HPA>
  ) => {
    try {
      await apiClient.updateHPA(hpaCluster, hpaNamespace, hpaName, updates);
      await fetchHPAs(); // Refresh list
    } catch (err) {
      throw err;
    }
  };

  useEffect(() => {
    // 🔧 FIX: Só fazer fetch se cluster estiver definido e for diferente do último
    if (cluster) {
      console.log(`[useHPAs] useEffect triggered - cluster: ${cluster}, namespace: ${namespace || 'all'}, current HPAs: ${hpas.length}`);
      fetchHPAs();
    } else if (lastValidCluster) {
      console.log(`[useHPAs] useEffect: cluster undefined but had data from ${lastValidCluster}, preserving ${hpas.length} HPAs`);
    } else {
      console.log('[useHPAs] useEffect triggered but cluster is undefined, skipping fetch');
    }
  }, [cluster, namespace]);

  // 🔧 FIX: Função de refetch que força busca de dados
  const safeRefetch = () => {
    if (cluster) {
      console.log(`[useHPAs] ⚡ SAFE REFETCH CALLED - cluster: ${cluster}, current HPAs: ${hpas.length}`);
      fetchHPAs(true); // Forçar refresh
    } else {
      console.log(`[useHPAs] ⚠️ SAFE REFETCH - cluster undefined, preserving ${hpas.length} HPAs`);
    }
  };

  // 🔧 FIX: Re-scan único 2 segundos após aplicação das alterações
  const postApplyRefetch = () => {
    console.log('[useHPAs] postApplyRefetch: aguardando 2s para re-scan após aplicação');
    
    // Re-scan ÚNICO após 2 segundos para aguardar propagação das mudanças no K8s
    setTimeout(() => {
      if (cluster) {
        console.log(`[useHPAs] Re-scan pós-aplicação (2s): FORCE REFRESH HPAs do cluster ${cluster}`);
        // Forçar novo fetch com parâmetro explicito
        fetchHPAs(true);
      } else {
        console.log('[useHPAs] Re-scan pós-aplicação (2s): cluster undefined, skipping');
      }
    }, 2000);
  };

  // 🔧 FIX: Refetch com parâmetros explícitos para garantir cluster correto
  const forceRefetchWithParams = (forceCluster?: string, forceNamespace?: string) => {
    const targetCluster = forceCluster || cluster;
    const targetNamespace = forceNamespace || namespace;
    
    console.log(`[useHPAs] 🎯 FORCE REFETCH START`);
    console.log(`[useHPAs] - targetCluster: ${targetCluster}`);
    console.log(`[useHPAs] - targetNamespace: ${targetNamespace}`);
    console.log(`[useHPAs] - current hpas.length: ${hpas.length}`);
    
    if (targetCluster) {
      // Temporariamente atualizar cluster se diferente
      if (targetCluster !== cluster) {
        console.log(`[useHPAs] ⚠️ Cluster mismatch! hook=${cluster}, forced=${targetCluster}`);
      }
      
      // Fazer fetch direto com parâmetros específicos
      const forceAPICall = async () => {
        try {
          console.log(`[useHPAs] 🔄 Setting loading=true...`);
          setLoading(true);
          setError(null);
          
          console.log(`[useHPAs] 📡 Making API call to getHPAs(${targetCluster}, ${targetNamespace})...`);
          const data = await apiClient.getHPAs(targetCluster, targetNamespace);
          
          console.log(`[useHPAs] 📦 API response received:`, data);
          console.log(`[useHPAs] 📦 Setting ${data.length} HPAs in state...`);
          
          setHPAs(data);
          setLastValidCluster(targetCluster);
          
          console.log(`[useHPAs] ✅ State updated successfully - ${data.length} HPAs set`);
          
          // Forçar re-render
          setTimeout(() => {
            console.log(`[useHPAs] 🔄 Post-update check - hpas.length is now: ${hpas.length}`);
          }, 100);
          
        } catch (err) {
          console.error('[useHPAs] ❌ API call failed:', err);
          setError(err instanceof Error ? err.message : "Force refetch failed");
        } finally {
          console.log(`[useHPAs] ⚙️ Setting loading=false...`);
          setLoading(false);
        }
      };
      
      forceAPICall();
    } else {
      console.log('[useHPAs] ❌ Force refetch: no target cluster provided');
    }
  };

  return { hpas, loading, error, refetch: safeRefetch, updateHPA, postApplyRefetch, forceRefetchWithParams };
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
      // Invalidar cache dos CronJobs
      queryClient.invalidateQueries({ 
        queryKey: ['cronjobs', variables.cluster, variables.namespace] 
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
      const data = await apiClient.getCronJobs(cluster, namespace);
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
      const data = await apiClient.getPrometheusResources(cluster, namespace);
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
