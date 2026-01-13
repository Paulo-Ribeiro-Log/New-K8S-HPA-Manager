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
  const [isCompleted, setIsCompleted] = useState(false);

  const eventSourceRef = useRef<EventSource | null>(null);

  // ✅ Usar refs para callbacks - evita reconexões quando callbacks mudam
  const onEventRef = useRef(onEvent);
  const onErrorRef = useRef(onError);
  const onCompleteRef = useRef(onComplete);

  // Atualizar refs quando callbacks mudam (sem triggerar useEffect)
  useEffect(() => {
    onEventRef.current = onEvent;
    onErrorRef.current = onError;
    onCompleteRef.current = onComplete;
  }, [onEvent, onError, onComplete]);

  useEffect(() => {
    // ✅ Não criar conexão se já completou ou se sessionId inválido
    if (!sessionId || isCompleted) {
      return;
    }

    // ⚠️ IMPORTANTE: Não verificar enabled aqui!
    // Queremos que a conexão permaneça ativa mesmo quando tab está inativa
    // Assim os eventos continuam acumulando em background

    // ✅ Evitar conexões duplicadas
    if (eventSourceRef.current && eventSourceRef.current.readyState !== EventSource.CLOSED) {
      console.log('[useHealthCheckProgress] Conexão SSE já existe, ignorando duplicata');
      return;
    }

    // Criar conexão SSE (com token via query param pois EventSource não suporta headers)
    const token = localStorage.getItem("auth_token") || "poc-token-123";
    const url = `/api/v1/healthcheck/progress?session=${sessionId}&token=${token}`;

    console.log('[useHealthCheckProgress] Criando nova conexão SSE:', { sessionId, url });

    const eventSource = new EventSource(url);

    eventSourceRef.current = eventSource;
    setIsConnected(true);

    // Handler de open
    eventSource.onopen = () => {
      console.log('[useHealthCheckProgress] Conexão SSE aberta com sucesso');
    };

    // Handler de mensagens
    eventSource.addEventListener('progress', (e: MessageEvent) => {
      try {
        const event: HealthCheckProgress = JSON.parse(e.data);

        console.log('[useHealthCheckProgress] Evento SSE recebido:', {
          type: event.type,
          phase: event.phase,
          progress: event.progress,
          status: event.status,
          message: event.message,
        });

        // Atualizar estados
        setLastEvent(event);
        setEvents((prev) => [...prev, event]);

        console.log('[useHealthCheckProgress] Estados atualizados - progress:', event.progress);

        // Callback de evento (usando ref)
        if (onEventRef.current) {
          onEventRef.current(event);
        }

        // Se evento é de conclusão ou erro, fechar conexão
        if (event.type === 'complete' || event.type === 'error') {
          console.log('[useHealthCheckProgress] Fechando conexão - tipo:', event.type);
          setIsCompleted(true);  // ✅ Marcar como completo para evitar reconexões
          eventSource.close();
          setIsConnected(false);

          if (event.type === 'complete' && onCompleteRef.current) {
            onCompleteRef.current();
          }
        }
      } catch (error) {
        console.error('[useHealthCheckProgress] Erro ao parsear evento:', error, 'Data:', e.data);
      }
    });

    // Handler de erros
    eventSource.onerror = (error) => {
      console.error('[useHealthCheckProgress] Erro na conexão SSE:', {
        readyState: eventSource.readyState,
        error,
      });
      setIsConnected(false);

      if (onErrorRef.current) {
        onErrorRef.current(error);
      }

      // Só fechar se não estiver já fechado
      if (eventSource.readyState !== EventSource.CLOSED) {
        eventSource.close();
      }
    };

    // Cleanup ao desmontar (só quando componente é destruído, não quando enabled muda)
    return () => {
      console.log('[useHealthCheckProgress] Cleanup: fechando conexão SSE');
      if (eventSource.readyState !== EventSource.CLOSED) {
        eventSource.close();
      }
      setIsConnected(false);
    };
  }, [sessionId, isCompleted]);  // ✅ REMOVIDO enabled - não queremos desconectar ao trocar de tab

  /**
   * Fecha conexão SSE manualmente
   */
  const disconnect = () => {
    console.log('[useHealthCheckProgress] disconnect() chamado');
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      setIsConnected(false);
    }
  };

  /**
   * Limpa histórico de eventos e reseta estado de conclusão
   */
  const clearEvents = () => {
    console.log('[useHealthCheckProgress] clearEvents() chamado');
    setEvents([]);
    setLastEvent(null);
    setIsCompleted(false);  // ✅ Reset flag de conclusão para permitir nova execução
  };

  /**
   * Retorna progresso atual (0-100)
   */
  const getProgress = (): number => {
    const progress = lastEvent ? lastEvent.progress : 0;
    console.log('[useHealthCheckProgress] getProgress() ->', { lastEvent, progress });
    return progress;
  };

  /**
   * Retorna fase atual da operação
   */
  const getCurrentPhase = (): string => {
    if (!lastEvent) return 'Aguardando...';

    const phaseMap: Record<string, string> = {
      init: 'Inicializando',
      deployments: 'Verificando Deployments',
      services: 'Testando Serviços Externos',
      configs: 'Validando ConfigMaps/Secrets',
      summary: 'Resumo',
      complete: 'Concluído',
      error: 'Erro',
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
