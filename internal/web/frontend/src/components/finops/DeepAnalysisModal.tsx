// Deep Analysis de um node pool (FINOPS-DEEP-ANALYSIS-PLAN.md): diagnóstico de alocação, DaemonSets,
// workloads, HPAs/throttling e simulação de VMs, com exportação em Markdown. Abas manuais com useState
// (o <Tabs> do shadcn quebra a cadeia flex dentro de modal — ver CLAUDE.md).
import { Fragment, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  AlertTriangle, ChevronDown, ChevronUp, Download, Info, Loader2, Microscope, OctagonAlert, RefreshCw,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { fmtBRL, fmtMi, fmtMillis } from "@/lib/finopsFormat";
import type {
  DeepCoverage, DeepDemand, DeepFinding, DeepNodeCalc, DeepNodeFit, DeepSimulationInputs, DeepResource, DeepSimulation, DeepWorkloadRow, PoolDeepAnalysis,
} from "@/types/finopsDeepAnalysis";

type Section = "summary" | "allocation" | "daemonsets" | "workloads" | "hpa" | "simulation";

const FLAG_LABELS: Record<string, string> = {
  no_requests: "sem requests",
  no_mem_request: "sem request de memória",
  no_mem_limit: "sem limit de memória",
  mem_under_requested: "memória acima do request",
  cpu_under_requested: "CPU acima do request",
  cpu_over_requested: "CPU request ≥ 4× uso",
  high_mem_limit_ratio: "limit mem > 2× request",
  hpa_pinned_max: "HPA no máximo",
  hpa_pinned_min: "HPA no mínimo",
  hpa_memory_metric: "HPA por memória",
  rec_adjusted_for_hpa: "rec. ajustado ao HPA",
  cpu_throttled: "throttling de CPU",
};
const DANGER_FLAGS = new Set(["mem_under_requested", "cpu_throttled", "hpa_pinned_max", "no_mem_request"]);

const LIMITING: Record<string, string> = { cpu: "CPU", memory: "memória", pods: "pods", min_nodes: "mínimo" };

const pctFmt = (v: number) => `${Math.round(v)}%`;

function usageColor(p: number) {
  if (p >= 95) return "text-red-600 dark:text-red-400 font-semibold";
  if (p >= 85) return "text-orange-600 dark:text-orange-400 font-medium";
  return "";
}

function FlagBadges({ flags }: { flags: string[] }) {
  if (!flags?.length) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {flags.map(f => (
        <Badge key={f} variant={DANGER_FLAGS.has(f) ? "destructive" : "secondary"} className="text-[10px] font-normal">
          {FLAG_LABELS[f] ?? f}
        </Badge>
      ))}
    </div>
  );
}

function FindingCard({ f }: { f: DeepFinding }) {
  const cfg = {
    critical: { icon: OctagonAlert, cls: "border-red-500/40 bg-red-500/5", icls: "text-red-600" },
    warning: { icon: AlertTriangle, cls: "border-orange-500/40 bg-orange-500/5", icls: "text-orange-500" },
    info: { icon: Info, cls: "border-blue-500/30 bg-blue-500/5", icls: "text-blue-500" },
  }[f.severity];
  const Icon = cfg.icon;
  return (
    <div className={`border rounded-lg p-3 flex gap-2.5 ${cfg.cls}`}>
      <Icon className={`h-4 w-4 mt-0.5 shrink-0 ${cfg.icls}`} />
      <div className="space-y-0.5">
        <p className="text-sm font-medium">{f.title}</p>
        <p className="text-xs text-muted-foreground leading-relaxed">{f.detail}</p>
      </div>
    </div>
  );
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="border rounded-lg p-3 bg-muted/20">
      <p className="text-[11px] text-muted-foreground uppercase tracking-wide">{label}</p>
      <p className="text-lg font-semibold leading-tight">{value}</p>
      {sub && <p className="text-[11px] text-muted-foreground">{sub}</p>}
    </div>
  );
}

// Barra de alocação: requests, uso e limits como % do alocável (limits pode passar de 100%).
function AllocationBar({ label, r, fmt }: { label: string; r: DeepResource; fmt: (v: number) => string }) {
  const usagePct = r.has_live ? r.usage_live_pct : r.usage_p95_pct;
  const hasUsage = r.has_live || r.has_p95;
  const rows: { name: string; pct: number; val: number; cls: string; show: boolean }[] = [
    { name: "Requests", pct: r.request_pct, val: r.requests, cls: "bg-indigo-500", show: true },
    { name: r.has_live ? "Uso (ao vivo)" : "Uso (P95)", pct: usagePct, val: r.has_live ? r.usage_live : r.usage_p95, cls: "bg-emerald-500", show: hasUsage },
    { name: "Limits", pct: r.limit_pct, val: r.limits, cls: "bg-amber-500", show: true },
  ];
  return (
    <div className="border rounded-lg p-3 space-y-2">
      <div className="flex justify-between text-sm">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground text-xs">alocável {fmt(r.allocatable)}</span>
      </div>
      {rows.filter(x => x.show).map(x => (
        <div key={x.name} className="space-y-0.5">
          <div className="flex justify-between text-xs">
            <span>{x.name}</span>
            <span className={usageColor(x.name.startsWith("Uso") ? x.pct : 0)}>{fmt(x.val)} · {pctFmt(x.pct)}</span>
          </div>
          <div className="h-2 rounded bg-muted overflow-hidden">
            <div className={`h-full ${x.cls}`} style={{ width: `${Math.min(100, x.pct)}%` }} />
          </div>
        </div>
      ))}
    </div>
  );
}

