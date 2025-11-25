// Hook para buscar alertas do Prometheus
import { useQuery } from "@tanstack/react-query";
import { apiClient as api } from "@/lib/api/client";

export interface HPAAlert {
  alertName: string;
  severity: string;
  state: string;
  description: string;
  activeAt: string;
  namespace: string;
  hpaName?: string;
  deployment?: string;
  pod?: string;
  container?: string;
  mitigation?: {
    autoMitigable: boolean;
    action: string;
    priority: string;
    steps: string[];
  };
}

export interface NodePoolAlert {
  alertName: string;
  severity: string;
  state: string;
  description: string;
  activeAt: string;
  node: string;
  mitigation?: {
    autoMitigable: boolean;
    action: string;
    priority: string;
    steps: string[];
  };
}

export interface AlertSummary {
  critical: number;
  warning: number;
  info: number;
  total: number;
}

// Hook para buscar alertas de HPA
export function useHPAAlerts(cluster: string, enabled = true) {
  return useQuery<HPAAlert[]>({
    queryKey: ["hpa-alerts", cluster],
    queryFn: async () => {
      const response: any = await api.getHPAAlerts(cluster);
      return response.alerts || [];
    },
    enabled: enabled && !!cluster,
    refetchInterval: 30000, // Atualizar a cada 30 segundos
    staleTime: 25000,
  });
}

// Hook para buscar alertas de HPA por namespace
export function useHPAAlertsByNamespace(
  cluster: string,
  namespace: string,
  enabled = true
) {
  return useQuery<HPAAlert[]>({
    queryKey: ["hpa-alerts", cluster, namespace],
    queryFn: async () => {
      const response: any = await api.getHPAAlertsByNamespace(cluster, namespace);
      return response.alerts || [];
    },
    enabled: enabled && !!cluster && !!namespace,
    refetchInterval: 30000,
    staleTime: 25000,
  });
}

// Hook para buscar alertas específicos de um HPA
export function useHPASpecificAlerts(
  cluster: string,
  namespace: string,
  hpaName: string,
  enabled = true
) {
  return useQuery<HPAAlert[]>({
    queryKey: ["hpa-specific-alerts", cluster, namespace, hpaName],
    queryFn: async () => {
      const response: any = await api.getHPAAlertsByNamespace(cluster, namespace);
      const allAlerts: HPAAlert[] = response.alerts || [];
      // Filtrar alertas relacionados a este HPA específico
      return allAlerts.filter(
        (alert) =>
          alert.hpaName === hpaName ||
          alert.deployment === hpaName ||
          alert.namespace === namespace
      );
    },
    enabled: enabled && !!cluster && !!namespace && !!hpaName,
    refetchInterval: 30000,
    staleTime: 25000,
  });
}

// Hook para buscar alertas de Node Pool
export function useNodePoolAlerts(cluster: string, enabled = true) {
  return useQuery<NodePoolAlert[]>({
    queryKey: ["nodepool-alerts", cluster],
    queryFn: async () => {
      const response: any = await api.getNodePoolAlerts(cluster);
      return response.alerts || [];
    },
    enabled: enabled && !!cluster,
    refetchInterval: 30000,
    staleTime: 25000,
  });
}

// Hook para buscar resumo de alertas
export function useAlertSummary(cluster: string, enabled = true) {
  return useQuery<AlertSummary>({
    queryKey: ["alert-summary", cluster],
    queryFn: async () => {
      const response: any = await api.getAlertSummary(cluster);
      return response.summary || { critical: 0, warning: 0, info: 0, total: 0 };
    },
    enabled: enabled && !!cluster,
    refetchInterval: 30000,
    staleTime: 25000,
  });
}

// Hook combinado para buscar todos os alertas de um cluster
export function useAllAlerts(cluster: string, enabled = true) {
  const hpaAlerts = useHPAAlerts(cluster, enabled);
  const nodePoolAlerts = useNodePoolAlerts(cluster, enabled);
  const summary = useAlertSummary(cluster, enabled);

  return {
    hpaAlerts: hpaAlerts.data || [],
    nodePoolAlerts: nodePoolAlerts.data || [],
    summary: summary.data || { critical: 0, warning: 0, info: 0, total: 0 },
    loading: hpaAlerts.isLoading || nodePoolAlerts.isLoading || summary.isLoading,
    error: hpaAlerts.error || nodePoolAlerts.error || summary.error,
  };
}

// Helper para contar alertas críticos
export function useCriticalAlertsCount(cluster: string, enabled = true) {
  const summary = useAlertSummary(cluster, enabled);
  return summary.data?.critical || 0;
}

// Helper para verificar se há alertas críticos
export function useHasCriticalAlerts(cluster: string, enabled = true) {
  const count = useCriticalAlertsCount(cluster, enabled);
  return count > 0;
}
