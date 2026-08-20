import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { AlertTriangle, CircleOff, CheckCircle2, Zap, Server } from "lucide-react";
import type { HealthCheckResult } from "@/types/healthcheck";

interface TriageScopeModalProps {
  result: HealthCheckResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Detalhe do Modo Triagem (HEALTHCHECK-TRIAGE-MODE-PLAN.md) — modal próprio, acionado por um
// badge sempre visível no cabeçalho do resultado de cada cluster (não depende de expandir o card
// nem de entrar na sub-aba "Relatório"). Achado real via feedback do usuário (2026-08-20): a
// versão anterior vivia só dentro da aba "Relatório" e "ficava perdida na leitura global do
// relatório" — esta é a ÚNICA renderização do escopo de triagem agora (HealthReportTab.tsx só
// aponta pra cá, não duplica o conteúdo).
export const TriageScopeModal = ({ result, open, onOpenChange }: TriageScopeModalProps) => {
  const ts = result?.triage_summary;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Zap className="h-5 w-5" />
            Escopo da Triagem
          </DialogTitle>
          {result && (
            <DialogDescription className="flex items-center gap-1">
              <Server className="h-3 w-3" /> {result.cluster}
            </DialogDescription>
          )}
        </DialogHeader>

        {!ts ? (
          <p className="text-sm text-muted-foreground">Sem dados de triagem para este resultado.</p>
        ) : ts.fell_back_to_full ? (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
            <p className="font-medium text-amber-700 dark:text-amber-400 mb-1">
              Nenhuma fonte de triagem disponível — Varredura Completa foi usada
            </p>
            <p className="text-muted-foreground">
              {ts.fallback_reason || "Nenhuma fonte de triagem disponível para este cluster."}
            </p>
          </div>
        ) : (
          <div className="flex-1 min-h-0 flex flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              {ts.namespaces.length === 0
                ? "Nenhuma fonte sinalizou problema — cluster aparenta saudável, varredura reduzida."
                : `${ts.namespaces.length} namespace(s) no escopo.`}
            </p>

            {/* Painel "farol" por fonte — cinza (indisponível) vs. verde (checou, sem problema)
                vs. âmbar (checou, achou problema) */}
            <div className="flex flex-wrap gap-2 flex-shrink-0">
              {ts.sources.map((s) => {
                const cls = !s.available
                  ? "bg-muted text-muted-foreground border-muted-foreground/30"
                  : s.namespaces.length > 0
                    ? "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30"
                    : "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30";
                return (
                  <Badge
                    key={s.name}
                    variant="outline"
                    title={s.error || undefined}
                    className={`text-xs gap-1 ${cls}`}
                  >
                    {!s.available ? (
                      <CircleOff className="h-3 w-3" />
                    ) : s.namespaces.length > 0 ? (
                      <AlertTriangle className="h-3 w-3" />
                    ) : (
                      <CheckCircle2 className="h-3 w-3" />
                    )}
                    {s.name}: {!s.available ? "indisponível" : `${s.namespaces.length} namespace(s)`}
                  </Badge>
                );
              })}
            </div>

            {/* Motivos por namespace — o "porquê" de cada namespace estar no escopo. Dedup +
                contagem (ex: "(×9)") já resolvidos no backend (resolveTriageTargets) — mesmo
                alerta disparado por vários pods/HPAs no namespace não aparece repetido aqui. */}
            {ts.reasons && Object.keys(ts.reasons).length > 0 && (
              <ScrollArea className="flex-1 min-h-0 border rounded-lg">
                <div className="p-3 space-y-2">
                  {Object.entries(ts.reasons).map(([ns, reasons]) => (
                    <div key={ns} className="text-xs rounded border bg-muted/30 p-2">
                      <span className="font-mono font-medium">{ns}</span>
                      <ul className="list-disc list-inside ml-1 text-muted-foreground mt-1 space-y-0.5">
                        {reasons.map((r, i) => (
                          <li key={i}>{r}</li>
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};
