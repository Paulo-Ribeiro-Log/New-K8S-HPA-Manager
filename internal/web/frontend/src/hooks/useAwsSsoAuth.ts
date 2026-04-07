import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";

/**
 * Infere o perfil AWS a partir do nome do cluster EKS.
 * Replica a lógica de inferAWSProfileFromEKSUserName do backend.
 * Exemplos:
 *   "arn:aws:eks:us-east-1:123456789:cluster/asaplog-staging-mandalore" → "asaplog"
 *   "asaplog-staging-tatooine-admin" → "asaplog"
 *   "asaplog-preprod"                → "asaplog"
 *   "asapops"                        → "asapops"
 */
export function inferAWSProfile(clusterName: string): string {
  // Extrair nome curto de ARNs EKS: arn:aws:eks:REGION:ACCOUNT:cluster/NAME → NAME
  let name = clusterName;
  if (name.startsWith("arn:aws:")) {
    const slashIdx = name.lastIndexOf("/");
    if (slashIdx >= 0) name = name.slice(slashIdx + 1);
  }
  // Remover sufixo -admin
  name = name.replace(/-admin$/, "");
  const suffixes = ["-staging-", "-preprod", "-production", "-debezium"];
  for (const suffix of suffixes) {
    const idx = name.indexOf(suffix);
    if (idx > 0) return name.slice(0, idx);
  }
  return name;
}

export interface AwsSsoState {
  open: boolean;
  profile: string;
  /** URL de autorização retornada pelo backend */
  url: string;
  /** Código exibido ao usuário (ex: ABCD-EFGH) */
  userCode: string;
  /** true enquanto aguarda o backend iniciar o processo */
  loading: boolean;
  /** true enquanto polling aguarda o usuário concluir no browser */
  polling: boolean;
  /** Mensagem de erro, se houver */
  error: string;
  /** true quando login foi concluído com sucesso */
  success: boolean;
  /** true se o perfil ainda não está configurado */
  needsConfig: boolean;
}

const INITIAL: AwsSsoState = {
  open: false,
  profile: "",
  url: "",
  userCode: "",
  loading: false,
  polling: false,
  error: "",
  success: false,
  needsConfig: false,
};

export function useAwsSsoAuth() {
  const [state, setState] = useState<AwsSsoState>(INITIAL);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  // Escuta o evento global disparado pelo apiClient quando detecta erro de token expirado.
  useEffect(() => {
    const handler = (e: Event) => {
      const { profile } = (e as CustomEvent<{ profile: string }>).detail;
      triggerLogin(profile);
    };
    window.addEventListener("aws-sso-token-expired", handler);
    return () => window.removeEventListener("aws-sso-token-expired", handler);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Cleanup do polling ao desmontar.
  useEffect(() => () => stopPolling(), [stopPolling]);

  const startPolling = useCallback((profile: string) => {
    stopPolling();
    pollRef.current = setInterval(async () => {
      try {
        const res = await apiClient.pollAwsSsoLogin(profile);
        if (res.done) {
          stopPolling();
          if (res.success) {
            setState((s) => ({ ...s, polling: false, success: true }));
            // Notificar outros componentes para re-buscar dados do cluster EKS
            window.dispatchEvent(new CustomEvent("aws-sso-login-success"));
            // Fechar automaticamente após 2s
            setTimeout(() => setState(INITIAL), 2000);
          } else {
            const detail = res.error_detail || "Login não concluído. Verifique se a VPN AWS está ativa e tente novamente.";
            setState((s) => ({ ...s, polling: false, error: detail }));
          }
        }
      } catch {
        // Ignorar erros de polling — tentar novamente no próximo tick
      }
    }, 3000);
  }, [stopPolling]);

  /**
   * Inicia o fluxo de login SSO para o perfil dado.
   * Chamado automaticamente pelo evento global, ou manualmente pelo componente.
   */
  const triggerLogin = useCallback(async (profile: string) => {
    // Evitar abrir múltiplas vezes para o mesmo perfil
    setState((s) => {
      if (s.open && s.profile === profile) return s;
      return { ...INITIAL, open: true, profile, loading: true };
    });

    try {
      const res = await apiClient.startAwsSsoLogin(profile);

      if (res.already_valid) {
        setState(INITIAL);
        return;
      }

      if (!res.url) {
        // Perfil não configurado em ~/.aws/config — exibir formulário de config
        setState((s) => ({ ...s, loading: false, needsConfig: true }));
        return;
      }

      setState((s) => ({
        ...s,
        loading: false,
        polling: true,
        url: res.url!,
        userCode: res.user_code ?? "",
      }));

      startPolling(profile);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Erro ao iniciar login AWS SSO";
      // Se o erro menciona "não configurado" ou perfil inexistente, mostrar formulário
      const needsConfig = msg.includes("no such file") || msg.includes("profile") || msg.includes("does not exist");
      setState((s) => ({ ...s, loading: false, error: needsConfig ? "" : msg, needsConfig }));
    }
  }, [startPolling]);

  /**
   * Chamado após o usuário salvar a config SSO — tenta login novamente.
   */
  const retryAfterConfig = useCallback(async (profile: string) => {
    setState((s) => ({ ...s, needsConfig: false, loading: true, error: "" }));
    try {
      const res = await apiClient.startAwsSsoLogin(profile);
      if (res.already_valid) { setState(INITIAL); return; }
      if (!res.url) {
        setState((s) => ({ ...s, loading: false, error: "Não foi possível iniciar o login. Verifique a configuração." }));
        return;
      }
      setState((s) => ({ ...s, loading: false, polling: true, url: res.url!, userCode: res.user_code ?? "" }));
      startPolling(profile);
    } catch (err: unknown) {
      setState((s) => ({ ...s, loading: false, error: err instanceof Error ? err.message : "Erro ao iniciar login" }));
    }
  }, [startPolling]);

  const close = useCallback(() => {
    stopPolling();
    setState(INITIAL);
  }, [stopPolling]);

  /**
   * Verifica proativamente se o token está válido para um cluster EKS.
   * Se inválido, abre o dialog de login automaticamente.
   * Deve ser chamado ao trocar para um cluster EKS.
   */
  const checkForCluster = useCallback(async (clusterName: string) => {
    const profile = inferAWSProfile(clusterName);
    if (!profile) return;
    try {
      const res = await apiClient.checkAwsSsoStatus(profile);
      if (!res.valid && !res.login_in_progress) {
        triggerLogin(profile);
      }
    } catch {
      // Se a chamada de status falhar, não bloquear o usuário
    }
  }, [triggerLogin]);

  return { state, triggerLogin, checkForCluster, retryAfterConfig, close };
}
