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
} from 'lucide-react';
import { toast } from 'sonner';
import {
  useGitHubTokenStatus,
  useSaveGitHubToken,
  useDeleteGitHubToken,
} from '@/hooks/useGitHubReleases';
import type { CredentialModalProps } from '@/types/profile';

export function GitHubCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [token, setToken] = useState('');
  const [email, setEmail] = useState('');
  const [showToken, setShowToken] = useState(false);

  const { data: tokenStatus, isLoading: isLoadingStatus, refetch: refetchStatus } = useGitHubTokenStatus();
  const saveTokenMutation = useSaveGitHubToken();
  const deleteTokenMutation = useDeleteGitHubToken();

  // Limpar input ao abrir modal
  useEffect(() => {
    if (open) {
      setToken('');
      setEmail('');
      setShowToken(false);
      refetchStatus();
    }
  }, [open, refetchStatus]);

  const handleSave = async () => {
    if (!token.trim()) {
      toast.error('Token e obrigatorio');
      return;
    }

    if (!email.trim()) {
      toast.error('Email/Login do GitHub e obrigatorio');
      return;
    }

    try {
      const result = await saveTokenMutation.mutateAsync({ token, email });
      toast.success(result.message, {
        description: result.github_user ? `Autenticado como: ${result.github_user}` : undefined,
      });
      setToken('');
      setEmail('');
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
            GitHub Token
          </DialogTitle>
          <DialogDescription>
            Configure seu token pessoal para acessar repositorios privados
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Status do Token */}
          {isLoadingStatus ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando token...</AlertDescription>
            </Alert>
          ) : tokenStatus?.valid ? (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertTitle className="text-green-900 dark:text-green-100">Token Valido</AlertTitle>
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
              <AlertTitle>Token Invalido</AlertTitle>
              <AlertDescription>
                {tokenStatus.error || 'Token configurado mas nao e valido'}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertDescription className="text-sm">
                Sem token: 60 req/h. Com token: 5000 req/h
              </AlertDescription>
            </Alert>
          )}

          {/* Input de Email/Login */}
          <div className="space-y-2">
            <Label htmlFor="github-email">Email ou Login do GitHub</Label>
            <Input
              id="github-email"
              type="text"
              placeholder="seu-usuario ou email@empresa.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={isProcessing}
              className="font-mono text-sm"
            />
          </div>

          {/* Input do Token */}
          <div className="space-y-2">
            <Label htmlFor="github-token">Personal Access Token</Label>
            <div className="flex gap-2">
              <Input
                id="github-token"
                type={showToken ? 'text' : 'password'}
                placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                value={token}
                onChange={(e) => setToken(e.target.value)}
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

          {/* Link para criar token */}
          <a
            href="https://github.com/settings/tokens"
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
          >
            <ExternalLink className="h-3 w-3" />
            Criar novo token no GitHub
          </a>
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
              disabled={!token.trim() || !email.trim() || isProcessing}
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
