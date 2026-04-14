import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/client';
import { toast } from 'sonner';

interface ADGroup {
  id: string;
  displayName: string;
}

export interface UserPermissions {
  email: string;
  isSRE: boolean;
  groups: ADGroup[];
}

/**
 * Hook para obter permissões do usuário atual
 * Verifica se o usuário faz parte do grupo VV_CLOUD_SRE do Azure AD
 */
export function useUserPermissions() {
  return useQuery<UserPermissions>({
    queryKey: ['user-permissions'],
    queryFn: async () => {
      // Modo JWT: decodificar claims localmente sem chamar o backend
      const claims = apiClient.getTokenClaims();
      if (claims !== null) {
        return { email: claims.email ?? '', isSRE: claims.isSRE ?? false, groups: [] };
      }
      // Modo token estático: comportamento original
      const response = await apiClient.get<UserPermissions>('/permissions');
      return response;
    },
    staleTime: 1000 * 60 * 60, // Cache por 1 hora
    retry: 1,
    refetchOnWindowFocus: false,
  });
}

/**
 * Hook para forçar atualização das permissões do usuário
 * Útil após mudanças de grupos no Azure AD
 */
export function useRefreshPermissions() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async () => {
      const response = await apiClient.post<{ message: string; email: string; isSRE: boolean; groups: ADGroup[] }>('/permissions/refresh');
      return response; // Backend retorna {message, email, isSRE, groups}
    },
    onSuccess: (data) => {
      // Atualizar cache do React Query (sem o campo message)
      queryClient.setQueryData(['user-permissions'], {
        email: data.email,
        isSRE: data.isSRE,
        groups: data.groups,
      });

      toast.success('Permissões atualizadas', {
        description: `Status SRE: ${data.isSRE ? 'Sim' : 'Não'}`,
      });
    },
    onError: (error: any) => {
      toast.error('Erro ao atualizar permissões', {
        description: error.response?.data?.error || 'Erro desconhecido',
      });
    },
  });
}

/**
 * Hook simplificado que retorna apenas se o usuário é SRE
 */
export function useIsSRE() {
  const { data, isLoading } = useUserPermissions();
  return {
    isSRE: data?.isSRE ?? false,
    isLoading,
  };
}
