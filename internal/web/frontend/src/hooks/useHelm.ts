import { useEffect, useRef, useCallback } from 'react';
import { useHelmStore } from '../store/helmStore.tsx';
import type {
  ListReleasesParams,
  GetReleaseParams,
  HistoryParams,
  HelmActionRequest,
  HelmListResponse,
  HelmDetailResponse,
  HelmHistoryResponse,
  HelmActionApiResponse,
  OperationEvent,
} from '../types/helm';

const API_BASE = '/api/v1/helm';

// Helper to get auth headers
const getAuthHeaders = () => {
  const token = localStorage.getItem('auth_token') || localStorage.getItem("auth_token");
  return {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  };
};

/**
 * Hook para listar releases Helm
 */
export function useHelmReleases(params: ListReleasesParams) {
  const {
    setReleases,
    setReleasesLoading,
    setReleasesError,
    filters,
  } = useHelmStore();

  const fetchReleases = useCallback(async () => {
    if (!params.cluster) {
      return;
    }

    setReleasesLoading(true);
    setReleasesError(null);

    try {
      const queryParams = new URLSearchParams({
        cluster: params.cluster,
      });

      if (filters.namespace) {
        queryParams.set('namespace', filters.namespace);
      }

      if (filters.search) {
        queryParams.set('search', filters.search);
      }

      if (filters.statuses.length > 0) {
        queryParams.set('statuses', filters.statuses.join(','));
      }

      if (params.limit) {
        queryParams.set('limit', params.limit.toString());
      }

      const response = await fetch(`${API_BASE}/releases?${queryParams}`, {
        headers: getAuthHeaders(),
      });
      const data: HelmListResponse = await response.json();

      if (!response.ok || !data.success) {
        throw new Error(data.error?.message || 'Failed to fetch releases');
      }

      setReleases(data.data.releases || []);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error';
      setReleasesError(message);
      console.error('Error fetching releases:', error);
    } finally {
      setReleasesLoading(false);
    }
  }, [params.cluster, params.limit, filters, setReleases, setReleasesLoading, setReleasesError]);

  useEffect(() => {
    fetchReleases();
  }, [fetchReleases]);

  return { refetch: fetchReleases };
}

/**
 * Hook para obter detalhes de um release
 */
export function useHelmRelease(params: GetReleaseParams | null) {
  const {
    setReleaseDetail,
    setReleaseDetailLoading,
    setReleaseDetailError,
  } = useHelmStore();

  const fetchRelease = useCallback(async () => {
    if (!params?.cluster || !params?.release) {
      setReleaseDetail(null);
      return;
    }

    // DEBUG: Log dos parâmetros da requisição
    console.log('[useHelmRelease] Fetching release:', {
      cluster: params.cluster,
      release: params.release,
      namespace: params.namespace,
      hasNamespace: !!params.namespace,
    });

    setReleaseDetailLoading(true);
    setReleaseDetailError(null);

    try {
      const queryParams = new URLSearchParams({
        cluster: params.cluster,
      });

      if (params.namespace) {
        queryParams.set('namespace', params.namespace);
      }

      const finalUrl = `${API_BASE}/releases/${params.release}?${queryParams}`;
      console.log('[useHelmRelease] Request URL:', finalUrl);

      const response = await fetch(finalUrl, {
        headers: getAuthHeaders(),
      });
      const data: HelmDetailResponse = await response.json();

      if (!response.ok || !data.success) {
        // Release not found - clear selection instead of showing error
        if (response.status === 500 && data.error?.message?.includes('release: not found')) {
          console.warn(`Release ${params.release} not found, clearing selection`);
          setReleaseDetail(null);
          setReleaseDetailError('Release not found. It may have been uninstalled.');
          return;
        }
        throw new Error(data.error?.message || 'Failed to fetch release details');
      }

      // DEBUG: Log dos dados recebidos
      console.log('[useHelmRelease] Response data:', {
        success: data.success,
        releaseName: data.data?.name,
        valuesRawLength: data.data?.valuesRaw?.length || 0,
        valuesRenderedLength: data.data?.valuesRendered?.length || 0,
        valuesRawPreview: data.data?.valuesRaw?.substring(0, 100),
      });

      setReleaseDetail(data.data);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error';
      setReleaseDetailError(message);
      console.error('Error fetching release:', error);
    } finally {
      setReleaseDetailLoading(false);
    }
  }, [params?.cluster, params?.release, params?.namespace, setReleaseDetail, setReleaseDetailLoading, setReleaseDetailError]);

  useEffect(() => {
    fetchRelease();
  }, [fetchRelease]);

  return { refetch: fetchRelease };
}

/**
 * Hook para obter histórico de revisões
 */
