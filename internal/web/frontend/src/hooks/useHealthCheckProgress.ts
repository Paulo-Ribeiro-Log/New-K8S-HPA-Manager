import { useEffect, useRef, useState } from 'react';
import type { HealthCheckProgress } from '@/types/healthcheck';

interface UseHealthCheckProgressOptions {
  sessionId: string;
  onEvent?: (event: HealthCheckProgress) => void;
  onError?: (error: Event) => void;
  onComplete?: () => void;
  enabled?: boolean;
}

/**
 * Hook para receber eventos SSE de progresso de health check
 *
 * @param options - Configurações do SSE
 * @returns Estado e controles do SSE
 *
 * @example
 * ```tsx
 * const { events, isConnected, progress } = useHealthCheckProgress({
 *   sessionId: 'uuid-session-id',
 *   onEvent: (event) => console.log('Progresso:', event.progress),
 *   onComplete: () => toast.success('Health check concluído!'),
 *   enabled: true
 * });
 * ```
 */
export function useHealthCheckProgress(options: UseHealthCheckProgressOptions) {
  const { sessionId, onEvent, onError, onComplete, enabled = true } = options;

  const [events, setEvents] = useState<HealthCheckProgress[]>([]);
  const [isConnected, setIsConnected] = useState(false);
  const [lastEvent, setLastEvent] = useState<HealthCheckProgress | null>(null);

  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!enabled || !sessionId) {
      return;
    }

    // Criar conexão SSE
    const url = `/api/v1/healthcheck/progress?session=${sessionId}`;
    const eventSource = new EventSource(url);

    eventSourceRef.current = eventSource;
    setIsConnected(true);

    // Handler de mensagens
    eventSource.addEventListener('progress', (e: MessageEvent) => {
      try {
        const event: HealthCheckProgress = JSON.parse(e.data);

        // Atualizar estados
        setLastEvent(event);
        setEvents((prev) => [...prev, event]);

        // Callback de evento
        if (onEvent) {
          onEvent(event);
        }

        // Se evento é de conclusão ou erro, fechar conexão
        if (event.phase === 'complete' || event.phase === 'error') {
          eventSource.close();
          setIsConnected(false);

          if (event.phase === 'complete' && onComplete) {
            onComplete();
          }
        }
      } catch (error) {
        console.error('[useHealthCheckProgress] Erro ao parsear evento:', error);
      }
    });

    // Handler de erros
    eventSource.onerror = (error) => {
      console.error('[useHealthCheckProgress] Erro na conexão SSE:', error);
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
  }, [sessionId, enabled, onEvent, onError, onComplete]);

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
    return lastEvent ? lastEvent.progress : 0;
  };

  /**
   * Retorna fase atual da operação
   */
  const getCurrentPhase = (): string => {
    if (!lastEvent) return 'Aguardando...';

    const phaseMap: Record<string, string> = {
      deployments: 'Verificando Deployments',
      services: 'Testando Serviços Externos',
      configs: 'Validando ConfigMaps/Secrets',
      complete: 'Concluído',
      error: 'Erro',
    };

    return phaseMap[lastEvent.phase] || lastEvent.phase;
  };

  /**
   * Verifica se operação está em progresso
   */
  const isInProgress = (): boolean => {
    if (!lastEvent) return false;
    return lastEvent.phase !== 'complete' && lastEvent.phase !== 'error';
  };

  /**
   * Verifica se operação foi concluída com sucesso
   */
  const isComplete = (): boolean => {
    return lastEvent?.phase === 'complete';
  };

  /**
   * Verifica se operação teve erro
   */
  const hasError = (): boolean => {
    return lastEvent?.phase === 'error';
  };

  /**
   * Retorna mensagem atual
   */
  const getCurrentMessage = (): string => {
    return lastEvent?.message || 'Iniciando health check...';
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
    getCurrentMessage,
    isInProgress,
    isComplete,
    hasError,
  };
}
