import { useMemo, useState } from "react";
import { isProdClusterName, isHlgClusterName } from "@/lib/clusterSafety";

export type EnvFilter = "all" | "hlg" | "prd";

/**
 * Filtro compartilhado dos seletores de cluster (Header.tsx / ClusterSelectorForTab.tsx) —
 * extraído porque os dois duplicavam exatamente o mesmo useState<EnvFilter> + filtro via
 * isProdClusterName/isHlgClusterName. Ganha, junto, o filtro por "jornada" (tag de recurso do
 * Azure, ex: "logistica"/"backoffice") — dinâmico: só existe quando há 2+ valores distintos entre
 * os clusters recebidos (clusterJourneys), consistente com o pedido de "se não tem outra tag
 * diferente, nada deve ser exibido".
 *
 * clusterJourneys é opcional e keyed por cluster (mesmo formato de clusterProviders já usado
 * nesta app: Record<context, valor>) — sem ele, journeyOptions fica sempre vazio e o
 * comportamento é idêntico ao filtro Todos/HLG/PRD de antes desta mudança.
 */
export function useClusterEnvFilter(clusters: string[], clusterJourneys?: Record<string, string>) {
  const [envFilter, setEnvFilter] = useState<EnvFilter>("all");
  const [selectedJourneys, setSelectedJourneys] = useState<Set<string>>(new Set());

  // Calculado sobre TODOS os clusters recebidos (não só os já filtrados por envFilter) — evita
  // que a lista de checkboxes mude de tamanho dependendo do Todos/HLG/PRD escolhido, o que
  // seria uma UX confusa (checkbox "some" ao trocar de aba HLG pra PRD, por exemplo).
  const journeyOptions = useMemo(() => {
    if (!clusterJourneys) return [];
    const distinct = new Set<string>();
    for (const cluster of clusters) {
      const journey = clusterJourneys[cluster];
      if (journey) distinct.add(journey);
    }
    return Array.from(distinct).sort((a, b) => a.localeCompare(b));
  }, [clusters, clusterJourneys]);

  const toggleJourney = (value: string) => {
    setSelectedJourneys((prev) => {
      const next = new Set(prev);
      if (next.has(value)) {
        next.delete(value);
      } else {
        next.add(value);
      }
      return next;
    });
  };

  // Nenhum ou todos marcados = "Todos" (sem filtro de jornada) — pedido explícito do usuário.
  const journeyFilterActive =
    journeyOptions.length > 0 && selectedJourneys.size > 0 && selectedJourneys.size < journeyOptions.length;

  const filteredClusters = useMemo(() => {
    return clusters.filter((cluster) => {
      if (envFilter === "prd" && !isProdClusterName(cluster)) return false;
      if (envFilter === "hlg" && !isHlgClusterName(cluster)) return false;

      if (journeyFilterActive) {
        const journey = clusterJourneys?.[cluster];
        // Cluster sem jornada conhecida (EKS/GKE, ou AKS ainda não tagueado) fica de fora
        // sempre que um filtro de jornada específico está ativo — decisão confirmada com o
        // usuário, mesma lógica de "filtro ativo estreita a lista" do Todos/HLG/PRD.
        if (!journey || !selectedJourneys.has(journey)) return false;
      }

      return true;
    });
  }, [clusters, envFilter, journeyFilterActive, selectedJourneys, clusterJourneys]);

  return {
    envFilter,
    setEnvFilter,
    journeyOptions,
    selectedJourneys,
    toggleJourney,
    filteredClusters,
  };
}
