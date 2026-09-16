import { useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Loader2, RefreshCw, X, Server, Search, Sparkles, Boxes, Gauge, Info, AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import ResourceGauge from "@/components/ResourceGauge";
import { fmtBRL, fmtMillis, fmtMi, VerdictBadge, KubectlBlock, SummaryCard } from "@/lib/finopsFormat";
import { DollarSign, TrendingDown, Layers } from "lucide-react";
import { ComposedChart, Line, XAxis, YAxis, ReferenceLine, ReferenceDot } from "recharts";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { useRightsizingReport } from "@/hooks/useRightsizingReport";

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
  // Data/hora em que o pico acima foi observado — sem isso, um "top" sozinho não diz se é de
  // ontem ou de 29 dias atrás. Ausente quando a fonte é Dynatrace (sem timestamp por ponto) ou
  // quando não há amostra no período.
  cpu_max_at?: string;
  mem_max_at?: string;
  // Uso "current" (live, via metrics-server) — snapshot do instante do scan, distinto de P95
  // (agregação histórica de uma janela de dias). Ausente quando o metrics-server não está
  // disponível no cluster.
  cpu_current_millis?: number;
  mem_current_mi?: number;
  // CreationTimestamp do pod Running mais antigo deste workload no momento do scan —
  // contextualiza "tempo de vida sem reiniciar" pra interpretar um pico histórico (um pico de
  // 20d atrás não diz muito se todos os pods de hoje têm só 2h de vida).
  oldest_pod_started_at?: string;
  cpu_recommended_millis?: number;
  mem_recommended_mi?: number;
  cpu_limit_recommended_millis?: number;
  mem_limit_recommended_mi?: number;
  // Réplicas — configurado (min/max/current, HPA ou fixo) + observado no período (via
  // Prometheus). hpa_max === hpa_min é o sinal de "sem HPA de verdade" (réplicas fixas) — ver
  // allocateCosts em calculator.go, que preenche os 3 com Pods quando não há HPA.
  hpa_min?: number;
  hpa_max?: number;
  hpa_current?: number;
  hpa_avg_replicas?: number;
  hpa_max_observed?: number;
  hpa_min_observed?: number;
  hpa_scale_events?: number;
  hpa_never_scaled?: boolean;
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
  // Data/hora em que CPUTopPct/MemTopPct foram observados — crítico pra nodes efêmeros (spot),
  // que podem ser evictados e recriados a qualquer momento: sem isso não dá pra saber se o
  // "top" é de agora ou de um momento qualquer nos últimos N dias.
  cpu_top_at?: string;
  mem_top_at?: string;
  // CreationTimestamp do node (objeto K8s Node) — "desde quando ele existe". Nodes spot
  // costumam ser recriados com frequência; um node muito jovem explica por que o "top" pode
  // estar ausente/limitado — ainda não há histórico suficiente no Prometheus pra esse nome.
  node_created_at?: string;
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
  // F1.2 (FINOPS-IMPROVEMENTS-PLAN.md) — true quando a capacidade desta SKU por node fica
  // abaixo do maior request individual (CPU/Mem) entre os workloads do pool + margem de
  // segurança: o maior pod ali rodando pode não conseguir ser agendado nesta SKU menor. Nunca
  // remove a alternativa, só sinaliza — a decisão final continua humana.
  insufficient_for_largest_workload?: boolean;
}

interface NodePoolTierSuggestion {
  node_pool: string;
  current_sku: string;
  // cpu_util_pct/mem_util_pct já inclui a margem de segurança (P95-ou-avg × 1.20) — é o número
  // que decide a sugestão de tier, não o uso real puro. cpu_p95_pct/mem_p95_pct é o percentil
  // REAL, sem margem — pedido explícito do usuário pra evidenciar o percentil por trás da decisão.
  cpu_util_pct: number;
  mem_util_pct: number;
  cpu_p95_pct?: number;
  mem_p95_pct?: number;
  workload_count: number;
  // Node count — atual (sempre presente) + min/max de autoscaling (best-effort, via cloud
  // provider ao vivo — 0/0 quando a chamada falhou no scan, ver ScanRightsizing).
  node_count?: number;
  min_node_count?: number;
  max_node_count?: number;
  autoscaling_enabled?: boolean;
  vm_cpu_cores?: number;
  vm_memory_gb?: number;
  // F1.1 (FINOPS-IMPROVEMENTS-PLAN.md) — PALPITE heurístico (substring por nome de workload
  // contra uma lista curada de infra conhecida: ingress-controller, istio, velero, cert-manager,
  // coredns, etc.), nunca uma verdade absoluta. Existe pra evitar o incidente real que motivou
  // esta fase: sugestão de downsize sem nenhum aviso pro pool "ingress"
  // (nginx-ingress-controller/velero/istio-ingressgateway).
  has_critical_workload?: boolean;
  critical_workload_names?: string; // nomes separados por ", "
  alternatives: VMAlternative[];
  generated_at: string;
}

export interface RightsizingResponse {
  cluster: string;
  scanned: boolean;
  last_scanned_at?: string;
  workloads?: WorkloadRecommendation[];
  node_pools?: NodePoolTierSuggestion[];
  nodes?: NodeUsageInfo[];
  // metrics_collection_error — só presente na resposta de um scan FRESCO (POST .../scan), nunca
  // na leitura persistida (GET .../rightsizing) — ver comentário completo em
  // internal/finops/models.go (FinOpsSummary.MetricsCollectionError). "" = consultas tiveram
  // sucesso mas sem nenhum dado real (cluster sem cobertura de monitoramento); não-vazia = pelo
  // menos uma consulta falhou de verdade (falha transitória, "reanalisar" pode resolver).
  metrics_collection_error?: string;
}

// Histórico de uso (CPU/Mem) sob demanda, ver GET /finops/rightsizing/history — buscado só quando
// o modal de detalhe de um workload abre (nunca no scan em lote da lista inteira).
interface WorkloadHistoryPoint {
  timestamp: string;
  value: number;
}

interface WorkloadHistoryResponse {
  available: boolean;
  reason?: string;
  cpu_millis?: WorkloadHistoryPoint[];
  mem_mi?: WorkloadHistoryPoint[];
}

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

/** F2.1 (FINOPS-IMPROVEMENTS-PLAN.md) — Rightsizing era a única das 8 abas do FinOps sem badge
 *  de contagem/urgência na TabsTrigger, sem nenhum sinal visual de que ali existe economia em
 *  R$. Mostra a economia potencial (melhor alternativa por pool, somada — nunca soma TODAS as
 *  até-3 alternativas de um mesmo pool, que dobraria/triplicaria a mesma oportunidade) quando
 *  há alguma relevante (> R$10/mês, mesmo limiar de "vale a pena mostrar" já usado nos cards de
 *  alternativa); cai pro número de workloads que precisam de atenção (`verdict != "ok"`) quando
 *  não há economia de tier mas ainda há algo a rever. Silencioso (sem badge) só quando os dois
 *  são zero — mesmo princípio de "badge só no caso relevante" já usado no CompanyAppBadge de
 *  DeploymentsTab.tsx. */
