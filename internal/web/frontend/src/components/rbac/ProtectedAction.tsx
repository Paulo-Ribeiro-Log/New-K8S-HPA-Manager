import { ReactNode } from 'react';
import { useUserPermissions } from '@/hooks/useUserPermissions';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';
import { ShieldAlert } from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

interface ProtectedActionProps {
  children: ReactNode;
  fallback?: ReactNode;
  showWarning?: boolean;
  loadingComponent?: ReactNode;
}

/**
 * Componente que renderiza children apenas se o usuário for SRE
 * Caso contrário, exibe um fallback ou nada
 *
 * @example
 * // Botão protegido
 * <ProtectedAction>
 *   <Button onClick={handleDelete}>Delete</Button>
 * </ProtectedAction>
 *
 * @example
 * // Com fallback customizado
 * <ProtectedAction fallback={<Button disabled>SRE Only</Button>}>
 *   <Button onClick={handleApply}>Apply</Button>
 * </ProtectedAction>
 *
 * @example
 * // Ocultar completamente se não for SRE
 * <ProtectedAction showWarning={false}>
 *   <DangerousButton />
 * </ProtectedAction>
 */
export function ProtectedAction({
  children,
  fallback = null,
  showWarning = true,
  loadingComponent = <Skeleton className="h-10 w-24" />
}: ProtectedActionProps) {
  const { data: permissions, isLoading } = useUserPermissions();

  if (isLoading) {
    return <>{loadingComponent}</>;
  }

  if (!permissions?.isSRE) {
    if (showWarning) {
      return (
        <Button
          variant="outline"
          disabled
          onClick={() => {
            toast.error('Acesso negado', {
              description: 'Esta operação requer permissão de SRE (grupo VV_CLOUD_SRE)',
              icon: <ShieldAlert className="h-4 w-4" />,
            });
          }}
        >
          {fallback || children}
        </Button>
      );
    }
    return null;
  }

  return <>{children}</>;
}

interface ConditionalRenderProps {
  children: ReactNode;
  fallback?: ReactNode;
}

/**
 * Componente simples que renderiza children se SRE, senão fallback
 * Não adiciona wrappers nem botões desabilitados
 *
 * @example
 * <ConditionalRender fallback={<ReadOnlyView />}>
 *   <EditableView />
 * </ConditionalRender>
 */
export function ConditionalRender({
  children,
  fallback = null,
}: ConditionalRenderProps) {
  const { data: permissions, isLoading } = useUserPermissions();

  if (isLoading) {
    return null;
  }

  return <>{permissions?.isSRE ? children : fallback}</>;
}
