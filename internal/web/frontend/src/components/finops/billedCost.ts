import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { BilledCostResponse } from "./types";

export function useBilledCost(cluster: string, enabled: boolean) {
  return useQuery({
    queryKey: ["finops-billed-cost", cluster],
    queryFn: () => apiClient.getFinOpsBilledCost<BilledCostResponse>(cluster),
    enabled: enabled && !!cluster,
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
}

// Fator para converter a moeda da fatura em BRL (null = moeda sem conversão conhecida).
export function billedToBRL(data: BilledCostResponse | undefined, exchangeRate: number): number | null {
  const cur = (data?.currency ?? "BRL").toUpperCase();
  if (cur === "BRL") return 1;
  if (cur === "USD" && exchangeRate > 0) return exchangeRate;
  return null;
}
