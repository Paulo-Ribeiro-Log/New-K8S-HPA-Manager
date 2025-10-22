import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { Session, SessionFolder, SessionTemplate } from '../lib/api/types';

// Hook para listar todas as sessões
export function useSessions() {
  return useQuery({
    queryKey: ['sessions'],
    queryFn: () => apiClient.getSessions(),
    staleTime: 30000, // 30 segundos
  });
}

// Hook para listar pastas de sessões
export function useSessionFolders() {
  return useQuery({
    queryKey: ['sessions', 'folders'],
    queryFn: () => apiClient.getSessionFolders(),
    staleTime: 60000, // 1 minuto (folders não mudam frequentemente)
  });
}

// Hook para listar sessões de uma pasta específica
export function useSessionsInFolder(folder: string) {
  return useQuery({
    queryKey: ['sessions', 'folder', folder],
    queryFn: () => apiClient.getSessionsInFolder(folder),
    enabled: !!folder,
    staleTime: 30000,
  });
}

// Hook para carregar uma sessão específica
export function useSession(name: string, folder?: string) {
  return useQuery({
    queryKey: ['sessions', 'detail', name, folder],
    queryFn: () => apiClient.getSession(name, folder),
    enabled: !!name,
    staleTime: 60000,
  });
}

// Hook para templates de sessões
export function useSessionTemplates() {
  return useQuery({
    queryKey: ['sessions', 'templates'],
    queryFn: async () => {
      // Mock templates por enquanto - depois integrar com backend
      return [
        {
          name: "Upscale Padrão",
          description: "Template para scale up de produção",
          pattern: "{action}_{env}_{date}_{time}",
          variables: ["{action}", "{env}", "{date}", "{time}"],
          example: "upscale_prod_18-10-25_19:30"
        },
        {
          name: "Downscale Padrão", 
          description: "Template para scale down de produção",
          pattern: "{action}_{cluster}_{date}",
          variables: ["{action}", "{cluster}", "{date}"],
          example: "downscale_akspriv-faturamento_18-10-25"
        },
        {
          name: "Node Pool Stress Test",
          description: "Template para testes de stress com node pools",
          pattern: "{action}_nodepool_{hpa_count}hpas_{date}",
          variables: ["{action}", "{hpa_count}", "{date}"],
          example: "stress_nodepool_5hpas_18-10-25"
        },
        {
          name: "Sessão Customizada",
          description: "Nome totalmente customizável",
          pattern: "{action}_custom_{timestamp}",
          variables: ["{action}", "{timestamp}"],
          example: "emergency_custom_18-10-25_19:30:15"
        }
      ] as SessionTemplate[];
    },
    staleTime: 300000, // 5 minutos (templates raramente mudam)
  });
}

// Hook para salvar sessão
export function useSaveSession() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: async (sessionData: {
      name: string;
      folder: string;
      description?: string;
      template: string;
      changes: any[];
      node_pool_changes: any[];
    }) => {
      // Usar o endpoint real do backend que já existe
      const response = await fetch('/api/v1/sessions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer poc-token-123`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(sessionData),
      });
      
      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.error?.message || `HTTP ${response.status}: ${response.statusText}`);
      }
      
      const data = await response.json();
      
      if (!data.success) {
        throw new Error(data.error?.message || 'Falha ao salvar sessão');
      }
      
      return data.data;
    },
    onSuccess: (data, variables) => {
      // Invalidar queries relacionadas
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
      queryClient.invalidateQueries({ queryKey: ['sessions', 'folder', variables.folder] });
      queryClient.invalidateQueries({ queryKey: ['sessions', 'folders'] });
    },
  });
}

// Hook para deletar sessão
export function useDeleteSession() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: ({ name, folder }: { name: string; folder?: string }) => {
      // TODO: Integrar com backend real
      // return apiClient.deleteSession(name, folder);
      console.log('Deletando sessão:', name, folder);
      return Promise.resolve({ success: true });
    },
    onSuccess: (data, variables) => {
      // Invalidar queries relacionadas
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
      if (variables.folder) {
        queryClient.invalidateQueries({ queryKey: ['sessions', 'folder', variables.folder] });
      }
      queryClient.invalidateQueries({ queryKey: ['sessions', 'folders'] });
      
      // Remover a sessão específica do cache
      queryClient.removeQueries({ 
        queryKey: ['sessions', 'detail', variables.name, variables.folder] 
      });
    },
  });
}