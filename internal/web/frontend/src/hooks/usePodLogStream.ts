import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";

// Mesmos valores de AllPodsLogsModal.tsx — ring buffer (sem "cache cheio") + throttle de flush
// (linhas chegam via SSE potencialmente muitas por segundo; sem agrupar, cada uma viraria um
// re-render do React).
const MAX_LOG_LINES = 2000;
const FLUSH_INTERVAL_MS = 150;

/** Remove o prefixo RFC3339Nano que o backend inclui em todo streaming (PodLogOptions.Timestamps
 * = true, hardcoded em pods_logs_stream.go — necessário lá pro AllPodsLogsModal intercalar várias
 * origens por tempo real). Pro caso single-pod a ordem já vem correta pela própria natureza do
 * stream, então o timestamp é só ruído visual — removido aqui. */
export function stripTimestampPrefix(raw: string): string {
  const spaceIdx = raw.indexOf(" ");
  if (spaceIdx === -1) return raw;
  if (Number.isNaN(Date.parse(raw.slice(0, spaceIdx)))) return raw;
  return raw.slice(spaceIdx + 1);
}

export interface PodLogStreamTarget {
  cluster: string;
  namespace: string;
  name: string;
  container?: string;
}

/**
 * Streaming ao vivo (Follow=true, mesmo `kubectl logs -f`) dos logs de UM pod — mesma
 * infraestrutura SSE já usada por AllPodsLogsModal (vários pods), adaptada pro caso single-pod
 * dos visualizadores de log (PodLogsPanel.tsx / aba "Logs" do PodQuickViewModal.tsx).
 *
 * Bug real corrigido: os dois consumidores single-pod faziam polling (getPodLogs a cada 2-3s,
 * REST simples sem Follow) e substituíam o buffer inteiro a cada resposta — o mesmo padrão que
 * já tinha sido identificado e corrigido no AllPodsLogsModal (ver comentário lá: "carrega, trava,
 * repete"), mas nunca replicado aqui. Cada poll pagava uma nova busca completa das últimas N
 * linhas no K8s (proxy até o kubelet lendo o arquivo de log do zero) — lento — e uma resposta
 * momentaneamente vazia/diferente fazia a tela "piscar"/resetar — sintoma de "o refresh limpa o
 * log". Com streaming real (mesmo mecanismo do k9s), a conexão fica aberta e cada linha nova é
 * apensada, nunca um replace do buffer inteiro.
 */
export function usePodLogStream(target: PodLogStreamTarget | null, tailLines: number, active: boolean) {
  const [lines, setLines] = useState<string[]>([]);
  const [connecting, setConnecting] = useState(false);
  const esRef = useRef<EventSource | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const bufferRef = useRef<string[]>([]);
  const flushTimerRef = useRef<number | null>(null);

  const flushBuffer = useCallback(() => {
    flushTimerRef.current = null;
    if (bufferRef.current.length === 0) return;
    const incoming = bufferRef.current;
    bufferRef.current = [];
    setLines((prev) => {
      const next = prev.length > 0 ? prev.concat(incoming) : incoming;
      return next.length > MAX_LOG_LINES ? next.slice(next.length - MAX_LOG_LINES) : next;
    });
  }, []);

  const scheduleFlush = useCallback(() => {
    if (flushTimerRef.current !== null) return;
    flushTimerRef.current = window.setTimeout(flushBuffer, FLUSH_INTERVAL_MS);
  }, [flushBuffer]);

  // closeStream fecha a conexão SSE atual e avisa o backend pra cancelar a goroutine de streaming
  // daquela sessão (libera o Follow=true em vez de deixá-lo pendurado no servidor).
  const closeStream = useCallback(() => {
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    if (sessionIdRef.current) {
      apiClient.cancelPodLogsStreamAll(sessionIdRef.current).catch(() => { /* best-effort */ });
      sessionIdRef.current = null;
    }
    if (flushTimerRef.current !== null) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    bufferRef.current = [];
  }, []);

  const openStream = useCallback(async () => {
    closeStream();
    if (!target) {
      setLines([]);
      return;
    }
    setLines([]);
    setConnecting(true);
    try {
      const { session_id } = await apiClient.startPodLogsStreamAll(
        target.cluster,
        [{ namespace: target.namespace, name: target.name, container: target.container }],
        tailLines
      );
      sessionIdRef.current = session_id;

      const es = new EventSource(apiClient.getPodLogsStreamAllURL(session_id));
      esRef.current = es;

      es.onopen = () => setConnecting(false);

      es.onmessage = (e) => {
        try {
          const event = JSON.parse(e.data);
          if (event.type === "complete") return; // goroutine terminou (ex: cancelada)
          const result = event.result;
          if (!result) return;
          const content = result.error
            ? `[erro ao ler logs] ${result.error}`
            : stripTimestampPrefix(result.line ?? "");
          bufferRef.current.push(content);
          scheduleFlush();
        } catch {
          /* ignora evento malformado — não derruba a conexão por uma linha ruim */
        }
      };

      es.onerror = () => setConnecting(false);
    } catch {
      setConnecting(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target?.cluster, target?.namespace, target?.name, target?.container, tailLines, scheduleFlush, closeStream]);

  useEffect(() => {
    if (active && target) {
      openStream();
    } else {
      closeStream();
    }
    return closeStream;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, target?.cluster, target?.namespace, target?.name, target?.container, tailLines]);

  return { lines, loading: connecting, refetch: openStream };
}
