import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";
import type { PodSummary } from "@/lib/api/types";

// Mesmo throttle de flush de usePodLogStream.ts/AllPodsLogsModal.tsx — eventos de Watch podem
// chegar em rajada (ex: rollout de um Deployment com várias réplicas trocando de status quase ao
// mesmo tempo); sem agrupar, cada evento viraria 1 re-render do React.
const FLUSH_INTERVAL_MS = 250;

function podKey(namespace: string, name: string): string {
  return `${namespace}/${name}`;
}

/**
 * Watch ao vivo de Pods (eventos empurrados pelo kube-apiserver via Watch/Informer, mesmo
 * mecanismo do k9s) — piloto restrito à aba Pods (PodsPanel.tsx), pedido explícito do usuário
 * depois de comparar como esta app atualiza Pods/Deployments (polling de 5s) com o k9s (Watch).
 *
 * Mesma infraestrutura SSE de usePodLogStream.ts, adaptada: em vez de um array de linhas de log,
 * mantém um Map<namespace/name, PodSummary> atualizado incrementalmente por eventos
 * pod_added/pod_modified/pod_deleted.
 *
 * Fallback: `watchFailed` fica true em qualquer erro/fechamento inesperado da conexão SSE — um
 * Watch nunca deveria fechar "normalmente" por natureza (é contínuo, sem fim), então qualquer
 * `onerror`/close inesperado é tratado como falha real (diferente de outros streams desta app
 * onde um fechamento normal do servidor pode disparar onerror sem ser uma falha de verdade — não
 * é o caso aqui). O componente pai (PodsPanel.tsx) decide o que fazer com `watchFailed` — volta
 * pro polling de 5s já existente, sem nenhuma mensagem de erro pro usuário.
 */
export function usePodsWatch(cluster: string, namespace: string, showSystem: boolean, active: boolean) {
  const [pods, setPods] = useState<PodSummary[]>([]);
  const [connected, setConnected] = useState(false);
  const [watchFailed, setWatchFailed] = useState(false);

  const esRef = useRef<EventSource | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const podsMapRef = useRef<Map<string, PodSummary>>(new Map());
  const bufferedRef = useRef(false);
  const flushTimerRef = useRef<number | null>(null);

  const flush = useCallback(() => {
    flushTimerRef.current = null;
    if (!bufferedRef.current) return;
    bufferedRef.current = false;
    setPods(Array.from(podsMapRef.current.values()));
  }, []);

  const scheduleFlush = useCallback(() => {
    bufferedRef.current = true;
    if (flushTimerRef.current !== null) return;
    flushTimerRef.current = window.setTimeout(flush, FLUSH_INTERVAL_MS);
  }, [flush]);

  // closeStream fecha a conexão SSE atual e avisa o backend pra cancelar o Watch daquela sessão
  // (libera o Informer em vez de deixá-lo pendurado no servidor).
  const closeStream = useCallback(() => {
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    if (sessionIdRef.current) {
      apiClient.cancelPodWatch(sessionIdRef.current).catch(() => { /* best-effort */ });
      sessionIdRef.current = null;
    }
    if (flushTimerRef.current !== null) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    podsMapRef.current = new Map();
    bufferedRef.current = false;
    setConnected(false);
  }, []);

  const openStream = useCallback(async () => {
    closeStream();
    if (!cluster) return;
    setWatchFailed(false);
    try {
      const { session_id } = await apiClient.startPodWatch(cluster, namespace, showSystem);
      sessionIdRef.current = session_id;

      const es = new EventSource(apiClient.getPodWatchStreamURL(session_id));
      esRef.current = es;

      es.onopen = () => setConnected(true);

      es.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          if (event.type === "complete") return; // Watch encerrado (cancelamento explícito)
          if (event.type === "error") {
            setWatchFailed(true);
            return;
          }
          if (event.type !== "pod_added" && event.type !== "pod_modified" && event.type !== "pod_deleted") return;
          const pod = event.result as PodSummary | undefined;
          if (!pod) return;
          const key = podKey(pod.namespace, pod.name);
          if (event.type === "pod_deleted") {
            podsMapRef.current.delete(key);
          } else {
            podsMapRef.current.set(key, pod);
          }
          scheduleFlush();
        } catch {
          /* ignora evento malformado — não derruba a conexão por um evento ruim */
        }
      };

      // Watch é contínuo por natureza — nunca deveria fechar sozinho. Qualquer onerror aqui é
      // tratado como falha real (não um fechamento normal), disparando o fallback pro polling.
      es.onerror = () => {
        setConnected(false);
        setWatchFailed(true);
      };
    } catch {
      setWatchFailed(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, namespace, showSystem, scheduleFlush, closeStream]);

  useEffect(() => {
    if (active && cluster) {
      openStream();
    } else {
      closeStream();
    }
    return closeStream;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, cluster, namespace, showSystem]);

  return { pods, connected, watchFailed };
}
