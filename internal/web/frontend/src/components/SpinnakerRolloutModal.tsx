import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Loader2, RotateCcw, CheckCircle2, HelpCircle, ExternalLink, Rocket, History, ChevronDown, FileWarning } from "lucide-react";
import type { SpinnakerRollbackInfo, SpinnakerStageSummary } from "@/lib/api/types";
import { SpinnakerStageLogModal } from "@/components/SpinnakerStageLogModal";

interface SpinnakerRolloutModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  deploymentName: string;
  namespace: string;
  info: SpinnakerRollbackInfo | undefined;
  loading?: boolean;
  error?: string;
}

// ChgValue renderiza o número da CHG como link direto pro ServiceNow quando a URL vem
// disponível (seção 9 item 3 do plano) — poupa copiar/colar o número e buscar manualmente.
// Sem URL, cai pro texto simples de sempre (nem toda squad/execução tem esse campo populado).
function ChgValue({ chg, url }: { chg: string | undefined; url: string | undefined }) {
  if (!chg) return <>—</>;
  if (!url) return <>{chg}</>;
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 text-primary hover:underline"
      title="Abrir CHG no ServiceNow"
    >
      {chg}
      <ExternalLink className="h-3 w-3 shrink-0" />
    </a>
  );
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

// fmtDuration formata a duração de uma etapa (ex: "1m 31s") a partir de started/completed —
// mesma info que a coluna "Duration" da UI do Deck, calculada no cliente (o backend só manda
// os dois timestamps).
function fmtDuration(startedAt: number | undefined, completedAt: number | undefined): string {
  if (!startedAt || !completedAt || completedAt < startedAt) return "—";
  const totalSeconds = Math.round((completedAt - startedAt) / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`;
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
  // Colapsado por padrão (mesmo padrão de NoteEntry.tsx/AIHistoryPanel.tsx) — o resultado
  // principal acima já responde a pergunta mais comum, o histórico é um detalhe opcional.
  const [historyOpen, setHistoryOpen] = useState(false);
  const [stagesOpen, setStagesOpen] = useState(false);
  // Etapa cujo log está aberto no modal de log (seção própria, gatilho a partir daqui) — null =
  // fechado. Guarda a etapa inteira (não só o texto) pra usar o nome dela no título do modal.
  const [logStage, setLogStage] = useState<SpinnakerStageSummary | null>(null);

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
                  <p className="font-mono font-medium mt-0.5 text-foreground">
                    <ChgValue chg={info.last_chg_applied} url={info.last_chg_applied_url} />
                  </p>
                </div>
                <div>
                  <span className="text-muted-foreground">Status da execução:</span>
                  <p className="font-medium mt-0.5 text-foreground">{info.execution_status || "—"}</p>
                </div>
                {/* Pedido explícito do usuário: "Versão" abaixo de "Status da execução" (mesma
                    coluna), na MESMA LINHA de "Data/hora da execução" (que fica na coluna
                    esquerda, abaixo de "Última CHG aplicada") — não mais col-span-2 sozinha. */}
                <div>
                  <span className="text-muted-foreground">Data/hora da execução:</span>
                  <p className="font-medium mt-0.5 text-foreground">{fmtDate(info.pipeline_executed_at)}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Versão:</span>
                  <p className="font-mono font-medium mt-0.5 text-foreground">{info.version || "—"}</p>
                </div>
                {/* Pedido explícito do usuário: qual era a versão anterior a este deploy —
                    última execução SUCCEEDED antes desta, pulando falhas no meio. Ausente
                    quando não há nenhuma dentro da janela de busca atual do Gate. */}
                {info.previous_version && (
                  <div className="col-span-2 pt-1 border-t border-border/60">
                    <span className="text-muted-foreground">Versão anterior:</span>
                    <p className="font-mono font-medium mt-0.5 text-foreground">
                      {info.previous_version}
                      {info.previous_version_chg && (
                        <>
                          {" "}
                          (<ChgValue chg={info.previous_version_chg} url={info.previous_version_chg_url} />)
                        </>
                      )}
                      {info.previous_version_executed_at && (
                        <span className="text-muted-foreground font-sans"> — {fmtDate(info.previous_version_executed_at)}</span>
                      )}
                    </p>
                  </div>
                )}
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
                      <span className="font-mono font-medium">
                        <ChgValue chg={info.failed_chg} url={info.failed_chg_url} />
                      </span>
                    </div>
                  </div>
                </div>
              )}

              {/* Etapas da execução (Step/Started/Completed/Duration/Status) — pedido explícito
                  do usuário depois de ver a tela "Execution Details" real do Deck. Uma etapa
                  pode ter status diferente do geral (ex: SKIPPED numa execução SUCCEEDED). */}
              {info.stages && info.stages.length > 0 && (
                <Collapsible open={stagesOpen} onOpenChange={setStagesOpen}>
                  <CollapsibleTrigger className="flex w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors">
                    <ChevronDown className={`h-3.5 w-3.5 shrink-0 transition-transform ${stagesOpen ? "rotate-180" : ""}`} />
                    Etapas da execução ({info.stages.length})
                  </CollapsibleTrigger>
                  <CollapsibleContent className="mt-2">
                    <div className="rounded-md border border-border overflow-hidden">
                      <table className="w-full text-xs">
                        <thead>
                          <tr className="bg-muted/40 text-muted-foreground">
                            <th className="text-left font-medium px-2.5 py-1.5">Etapa</th>
                            <th className="text-left font-medium px-2.5 py-1.5">Concluída em</th>
                            <th className="text-left font-medium px-2.5 py-1.5">Duração</th>
                            <th className="text-left font-medium px-2.5 py-1.5">Status</th>
                            <th className="px-2.5 py-1.5" />
                          </tr>
                        </thead>
                        <tbody>
                          {info.stages.map((stage, i) => (
                            <tr key={`${stage.name}-${i}`} className={i > 0 ? "border-t border-border" : ""}>
                              <td className="px-2.5 py-1.5 font-mono truncate max-w-[140px]" title={stage.name}>
                                {stage.name}
                              </td>
                              <td className="px-2.5 py-1.5 whitespace-nowrap">{fmtDate(stage.completed_at)}</td>
                              <td className="px-2.5 py-1.5 whitespace-nowrap">{fmtDuration(stage.started_at, stage.completed_at)}</td>
                              <td className="px-2.5 py-1.5">
                                <Badge
                                  variant="outline"
                                  className={`text-[10px] py-0 ${
                                    stage.status === "SUCCEEDED"
                                      ? "border-emerald-500/40 text-emerald-600 dark:text-emerald-400"
                                      : stage.status === "SKIPPED"
                                        ? "border-muted-foreground/40 text-muted-foreground"
                                        : "border-amber-500/40 text-amber-600 dark:text-amber-400"
                                  }`}
                                >
                                  {stage.status}
                                </Badge>
                              </td>
                              <td className="px-2.5 py-1.5">
                                {/* Só aparece em etapas com falha real (log reconstruído no
                                    backend) — pedido explícito do usuário. */}
                                {stage.log && (
                                  <button
                                    type="button"
                                    onClick={() => setLogStage(stage)}
                                    className="inline-flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400 hover:underline whitespace-nowrap"
                                    title="Ver detalhes da falha desta etapa"
                                  >
                                    <FileWarning className="h-3 w-3 shrink-0" />
                                    Ver log
                                  </button>
                                )}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </CollapsibleContent>
                </Collapsible>
              )}

              {/* Histórico curto (seção 9 item 5 do plano) — últimas execuções, não só a que
                  decidiu o resultado acima. Ausente quando FromCache (não persistido). */}
              {info.recent_executions && info.recent_executions.length > 0 && (
                <Collapsible open={historyOpen} onOpenChange={setHistoryOpen}>
                  <CollapsibleTrigger className="flex w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors">
                    <ChevronDown className={`h-3.5 w-3.5 shrink-0 transition-transform ${historyOpen ? "rotate-180" : ""}`} />
                    Histórico recente ({info.recent_executions.length} execuç{info.recent_executions.length === 1 ? "ão" : "ões"})
                  </CollapsibleTrigger>
                  <CollapsibleContent className="mt-2 space-y-1.5">
                    {info.recent_executions.map((ex) => (
                      <div
                        key={ex.execution_id}
                        className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/20 px-2.5 py-1.5 text-xs"
                      >
                        <div className="flex items-center gap-1.5 min-w-0">
                          {ex.is_rollback ? (
                            <RotateCcw className="h-3 w-3 shrink-0 text-amber-600 dark:text-amber-400" />
                          ) : (
                            <Rocket className="h-3 w-3 shrink-0 text-muted-foreground" />
                          )}
                          <span className="font-mono truncate" title={ex.pipeline_name}>
                            {ex.version || ex.pipeline_name}
                          </span>
                        </div>
                        <div className="flex items-center gap-2 shrink-0">
                          <span className="font-mono">
                            <ChgValue chg={ex.chg} url={ex.chg_url} />
                          </span>
                          <Badge
                            variant="outline"
                            className={`text-[10px] py-0 ${
                              ex.status === "SUCCEEDED"
                                ? "border-emerald-500/40 text-emerald-600 dark:text-emerald-400"
                                : "border-muted-foreground/40 text-muted-foreground"
                            }`}
                          >
                            {ex.status}
                          </Badge>
                          <span className="text-muted-foreground whitespace-nowrap">{fmtDate(ex.executed_at)}</span>
                        </div>
                      </div>
                    ))}
                  </CollapsibleContent>
                </Collapsible>
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

      {/* Modal de log — próprio, redimensionável, gatilho a partir da tabela de etapas acima
          (só aparece em etapas com falha real). Fica fora do DialogContent principal, mesmo
          padrão de modal-dentro-de-modal já usado em PodQuickViewModal.tsx (Describe). */}
      <SpinnakerStageLogModal
        open={logStage !== null}
        onOpenChange={(o) => { if (!o) setLogStage(null); }}
        stageName={logStage?.name ?? ""}
        log={logStage?.log ?? ""}
      />
    </Dialog>
  );
}
