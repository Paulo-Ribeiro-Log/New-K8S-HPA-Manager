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
  /** Intervalo de polling em milissegundos (padrão: 60000 = 1 minuto) */
  pollingInterval?: number;
  /** Se deve verificar VPN imediatamente ao montar (padrão: true) */
  checkOnMount?: boolean;
  /** Não usado — mantido para compatibilidade */
  showToastOnDisconnect?: boolean;
}

export function useVPNMonitor(options: UseVPNMonitorOptions = {}) {
  const {
    cluster,
    pollingInterval = 60000,
    checkOnMount = true,
  } = options;

  const [isConnected, setIsConnected] = useState<boolean>(true);
  const [isChecking, setIsChecking] = useState<boolean>(false);
  const [lastCheck, setLastCheck] = useState<Date | null>(null);
  const [lastStatus, setLastStatus] = useState<VPNStatus | null>(null);

  const checkInProgressRef = useRef<boolean>(false);

  const checkVPN = useCallback(async (overrideCluster?: string): Promise<boolean> => {
    if (checkInProgressRef.current) return isConnected;

    const target = overrideCluster ?? cluster;

    // Sem cluster selecionado — não mostrar banner
    if (!target) {
      setIsConnected(true);
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
      return data.connected;
    } catch {
      setIsConnected(false);
      return false;
    } finally {
      setIsChecking(false);
      checkInProgressRef.current = false;
    }
  }, [cluster, isConnected]);

  // Re-verificar quando o cluster mudar
  useEffect(() => {
    if (cluster) checkVPN();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cluster]);

  // Polling periódico
  useEffect(() => {
    if (checkOnMount && cluster) checkVPN();

    const intervalId = setInterval(() => {
      if (cluster) checkVPN();
    }, pollingInterval);

    return () => clearInterval(intervalId);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pollingInterval, checkOnMount, cluster]);

  return {
    isConnected,
    isChecking,
    lastCheck,
    lastStatus,
    checkVPN,
  };
}
