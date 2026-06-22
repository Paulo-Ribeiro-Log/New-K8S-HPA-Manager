import { useState, useEffect, useCallback } from 'react';
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
import {
  CheckCircle2,
  XCircle,
  Info,
  Loader2,
  Trash2,
  ExternalLink,
  Github,
  Eye,
  EyeOff,
  ShieldCheck,
  User,
  KeyRound,
  Plus,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  useGitHubTokenStatus,
  useSaveGitHubToken,
  useDeleteGitHubToken,
} from '@/hooks/useGitHubReleases';
import { useUserPermissions } from '@/hooks/useUserPermissions';
import { apiClient } from '@/lib/api/client';
import type { CredentialModalProps } from '@/types/profile';

// ── Perfis do Editor de Código (localStorage) ──────────────────────────────
interface GitHubProfile { id: string; name: string; token: string }
const CE_PROFILES_KEY = 'ce_github_profiles';
const CE_REPO_PROFILE_KEY = 'ce_repo_profile';
function ceLoadProfiles(): GitHubProfile[] {
  try { return JSON.parse(localStorage.getItem(CE_PROFILES_KEY) || '[]'); } catch { return []; }
}
function ceSaveProfiles(p: GitHubProfile[]) { localStorage.setItem(CE_PROFILES_KEY, JSON.stringify(p)); }
function ceDeleteProfile(id: string) {
  ceSaveProfiles(ceLoadProfiles().filter(p => p.id !== id));
  const map: Record<string, string> = JSON.parse(localStorage.getItem(CE_REPO_PROFILE_KEY) || '{}');
  Object.keys(map).forEach(k => { if (map[k] === id) delete map[k]; });
  localStorage.setItem(CE_REPO_PROFILE_KEY, JSON.stringify(map));
}

