// Ambientes não produtivos fora do horário (Fase 3): quanto se economiza parando o AKS ou
// escalando os pools User a 0 à noite e no fim de semana. Conta em capacity.ts (offHoursEstimate).
import { useState } from "react";
import { Moon, Info } from "lucide-react";
import { fmtBRL, KubectlBlock } from "@/lib/finopsFormat";
import type { FinOpsPool } from "./types";
import { type OffHoursSchedule, offHoursEstimate } from "./capacity";
import { SectionTitle } from "./charts";

const HOURS = Array.from({ length: 24 }, (_, h) => h);

export function OffHoursCard({ pools, cluster, provider }: { pools: FinOpsPool[]; cluster: string; provider?: string }) {
  const [s, setS] = useState<OffHoursSchedule>({ offStart: 20, offEnd: 8, weekends: true });
  const est = offHoursEstimate(pools, s);
  const name = cluster.replace(/-admin$/, "");
  const userPools = est.pools.filter(p => !p.stays);

  const option = (title: string, sub: string, v: { certainBRL: number; committedBRL: number }) => (
    <div className="rounded-lg border bg-background/60 p-3">
      <p className="text-xs font-medium">{title}</p>
      <p className="text-[11px] text-muted-foreground">{sub}</p>
      <p className="mt-1.5 text-2xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">
        {v.certainBRL >= 1 ? `${fmtBRL(v.certainBRL)}/mês` : "—"}
      </p>
      <p className="text-[11px] text-muted-foreground">
        {v.certainBRL >= 1 ? `${fmtBRL(v.certainBRL * 12)}/ano` : "nada certo"}
        {v.committedBRL >= 1 && <> · <span className="text-amber-600 dark:text-amber-400">+ até {fmtBRL(v.committedBRL)} em pools sob reserva (só reaproveitando a reserva)</span></>}
      </p>
    </div>
  );

  return (
    <div className="rounded-xl border border-indigo-500/30 bg-gradient-to-br from-indigo-500/5 to-violet-500/5 p-5 shadow-sm">
      <SectionTitle icon={Moon} title="Fora do horário"
        sub={`Ambiente não produtivo: desligado ${Math.round(est.fraction * 100)}% da semana (${Math.round(est.fraction * 168)} h de 168 h)`}
        right={
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1">desliga às
              <select value={s.offStart} onChange={e => setS({ ...s, offStart: Number(e.target.value) })} className="h-7 rounded border bg-background px-1">
                {HOURS.map(h => <option key={h} value={h}>{h}h</option>)}
              </select>
            </label>
            <label className="flex items-center gap-1">liga às
              <select value={s.offEnd} onChange={e => setS({ ...s, offEnd: Number(e.target.value) })} className="h-7 rounded border bg-background px-1">
                {HOURS.map(h => <option key={h} value={h}>{h}h</option>)}
              </select>
            </label>
            <label className="flex items-center gap-1 cursor-pointer">
              <input type="checkbox" checked={s.weekends} onChange={e => setS({ ...s, weekends: e.target.checked })} className="h-3.5 w-3.5" />
              fim de semana inteiro
            </label>
          </div>
        } />

      <div className="grid gap-3 md:grid-cols-2">
        {option("Parar o cluster", "az aks stop: todos os nodes saem (os discos continuam cobrados)", est.stopCluster)}
        {option("Escalar pools User a 0", `o pool System fica ligado; ${userPools.length} pool(s) User`, est.scaleUserPools)}
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        {est.pools.map(p => (
          <span key={p.pool.name} className={`rounded-full border px-2 py-0.5 text-[11px] ${p.committed ? "border-amber-500/40 text-amber-700 dark:text-amber-300" : ""}`}
            title={p.committed ? "Sob reserva/Savings Plan: desligar não reduz a fatura, a menos que a reserva seja usada por outra carga" : `custo/mês ${fmtBRL(p.monthlyBRL)}`}>
            {p.pool.name}{p.stays ? " (System)" : ""} · {fmtBRL(p.savingBRL)}{p.committed ? " · reserva" : ""}
          </span>
        ))}
      </div>

      {provider === "aks" && (
        <div className="mt-3 space-y-1.5">
          <KubectlBlock cmd={`az aks stop --resource-group <RESOURCE_GROUP> --name ${name}   # às ${s.offStart}h; para religar: az aks start ...`} />
          {userPools.length > 0 && (
            <KubectlBlock cmd={userPools.map(p => `az aks nodepool update --resource-group <RESOURCE_GROUP> --cluster-name ${name} --name ${p.pool.name} --update-cluster-autoscaler --min-count 0 --max-count <MAX>`).join(" && ")} />
          )}
        </div>
      )}
      <p className="mt-2 flex items-start gap-1 text-[11px] text-muted-foreground">
        <Info className="h-3 w-3 mt-0.5 shrink-0" />
        Estimativa com o custo atual dos pools (efetivo quando há cobertura) e supondo o ambiente ligado 24 h hoje. Para automatizar, use um agendamento (Azure Automation, pipeline ou KEDA cron). Com min 0 no autoscaler, os nodes só saem quando os pods também saem (escale os deployments a 0 no mesmo horário).
      </p>
    </div>
  );
}
