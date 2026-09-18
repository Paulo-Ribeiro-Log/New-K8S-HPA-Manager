import { useCallback, useEffect, useState } from "react";
import { apiClient } from "@/lib/api/client";
import type { VMInstance, SSHCredentialProfile, VMSSMStatus } from "@/lib/api/types";

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

// useVMCredentialProfiles — perfis de credencial SSH do usuário logado (Fase 3). Escopados por
// user_email no backend — cada usuário só vê os próprios perfis.
export function useVMCredentialProfiles() {
  const [profiles, setProfiles] = useState<SSHCredentialProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchProfiles = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await apiClient.listVMCredentialProfiles();
      setProfiles(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha ao listar perfis de credencial SSH");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchProfiles();
  }, [fetchProfiles]);

  return { profiles, loading, error, refetch: fetchProfiles };
}

// useVMSSMStatus — disponibilidade de aws CLI + session-manager-plugin no servidor (Fase 5). Só
// pra popular um badge informativo — nunca gateia o botão "Conectar via SSM" em si.
export function useVMSSMStatus() {
  const [status, setStatus] = useState<VMSSMStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchStatus = useCallback(async () => {
    try {
      setLoading(true);
      const data = await apiClient.getVMSSMStatus();
      setStatus(data);
    } catch {
      setStatus(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  return { status, loading, refetch: fetchStatus };
}
