import { useEffect, useState, useCallback } from "react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2, CheckCircle2, XCircle, RefreshCcw, KeyRound } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface ResyncAkvModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  secretName: string;
  // Opcional — chamado quando o `kubectl annotate` do resync retorna com sucesso. O resync em si
  // é assíncrono (o external-secrets ainda precisa buscar do AKV e atualizar o Secret no cluster),
  // então isso não significa "valor já atualizado", só "comando disparado com sucesso" — quem usa
  // esse callback decide o que fazer depois (ex: poll de releitura, ver DependenciesTab.tsx).
  // SecretsTab.tsx não passa esse prop — comportamento dela fica inalterado.
  onResyncSuccess?: () => void;
}

type Status = "running" | "success" | "error";

export const ResyncAkvModal = ({ open, onOpenChange, cluster, namespace, secretName, onResyncSuccess }: ResyncAkvModalProps) => {
  const [status, setStatus] = useState<Status>("running");
  const [command, setCommand] = useState("");
  const [output, setOutput] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [finishedAt, setFinishedAt] = useState<string | null>(null);

  const runResync = useCallback(async () => {
    setStatus("running");
    setOutput("");
    setErrorMessage("");
    setFinishedAt(null);
    try {
      const result = await apiClient.resyncAkv(cluster, namespace);
      setCommand(result.command || "");
      setOutput(result.output || "");
      setStatus("success");
      setFinishedAt(new Date().toLocaleString());
      toast.success("Resync AKV disparado", {
        description: `${namespace}/${result.resourceName || "sre-tools-external-secrets"}`,
      });
      onResyncSuccess?.();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Erro desconhecido";
      setErrorMessage(msg);
      setStatus("error");
      setFinishedAt(new Date().toLocaleString());
      toast.error("Falha ao disparar Resync AKV", { description: msg });
    }
  }, [cluster, namespace, onResyncSuccess]);

  useEffect(() => {
    if (open) {
      runResync();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const externalSecretName = `sre-tools-external-secrets-${namespace}`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl bg-background border-border">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-lg font-semibold text-primary">
            <KeyRound className="w-5 h-5" />
            Resync AKV
          </DialogTitle>
          <DialogDescription>
            Força o external-secrets a ressincronizar com o Azure Key Vault para o Secret{" "}
            <span className="font-mono text-foreground">{namespace}/{secretName}</span>.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3 text-sm">
          <div className="grid grid-cols-2 gap-3 text-xs border-b border-border/50 pb-3">
            <div>
              <span className="text-muted-foreground uppercase block mb-0.5">Cluster</span>
              <span className="font-medium">{cluster}</span>
            </div>
            <div>
              <span className="text-muted-foreground uppercase block mb-0.5">Namespace</span>
              <span className="font-medium">{namespace}</span>
            </div>
            <div className="col-span-2">
              <span className="text-muted-foreground uppercase block mb-0.5">ExternalSecret</span>
              <span className="font-mono">{externalSecretName}</span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {status === "running" && (
              <>
                <Loader2 className="w-4 h-4 animate-spin text-primary" />
                <span className="text-muted-foreground">Executando annotate no cluster...</span>
              </>
            )}
            {status === "success" && (
              <>
                <CheckCircle2 className="w-4 h-4 text-green-500" />
                <span className="text-green-500 font-medium">Resync disparado com sucesso</span>
              </>
            )}
            {status === "error" && (
              <>
                <XCircle className="w-4 h-4 text-destructive" />
                <span className="text-destructive font-medium">Falha ao disparar resync</span>
              </>
            )}
            {finishedAt && (
              <span className="text-xs text-muted-foreground ml-auto">{finishedAt}</span>
            )}
          </div>

          {command && (
            <div>
              <span className="text-xs text-muted-foreground uppercase block mb-1">Comando</span>
              <pre className="text-xs font-mono bg-secondary/40 border border-border/50 rounded p-2 overflow-x-auto whitespace-pre-wrap break-all">
                {command}
              </pre>
            </div>
          )}

          {status === "success" && (
            <div>
              <span className="text-xs text-muted-foreground uppercase block mb-1">Saída</span>
              <pre className="text-xs font-mono bg-secondary/40 border border-border/50 rounded p-2 overflow-x-auto whitespace-pre-wrap break-all min-h-[2rem]">
                {output || "(sem saída)"}
              </pre>
            </div>
          )}

          {status === "error" && (
            <div>
              <span className="text-xs text-muted-foreground uppercase block mb-1">Erro</span>
              <pre className="text-xs font-mono bg-destructive/10 border border-destructive/30 text-destructive rounded p-2 overflow-x-auto whitespace-pre-wrap break-all">
                {errorMessage}
              </pre>
            </div>
          )}
        </div>

        <DialogFooter className="gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={runResync}
            disabled={status === "running"}
          >
            <RefreshCcw className="w-4 h-4 mr-2" />
            Executar novamente
          </Button>
          <Button variant="default" size="sm" onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
