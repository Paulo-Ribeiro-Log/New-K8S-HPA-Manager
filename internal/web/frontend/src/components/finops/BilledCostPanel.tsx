// Custo faturado (amortizado, 30 dias) × custo estimado pelo relatório (finops_billed_cost.go): AKS
// pelo Cost Management (RG do AKS, RG de nodes e RG de dados), EKS pelo Cost Explorer (tag
// aws:eks:cluster-name), GKE pelo export de billing no BigQuery. A diferença mostra o que a
// estimativa não vê (rede, load balancer, monitoramento, plano do cluster) e o desconto das reservas.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Receipt, RefreshCw, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { fmtBRL } from "@/lib/finopsFormat";
import { apiClient } from "@/lib/api/client";
import type { BilledCostResponse } from "./types";
import type { CostBreakdown } from "./capacity";
import { SectionTitle } from "./charts";

export function useBilledCost(cluster: string, enabled: boolean) {
  return useQuery({
    queryKey: ["finops-billed-cost", cluster],
    queryFn: () => apiClient.getFinOpsBilledCost<BilledCostResponse>(cluster),
    enabled: enabled && !!cluster,
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
}

// Fator para converter a moeda da fatura em BRL (null = moeda sem conversão conhecida).
export function billedToBRL(data: BilledCostResponse | undefined, exchangeRate: number): number | null {
  const cur = (data?.currency ?? "BRL").toUpperCase();
  if (cur === "BRL") return 1;
  if (cur === "USD" && exchangeRate > 0) return exchangeRate;
  return null;
}

// Serviços que correspondem ao compute e aos discos estimados, por provedor. No GKE o "Compute
// Engine" inclui os discos (PD), então compute e discos vão juntos.
const SERVICES: Record<string, { cloud: string; compute: string[]; disks: string[] }> = {
  aks: { cloud: "Azure", compute: ["Virtual Machines"], disks: ["Storage"] },
  eks: { cloud: "AWS", compute: ["Amazon Elastic Compute Cloud - Compute", "EC2 - Other"], disks: [] },
  gke: { cloud: "Google Cloud", compute: ["Compute Engine"], disks: [] },
};
const ROLE: Record<string, string> = { cluster: "cluster", nodes: "nodes", data: "dados" };

const signedPct = (a: number, b: number) => {
  if (b <= 0) return "—";
  const p = Math.round(((a - b) / b) * 100);
  return `${p > 0 ? "+" : ""}${p}%`;
};

export function BilledCostPanel({ cluster, provider, cost, exchangeRate }: {
  cluster: string;
  provider?: string;
  cost: CostBreakdown;
  exchangeRate: number;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, isFetching } = useBilledCost(cluster, !!provider);
  if (!provider) return null;
  const map = SERVICES[data?.provider ?? provider] ?? SERVICES.aks;

  const refresh = async () => {
    const fresh = await apiClient.getFinOpsBilledCost<BilledCostResponse>(cluster, true);
    queryClient.setQueryData(["finops-billed-cost", cluster], fresh);
  };

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 rounded-xl border bg-card p-4 text-xs text-muted-foreground shadow-sm">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Consultando a fatura ({map.cloud})…
      </div>
    );
  }
  if (!data?.available) {
    return (
      <div className="flex items-start gap-2 rounded-xl border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        <Receipt className="h-3.5 w-3.5 mt-0.5 shrink-0" />
        <span><strong className="text-foreground">Custo faturado indisponível.</strong> {data?.reason ?? "Sem resposta do Cost Management."}</span>
      </div>
    );
  }

  const factor = billedToBRL(data, exchangeRate);
  const money = (v: number) => factor !== null ? fmtBRL(v * factor) : `${v.toLocaleString("pt-BR", { maximumFractionDigits: 0 })} ${data.currency}`;
  const brl = (v: number) => v * (factor ?? 1);
  const services = data.services ?? [];
  const billedOf = (names: string[]) => services.filter(s => names.includes(s.service)).reduce((s, x) => s + x.cost, 0);
  const vm = billedOf(map.compute);
  const disks = billedOf(map.disks);
  const known = [...map.compute, ...map.disks];
  const dataRG = (data.resource_groups ?? []).find(r => r.role === "data");
  const others = services.filter(s => !known.includes(s.service));
  const othersTotal = others.reduce((s, x) => s + x.cost, 0);
  const estDisks = cost.osDiskBRL + cost.pvcBRL + cost.orphanBRL;
  const joined = map.disks.length === 0; // EKS/GKE: o serviço de compute já inclui os discos

  const rows = [
    joined
      ? { label: "Compute e discos dos nodes", billed: brl(vm), est: (cost.computeBRL + estDisks) as number | null }
      : { label: "Compute (VMs dos nodes)", billed: brl(vm), est: cost.computeBRL as number | null },
    ...(joined ? [] : [{ label: "Discos (SO, PVCs e órfãos)", billed: brl(disks), est: estDisks as number | null }]),
    { label: dataRG ? "Não estimado (inclui o RG de dados)" : "Não estimado pela ferramenta", billed: brl(othersTotal), est: null },
  ].filter(r => r.billed > 0 || (r.est ?? 0) > 0);
  const max = Math.max(1, ...rows.flatMap(r => [r.billed, r.est ?? 0]));
  const billedTotal = brl(data.total);
  const diff = billedTotal - cost.totalBRL;

  return (
    <div className="rounded-xl border bg-card p-5 shadow-sm">
      <SectionTitle icon={Receipt} title={`Fatura da ${map.cloud} × estimativa`}
        sub={`Custo amortizado (com reservas e Savings Plan) de ${data.from?.split("-").reverse().join("/")} a ${data.to?.split("-").reverse().join("/")}`}
        right={
          <Button size="sm" variant="ghost" className="h-7 gap-1.5 text-xs" onClick={refresh} disabled={isFetching}
            title={`Consultado ${data.fetched_at ? new Date(data.fetched_at).toLocaleString("pt-BR") : ""} — o Cost Management atualiza ~1 vez por dia`}>
            <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`} /> Atualizar
          </Button>
        } />

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg bg-indigo-500/10 p-3 ring-1 ring-indigo-500/20">
          <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Faturado — {data.window_days} dias</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums text-indigo-700 dark:text-indigo-300">{money(data.total)}</p>
        </div>
        <div className="rounded-lg bg-muted/50 p-3 ring-1 ring-border">
          <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Estimado pelo relatório — mês</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums">{fmtBRL(cost.totalBRL)}</p>
        </div>
        <div className="rounded-lg bg-muted/50 p-3 ring-1 ring-border">
          <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Fatura × estimativa</p>
          <p className={`mt-1 text-2xl font-semibold tabular-nums ${Math.abs(diff) / Math.max(1, cost.totalBRL) < 0.1 ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"}`}>
            {factor !== null ? signedPct(billedTotal, cost.totalBRL) : "—"}
          </p>
          <p className="text-[11px] text-muted-foreground">{factor !== null ? `${diff >= 0 ? "+" : "−"}${fmtBRL(Math.abs(diff))}` : `moeda ${data.currency}, sem conversão`}</p>
        </div>
      </div>

      {factor !== null && (
        <div className="mt-4 space-y-3">
          {rows.map(r => (
            <div key={r.label}>
              <div className="flex items-baseline justify-between gap-2 text-xs">
                <span className="font-medium">{r.label}</span>
                <span className="tabular-nums text-muted-foreground">
                  <span className="font-semibold text-indigo-600 dark:text-indigo-400">{fmtBRL(r.billed)}</span> faturado
                  {r.est !== null && <> · {fmtBRL(r.est)} estimado ({signedPct(r.billed, r.est)})</>}
                </span>
              </div>
              <div className="mt-1 space-y-0.5">
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                  <div className="h-full rounded-full bg-gradient-to-r from-indigo-500 to-violet-500" style={{ width: `${(r.billed / max) * 100}%` }} />
                </div>
                {r.est !== null && (
                  <div className="h-2 rounded-full bg-muted overflow-hidden">
                    <div className="h-full rounded-full bg-slate-400/70 dark:bg-slate-500/70" style={{ width: `${(r.est / max) * 100}%` }} />
                  </div>
                )}
              </div>
            </div>
          ))}
          <p className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-full bg-indigo-500" />faturado</span>
            <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-full bg-slate-400" />estimado pelo relatório</span>
          </p>
        </div>
      )}

      {others.length > 0 && (
        <div className="mt-4">
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Serviços fora da estimativa</p>
          <div className="flex flex-wrap gap-1.5">
            {others.slice(0, 10).map(s => (
              <span key={s.service} className="rounded-full border bg-muted/40 px-2 py-0.5 text-[11px]">
                {s.service} <span className="font-semibold tabular-nums">{money(s.cost)}</span>
              </span>
            ))}
          </div>
        </div>
      )}

      <p className="mt-3 text-[11px] text-muted-foreground">
        A fatura cobre {data.window_days} dias e os escopos {(data.resource_groups ?? []).map(r => `${r.name} (${ROLE[r.role] ?? r.role}: ${money(r.total)})`).join(", ")}; a estimativa é mensal e não inclui
        rede, load balancer, monitoramento{dataRG ? " nem os recursos de dados" : ""}.
      </p>
    </div>
  );
}