export function RightsizingTabBadge({ cluster }: { cluster: string }) {
  const { data } = useRightsizingReport(cluster);
  const { totalSavings, attentionCount } = useMemo(() => {
    let savings = 0;
    for (const pool of data?.node_pools ?? []) {
      const best = Math.max(0, ...pool.alternatives.map((a) => a.monthly_savings_brl));
      if (best > 10) savings += best;
    }
    const attention = (data?.workloads ?? []).filter((w) => w.verdict !== "ok").length;
    return { totalSavings: savings, attentionCount: attention };
  }, [data]);

  if (totalSavings > 10) {
    return (
      <Badge className="ml-1 text-[10px] bg-green-600" title={`Economia potencial de tier de VM: ${fmtBRL(totalSavings)}/mês`}>
        -{fmtBRL(totalSavings)}
      </Badge>
    );
  }
  if (attentionCount > 0) {
    return (
      <Badge variant="destructive" className="ml-1 text-[10px]" title={`${attentionCount} workload(s) precisam de atenção (verdict diferente de "ok")`}>
        {attentionCount}
      </Badge>
    );
  }
  return null;
}

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

/** F2.3 (FINOPS-IMPROVEMENTS-PLAN.md) — `last_scanned_at` era mostrado sempre em cinza neutro,
 *  "45d atrás" com a mesma aparência de "5min atrás", sem nenhum sinal de que o dado pode estar
 *  desatualizado (uso real muda — request/limit de workload, tier de VM sugerido — então uma
 *  análise de semanas atrás pode já não refletir a realidade do cluster). Limiares escolhidos
 *  pelo próprio plano: >7 dias → âmbar (aviso leve), >30 dias → vermelho (provavelmente
 *  desatualizado). Nunca some o dado nem bloqueia nada — só chama atenção visual. */
function scanAgeSeverity(iso?: string): "fresh" | "stale" | "very_stale" {
  if (!iso) return "fresh";
  const days = (Date.now() - new Date(iso).getTime()) / 86400000;
  if (days > 30) return "very_stale";
  if (days > 7) return "stale";
  return "fresh";
}

/** Data/hora curta (DD/MM HH:MM, timezone do browser) de quando um "top"/pico foi observado —
 *  o usuário pediu explicitamente "data e hora das ocorrências" pros valores de top, não só um
 *  número solto (que não diz se é de ontem ou de 29 dias atrás). undefined quando a fonte não
 *  traz timestamp (ex: Dynatrace, que só devolve um valor agregado por janela). */
function fmtOccurredAt(iso?: string): string | undefined {
  if (!iso) return undefined;
  return new Date(iso).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}

/** Data/hora completa (usada em tooltip) — complementa fmtOccurredAt, que é deliberadamente
 *  curto pra caber inline ao lado do valor de "top". */
function fmtOccurredAtFull(iso?: string): string | undefined {
  if (!iso) return undefined;
  return new Date(iso).toLocaleString("pt-BR");
}

/** "Tempo de vida" — há quanto tempo um node existe (CreationTimestamp) ou um workload está no
 *  ar sem reiniciar (CreationTimestamp do pod Running mais antigo) — pedido explícito do
 *  usuário ("periodo de tempo de vida de sua existencia"), contextualiza se um "top" ausente é
 *  genuinamente "sem uso" ou só "recurso jovem demais pra ter histórico ainda". */
function fmtLifetime(iso?: string): string | undefined {
  if (!iso) return undefined;
  const diffMs = Date.now() - new Date(iso).getTime();
  if (diffMs < 0) return undefined;
  const days = Math.floor(diffMs / 86400000);
  if (days >= 1) return `${days}d`;
  const hours = Math.floor(diffMs / 3600000);
  if (hours >= 1) return `${hours}h`;
  const mins = Math.max(1, Math.floor(diffMs / 60000));
  return `${mins}min`;
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
  const cpuTopAt = fmtOccurredAt(node.cpu_top_at);
  const memTopAt = fmtOccurredAt(node.mem_top_at);
  const age = fmtLifetime(node.node_created_at);
  return (
    <p className="text-[10px] text-muted-foreground flex flex-wrap items-center gap-x-1.5" title={`Node ${node.node_name}`}>
      <Server className="h-3 w-3" /> Node <span className="font-mono">{node.node_name}</span>
      {age && <span title={fmtOccurredAtFull(node.node_created_at)}>(existe há {age})</span>}
      <span>
        CPU agora <span className={`font-semibold ${pctColorClass(cpuNow)}`}>{cpuNow.toFixed(0)}%</span>
        {cpuTop > 0 && (
          <>
            {" "}(pico <span className={`font-semibold ${pctColorClass(cpuTop)}`}>{cpuTop.toFixed(0)}%</span>
            {cpuTopAt && <> em {cpuTopAt}</>})
          </>
        )}
      </span>
      <span>
        Mem agora <span className={`font-semibold ${pctColorClass(memNow)}`}>{memNow.toFixed(0)}%</span>
        {memTop > 0 && (
          <>
            {" "}(pico <span className={`font-semibold ${pctColorClass(memTop)}`}>{memTop.toFixed(0)}%</span>
            {memTopAt && <> em {memTopAt}</>})
          </>
        )}
      </span>
      {cpuTop === 0 && memTop === 0 && age && (
        <span className="italic">sem pico registrado — pode ser dado genuinamente ausente ou o node não ter histórico suficiente ainda</span>
      )}
    </p>
  );
}

/** Uso agregado do pool inteiro (não só 1 node) — soma ponderada pela capacidade de cada node,
 *  não uma média simples de percentuais (um node maior pesa mais no total do pool). Mesmo
 *  princípio de "current" (live)/"top" (pico) já usado por workload, agora por pool. */
function poolAggregateUsage(nodes: NodeUsageInfo[]): {
  cpuNowPct: number; cpuTopPct: number; memNowPct: number; memTopPct: number;
  cpuCapMillis: number; memCapMi: number; anyMetricsUnavailable: boolean;
} {
  let capCPU = 0, capMem = 0, curCPU = 0, curMem = 0, topCPU = 0, topMem = 0;
  let anyMetricsUnavailable = false;
  for (const n of nodes) {
    if (!n.metrics_available) { anyMetricsUnavailable = true; continue; }
    const capC = n.cpu_cap_millis ?? 0;
    const capM = n.mem_cap_mi ?? 0;
    capCPU += capC;
    capMem += capM;
    curCPU += (capC * (n.cpu_current_pct ?? 0)) / 100;
    curMem += (capM * (n.mem_current_pct ?? 0)) / 100;
    topCPU += (capC * (n.cpu_top_pct ?? 0)) / 100;
    topMem += (capM * (n.mem_top_pct ?? 0)) / 100;
  }
  return {
    cpuNowPct: capCPU > 0 ? (curCPU / capCPU) * 100 : 0,
    cpuTopPct: capCPU > 0 ? (topCPU / capCPU) * 100 : 0,
    memNowPct: capMem > 0 ? (curMem / capMem) * 100 : 0,
    memTopPct: capMem > 0 ? (topMem / capMem) * 100 : 0,
    cpuCapMillis: capCPU,
    memCapMi: capMem,
    anyMetricsUnavailable,
  };
}

