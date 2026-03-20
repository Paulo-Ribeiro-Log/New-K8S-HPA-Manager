import { useState, useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Brain, Server, Zap, Users, Tag, GitBranch, Gauge,
  ChevronLeft, Loader2, AlertTriangle, Activity, Unlink, Maximize2,
} from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { useToast } from "@/components/ui/use-toast";
import type { DynatraceHealth } from "@/types/healthcheck";
import { SeverityColors, SeverityLabels } from "@/types/healthcheck";

// ─── Visual Resolution Path (SVG) ─────────────────────────────────────────

const SEV_COLOR: Record<string, string> = {
  critical: "#ef4444", high: "#f97316", medium: "#eab308", low: "#3b82f6", info: "#6b7280",
};
const DT_SEV_LABEL: Record<string, string> = {
  AVAILABILITY: "Availability", ERROR: "Error",
  PERFORMANCE: "Performance", RESOURCE_CONTENTION: "Resource", CUSTOM_ALERT: "Custom",
};

const VisualResolutionPath = ({ problem }: { problem: DynatraceHealth }) => {
  const rootCause = problem.evidence?.find(e => e.startsWith("[Root Cause]"))?.replace("[Root Cause] ", "");
  const workloads = problem.k8s_workloads ?? [];
  const entities  = (problem.affected_entities ?? [])
    .filter(e => !workloads.some(w => e.includes(w.split("/")[1] ?? "___")))
    .slice(0, 4);
  const allTargets = [...workloads, ...entities];
  const hasRoot    = !!rootCause;
  const sev        = SEV_COLOR[problem.severity] ?? "#6b7280";

  const NW = 138; const NH = 44; const GAP_X = 60; const GAP_Y = 12;
  const rows      = Math.max(allTargets.length, 1);
  const totalH    = rows * NH + (rows - 1) * GAP_Y + 24;
  const cols      = hasRoot ? 3 : 2;
  const totalW    = cols * NW + (cols - 1) * GAP_X + 32;
  const midY      = (totalH - NH) / 2;
  const probX     = 16 + (hasRoot ? NW + GAP_X : 0);
  const probY     = midY;
  const tgtX      = probX + NW + GAP_X;
  const tgtTotalH = rows * NH + (rows - 1) * GAP_Y;
  const tgtStartY = (totalH - tgtTotalH) / 2;
  const mk        = problem.problem_id.replace(/[^a-z0-9]/gi, "x");

  if (allTargets.length === 0 && !hasRoot) {
    return (
      <div className="flex flex-col items-center justify-center h-20 gap-1 text-xs text-muted-foreground">
        <Unlink className="h-5 w-5 opacity-30" />
        <span>Sem entidades frontend ou serviço neste problem</span>
      </div>
    );
  }

  return (
    <div className="overflow-auto rounded">
      <svg width={totalW} height={totalH} className="block">
        <defs>
          {(["r:#ef4444", `s:${sev}`, "b:#3b82f6", "p:#8b5cf6"] as const).map(entry => {
            const [id, col] = entry.split(":");
            return (
              <marker key={id} id={`${id}${mk}`} markerWidth="7" markerHeight="7" refX="5" refY="3.5" orient="auto">
                <path d="M0,0 L0,7 L7,3.5 z" fill={col} />
              </marker>
            );
          })}
        </defs>

        {/* Root cause node */}
        {hasRoot && (
          <>
            <rect x={16} y={midY} width={NW} height={NH} rx={6} fill="#fef2f2" stroke="#ef4444" strokeWidth={1.5} />
            <text x={16 + NW / 2} y={midY + 14} textAnchor="middle" fontSize={9} fill="#991b1b" fontWeight="700">
              🎯 Root Cause
            </text>
            <foreignObject x={20} y={midY + 19} width={NW - 8} height={22}>
              <div xmlns="http://www.w3.org/1999/xhtml"
                style={{ fontSize: 8, color: "#7f1d1d", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {rootCause}
              </div>
            </foreignObject>
            <path
              d={`M${16 + NW},${midY + NH / 2} C${16 + NW + GAP_X * 0.5},${midY + NH / 2} ${probX - GAP_X * 0.5},${probY + NH / 2} ${probX},${probY + NH / 2}`}
              fill="none" stroke="#ef4444" strokeWidth={1.5} markerEnd={`url(#r${mk})`} />
          </>
        )}

        {/* Problem node */}
        <rect x={probX} y={probY} width={NW} height={NH} rx={6} fill={sev + "18"} stroke={sev} strokeWidth={2} />
        <text x={probX + NW / 2} y={probY + 14} textAnchor="middle" fontSize={9} fill={sev} fontWeight="700">
          {problem.display_id}
        </text>
        <text x={probX + NW / 2} y={probY + 27} textAnchor="middle" fontSize={8} fill={sev + "cc"}>
          {DT_SEV_LABEL[problem.dt_severity] ?? problem.dt_severity}
        </text>
        <text x={probX + NW / 2} y={probY + 39} textAnchor="middle" fontSize={7} fill={sev + "88"}>
          {problem.impact_level}
        </text>

        {/* Target nodes */}
        {allTargets.map((t, i) => {
          const isWl  = i < workloads.length;
          const wy    = tgtStartY + i * (NH + GAP_Y);
          const parts = t.split("/");
          const ns    = isWl ? (parts[0] ?? "") : "entity";
          const name  = isWl ? (parts[1] ?? t) : t;
          const col   = isWl ? "#3b82f6" : "#8b5cf6";
          const bg    = isWl ? "#eff6ff" : "#f5f3ff";
          const mkId  = isWl ? `b${mk}` : `p${mk}`;
          const cx1   = probX + NW + GAP_X * 0.4;
          const cx2   = tgtX - GAP_X * 0.4;
          return (
            <g key={t + i}>
              <path
                d={`M${probX + NW},${probY + NH / 2} C${cx1},${probY + NH / 2} ${cx2},${wy + NH / 2} ${tgtX},${wy + NH / 2}`}
                fill="none" stroke={col} strokeWidth={1.2}
                strokeDasharray={allTargets.length > 1 ? "5 3" : ""}
                markerEnd={`url(#${mkId})`} />
              <rect x={tgtX} y={wy} width={NW} height={NH} rx={6} fill={bg} stroke={col} strokeWidth={1} />
              <text x={tgtX + NW / 2} y={wy + 14} textAnchor="middle" fontSize={8} fill={col} fontWeight="600">{ns}</text>
              <foreignObject x={tgtX + 4} y={wy + 19} width={NW - 8} height={22}>
                <div xmlns="http://www.w3.org/1999/xhtml"
                  style={{ fontSize: 8, color: col, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>
                  {name.length > 20 ? name.slice(0, 20) + "…" : name}
                </div>
              </foreignObject>
            </g>
          );
        })}
      </svg>
    </div>
  );
};

// ─── Compact card (2-col grid) ────────────────────────────────────────────

const DT_ICON: Record<string, string> = {
  AVAILABILITY: "🔴", ERROR: "🟠", PERFORMANCE: "🟡", RESOURCE_CONTENTION: "🟡", CUSTOM_ALERT: "🔵",
};
const ENV_CLS: Record<string, string> = {
  prd: "bg-red-100 text-red-700 dark:bg-red-950/30 dark:text-red-400",
  hlg: "bg-yellow-100 text-yellow-700 dark:bg-yellow-950/30 dark:text-yellow-400",
  dev: "bg-blue-100 text-blue-700 dark:bg-blue-950/30 dark:text-blue-400",
};

const DTProblemCompactCard = ({
  problem, selected, onSelect,
}: { problem: DynatraceHealth; selected: boolean; onSelect: () => void }) => {
  const sev     = problem.severity as keyof typeof SeverityColors;
  const color   = SeverityColors[sev] ?? "text-gray-500";
  const wls     = problem.k8s_workloads ?? [];
  const metrics = problem.metrics_summary ?? {};

  return (
    <button onClick={onSelect}
      className={`w-full text-left rounded-lg border p-2.5 transition-all hover:shadow-sm space-y-1.5 ${
        selected
          ? "border-purple-500 ring-1 ring-purple-500/40 bg-purple-500/5"
          : "border-border bg-card hover:border-muted-foreground/30"
      }`}>
      {/* Row 1 */}
      <div className="flex items-center justify-between gap-1">
        <div className="flex items-center gap-1 min-w-0">
          <span className="text-sm leading-none shrink-0">{DT_ICON[problem.dt_severity] ?? "⚪"}</span>
          <span className="font-mono text-[10px] text-muted-foreground">{problem.display_id}</span>
          <Badge variant="outline" className={`text-[10px] h-4 px-1 ${color}`}>{SeverityLabels[sev]}</Badge>
          <Badge variant="outline" className="text-[10px] h-4 px-1">{problem.impact_level}</Badge>
        </div>
        <span className="text-[10px] text-muted-foreground shrink-0">
          {new Date(problem.start_time).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" })}
        </span>
      </div>
      {/* Row 2: title */}
      <p className="text-xs font-semibold line-clamp-1">{problem.title}</p>
      {/* Row 3: workloads + metrics */}
      <div className="flex items-center gap-1 flex-wrap">
        {wls.slice(0, 2).map((wl, i) => (
          <span key={i} className="text-[10px] font-mono bg-muted px-1 py-0.5 rounded truncate max-w-[100px]">
            {wl.split("/")[1] ?? wl}
          </span>
        ))}
        {wls.length > 2 && <span className="text-[10px] text-muted-foreground">+{wls.length - 2}</span>}
        <div className="ml-auto flex items-center gap-1.5">
          {metrics.error_rate    !== undefined && <span className="text-[10px] font-medium text-red-600">ERR {metrics.error_rate.toFixed(1)}%</span>}
          {metrics.response_p90_ms !== undefined && <span className="text-[10px] font-medium text-orange-600">P90 {Math.round(metrics.response_p90_ms)}ms</span>}
        </div>
      </div>
      {/* Row 4: squad + env + Davis */}
      <div className="flex items-center gap-1.5 flex-wrap">
        {(problem.squads ?? []).slice(0, 1).map((s, i) => (
          <span key={i} className="text-[10px] text-muted-foreground flex items-center gap-0.5">
            <Users className="h-2.5 w-2.5" />{s}
          </span>
        ))}
        {(problem.environments ?? []).slice(0, 2).map((env, i) => (
          <span key={i} className={`text-[10px] px-1 rounded font-medium ${ENV_CLS[env.toLowerCase()] ?? "bg-muted text-muted-foreground"}`}>
            {env}
          </span>
        ))}
        {problem.context_fetched && (
          <span className="text-[10px] text-purple-500 flex items-center gap-0.5 ml-auto">
            <Brain className="h-2.5 w-2.5" />Davis AI
          </span>
        )}
      </div>
    </button>
  );
};

// ─── Detail view ──────────────────────────────────────────────────────────

const DTProblemDetailView = ({
  problem, affectedClusters, onBack,
}: { problem: DynatraceHealth; affectedClusters?: string[]; onBack: () => void }) => {
  const { toast } = useToast();
  const [analyzing, setAnalyzing] = useState(false);
  const [analysis, setAnalysis]   = useState<string | null>(null);
  const [graphMax, setGraphMax]   = useState(false);

  const sev       = problem.severity as keyof typeof SeverityColors;
  const color     = SeverityColors[sev] ?? "text-gray-500";
  const wls       = problem.k8s_workloads ?? [];
  const entities  = problem.affected_entities ?? [];
  const metrics   = problem.metrics_summary ?? {};
  const evidence  = problem.evidence ?? [];
  const rootCause = evidence.find(e => e.startsWith("[Root Cause]"))?.replace("[Root Cause] ", "");
  const otherEvid = evidence.filter(e => !e.startsWith("[Root Cause]"));

  const handleAnalyze = async () => {
    const email = localStorage.getItem("ai_email") ?? "";
    if (!email) { toast({ title: "Configure seu e-mail em Configurações de AI", variant: "destructive" }); return; }
    setAnalyzing(true);
    try {
      const r = await apiClient.analyzeDynatraceProblem(problem.problem_id, email);
      setAnalysis(r.analysis);
    } catch (e: any) {
      toast({ title: "Falha na análise AI", description: e.message, variant: "destructive" });
    } finally { setAnalyzing(false); }
  };

  return (
    <div className="space-y-2.5">
      {/* Header */}
      <div className="flex items-center gap-2 flex-wrap">
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs gap-1 shrink-0" onClick={onBack}>
          <ChevronLeft className="h-3.5 w-3.5" /> Voltar
        </Button>
        <span className="text-base leading-none shrink-0">{DT_ICON[problem.dt_severity] ?? "⚪"}</span>
        <span className="font-mono text-xs text-muted-foreground shrink-0">{problem.display_id}</span>
        <Badge variant="outline" className={`text-xs ${color}`}>{SeverityLabels[sev]}</Badge>
        <Badge variant="outline" className="text-xs">{problem.impact_level}</Badge>
        {(affectedClusters?.length ?? 0) > 1 && (
          <Badge variant="outline" className="text-xs text-blue-600 border-blue-400 gap-1">
            <Server className="h-2.5 w-2.5" />Afeta {affectedClusters!.length} clusters
          </Badge>
        )}
        <p className="text-sm font-semibold truncate flex-1 min-w-0">{problem.title}</p>
        <span className="text-xs text-muted-foreground shrink-0">
          {new Date(problem.start_time).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" })}
        </span>
        <Button size="sm" variant="outline"
          className="h-7 text-xs gap-1 shrink-0 text-purple-600 border-purple-300 hover:bg-purple-50 dark:hover:bg-purple-950/20"
          onClick={handleAnalyze} disabled={analyzing}>
          {analyzing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Brain className="h-3 w-3" />}
          {analyzing ? "Analisando..." : "Analisar com AI"}
        </Button>
      </div>

      {/* Impact summary strip */}
      <div className="grid grid-cols-4 gap-1.5">
        {[
          { label: "Workloads K8s", v: wls.length,                           icon: <Server className="h-3 w-3" />,  c: "text-blue-600"   },
          { label: "Entidades DT",  v: entities.length,                       icon: <Zap className="h-3 w-3" />,     c: "text-purple-600" },
          { label: "Namespaces",    v: problem.k8s_namespaces?.length ?? 0,   icon: <Tag className="h-3 w-3" />,     c: "text-green-600"  },
          { label: "Métricas",      v: Object.keys(metrics).length,           icon: <Gauge className="h-3 w-3" />,   c: "text-orange-600" },
        ].map(({ label, v, icon, c }) => (
          <div key={label} className="rounded border bg-muted/30 px-2.5 py-1.5 flex items-center gap-2">
            <span className={c}>{icon}</span>
            <div>
              <p className="text-base font-bold leading-none">{v}</p>
              <p className="text-[10px] text-muted-foreground mt-0.5">{label}</p>
            </div>
          </div>
        ))}
      </div>

      {/* Metrics + meta bar */}
      {Object.keys(metrics).length > 0 && (
        <div className="flex items-center gap-3 flex-wrap rounded border bg-muted/20 px-3 py-1.5 text-xs">
          <Gauge className="h-3 w-3 text-muted-foreground shrink-0" />
          {metrics.error_rate      !== undefined && <span className="font-medium text-red-600">ERR {metrics.error_rate.toFixed(1)}%</span>}
          {metrics.response_p90_ms !== undefined && <span className="font-medium text-orange-600">P90 {Math.round(metrics.response_p90_ms)}ms</span>}
          {metrics.response_p99_ms !== undefined && <span className="font-medium text-orange-700">P99 {Math.round(metrics.response_p99_ms)}ms</span>}
          {metrics.throughput_rpm  !== undefined && <span className="text-muted-foreground">{Math.round(metrics.throughput_rpm)} req/min</span>}
          <div className="ml-auto flex items-center gap-2 flex-wrap">
            {(problem.squads     ?? []).map((s, i) => <span key={i} className="text-[10px] text-muted-foreground flex items-center gap-0.5"><Users className="h-2.5 w-2.5" />{s}</span>)}
            {(problem.journeys   ?? []).map((j, i) => <span key={i} className="text-[10px] text-muted-foreground flex items-center gap-0.5"><Tag className="h-2.5 w-2.5" />{j}</span>)}
            {(problem.environments ?? []).map((env, i) => (
              <span key={i} className={`text-[10px] px-1 rounded font-medium ${ENV_CLS[env.toLowerCase()] ?? "bg-muted text-muted-foreground"}`}>{env}</span>
            ))}
          </div>
        </div>
      )}

      {/* 2-panel layout */}
      <div className={graphMax ? "" : "grid gap-3"} style={graphMax ? {} : { gridTemplateColumns: "55% 1fr" }}>

        {/* LEFT: Impact */}
        {!graphMax && (
          <div className="space-y-2 min-w-0">
            <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wide flex items-center gap-1">
              <AlertTriangle className="h-3 w-3" /> Impact
            </p>

            {wls.length > 0 && (
              <div className="rounded border bg-muted/20 p-2 space-y-1.5">
                <p className="text-[10px] font-semibold text-blue-600 flex items-center gap-1">
                  <Server className="h-3 w-3" /> Workloads K8s ({wls.length})
                </p>
                <div className="grid grid-cols-2 gap-1">
                  {wls.map((wl, i) => {
                    const [ns, name] = wl.split("/");
                    const ver = problem.app_versions?.[name ?? wl] ?? problem.app_versions?.[wl];
                    return (
                      <div key={i} className="flex items-center justify-between rounded bg-background border px-2 py-1 gap-1 min-w-0">
                        <div className="min-w-0">
                          <p className="text-[10px] text-muted-foreground truncate">{ns}</p>
                          <p className="text-[10px] font-mono font-medium truncate">{name ?? wl}</p>
                        </div>
                        {ver && <span className="text-[10px] text-blue-600 shrink-0">v{ver}</span>}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            {entities.length > 0 && (
              <div className="rounded border bg-muted/20 p-2 space-y-1.5">
                <p className="text-[10px] font-semibold text-purple-600 flex items-center gap-1">
                  <Zap className="h-3 w-3" /> Entidades afetadas ({entities.length})
                </p>
                <div className="grid grid-cols-2 gap-1">
                  {entities.slice(0, 8).map((e, i) => (
                    <div key={i} className="rounded bg-background border px-2 py-1">
                      <p className="text-[10px] font-mono truncate">{e}</p>
                    </div>
                  ))}
                  {entities.length > 8 && <p className="text-[10px] text-muted-foreground col-span-2">+{entities.length - 8} entidades</p>}
                </div>
              </div>
            )}

            {wls.length === 0 && entities.length === 0 && (
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground py-2">
                <Unlink className="h-3 w-3" />Sem correlação K8s — verifique tags OneAgent no Dynatrace
              </div>
            )}

            {(problem.recent_events?.length ?? 0) > 0 && (
              <div className="rounded border bg-muted/20 p-2 space-y-0.5">
                <p className="text-[10px] font-semibold text-muted-foreground">Eventos recentes</p>
                {problem.recent_events!.map((ev, i) => (
                  <p key={i} className="text-[10px] text-muted-foreground">• {ev}</p>
                ))}
              </div>
            )}

            {(problem.github_repos?.length ?? 0) > 0 && (
              <div className="flex items-center gap-1.5 flex-wrap">
                <GitBranch className="h-3 w-3 text-muted-foreground shrink-0" />
                {problem.github_repos!.slice(0, 3).map((r, i) => (
                  <span key={i} className="text-[10px] text-muted-foreground">{r}</span>
                ))}
              </div>
            )}
          </div>
        )}

        {/* RIGHT: Root Cause + Visual Resolution Path */}
        <div className="space-y-2" style={{ minWidth: graphMax ? undefined : 300 }}>
          {/* Root cause */}
          <div className="rounded border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-950/20 p-2 space-y-1">
            <p className="text-[10px] font-semibold text-red-700 dark:text-red-300 uppercase tracking-wide">🎯 Root Cause</p>
            {rootCause
              ? <p className="text-xs text-red-800 dark:text-red-200 font-medium">{rootCause}</p>
              : <p className="text-[10px] text-muted-foreground italic">Não identificado — possíveis causas: retenção expirada ou permissões insuficientes.</p>
            }
            {otherEvid.length > 0 && (
              <div className="pt-1 border-t border-red-200 dark:border-red-800 space-y-0.5">
                <p className="text-[10px] font-semibold text-purple-700 dark:text-purple-300 flex items-center gap-1">
                  <Brain className="h-2.5 w-2.5" /> Evidências Davis AI
                </p>
                {otherEvid.map((ev, i) => (
                  <p key={i} className="text-[10px] text-purple-700 dark:text-purple-300">• {ev}</p>
                ))}
              </div>
            )}
          </div>

          {/* Visual Resolution Path */}
          <div className="rounded border bg-muted/10 p-2 space-y-1.5">
            <div className="flex items-center justify-between">
              <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wide flex items-center gap-1">
                <Activity className="h-3 w-3" /> Visual Resolution Path
              </p>
              <Button variant="ghost" size="icon" className="h-5 w-5" onClick={() => setGraphMax(g => !g)}
                title={graphMax ? "Restaurar" : "Maximizar"}>
                <Maximize2 className="h-3 w-3" />
              </Button>
            </div>
            <VisualResolutionPath problem={problem} />
          </div>

          {/* Suggestions */}
          {problem.suggestions.length > 0 && (
            <div className="rounded border bg-muted/20 p-2 space-y-0.5">
              <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wide">💡 Sugestões</p>
              {problem.suggestions.slice(0, 3).map((s, i) => (
                <p key={i} className="text-[10px] text-muted-foreground">• {s}</p>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* AI analysis */}
      {analysis && (
        <div className="rounded border border-purple-200 dark:border-purple-800 bg-purple-50 dark:bg-purple-950/20 p-3">
          <p className="text-xs font-semibold text-purple-700 dark:text-purple-300 flex items-center gap-1 mb-1.5">
            <Brain className="h-3 w-3" /> Análise AI
          </p>
          <p className="text-xs text-purple-900 dark:text-purple-100 whitespace-pre-wrap leading-relaxed">{analysis}</p>
        </div>
      )}
    </div>
  );
};

// ─── Main export ──────────────────────────────────────────────────────────

export interface HealthCheckDTTabProps {
  problems: DynatraceHealth[];
  displayIdClusters?: Map<string, string[]>;
}

export const HealthCheckDTTab = ({ problems, displayIdClusters }: HealthCheckDTTabProps) => {
  const [selected, setSelected] = useState<string | null>(null);

  const selectedProblem = useMemo(
    () => problems.find(p => p.problem_id === selected) ?? null,
    [problems, selected]
  );

  const criticalCount = problems.filter(p => p.severity === "critical").length;
  const highCount     = problems.filter(p => p.severity === "high").length;

  if (problems.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-xs text-muted-foreground gap-2">
        <Zap className="h-8 w-8 opacity-30 text-purple-500" />
        <p>Nenhum problem Dynatrace correlacionado com este cluster</p>
        <p className="text-[10px]">Ative "Problems Dynatrace" nas opções e certifique-se de que o token DT está configurado nas Configurações de AI</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {!selectedProblem && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground border-b pb-2">
          <Zap className="h-3.5 w-3.5 text-purple-500 shrink-0" />
          <span>{problems.length} problem(s) correlacionado(s)</span>
          {criticalCount > 0 && <Badge variant="destructive" className="text-[10px] h-4 px-1">{criticalCount} crítico</Badge>}
          {highCount     > 0 && <Badge variant="outline"     className="text-[10px] h-4 px-1 text-orange-600 border-orange-400">{highCount} alto</Badge>}
          <span className="ml-auto text-[10px]">Clique para ver detalhes + resolution path</span>
        </div>
      )}

      {selectedProblem ? (
        <DTProblemDetailView
          problem={selectedProblem}
          affectedClusters={displayIdClusters?.get(selectedProblem.display_id)}
          onBack={() => setSelected(null)}
        />
      ) : (
        <div className="grid grid-cols-2 gap-2">
          {problems.map(p => (
            <DTProblemCompactCard
              key={p.problem_id}
              problem={p}
              selected={selected === p.problem_id}
              onSelect={() => setSelected(p.problem_id)}
            />
          ))}
        </div>
      )}
    </div>
  );
};
