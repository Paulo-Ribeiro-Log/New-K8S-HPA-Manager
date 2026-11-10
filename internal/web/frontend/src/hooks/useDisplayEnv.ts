import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { DisplayReport } from "@/lib/wslgTutorial";

// Diagnóstico do ambiente gráfico do servidor (WSLg, navegador). Só leitura.
export function useDisplayEnv() {
  const qc = useQueryClient();
  const q = useQuery<DisplayReport>({
    queryKey: ["system-display"],
    queryFn: () => apiClient.get<DisplayReport>("/system/display"),
    staleTime: 60_000,
    retry: false,
  });
  // Refaz o teste ignorando o cache do servidor.
  const recheck = async () => {
    const r = await apiClient.get<DisplayReport>("/system/display?refresh=true");
    qc.setQueryData(["system-display"], r);
    return r;
  };
  return { report: q.data, loading: q.isLoading, recheck };
}
