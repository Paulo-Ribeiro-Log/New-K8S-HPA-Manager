// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — este arquivo tinha 4589 linhas num único componente
// gigante. Extraído em internal/web/frontend/src/components/finops/ (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx): types.ts (interfaces compartilhadas), helpers.ts
// (buildRecommendation/financeProviderInfo/metricsCollectionLikelyFailed) e um arquivo por aba
// (DashboardTab/NodePoolsTab/WorkloadsTab/HPAHistoryTab/StorageTab/OpportunitiesTab/
// RelatorioTab.tsx) — Rightsizing já era arquivo próprio desde antes (RightsizingTab.tsx). Este
// arquivo agora só orquestra: fetch do relatório principal + a barra de abas.
import { useState, useCallback, useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertTriangle, Loader2, RefreshCw, Server, CircleDollarSign,
  ChevronDown, ChevronUp, Download, Brain, Activity, Check, ChevronsUpDown, X,
} from "lucide-react";
import { toast } from "sonner";
import { useClusters } from "@/hooks/useAPI";
import { RightsizingTab, RightsizingTabBadge } from "@/components/RightsizingTab";
import type { FinOpsReport } from "./finops/types";
import { financeProviderInfo, metricsCollectionLikelyFailed, metricsFailureReason } from "./finops/helpers";
import { DashboardTab } from "./finops/DashboardTab";
import { NodePoolsTab } from "./finops/NodePoolsTab";
import { WorkloadsTab } from "./finops/WorkloadsTab";
import { HPAHistoryTab } from "./finops/HPAHistoryTab";
import { StorageTab } from "./finops/StorageTab";
import { OpportunitiesTab } from "./finops/OpportunitiesTab";
import { RelatorioTab } from "./finops/RelatorioTab";
import { UnattachedDisksTab } from "./finops/UnattachedDisksTab";
import { DataResourcesPanel } from "./DataResourcesPanel";

