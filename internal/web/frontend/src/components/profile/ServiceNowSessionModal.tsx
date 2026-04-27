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
  Monitor,
  KeyRound,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
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

interface BrowserEnv {
  is_wsl: boolean;
  has_display: boolean;
  xvfb_installed: boolean;
  xvfb_hint: string;
  sso_login_identifier?: string; // "email" ou "matricula"
}

export function ServiceNowSessionModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [sessionStatus, setSessionStatus] = useState<SessionStatus | null>(null);
  const [browserEnv, setBrowserEnv] = useState<BrowserEnv | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isClearing, setIsClearing] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [ssoLoginIdentifier, setSsoLoginIdentifier] = useState<'email' | 'matricula'>('email');
  const [isSavingIdentifier, setIsSavingIdentifier] = useState(false);
  const [ssoProfileConfigured, setSsoProfileConfigured] = useState(false);

  useEffect(() => {
    if (open) {
      fetchSessionStatus();
      fetchBrowserEnv();
      apiClient.getSSOProfile().then(p => setSsoProfileConfigured(p.configured)).catch(() => {});
    }
  }, [open]);

  const fetchBrowserEnv = async () => {
    try {
      const response = await fetch('/api/v1/servicenow/browser-config', {
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token")}` },
      });
      const data = await response.json();
      setBrowserEnv(data);
      if (data.sso_login_identifier) {
        setSsoLoginIdentifier(data.sso_login_identifier as 'email' | 'matricula');
      }
    } catch {
      // silencioso — info opcional
    }
  };

  const handleSaveIdentifier = async (identifier: 'email' | 'matricula') => {
    setSsoLoginIdentifier(identifier);
    setIsSavingIdentifier(true);
    try {
      await fetch('/api/v1/servicenow/browser-config', {
        method: 'PUT',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem("auth_token")}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ sso_login_identifier: identifier }),
      });
    } catch {
      // silencioso — falha não impede o uso
    } finally {
      setIsSavingIdentifier(false);
    }
  };

  const fetchSessionStatus = async () => {
    setIsLoading(true);
    try {
      const response = await fetch('/api/v1/servicenow/session-status', {
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token")}` },
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
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token")}` },
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
    } catch {
      toast.error('Erro ao limpar sessao');
    } finally {
      setIsClearing(false);
    }
  };

  const handleTestSession = async () => {
    setIsTesting(true);
    const usesXvfb = browserEnv?.is_wsl && !browserEnv?.has_display;
    toast.info('Abrindo Chromium para login...', {
      description: usesXvfb
        ? 'WSL sem display gráfico: Xvfb será usado. Para ver o browser instale WSLg (Windows 11) ou use x11vnc.'
        : 'Complete o login no Azure AD na janela que abrir.',
      duration: 12000,
    });

    try {
      const response = await fetch('/api/v1/servicenow/session/test', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token")}` },
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
    } catch {
      toast.error('Erro ao testar sessao');
    } finally {
      setIsTesting(false);
    }
  };

  const getStatusIcon = () => {
    if (!sessionStatus) return <Info className="h-4 w-4" />;
    switch (sessionStatus.status) {
      case 'valid': return <CheckCircle2 className="h-4 w-4 text-green-600" />;
      case 'expired': return <AlertTriangle className="h-4 w-4 text-yellow-600" />;
      default: return <XCircle className="h-4 w-4 text-muted-foreground" />;
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
            <AlertDescription className="text-green-800 dark:text-green-200">{sessionStatus.message}</AlertDescription>
          </Alert>
        );
      case 'expired':
        return (
          <Alert className="border-yellow-200 bg-yellow-50 dark:bg-yellow-950 dark:border-yellow-800">
            <AlertTriangle className="h-4 w-4 text-yellow-600" />
            <AlertTitle className="text-yellow-900 dark:text-yellow-100">Sessao Expirada</AlertTitle>
            <AlertDescription className="text-yellow-800 dark:text-yellow-200">{sessionStatus.message}</AlertDescription>
          </Alert>
        );
      default:
        return (
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>Sessao Nao Encontrada</AlertTitle>
            <AlertDescription>{sessionStatus.message}</AlertDescription>
          </Alert>
        );
    }
  };

  const formatHours = (hours: number) => {
    if (hours < 1) return `${Math.round(hours * 60)} minutos`;
    return `${hours.toFixed(1)} horas`;
  };

  const showXvfbWarning = browserEnv?.is_wsl && !browserEnv?.has_display;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto" onInteractOutside={(e) => e.preventDefault()}>
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
          {isLoading ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando sessao...</AlertDescription>
            </Alert>
          ) : (
            getStatusAlert()
          )}

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
                <div>
                  <span className="font-mono text-xs truncate max-w-[200px] block" title={sessionStatus.session_dir}>
                    {sessionStatus.session_dir}
                  </span>
                </div>
              </div>

              <Separator />

              <Alert>
                <Info className="h-4 w-4" />
                <AlertDescription className="text-xs">
                  A sessao e compartilhada com o Azure AD SSO. Sessoes tipicamente expiram apos 8-12 horas.
                  Ao limpar a sessao, um novo login sera necessario.
                </AlertDescription>
              </Alert>
            </div>
          )}

          {/* Auto-login SSO */}
          <Separator />
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <KeyRound className="h-4 w-4 text-blue-500" />
              <span className="text-sm font-medium">Auto-login SSO</span>
              {isSavingIdentifier && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
            </div>

            {!ssoProfileConfigured ? (
              <Alert>
                <Info className="h-4 w-4" />
                <AlertDescription className="text-xs">
                  <strong>Perfil SSO não configurado.</strong> Configure em{' '}
                  <strong>Credenciais → Perfil SSO corporativo</strong> para que o browser
                  preencha o Azure AD automaticamente no próximo login.
                </AlertDescription>
              </Alert>
            ) : (
              <>
                <p className="text-xs text-muted-foreground">
                  Perfil SSO ativo. Selecione qual identificador usar no formulário Azure AD:
                </p>
                <div className="flex flex-col gap-1.5">
                  {(['email', 'matricula'] as const).map((id) => (
                    <label key={id} className="flex items-center gap-2 text-sm cursor-pointer">
                      <input
                        type="radio"
                        name="sn-login-identifier"
                        value={id}
                        checked={ssoLoginIdentifier === id}
                        onChange={() => handleSaveIdentifier(id)}
                        className="accent-blue-500"
                        disabled={isSavingIdentifier}
                      />
                      <span>{id === 'matricula' ? 'Matrícula' : 'Email'}</span>
                    </label>
                  ))}
                </div>
              </>
            )}
          </div>

          {/* Aviso WSL sem display */}
          {showXvfbWarning && (
            <>
              <Separator />
              <Alert className={browserEnv.xvfb_installed
                ? 'border-blue-200 bg-blue-50 dark:bg-blue-950 dark:border-blue-800'
                : 'border-yellow-200 bg-yellow-50 dark:bg-yellow-950 dark:border-yellow-800'
              }>
                <Monitor className="h-4 w-4" />
                <AlertTitle className="text-sm">
                  {browserEnv.xvfb_installed ? 'WSL — Display Virtual (Xvfb)' : 'WSL — Sem Display Grafico'}
                </AlertTitle>
                <AlertDescription className="text-xs space-y-1">
                  {browserEnv.xvfb_installed ? (
                    <>
                      <p>O Chromium abrira no display virtual Xvfb <code className="bg-muted px-1 rounded">:99</code>. O browser e invisivel por padrao.</p>
                      <p>Para visualizar: instale <strong>WSLg</strong> (Windows 11) ou use <code className="bg-muted px-1 rounded">x11vnc -display :99 -forever</code>.</p>
                      <p>O SSO corporativo pode autenticar silenciosamente sem interacao visual.</p>
                    </>
                  ) : (
                    <>
                      <p>Xvfb nao instalado. Instale para habilitar o browser no WSL:</p>
                      <code className="block bg-muted px-2 py-1 rounded mt-1">{browserEnv.xvfb_hint}</code>
                    </>
                  )}
                </AlertDescription>
              </Alert>
            </>
          )}
        </div>

        <DialogFooter className="flex flex-wrap justify-between gap-y-2 sm:justify-between">
          <div className="flex gap-2">
            {sessionStatus?.exists && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleClearSession}
                disabled={isClearing || isTesting}
                className="text-destructive hover:text-destructive"
              >
                {isClearing ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Trash2 className="h-4 w-4 mr-1" />}
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
