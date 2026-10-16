// Reserva para a carga base (Fase 3): por pool, os nodes que ficam ligados o tempo todo (mínimo do
// autoscaler ou pool fixo), a parte já coberta e a economia de reserva/Savings Plan de 1 e 3 anos,
// com preço da Azure Retail Prices API (GET /finops/base-reservations).
import { useQuery } from "@tanstack/react-query";
import { BadgePercent, Loader2, Info } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { fmtBRL } from "@/lib/finopsFormat";
import type { BaseReservationSuggestion, CommitmentOption } from "./types";
import { SectionTitle } from "./charts";

const LABEL: Record<string, string> = { "reservation-1y": "Reserva 1 ano", "reservation-3y": "Reserva 3 anos", "savings_plan-1y": "Savings Plan 1 ano", "savings_plan-3y": "Savings Plan 3 anos" };
const keyOf = (o: CommitmentOption) => `${o.kind}-${o.term}`;

export function BaseReservationCard({ cluster, reportAt }: { cluster: string; reportAt: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["finops-base-reservations", cluster, reportAt],
    queryFn: () => apiClient.get<{ available: boolean; reason?: string; pools?: BaseReservationSuggestion[] }>(`/finops/base-reservations?cluster=${encodeURIComponent(cluster)}`),
    staleTime: 10 * 60_000,
  });
  if (isLoading) return <p className="flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" /> Consultando preços de reserva…</p>;
  if (!data?.available) return null;
  const pools = data.pools ?? [];
  const actionable = pools.filter(p => (p.options?.length ?? 0) > 0);
  const totals = new Map<string, { saving: number; commitment: number; pools: number }>();
  for (const p of actionable) for (const o of p.options!) {
    const t = totals.get(keyOf(o)) ?? { saving: 0, commitment: 0, pools: 0 };
    t.saving += o.saving_month_brl;
    t.commitment += o.commitment_brl;
    t.pools++;
    totals.set(keyOf(o), t);
  }
  const order = ["reservation-1y", "reservation-3y", "savings_plan-1y", "savings_plan-3y"];

  return (
    <div className="rounded-xl border bg-card p-5 shadow-sm">
      <SectionTitle icon={BadgePercent} title="Reserva para a carga base"
        sub="Nodes que ficam ligados o tempo todo — o que os requests pedem com todo HPA no mínimo (ou o min do autoscaler, se maior) — menos o que já está sob reserva/Savings Plan" />

      {actionable.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nenhum pool com carga base descoberta — veja o motivo de cada pool abaixo.</p>
      ) : (
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          {order.filter(k => totals.has(k)).map(k => (
            <div key={k} className="rounded-lg border bg-background/60 p-3">
              <p className="text-[11px] uppercase tracking-wide text-muted-foreground">{LABEL[k]}</p>
              <p className="mt-1 text-xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">{fmtBRL(totals.get(k)!.saving)}/mês</p>
              <p className="text-[11px] text-muted-foreground">
                {totals.get(k)!.pools < actionable.length && <span className="text-amber-600 dark:text-amber-400">em {totals.get(k)!.pools} de {actionable.length} pools (sem preço de reserva nos demais) · </span>}
                compromisso de {fmtBRL(totals.get(k)!.commitment)} no prazo
              </p>
            </div>
          ))}
        </div>
      )}

      <div className="mt-4 overflow-x-auto">
        <table className="w-full text-xs">
          <thead className="text-[10px] uppercase tracking-wide text-muted-foreground">
            <tr className="border-b">
              <th className="py-1.5 text-left font-medium">Pool</th>
              <th className="py-1.5 text-right font-medium">Nodes</th>
              <th className="py-1.5 text-right font-medium" title="nodes que os requests pedem com todo HPA no mínimo, ou o min do autoscaler se maior; pool fixo = inteiro">Carga base</th>
              <th className="py-1.5 text-right font-medium" title="estimado pela fatia de reserva + Savings Plan no custo do pool">Já coberto</th>
              <th className="py-1.5 text-right font-medium">Sugerido</th>
              <th className="py-1.5 pl-4 text-left font-medium">Economia por mês (nos nodes sugeridos)</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {pools.map(p => (
              <tr key={p.pool}>
                <td className="py-2">
                  <p className="font-medium">{p.pool}</p>
                  <p className="text-[10px] text-muted-foreground">{p.vm_size}{p.on_demand_node_brl > 0 && ` · ${fmtBRL(p.on_demand_node_brl)}/node sob demanda`}</p>
                </td>
                <td className="py-2 text-right tabular-nums">{p.nodes}</td>
                <td className="py-2 text-right tabular-nums" title={`Com todo HPA no mínimo, os requests pedem ${p.nodes_at_hpa_min} node(s)${p.autoscaling ? `; o min do autoscaler é ${p.min_nodes ?? 0}` : "; pool sem autoscaler"}`}>
                  {p.base_nodes}
                  <span className="block text-[10px] text-muted-foreground">{p.base_source === "hpa_min" ? "HPAs no mínimo" : p.base_source === "autoscaler_min" ? "min do autoscaler" : "pool fixo"}</span>
                </td>
                <td className="py-2 text-right tabular-nums">{p.covered_nodes}</td>
                <td className="py-2 text-right tabular-nums font-semibold">{p.skip ? "—" : p.suggested_nodes}</td>
                <td className="py-2 pl-4">
                  {p.options?.length ? (
                    <div className="flex flex-wrap gap-1.5">
                      {p.options.map(o => (
                        <span key={keyOf(o)} className={`rounded-full px-2 py-0.5 text-[10px] ${o.kind === "reservation" ? "bg-blue-500/10 text-blue-700 dark:text-blue-300" : "bg-purple-500/10 text-purple-700 dark:text-purple-300"}`}
                          title={`${fmtBRL(o.monthly_node_brl)}/node · compromisso de ${fmtBRL(o.commitment_brl)} no prazo`}>
                          {LABEL[keyOf(o)]}: {fmtBRL(o.saving_month_brl)} (−{Math.round(o.saving_pct)}%)
                        </span>
                      ))}
                    </div>
                  ) : <span className="text-[11px] text-muted-foreground">{p.skip}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-3 flex items-start gap-1 text-[11px] text-muted-foreground">
        <Info className="h-3 w-3 mt-0.5 shrink-0" />
        Preço de tabela da Azure Retail Prices API, convertido pelo câmbio do relatório. Reserva vale para o SKU (com flexibilidade de tamanho na série); Savings Plan vale para qualquer VM da subscription. A carga base supõe todo HPA no mínimo ao mesmo tempo (85% de aproveitamento dos nodes): é a escolha conservadora — se o pool nunca desce até ela, cabe reservar mais. A compra é feita no portal (Reservations / Savings plans) por quem tem acesso de billing.
      </p>
    </div>
  );
}
