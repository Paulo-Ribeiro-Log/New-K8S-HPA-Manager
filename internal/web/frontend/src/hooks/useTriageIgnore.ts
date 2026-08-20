import { useState, useCallback } from "react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

// Modo Triagem — supressão de sinal externo (HEALTHCHECK-TRIAGE-MODE-PLAN.md seção 2.5, Fase 4).
// Distinto de useFilters.ts: aquele suprime ACHADOS de postura K8s (ConfigMap vazio, Secret de
// sistema); este suprime um SINAL EXTERNO (alerta/problem/trigger) ANTES de virar escopo de
// namespace no Modo Triagem.
export type TriageIgnoreSource =
  | "prometheus_alert"
  | "dynatrace_problem"
  | "zabbix_trigger"
  | "elasticsearch_pattern";

export interface TriageIgnoreEntry {
  id: string;
  source: TriageIgnoreSource;
  value: string;
  reason?: string;
  created_at: string;
  created_by: string;
}

export interface TriageIgnoreSourceOption {
  value: TriageIgnoreSource;
  label: string;
  field_label: string;
  enabled: boolean;
}

export const useTriageIgnore = () => {
  const [entries, setEntries] = useState<TriageIgnoreEntry[]>([]);
  const [sources, setSources] = useState<TriageIgnoreSourceOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchEntries = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await apiClient.getTriageIgnoreEntries();
      setEntries(response.data.entries ?? []);
    } catch (err: any) {
      const errorMsg = err.message || "Falha ao carregar lista de supressão";
      setError(errorMsg);
      toast.error(errorMsg);
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchSources = useCallback(async () => {
    try {
      const response = await apiClient.getTriageIgnoreSources();
      setSources(response.data ?? []);
    } catch (err: any) {
      console.error("Failed to fetch triage ignore sources:", err);
    }
  }, []);

  const addEntry = useCallback(
    async (entry: { source: TriageIgnoreSource; value: string; reason?: string }) => {
      try {
        await apiClient.addTriageIgnoreEntry(entry);
        toast.success("Entrada adicionada com sucesso");
        await fetchEntries();
        return true;
      } catch (err: any) {
        const errorMsg = err.message || "Falha ao adicionar entrada";
        toast.error(errorMsg);
        return false;
      }
    },
    [fetchEntries]
  );

  const removeEntry = useCallback(
    async (id: string) => {
      try {
        await apiClient.removeTriageIgnoreEntry(id);
        toast.success("Entrada removida com sucesso");
        await fetchEntries();
        return true;
      } catch (err: any) {
        const errorMsg = err.message || "Falha ao remover entrada";
        toast.error(errorMsg);
        return false;
      }
    },
    [fetchEntries]
  );

  return {
    entries,
    sources,
    loading,
    error,
    fetchEntries,
    fetchSources,
    addEntry,
    removeEntry,
  };
};
