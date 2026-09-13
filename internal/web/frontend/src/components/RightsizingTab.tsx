import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Loader2, RefreshCw, Server, Search, Sparkles } from "lucide-react";
import ResourceGauge from "@/components/ResourceGauge";
import { fmtBRL, fmtMillis, fmtMi, VerdictBadge, KubectlBlock, SummaryCard } from "@/lib/finopsFormat";
import { DollarSign, TrendingDown, Layers } from "lucide-react";

// ─── Tipos (shape de GET/POST /api/v1/finops/rightsizing, ver
//      internal/web/handlers/finops_rightsizing.go / internal/storage/finops_rightsizing_store.go) ──

interface WorkloadRecommendation {
  namespace: string;
  workload: string;
  node_pool?: string;
  node_name?: string;
  pods: number;
  cpu_request_millis: number;
  mem_request_mi: number;
  cpu_limit_millis?: number;
  mem_limit_mi?: number;
  cpu_p95_millis?: number;
  mem_p95_mi?: number;
  // Pico histórico (top) — max_over_time (Prometheus) ou "max" (Dynatrace), distinto de P95.
  cpu_max_millis?: number;
  mem_max_mi?: number;
  // Uso "current" (live, via metrics-server) — snapshot do instante do scan, distinto de P95
  // (agregação histórica de uma janela de dias). Ausente quando o metrics-server não está
  // disponível no cluster.
  cpu_current_millis?: number;
  mem_current_mi?: number;
  cpu_recommended_millis?: number;
  mem_recommended_mi?: number;
  cpu_limit_recommended_millis?: number;
  mem_limit_recommended_mi?: number;
  verdict: string;
  waste_brl?: number;
  metrics_source?: string;
  window_days: number;
  generated_at: string;
}

// Uso current (live)/top (pico histórico) de um node único onde workloads rodam — ver
// finops.NodeUsage/ComputeNodeUsage (internal/finops/live_metrics.go).
interface NodeUsageInfo {
  node_name: string;
  node_pool?: string;
  cpu_cap_millis?: number;
  mem_cap_mi?: number;
  cpu_current_pct?: number;
  mem_current_pct?: number;
  cpu_top_pct?: number;
  mem_top_pct?: number;
  metrics_available: boolean;
  metrics_error?: string;
}

interface VMAlternative {
  vm_size: string;
  cpu_cores: number;
  memory_gb: number;
  mem_per_cpu_gb: number;
  price_usd_hour: number;
  price_source: string;
  cost_delta_pct: number;
  monthly_savings_brl: number;
  reason: string;
  verdict: "recommended" | "consider" | "cheaper";
}

interface NodePoolTierSuggestion {
  node_pool: string;
  current_sku: string;
  cpu_util_pct: number;
  mem_util_pct: number;
  workload_count: number;
  alternatives: VMAlternative[];
  generated_at: string;
}

interface RightsizingResponse {
  cluster: string;
  scanned: boolean;
  last_scanned_at?: string;
  workloads?: WorkloadRecommendation[];
  node_pools?: NodePoolTierSuggestion[];
  nodes?: NodeUsageInfo[];
}

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

function workloadCardId(w: WorkloadRecommendation) {
  return `rightsizing-wl-${w.namespace}-${w.workload}`;
}

