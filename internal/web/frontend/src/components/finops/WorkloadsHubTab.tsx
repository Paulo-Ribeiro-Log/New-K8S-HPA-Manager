// Aba Workloads do FinOps (Fase 2 do FINOPS-REVISAO-PLAN.md): junta o Rightsizing (request/limit
// por workload, scan e histórico próprios) e as Oportunidades (ações por workload: HPA, requests,
// riscos). Abas manuais — shadcn <Tabs> quebra a cadeia flex (ver CLAUDE.md).
import { useState } from "react";
import { Gauge, ListChecks, Send } from "lucide-react";
import { RightsizingTab } from "@/components/RightsizingTab";
import type { FinOpsReport } from "./types";
import { OpportunitiesTab } from "./OpportunitiesTab";
import { ActionsTrackingTab } from "./ActionsTrackingTab";
import { isOpportunity } from "./helpers";
import { isCommittedPool, poolCostFactors, toEffectiveReport } from "./capacity";

type View = "rightsizing" | "opportunities" | "tracking";

export function WorkloadsHubTab({ cluster, report, provider }: { cluster: string; report: FinOpsReport; provider?: string }) {
  const [view, setView] = useState<View>("rightsizing");
  // Oportunidades em custo efetivo; o Rightsizing recebe o fator por pool (os dados dele vêm do scan).
  const workloads = toEffectiveReport(report).workloads ?? [];
  const factorOf = poolCostFactors(report);
  const committedPools = new Set((report.node_pools ?? []).filter(isCommittedPool).map(p => p.name));
  const opportunities = workloads.filter(isOpportunity).length;

  const btn = (v: View, label: string, Icon: typeof Gauge, count?: number) => (
    <button
      onClick={() => setView(v)}
      className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors ${
        view === v ? "bg-background shadow-sm font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
      }`}
    >
      <Icon className="h-3.5 w-3.5" />
      {label}
      {count !== undefined && count > 0 && (
        <span className="rounded-full bg-primary/10 px-1.5 text-[10px] font-semibold text-primary tabular-nums">{count}</span>
      )}
    </button>
  );

  return (
    <div className="space-y-3">
      <div className="inline-flex rounded-lg bg-muted p-1">
        {btn("rightsizing", "Request e limit", Gauge)}
        {btn("opportunities", "Oportunidades", ListChecks, opportunities)}
        {btn("tracking", "Acompanhamento", Send)}
      </div>
      {view === "tracking" ? (
        <ActionsTrackingTab cluster={cluster} report={report} />
      ) : view === "rightsizing" ? (
        <RightsizingTab cluster={cluster} provider={provider}
          costFactor={factorOf}
          committedPool={pool => !!pool && committedPools.has(pool)} />
      ) : (
        <OpportunitiesTab
          cluster={cluster}
          workloads={workloads}
          summary={report.summary}
          windowDays={report.window_days || 30}
          historicalRan={report.window_days > 0}
        />
      )}
    </div>
  );
}
