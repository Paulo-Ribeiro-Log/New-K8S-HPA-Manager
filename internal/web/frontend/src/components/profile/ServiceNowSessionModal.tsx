import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Info,
  Loader2,
  Trash2,
  RefreshCw,
  Globe,
  Clock,
  FolderOpen,
} from 'lucide-react';
import { toast } from 'sonner';
import type { CredentialModalProps } from '@/types/profile';

interface SessionStatus {
  exists: boolean;
  valid: boolean;
  status: 'valid' | 'expired' | 'not_found' | 'empty' | 'error';
  session_dir: string;
  last_modified?: string;
  hours_since_update?: number;
  message: string;
}

export function ServiceNowSessionModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [sessionStatus, setSessionStatus] = useState<SessionStatus | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isClearing, setIsClearing] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [forceWindowsBrowser, setForceWindowsBrowser] = useState(false);
  const [wslAutoMode, setWslAutoMode] = useState(false);
  const [savingBrowserConfig, setSavingBrowserConfig] = useState(false);

  // Carregar status ao abrir modal
  useEffect(() => {
    if (open) {
      fetchSessionStatus();
      fetchBrowserConfig();
    }
  }, [open]);

  const fetchBrowserConfig = async () => {
    try {
      const response = await fetch('/api/v1/servicenow/browser-config', {
        headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}` },
      });
      const data = await response.json();
      setForceWindowsBrowser(data.force_windows_browser ?? false);
      setWslAutoMode(data.needs_windows_browser && !data.force_windows_browser);
    } catch {
      // silencioso — config opcional
    }
  };

  const handleToggleWindowsBrowser = async (value: boolean) => {
    setSavingBrowserConfig(true);
    try {
      await fetch('/api/v1/servicenow/browser-config', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ force_windows_browser: value }),
      });
      setForceWindowsBrowser(value);
      toast.success(
        value
          ? 'Modo Windows ativado. Chrome/Edge do Windows será usado para autenticar.'
          : 'Modo automático restaurado.'
      );
    } catch {
      toast.error('Erro ao salvar configuração de browser.');
    } finally {
      setSavingBrowserConfig(false);
    }
  };

  const fetchSessionStatus = async () => {
    setIsLoading(true);
    try {
      const response = await fetch('/api/v1/servicenow/session-status', {
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`,
        },
      });
      const data = await response.json();
      setSessionStatus(data);
    } catch (error) {
      console.error('Erro ao buscar status da sessao:', error);
      toast.error('Erro ao verificar status da sessao');
    } finally {
      setIsLoading(false);
    }
  };

  const handleClearSession = async () => {
    if (!confirm('Tem certeza que deseja limpar a sessao? Voce precisara fazer login novamente no Azure AD.')) {
      return;
    }

    setIsClearing(true);
    try {
      const response = await fetch('/api/v1/servicenow/session', {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`,
        },
      });
      const data = await response.json();

      if (data.success) {
        toast.success('Sessao limpa com sucesso', {
          description: 'Novo login sera necessario na proxima extracao.',
        });
        fetchSessionStatus();
        onSaved?.();
      } else {
        toast.error('Erro ao limpar sessao', { description: data.error });
      }
    } catch (error) {
      toast.error('Erro ao limpar sessao');
    } finally {
      setIsClearing(false);
    }
  };

  const handleTestSession = async () => {
    setIsTesting(true);
    const usingWindows = forceWindowsBrowser || wslAutoMode;
    toast.info(
      usingWindows ? 'Abrindo Chrome/Edge do Windows...' : 'Abrindo browser para login...',
      {
        description: usingWindows
          ? 'Uma janela do Chrome/Edge será aberta no Windows. Complete o login no Azure AD.'
          : 'Complete o login no Azure AD na janela que abrir.',
        duration: 10000,
      }
    );

    try {
      const response = await fetch('/api/v1/servicenow/session/test', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`,
        },
      });
      const data = await response.json();

      if (data.success) {
        toast.success('Login realizado com sucesso!', {
          description: 'Sessao salva para proximas extracoes.',
        });
      } else {
        toast.warning('Sessao pode nao estar completa', {
          description: 'Verifique se completou o login corretamente.',
        });
      }

      fetchSessionStatus();
      onSaved?.();
    } catch (error) {
      toast.error('Erro ao testar sessao');
    } finally {
      setIsTesting(false);
    }
  };

  const getStatusIcon = () => {
    if (!sessionStatus) return <Info className="h-4 w-4" />;

    switch (sessionStatus.status) {
      case 'valid':
        return <CheckCircle2 className="h-4 w-4 text-green-600" />;
      case 'expired':
        return <AlertTriangle className="h-4 w-4 text-yellow-600" />;
      case 'not_found':
      case 'empty':
        return <XCircle className="h-4 w-4 text-muted-foreground" />;
      default:
        return <XCircle className="h-4 w-4 text-red-600" />;
    }
  };

  const getStatusBadge = () => {
    if (!sessionStatus) return null;

    switch (sessionStatus.status) {
      case 'valid':
        return <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100">Valida</Badge>;
      case 'expired':
        return <Badge className="bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-100">Expirada</Badge>;
      case 'not_found':
        return <Badge variant="secondary">Nao encontrada</Badge>;
      case 'empty':
        return <Badge variant="secondary">Vazia</Badge>;
      default:
        return <Badge variant="destructive">Erro</Badge>;
    }
  };

  const getStatusAlert = () => {
    if (!sessionStatus) return null;

    switch (sessionStatus.status) {
      case 'valid':
        return (
          <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
            <CheckCircle2 className="h-4 w-4 text-green-600" />
            <AlertTitle className="text-green-900 dark:text-green-100">Sessao Valida</AlertTitle>
            <AlertDescription className="text-green-800 dark:text-green-200">
              {sessionStatus.message}
            </AlertDescription>
          </Alert>
        );
      case 'expired':
        return (
          <Alert className="border-yellow-200 bg-yellow-50 dark:bg-yellow-950 dark:border-yellow-800">
            <AlertTriangle className="h-4 w-4 text-yellow-600" />
            <AlertTitle className="text-yellow-900 dark:text-yellow-100">Sessao Expirada</AlertTitle>
            <AlertDescription className="text-yellow-800 dark:text-yellow-200">
              {sessionStatus.message}
            </AlertDescription>
          </Alert>
        );
      case 'not_found':
      case 'empty':
        return (
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>Sessao Nao Encontrada</AlertTitle>
            <AlertDescription>
              {sessionStatus.message}
            </AlertDescription>
          </Alert>
        );
      default:
        return (
          <Alert variant="destructive">
            <XCircle className="h-4 w-4" />
            <AlertTitle>Erro</AlertTitle>
            <AlertDescription>{sessionStatus.message}</AlertDescription>
          </Alert>
        );
    }
  };

  const formatHours = (hours: number) => {
    if (hours < 1) {
      return `${Math.round(hours * 60)} minutos`;
    }
    return `${hours.toFixed(1)} horas`;
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Globe className="h-5 w-5" />
            Sessao ServiceNow
          </DialogTitle>
          <DialogDescription>
            Gerencie sua sessao de autenticacao Azure AD para extrair dados do ServiceNow
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Status da Sessao */}
          {isLoading ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando sessao...</AlertDescription>
            </Alert>
          ) : (
            getStatusAlert()
          )}

          {/* Detalhes da Sessao */}
          {sessionStatus && !isLoading && (
            <div className="space-y-3 text-sm">
              <Separator />

              <div className="grid grid-cols-2 gap-2">
                <div className="flex items-center gap-2 text-muted-foreground">
                  {getStatusIcon()}
                  <span>Status:</span>
                </div>
                <div>{getStatusBadge()}</div>

                {sessionStatus.hours_since_update !== undefined && (
                  <>
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Clock className="h-4 w-4" />
                      <span>Ultima atualizacao:</span>
                    </div>
                    <div>{formatHours(sessionStatus.hours_since_update)} atras</div>
                  </>
                )}

                <div className="flex items-center gap-2 text-muted-foreground">
                  <FolderOpen className="h-4 w-4" />
                  <span>Diretorio:</span>
                </div>
                <div className="font-mono text-xs truncate" title={sessionStatus.session_dir}>
                  {sessionStatus.session_dir}
                </div>
              </div>

              <Separator />

              {/* Informacoes sobre o Azure AD */}
              <Alert>
                <Info className="h-4 w-4" />
                <AlertDescription className="text-xs">
                  A sessao e compartilhada com o Azure AD SSO. Sessoes tipicamente expiram apos 8-12 horas.
                  Ao limpar a sessao, um novo login sera necessario.
                </AlertDescription>
              </Alert>
            </div>
          )}

          {/* Toggle: Usar Chrome/Edge do Windows */}
          <Separator />
          <div className="flex items-center justify-between py-1">
            <div className="space-y-0.5">
              <p className="text-sm font-medium">Usar Chrome/Edge do Windows</p>
              <p className="text-xs text-muted-foreground">
                {wslAutoMode
                  ? 'Ativo automaticamente (WSL sem display gráfico)'
                  : forceWindowsBrowser
                  ? 'Ativo — Chrome/Edge Windows abre para autenticar (CDP porta 9223)'
                  : 'Usar o Chrome/Edge instalado no Windows em vez do Chromium local'}
              </p>
            </div>
            <button
              onClick={() => handleToggleWindowsBrowser(!forceWindowsBrowser)}
              disabled={savingBrowserConfig || wslAutoMode}
              title={wslAutoMode ? 'Ativado automaticamente no WSL sem display' : undefined}
              className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 ${
                forceWindowsBrowser || wslAutoMode ? 'bg-blue-600' : 'bg-input'
              }`}
              role="switch"
              aria-checked={forceWindowsBrowser || wslAutoMode}
            >
              <span
                className={`pointer-events-none block h-4 w-4 rounded-full bg-background shadow-lg ring-0 transition-transform ${
                  forceWindowsBrowser || wslAutoMode ? 'translate-x-4' : 'translate-x-0'
                }`}
              />
            </button>
          </div>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            {sessionStatus?.exists && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleClearSession}
                disabled={isClearing || isTesting}
                className="text-destructive hover:text-destructive"
              >
                {isClearing ? (
                  <Loader2 className="h-4 w-4 animate-spin mr-1" />
                ) : (
                  <Trash2 className="h-4 w-4 mr-1" />
                )}
                Limpar
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={fetchSessionStatus}
              disabled={isLoading || isTesting}
            >
              <RefreshCw className={`h-4 w-4 mr-1 ${isLoading ? 'animate-spin' : ''}`} />
              Atualizar
            </Button>

            <Button
              size="sm"
              onClick={handleTestSession}
              disabled={isTesting || isClearing}
            >
              {isTesting ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : (
                <Globe className="h-4 w-4 mr-2" />
              )}
              {isTesting ? 'Aguardando login...' : 'Fazer Login'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
