import { useState, useCallback } from "react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

// Tipos de filtros
export type FilterType = "exact" | "namespace" | "category";
export type ResourceType = "Deployment" | "ConfigMap" | "Secret";

export type FilterCategory =
  | "deployment_no_probes"
  | "deployment_no_liveness_probe"
  | "deployment_no_readiness_probe"
  | "configmap_empty"
  | "secret_empty"
  | "secret_service_account"
  | "secret_invalid_values";

export interface FilterRule {
  id: string;
  type: FilterType;
  resource_type: ResourceType;
  namespace: string;
  name: string;
  category: FilterCategory;
  reason: string;
  created_at: string;
  created_by: string;
}

export interface FilterCategoryOption {
  value: string;
  label: string;
  description: string;
}

export interface FiltersStats {
  total: number;
  exact: number;
  namespace: number;
  category: number;
}

export interface FiltersResponse {
  success: boolean;
  data: {
    rules: FilterRule[];
    stats: FiltersStats;
  };
}

export interface CategoriesResponse {
  success: boolean;
  data: FilterCategoryOption[];
}

export const useFilters = () => {
  const [rules, setRules] = useState<FilterRule[]>([]);
  const [stats, setStats] = useState<FiltersStats | null>(null);
  const [categories, setCategories] = useState<FilterCategoryOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Listar regras
  const fetchRules = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const response = await apiClient.getFilters();
      setRules(response.data.rules);
      setStats(response.data.stats);
    } catch (err: any) {
      const errorMsg = err.message || "Falha ao carregar filtros";
      setError(errorMsg);
      toast.error(errorMsg);
    } finally {
      setLoading(false);
    }
  }, []);

  // Listar categorias
  const fetchCategories = useCallback(async () => {
    try {
      const response = await apiClient.getFilterCategories();
      setCategories(response.data);
    } catch (err: any) {
      console.error("Failed to fetch categories:", err);
    }
  }, []);

  // Adicionar regra
  const addRule = useCallback(async (rule: Partial<FilterRule>) => {
    try {
      await apiClient.addFilterRule(rule);
      toast.success("Filtro adicionado com sucesso");
      await fetchRules(); // Recarregar lista
      return true;
    } catch (err: any) {
      const errorMsg = err.message || "Falha ao adicionar filtro";
      toast.error(errorMsg);
      return false;
    }
  }, [fetchRules]);

  // Remover regra
  const removeRule = useCallback(async (id: string) => {
    try {
      await apiClient.removeFilterRule(id);
      toast.success("Filtro removido com sucesso");
      await fetchRules(); // Recarregar lista
      return true;
    } catch (err: any) {
      const errorMsg = err.message || "Falha ao remover filtro";
      toast.error(errorMsg);
      return false;
    }
  }, [fetchRules]);

  return {
    rules,
    stats,
    categories,
    loading,
    error,
    fetchRules,
    fetchCategories,
    addRule,
    removeRule,
  };
};
