import { useCallback, useEffect, useRef, useState } from "react";
import { apiClient } from "@/lib/api/client";

export interface GcpSsoState {
  open: boolean;
  cluster: string;
  /** URL real de autenticação do Google, capturada da saída de `gcloud auth login` rodado como
   *  subprocesso no backend (ver internal/cloudprovider/gcp/auth.go) — mesma URL que apareceria
   *  rodando o comando manualmente num terminal sem browser. */
  url: string;
  /** true enquanto aguarda o backend iniciar o processo */
  loading: boolean;
  /** true enquanto polling aguarda o usuário concluir no browser (o gcloud completa sozinho via
   *  seu próprio listener loopback assim que o redirect chega — não há código pra digitar) */
  polling: boolean;
  /** Mensagem de erro, se houver */
  error: string;
  /** true quando login foi concluído com sucesso */
  success: boolean;
}

const INITIAL: GcpSsoState = {
  open: false,
  cluster: "",
  url: "",
  loading: false,
  polling: false,
  error: "",
  success: false,
};

/**
 * Equivalente GCP do useAwsSsoAuth.ts — verifica proativamente se a autenticação GCP ainda é
 * válida ao trocar para um cluster GKE, e abre o fluxo de login automaticamente quando não está.
 * Antes desta implementação, clusters GKE não tinham NENHUMA verificação proativa equivalente —
 * o usuário só descobria que precisava reautenticar quando uma chamada ao cluster já tinha
 * falhado silenciosamente em algum outro lugar da UI.
 *
 * Diferença chave em relação ao AWS: não há conceito de "perfil" — a autenticação GCP é global ao
 * servidor (uma única conta ativa no `gcloud` local), não por cluster/conta, então não existe um
 * equivalente a retryAfterConfig/needsConfig aqui.
 */
export function useGcpSsoAuth() {
  const [state, setState] = useState<GcpSsoState>(INITIAL);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => () => stopPolling(), [stopPolling]);

  const startPolling = useCallback((sessionId: string, intervalSec: number) => {
    stopPolling();
    pollRef.current = setInterval(async () => {
      try {
        const res = await apiClient.pollGcpLogin(sessionId);
        if (res.done) {
          stopPolling();
          if (res.success) {
            setState((s) => ({ ...s, polling: false, success: true }));
            window.dispatchEvent(new CustomEvent("gcp-sso-login-success"));
            // Fechar automaticamente após 2s — mesmo padrão do AWS SSO
            setTimeout(() => setState(INITIAL), 2000);
          } else {
            setState((s) => ({
              ...s,
              polling: false,
              // Mensagem real do backend quando disponível (ex: "login cancelado ou falhou: ...")
              // — mais útil que um texto genérico pra diferenciar cancelamento de timeout de rede.
              error: res.error || "Autenticação GCP falhou ou expirou. Verifique se a VPN/rede está OK e tente novamente.",
            }));
          }
        }
      } catch {
        // Ignorar erros de polling — tentar novamente no próximo tick
      }
    }, Math.max(intervalSec || 5, 3) * 1000);
  }, [stopPolling]);

  /**
   * Inicia (ou reinicia) o login (`gcloud auth login` via backend) pro cluster dado.
   * Chamado automaticamente por checkForCluster, ou manualmente pelo botão "Tentar novamente"
   * do GcpAuthDialog quando um erro anterior precisa ser reexecutado do zero.
   */
  const triggerLogin = useCallback(async (clusterName: string) => {
    setState((s) => {
      if (s.open && s.cluster === clusterName && (s.loading || s.polling)) return s;
      return { ...INITIAL, open: true, cluster: clusterName, loading: true };
    });

    try {
      const res = await apiClient.startGcpLogin();

      // Sessão GCP já válida — não abrir o dialog nem iniciar polling (mesmo tratamento do
      // useAwsSsoAuth para already_valid). Sem isso, uma checagem reativa disparada por engano
      // (ex: erro transitório de rede, ou uma segunda chamada em voo com o token antigo logo
      // após o usuário já ter reautenticado) reabriria o modal mesmo estando tudo certo.
      if (res.already_valid || !res.session_id || !res.verify_url) {
        setState(INITIAL);
        return;
      }

      setState((s) => ({
        ...s,
        loading: false,
        polling: true,
        url: res.verify_url!,
      }));
      startPolling(res.session_id, res.interval_sec ?? 5);
    } catch (err: unknown) {
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : "Erro ao iniciar autenticação GCP",
      }));
    }
  }, [startPolling]);

  /**
   * Verifica proativamente se a autenticação GCP ainda é válida. Se não estiver, abre o dialog de
   * login automaticamente. Deve ser chamado ao trocar para um cluster GKE (mesmo padrão de
   * checkForCluster do AWS SSO).
   */
  const checkForCluster = useCallback(async (clusterName: string) => {
    try {
      const status = await apiClient.getGcpAuthStatus();
      if (!status.authenticated) {
        triggerLogin(clusterName);
      }
    } catch {
      // Se a checagem falhar, não bloquear o usuário
    }
  }, [triggerLogin]);

  const close = useCallback(() => {
    stopPolling();
    setState(INITIAL);
  }, [stopPolling]);

  return { state, triggerLogin, checkForCluster, close };
}
