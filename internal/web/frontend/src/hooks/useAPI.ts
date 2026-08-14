// Custom React hook for API operations

import { useState, useEffect, useRef } from "react";
import { apiClient } from "@/lib/api/client";
import type {
  Cluster,
  Namespace,
  HPA,
  NodePool,
  ConfigMapSummary,
  ConfigMapUsage,
  SecretSummary,
  DeploymentSummary,
  DaemonSetSummary,
  StatefulSetSummary,
  IngressSummary,
  GatewaySummary,
  PodSummary,
  ServiceSummary,
  VPASummary,
  APIResourceInfo,
  GenericResourceSummary,
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      apiClient.getHPAs(cluster, namespace || undefined, true, showSystem)
        .then(data => { if (!cancelled) setHPAs(data); })
        .catch(() => {});
    }, 30000);
    return () => { cancelled = true; clearInterval(interval); };
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
  const [notSupported, setNotSupported] = useState(false);
  const lastClusterRef = useRef<string | undefined>(undefined);
  // Sempre reflete o `cluster` mais recente (atualizado a cada render, não só quando muda) —
  // usado por fetchNodePools pra descartar respostas de uma busca já obsoleta. Bug real
  // corrigido: ao trocar de cluster rapidamente (antes da resposta anterior chegar), a resposta
  // da busca ANTIGA podia resolver DEPOIS da busca nova e sobrescrever os dados corretos com os
  // do cluster errado — sintoma relatado: node pools de um cluster aparecendo com outro cluster
  // selecionado (mesmo bug existia, de forma ainda mais visível, no fetchPods de PodsPanel.tsx).
  // O refresh automático de 60s (useEffect abaixo) já tinha proteção equivalente (`cancelled`);
  // só a busca inicial/manual estava desprotegida.
  const currentClusterRef = useRef(cluster);
  currentClusterRef.current = cluster;

  const fetchNodePools = async () => {
    if (!cluster) {
      setNodePools([]);
      setNotSupported(false);
      lastClusterRef.current = undefined;
      return;
    }

    const requestedCluster = cluster;

    // Limpa dados apenas quando o cluster muda — evita flash no refresh manual
    if (cluster !== lastClusterRef.current) {
      setNodePools([]);
      setNotSupported(false);
      lastClusterRef.current = cluster;
    }

    try {
      setLoading(true);
      setError(null);
      const { pools, notSupported: ns } = await apiClient.getNodePools(cluster);
      if (currentClusterRef.current !== requestedCluster) return; // resposta obsoleta, ignora
      setNodePools(pools);
      setNotSupported(ns);
    } catch (err) {
      if (currentClusterRef.current !== requestedCluster) return;
      console.error('[useNodePools] Error fetching node pools:', err);
      setNodePools([]);
      setError(
        err instanceof Error ? err.message : "Failed to fetch node pools"
      );
    } finally {
      if (currentClusterRef.current === requestedCluster) {
        setLoading(false);
      }
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      apiClient.getNodePools(cluster)
        .then(({ pools, notSupported: ns }) => {
          if (!cancelled) {
            setNodePools(pools);
            setNotSupported(ns);
          }
        })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
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
    notSupported,
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getConfigMaps(cluster, namespaces, undefined, showSystem, true)
        .then(data => { if (!cancelled) setConfigMaps(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchConfigMaps(true);
  const silentRefetch = () => {
    if (!cluster) return;
    apiClient.getConfigMaps(cluster, namespaces, undefined, showSystem, true)
      .then(data => setConfigMaps(data))
      .catch(() => {});
  };

  return { configMaps, loading, error, refetch, silentRefetch };
}

export function useConfigMapUsage(cluster?: string, namespace?: string) {
  const [usage, setUsage] = useState<ConfigMapUsage[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchUsage = async () => {
    if (!cluster) {
      setUsage([]);
      return;
    }
    try {
      setLoading(true);
      const data = await apiClient.getConfigMapUsage(cluster, namespace);
      setUsage(data);
    } catch {
      // degradação graciosa: badges simplesmente não aparecem
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchUsage();
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getConfigMapUsage(cluster, namespace)
        .then(data => { if (!cancelled) setUsage(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespace]);

  return { usage, loading, refetch: fetchUsage };
}

/**
 * Status de monitoramento Dynatrace por pod, pra o indicador visual na aba Pods (painéis
 * esquerdo e direito). clusterSupported=false cobre tanto "cluster não é AKS" quanto "Dynatrace
 * não configurado" — nesses casos monitoredKeys fica sempre vazio. Poll bem mais espaçado que o
 * resto da tela de pods (refresh de 30s) — status de monitoramento muda devagar.
 */
export function useDynatracePodStatus(cluster?: string, aiEmail?: string) {
  const [clusterSupported, setClusterSupported] = useState(false);
  const [monitoredKeys, setMonitoredKeys] = useState<Set<string>>(new Set());
  // hasLoaded distingue "ainda não sabemos" (não renderizar ícone nenhum) de "sabemos que não
  // é suportado" (renderizar o ícone de proibido) — sem isso, o ícone piscaria "proibido" por um
  // instante em todo cluster suportado, até a 1ª resposta chegar.
  const [hasLoaded, setHasLoaded] = useState(false);
  // checkError: a checagem em si falhou (auth/rede no Dynatrace), distinto de "cluster suportado
  // mas este pod não aparece monitorado" — sem isso as duas causas eram indistinguíveis na UI.
  const [checkError, setCheckError] = useState<string | undefined>(undefined);

  const fetchStatus = async () => {
    if (!cluster) {
      setClusterSupported(false);
      setMonitoredKeys(new Set());
      setHasLoaded(false);
      setCheckError(undefined);
      return;
    }
    try {
      const data = await apiClient.getPodsDynatraceStatus(cluster, aiEmail);
      setClusterSupported(data.cluster_supported);
      setMonitoredKeys(new Set(data.monitored));
      setCheckError(data.check_error);
      setHasLoaded(true);
    } catch {
      // degradação graciosa: ícone simplesmente não aparece (hasLoaded fica false; o polling
      // de 3min tenta de novo — não marcamos hasLoaded aqui pra não confundir "falha real" com
      // "sabemos que não é suportado", que teria ícone próprio)
    }
  };

  useEffect(() => {
    fetchStatus();
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getPodsDynatraceStatus(cluster, aiEmail)
        .then(data => {
          if (cancelled) return;
          setClusterSupported(data.cluster_supported);
          setMonitoredKeys(new Set(data.monitored));
          setCheckError(data.check_error);
          setHasLoaded(true);
        })
        .catch(() => {});
    }, 3 * 60 * 1000);
    return () => { cancelled = true; clearInterval(interval); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, aiEmail]);

  // refetch — permite ao painel (botão "Atualizar" de Pods/Deployments/DaemonSets) forçar uma
  // nova checagem em vez de esperar o poll de 3min ou o cache de 2min do backend expirar. Útil
  // sobretudo depois da correção de paginação: um cluster grande que antes só retornava as
  // primeiras 500 entidades (bug real corrigido) se beneficia de poder re-checar sob demanda.
  return { clusterSupported, monitoredKeys, hasLoaded, checkError, refetch: fetchStatus };
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getSecrets(cluster, namespaces, showSystem, true)
        .then(data => { if (!cancelled) setSecrets(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchSecrets(true);
  const silentRefetch = () => {
    if (!cluster) return;
    apiClient.getSecrets(cluster, namespaces, showSystem, true)
      .then(data => setSecrets(data))
      .catch(() => {});
  };

  return { secrets, loading, error, refetch, silentRefetch };
}

export function useDeployments(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [deployments, setDeployments] = useState<DeploymentSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  const fetchDeployments = async (bypassCache: boolean = false, silent: boolean = false) => {
    if (!cluster) {
      setDeployments([]);
      return;
    }

    try {
      if (!silent) setLoading(true);
      setError(null);
      const data = await apiClient.getDeployments(cluster, namespaces, undefined, showSystem, bypassCache);
      setDeployments(data);
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : "Failed to fetch deployments");
    } finally {
      if (!silent) setLoading(false);
    }
  };

  useEffect(() => {
    fetchDeployments();
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getDeployments(cluster, namespaces, undefined, showSystem, true)
        .then(data => { if (!cancelled) setDeployments(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchDeployments(true);
  const silentRefetch = () => fetchDeployments(true, true);

  return { deployments, loading, error, refetch, silentRefetch };
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getDaemonSets(cluster, namespaces, undefined, showSystem, true)
        .then(data => { if (!cancelled) setDaemonSets(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchDaemonSets(true);
  const silentRefetch = async () => {
    if (!cluster) return;
    apiClient.getDaemonSets(cluster, namespaces, undefined, showSystem, true)
      .then(data => setDaemonSets(data))
      .catch(() => {});
  };

  return { daemonsets, loading, error, refetch, silentRefetch };
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
    if (!cluster) return;
    let cancelled = false;
    const interval = setInterval(() => {
      if (document.visibilityState !== "visible") return;
      apiClient.getStatefulSets(cluster, namespaces, undefined, showSystem, true)
        .then(data => { if (!cancelled) setStatefulSets(data); })
        .catch(() => {});
    }, 60000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchStatefulSets(true);
  const silentRefetch = async () => {
    if (!cluster) return;
    apiClient.getStatefulSets(cluster, namespaces, undefined, showSystem, true)
      .then(data => setStatefulSets(data))
      .catch(() => {});
  };

  return { statefulsets, loading, error, refetch, silentRefetch };
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
  const silentRefetch = () => {
    if (!cluster) return;
    apiClient.getIngresses(cluster, namespaces, undefined, showSystem, true)
      .then(data => setIngresses(data))
      .catch(() => {});
  };

  return { ingresses, loading, error, refetch, silentRefetch };
}

export function useGateways(cluster?: string, namespace?: string, kind: string = "gateway") {
  const [gateways, setGateways] = useState<GatewaySummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchGateways = async (bypassCache: boolean = false) => {
    if (!cluster) {
      setGateways([]);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      const { data, installReason } = await apiClient.getGateways(cluster, namespace, kind, bypassCache);
      setGateways(data);
      if (installReason) {
        setError(installReason);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch gateways");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchGateways();
  }, [cluster, namespace, kind]);

  const refetch = () => fetchGateways(true);
  return { gateways, loading, error, refetch };
}

export function usePods(cluster?: string, namespaces?: string[], showSystem: boolean = false) {
  const [pods, setPods] = useState<PodSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const namespaceKey = namespaces && namespaces.length > 0 ? namespaces.join(",") : "";

  // silent=true não mexe em loading/error — usado no poll periódico de fundo (ex:
  // ContainerMonitorTable) pra não piscar spinner nem esconder a tabela a cada ciclo.
  const fetchPods = async (bypassCache: boolean = false, silent: boolean = false) => {
    if (!cluster) {
      setPods([]);
      return;
    }

    try {
      if (!silent) setLoading(true);
      if (!silent) setError(null);
      const data = await apiClient.getPods(cluster, namespaces, undefined, showSystem, bypassCache);
      setPods(data);
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : "Failed to fetch pods");
    } finally {
      if (!silent) setLoading(false);
    }
  };

  useEffect(() => {
    fetchPods();
  }, [cluster, namespaceKey, showSystem]);

  const refetch = () => fetchPods(true);
  const silentRefetch = () => fetchPods(true, true);

  return { pods, loading, error, refetch, silentRefetch };
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
    onSuccess: (_data, variables) => {
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
    onSuccess: (_data, variables) => {
      // Invalidar cache dos recursos Prometheus
      queryClient.invalidateQueries({ 
        queryKey: ['prometheus', variables.cluster, variables.namespace] 
      });
    },
  });
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
  const silentRefetch = async () => {
    if (!cluster) return;
    apiClient.getServices(cluster, namespaces || [], showSystem)
      .then(data => setServices(data))
      .catch(() => {});
  };

  return { services, loading, error, refetch, silentRefetch };
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
  const silentRefetch = () => {
    if (!cluster) return;
    apiClient.getVPAs(cluster, namespaces, showSystem)
      .then(result => setVPAs(result.data))
      .catch(() => {});
  };

  return { vpas, crdNotInstalled, loading, error, refetch, silentRefetch };
}

// Cache local para tipos de recursos (lista raramente muda)
const apiResourcesCache: Record<string, { data: APIResourceInfo[]; ts: number }> = {};
const API_RESOURCES_TTL = 5 * 60 * 1000; // 5 minutos

export function useAPIResources(cluster?: string) {
  const [resources, setResources] = useState<APIResourceInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!cluster) {
      setResources([]);
      return;
    }
    const cached = apiResourcesCache[cluster];
    if (cached && Date.now() - cached.ts < API_RESOURCES_TTL) {
      setResources(cached.data);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    apiClient
      .getAPIResources(cluster)
      .then((data) => {
        if (cancelled) return;
        apiResourcesCache[cluster] = { data, ts: Date.now() };
        setResources(data);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to fetch API resources");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [cluster]);

  const invalidateCache = () => {
    if (cluster) delete apiResourcesCache[cluster];
  };

  return { resources, loading, error, invalidateCache };
}

export function useGenericResources(
  cluster?: string,
  resourceName?: string,
  group?: string,
  namespace?: string,
) {
  const [items, setItems] = useState<GenericResourceSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchItems = async () => {
    if (!cluster || !resourceName) {
      setItems([]);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.listGenericResources(cluster, resourceName, group || "", namespace);
      setItems(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch resources");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchItems();
  }, [cluster, resourceName, group, namespace]);

  const refetch = () => fetchItems();

  return { items, loading, error, refetch };
}
