import { useEffect, useMemo, useState } from "react";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Loader2, FolderOpen, CheckCircle2 } from "lucide-react";
import { toast } from "sonner";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { CertificateSourcePickerModal } from "@/components/CertificateSourcePickerModal";
import { useCertificates } from "@/hooks/useCertificates";
import { countPemCertificates } from "@/lib/pemUtils";
import type { MutatingWebhookConfigSummary, UpdateMutatingWebhookCABundleResult } from "@/types/certificates";

interface WebhookCABundleUpdateModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  config: MutatingWebhookConfigSummary | null;
  onSuccess: () => void;
}

// Modal de atualização do caBundle de um MutatingWebhookConfiguration (Delinea DSV injector,
// Istio sidecar injector, ou qualquer outro webhook cuja confiança do apiserver precise ser
// rotacionada junto com o certificado de serviço). Não existe RollbackStore dedicado pra esta
// operação (ver comentário do backend, internal/certificates/mutating_webhook.go) — o "before" de
// cada entrada continua visível na resposta de sucesso, pra copiar/anotar manualmente se preciso.
export function WebhookCABundleUpdateModal({ open, onOpenChange, cluster, config, onSuccess }: WebhookCABundleUpdateModalProps) {
  const { updateMutatingWebhookCABundle } = useCertificates();

  const [selectedNames, setSelectedNames] = useState<Set<string>>(new Set());
  const [caBundlePEM, setCaBundlePEM] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<UpdateMutatingWebhookCABundleResult | null>(null);

  // Resetar ao abrir pra um config diferente (ou reabrir o mesmo depois de já ter concluído
  // uma atualização) — sem isso, os checkboxes/textarea ficariam com o estado da última vez.
  useEffect(() => {
    if (open) {
      setSelectedNames(new Set(config?.webhooks.map((w) => w.name) ?? []));
      setCaBundlePEM("");
      setResult(null);
    }
  }, [open, config]);

  const pemCertCount = useMemo(() => countPemCertificates(caBundlePEM), [caBundlePEM]);
  const canSubmit = selectedNames.size > 0 && pemCertCount > 0;

  const toggleName = (name: string) => {
    setSelectedNames((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const handleConfirm = async () => {
    if (!config) return;
    setConfirmOpen(false);
    setSubmitting(true);
    try {
      const res = await updateMutatingWebhookCABundle({
        cluster,
        configName: config.name,
        webhookNames: Array.from(selectedNames),
        caBundlePEM,
      });
      setResult(res);
      toast.success(`caBundle atualizado em ${res.updatedCount} entrada(s) de ${config.name}`);
      onSuccess();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao atualizar caBundle");
    } finally {
      setSubmitting(false);
    }
  };

  if (!config) return null;

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Atualizar CA Bundle — {config.name}</DialogTitle>
            <DialogDescription>
              Sobrescreve o certificado (CA/chain) que o kube-apiserver confia ao chamar este
              webhook. Use quando o certificado de serviço do webhook (ex: Delinea DSV injector,
              Istio sidecar injector) foi renovado, mas o MutatingWebhookConfiguration continua
              apontando pro CA antigo.
            </DialogDescription>
          </DialogHeader>

          {result ? (
            <div className="space-y-3">
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle2 className="h-4 w-4" />
                {result.updatedCount} entrada(s) atualizada(s): {result.updatedNames.join(", ")}
              </div>
              <div className="text-sm space-y-1 p-3 bg-muted rounded-md">
                <div><span className="text-muted-foreground">Novo subject:</span> {result.newSubject}</div>
                <div><span className="text-muted-foreground">Novo issuer:</span> {result.newIssuer}</div>
                {result.newNotAfter && (
                  <div><span className="text-muted-foreground">Válido até:</span> {new Date(result.newNotAfter).toLocaleString("pt-BR")}</div>
                )}
              </div>
              <div className="text-xs text-muted-foreground">
                Estado anterior (pra referência, caso precise reverter manualmente):
                {result.before.map((b) => (
                  <div key={b.name} className="ml-2">
                    • {b.name}: {b.caBundleEmpty ? "vazio" : `${b.caBundleSubject || "(sem subject)"} (emitido por ${b.caBundleIssuer || "?"})`}
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Entradas a atualizar (webhooks[])</Label>
                <div className="space-y-2 rounded-md border p-3">
                  {config.webhooks.map((wh) => (
                    <div key={wh.name} className="flex items-start gap-2">
                      <Checkbox
                        checked={selectedNames.has(wh.name)}
                        onCheckedChange={() => toggleName(wh.name)}
                        className="mt-0.5"
                      />
                      <div className="text-sm flex-1 min-w-0">
                        <div className="font-mono text-xs truncate" title={wh.name}>{wh.name}</div>
                        <div className="flex items-center gap-2 flex-wrap mt-0.5">
                          {wh.caBundleEmpty ? (
                            <Badge variant="secondary" className="text-xs">caBundle vazio</Badge>
                          ) : (
                            <>
                              {wh.caBundleStatus && getStatusBadge(wh.caBundleStatus)}
                              <span className="text-xs text-muted-foreground truncate">
                                {wh.caBundleSubject} — emitido por {wh.caBundleIssuer}
                              </span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="ca-bundle-pem">Novo CA Bundle (PEM)</Label>
                  <Button variant="outline" size="sm" onClick={() => setPickerOpen(true)}>
                    <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                    Selecionar de backup existente
                  </Button>
                </div>
                <Textarea
                  id="ca-bundle-pem"
                  value={caBundlePEM}
                  onChange={(e) => setCaBundlePEM(e.target.value)}
                  placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                  className="font-mono text-xs h-32"
                />
                <p className="text-xs text-muted-foreground">
                  {pemCertCount > 0
                    ? `${pemCertCount} certificado(s) neste campo.`
                    : "Cole o certificado (ou chain completa) que deve ser confiado — pode incluir vários blocos CERTIFICATE."}
                </p>
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
              {result ? "Fechar" : "Cancelar"}
            </Button>
            {!result && (
              <Button onClick={() => setConfirmOpen(true)} disabled={!canSubmit || submitting}>
                {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Atualizar CA Bundle
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar atualização de CA Bundle?</AlertDialogTitle>
            <AlertDialogDescription>
              Isso sobrescreve o caBundle de {selectedNames.size} entrada(s) em{" "}
              <strong>{config.name}</strong> (cluster {cluster}). Se o certificado colado estiver
              errado, o kube-apiserver deixará de confiar neste webhook — dependendo do{" "}
              <code>failurePolicy</code> configurado, isso pode bloquear a criação/edição de
              recursos que dependem dele. Confira o conteúdo colado antes de confirmar.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirm}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <CertificateSourcePickerModal
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        cluster=""
        namespace=""
        secretName=""
        defaultTab="manual"
        onSelect={(tlsCrt) => {
          // caBundle nunca precisa da chave privada — só o(s) certificado(s) público(s).
          setCaBundlePEM(tlsCrt);
          setPickerOpen(false);
        }}
      />
    </>
  );
}
