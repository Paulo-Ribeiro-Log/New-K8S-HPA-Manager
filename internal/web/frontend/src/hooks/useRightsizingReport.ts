import { useQuery } from "@tanstack/react-query";
import type { RightsizingResponse } from "@/components/RightsizingTab";

// Extraído de RightsizingTab.tsx pra ficar num arquivo dedicado (mesma convenção dos demais
// hooks deste diretório) — reaproveitado tanto pela aba Rightsizing em si quanto pelo badge da
// TabsTrigger (F2.1 do FINOPS-IMPROVEMENTS-PLAN.md, RightsizingTabBadge em RightsizingTab.tsx) —
// mesma queryKey nos dois, então o React Query deduplica a requisição de rede quando os dois
// estão montados ao mesmo tempo (FinOpsTab sempre monta o badge; RightsizingTab só monta quando
// a aba está aberta). Nunca dispara um scan — só lê o que já está persistido no SQLite.
const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

export function useRightsizingReport(cluster: string) {
  return useQuery<RightsizingResponse>({
    queryKey: ["finops-rightsizing", cluster],
    queryFn: async () => {
      const r = await fetch(`/api/v1/finops/rightsizing?cluster=${encodeURIComponent(cluster)}`, {
        headers: authHeaders(),
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 60 * 1000,
  });
}
