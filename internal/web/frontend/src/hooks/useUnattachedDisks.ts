import { useCallback, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import type { UnattachedDisksReport } from "@/components/finops/types";

// Mesma convenção de useRightsizingReport.ts (hooks do FinOps usam fetch direto + header de auth).
// O backend guarda a listagem do cloud em cache de 5min; "Atualizar" manda refresh=true e ignora
// esse cache. Só lê — a app nunca exclui disco.
const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

async function fetchUnattachedDisks(cluster: string, refresh: boolean, signal?: AbortSignal): Promise<UnattachedDisksReport> {
  const url = `/api/v1/finops/unattached-disks?cluster=${encodeURIComponent(cluster)}${refresh ? "&refresh=true" : ""}`;
  const r = await fetch(url, { signal, headers: authHeaders() });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
  }
  return r.json();
}

export function useUnattachedDisks(cluster: string) {
  // O próximo fetch é "forçado" quando o usuário clica em Atualizar — via ref pra não entrar na
  // queryKey (senão o resultado forçado e o normal virariam entradas de cache diferentes).
  const forceRef = useRef(false);

  const query = useQuery<UnattachedDisksReport>({
    queryKey: ["finops-unattached-disks", cluster],
    queryFn: ({ signal }) => {
      const refresh = forceRef.current;
      forceRef.current = false;
      return fetchUnattachedDisks(cluster, refresh, signal);
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const { refetch } = query;
  const refresh = useCallback(() => {
    forceRef.current = true;
    return refetch();
  }, [refetch]);

  return { ...query, refresh };
}
