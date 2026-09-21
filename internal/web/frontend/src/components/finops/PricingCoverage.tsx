import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { BadgePercent, Loader2, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { fmtBRL } from "@/lib/finopsFormat";

// Cobertura de reserva / Savings Plan dos node pools — ver internal/finops/coverage.go e
// internal/web/handlers/finops_coverage.go. As sugestões de tier calculam a economia com o preço de
// TABELA; num pool coberto por reserva isso engana (a reserva é paga de qualquer jeito). Os números
// vêm do custo AMORTIZADO do Cost Management (só leitura), por node pool.

export interface PoolCoverage {
  reservation: number; // 0..1, fração do custo efetivo
  savings_plan: number;
  on_demand: number;
  spot: number;
  other?: number;
  effective_cost: number;
  currency?: string;
  window_days: number;
  fetched_at: string;
}

// Intervalo real do efeito da troca em pool com reserva, em R$/mês com sinal de ECONOMIA (positivo =
// economiza, negativo = gasta mais). A reserva é paga de qualquer jeito: o melhor caso supõe que ela é
// reaproveitada por outras VMs do mesmo SKU (= economia de tabela); o pior, que fica ociosa.
export interface SwapScenarios {
  best_case_savings_brl: number;
  worst_case_savings_brl: number;
  note: string;
}

// Este SKU (ou a série dele) já roda sob reserva/Savings Plan na frota — uso observado no Cost
// Management. Não diz se há capacidade ociosa (isso exigiria ler as reservas).
export interface ReservedHint {
  kind: "reservation" | "savings_plan";
  scope: "sku" | "series";
  note: string;
}

export interface CoverageWarning {
  level: "reservation" | "savings_plan";
  note: string;
  scenarios?: SwapScenarios;
}

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

function ago(iso: string): string {
  const mins = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  if (mins < 60) return `${mins} min`;
  const h = Math.round(mins / 60);
  if (h < 48) return `${h} h`;
  return `${Math.round(h / 24)} d`;
}

const SEGMENTS: { key: keyof PoolCoverage; label: string; bar: string }[] = [
  { key: "reservation", label: "Reserva", bar: "bg-blue-500" },
  { key: "savings_plan", label: "Savings Plan", bar: "bg-purple-500" },
  { key: "on_demand", label: "Sob demanda", bar: "bg-slate-400" },
  { key: "spot", label: "Spot", bar: "bg-amber-500" },
];

/** Barra + resumo de quanto do custo efetivo do pool vem de cada modelo de preço. */
export function CoverageLine({ coverage }: { coverage?: PoolCoverage }) {
  if (!coverage) {
    return (
      <p className="text-[10px] text-muted-foreground italic" title='Use "Atualizar cobertura" para consultar o Cost Management'>
        Reserva/Savings Plan: não consultado
      </p>
    );
  }
  const parts = SEGMENTS.map((s) => ({ ...s, v: Number(coverage[s.key] ?? 0) })).filter((s) => s.v >= 0.005);
  const title =
    `Participação no CUSTO EFETIVO (amortizado) do pool nos últimos ${coverage.window_days} dias — não em horas: ` +
    `uma hora coberta por reserva custa menos que uma sob demanda, então a fatia de reserva aparece um pouco menor que a real. ` +
    `Custo efetivo: ${coverage.effective_cost.toLocaleString("pt-BR", { maximumFractionDigits: 0 })} ${coverage.currency ?? ""}.`;
  return (
    <div className="space-y-0.5" title={title}>
      <div className="flex h-1.5 w-full max-w-[220px] overflow-hidden rounded bg-muted">
        {parts.map((p) => (
          <div key={p.key} className={p.bar} style={{ width: `${p.v * 100}%` }} />
        ))}
      </div>
      <p className="text-[10px] text-muted-foreground">
        {parts.map((p) => `${p.label} ${Math.round(p.v * 100)}%`).join(" · ")} ({coverage.window_days}d · há {ago(coverage.fetched_at)})
      </p>
    </div>
  );
}

/** Aviso, por alternativa de SKU, de que a economia (preço de tabela) pode não se realizar. */
/** Etiqueta "Reserva na frota" / "Savings Plan na frota" de uma alternativa de SKU. */
export function ReservedHintChip({ hint }: { hint?: ReservedHint }) {
  if (!hint) return null;
  const isPlan = hint.kind === "savings_plan";
  const bySeries = hint.scope === "series";
  const label = isPlan ? "Savings Plan na frota" : bySeries ? "Reserva da série na frota" : "Reserva na frota";
  const cls = isPlan
    ? "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300"
    : "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${cls} ${bySeries ? "border border-dashed border-current" : ""}`}
      title={hint.note}
    >
      <BadgePercent className="h-3 w-3" /> {label}
    </span>
  );
}

