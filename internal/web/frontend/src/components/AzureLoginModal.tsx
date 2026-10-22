import { Copy, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { CopyValue } from "@/components/CopyValue";
import { copyText } from "@/lib/clipboard";
import { parseAzureLoginError } from "@/lib/azureError";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  errors: string[]; // mensagens completas, como vieram do servidor
  onRetry?: () => void;
}

// Sessão do Azure CLI do servidor expirada: mostra o erro e manda refazer o `az login`.
export function AzureLoginModal({ open, onOpenChange, errors, onRetry }: Props) {
  const info = parseAzureLoginError(errors.join("\n"));
  const issued = info.issuedAt ? new Date(info.issuedAt).toLocaleString("pt-BR") : undefined;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Entre de novo no Azure (az login)</DialogTitle>
          <DialogDescription>
            A sessão do Azure CLI do servidor expirou, então não deu para ler os grupos admin do AKS nem resolver nomes no Entra ID.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          {info.signInFrequency && (
            <p className="text-muted-foreground">
              O acesso condicional do tenant exige novo login após um tempo
              {info.maxLifetimeHours ? ` (${info.maxLifetimeHours} h)` : ""}
              {issued ? `; o último login foi em ${issued}` : ""}. Vai se repetir enquanto o servidor ficar ligado por mais tempo que isso.
            </p>
          )}

          <div>
            <div className="font-medium mb-1">Como resolver</div>
            <ol className="list-decimal ml-5 space-y-1 text-muted-foreground">
              <li>
                No terminal da máquina do servidor, rode{" "}
                <code className="bg-black/30 rounded px-1.5 py-0.5 text-foreground">
                  <CopyValue value="az login" label="comando">az login</CopyValue>
                </code>{" "}
                e conclua no navegador.
              </li>
              <li>Volte aqui e clique em "Tentar novamente". Não precisa reiniciar o servidor.</li>
            </ol>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <span className="font-medium">Erro retornado</span>
              <Button size="sm" variant="ghost" onClick={() => copyText(errors.join("\n"), "erro")}>
                <Copy className="w-3.5 h-3.5 mr-1.5" />
                Copiar erro
              </Button>
            </div>
            <pre className="text-xs bg-black/30 rounded p-2 whitespace-pre-wrap break-words select-text">{errors.join("\n")}</pre>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
          {onRetry && (
            <Button
              onClick={() => {
                onOpenChange(false);
                onRetry();
              }}
            >
              <RefreshCw className="w-4 h-4 mr-2" />
              Tentar novamente
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
