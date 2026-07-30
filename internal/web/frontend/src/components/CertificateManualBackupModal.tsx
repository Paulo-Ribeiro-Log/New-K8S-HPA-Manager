import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Loader2, FolderPlus } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import type { CertificateInfo } from "@/types/certificates";

interface CertificateManualBackupModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cert: CertificateInfo | null;
}

/**
 * CertificateManualBackupModal — botão "Copiar para Backup" no detalhe de um certificado. Salva o
 * conteúdo JÁ INSTALADO num backup separado do RollbackStore (mecanismo distinto, disparado sob
 * demanda pelo usuário, não automaticamente antes de uma sobrescrita), com comentário opcional.
 */
export function CertificateManualBackupModal({ open, onOpenChange, cert }: CertificateManualBackupModalProps) {
  const { saveManualBackup } = useCertificates();
  const [comment, setComment] = useState("");
  const [saving, setSaving] = useState(false);

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) setComment("");
    onOpenChange(nextOpen);
  };

  const handleSave = async () => {
    if (!cert) return;
    setSaving(true);
    try {
      const info = await saveManualBackup(cert.cluster, cert.namespace, cert.secretName, comment.trim() || undefined);
      toast.success("Backup salvo", {
        description: `${cert.secretName} — ${new Date(info.backed_up_at).toLocaleString("pt-BR")}`,
      });
      handleClose(false);
    } catch (err) {
      toast.error("Erro ao salvar backup", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setSaving(false);
    }
  };

  if (!cert) return null;

  // Título mostra nome do cert, validade e data da cópia — pedido explícito do usuário, pra
  // identificar o backup só de bater o olho, sem precisar abrir metadata.
  const title = `${cert.secretName} — válido até ${new Date(cert.notAfter).toLocaleDateString("pt-BR")} — cópia de ${new Date().toLocaleDateString("pt-BR")}`;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderPlus className="h-5 w-5" />
            Copiar para Backup
          </DialogTitle>
          <DialogDescription className="break-words">{title}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3 text-xs border-b border-border/50 pb-3">
            <div>
              <span className="text-muted-foreground uppercase block mb-0.5">Cluster</span>
              <span className="font-medium">{cert.cluster}</span>
            </div>
            <div>
              <span className="text-muted-foreground uppercase block mb-0.5">Namespace</span>
              <span className="font-medium">{cert.namespace}</span>
            </div>
          </div>

          <div className="space-y-1">
            <Label htmlFor="manual-backup-comment" className="text-sm">
              Comentário <span className="text-muted-foreground">(opcional)</span>
            </Label>
            <textarea
              id="manual-backup-comment"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="ex: antes de renovar via AWX, cert funcionando em produção"
              className="w-full mt-1 h-24 p-2 text-sm bg-background border rounded resize-none"
              disabled={saving}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={saving}>
            Cancelar
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <FolderPlus className="h-4 w-4 mr-2" />}
            Salvar Backup
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
