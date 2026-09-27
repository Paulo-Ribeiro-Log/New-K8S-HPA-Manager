import { useCallback, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import type { OrphanResourcesResponse, UnattachedDisksReport } from "@/components/finops/types";

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

export function useUnattachedDisks(cluster: string, enabled = true) {
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
    enabled: !!cluster && enabled,
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

async function fetchOrphanResources(cluster: string, journeys: string[], refresh: boolean, signal?: AbortSignal): Promise<OrphanResourcesResponse> {
  const params = new URLSearchParams({ cluster });
  if (journeys.length > 0) params.set("journeys", journeys.join(","));
  if (refresh) params.set("refresh", "true");
  const r = await fetch(`/api/v1/finops/orphan-resources?${params.toString()}`, { signal, headers: authHeaders() });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
  }
  return r.json();
}

// Recursos órfãos (Azure) nos resource groups das jornadas selecionadas no cabeçalho (journeys
// vazio = todas), restritos ao ambiente (prd/hlg/...) do cluster analisado — o backend deriva o
// ambiente do nome. Mesmo esquema de cache/refresh de useUnattachedDisks.
export function useOrphanResources(cluster: string, journeys: string[], enabled = true) {
  const forceRef = useRef(false);
  const key = [...journeys].sort().join(",");

  const query = useQuery<OrphanResourcesResponse>({
    queryKey: ["finops-orphan-resources", cluster, key],
    queryFn: ({ signal }) => {
      const refresh = forceRef.current;
      forceRef.current = false;
      return fetchOrphanResources(cluster, journeys, refresh, signal);
    },
    enabled: enabled && !!cluster,
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
