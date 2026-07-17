import { useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AlertTriangle, Server, Clock } from "lucide-react";
import type { HealthCheckResult, Severity, NodeHealth } from "@/types/healthcheck";
import { SeverityWeight, SeverityColors, SeverityBgColors, SeverityLabels } from "@/types/healthcheck";

interface ReportFinding {
  severity: Severity;
  resourceKind: string;
  resourceName: string;
  message: string;
  isChronic?: boolean;
  chronicSince?: string;
  resourceVerdict?: string;
}

// Prioriza dados correlacionados (K8s+DT) quando disponíveis — já vêm com severidade final
// calculada e, para eventos, a classificação crônico/agudo (Fase 1). Sem Dynatrace configurado,
// correlated_items fica vazio (Correlate() só roda quando há problems DT) e caímos no fallback
// abaixo, montado a partir dos resultados brutos de cada checker.
function buildFindings(result: HealthCheckResult): ReportFinding[] {
  if (result.correlated_items && result.correlated_items.length > 0) {
    const list: ReportFinding[] = [];
    for (const item of result.correlated_items) {
      for (const issue of item.k8s_issues ?? []) {
        list.push({
          severity: issue.severity,
          resourceKind: issue.resource_kind,
          resourceName: `${item.namespace}/${issue.resource_name}`,
          message: issue.message,
          isChronic: issue.chronicity?.is_chronic,
          chronicSince: issue.chronicity?.first_seen_ever,
          resourceVerdict: issue.resource_verdict,
        });
      }
    }
    return list.sort((a, b) => SeverityWeight[b.severity] - SeverityWeight[a.severity]);
  }

  const list: ReportFinding[] = [];
  for (const d of result.deployment_results ?? []) {
    if (d.status === "healthy") continue;
    list.push({
      severity: d.status === "critical" ? "critical" : "medium",
      resourceKind: "Deployment",
      resourceName: `${d.namespace}/${d.name}`,
      message: d.message,
      resourceVerdict: d.resource_verdict,
    });
  }
  for (const e of result.event_results ?? []) {
    if (e.status === "healthy") continue;
    list.push({
      severity: e.severity,
      resourceKind: "Event",
      resourceName: `${e.namespace}/${e.involved_name}`,
      message: e.message,
    });
  }
  for (const h of result.hpa_results ?? []) {
    if (h.status === "healthy") continue;
    list.push({
      severity: h.status === "critical" ? "critical" : "medium",
      resourceKind: "HPA",
      resourceName: `${h.namespace}/${h.name}`,
      message: h.message,
    });
  }
  for (const p of result.pvc_results ?? []) {
    if (p.status === "healthy") continue;
    list.push({
      severity: p.status === "critical" ? "critical" : "medium",
      resourceKind: "PVC",
      resourceName: `${p.namespace}/${p.name}`,
      message: p.message,
    });
  }
  return list.sort((a, b) => SeverityWeight[b.severity] - SeverityWeight[a.severity]);
}

function nodeStatusBadgeClass(status: NodeHealth["status"]): string {
  switch (status) {
    case "critical":
      return "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/30";
    case "warning":
      return "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30";
    default:
      return "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30";
  }
}

