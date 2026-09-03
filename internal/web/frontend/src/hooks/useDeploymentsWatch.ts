import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";
import type { DeploymentSummary } from "@/lib/api/types";

// Mesma estrutura de usePodsWatch.ts — ver comentário lá pra contexto completo do mecanismo
// (Reflector/SSE, fallback pro polling em caso de erro).
const FLUSH_INTERVAL_MS = 250;

function depKey(namespace: string, name: string): string {
  return `${namespace}/${name}`;
}

// Campos "quentes" que o backend entrega via Watch (deployments_watch.go usa
// kubeclient.BuildDeploymentSummary — a conversão BARATA, sem cruzamento com Pods). Extraída aqui
// como fonte única de verdade sobre o que é seguro sobrescrever — `unhealthyPodCount`/
// `podIssueReason`/`serviceClusterIPs`/`serviceExternalIPs` NUNCA vêm de um evento de Watch (só do
// enriquecimento que só o polling de 60s faz), e por isso NUNCA devem ser tocados aqui.
export function pickDeploymentHotFields(dep: DeploymentSummary): Partial<DeploymentSummary> {
  return {
    labels: dep.labels,
    replicas: dep.replicas,
    readyReplicas: dep.readyReplicas,
    availableReplicas: dep.availableReplicas,
    updatedReplicas: dep.updatedReplicas,
    unavailableReplicas: dep.unavailableReplicas,
    currentReplicas: dep.currentReplicas,
    statusCondition: dep.statusCondition,
    statusReason: dep.statusReason,
    statusMessage: dep.statusMessage,
    resourceVersion: dep.resourceVersion,
    updatedAt: dep.updatedAt,
    // isCompanyApp é derivado só do próprio objeto Deployment (imagem/labels/annotations) — zero
    // custo extra, igual aos demais campos quentes acima, então é seguro sobrescrever via Watch.
    isCompanyApp: dep.isCompanyApp,
  };
}

/**
 * Watch ao vivo de Deployments — mesmo mecanismo/estrutura de usePodsWatch.ts, promovido a
 * pedido explícito do usuário depois do piloto de Pods validado. `items` traz só os campos
 * "quentes" (ver pickDeploymentHotFields) — quem consome precisa fazer o merge com o que já tem
 * do polling (nunca substituir o objeto inteiro, senão perde UnhealthyPodCount/PodIssueReason).
 */
export function useDeploymentsWatch(cluster: string, namespace: string, active: boolean) {
  const [items, setItems] = useState<DeploymentSummary[]>([]);
  const [connected, setConnected] = useState(false);
  const [watchFailed, setWatchFailed] = useState(false);

  const esRef = useRef<EventSource | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const itemsMapRef = useRef<Map<string, DeploymentSummary>>(new Map());
  const bufferedRef = useRef(false);
  const flushTimerRef = useRef<number | null>(null);

  const flush = useCallback(() => {
    flushTimerRef.current = null;
    if (!bufferedRef.current) return;
    bufferedRef.current = false;
    setItems(Array.from(itemsMapRef.current.values()));
  }, []);

  const scheduleFlush = useCallback(() => {
    bufferedRef.current = true;
    if (flushTimerRef.current !== null) return;
    flushTimerRef.current = window.setTimeout(flush, FLUSH_INTERVAL_MS);
  }, [flush]);

  const closeStream = useCallback(() => {
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    if (sessionIdRef.current) {
      apiClient.cancelDeploymentWatch(sessionIdRef.current).catch(() => { /* best-effort */ });
      sessionIdRef.current = null;
    }
    if (flushTimerRef.current !== null) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    itemsMapRef.current = new Map();
    bufferedRef.current = false;
    setConnected(false);
  }, []);

  const openStream = useCallback(async () => {
    closeStream();
    if (!cluster) return;
    setWatchFailed(false);
    try {
      const { session_id } = await apiClient.startDeploymentWatch(cluster, namespace);
      sessionIdRef.current = session_id;

      const es = new EventSource(apiClient.getDeploymentWatchStreamURL(session_id));
      esRef.current = es;

      es.onopen = () => setConnected(true);

      es.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          if (event.type === "complete") return;
          if (event.type === "error") { setWatchFailed(true); return; }
          if (event.type !== "deployment_added" && event.type !== "deployment_modified" && event.type !== "deployment_deleted") return;
          const dep = event.result as DeploymentSummary | undefined;
          if (!dep) return;
          const key = depKey(dep.namespace, dep.name);
          if (event.type === "deployment_deleted") {
            itemsMapRef.current.delete(key);
          } else {
            itemsMapRef.current.set(key, dep);
          }
          scheduleFlush();
        } catch {
          /* ignora evento malformado */
        }
      };

      // Watch é contínuo por natureza — qualquer onerror é tratado como falha real.
      es.onerror = () => {
        setConnected(false);
        setWatchFailed(true);
      };
    } catch {
      setWatchFailed(true);
    }
  }, [cluster, namespace, scheduleFlush, closeStream]);

  useEffect(() => {
    if (active && cluster) {
      openStream();
    } else {
      closeStream();
    }
    return closeStream;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, cluster, namespace]);

  return { items, connected, watchFailed };
}
