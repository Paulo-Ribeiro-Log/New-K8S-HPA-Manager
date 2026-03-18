import { useState, useRef, type ReactNode } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { DynatraceMetricsPanel } from "@/components/DynatraceMetricsPanel";
import { DynatraceContextPanel } from "@/components/DynatraceContextPanel";
import { DynatraceGitHubSection } from "@/components/DynatraceGitHubSection";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  AlertTriangle, RefreshCw, Bot, Clock, Layers, AlertCircle,
  Info, Target, Server, Search, Network, MapPin, ArrowRight,
  Activity, X, BarChart3, GitBranch, Users, Tag, Cpu,
  Package, GitCommit, Rocket, ChevronDown, ChevronRight,
  Shield, Globe, Database, Boxes, CheckCircle2,
} from "lucide-react";
import { apiClient } from "@/lib/api/client";
import type { NodePoolLookupResult } from "@/lib/api/types";
import { DynatraceProblem } from "@/types/healthcheck";
import { SplitView } from "@/components/SplitView";
import ReactMarkdown from "react-markdown";

// ── helpers ─────────────────────────────────────────────────────────────────────

const SEV_BG: Record<string, string> = {
  AVAILABILITY:        "bg-red-500/10 border-red-500/40",
  ERROR:               "bg-orange-500/10 border-orange-500/40",
  PERFORMANCE:         "bg-yellow-500/10 border-yellow-500/40",
  RESOURCE_CONTENTION: "bg-yellow-500/10 border-yellow-500/40",
  CUSTOM_ALERT:        "bg-blue-500/10 border-blue-500/40",
};

const SEV_BADGE_COLOR: Record<string, string> = {
  AVAILABILITY:        "bg-red-500/20 text-red-400 border-red-500/30",
  ERROR:               "bg-orange-500/20 text-orange-400 border-orange-500/30",
  PERFORMANCE:         "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
  RESOURCE_CONTENTION: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
  CUSTOM_ALERT:        "bg-blue-500/20 text-blue-400 border-blue-500/30",
};

const SEV_ICON: Record<string, React.ReactNode> = {
  AVAILABILITY:        <AlertCircle className="h-4 w-4 text-red-500 shrink-0" />,
  ERROR:               <AlertTriangle className="h-4 w-4 text-orange-500 shrink-0" />,
  PERFORMANCE:         <Activity className="h-4 w-4 text-yellow-500 shrink-0" />,
  RESOURCE_CONTENTION: <Activity className="h-4 w-4 text-yellow-500 shrink-0" />,
  CUSTOM_ALERT:        <Info className="h-4 w-4 text-blue-500 shrink-0" />,
};

const SEV_LABEL: Record<string, string> = {
  AVAILABILITY:        "Disponibilidade",
  ERROR:               "Erro",
  PERFORMANCE:         "Performance",
  RESOURCE_CONTENTION: "Contenção de Recursos",
  CUSTOM_ALERT:        "Alerta Customizado",
};

const IMPACT_LABEL: Record<string, string> = {
  APPLICATION:    "Aplicação",
  ENVIRONMENT:    "Ambiente",
  INFRASTRUCTURE: "Infraestrutura",
  SERVICE:        "Serviço",
};

const ENTITY_TYPE_LABEL: Record<string, string> = {
  SERVICE:                  "Serviço",
  APPLICATION:              "Aplicação",
  PROCESS_GROUP:            "Grupo de Processos",
  PROCESS_GROUP_INSTANCE:   "Processo",
  HOST:                     "Host",
  KUBERNETES_NODE:          "Nó K8s",
  KUBERNETES_CLUSTER:       "Cluster K8s",
  KUBERNETES_SERVICE:       "Service K8s",
  CLOUD_APPLICATION:        "Cloud App",
  CLOUD_APPLICATION_INSTANCE: "Pod",
};

function sevIcon(sev: string) {
  return SEV_ICON[sev] ?? <Info className="h-4 w-4 shrink-0 text-muted-foreground" />;
}
function sevLabel(sev: string) { return SEV_LABEL[sev] ?? sev; }
function impactLabel(lvl: string) { return IMPACT_LABEL[lvl] ?? lvl; }
function entityTypeLabel(t: string) { return ENTITY_TYPE_LABEL[t] ?? t; }

