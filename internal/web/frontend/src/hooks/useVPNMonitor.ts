import { useEffect, useState, useCallback, useRef } from 'react';

interface VPNStatus {
  connected: boolean;
  message: string;
  timestamp: number;
  cloud_provider?: string;
}

interface UseVPNMonitorOptions {
  /** Cluster selecionado atualmente */
  cluster?: string;
  /** Intervalo de polling em milissegundos ENQUANTO conectado (padrão: 60000 = 1 minuto) */
  pollingInterval?: number;
  /**
   * Intervalo de polling em milissegundos ENQUANTO desconectado (padrão: 10000 = 10s) —
   * deliberadamente mais curto. Achado real (pedido do usuário: "faça um estudo para que o
   * travamento de vpn não aconteça mais... não existia para kubectl e nem para o K9S"):
   * `kubectl`/`k9s` nunca travam porque tentam a requisição real direto, com o timeout generoso
   * default do client-go (~30s) — só esta aplicação insere um pré-check próprio, e antes desta
   * correção, uma checagem que falhasse (um blip de rede de poucos segundos) deixava o banner
   * "VPN desconectada" travado até o PRÓXIMO poll do intervalo normal (2min, configurado em
   * Index.tsx) — uma extensão autoinfligida da indisponibilidade percebida, bem maior que
   * qualquer blip real. Com polling adaptativo, o pior caso passa a ser ~10s até o banner
   * limpar sozinho assim que a rede volta, não até 2 minutos.
   */
  reconnectPollingInterval?: number;
  /** Se deve verificar VPN imediatamente ao montar (padrão: true) */
  checkOnMount?: boolean;
  /** Não usado — mantido para compatibilidade */
  showToastOnDisconnect?: boolean;
}

export function useVPNMonitor(options: UseVPNMonitorOptions = {}) {
  const {
    cluster,
    pollingInterval = 60000,
    reconnectPollingInterval = 10000,
    checkOnMount = true,
  } = options;

  const [isConnected, setIsConnected] = useState<boolean>(true);
  const [isChecking, setIsChecking] = useState<boolean>(false);
  const [lastCheck, setLastCheck] = useState<Date | null>(null);
  const [lastStatus, setLastStatus] = useState<VPNStatus | null>(null);

  const checkInProgressRef = useRef<boolean>(false);
  // Espelha isConnected sem depender do closure do state (lido dentro do callback de
  // checkInProgressRef.current, que não pode reagir a re-renders).
  const isConnectedRef = useRef<boolean>(true);

  const checkVPN = useCallback(async (overrideCluster?: string): Promise<boolean> => {
    if (checkInProgressRef.current) return isConnectedRef.current;

    const target = overrideCluster ?? cluster;

    // Sem cluster selecionado — não mostrar banner
    if (!target) {
      setIsConnected(true);
      isConnectedRef.current = true;
      return true;
    }

    checkInProgressRef.current = true;
    setIsChecking(true);

    try {
      const response = await fetch(`/api/v1/vpn/status?cluster=${encodeURIComponent(target)}`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' },
      });

      const data: VPNStatus = await response.json();

      setLastCheck(new Date());
      setLastStatus(data);
      setIsConnected(data.connected);
      isConnectedRef.current = data.connected;
      return data.connected;
    } catch {
      setIsConnected(false);
      isConnectedRef.current = false;
      return false;
    } finally {
      setIsChecking(false);
      checkInProgressRef.current = false;
    }
  }, [cluster]);

  // Polling adaptativo — enquanto conectado, usa `pollingInterval` (ritmo normal, sem
  // necessidade de checar com frequência quando tudo está bem); assim que uma checagem falha,
  // passa a repetir a cada `reconnectPollingInterval` (bem mais curto) até reconectar, então
  // volta ao ritmo normal sozinho. setTimeout recursivo (não setInterval fixo) porque o
  // intervalo muda dinamicamente conforme o resultado da checagem anterior.
  useEffect(() => {
    if (!cluster) return;

    let cancelled = false;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;

    const scheduleNext = (connected: boolean) => {
      if (cancelled) return;
      const delay = connected ? pollingInterval : reconnectPollingInterval;
      timeoutId = setTimeout(async () => {
        if (cancelled) return;
        const connectedNow = await checkVPN();
        scheduleNext(connectedNow);
      }, delay);
    };

    if (checkOnMount) {
      checkVPN().then((connected) => {
        if (!cancelled) scheduleNext(connected);
      });
    } else {
      scheduleNext(isConnectedRef.current);
    }

    return () => {
      cancelled = true;
      if (timeoutId) clearTimeout(timeoutId);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster, pollingInterval, reconnectPollingInterval, checkOnMount]);

  return {
    isConnected,
    isChecking,
    lastCheck,
    lastStatus,
    checkVPN,
  };
}
