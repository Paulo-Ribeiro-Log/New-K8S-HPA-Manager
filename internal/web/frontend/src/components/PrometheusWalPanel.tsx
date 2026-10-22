import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Loader2, Stethoscope, AlertTriangle } from "lucide-react";
import { useAnalyzePrometheusWAL } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { PrometheusWalCleanupDialog } from "@/components/PrometheusWalCleanupDialog";
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
import { toast } from "sonner";
import type { PrometheusResource, PrometheusWALFixOption, PrometheusWALReport, PrometheusWALVerdict } from "@/lib/api/types";

const selectorText = (sel: Record<string, string>) =>
  Object.keys(sel).length === 0 ? "(sem nodeSelector)" : Object.entries(sel).map(([k, v]) => `${k}=${v}`).join(", ");

interface PrometheusWalPanelProps {
  resource: PrometheusResource;
  selectedCluster: string;
  /** Pré-preenche o editor de recursos da aba com o memory limit sugerido (não aplica nada). */
  onSuggestMemory?: (memory: string) => void;
  /** Chamado depois de uma limpeza/restauração, para recarregar a lista de recursos. */
  onChanged?: () => void;
}

const VERDICTS: Record<PrometheusWALVerdict, { label: string; className: string }> = {
  saudavel: { label: "Saudável", className: "bg-green-600 hover:bg-green-600 text-white" },
  aumentar_memoria: { label: "Aumentar memória", className: "bg-amber-500 hover:bg-amber-500 text-white" },
  limpar_wal: { label: "Limpar WAL indicado", className: "bg-red-600 hover:bg-red-600 text-white" },
  investigar: { label: "Investigar", className: "bg-slate-500 hover:bg-slate-500 text-white" },
};

const formatBytes = (b: number) => {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GiB`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(0)} MiB`;
  return `${Math.round(b / 1024)} KiB`;
};

// O StatefulSet do operator se chama "prometheus-<recurso Prometheus>"; vazio = o backend usa o único do namespace.
const crNameOf = (r: PrometheusResource) =>
  r.name.startsWith("prometheus-") ? r.name.slice("prometheus-".length) : "";

