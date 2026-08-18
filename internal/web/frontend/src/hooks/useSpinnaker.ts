import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { SpinnakerConfig, SpinnakerProject } from '../lib/api/types';
import { deriveSpinnakerEnv } from '../lib/spinnakerEnv';

// Config da integração (login, URLs, projeto selecionado) — mesma queryKey usada tanto pela
// tela de configuração (SSOProfileModal) quanto por quem consome o status de rollout, pra
// invalidação ficar simples num só lugar.
export function useSpinnakerConfig() {
  return useQuery({
    queryKey: ['spinnaker-config'],
    queryFn: () => apiClient.getSpinnakerConfig(),
    staleTime: 5 * 60 * 1000,
  });
}

export function useSaveSpinnakerConfig() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (config: SpinnakerConfig) => apiClient.saveSpinnakerConfig(config),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['spinnaker-config'] });
    },
  });
}

// Lista de projetos Spinnaker — só busca quando o usuário pede (botão), não automático, porque
// exige login real no Gate (ver seção 3 do plano) e as URLs precisam estar salvas antes.
export function useSpinnakerProjects(env: 'hlg' | 'prd' | null, enabled: boolean) {
  return useQuery<SpinnakerProject[]>({
    queryKey: ['spinnaker-projects', env],
    queryFn: () => apiClient.listSpinnakerProjects(env as 'hlg' | 'prd'),
    enabled: enabled && !!env,
    staleTime: 15 * 60 * 1000, // lista de projetos muda raramente
    retry: false, // erro aqui é quase sempre config incompleta — não adianta insistir sozinho
  });
}

// Status de rollout/rollback em lote (badge + modal em DeploymentsTab, seção 8 do plano).
// Uma chamada por cluster+namespace+env cobre todos os Deployments visíveis de uma vez —
// nunca uma chamada por deployment (seção 9.1 do plano, motivo de performance).
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