/** Cenário de resize de NODE COUNT — heurística clara, baseada no uso agregado do pool (que já
 *  vem do uso RECOMENDADO real dos workloads, não do request nominal — ver poolAgg em
 *  ScanRightsizing). Nunca afirma com certeza, só orienta — mesma fraseologia neutra do resto
 *  da app quando o dado é parcial (min/max ausente = falha ao consultar o cloud provider). */
function nodeCountScenarioText(pool: NodePoolTierSuggestion): string {
  const nodeCount = pool.node_count ?? 0;
  const minCount = pool.min_node_count ?? 0;
  const maxCount = pool.max_node_count ?? 0;
  const worstUtil = Math.max(pool.cpu_util_pct, pool.mem_util_pct);
  const haveRange = minCount > 0 || maxCount > 0;

  if (!haveRange) {
    return `Node count atual: ${nodeCount}. Não foi possível confirmar o min/max de autoscaling ao vivo neste scan (falha ao consultar o cloud provider) — sem esse dado não dá pra sugerir um range com segurança.`;
  }

  const autoscalePart = pool.autoscaling_enabled
    ? `autoscaling ligado, faixa ${minCount}–${maxCount}`
    : `autoscaling desligado, fixo em ${nodeCount}`;

  if (worstUtil >= 80) {
    const suggestedMin = Math.min(maxCount || nodeCount + 1, (minCount || nodeCount) + 1);
    return `Uso agregado do pool está ALTO (${worstUtil.toFixed(0)}% do pior recurso entre CPU/Mem, baseado no uso recomendado real dos workloads) — hoje ${autoscalePart}. Considere aumentar o mínimo de nodes${pool.autoscaling_enabled ? ` de ${minCount} para pelo menos ${suggestedMin}` : ` (ligar autoscaling ajudaria a absorver picos automaticamente)`}.`;
  }
  if (worstUtil > 0 && worstUtil < 30 && minCount > 1) {
    const suggestedMin = Math.max(1, minCount - 1);
    return `Uso agregado do pool está BAIXO (${worstUtil.toFixed(0)}%) — hoje ${autoscalePart}. Considere reduzir o mínimo de nodes de ${minCount} para ${suggestedMin}, se essa folga não for necessária pra absorver picos de tráfego.`;
  }
  return `Uso agregado do pool está numa faixa saudável (${worstUtil.toFixed(0)}%) — ${autoscalePart} parece adequado pro padrão de uso atual.`;
}

/** Cenário de resize de RÉPLICAS — heurística clara, usando min/max configurado (HPA) vs.
 *  observado (via Prometheus, quando disponível). hpa_max===hpa_min é o sinal de workload sem
 *  HPA de verdade (réplicas fixas — ver allocateCosts em calculator.go). */
function replicaScenarioText(w: WorkloadRecommendation): string {
  const min = w.hpa_min ?? w.pods;
  const max = w.hpa_max ?? w.pods;
  const current = w.hpa_current ?? w.pods;
  const hasRealHPA = max !== min;

  if (!hasRealHPA) {
    return `Sem HPA configurado — réplicas fixas em ${current}. Sem histórico de escala pra sugerir um range de min/max.`;
  }
  if (w.hpa_never_scaled) {
    const peak = w.hpa_max_observed ?? min;
    return `O HPA nunca escalou além do mínimo (${min}) nos últimos ${w.window_days}d — pico observado de réplicas: ${peak}. Considere reduzir o máximo configurado (hoje ${max}) pra algo próximo de ${peak}, ou remover o HPA se o tráfego é sempre estável.`;
  }
  if (w.hpa_max_observed !== undefined && w.hpa_max_observed > 0) {
    if (w.hpa_max_observed < max) {
      return `Pico observado de réplicas (${w.hpa_max_observed}) ficou abaixo do máximo configurado (${max}) nos últimos ${w.window_days}d. Considere reduzir o máximo pra algo próximo de ${w.hpa_max_observed}, com uma margem de segurança.`;
    }
    return `O workload já escalou até perto do máximo configurado (${max}) nos últimos ${w.window_days}d (pico observado: ${w.hpa_max_observed}) — se picos de tráfego maiores são esperados, considere aumentar o máximo.`;
  }
  return `Min/max configurado: ${min}–${max} (atual: ${current}). Sem dado observado de réplicas no período (Prometheus indisponível ou sem histórico de HPA) pra confirmar se o range está adequado.`;
}

/** Gráfico de evolução histórica (CPU ou Mem) de um workload — substitui o ResourceGauge estático
 *  no modal de detalhe (pedido explícito do usuário: "não seria melhor usar um gráfico para poder
 *  ver a evolução das métricas e onde o ponto de pico existiu dentro dos dados históricos?").
 *  Linhas de referência tracejadas marcam request/limit/recomendado (mesmo papel que os arcos do
 *  gauge cumpriam antes); um ReferenceDot marca explicitamente o ponto de maior valor da série. */
function WorkloadHistoryChart({
  title, points, request, limit, recommended, color, formatValue, windowDays,
}: {
  title: string;
  points: WorkloadHistoryPoint[] | undefined;
  request?: number;
  limit?: number;
  recommended?: number;
  color: string;
  formatValue: (v: number) => string;
  windowDays: number;
}) {
  const chartData = useMemo(() => {
    if (!points?.length) return [];
    return points.map((p) => ({
      time: new Date(p.timestamp).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }),
      value: p.value,
    }));
  }, [points]);

  const peak = useMemo(() => {
    if (!chartData.length) return null;
    return chartData.reduce((max, p) => (p.value > max.value ? p : max), chartData[0]);
  }, [chartData]);

  if (!chartData.length) {
    return (
      <div className="space-y-1">
        <p className="text-[10px] text-muted-foreground uppercase tracking-wide">{title}</p>
        <div className="h-[130px] flex items-center justify-center border rounded-md">
          <p className="text-[11px] text-muted-foreground">Sem dados de uso no período.</p>
        </div>
      </div>
    );
  }

  const maxVal = Math.max(...chartData.map((p) => p.value), request ?? 0, limit ?? 0, recommended ?? 0, 1);
  const xInterval = Math.max(0, Math.floor(chartData.length / 5) - 1);

  return (
    <div className="space-y-1">
      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">{title} — últimos {windowDays}d</p>
      <ChartContainer config={{ value: { label: title, color } }} className="h-[130px] w-full">
        <ComposedChart data={chartData} margin={{ top: 10, right: 10, left: -18, bottom: 0 }}>
          <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
          <YAxis
            tick={{ fontSize: 9 }}
            tickLine={false}
            axisLine={false}
            width={54}
            domain={[0, maxVal * 1.15]}
            tickFormatter={(v: number) => formatValue(v)}
          />
          <ChartTooltip
            content={
              <ChartTooltipContent
                labelFormatter={(l) => l as string}
                formatter={(value) => (
                  <div className="flex flex-1 justify-between items-center leading-none gap-3">
                    <span className="text-muted-foreground">{title}</span>
                    <span className="font-mono font-medium tabular-nums text-foreground">{formatValue(Number(value))}</span>
                  </div>
                )}
              />
            }
          />
          {!!request && (
            <ReferenceLine y={request} stroke="#94a3b8" strokeDasharray="4 3" strokeWidth={1}
              label={{ value: "request", position: "insideTopLeft", fontSize: 9, fill: "#94a3b8" }} />
          )}
          {!!limit && (
            <ReferenceLine y={limit} stroke="#ef4444" strokeDasharray="4 3" strokeWidth={1}
              label={{ value: "limit", position: "insideTopLeft", fontSize: 9, fill: "#ef4444" }} />
          )}
          {!!recommended && (
            <ReferenceLine y={recommended} stroke="#a855f7" strokeDasharray="4 3" strokeWidth={1}
              label={{ value: "recomendado", position: "insideBottomLeft", fontSize: 9, fill: "#a855f7" }} />
          )}
          <Line type="monotone" dataKey="value" stroke={color} strokeWidth={1.75} dot={false} isAnimationActive={false} />
          {peak && (
            <ReferenceDot x={peak.time} y={peak.value} r={3.5} fill="#f59e0b" stroke="#fff" strokeWidth={1}
              label={{ value: `pico ${formatValue(peak.value)}`, position: "top", fontSize: 9, fill: "#f59e0b" }} />
          )}
        </ComposedChart>
      </ChartContainer>
    </div>
  );
}

