// Acompanhamento das recomendações enviadas (Fase 3): a foto do envio contra o relatório mais
// recente (GET /finops/actions, avaliação em internal/finops/actions.go). Valores em reais no custo
// efetivo do pool (mesmo fator da tela), para bater com a economia mostrada nas Oportunidades.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CheckCircle2, Clock, CircleDashed, CircleHelp, Trash2, PiggyBank, Send, FileCode2, Terminal, Loader2 } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { fmtBRL, fmtMillis, fmtMi } from "@/lib/finopsFormat";
import type { FinOpsActionRow, FinOpsReport } from "./types";
import { poolCostFactors } from "./capacity";
import { StatTile } from "./charts";

const STATUS: Record<FinOpsActionRow["evaluation"]["status"], { label: string; cls: string; icon: typeof Clock; hint: string }> = {
  aguardando_scan: { label: "Aguardando scan", cls: "bg-slate-500/15 text-slate-700 dark:text-slate-300", icon: Clock, hint: "Rode Analisar depois de aplicar para medir" },
  pendente: { label: "Não aplicada", cls: "bg-amber-500/15 text-amber-700 dark:text-amber-300", icon: CircleDashed, hint: "O cluster ainda está com os valores da época do envio" },
  parcial: { label: "Parcial", cls: "bg-sky-500/15 text-sky-700 dark:text-sky-300", icon: CircleDashed, hint: "Parte do alvo chegou ao cluster" },
  aplicada: { label: "Aplicada", cls: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300", icon: CheckCircle2, hint: "O cluster está com o alvo (≥ 80% do caminho)" },
  nao_encontrado: { label: "Workload sumiu", cls: "bg-rose-500/15 text-rose-700 dark:text-rose-300", icon: CircleHelp, hint: "Não está no relatório mais recente (renomeado ou removido)" },
};

const CHANNEL = { staging: { label: "Staging", icon: Send }, values: { label: "Patch de values", icon: FileCode2 }, kubectl: { label: "kubectl", icon: Terminal } };

function Change({ label, before, after, now, fmt }: { label: string; before: number; after?: number; now?: number; fmt: (v: number) => string }) {
  if (after === undefined || after <= 0) return null;
  const done = now !== undefined && Math.abs(now - after) <= Math.max(1, after * 0.2);
  return (
    <span className="whitespace-nowrap">
      {label} {fmt(before)} → <strong>{fmt(after)}</strong>
      {now !== undefined && <span className={done ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground"}> (hoje {fmt(now)})</span>}
    </span>
  );
}

export function ActionsTrackingTab({ cluster, report }: { cluster: string; report: FinOpsReport }) {
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["finops-actions", cluster, report.generated_at],
    queryFn: () => apiClient.listFinOpsActions<FinOpsActionRow>(cluster),
    staleTime: 30_000,
  });
  const factorOf = poolCostFactors(report);
  const poolOf = new Map((report.workloads ?? []).map(w => [`${w.namespace}/${w.workload}`, w.node_pool]));
  const f = (a: FinOpsActionRow) => factorOf(poolOf.get(`${a.namespace}/${a.workload}`));

  const remove = async (a: FinOpsActionRow) => {
    try {
      await apiClient.deleteFinOpsAction(cluster, a.id);
      await queryClient.invalidateQueries({ queryKey: ["finops-actions", cluster] });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    }
  };

  if (isLoading) return <p className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" /> Carregando…</p>;
  const actions = data?.actions ?? [];
  if (actions.length === 0) {
    return (
      <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">
        Nenhuma recomendação em acompanhamento. Em <strong>Oportunidades</strong>, use <strong>Enviar ao Staging</strong> ou <strong>Patch de values</strong>:
        o envio guarda a foto do workload e, no próximo <strong>Analisar</strong>, aparece aqui se foi aplicado e quanto economizou.
      </div>
    );
  }

  const measured = actions.filter(a => a.evaluation.status === "aplicada" || a.evaluation.status === "parcial");
  const realized = measured.reduce((s, a) => s + a.evaluation.realized_saving_brl * f(a), 0);
  const expected = actions.reduce((s, a) => s + a.expected_saving_brl, 0);
  const waiting = actions.filter(a => a.evaluation.status === "aguardando_scan" || a.evaluation.status === "pendente").length;

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile icon={Send} label="Enviadas" value={actions.length} sub={`${waiting} aguardando aplicação ou scan`} />
        <StatTile icon={CheckCircle2} label="Aplicadas" value={measured.length} sub="total ou parcialmente" tone={measured.length > 0 ? "good" : "default"} />
        <StatTile icon={PiggyBank} label="Economia realizada" value={measured.length ? `${fmtBRL(realized)}/mês` : "—"}
          sub="custo na foto − custo no último scan" tone={realized > 0 ? "good" : realized < 0 ? "warn" : "default"} />
        <StatTile icon={PiggyBank} label="Economia esperada" value={`${fmtBRL(expected)}/mês`} sub="soma do previsto no envio" />
      </div>

      <div className="divide-y rounded-xl border bg-card shadow-sm">
        {actions.map(a => {
          const st = STATUS[a.evaluation.status];
          const ch = CHANNEL[a.channel];
          const ev = a.evaluation;
          const saving = ev.realized_saving_brl * f(a);
          const showMeasured = ev.status === "aplicada" || ev.status === "parcial";
          return (
            <div key={a.id} className="flex flex-wrap items-start justify-between gap-3 px-4 py-3 text-sm">
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{a.workload}</span>
                  <span className="text-xs text-muted-foreground">{a.namespace}</span>
                  <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${st.cls}`} title={st.hint}>
                    <st.icon className="h-3 w-3" /> {st.label}{ev.targets > 0 && showMeasured ? ` ${ev.applied}/${ev.targets}` : ""}
                  </span>
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-muted-foreground">
                  <Change label="min" before={a.baseline_hpa_min} after={a.target_hpa_min} now={ev.current_hpa_min} fmt={String} />
                  <Change label="max" before={a.baseline_hpa_max} after={a.target_hpa_max} now={ev.current_hpa_max} fmt={String} />
                  <Change label="CPU" before={a.baseline_cpu_millis} after={a.target_cpu_millis} now={ev.current_cpu_millis} fmt={fmtMillis} />
                  <Change label="Mem" before={a.baseline_mem_mi} after={a.target_mem_mi} now={ev.current_mem_mi} fmt={fmtMi} />
                </div>
                <p className="flex items-center gap-1 text-[11px] text-muted-foreground">
                  <ch.icon className="h-3 w-3" /> {ch.label} · {new Date(a.created_at).toLocaleString("pt-BR")}{a.created_by ? ` · ${a.created_by}` : ""}
                </p>
              </div>
              <div className="flex items-start gap-3">
                <div className="text-right">
                  {showMeasured ? (
                    <>
                      <p className={`text-base font-semibold tabular-nums ${saving >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"}`}>
                        {saving >= 0 ? "-" : "+"}{fmtBRL(Math.abs(saving))}/mês
                      </p>
                      <p className="text-[11px] text-muted-foreground">realizado · esperado {fmtBRL(a.expected_saving_brl)}</p>
                    </>
                  ) : (
                    <p className="text-[11px] text-muted-foreground">esperado {fmtBRL(a.expected_saving_brl)}/mês</p>
                  )}
                </div>
                <button onClick={() => remove(a)} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-rose-600" title="Tirar do acompanhamento">
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          );
        })}
      </div>
      <p className="text-[11px] text-muted-foreground">
        A economia realizada compara o custo do workload na foto do envio com o do último scan. Ela inclui variação de carga (mais ou menos réplicas) além da mudança aplicada.
      </p>
    </div>
  );
}