export const PrometheusWalPanel = ({ resource, selectedCluster, onSuggestMemory, onChanged }: PrometheusWalPanelProps) => {
  const [deep, setDeep] = useState(false);
  const [report, setReport] = useState<PrometheusWALReport | null>(null);
  const analyze = useAnalyzePrometheusWAL();
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [fixId, setFixId] = useState<string | null>(null);
  const [fixConfirm, setFixConfirm] = useState(false);
  const [fixing, setFixing] = useState(false);

  const run = async () => {
    setReport(null);
    try {
      setReport(
        await analyze.mutateAsync({
          cluster: selectedCluster,
          namespace: resource.namespace,
          name: crNameOf(resource),
          deep,
        })
      );
    } catch {
      /* o erro é exibido abaixo via analyze.error */
    }
  };

  const restore = async () => {
    if (!report) return;
    setRestoring(true);
    try {
      const { restored_replicas } = await apiClient.restorePrometheusWAL(selectedCluster, report.namespace, report.name);
      toast.success(`Prometheus religado com ${restored_replicas} réplica(s)`);
      onChanged?.();
      await run();
    } catch (err) {
      toast.error("Falha ao restaurar", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setRestoring(false);
    }
  };

  const applyFix = async (opt: PrometheusWALFixOption) => {
    if (!report) return;
    setFixing(true);
    try {
      const res = await apiClient.fixPrometheusScheduling(
        selectedCluster,
        report.namespace,
        report.name,
        opt.node_selector,
        opt.tolerations ?? []
      );
      const recreated = res.recreated_pods?.length ? ` Pod(s) recriado(s): ${res.recreated_pods.join(", ")}.` : "";
      toast.success("Agendamento corrigido", {
        description: `nodeSelector agora: ${selectorText(res.after_node_selector)}.${recreated}`,
      });
      (res.warnings ?? []).forEach((w) => toast.warning(w, { duration: 15000 }));
      setFixId(null);
      onChanged?.();
      await run();
    } catch (err) {
      toast.error("Falha ao corrigir o agendamento", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setFixing(false);
      setFixConfirm(false);
    }
  };

  const selectedFix = report?.pods.flatMap((p) => p.placement?.options ?? []).find((o) => o.id === fixId);

  const affectedWithoutDeep = report
    ? (report.affected_replicas ?? []).filter((p) => !(report.deep ?? []).some((d) => d.pod === p))
    : [];

  const verdict = report ? VERDICTS[report.verdict] : null;

  return (
    <div className="space-y-3 rounded-md border p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Stethoscope className="w-4 h-4 text-muted-foreground" />
          <h4 className="text-sm font-semibold">Diagnóstico do WAL</h4>
        </div>
        <Button size="sm" variant="outline" onClick={run} disabled={analyze.isPending}>
          {analyze.isPending ? (
            <>
              <Loader2 className="w-4 h-4 mr-1 animate-spin" />
              Analisando...
            </>
          ) : (
            "Analisar WAL"
          )}
        </Button>
      </div>

      <label className="flex items-start gap-2 text-xs text-muted-foreground cursor-pointer">
        <Checkbox checked={deep} onCheckedChange={(v) => setDeep(v === true)} disabled={analyze.isPending} className="mt-0.5" />
        <span>
          Análise profunda: monta o PVC <b>somente leitura</b> em um pod temporário no cluster (mesmo nó do Prometheus) para
          medir o WAL e calcular a janela de dados que seria perdida. Mais lenta; o pod é removido ao final.
        </span>
      </label>

      {analyze.error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>
            {analyze.error instanceof Error ? analyze.error.message : String(analyze.error)}
          </AlertDescription>
        </Alert>
      )}

      {report && verdict && (
        <div className="space-y-3 text-sm">
          <div className="flex items-center gap-2">
            <Badge className={verdict.className}>{verdict.label}</Badge>
            <span className="text-xs text-muted-foreground">
              {report.namespace}/{report.name}
            </span>
          </div>
          <p>{report.summary}</p>

          {report.verdict === "aumentar_memoria" && report.suggested_memory && (
            <div className="rounded-md bg-amber-500/10 border border-amber-500/30 p-3 space-y-2">
              <p>
                Memory limit sugerido: <b>{report.suggested_memory}</b>. A ferramenta só informa o valor; a alteração é feita
                no editor de recursos desta aba.
              </p>
              {onSuggestMemory && (
                <Button size="sm" variant="outline" onClick={() => onSuggestMemory(report.suggested_memory!)}>
                  Abrir o editor com {report.suggested_memory}
                </Button>
              )}
              <p className="text-xs text-muted-foreground">
                Se o prometheus-operator reverter a edição do StatefulSet, altere o limit no recurso Prometheus (CRD/Helm).
              </p>
            </div>
          )}

          {report.recurrence?.total > 0 && (
            <div
              className={
                report.recurrence.recurrent
                  ? "rounded-md border border-red-500/40 bg-red-500/10 p-3 text-xs space-y-1"
                  : "text-xs text-muted-foreground"
              }
            >
              <p className={report.recurrence.recurrent ? "font-semibold text-red-700 dark:text-red-400" : undefined}>
                {report.recurrence.recurrent && "Problema recorrente — "}
                {report.recurrence.count_30d} limpeza(s) do WAL nos últimos 30 dias (total conhecido: {report.recurrence.total}
                {report.recurrence.last_at && `; última em ${new Date(report.recurrence.last_at).toLocaleString("pt-BR")}`}).
              </p>
              {report.recurrence.recurrent && (
                <p>
                  Limpar de novo trata o sintoma. Investigue cardinalidade, memory limit, disco e retenção antes de repetir.
                </p>
              )}
            </div>
          )}

          {report.pending_restore_replicas != null && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription className="space-y-2">
                <p>
                  Uma limpeza anterior desligou o Prometheus e não chegou a religar (réplicas originais:{" "}
                  {report.pending_restore_replicas}).
                </p>
                <Button size="sm" onClick={restore} disabled={restoring}>
                  {restoring ? "Restaurando..." : `Restaurar (${report.pending_restore_replicas} réplica(s))`}
                </Button>
              </AlertDescription>
            </Alert>
          )}

          {report.verdict === "limpar_wal" && (
            <div className="rounded-md bg-red-500/10 border border-red-500/30 p-3 space-y-2">
              <p>
                As métricas que ainda não viraram bloco compactado serão perdidas.
                {affectedWithoutDeep.length > 0 &&
                  " Para liberar a limpeza, rode a análise profunda: é ela que mostra os dados que seriam perdidos."}
              </p>
              <Button
                size="sm"
                variant="destructive"
                disabled={affectedWithoutDeep.length > 0}
                onClick={() => setCleanupOpen(true)}
              >
                Limpar WAL…
              </Button>
            </div>
          )}

          {report.warnings && report.warnings.length > 0 && (
            <ul className="space-y-1">
              {report.warnings.map((w) => (
                <li key={w} className="flex gap-2 text-xs text-amber-700 dark:text-amber-400">
                  <AlertTriangle className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />
                  <span>{w}</span>
                </li>
              ))}
            </ul>
          )}

          <div className="space-y-2">
            {report.pods.map((p) => (
              <div key={p.pod} className="rounded-md border p-2">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs">{p.pod}</span>
                  <Badge variant="outline" className="text-[10px]">{VERDICTS[p.verdict].label}</Badge>
                </div>
                <p className="text-xs mt-1">{p.cause}</p>
                {p.placement && (
                  <div className="mt-2 rounded-md bg-amber-500/10 border border-amber-500/30 p-2 space-y-1.5 text-xs">
                    <p className="font-semibold">Por que o pod não cabe em nenhum node</p>
                    <p>{p.placement.summary}</p>
                    {p.placement.nodes && p.placement.nodes.length > 0 && (
                      <ul className="space-y-1">
                        {p.placement.nodes.map((n) => (
                          <li key={n.node} className="flex gap-1.5">
                            <span
                              className={n.fits && !n.blocker ? "text-green-600" : n.fits ? "text-amber-600" : "text-red-600"}
                              aria-label={n.fits && !n.blocker ? "serve" : n.fits ? "passa no seletor, mas está bloqueado" : "não serve"}
                            >
                              {n.fits && !n.blocker ? "✔" : n.fits ? "⚠" : "✖"}
                            </span>
                            <span className="min-w-0">
                              <span className="font-mono break-all">{n.node}</span>
                              {n.pool && <span className="text-muted-foreground"> · pool {n.pool}</span>}
                              {n.blocker && <span className="block text-amber-700 dark:text-amber-400">bloqueado: {n.blocker}</span>}
                              {n.reasons && n.reasons.length > 0 && (
                                <span className="block text-muted-foreground">{n.reasons.join("; ")}</span>
                              )}
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                    <p className="text-[11px] text-muted-foreground">{p.placement.note}</p>
                    {p.placement.label_advice && (
                      <div className="rounded-md border border-green-600/40 bg-green-500/10 p-2 space-y-1.5" data-testid="prometheus-label-advice">
                        <p className="font-semibold">Recomendado: devolver o label ao node pool</p>
                        <p>
                          O pod exige <span className="font-mono">{selectorText(p.placement.label_advice.selector)}</span> e nenhum node tem esse
                          label. Restaurá-lo no node pool dedicado não altera o recurso Prometheus, então um upgrade do Helm não
                          desfaz a correção. A ferramenta não roda comandos de escrita na nuvem: execute você.
                        </p>
                        <ul className="text-[11px] text-muted-foreground">
                          {p.placement.label_advice.pools.map((pl) => (
                            <li key={pl.pool}>
                              pool <span className="font-mono">{pl.pool}</span> ({pl.nodes} node(s)){pl.current ? ` · hoje: ${pl.current}` : ""}
                            </li>
                          ))}
                        </ul>
                        {p.placement.label_advice.command && (
                          <div className="flex items-start gap-2">
                            <code className="flex-1 rounded bg-muted p-1.5 text-[11px] break-all">{p.placement.label_advice.command}</code>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => {
                                void navigator.clipboard.writeText(p.placement?.label_advice?.command ?? "");
                                toast.success("Comando copiado");
                              }}
                            >
                              Copiar
                            </Button>
                          </div>
                        )}
                        {p.placement.label_advice.warning && (
                          <p className="text-amber-700 dark:text-amber-400">⚠ {p.placement.label_advice.warning}</p>
                        )}
                      </div>
                    )}
                    {p.placement.options && p.placement.options.length > 0 && (
                      <div className="rounded-md border bg-background p-2 space-y-1.5" data-testid="prometheus-scheduling-fix">
                        <p className="font-semibold">{p.placement.label_advice ? "Alternativa: mudar o recurso Prometheus" : "Corrigir o agendamento"}</p>
                        <p className="text-[11px] text-muted-foreground">
                          Troca o nodeSelector do recurso Prometheus e recria o pod que está Pending. Não mexe no WAL nem no
                          volume. Cada opção foi simulada contra os nodes atuais (seletor, afinidade, taints, CPU/memória livres e
                          zona do volume). Um helm upgrade ou sync do Argo CD desfaz a mudança.
                        </p>
                        <div className="space-y-1">
                          {p.placement.options.map((o) => (
                            <button
                              key={o.id}
                              type="button"
                              disabled={!!o.blocked}
                              onClick={() => setFixId(o.id)}
                              className={`w-full text-left rounded-md border p-2 text-xs transition-colors ${
                                o.blocked ? "opacity-60 cursor-not-allowed" : fixId === o.id ? "border-primary bg-primary/10" : "hover:bg-muted/50"
                              }`}
                            >
                              <span className="font-medium">{o.label}</span>
                              {o.blocked ? (
                                <span className="block text-red-600">✖ não resolve: {o.blocked}</span>
                              ) : (
                                <span className="block text-muted-foreground">nodes que passam a servir: {o.nodes.join(", ")}</span>
                              )}
                              {!o.blocked &&
                                o.excluded?.map((x) => (
                                  <span key={x} className="block text-muted-foreground">– descartado {x}</span>
                                ))}
                              {o.notes?.map((n) => (
                                <span key={n} className="block text-amber-700 dark:text-amber-400">• {n}</span>
                              ))}
                            </button>
                          ))}
                        </div>
                        <Button size="sm" disabled={!fixId || fixing} onClick={() => setFixConfirm(true)}>
                          {fixing ? "Aplicando..." : "Aplicar correção…"}
                        </Button>
                      </div>
                    )}
                  </div>
                )}
                {p.evidence && p.evidence.length > 0 && (
                  <ul className="mt-1 list-disc pl-4 text-xs text-muted-foreground">
                    {p.evidence.map((e) => (
                      <li key={e}>{e}</li>
                    ))}
                  </ul>
                )}
              </div>
            ))}
          </div>

          {report.deep && report.deep.length > 0 && (
            <div className="space-y-1">
              <p className="text-xs font-semibold">Volume (análise profunda)</p>
              {report.deep.map((d) => (
                <div key={d.pod} className="rounded-md bg-muted/50 p-2 text-xs space-y-0.5">
                  <div className="font-mono">{d.pvc}</div>
                  <div>
                    {d.blocks} bloco(s) compactado(s) · WAL {formatBytes(d.wal_bytes)} · chunks_head {formatBytes(d.chunks_head_bytes)}
                  </div>
                  <div>
                    {d.last_block_ends_at
                      ? `Último bloco termina em ${new Date(d.last_block_ends_at).toLocaleString("pt-BR")} — limpar o WAL perderia ~${d.lost_window_hours.toFixed(1)} h de métricas.`
                      : "Nenhum bloco compactado: todo o histórico está no WAL."}
                  </div>
                </div>
              ))}
            </div>
          )}

          {report.events && report.events.length > 0 && (
            <details className="text-xs">
              <summary className="cursor-pointer text-muted-foreground">Eventos Warning recentes ({report.events.length})</summary>
              <ul className="mt-1 list-disc pl-4 text-muted-foreground">
                {report.events.map((e) => (
                  <li key={e}>{e}</li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}

      <AlertDialog open={fixConfirm && !!selectedFix} onOpenChange={(o) => !fixing && setFixConfirm(o)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Corrigir o agendamento do Prometheus?</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-2 text-sm">
                <p>
                  O recurso Prometheus <span className="font-mono">{report?.namespace}/{report?.name}</span> passa a usar:
                </p>
                <p className="font-mono text-xs">nodeSelector: {selectedFix ? selectorText(selectedFix.node_selector) : ""}</p>
                {selectedFix?.tolerations && selectedFix.tolerations.length > 0 && (
                  <p className="font-mono text-xs">
                    + toleration: {selectedFix.tolerations.map((t) => `${t.key}${t.value ? "=" + t.value : ""}:${t.effect}`).join(", ")}
                  </p>
                )}
                {selectedFix?.notes && selectedFix.notes.length > 0 && (
                  <ul className="list-disc pl-4 text-xs text-amber-700 dark:text-amber-400">
                    {selectedFix.notes.map((n) => (
                      <li key={n}>{n}</li>
                    ))}
                  </ul>
                )}
                <p>O pod que está Pending será recriado. O WAL e o volume não são tocados.</p>
                <p className="text-xs">
                  Um helm upgrade ou sync do Argo CD restaura o nodeSelector antigo. Se o Prometheus já estiver rodando neste
                  pool, o pod sai dele e fica Pending de novo. Leve a mudança também para os values/manifestos, ou prefira
                  devolver o label ao node pool.
                </p>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={fixing}>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              disabled={fixing}
              onClick={(e) => {
                e.preventDefault();
                if (selectedFix) void applyFix(selectedFix);
              }}
            >
              {fixing ? "Aplicando..." : "Aplicar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {report && report.verdict === "limpar_wal" && cleanupOpen && (
        <PrometheusWalCleanupDialog
          open={cleanupOpen}
          onOpenChange={setCleanupOpen}
          report={report}
          onFinished={() => {
            setReport(null);
            onChanged?.();
          }}
        />
      )}
    </div>
  );
};
