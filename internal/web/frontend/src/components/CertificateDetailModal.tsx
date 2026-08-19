import { useEffect, useState, type ReactNode } from "react";
import { Shield, ExternalLink, Waypoints, Lock, ShieldCheck, ShieldAlert, AlertTriangle, Loader2, History, FolderPlus } from "lucide-react";
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
import { CertificateManualBackupModal } from "@/components/CertificateManualBackupModal";
import type { CertificateInfo, ChainValidationResult, HostIssue, BackendTLSCheckResult } from "@/types/certificates";

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

// HostBadges — badges de host com indicador de severidade (borda/ícone vermelho para "error",
// âmbar para "warning") quando hostIssues contém 1+ entrada pra aquele host — compartilhado entre
// o card Ingresses e o card Gateways, ambos iterando hosts[] + hostIssues[] no mesmo formato
// (IngressRef/GatewayRef, ver types/certificates.ts).
function HostBadges({ hosts, hostIssues }: { hosts: string[]; hostIssues?: HostIssue[] }) {
  return (
    <div className="flex flex-wrap gap-1 mt-1">
      {hosts.map((host, j) => {
        const issues = hostIssues?.filter((i) => i.host === host) ?? [];
        const hasError = issues.some((i) => i.severity === "error");
        const hasWarning = issues.some((i) => i.severity === "warning");
        const colorClass = hasError
          ? "border-red-500/50 text-red-400"
          : hasWarning
          ? "border-amber-500/50 text-amber-400"
          : "";
        return (
          <Badge
            key={j}
            variant="outline"
            className={`text-xs ${colorClass}`}
            title={issues.length > 0 ? issues.map((i) => i.message).join("; ") : undefined}
          >
            {hasError ? (
              <ShieldAlert className="h-3 w-3 mr-1" />
            ) : hasWarning ? (
              <AlertTriangle className="h-3 w-3 mr-1" />
            ) : null}
            {host}
          </Badge>
        );
      })}
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
  const { validateInstalledChain, checkBackendTLS } = useCertificates();
  const [validationResult, setValidationResult] = useState<ChainValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [rollbackModalOpen, setRollbackModalOpen] = useState(false);
  const [manualBackupModalOpen, setManualBackupModalOpen] = useState(false);
  const [backendTLSResult, setBackendTLSResult] = useState<BackendTLSCheckResult | null>(null);
  const [checkingBackendTLS, setCheckingBackendTLS] = useState(false);

  // Pré-popula com o resultado já calculado durante o scan (Scanner.scanCluster chama
  // ValidateCertificateChain pra cada secret) — pintura instantânea, sem esperar rede, enquanto a
  // checagem completa (efeito abaixo) ainda não respondeu.
  useEffect(() => {
    if (!open) return;
    setValidationResult(cert?.chainValidation ?? null);
    setBackendTLSResult(null); // Diagnóstico Avançado é sempre sob demanda, nunca herda do cert anterior
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

  // Diagnóstico Avançado (Fase 8) — sempre disparo manual (nunca automático): mais custoso que
  // handleValidateChain (lê logs de todos os pods do ingress-controller) e só se aplica a uma
  // minoria de Ingresses (backend-protocol HTTPS/GRPCS ou ssl-passthrough).
  const handleCheckBackendTLS = async () => {
    if (!cert) return;
    setCheckingBackendTLS(true);
    try {
      const result = await checkBackendTLS(cert.cluster, cert.namespace, cert.secretName);
      setBackendTLSResult(result);
    } catch {
      setBackendTLSResult(null);
    } finally {
      setCheckingBackendTLS(false);
    }
  };

  // Dispara a validação completa automaticamente ao abrir o modal — inclui LivePropagation
  // (Prometheus/handshake TLS direto, Fase 3/4), ausente do resultado pré-populado do scan. Sem
  // isso, a linha de propagação nunca aparecia a menos que o usuário clicasse manualmente em
  // "Validar Cadeia", o que não era óbvio. Só dispara de novo ao trocar de certificado (não
  // depende de cert.chainValidation) — o botão "Validar Cadeia" continua disponível pra reconferir
  // sob demanda (ex: depois de uma atualização de certificado feita fora desta sessão).
  useEffect(() => {
    if (!open || !cert) return;
    handleValidateChain();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, cert?.cluster, cert?.namespace, cert?.secretName]);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setValidationResult(null);
          setBackendTLSResult(null);
        }
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
                        <div className="flex items-center gap-1.5 flex-wrap">
                          <p className="font-medium">{ing.namespace}/{ing.name}</p>
                          {ing.ingressClass && (
                            <Badge variant="secondary" className="text-[10px]">{ing.ingressClass}</Badge>
                          )}
                        </div>
                        <HostBadges hosts={ing.hosts ?? []} hostIssues={ing.hostIssues} />
                      </div>
                    ))}

                    {/* Diagnóstico Avançado (Fase 8) — só quando ao menos 1 Ingress usa
                        re-encryption pro backend (backend-protocol HTTPS/GRPCS ou
                        ssl-passthrough). Sempre sob demanda, nunca automático. */}
                    {cert.usedByIngresses.some((ing) => ing.backendTLS) && (
                      <div className="pt-1 space-y-2">
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs w-full"
                          onClick={handleCheckBackendTLS}
                          disabled={checkingBackendTLS}
                        >
                          {checkingBackendTLS ? (
                            <Loader2 className="h-3 w-3 mr-2 animate-spin" />
                          ) : (
                            <Waypoints className="h-3 w-3 mr-2" />
                          )}
                          Diagnóstico Avançado — TLS com backend
                        </Button>

                        {backendTLSResult && (
                          <div className="rounded border border-border/50 bg-card/30 p-2 text-xs space-y-1">
                            {backendTLSResult.signals && backendTLSResult.signals.length > 0 && (
                              <ul className="space-y-0.5">
                                {backendTLSResult.signals.map((s, i) => (
                                  <li key={i} className="text-red-500">• {s}</li>
                                ))}
                              </ul>
                            )}
                            {backendTLSResult.notes && backendTLSResult.notes.length > 0 && (
                              <ul className="space-y-0.5 text-muted-foreground">
                                {backendTLSResult.notes.map((n, i) => (
                                  <li key={i}>• {n}</li>
                                ))}
                              </ul>
                            )}
                          </div>
                        )}
                      </div>
                    )}
                  </CardContent>
                </Card>
              )}

              {/* Gateways (Gateway API) — mesmo padrão do card Ingresses */}
              {cert.usedByGateways && cert.usedByGateways.length > 0 && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium flex items-center gap-1">
                      <Waypoints className="h-3 w-3" />
                      Gateways ({cert.usedByGateways.length})
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="px-3 pb-3 space-y-2">
                    {cert.usedByGateways.map((gw, i) => (
                      <div key={i} className="text-xs border-b border-border/30 pb-1">
                        <p className="font-medium">{gw.namespace}/{gw.name}</p>
                        <HostBadges hosts={gw.hosts ?? []} hostIssues={gw.hostIssues} />
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

              {/* Chain — bug real corrigido (relatado ao vivo: "Chain (3 certificados)" só listava
                  2 itens, parecia inconsistente/errado). Não era: `cert.chainLength` (backend,
                  ParseTLSSecret) conta a chain INTEIRA (leaf + CAs), mas `cert.chainDetails` só
                  traz os certificados ALÉM do leaf (`certs[1:]`) — o leaf já é o certificado
                  principal, mostrado em Subject/Issuer/Expira no topo deste mesmo modal, então
                  repeti-lo aqui seria redundante. O título usava a contagem errada (chainLength,
                  a da chain inteira) pra rotular uma lista que só tem os N-1 restantes — corrigido
                  pra usar `chainDetails.length` (o que de fato é listado abaixo) e deixar explícito
                  que o certificado principal já foi mostrado antes. */}
              {cert.chainDetails && cert.chainDetails.length > 0 && (
                <Card className="bg-card/50">
                  <CardHeader className="py-2 px-3">
                    <CardTitle className="text-xs font-medium">
                      Chain — CAs ({cert.chainDetails.length}, além do certificado principal acima)
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
          {cert && (
            <Button variant="outline" onClick={() => setManualBackupModalOpen(true)}>
              <FolderPlus className="h-4 w-4 mr-2" />
              Copiar para Backup
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

      <CertificateManualBackupModal
        open={manualBackupModalOpen}
        onOpenChange={setManualBackupModalOpen}
        cert={cert}
      />
    </Dialog>
  );
}
