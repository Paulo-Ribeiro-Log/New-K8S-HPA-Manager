import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import { deriveSpinnakerEnv } from '../lib/spinnakerEnv';

// Status de rollout/rollback em lote (badge + modal em DeploymentsTab, seção 8 do plano).
// Uma chamada por cluster+namespace+env cobre todos os Deployments visíveis de uma vez —
// nunca uma chamada por deployment (seção 9.1 do plano, motivo de performance).
//
// Configuração (login, URLs, projeto) fica em SpinnakerCredentialModal.tsx (menu de perfil →
// Credenciais → Spinnaker, mesmo padrão de AWXCredentialModal.tsx/DynatraceCredentialModal.tsx)
// — usa apiClient direto (sem React Query), não esses hooks.
export function useSpinnakerRolloutStatus(cluster: string | undefined, namespace: string | undefined) {
  const env = deriveSpinnakerEnv(cluster);
  // "__all__" é o sentinel usado pelo <Select> de namespace em várias abas (ex: IngressTab.tsx)
  // pra dar um valor não-vazio à opção "Todos" — o estado real (deploymentsNamespace em
  // Index.tsx) fica com esse literal por padrão, não convertido pra "" antes de chegar aqui.
  // Sem tratar isso, "__all__" ia como filtro de namespace literal pro backend (que não acha
  // nenhum Deployment com esse nome) — o badge ficava sempre vazio por padrão, mesmo com tudo
  // configurado. Mesmo idioma já usado por outras abas pra esse mesmo sentinel.
  const effectiveNamespace = namespace && namespace !== '__all__' ? namespace : undefined;
  return useQuery({
    queryKey: ['spinnaker-rollout-status', cluster, effectiveNamespace, env],
    queryFn: () => apiClient.getSpinnakerRolloutStatusBatch(cluster as string, effectiveNamespace, env as 'hlg' | 'prd'),
    enabled: !!cluster && !!env,
    staleTime: 2 * 60 * 1000,
    retry: 1,
  });
}