const cores = (m: number) => `${(m / 1000).toFixed(1)} cores`;
const gib = (mi: number) => `${(mi / 1024).toFixed(1)} GiB`;

function usageCell(w: DeepWorkloadRow, cpu: boolean) {
  if (w.usage_basis === "p95") return cpu ? `P95 ${fmtMillis(w.cpu_p95_millis ?? 0)}` : `P95 ${fmtMi(w.mem_p95_mi ?? 0)}`;
  if (w.usage_basis === "live") return cpu
    ? `${fmtMillis(w.cpu_usage_avg_millis)} / ${fmtMillis(w.cpu_usage_max_millis)}`
    : `${fmtMi(w.mem_usage_avg_mi)} / ${fmtMi(w.mem_usage_max_mi)}`;
  return "—";
}

function recCell(cur: number, rec: number, fmt: (v: number) => string) {
  if (!rec) return <span>{cur ? fmt(cur) : "—"}</span>;
  const down = rec < cur;
  return (
    <span className="whitespace-nowrap">
      {cur ? fmt(cur) : "—"} → <span className={down ? "text-emerald-600 dark:text-emerald-400 font-medium" : "text-orange-600 dark:text-orange-400 font-medium"}>{fmt(rec)}</span>
    </span>
  );
}

function coverageShares(c: DeepCoverage) {
  return [
    [c.reservation, "reserva"], [c.savings_plan, "Savings Plan"], [c.on_demand, "sob demanda"], [c.spot, "Spot"],
  ].filter(([v]) => (v as number) >= 0.005).map(([v, l]) => `${Math.round((v as number) * 100)}% ${l}`).join(", ");
}

function SignedBRL({ v }: { v: number }) {
  return v >= 0
    ? <span className="text-emerald-600 dark:text-emerald-400 font-medium">{fmtBRL(v)}</span>
    : <span className="text-red-600 dark:text-red-400">+{fmtBRL(-v)}</span>;
}

const VERDICT_UI: Record<string, { label: string; cls: string }> = {
  justified: { label: "justificada", cls: "border-emerald-500 text-emerald-600 dark:text-emerald-400" },
  conditional: { label: "com ressalva", cls: "border-amber-500 text-amber-600 dark:text-amber-400" },
  technical: { label: "ganho técnico", cls: "border-sky-500 text-sky-600 dark:text-sky-400" },
  not_justified: { label: "não justificada", cls: "border-red-500/60 text-red-600 dark:text-red-400" },
  unknown: { label: "sem dados", cls: "border-muted-foreground text-muted-foreground" },
};

function FitDetails({ f }: { f: DeepNodeFit }) {
  return (
    <div className="mb-2 rounded border bg-muted/20 p-2 text-[11px] space-y-0.5">
      <div className="font-medium text-xs">O que cabe em 1 node desta VM</div>
      <div>DaemonSets: <strong>{Math.round(f.ds_pods)} pods</strong> fixos, ocupando {f.ds_cpu_pct.toFixed(1)}% da CPU e {f.ds_mem_pct.toFixed(1)}% da memória do node.</div>
      <div>Pods de aplicação (pod médio do pool): cabem <strong>{f.pods_fit}</strong> por node — {f.pods_by_cpu} pela CPU · {f.pods_by_mem} pela memória · {f.pods_by_limit} pelo limite de pods (limitado por <strong>{LIMITING[f.fit_limiting] ?? f.fit_limiting}</strong>).</div>
      {f.biggest_fits > 0 || f.biggest_pct_cpu > 0 ? (
        <div>O maior pod ocupa {f.biggest_pct_cpu.toFixed(0)}% da CPU e {f.biggest_pct_mem.toFixed(0)}% da memória de um node; cabem <strong>{f.biggest_fits}</strong> iguais a ele.</div>
      ) : null}
      <div>Com os nodes recomendados, cada node recebe ~{f.pods_per_node.toFixed(0)} pods de aplicação e fica com <strong>{f.occ_cpu_pct.toFixed(0)}%</strong> da CPU · <strong>{f.occ_mem_pct.toFixed(0)}%</strong> da memória · <strong>{f.occ_pods_pct.toFixed(0)}%</strong> do limite de pods reservados.</div>
    </div>
  );
}

