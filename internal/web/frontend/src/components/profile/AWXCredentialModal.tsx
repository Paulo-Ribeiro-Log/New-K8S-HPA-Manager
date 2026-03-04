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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
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
  ExternalLink,
  ShieldCheck,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
import type { AWXStatus } from '@/lib/api/types';
import type { CredentialModalProps } from '@/types/profile';

const AWX_LOGIN_URL = 'https://awx.via.com.br/#/login';

export function AWXCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [status, setStatus] = useState<AWXStatus | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Campos do formulário
  const [baseURL, setBaseURL] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  useEffect(() => {
    if (open) {
      fetchStatus();
    }
  }, [open]);

  const fetchStatus = async () => {
    setIsLoading(true);
    try {
      const s = await apiClient.getAWXStatus();
      setStatus(s);
      if (s.configured) {
        if (s.base_url) setBaseURL(s.base_url);
        if (s.username) setUsername(s.username);
        setPassword(''); // não preenche senha por segurança
      }
    } catch {
      setStatus({ configured: false, reachable: false });
    } finally {
      setIsLoading(false);
    }
  };

  const handleTest = async () => {
    if (!baseURL.trim() || !username.trim()) {
      toast.error('Preencha URL e usuário antes de testar');
      return;
    }
    if (!password && !status?.configured) {
      toast.error('Preencha a senha para testar');
      return;
    }

    setIsTesting(true);
    try {
      // Salva temporariamente para testar
      await apiClient.saveAWXCredentials(baseURL.trim(), username.trim(), password || '');
      const s = await apiClient.getAWXStatus();
      setStatus(s);
      if (s.reachable) {
        toast.success(`AWX acessível${s.version ? ` (v${s.version})` : ''}`);
        onSaved?.();
      } else {
        toast.error(s.error || 'AWX inacessível — verifique URL e credenciais');
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao testar conexão');
    } finally {
      setIsTesting(false);
    }
  };

  const handleSave = async () => {
    if (!baseURL.trim() || !username.trim()) {
      toast.error('URL e usuário são obrigatórios');
      return;
    }
    if (!password && !status?.configured) {
      toast.error('Senha é obrigatória na primeira configuração');
      return;
    }

    setIsSaving(true);
    try {
      await apiClient.saveAWXCredentials(baseURL.trim(), username.trim(), password || '');
      toast.success('Credenciais AWX salvas com sucesso!');
      setPassword('');
      onSaved?.();
      fetchStatus();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao salvar credenciais');
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Remover as credenciais AWX salvas?')) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteAWXCredentials();
      setStatus({ configured: false, reachable: false });
      setBaseURL('');
      setUsername('');
      setPassword('');
      toast.success('Credenciais AWX removidas');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao remover credenciais');
    } finally {
      setIsDeleting(false);
    }
  };

  const isProcessing = isLoading || isTesting || isSaving || isDeleting;

  const getStatusAlert = () => {
    if (isLoading) {
      return (
        <Alert>
          <Loader2 className="h-4 w-4 animate-spin" />
          <AlertDescription>Verificando conexão com o AWX...</AlertDescription>
        </Alert>
      );
    }
    if (!status) return null;

    if (!status.configured) {
      return (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertTitle>Não configurado</AlertTitle>
          <AlertDescription>
            Informe a URL, usuário e senha para conectar ao AWX.
          </AlertDescription>
        </Alert>
      );
    }
    if (status.configured && status.reachable) {
      return (
        <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
          <CheckCircle2 className="h-4 w-4 text-green-600" />
          <AlertTitle className="text-green-900 dark:text-green-100">Conectado</AlertTitle>
          <AlertDescription className="text-green-800 dark:text-green-200">
            AWX acessível{status.version ? ` — versão ${status.version}` : ''}.
            Usuário: <strong>{status.username}</strong>
          </AlertDescription>
        </Alert>
      );
    }
    // configurado mas inacessível
    return (
      <Alert className="border-yellow-200 bg-yellow-50 dark:bg-yellow-950 dark:border-yellow-800">
        <AlertTriangle className="h-4 w-4 text-yellow-600" />
        <AlertTitle className="text-yellow-900 dark:text-yellow-100">Inacessível</AlertTitle>
        <AlertDescription className="text-yellow-800 dark:text-yellow-200">
          {status.error || 'Não foi possível conectar ao AWX. Verifique a VPN e as credenciais.'}
        </AlertDescription>
      </Alert>
    );
  };

  const getStatusBadge = () => {
    if (!status || !status.configured) return <Badge variant="secondary">Não configurado</Badge>;
    if (status.reachable) return <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100">Conectado</Badge>;
    return <Badge className="bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-100">Inacessível</Badge>;
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-5 w-5 text-blue-500" />
            AWX / Ansible Tower
          </DialogTitle>
          <DialogDescription>
            Configure suas credenciais para acessar o AWX e gerenciar certificados TLS nos clusters.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Status atual */}
          {getStatusAlert()}

          {/* Detalhes do status */}
          {status && !isLoading && (
            <div className="grid grid-cols-2 gap-2 text-sm">
              <span className="text-muted-foreground flex items-center gap-1">
                {status.configured && status.reachable
                  ? <CheckCircle2 className="h-3.5 w-3.5 text-green-600" />
                  : status.configured
                  ? <AlertTriangle className="h-3.5 w-3.5 text-yellow-600" />
                  : <XCircle className="h-3.5 w-3.5 text-muted-foreground" />
                }
                Status:
              </span>
              <div>{getStatusBadge()}</div>

              {status.configured && status.base_url && (
                <>
                  <span className="text-muted-foreground">URL:</span>
                  <span className="font-mono text-xs truncate" title={status.base_url}>{status.base_url}</span>
                </>
              )}
            </div>
          )}

          <Separator />

          {/* Formulário */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="awx-url">URL do AWX</Label>
              <div className="flex gap-2">
                <Input
                  id="awx-url"
                  value={baseURL}
                  onChange={(e) => {
                    // Remove fragmento (#/login etc.) automaticamente
                    const v = e.target.value;
                    const idx = v.indexOf('#');
                    setBaseURL(idx !== -1 ? v.slice(0, idx).trimEnd() : v);
                  }}
                  placeholder="https://awx.via.com.br"
                  disabled={isProcessing}
                  className="flex-1"
                />
                <Button
                  variant="outline"
                  size="icon"
                  title="Abrir AWX no navegador"
                  onClick={() => window.open(AWX_LOGIN_URL, '_blank')}
                  disabled={isProcessing}
                >
                  <ExternalLink className="h-4 w-4" />
                </Button>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="awx-username">Usuário</Label>
                <Input
                  id="awx-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="seu.usuario"
                  disabled={isProcessing}
                  autoComplete="username"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="awx-password">
                  Senha
                  {status?.configured && (
                    <span className="ml-1 text-xs text-muted-foreground">(deixe em branco para manter)</span>
                  )}
                </Label>
                <Input
                  id="awx-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={status?.configured ? '••••••••' : 'Sua senha'}
                  disabled={isProcessing}
                  autoComplete="current-password"
                />
              </div>
            </div>
          </div>

          {/* Info */}
          <Alert>
            <Info className="h-4 w-4" />
            <AlertDescription className="text-xs">
              Use o mesmo usuário e senha do seu login corporativo (SSO).
              Clique em <strong>Abrir AWX</strong> <ExternalLink className="inline h-3 w-3" /> para acessar a interface web.
            </AlertDescription>
          </Alert>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={fetchStatus}
              disabled={isProcessing}
            >
              <RefreshCw className={`h-4 w-4 mr-1 ${isLoading ? 'animate-spin' : ''}`} />
              Atualizar
            </Button>
            {status?.configured && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleDelete}
                disabled={isProcessing}
                className="text-destructive hover:text-destructive"
              >
                {isDeleting
                  ? <Loader2 className="h-4 w-4 animate-spin" />
                  : <Trash2 className="h-4 w-4" />
                }
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={handleTest} disabled={isProcessing}>
              {isTesting ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : null}
              Testar
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isProcessing}>
              {isSaving ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : null}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
