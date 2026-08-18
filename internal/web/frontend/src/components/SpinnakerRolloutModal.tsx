import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Loader2, RotateCcw, CheckCircle2, HelpCircle, ExternalLink, Rocket, History } from "lucide-react";
import type { SpinnakerRollbackInfo } from "@/lib/api/types";

interface SpinnakerRolloutModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  deploymentName: string;
  namespace: string;
  info: SpinnakerRollbackInfo | undefined;
  loading?: boolean;
  error?: string;
}

function fmtDate(epochMs: number | undefined): string {
  if (!epochMs) return "—";
  return new Date(epochMs).toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

// Badge/modal gatilho pra este componente vive em DeploymentsTab.tsx — ver seção 8 do
// SPINNAKER-INTEGRATION-PLAN.md. `info` já vem resolvido do batch endpoint (nunca uma chamada
// própria daqui — evita N chamadas ao Gate, ver seção 9.1 do plano).
export function SpinnakerRolloutModal({
  open,
  onOpenChange,
  deploymentName,
  namespace,
  info,
  loading,
  error,
}: SpinnakerRolloutModalProps) {
  const isRollback = info?.is_rollback;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Rocket className="h-5 w-5 text-indigo-500" />
            Spinnaker — {deploymentName}
          </DialogTitle>
          <DialogDescription>{namespace}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {loading && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              Consultando o Spinnaker...
            </div>
          )}

          {!loading && error && (
            <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
              <HelpCircle className="h-4 w-4 mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {!loading && !error && (!info || !info.matched) && (
            <div className="flex items-start gap-2 rounded-md border border-border bg-muted/30 p-3 text-sm text-muted-foreground">
              <HelpCircle className="h-4 w-4 mt-0.5 shrink-0" />
              <span>
                Não determinado — nenhuma execução do Spinnaker correspondente foi encontrada pra
                este Deployment (não confirma nem descarta rollback).
              </span>
            </div>
          )}

          {!loading && !error && info?.matched && (
            <>
              {info.from_cache && (
                <div className="flex items-start gap-2 rounded-md border border-blue-500/30 bg-blue-500/5 p-3 text-sm text-blue-700 dark:text-blue-400">
                  <History className="h-4 w-4 mt-0.5 shrink-0" />
                  <span>
                    Dado de um scan anterior ({fmtDate(info.cached_at)}) — este deployment não
                    teve execução dentro da janela de busca atual do Spinnaker (~28 dias), mas o
                    último resultado confirmado foi preservado.
                  </span>
                </div>
              )}

              <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                <div>
                  <span className="text-muted-foreground">Última CHG aplicada:</span>
                  <p className="font-mono font-medium mt-0.5 text-foreground">{info.last_chg_applied || "—"}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Status da execução:</span>
                  <p className="font-medium mt-0.5 text-foreground">{info.execution_status || "—"}</p>
                </div>
                <div className="col-span-2">
                  <span className="text-muted-foreground">Data/hora da execução:</span>
                  <p className="font-medium mt-0.5 text-foreground">{fmtDate(info.pipeline_executed_at)}</p>
                </div>
              </div>

              {isRollback === false && (
                <div className="flex items-center gap-2 rounded-md border border-green-500/30 bg-green-500/5 p-3 text-sm text-green-700 dark:text-green-400">
                  <CheckCircle2 className="h-4 w-4 shrink-0" />
                  Deploy normal — sem rollback.
                </div>
              )}

              {isRollback === null && (
                <div className="flex items-start gap-2 rounded-md border border-border bg-muted/30 p-3 text-sm text-muted-foreground">
                  <HelpCircle className="h-4 w-4 mt-0.5 shrink-0" />
                  <span>
                    Não determinado pela integração — a execução encontrada não corresponde à
                    versão vigente. Não confirma nem descarta rollback.
                  </span>
                </div>
              )}

              {isRollback === true && (
                <div className="space-y-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
                  <div className="flex items-center gap-2 text-sm font-medium text-amber-700 dark:text-amber-400">
                    <RotateCcw className="h-4 w-4" />
                    Rollback detectado
                    <Badge variant="outline" className="text-[10px] py-0">
                      {info.rollback_type === "explicit" ? "manual, pipeline dedicado" : "implícito — deploy falhou"}
                    </Badge>
                  </div>
                  {info.rollback_type === "explicit" ? (
                    <p className="text-xs text-muted-foreground">
                      Revertido manualmente via pipeline{" "}
                      {info.rollback_pipeline_name && <code className="font-mono">{info.rollback_pipeline_name}</code>}.
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      O deploy falhou e a versão anterior nunca foi substituída — a aplicação
                      permaneceu na versão anterior automaticamente (estratégia RollingUpdate do
                      Kubernetes, sem ação de rollback explícita no Spinnaker).
                    </p>
                  )}
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                    <div>
                      <span className="text-muted-foreground">Início:</span>{" "}
                      <span className="font-medium">{fmtDate(info.rollback_started_at)}</span>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Fim:</span>{" "}
                      <span className="font-medium">{fmtDate(info.rollback_ended_at)}</span>
                    </div>
                    <div className="col-span-2">
                      <span className="text-muted-foreground">CHG que falhou:</span>{" "}
                      <span className="font-mono font-medium">{info.failed_chg || "—"}</span>
                    </div>
                  </div>
                </div>
              )}

              {info.spinnaker_execution_url && (
                <Button variant="outline" size="sm" asChild>
                  <a href={info.spinnaker_execution_url} target="_blank" rel="noopener noreferrer">
                    <ExternalLink className="h-3.5 w-3.5 mr-1.5" />
                    Ver no Spinnaker
                  </a>
                </Button>
              )}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
