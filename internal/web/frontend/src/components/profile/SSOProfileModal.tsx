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
  Info,
  Loader2,
  Trash2,
  RefreshCw,
  ShieldCheck,
  KeyRound,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
import type { SSOProfile } from '@/lib/api/types';
import type { CredentialModalProps } from '@/types/profile';

export function SSOProfileModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [profile, setProfile] = useState<SSOProfile | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const [email, setEmail] = useState('');
  const [matricula, setMatricula] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');

  useEffect(() => {
    if (open) fetchProfile();
  }, [open]);

  const fetchProfile = async () => {
    setIsLoading(true);
    try {
      const p = await apiClient.getSSOProfile();
      setProfile(p);
      if (p.configured) {
        setEmail(p.email ?? '');
        setMatricula(p.matricula ?? '');
      }
    } catch {
      setProfile({ configured: false });
    } finally {
      setIsLoading(false);
    }
  };

  const handleSave = async () => {
    if (!email && !matricula) {
      toast.error('Informe ao menos email ou matrícula');
      return;
    }
    if (!profile?.configured && !password) {
      toast.error('Senha é obrigatória na primeira configuração');
      return;
    }
    if (password && password !== confirmPassword) {
      toast.error('As senhas não coincidem');
      return;
    }

    setIsSaving(true);
    try {
      await apiClient.saveSSOProfile(email.trim(), matricula.trim(), password);
      toast.success('Perfil SSO salvo com sucesso!');
      setPassword('');
      setConfirmPassword('');
      onSaved?.();
      fetchProfile();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao salvar perfil SSO');
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Remover o perfil SSO? Serviços configurados para usar SSO deixarão de funcionar.')) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteSSOProfile();
      setProfile({ configured: false });
      setEmail('');
      setMatricula('');
      setPassword('');
      setConfirmPassword('');
      toast.success('Perfil SSO removido');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao remover perfil SSO');
    } finally {
      setIsDeleting(false);
    }
  };

  const isProcessing = isLoading || isSaving || isDeleting;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="h-5 w-5 text-blue-500" />
            Perfil SSO Corporativo
          </DialogTitle>
          <DialogDescription>
            Credenciais compartilhadas entre todos os serviços corporativos (AWX, ServiceNow, etc).
            Cada serviço usa email <strong>ou</strong> matrícula conforme configurado nele.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Status */}
          {isLoading ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando perfil...</AlertDescription>
            </Alert>
          ) : profile?.configured ? (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertTitle className="text-green-900 dark:text-green-100">Perfil configurado</AlertTitle>
              <AlertDescription className="text-green-800 dark:text-green-200 text-xs">
                {profile.email && <span>Email: <strong>{profile.email}</strong></span>}
                {profile.email && profile.matricula && <span className="mx-2">·</span>}
                {profile.matricula && <span>Matrícula: <strong>{profile.matricula}</strong></span>}
                {profile.has_password && <span className="block mt-0.5 opacity-70">Senha: configurada</span>}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Não configurado</AlertTitle>
              <AlertDescription className="text-sm">
                Preencha os campos abaixo para configurar suas credenciais corporativas.
              </AlertDescription>
            </Alert>
          )}

          <Separator />

          {/* Campos de identificação */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="sso-email">Email corporativo</Label>
              <Input
                id="sso-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="nome.sobrenome@empresa.com.br"
                disabled={isProcessing}
                autoComplete="email"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="sso-matricula">
                Matrícula
                <span className="ml-1 text-xs text-muted-foreground">(número de funcionário)</span>
              </Label>
              <Input
                id="sso-matricula"
                value={matricula}
                onChange={(e) => setMatricula(e.target.value)}
                placeholder="ex: 123456"
                disabled={isProcessing}
              />
            </div>
          </div>

          <Separator />

          {/* Senha */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="sso-password">
                Senha SSO
                {profile?.configured && (
                  <span className="ml-1 text-xs text-muted-foreground">(deixe em branco para manter)</span>
                )}
              </Label>
              <Input
                id="sso-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={profile?.configured ? '••••••••' : 'Senha corporativa'}
                disabled={isProcessing}
                autoComplete="current-password"
              />
            </div>

            {password && (
              <div className="space-y-1.5">
                <Label htmlFor="sso-confirm">Confirmar senha</Label>
                <Input
                  id="sso-confirm"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Repita a senha"
                  disabled={isProcessing}
                  className={confirmPassword && confirmPassword !== password ? 'border-destructive' : ''}
                />
                {confirmPassword && confirmPassword !== password && (
                  <p className="text-xs text-destructive flex items-center gap-1">
                    <XCircle className="h-3 w-3" /> As senhas não coincidem
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Info sobre uso */}
          <Alert>
            <ShieldCheck className="h-4 w-4" />
            <AlertDescription className="text-xs space-y-1">
              <p>A senha é armazenada com criptografia AES-256-GCM localmente.</p>
              <p>
                Serviços usam este perfil quando configurados com{' '}
                <Badge variant="outline" className="text-[10px] py-0">Usar Perfil SSO</Badge>.
                Cada serviço define se usa <strong>email</strong> ou <strong>matrícula</strong> como login.
              </p>
            </AlertDescription>
          </Alert>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={fetchProfile} disabled={isProcessing}>
              <RefreshCw className={`h-4 w-4 mr-1 ${isLoading ? 'animate-spin' : ''}`} />
              Atualizar
            </Button>
            {profile?.configured && (
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

          <Button size="sm" onClick={handleSave} disabled={isProcessing}>
            {isSaving && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
            Salvar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
