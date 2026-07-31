import { CheckCircle2, XCircle, AlertTriangle, ChevronRight, Radio, Info } from "lucide-react";
import type { ChainValidationResult } from "@/types/certificates";
import { IssueList } from "@/components/IssueList";

interface CheckRowProps {
  label: string;
  ok: boolean;
  neutralWhenFalse?: boolean; // TrustedByPublicCA=false é comum/OK (CA privada), não "falho"
}

function CheckRow({ label, ok, neutralWhenFalse }: CheckRowProps) {
  if (!ok && neutralWhenFalse) {
    return (
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <AlertTriangle className="w-3.5 h-3.5 text-amber-500 shrink-0" />
        <span>{label}</span>
      </div>
    );
  }
  return (
    <div className="flex items-center gap-1.5 text-xs">
      {ok ? (
        <CheckCircle2 className="w-3.5 h-3.5 text-green-500 shrink-0" />
      ) : (
        <XCircle className="w-3.5 h-3.5 text-red-500 shrink-0" />
      )}
      <span className={ok ? "text-foreground" : "text-red-500"}>{label}</span>
    </div>
  );
}

interface Props {
  result: ChainValidationResult;
  className?: string;
}

/**
 * CertificateChainValidationPanel — exibe o resultado de ValidateCertificateChain (backend).
 * Reaproveitado em CertificateRenewModal.tsx (automático pós-upload) e no botão "Validar Cadeia"
 * de CertificateDetailModal.tsx (disparo manual).
 */
export function CertificateChainValidationPanel({ result, className }: Props) {
  return (
    <div className={`rounded border p-3 space-y-2 ${result.valid ? "border-green-500/30 bg-green-500/5" : "border-red-500/30 bg-red-500/5"} ${className ?? ""}`}>
      <div className="flex items-center gap-2">
        {result.valid ? (
          <CheckCircle2 className="w-4 h-4 text-green-500" />
        ) : (
          <XCircle className="w-4 h-4 text-red-500" />
        )}
        <span className="text-sm font-medium">
          {result.valid ? "Cadeia válida" : "Cadeia com problemas"}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-x-4 gap-y-1">
        <CheckRow label="Chave corresponde ao certificado" ok={result.key_matches_cert} />
        <CheckRow label="Ordem da cadeia correta" ok={result.chain_order_correct} />
        <CheckRow label="Dentro da validade" ok={result.expiry_ok} />
        <CheckRow label="Confiável por CA pública" ok={result.trusted_by_public_ca} neutralWhenFalse />
      </div>

      {result.chain_subjects.length > 0 && (
        <div className="flex items-center flex-wrap gap-1 text-[11px] text-muted-foreground pt-1 border-t border-border/50">
          {result.chain_subjects.map((subj, i) => (
            <span key={i} className="flex items-center gap-1">
              {i > 0 && <ChevronRight className="w-3 h-3" />}
              <span className="font-mono">{subj}</span>
            </span>
          ))}
        </div>
      )}

      <IssueList errors={result.errors} warnings={result.warnings} />

      {result.live_propagation &&
        (result.live_propagation.checked ||
          (result.live_propagation.notes && result.live_propagation.notes.length > 0)) && (
        <div className="pt-1 border-t border-border/50 space-y-1">
          {result.live_propagation.checked ? (
            <>
              <div className="flex items-center gap-1.5 text-xs">
                <Radio className="w-3.5 h-3.5 text-sky-500 shrink-0" />
                <span>
                  {result.live_propagation.replicas_current}/{result.live_propagation.total_replicas_found}{" "}
                  {result.live_propagation.method === "tls-dial"
                    ? "host(s) respondendo com o certificado atual (handshake TLS direto)"
                    : "réplica(s) do ingress-nginx com o certificado atual"}
                </span>
              </div>
              {result.live_propagation.replicas_stale && result.live_propagation.replicas_stale.length > 0 && (
                <div className="flex items-start gap-1.5 text-xs text-amber-500">
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  <span>
                    Propagação em andamento — ainda servindo o certificado anterior:{" "}
                    {result.live_propagation.replicas_stale.join(", ")}
                  </span>
                </div>
              )}
              {result.live_propagation.possible_external_layer && result.live_propagation.possible_external_layer.length > 0 && (
                <div className="flex items-start gap-1.5 text-xs text-sky-500">
                  <Info className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  <span>
                    Emissor completamente diferente do esperado — provavelmente há uma camada externa
                    (CDN/WAF/proxy corporativo) terminando TLS antes do cluster, não é necessariamente
                    um problema de propagação: {result.live_propagation.possible_external_layer.join(", ")}
                  </span>
                </div>
              )}
            </>
          ) : (
            <div className="flex items-start gap-1.5 text-xs text-muted-foreground">
              <Radio className="w-3.5 h-3.5 shrink-0 mt-0.5 opacity-50" />
              <div className="space-y-0.5">
                <span className="block">Checagem de propagação não realizada:</span>
                <ul className="space-y-0.5">
                  {result.live_propagation.notes!.map((n, i) => (
                    <li key={i} className="pl-2">• {n}</li>
                  ))}
                </ul>
              </div>
            </div>
          )}
        </div>
      )}

      {result.openssl_notes && result.openssl_notes.length > 0 && (
        <details className="pt-1">
          <summary className="text-[11px] text-muted-foreground cursor-pointer select-none">Diagnóstico OpenSSL (opcional)</summary>
          <ul className="mt-1 space-y-0.5">
            {result.openssl_notes.map((n, i) => (
              <li key={i} className="text-[11px] text-muted-foreground pl-2">{n}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