function SimulationBasis({ inp }: { inp: DeepSimulationInputs }) {
  const row = (label: string, d: DeepDemand) => (
    <div><span className="text-muted-foreground">{label}:</span> {cores(d.cpu_millis)} CPU · {gib(d.mem_mi)} · {d.pods} pods</div>
  );
  return (
    <div className="rounded-md border bg-muted/30 p-2.5 text-xs space-y-0.5">
      <div className="font-medium">Base do cálculo</div>
      {row("Requests atuais do pool", inp.current)}
      {row("Requests recomendados", inp.recommended)}
      <div><span className="text-muted-foreground">Limite por node:</span> <strong>{inp.max_pods_per_node} pods</strong> ({Math.round(inp.ds_pods)} já ocupados por DaemonSets) · <span className="text-muted-foreground">DaemonSets por node:</span> {cores(inp.ds_cpu_millis)} CPU · {gib(inp.ds_mem_mi)} · <span className="text-muted-foreground">mínimo:</span> {inp.min_nodes} nodes</div>
      <div>
        <span className="text-muted-foreground">Hoje no pool:</span> {inp.inventory.kinds.map(k => `${k.workloads} ${k.kind}${k.workloads > 1 ? "s" : ""} (${k.pods} pods)`).join(" · ")}
        {inp.inventory.daemonsets > 0 && <> · {inp.inventory.daemonsets} DaemonSets ({Math.round(inp.inventory.ds_pods_per_node)} pods por node)</>}
        {" "}· {inp.inventory.total_pods} pods em {inp.inventory.nodes} nodes
        {inp.inventory.avg_pods_per_node > 0 && <> (média {inp.inventory.avg_pods_per_node.toFixed(0)} pods, {inp.inventory.avg_cpu_req_pct.toFixed(0)}% de CPU e {inp.inventory.avg_mem_req_pct.toFixed(0)}% de memória reservados por node)</>}
      </div>
      {inp.inventory.biggest_pod && (
        <div><span className="text-muted-foreground">Maior pod:</span> <span className="font-mono">{inp.inventory.biggest_pod}</span> ({cores(inp.inventory.biggest_pod_cpu_millis ?? 0)} CPU · {gib(inp.inventory.biggest_pod_mem_mi ?? 0)}) · pod médio {cores(inp.inventory.avg_pod_cpu_millis)} CPU · {gib(inp.inventory.avg_pod_mem_mi)}</div>
      )}
      <div className="text-[11px] text-muted-foreground">Nodes por recurso = demanda ÷ (livre por node × {Math.round(inp.headroom * 100)}%), arredondado para cima; o maior entre CPU, memória e pods define o resultado.</div>
    </div>
  );
}