/** F1.1 (FINOPS-IMPROVEMENTS-PLAN.md) — aviso de infra crítica no pool, compartilhado entre
 *  NodePoolTierCard (card principal da lista) e WorkloadNodeDetailModal (modal de detalhe). Só
 *  renderiza quando `has_critical_workload` vem true — silencioso no caso comum (a maioria dos
 *  pools é app de negócio, não infra), mesmo princípio de "badge só no caso especial" já usado
 *  no CompanyAppBadge de DeploymentsTab.tsx. Nunca esconde a sugestão de downsize abaixo — só
 *  pede atenção extra antes de aplicá-la. */
function CriticalWorkloadWarning({ pool }: { pool: NodePoolTierSuggestion }) {
  if (!pool.has_critical_workload) return null;
  return (
    <div className="flex items-start gap-1.5 rounded-md border border-amber-400/50 bg-amber-50 dark:bg-amber-950/30 px-2 py-1.5 text-[11px] text-amber-800 dark:text-amber-300">
      <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5" />
      <span>
        Este pool roda componente(s) de infraestrutura
        {pool.critical_workload_names ? <> (<span className="font-medium">{pool.critical_workload_names}</span>)</> : null}
        {" "}— reveja com cuidado antes de aplicar qualquer sugestão de downsize abaixo (palpite por nome, não uma garantia).
      </span>
    </div>
  );
}

/** F1.2 (FINOPS-IMPROVEMENTS-PLAN.md) — aviso por alternativa, quando ela pode não comportar o
 *  maior pod individual do pool. Compartilhado entre os 2 pontos que renderizam
 *  `alt`/VMAlternative. */
function InsufficientCapacityWarning({ alt }: { alt: VMAlternative }) {
  if (!alt.insufficient_for_largest_workload) return null;
  return (
    <p className="text-[10px] font-medium text-red-600 dark:text-red-400 flex items-center gap-1">
      <AlertTriangle className="h-3 w-3 shrink-0" /> Capacidade pode não comportar o maior pod individual do pool — revisar antes de aplicar.
    </p>
  );
}

/** Modal de detalhe combinado — aberto ao clicar no nome de uma aplicação dentro do card do
 *  Node Pool/Node Group (pedido explícito do usuário: informação completa de node+app+cenários
 *  de resize num só lugar, sem precisar caçar em vários cards). */
