import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/client';
import type { K8sNamespacePermissions } from '@/lib/api/types';

// Permissões conservadoras usadas enquanto a query carrega ou quando cluster/namespace
// ainda não estão disponíveis. Permite leitura, bloqueia escrita.
const READ_ONLY_PERMISSIONS: K8sNamespacePermissions = {
  cluster: '',
  namespace: '',
  canListHPA: true,
  canGetHPA: true,
  canUpdateHPA: false,
  canListPods: true,
  canExecPods: false,
  canViewLogs: true,
  canListDeployments: true,
  canUpdateDeployment: false,
  canWriteSecrets: false,
  canWriteConfigMaps: false,
  canWriteCronJobs: false,
  canWriteServices: false,
  canWriteIngress: false,
  canWriteStatefulSets: false,
  canWriteDaemonSets: false,
  canWritePods: false,
  incomplete: false,
};

/**
 * Retorna as permissões reais do K8s para o cluster+namespace via SelfSubjectRulesReview.
 * Usa o kubeconfig do servidor (que é o do usuário em modo local), portanto reflete
 * exatamente o que o RBAC do cluster permite — independente de grupos AD.
 *
 * Cache: 5 minutos (staleTime). Não refetch no foco da janela.
 */
export function useK8sPermissions(cluster: string, namespace: string) {
  const enabled = !!cluster && !!namespace;

  const query = useQuery<K8sNamespacePermissions>({
    queryKey: ['k8s-permissions', cluster, namespace],
    queryFn: () => apiClient.getK8sPermissions(cluster, namespace),
    staleTime: 5 * 60 * 1000,
    enabled,
    refetchOnWindowFocus: false,
    retry: 1,
    // Em caso de erro (ex: cluster inacessível), assume somente leitura.
    placeholderData: READ_ONLY_PERMISSIONS,
  });

  return {
    ...query,
    permissions: query.data ?? READ_ONLY_PERMISSIONS,
    canWrite: (query.data ?? READ_ONLY_PERMISSIONS).canUpdateHPA,
  };
}
