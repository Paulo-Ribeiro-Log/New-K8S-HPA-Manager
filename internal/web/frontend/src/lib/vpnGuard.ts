import { toast } from 'sonner';

/**
 * VPN Guard - Valida conectividade VPN antes de operações críticas
 *
 * @param checkVPN - Função de verificação VPN (do hook useVPNMonitor)
 * @param operationName - Nome da operação para mensagem de erro
 * @returns Promise<boolean> - true se VPN conectada, false caso contrário
 */
export async function guardVPNOperation(
  checkVPN: () => Promise<boolean>,
  operationName: string = 'operação'
): Promise<boolean> {
  console.log(`[VPN Guard] Verificando VPN antes de: ${operationName}`);

  const connected = await checkVPN();

  if (!connected) {
    console.error(`[VPN Guard] VPN desconectada - bloqueando ${operationName}`);
    toast.error('🔌 VPN Desconectada', {
      description: `Não é possível executar "${operationName}". Conecte-se à VPN e tente novamente.`,
      duration: 8000,
    });
    return false;
  }

  console.log(`[VPN Guard] VPN conectada - autorizando ${operationName}`);
  return true;
}

/**
 * Decorador para adicionar validação VPN em handlers
 *
 * @example
 * ```typescript
 * const handleApplyChanges = withVPNGuard(
 *   checkVPN,
 *   'Aplicar Alterações',
 *   async () => {
 *     // Lógica da operação...
 *   }
 * );
 * ```
 */
export function withVPNGuard<T extends (...args: any[]) => Promise<any>>(
  checkVPN: () => Promise<boolean>,
  operationName: string,
  handler: T
): T {
  return (async (...args: any[]) => {
    const connected = await guardVPNOperation(checkVPN, operationName);
    if (!connected) {
      return; // Bloquear operação
    }
    return handler(...args);
  }) as T;
}