function formatRelativeTime(isoStr: string): string {
  const diff = Date.now() - new Date(isoStr).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 1) return "agora";
  if (m < 60) return `há ${m}min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `há ${h}h`;
  return `há ${Math.floor(h / 24)}d`;
}

function formatDuration(startIso: string, endIso?: string): string {
  const start = new Date(startIso).getTime();
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  const m = Math.floor((end - start) / 60000);
  if (m < 60) return `${m}min`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  if (h < 24) return rm > 0 ? `${h}h ${rm}min` : `${h}h`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

// ── ProblemCard (compacto, lado esquerdo) ────────────────────────────────────────

function ProblemCard({
  problem, selected, uiBaseUrl, onClick,
}: {
  problem: DynatraceProblem;
  selected: boolean;
  uiBaseUrl?: string;
  onClick: () => void;
}) {
  const bg = SEV_BG[problem.severityLevel] ?? SEV_BG.CUSTOM_ALERT;
  const badgeColor = SEV_BADGE_COLOR[problem.severityLevel] ?? SEV_BADGE_COLOR.CUSTOM_ALERT;
  const entityCount = problem.affectedEntities?.length ?? 0;
  const k8sCount = (problem.k8sWorkloads ?? []).filter(w => w.Workload).length;
  const isClosed = problem.status === "CLOSED";

  return (
    <div
      onClick={onClick}
      className={`rounded-lg border p-3 cursor-pointer transition-all hover:border-primary/60
        ${bg} ${selected ? "ring-2 ring-primary border-primary/60" : ""} ${isClosed ? "opacity-70" : ""}`}
    >
      <div className="flex items-start gap-2">
        {isClosed
          ? <CheckCircle2 className="h-4 w-4 text-green-500 shrink-0 mt-0.5" />
          : sevIcon(problem.severityLevel)
        }
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5 flex-wrap mb-1">
            <span className={`inline-flex items-center rounded border px-1.5 py-0 text-[10px] font-mono font-medium ${badgeColor}`}>
              {problem.displayId}
            </span>
            <span className={`inline-flex items-center rounded border px-1.5 py-0 text-[10px] font-medium ${badgeColor}`}>
              {sevLabel(problem.severityLevel)}
            </span>
            {isClosed && (
              <span className="inline-flex items-center gap-0.5 rounded border border-green-500/40 bg-green-500/10 px-1.5 py-0 text-[10px] font-medium text-green-400">
                <CheckCircle2 className="h-2.5 w-2.5" />Resolvido
              </span>
            )}
            <span className="text-[10px] text-muted-foreground ml-auto shrink-0 flex items-center gap-0.5">
              <Clock className="h-2.5 w-2.5" />
              {formatRelativeTime(problem.startTime)}
            </span>
          </div>
          <p className="text-sm font-medium leading-snug line-clamp-2">{problem.title}</p>
          <div className="flex items-center gap-2 mt-1.5 text-[10px] text-muted-foreground">
            <span className="flex items-center gap-0.5">
              <Server className="h-2.5 w-2.5" />{entityCount} entidade(s)
            </span>
            {k8sCount > 0 && (
              <span className="flex items-center gap-0.5">
                <Layers className="h-2.5 w-2.5" />{k8sCount} workload(s) K8s
              </span>
            )}
            <span className="flex items-center gap-0.5">
              <Activity className="h-2.5 w-2.5" />{impactLabel(problem.impactLevel)}
            </span>
          </div>
        </div>
        <ArrowRight className={`h-3.5 w-3.5 shrink-0 mt-1 transition-colors ${selected ? "text-primary" : "text-muted-foreground/40"}`} />
      </div>

      {/* Management zones + link Dynatrace */}
      <div className="flex items-center gap-1 flex-wrap mt-2 pl-6">
        {(problem.managementZones ?? []).length > 0 && (
          <>
            <MapPin className="h-2.5 w-2.5 text-muted-foreground shrink-0" />
            {problem.managementZones!.map(z => (
              <span key={z.id} className="text-[10px] text-muted-foreground border rounded px-1 py-0">{z.name}</span>
            ))}
          </>
        )}
        {uiBaseUrl && (
          <a
            href={`${uiBaseUrl}/ui/apps/dynatrace.davis.problems/problem/${problem.problemId}`}
            target="_blank"
            rel="noopener noreferrer"
            onClick={e => e.stopPropagation()}
            className="ml-auto text-[10px] text-blue-400 hover:text-blue-300 hover:underline flex items-center gap-0.5 shrink-0"
          >
            <Globe className="h-2.5 w-2.5" />Ver no Dynatrace
          </a>
        )}
      </div>
    </div>
  );
}

// ── ProblemHeader (reutilizado nas 3 abas) ──────────────────────────────────────

function ProblemHeader({ problem, uiBaseUrl }: { problem: DynatraceProblem; uiBaseUrl?: string }) {
  const bg = SEV_BG[problem.severityLevel] ?? SEV_BG.CUSTOM_ALERT;
  const badgeColor = SEV_BADGE_COLOR[problem.severityLevel] ?? SEV_BADGE_COLOR.CUSTOM_ALERT;
  const duration = formatDuration(problem.startTime, problem.endTime);
  const startDate = new Date(problem.startTime).toLocaleString("pt-BR");

  return (
    <div className={`rounded-xl border p-4 space-y-3 ${bg}`}>
      <div className="flex items-start gap-3">
        <div className="mt-0.5">
          {problem.status === "CLOSED"
            ? <CheckCircle2 className="h-5 w-5 text-green-500" />
            : sevIcon(problem.severityLevel)
          }
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-base font-semibold leading-snug">{problem.title}</h2>
          <div className="flex items-center gap-1.5 flex-wrap mt-2">
            <span className={`inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-mono ${badgeColor}`}>
              {problem.displayId}
            </span>
            <span className={`inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-medium ${badgeColor}`}>
              {sevLabel(problem.severityLevel)}
            </span>
            <Badge variant="outline" className="text-xs">{impactLabel(problem.impactLevel)}</Badge>
            {problem.status === "CLOSED"
              ? <Badge className="text-xs bg-green-600 hover:bg-green-600 gap-1"><CheckCircle2 className="h-3 w-3" />Resolvido</Badge>
              : <Badge variant="destructive" className="text-xs">Aberto</Badge>
            }
          </div>
        </div>
      </div>
      <div className="flex items-center gap-4 text-xs text-muted-foreground border-t border-white/10 pt-2">
        <span className="flex items-center gap-1">
          <Clock className="h-3 w-3" /> Início: <strong className="text-foreground">{startDate}</strong>
        </span>
        <span className="flex items-center gap-1">
          <Activity className="h-3 w-3" /> Duração: <strong className="text-foreground">{duration}</strong>
        </span>
        {uiBaseUrl && (
          <a
            href={`${uiBaseUrl}/ui/apps/dynatrace.davis.problems/problem/${problem.problemId}`}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-auto flex items-center gap-1 text-blue-400 hover:text-blue-300 hover:underline"
          >
            <Globe className="h-3 w-3" />Abrir no Dynatrace
          </a>
        )}
      </div>
    </div>
  );
}

// ── Helpers de exibição ─────────────────────────────────────────────────────────

function EnvBadge({ env }: { env: string }) {
  const e = env.toLowerCase();
  const cls = e === "prd" || e === "prod"
    ? "bg-red-500/15 text-red-400 border-red-500/30"
    : e === "hlg" || e === "staging"
    ? "bg-yellow-500/15 text-yellow-400 border-yellow-500/30"
    : "bg-blue-500/15 text-blue-400 border-blue-500/30";
  return (
    <span className={`inline-flex items-center rounded border px-1.5 py-0 text-[10px] font-semibold tracking-wider ${cls}`}>
      {env.toUpperCase()}
    </span>
  );
}

function InfoRow({ icon, label, value, mono = false, subtle = false }: {
  icon: React.ReactNode; label: string; value: string; mono?: boolean; subtle?: boolean;
}) {
  if (!value) return null;
  return (
    <div className="flex items-center gap-1.5 min-w-0">
      <span className="text-muted-foreground/60 shrink-0">{icon}</span>
      <span className="text-muted-foreground text-[10px] shrink-0">{label}:</span>
      <span className={`text-[11px] truncate ${mono ? "font-mono" : ""} ${subtle ? "text-muted-foreground" : "text-foreground"}`}>
        {value}
      </span>
    </div>
  );
}

function SectionHeader({ icon, title, count, color = "text-muted-foreground" }: {
  icon: React.ReactNode; title: string; count?: number; color?: string;
}) {
  return (
    <h3 className={`text-xs font-semibold flex items-center gap-1.5 uppercase tracking-wide ${color}`}>
      {icon} {title}{count !== undefined && <span className="font-normal normal-case tracking-normal text-muted-foreground">({count})</span>}
    </h3>
  );
}

// Parseia nomes de processo Dynatrace: "SpringBoot - service-name - context"
function parseProcessDisplayName(name: string): { tech?: string; service?: string; context?: string } | null {
  const parts = name.split(" - ").map(s => s.trim()).filter(Boolean);
  if (parts.length >= 3) return { tech: parts[0], service: parts[1], context: parts[2] };
  if (parts.length === 2) return { tech: parts[0], service: parts[1] };
  return null;
}

// Detecta se o nome segue o padrão AKS: aks-<nodepool>-<digits>-vmss<hex>
const AKS_NODE_RE = /^aks-(.+?)-\d{5,8}-vmss[0-9a-f]+/i;
function extractAksNodePool(name: string): string | null {
  const m = name.match(AKS_NODE_RE);
  return m ? m[1] : null;
}

// Extrai env de um context string: "vendemais-sync-prd" → "prd"
function inferEnvFromContext(ctx: string): string | null {
  const lower = ctx.toLowerCase();
  if (lower.endsWith("-prd") || lower.includes("-prd-") || lower.includes("prod")) return "prd";
  if (lower.endsWith("-hlg") || lower.includes("-hlg-") || lower.includes("staging")) return "hlg";
  if (lower.endsWith("-dev") || lower.includes("-dev-")) return "dev";
  return null;
}

// Card expandível de entidade afetada — sempre abrível, mostra tudo disponível
function EntityCard({ entity, isRoot = false }: {
  entity: DynatraceProblem["affectedEntities"][0];
  isRoot?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const l = entity.labels;
  const hasLabels = !!l && Object.values(l).some(Boolean);

  // Detectar nó AKS e fazer lookup no registry
  const rawName = entity.name || entity.displayName || "";
  const aksPoolName = extractAksNodePool(rawName);

  const { data: aksLookup } = useQuery<NodePoolLookupResult>({
    queryKey: ["nodepool-registry-lookup", rawName],
    queryFn: () => apiClient.lookupNodePoolByEntityName(rawName),
    enabled: !!aksPoolName,
    staleTime: 10 * 60_000,
  });

  // Tentar extrair info do nome do processo (SpringBoot / NodeJS / etc.)
  const displayName = entity.displayName || entity.name || "";
  const parsed = parseProcessDisplayName(displayName);
  const isParseable = !!parsed?.tech && !!parsed?.service;

  // Caminho K8s: prioriza labels, fallback para campos diretos, fallback para parsed name
  const path = [
    entity.k8sCluster || l?.hostGroup,
    entity.k8sNamespace || l?.namespace,
    entity.k8sWorkload || l?.appName,
  ].filter(Boolean);

  const inferredEnv = l?.appEnvironment
    || (parsed?.context ? inferEnvFromContext(parsed.context) : null);

  return (
    <div className={`rounded-lg border text-xs ${isRoot ? "border-orange-500/30 bg-orange-500/5" : "border-border bg-muted/20"}`}>
      {/* Cabeçalho — sempre clicável para expandir */}
      <div
        className="flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-muted/30 transition-colors"
        onClick={() => setOpen(o => !o)}
      >
        <div className="flex-1 min-w-0 space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            {/* Nome principal: prioriza parsed tech+service, senão nome bruto */}
            {isParseable ? (
              <>
                <span className="text-muted-foreground/60 text-[10px] font-mono shrink-0">{parsed!.tech}</span>
                <span className="font-semibold truncate max-w-[300px]">{parsed!.service}</span>
              </>
            ) : (
              <span className="font-medium truncate max-w-[360px]">{displayName || entity.entityId.id}</span>
            )}
            <Badge variant="outline" className="text-[10px] font-mono py-0 h-4 shrink-0">
              {entityTypeLabel(entity.entityId.type)}
            </Badge>
            {l?.appVersion && (
              <Badge className="text-[10px] font-mono py-0 h-4 bg-blue-500/15 text-blue-400 border border-blue-500/30 shrink-0">
                v{l.appVersion}
              </Badge>
            )}
            {inferredEnv && <EnvBadge env={inferredEnv} />}
            {l?.stage && l.stage !== "stable" && (
              <Badge className="text-[10px] h-4 bg-purple-500/15 text-purple-400 border-purple-500/30">{l.stage}</Badge>
            )}
            {l?.isCanary === "true" && (
              <Badge className="text-[10px] h-4 bg-yellow-500/15 text-yellow-400 border-yellow-500/30">canary</Badge>
            )}
            {/* AKS node pool badge inline */}
            {aksPoolName && aksLookup?.found && aksLookup.matches.length > 0 && (
              <Badge className="text-[10px] h-4 bg-teal-500/15 text-teal-400 border border-teal-500/30 font-mono shrink-0">
                <Server className="h-2.5 w-2.5 mr-0.5" />
                {aksLookup.matches[0].cluster} › {aksPoolName}
              </Badge>
            )}
            {aksPoolName && !aksLookup?.found && (
              <Badge variant="outline" className="text-[10px] h-4 text-muted-foreground font-mono shrink-0">
                pool: {aksPoolName}
              </Badge>
            )}
          </div>

          {/* Contexto inferido do nome (vendemais-sync-prd) */}
          {isParseable && parsed?.context && !path.length && (
            <div className="flex items-center gap-1 text-muted-foreground text-[10px]">
              <Layers className="h-2.5 w-2.5 shrink-0" />
              <span className="font-mono">{parsed.context}</span>
            </div>
          )}

          {/* Caminho K8s se disponível */}
          {path.length > 0 && (
            <div className="flex items-center gap-1 text-muted-foreground text-[10px]">
              <Layers className="h-2.5 w-2.5 shrink-0" />
              {path.join(" › ")}
            </div>
          )}

          {/* Ownership inline */}
          {(l?.componentSquad || l?.componentJourney || l?.componentTribe) && (
            <div className="flex items-center gap-2 flex-wrap text-[10px] text-muted-foreground">
              {l?.componentSquad && <span className="flex items-center gap-0.5"><Users className="h-2.5 w-2.5" />{l.componentSquad}</span>}
              {l?.componentJourney && <span className="flex items-center gap-0.5"><Tag className="h-2.5 w-2.5" />{l.componentJourney}</span>}
              {l?.componentTribe && <span className="text-muted-foreground/60">tribo: {l.componentTribe}</span>}
            </div>
          )}
        </div>
        <span className="text-muted-foreground/40 shrink-0">
          {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        </span>
      </div>

      {/* Detalhes expandidos — sempre mostra o que tiver */}
      {open && (
        <div className="px-3 pb-3 border-t border-border/50 pt-2 space-y-2.5">

          {/* Entidade Dynatrace (sempre) */}
          <div className="space-y-0.5">
            <p className="text-[9px] font-semibold uppercase tracking-wider text-muted-foreground/50">Entidade Dynatrace</p>
            <InfoRow icon={<Database className="h-2.5 w-2.5" />} label="entity-id" value={entity.entityId.id} mono subtle />
            {entity.name && entity.name !== entity.displayName && (
              <InfoRow icon={<Cpu className="h-2.5 w-2.5" />} label="pod / instância" value={entity.name.replace(/[()]/g, "")} mono />
            )}
            {isParseable && (
              <>
                <InfoRow icon={<Package className="h-2.5 w-2.5" />} label="tecnologia" value={parsed!.tech ?? ""} />
                <InfoRow icon={<Server className="h-2.5 w-2.5" />} label="serviço" value={parsed!.service ?? ""} mono />
                {parsed?.context && <InfoRow icon={<Layers className="h-2.5 w-2.5" />} label="contexto/namespace" value={parsed.context} mono />}
              </>
            )}
          </div>

          {/* AKS Node Pool Registry — quando entidade é nó AKS */}
          {aksPoolName && (
            <div className="space-y-0.5 border-t border-border/30 pt-2">
              <p className="text-[9px] font-semibold uppercase tracking-wider text-teal-400/70">Node Pool AKS</p>
              <InfoRow icon={<Server className="h-2.5 w-2.5" />} label="node pool extraído" value={aksPoolName} mono />
              {aksLookup?.found && aksLookup.matches.map(m => (
                <div key={`${m.cluster}-${m.nodepool}`} className="pl-4 space-y-0.5">
                  <InfoRow icon={<Database className="h-2.5 w-2.5" />} label="cluster" value={m.cluster} mono />
                  {m.vm_size && <InfoRow icon={<Cpu className="h-2.5 w-2.5" />} label="vm-size" value={m.vm_size} mono />}
                  {m.mode && <InfoRow icon={<Shield className="h-2.5 w-2.5" />} label="mode" value={m.mode} />}
                  {m.os_sku && <InfoRow icon={<Globe className="h-2.5 w-2.5" />} label="os-sku" value={m.os_sku} />}
                  <InfoRow icon={<Layers className="h-2.5 w-2.5" />} label="nodes" value={String(m.node_count)} />
                </div>
              ))}
              {aksLookup && !aksLookup.found && (
                <p className="text-[10px] text-yellow-500/70 flex items-center gap-1 pl-4">
                  <AlertTriangle className="h-2.5 w-2.5 shrink-0" />
                  Node pool não encontrado no registry — use o botão "Escanear Clusters" para catalogar
                </p>
              )}
              {!aksLookup && (
                <p className="text-[10px] text-muted-foreground/50 pl-4">Consultando registry...</p>
              )}
            </div>
          )}

          {/* DTLabels — quando disponível */}
          {hasLabels && (
            <div className="space-y-2 border-t border-border/30 pt-2">
              <p className="text-[9px] font-semibold uppercase tracking-wider text-muted-foreground/50">DTLabels (OneAgent)</p>

              {(l!.appName || l!.releaseName || l!.helmChart || l!.appInstance || l!.stage || l!.deployedBy) && (
                <div className="space-y-0.5">
                  <p className="text-[9px] uppercase tracking-wider text-blue-400/70 font-semibold">Identidade</p>
                  <InfoRow icon={<Package className="h-2.5 w-2.5" />} label="app" value={l!.appName ?? ""} mono />
                  <InfoRow icon={<Rocket className="h-2.5 w-2.5" />} label="release" value={l!.releaseName ?? ""} mono />
                  <InfoRow icon={<Boxes className="h-2.5 w-2.5" />} label="helm-chart" value={l!.helmChart ?? ""} mono />
                  <InfoRow icon={<Cpu className="h-2.5 w-2.5" />} label="instância" value={l!.appInstance ?? ""} mono />
                  <InfoRow icon={<Shield className="h-2.5 w-2.5" />} label="estágio" value={l!.stage ?? ""} />
                  <InfoRow icon={<GitCommit className="h-2.5 w-2.5" />} label="deployed-by" value={l!.deployedBy ?? ""} />
                </div>
              )}

              {(l!.componentSquad || l!.componentJourney || l!.componentTribe || l!.componentName) && (
                <div className="space-y-0.5">
                  <p className="text-[9px] uppercase tracking-wider text-green-400/70 font-semibold">Ownership</p>
                  <InfoRow icon={<Users className="h-2.5 w-2.5" />} label="squad" value={l!.componentSquad ?? ""} />
                  <InfoRow icon={<Tag className="h-2.5 w-2.5" />} label="jornada" value={l!.componentJourney ?? ""} />
                  <InfoRow icon={<Globe className="h-2.5 w-2.5" />} label="tribo" value={l!.componentTribe ?? ""} />
                  <InfoRow icon={<Database className="h-2.5 w-2.5" />} label="componente" value={l!.componentName ?? ""} mono />
                </div>
              )}

              {(l!.namespace || l!.hostGroup) && (
                <div className="space-y-0.5">
                  <p className="text-[9px] uppercase tracking-wider text-yellow-400/70 font-semibold">Infra K8s</p>
                  <InfoRow icon={<Layers className="h-2.5 w-2.5" />} label="namespace" value={l!.namespace ?? ""} mono />
                  <InfoRow icon={<Server className="h-2.5 w-2.5" />} label="host-group (cluster AKS)" value={l!.hostGroup ?? ""} mono />
                </div>
              )}

              {(l!.githubRepoId || l!.appEnvironment) && (
                <div className="space-y-0.5">
                  <p className="text-[9px] uppercase tracking-wider text-purple-400/70 font-semibold">Rastreabilidade</p>
                  <InfoRow icon={<GitBranch className="h-2.5 w-2.5" />} label="github-repo-id" value={l!.githubRepoId ?? ""} mono subtle />
                  <InfoRow icon={<MapPin className="h-2.5 w-2.5" />} label="ambiente" value={l!.appEnvironment ?? ""} />
                </div>
              )}
            </div>
          )}

          {/* Aviso: sem DTLabels */}
          {!hasLabels && (
            <p className="text-[10px] text-muted-foreground/50 italic border-t border-border/30 pt-2">
              Sem DTLabels (OneAgent) — configure as tags <code className="bg-muted px-1 rounded">app.kubernetes.io/*</code> e <code className="bg-muted px-1 rounded">devops.k8s.io/*</code> nesta aplicação para enriquecer os dados
            </p>
          )}
        </div>
      )}
    </div>
  );
}

// ── Seção "O que investigar" — guia rápido contextual ──────────────────────────

const INVESTIGATION_GUIDE: Record<string, {
  icon: ReactNode;
  title: string;
  checks: string[];
  kubectl: string[];
}> = {
  RESOURCE_CONTENTION: {
    icon: <Cpu className="h-3.5 w-3.5" />,
    title: "Contenção de Recursos",
    checks: [
      "Compare o heap JVM (Xmx) com os memory limits do container — GC longo indica heap insuficiente ou memory leak",
      "Verifique se houve OOM Kill no pod (evento OOMKilled no describe)",
      "Confirme se o HPA está no limite máximo de réplicas para o serviço",
      "Analise se há memory leak comparando uso ao longo do tempo nas métricas",
      "Verifique requests/limits de CPU — throttling causa pausa de GC",
    ],
    kubectl: [
      "kubectl top pods -n <namespace> --sort-by=memory",
      "kubectl describe pod <pod> -n <namespace> | grep -A10 'Limits\\|OOM\\|Last State'",
      "kubectl get events -n <namespace> --field-selector=reason=OOMKilling",
      "kubectl logs <pod> -n <namespace> | grep -i 'OutOfMemoryError\\|GC\\|heap\\|Xmx'",
    ],
  },
  AVAILABILITY: {
    icon: <AlertCircle className="h-3.5 w-3.5" />,
    title: "Indisponibilidade de Serviço",
    checks: [
      "Verifique pods em CrashLoopBackOff, Error ou Pending",
      "Confirme se as probes (liveness/readiness) estão configuradas e passando",
      "Analise os logs do pod anterior ao crash (--previous)",
      "Verifique se há problemas de image pull (ImagePullBackOff)",
    ],
    kubectl: [
      "kubectl get pods -n <namespace> | grep -v Running",
      "kubectl describe pod <pod> -n <namespace>",
      "kubectl logs <pod> -n <namespace> --previous --tail=100",
      "kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20",
    ],
  },
  ERROR: {
    icon: <AlertTriangle className="h-3.5 w-3.5" />,
    title: "Taxa de Erros na Aplicação",
    checks: [
      "Verifique se houve deploy recente — correlacione o horário do erro com a aba GitHub",
      "Confirme se as dependências externas (BD, cache, filas) estão respondendo",
      "Analise stack traces nos logs do pod",
      "Verifique circuit breakers abertos no serviço",
    ],
    kubectl: [
      "kubectl logs <pod> -n <namespace> --tail=200 | grep -i 'ERROR\\|Exception\\|FATAL'",
      "kubectl get events -n <namespace> --sort-by='.lastTimestamp'",
      "kubectl rollout history deployment/<deploy> -n <namespace>",
    ],
  },
  PERFORMANCE: {
    icon: <Activity className="h-3.5 w-3.5" />,
    title: "Degradação de Performance",
    checks: [
      "Verifique se o HPA está no máximo de réplicas (sem margem para escalar)",
      "Analise se há saturação de conexões com banco de dados ou cache",
      "Confirme CPU throttling — requests muito baixos causam latência artificial",
      "Verifique se o tráfego aumentou (evento de campanha/pico) sem auto-scale",
    ],
    kubectl: [
      "kubectl top pods -n <namespace> --sort-by=cpu",
      "kubectl get hpa -n <namespace>",
      "kubectl describe hpa <hpa-name> -n <namespace> | grep -A5 'Conditions\\|Current\\|Desired'",
    ],
  },
  CUSTOM_ALERT: {
    icon: <Info className="h-3.5 w-3.5" />,
    title: "Alerta Customizado",
    checks: [
      "Verifique a regra do alerta no Dynatrace (Settings → Anomaly Detection)",
      "Analise as métricas customizadas que dispararam o alerta",
      "Confirme se é um alerta esperado (deploy, manutenção programada)",
    ],
    kubectl: [
      "kubectl get events -n <namespace> --sort-by='.lastTimestamp'",
    ],
  },
};

function QuickInvestigation({
  severityType,
  mgmtZones,
  entityNames,
}: {
  severityType: string;
  mgmtZones: { id: string; name: string }[];
  entityNames: string[];
}) {
  const [showKubectl, setShowKubectl] = useState(false);
  const guide = INVESTIGATION_GUIDE[severityType] ?? INVESTIGATION_GUIDE.CUSTOM_ALERT;

  // Tentar extrair namespace/cluster das management zones e nomes de entidade
  const contextHints: string[] = [];
  mgmtZones.forEach(z => {
    const m = z.name.match(/^\d+-(.+)$/);
    if (m) contextHints.push(m[1].trim());
  });
  entityNames.forEach(n => {
    const parsed = parseProcessDisplayName(n);
    if (parsed?.context) contextHints.push(parsed.context);
  });
  const uniqueHints = [...new Set(contextHints)];

  return (
    <div className="rounded-lg border border-blue-500/20 bg-blue-500/5 overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 bg-blue-500/10 border-b border-blue-500/15">
        <span className="text-blue-400">{guide.icon}</span>
        <span className="text-xs font-semibold text-blue-300">O que investigar — {guide.title}</span>
        {uniqueHints.length > 0 && (
          <div className="ml-auto flex items-center gap-1 flex-wrap">
            {uniqueHints.slice(0, 2).map(h => (
              <span key={h} className="text-[10px] font-mono bg-blue-500/15 text-blue-300 px-1.5 py-0.5 rounded">{h}</span>
            ))}
          </div>
        )}
      </div>
      <div className="px-3 py-2.5 space-y-2">
        <ul className="space-y-1.5">
          {guide.checks.map((c, i) => (
            <li key={i} className="flex items-start gap-2 text-xs text-foreground/80">
              <span className="text-blue-400 mt-0.5 shrink-0 font-bold text-[10px]">{i + 1}.</span>
              <span>{c}</span>
            </li>
          ))}
        </ul>

        {guide.kubectl.length > 0 && (
          <div className="border-t border-blue-500/15 pt-2">
            <button
              className="flex items-center gap-1 text-[10px] text-blue-400/70 hover:text-blue-300 transition-colors"
              onClick={() => setShowKubectl(s => !s)}
            >
              {showKubectl ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              Comandos kubectl para investigação
            </button>
            {showKubectl && (
              <div className="mt-1.5 space-y-1">
                {guide.kubectl.map((cmd, i) => {
                  // Substituir <namespace> pelo hint se disponível
                  const filled = uniqueHints.length > 0
                    ? cmd.replace("<namespace>", uniqueHints[0])
                    : cmd;
                  return (
                    <code key={i} className="block text-[10px] font-mono bg-black/30 text-green-400 px-2 py-1 rounded leading-relaxed">
                      {filled}
                    </code>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Aba Detalhes ────────────────────────────────────────────────────────────────

function DetailsTab({ problem, uiBaseUrl }: { problem: DynatraceProblem; uiBaseUrl?: string }) {
  const affectedEntities = problem.affectedEntities ?? [];
  const impactedEntities = (problem.impactedEntities ?? []).filter(
    e => !affectedEntities.some(a => a.entityId.id === e.entityId.id),
  );
  const k8sWorkloads = (problem.k8sWorkloads ?? []).filter(w => w.Workload || w.AppName);
  const mgmtZones = problem.managementZones ?? [];

  // Agregados cross-entidade para visão rápida de ownership
  const allLabels = affectedEntities.map(e => e.labels).filter(Boolean);
  const squads = [...new Set(allLabels.map(l => l?.componentSquad).filter(Boolean))];
  const journeys = [...new Set(allLabels.map(l => l?.componentJourney).filter(Boolean))];
  const tribes = [...new Set(allLabels.map(l => l?.componentTribe).filter(Boolean))];
  const envs = [...new Set([
    ...allLabels.map(l => l?.appEnvironment).filter(Boolean),
    ...k8sWorkloads.map(w => w.Environment).filter(Boolean),
  ])];
  const gitRepos = [...new Set([
    ...allLabels.map(l => l?.githubRepoId).filter(Boolean),
    ...k8sWorkloads.map(w => w.GitHubRepoID).filter(Boolean),
  ])];
  const stages = [...new Set(allLabels.map(l => l?.stage).filter(Boolean))];
  const deployedBys = [...new Set(allLabels.map(l => l?.deployedBy).filter(Boolean))];

  const hasOwnership = squads.length > 0 || journeys.length > 0 || tribes.length > 0;
  const hasDevOps = gitRepos.length > 0 || stages.length > 0 || deployedBys.length > 0;

  // Entidades por tipo
  const entityGroups = affectedEntities.reduce((acc, e) => {
    const t = e.entityId.type;
    if (!acc[t]) acc[t] = [];
    acc[t].push(e);
    return acc;
  }, {} as Record<string, typeof affectedEntities>);

  return (
    <div className="space-y-5">
      <ProblemHeader problem={problem} uiBaseUrl={uiBaseUrl} />

      {/* ── O que investigar — guia contextual por tipo de problem ── */}
      <QuickInvestigation
        severityType={problem.severityLevel}
        mgmtZones={problem.managementZones ?? []}
        entityNames={(problem.affectedEntities ?? []).map(e => e.displayName || e.name || "").filter(Boolean)}
      />

      {/* ── Painel de Ownership & DevOps (resumo aggregado) ── */}
      {(hasOwnership || hasDevOps || envs.length > 0) && (
        <section className="rounded-lg border border-border bg-muted/10 px-4 py-3 space-y-3">
          <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Visão Geral das Aplicações Afetadas</p>

          <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-xs">
            {/* Ambientes */}
            {envs.length > 0 && (
              <div className="flex items-center gap-2 flex-wrap col-span-2">
                <span className="text-muted-foreground text-[10px] w-16 shrink-0">Ambiente:</span>
                {envs.map(e => <EnvBadge key={e} env={e!} />)}
              </div>
            )}
            {/* Squads */}
            {squads.length > 0 && (
              <div className="flex items-start gap-2 col-span-2">
                <Users className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                <div>
                  <span className="text-muted-foreground text-[10px]">Squad(s): </span>
                  {squads.map(s => (
                    <Badge key={s} variant="outline" className="text-[10px] mr-1 border-green-500/30 text-green-400">{s}</Badge>
                  ))}
                </div>
              </div>
            )}
            {/* Jornadas */}
            {journeys.length > 0 && (
              <div className="flex items-start gap-2 col-span-2">
                <Tag className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                <div>
                  <span className="text-muted-foreground text-[10px]">Jornada(s): </span>
                  {journeys.map(j => (
                    <Badge key={j} variant="outline" className="text-[10px] mr-1 border-purple-500/30 text-purple-400">{j}</Badge>
                  ))}
                </div>
              </div>
            )}
            {/* Tribos */}
            {tribes.length > 0 && (
              <div className="flex items-center gap-2">
                <Globe className="h-3 w-3 text-muted-foreground shrink-0" />
                <span className="text-muted-foreground text-[10px]">Tribo: </span>
                <span className="text-[11px]">{tribes.join(", ")}</span>
              </div>
            )}
            {/* Estágios */}
            {stages.length > 0 && (
              <div className="flex items-center gap-2">
                <Shield className="h-3 w-3 text-muted-foreground shrink-0" />
                <span className="text-muted-foreground text-[10px]">Estágio: </span>
                <span className="text-[11px]">{stages.join(", ")}</span>
              </div>
            )}
            {/* Deploy por */}
            {deployedBys.length > 0 && (
              <div className="flex items-center gap-2">
                <Rocket className="h-3 w-3 text-muted-foreground shrink-0" />
                <span className="text-muted-foreground text-[10px]">Deploy via: </span>
                <span className="text-[11px] font-mono">{deployedBys.join(", ")}</span>
              </div>
            )}
            {/* GitHub Repos */}
            {gitRepos.length > 0 && (
              <div className="flex items-start gap-2 col-span-2">
                <GitBranch className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                <div className="min-w-0">
                  <span className="text-muted-foreground text-[10px]">Repos GitHub: </span>
                  {gitRepos.map(r => (
                    <span key={r} className="inline-block font-mono text-[10px] text-blue-400 mr-2">{r}</span>
                  ))}
                </div>
              </div>
            )}
          </div>
        </section>
      )}

      {/* ── Causa Raiz ── */}
      {problem.rootCauseEntity && (
        <section className="space-y-2">
          <SectionHeader icon={<Target className="h-3.5 w-3.5" />} title="Causa Raiz (Davis AI)" color="text-orange-400" />
          <EntityCard entity={problem.rootCauseEntity as any} isRoot />
        </section>
      )}

      {/* ── Workloads K8s (detalhes completos) ── */}
      {k8sWorkloads.length > 0 && (
        <section className="space-y-2">
          <SectionHeader icon={<Layers className="h-3.5 w-3.5" />} title="Workloads K8s" count={k8sWorkloads.length} />
          <div className="space-y-2">
            {k8sWorkloads.map((w, i) => (
              <div key={i} className="rounded-lg border bg-muted/20 px-3 py-2.5 text-xs space-y-2">
                {/* Linha 1: caminho + versão */}
                <div className="flex items-center gap-1.5 flex-wrap">
                  {w.Cluster && (
                    <><span className="text-muted-foreground font-mono text-[10px] bg-muted/50 px-1.5 py-0.5 rounded">{w.Cluster}</span>
                    <ArrowRight className="h-2.5 w-2.5 text-muted-foreground/40 shrink-0" /></>
                  )}
                  <span className="text-muted-foreground font-mono text-[10px] bg-muted/50 px-1.5 py-0.5 rounded">{w.Namespace}</span>
                  <ArrowRight className="h-2.5 w-2.5 text-muted-foreground/40 shrink-0" />
                  <span className="font-semibold font-mono">{w.Workload || w.AppName}</span>
                  {w.AppName && w.AppName !== w.Workload && (
                    <span className="text-muted-foreground text-[10px]">({w.AppName})</span>
                  )}
                  <div className="ml-auto flex items-center gap-1.5 flex-wrap">
                    {w.AppVersion && (
                      <Badge className="text-[10px] font-mono h-5 bg-blue-500/15 text-blue-400 border border-blue-500/30">
                        v{w.AppVersion}
                      </Badge>
                    )}
                    {w.Environment && <EnvBadge env={w.Environment} />}
                  </div>
                </div>
                {/* Linha 2: pod + repo */}
                {(w.PodName || w.GitHubRepoID) && (
                  <div className="flex items-center gap-3 text-[10px] text-muted-foreground flex-wrap border-t border-border/30 pt-1.5">
                    {w.PodName && (
                      <span className="flex items-center gap-1">
                        <Cpu className="h-2.5 w-2.5" />
                        <span className="font-mono">{w.PodName}</span>
                      </span>
                    )}
                    {w.GitHubRepoID && (
                      <span className="flex items-center gap-1">
                        <GitBranch className="h-2.5 w-2.5" />
                        <span className="font-mono text-blue-400/70">{w.GitHubRepoID}</span>
                      </span>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      )}

      {/* ── Entidades Afetadas (agrupadas por tipo, expandíveis) ── */}
      {affectedEntities.length > 0 && (
        <section className="space-y-2">
          <SectionHeader icon={<Server className="h-3.5 w-3.5" />} title="Entidades Afetadas" count={affectedEntities.length} />
          <div className="space-y-3">
            {Object.entries(entityGroups).map(([type, entities]) => (
              <div key={type}>
                <p className="text-[10px] font-semibold text-muted-foreground/70 uppercase tracking-wider mb-1.5 flex items-center gap-1">
                  {entityTypeLabel(type)} <span className="font-normal">({entities.length})</span>
                </p>
                <div className="space-y-1.5">
                  {entities.map((e, i) => (
                    <EntityCard key={i} entity={e} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* ── Propagação do Impacto ── */}
      {impactedEntities.length > 0 && (
        <section className="space-y-2">
          <SectionHeader icon={<Network className="h-3.5 w-3.5" />} title="Propagação do Impacto" count={impactedEntities.length} />
          <div className="space-y-1">
            {impactedEntities.map((e, i) => (
              <EntityCard key={i} entity={e} />
            ))}
          </div>
        </section>
      )}

      {/* ── Management Zones ── */}
      {mgmtZones.length > 0 && (
        <section className="space-y-2">
          <SectionHeader icon={<MapPin className="h-3.5 w-3.5" />} title="Management Zones" />
          <div className="flex flex-wrap gap-1.5">
            {mgmtZones.map(z => (
              <Badge key={z.id} variant="secondary" className="text-xs">{z.name}</Badge>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

// ── Aba Diagnóstico IA ──────────────────────────────────────────────────────────

function DiagnosticTab({
  problem,
  uiBaseUrl,
  onAnalyze,
  analyzing,
  analysisResult,
}: {
  problem: DynatraceProblem;
  uiBaseUrl?: string;
  onAnalyze: () => void;
  analyzing: boolean;
  analysisResult: string;
}) {
  return (
    <div className="space-y-4">
      <ProblemHeader problem={problem} uiBaseUrl={uiBaseUrl} />

      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold flex items-center gap-1.5">
            <Bot className="h-4 w-4" /> Diagnóstico com IA
          </h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            A IA analisa métricas, evidências Davis, topologia, traces e eventos para sugerir próximos passos.
          </p>
        </div>
        <Button size="sm" variant={analysisResult ? "ghost" : "default"} onClick={onAnalyze} disabled={analyzing} className="shrink-0">
          {analyzing
            ? <><RefreshCw className="h-3 w-3 mr-1.5 animate-spin" />Analisando...</>
            : analysisResult
              ? <><RefreshCw className="h-3 w-3 mr-1.5" />Re-analisar</>
              : <><Bot className="h-3 w-3 mr-1.5" />Analisar com IA</>
          }
        </Button>
      </div>

      {analyzing && (
        <div className="flex flex-col items-center gap-2 py-8 text-center">
          <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
          <p className="text-sm font-medium">Analisando com IA...</p>
          <p className="text-xs text-muted-foreground">Coletando métricas, evidências e correlações K8s do Dynatrace</p>
        </div>
      )}

      {!analyzing && !analysisResult && (
        <div className="flex flex-col items-center gap-2 py-12 text-center text-muted-foreground">
          <Bot className="h-10 w-10 opacity-20" />
          <p className="text-sm">Clique em <strong>Analisar com IA</strong> para obter um diagnóstico detalhado.</p>
        </div>
      )}

      {analysisResult && !analyzing && (
        <div className="rounded-lg border bg-muted/10 p-4">
          <div className="prose prose-sm dark:prose-invert max-w-none">
            <ReactMarkdown>{analysisResult}</ReactMarkdown>
          </div>
        </div>
      )}
    </div>
  );
}

// ── ProblemDetailPanel — wrapper com abas ───────────────────────────────────────

function ProblemDetailPanel({
  problem,
  aiEmail,
  uiBaseUrl,
  onAnalyze,
  analyzing,
  analysisResult,
}: {
  problem: DynatraceProblem;
  aiEmail: string;
  uiBaseUrl?: string;
  onAnalyze: () => void;
  analyzing: boolean;
  analysisResult: string;
}) {
  const hasWorkloads = (problem.k8sWorkloads ?? []).some(w => w.AppName);

  return (
    <Tabs defaultValue="details" className="w-full">
      <TabsList className="w-full grid grid-cols-4 h-8 mb-4">
        <TabsTrigger value="details" className="text-xs gap-1.5">
          <Info className="h-3 w-3" />Detalhes
        </TabsTrigger>
        <TabsTrigger value="metrics" className="text-xs gap-1.5">
          <BarChart3 className="h-3 w-3" />Métricas
        </TabsTrigger>
        <TabsTrigger value="github" className="text-xs gap-1.5">
          <GitBranch className="h-3 w-3" />
          <span>GitHub</span>
          {hasWorkloads && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
        </TabsTrigger>
        <TabsTrigger value="ai" className="text-xs gap-1.5">
          <Bot className="h-3 w-3" />IA
          {analyzing && <RefreshCw className="h-2.5 w-2.5 animate-spin" />}
          {analysisResult && !analyzing && <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-500" />}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="details" className="mt-0">
        <DetailsTab problem={problem} uiBaseUrl={uiBaseUrl} />
      </TabsContent>

      <TabsContent value="metrics" className="mt-0 space-y-6">
        <DynatraceMetricsPanel
          problemId={problem.problemId}
          aiEmail={aiEmail}
          problemTitle={problem.title}
        />
        <DynatraceContextPanel
          problemId={problem.problemId}
          aiEmail={aiEmail}
        />
      </TabsContent>

      <TabsContent value="github" className="mt-0">
        <DynatraceGitHubSection problem={problem} />
      </TabsContent>

      <TabsContent value="ai" className="mt-0">
        <DiagnosticTab
          problem={problem}
          uiBaseUrl={uiBaseUrl}
          onAnalyze={onAnalyze}
          analyzing={analyzing}
          analysisResult={analysisResult}
        />
      </TabsContent>
    </Tabs>
  );
}

// ── DynatraceTab ────────────────────────────────────────────────────────────────

interface DynatraceTabProps {
  selectedCluster?: string;
}

export function DynatraceTab({ selectedCluster: _cluster }: DynatraceTabProps) {
  const aiEmail = localStorage.getItem("ai_email") ?? "";
  const queryClient = useQueryClient();

  const [selectedProblem, setSelectedProblem] = useState<DynatraceProblem | null>(null);
  const [analysisResult, setAnalysisResult] = useState<string>("");
  const [analyzingId, setAnalyzingId] = useState<string | null>(null);

  // Filtro por management zone / tag (na própria aba)
  const [filterInput, setFilterInput] = useState("");
  const [activeFilter, setActiveFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("OPEN"); // OPEN | CLOSED | ALL
  const filterInputRef = useRef<HTMLInputElement>(null);

  const applyFilter = () => {
    const f = filterInput.trim();
    setActiveFilter(f);
    setSelectedProblem(null);
    setAnalysisResult("");
    queryClient.invalidateQueries({ queryKey: ["dynatrace-problems", aiEmail, f, statusFilter] });
  };

  const clearFilter = () => {
    setFilterInput("");
    setActiveFilter("");
    setSelectedProblem(null);
    setAnalysisResult("");
  };

  const { data, isLoading, refetch, isRefetching } = useQuery({
    queryKey: ["dynatrace-problems", aiEmail, activeFilter, statusFilter],
    queryFn: () => apiClient.getDynatraceProblems(aiEmail, activeFilter || undefined, statusFilter),
    enabled: !!aiEmail,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  // Scan do registry de node pools (cataloga clusters para correlação AKS)
  const [scanStatus, setScanStatus] = useState<"idle" | "scanning" | "done" | "error">("idle");
  const scanRegistryMutation = useMutation({
    mutationFn: () => apiClient.scanNodePoolRegistry(),
    onMutate: () => setScanStatus("scanning"),
    onSuccess: () => {
      setScanStatus("done");
      queryClient.invalidateQueries({ queryKey: ["nodepool-registry-lookup"] });
      setTimeout(() => setScanStatus("idle"), 3000);
    },
    onError: () => {
      setScanStatus("error");
      setTimeout(() => setScanStatus("idle"), 3000);
    },
  });

  const analyzeMutation = useMutation({
    mutationFn: (p: DynatraceProblem) =>
      apiClient.analyzeDynatraceProblem(p.problemId, aiEmail),
    onMutate: (p) => {
      setAnalyzingId(p.problemId);
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

  // ── Não configurado ────────────────────────────────────────────────────────────
  if (!aiEmail) {
    return (
      <div className="flex items-center justify-center h-96">
        <Card className="max-w-md w-full">
          <CardContent className="pt-6 text-center space-y-2">
            <AlertTriangle className="h-10 w-10 text-yellow-500 mx-auto" />
            <p className="font-medium">Email não configurado</p>
            <p className="text-sm text-muted-foreground">Configure seu email em <strong>AI Settings</strong>.</p>
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
            <p className="text-sm text-muted-foreground">Configure URL e token em <strong>AI Settings → Dynatrace</strong>.</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  const problems = data?.problems ?? [];
  const uiBaseUrl = data?.ui_base_url ?? "";

  // ── Painel esquerdo ────────────────────────────────────────────────────────────
  const leftContent = (
    <div className="flex flex-col gap-3 h-full">
      {/* Seletor de status */}
      <div className="flex gap-1">
        {(["OPEN", "CLOSED", "ALL"] as const).map(s => (
          <Button
            key={s}
            size="sm"
            variant={statusFilter === s ? "default" : "outline"}
            className="h-7 text-xs px-2.5 flex-1"
            onClick={() => {
              setStatusFilter(s);
              setSelectedProblem(null);
              setAnalysisResult("");
              queryClient.invalidateQueries({ queryKey: ["dynatrace-problems", aiEmail, activeFilter, s] });
            }}
          >
            {s === "OPEN" ? "Abertos" : s === "CLOSED" ? "Fechados" : "Todos"}
          </Button>
        ))}
      </div>

      {/* Filtro por tag/management zone */}
      <div className="flex gap-1.5">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
          <Input
            ref={filterInputRef}
            placeholder="Filtrar por Management Zone ou tag:..."
            value={filterInput}
            onChange={e => setFilterInput(e.target.value)}
            onKeyDown={e => e.key === "Enter" && applyFilter()}
            className="pl-7 h-8 text-xs"
          />
        </div>
        {activeFilter ? (
          <Button size="sm" variant="ghost" className="h-8 w-8 p-0 shrink-0" onClick={clearFilter} title="Limpar filtro">
            <X className="h-3.5 w-3.5" />
          </Button>
        ) : (
          <Button size="sm" variant="secondary" className="h-8 shrink-0 text-xs px-2.5" onClick={applyFilter}>
            Filtrar
          </Button>
        )}
      </div>

      {/* Status */}
      {data && !data.dt_not_configured && (
        <div className="text-[10px] text-muted-foreground flex items-center gap-1.5">
          {activeFilter && (
            <Badge variant="outline" className="text-[10px] gap-1">
              <MapPin className="h-2.5 w-2.5" />{activeFilter}
            </Badge>
          )}
          <span>
            {data.total === 0 ? "Nenhum problem encontrado" : `${problems.length} de ${data.total} problem(s)`}
          </span>
          <span className="ml-auto">{new Date(data.fetched_at).toLocaleTimeString("pt-BR")}</span>
        </div>
      )}

      {/* Lista */}
      <div className="flex-1 overflow-auto space-y-2 pr-0.5">
        {isLoading || isRefetching ? (
          <div className="flex items-center justify-center py-16">
            <RefreshCw className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : problems.length === 0 ? (
          <div className="text-center py-16 text-sm text-muted-foreground">
            {activeFilter ? `Nenhum problem para "${activeFilter}"` : "Nenhum problem aberto"}
          </div>
        ) : (
          problems.map(p => (
            <ProblemCard
              key={p.problemId}
              problem={p}
              selected={selectedProblem?.problemId === p.problemId}
              uiBaseUrl={uiBaseUrl}
              onClick={() => {
                setSelectedProblem(p);
                setAnalysisResult("");
              }}
            />
          ))
        )}
      </div>
    </div>
  );

  // ── Painel direito ─────────────────────────────────────────────────────────────
  const rightContent = !selectedProblem ? (
    <div className="flex flex-col items-center justify-center h-full gap-3 text-center px-6 py-20 text-muted-foreground">
      <AlertCircle className="h-10 w-10 opacity-20" />
      <p className="text-sm">Selecione um problem à esquerda para ver os detalhes completos.</p>
    </div>
  ) : (
    <div className="overflow-auto h-full pr-0.5">
      <ProblemDetailPanel
        problem={selectedProblem}
        aiEmail={aiEmail}
        uiBaseUrl={uiBaseUrl}
        analyzing={analyzingId === selectedProblem.problemId}
        analysisResult={analysisResult}
        onAnalyze={() => analyzeMutation.mutate(selectedProblem)}
      />
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
                <Badge variant={problems.length > 0 ? "destructive" : "secondary"} className="text-xs">
                  {problems.length}
                </Badge>
              )}
              {/* Botão de scan do registry de node pools */}
              <Button
                variant="ghost"
                size="sm"
                className={`h-6 px-2 text-[10px] gap-1 ${
                  scanStatus === "done" ? "text-teal-400" :
                  scanStatus === "error" ? "text-red-400" :
                  "text-muted-foreground"
                }`}
                onClick={() => scanRegistryMutation.mutate()}
                disabled={scanStatus === "scanning"}
                title="Escanear clusters para catalogar node pools (correlação aks-<pool>-vmss*)"
              >
                {scanStatus === "scanning"
                  ? <><RefreshCw className="h-2.5 w-2.5 animate-spin" />Escaneando...</>
                  : scanStatus === "done"
                  ? <><Server className="h-2.5 w-2.5" />Catalogado!</>
                  : scanStatus === "error"
                  ? <><AlertTriangle className="h-2.5 w-2.5" />Erro</>
                  : <><Server className="h-2.5 w-2.5" />Escanear Clusters</>
                }
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => refetch()}
                disabled={isRefetching || isLoading}
                title="Atualizar"
              >
                <RefreshCw className={`h-3 w-3 ${isRefetching ? "animate-spin" : ""}`} />
              </Button>
            </div>
          ),
          content: leftContent,
        }}
        rightPanel={{
          title: selectedProblem ? selectedProblem.displayId + " — Detalhes" : "Detalhes do Problem",
          content: rightContent,
        }}
      />
    </div>
  );
}
