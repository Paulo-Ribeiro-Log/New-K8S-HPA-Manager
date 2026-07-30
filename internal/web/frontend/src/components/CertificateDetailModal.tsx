import { useEffect, useState, type ReactNode } from "react";
import { Shield, ExternalLink, Lock, ShieldCheck, Loader2, History } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useCertificates } from "@/hooks/useCertificates";
import { CertificateChainValidationPanel } from "@/components/CertificateChainValidationPanel";
import { CertificateRollbackModal } from "@/components/CertificateRollbackModal";
import type { CertificateInfo, ChainValidationResult } from "@/types/certificates";

interface CertificateDetailModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cert: CertificateInfo | null;
  /** Botões extras no footer (ex: "Copiar para..." da aba Certificados) */
  footerExtra?: ReactNode;
  /** Chamado após um rollback bem-sucedido via o botão "Backups / Rollback" — cada tab decide o
   *  que "atualizar" significa pra si. Opcional. */
  onRestored?: () => void;
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium truncate ml-2 max-w-[60%] text-right" title={value}>
        {value}
      </span>
    </div>
  );
}

function TimelineBar({ cert }: { cert: CertificateInfo }) {
  const now = new Date().getTime();
  const start = new Date(cert.notBefore).getTime();
  const end = new Date(cert.notAfter).getTime();
  const total = end - start;
  const elapsed = now - start;
  const percent = Math.max(0, Math.min(100, (elapsed / total) * 100));

  const barColor =
    cert.status === "expired"
      ? "bg-red-500"
      : cert.status === "expiring"
      ? "bg-yellow-500"
      : "bg-green-500";

  return (
    <div>
      <div className="flex justify-between text-xs text-muted-foreground mb-1">
        <span>{new Date(cert.notBefore).toLocaleDateString("pt-BR")}</span>
        <span>{new Date(cert.notAfter).toLocaleDateString("pt-BR")}</span>
      </div>
      <div className="w-full h-3 bg-muted rounded-full overflow-hidden">
        <div
          className={`h-full ${barColor} rounded-full transition-all`}
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="text-xs text-center text-muted-foreground mt-1">
        {percent.toFixed(0)}% decorrido
      </p>
    </div>
  );
}

export function getStatusBadge(status: string) {
  switch (status) {
    case "valid":
      return <Badge className="bg-green-500/20 text-green-400 border-green-500/30">Valido</Badge>;
    case "expiring":
      return <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30">Expirando</Badge>;
    case "expired":
      return <Badge className="bg-red-500/20 text-red-400 border-red-500/30">Expirado</Badge>;
    default:
      return <Badge variant="secondary">{status}</Badge>;
  }
}

