import { useState, useMemo } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import {
  CheckCircle2,
  AlertCircle,
  XCircle,
  Activity,
  Server,
  Database,
  Settings,
  Loader2,
  Eye,
  Clock,
  ChevronDown,
  ChevronRight,
  ListChecks,
  X,
  Filter,
  AlertTriangle,
  TrendingUp,
  HardDrive,
  Zap,
  GitBranch,
  Users,
  Tag,
  Brain,
  Gauge,
  Radio,
  Unlink,
} from "lucide-react";
import type { HealthCheckResult, DynatraceHealth, Severity } from "@/types/healthcheck";
import { SeverityColors, SeverityBgColors, SeverityLabels } from "@/types/healthcheck";
import { HealthCheckCard } from "@/components/HealthCheckCard";
import { apiClient } from "@/lib/api/client";
import { useToast } from "@/components/ui/use-toast";

interface HealthCheckResultsPanelProps {
  results: HealthCheckResult[];
  isRunning?: boolean;
  runningClusters?: string[];
  onShowProgress?: (cluster: string, result: HealthCheckResult) => void;
  onAddToWhitelist?: (alerts: SelectedAlert[]) => void;
}

// Tipo para alerta selecionado
export interface SelectedAlert {
  namespace: string;
  name: string;
  resource_type: "Deployment" | "ConfigMap" | "Secret";
  message: string;
}

