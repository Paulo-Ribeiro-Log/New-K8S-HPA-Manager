import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../lib/api/client";
import type { PortForwardSession, StartPortForwardRequest } from "../lib/api/types";

// Port Forward — sessões globais (todo usuário vê as sessões de todo mundo, mesma transparência
// de outras ferramentas server-side desta app), sem escopo por cluster/aba — mesmo padrão de
// useCertEndpoints. Polling só enquanto `enabled` (o modal aberto) evita chamadas desnecessárias
// em segundo plano quando ninguém está olhando pra lista.
const PORTFORWARD_KEY = ["portforward-sessions"];

export function usePortForwardSessions(enabled: boolean) {
  return useQuery({
    queryKey: PORTFORWARD_KEY,
    queryFn: () => apiClient.listPortForwards(),
    enabled,
    refetchInterval: enabled ? 3000 : false,
    staleTime: 2000,
  });
}

export function useStartPortForward() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: StartPortForwardRequest) => apiClient.startPortForward(req),
    onSuccess: (session: PortForwardSession) => {
      // Popula o cache imediatamente com a sessão recém-criada (não espera o próximo poll de
      // 3s) — refetch em seguida garante consistência com o servidor.
      queryClient.setQueryData<PortForwardSession[]>(PORTFORWARD_KEY, (prev) => [
        session,
        ...(prev || []).filter((s) => s.id !== session.id),
      ]);
      queryClient.invalidateQueries({ queryKey: PORTFORWARD_KEY });
    },
  });
}

export function useStopPortForward() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiClient.stopPortForward(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: PORTFORWARD_KEY });
    },
  });
}
