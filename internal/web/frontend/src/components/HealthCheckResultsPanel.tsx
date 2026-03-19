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
import type { HealthCheckResult, DynatraceHealth, Severity, CorrelatedHealthItem, CorrelatedK8sIssue, OneAgentSignal } from "@/types/healthcheck";
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

// Badge de fonte do item correlacionado
const CorrelationSourceBadge = ({ item }: { item: CorrelatedHealthItem }) => {
  const hasK8s = (item.k8s_issues?.length ?? 0) > 0;
  const hasDT = (item.dt_problems?.length ?? 0) > 0;
  if (item.correlated) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-violet-500/15 border border-violet-500/30 text-violet-700 dark:text-violet-300 text-[10px] font-semibold px-2 py-0.5">
        <Zap className="h-2.5 w-2.5" />
        K8s + DT
      </span>
    );
  }
  if (hasK8s) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-blue-500/15 border border-blue-500/30 text-blue-700 dark:text-blue-300 text-[10px] font-semibold px-2 py-0.5">
        <Server className="h-2.5 w-2.5" />
        K8s only
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-orange-500/15 border border-orange-500/30 text-orange-700 dark:text-orange-300 text-[10px] font-semibold px-2 py-0.5">
      <Zap className="h-2.5 w-2.5" />
      DT only
    </span>
  );
};

