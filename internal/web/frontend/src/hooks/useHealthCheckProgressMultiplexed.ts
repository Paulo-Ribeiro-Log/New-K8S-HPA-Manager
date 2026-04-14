import { useEffect, useRef, useState } from 'react';
import type { HealthCheckProgress } from '@/types/healthcheck';

interface UseHealthCheckProgressMultiplexedOptions {
  sessionId: string;
  clusters: string[];
  onEvent?: (cluster: string, event: HealthCheckProgress) => void;
  onError?: (error: Event) => void;
  onComplete?: (cluster: string) => void;
  enabled?: boolean;
}

/**
 * Hook para receber eventos SSE multiplexados de TODOS os clusters em uma única conexão
 *
 * Resolve problema de limite de conexões HTTP/1.1 do navegador (~6 por domínio)
 *
 * @param options - Configurações do SSE multiplexado
 * @returns Estado e controles agregados de TODOS os clusters
 *
 * @example
 * ```tsx
 * const { eventsPerCluster, isConnected, disconnect } = useHealthCheckProgressMultiplexed({
 *   sessionId: 'base-uuid',
 *   clusters: ['cluster1', 'cluster2', 'cluster3'],
 *   onComplete: (cluster) => toast.success(`${cluster} concluído!`),
 *   enabled: true
 * });
 *
 * // Acessar eventos de cluster específico
 * const cluster1Events = eventsPerCluster['cluster1'] || [];
 * ```
 */
