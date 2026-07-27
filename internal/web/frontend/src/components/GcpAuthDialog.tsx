import { useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { CloudAccountHintField } from "@/components/CloudAccountHintField";
import type { GcpSsoState } from "@/hooks/useGcpSsoAuth";

interface Props {
  state: GcpSsoState;
  onRetry: () => void;
  onClose: () => void;
}

/**
 * Equivalente GCP do AwsSsoLoginDialog.tsx — Device Authorization Grant (accounts.google.com/device)
 * disparado automaticamente ao trocar para um cluster GKE sem autenticação GCP válida (ver
 * useGcpSsoAuth.checkForCluster). Antes desta implementação, essa verificação/dialog simplesmente
 * não existia pra GKE — só AWS EKS tinha o equivalente.
 */
export function GcpAuthDialog({ state, onRetry, onClose }: Props) {
  const handleCopyUrl = useCallback(() => {
    if (state.url) {
      navigator.clipboard.writeText(state.url);
      toast.success("URL copiada!");
    }
  }, [state.url]);

  const handleOpenUrl = useCallback(() => {
    if (state.url) window.open(state.url, "_blank");
  }, [state.url]);

  const title = state.success ? "Autenticado com sucesso!" : "Login GCP necessário";

  return (
    <Dialog open={state.open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>🔐</span> {title}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="space-y-1 text-sm">
              <span>Cluster GKE: </span>
              <Badge variant="secondary" className="font-mono">{state.cluster || "—"}</Badge>
            </div>
          </DialogDescription>
        </DialogHeader>

        {/* Sucesso */}
        {state.success && (
          <div className="flex flex-col items-center gap-3 py-4 text-center">
            <span className="text-4xl">✅</span>
            <p className="text-sm text-muted-foreground">
              Autenticação renovada. As operações no cluster serão retomadas automaticamente.
            </p>
          </div>
        )}

        {/* Carregando */}
        {state.loading && !state.success && (
          <div className="flex flex-col items-center gap-3 py-6 text-center">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
            <p className="text-sm text-muted-foreground">Iniciando autenticação GCP…</p>
          </div>
        )}

        {/* Erro */}
        {state.error && !state.loading && (
          <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
            {state.error}
          </div>
        )}

        {/* URL de autorização + código */}
        {state.url && !state.loading && !state.success && (
          <div className="space-y-4">
            <div className="rounded-md bg-muted p-3 space-y-2">
              <p className="text-sm font-medium">1. Abra a URL no navegador:</p>
              <div className="flex gap-2">
                <Input readOnly value={state.url} className="font-mono text-xs" />
                <Button variant="outline" size="sm" onClick={handleCopyUrl}>Copiar</Button>
                <Button variant="outline" size="sm" onClick={handleOpenUrl}>Abrir</Button>
              </div>
            </div>

            {state.userCode && (
              <div className="rounded-md bg-muted p-3 space-y-1 text-center">
                <p className="text-sm font-medium">2. Insira o código:</p>
                <p className="font-mono text-2xl font-bold tracking-widest text-primary">
                  {state.userCode}
                </p>
              </div>
            )}

            <CloudAccountHintField provider="gcp" />

            {state.polling && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent" />
                Aguardando autenticação…
              </div>
            )}
          </div>
        )}

        <DialogFooter className="gap-2">
          {state.error && !state.loading && (
            <Button onClick={onRetry}>Tentar novamente</Button>
          )}
          {!state.success && (
            <Button variant="outline" onClick={onClose}>
              {state.polling ? "Cancelar" : "Fechar"}
            </Button>
          )}
          {state.success && (
            <Button onClick={onClose}>Fechar</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
