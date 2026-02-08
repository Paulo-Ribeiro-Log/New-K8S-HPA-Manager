import { useState, useCallback } from 'react';
import { apiClient } from '../lib/api/client';
import type {
  NexusConfig,
  ValuesFileRequest,
  ValuesFileResponse,
  CompareValuesRequest,
  CompareValuesResponse,
  TestConnectionResponse,
  NexusStatus,
  BrowseResponse,
  BrowseItem,
} from '../types/nexus';

const API_BASE = '/nexus';

// Hook para gerenciar configuração do Nexus
export const useNexusConfig = () => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const testConnection = useCallback(async (config: NexusConfig): Promise<TestConnectionResponse> => {
    setLoading(true);
    setError(null);
    try {
      const response = await apiClient.post<TestConnectionResponse>(`${API_BASE}/test`, config);
      return response;
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to test connection';
      setError(errorMsg);
      throw err;
    } finally {
      setLoading(false);
    }
  }, []);

  const saveConfig = useCallback(async (config: NexusConfig): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      await apiClient.post(`${API_BASE}/config`, config);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to save configuration';
      setError(errorMsg);
      throw err;
    } finally {
      setLoading(false);
    }
  }, []);

  const loadConfig = useCallback(async (): Promise<NexusConfig | null> => {
    setLoading(true);
    setError(null);
    try {
      const response = await apiClient.get<NexusConfig>(`${API_BASE}/config`);
      return response;
    } catch (err: any) {
      if (err.message?.includes('404') || err.message?.includes('not found')) {
        return null; // No config found
      }
      const errorMsg = err instanceof Error ? err.message : 'Failed to load configuration';
      setError(errorMsg);
      throw err;
    } finally {
      setLoading(false);
    }
  }, []);

  const deleteConfig = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      await apiClient.delete(`${API_BASE}/config`);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to delete configuration';
      setError(errorMsg);
      throw err;
    } finally {
      setLoading(false);
    }
  }, []);

  const checkStatus = useCallback(async (): Promise<NexusStatus> => {
    try {
      const response = await apiClient.get<NexusStatus>(`${API_BASE}/status`);
      return response;
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to check status';
      setError(errorMsg);
      throw err;
    }
  }, []);

  const browseRepository = useCallback(async (path: string = '', query: string = ''): Promise<BrowseItem[]> => {
    try {
      let url = `${API_BASE}/browse?path=${encodeURIComponent(path)}`;
      if (query) url += `&q=${encodeURIComponent(query)}`;
      const response = await apiClient.get<BrowseResponse>(url);
      return response.items || [];
    } catch (err) {
      console.error('[useNexus] Browse failed:', err);
      return [];
    }
  }, []);

  return {
    testConnection,
    saveConfig,
    loadConfig,
    deleteConfig,
    checkStatus,
    browseRepository,
    loading,
    error,
  };
};

// Hook para operações com values do Nexus
export const useNexusValues = () => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const downloadValues = useCallback(async (request: ValuesFileRequest): Promise<ValuesFileResponse> => {
    setLoading(true);
    setError(null);
    console.log('[useNexus] Downloading values:', request);
    try {
      const response = await apiClient.post<ValuesFileResponse>(`${API_BASE}/values/download`, request);
      console.log('[useNexus] Download response:', response);
      if (response.error) {
        console.error('[useNexus] Response contains error:', response.error);
        // Retorna a resposta com erro para o componente tratar
        return response;
      }
      console.log('[useNexus] Download successful:', response.size, 'bytes');
      return response;
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to download values';
      console.error('[useNexus] Download failed:', errorMsg);
      setError(errorMsg);
      // Retorna um objeto de erro em vez de lançar exceção
      return { content: '', filePath: '', fullUrl: '', size: 0, error: errorMsg };
    } finally {
      setLoading(false);
    }
  }, []);

  const compareValues = useCallback(async (request: CompareValuesRequest): Promise<CompareValuesResponse> => {
    setLoading(true);
    setError(null);
    console.log('[useNexus] Comparing values:', request);
    console.log('[useNexus] Request file1:', JSON.stringify(request.file1));
    console.log('[useNexus] Request file2:', JSON.stringify(request.file2));
    try {
      const response = await apiClient.post<CompareValuesResponse>(`${API_BASE}/values/compare`, request);
      console.log('[useNexus] Compare response received');
      console.log('[useNexus] Response file1:', response.file1 ? `content=${response.file1.content?.length || 0} bytes, error=${response.file1.error || 'none'}` : 'undefined');
      console.log('[useNexus] Response file2:', response.file2 ? `content=${response.file2.content?.length || 0} bytes, error=${response.file2.error || 'none'}` : 'undefined');
      console.log('[useNexus] Response error:', response.error || 'none');

      // Retorna a resposta para o componente tratar os erros
      // Não lança exceção aqui para permitir tratamento granular
      return response;
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to compare values';
      console.error('[useNexus] Compare failed with exception:', errorMsg, err);
      setError(errorMsg);
      // Retorna um objeto de erro em vez de lançar exceção
      return {
        file1: { content: '', filePath: '', fullUrl: '', size: 0, error: errorMsg },
        file2: { content: '', filePath: '', fullUrl: '', size: 0, error: errorMsg },
        error: errorMsg,
      };
    } finally {
      setLoading(false);
    }
  }, []);

  const clearError = useCallback(() => {
    setError(null);
  }, []);

  return {
    downloadValues,
    compareValues,
    loading,
    error,
    clearError,
  };
};
