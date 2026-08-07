import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Loader2 } from "lucide-react";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { useCertEndpointHistory } from "@/hooks/useCertEndpoints";
import type { CertEndpointWithStatus } from "@/lib/api/types";

interface ExternalEndpointDetailModalProps {
  endpoint: CertEndpointWithStatus | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Modal de detalhe do endpoint externo — enxuto e independente de CertificateDetailModal.tsx de
// propósito: esse é fortemente tipado em cima de CertificateInfo, com campos K8s-only
// (UsedByIngresses/UsedByGateways/CertManager) que não existem aqui. getStatusBadge é a única
// peça reaproveitada (mesmas cores/rótulos de status em toda a app).
export function ExternalEndpointDetailModal({ endpoint, open, onOpenChange }: ExternalEndpointDetailModalProps) {
  const { data: history = [], isLoading } = useCertEndpointHistory(open ? endpoint?.id ?? null : null);

  if (!endpoint) return null;
  const latest = endpoint.latest_check;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 flex-wrap">
            {endpoint.name}
            {latest?.success && latest.is_default_fake_cert && (
              <Badge className="bg-orange-500/20 text-orange-400 border-orange-500/30">
                Cert. Fake (Ingress)
              </Badge>
            )}
            {latest?.success && !latest.is_default_fake_cert && latest.status && getStatusBadge(latest.status)}
            {latest && !latest.success && (
              <Badge className="bg-red-500/20 text-red-400 border-red-500/30">Erro</Badge>
            )}
          </DialogTitle>
          <DialogDescription className="font-mono text-xs">
            {endpoint.host}:{endpoint.port}
            {endpoint.sni ? ` (SNI: ${endpoint.sni})` : ""}
          </DialogDescription>
        </DialogHeader>

        {!latest ? (
          <p className="text-sm text-muted-foreground py-4">Este endpoint ainda não foi verificado.</p>
        ) : !latest.success ? (
          <div className="text-sm text-red-400 py-2 break-words">
            Falha na última checagem: {latest.error_message || "erro desconhecido"}
          </div>
        ) : (
          <div className="space-y-2 text-sm py-2">
            {latest.is_default_fake_cert && (
              <div className="rounded-md border border-orange-500/30 bg-orange-500/10 text-orange-300 text-xs px-3 py-2">
                Este é o certificado autoassinado <strong>padrão</strong> do ingress-nginx, não o
                certificado real da aplicação — indica que o SNI/host deste endpoint não bate com
                nenhum Ingress configurado no cluster (ou não há TLS configurado pra ele). O
                certificado abaixo não expira tão cedo, mas isso não significa que este endpoint
                tenha TLS válido de verdade.
              </div>
            )}
            <DetailRow label="Subject" value={latest.subject} />
            <DetailRow label="Emissor" value={latest.issuer} />
            <DetailRow label="Serial" value={latest.serial_number} mono />
            <DetailRow
              label="Válido de"
              value={latest.not_before ? new Date(latest.not_before).toLocaleDateString("pt-BR") : undefined}
            />
            <DetailRow
              label="Válido até"
              value={latest.not_after ? new Date(latest.not_after).toLocaleDateString("pt-BR") : undefined}
            />
            <DetailRow label="Dias restantes" value={String(latest.days_remaining ?? "—")} />
            <DetailRow label="Tamanho da cadeia" value={String(latest.chain_length ?? "—")} />
            <DetailRow
              label="Confiável por CA pública"
              value={latest.trusted_by_public_ca ? "Sim" : "Não (comum em CA interna/autoassinado)"}
            />
            {latest.dns_names && latest.dns_names.length > 0 && (
              <div>
                <span className="text-muted-foreground">SAN (DNS Names):</span>
                <div className="flex flex-wrap gap-1 mt-1">
                  {latest.dns_names.map((d) => (
                    <Badge key={d} variant="outline" className="text-[10px]">
                      {d}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        <Separator />

        <div>
          <p className="text-xs font-medium text-muted-foreground mb-2">Histórico de checagens</p>
          {isLoading ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
              <Loader2 className="h-3.5 w-3.5 animate-spin" /> Carregando...
            </div>
          ) : history.length === 0 ? (
            <p className="text-xs text-muted-foreground">Nenhuma checagem registrada ainda.</p>
          ) : (
            <div className="space-y-1 max-h-48 overflow-y-auto pr-1">
              {history.map((h) => (
                <div
                  key={h.id}
                  className="flex items-center justify-between text-xs border-b border-border/50 py-1.5 last:border-0"
                >
                  <span className="text-muted-foreground">{new Date(h.checked_at).toLocaleString("pt-BR")}</span>
                  {h.success && h.is_default_fake_cert ? (
                    <Badge className="bg-orange-500/20 text-orange-400 border-orange-500/30">
                      Cert. Fake (Ingress)
                    </Badge>
                  ) : h.success ? (
                    getStatusBadge(h.status || "")
                  ) : (
                    <Badge className="bg-red-500/20 text-red-400 border-red-500/30" title={h.error_message}>
                      Erro
                    </Badge>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DetailRow({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
  return (
    <div className="flex justify-between gap-4">
      <span className="text-muted-foreground flex-shrink-0">{label}:</span>
      <span className={`text-right truncate ${mono ? "font-mono text-xs" : ""}`}>{value || "—"}</span>
    </div>
  );
}
