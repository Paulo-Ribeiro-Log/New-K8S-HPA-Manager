import { useCallback, useEffect, useState } from "react";
import { apiClient } from "@/lib/api/client";
import type { VMInstance } from "@/lib/api/types";

// useVMAwsProfiles — lista os profiles AWS configurados no host do servidor (mesmo mecanismo já
// usado pelo autodiscovery de EKS, ver internal/config/eks_discovery.go:ListAWSProfiles).
export function useVMAwsProfiles() {
  const [profiles, setProfiles] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchProfiles = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.listVMAwsProfiles();
      setProfiles(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha ao listar profiles AWS");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchProfiles();
  }, [fetchProfiles]);

  return { profiles, loading, error, refetch: fetchProfiles };
}

// useVMInstances — lista instâncias EC2 de um profile+região. Só busca quando os dois estão
// preenchidos (enabled implícito) — evita uma chamada de API com parâmetro vazio.
export function useVMInstances(profile: string, region: string) {
  const [instances, setInstances] = useState<VMInstance[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchInstances = useCallback(
    async (opts?: { refresh?: boolean }) => {
      if (!profile || !region) {
        setInstances([]);
        return;
      }
      try {
        setLoading(true);
        setError(null);
        const data = await apiClient.listVMInstances(profile, region, opts);
        setInstances(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Falha ao listar instâncias EC2");
      } finally {
        setLoading(false);
      }
    },
    [profile, region]
  );

  useEffect(() => {
    fetchInstances();
  }, [fetchInstances]);

  return { instances, loading, error, refetch: fetchInstances };
}
