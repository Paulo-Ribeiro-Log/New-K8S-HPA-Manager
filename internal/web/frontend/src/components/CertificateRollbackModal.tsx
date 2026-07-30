import { useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, History, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import { CertificateChainValidationPanel } from "@/components/CertificateChainValidationPanel";
import type { RollbackBackupInfo, ChainValidationResult } from "@/types/certificates";

interface CertificateRollbackModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  secretName: string;
  /** Chamado após um rollback bem-sucedido — cada tab decide o que "atualizar" significa pra si
   *  (recarregar editor, invalidar cache de lista, etc). Opcional: sem callback, o modal só fecha. */
  onRestored?: () => void;
}

/**
 * CertificateRollbackModal — lista os backups de um Secret TLS (Fase 2 do
 * CERT-ROLLBACK-VALIDATION-PLAN.md) salvos automaticamente antes de cada sobrescrita, com opção de
 * restaurar qualquer um deles. Reaproveita CertificateChainValidationPanel pra mostrar o resultado
 * da validação do certificado restaurado, mesmo padrão já usado em CertificateRenewModal.tsx.
 */
export function CertificateRollbackModal({
  open,
  onOpenChange,
  cluster,
  namespace,
  secretName,
  onRestored,
}: CertificateRollbackModalProps) {
  const { listRollbacks, rollbackCertificate } = useCertificates();
  const [backups, setBackups] = useState<RollbackBackupInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [pendingRestore, setPendingRestore] = useState<RollbackBackupInfo | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [validationResult, setValidationResult] = useState<ChainValidationResult | null>(null);

  // restoringRef espelha `restoring`, mas lido de forma síncrona por `onOpenChange` do
  // AlertDialog — `AlertDialogAction` (Radix) fecha o diálogo de confirmação automaticamente ao
  // clicar, disparando `onOpenChange(false)` ANTES do próximo render aplicar `restoring=true` (o
  // valor de estado capturado no closure ainda é o de antes do clique) — checar `restoring`
  // (state) ali sempre lia o valor velho e derrubava a confirmação na hora, mesmo com a
  // restauração ainda em andamento. Mesmo padrão de bug já corrigido no AWXCertForm.tsx.
  const restoringRef = useRef(false);

  useEffect(() => {
    if (!open) return;
    setValidationResult(null);
    setLoadError(null);
    setLoading(true);
    listRollbacks(cluster, namespace, secretName)
      .then(setBackups)
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro desconhecido"))
      .finally(() => setLoading(false));
  }, [open, cluster, namespace, secretName, listRollbacks]);

  const handleConfirmRestore = async () => {
    if (!pendingRestore) return;
    restoringRef.current = true;
    setRestoring(true);
    try {
      const { validation } = await rollbackCertificate(cluster, namespace, secretName, pendingRestore.backup_id);
      toast.success("Certificado restaurado", {
        description: `Backup de ${new Date(pendingRestore.backed_up_at).toLocaleString("pt-BR")}`,
      });
      setValidationResult(validation);
      setPendingRestore(null);
      onRestored?.();
    } catch (err) {
      toast.error("Erro ao restaurar backup", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      restoringRef.current = false;
      setRestoring(false);
    }
  };

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      setValidationResult(null);
      setPendingRestore(null);
    }
    onOpenChange(nextOpen);
  };

  return (
    <>
      <Dialog open={open} onOpenChange={handleClose}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <History className="h-5 w-5" />
              Backups / Rollback
            </DialogTitle>
            <DialogDescription>
              {namespace} / {cluster} — Secret <span className="font-mono">{secretName}</span>
            </DialogDescription>
          </DialogHeader>

          {validationResult ? (
            <>
              <div className="space-y-3">
                <CertificateChainValidationPanel result={validationResult} />
              </div>
              <DialogFooter>
                <Button onClick={() => handleClose(false)}>Fechar</Button>
              </DialogFooter>
            </>
          ) : restoring ? (
            // Precisa vir ANTES da lista de backups: o AlertDialogAction de confirmação (abaixo)
            // fecha sozinho ao clicar (comportamento padrão do Radix), então sem esse estado
            // intermediário o usuário via a lista de backups de novo — parecia ter "voltado" em
            // vez de avançar pra aplicação do certificado, mesmo com a restauração em andamento.
            <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Restaurando certificado...
            </div>
          ) : loading ? (
            <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Carregando backups...
            </div>
          ) : loadError ? (
            <div className="text-sm text-red-500 py-4">{loadError}</div>
          ) : backups.length === 0 ? (
            <div className="text-sm text-muted-foreground py-8 text-center">
              Nenhum backup encontrado para este certificado — backups são criados automaticamente
              a cada atualização.
            </div>
          ) : (
            <ScrollArea className="max-h-[50vh] pr-4">
              <div className="space-y-2">
                {backups.map((b) => (
                  <div
                    key={b.backup_id}
                    className="flex items-center justify-between gap-3 rounded border border-border/50 p-2.5 text-xs"
                  >
                    <div className="space-y-0.5 min-w-0">
                      <p className="font-medium">{new Date(b.backed_up_at).toLocaleString("pt-BR")}</p>
                      <p className="text-muted-foreground truncate" title={b.subject}>
                        {b.subject || "(subject indisponível)"}
                      </p>
                      {b.not_after && (
                        <p className="text-muted-foreground">
                          Expirava em {new Date(b.not_after).toLocaleDateString("pt-BR")}
                        </p>
                      )}
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-shrink-0"
                      onClick={() => setPendingRestore(b)}
                    >
                      <RotateCcw className="h-3.5 w-3.5 mr-1.5" />
                      Restaurar
                    </Button>
                  </div>
                ))}
              </div>
            </ScrollArea>
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!pendingRestore} onOpenChange={(o) => !o && !restoringRef.current && setPendingRestore(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Restaurar este backup?</AlertDialogTitle>
            <AlertDialogDescription>
              Isso vai sobrescrever o certificado atualmente instalado em{" "}
              <strong>{namespace}/{secretName}</strong> pelo backup de{" "}
              <strong>{pendingRestore && new Date(pendingRestore.backed_up_at).toLocaleString("pt-BR")}</strong>.
              O estado atual também será salvo como um novo backup antes da restauração.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={restoring}>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmRestore} disabled={restoring}>
              {restoring && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Restaurar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
