import { useRef, useCallback } from "react";
import { Cluster } from "@/lib/api/types";

const SESSION_KEY_PREFIX = "vpn_warned_";

export interface CloudChangeInfo {
  changed: boolean;
  from: string;
  to: string;
  /** true se é a primeira vez que o usuário é avisado sobre esse cloud nesta sessão */
  firstWarning: boolean;
}

/**
 * Hook para detectar mudança de cloud provider ao trocar de cluster.
 * Mantém rastreamento da sessão para exibir banner na primeira vez
 * e toast nas subsequentes.
 */
export function useCloudProvider(clusters: Cluster[]) {
  const currentCloudRef = useRef<string>("");

  const getCloudProvider = useCallback(
    (clusterContext: string): string => {
      const found = clusters.find((c) => c.context === clusterContext);
      return found?.cloud_provider ?? "unknown";
    },
    [clusters]
  );

  /**
   * Verifica se o cloud mudou ao selecionar `newCluster`.
   * Deve ser chamado ANTES de atualizar o cluster selecionado.
   */
  const detectCloudChange = useCallback(
    (newCluster: string): CloudChangeInfo => {
      const to = getCloudProvider(newCluster);
      const from = currentCloudRef.current;
      const changed = from !== "" && from !== to;
      const firstWarning = changed && !sessionStorage.getItem(SESSION_KEY_PREFIX + to);
      return { changed, from, to, firstWarning };
    },
    [getCloudProvider]
  );

  /**
   * Registra o cloud do cluster recém-selecionado como o atual.
   * Deve ser chamado APÓS confirmar a troca de cluster.
   */
  const commitCloudChange = useCallback(
    (clusterContext: string) => {
      const provider = getCloudProvider(clusterContext);
      if (currentCloudRef.current !== provider && provider !== "unknown") {
        sessionStorage.setItem(SESSION_KEY_PREFIX + provider, "1");
      }
      currentCloudRef.current = provider;
    },
    [getCloudProvider]
  );

  return { getCloudProvider, detectCloudChange, commitCloudChange };
}

/** Retorna o label e a classe CSS do badge para um cloud provider. */
export function cloudProviderBadge(provider: string | undefined): { label: string; className: string } | null {
  switch (provider) {
    case "aks":
      return { label: "AKS", className: "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300" };
    case "eks":
      return { label: "EKS", className: "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300" };
    case "gke":
      return { label: "GKE", className: "bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300" };
    default:
      return null;
  }
}