function WorkloadNodeDetailModal({
  cluster, workload, pool, poolNodes, onClose,
}: {
  cluster: string;
  workload: WorkloadRecommendation;
  pool: NodePoolTierSuggestion | undefined;
  poolNodes: NodeUsageInfo[];
  onClose: () => void;
}) {
  const agg = useMemo(() => poolAggregateUsage(poolNodes), [poolNodes]);
  const resizeCmd = buildResizeCommand(workload);
  const totalCPUCores = (pool?.vm_cpu_cores ?? 0) * (pool?.node_count ?? 0);
  const totalMemGB = (pool?.vm_memory_gb ?? 0) * (pool?.node_count ?? 0);

  // Histórico on-demand — só busca quando este modal está de fato montado (fechar o modal
  // desmonta o componente, cancelando a query via React Query; nunca roda no scan em lote).
  const { data: historyResp, isLoading: historyLoading } = useQuery<WorkloadHistoryResponse>({
    queryKey: ["finops-workload-history", cluster, workload.namespace, workload.workload, workload.window_days],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/rightsizing/history?cluster=${encodeURIComponent(cluster)}&namespace=${encodeURIComponent(workload.namespace)}&workload=${encodeURIComponent(workload.workload)}&days=${workload.window_days}`,
        { headers: authHeaders() }
      );
      if (!r.ok) throw new Error("workload-history error");
      return r.json();
    },
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-3xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Sparkles className="h-4 w-4 text-purple-500" />
            {workload.workload}
            <span className="text-xs font-normal text-muted-foreground">— {workload.namespace}</span>
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-5">
          {/* ── Node / Node Pool ──────────────────────────────────────────────────────────── */}
          <section className="space-y-2">
            <h4 className="text-xs font-semibold flex items-center gap-1.5 text-muted-foreground uppercase tracking-wide">
              <Boxes className="h-3.5 w-3.5" /> Node Pool — {pool?.node_pool ?? workload.node_pool ?? "desconhecido"}
            </h4>
            {!pool ? (
              <Alert variant="destructive">
                <AlertDescription>Sem dado de node pool persistido pra este workload — reanalise o cluster.</AlertDescription>
              </Alert>
            ) : (
              <div className="border rounded-lg p-3 space-y-3 bg-muted/20">
                <CriticalWorkloadWarning pool={pool} />
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs">
                  <div><span className="text-muted-foreground">Tier (SKU) atual:</span> <span className="font-mono font-medium">{pool.current_sku || "—"}</span></div>
                  <div><span className="text-muted-foreground">Autoscaling:</span> <span className="font-medium">{pool.autoscaling_enabled ? "Ligado" : "Desligado"}</span></div>
                  <div><span className="text-muted-foreground">Node count — mínimo:</span> <span className="font-mono font-medium">{pool.min_node_count || "—"}</span></div>
                  <div><span className="text-muted-foreground">Node count — máximo:</span> <span className="font-mono font-medium">{pool.max_node_count || "—"}</span></div>
                  <div><span className="text-muted-foreground">Node count — atual:</span> <span className="font-mono font-medium">{pool.node_count ?? "—"}</span></div>
                  <div><span className="text-muted-foreground">Capacidade por node:</span> <span className="font-mono font-medium">{pool.vm_cpu_cores ?? "?"} vCPU / {pool.vm_memory_gb ?? "?"} GB</span></div>
                  <div className="col-span-2"><span className="text-muted-foreground">Capacidade total do pool:</span> <span className="font-mono font-medium">{totalCPUCores || "?"} vCPU / {totalMemGB || "?"} GB</span></div>
                </div>

                <div className="grid grid-cols-2 gap-4 pt-1 border-t">
                  <div className="text-xs">
                    <p className="text-muted-foreground mb-0.5">CPU do pool (agregado, ponderado por node)</p>
                    <p>agora: <span className={`font-semibold font-mono ${pctColorClass(agg.cpuNowPct)}`}>{agg.cpuNowPct.toFixed(0)}%</span>
                      {" "}· pico: <span className={`font-semibold font-mono ${pctColorClass(agg.cpuTopPct)}`}>{agg.cpuTopPct.toFixed(0)}%</span></p>
                  </div>
                  <div className="text-xs">
                    <p className="text-muted-foreground mb-0.5">Mem do pool (agregado, ponderado por node)</p>
                    <p>agora: <span className={`font-semibold font-mono ${pctColorClass(agg.memNowPct)}`}>{agg.memNowPct.toFixed(0)}%</span>
                      {" "}· pico: <span className={`font-semibold font-mono ${pctColorClass(agg.memTopPct)}`}>{agg.memTopPct.toFixed(0)}%</span></p>
                  </div>
                </div>
                {agg.anyMetricsUnavailable && (
                  <p className="text-[10px] text-amber-600 dark:text-amber-400">⚠ Métricas ao vivo indisponíveis em pelo menos 1 node do pool — agregado acima considera só os nodes com dado.</p>
                )}

                {poolNodes.length > 0 && (
                  <div className="space-y-1 pt-1 border-t">
                    <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Por node ({poolNodes.length})</p>
                    {poolNodes.map((n) => {
                      const age = fmtLifetime(n.node_created_at);
                      const cpuTopAt = fmtOccurredAt(n.cpu_top_at);
                      const memTopAt = fmtOccurredAt(n.mem_top_at);
                      return (
                        <div key={n.node_name} className="text-[11px] flex flex-wrap items-center gap-x-2 gap-y-0.5">
                          <Server className="h-3 w-3 text-muted-foreground shrink-0" />
                          <span className="font-mono">{n.node_name}</span>
                          {age && <span className="text-muted-foreground" title={fmtOccurredAtFull(n.node_created_at)}>(existe há {age})</span>}
                          {n.metrics_available ? (
                            <>
                              <span>CPU <span className={`font-semibold ${pctColorClass(n.cpu_current_pct ?? 0)}`}>{(n.cpu_current_pct ?? 0).toFixed(0)}%</span> (pico {(n.cpu_top_pct ?? 0).toFixed(0)}%{cpuTopAt ? ` em ${cpuTopAt}` : ""})</span>
                              <span>Mem <span className={`font-semibold ${pctColorClass(n.mem_current_pct ?? 0)}`}>{(n.mem_current_pct ?? 0).toFixed(0)}%</span> (pico {(n.mem_top_pct ?? 0).toFixed(0)}%{memTopAt ? ` em ${memTopAt}` : ""})</span>
                              {(n.cpu_top_pct ?? 0) === 0 && (n.mem_top_pct ?? 0) === 0 && (
                                <span className="italic text-muted-foreground">sem pico registrado{age ? " (node jovem, ou genuinamente sem uso no período)" : ""}</span>
                              )}
                            </>
                          ) : (
                            <span className="text-amber-600 dark:text-amber-400" title={n.metrics_error}>⚠ métricas indisponíveis</span>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}

                <div className="pt-1 border-t">
                  <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 flex items-center gap-1"><Gauge className="h-3 w-3" /> Cenário de resize — node count</p>
                  <p className="text-[11px]">{nodeCountScenarioText(pool)}</p>
                </div>

                <div className="pt-1 border-t">
                  <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 flex items-center gap-1"><Gauge className="h-3 w-3" /> Cenário de resize — tier de VM</p>
                  {pool.alternatives.length === 0 ? (
                    <p className="text-[11px] text-muted-foreground">Nenhuma alternativa de tier identificada para o padrão de uso atual.</p>
                  ) : (
                    <div className="grid gap-2">
                      {pool.alternatives.map((alt) => {
                        const cfg = tierVerdictCfg[alt.verdict] ?? tierVerdictCfg.consider;
                        return (
                          <div key={alt.vm_size} className="border rounded-lg p-2 space-y-1 bg-background">
                            <div className="flex items-center justify-between gap-2">
                              <span className="text-xs font-mono font-semibold">{alt.vm_size}</span>
                              <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${cfg.cls}`}>{cfg.label}</span>
                            </div>
                            <p className="text-[11px] text-muted-foreground">
                              {alt.cpu_cores} vCPU · {alt.memory_gb} GB RAM · ${alt.price_usd_hour.toFixed(3)}/hora ({alt.cost_delta_pct > 0 ? "+" : ""}{alt.cost_delta_pct}%)
                            </p>
                            {alt.monthly_savings_brl > 10 && <p className="text-[11px] font-semibold text-green-600">-{fmtBRL(alt.monthly_savings_brl)}/mês na frota do pool</p>}
                            {alt.monthly_savings_brl < -10 && <p className="text-[11px] font-semibold text-orange-600">+{fmtBRL(Math.abs(alt.monthly_savings_brl))}/mês na frota do pool</p>}
                            <p className="text-[11px] italic text-muted-foreground">{alt.reason}</p>
                            <InsufficientCapacityWarning alt={alt} />
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            )}
          </section>

          {/* ── Aplicação ─────────────────────────────────────────────────────────────────── */}
          <section className="space-y-2">
            <h4 className="text-xs font-semibold flex items-center gap-1.5 text-muted-foreground uppercase tracking-wide">
              <Info className="h-3.5 w-3.5" /> Aplicação
            </h4>
            <div className="border rounded-lg p-3 space-y-3 bg-muted/20">
              <div className="flex items-center justify-between">
                <VerdictBadge verdict={workload.verdict} />
                {(workload.waste_brl ?? 0) > 0 && <span className="text-xs font-semibold text-red-500">-{fmtBRL(workload.waste_brl!)}/mês de desperdício</span>}
              </div>

              <div className="grid grid-cols-3 gap-x-4 gap-y-1.5 text-xs">
                <div><span className="text-muted-foreground">Réplicas — mínimo:</span> <span className="font-mono font-medium">{workload.hpa_min ?? workload.pods}</span></div>
                <div><span className="text-muted-foreground">Réplicas — máximo:</span> <span className="font-mono font-medium">{workload.hpa_max ?? workload.pods}</span></div>
                <div><span className="text-muted-foreground">Réplicas — atual:</span> <span className="font-mono font-medium">{workload.hpa_current ?? workload.pods}</span></div>
                {(workload.hpa_avg_replicas ?? 0) > 0 && (
                  <>
                    <div><span className="text-muted-foreground">Observado — mín:</span> <span className="font-mono">{workload.hpa_min_observed ?? "—"}</span></div>
                    <div><span className="text-muted-foreground">Observado — méd:</span> <span className="font-mono">{workload.hpa_avg_replicas!.toFixed(1)}</span></div>
                    <div><span className="text-muted-foreground">Observado — máx:</span> <span className="font-mono">{workload.hpa_max_observed ?? "—"}</span></div>
                  </>
                )}
              </div>

              {(() => {
                const podAge = fmtLifetime(workload.oldest_pod_started_at);
                return podAge ? (
                  <p className="text-[10px] text-muted-foreground" title={fmtOccurredAtFull(workload.oldest_pod_started_at)}>
                    Pod mais antigo (Running) no ar há {podAge} sem reiniciar — contextualiza se um pico histórico abaixo ainda é relevante (pods de hoje podem já não ser os mesmos que geraram o pico).
                  </p>
                ) : null;
              })()}

              <div className="grid grid-cols-2 gap-4 py-1">
                {historyLoading ? (
                  <>
                    <div className="h-[150px] rounded-md bg-muted/40 animate-pulse" />
                    <div className="h-[150px] rounded-md bg-muted/40 animate-pulse" />
                  </>
                ) : !historyResp?.available ? (
                  <div className="col-span-2 text-[11px] text-muted-foreground border rounded-md p-3 text-center">
                    {historyResp?.reason ?? "Histórico de uso indisponível para este workload."}
                  </div>
                ) : (
                  <>
                    <WorkloadHistoryChart
                      title="CPU"
                      points={historyResp.cpu_millis}
                      request={workload.cpu_request_millis}
                      limit={workload.cpu_limit_millis}
                      recommended={workload.cpu_recommended_millis}
                      color="#3b82f6"
                      formatValue={fmtMillis}
                      windowDays={workload.window_days}
                    />
                    <WorkloadHistoryChart
                      title="Memória"
                      points={historyResp.mem_mi}
                      request={workload.mem_request_mi}
                      limit={workload.mem_limit_mi}
                      recommended={workload.mem_recommended_mi}
                      color="#10b981"
                      formatValue={fmtMi}
                      windowDays={workload.window_days}
                    />
                  </>
                )}
              </div>

              <div className="grid grid-cols-2 gap-4 text-[10px] text-muted-foreground">
                <div className="space-x-2">
                  {(workload.cpu_current_millis ?? 0) > 0 && <span>agora: <span className="font-mono">{fmtMillis(workload.cpu_current_millis!)}</span></span>}
                  {(workload.cpu_p95_millis ?? 0) > 0 && <span>P95: <span className="font-mono">{fmtMillis(workload.cpu_p95_millis!)}</span></span>}
                  {(workload.cpu_max_millis ?? 0) > 0 && (
                    <span>
                      top: <span className="font-mono">{fmtMillis(workload.cpu_max_millis!)}</span>
                      {fmtOccurredAt(workload.cpu_max_at) && <span title={fmtOccurredAtFull(workload.cpu_max_at)}> em {fmtOccurredAt(workload.cpu_max_at)}</span>}
                    </span>
                  )}
                </div>
                <div className="space-x-2">
                  {(workload.mem_current_mi ?? 0) > 0 && <span>agora: <span className="font-mono">{fmtMi(workload.mem_current_mi!)}</span></span>}
                  {(workload.mem_p95_mi ?? 0) > 0 && <span>P95: <span className="font-mono">{fmtMi(workload.mem_p95_mi!)}</span></span>}
                  {(workload.mem_max_mi ?? 0) > 0 && (
                    <span>
                      top: <span className="font-mono">{fmtMi(workload.mem_max_mi!)}</span>
                      {fmtOccurredAt(workload.mem_max_at) && <span title={fmtOccurredAtFull(workload.mem_max_at)}> em {fmtOccurredAt(workload.mem_max_at)}</span>}
                    </span>
                  )}
                </div>
              </div>

              <div className="text-[11px] text-muted-foreground space-y-0.5 pt-1 border-t">
                {workload.cpu_recommended_millis ? (
                  <p>
                    CPU — request: <span className="font-mono">{fmtMillis(workload.cpu_request_millis)}</span>
                    {workload.cpu_limit_millis ? <> · limit: <span className="font-mono">{fmtMillis(workload.cpu_limit_millis)}</span></> : null}
                    {" → "}
                    recomendado: <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMillis(workload.cpu_recommended_millis)}</span>
                    {workload.cpu_limit_recommended_millis ? <> / <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMillis(workload.cpu_limit_recommended_millis)}</span></> : null}
                  </p>
                ) : null}
                {workload.mem_recommended_mi ? (
                  <p>
                    Mem — request: <span className="font-mono">{fmtMi(workload.mem_request_mi)}</span>
                    {workload.mem_limit_mi ? <> · limit: <span className="font-mono">{fmtMi(workload.mem_limit_mi)}</span></> : null}
                    {" → "}
                    recomendado: <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMi(workload.mem_recommended_mi)}</span>
                    {workload.mem_limit_recommended_mi ? <> / <span className="font-mono text-purple-600 dark:text-purple-400">{fmtMi(workload.mem_limit_recommended_mi)}</span></> : null}
                  </p>
                ) : null}
                {workload.metrics_source && (
                  <p>fonte: <span className="font-mono">{workload.metrics_source === "dynatrace" ? "Dynatrace" : "Prometheus"}</span> · janela {workload.window_days}d</p>
                )}
              </div>

              <div className="pt-1 border-t">
                <p className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1 flex items-center gap-1"><Gauge className="h-3 w-3" /> Cenário de resize — réplicas</p>
                <p className="text-[11px]">{replicaScenarioText(workload)}</p>
              </div>

              {resizeCmd && (
                <div>
                  <p className="text-[10px] text-muted-foreground mb-1">Ação recomendada (request + limit):</p>
                  <KubectlBlock cmd={resizeCmd} />
                </div>
              )}
            </div>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}

