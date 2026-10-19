import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Loader2, Stethoscope, AlertTriangle } from "lucide-react";
import { useAnalyzePrometheusWAL } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { PrometheusWalCleanupDialog } from "@/components/PrometheusWalCleanupDialog";
import { toast } from "sonner";
import type { PrometheusResource, PrometheusWALReport, PrometheusWALVerdict } from "@/lib/api/types";

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