// Card expandido para exibir um problem Dynatrace no Health Check
const DynatraceProblemCard = ({ problem, affectedClusters }: { problem: DynatraceHealth; affectedClusters?: string[] }) => {
  const { toast } = useToast();
  const [analyzing, setAnalyzing] = useState(false);
  const [analysisResult, setAnalysisResult] = useState<string | null>(null);
  const [analysisOpen, setAnalysisOpen] = useState(false);
  const [eventsOpen, setEventsOpen] = useState(false);

  const severityColor = SeverityColors[problem.severity as Severity] || "text-gray-500";
  const severityBg = SeverityBgColors[problem.severity as Severity] || "bg-gray-500/10 border-gray-500/30";
  const severityLabel = SeverityLabels[problem.severity as Severity] || problem.severity;

  const dtSeverityIcon: Record<string, string> = {
    AVAILABILITY: "🔴",
    ERROR: "🟠",
    PERFORMANCE: "🟡",
    RESOURCE_CONTENTION: "🟡",
    CUSTOM_ALERT: "🔵",
  };

  const envColor: Record<string, string> = {
    prd: "bg-red-100 text-red-700 dark:bg-red-950/30 dark:text-red-400",
    hlg: "bg-yellow-100 text-yellow-700 dark:bg-yellow-950/30 dark:text-yellow-400",
    dev: "bg-blue-100 text-blue-700 dark:bg-blue-950/30 dark:text-blue-400",
  };

  const hasMetrics = problem.metrics_summary && Object.keys(problem.metrics_summary).length > 0;
  const hasEvidence = (problem.evidence?.length ?? 0) > 0;
  const hasRecentEvents = (problem.recent_events?.length ?? 0) > 0;
  const noK8sCorrelation = (problem.k8s_workloads?.length ?? 0) === 0;

  const handleAnalyze = async () => {
    const aiEmail = localStorage.getItem("ai_email") ?? "";
    if (!aiEmail) {
      toast({ title: "Configure seu email em Configurações de AI primeiro", variant: "destructive" });
      return;
    }
    setAnalyzing(true);
    try {
      const result = await apiClient.analyzeDynatraceProblem(problem.problem_id, aiEmail);
      setAnalysisResult(result.analysis);
      setAnalysisOpen(true);
    } catch (err) {
      toast({ title: "Falha na análise AI", description: err instanceof Error ? err.message : "Erro desconhecido", variant: "destructive" });
    } finally {
      setAnalyzing(false);
    }
  };

  return (
    <div className={`rounded-lg border p-3 space-y-2 ${severityBg}`}>
      {/* Header: ID + título + severidade + timestamps */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-base leading-none">{dtSeverityIcon[problem.dt_severity] || "⚪"}</span>
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="font-mono text-xs text-muted-foreground">{problem.display_id}</span>
              <Badge variant="outline" className={`text-xs ${severityColor}`}>{severityLabel}</Badge>
              <Badge variant="outline" className="text-xs">{problem.impact_level}</Badge>
              {problem.context_fetched && (
                <Badge variant="outline" className="text-xs text-purple-600 border-purple-400 gap-1">
                  <Brain className="h-2.5 w-2.5" /> Contexto completo
                </Badge>
              )}
              {(affectedClusters?.length ?? 0) > 1 && (
                <Badge variant="outline" className="text-xs text-blue-600 border-blue-400 gap-1" title={affectedClusters!.join(", ")}>
                  <Server className="h-2.5 w-2.5" /> Afeta {affectedClusters!.length} clusters
                </Badge>
              )}
            </div>
            <p className="text-sm font-medium mt-0.5 leading-snug">{problem.title}</p>
          </div>
        </div>
        <span className="text-xs text-muted-foreground whitespace-nowrap">
          {new Date(problem.start_time).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" })}
        </span>
      </div>

      {/* Workloads K8s ou badge de sem correlação */}
      {noK8sCorrelation ? (
        <div className="flex items-center gap-1.5">
          <Unlink className="h-3 w-3 text-muted-foreground shrink-0" />
          <span className="text-xs text-muted-foreground italic">
            Sem correlação K8s — verifique tags OneAgent no Dynatrace
          </span>
        </div>
      ) : (
        <div className="flex items-center gap-1.5 flex-wrap">
          <Server className="h-3 w-3 text-muted-foreground shrink-0" />
          {problem.k8s_workloads!.map((wl, i) => {
            const version = problem.app_versions?.[wl.split("/")[1]] || problem.app_versions?.[wl];
            return (
              <span key={i} className="inline-flex items-center gap-1 text-xs font-mono bg-muted px-1.5 py-0.5 rounded">
                {wl}
                {version && <span className="text-blue-600 dark:text-blue-400">v{version}</span>}
              </span>
            );
          })}
        </div>
      )}

      {/* Métricas resumidas */}
      {hasMetrics && (
        <div className="flex items-center gap-1.5 flex-wrap">
          <Gauge className="h-3 w-3 text-muted-foreground shrink-0" />
          {problem.metrics_summary!.error_rate !== undefined && (
            <Badge variant="outline" className="text-xs text-red-600 border-red-300 gap-1">
              ERR: {problem.metrics_summary!.error_rate.toFixed(1)}%
            </Badge>
          )}
          {problem.metrics_summary!.response_p90_ms !== undefined && (
            <Badge variant="outline" className="text-xs text-orange-600 border-orange-300 gap-1">
              P90: {Math.round(problem.metrics_summary!.response_p90_ms)}ms
            </Badge>
          )}
          {problem.metrics_summary!.response_p99_ms !== undefined && (
            <Badge variant="outline" className="text-xs text-orange-700 border-orange-400 gap-1">
              P99: {Math.round(problem.metrics_summary!.response_p99_ms)}ms
            </Badge>
          )}
          {problem.metrics_summary!.throughput_rpm !== undefined && (
            <Badge variant="outline" className="text-xs gap-1">
              {Math.round(problem.metrics_summary!.throughput_rpm)} req/min
            </Badge>
          )}
        </div>
      )}

      {/* Evidências Davis AI */}
      {hasEvidence && (
        <div className="rounded-md border border-purple-200 dark:border-purple-800 bg-purple-50 dark:bg-purple-950/20 p-2 space-y-1">
          <p className="text-xs font-semibold text-purple-700 dark:text-purple-300 flex items-center gap-1">
            <Brain className="h-3 w-3" /> Davis AI — Evidências
          </p>
          {problem.evidence!.map((ev, i) => (
            <p key={i} className={`text-xs ${ev.startsWith("[Root Cause]") ? "text-red-700 dark:text-red-400 font-medium" : "text-purple-700 dark:text-purple-300"}`}>
              {ev.startsWith("[Root Cause]") ? "🎯 " : "• "}{ev}
            </p>
          ))}
        </div>
      )}

      {/* Eventos recentes (colapsável) */}
      {hasRecentEvents && (
        <Collapsible open={eventsOpen} onOpenChange={setEventsOpen}>
          <CollapsibleTrigger className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors">
            <Radio className="h-3 w-3" />
            Eventos recentes ({problem.recent_events!.length})
            {eventsOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-1 space-y-0.5">
            {problem.recent_events!.map((ev, i) => (
              <p key={i} className="text-xs text-muted-foreground pl-4">• {ev}</p>
            ))}
          </CollapsibleContent>
        </Collapsible>
      )}

      {/* Squads + Journeys + Environments + Repos */}
      <div className="flex items-center gap-2 flex-wrap text-xs">
        {(problem.squads?.length ?? 0) > 0 && (
          <span className="flex items-center gap-1 text-muted-foreground">
            <Users className="h-3 w-3" />
            {problem.squads!.join(", ")}
          </span>
        )}
        {(problem.journeys?.length ?? 0) > 0 && (
          <span className="flex items-center gap-1 text-muted-foreground">
            <Tag className="h-3 w-3" />
            {problem.journeys!.join(", ")}
          </span>
        )}
        {(problem.environments?.length ?? 0) > 0 && (
          <div className="flex items-center gap-1">
            {problem.environments!.map((env, i) => (
              <span key={i} className={`px-1.5 py-0.5 rounded text-xs font-medium ${envColor[env.toLowerCase()] || "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300"}`}>
                {env}
              </span>
            ))}
          </div>
        )}
        {(problem.github_repos?.length ?? 0) > 0 && (
          <span className="flex items-center gap-1 text-muted-foreground">
            <GitBranch className="h-3 w-3" />
            {problem.github_repos!.slice(0, 2).join(", ")}
            {problem.github_repos!.length > 2 && ` +${problem.github_repos!.length - 2}`}
          </span>
        )}
      </div>

      {/* Footer: sugestão + botão Analisar com AI */}
      <div className="flex items-center justify-between gap-2 border-t pt-1.5">
        {problem.suggestions.length > 0 && (
          <p className="text-xs text-muted-foreground flex-1">💡 {problem.suggestions[0]}</p>
        )}
        <Button
          size="sm"
          variant="outline"
          className="h-6 text-xs gap-1 shrink-0 text-purple-600 border-purple-300 hover:bg-purple-50 dark:hover:bg-purple-950/20"
          onClick={handleAnalyze}
          disabled={analyzing}
        >
          {analyzing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Brain className="h-3 w-3" />}
          {analyzing ? "Analisando..." : "Analisar com AI"}
        </Button>
      </div>

      {/* Resultado da análise AI (colapsável) */}
      {analysisResult && (
        <Collapsible open={analysisOpen} onOpenChange={setAnalysisOpen}>
          <CollapsibleTrigger className="flex items-center gap-1.5 text-xs font-medium text-purple-600 dark:text-purple-400 hover:underline">
            <Brain className="h-3 w-3" />
            Análise AI
            {analysisOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="mt-1.5 rounded-md border border-purple-200 dark:border-purple-800 bg-purple-50 dark:bg-purple-950/20 p-2">
              <p className="text-xs text-purple-900 dark:text-purple-100 whitespace-pre-wrap leading-relaxed">{analysisResult}</p>
            </div>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
};

export const HealthCheckResultsPanel = ({
  results,
  isRunning = false,
  runningClusters = [],
  onShowProgress,
  onAddToWhitelist,
}: HealthCheckResultsPanelProps) => {
  const [expandedCluster, setExpandedCluster] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedAlerts, setSelectedAlerts] = useState<Set<string>>(new Set());

  // Fase 4.1: mapa display_id → clusters onde o problem foi visto (dedup cross-cluster)
  const displayIdClusters = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const result of results) {
      for (const problem of result.dynatrace_results ?? []) {
        const existing = map.get(problem.display_id) ?? [];
        if (!existing.includes(result.cluster)) {
          map.set(problem.display_id, [...existing, result.cluster]);
        }
      }
    }
    return map;
  }, [results]);

  // Funções de seleção
  const toggleAlert = (alertKey: string) => {
    setSelectedAlerts((prev) => {
      const next = new Set(prev);
      if (next.has(alertKey)) {
        next.delete(alertKey);
      } else {
        next.add(alertKey);
      }
      return next;
    });
  };

  const cancelSelection = () => {
    setSelectionMode(false);
    setSelectedAlerts(new Set());
  };

  const addToWhitelist = () => {
    if (!onAddToWhitelist || selectedAlerts.size === 0) return;

    // Construir array de alertas a partir das chaves selecionadas
    const alerts: SelectedAlert[] = [];

    results.forEach((result) => {
      result.deployment_results.forEach((dep) => {
        const key = `deployment-${result.cluster}-${dep.namespace}-${dep.name}`;
        if (selectedAlerts.has(key)) {
          alerts.push({
            namespace: dep.namespace,
            name: dep.name,
            resource_type: "Deployment",
            message: dep.message,
          });
        }
      });

      result.config_results.forEach((cfg) => {
        const key = `config-${result.cluster}-${cfg.namespace}-${cfg.name}`;
        if (selectedAlerts.has(key)) {
          alerts.push({
            namespace: cfg.namespace,
            name: cfg.name,
            resource_type: cfg.resource_type === "ConfigMap" ? "ConfigMap" : "Secret",
            message: cfg.message,
          });
        }
      });

      result.service_results.forEach((svc) => {
        const key = `service-${result.cluster}-${svc.namespace}-${svc.name}`;
        if (selectedAlerts.has(key)) {
          // Nota: Services não têm mapeamento direto para filtros atuais
          // Por ora, pular services
        }
      });
    });

    onAddToWhitelist(alerts);
    cancelSelection();
  };

  // Agrupar resultados por cluster (pegar o mais recente)
  const clusterResults = results.reduce((acc, result) => {
    const existing = acc[result.cluster];
    if (!existing || new Date(result.finished_at) > new Date(existing.finished_at)) {
      acc[result.cluster] = result;
    }
    return acc;
  }, {} as Record<string, HealthCheckResult>);

  // Clusters finalizados
  const completedClusters = Object.keys(clusterResults);

  // Clusters em execução (que não estão nos resultados ainda)
  const activeClusters = runningClusters.filter(c => !completedClusters.includes(c));

  // Total de clusters sendo monitorados
  const allClusters = [...new Set([...completedClusters, ...activeClusters])];

  // Se não há resultados E não está rodando, mostrar mensagem vazia
  if (!isRunning && results.length === 0) {
    return (
      <div className="p-8 text-center">
        <Activity className="h-12 w-12 mx-auto text-muted-foreground opacity-50 mb-4" />
        <p className="text-sm text-muted-foreground">Nenhum resultado disponível</p>
      </div>
    );
  }

  // Status badge color
  const getStatusBadge = (status: string) => {
    switch (status) {
      case "healthy":
        return (
          <Badge className="bg-green-600">
            <CheckCircle2 className="mr-1 h-3 w-3" />
            Healthy
          </Badge>
        );
      case "warning":
        return (
          <Badge className="bg-yellow-600">
            <AlertCircle className="mr-1 h-3 w-3" />
            Warning
          </Badge>
        );
      case "critical":
        return (
          <Badge className="bg-red-600">
            <XCircle className="mr-1 h-3 w-3" />
            Critical
          </Badge>
        );
      case "running":
        return (
          <Badge className="bg-blue-600">
            <Loader2 className="mr-1 h-3 w-3 animate-spin" />
            Em Progresso
          </Badge>
        );
      default:
        return (
          <Badge variant="outline">
            <Activity className="mr-1 h-3 w-3" />
            Unknown
          </Badge>
        );
    }
  };

  return (
    <div className="p-4 space-y-4">
      {/* Header com botão de progresso */}
      {isRunning && onShowProgress && (
        <div className="flex items-center justify-between bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
          <div className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin text-blue-600" />
            <span className="text-sm font-medium">
              Health Check em Progresso ({activeClusters.length}/{allClusters.length} clusters)
            </span>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => onShowProgress?.("", results[0])}
            className="gap-2"
          >
            <Eye className="h-4 w-4" />
            Ver Progresso
          </Button>
        </div>
      )}

      {/* Resumo Geral */}
      {allClusters.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="h-5 w-5" />
              Resumo Geral
            </CardTitle>
            <CardDescription>
              {completedClusters.length} de {allClusters.length} cluster(s) analisado(s)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              <div className="text-center p-3 border rounded-lg">
                <div className="text-2xl font-bold">
                  {allClusters.length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Total Clusters</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-green-50 dark:bg-green-950/20">
                <div className="text-2xl font-bold text-green-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "healthy").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Healthy</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-yellow-50 dark:bg-yellow-950/20">
                <div className="text-2xl font-bold text-yellow-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "warning").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Warning</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-red-50 dark:bg-red-950/20">
                <div className="text-2xl font-bold text-red-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "critical").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Critical</div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Clusters em Progresso */}
      {activeClusters.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-semibold flex items-center gap-2">
            <Clock className="h-4 w-4" />
            Em Execução
          </h3>
          {activeClusters.map((cluster) => (
            <Card key={cluster} className="border-blue-200 dark:border-blue-800">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Server className="h-4 w-4" />
                    <CardTitle className="text-sm font-mono">{cluster}</CardTitle>
                  </div>
                  {getStatusBadge("running")}
                </div>
                <CardDescription className="text-xs">
                  Análise em andamento... Clique em "Ver Progresso" para acompanhar
                </CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      )}

      {/* Resultados por Cluster (Colapsáveis) */}
      {completedClusters.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4" />
              Clusters Analisados
            </h3>

            {/* Botões de controle de seleção */}
            <div className="flex items-center gap-2">
              {!selectionMode ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSelectionMode(true)}
                  className="gap-2"
                >
                  <ListChecks className="h-4 w-4" />
                  Selecionar Alertas
                </Button>
              ) : (
                <>
                  <Badge variant="secondary" className="gap-1">
                    {selectedAlerts.size} selecionado(s)
                  </Badge>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={cancelSelection}
                    className="gap-2"
                  >
                    <X className="h-4 w-4" />
                    Cancelar
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={addToWhitelist}
                    disabled={selectedAlerts.size === 0}
                    className="gap-2 bg-blue-600 hover:bg-blue-700"
                  >
                    <Filter className="h-4 w-4" />
                    Adicionar à Whitelist
                  </Button>
                </>
              )}
            </div>
          </div>
          <ScrollArea className="h-[600px]">
            <div className="space-y-3">
              {completedClusters.map((cluster) => {
                const result = clusterResults[cluster];
                const isExpanded = expandedCluster === cluster;

                return (
                  <Collapsible
                    key={cluster}
                    open={isExpanded}
                    onOpenChange={() =>
                      setExpandedCluster(isExpanded ? null : cluster)
                    }
                  >
                    <Card>
                      <CollapsibleTrigger className="w-full">
                        <CardHeader className="pb-3 cursor-pointer hover:bg-muted/50 transition-colors">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              {isExpanded ? (
                                <ChevronDown className="h-4 w-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="h-4 w-4 text-muted-foreground" />
                              )}
                              <Server className="h-4 w-4" />
                              <CardTitle className="text-sm font-mono">{cluster}</CardTitle>
                            </div>
                            {getStatusBadge(result.overall_status)}
                          </div>
                          <CardDescription className="text-xs text-left ml-6">
                            {result.namespace ? `Namespace: ${result.namespace}` : "Todos os namespaces"} •{" "}
                            Tempo: {(result.duration_ms / 1000).toFixed(2)}s
                          </CardDescription>
                        </CardHeader>
                      </CollapsibleTrigger>

                      {/* Métricas Resumidas com Badges Clicáveis */}
                      <CardContent className="pt-0 pb-3">
                        <div className="flex items-center gap-4 ml-6 flex-wrap">
                          {/* Badge Healthy */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-green-500 hover:bg-green-50 dark:hover:bg-green-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <CheckCircle2 className="h-4 w-4 text-green-600" />
                              <span className="text-sm font-medium text-green-700 dark:text-green-500">
                                Healthy
                              </span>
                              {/* Círculo com contador */}
                              {result.healthy_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-green-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.healthy_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Warning */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-yellow-500 hover:bg-yellow-50 dark:hover:bg-yellow-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <AlertCircle className="h-4 w-4 text-yellow-600" />
                              <span className="text-sm font-medium text-yellow-700 dark:text-yellow-500">
                                Warning
                              </span>
                              {result.warning_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-yellow-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.warning_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Critical */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-red-500 hover:bg-red-50 dark:hover:bg-red-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <XCircle className="h-4 w-4 text-red-600" />
                              <span className="text-sm font-medium text-red-700 dark:text-red-500">
                                Critical
                              </span>
                              {result.critical_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-red-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.critical_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Total */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-gray-400 hover:bg-gray-50 dark:hover:bg-gray-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <Activity className="h-4 w-4" />
                              <span className="text-sm font-medium">Total</span>
                              {result.total_checks > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-gray-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.total_checks}
                                </div>
                              )}
                            </div>
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground mt-2 ml-6">
                          💡 Clique nos badges para ver logs detalhados da análise
                        </p>
                      </CardContent>

                      {/* Detalhes Expandidos */}
                      <CollapsibleContent>
                        <CardContent className="pt-0 border-t">
                          <Tabs defaultValue="deployments" className="mt-4">
                            <TabsList className="grid w-full grid-cols-7">
                              <TabsTrigger value="deployments" className="gap-1 text-xs">
                                <Server className="h-3 w-3" />
                                Deploys ({result.deployment_results.length})
                              </TabsTrigger>
                              <TabsTrigger value="services" className="gap-1 text-xs">
                                <Database className="h-3 w-3" />
                                Services ({result.service_results.length})
                              </TabsTrigger>
                              <TabsTrigger value="configs" className="gap-1 text-xs">
                                <Settings className="h-3 w-3" />
                                Configs ({result.config_results.length})
                              </TabsTrigger>
                              <TabsTrigger value="events" className="gap-1 text-xs">
                                <AlertTriangle className="h-3 w-3" />
                                Events ({result.event_results?.length || 0})
                              </TabsTrigger>
                              <TabsTrigger value="hpas" className="gap-1 text-xs">
                                <TrendingUp className="h-3 w-3" />
                                HPAs ({result.hpa_results?.length || 0})
                              </TabsTrigger>
                              <TabsTrigger value="pvcs" className="gap-1 text-xs">
                                <HardDrive className="h-3 w-3" />
                                PVCs ({result.pvc_results?.length || 0})
                              </TabsTrigger>
                              <TabsTrigger value="dynatrace" className="gap-1 text-xs">
                                <Zap className="h-3 w-3 text-purple-500" />
                                <span className="text-purple-600 dark:text-purple-400">
                                  DT ({result.dynatrace_results?.length || 0})
                                </span>
                              </TabsTrigger>
                            </TabsList>

                            <TabsContent value="deployments" className="space-y-2 mt-3">
                              {result.deployment_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum deployment verificado
                                </div>
                              ) : (
                                result.deployment_results.map((deployment, i) => {
                                  const alertKey = `deployment-${result.cluster}-${deployment.namespace}-${deployment.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={deployment}
                                      type="deployment"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="services" className="space-y-2 mt-3">
                              {result.service_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum serviço externo testado
                                </div>
                              ) : (
                                result.service_results.map((service, i) => {
                                  const alertKey = `service-${result.cluster}-${service.namespace}-${service.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={service}
                                      type="service"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="configs" className="space-y-2 mt-3">
                              {result.config_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum ConfigMap/Secret validado
                                </div>
                              ) : (
                                result.config_results.map((config, i) => {
                                  const alertKey = `config-${result.cluster}-${config.namespace}-${config.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={config}
                                      type="config"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="events" className="space-y-2 mt-3">
                              {!result.event_results || result.event_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum evento crítico detectado
                                </div>
                              ) : (
                                result.event_results.map((event, i) => {
                                  const alertKey = `event-${result.cluster}-${event.namespace}-${event.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={event}
                                      type="event"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="hpas" className="space-y-2 mt-3">
                              {!result.hpa_results || result.hpa_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum HPA com problemas detectado
                                </div>
                              ) : (
                                result.hpa_results.map((hpa, i) => {
                                  const alertKey = `hpa-${result.cluster}-${hpa.namespace}-${hpa.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={hpa}
                                      type="hpa"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="pvcs" className="space-y-2 mt-3">
                              {!result.pvc_results || result.pvc_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum PVC com problemas detectado
                                </div>
                              ) : (
                                result.pvc_results.map((pvc, i) => {
                                  const alertKey = `pvc-${result.cluster}-${pvc.namespace}-${pvc.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={pvc}
                                      type="pvc"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="dynatrace" className="space-y-2 mt-3">
                              {!result.dynatrace_results || result.dynatrace_results.length === 0 ? (
                                <div className="text-center py-6 text-xs text-muted-foreground space-y-2">
                                  <Zap className="h-8 w-8 mx-auto opacity-30 text-purple-500" />
                                  <p>Nenhum problem Dynatrace correlacionado com este cluster</p>
                                  <p className="text-[10px]">Ative "Problems Dynatrace" nas opções e certifique-se de que o token DT está configurado nas Configurações de AI</p>
                                </div>
                              ) : (
                                <div className="space-y-2">
                                  <div className="flex items-center gap-2 text-xs text-muted-foreground pb-1">
                                    <Zap className="h-3.5 w-3.5 text-purple-500" />
                                    <span>{result.dynatrace_results.length} problem(s) Dynatrace correlacionado(s) com workloads deste cluster</span>
                                  </div>
                                  {result.dynatrace_results.map((problem, i) => (
                                    <DynatraceProblemCard
                                      key={i}
                                      problem={problem}
                                      affectedClusters={displayIdClusters.get(problem.display_id)}
                                    />
                                  ))}
                                </div>
                              )}
                            </TabsContent>
                          </Tabs>
                        </CardContent>
                      </CollapsibleContent>
                    </Card>
                  </Collapsible>
                );
              })}
            </div>
          </ScrollArea>
        </div>
      )}
    </div>
  );
};