function timeAgo(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const mins = Math.round(diffMs / 60000);
  if (mins < 1) return "agora mesmo";
  if (mins < 60) return `${mins}min atrás`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h atrás`;
  const days = Math.round(hours / 24);
  return `${days}d atrás`;
}

/** Comando kubectl cobrindo requests E limits juntos (diferente do comando da aba Oportunidades,
 *  que hoje só mexe em --requests). Só inclui um recurso quando há delta real (>15%) — mesmo
 *  limiar já usado no resto do FinOps. */
function buildResizeCommand(w: WorkloadRecommendation): string | null {
  const reqParts: string[] = [];
  const limParts: string[] = [];

  const cpuDelta = w.cpu_recommended_millis && w.cpu_request_millis
    ? Math.abs(w.cpu_recommended_millis - w.cpu_request_millis) / w.cpu_request_millis
    : 0;
  const memDelta = w.mem_recommended_mi && w.mem_request_mi
    ? Math.abs(w.mem_recommended_mi - w.mem_request_mi) / w.mem_request_mi
    : 0;

  if (cpuDelta > 0.15 && w.cpu_recommended_millis) reqParts.push(`cpu=${Math.round(w.cpu_recommended_millis)}m`);
  if (memDelta > 0.15 && w.mem_recommended_mi) reqParts.push(`memory=${Math.round(w.mem_recommended_mi)}Mi`);
  if (w.cpu_limit_recommended_millis) limParts.push(`cpu=${Math.round(w.cpu_limit_recommended_millis)}m`);
  if (w.mem_limit_recommended_mi) limParts.push(`memory=${Math.round(w.mem_limit_recommended_mi)}Mi`);

  if (reqParts.length === 0 && limParts.length === 0) return null;

  let cmd = `kubectl set resources deployment ${w.workload} -n ${w.namespace}`;
  if (reqParts.length > 0) cmd += ` --requests=${reqParts.join(",")}`;
  if (limParts.length > 0) cmd += ` --limits=${limParts.join(",")}`;
  return cmd;
}

/** Cor por faixa de %, mesmos limiares de ResourceGauge.getColorForPercent (consistência visual
 *  entre o gauge e os badges textuais de node). */
function pctColorClass(pct: number): string {
  if (pct < 50) return "text-green-600 dark:text-green-400";
  if (pct < 70) return "text-amber-600 dark:text-amber-400";
  if (pct < 85) return "text-orange-600 dark:text-orange-400";
  return "text-red-600 dark:text-red-400";
}

/** Badge compacto "este workload roda no node X, que está em Y% agora (pico Z%)" — correlaciona
 *  FinOpsWorkload.NodeName com o NodeUsage computado 1x por node (ver ComputeNodeUsage). */
function NodeUsageBadge({ node }: { node?: NodeUsageInfo }) {
  if (!node) return null;
  if (!node.metrics_available) {
    return (
      <p className="text-[10px] text-muted-foreground flex items-center gap-1" title={node.metrics_error}>
        <Server className="h-3 w-3" /> Node {node.node_name} · ⚠ métricas ao vivo indisponíveis
      </p>
    );
  }
  const cpuNow = node.cpu_current_pct ?? 0;
  const memNow = node.mem_current_pct ?? 0;
  const cpuTop = node.cpu_top_pct ?? 0;
  const memTop = node.mem_top_pct ?? 0;
  return (
    <p className="text-[10px] text-muted-foreground flex flex-wrap items-center gap-x-1.5" title={`Node ${node.node_name}`}>
      <Server className="h-3 w-3" /> Node <span className="font-mono">{node.node_name}</span>
      <span>
        CPU agora <span className={`font-semibold ${pctColorClass(cpuNow)}`}>{cpuNow.toFixed(0)}%</span>
        {cpuTop > 0 && <> (pico <span className={`font-semibold ${pctColorClass(cpuTop)}`}>{cpuTop.toFixed(0)}%</span>)</>}
      </span>
      <span>
        Mem agora <span className={`font-semibold ${pctColorClass(memNow)}`}>{memNow.toFixed(0)}%</span>
        {memTop > 0 && <> (pico <span className={`font-semibold ${pctColorClass(memTop)}`}>{memTop.toFixed(0)}%</span>)</>}
      </span>
    </p>
  );
}

const tierVerdictCfg: Record<string, { label: string; cls: string }> = {
  recommended: { label: "Recomendado", cls: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400" },
  consider: { label: "Considerar", cls: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400" },
  cheaper: { label: "Mais barato", cls: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400" },
};

function WorkloadCard({ w, node }: { w: WorkloadRecommendation; node?: NodeUsageInfo }) {
  const resizeCmd = buildResizeCommand(w);
  // Gauge mostra o "current" de verdade (live, metrics-server) quando disponível — fallback pro
  // P95 histórico quando o metrics-server não está acessível (mesmo padrão de graceful
  // degradation do resto da app). Título reflete qual dos dois está sendo exibido.
  const hasLiveCPU = (w.cpu_current_millis ?? 0) > 0;
  const hasLiveMem = (w.mem_current_mi ?? 0) > 0;
  return (
    <Card id={workloadCardId(w)} className="scroll-mt-4">
      <CardContent className="p-4 space-y-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <p className="text-[10px] text-muted-foreground truncate">{w.namespace}</p>
            <p className="font-medium truncate">{w.workload}</p>
            {w.node_pool && (
              <p className="text-[10px] text-muted-foreground mt-0.5">
                <Server className="h-3 w-3 inline mr-1" />
                {w.node_pool} · {w.pods} pod(s)
              </p>
            )}
          </div>
          <div className="flex flex-col items-end gap-1 shrink-0">
            <VerdictBadge verdict={w.verdict} />
            {(w.waste_brl ?? 0) > 0 && (
              <span className="text-xs font-semibold text-red-500">-{fmtBRL(w.waste_brl!)}/mês</span>
            )}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4 py-2">
          <ResourceGauge
            title={hasLiveCPU ? "CPU (agora)" : "CPU (P95)"}
            current={hasLiveCPU ? w.cpu_current_millis! : (w.cpu_p95_millis ?? 0)}
            request={w.cpu_request_millis}
            limit={w.cpu_limit_millis ?? 0}
            recommended={w.cpu_recommended_millis}
            unit="millicores"
            formatValue={fmtMillis}
          />
          <ResourceGauge
            title={hasLiveMem ? "Memória (agora)" : "Memória (P95)"}
            current={hasLiveMem ? w.mem_current_mi! : (w.mem_p95_mi ?? 0)}
            request={w.mem_request_mi}
            limit={w.mem_limit_mi ?? 0}
            recommended={w.mem_recommended_mi}
            unit="Mi"
            formatValue={fmtMi}
          />
        </div>

        {/* Current (live) + P95 + Top (pico histórico) lado a lado — a queixa original era que só
         *  existia P95, sem "current" de verdade nem "top" de CPU. */}
        <div className="grid grid-cols-2 gap-4 text-[10px] text-muted-foreground -mt-1">
          <div className="space-x-2">
            {hasLiveCPU && <span>agora: <span className="font-mono">{fmtMillis(w.cpu_current_millis!)}</span></span>}
            {(w.cpu_p95_millis ?? 0) > 0 && <span>P95: <span className="font-mono">{fmtMillis(w.cpu_p95_millis!)}</span></span>}
            {(w.cpu_max_millis ?? 0) > 0 && <span>top: <span className="font-mono">{fmtMillis(w.cpu_max_millis!)}</span></span>}
          </div>
          <div className="space-x-2">
            {hasLiveMem && <span>agora: <span className="font-mono">{fmtMi(w.mem_current_mi!)}</span></span>}
            {(w.mem_p95_mi ?? 0) > 0 && <span>P95: <span className="font-mono">{fmtMi(w.mem_p95_mi!)}</span></span>}
            {(w.mem_max_mi ?? 0) > 0 && <span>top: <span className="font-mono">{fmtMi(w.mem_max_mi!)}</span></span>}
          </div>
        </div>

        <NodeUsageBadge node={node} />

        <div className="text-[11px] text-muted-foreground space-y-0.5">
          {w.cpu_recommended_millis ? (
            <p>
              CPU — request: <span className="font-mono">{fmtMillis(w.cpu_request_millis)}</span>
              {w.cpu_limit_millis ? <> · limit: <span className="font-mono">{fmtMillis(w.cpu_limit_millis)}</span></> : null}
              {" → "}
              recomendado: <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMillis(w.cpu_recommended_millis)}</span>
              {w.cpu_limit_recommended_millis ? <> / <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMillis(w.cpu_limit_recommended_millis)}</span></> : null}
            </p>
          ) : null}
          {w.mem_recommended_mi ? (
            <p>
              Mem — request: <span className="font-mono">{fmtMi(w.mem_request_mi)}</span>
              {w.mem_limit_mi ? <> · limit: <span className="font-mono">{fmtMi(w.mem_limit_mi)}</span></> : null}
              {" → "}
              recomendado: <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMi(w.mem_recommended_mi)}</span>
              {w.mem_limit_recommended_mi ? <> / <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMi(w.mem_limit_recommended_mi)}</span></> : null}
            </p>
          ) : null}
          {w.metrics_source && (
            <p className="text-[10px]">
              fonte: <span className="font-mono">{w.metrics_source === "dynatrace" ? "Dynatrace" : "Prometheus"}</span> · janela {w.window_days}d
            </p>
          )}
        </div>

        {resizeCmd && (
          <div>
            <p className="text-[10px] text-muted-foreground mb-1">Ação recomendada (request + limit):</p>
            <KubectlBlock cmd={resizeCmd} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function NodePoolTierCard({ pool, relatedWorkloads }: { pool: NodePoolTierSuggestion; relatedWorkloads: WorkloadRecommendation[] }) {
  return (
    <Card>
      <CardContent className="p-4 space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div>
            <p className="font-medium">{pool.node_pool}</p>
            <p className="text-[11px] text-muted-foreground font-mono">{pool.current_sku}</p>
          </div>
          <div className="text-right text-[11px] text-muted-foreground">
            <p>CPU: {pool.cpu_util_pct.toFixed(0)}%</p>
            <p>Mem: {pool.mem_util_pct.toFixed(0)}%</p>
          </div>
        </div>

        {relatedWorkloads.length > 0 && (
          <p className="text-[10px] text-muted-foreground">
            Baseado no uso real de {pool.workload_count} workload(s):{" "}
            {relatedWorkloads.map((w, i) => (
              <span key={`${w.namespace}/${w.workload}`}>
                {i > 0 && ", "}
                <button
                  type="button"
                  className="underline hover:text-foreground"
                  onClick={() => document.getElementById(workloadCardId(w))?.scrollIntoView({ behavior: "smooth", block: "center" })}
                >
                  {w.workload}
                </button>
              </span>
            ))}
          </p>
        )}

        {pool.alternatives.length === 0 ? (
          <p className="text-[11px] text-muted-foreground">Nenhuma alternativa de tier identificada para o padrão de uso atual.</p>
        ) : (
          <div className="grid gap-2">
            {pool.alternatives.map((alt) => {
              const cfg = tierVerdictCfg[alt.verdict] ?? tierVerdictCfg.consider;
              return (
                <div key={alt.vm_size} className="border rounded-lg p-2.5 space-y-1 bg-muted/20">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs font-mono font-semibold">{alt.vm_size}</span>
                    <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${cfg.cls}`}>{cfg.label}</span>
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    {alt.cpu_cores} vCPU · {alt.memory_gb} GB RAM · {alt.mem_per_cpu_gb} GB/vCPU · ${alt.price_usd_hour.toFixed(3)}/hora ({alt.cost_delta_pct > 0 ? "+" : ""}{alt.cost_delta_pct}%)
                  </p>
                  {alt.monthly_savings_brl > 10 && (
                    <p className="text-[11px] font-semibold text-green-600">-{fmtBRL(alt.monthly_savings_brl)}/mês na frota do pool</p>
                  )}
                  {alt.monthly_savings_brl < -10 && (
                    <p className="text-[11px] font-semibold text-orange-600">+{fmtBRL(Math.abs(alt.monthly_savings_brl))}/mês na frota do pool</p>
                  )}
                  <p className="text-[11px] italic text-muted-foreground">{alt.reason}</p>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function RightsizingTab({ cluster }: { cluster: string }) {
  const queryClient = useQueryClient();
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState<string | null>(null);
  const [namespaceFilter, setNamespaceFilter] = useState<string>("all");
  const [verdictFilter, setVerdictFilter] = useState<string>("all");
  const [search, setSearch] = useState("");

  const { data, isLoading, error } = useQuery<RightsizingResponse>({
    queryKey: ["finops-rightsizing", cluster],
    queryFn: async () => {
      const r = await fetch(`/api/v1/finops/rightsizing?cluster=${encodeURIComponent(cluster)}`, {
        headers: authHeaders(),
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 60 * 1000,
  });

  const runScan = async () => {
    setScanning(true);
    setScanError(null);
    try {
      const r = await fetch(`/api/v1/finops/rightsizing/scan?cluster=${encodeURIComponent(cluster)}`, {
        method: "POST",
        headers: authHeaders(),
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      const fresh: RightsizingResponse = await r.json();
      queryClient.setQueryData(["finops-rightsizing", cluster], fresh);
    } catch (e) {
      setScanError(e instanceof Error ? e.message : "Falha ao analisar");
    } finally {
      setScanning(false);
    }
  };

  const workloads = useMemo(() => {
    const list = data?.workloads ?? [];
    return [...list].sort((a, b) => (b.waste_brl ?? 0) - (a.waste_brl ?? 0));
  }, [data]);

  const namespaces = useMemo(() => Array.from(new Set(workloads.map((w) => w.namespace))).sort(), [workloads]);

  const nodesByName = useMemo(() => {
    const m = new Map<string, NodeUsageInfo>();
    for (const n of data?.nodes ?? []) m.set(n.node_name, n);
    return m;
  }, [data]);

  const filtered = useMemo(() => {
    return workloads.filter((w) => {
      if (namespaceFilter !== "all" && w.namespace !== namespaceFilter) return false;
      if (verdictFilter !== "all" && w.verdict !== verdictFilter) return false;
      if (search && !w.workload.toLowerCase().includes(search.toLowerCase())) return false;
      return true;
    });
  }, [workloads, namespaceFilter, verdictFilter, search]);

  const totalWaste = workloads.reduce((sum, w) => sum + (w.waste_brl ?? 0), 0);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20 text-muted-foreground gap-2">
        <Loader2 className="h-5 w-5 animate-spin" /> Carregando análise…
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error instanceof Error ? error.message : "Falha ao carregar rightsizing"}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header: última análise + botão de reanalisar */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold flex items-center gap-1.5">
            <Sparkles className="h-4 w-4 text-purple-500" />
            Rightsizing — Request/Limit e Tier de VM
          </h3>
          <p className="text-xs text-muted-foreground">
            {data?.scanned && data.last_scanned_at
              ? `Última análise: ${timeAgo(data.last_scanned_at)}`
              : "Nunca analisado"}
          </p>
        </div>
        <Button size="sm" onClick={runScan} disabled={scanning}>
          {scanning ? <Loader2 className="h-4 w-4 mr-1.5 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-1.5" />}
          {data?.scanned ? "Reanalisar agora" : "Analisar agora"}
        </Button>
      </div>

      {scanError && (
        <Alert variant="destructive">
          <AlertDescription>{scanError}</AlertDescription>
        </Alert>
      )}

      {!data?.scanned && !scanning && (
        <Card>
          <CardContent className="p-10 text-center space-y-2">
            <Sparkles className="h-8 w-8 mx-auto text-muted-foreground" />
            <p className="font-medium">Este cluster ainda não foi analisado</p>
            <p className="text-sm text-muted-foreground">
              Clique em "Analisar agora" para calcular sugestões de request/limit por workload e de
              tier de VM por node pool, baseadas em uso real histórico (Prometheus/Dynatrace).
              A análise fica salva — reabrir esta aba não escaneia de novo sozinho.
            </p>
          </CardContent>
        </Card>
      )}

      {data?.scanned && (
        <>
          {/* KPIs */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <SummaryCard icon={DollarSign} label="Desperdício Total" value={fmtBRL(totalWaste)} color="text-red-500" />
            <SummaryCard icon={TrendingDown} label="Workloads Analisados" value={String(workloads.length)} color="text-blue-500" />
            <SummaryCard icon={Layers} label="Node Pools" value={String(data.node_pools?.length ?? 0)} color="text-purple-500" />
            <SummaryCard
              icon={Sparkles}
              label="Com Oportunidade"
              value={String(workloads.filter((w) => (w.waste_brl ?? 0) > 0).length)}
              color="text-amber-500"
            />
          </div>

          {/* Seção Node Pools — tier de VM */}
          {(data.node_pools?.length ?? 0) > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-muted-foreground mb-2">Tier de VM por Node Pool</h4>
              <div className="grid md:grid-cols-2 gap-3">
                {data.node_pools!.map((pool) => (
                  <NodePoolTierCard
                    key={pool.node_pool}
                    pool={pool}
                    relatedWorkloads={workloads.filter((w) => w.node_pool === pool.node_pool)}
                  />
                ))}
              </div>
            </div>
          )}

          {/* Filtros */}
          <div className="flex items-center gap-2 flex-wrap">
            <div className="relative w-48">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
              <Input
                placeholder="Buscar workload…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-7 h-8 text-xs"
              />
            </div>
            <Select value={namespaceFilter} onValueChange={setNamespaceFilter}>
              <SelectTrigger className="w-40 h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos namespaces</SelectItem>
                {namespaces.map((ns) => <SelectItem key={ns} value={ns}>{ns}</SelectItem>)}
              </SelectContent>
            </Select>
            <Select value={verdictFilter} onValueChange={setVerdictFilter}>
              <SelectTrigger className="w-40 h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos veredictos</SelectItem>
                <SelectItem value="superprovisioned">Desperdício</SelectItem>
                <SelectItem value="oom_risk">Risco OOM</SelectItem>
                <SelectItem value="ok">Eficiente</SelectItem>
                <SelectItem value="no_request">Sem Request</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Seção Workloads */}
          {filtered.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">Nenhum workload corresponde aos filtros.</p>
          ) : (
            <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-3">
              {filtered.map((w) => (
                <WorkloadCard key={`${w.namespace}/${w.workload}`} w={w} node={w.node_name ? nodesByName.get(w.node_name) : undefined} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

export default RightsizingTab;
