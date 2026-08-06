import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { CertEndpointCheck, CertEndpointWithStatus } from '../lib/api/types';

// Monitor de Certificados Externos — lista de endpoints fora de qualquer cluster K8s (ver
// EXTERNAL-CERT-MONITOR-PLAN.md). Diferente de useNotes (escopado por cluster+aba), esta lista é
// global e compartilhada entre todos os clusters/usuários — um único queryKey sem parâmetros.
const CERT_ENDPOINTS_KEY = ['cert-endpoints'];

export function useCertEndpoints() {
  return useQuery({
    queryKey: CERT_ENDPOINTS_KEY,
    queryFn: () => apiClient.listCertEndpoints(),
    staleTime: 30000,
  });
}

export interface CertEndpointFormData {
  name: string;
  host: string;
  port?: number;
  sni?: string;
  group_label?: string;
}

export function useCreateCertEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CertEndpointFormData) => apiClient.createCertEndpoint(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: CERT_ENDPOINTS_KEY });
    },
  });
}

export function useUpdateCertEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: CertEndpointFormData & { enabled?: boolean } }) =>
      apiClient.updateCertEndpoint(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: CERT_ENDPOINTS_KEY });
    },
  });
}

export function useDeleteCertEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => apiClient.deleteCertEndpoint(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: CERT_ENDPOINTS_KEY });
    },
  });
}

// Recheck individual — usado pelo botão de uma linha específica da tabela.
export function useCheckCertEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => apiClient.checkCertEndpoint(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: CERT_ENDPOINTS_KEY });
    },
  });
}

// "Verificar agora" — checa todos os endpoints habilitados em paralelo (backend) e já devolve a
// listagem inteira atualizada; usar setQueryData em vez de invalidateQueries evita um refetch
// redundante logo depois de uma resposta que já tem os dados completos.
export function useCheckAllCertEndpoints() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => apiClient.checkAllCertEndpoints(),
    onSuccess: (data: CertEndpointWithStatus[]) => {
      queryClient.setQueryData(CERT_ENDPOINTS_KEY, data);
    },
  });
}

export function useCertEndpointHistory(id: number | null, limit = 20) {
  return useQuery<CertEndpointCheck[]>({
    queryKey: ['cert-endpoint-history', id, limit],
    queryFn: () => apiClient.getCertEndpointHistory(id as number, limit),
    enabled: id !== null,
    staleTime: 15000,
  });
}