export function CertificateDetailModal({
  open,
  onOpenChange,
  cert,
  footerExtra,
  onRestored,
}: CertificateDetailModalProps) {
  const { validateInstalledChain } = useCertificates();
  const [validationResult, setValidationResult] = useState<ChainValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [rollbackModalOpen, setRollbackModalOpen] = useState(false);

  // Pré-popula com o resultado já calculado durante o scan (Scanner.scanCluster chama
  // ValidateCertificateChain pra cada secret) — evita precisar clicar em "Validar Cadeia" só pra
  // ver o que o scan já sabe. O botão continua disponível pra uma checagem nova/ao vivo (que também
  // traz LivePropagation via Prometheus, ausente do resultado do scan).
  useEffect(() => {
    if (!open) return;
    setValidationResult(cert?.chainValidation ?? null);
  }, [open, cert?.cluster, cert?.namespace, cert?.secretName, cert?.chainValidation]);

  const handleValidateChain = async () => {
    if (!cert) return;
    setValidating(true);
    try {
      const result = await validateInstalledChain(cert.cluster, cert.namespace, cert.secretName);
      setValidationResult(result);
    } catch {
      // best-effort — falha na validação não deveria travar o resto do modal
      setValidationResult(null);
    } finally {
      setValidating(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setValidationResult(null);
        onOpenChange(next);
      }}
    >
      <DialogContent className="max-w-2xl max-h-[85vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            {cert?.secretName}
          </DialogTitle>
          <DialogDescription>
            {cert?.namespace} / {cert?.cluster}
          </DialogDescription>
        </DialogHeader>

        {cert && (
          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="space-y-4">
              {/* Status + dias restantes */}
              <div className="flex items-center justify-between">
                {getStatusBadge(cert.status)}
                <span className={`text-sm font-bold ${
                  cert.daysRemaining < 0 ? "text-red-400" :
                  cert.daysRemaining <= 30 ? "text-yellow-400" :
                  "text-green-400"
                }`}>
                  {cert.daysRemaining} dias restantes
                </span>
              </div>

              <TimelineBar cert={cert} />

              {/* x509 Info */}
              <Card className="bg-card/50">
                <CardHeader className="py-2 px-3">
                  <CardTitle className="text-xs font-medium">Informacoes x509</CardTitle>
                </CardHeader>
                <CardContent className="px-3 pb-3 space-y-1">
                  <InfoRow label="Subject" value={cert.subject} />
                  <InfoRow label="Issuer" value={cert.issuer} />
                  <InfoRow label="Serial" value={cert.serialNumber} />
                  <InfoRow label="Algoritmo" value={`${cert.keyAlgorithm} ${cert.keySize}bit`} />
                  <InfoRow label="CA" value={cert.isCA ? "Sim" : "Nao"} />
                  <InfoRow label="Emitido" value={new Date(cert.notBefore).toLocaleDateString("pt-BR")} />
                  <InfoRow label="Expira" value={new Date(cert.notAfter).toLocaleDateString("pt-BR")} />
                </CardContent>
              </Card>

              {/* SANs */}
              {cert.dnsNames && cert.dnsNames.length > 0 && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium">
                      SANs ({cert.dnsNames.length})
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-3 pb-3">
                    <div className="flex flex-wrap gap-1">
                      {cert.dnsNames.map((san, i) => (
                        <Badge key={i} variant="secondary" className="text-xs">
                          {san}
                        </Badge>
                      ))}
                    </div>
                  </CardContent>
                </Card>
              )}

              {/* Ingresses */}
              {cert.usedByIngresses && cert.usedByIngresses.length > 0 && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium flex items-center gap-1">
                      <ExternalLink className="h-3 w-3" />
                      Ingresses ({cert.usedByIngresses.length})
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-3 pb-3 space-y-2">
                    {cert.usedByIngresses.map((ing, i) => (
                      <div key={i} className="text-xs border-b border-border/30 pb-1">
                        <p className="font-medium">{ing.namespace}/{ing.name}</p>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {ing.hosts?.map((host, j) => (
                            <Badge key={j} variant="outline" className="text-xs">
                              {host}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    ))}
                  </CardContent>
                </Card>
              )}

              {/* cert-manager */}
              {cert.certManager && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium flex items-center gap-1">
                      <Lock className="h-3 w-3" />
                      cert-manager
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-3 pb-3 space-y-1">
                    <InfoRow label="Certificate" value={cert.certManager.certificateName} />
                    <InfoRow label="Issuer" value={cert.certManager.issuerName} />
                    <InfoRow label="Tipo" value={cert.certManager.issuerKind} />
                    <InfoRow label="Ready" value={cert.certManager.isReady ? "Sim" : "Nao"} />
                  </CardContent>
                </Card>
              )}

              {/* Chain */}
              {cert.chainDetails && cert.chainDetails.length > 0 && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium">
                      Chain ({cert.chainLength} certificados)
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-3 pb-3 space-y-2">
                    {cert.chainDetails.map((chain, i) => (
                      <div key={i} className="text-xs border-b border-border/30 pb-1">
                        <p className="font-medium">{chain.subject}</p>
                        <p className="text-muted-foreground">Issuer: {chain.issuer}</p>
                        <p className="text-muted-foreground">
                          Expira: {new Date(chain.notAfter).toLocaleDateString("pt-BR")}
                          {chain.isCA && (
                            <Badge className="ml-1 text-xs" variant="secondary">CA</Badge>
                          )}
                        </p>
                      </div>
                    ))}
                  </CardContent>
                </Card>
              )}

              {/* Validação de cadeia (Fase 1 do CERT-ROLLBACK-VALIDATION-PLAN.md) — disparo
                  manual, útil pra reconferir um cert instalado há tempos. */}
              {validationResult && <CertificateChainValidationPanel result={validationResult} />}
            </div>
          </ScrollArea>
        )}

        <DialogFooter className="flex-wrap gap-2 sm:space-x-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
          {cert && (
            <Button variant="outline" onClick={handleValidateChain} disabled={validating}>
              {validating ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <ShieldCheck className="h-4 w-4 mr-2" />
              )}
              Validar Cadeia
            </Button>
          )}
          {cert && (
            <Button variant="outline" onClick={() => setRollbackModalOpen(true)}>
              <History className="h-4 w-4 mr-2" />
              Backups / Rollback
            </Button>
          )}
          {footerExtra}
        </DialogFooter>
      </DialogContent>

      {cert && (
        <CertificateRollbackModal
          open={rollbackModalOpen}
          onOpenChange={setRollbackModalOpen}
          cluster={cert.cluster}
          namespace={cert.namespace}
          secretName={cert.secretName}
          onRestored={onRestored}
        />
      )}
    </Dialog>
  );
}