function resourceVerdictLabel(verdict?: string): { label: string; className: string } | null {
  switch (verdict) {
    case "oom_risk":
      return { label: "risco OOM/throttling", className: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/30" };
    case "superprovisioned":
      return { label: "superprovisionado", className: "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30" };
    default:
      return null;
  }
}

// Aba "Relatório" — visão estruturada e priorizada por severidade (tabela-resumo, tabela de nós,
// badges de crônico/agudo e de uso real de recursos), em vez do texto de IA solto que as outras
// abas expõem via "Analisar com AI". Não substitui a análise de IA (ainda acessível na aba K8s↔DT)
// — é a camada de dados estruturados que a IA também recebe no prompt, mas legível diretamente.
export const HealthReportTab = ({ result }: { result: HealthCheckResult }) => {
  const findings = useMemo(() => buildFindings(result), [result]);
  const nodes = result.node_results ?? [];

  const counts = useMemo(() => {
    const c: Record<Severity, number> = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
    for (const f of findings) c[f.severity]++;
    return c;
  }, [findings]);

  const startedAt = result.started_at ? new Date(result.started_at) : null;
  const finishedAt = result.finished_at ? new Date(result.finished_at) : null;

  return (
    <div className="space-y-4">
      {/* Cabeçalho: cluster/período/contadores por severidade */}
      <div className="rounded-lg border p-3 bg-muted/30">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground mb-2">
          <span className="flex items-center gap-1">
            <Server className="h-3 w-3" /> {result.cluster}
          </span>
          {startedAt && finishedAt && (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {startedAt.toLocaleString("pt-BR")} — {finishedAt.toLocaleTimeString("pt-BR")}
            </span>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {(["critical", "high", "medium", "low"] as Severity[]).map((sev) =>
            counts[sev] > 0 ? (
              <Badge key={sev} variant="outline" className={`text-xs ${SeverityBgColors[sev]} ${SeverityColors[sev]}`}>
                {counts[sev]} {SeverityLabels[sev]}
              </Badge>
            ) : null
          )}
          {findings.length === 0 && (
            <span className="text-xs text-muted-foreground">Nenhum problema encontrado nesta execução</span>
          )}
        </div>
      </div>

      {/* Tabela-resumo priorizada por severidade */}
      {findings.length > 0 && (
        <div className="rounded-lg border overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="text-xs">Severidade</TableHead>
                <TableHead className="text-xs">Recurso</TableHead>
                <TableHead className="text-xs">Mensagem</TableHead>
                <TableHead className="text-xs">Contexto</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {findings.map((f, i) => {
                const verdict = resourceVerdictLabel(f.resourceVerdict);
                return (
                  <TableRow key={i}>
                    <TableCell className="align-top">
                      <Badge variant="outline" className={`text-xs whitespace-nowrap ${SeverityBgColors[f.severity]} ${SeverityColors[f.severity]}`}>
                        {SeverityLabels[f.severity]}
                      </Badge>
                    </TableCell>
                    <TableCell className="align-top text-xs font-mono whitespace-nowrap">
                      {f.resourceKind} <br />
                      <span className="text-muted-foreground">{f.resourceName}</span>
                    </TableCell>
                    <TableCell className="align-top text-xs max-w-md">{f.message}</TableCell>
                    <TableCell className="align-top">
                      <div className="flex flex-col gap-1">
                        {f.isChronic && (
                          <Badge variant="outline" className="text-xs w-fit gap-1 bg-orange-500/10 text-orange-600 dark:text-orange-400 border-orange-500/30">
                            <AlertTriangle className="h-3 w-3" /> Crônico{f.chronicSince ? ` desde ${new Date(f.chronicSince).toLocaleDateString("pt-BR")}` : ""}
                          </Badge>
                        )}
                        {verdict && (
                          <Badge variant="outline" className={`text-xs w-fit ${verdict.className}`}>
                            {verdict.label}
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Tabela de utilização dos nós — só aparece quando check_nodes estava habilitado */}
      {nodes.length > 0 && (
        <div>
          <p className="text-xs font-medium text-muted-foreground mb-1.5">Utilização dos Nós</p>
          <div className="rounded-lg border overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="text-xs">Nó</TableHead>
                  <TableHead className="text-xs">Status</TableHead>
                  <TableHead className="text-xs">Pods</TableHead>
                  <TableHead className="text-xs">Utilização de Pods</TableHead>
                  <TableHead className="text-xs">CPU</TableHead>
                  <TableHead className="text-xs">Memória</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((n) => (
                  <TableRow key={n.name}>
                    <TableCell className="text-xs font-mono">{n.name}</TableCell>
                    <TableCell>
                      <Badge variant="outline" className={`text-xs ${nodeStatusBadgeClass(n.status)}`}>
                        {n.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs">{n.allocated.pods}/{n.allocatable.pods}</TableCell>
                    <TableCell className="text-xs">{n.pod_utilization_percent.toFixed(1)}%</TableCell>
                    <TableCell className="text-xs">{n.cpu_utilization_percent.toFixed(1)}%</TableCell>
                    <TableCell className="text-xs">{n.memory_utilization_percent.toFixed(1)}%</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <p className="text-[10px] text-muted-foreground mt-1">
            Baixa utilização de pods com CPU/memória elevadas costuma indicar requests/limits mal configurados nos workloads, não excesso de pods por nó.
          </p>
        </div>
      )}
    </div>
  );
};
