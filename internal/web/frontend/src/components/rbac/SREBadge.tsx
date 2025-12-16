import { Shield, ShieldAlert, Loader2 } from 'lucide-react';
import { useUserPermissions, useRefreshPermissions } from '@/hooks/useUserPermissions';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { RefreshCw } from 'lucide-react';

/**
 * Badge que exibe status SRE do usuário atual
 * Útil para exibir no header da aplicação
 */
export function SREBadge() {
  const { data: permissions, isLoading } = useUserPermissions();
  const refreshMutation = useRefreshPermissions();

  if (isLoading) {
    return (
      <Badge variant="outline" className="gap-2">
        <Loader2 className="h-3 w-3 animate-spin" />
        Carregando...
      </Badge>
    );
  }

  if (!permissions) {
    return null;
  }

  return (
    <TooltipProvider>
      <Popover>
        <PopoverTrigger asChild>
          <div>
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge
                  variant={permissions.isSRE ? 'default' : 'secondary'}
                  className="gap-2 cursor-pointer hover:opacity-80 transition-opacity"
                >
                  {permissions.isSRE ? (
                    <Shield className="h-3 w-3" />
                  ) : (
                    <ShieldAlert className="h-3 w-3" />
                  )}
                  {permissions.isSRE ? 'SRE' : 'Read-Only'}
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                <p className="text-xs">
                  {permissions.isSRE
                    ? 'Você tem permissões de SRE'
                    : 'Você tem acesso somente leitura'}
                </p>
              </TooltipContent>
            </Tooltip>
          </div>
        </PopoverTrigger>
        <PopoverContent className="w-80" align="end">
          <div className="space-y-3">
            <div className="space-y-1">
              <h4 className="text-sm font-semibold">Permissões do Usuário</h4>
              <p className="text-xs text-muted-foreground">{permissions.email}</p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Status SRE:</span>
                <Badge variant={permissions.isSRE ? 'default' : 'secondary'}>
                  {permissions.isSRE ? 'Habilitado' : 'Desabilitado'}
                </Badge>
              </div>

              <div className="space-y-1">
                <span className="text-xs font-medium">Grupos do Azure AD:</span>
                <div className="max-h-32 overflow-y-auto space-y-1">
                  {permissions.groups.slice(0, 5).map((group) => (
                    <div
                      key={group.id}
                      className="text-xs text-muted-foreground bg-muted px-2 py-1 rounded"
                    >
                      {group.displayName}
                    </div>
                  ))}
                  {permissions.groups.length > 5 && (
                    <p className="text-xs text-muted-foreground italic">
                      +{permissions.groups.length - 5} outros grupos
                    </p>
                  )}
                </div>
              </div>
            </div>

            <Button
              size="sm"
              variant="outline"
              className="w-full"
              onClick={() => refreshMutation.mutate()}
              disabled={refreshMutation.isPending}
            >
              {refreshMutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                  Atualizando...
                </>
              ) : (
                <>
                  <RefreshCw className="mr-2 h-3 w-3" />
                  Atualizar Permissões
                </>
              )}
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </TooltipProvider>
  );
}