function SimulationTable({ sims, coverage }: { sims: DeepSimulation[]; coverage?: DeepCoverage }) {
  const covOK = !!coverage?.computable;
  const [open, setOpen] = useState<Set<string>>(new Set());
  const toggle = (vm: string) => setOpen(prev => {
    const next = new Set(prev);
    if (next.has(vm)) next.delete(vm); else next.add(vm);
    return next;
  });
  const best = useMemo(() => {
    const ok = sims.filter(s => !s.is_current && s.feasible && s.available && s.price_usd_hour > 0 && s.node_loss_impact_pct <= 15
      && (s.justification?.verdict === "justified" || s.justification?.verdict === "conditional"));
    // Com reserva no pool, decide pela economia efetiva no pior caso; senão, pelo custo de tabela.
    const key = (s: DeepSimulation) => (covOK && s.coverage ? -s.coverage.worst_savings_brl : s.cost_recommended_brl);
    return ok.sort((a, b) => key(a) - key(b))[0]?.vm_size;
  }, [sims, covOK]);
  const colCount = covOK ? 8 : 7;

  // Células em duas linhas (valor principal + detalhe), sem whitespace-nowrap nas longas: a tabela precisa
  // caber na largura do modal. Observações longas (notas de reserva) ficam na linha expandida.
  const nodesCell = (s: DeepSimulation, n: number, cost: number, lim: string, nodesDelta: number, savings: number, calc: DeepNodeCalc) => {
    if (!s.feasible || n === 0) return <span className="text-muted-foreground">não comporta</span>;
    return (
      <>
        <div className="font-medium">{n} nodes</div>
        <div className="text-[11px] text-muted-foreground">
          {s.price_usd_hour > 0 ? <>{fmtBRL(cost / n)}/node × {n} = <strong className="text-foreground">{fmtBRL(cost)}/mês</strong></> : "sem preço"} · {LIMITING[lim] ?? lim}
        </div>
        <div className="text-[11px] whitespace-nowrap text-muted-foreground" title="Nodes que cada recurso exigiria sozinho; o maior define o resultado (mínimo de 3 nodes ou nº de zonas)">
          <span className={lim === "cpu" ? "font-semibold text-foreground" : ""}>CPU {calc.by_cpu}</span> ·{" "}
          <span className={lim === "memory" ? "font-semibold text-foreground" : ""}>mem {calc.by_mem}</span> ·{" "}
          <span className={lim === "pods" ? "font-semibold text-foreground" : ""}>pods {calc.by_pods}</span>
        </div>
        {s.vs_current_ok && (
          <div className="text-[11px] whitespace-nowrap" title="Diferença contra a VM atual, no mesmo cenário e na mesma ocupação">
            <span className="text-muted-foreground">vs atual (pool): </span>
            {nodesDelta > 0 ? "+" : ""}{nodesDelta} nodes · <SignedBRL v={savings} />/mês
          </div>
        )}
      </>
    );
  };

  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead>VM</TableHead>
          <TableHead>Configuração</TableHead>
          <TableHead>Alocável/node</TableHead>
          <TableHead>Requests atuais<div className="text-[10px] font-normal text-muted-foreground">pool inteiro, por mês</div></TableHead>
          <TableHead>Requests recomendados{covOK && " (tabela)"}<div className="text-[10px] font-normal text-muted-foreground">pool inteiro, por mês</div></TableHead>
          {covOK ? (<>
            <TableHead>Custo efetivo<div className="text-[10px] font-normal text-muted-foreground">pool inteiro, por mês</div></TableHead>
            <TableHead>Economia efetiva<div className="text-[10px] font-normal text-muted-foreground">pool inteiro, por mês</div></TableHead>
          </>) : <TableHead title="Economia mensal da troca de VM contra a VM atual (requests recomendados, mesma ocupação)">Economia vs atual<div className="text-[10px] font-normal text-muted-foreground">pool inteiro, por mês</div></TableHead>}
          <TableHead className="w-[1%]" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {sims.map(s => {
          const notes = [
            ...(s.reserved_hint ? [s.reserved_hint.note] : []),
            ...s.notes,
            ...(covOK && s.coverage ? [s.coverage.note] : []),
          ];
          const isOpen = open.has(s.vm_size);
          const just = s.justification;
          const nDetails = notes.length + (just ? just.pros.length + just.cons.length : 0) + (s.fit ? 1 : 0);
          const rowCls = s.is_current ? "bg-muted/40" : !s.available ? "opacity-50" : s.vm_size === best ? "bg-emerald-500/5" : "";
          return (
            <Fragment key={s.vm_size}>
              <TableRow key={s.vm_size} className={`${rowCls} align-top`}>
                <TableCell>
                  <div className="font-mono whitespace-nowrap">{s.vm_size}</div>
                  {just && !s.is_current && VERDICT_UI[just.verdict] && (
                    <div className="mt-1 max-w-[16rem]">
                      <Badge variant="outline" className={`text-[10px] ${VERDICT_UI[just.verdict].cls}`}>{VERDICT_UI[just.verdict].label}</Badge>
                      <div className="text-[11px] text-muted-foreground mt-0.5 leading-tight">{just.headline}</div>
                    </div>
                  )}
                  {(s.is_current || s.vm_size === best || (s.catalog_known && !s.available) || s.reserved_hint) && (
                    <div className="flex flex-wrap gap-1 mt-1">
                      {s.is_current && <Badge variant="outline" className="text-[10px]">atual</Badge>}
                      {s.vm_size === best && <Badge className="text-[10px] bg-emerald-600">melhor</Badge>}
                      {s.catalog_known && !s.available && <Badge variant="destructive" className="text-[10px]">indisponível</Badge>}
                      {s.reserved_hint && (
                        <Badge variant="outline" className="text-[10px] border-violet-500 text-violet-600 dark:text-violet-400" title={s.reserved_hint.note}>
                          {s.reserved_hint.kind === "reservation" ? "reserva" : "Savings Plan"} na frota{s.reserved_hint.scope === "series" ? " (série)" : ""}
                        </Badge>
                      )}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <div className="whitespace-nowrap">{s.vcpu} vCPU / {s.mem_gb} GB</div>
                  <div className="text-[11px] text-muted-foreground">{s.cpu ?? "—"}</div>
                  {(!s.smt || s.ephemeral_os_disk === false) && (
                    <div className="flex flex-wrap gap-1 mt-1">
                      {!s.smt && <Badge variant="secondary" className="text-[10px]">sem SMT</Badge>}
                      {s.ephemeral_os_disk === false && <Badge variant="outline" className="text-[10px]">sem disco efêmero</Badge>}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <div className="whitespace-nowrap">{cores(s.alloc_cpu_millis)}</div>
                  <div className="whitespace-nowrap">{gib(s.alloc_mem_mi)}</div>
                  <div className="text-[11px] text-muted-foreground whitespace-nowrap" title="Livre por node depois dos DaemonSets: é a base do cálculo de nodes">
                    livre {cores(s.free_cpu_millis)} · {gib(s.free_mem_mi)} · {Math.round(s.free_pods)} pods
                  </div>
                  {s.alloc_estimated && <div className="text-[11px] text-muted-foreground">estimado</div>}
                </TableCell>
                <TableCell>{nodesCell(s, s.nodes_current_req, s.cost_current_req_brl, s.limiting_current_req, s.nodes_delta_current_req, s.savings_vs_current_req_brl, s.calc_current_req)}</TableCell>
                <TableCell>{nodesCell(s, s.nodes_recommended, s.cost_recommended_brl, s.limiting_recommended, s.nodes_delta_recommended, s.savings_vs_current_rec_brl, s.calc_recommended)}</TableCell>
                {covOK ? (<>
                  <TableCell>
                    {s.coverage ? (<>
                      <div className="whitespace-nowrap"><span className="text-muted-foreground">pior </span>{fmtBRL(s.coverage.worst_monthly_brl)}</div>
                      <div className="whitespace-nowrap"><span className="text-muted-foreground">melhor </span>{fmtBRL(s.coverage.best_monthly_brl)}</div>
                    </>) : "—"}
                  </TableCell>
                  <TableCell>
                    {s.coverage ? (<>
                      <div className="whitespace-nowrap"><span className="text-muted-foreground">pior </span><SignedBRL v={s.coverage.worst_savings_brl} /></div>
                      <div className="whitespace-nowrap"><span className="text-muted-foreground">melhor </span><SignedBRL v={s.coverage.best_savings_brl} /></div>
                    </>) : "—"}
                  </TableCell>
                </>) : (
                  <TableCell>{s.is_current ? <span className="text-muted-foreground">base</span> : !s.vs_current_ok ? "—" : <SignedBRL v={s.savings_vs_current_rec_brl} />}</TableCell>
                )}
                <TableCell>
                  {nDetails > 0 && (
                    <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]" onClick={() => toggle(s.vm_size)} title="Justificativa e observações">
                      {isOpen ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                      <span className="ml-1">{nDetails}</span>
                    </Button>
                  )}
                </TableCell>
              </TableRow>
              {isOpen && (
                <TableRow key={`${s.vm_size}-notas`} className={rowCls}>
                  <TableCell colSpan={colCount} className="pt-0">
                    {s.fit && <FitDetails f={s.fit} />}
                    {just && (just.pros.length > 0 || just.cons.length > 0) && (
                      <ul className="space-y-0.5 text-[11px] mb-1">
                        {just.pros.map(p => <li key={p} className="text-emerald-700 dark:text-emerald-400">✔ {p}</li>)}
                        {just.cons.map(c => <li key={c} className="text-amber-700 dark:text-amber-400">⚠ {c}</li>)}
                      </ul>
                    )}
                    {notes.length > 0 && (
                      <ul className="list-disc pl-5 space-y-0.5 text-[11px] text-muted-foreground">
                        {notes.map(n => <li key={n}>{n}</li>)}
                      </ul>
                    )}
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          );
        })}
      </TableBody>
    </Table>
  );
}

export function DeepAnalysisModal({ cluster, pool, onClose }: { cluster: string; pool: string; onClose: () => void }) {
  const [section, setSection] = useState<Section>("summary");
  const [filter, setFilter] = useState("");
  const [exporting, setExporting] = useState(false);

  const { data, isFetching, error, refetch } = useQuery<PoolDeepAnalysis>({
    queryKey: ["finops-deep-analysis", cluster, pool],
    queryFn: () => apiClient.getFinOpsDeepAnalysis(cluster, pool),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const exportMarkdown = async () => {
    setExporting(true);
    try {
      const md = await apiClient.getFinOpsDeepAnalysisMarkdown(cluster, pool);
      const url = URL.createObjectURL(new Blob([md], { type: "text/markdown;charset=utf-8" }));
      const a = document.createElement("a");
      const safe = (s: string) => s.replace(/[^a-zA-Z0-9._-]/g, "-");
      a.href = url;
      a.download = `deep-analysis-${safe(cluster)}-${safe(pool)}.md`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      toast.error(`Falha ao exportar: ${(e as Error).message}`);
    } finally {
      setExporting(false);
    }
  };

  const workloads = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const all = data?.workloads ?? [];
    return q ? all.filter(w => `${w.namespace}/${w.workload}`.toLowerCase().includes(q)) : all;
  }, [data, filter]);
  const hpaRows = (data?.workloads ?? []).filter(w => w.hpa);
  const throttled = (data?.workloads ?? []).filter(w => w.has_throttle && (w.throttle_p95_pct ?? 0) >= 1)
    .sort((a, b) => (b.throttle_p95_pct ?? 0) - (a.throttle_p95_pct ?? 0));

  const sections: { id: Section; label: string; count?: number }[] = [
    { id: "summary", label: "Resumo", count: data?.findings.filter(f => f.severity !== "info").length },
    { id: "allocation", label: "Alocação e nodes" },
    { id: "daemonsets", label: "DaemonSets", count: data?.daemonsets.items.length },
    { id: "workloads", label: "Workloads", count: data?.workloads.length },
    { id: "hpa", label: "HPAs e throttling", count: hpaRows.length || undefined },
    { id: "simulation", label: "Simulação de VMs", count: data?.simulation.length },
  ];

  const ov = data?.overview;
  const al = data?.allocation;
  const ds = data?.daemonsets;

  return (
    <Dialog open onOpenChange={v => !v && onClose()}>
      <DialogContent className="max-w-7xl max-h-[92vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base flex-wrap">
            <Microscope className="h-4 w-4 text-indigo-500" />
            Deep Analysis — {pool}
            <span className="text-xs font-normal text-muted-foreground">{cluster}</span>
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Button size="sm" variant="outline" className="h-7" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={`h-3.5 w-3.5 mr-1 ${isFetching ? "animate-spin" : ""}`} /> Atualizar
          </Button>
          <Button size="sm" variant="outline" className="h-7" onClick={exportMarkdown} disabled={!data || exporting}>
            {exporting ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <Download className="h-3.5 w-3.5 mr-1" />}
            Exportar Markdown
          </Button>
          {data && (
            <span className="text-muted-foreground ml-auto">
              Gerada em {new Date(data.generated_at).toLocaleString("pt-BR")}
              {data.history.available && data.history.generated_at && ` · histórico de ${new Date(data.history.generated_at).toLocaleString("pt-BR")}${data.history.window_days ? ` (${data.history.window_days}d)` : ""}`}
            </span>
          )}
        </div>

        {isFetching && !data && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground py-10 justify-center">
            <Loader2 className="h-4 w-4 animate-spin" /> Coletando nodes, pods, HPAs e métricas do pool…
          </div>
        )}
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{(error as Error).message}</AlertDescription>
          </Alert>
        )}

        {data && ov && al && ds && (
          <>
            {data.warnings.length > 0 && (
              <Alert>
                <AlertTriangle className="h-4 w-4" />
                <AlertDescription className="text-xs space-y-0.5">
                  {data.warnings.map(w => <p key={w}>{w}</p>)}
                </AlertDescription>
              </Alert>
            )}

            <div className="flex gap-1 border-b overflow-x-auto flex-shrink-0">
              {sections.map(s => (
                <button
                  key={s.id}
                  onClick={() => setSection(s.id)}
                  className={`px-3 py-1.5 text-xs whitespace-nowrap border-b-2 -mb-px ${section === s.id ? "border-indigo-500 font-medium" : "border-transparent text-muted-foreground hover:text-foreground"}`}
                >
                  {s.label}
                  {!!s.count && <Badge variant="secondary" className="ml-1 text-[10px]">{s.count}</Badge>}
                </button>
              ))}
            </div>

            {/* Só o conteúdo rola: título, controles e abas ficam fixos, e o cabeçalho das tabelas
                (sticky) fica preso no topo desta área. O wrapper overflow-auto do Table do shadcn
                viraria o contêiner do sticky, então aqui ele deixa de rolar. */}
            <div className="flex-1 min-h-0 overflow-y-auto space-y-3 [&_div.relative.w-full.overflow-auto]:overflow-visible [&_thead_th]:sticky [&_thead_th]:top-0 [&_thead_th]:z-10 [&_thead_th]:bg-muted [&_thead_th]:text-foreground [&_thead_th]:font-semibold [&_thead_th]:text-xs [&_thead_th]:uppercase [&_thead_th]:tracking-wide [&_thead_th]:border-b-2 [&_thead_th]:border-indigo-500/60 [&_thead_th]:h-10">
            {section === "summary" && (
              <div className="space-y-3">
                <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
                  <Stat label="VM" value={ov.vm_size.replace("Standard_", "")} sub={`${ov.vcpu} vCPU / ${ov.mem_gb} GB${ov.cpu ? ` · ${ov.cpu}` : ""}`} />
                  <Stat label="Nodes" value={String(ov.nodes)} sub={`${ov.priority === "spot" ? "Spot" : "Regular"}${ov.os_disk ? ` · disco ${ov.os_disk}` : ""} · ${ov.zones.length || "?"} zona(s)`} />
                  {ov.coverage ? (
                    <Stat label="Custo efetivo/mês" value={fmtBRL(ov.coverage.effective_monthly_brl)}
                      sub={`${coverageShares(ov.coverage)} · tabela ${ov.monthly_cost_brl ? fmtBRL(ov.monthly_cost_brl) : "—"}`} />
                  ) : (
                    <Stat label="Custo/mês (tabela)" value={ov.monthly_cost_brl ? fmtBRL(ov.monthly_cost_brl) : "—"} sub={ov.price_usd_hour ? `US$ ${ov.price_usd_hour.toFixed(3)}/h por node · sem dado de reserva` : "preço indisponível"} />
                  )}
                  <Stat label="CPU reservada / usada" value={`${pctFmt(al.cpu.request_pct)} / ${al.cpu.has_live ? pctFmt(al.cpu.usage_live_pct) : "—"}`} sub={`gargalo de agendamento: ${al.scheduling_bound === "cpu" ? "CPU" : "memória"}`} />
                  <Stat label="Memória reservada / usada" value={`${pctFmt(al.mem.request_pct)} / ${al.mem.has_live ? pctFmt(al.mem.usage_live_pct) : "—"}`} sub={al.real_bottleneck ? `gargalo real: ${al.real_bottleneck === "cpu" ? "CPU" : "memória"}` : "sem uso real"} />
                </div>
                {data.findings.length === 0
                  ? <p className="text-sm text-muted-foreground">Nenhum problema relevante encontrado.</p>
                  : data.findings.map(f => <FindingCard key={f.code} f={f} />)}
              </div>
            )}

            {section === "allocation" && (
              <div className="space-y-3">
                <p className={`text-sm ${al.mismatch ? "text-orange-700 dark:text-orange-300" : ""}`}>{al.explanation}</p>
                <div className="grid md:grid-cols-2 gap-3">
                  <AllocationBar label="CPU" r={al.cpu} fmt={cores} />
                  <AllocationBar label="Memória" r={al.mem} fmt={gib} />
                </div>
                {al.cpu.has_live && (
                  <p className="text-xs text-muted-foreground">
                    Nodes com memória ≥ 90%: <strong>{al.nodes_mem_above_90}</strong> (≥ 95%: <strong>{al.nodes_mem_above_95}</strong>) · CPU ≥ 90%: {al.nodes_cpu_above_90}
                  </p>
                )}
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Node</TableHead><TableHead>Zona</TableHead><TableHead>Pods</TableHead>
                      <TableHead>CPU req</TableHead><TableHead>CPU uso</TableHead><TableHead>Mem req</TableHead>
                      <TableHead>Mem uso</TableHead><TableHead>Mem limits</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.nodes.map(n => (
                      <TableRow key={n.name}>
                        <TableCell className="font-mono text-xs">{n.name}</TableCell>
                        <TableCell className="text-xs">{n.zone || "—"}</TableCell>
                        <TableCell className="text-xs">{n.pods}</TableCell>
                        <TableCell className="text-xs">{pctFmt(n.cpu_req_pct)}</TableCell>
                        <TableCell className={`text-xs ${usageColor(n.cpu_usage_pct)}`}>{n.has_usage ? pctFmt(n.cpu_usage_pct) : "—"}</TableCell>
                        <TableCell className="text-xs">{pctFmt(n.mem_req_pct)}</TableCell>
                        <TableCell className={`text-xs ${usageColor(n.mem_usage_pct)}`}>{n.has_usage ? pctFmt(n.mem_usage_pct) : "—"}</TableCell>
                        <TableCell className={`text-xs ${n.mem_lim_pct > 150 ? "text-orange-600 dark:text-orange-400" : ""}`}>{pctFmt(n.mem_lim_pct)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}

            {section === "daemonsets" && (
              <div className="space-y-3">
                <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                  <Stat label="Memória por node" value={fmtMi(Math.max(ds.per_node_mem_usage_mi, ds.per_node_mem_req_mi))} sub={`${pctFmt(ds.mem_overhead_pct)} do alocável · req ${fmtMi(ds.per_node_mem_req_mi)}`} />
                  <Stat label="CPU por node" value={fmtMillis(Math.max(ds.per_node_cpu_usage_millis, ds.per_node_cpu_req_millis))} sub={`${pctFmt(ds.cpu_overhead_pct)} do alocável · req ${fmtMillis(ds.per_node_cpu_req_millis)}`} />
                  <Stat label="Pods por node" value={ds.per_node_pods.toFixed(1)} sub={`de ${ov.max_pods} (maxPods)`} />
                  <Stat label="Custo fixo/mês" value={ds.monthly_cost_brl ? fmtBRL(ds.monthly_cost_brl) : "—"} sub="fatia do pool ocupada por DaemonSets" />
                </div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>DaemonSet</TableHead><TableHead>CPU req</TableHead><TableHead>CPU uso méd/máx</TableHead>
                      <TableHead>Mem req</TableHead><TableHead>Mem uso méd/máx</TableHead><TableHead>Observações</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {ds.items.map(d => (
                      <TableRow key={`${d.namespace}/${d.name}`}>
                        <TableCell className="text-xs"><span className="text-muted-foreground">{d.namespace}/</span>{d.name}</TableCell>
                        <TableCell className="text-xs">{d.cpu_req_millis ? fmtMillis(d.cpu_req_millis) : "—"}</TableCell>
                        <TableCell className="text-xs">{d.has_usage ? `${fmtMillis(d.cpu_usage_avg_millis)} / ${fmtMillis(d.cpu_usage_max_millis)}` : "—"}</TableCell>
                        <TableCell className="text-xs">{d.mem_req_mi ? fmtMi(d.mem_req_mi) : "—"}</TableCell>
                        <TableCell className="text-xs">{d.has_usage ? `${fmtMi(d.mem_usage_avg_mi)} / ${fmtMi(d.mem_usage_max_mi)}` : "—"}</TableCell>
                        <TableCell><FlagBadges flags={d.flags} /></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}

            {section === "workloads" && (
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Input placeholder="Filtrar workload…" value={filter} onChange={e => setFilter(e.target.value)} className="h-8 max-w-xs text-xs" />
                  <span className="text-xs text-muted-foreground">
                    Valores por pod. Uso = P95 histórico ({data.history.covered}/{data.history.total} com histórico), senão o uso ao vivo médio/máximo.
                  </span>
                </div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Workload</TableHead><TableHead>Pods</TableHead><TableHead>CPU req → rec</TableHead>
                      <TableHead>CPU uso</TableHead><TableHead>Mem req → rec</TableHead><TableHead>Mem uso</TableHead>
                      <TableHead>Mem limit → rec</TableHead><TableHead>% CPU req</TableHead><TableHead>Observações</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {workloads.map(w => (
                      <TableRow key={`${w.namespace}/${w.workload}`}>
                        <TableCell className="text-xs"><span className="text-muted-foreground">{w.namespace}/</span>{w.workload}</TableCell>
                        <TableCell className="text-xs">{w.pods_on_pool}</TableCell>
                        <TableCell className="text-xs">{recCell(w.cpu_req_millis, w.cpu_rec_millis, fmtMillis)}</TableCell>
                        <TableCell className="text-xs whitespace-nowrap">{usageCell(w, true)}</TableCell>
                        <TableCell className="text-xs">{recCell(w.mem_req_mi, w.mem_rec_mi, fmtMi)}</TableCell>
                        <TableCell className={`text-xs whitespace-nowrap ${w.mem_usage_vs_request_pct > 100 ? "text-red-600 dark:text-red-400 font-medium" : ""}`}>
                          {usageCell(w, false)}{w.usage_basis && ` (${pctFmt(w.mem_usage_vs_request_pct)})`}
                        </TableCell>
                        <TableCell className="text-xs">{recCell(w.mem_lim_mi, w.mem_limit_rec_mi, fmtMi)}</TableCell>
                        <TableCell className="text-xs">{w.cpu_request_share_pct.toFixed(1)}%</TableCell>
                        <TableCell><FlagBadges flags={w.flags} /></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}

            {section === "hpa" && (
              <div className="space-y-4">
                <section className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">HPAs</h4>
                  {hpaRows.length === 0 ? <p className="text-xs text-muted-foreground">Nenhum workload do pool tem HPA.</p> : (
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Workload</TableHead><TableHead>Métricas (alvo)</TableHead><TableHead>Min / Max / Atual</TableHead>
                          <TableHead>Estado</TableHead><TableHead>Histórico</TableHead><TableHead>Projeção com o request recomendado</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {hpaRows.map(w => {
                          const h = w.hpa!;
                          const state = { fixed: "fixo", pinned_min: "preso no mínimo", pinned_max: "preso no máximo", scaling: "escalando" }[h.state];
                          return (
                            <TableRow key={`${w.namespace}/${w.workload}`}>
                              <TableCell className="text-xs">{w.workload}</TableCell>
                              <TableCell className="text-xs">
                                {h.metrics.map(m => `${m.name === "memory" ? "memória" : m.name}${m.target_utilization ? ` ${m.target_utilization}%` : m.target_value ? ` ${m.target_value}` : ""}`).join(", ") || "—"}
                              </TableCell>
                              <TableCell className="text-xs">{h.min} / {h.max} / {h.current}</TableCell>
                              <TableCell><Badge variant={h.state === "pinned_max" ? "destructive" : "secondary"} className="text-[10px]">{state}</Badge></TableCell>
                              <TableCell className="text-xs text-muted-foreground">
                                {h.avg_replicas || h.scale_events ? `méd ${Math.round(h.avg_replicas ?? 0)}, ${h.min_observed ?? "?"}–${h.max_observed ?? "?"}, ${h.scale_events ?? 0} eventos` : "—"}
                              </TableCell>
                              <TableCell className="text-xs">
                                {h.projections.length === 0 ? "—" : h.projections.map(p => (
                                  <div key={p.resource} className={p.adjusted_for_hpa ? "text-orange-600 dark:text-orange-400" : ""}>
                                    {p.resource === "memory" ? "memória" : "CPU"}: {pctFmt(p.current_pct)} → {pctFmt(p.projected_pct)} (alvo {p.target}%)
                                    {p.adjusted_for_hpa && ` · ajustado (o puro daria ${pctFmt(p.plain_projected_pct)})`}
                                  </div>
                                ))}
                              </TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  )}
                </section>
                <section className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
                    Throttling de CPU {data.throttle_window_days > 0 && `(P95 em ${data.throttle_window_days} dias)`}
                  </h4>
                  {throttled.length === 0 ? (
                    <p className="text-xs text-muted-foreground">
                      {data.throttle_window_days > 0 ? "Nenhum workload com throttling na janela." : "Throttling não coletado (Prometheus indisponível)."}
                    </p>
                  ) : (
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Workload</TableHead><TableHead>CPU limit</TableHead><TableHead>Throttling P95</TableHead>
                          <TableHead>Atual</TableHead><TableHead>Limit sugerido</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {throttled.map(w => (
                          <TableRow key={`${w.namespace}/${w.workload}`}>
                            <TableCell className="text-xs">{w.workload}</TableCell>
                            <TableCell className="text-xs">{w.cpu_lim_millis ? fmtMillis(w.cpu_lim_millis) : "sem limit"}</TableCell>
                            <TableCell className={`text-xs ${(w.throttle_p95_pct ?? 0) >= 10 ? "text-red-600 dark:text-red-400 font-medium" : ""}`}>{pctFmt(w.throttle_p95_pct ?? 0)}</TableCell>
                            <TableCell className="text-xs">{pctFmt(w.throttle_current_pct ?? 0)}</TableCell>
                            <TableCell className="text-xs">{w.cpu_limit_rec_millis ? fmtMillis(w.cpu_limit_rec_millis) : "—"}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  )}
                </section>
              </div>
            )}

            {section === "simulation" && (
              <div className="space-y-2">
                <p className="text-xs text-muted-foreground">
                  Nodes necessários por SKU com {Math.round(data.headroom * 100)}% de ocupação dos nodes (o que sobra depois dos DaemonSets), com no mínimo 3 nodes.
                  <strong> Requests atuais</strong> = só trocar a VM; <strong>recomendados</strong> = depois do ajuste de requests. "<strong>vs atual</strong>" compara cada SKU com a VM atual do pool, no mesmo cenário e na mesma ocupação (a linha "atual" é a base).
                  Alocável "(est.)" vem da fórmula de reserva da AKS. Preços de tabela sob demanda.
                  {data.sku_catalog_status === "loading" && " Catálogo de SKUs da região ainda carregando: disponibilidade não verificada."}
                </p>
                {ov.coverage?.computable && (
                  <Alert>
                    <AlertDescription className="text-xs">
                      <strong>Reserva no pool:</strong> custo efetivo hoje de {fmtBRL(ov.coverage.effective_monthly_brl)}/mês ({coverageShares(ov.coverage)}, Cost Management, {ov.coverage.window_days} dias).
                      A reserva cobre ~{ov.coverage.reserved_nodes.toFixed(1)} de {ov.nodes} nodes, com desconto de ~{Math.round(ov.coverage.table_discount_pct)}% sobre a tabela.
                      O custo efetivo de cada SKU vai do <strong>pior caso</strong> (a reserva atual segue paga e ociosa) ao <strong>melhor caso</strong> (a reserva é reaproveitada por outro pool/cluster ou trocada); a tabela é ordenada pelo pior caso.
                    </AlertDescription>
                  </Alert>
                )}
                {ov.coverage && !ov.coverage.computable && ov.coverage.note && (
                  <Alert><AlertDescription className="text-xs"><strong>Reserva/Savings Plan:</strong> {ov.coverage.note}</AlertDescription></Alert>
                )}
                <p className="text-xs rounded-md border border-sky-500/30 bg-sky-500/5 px-2.5 py-1.5">
                  <strong>Todos os valores em R$ são do pool inteiro, por mês</strong> (nodes × preço de um node × 730 h), não de um node. Em cada célula aparece o preço de <em>um</em> node e a multiplicação; "vs atual (pool)" é a diferença do pool inteiro contra o pool na VM atual.
                </p>
                {data.simulation_inputs && <SimulationBasis inp={data.simulation_inputs} />}
                <SimulationTable sims={data.simulation} coverage={ov.coverage} />
              </div>
            )}
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