export function useHealthCheckProgressMultiplexed(options: UseHealthCheckProgressMultiplexedOptions) {
  const { sessionId, clusters, onEvent, onError, onComplete, enabled = true } = options;

  // Estado: eventos agrupados por cluster
  const [eventsPerCluster, setEventsPerCluster] = useState<Record<string, HealthCheckProgress[]>>({});
  const [isConnected, setIsConnected] = useState(false);
  const [completedClusters, setCompletedClusters] = useState<Set<string>>(new Set());

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
    // ✅ Não criar conexão se desabilitado ou sem clusters
    if (!enabled || !sessionId || clusters.length === 0) {
      return;
    }

    // ✅ Evitar conexões duplicadas
    if (eventSourceRef.current && eventSourceRef.current.readyState !== EventSource.CLOSED) {
      console.log('[useHealthCheckProgressMultiplexed] Conexão SSE já existe, ignorando duplicata');
      return;
    }

    // ✅ Inicializar eventos vazios para cada cluster
    const initialEvents: Record<string, HealthCheckProgress[]> = {};
    clusters.forEach(cluster => {
      initialEvents[cluster] = [];
    });
    setEventsPerCluster(initialEvents);
    setCompletedClusters(new Set());

    // Criar conexão SSE multiplexada (com token via query param)
    const token = localStorage.getItem("auth_token");
    const clustersParam = clusters.join(',');
    const url = `/api/v1/healthcheck/progress-multiplex?session=${sessionId}&clusters=${encodeURIComponent(clustersParam)}&token=${token}`;

    console.log('[useHealthCheckProgressMultiplexed] Criando nova conexão SSE multiplexada:', {
      sessionId,
      clusters,
      url
    });

    const eventSource = new EventSource(url);
    eventSourceRef.current = eventSource;
    setIsConnected(true);

    // Handler de open
    eventSource.onopen = () => {
      console.log('[useHealthCheckProgressMultiplexed] Conexão SSE multiplexada aberta com sucesso');
    };

    // Handler de mensagens
    eventSource.addEventListener('progress', (e: MessageEvent) => {
      try {
        const event: HealthCheckProgress = JSON.parse(e.data);

        // ✅ Extrair cluster do evento
        // Backend adiciona "cluster:nome" no campo details (lowercase - JSON tag)
        let cluster = '';
        const details = (event as any).details || event.Details;  // Tenta lowercase primeiro, fallback para uppercase
        if (details && details.startsWith('cluster:')) {
          cluster = details.replace('cluster:', '');
        } else if (event.ID || event.id) {
          // Fallback: extrair do sessionID (formato: baseSessionID-clusterName)
          const id = event.ID || event.id;
          const parts = id.split('-');
          if (parts.length > 1) {
            cluster = parts[parts.length - 1];
          }
        }

        if (!cluster) {
          console.warn('[useHealthCheckProgressMultiplexed] Evento sem cluster identificável:', event);
          return;
        }

        console.log('[useHealthCheckProgressMultiplexed] Evento SSE recebido:', {
          cluster,
          type: event.type,
          phase: event.phase,
          progress: event.progress,
          message: event.message,
        });

        // Atualizar eventos do cluster específico
        setEventsPerCluster((prev) => ({
          ...prev,
          [cluster]: [...(prev[cluster] || []), event],
        }));

        // Callback de evento (usando ref)
        if (onEventRef.current) {
          onEventRef.current(cluster, event);
        }

        // Se evento é de conclusão ou erro, marcar cluster como completo
        if (event.type === 'complete' || event.type === 'error') {
          console.log('[useHealthCheckProgressMultiplexed] Cluster completo:', cluster, 'tipo:', event.type);

          setCompletedClusters((prev) => {
            const newSet = new Set(prev);
            newSet.add(cluster);
            return newSet;
          });

          if (event.type === 'complete' && onCompleteRef.current) {
            onCompleteRef.current(cluster);
          }
        }
      } catch (error) {
        console.error('[useHealthCheckProgressMultiplexed] Erro ao parsear evento:', error, 'Data:', e.data);
      }
    });

    // Handler de erros
    eventSource.onerror = (error) => {
      // ✅ Ignorar erro se todos os clusters já completaram (fechamento normal)
      const allCompleted = completedClusters.size === clusters.length;
      
      if (allCompleted) {
        console.log('[useHealthCheckProgressMultiplexed] Conexão SSE fechada - todos os clusters completaram');
      } else {
        console.error('[useHealthCheckProgressMultiplexed] Erro na conexão SSE:', {
          readyState: eventSource.readyState,
          error,
          completedClusters: completedClusters.size,
          totalClusters: clusters.length,
        });
        
        if (onErrorRef.current) {
          onErrorRef.current(error);
        }
      }
      
      setIsConnected(false);

      // Só fechar se não estiver já fechado
      if (eventSource.readyState !== EventSource.CLOSED) {
        eventSource.close();
      }
    };

    // Cleanup ao desmontar
    return () => {
      console.log('[useHealthCheckProgressMultiplexed] Cleanup: fechando conexão SSE multiplexada');
      if (eventSource.readyState !== EventSource.CLOSED) {
        eventSource.close();
      }
      setIsConnected(false);
    };
  }, [sessionId, clusters, enabled]);

  /**
   * Fecha conexão SSE manualmente
   */
  const disconnect = () => {
    console.log('[useHealthCheckProgressMultiplexed] disconnect() chamado');
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      setIsConnected(false);
    }
  };

  /**
   * Limpa histórico de eventos de todos os clusters
   */
  const clearEvents = () => {
    console.log('[useHealthCheckProgressMultiplexed] clearEvents() chamado');
    const emptyEvents: Record<string, HealthCheckProgress[]> = {};
    clusters.forEach(cluster => {
      emptyEvents[cluster] = [];
    });
    setEventsPerCluster(emptyEvents);
    setCompletedClusters(new Set());
  };

  /**
   * Retorna eventos de um cluster específico
   */
  const getClusterEvents = (cluster: string): HealthCheckProgress[] => {
    return eventsPerCluster[cluster] || [];
  };

  /**
   * Retorna último evento de um cluster específico
   */
  const getLastEvent = (cluster: string): HealthCheckProgress | null => {
    const events = eventsPerCluster[cluster] || [];
    return events.length > 0 ? events[events.length - 1] : null;
  };

  /**
   * Retorna progresso atual de um cluster (0-100)
   */
  const getProgress = (cluster: string): number => {
    const lastEvent = getLastEvent(cluster);
    return lastEvent ? lastEvent.progress : 0;
  };

  /**
   * Retorna fase atual de um cluster
   */
  const getCurrentPhase = (cluster: string): string => {
    const lastEvent = getLastEvent(cluster);
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
   * Retorna mensagem atual de um cluster
   */
  const getCurrentMessage = (cluster: string): string => {
    const lastEvent = getLastEvent(cluster);
    return lastEvent?.message || 'Iniciando health check...';
  };

  /**
   * Verifica se cluster está em progresso
   */
  const isInProgress = (cluster: string): boolean => {
    const lastEvent = getLastEvent(cluster);
    if (!lastEvent) return false;
    return lastEvent.type !== 'complete' && lastEvent.type !== 'error';
  };

  /**
   * Verifica se cluster foi concluído com sucesso
   */
  const isComplete = (cluster: string): boolean => {
    return completedClusters.has(cluster) && getLastEvent(cluster)?.type === 'complete';
  };

  /**
   * Verifica se cluster teve erro
   */
  const hasError = (cluster: string): boolean => {
    return completedClusters.has(cluster) && getLastEvent(cluster)?.type === 'error';
  };

  return {
    // Estados agregados
    eventsPerCluster,
    completedClusters,
    isConnected,

    // Métodos utilitários por cluster
    disconnect,
    clearEvents,
    getClusterEvents,
    getLastEvent,
    getProgress,
    getCurrentPhase,
    getCurrentMessage,
    isInProgress,
    isComplete,
    hasError,
  };
}
