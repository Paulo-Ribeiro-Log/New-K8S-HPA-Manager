import { useEffect, useRef, useState } from 'react';

export interface ProgressEvent {
  id: string;
  type: 'cordon' | 'drain' | 'azure' | 'complete' | 'error' | 'connected';
  phase: 'started' | 'in_progress' | 'completed' | 'failed';
  message: string;
  progress: number; // 0.0 - 1.0
  details?: string;
  timestamp: string;
  node_name?: string;
  pods_count?: number;
  error?: string;
}

interface UseSSEOptions {
  operationId: string;
  onEvent?: (event: ProgressEvent) => void;
  onError?: (error: Event) => void;
  onComplete?: () => void;
  enabled?: boolean;
}

/**
 * Hook para receber eventos SSE (Server-Sent Events) de progresso
 *
 * @param options - Configurações do SSE
 * @returns Estado e controles do SSE
 *
 * @example
 * ```tsx
 * const { events, isConnected, lastEvent } = useSSE({
 *   operationId: 'nodepool-cluster-name-123456',
 *   onEvent: (event) => console.log('Progresso:', event.progress),
 *   onComplete: () => toast.success('Operação concluída!'),
 *   enabled: true
 * });
 * ```
 */
export function useSSE(options: UseSSEOptions) {
  const { operationId, onEvent, onError, onComplete, enabled = true } = options;

  const [events, setEvents] = useState<ProgressEvent[]>([]);
  const [isConnected, setIsConnected] = useState(false);
  const [lastEvent, setLastEvent] = useState<ProgressEvent | null>(null);

  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!enabled || !operationId) {
      return;
    }

    // Criar conexão SSE
    const url = `/api/v1/nodepools/progress/${operationId}`;
    const eventSource = new EventSource(url);

    eventSourceRef.current = eventSource;
    setIsConnected(true);

    // Handler de mensagens
    eventSource.onmessage = (e) => {
      try {
        const event: ProgressEvent = JSON.parse(e.data);

        // Atualizar estados
        setLastEvent(event);
        setEvents((prev) => [...prev, event]);

        // Callback de evento
        if (onEvent) {
          onEvent(event);
        }

        // Se evento é de conclusão ou erro, fechar conexão
        if (event.type === 'complete' || event.type === 'error') {
          eventSource.close();
          setIsConnected(false);

          if (event.type === 'complete' && onComplete) {
            onComplete();
          }
        }
      } catch (error) {
        console.error('[useSSE] Erro ao parsear evento:', error);
      }
    };

    // Handler de erros
    eventSource.onerror = (error) => {
      console.error('[useSSE] Erro na conexão SSE:', error);
      setIsConnected(false);

      if (onError) {
        onError(error);
      }

      eventSource.close();
    };

    // Cleanup ao desmontar
    return () => {
      if (eventSource.readyState !== EventSource.CLOSED) {
        eventSource.close();
      }
      setIsConnected(false);
    };
  }, [operationId, enabled, onEvent, onError, onComplete]);

  /**
   * Fecha conexão SSE manualmente
   */
  const disconnect = () => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      setIsConnected(false);
    }
  };

  /**
   * Limpa histórico de eventos
   */
  const clearEvents = () => {
    setEvents([]);
    setLastEvent(null);
  };

  /**
   * Retorna progresso atual (0-100)
   */
  const getProgress = (): number => {
    return lastEvent ? Math.round(lastEvent.progress * 100) : 0;
  };

  /**
   * Retorna fase atual da operação
   */
  const getCurrentPhase = (): string => {
    if (!lastEvent) return 'Aguardando...';

    const phaseMap: Record<string, string> = {
      cordon: 'CORDON',
      drain: 'DRAIN',
      azure: 'Azure CLI',
      complete: 'Concluído',
      error: 'Erro',
      connected: 'Conectado',
    };

    return phaseMap[lastEvent.type] || lastEvent.type;
  };

  /**
   * Verifica se operação está em progresso
   */
  const isInProgress = (): boolean => {
    if (!lastEvent) return false;
    return lastEvent.type !== 'complete' && lastEvent.type !== 'error';
  };

  /**
   * Verifica se operação foi concluída com sucesso
   */
  const isComplete = (): boolean => {
    return lastEvent?.type === 'complete';
  };

  /**
   * Verifica se operação teve erro
   */
  const hasError = (): boolean => {
    return lastEvent?.type === 'error';
  };

  return {
    // Estados
    events,
    lastEvent,
    isConnected,

    // Métodos utilitários
    disconnect,
    clearEvents,
    getProgress,
    getCurrentPhase,
    isInProgress,
    isComplete,
    hasError,
  };
}
