// Integridade do Prometheus do cluster ATIVO: consulta assim que o cluster é selecionado, na troca de cluster e
// a cada 60 s enquanto ele continua ativo (docs: PROMETHEUS-WAL-CLEANUP.md). Independente do Alertmanager: com o
// Prometheus fora do ar, é ele que não consegue avisar.
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { PrometheusHealthItem, PrometheusWALHealth } from "@/lib/api/types";

const REFRESH_MS = 60_000;

export function usePrometheusHealth(cluster: string, enabled = true) {
  return useQuery<PrometheusWALHealth>({
    queryKey: ["prometheus-health", cluster],
    queryFn: () => apiClient.getPrometheusWALHealth(cluster),
    enabled: enabled && !!cluster,
    // Sem polling para cluster que deixou de ser o ativo (mesmo guard de useHPAAlerts).
    refetchInterval: (query) => (query.queryKey[1] === cluster ? REFRESH_MS : false),
    staleTime: 45_000,
    retry: false,
  });
}

/** Só os itens que viram alerta (critical/warning). Não verificável, iniciando etc. nunca entram. */
export function usePrometheusHealthAlerts(cluster: string, enabled = true) {
  const query = usePrometheusHealth(cluster, enabled);
  const items: PrometheusHealthItem[] = (query.data?.verifiable ? query.data.items : []).filter(
    (i) => i.severity === "critical" || i.severity === "warning"
  );
  return {
    items,
    critical: items.filter((i) => i.severity === "critical").length,
    warning: items.filter((i) => i.severity === "warning").length,
    isLoading: query.isLoading,
  };
}