const tierVerdictCfg: Record<string, { label: string; cls: string }> = {
  recommended: { label: "Recomendado", cls: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400" },
  consider: { label: "Considerar", cls: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400" },
  cheaper: { label: "Mais barato", cls: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400" },
};

function WorkloadCard({
  w, node, onOpenDetail,
}: {
  w: WorkloadRecommendation;
  node?: NodeUsageInfo;
  onOpenDetail: (w: WorkloadRecommendation) => void;
}) {
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
            <button
              type="button"
              className="font-medium truncate underline decoration-dotted hover:text-primary text-left"
              title="Ver detalhe completo (node + app + cenários de resize)"
              onClick={() => onOpenDetail(w)}
            >
              {w.workload}
            </button>
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
            {(w.cpu_max_millis ?? 0) > 0 && (
              <span>
                top: <span className="font-mono">{fmtMillis(w.cpu_max_millis!)}</span>
                {fmtOccurredAt(w.cpu_max_at) && <span title={fmtOccurredAtFull(w.cpu_max_at)}> em {fmtOccurredAt(w.cpu_max_at)}</span>}
              </span>
            )}
          </div>
          <div className="space-x-2">
            {hasLiveMem && <span>agora: <span className="font-mono">{fmtMi(w.mem_current_mi!)}</span></span>}
            {(w.mem_p95_mi ?? 0) > 0 && <span>P95: <span className="font-mono">{fmtMi(w.mem_p95_mi!)}</span></span>}
            {(w.mem_max_mi ?? 0) > 0 && (
              <span>
                top: <span className="font-mono">{fmtMi(w.mem_max_mi!)}</span>
                {fmtOccurredAt(w.mem_max_at) && <span title={fmtOccurredAtFull(w.mem_max_at)}> em {fmtOccurredAt(w.mem_max_at)}</span>}
              </span>
            )}
          </div>
        </div>

        {fmtLifetime(w.oldest_pod_started_at) && (
          <p className="text-[10px] text-muted-foreground -mt-1" title={fmtOccurredAtFull(w.oldest_pod_started_at)}>
            Pod mais antigo no ar há {fmtLifetime(w.oldest_pod_started_at)} sem reiniciar
          </p>
        )}

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

function NodePoolTierCard({
  pool, relatedWorkloads, onSelectWorkload,
}: {
  pool: NodePoolTierSuggestion;
  relatedWorkloads: WorkloadRecommendation[];
  onSelectWorkload: (w: WorkloadRecommendation) => void;
}) {
  const hasNodeRange = (pool.min_node_count ?? 0) > 0 || (pool.max_node_count ?? 0) > 0;
  return (
    <Card>
      <CardContent className="p-4 space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div>
            <p className="font-medium">{pool.node_pool}</p>
            <p className="text-[11px] text-muted-foreground font-mono">{pool.current_sku}</p>
            <p className="text-[10px] text-muted-foreground mt-0.5">
              {pool.node_count ?? "?"} node(s)
              {hasNodeRange && <> · min {pool.min_node_count} / máx {pool.max_node_count}{pool.autoscaling_enabled ? " (autoscaling)" : " (fixo)"}</>}
            </p>
          </div>
          <div className="text-right text-[11px] text-muted-foreground">
            <p title="Usado para decidir a sugestão de tier abaixo — já inclui 20% de margem de segurança sobre o uso real">
              CPU: {pool.cpu_util_pct.toFixed(0)}%
            </p>
            <p title="Usado para decidir a sugestão de tier abaixo — já inclui 20% de margem de segurança sobre o uso real">
              Mem: {pool.mem_util_pct.toFixed(0)}%
            </p>
            {((pool.cpu_p95_pct ?? 0) > 0 || (pool.mem_p95_pct ?? 0) > 0) && (
              <p
                className="mt-1 pt-1 border-t text-[10px] text-foreground/80"
                title="Percentil de uso real do pool (P95), sem nenhuma margem de segurança — mais fiel pra avaliar troca de família de máquina"
              >
                P95 real: {(pool.cpu_p95_pct ?? 0).toFixed(0)}% CPU / {(pool.mem_p95_pct ?? 0).toFixed(0)}% Mem
              </p>
            )}
          </div>
        </div>

        <CriticalWorkloadWarning pool={pool} />

        {relatedWorkloads.length > 0 && (
          <p className="text-[10px] text-muted-foreground">
            Baseado no uso real de {pool.workload_count} workload(s) — clique num nome pra ver o detalhe completo (node + app + cenários de resize):{" "}
            {relatedWorkloads.map((w, i) => (
              <span key={`${w.namespace}/${w.workload}`}>
                {i > 0 && ", "}
                <button
                  type="button"
                  className="underline hover:text-foreground font-medium"
                  onClick={() => onSelectWorkload(w)}
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
                  <InsufficientCapacityWarning alt={alt} />
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
  // Mesmo recurso já corrigido no botão "Analisar" da aba Dashboard (FinOpsTab.tsx): enquanto o
  // scan de rightsizing está rodando (POST /rightsizing/scan, síncrono, pode levar ~2min), o
  // botão vira "Cancelar" e aborta a requisição em andamento em vez de ficar só desabilitado.
  const scanAbortRef = useRef<AbortController | null>(null);
  const [namespaceFilter, setNamespaceFilter] = useState<string>("all");
  const [verdictFilter, setVerdictFilter] = useState<string>("all");
  const [search, setSearch] = useState("");
  const [detailWorkload, setDetailWorkload] = useState<WorkloadRecommendation | null>(null);

  const { data, isLoading, error } = useRightsizingReport(cluster);

  const runScan = async () => {
    const controller = new AbortController();
    scanAbortRef.current = controller;
    setScanning(true);
    setScanError(null);
    try {
      const r = await fetch(`/api/v1/finops/rightsizing/scan?cluster=${encodeURIComponent(cluster)}`, {
        method: "POST",
        headers: authHeaders(),
        signal: controller.signal,
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      const fresh: RightsizingResponse = await r.json();
      queryClient.setQueryData(["finops-rightsizing", cluster], fresh);
    } catch (e) {
      if (e instanceof DOMException && e.name === "AbortError") {
        // cancelado pelo usuário — nenhum erro a mostrar
      } else {
        setScanError(e instanceof Error ? e.message : "Falha ao analisar");
      }
    } finally {
      scanAbortRef.current = null;
      setScanning(false);
    }
  };

  const cancelScan = () => {
    scanAbortRef.current?.abort();
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

  const poolsByName = useMemo(() => {
    const m = new Map<string, NodePoolTierSuggestion>();
    for (const p of data?.node_pools ?? []) m.set(p.node_pool, p);
    return m;
  }, [data]);

  const nodesByPool = useMemo(() => {
    const m = new Map<string, NodeUsageInfo[]>();
    for (const n of data?.nodes ?? []) {
      const key = n.node_pool ?? "";
      if (!m.has(key)) m.set(key, []);
      m.get(key)!.push(n);
    }
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

  // P95 agregado do CLUSTER (não só por pool) — pedido explícito do usuário: "preciso que o
  // percentil de uso dos clusters... seja evidenciado nas análises". Média ponderada por
  // capacidade (vCPU/GB × node_count de cada pool), não uma média simples dos %— um pool de 2
  // nodes rodando a 90% não deveria pesar igual a um pool de 20 nodes rodando a 40%. Calculado
  // aqui no frontend (não persistido) porque é só uma agregação dos node_pools já recebidos —
  // sem custo de mais uma consulta/coluna no backend pra um número derivado.
  const clusterP95 = useMemo(() => {
    const pools = data?.node_pools ?? [];
    let cpuNum = 0, cpuDen = 0, memNum = 0, memDen = 0;
    for (const p of pools) {
      const cpuCap = (p.vm_cpu_cores ?? 0) * (p.node_count ?? 0);
      const memCap = (p.vm_memory_gb ?? 0) * (p.node_count ?? 0);
      if (cpuCap > 0 && (p.cpu_p95_pct ?? 0) > 0) {
        cpuNum += (p.cpu_p95_pct ?? 0) * cpuCap;
        cpuDen += cpuCap;
      }
      if (memCap > 0 && (p.mem_p95_pct ?? 0) > 0) {
        memNum += (p.mem_p95_pct ?? 0) * memCap;
        memDen += memCap;
      }
    }
    return {
      cpu: cpuDen > 0 ? cpuNum / cpuDen : 0,
      mem: memDen > 0 ? memNum / memDen : 0,
      hasData: cpuDen > 0 || memDen > 0,
    };
  }, [data?.node_pools]);

  // Rightsizing SEMPRE exige uso real (ScanRightsizing só roda se Dynatrace ou Prometheus foi
  // configurado — sem isso o scan já falha com 400 antes de chegar aqui, ver ScanRightsizing),
  // então nenhum workload com metrics_source é sempre um sinal de falha de coleta (VPN/rede/API
  // indisponível no momento do scan), nunca "cluster genuinamente sem desperdício algum" — bug
  // real corrigido, relatado pelo usuário com um scan real onde TODOS os pools vieram com CPU/
  // Mem 0%, "baseado no uso real de 0 workload(s)" e Desperdício Total R$0 ao mesmo tempo.
  const metricsFailureLikely = workloads.length > 0 && workloads.every((w) => !w.metrics_source);

  // metricsFailureReason — bug real corrigido: distingue "pelo menos uma consulta a Dynatrace/
  // Prometheus falhou de verdade nesta rodada" (metrics_collection_error não-vazio — falha
  // transitória, "Reanalisar agora" pode resolver) de "as consultas tiveram sucesso mas o
  // cluster genuinamente não tem nenhuma métrica pra devolver" (metrics_collection_error === "",
  // problema estrutural de cobertura) — só presente na resposta de um scan FRESCO (ver
  // RightsizingResponse.metrics_collection_error); undefined = leitura persistida (GET, sem essa
  // informação disponível), mantém a mensagem genérica/conservadora de antes.
  const metricsFailureReason: "transient" | "structural" | "unknown" =
    data?.metrics_collection_error === undefined ? "unknown" : data.metrics_collection_error === "" ? "structural" : "transient";

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

  const ageSeverity = data?.last_scanned_at ? scanAgeSeverity(data.last_scanned_at) : "fresh";

  return (
    <div className="space-y-4">
      {/* Header: última análise + botão de reanalisar */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold flex items-center gap-1.5">
            <Sparkles className="h-4 w-4 text-purple-500" />
            Rightsizing — Request/Limit e Tier de VM
          </h3>
          {data?.scanned && data.last_scanned_at ? (
            <p
              className={`text-xs flex items-center gap-1 ${
                ageSeverity === "very_stale"
                  ? "text-red-600 dark:text-red-400 font-medium"
                  : ageSeverity === "stale"
                    ? "text-amber-600 dark:text-amber-400 font-medium"
                    : "text-muted-foreground"
              }`}
            >
              {ageSeverity !== "fresh" && <AlertTriangle className="h-3 w-3 shrink-0" />}
              Última análise: {timeAgo(data.last_scanned_at)}
              {ageSeverity === "stale" && " — considere reanalisar"}
              {ageSeverity === "very_stale" && " — provavelmente desatualizado, reanalise"}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">Nunca analisado</p>
          )}
        </div>
        <Button size="sm" variant={scanning ? "destructive" : "default"} onClick={scanning ? cancelScan : runScan}>
          {scanning
            ? <X className="h-4 w-4 mr-1.5" />
            : <RefreshCw className="h-4 w-4 mr-1.5" />}
          {scanning ? "Cancelar" : (data?.scanned ? "Reanalisar agora" : "Analisar agora")}
        </Button>
      </div>

      {scanError && (
        <Alert variant="destructive">
          <AlertDescription>{scanError}</AlertDescription>
        </Alert>
      )}

      {data?.scanned && metricsFailureLikely && (
        <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
          <AlertTriangle className="h-4 w-4 text-red-600" />
          <AlertDescription className="text-sm text-red-700 dark:text-red-400">
            <strong>Nenhum dos {workloads.length} workloads recebeu dado real de uso</strong> (Dynatrace/Prometheus) nesta análise —
            os valores de CPU/Mem, desperdício e "Com Oportunidade" abaixo provavelmente não refletem a realidade.{" "}
            {metricsFailureReason === "structural" ? (
              <>
                As consultas a Dynatrace/Prometheus completaram <strong>sem erro</strong>, mas não retornaram nenhum dado real pra nenhum dos{" "}
                {workloads.length} workloads — é mais provável que este cluster genuinamente não tenha cobertura de monitoramento (sem OneAgent
                Dynatrace instalado / sem Prometheus com as métricas de container) do que uma falha transitória. "Reanalisar agora" não deve resolver
                sozinho — verifique se Dynatrace/Prometheus estão de fato configurados e coletando dados para este cluster.
              </>
            ) : metricsFailureReason === "transient" ? (
              <>
                Pelo menos uma consulta a Dynatrace/Prometheus falhou de verdade durante este scan ({data?.metrics_collection_error}) — é mais
                provável que seja uma falha transitória de coleta (VPN/rede/API indisponível no momento do scan) do que o cluster genuinamente não
                ter desperdício em lugar nenhum. Clique em "Reanalisar agora" em alguns minutos.
              </>
            ) : (
              <>
                É mais provável que seja uma falha de coleta (VPN/rede/API indisponível no momento do scan) do que o cluster genuinamente não ter
                desperdício em lugar nenhum. Clique em "Reanalisar agora" em alguns minutos.
              </>
            )}
          </AlertDescription>
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
          <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
            <SummaryCard icon={DollarSign} label="Desperdício Total" value={fmtBRL(totalWaste)} color="text-red-500" />
            <SummaryCard icon={TrendingDown} label="Workloads Analisados" value={String(workloads.length)} color="text-blue-500" />
            <SummaryCard icon={Layers} label="Node Pools" value={String(data.node_pools?.length ?? 0)} color="text-purple-500" />
            <SummaryCard
              icon={Sparkles}
              label="Com Oportunidade"
              value={String(workloads.filter((w) => (w.waste_brl ?? 0) > 0).length)}
              color="text-amber-500"
            />
            {clusterP95.hasData && (
              <SummaryCard
                icon={Gauge}
                label="P95 Real do Cluster"
                value={`${clusterP95.cpu.toFixed(0)}% CPU`}
                sub={`${clusterP95.mem.toFixed(0)}% Mem — média ponderada por capacidade dos pools`}
                color="text-teal-500"
              />
            )}
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
                    onSelectWorkload={setDetailWorkload}
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
                <WorkloadCard
                  key={`${w.namespace}/${w.workload}`}
                  w={w}
                  node={w.node_name ? nodesByName.get(w.node_name) : undefined}
                  onOpenDetail={setDetailWorkload}
                />
              ))}
            </div>
          )}
        </>
      )}

      {detailWorkload && (
        <WorkloadNodeDetailModal
          cluster={cluster}
          workload={detailWorkload}
          pool={detailWorkload.node_pool ? poolsByName.get(detailWorkload.node_pool) : undefined}
          poolNodes={detailWorkload.node_pool ? (nodesByPool.get(detailWorkload.node_pool) ?? []) : []}
          onClose={() => setDetailWorkload(null)}
        />
      )}
    </div>
  );
}

export default RightsizingTab;