export function GitHubCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [token, setToken] = useState('');
  const [org, setOrg] = useState(() => apiClient.getGitHubOrg());
  const [showToken, setShowToken] = useState(false);

  // Perfis do Editor de Código
  const [ceProfiles, setCeProfiles] = useState<GitHubProfile[]>([]);
  const [ceNewName, setCeNewName] = useState('');
  const [ceNewToken, setCeNewToken] = useState('');
  const [ceShowSection, setCeShowSection] = useState(false);

  const refreshCeProfiles = useCallback(() => setCeProfiles(ceLoadProfiles()), []);

  // Email vem do contexto RBAC (Azure AD) — não é digitado manualmente
  const { data: userPerms } = useUserPermissions();
  const rbacEmail = userPerms?.email || '';

  const { data: tokenStatus, isLoading: isLoadingStatus, refetch: refetchStatus } = useGitHubTokenStatus();
  const saveTokenMutation = useSaveGitHubToken();
  const deleteTokenMutation = useDeleteGitHubToken();

  // Recarregar estado ao abrir modal
  useEffect(() => {
    if (open) {
      setToken('');
      setOrg(apiClient.getGitHubOrg());
      setShowToken(false);
      refetchStatus();
      refreshCeProfiles();
      setCeNewName(''); setCeNewToken('');
    }
  }, [open, refetchStatus, refreshCeProfiles]);

  const handleSave = async () => {
    if (!token.trim()) {
      toast.error('Token é obrigatório');
      return;
    }

    try {
      const result = await saveTokenMutation.mutateAsync({ token, email: rbacEmail });
      // Armazenar email RBAC para o header X-GitHub-Email (fallback em ambientes sem RBAC)
      if (rbacEmail) {
        apiClient.setGitHubEmail(rbacEmail);
      }
      // Salvar organização
      apiClient.setGitHubOrg(org.trim() || 'casas-bahia');
      toast.success(result.message, {
        description: result.github_user ? `Autenticado como: ${result.github_user}` : undefined,
      });
      setToken('');
      refetchStatus();
      onSaved?.();
    } catch (error) {
      toast.error('Erro ao salvar token', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    }
  };

  const handleDelete = async () => {
    if (!tokenStatus?.configured) {
      toast.error('Nenhum token configurado');
      return;
    }

    if (!confirm('Tem certeza que deseja remover seu token GitHub?')) {
      return;
    }

    try {
      const result = await deleteTokenMutation.mutateAsync();
      toast.success(result.message);
      refetchStatus();
      onSaved?.();
    } catch (error) {
      toast.error('Erro ao remover token', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    }
  };

  const isProcessing = saveTokenMutation.isPending || deleteTokenMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Github className="h-5 w-5" />
            GitHub Token (SSO/SAML)
          </DialogTitle>
          <DialogDescription>
            Configure seu token para acessar repositórios da organização com SAML SSO
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Identidade vinculada (RBAC) */}
          <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-muted/50 border text-sm">
            <User className="h-4 w-4 text-muted-foreground shrink-0" />
            <div className="min-w-0">
              <p className="text-[11px] text-muted-foreground">Token vinculado ao usuário</p>
              <p className="font-mono font-medium truncate">{rbacEmail || 'Carregando...'}</p>
            </div>
          </div>

          {/* Status do Token */}
          {isLoadingStatus ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando token...</AlertDescription>
            </Alert>
          ) : tokenStatus?.valid ? (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertTitle className="text-green-900 dark:text-green-100">Token Válido</AlertTitle>
              <AlertDescription className="text-green-800 dark:text-green-200">
                <div className="flex flex-wrap gap-2 mt-1">
                  <Badge variant="secondary">{tokenStatus.username}</Badge>
                  <Badge variant="outline">
                    {tokenStatus.remaining}/{tokenStatus.limit} req
                  </Badge>
                </div>
              </AlertDescription>
            </Alert>
          ) : tokenStatus?.configured ? (
            <Alert variant="destructive">
              <XCircle className="h-4 w-4" />
              <AlertTitle>Token Inválido</AlertTitle>
              <AlertDescription>
                {tokenStatus.error || 'Token configurado mas não é válido'}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertDescription className="text-sm">
                Sem token: 60 req/h. Com token autorizado para SSO: 5000 req/h
              </AlertDescription>
            </Alert>
          )}

          {/* Organização GitHub */}
          <div className="space-y-2">
            <Label htmlFor="github-org">Organização GitHub</Label>
            <Input
              id="github-org"
              placeholder="casas-bahia"
              value={org}
              onChange={(e) => setOrg(e.target.value)}
              disabled={isProcessing}
              className="font-mono text-sm"
            />
            <p className="text-[11px] text-muted-foreground">
              Default: <code>casas-bahia</code> — altere se sua organização for diferente
            </p>
          </div>

          {/* Input do Token */}
          <div className="space-y-2">
            <Label htmlFor="github-token">Personal Access Token</Label>
            <div className="flex gap-2">
              <Input
                id="github-token"
                type={showToken ? 'text' : 'password'}
                placeholder="ghp_... ou github_pat_..."
                value={token}
                onChange={(e) => setToken(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSave()}
                disabled={isProcessing}
                className="font-mono text-sm"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => setShowToken(!showToken)}
                disabled={isProcessing}
              >
                {showToken ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </Button>
            </div>
          </div>

          {/* Guia SSO/SAML */}
          <Alert className="border-amber-200 bg-amber-50 dark:bg-amber-950/40 dark:border-amber-800">
            <ShieldCheck className="h-4 w-4 text-amber-600" />
            <AlertTitle className="text-amber-800 dark:text-amber-300 text-sm">Configuração SSO/SAML obrigatória</AlertTitle>
            <AlertDescription className="text-xs space-y-1 mt-1">
              <p className="font-semibold text-amber-700 dark:text-amber-300">Opção A — Classic PAT (recomendado):</p>
              <ol className="list-decimal list-inside space-y-0.5 text-muted-foreground pl-1">
                <li>Acesse <strong>Settings → Developer settings → Personal access tokens → Tokens (classic)</strong></li>
                <li>Crie token com permissão <code className="bg-muted px-1 rounded">repo</code></li>
                <li>Após criar, clique em <strong>"Configure SSO"</strong> ao lado do token</li>
                <li>Autorize para <strong>{org || 'casas-bahia'}</strong> e complete o SSO</li>
              </ol>
              <p className="font-semibold text-amber-700 dark:text-amber-300 mt-2">Opção B — Fine-grained PAT:</p>
              <ol className="list-decimal list-inside space-y-0.5 text-muted-foreground pl-1">
                <li>Acesse <strong>Settings → Developer settings → Fine-grained tokens</strong></li>
                <li>Selecione <strong>Resource owner: {org || 'casas-bahia'}</strong></li>
                <li>Conceda permissão <code className="bg-muted px-1 rounded">Contents: Read</code></li>
              </ol>
            </AlertDescription>
          </Alert>

          {/* Links */}
          <div className="flex gap-4">
            <a
              href="https://github.com/settings/tokens"
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
            >
              <ExternalLink className="h-3 w-3" />
              Classic Tokens
            </a>
            <a
              href="https://github.com/settings/personal-access-tokens"
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
            >
              <ExternalLink className="h-3 w-3" />
              Fine-grained Tokens
            </a>
          </div>
        </div>

        {/* ── Perfis do Editor de Código ── */}
        <div className="border-t pt-3">
          <button
            className="flex items-center gap-2 text-sm font-medium w-full text-left hover:text-foreground text-muted-foreground transition-colors"
            onClick={() => setCeShowSection(v => !v)}
          >
            {ceShowSection ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
            <KeyRound className="h-4 w-4" />
            Perfis do Editor de Código
            {ceProfiles.length > 0 && (
              <span className="ml-auto text-xs bg-muted rounded-full px-2 py-0.5">{ceProfiles.length}</span>
            )}
          </button>

          {ceShowSection && (
            <div className="mt-3 space-y-3">
              <p className="text-xs text-muted-foreground">
                Perfis nomeados para push/pull no Editor de Código — cada repositório pode usar um perfil diferente (pessoal, profissional, etc.).
              </p>

              {ceProfiles.length > 0 && (
                <div className="space-y-1.5">
                  {ceProfiles.map(p => (
                    <div key={p.id} className="flex items-center justify-between rounded border border-border/50 bg-muted/30 px-3 py-1.5">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium">{p.name}</span>
                        <span className="text-xs text-muted-foreground font-mono">
                          {p.token.length > 8 ? p.token.slice(0, 4) + '...' + p.token.slice(-4) : '****'}
                        </span>
                      </div>
                      <button
                        onClick={() => { ceDeleteProfile(p.id); refreshCeProfiles(); }}
                        className="text-muted-foreground hover:text-destructive transition-colors"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="flex gap-2">
                <input
                  className="flex-1 text-xs bg-muted border border-border/50 rounded px-2 py-1.5 text-foreground placeholder:text-muted-foreground"
                  placeholder="Nome (ex: Pessoal)"
                  value={ceNewName}
                  onChange={e => setCeNewName(e.target.value)}
                />
                <input
                  type="password"
                  className="flex-[2] text-xs bg-muted border border-border/50 rounded px-2 py-1.5 font-mono text-foreground placeholder:text-muted-foreground"
                  placeholder="ghp_... ou github_pat_..."
                  value={ceNewToken}
                  onChange={e => setCeNewToken(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && ceNewName.trim() && ceNewToken.trim()) {
                      ceSaveProfiles([...ceLoadProfiles(), { id: crypto.randomUUID(), name: ceNewName.trim(), token: ceNewToken.trim() }]);
                      refreshCeProfiles(); setCeNewName(''); setCeNewToken('');
                    }
                  }}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!ceNewName.trim() || !ceNewToken.trim()}
                  onClick={() => {
                    ceSaveProfiles([...ceLoadProfiles(), { id: crypto.randomUUID(), name: ceNewName.trim(), token: ceNewToken.trim() }]);
                    refreshCeProfiles(); setCeNewName(''); setCeNewToken('');
                  }}
                >
                  <Plus className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          )}
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div>
            {tokenStatus?.configured && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleDelete}
                disabled={isProcessing}
                className="text-destructive hover:text-destructive"
              >
                {deleteTokenMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Trash2 className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Cancelar
            </Button>
            <Button
              size="sm"
              onClick={handleSave}
              disabled={!token.trim() || isProcessing}
            >
              {saveTokenMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : null}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