function ScenarioValue({ v }: { v: number }) {
  // Sinal de economia: positivo = economiza; negativo = o custo sobe.
  if (Math.abs(v) < 1) return <strong>≈ R$ 0/mês</strong>;
  return v > 0
    ? <strong className="text-green-700 dark:text-green-400">economiza {fmtBRL(v)}/mês</strong>
    : <strong className="text-red-700 dark:text-red-400">custa +{fmtBRL(Math.abs(v))}/mês</strong>;
}

export function CoverageWarningBox({ warning }: { warning?: CoverageWarning }) {
  if (!warning) return null;
  const sc = warning.scenarios;
  return (
    <div className="flex gap-1.5 rounded-md border border-amber-300 bg-amber-50 p-1.5 text-[10px] text-amber-800 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-300">
      <AlertTriangle className="h-3 w-3 shrink-0 mt-0.5" />
      <span>
        <strong>{warning.level === "reservation" ? "Pool coberto por reserva. " : "Pool coberto por Savings Plan. "}</strong>
        {warning.note}
        {sc && (
          <span className="mt-1 block rounded bg-background/60 p-1 text-foreground" title={sc.note}>
            <span className="block"><span className="text-muted-foreground">Melhor caso (reserva reaproveitada): </span><ScenarioValue v={sc.best_case_savings_brl} /></span>
            <span className="block"><span className="text-muted-foreground">Pior caso (reserva ociosa): </span><ScenarioValue v={sc.worst_case_savings_brl} /></span>
            <span className="block text-[9px] text-muted-foreground">Passe o mouse para ver o cálculo. Considera os nodes atuais e o pool inteiro trocado.</span>
          </span>
        )}
      </span>
    </div>
  );
}

export function CoverageRefreshButton({ cluster }: { cluster: string }) {
  const queryClient = useQueryClient();
  const [loading, setLoading] = useState(false);
  const notAzure = cluster.startsWith("arn:aws:eks:") || cluster.startsWith("gke_");

  const refresh = async () => {
    setLoading(true);
    try {
      const r = await fetch("/api/v1/finops/pricing-coverage/refresh", {
        method: "POST",
        headers: { ...authHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify({ cluster }),
      });
      const body = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error((body as { error?: string }).error ?? `Erro ${r.status}`);
      const b = body as { pools: unknown[]; window_days: number; from: string; to: string; sku_index?: { skus: number; error?: string } };
      toast.success(`Cobertura atualizada: ${b.pools.length} pool(s), ${b.from} → ${b.to}` +
        (b.sku_index && !b.sku_index.error ? ` · ${b.sku_index.skus} SKU(s) com reserva/Savings Plan na frota` : ""));
      if (b.sku_index?.error) toast.warning(`Índice de SKUs não atualizado: ${b.sku_index.error}`);
      // Aviso e cobertura são montados na LEITURA do rightsizing — recarrega para aparecerem.
      await queryClient.invalidateQueries({ queryKey: ["finops-rightsizing", cluster] });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Button
      size="sm"
      variant="outline"
      onClick={refresh}
      disabled={loading || notAzure}
      title={
        notAzure
          ? "Disponível só para clusters AKS (Azure)"
          : "Lê o custo amortizado dos últimos 30 dias no Cost Management (somente leitura) para saber quanto de cada pool está em reserva/Savings Plan"
      }
    >
      {loading ? <Loader2 className="h-4 w-4 mr-1.5 animate-spin" /> : <BadgePercent className="h-4 w-4 mr-1.5" />}
      Atualizar cobertura
    </Button>
  );
}