export function useHelmHistory(params: HistoryParams | null) {
  const {
    setRevisions,
    setRevisionsLoading,
    setRevisionsError,
  } = useHelmStore();

  const fetchHistory = useCallback(async () => {
    if (!params?.cluster || !params?.release) {
      setRevisions([]);
      return;
    }

    setRevisionsLoading(true);
    setRevisionsError(null);

    try {
      const queryParams = new URLSearchParams({
        cluster: params.cluster,
      });

      if (params.namespace) {
        queryParams.set('namespace', params.namespace);
      }

      if (params.limit) {
        queryParams.set('limit', params.limit.toString());
      }

      const response = await fetch(`${API_BASE}/releases/${params.release}/history?${queryParams}`, {
        headers: getAuthHeaders(),
      });
      const data: HelmHistoryResponse = await response.json();

      if (!response.ok || !data.success) {
        // Release not found - just clear revisions silently
        if (response.status === 500 && data.error?.message?.includes('release: not found')) {
          console.warn(`History for ${params.release} not found, clearing revisions`);
          setRevisions([]);
          return;
        }
        throw new Error(data.error?.message || 'Failed to fetch release history');
      }

      setRevisions(data.data.revisions || []);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error';
      setRevisionsError(message);
      console.error('Error fetching history:', error);
    } finally {
      setRevisionsLoading(false);
    }
  }, [params?.cluster, params?.release, params?.namespace, params?.limit, setRevisions, setRevisionsLoading, setRevisionsError]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  return { refetch: fetchHistory };
}

/**
 * Hook para obter values de uma revisão específica
 */
export function useFetchRevisionValues() {
  const fetchRevisionValues = useCallback(
    async (cluster: string, release: string, namespace: string, revision: number) => {
      const queryParams = new URLSearchParams({ cluster });
      
      if (namespace) {
        queryParams.set('namespace', namespace);
      }

      const response = await fetch(
        `${API_BASE}/releases/${release}/revisions/${revision}/values?${queryParams}`,
        { headers: getAuthHeaders() }
      );

      const data = await response.json();

      if (!response.ok || !data.success) {
        throw new Error(data.error?.message || 'Failed to fetch revision values');
      }

      return {
        revision: data.data.revision,
        valuesRaw: data.data.valuesRaw || '',
        valuesRendered: data.data.valuesRendered || '',
      };
    },
    []
  );

  return { fetchRevisionValues };
}

/**
 * Hook para executar operações Helm com streaming de logs
 */
export function useHelmOperation() {
  const {
    addOperation,
    updateOperation,
    setActiveOperationId,
  } = useHelmStore();

  const eventSourceRef = useRef<EventSource | null>(null);

  const executeOperation = useCallback(
    async (cluster: string, request: HelmActionRequest) => {
      const queryParams = new URLSearchParams({ cluster });

      // CRÍTICO: Adicionar namespace ao queryParams para todas as operações
      // Sem isso, helm uninstall procura no namespace default e falha
      if (request.namespace) {
        queryParams.set('namespace', request.namespace);
      }

      let endpoint = `${API_BASE}/releases`;
      let method = 'POST';

      switch (request.action) {
        case 'install':
          endpoint = `${API_BASE}/releases?${queryParams}`;
          method = 'POST';
          break;
        case 'upgrade':
          endpoint = `${API_BASE}/releases/${request.releaseName}?${queryParams}`;
          method = 'PUT';
          break;
        case 'rollback':
          endpoint = `${API_BASE}/releases/${request.releaseName}/rollback?${queryParams}`;
          method = 'POST';
          break;
        case 'uninstall':
          endpoint = `${API_BASE}/releases/${request.releaseName}?${queryParams}`;
          method = 'DELETE';
          break;
      }

      try {
        const response = await fetch(endpoint, {
          method,
          headers: getAuthHeaders(),
          body: request.action !== 'uninstall' ? JSON.stringify(request) : undefined,
        });

        const data: HelmActionApiResponse = await response.json();

        if (!response.ok || !data.success) {
          throw new Error(data.error?.message || `Failed to execute ${request.action}`);
        }

        const operationId = data.data.operationId;
        addOperation(operationId, data.data);
        setActiveOperationId(operationId);

        return operationId;
      } catch (error) {
        console.error('Error executing operation:', error);
        throw error;
      }
    },
    [addOperation, setActiveOperationId]
  );

  const streamOperation = useCallback(
    (operationId: string, onEvent?: (event: OperationEvent) => void) => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }

      const eventSource = new EventSource(`${API_BASE}/operations/${operationId}/stream`);
      eventSourceRef.current = eventSource;

      eventSource.addEventListener('helm-operation', (e) => {
        try {
          const event: OperationEvent = JSON.parse(e.data);
          
          if (onEvent) {
            onEvent(event);
          }

          // Update operation in store
          updateOperation(operationId, {
            operationId: event.operationId,
            status: event.phase as any,
            startedAt: '',
            logs: [event.message],
          });
        } catch (error) {
          console.error('Error parsing SSE event:', error);
        }
      });

      eventSource.onerror = (error) => {
        console.error('SSE connection error:', error);
        eventSource.close();
        eventSourceRef.current = null;
      };

      return () => {
        eventSource.close();
        eventSourceRef.current = null;
      };
    },
    [updateOperation]
  );

  useEffect(() => {
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
    };
  }, []);

  return {
    executeOperation,
    streamOperation,
  };
}
