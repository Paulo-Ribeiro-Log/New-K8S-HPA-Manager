import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";
import type { HPA } from "@/lib/api/types";

// Mesma estrutura de usePodsWatch.ts/useDeploymentsWatch.ts — ver comentário lá pra contexto
// completo do mecanismo.
const FLUSH_INTERVAL_MS = 250;

function hpaKey(namespace: string, name: string): string {
  return `${namespace}/${name}`;
}

// Campos "quentes" que o backend entrega via Watch (hpas_watch.go usa
// kubeclient.ConvertHPAToModel — a conversão BARATA, sem o Get() extra no Deployment associado).
// `deployment_name`/`image_version`/`*_request`/`*_limit` NUNCA vêm de um evento de Watch (só do
// enriquecimento que só o polling de 30s faz via EnrichHPAWithDeploymentResources), e por isso
// NUNCA devem ser tocados aqui.
export function pickHPAHotFields(hpa: HPA): Partial<HPA> {
  return {
    min_replicas: hpa.min_replicas,
    max_replicas: hpa.max_replicas,
    current_replicas: hpa.current_replicas,
    target_cpu: hpa.target_cpu,
    target_memory: hpa.target_memory,
  };
}

/**
 * Watch ao vivo de HPAs — mesmo mecanismo/estrutura de usePodsWatch.ts, promovido a pedido
 * explícito do usuário depois do piloto de Pods validado. `items` traz só os campos "quentes"
 * (ver pickHPAHotFields) — quem consome precisa fazer o merge com o que já tem do polling.
 */
export function useHPAsWatch(cluster: string, namespace: string, active: boolean) {
  const [items, setItems] = useState<HPA[]>([]);
  const [connected, setConnected] = useState(false);
  const [watchFailed, setWatchFailed] = useState(false);

  const esRef = useRef<EventSource | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const itemsMapRef = useRef<Map<string, HPA>>(new Map());
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
      apiClient.cancelHPAWatch(sessionIdRef.current).catch(() => { /* best-effort */ });
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
      const { session_id } = await apiClient.startHPAWatch(cluster, namespace);
      sessionIdRef.current = session_id;

      const es = new EventSource(apiClient.getHPAWatchStreamURL(session_id));
      esRef.current = es;

      es.onopen = () => setConnected(true);

      es.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          if (event.type === "complete") return;
          if (event.type === "error") { setWatchFailed(true); return; }
          if (event.type !== "hpa_added" && event.type !== "hpa_modified" && event.type !== "hpa_deleted") return;
          const hpa = event.result as HPA | undefined;
          if (!hpa) return;
          const key = hpaKey(hpa.namespace, hpa.name);
          if (event.type === "hpa_deleted") {
            itemsMapRef.current.delete(key);
          } else {
            itemsMapRef.current.set(key, hpa);
          }
          scheduleFlush();
        } catch {
          /* ignora evento malformado */
        }
      };

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
