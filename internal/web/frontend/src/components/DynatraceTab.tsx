import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { AlertTriangle, RefreshCw, Bot, Clock, Layers, AlertCircle, Info, Target, Server } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { DynatraceProblem } from "@/types/healthcheck";
import { SplitView } from "@/components/SplitView";
import ReactMarkdown from "react-markdown";

// ── helpers ────────────────────────────────────────────────────────────────────

const DT_SEVERITY_COLORS: Record<string, string> = {
  AVAILABILITY:        "bg-red-500/10 border-red-500/40",
  ERROR:               "bg-orange-500/10 border-orange-500/40",
  PERFORMANCE:         "bg-yellow-500/10 border-yellow-500/40",
  RESOURCE_CONTENTION: "bg-yellow-500/10 border-yellow-500/40",
  CUSTOM_ALERT:        "bg-blue-500/10 border-blue-500/40",
};

const DT_SEVERITY_BADGE: Record<string, string> = {
  AVAILABILITY:        "destructive",
  ERROR:               "destructive",
  PERFORMANCE:         "outline",
  RESOURCE_CONTENTION: "outline",
  CUSTOM_ALERT:        "secondary",
};

const DT_SEVERITY_ICON: Record<string, React.ReactNode> = {
  AVAILABILITY:        <AlertCircle className="h-4 w-4 text-red-500 shrink-0" />,
  ERROR:               <AlertTriangle className="h-4 w-4 text-orange-500 shrink-0" />,
  PERFORMANCE:         <AlertTriangle className="h-4 w-4 text-yellow-500 shrink-0" />,
  RESOURCE_CONTENTION: <AlertTriangle className="h-4 w-4 text-yellow-500 shrink-0" />,
  CUSTOM_ALERT:        <Info className="h-4 w-4 text-blue-500 shrink-0" />,
};

const DT_SEVERITY_LABEL: Record<string, string> = {
  AVAILABILITY:        "Disponibilidade",
  ERROR:               "Erro",
  PERFORMANCE:         "Performance",
  RESOURCE_CONTENTION: "Recursos",
  CUSTOM_ALERT:        "Alerta",
};

