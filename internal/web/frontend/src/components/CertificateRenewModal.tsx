import { useEffect, useState } from "react";
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
import { Switch } from "@/components/ui/switch";
import { Loader2, RefreshCw, TriangleAlert, FolderOpen } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { CertificateChainValidationPanel } from "@/components/CertificateChainValidationPanel";
import { CertificateSourcePickerModal } from "@/components/CertificateSourcePickerModal";
import { AWXCertForm } from "@/components/AWXCertForm";
import { apiClient } from "@/lib/api/client";
import { countPemCertificates } from "@/lib/pemUtils";
import type { CertificateInfo, ChainValidationResult } from "@/types/certificates";

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
  const { uploadCertificateWithValidation, validateInstalledChain, backupCertificate } = useCertificates();
  const [tlsCrt, setTlsCrt] = useState("");
  const [tlsKey, setTlsKey] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const [awxConfigured, setAwxConfigured] = useState(false);
  const [mode, setMode] = useState<"manual" | "awx">("manual");

  // Validação de cadeia (Fase 1 do CERT-ROLLBACK-VALIDATION-PLAN.md) — disparada automaticamente
  // logo após o certificado ser instalado, tanto pelo caminho manual (já vem no campo
  // "validation" da resposta de /upload) quanto pelo AWX (que só sinaliza sucesso via evento SSE,
  // sem devolver o cert — por isso busca de novo via GET .../validate-chain).
  const [validationResult, setValidationResult] = useState<ChainValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [sourcePickerOpen, setSourcePickerOpen] = useState(false);

  useEffect(() => {
    apiClient
      .getAWXStatus()
      .then((s) => setAwxConfigured(s.configured && s.reachable))
      .catch(() => setAwxConfigured(false));
  }, []);

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      setTlsCrt("");
      setTlsKey("");
      setMode("manual");
      setValidationResult(null);
    }
    onOpenChange(nextOpen);
  };

  const handleSubmit = async () => {
    setIsSubmitting(true);
    try {
      const { validation } = await uploadCertificateWithValidation({
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
      setValidationResult(validation);
      onSuccess();
      // Modal fica aberto pra exibir o resultado da validação — usuário fecha manualmente.
    } catch (err) {
      toast.error("Erro ao atualizar certificado", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  // Chamado quando o job AWX termina com sucesso — diferente do caminho manual, a resposta do
  // AWX vem só via evento SSE de "concluído" (sem devolver o cert), então buscamos a validação de
  // novo lendo o Secret já atualizado no cluster.
  const handleAwxSuccess = async () => {
    setMode("manual"); // troca já, ANTES da validação — senão a área de loading/painel nunca aparece
    setValidating(true);
    try {
      const result = await validateInstalledChain(cluster, namespace, secretName);
      setValidationResult(result);
    } catch {
      // best-effort — se a validação falhar (ex: rede), o usuário ainda sabe que o AWX terminou
      // com sucesso via toast já emitido pelo próprio AWXCertForm
    } finally {
      setValidating(false);
    }
    onSuccess();
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className={mode === "awx" ? "max-w-2xl max-h-[90vh] flex flex-col" : "max-w-lg"}>
        <DialogHeader>
          <div className="flex items-start justify-between gap-4 pr-6">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <RefreshCw className="h-5 w-5" />
                Atualizar Certificado TLS
              </DialogTitle>
              <DialogDescription className="mt-1">
                {mode === "awx"
                  ? "Instalar ou renovar via AWX"
                  : (
                    <>
                      {namespace} / {cluster} — Secret <span className="font-mono">{secretName}</span>
                    </>
                  )}
              </DialogDescription>
            </div>
            <div className="flex flex-col items-end gap-1.5 flex-shrink-0 mt-1">
              {certInfo && (
                <div className="flex flex-col items-end gap-1">
                  {getStatusBadge(certInfo.status)}
                  <span className="text-xs text-muted-foreground">
                    {certInfo.daysRemaining < 0
                      ? `Expirado há ${Math.abs(certInfo.daysRemaining)}d`
                      : `${certInfo.daysRemaining}d restantes`}
                  </span>
                </div>
              )}
              {awxConfigured && (
                <div className="flex items-center gap-2">
                  <span className={`text-xs ${mode === "manual" ? "font-medium text-foreground" : "text-muted-foreground"}`}>
                    Manual
                  </span>
                  <Switch
                    checked={mode === "awx"}
                    onCheckedChange={(checked) => setMode(checked ? "awx" : "manual")}
                  />
                  <span className={`text-xs ${mode === "awx" ? "font-medium text-foreground" : "text-muted-foreground"}`}>
                    AWX
                  </span>
                </div>
              )}
            </div>
          </div>
        </DialogHeader>

        {mode === "awx" ? (
          <div className="overflow-y-auto flex-1 min-h-0">
            <AWXCertForm
              key={`${cluster}-${namespace}`}
              cluster={cluster}
              namespace={namespace}
              onCancel={() => handleClose(false)}
              onSuccess={handleAwxSuccess}
              onBeforeLaunch={() => backupCertificate(cluster, namespace, secretName).then(() => {})}
            />
          </div>
        ) : validationResult ? (
          <>
            <div className="space-y-3">
              <CertificateChainValidationPanel result={validationResult} />
            </div>
            <DialogFooter>
              <Button onClick={() => handleClose(false)}>Fechar</Button>
            </DialogFooter>
          </>
        ) : validating ? (
          <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> Validando cadeia do certificado instalado...
          </div>
        ) : (
          <>
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

              <div className="flex justify-end">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="text-xs h-7"
                  onClick={() => setSourcePickerOpen(true)}
                  disabled={isSubmitting}
                >
                  <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                  Escolher de um backup...
                </Button>
              </div>

              <div>
                <div className="flex items-center justify-between">
                  <Label className="text-sm">Certificado (tls.crt — PEM)</Label>
                  {tlsCrt.trim() && (
                    <span className="text-[11px] text-muted-foreground">
                      {countPemCertificates(tlsCrt)} certificado(s) neste campo
                      {countPemCertificates(tlsCrt) > 1 && " (chain incluída)"}
                    </span>
                  )}
                </div>
                {/* Caixa fixa (~6-8 linhas visíveis) — um bundle leaf+chain tem 60-90 linhas de
                    PEM, então sem o contador acima parece "só 1 certificado" mesmo com a chain
                    presente, exigindo rolar pra confirmar. Ver CLAUDE.md. */}
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
          </>
        )}
      </DialogContent>

      <CertificateSourcePickerModal
        open={sourcePickerOpen}
        onOpenChange={setSourcePickerOpen}
        cluster={cluster}
        namespace={namespace}
        secretName={secretName}
        onSelect={(crt, key) => {
          setTlsCrt(crt);
          setTlsKey(key);
        }}
      />
    </Dialog>
  );
}