// Card de item correlacionado K8s ↔ Dynatrace
const CorrelatedItemCard = ({ item }: { item: CorrelatedHealthItem }) => {
  const { toast } = useToast();
  const [open, setOpen] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [analysisResult, setAnalysisResult] = useState<string | null>(item.ai_analysis ?? null);
  const [analysisOpen, setAnalysisOpen] = useState(false);
  const severityColor = SeverityColors[item.final_severity] || "text-gray-500";
  const severityBg = SeverityBgColors[item.final_severity] || "bg-gray-500/10 border-gray-500/30";
  const severityLabel = SeverityLabels[item.final_severity] || item.final_severity;

  const k8sIssues = item.k8s_issues ?? [];
  const dtProblems = item.dt_problems ?? [];

  const handleAnalyze = async () => {
    const aiEmail = localStorage.getItem("ai_email") ?? "";
    if (!aiEmail) {
      toast({ title: "Configure seu e-mail em Configurações de AI", variant: "destructive" });
      return;
    }
    setAnalyzing(true);
    try {
      const result = await apiClient.analyzeCorrelatedItem(item, aiEmail);
      setAnalysisResult(result.analysis);
      setAnalysisOpen(true);
    } catch (err) {
      toast({ title: "Falha na análise AI", description: err instanceof Error ? err.message : "Erro desconhecido", variant: "destructive" });
    } finally {
      setAnalyzing(false);
    }
  };

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="w-full text-left">
        <div className={`rounded-lg border p-3 hover:brightness-95 transition-all ${severityBg} ${item.correlated ? "ring-1 ring-violet-500/30" : ""}`}>
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              {open ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              <div className="min-w-0">
                <p className={`text-xs font-semibold truncate ${severityColor}`}>
                  {item.workload_name}
                </p>
                <p className="text-[10px] text-muted-foreground">{item.namespace}</p>
              </div>
            </div>
            <div className="flex items-center gap-1.5 shrink-0">
              <CorrelationSourceBadge item={item} />
              <Badge variant="outline" className={`text-[10px] ${severityColor}`}>
                {severityLabel}
              </Badge>
            </div>
          </div>
        </div>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className={`rounded-b-lg border border-t-0 px-3 pb-3 pt-2 space-y-3 ${severityBg}`}>
          {/* K8s issues */}
          {k8sIssues.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-[10px] font-semibold text-blue-600 dark:text-blue-400 flex items-center gap-1">
                <Server className="h-3 w-3" /> Sintomas K8s ({k8sIssues.length})
              </p>
              {k8sIssues.map((issue, i) => (
                <div key={i} className="rounded border border-blue-200 dark:border-blue-800 bg-blue-50/50 dark:bg-blue-950/20 px-2 py-1.5">
                  <div className="flex items-center gap-1.5 mb-0.5">
                    <Badge variant="outline" className="text-[9px] text-blue-600">{issue.resource_kind}</Badge>
                    <span className="text-[10px] font-medium">{issue.resource_name}</span>
                  </div>
                  <p className="text-[10px] text-muted-foreground">{issue.message}</p>
                </div>
              ))}
            </div>
          )}

          {/* DT problems */}
          {dtProblems.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-[10px] font-semibold text-orange-600 dark:text-orange-400 flex items-center gap-1">
                <Zap className="h-3 w-3" /> Problems Dynatrace ({dtProblems.length})
              </p>
              {dtProblems.map((p, i) => (
                <div key={i} className="rounded border border-orange-200 dark:border-orange-800 bg-orange-50/50 dark:bg-orange-950/20 px-2 py-1.5">
                  <div className="flex items-center gap-1.5 mb-0.5">
                    <Badge variant="outline" className="text-[9px] text-orange-600">{p.display_id}</Badge>
                    <span className="text-[10px] font-medium truncate">{p.title}</span>
                  </div>
                  <p className="text-[10px] text-muted-foreground">{p.dt_severity} · {p.impact_level}</p>
                  {p.evidence && p.evidence.length > 0 && (
                    <div className="mt-1 space-y-0.5">
                      {p.evidence.slice(0, 2).map((ev, j) => (
                        <p key={j} className="text-[10px] text-violet-700 dark:text-violet-300">🧠 {ev}</p>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Botão Analisar com AI */}
          <div className="flex justify-end pt-1 border-t">
            <Button
              size="sm"
              variant="outline"
              onClick={handleAnalyze}
              disabled={analyzing}
              className="h-6 text-xs gap-1 text-violet-600 border-violet-300 hover:bg-violet-50 dark:hover:bg-violet-950/20"
            >
              {analyzing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Brain className="h-3 w-3" />}
              {analyzing ? "Analisando..." : "Analisar com AI"}
            </Button>
          </div>

          {/* Resultado da análise AI */}
          {analysisResult && (
            <Collapsible open={analysisOpen} onOpenChange={setAnalysisOpen}>
              <CollapsibleTrigger className="flex items-center gap-1.5 text-xs font-medium text-violet-600 dark:text-violet-400 hover:underline">
                <Brain className="h-3 w-3" />
                Análise AI
                {analysisOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="mt-1.5 rounded-md border border-violet-200 dark:border-violet-800 bg-violet-50 dark:bg-violet-950/20 p-2">
                  <p className="text-xs text-violet-900 dark:text-violet-100 whitespace-pre-wrap leading-relaxed">{analysisResult}</p>
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
};

// Card individual para sinal OneAgent
const OneAgentSignalCard = ({ signal, aiEmail }: { signal: OneAgentSignal; aiEmail?: string }) => {
  const { toast } = useToast();
  const [open, setOpen] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [analysis, setAnalysis] = useState<string | null>(signal.ai_analysis ?? null);

  const riskColor = signal.risk_level === "critical" ? "text-red-600 dark:text-red-400"
    : signal.risk_level === "high" ? "text-orange-600 dark:text-orange-400"
    : signal.risk_level === "medium" ? "text-yellow-600 dark:text-yellow-400"
    : "text-blue-500";

  const riskBg = signal.risk_level === "critical" ? "border-red-200 dark:border-red-800"
    : signal.risk_level === "high" ? "border-orange-200 dark:border-orange-800"
    : signal.risk_level === "medium" ? "border-yellow-200 dark:border-yellow-800"
    : "border-blue-200 dark:border-blue-800";

  const handleAnalyze = async () => {
    const email = aiEmail || localStorage.getItem("ai_email") || "";
    if (!email) {
      toast({ title: "Configure seu e-mail em Configurações de AI primeiro", variant: "destructive" });
      return;
    }
    setAnalyzing(true);
    try {
      const res = await apiClient.analyzeOneAgentSignal(signal, email);
      setAnalysis(res.analysis);
      setOpen(true);
    } catch (e: any) {
      toast({ title: "Erro na análise AI", description: e.message, variant: "destructive" });
    } finally {
      setAnalyzing(false);
    }
  };

  return (
    <div className={`border rounded-md p-3 space-y-2 ${riskBg}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className={`font-semibold text-sm ${riskColor}`}>{signal.namespace}/{signal.workload_name}</span>
            {signal.entity_type && <Badge variant="outline" className="text-[10px] px-1 py-0">{signal.entity_type}</Badge>}
            {signal.from_k8s_issue && (
              <Badge variant="outline" className="text-[10px] px-1 py-0 border-amber-400 text-amber-600 dark:text-amber-400">K8s Issue</Badge>
            )}
            {signal.has_dt_problem && (
              <Badge variant="outline" className="text-[10px] px-1 py-0 border-purple-400 text-purple-600 dark:text-purple-400">Problem DT ativo</Badge>
            )}
          </div>
          {signal.entity_id && <p className="text-[10px] text-muted-foreground">{signal.entity_id}</p>}
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <Badge className={`text-[10px] ${SeverityColors[signal.risk_level]}`}>{SeverityLabels[signal.risk_level]}</Badge>
          <Button size="sm" variant="ghost" className="h-6 px-2 text-[10px]" onClick={handleAnalyze} disabled={analyzing}>
            {analyzing ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : <Brain className="h-3 w-3 mr-1" />}
            AI
          </Button>
        </div>
      </div>

      {signal.risk_reasons && signal.risk_reasons.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {signal.risk_reasons.map((r, i) => (
            <span key={i} className="text-[10px] bg-muted px-1.5 py-0.5 rounded">{r}</span>
          ))}
        </div>
      )}

      {signal.k8s_issues && signal.k8s_issues.length > 0 && (
        <div className="text-[10px] bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded px-2 py-1 space-y-0.5">
          <span className="font-semibold text-amber-700 dark:text-amber-400">Issues K8s detectados:</span>
          {signal.k8s_issues.map((issue, i) => (
            <div key={i} className="text-muted-foreground">• {issue}</div>
          ))}
        </div>
      )}

      <div className="grid grid-cols-3 gap-x-3 gap-y-0.5 text-[10px] text-muted-foreground">
        {(signal.error_rate ?? 0) > 0 && <span>Erro: <strong className="text-foreground">{signal.error_rate!.toFixed(1)}%</strong></span>}
        {(signal.response_p90_ms ?? 0) > 0 && <span>P90: <strong className="text-foreground">{signal.response_p90_ms!.toFixed(0)}ms</strong></span>}
        {(signal.pod_restarts ?? 0) > 0 && <span>Restarts: <strong className="text-foreground">{signal.pod_restarts!.toFixed(0)}</strong></span>}
        {(signal.cpu_throttle_pct ?? 0) > 0 && <span>CPU throttle: <strong className="text-foreground">{signal.cpu_throttle_pct!.toFixed(1)}%</strong></span>}
        {(signal.pods_ready_pct ?? 0) > 0 && <span>Pods prontos: <strong className="text-foreground">{signal.pods_ready_pct!.toFixed(1)}%</strong></span>}
      </div>

      {(signal.depended_by?.length || signal.depends_on?.length) ? (
        <div className="text-[10px] text-muted-foreground space-y-0.5">
          {signal.depended_by && signal.depended_by.length > 0 && (
            <div className="flex items-center gap-1"><Users className="h-3 w-3" />Dependido por: {signal.depended_by.join(", ")}</div>
          )}
          {signal.depends_on && signal.depends_on.length > 0 && (
            <div className="flex items-center gap-1"><GitBranch className="h-3 w-3" />Depende de: {signal.depends_on.join(", ")}</div>
          )}
        </div>
      ) : null}

      {analysis && (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="sm" className="h-6 px-2 text-[10px] gap-1 w-full justify-start">
              {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              <Brain className="h-3 w-3 text-blue-500" />
              Análise AI
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="mt-1 p-2 bg-muted/50 rounded text-xs whitespace-pre-wrap">{analysis}</div>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
};

// Aba "DT Sinais" com filtro por risco e scan de sinais OneAgent
const OneAgentTab = ({ signals }: { signals: OneAgentSignal[] }) => {
  const [filter, setFilter] = useState<string>("all");
  const filtered = useMemo(() => {
    if (filter === "all") return signals;
    if (filter === "has_problem") return signals.filter(s => s.has_dt_problem);
    if (filter === "k8s_issue") return signals.filter(s => s.from_k8s_issue);
    return signals.filter(s => s.risk_level === filter);
  }, [signals, filter]);

  const counts = useMemo(() => ({
    critical: signals.filter(s => s.risk_level === "critical").length,
    high: signals.filter(s => s.risk_level === "high").length,
    medium: signals.filter(s => s.risk_level === "medium").length,
    info: signals.filter(s => s.risk_level === "info").length,
    has_problem: signals.filter(s => s.has_dt_problem).length,
    k8s_issue: signals.filter(s => s.from_k8s_issue).length,
  }), [signals]);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-1.5 flex-wrap text-[10px]">
        <span className="text-muted-foreground">Filtro:</span>
        {(["all", "critical", "high", "medium", "info"] as const).map(lvl => (
          <button
            key={lvl}
            onClick={() => setFilter(lvl)}
            className={`px-2 py-0.5 rounded border text-[10px] transition-colors ${filter === lvl ? "bg-primary text-primary-foreground border-primary" : "border-border hover:bg-muted"}`}
          >
            {lvl === "all" ? `Todos (${signals.length})` : `${SeverityLabels[lvl as Severity] ?? lvl} (${counts[lvl as keyof typeof counts] ?? 0})`}
          </button>
        ))}
        {counts.k8s_issue > 0 && (
          <button
            onClick={() => setFilter("k8s_issue")}
            className={`px-2 py-0.5 rounded border text-[10px] transition-colors border-amber-400 ${filter === "k8s_issue" ? "bg-amber-600 text-white" : "hover:bg-amber-50 dark:hover:bg-amber-950 text-amber-600 dark:text-amber-400"}`}
          >
            K8s Issue ({counts.k8s_issue})
          </button>
        )}
        {counts.has_problem > 0 && (
          <button
            onClick={() => setFilter("has_problem")}
            className={`px-2 py-0.5 rounded border text-[10px] transition-colors border-purple-400 ${filter === "has_problem" ? "bg-purple-600 text-white" : "hover:bg-purple-50 dark:hover:bg-purple-950 text-purple-600 dark:text-purple-400"}`}
          >
            Com Problem DT ({counts.has_problem})
          </button>
        )}
      </div>
      <div className="space-y-2">
        {filtered.map((signal, idx) => (
          <OneAgentSignalCard key={signal.entity_id ?? `${signal.namespace}/${signal.workload_name}-${idx}`} signal={signal} />
        ))}
      </div>
    </div>
  );
};

// Aba K8s↔DT com suporte a análise AI em batch de todos os itens correlacionados
const CorrelatedTab = ({ items }: { items: CorrelatedHealthItem[] }) => {
  const { toast } = useToast();
  const [batchAnalyzing, setBatchAnalyzing] = useState(false);
  const [batchResult, setBatchResult] = useState<string | null>(null);
  const [batchOpen, setBatchOpen] = useState(false);

  const handleBatchAnalyze = async () => {
    const aiEmail = localStorage.getItem("ai_email") ?? "";
    if (!aiEmail) {
      toast({ title: "Configure seu e-mail em Configurações de AI", variant: "destructive" });
      return;
    }
    setBatchAnalyzing(true);
    try {
      const result = await apiClient.analyzeCorrelatedBatch(items, aiEmail);
      setBatchResult(result.analysis);
      setBatchOpen(true);
    } catch (err) {
      toast({ title: "Falha na análise batch", description: err instanceof Error ? err.message : "Erro desconhecido", variant: "destructive" });
    } finally {
      setBatchAnalyzing(false);
    }
  };

  return (
    <div className="space-y-2">
      {/* Cabeçalho com contadores e botão batch */}
      <div className="flex items-center justify-between gap-2 pb-1">
        <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full bg-violet-500" />
            {items.filter(i => i.correlated).length} correlacionado(s) K8s+DT
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full bg-blue-500" />
            {items.filter(i => !i.correlated && (i.k8s_issues?.length ?? 0) > 0).length} só K8s
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-full bg-orange-500" />
            {items.filter(i => !i.correlated && (i.dt_problems?.length ?? 0) > 0).length} só DT
          </span>
        </div>
        <Button
          size="sm"
          variant="outline"
          className="h-6 text-[10px] gap-1 border-purple-400 text-purple-700 dark:text-purple-300 hover:bg-purple-50 dark:hover:bg-purple-950/30"
          onClick={handleBatchAnalyze}
          disabled={batchAnalyzing}
        >
          {batchAnalyzing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Brain className="h-3 w-3" />}
          {batchAnalyzing ? "Analisando..." : "Analisar tudo com AI"}
        </Button>
      </div>

      {/* Resultado da análise batch */}
      {batchResult && (
        <Collapsible open={batchOpen} onOpenChange={setBatchOpen}>
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="sm" className="w-full justify-between h-6 text-[10px] text-purple-700 dark:text-purple-300 hover:bg-purple-50 dark:hover:bg-purple-950/30 border border-purple-200 dark:border-purple-800 rounded-md px-2">
              <span className="flex items-center gap-1"><Brain className="h-3 w-3" /> Diagnóstico Consolidado AI</span>
              {batchOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="mt-1.5 rounded-md border border-purple-200 dark:border-purple-800 bg-purple-50 dark:bg-purple-950/20 p-3">
              <p className="text-xs text-purple-900 dark:text-purple-100 whitespace-pre-wrap leading-relaxed">{batchResult}</p>
            </div>
          </CollapsibleContent>
        </Collapsible>
      )}

      {/* Cards individuais */}
      {items.map((item, i) => (
        <CorrelatedItemCard key={i} item={item} />
      ))}
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
                            <TabsList className="grid w-full grid-cols-9">
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
                              <TabsTrigger value="correlated" className="gap-1 text-xs">
                                <Radio className="h-3 w-3 text-violet-500" />
                                <span className={result.correlated_items?.some(i => i.correlated) ? "text-violet-600 dark:text-violet-400 font-semibold" : ""}>
                                  K8s↔DT ({result.correlated_items?.length || 0})
                                </span>
                              </TabsTrigger>
                              <TabsTrigger value="oneagent" className="gap-1 text-xs">
                                <Activity className="h-3 w-3 text-emerald-500" />
                                <span className={(result.oneagent_signals?.length ?? 0) > 0 ? "text-emerald-600 dark:text-emerald-400 font-semibold" : ""}>
                                  DT Sinais ({result.oneagent_signals?.length || 0})
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

                            <TabsContent value="oneagent" className="space-y-2 mt-3">
                              {result.oneagent_signals == null ? (
                                <div className="text-center py-6 text-xs text-muted-foreground space-y-2">
                                  <Activity className="h-8 w-8 mx-auto opacity-30 text-emerald-500" />
                                  <p className="font-medium">Scan OneAgent não executado</p>
                                  <p className="text-[10px]">Ative "Sinais OneAgent" nas opções do Health Check para escanear métricas de todas as entidades instrumentadas</p>
                                </div>
                              ) : result.oneagent_signals.length === 0 ? (
                                <div className="text-center py-6 text-xs text-muted-foreground space-y-2">
                                  <Activity className="h-8 w-8 mx-auto opacity-30 text-emerald-500" />
                                  <p className="font-medium">Nenhuma entidade encontrada no Dynatrace</p>
                                  <p className="text-[10px]">Verifique nos logs do servidor: tag <code className="bg-muted px-1 rounded">dt.host_group.id</code> no OneAgent deve conter o nome do cluster sem <code className="bg-muted px-1 rounded">-admin</code>. Token DT precisa da permissão <strong>Read entities</strong>.</p>
                                </div>
                              ) : (
                                <OneAgentTab signals={result.oneagent_signals} />
                              )}
                            </TabsContent>

                            <TabsContent value="correlated" className="space-y-2 mt-3">
                              {!result.correlated_items || result.correlated_items.length === 0 ? (
                                <div className="text-center py-6 text-xs text-muted-foreground space-y-2">
                                  <Radio className="h-8 w-8 mx-auto opacity-30 text-violet-500" />
                                  <p>Nenhuma correlação K8s ↔ Dynatrace encontrada</p>
                                  <p className="text-[10px]">Ative "Problems Dynatrace" nas opções para cruzar sintomas K8s com problems DT</p>
                                </div>
                              ) : (
                                <CorrelatedTab items={result.correlated_items} />
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
