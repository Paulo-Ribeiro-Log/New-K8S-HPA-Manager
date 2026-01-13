import { useState, useEffect, useCallback, useRef } from "react";
import { useTabManager } from "@/contexts/TabContext";

/**
 * Hook customizado para persistir estado de componentes entre trocas de aba
 *
 * @param namespace - Namespace do componente (ex: 'pods', 'configmaps', 'deployments')
 * @param key - Chave do estado (ex: 'selectedPod', 'searchTerm')
 * @param initialValue - Valor inicial se não houver valor salvo
 *
 * @example
 * // Ao invés de:
 * const [selectedPod, setSelectedPod] = useState(null);
 *
 * // Use:
 * const [selectedPod, setSelectedPod] = usePersistedTabState('pods', 'selectedPod', null);
 */
export function usePersistedTabState<T>(
  namespace: string,
  key: string,
  initialValue: T
): [T, (value: T | ((prev: T) => T)) => void] {
  const { updateActiveTabState, getActiveTab } = useTabManager();
  const isRestoringRef = useRef(false);

  // Tentar restaurar valor salvo do TabContext
  const getSavedValue = useCallback((): T => {
    const activeTab = getActiveTab();
    if (activeTab?.pageState?.workloadStates?.[namespace]?.[key] !== undefined) {
      return activeTab.pageState.workloadStates[namespace][key] as T;
    }
    return initialValue;
  }, [getActiveTab, namespace, key, initialValue]);

  // Estado local (inicializado com valor salvo ou inicial)
  const [state, setState] = useState<T>(getSavedValue);

  // ✅ RESTAURAR estado quando componente monta (ao voltar para a aba)
  useEffect(() => {
    const savedValue = getSavedValue();

    // Só restaurar se houver valor salvo diferente do inicial
    if (savedValue !== initialValue || savedValue !== state) {
      isRestoringRef.current = true;
      setState(savedValue);

      // Reset flag após restauração
      setTimeout(() => {
        isRestoringRef.current = false;
      }, 0);
    }
  }, [namespace, key]); // Só roda quando namespace/key mudam (montagem do componente)

  // Persistir no TabContext sempre que o estado mudar
  useEffect(() => {
    // Não persistir durante restauração (evita sobrescrever com valor antigo)
    if (isRestoringRef.current) return;

    const activeTab = getActiveTab();
    const currentWorkloadStates = activeTab?.pageState?.workloadStates || {};

    // Atualizar apenas o namespace específico
    const updatedWorkloadStates = {
      ...currentWorkloadStates,
      [namespace]: {
        ...(currentWorkloadStates[namespace] || {}),
        [key]: state,
      },
    };

    updateActiveTabState({
      workloadStates: updatedWorkloadStates,
    });
  }, [state, namespace, key, updateActiveTabState, getActiveTab]);

  return [state, setState];
}
