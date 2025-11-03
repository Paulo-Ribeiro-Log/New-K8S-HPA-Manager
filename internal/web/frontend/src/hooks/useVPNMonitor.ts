import { useEffect, useState, useCallback, useRef } from 'react';
import { toast } from 'sonner';

interface VPNStatus {
  connected: boolean;
  message: string;
  timestamp: number;
}

interface UseVPNMonitorOptions {
  /** Intervalo de polling em milissegundos (padrão: 60000 = 1 minuto) */
  pollingInterval?: number;
  /** Se deve exibir toast ao detectar desconexão (padrão: true) */
  showToastOnDisconnect?: boolean;
  /** Se deve verificar VPN imediatamente ao montar (padrão: true) */
  checkOnMount?: boolean;
}

/**
 * Hook para monitoramento contínuo de VPN
 *
 * Funcionalidades:
 * - Polling periódico do status VPN (padrão: 1 minuto)
 * - Verificação on-demand via checkVPN()
 * - Toast notification ao detectar desconexão
 * - Previne múltiplas verificações simultâneas
 *
 * @example
 * ```typescript
 * const { isConnected, isChecking, checkVPN, lastCheck } = useVPNMonitor({
 *   pollingInterval: 60000, // 1 minuto
 *   showToastOnDisconnect: true
 * });
 *
 * // Verificar antes de operação crítica
 * const handleApplyChanges = async () => {
 *   const connected = await checkVPN();
 *   if (!connected) {
 *     toast.error("VPN desconectada. Conecte-se e tente novamente.");
 *     return;
 *   }
 *   // Prosseguir com operação...
 * };
 * ```
 */
export function useVPNMonitor(options: UseVPNMonitorOptions = {}) {
  const {
    pollingInterval = 60000, // 1 minuto
    showToastOnDisconnect = true,
    checkOnMount = true,
  } = options;

  const [isConnected, setIsConnected] = useState<boolean>(true);
  const [isChecking, setIsChecking] = useState<boolean>(false);
  const [lastCheck, setLastCheck] = useState<Date | null>(null);
  const [lastStatus, setLastStatus] = useState<VPNStatus | null>(null);

  // Ref para prevenir múltiplas verificações simultâneas
  const checkInProgressRef = useRef<boolean>(false);

  // Ref para armazenar se já exibimos toast (evitar spam)
  const toastShownRef = useRef<boolean>(false);

  /**
   * Verifica status VPN fazendo chamada à API
   * Retorna true se conectado, false se desconectado
   */
  const checkVPN = useCallback(async (): Promise<boolean> => {
    // Prevenir múltiplas verificações simultâneas
    if (checkInProgressRef.current) {
      console.log('[VPN Monitor] Verificação já em andamento, aguardando...');
      return isConnected;
    }

    checkInProgressRef.current = true;
    setIsChecking(true);

    try {
      const response = await fetch('/api/v1/vpn/status', {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
        },
      });

      const data: VPNStatus = await response.json();
      const now = new Date();

      setLastCheck(now);
      setLastStatus(data);
      setIsConnected(data.connected);

      // Log status change
      if (data.connected !== isConnected) {
        console.log(`[VPN Monitor] Status mudou: ${isConnected ? 'Conectado' : 'Desconectado'} → ${data.connected ? 'Conectado' : 'Desconectado'}`);
      }

      // Exibir toast se desconectou
      if (!data.connected && showToastOnDisconnect && !toastShownRef.current) {
        toast.error('🔌 VPN Desconectada', {
          description: 'Conecte-se à VPN para continuar operando. Algumas funcionalidades podem não funcionar.',
          duration: 10000,
        });
        toastShownRef.current = true;
      }

      // Resetar flag de toast se reconectou
      if (data.connected && toastShownRef.current) {
        toast.success('✅ VPN Reconectada', {
          description: 'Conexão com Kubernetes restabelecida.',
          duration: 5000,
        });
        toastShownRef.current = false;
      }

      return data.connected;
    } catch (error) {
      console.error('[VPN Monitor] Erro ao verificar VPN:', error);

      // Em caso de erro, assumir desconectado
      setIsConnected(false);

      if (showToastOnDisconnect && !toastShownRef.current) {
        toast.error('🔌 Erro ao Verificar VPN', {
          description: 'Não foi possível verificar status da VPN. Conecte-se e tente novamente.',
          duration: 10000,
        });
        toastShownRef.current = true;
      }

      return false;
    } finally {
      setIsChecking(false);
      checkInProgressRef.current = false;
    }
  }, [isConnected, showToastOnDisconnect]);

  // Polling periódico
  useEffect(() => {
    // Verificar imediatamente ao montar se configurado
    if (checkOnMount) {
      checkVPN();
    }

    // Setup intervalo de polling
    const intervalId = setInterval(() => {
      checkVPN();
    }, pollingInterval);

    // Cleanup
    return () => {
      clearInterval(intervalId);
    };
  }, [checkVPN, pollingInterval, checkOnMount]);

  return {
    /** Status atual da conexão VPN */
    isConnected,
    /** Se está verificando VPN no momento */
    isChecking,
    /** Data/hora da última verificação */
    lastCheck,
    /** Dados completos do último status */
    lastStatus,
    /** Função para verificar VPN on-demand (retorna Promise<boolean>) */
    checkVPN,
  };
}
