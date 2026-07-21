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
import { Loader2, RefreshCw, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import type { CertificateInfo } from "@/types/certificates";

interface CertificateRenewModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  secretName: string;
  certInfo: CertificateInfo | null;
  onSuccess: () => void;
}

export function CertificateRenewModal({
  open,
  onOpenChange,
  cluster,
  namespace,
  secretName,
  certInfo,
  onSuccess,
}: CertificateRenewModalProps) {
  const { uploadCertificate } = useCertificates();
  const [tlsCrt, setTlsCrt] = useState("");
  const [tlsKey, setTlsKey] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      setTlsCrt("");
      setTlsKey("");
    }
    onOpenChange(nextOpen);
  };

  const handleSubmit = async () => {
    setIsSubmitting(true);
    try {
      await uploadCertificate({
        name: secretName,
        tlsCrt,
        tlsKey,
        targetClusters: [cluster],
        targetNamespaces: [namespace],
      });
      toast.success("Certificado atualizado", {
        description: `${namespace}/${secretName}`,
      });
      setTlsCrt("");
      setTlsKey("");
      onOpenChange(false);
      onSuccess();
    } catch (err) {
      toast.error("Erro ao atualizar certificado", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <div className="flex items-start justify-between gap-4">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <RefreshCw className="h-5 w-5" />
                Atualizar Certificado TLS
              </DialogTitle>
              <DialogDescription className="mt-1">
                {namespace} / {cluster} — Secret <span className="font-mono">{secretName}</span>
              </DialogDescription>
            </div>
            {certInfo && (
              <div className="flex flex-col items-end gap-1 flex-shrink-0">
                {getStatusBadge(certInfo.status)}
                <span className="text-xs text-muted-foreground">
                  {certInfo.daysRemaining < 0
                    ? `Expirado há ${Math.abs(certInfo.daysRemaining)}d`
                    : `${certInfo.daysRemaining}d restantes`}
                </span>
              </div>
            )}
          </div>
        </DialogHeader>

        <div className="space-y-4">
          {certInfo?.certManager && (
            <div className="flex items-start gap-2 text-xs bg-yellow-500/10 border border-yellow-500/30 text-yellow-400 rounded p-2.5">
              <TriangleAlert className="h-4 w-4 flex-shrink-0 mt-0.5" />
              <span>
                Este Secret é gerenciado por cert-manager (Certificate{" "}
                <span className="font-mono">{certInfo.certManager.certificateName}</span>). A
                atualização manual pode ser sobrescrita na próxima reconciliação.
              </span>
            </div>
          )}

          <div>
            <Label className="text-sm">Certificado (tls.crt — PEM)</Label>
            <textarea
              value={tlsCrt}
              onChange={(e) => setTlsCrt(e.target.value)}
              placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
              className="w-full mt-1 h-32 p-2 text-xs font-mono bg-background border rounded resize-none"
              disabled={isSubmitting}
            />
          </div>
          <div>
            <Label className="text-sm">Chave Privada (tls.key — PEM)</Label>
            <textarea
              value={tlsKey}
              onChange={(e) => setTlsKey(e.target.value)}
              placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
              className="w-full mt-1 h-32 p-2 text-xs font-mono bg-background border rounded resize-none"
              disabled={isSubmitting}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isSubmitting}>
            Cancelar
          </Button>
          <Button onClick={handleSubmit} disabled={isSubmitting || !tlsCrt.trim() || !tlsKey.trim()}>
            {isSubmitting ? (
              <Loader2 className="h-4 w-4 mr-2 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4 mr-2" />
            )}
            Atualizar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
