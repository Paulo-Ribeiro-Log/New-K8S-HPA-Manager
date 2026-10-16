import { useQuery } from "@tanstack/react-query";
import type { RightsizingResponse } from "@/components/RightsizingTab";
import { apiClient } from "@/lib/api/client";

// Leitura do rightsizing persistido no SQLite (nunca dispara scan). Mesma queryKey que o
// FinOpsTab invalida depois de "Analisar".
export function useRightsizingReport(cluster: string) {
  return useQuery<RightsizingResponse>({
    queryKey: ["finops-rightsizing", cluster],
    queryFn: () => apiClient.get<RightsizingResponse>(`/finops/rightsizing?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster,
    staleTime: 60 * 1000,
  });
}
