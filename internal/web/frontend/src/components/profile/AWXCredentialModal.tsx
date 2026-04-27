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
  KeyRound,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
import type { AWXStatus, SSOProfile } from '@/lib/api/types';
import type { CredentialModalProps } from '@/types/profile';

const AWX_LOGIN_URL = 'https://awx.via.com.br/#/login';

export function AWXCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [status, setStatus] = useState<AWXStatus | null>(null);
  const [ssoProfile, setSsoProfile] = useState<SSOProfile | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Campos do formulário
  const [baseURL, setBaseURL] = useState('');

  // Modo de login
  const [useSSOProfile, setUseSSOProfile] = useState(false);
  const [loginIdentifier, setLoginIdentifier] = useState<'email' | 'matricula'>('matricula');

  // Campos modo manual
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  useEffect(() => {
    if (open) {
      fetchStatus();
      fetchSSOProfile();
    }
  }, [open]);

  const fetchSSOProfile = async () => {
    try {
      const p = await apiClient.getSSOProfile();
      setSsoProfile(p);
    } catch {
      setSsoProfile({ configured: false });
    }
  };

  const fetchStatus = async () => {
    setIsLoading(true);
    try {
      const s = await apiClient.getAWXStatus();
      setStatus(s);
      if (s.configured || s.base_url) {
        if (s.base_url) setBaseURL(s.base_url);
        setUseSSOProfile(s.use_sso_profile ?? false);
        setLoginIdentifier((s.login_identifier as 'email' | 'matricula') ?? 'matricula');
        if (!s.use_sso_profile && s.username) {
          setUsername(s.username);
        }
        setPassword('');
      }
    } catch {
      setStatus({ configured: false, reachable: false });
    } finally {
      setIsLoading(false);
    }
  };

  const handleTest = async () => {
    if (!baseURL.trim()) {
      toast.error('Preencha a URL antes de testar');
      return;
    }
    if (!useSSOProfile && !username.trim()) {
      toast.error('Preencha o usuário antes de testar');
      return;
    }
    if (!useSSOProfile && !password && !status?.configured) {
      toast.error('Preencha a senha para testar');
      return;
    }
    if (useSSOProfile && !ssoProfile?.configured) {
      toast.error('Configure o Perfil SSO corporativo primeiro');
      return;
    }

    setIsTesting(true);
    try {
      // Salva temporariamente para testar
      if (useSSOProfile) {
        await apiClient.saveAWXCredentials(baseURL.trim(), { useSSOProfile: true, loginIdentifier });
      } else {
        await apiClient.saveAWXCredentials(baseURL.trim(), { username: username.trim(), password: password || '' });
      }
      const s = await apiClient.getAWXStatus();
      setStatus(s);
      if (s.reachable) {
        toast.success(`AWX acessível${s.version ? ` (v${s.version})` : ''}. Login: ${s.username}`);
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
    if (!baseURL.trim()) {
      toast.error('URL é obrigatória');
      return;
    }
    if (useSSOProfile && !ssoProfile?.configured) {
      toast.error('Configure o Perfil SSO corporativo primeiro (Credenciais → Perfil SSO)');
      return;
    }
    if (!useSSOProfile && !username.trim()) {
      toast.error('Usuário é obrigatório no modo manual');
      return;
    }
    if (!useSSOProfile && !password && !status?.configured) {
      toast.error('Senha é obrigatória na primeira configuração');
      return;
    }

    setIsSaving(true);
    try {
      if (useSSOProfile) {
        await apiClient.saveAWXCredentials(baseURL.trim(), { useSSOProfile: true, loginIdentifier });
      } else {
        await apiClient.saveAWXCredentials(baseURL.trim(), { username: username.trim(), password: password || '' });
      }
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
      setUseSSOProfile(false);
      toast.success('Credenciais AWX removidas');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao remover credenciais');
    } finally {
      setIsDeleting(false);
    }
  };

  const isProcessing = isLoading || isTesting || isSaving || isDeleting;

  // Username efetivo que será usado (para exibição)
  const effectiveUsername = useSSOProfile
    ? (loginIdentifier === 'email' ? ssoProfile?.email : ssoProfile?.matricula) ?? '—'
    : username;

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
          <AlertDescription>Configure a URL e o modo de login abaixo.</AlertDescription>
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
            {status.use_sso_profile && (
              <Badge variant="outline" className="ml-2 text-[10px] py-0">
                Perfil SSO · {status.login_identifier}
              </Badge>
            )}
          </AlertDescription>
        </Alert>
      );
    }
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
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto" onInteractOutside={(e) => e.preventDefault()}>
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
          {getStatusAlert()}

          {status && !isLoading && (
            <div className="flex items-center gap-2 text-sm">
              <span className="text-muted-foreground">Status:</span>
              {getStatusBadge()}
              {status.configured && status.username && (
                <span className="text-xs text-muted-foreground ml-auto">login: <strong>{status.username}</strong></span>
              )}
            </div>
          )}

          <Separator />

          {/* URL */}
          <div className="space-y-1.5">
            <Label htmlFor="awx-url">URL do AWX</Label>
            <div className="flex gap-2">
              <Input
                id="awx-url"
                value={baseURL}
                onChange={(e) => {
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

          <Separator />

          {/* Modo de login */}
          <div className="space-y-3">
            <Label className="text-sm font-medium">Modo de login</Label>

            {/* Toggle SSO Profile */}
            <div
              className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                useSSOProfile
                  ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/40'
                  : 'border-border hover:border-blue-300'
              }`}
              onClick={() => !isProcessing && setUseSSOProfile(true)}
            >
              <div className={`mt-0.5 h-4 w-4 rounded-full border-2 flex-shrink-0 ${
                useSSOProfile ? 'border-blue-500 bg-blue-500' : 'border-muted-foreground'
              }`}>
                {useSSOProfile && <div className="h-2 w-2 rounded-full bg-white m-auto mt-0.5" />}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <KeyRound className="h-4 w-4 text-blue-500" />
                  <span className="text-sm font-medium">Usar Perfil SSO corporativo</span>
                  {ssoProfile?.configured
                    ? <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 text-[10px] py-0">Configurado</Badge>
                    : <Badge variant="destructive" className="text-[10px] py-0">Não configurado</Badge>
                  }
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Usa as credenciais do Perfil SSO. Selecione qual identificador este serviço aceita:
                </p>

                {useSSOProfile && (
                  <div className="mt-2 space-y-2">
                    {!ssoProfile?.configured && (
                      <Alert className="py-2">
                        <Info className="h-3.5 w-3.5" />
                        <AlertDescription className="text-xs">
                          Configure o <strong>Perfil SSO</strong> em Credenciais → Perfil SSO primeiro.
                        </AlertDescription>
                      </Alert>
                    )}

                    <div className="flex flex-col gap-1.5">
                      {(['matricula', 'email'] as const).map((id) => (
                        <label
                          key={id}
                          className={`flex items-center gap-1.5 text-sm cursor-pointer px-2 py-1 rounded border transition-colors ${
                            loginIdentifier === id
                              ? 'border-blue-500 bg-blue-100 dark:bg-blue-900/40'
                              : 'border-border hover:border-blue-300'
                          }`}
                          onClick={(e) => { e.stopPropagation(); setLoginIdentifier(id); }}
                        >
                          <input
                            type="radio"
                            name="login-identifier"
                            value={id}
                            checked={loginIdentifier === id}
                            onChange={() => setLoginIdentifier(id)}
                            className="accent-blue-500"
                            disabled={isProcessing}
                          />
                          <span className="capitalize">{id === 'matricula' ? 'Matrícula' : 'Email'}</span>
                          {ssoProfile?.configured && (
                            <span className="text-xs text-muted-foreground">
                              ({id === 'matricula' ? (ssoProfile.matricula || '—') : (ssoProfile.email || '—')})
                            </span>
                          )}
                        </label>
                      ))}
                    </div>

                    {ssoProfile?.configured && (
                      <p className="text-xs text-muted-foreground">
                        Login efetivo: <strong>{effectiveUsername}</strong>
                      </p>
                    )}
                  </div>
                )}
              </div>
            </div>

            {/* Modo manual */}
            <div
              className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                !useSSOProfile
                  ? 'border-border bg-muted/30'
                  : 'border-border hover:border-muted-foreground'
              }`}
              onClick={() => !isProcessing && setUseSSOProfile(false)}
            >
              <div className={`mt-0.5 h-4 w-4 rounded-full border-2 flex-shrink-0 ${
                !useSSOProfile ? 'border-primary bg-primary' : 'border-muted-foreground'
              }`}>
                {!useSSOProfile && <div className="h-2 w-2 rounded-full bg-white m-auto mt-0.5" />}
              </div>
              <div className="flex-1 min-w-0">
                <span className="text-sm font-medium">Credenciais manuais</span>
                <p className="text-xs text-muted-foreground mt-0.5">Usuário e senha informados diretamente.</p>

                {!useSSOProfile && (
                  <div className="mt-2 grid grid-cols-2 gap-2">
                    <div className="space-y-1">
                      <Label htmlFor="awx-username" className="text-xs">Usuário</Label>
                      <Input
                        id="awx-username"
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        placeholder="usuario ou matrícula"
                        disabled={isProcessing}
                        autoComplete="username"
                        className="h-8 text-sm"
                        onClick={(e) => e.stopPropagation()}
                      />
                    </div>
                    <div className="space-y-1">
                      <Label htmlFor="awx-password" className="text-xs">
                        Senha
                        {status?.configured && <span className="ml-1 opacity-60">(manter=vazio)</span>}
                      </Label>
                      <Input
                        id="awx-password"
                        type="password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder={status?.configured ? '••••••••' : 'Senha'}
                        disabled={isProcessing}
                        autoComplete="current-password"
                        className="h-8 text-sm"
                        onClick={(e) => e.stopPropagation()}
                      />
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={fetchStatus} disabled={isProcessing}>
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
                {isDeleting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={handleTest} disabled={isProcessing}>
              {isTesting && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
              Testar
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isProcessing}>
              {isSaving && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