export const FinOpsTab = ({ selectedCluster }: { selectedCluster?: string }) => {
  const { clusters } = useClusters();

  // Usar o contexto real do kubeconfig (campo `context`) — cada máquina tem seu próprio
  // padrão de nomenclatura (com ou sem sufixo -admin). Não forçar sufixo no frontend.
  const clusterOptions = (clusters ?? [])
    .map(c => c.context)
    .filter((v, i, a) => !!v && a.indexOf(v) === i);

  const defaultCluster = selectedCluster
    ? ((clusters ?? []).find(c => c.context === selectedCluster || c.name === selectedCluster.replace(/-admin$/, ""))?.context ?? selectedCluster)
    : clusterOptions[0] ?? "";

  const [cluster, setCluster] = useState(defaultCluster);
  const [clusterOpen, setClusterOpen] = useState(false);
  const [withPrometheus, setWithPrometheus] = useState(true);
  const [windowDays, setWindowDays] = useState(30);
  // O queryFn lê a janela por ref: "Reanalisar com 7 dias" (banner de timeout) precisa disparar o
  // refetch NA MESMA chamada em que muda a janela — via state ela só valeria no próximo render.
  const windowDaysRef = useRef(windowDays);
  windowDaysRef.current = windowDays;
  const [aiAnalysis, setAiAnalysis] = useState<string | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiExpanded, setAiExpanded] = useState(true);
  const aiAbortRef = useRef<AbortController | null>(null);

  // Estado persistente da aba HPA Histórico (sobrevive a troca de tabs)
  const [hpaHistoryDays, setHpaHistoryDays] = useState(30);

  // Sub-aba ativa. Controlada (não defaultValue) porque "Discos Desatachados" não depende do
  // relatório principal — a barra de abas precisa existir mesmo sem "Analisar" ter rodado.
  const [subTab, setSubTab] = useState("dashboard");

  const queryClient = useQueryClient();

  // Bug real corrigido, relatado pelo usuário: "o botão analisar... não funciona, impedindo de
  // executar novas análises no mesmo cluster" — `isLoading` do React Query v5 é
  // `isPending && isFetching`, e `isPending` vira `false` pra sempre assim que a query tem
  // sucesso UMA vez (não volta a `true` num refetch manual, mesmo com dado antigo em tela). O
  // botão "Analisar"/spinner/gate do relatório usavam `isLoading` — então, depois do 1º scan
  // bem-sucedido, clicar "Analisar" de novo chamava `refetch()` normalmente (o clique em si
  // funcionava), mas a UI inteira continuava achando que nada estava acontecendo: sem spinner,
  // sem troca pra "Cancelar", e o relatório VELHO continuava exibido por cima (gate era
  // `report && !isLoading`, sempre true durante o refetch) — um scan de ~2min rodando de verdade
  // no fundo, mas com zero sinal visual, indistinguível de "o botão não fez nada". `isFetching`
  // (true em QUALQUER fetch, inicial ou refetch) substitui `isLoading` em todo lugar abaixo.
  const { data: report, isFetching, error, refetch } = useQuery<FinOpsReport>({
    queryKey: ["finops-report", cluster],
    queryFn: async ({ signal }) => {
      let url = `/api/v1/finops/report?cluster=${encodeURIComponent(cluster)}`;
      if (withPrometheus) {
        // persist_rightsizing=true: o MESMO relatório que esta chamada já constrói (Dynatrace/
        // Prometheus/K8s/storage) também alimenta a aba Rightsizing (ver RightsizingTab.tsx) — o
        // backend persiste as tabelas de rightsizing como efeito colateral, sem nenhum re-scan.
        // Bug real corrigido, relatado pelo usuário ("o que me leva a crer que está fazendo o
        // mesmo scan 2 vezes" — cada "Analisar" chegou a levar ~2min + mais ~2min de rightsizing
        // logo em seguida, porque a versão anterior disparava um 2º scan completo do zero).
        url += `&with_prometheus=true&window_days=${windowDaysRef.current}&persist_rightsizing=true`;
      }
      const r = await fetch(url, {
        signal,
        headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      return r.json();
    },
    enabled: false,        // nunca re-fetcha automaticamente ao montar
    staleTime: Infinity,   // cache permanece válido indefinidamente
    retry: false,
  });

  // Restaura o último scan já feito (sem NUNCA disparar um re-scan) sempre que o cluster muda ou
  // a aba monta — bug real corrigido, relatado pelo usuário: "sempre que chamamos a aba finops,
  // ela vem vazia só com as seleções de cluster e os botões... ajuste para que venha com a
  // exibição do último scan". Antes, `enabled: false` acima significava que NADA aparecia até um
  // clique manual em "Analisar" — mesmo que o cluster já tivesse sido analisado minutos antes,
  // bastava trocar de aba (desmontando este componente) ou recarregar a página pra perder tudo.
  // GET /finops/report/last só lê um cache já persistido no backend (SQLite) — nunca consulta
  // Dynatrace/Prometheus/K8s. Só busca se a queryKey ainda não tiver dado (evita sobrescrever um
  // relatório recém-buscado ao vivo nesta mesma sessão, e evita rebuscar à toa).
  useEffect(() => {
    if (!cluster) return;
    if (queryClient.getQueryData(["finops-report", cluster])) return;
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch(`/api/v1/finops/report/last?cluster=${encodeURIComponent(cluster)}`, {
          headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
        });
        if (!r.ok || cancelled) return;
        const data = await r.json();
        if (cancelled || data?.scanned === false) return;
        queryClient.setQueryData(["finops-report", cluster], data);
      } catch {
        // best-effort e silencioso — sem cache, a tela simplesmente fica no estado vazio já
        // existente ("Selecione um cluster e clique em Analisar"), nunca um erro visível.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [cluster, queryClient]);

  // Depois que o relatório principal (já persistindo rightsizing como efeito colateral, acima)
  // termina, só invalida a query de leitura (["finops-rightsizing", cluster], mesma chave que
  // RightsizingTab.tsx observa) — invalidateQueries refaz automaticamente o fetch se a aba
  // Rightsizing estiver aberta no momento (React Query só refetcha queries ativas/montadas) e
  // marca como stale pra quando o usuário for lá depois; como GET /rightsizing só lê do SQLite
  // (rápido, sem Prometheus/Dynatrace), isso nunca reintroduz o custo do 2º scan completo.
  const refreshRightsizingCache = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["finops-rightsizing", cluster] });
  }, [queryClient, cluster]);

  const exportCSV = () => {
    if (!report) return;
    const hasP95 = report.workloads.some(w => (w.cpu_p95_millis ?? 0) > 0);
    const header = [
      "Namespace", "Workload", "Pods",
      "CPU Request (m)", "Mem Request (Mi)",
      ...(hasP95 ? ["CPU P95 (m)", "Mem P95 (Mi)", "Desperdício R$/mês"] : []),
      "Custo R$/mês", "HPA Min", "HPA Atual", "HPA Max",
      "Custo HPA Min R$", "Custo HPA Max R$", "Veredicto",
    ].join(",");
    const rows = report.workloads.map(w =>
      [w.namespace, w.workload, w.pods,
       Math.round(w.cpu_request_millis), Math.round(w.mem_request_mi),
       ...(hasP95 ? [Math.round(w.cpu_p95_millis ?? 0), Math.round(w.mem_p95_mi ?? 0), (w.waste_brl ?? 0).toFixed(2)] : []),
       w.cost_share_brl.toFixed(2),
       w.hpa_min, w.hpa_current, w.hpa_max,
       w.hpa_cost_min_brl.toFixed(2), w.hpa_cost_max_brl.toFixed(2),
       w.verdict,
      ].join(",")
    );
    const csv = [header, ...rows].join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `finops-${report.cluster.replace("-admin", "")}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("CSV exportado com sucesso");
  };

  // Reanálise com outra janela (usada pelo banner de timeout do Prometheus).
  const reanalyzeWithWindow = async (days: number) => {
    windowDaysRef.current = days;
    setWindowDays(days);
    setAiAnalysis(null);
    const result = await refetch();
    if (result.isSuccess) {
      refreshRightsizingCache();
    }
  };

  const analyzeWithAI = async () => {
    // Se já está carregando, cancela
    if (aiLoading) {
      aiAbortRef.current?.abort();
      setAiLoading(false);
      toast.info("Análise AI cancelada");
      return;
    }
    if (!report) return;
    const aiEmail = localStorage.getItem("ai_email") ?? "";
    if (!aiEmail) {
      toast.error("Configure seu e-mail de AI em Configurações → AI Settings");
      return;
    }
    aiAbortRef.current = new AbortController();
    setAiLoading(true);
    setAiAnalysis(null);
    try {
      const r = await fetch("/api/v1/finops/analyze", {
        signal: aiAbortRef.current.signal,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
        body: JSON.stringify({ ai_email: aiEmail, report }),
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        throw new Error((err as { error?: string }).error ?? `Erro ${r.status}`);
      }
      const data = await r.json();
      setAiAnalysis(data.analysis);
      setAiExpanded(true);
      toast.success("Análise AI concluída");
    } catch (err) {
      if ((err as Error).name === "AbortError") return; // cancelado pelo usuário
      toast.error("Falha na análise AI: " + (err as Error).message);
    } finally {
      setAiLoading(false);
    }
  };

  return (
    <div className="flex flex-col h-full p-4 gap-4 overflow-auto">
      {/* Header */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-lg font-semibold flex items-center gap-2">
            <CircleDollarSign className="h-5 w-5 text-blue-500" />
            FinOps — Análise de Custo{cluster ? ` ${financeProviderInfo(cluster).label}` : ""}
          </h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            {cluster
              ? `Custo real baseado na ${financeProviderInfo(cluster).source}`
              : "Selecione um cluster para começar"}
          </p>
        </div>
        <div className="flex flex-col gap-2 items-end">
          <div className="flex items-center gap-2 flex-wrap justify-end">
            <Popover open={clusterOpen} onOpenChange={setClusterOpen}>
              <PopoverTrigger asChild>
                <Button variant="outline" role="combobox" aria-expanded={clusterOpen}
                  className="w-64 h-8 text-sm justify-between font-normal">
                  <span className="truncate">
                    {cluster ? cluster.replace("-admin", "") : "Selecionar cluster..."}
                  </span>
                  <ChevronsUpDown className="ml-2 h-3.5 w-3.5 shrink-0 opacity-50" />
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-64 p-0" align="start">
                <Command>
                  <CommandInput placeholder="Buscar cluster..." className="h-8 text-sm" />
                  <CommandList>
                    <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                    <CommandGroup>
                      {clusterOptions.map(c => (
                        <CommandItem key={c} value={c.replace("-admin", "")}
                          onSelect={() => { setCluster(c); setClusterOpen(false); }}>
                          <Check className={`mr-2 h-3.5 w-3.5 ${cluster === c ? "opacity-100" : "opacity-0"}`} />
                          {c.replace("-admin", "")}
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
            <Button size="sm" variant={isFetching ? "destructive" : "outline"} className="h-8 gap-1"
              onClick={async () => {
                if (isFetching) {
                  queryClient.cancelQueries({ queryKey: ["finops-report", cluster] });
                } else {
                  setAiAnalysis(null);
                  const result = await refetch();
                  if (result.isSuccess) {
                    refreshRightsizingCache();
                  }
                }
              }}>
              {isFetching
                ? <X className="h-3.5 w-3.5" />
                : <RefreshCw className="h-3.5 w-3.5" />}
              {isFetching ? "Cancelar" : "Analisar"}
            </Button>
            {report && (
              <>
                <Button size="sm" variant="outline" className="h-8 gap-1" onClick={exportCSV}>
                  <Download className="h-3.5 w-3.5" />
                  CSV
                </Button>
                <Button size="sm" variant={aiLoading ? "destructive" : "outline"} className="h-8 gap-1"
                  onClick={analyzeWithAI}>
                  {aiLoading
                    ? <X className="h-3.5 w-3.5" />
                    : <Brain className="h-3.5 w-3.5" />}
                  {aiLoading ? "Cancelar AI" : "Analisar com AI"}
                </Button>
              </>
            )}
          </div>
          {/* Toggle análise histórica Prometheus */}
          <div className="flex items-center gap-2 flex-wrap justify-end">
            <label className="flex items-center gap-1.5 cursor-pointer select-none text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={withPrometheus}
                onChange={e => setWithPrometheus(e.target.checked)}
                className="h-3.5 w-3.5 cursor-pointer"
              />
              <Activity className="h-3 w-3" />
              <span title="Fonte primária: Dynatrace (quando configurado). Fallback: Prometheus">Análise histórica</span>
            </label>
            {withPrometheus && (
              <Select value={String(windowDays)} onValueChange={v => setWindowDays(Number(v))}>
                <SelectTrigger className="h-7 w-24 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[7, 14, 30].map(d => (
                    <SelectItem key={d} value={String(d)}>{d} dias</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
        </div>
      </div>

      {/* Estados */}
      {isFetching && (
        <div className="flex-1 flex items-center justify-center gap-3 text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
          <span>Coletando dados do cluster e preços Azure...</span>
        </div>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>
            {(error as Error).message}
            {(error as Error).message.includes("scan") && (
              <span className="block mt-1 text-xs">
                Acesse a aba <strong>Dynatrace → Node Pools</strong> e clique em "Escanear Clusters" para popular o registry.
              </span>
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* Relatório */}
      {report && !isFetching && (
        <>
          {metricsCollectionLikelyFailed(report.summary) && (() => {
            const reason = metricsFailureReason(report.summary);
            return (
              <Alert className="border-red-200 bg-red-50 dark:bg-red-950/20">
                <AlertTriangle className="h-4 w-4 text-red-600" />
                <AlertDescription className="text-sm text-red-700 dark:text-red-400">
                  <strong>Nenhum dos {report.summary.workloads_analyzed} workloads recebeu dado real de uso</strong> (Dynatrace/Prometheus) nesta análise —
                  os valores de desperdício, CPU/Mem e "Com Oportunidade" abaixo (e na aba Rightsizing) provavelmente não refletem a realidade.{" "}
                  {reason === "timeout" ? (
                    <>
                      As consultas <strong>pesadas</strong> de CPU/memória ao Prometheus (janela de {report.window_days || windowDays} dias sobre todos os pods
                      do cluster) <strong>estouraram o tempo limite</strong>, mas as consultas leves de HPA responderam — ou seja, o Prometheus está no ar e a
                      rede/VPN <strong>não</strong> é o problema. Reanalisar na mesma janela tende a falhar de novo: use uma janela menor.
                      <span className="block mt-2">
                        {[7, 14].filter(d => d < (report.window_days || windowDays)).map(d => (
                          <Button key={d} size="sm" variant="outline" className="h-7 mr-2 gap-1 border-red-300 text-red-700 hover:bg-red-100"
                            onClick={() => void reanalyzeWithWindow(d)}>
                            <RefreshCw className="h-3 w-3" /> Reanalisar com {d} dias
                          </Button>
                        ))}
                      </span>
                    </>
                  ) : reason === "structural" ? (
                    <>
                      As consultas a Dynatrace/Prometheus completaram <strong>sem erro</strong>, mas não retornaram nenhum dado real pra nenhum dos{" "}
                      {report.summary.workloads_analyzed} workloads — é mais provável que este cluster genuinamente não tenha cobertura de monitoramento
                      (sem OneAgent Dynatrace instalado / sem Prometheus com as métricas de container) do que uma falha transitória. "Reanalisar" não deve
                      resolver sozinho — verifique se Dynatrace/Prometheus estão de fato configurados e coletando dados para este cluster.
                    </>
                  ) : reason === "transient" ? (
                    <>
                      Pelo menos uma consulta a Dynatrace/Prometheus falhou de verdade durante este scan ({report.summary.metrics_collection_error}) —
                      é mais provável que seja uma falha transitória de coleta (VPN/rede/API indisponível no momento do scan) do que o cluster genuinamente
                      não ter desperdício em lugar nenhum. Reanalise em alguns minutos.
                    </>
                  ) : (
                    <>
                      É mais provável que seja uma falha de coleta (VPN/rede/API indisponível no momento do scan) do que o cluster genuinamente não ter
                      desperdício em lugar nenhum. Reanalise em alguns minutos; se persistir, verifique a conectividade com Prometheus/Dynatrace.
                    </>
                  )}
                </AlertDescription>
              </Alert>
            );
          })()}
          <div className="flex items-center gap-2 text-xs text-muted-foreground -mb-1 flex-wrap">
            <Server className="h-3.5 w-3.5 shrink-0" />
            <span>
              {report.cluster.replace("-admin", "")} · gerado em {new Date(report.generated_at).toLocaleTimeString("pt-BR")}
              {" · "}câmbio USD/BRL: <strong>R$ {report.exchange_rate.toFixed(4)}</strong> ({report.exchange_date})
              {" · "}{(report.node_pools ?? []).length} node pools · {report.summary.workloads_analyzed} workloads
            </span>
            {!withPrometheus && report.window_days === 0 && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400 border border-amber-300 dark:border-amber-700">
                <AlertTriangle className="h-3 w-3" />
                Sem Prometheus — saving estimado por HPA config apenas
              </span>
            )}
          </div>

          {/* Resultado AI */}
          {aiAnalysis && (
            <Card className="border-blue-200 dark:border-blue-800">
              <CardHeader className="py-2 px-4 cursor-pointer" onClick={() => setAiExpanded(e => !e)}>
                <CardTitle className="text-sm flex items-center justify-between">
                  <span className="flex items-center gap-2">
                    <Brain className="h-4 w-4 text-blue-500" />
                    Análise AI — Recomendações FinOps
                  </span>
                  {aiExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                </CardTitle>
              </CardHeader>
              {aiExpanded && (
                <CardContent className="px-4 pb-4">
                  <pre className="text-xs whitespace-pre-wrap font-sans leading-relaxed text-foreground">
                    {aiAnalysis}
                  </pre>
                </CardContent>
              )}
            </Card>
          )}

        </>
      )}

      {/* Abas: as que dependem do relatório só aparecem com ele; "Discos Desatachados" consulta o
          cloud direto e fica disponível assim que há um cluster selecionado. */}
      {cluster && !isFetching && (
        <>
          {!report && !error && (
            <p className="text-xs text-muted-foreground">
              Clique em <strong>Analisar</strong> para liberar Dashboard, Node Pools, Workloads e as demais abas —
              Discos Desatachados já está disponível abaixo.
            </p>
          )}
          <Tabs value={report || subTab === "data" ? subTab : "disks"} onValueChange={setSubTab} className="flex-1 flex flex-col min-h-0">
            <TabsList className="w-fit">
              {report && (<>
              <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
              <TabsTrigger value="nodepools">
                Node Pools
                <Badge variant="secondary" className="ml-1 text-[10px]">{(report.node_pools ?? []).length}</Badge>
              </TabsTrigger>
              <TabsTrigger value="workloads">
                Workloads
                <Badge variant="secondary" className="ml-1 text-[10px]">{report.summary.workloads_analyzed}</Badge>
              </TabsTrigger>
              <TabsTrigger value="hpa-history">
                HPA {report.window_days > 0 ? `${report.window_days}d` : "Histórico"}
                {report.summary.hpa_removable_count > 0 && (
                  <Badge className="ml-1 text-[10px] bg-purple-600">{report.summary.hpa_removable_count}</Badge>
                )}
              </TabsTrigger>
              {report.storage && (
                <TabsTrigger value="storage">
                  Armazenamento
                  {(report.storage.orphaned_pvc_count ?? 0) > 0 && (
                    <Badge variant="destructive" className="ml-1 text-[10px]">{report.storage.orphaned_pvc_count}</Badge>
                  )}
                </TabsTrigger>
              )}
              <TabsTrigger value="opportunities">
                Oportunidades
                {(report.summary.superprovisioned_count + report.summary.oom_risk_count + (report.summary.fixed_high_cost_count ?? 0)) > 0 && (
                  <Badge variant="destructive" className="ml-1 text-[10px]">
                    {report.summary.superprovisioned_count + report.summary.oom_risk_count + (report.summary.fixed_high_cost_count ?? 0)}
                  </Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="report">
                Relatório
                {(report.summary.superprovisioned_count + report.summary.oom_risk_count + (report.storage?.orphaned_pvc_count ?? 0)) > 0 && (
                  <Badge className="ml-1 text-[10px] bg-orange-500">
                    {report.summary.superprovisioned_count + report.summary.oom_risk_count + (report.storage?.orphaned_pvc_count ?? 0)}
                  </Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="rightsizing">
                Rightsizing
                <RightsizingTabBadge cluster={cluster} />
              </TabsTrigger>
              </>)}
              <TabsTrigger value="data">Recursos de Dados</TabsTrigger>
              <TabsTrigger value="disks">Discos Desatachados</TabsTrigger>
            </TabsList>

            <div className="flex-1 overflow-auto mt-3">
              {report && (<>
              <TabsContent value="dashboard" className="mt-0 h-full">
                <DashboardTab cluster={cluster} report={report} />
              </TabsContent>
              <TabsContent value="nodepools" className="mt-0 h-full">
                <NodePoolsTab pools={report.node_pools ?? []} workloads={report.workloads ?? []} cluster={cluster} />
              </TabsContent>
              <TabsContent value="workloads" className="mt-0 h-full">
                <WorkloadsTab workloads={report.workloads ?? []} windowDays={report.window_days || windowDays} />
              </TabsContent>
              <TabsContent value="hpa-history" className="mt-0 h-full">
                <HPAHistoryTab cluster={cluster} days={hpaHistoryDays} setDays={setHpaHistoryDays} />
              </TabsContent>
              {report.storage && (
                <TabsContent value="storage" className="mt-0 h-full">
                  <StorageTab cluster={cluster} pvcs={report.pvcs ?? []} storage={report.storage} />
                </TabsContent>
              )}
              <TabsContent value="opportunities" className="mt-0 h-full">
                <OpportunitiesTab workloads={report.workloads ?? []} summary={report.summary} windowDays={report.window_days || windowDays} />
              </TabsContent>
              <TabsContent value="report" className="mt-0 h-full">
                <RelatorioTab report={report} windowDays={report.window_days || windowDays} cluster={cluster} />
              </TabsContent>
              <TabsContent value="rightsizing" className="mt-0 h-full">
                <RightsizingTab cluster={cluster} />
              </TabsContent>
              </>)}
              <TabsContent value="data" className="mt-0 h-full">
                <DataResourcesPanel cluster={cluster} />
              </TabsContent>
              <TabsContent value="disks" className="mt-0 h-full">
                <UnattachedDisksTab cluster={cluster} />
              </TabsContent>
            </div>
          </Tabs>
        </>
      )}

      {!report && !isFetching && !error && !cluster && (
        <div className="flex-1 flex items-center justify-center flex-col gap-3 text-muted-foreground">
          <CircleDollarSign className="h-12 w-12 opacity-20" />
          <p>Selecione um cluster e clique em <strong>Analisar</strong></p>
        </div>
      )}
    </div>
  );
};
