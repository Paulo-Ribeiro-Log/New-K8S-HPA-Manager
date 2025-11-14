import { useState, useEffect, useCallback } from "react";
import { apiClient } from "@/lib/api/client";

export function useVPNStatus() {
  const [isVPNConnected, setIsVPNConnected] = useState<boolean | null>(null);
  const [lastCheck, setLastCheck] = useState<Date>(new Date());
  const [isChecking, setIsChecking] = useState(false);

  const checkVPNConnection = useCallback(async () => {
    if (isChecking) return;

    setIsChecking(true);
    try {
      // Tentar buscar clusters para validar conectividade K8s
      const response = await apiClient.getClusters();

      // Se conseguiu buscar clusters e tem pelo menos 1, VPN está conectada
      if (response && response.length > 0) {
        setIsVPNConnected(true);
        setLastCheck(new Date());
        return true;
      } else {
        // Nenhum cluster encontrado - pode ser falta de autodiscover ou VPN
        setIsVPNConnected(false);
        setLastCheck(new Date());
        return false;
      }
    } catch (err) {
      // Erro ao conectar - VPN desconectada
      console.error("[VPN Check] Failed to connect:", err);
      setIsVPNConnected(false);
      setLastCheck(new Date());
      return false;
    } finally {
      setIsChecking(false);
    }
  }, [isChecking]);

  // Check inicial ao montar componente
  useEffect(() => {
    checkVPNConnection();
  }, []);

  // Re-check periódico a cada 30 segundos
  useEffect(() => {
    const interval = setInterval(() => {
      checkVPNConnection();
    }, 30000); // 30 segundos

    return () => clearInterval(interval);
  }, [checkVPNConnection]);

  // Listener para evento customizado de mudança de tab
  useEffect(() => {
    const handleTabChange = () => {
      console.log("[VPN Check] Tab changed - checking VPN status");
      checkVPNConnection();
    };

    window.addEventListener("tabChanged", handleTabChange);

    return () => {
      window.removeEventListener("tabChanged", handleTabChange);
    };
  }, [checkVPNConnection]);

  // Listener para evento customizado de ação do usuário
  useEffect(() => {
    const handleUserAction = () => {
      console.log("[VPN Check] User action detected - checking VPN status");
      checkVPNConnection();
    };

    window.addEventListener("userAction", handleUserAction);

    return () => {
      window.removeEventListener("userAction", handleUserAction);
    };
  }, [checkVPNConnection]);

  return {
    isVPNConnected,
    lastCheck,
    isChecking,
    recheckVPN: checkVPNConnection,
  };
}
