import { useEffect, useRef } from 'react';

/**
 * Hook para enviar sinais de heartbeat ao servidor
 * Mantém o servidor ativo enquanto há páginas abertas
 * 
 * Funcionamento:
 * - Envia POST /heartbeat a cada 5 minutos
 * - Servidor desliga automaticamente após 20 minutos sem heartbeat
 * - Limpa o intervalo quando o componente é desmontado
 */
export const useHeartbeat = () => {
  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const isActiveRef = useRef<boolean>(true);

  useEffect(() => {
    // Função para enviar heartbeat
    const sendHeartbeat = async () => {
      try {
        const response = await fetch('/heartbeat', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
        });

        if (response.ok) {
          const data = await response.json();
          console.log('💓 Heartbeat enviado:', data.last_heartbeat);
        } else {
          console.warn('⚠️  Heartbeat falhou:', response.status);
        }
      } catch (error) {
        console.error('❌ Erro ao enviar heartbeat:', error);
      }
    };

    // Enviar heartbeat imediatamente ao montar
    sendHeartbeat();

    // Configurar intervalo de 5 minutos (300000ms)
    intervalRef.current = setInterval(() => {
      if (isActiveRef.current) {
        sendHeartbeat();
      }
    }, 5 * 60 * 1000); // 5 minutos

    console.log('⏰ Heartbeat iniciado (intervalo: 5 minutos)');

    // Cleanup ao desmontar
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      isActiveRef.current = false;
      console.log('🛑 Heartbeat parado');
    };
  }, []); // Executa apenas uma vez ao montar

  return null;
};
