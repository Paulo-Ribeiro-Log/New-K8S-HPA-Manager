import { Fragment, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { FileDown, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/lib/api/client";
import { buildComparison, fmtCompareValue, fmtDelta, verdict } from "@/lib/metricsCompare";
import { buildComparisonCSV, buildComparisonMarkdown } from "@/lib/metricsCompareExport";
import { downloadText, fileStamp } from "@/lib/metricsReportExport";
import { eventLabel, fileSlug, type SavedEvent } from "@/lib/metricsEvents";
import type { ReportResult } from "@/lib/metricsReport";

const VERDICT_CLASS = { pior: "text-red-400", melhor: "text-emerald-400", igual: "text-muted-foreground", "sem-base": "text-muted-foreground" } as const;

/**
 * Compara o relatório atual com um evento salvo: coleta o mesmo conjunto de aplicações na janela do evento
 * e mostra, por aplicação, valor atual × valor do evento e a variação. Só taxas e picos (ver metricsCompare.ts).
 */
export function MetricsComparePanel({ result, events }: { result: ReportResult; events: SavedEvent[] }) {
  const [eventName, setEventName] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);
  const ev = events.find((e) => e.name === eventName);

  const start = useMutation({
    mutationFn: () =>
      apiClient.startMetricsReport({
        items: result.rows.map((r) => ({ cluster: r.cluster, namespace: r.namespace, workload: r.workload })),
        start: ev!.start, end: ev!.end, percentiles: result.percentiles,
      }),
    onSuccess: (r) => setJobId(r.id),
    onError: (e: Error) => toast.error(e.message || "Falha ao iniciar a coleta do evento"),
  });
  const job = useQuery({
    queryKey: ["metrics-compare-job", jobId],
    queryFn: () => apiClient.metricsReportJob(jobId!),
    enabled: !!jobId,
    retry: false,
    refetchInterval: (q) => (q.state.data?.state === "running" ? 2000 : false),
  });
  const running = start.isPending || job.data?.state === "running";
  const other = job.data?.state === "done" ? job.data.result : undefined;
  const cmp = useMemo(() => (other ? buildComparison(result, other) : null), [result, other]);

  // o nome do evento vai no título, numa coluna do CSV e no nome do arquivo
  const exportCmp = (kind: "md" | "csv") => {
    if (!cmp || !ev || !other) return;
    const base = `comparacao_${fileSlug(ev.name) || "evento"}_${fileStamp(new Date().toISOString())}`;
    if (kind === "md") downloadText(buildComparisonMarkdown(cmp, result, other, ev.name, new Date().toISOString()), "text/markdown", `${base}.md`);
    else downloadText(buildComparisonCSV(cmp, ev.name), "text/csv", `${base}.csv`);
  };

  return (
    <section className="rounded-md border border-border p-3 space-y-2">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-sm font-medium">Comparar com um evento</span>
        <select className="h-7 rounded border border-border bg-background px-1 text-xs max-w-[28rem]" value={eventName} disabled={running} onChange={(e) => { setEventName(e.target.value); setJobId(null); }}>
          <option value="">Escolha um evento salvo…</option>
          {events.map((e) => <option key={e.name} value={e.name}>{eventLabel(e)}</option>)}
        </select>
        <Button size="sm" className="h-7 text-xs gap-1" disabled={!ev || running} onClick={() => start.mutate()}>
          {running && <Loader2 className="h-3.5 w-3.5 animate-spin" />}Comparar
        </Button>
        {job.data?.state === "running" && <span className="text-muted-foreground">coletando o evento ({job.data.done} de {job.data.total})…</span>}
        {job.data?.state === "failed" && <span className="text-red-400">{job.data.error || "falha na coleta do evento"}</span>}
        {job.data?.state === "cancelled" && <span className="text-muted-foreground">coleta cancelada</span>}
      </div>
      <div className="text-[11px] text-muted-foreground">A coleta usa os mesmos alvos e percentis do relatório atual, na janela do evento. O relatório atual é a coluna "atual"; o evento é a base da variação.</div>

      {cmp && ev && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium">Exportar a comparação</span>
            <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => exportCmp("md")} title="Baixa a comparação em Markdown (uma tabela por cluster)"><FileDown className="h-3 w-3" />Markdown</Button>
            <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => exportCmp("csv")} title="Baixa a comparação em CSV (um registro por aplicação: atual, evento e variação)"><FileDown className="h-3 w-3" />CSV</Button>
          </div>
          {cmp.notes.map((n) => <div key={n} className="text-[11px] text-amber-400">{n}</div>)}
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-xs">
              <thead className="bg-muted/50 text-left">
                <tr>
                  <th className="px-2 py-1.5 font-medium">Aplicação</th>
                  {cmp.rows[0]?.metrics.map((m) => <th key={m.key} className="px-2 py-1.5 font-medium whitespace-nowrap">{m.label}</th>)}
                </tr>
              </thead>
              <tbody>
                {cmp.rows.map((r) => (
                  <Fragment key={`${r.cluster}|${r.namespace}|${r.workload}`}>
                    <tr className="border-t border-border/60 align-top">
                      <td className="px-2 py-1.5">
                        <div className="font-medium">{r.workload} <span className="text-[10px] text-muted-foreground">ns: {r.namespace}</span></div>
                        <div className="text-[10px] text-muted-foreground">{r.cluster}{r.presence !== "ambos" ? ` · só no ${r.presence === "so-atual" ? "relatório atual" : "evento"}` : ""}</div>
                      </td>
                      {r.metrics.map((m) => {
                        const v = verdict(m);
                        return (
                          <td key={m.key} className="px-2 py-1.5 whitespace-nowrap">
                            <div><b>{fmtCompareValue(m.kind, m.a)}</b> <span className="text-muted-foreground">atual</span></div>
                            <div className="text-muted-foreground">{fmtCompareValue(m.kind, m.b)} evento</div>
                            <div className={VERDICT_CLASS[v]} title={v === "pior" ? "Piorou em relação ao evento" : v === "melhor" ? "Melhorou em relação ao evento" : v === "sem-base" ? "Sem base: o evento tem zero ou falta dado" : "Variação dentro de 5% ou métrica só informativa"}>{fmtDelta(m.deltaPct)}</div>
                          </td>
                        );
                      })}
                    </tr>
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  );
}