function formatRelativeTime(isoStr: string): string {
  const diff = Date.now() - new Date(isoStr).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 60) return `há ${m}min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `há ${h}h`;
  return `há ${Math.floor(h / 24)}d`;
}

// ── ProblemCard ────────────────────────────────────────────────────────────────

interface ProblemCardProps {
  problem: DynatraceProblem;
  onSelect: (p: DynatraceProblem) => void;
  onAnalyze: (p: DynatraceProblem) => void;
  analyzing: boolean;
  selected: boolean;
}

function ProblemCard({ problem, onSelect, onAnalyze, analyzing, selected }: ProblemCardProps) {
  const colorClass = DT_SEVERITY_COLORS[problem.severityLevel] ?? DT_SEVERITY_COLORS.CUSTOM_ALERT;
  const badgeVariant = (DT_SEVERITY_BADGE[problem.severityLevel] ?? "secondary") as any;
  const icon = DT_SEVERITY_ICON[problem.severityLevel] ?? <Info className="h-4 w-4 shrink-0" />;
  const label = DT_SEVERITY_LABEL[problem.severityLevel] ?? problem.severityLevel;

  const workloads = (problem.k8sWorkloads ?? [])
    .filter((w) => w.Workload)
    .map((w) => `${w.Namespace}/${w.Workload}`);

  const entityCount = problem.affectedEntities?.length ?? 0;

  return (
    <div
      className={`rounded-lg border p-3 space-y-2 transition-all cursor-pointer hover:border-primary/50 ${colorClass} ${selected ? "ring-2 ring-primary" : ""}`}
      onClick={() => onSelect(problem)}
    >
      {/* Cabeçalho */}
      <div className="flex items-start gap-2">
        {icon}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5 flex-wrap mb-1">
            <Badge variant="outline" className="text-xs font-mono">{problem.displayId}</Badge>
            <Badge variant={badgeVariant} className="text-xs">{label}</Badge>
            <Badge variant="secondary" className="text-xs">{problem.impactLevel}</Badge>
            <span className="text-xs text-muted-foreground flex items-center gap-0.5 ml-auto shrink-0">
              <Clock className="h-3 w-3" />
              {formatRelativeTime(problem.startTime)}
            </span>
          </div>
          <p className="text-sm font-medium leading-snug">{problem.title}</p>
        </div>
      </div>

      {/* K8s workloads */}
      {workloads.length > 0 && (
        <div className="flex items-center gap-1 flex-wrap pl-6">
          <Layers className="h-3 w-3 text-muted-foreground shrink-0" />
          {workloads.map((w) => (
            <Badge key={w} variant="outline" className="text-xs font-mono">{w}</Badge>
          ))}
        </div>
      )}

      {entityCount > 0 && (
        <p className="text-xs text-muted-foreground pl-6">
          {entityCount} entidade(s) afetada(s) — clique para ver detalhes
        </p>
      )}

      {/* Ação */}
      <div className="flex justify-end pl-6" onClick={(e) => e.stopPropagation()}>
        <Button size="sm" variant="secondary" onClick={() => onAnalyze(problem)} disabled={analyzing}>
          {analyzing
            ? <><RefreshCw className="h-3 w-3 mr-1.5 animate-spin" />Analisando...</>
            : <><Bot className="h-3 w-3 mr-1.5" />Analisar com AI</>
          }
        </Button>
      </div>
    </div>
  );
}

// ── ProblemDetail ──────────────────────────────────────────────────────────────

function formatDuration(startIso: string, endIso?: string): string {
  const start = new Date(startIso).getTime();
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  const diffMs = end - start;
  const m = Math.floor(diffMs / 60000);
  if (m < 60) return `${m}min`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  if (h < 24) return rm > 0 ? `${h}h ${rm}min` : `${h}h`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

function ProblemDetail({ problem }: { problem: DynatraceProblem }) {
  const colorClass = DT_SEVERITY_COLORS[problem.severityLevel] ?? DT_SEVERITY_COLORS.CUSTOM_ALERT;
  const icon = DT_SEVERITY_ICON[problem.severityLevel] ?? <Info className="h-4 w-4 shrink-0" />;
  const label = DT_SEVERITY_LABEL[problem.severityLevel] ?? problem.severityLevel;
  const duration = formatDuration(problem.startTime, problem.endTime);
  const startDate = new Date(problem.startTime).toLocaleString("pt-BR");

  const entities = problem.affectedEntities ?? [];
  const k8sWorkloads = (problem.k8sWorkloads ?? []).filter(w => w.Workload);

  return (
    <div className="space-y-4">
      {/* Cabeçalho do problem */}
      <div className={`rounded-lg border p-3 space-y-2 ${colorClass}`}>
        <div className="flex items-start gap-2">
          {icon}
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-1.5 flex-wrap mb-1">
              <Badge variant="outline" className="text-xs font-mono">{problem.displayId}</Badge>
              <Badge variant={DT_SEVERITY_BADGE[problem.severityLevel] as any ?? "secondary"} className="text-xs">{label}</Badge>
              <Badge variant="outline" className="text-xs">{problem.impactLevel}</Badge>
              <Badge variant="destructive" className="text-xs">ABERTO</Badge>
            </div>
            <p className="text-sm font-semibold leading-snug">{problem.title}</p>
          </div>
        </div>
        <div className="flex items-center gap-3 text-xs text-muted-foreground pl-6">
          <span className="flex items-center gap-1"><Clock className="h-3 w-3" />{startDate}</span>
          <span className="flex items-center gap-1">Duração: <strong>{duration}</strong></span>
        </div>
      </div>

      {/* Causa raiz */}
      {problem.rootCauseEntity && (
        <div className="space-y-1.5">
          <p className="text-xs font-semibold flex items-center gap-1.5 text-orange-500">
            <Target className="h-3.5 w-3.5" />Causa Raiz (Dynatrace)
          </p>
          <div className="rounded border bg-orange-500/5 border-orange-500/20 px-3 py-2 text-sm">
            <span className="font-medium">{problem.rootCauseEntity.displayName || problem.rootCauseEntity.entityId.id}</span>
            <Badge variant="outline" className="text-xs ml-2 font-mono">{problem.rootCauseEntity.entityId.type}</Badge>
          </div>
        </div>
      )}

      {/* Entidades afetadas */}
      {entities.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-semibold flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5" />Entidades Afetadas ({entities.length})
          </p>
          <div className="space-y-1">
            {entities.map((e, i) => (
              <div key={i} className="rounded border bg-muted/30 px-3 py-1.5 text-xs flex flex-col gap-0.5">
                <div className="flex items-center gap-1.5 flex-wrap">
                  <span className="font-medium">{e.displayName || e.entityId.id}</span>
                  <Badge variant="outline" className="text-xs font-mono py-0">{e.entityId.type}</Badge>
                </div>
                {(e.k8sNamespace || e.k8sWorkload) && (
                  <span className="text-muted-foreground flex items-center gap-1">
                    <Layers className="h-3 w-3" />
                    {[e.k8sCluster, e.k8sNamespace, e.k8sWorkload].filter(Boolean).join(" / ")}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* K8s Workloads correlacionados */}
      {k8sWorkloads.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-semibold flex items-center gap-1.5">
            <Layers className="h-3.5 w-3.5" />Workloads K8s Correlacionados
          </p>
          <div className="flex flex-wrap gap-1.5">
            {k8sWorkloads.map((w, i) => (
              <Badge key={i} variant="secondary" className="text-xs font-mono">
                {w.Cluster && <span className="text-muted-foreground mr-1">{w.Cluster} /</span>}
                {w.Namespace}/{w.Workload}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ── DynatraceTab ───────────────────────────────────────────────────────────────

interface DynatraceTabProps {
  selectedCluster?: string;
}

export function DynatraceTab({ selectedCluster: _cluster }: DynatraceTabProps) {
  const aiEmail = localStorage.getItem("ai_email") ?? "";
  const [selectedProblem, setSelectedProblem] = useState<DynatraceProblem | null>(null);
  const [analysisResult, setAnalysisResult] = useState<string>("");
  const [analyzingId, setAnalyzingId] = useState<string | null>(null);

  const { data, isLoading, refetch, isRefetching } = useQuery({
    queryKey: ["dynatrace-problems", aiEmail],
    queryFn: () => apiClient.getDynatraceProblems(aiEmail),
    enabled: !!aiEmail,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  const analyzeMutation = useMutation({
    mutationFn: (p: DynatraceProblem) =>
      apiClient.analyzeDynatraceProblem(p.problemId, aiEmail),
    onMutate: (p) => {
      setAnalyzingId(p.problemId);
      setSelectedProblem(p);  // garante que o painel direito mostra este problem
      setAnalysisResult("");
    },
    onSuccess: (result) => {
      setAnalysisResult(result.analysis);
      setAnalyzingId(null);
    },
    onError: (err: any) => {
      setAnalysisResult(`**Erro na análise:** ${err.message ?? "Erro desconhecido"}`);
      setAnalyzingId(null);
    },
  });

  // ── Não configurado ──────────────────────────────────────────────────────────
  if (!aiEmail) {
    return (
      <div className="flex items-center justify-center h-96">
        <Card className="max-w-md w-full">
          <CardContent className="pt-6 text-center space-y-2">
            <AlertTriangle className="h-10 w-10 text-yellow-500 mx-auto" />
            <p className="font-medium">Email não configurado</p>
            <p className="text-sm text-muted-foreground">
              Configure seu email em <strong>AI Settings</strong> para usar o Dynatrace.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!isLoading && data?.dt_not_configured) {
    return (
      <div className="flex items-center justify-center h-96">
        <Card className="max-w-md w-full">
          <CardContent className="pt-6 text-center space-y-2">
            <AlertTriangle className="h-10 w-10 text-yellow-500 mx-auto" />
            <p className="font-medium">Dynatrace não configurado</p>
            <p className="text-sm text-muted-foreground">
              Configure a URL e o token em <strong>AI Settings → Dynatrace</strong>.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  const problems = data?.problems ?? [];

  // ── Conteúdo do painel esquerdo ───────────────────────────────────────────────
  const leftContent = (
    <>
      {/* Info linha */}
      {data && !data.dt_not_configured && (
        <div className="mb-3 px-1 py-1 bg-muted/40 rounded text-xs text-muted-foreground">
          {data.total === 0
            ? "Nenhum problem encontrado"
            : `${data.total} problem(s) — ${new Date(data.fetched_at).toLocaleTimeString("pt-BR")}`}
        </div>
      )}

      {/* Lista */}
      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : problems.length === 0 ? (
        <div className="text-center py-16 text-sm text-muted-foreground">
          Nenhum problem aberto
        </div>
      ) : (
        <div className="space-y-3">
          {problems.map((p) => (
            <ProblemCard
              key={p.problemId}
              problem={p}
              onSelect={(prob) => { setSelectedProblem(prob); setAnalysisResult(""); }}
              onAnalyze={(prob) => analyzeMutation.mutate(prob)}
              analyzing={analyzingId === p.problemId}
              selected={selectedProblem?.problemId === p.problemId}
            />
          ))}
        </div>
      )}
    </>
  );

  // ── Conteúdo do painel direito ────────────────────────────────────────────────
  const isAnalyzing = selectedProblem != null && analyzingId === selectedProblem.problemId;

  const rightContent = !selectedProblem ? (
    <div className="flex items-center justify-center h-40 text-muted-foreground text-sm text-center px-4">
      Selecione um problem e clique em "Analisar com AI" para obter diagnóstico detalhado
    </div>
  ) : (
    <div className="space-y-5">
      {/* Detalhes estruturados do problem */}
      <ProblemDetail problem={selectedProblem} />

      {/* Análise AI */}
      <Separator />
      <div className="space-y-2">
        <p className="text-xs font-semibold flex items-center gap-1.5">
          <Bot className="h-3.5 w-3.5" />Análise AI
        </p>
        {isAnalyzing ? (
          <div className="flex flex-col items-center gap-3 py-8">
            <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            <p className="text-sm text-muted-foreground">Coletando métricas e eventos do Dynatrace...</p>
          </div>
        ) : analysisResult ? (
          <div className="prose prose-sm dark:prose-invert max-w-none">
            <ReactMarkdown>{analysisResult}</ReactMarkdown>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            Clique em "Analisar com AI" no card à esquerda para gerar o diagnóstico.
          </p>
        )}
      </div>
    </div>
  );

  return (
    <div className="h-full">
      <SplitView
        leftPanel={{
          title: "Problems Abertos",
          titleAction: (
            <div className="flex items-center gap-2">
              {!isLoading && (
                <Badge variant={problems.length > 0 ? "destructive" : "secondary"}>
                  {problems.length}
                </Badge>
              )}
              <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => refetch()} disabled={isRefetching || isLoading}>
                <RefreshCw className={`h-3 w-3 ${isRefetching ? "animate-spin" : ""}`} />
              </Button>
            </div>
          ),
          content: leftContent,
        }}
        rightPanel={{
          title: "Detalhes do Problem",
          content: rightContent,
        }}
      />
    </div>
  );
}
