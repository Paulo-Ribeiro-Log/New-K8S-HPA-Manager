import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { CheckCircle2, XCircle, Info, Loader2, Save, Trash2, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import {
  useGitHubTokenStatus,
  useSaveGitHubToken,
  useDeleteGitHubToken,
} from "@/hooks/useGitHubReleases";

interface TokenConfigModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export const TokenConfigModal = ({
  open,
  onOpenChange,
}: TokenConfigModalProps) => {
  const [token, setToken] = useState("");
  const [showToken, setShowToken] = useState(false);

  const { data: tokenStatus, isLoading: isLoadingStatus, refetch: refetchStatus } = useGitHubTokenStatus();
  const saveTokenMutation = useSaveGitHubToken();
  const deleteTokenMutation = useDeleteGitHubToken();

  // Limpar input ao abrir modal
  useEffect(() => {
    if (open) {
      setToken("");
      setShowToken(false);
      refetchStatus();
    }
  }, [open, refetchStatus]);

  const handleSave = async () => {
    if (!token.trim()) {
      toast.error("Token é obrigatório");
      return;
    }

    try {
      const result = await saveTokenMutation.mutateAsync(token);
      toast.success(result.message, {
        description: result.github_user ? `Autenticado como: ${result.github_user}` : undefined,
      });
      setToken("");
      refetchStatus();
    } catch (error) {
      toast.error("Erro ao salvar token", {
        description: error instanceof Error ? error.message : "Erro desconhecido",
      });
    }
  };

  const handleDelete = async () => {
    if (!tokenStatus?.configured) {
      toast.error("Nenhum token configurado");
      return;
    }

    if (!confirm("Tem certeza que deseja remover seu token GitHub?")) {
      return;
    }

    try {
      const result = await deleteTokenMutation.mutateAsync();
      toast.success(result.message);
      refetchStatus();
    } catch (error) {
      toast.error("Erro ao remover token", {
        description: error instanceof Error ? error.message : "Erro desconhecido",
      });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Configuração do Token GitHub</DialogTitle>
          <DialogDescription>
            Configure seu token pessoal do GitHub para acessar repositórios privados
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Status do Token */}
          <div>
            <Label>Status do Token</Label>
            {isLoadingStatus ? (
              <Alert>
                <Loader2 className="h-4 w-4 animate-spin" />
                <AlertTitle>Verificando...</AlertTitle>
                <AlertDescription>
                  Validando token configurado
                </AlertDescription>
              </Alert>
            ) : tokenStatus?.valid ? (
              <Alert className="border-green-200 bg-green-50">
                <CheckCircle2 className="h-4 w-4 text-green-600" />
                <AlertTitle className="text-green-900">Token Válido</AlertTitle>
                <AlertDescription className="text-green-800">
                  <div className="space-y-1">
                    <div>
                      <strong>Autenticado como:</strong> {tokenStatus.username}
                    </div>
                    {tokenStatus.email && (
                      <div>
                        <strong>Email:</strong> {tokenStatus.email}
                      </div>
                    )}
                    <div>
                      <strong>Rate Limit:</strong> {tokenStatus.remaining}/{tokenStatus.limit} requisições restantes
                    </div>
                    {tokenStatus.reset_at && (
                      <div className="text-xs text-muted-foreground">
                        Reset em: {new Date(tokenStatus.reset_at).toLocaleString('pt-BR')}
                      </div>
                    )}
                  </div>
                </AlertDescription>
              </Alert>
            ) : tokenStatus?.configured ? (
              <Alert variant="destructive">
                <XCircle className="h-4 w-4" />
                <AlertTitle>Token Inválido</AlertTitle>
                <AlertDescription>
                  {tokenStatus.error || "Token configurado mas não é válido"}
                </AlertDescription>
              </Alert>
            ) : (
              <Alert>
                <Info className="h-4 w-4" />
                <AlertTitle>Token Não Configurado</AlertTitle>
                <AlertDescription>
                  Configure um token para acessar repositórios privados e ter rate limit de 5000 req/h
                  (ao invés de 60 req/h)
                </AlertDescription>
              </Alert>
            )}
          </div>

          {/* Input do Token */}
          <div>
            <Label htmlFor="token">Personal Access Token</Label>
            <div className="flex gap-2">
              <Input
                id="token"
                type={showToken ? "text" : "password"}
                placeholder="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                className="font-mono text-sm"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShowToken(!showToken)}
              >
                {showToken ? "Ocultar" : "Mostrar"}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Token com escopo <code className="bg-muted px-1 py-0.5 rounded">repo</code> para acesso a repositórios privados
            </p>
          </div>

          {/* Como obter token */}
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>Como obter um token?</AlertTitle>
            <AlertDescription>
              <ol className="list-decimal list-inside space-y-1 mt-2">
                <li>
                  Acesse{" "}
                  <a
                    href="https://github.com/settings/tokens"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-primary hover:underline inline-flex items-center gap-1"
                  >
                    github.com/settings/tokens
                    <ExternalLink className="h-3 w-3" />
                  </a>
                </li>
                <li>Clique em "Generate new token (classic)"</li>
                <li>
                  Selecione escopo <code className="bg-muted px-1 py-0.5 rounded">repo</code> (acesso total a repositórios)
                </li>
                <li>Copie o token gerado e cole acima</li>
              </ol>
            </AlertDescription>
          </Alert>

          {/* Botões */}
          <div className="flex justify-between gap-2">
            <div>
              {tokenStatus?.configured && (
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  onClick={handleDelete}
                  disabled={deleteTokenMutation.isPending}
                >
                  {deleteTokenMutation.isPending ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      Removendo...
                    </>
                  ) : (
                    <>
                      <Trash2 className="mr-2 h-4 w-4" />
                      Remover Token
                    </>
                  )}
                </Button>
              )}
            </div>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancelar
              </Button>
              <Button
                type="button"
                onClick={handleSave}
                disabled={!token.trim() || saveTokenMutation.isPending}
              >
                {saveTokenMutation.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Salvando...
                  </>
                ) : (
                  <>
                    <Save className="mr-2 h-4 w-4" />
                    Salvar Token
                  </>
                )}
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
