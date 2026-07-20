import { useState, useRef, useCallback, useEffect, type ReactNode } from "react";
import { AreaChart, Area, XAxis, YAxis, Tooltip as RechartsTooltip, ResponsiveContainer } from "recharts";
import jsPDF from "jspdf";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { DynatraceMetricsPanel, EntityMetricsSection } from "@/components/DynatraceMetricsPanel";
import { DynatraceContextPanel } from "@/components/DynatraceContextPanel";
import { DynatraceGitHubSection } from "@/components/DynatraceGitHubSection";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AlertTriangle, RefreshCw, Bot, Clock, Layers, AlertCircle,
  Info, Target, Server, Search, Network, MapPin, ArrowRight,
  Activity, X, BarChart3, GitBranch, Users, Tag, Cpu,
  Package, GitCommit, Rocket, ChevronDown, ChevronRight,
  Shield, Globe, Database, Boxes, CheckCircle2, Maximize2, ZoomIn, ZoomOut,
  ListChecks, Share2, FileDown, FileText, Microscope,
} from "lucide-react";
import {
  Accordion, AccordionContent, AccordionItem, AccordionTrigger,
} from "@/components/ui/accordion";
import { addLogoHeaderToPDF } from "@/lib/logoUtils";
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

  // Dados de topologia (call chain)
  const callsToCount = entity.callsTo?.length ?? 0;
  const calledByCount = entity.calledBy?.length ?? 0;
  const hasTopology = callsToCount > 0 || calledByCount > 0;

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
    <div className={`rounded-lg border text-xs ${isRoot ? "border-orange-500/40 bg-orange-500/5" : "border-border bg-muted/20"}`}>
      {/* Cabeçalho — sempre clicável para expandir */}
      <div
        className="flex items-center gap-2 px-3 py-2 cursor-pointer hover:bg-muted/30 transition-colors"
        onClick={() => setOpen(o => !o)}
      >
        <div className="flex-1 min-w-0 space-y-0.5">
          <div className="flex items-center gap-1.5 flex-wrap">
            {/* Badge causa raiz (destaque) */}
            {isRoot && (
              <span className="inline-flex items-center gap-0.5 rounded border border-orange-500/50 bg-orange-500/15 px-1.5 py-0 text-[9px] font-semibold text-orange-400 shrink-0">
                <Target className="h-2.5 w-2.5" />CAUSA RAIZ
              </span>
            )}
            {/* Nome principal: prioriza parsed tech+service, senão nome bruto */}
            {isParseable ? (
              <>
                <span className="text-muted-foreground/60 text-[10px] font-mono shrink-0">{parsed!.tech}</span>
                <span className="font-semibold truncate max-w-[300px]">{parsed!.service}</span>
              </>
            ) : (
              <span className={`font-medium truncate max-w-[360px] ${isRoot ? "text-orange-300" : ""}`}>{displayName || entity.entityId.id}</span>
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

          {/* Topologia de chamadas (quando disponível) */}
          {hasTopology && (
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
              <Network className="h-2.5 w-2.5 shrink-0 text-blue-400/60" />
              {calledByCount > 0 && <span className="text-muted-foreground/70"><span className="text-blue-400/80 font-mono">{calledByCount}</span> chamam esta</span>}
              {callsToCount > 0 && <span className="text-muted-foreground/70">chama <span className="text-cyan-400/80 font-mono">{callsToCount}</span> downstream</span>}
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

          {/* Topologia de chamadas (quando disponível via GetEntity) */}
          {hasTopology && (
            <div className="space-y-1 border-t border-border/30 pt-2">
              <p className="text-[9px] font-semibold uppercase tracking-wider text-blue-400/70">Topologia (call chain)</p>
              {calledByCount > 0 && (
                <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <ArrowRight className="h-2.5 w-2.5 rotate-180 text-blue-400/60 shrink-0" />
                  <span><span className="text-blue-400 font-mono">{calledByCount}</span> entidade(s) upstream chamam esta</span>
                </div>
              )}
              {callsToCount > 0 && (
                <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <ArrowRight className="h-2.5 w-2.5 text-cyan-400/60 shrink-0" />
                  <span>Chama <span className="text-cyan-400 font-mono">{callsToCount}</span> entidade(s) downstream</span>
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

// ── DTResizeDivider — divisor de coluna arrastável ───────────────────────────────

function DTResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => {
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [onDrag]);

  return (
    <div
      className="w-2 flex-shrink-0 mx-1 rounded bg-border/30 hover:bg-primary/50 active:bg-primary cursor-col-resize transition-colors self-stretch"
      onMouseDown={(e) => {
        dragging.current = true;
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    />
  );
}

// ── Visual Resolution Path (grafo SVG com zoom modal) ───────────────────────────

const SEV_NODE_COLOR: Record<string, string> = {
  AVAILABILITY:        "#ef4444",
  ERROR:               "#f97316",
  PERFORMANCE:         "#eab308",
  RESOURCE_CONTENTION: "#eab308",
  CUSTOM_ALERT:        "#3b82f6",
};

// Ícone SVG por tipo de entidade (inline, monochrome)
function entityIcon(type: string, cx: number, cy: number, size = 14, color = "#f1f5f9") {
  switch (type) {
    case "SERVICE":
      return <g key={`icon-${cx}-${cy}`}>
        <circle cx={cx} cy={cy} r={size / 2} fill="none" stroke={color} strokeWidth="1.4" />
        <circle cx={cx} cy={cy} r={size / 4} fill={color} />
      </g>;
    case "HOST":
      return <rect key={`icon-${cx}-${cy}`} x={cx - size/2} y={cy - size/2} width={size} height={size} rx={2} fill="none" stroke={color} strokeWidth="1.4" />;
    case "KUBERNETES_NODE": case "KUBERNETES_CLUSTER":
      return <g key={`icon-${cx}-${cy}`}>
        <polygon points={`${cx},${cy-size/2} ${cx+size/2},${cy+size/4} ${cx-size/2},${cy+size/4}`} fill="none" stroke={color} strokeWidth="1.4" />
      </g>;
    case "PROCESS_GROUP": case "PROCESS_GROUP_INSTANCE":
      return <g key={`icon-${cx}-${cy}`}>
        <rect x={cx-size/2} y={cy-size/3} width={size} height={size/1.5} rx={3} fill="none" stroke={color} strokeWidth="1.4"/>
        <line x1={cx-size/4} y1={cy} x2={cx+size/4} y2={cy} stroke={color} strokeWidth="1.2"/>
      </g>;
    default:
      return <circle key={`icon-${cx}-${cy}`} cx={cx} cy={cy} r={size / 2} fill="none" stroke={color} strokeWidth="1.4" />;
  }
}

// Quebra nome em 2 linhas de até `maxChars` chars cada.
function splitLabel(name: string, maxChars = 20): [string, string | undefined] {
  if (name.length <= maxChars) return [name, undefined];
  const breakAt = Math.max(name.lastIndexOf(" ", maxChars), name.lastIndexOf("-", maxChars));
  if (breakAt > 3) {
    const l1 = name.slice(0, breakAt + 1).trimEnd();
    const rest = name.slice(breakAt + 1).trim();
    return [l1, rest.length > maxChars ? rest.slice(0, maxChars - 1) + "…" : rest];
  }
  return [name.slice(0, maxChars - 1) + "…", name.slice(maxChars - 1, maxChars * 2 - 2) + (name.length > maxChars * 2 - 2 ? "…" : "")];
}

// ── DAG Layout ───────────────────────────────────────────────────────────────

const DAG_NR = 28;        // raio do círculo
const DAG_SW = 130;       // largura do slot (centro do círculo + espaço para texto)
const DAG_SH = DAG_NR * 2 + 60; // altura do slot
const DAG_HGAP = 70;      // gap horizontal entre colunas
const DAG_VGAP = 20;      // gap vertical entre nós na coluna
const DAG_PAD = 24;       // padding externo

interface DAGNode {
  id: string;
  entity: DynatraceProblem["affectedEntities"][0];
  isRoot: boolean;
  hasAlert: boolean;
  cx: number;
  cy: number;
  connections: number; // callsTo + calledBy count (para badge)
}

function buildDAGLayout(problem: DynatraceProblem): {
  nodes: DAGNode[];
  edges: { from: string; to: string; isRoot: boolean }[];
  totalW: number;
  totalH: number;
  hasTopo: boolean;
} {
  const rootId = problem.rootCauseEntity?.entityId.id;
  const affected = problem.affectedEntities ?? [];
  const impacted = problem.impactedEntities ?? [];

  const entityMap = new Map<string, typeof affected[0]>();
  for (const e of [...affected, ...impacted]) entityMap.set(e.entityId.id, e);
  if (rootId && problem.rootCauseEntity && !entityMap.has(rootId)) {
    entityMap.set(rootId, problem.rootCauseEntity as typeof affected[0]);
  }

  const allIds = [...entityMap.keys()];
  if (allIds.length === 0) return { nodes: [], edges: [], totalW: 0, totalH: 0, hasTopo: false };

  const inProblem = new Set(allIds);
  const callsTo = new Map<string, string[]>();
  const calledBy = new Map<string, string[]>();
  for (const id of allIds) { callsTo.set(id, []); calledBy.set(id, []); }

  for (const id of allIds) {
    const e = entityMap.get(id)!;
    for (const rel of (e.callsTo ?? [])) {
      if (inProblem.has(rel.id) && rel.id !== id && !callsTo.get(id)!.includes(rel.id)) {
        callsTo.get(id)!.push(rel.id);
        calledBy.get(rel.id)!.push(id);
      }
    }
  }

  const hasTopo = allIds.some(id => (callsTo.get(id)?.length ?? 0) > 0);

  // Longest-path layering via topo sort
  const layers = new Map<string, number>();
  const visiting = new Set<string>();
  function assignLayer(id: string): number {
    if (layers.has(id)) return layers.get(id)!;
    if (visiting.has(id)) return 0;
    visiting.add(id);
    let max = -1;
    for (const prev of calledBy.get(id) ?? []) max = Math.max(max, assignLayer(prev));
    visiting.delete(id);
    const layer = max + 1;
    layers.set(id, layer);
    return layer;
  }
  for (const id of allIds) assignLayer(id);

  // Sem topologia: root na camada 0, demais na camada 1
  if (!hasTopo) {
    for (const id of allIds) layers.set(id, id === rootId ? 0 : 1);
  }

  const byLayer = new Map<number, string[]>();
  for (const [id, l] of layers) {
    if (!byLayer.has(l)) byLayer.set(l, []);
    byLayer.get(l)!.push(id);
  }
  for (const ids of byLayer.values()) {
    ids.sort((a, b) => {
      if (a === rootId) return -1;
      if (b === rootId) return 1;
      const na = entityMap.get(a)?.displayName || entityMap.get(a)?.name || "";
      const nb = entityMap.get(b)?.displayName || entityMap.get(b)?.name || "";
      return na.localeCompare(nb);
    });
  }

  const maxLayer = Math.max(...layers.values(), 0);

  // Sub-colunas: camadas com muitos nós ganham colunas extras p/ evitar pilha alta
  const MAX_PER_COL = 2;
  const layerColOffset = new Map<number, number>(); // layer → índice de coluna inicial
  const nodeEffCol = new Map<string, number>();      // id → coluna efetiva
  const nodeEffRow = new Map<string, number>();      // id → linha na coluna efetiva
  let colOffset = 0;
  for (let l = 0; l <= maxLayer; l++) {
    const ids = byLayer.get(l) ?? [];
    layerColOffset.set(l, colOffset);
    const subCols = Math.max(1, Math.ceil(ids.length / MAX_PER_COL));
    for (let i = 0; i < ids.length; i++) {
      nodeEffCol.set(ids[i], colOffset + Math.floor(i / MAX_PER_COL));
      nodeEffRow.set(ids[i], i % MAX_PER_COL);
    }
    colOffset += subCols;
  }
  const totalCols = colOffset;

  // Altura máxima: não ultrapassa MAX_PER_COL nós por coluna
  const maxRowCount = Math.min(MAX_PER_COL, Math.max(...[...byLayer.values()].map(ids => ids.length), 1));
  const maxLayerH = maxRowCount * DAG_SH + Math.max(0, maxRowCount - 1) * DAG_VGAP;

  const nodePositions = new Map<string, { cx: number; cy: number }>();
  for (const id of allIds) {
    const effCol = nodeEffCol.get(id)!;
    const effRow = nodeEffRow.get(id)!;
    const l = layers.get(id)!;
    const layerIds = byLayer.get(l) ?? [];
    const colStart = layerColOffset.get(l)!;
    const subColIdx = effCol - colStart;
    const startInSubCol = subColIdx * MAX_PER_COL;
    const nodesInSubCol = Math.min(MAX_PER_COL, layerIds.length - startInSubCol);
    const colH = nodesInSubCol * DAG_SH + Math.max(0, nodesInSubCol - 1) * DAG_VGAP;
    const startY = DAG_PAD + (maxLayerH - colH) / 2 + DAG_NR;
    const cx = DAG_PAD + effCol * (DAG_SW + DAG_HGAP) + DAG_SW / 2;
    nodePositions.set(id, { cx, cy: startY + effRow * (DAG_SH + DAG_VGAP) });
  }

  const totalW = DAG_PAD * 2 + totalCols * DAG_SW + Math.max(0, totalCols - 1) * DAG_HGAP;
  const totalH = DAG_PAD * 2 + maxLayerH + 16;

  const affectedIds = new Set(affected.map(e => e.entityId.id));
  const nodes: DAGNode[] = allIds.map(id => {
    const pos = nodePositions.get(id)!;
    const e = entityMap.get(id)!;
    const connections = (e.callsTo?.length ?? 0) + (e.calledBy?.length ?? 0);
    return { id, entity: e, isRoot: id === rootId, hasAlert: affectedIds.has(id), cx: pos.cx, cy: pos.cy, connections };
  });

  // Transitive reduction: remove edge A→C quando já existe A→B→C (evita linhas cruzando nós intermediários)
  function canReachVia(start: string, target: string): boolean {
    const visited = new Set<string>();
    const queue: string[] = [];
    for (const nb of callsTo.get(start) ?? []) {
      if (nb !== target) queue.push(nb);
    }
    while (queue.length) {
      const cur = queue.shift()!;
      if (cur === target) return true;
      if (!visited.has(cur)) {
        visited.add(cur);
        for (const nb of callsTo.get(cur) ?? []) queue.push(nb);
      }
    }
    return false;
  }

  const rawEdges: { from: string; to: string; isRoot: boolean }[] = [];
  if (hasTopo) {
    for (const id of allIds)
      for (const toId of callsTo.get(id) ?? [])
        rawEdges.push({ from: id, to: toId, isRoot: id === rootId });
  } else if (rootId) {
    for (const id of allIds)
      if (id !== rootId) rawEdges.push({ from: rootId, to: id, isRoot: true });
  }

  // Aplica redução transitiva apenas no caso com topologia
  const edges = hasTopo
    ? rawEdges.filter(e => !canReachVia(e.from, e.to))
    : rawEdges;

  return { nodes, edges, totalW, totalH, hasTopo };
}

// Tooltip para hover nos nós do VRP
interface VRPTooltip {
  node: DAGNode;
  x: number;
  y: number;
}

function VRPNodeTooltip({ tooltip }: { tooltip: VRPTooltip }) {
  const { node, x, y } = tooltip;
  const e = node.entity;
  const name = e.displayName || e.name || e.entityId.id;
  const kind = node.isRoot ? "Causa Raiz" : node.hasAlert ? "Afetado" : "Propagação";
  const kindColor = node.isRoot ? "text-orange-400" : node.hasAlert ? "text-red-400" : "text-violet-400";
  const k8sPath = [e.k8sCluster, e.k8sNamespace, e.k8sWorkload].filter(Boolean).join(" › ");
  const hasTopo = (e.callsTo?.length ?? 0) > 0 || (e.calledBy?.length ?? 0) > 0;

  return (
    <div
      className="fixed z-[9999] pointer-events-none"
      style={{ left: x + 14, top: y - 10 }}
    >
      <div className="bg-popover border border-border rounded-lg shadow-xl px-3 py-2.5 text-xs min-w-[220px] max-w-[300px] space-y-1.5">
        {/* Header */}
        <div className="flex items-center gap-1.5">
          <span className="text-[9px] uppercase tracking-wider text-muted-foreground font-semibold">
            {entityTypeLabel(e.entityId.type)}
          </span>
          <span className={`ml-auto text-[9px] font-semibold ${kindColor}`}>{kind}</span>
        </div>
        {/* Nome completo */}
        <p className="font-medium text-foreground leading-snug break-words">{name}</p>
        {/* Entity ID */}
        <div className="flex items-center gap-1 text-[9px] text-muted-foreground">
          <Database className="h-2.5 w-2.5 shrink-0" />
          <span className="font-mono truncate">{e.entityId.id}</span>
        </div>
        {/* K8s path */}
        {k8sPath && (
          <div className="flex items-center gap-1 text-[9px] text-muted-foreground">
            <Layers className="h-2.5 w-2.5 shrink-0" />
            <span className="font-mono">{k8sPath}</span>
          </div>
        )}
        {/* Topologia */}
        {hasTopo && (
          <div className="flex items-center gap-3 text-[9px] text-muted-foreground border-t border-border/40 pt-1.5">
            {(e.calledBy?.length ?? 0) > 0 && <span><span className="text-blue-400 font-mono">{e.calledBy!.length}</span> upstream</span>}
            {(e.callsTo?.length ?? 0) > 0 && <span><span className="text-cyan-400 font-mono">{e.callsTo!.length}</span> downstream</span>}
          </div>
        )}
        <p className="text-[9px] text-muted-foreground/50 border-t border-border/30 pt-1">
          Clique nos cards abaixo para ver detalhes
        </p>
      </div>
    </div>
  );
}

function VRPGraph({
  problem,
  onNodeHover,
  onNodeLeave,
  scale = 1,
  onDragActive,
  readonly = false,
}: {
  problem: DynatraceProblem;
  onNodeHover: (node: DAGNode, x: number, y: number) => void;
  onNodeLeave: () => void;
  scale?: number;
  onDragActive?: (active: boolean) => void;
  readonly?: boolean;
}) {
  const { nodes, edges, totalW, totalH } = buildDAGLayout(problem);

  // Offsets de drag por nó (resetados quando o problem muda)
  const [offsets, setOffsets] = useState<Map<string, { dx: number; dy: number }>>(new Map());
  const dragRef = useRef<{ id: string; startX: number; startY: number; origDx: number; origDy: number } | null>(null);
  useEffect(() => { setOffsets(new Map()); }, [problem.problemId]);

  if (nodes.length === 0) return null;

  const sevColor = SEV_NODE_COLOR[problem.severityLevel] ?? "#6b7280";

  // Posição efetiva = base + offset de drag
  const getPos = (id: string, baseCx: number, baseCy: number) => {
    const off = offsets.get(id) ?? { dx: 0, dy: 0 };
    return { cx: baseCx + off.dx, cy: baseCy + off.dy };
  };

  const onNodeMouseDown = (e: React.MouseEvent, id: string) => {
    if (readonly) return;
    e.stopPropagation();
    e.preventDefault();
    const off = offsets.get(id) ?? { dx: 0, dy: 0 };
    dragRef.current = { id, startX: e.clientX, startY: e.clientY, origDx: off.dx, origDy: off.dy };
    onDragActive?.(true);
  };

  const onSvgMouseMove = (e: React.MouseEvent) => {
    if (!dragRef.current) return;
    const { id, startX, startY, origDx, origDy } = dragRef.current;
    setOffsets(prev => {
      const next = new Map(prev);
      next.set(id, {
        dx: origDx + (e.clientX - startX) / scale,
        dy: origDy + (e.clientY - startY) / scale,
      });
      return next;
    });
  };

  const onSvgEnd = () => {
    if (!dragRef.current) return;
    dragRef.current = null;
    onDragActive?.(false);
  };

  return (
    <svg
      width={totalW} height={totalH} viewBox={`0 0 ${totalW} ${totalH}`}
      style={{ display: "block", overflow: "visible" }}
      onMouseMove={onSvgMouseMove}
      onMouseUp={onSvgEnd}
      onMouseLeave={onSvgEnd}
    >
      <defs>
        <marker id="vrp-arr" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
          <path d="M0,0 L0,8 L8,4 z" fill="#475569" />
        </marker>
        <marker id="vrp-arr-root" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
          <path d="M0,0 L0,8 L8,4 z" fill="#ef4444" />
        </marker>
        <marker id="vrp-arr-alert" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
          <path d="M0,0 L0,8 L8,4 z" fill={sevColor} />
        </marker>
      </defs>

      {/* Arestas — recalculadas com posições efetivas */}
      {edges.map((edge, i) => {
        const srcBase = nodes.find(n => n.id === edge.from);
        const dstBase = nodes.find(n => n.id === edge.to);
        if (!srcBase || !dstBase) return null;
        const src = getPos(srcBase.id, srcBase.cx, srcBase.cy);
        const dst = getPos(dstBase.id, dstBase.cx, dstBase.cy);
        const x1 = src.cx + DAG_NR, y1 = src.cy;
        const x2 = dst.cx - DAG_NR, y2 = dst.cy;
        const mx = (x1 + x2) / 2;
        const color = srcBase.isRoot ? "#ef4444" : (srcBase.hasAlert || dstBase.hasAlert) ? sevColor : "#475569";
        const markerId = srcBase.isRoot ? "vrp-arr-root" : (srcBase.hasAlert || dstBase.hasAlert) ? "vrp-arr-alert" : "vrp-arr";
        return (
          <path
            key={`e-${i}`}
            d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`}
            fill="none" stroke={color} strokeWidth={srcBase.isRoot ? 2 : 1.5}
            strokeOpacity={0.8} markerEnd={`url(#${markerId})`}
          />
        );
      })}

      {/* Nós arrastáveis */}
      {nodes.map(node => {
        const { id, cx: baseCx, cy: baseCy, isRoot, hasAlert, entity, connections } = node;
        const { cx, cy } = getPos(id, baseCx, baseCy);
        const type = entity?.entityId.type ?? "SERVICE";
        const nameFull = entity?.displayName || entity?.name || entity?.entityId.id || "";
        const [l1, l2] = splitLabel(nameFull, 18);
        const stroke    = isRoot ? "#ef4444" : hasAlert ? sevColor : "#475569";
        const fill      = isRoot ? "#ef4444" : hasAlert ? `${sevColor}33` : "#1e293b";
        const iconColor = isRoot ? "#ffffff" : hasAlert ? sevColor : "#94a3b8";
        const textColor = isRoot ? "#fca5a5" : hasAlert ? sevColor : "#94a3b8";
        const yL1  = cy + DAG_NR + 12;
        const yL2  = yL1 + 11;
        const yType = (l2 ? yL2 : yL1) + 11;
        const isDragging = dragRef.current?.id === id;

        return (
          <g
            key={id}
            style={{ cursor: readonly ? "default" : isDragging ? "grabbing" : "grab" }}
            onMouseDown={(e) => onNodeMouseDown(e, id)}
            onMouseEnter={(e) => { if (!dragRef.current) onNodeHover(node, e.clientX, e.clientY); }}
            onMouseMove={(e) => { if (!dragRef.current) onNodeHover(node, e.clientX, e.clientY); }}
            onMouseLeave={() => { if (!dragRef.current) onNodeLeave(); }}
          >
            {/* Círculo principal */}
            <circle cx={cx} cy={cy} r={DAG_NR} fill={fill} stroke={stroke} strokeWidth={isRoot ? 2.5 : 1.5} />

            {/* Ícone */}
            {entityIcon(type, cx, cy, 16, iconColor)}

            {/* Badge de conexões */}
            {connections > 0 && (
              <>
                <rect x={cx - 14} y={cy - DAG_NR - 2} width={28} height={14} rx={7} fill="#1e293b" stroke="#475569" strokeWidth={1} />
                <text x={cx} y={cy - DAG_NR + 8} fontSize={9} fill="#94a3b8" textAnchor="middle" fontFamily="sans-serif">{connections}</text>
              </>
            )}

            {/* Nome */}
            <text x={cx} y={yL1} fontSize={9} fill={textColor} textAnchor="middle"
              fontWeight={isRoot ? "700" : "500"} fontFamily="sans-serif">{l1}</text>
            {l2 && <text x={cx} y={yL2} fontSize={9} fill={textColor} textAnchor="middle"
              fontWeight={isRoot ? "700" : "500"} fontFamily="sans-serif">{l2}</text>}

            {/* Tipo */}
            <text x={cx} y={yType} fontSize={8} fill="#475569" textAnchor="middle" fontFamily="sans-serif">
              {entityTypeLabel(type)}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

function VRPLegend({ problem }: { problem: DynatraceProblem }) {
  const sevColor = SEV_NODE_COLOR[problem.severityLevel] ?? "#6b7280";
  const hasRoot = !!problem.rootCauseEntity;
  const { hasTopo, nodes } = buildDAGLayout(problem);
  const entityCount = nodes.length;
  return (
    <div className="flex items-center gap-3 flex-wrap">
      {hasRoot && <span className="flex items-center gap-1 text-[9px] text-red-400"><span className="w-2.5 h-2.5 rounded-full bg-red-500 border border-red-600 inline-block" />Causa Raiz</span>}
      <span className="flex items-center gap-1 text-[9px]" style={{ color: sevColor }}><span className="w-2.5 h-2.5 rounded-full inline-block border" style={{ background: `${sevColor}33`, borderColor: sevColor }} />Afetado</span>
      <span className="flex items-center gap-1 text-[9px] text-muted-foreground"><span className="w-2.5 h-2.5 rounded-full bg-slate-700 border border-slate-600 inline-block" />Entidade DT</span>
      <span className="text-[9px] text-muted-foreground/50">{entityCount} entidade(s)</span>
      {hasTopo
        ? <span className="text-[9px] text-green-400/70 ml-auto flex items-center gap-0.5"><Network className="h-2.5 w-2.5" />topologia real</span>
        : <span className="text-[9px] text-muted-foreground/50 ml-auto flex items-center gap-0.5"><Network className="h-2.5 w-2.5" />layout estimado</span>
      }
    </div>
  );
}

function VisualResolutionPath({ problem }: { problem: DynatraceProblem }) {
  const [zoomOpen, setZoomOpen] = useState(false);

  // ── Visão normal ────────────────────────────────────────────────────────────
  const [normalScale, setNormalScale] = useState(1.0);
  const [panX, setPanX] = useState(0);
  const [panY, setPanY] = useState(0);
  const [isPanning, setIsPanning] = useState(false);
  const panStart = useRef<{ x: number; y: number } | null>(null);
  const normalRef = useRef<HTMLDivElement>(null);

  // ── Modal ampliado ──────────────────────────────────────────────────────────
  const [modalScale, setModalScale] = useState(1.5);
  const [modalPanX, setModalPanX] = useState(0);
  const [modalPanY, setModalPanY] = useState(0);
  const [isModalPanning, setIsModalPanning] = useState(false);
  const modalPanStart = useRef<{ x: number; y: number } | null>(null);
  const modalRef = useRef<HTMLDivElement>(null);

  const hasContent = (problem.affectedEntities?.length ?? 0) > 0 || !!problem.rootCauseEntity;
  const [tooltip, setTooltip] = useState<VRPTooltip | null>(null);
  // Flag para desabilitar pan do canvas enquanto um nó está sendo arrastado
  const isNodeDraggingRef = useRef(false);

  // Ctrl+scroll zoom — visão normal (useEffect p/ passive:false obrigatório)
  useEffect(() => {
    const el = normalRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey) return;
      e.preventDefault();
      setNormalScale(s => Math.min(4, Math.max(0.3, s - e.deltaY * 0.002)));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  // Ctrl+scroll zoom — modal ampliado
  // Listener no document (com target check) para garantir preventDefault mesmo dentro do Dialog/Portal
  useEffect(() => {
    if (!zoomOpen) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey) return;
      const el = modalRef.current;
      if (!el || !el.contains(e.target as Node)) return;
      e.preventDefault();
      e.stopPropagation();
      setModalScale(s => Math.min(5, Math.max(0.3, s - e.deltaY * 0.002)));
    };
    document.addEventListener("wheel", onWheel, { passive: false });
    return () => document.removeEventListener("wheel", onWheel);
  }, [zoomOpen]);

  function makePanHandlers(
    panStartRef: React.MutableRefObject<{ x: number; y: number } | null>,
    setX: React.Dispatch<React.SetStateAction<number>>,
    setY: React.Dispatch<React.SetStateAction<number>>,
    setPanning: React.Dispatch<React.SetStateAction<boolean>>,
  ) {
    return {
      onMouseDown: (e: React.MouseEvent) => {
        if (isNodeDraggingRef.current) return; // nó sendo arrastado — não iniciar pan
        panStartRef.current = { x: e.clientX, y: e.clientY };
        setPanning(true);
        e.preventDefault();
      },
      onMouseMove: (e: React.MouseEvent) => {
        if (isNodeDraggingRef.current) return;
        const start = panStartRef.current;
        if (!start) return;
        setX(p => p + e.clientX - start.x);
        setY(p => p + e.clientY - start.y);
        panStartRef.current = { x: e.clientX, y: e.clientY };
      },
      onMouseUp: () => { panStartRef.current = null; setPanning(false); },
      onMouseLeave: () => { panStartRef.current = null; setPanning(false); },
    };
  }

  const normalPan = makePanHandlers(panStart, setPanX, setPanY, setIsPanning);
  const modalPan = makePanHandlers(modalPanStart, setModalPanX, setModalPanY, setIsModalPanning);

  const resetNormal = () => { setPanX(0); setPanY(0); setNormalScale(1.0); };
  const resetModal  = () => { setModalPanX(0); setModalPanY(0); setModalScale(1.5); };

  return (
    <>
      <div className="rounded-lg border border-border bg-muted/10 overflow-hidden">
        <div className="flex items-center justify-between px-3 py-1.5 bg-muted/20 border-b border-border/50">
          <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1.5">
            <Network className="h-3 w-3" /> Visual resolution path
          </span>
          {hasContent && (
            <div className="flex items-center gap-2">
              {/* Indicador de zoom atual */}
              <span className="text-[10px] tabular-nums text-muted-foreground/60 w-9 text-right">
                {Math.round(normalScale * 100)}%
              </span>
              <button onClick={resetNormal} className="text-[10px] text-muted-foreground hover:text-foreground transition-colors">
                Reset
              </button>
              <button
                className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                onClick={() => setZoomOpen(true)}
              >
                <Maximize2 className="h-3 w-3" /> Maximizar
              </button>
            </div>
          )}
        </div>

        {!hasContent ? (
          <div className="text-center py-6 text-[11px] text-muted-foreground space-y-1">
            <p className="font-medium">Sem entidades para visualizar</p>
            <p className="text-[10px]">Nenhuma entidade de serviço encontrada neste Problem.</p>
          </div>
        ) : (
          <div
            ref={normalRef}
            className="overflow-hidden p-3 min-h-[180px] select-none"
            style={{ cursor: isPanning ? "grabbing" : "grab" }}
            {...normalPan}
          >
            <div style={{ transform: `translate(${panX}px, ${panY}px) scale(${normalScale})`, transformOrigin: "top left", display: "inline-block" }}>
              <VRPGraph
                problem={problem}
                scale={normalScale}
                onDragActive={active => { isNodeDraggingRef.current = active; }}
                onNodeHover={(node, x, y) => setTooltip({ node, x, y })}
                onNodeLeave={() => setTooltip(null)}
              />
            </div>
          </div>
        )}
        {!zoomOpen && tooltip && <VRPNodeTooltip tooltip={tooltip} />}

        {hasContent && (
          <div className="px-3 pb-2 flex items-center justify-between">
            <VRPLegend problem={problem} />
            <span className="text-[9px] text-muted-foreground/40 shrink-0 ml-2">arrastar nó = reposicionar · arrastar fundo = pan · Ctrl+scroll = zoom</span>
          </div>
        )}
      </div>

      <Dialog open={zoomOpen} onOpenChange={setZoomOpen}>
        <DialogContent className="max-w-[95vw] w-[95vw] max-h-[92vh] h-[85vh] flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="text-sm flex items-center gap-2">
              <Network className="h-4 w-4" /> Visual Resolution Path — {problem.displayId}
            </DialogTitle>
          </DialogHeader>
          <div className="flex items-center gap-2 px-1">
            <button onClick={() => setModalScale(s => Math.max(0.3, +(s - 0.25).toFixed(2)))} className="p-1.5 rounded hover:bg-muted transition-colors" title="Zoom out"><ZoomOut className="h-3.5 w-3.5" /></button>
            <span className="text-xs tabular-nums w-10 text-center">{Math.round(modalScale * 100)}%</span>
            <button onClick={() => setModalScale(s => Math.min(5, +(s + 0.25).toFixed(2)))} className="p-1.5 rounded hover:bg-muted transition-colors" title="Zoom in"><ZoomIn className="h-3.5 w-3.5" /></button>
            <button onClick={resetModal} className="text-[10px] px-2 py-1 rounded hover:bg-muted transition-colors text-muted-foreground">Reset</button>
            <span className="text-[10px] text-muted-foreground ml-2">Ctrl+scroll = zoom · Arrastar = pan</span>
          </div>
          <div
            ref={modalRef}
            className="flex-1 overflow-hidden border rounded-lg bg-muted/5 p-4 select-none"
            style={{ cursor: isModalPanning ? "grabbing" : "grab" }}
            {...modalPan}
          >
            <div style={{ transform: `translate(${modalPanX}px, ${modalPanY}px) scale(${modalScale})`, transformOrigin: "top left", display: "inline-block" }}>
              <VRPGraph
                problem={problem}
                scale={modalScale}
                onDragActive={active => { isNodeDraggingRef.current = active; }}
                onNodeHover={(node, x, y) => setTooltip({ node, x, y })}
                onNodeLeave={() => setTooltip(null)}
              />
            </div>
          </div>
          {tooltip && <VRPNodeTooltip tooltip={tooltip} />}
          <div className="border-t pt-2"><VRPLegend problem={problem} /></div>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ── Aba Detalhes (layout 2 colunas) ─────────────────────────────────────────────

function DetailsTab({ problem, uiBaseUrl }: { problem: DynatraceProblem; uiBaseUrl?: string }) {
  const affectedEntities = problem.affectedEntities ?? [];
  const impactedEntities = (problem.impactedEntities ?? []).filter(e => !affectedEntities.some(a => a.entityId.id === e.entityId.id));
  const k8sWorkloads = (problem.k8sWorkloads ?? []).filter(w => w.Workload || w.AppName);
  const mgmtZones = problem.managementZones ?? [];

  const allLabels = affectedEntities.map(e => e.labels).filter(Boolean);
  const squads = [...new Set(allLabels.map(l => l?.componentSquad).filter(Boolean))];
  const journeys = [...new Set(allLabels.map(l => l?.componentJourney).filter(Boolean))];
  const tribes = [...new Set(allLabels.map(l => l?.componentTribe).filter(Boolean))];
  const envs = [...new Set([...allLabels.map(l => l?.appEnvironment).filter(Boolean), ...k8sWorkloads.map(w => w.Environment).filter(Boolean)])];
  const gitRepos = [...new Set([...allLabels.map(l => l?.githubRepoId).filter(Boolean), ...k8sWorkloads.map(w => w.GitHubRepoID).filter(Boolean)])];
  const stages = [...new Set(allLabels.map(l => l?.stage).filter(Boolean))];
  const deployedBys = [...new Set(allLabels.map(l => l?.deployedBy).filter(Boolean))];
  const hasOwnership = squads.length > 0 || journeys.length > 0 || tribes.length > 0;
  const hasDevOps = gitRepos.length > 0 || stages.length > 0 || deployedBys.length > 0;

  const entityGroups = affectedEntities.reduce((acc, e) => {
    const t = e.entityId.type;
    if (!acc[t]) acc[t] = [];
    acc[t].push(e);
    return acc;
  }, {} as Record<string, typeof affectedEntities>);

  const infraTypes = ["HOST", "KUBERNETES_NODE", "KUBERNETES_CLUSTER"];
  const serviceTypes = ["SERVICE", "PROCESS_GROUP", "PROCESS_GROUP_INSTANCE"];
  const infraCount = infraTypes.flatMap(t => entityGroups[t] ?? []).length;
  const serviceCount = serviceTypes.flatMap(t => entityGroups[t] ?? []).length;
  const otherGroups = Object.entries(entityGroups).filter(([t]) => ![...infraTypes, ...serviceTypes].includes(t));
  const otherCount = otherGroups.reduce((s, [, v]) => s + v.length, 0);

  const defaultTab = k8sWorkloads.length > 0 ? "k8s" : infraCount > 0 ? "infra" : serviceCount > 0 ? "services" : "other";
  const [rightWidth, setRightWidth] = useState(380);

  const impactItems = [
    { label: "Infraestrutura", count: infraCount, color: "text-orange-400 border-orange-500/30 bg-orange-500/10" },
    { label: "Serviços", count: serviceCount, color: "text-blue-400 border-blue-500/30 bg-blue-500/10" },
    { label: "K8s Workloads", count: k8sWorkloads.length, color: "text-cyan-400 border-cyan-500/30 bg-cyan-500/10" },
    { label: "Propagação", count: impactedEntities.length, color: "text-violet-400 border-violet-500/30 bg-violet-500/10" },
    { label: "Outras", count: otherCount, color: "text-muted-foreground border-border bg-muted/30" },
  ].filter(i => i.count > 0);

  return (
    <div className="space-y-3">
      <ProblemHeader problem={problem} uiBaseUrl={uiBaseUrl} />

      {/* Impact summary strip */}
      <div className="flex items-center gap-2 flex-wrap">
        {impactItems.map(item => (
          <div key={item.label} className={`flex items-center gap-1.5 rounded-md border px-2.5 py-1 ${item.color}`}>
            <span className="text-sm font-bold tabular-nums">{item.count}</span>
            <span className="text-[10px] font-normal">{item.label}</span>
          </div>
        ))}
      </div>

      {/* 2-column resizable layout */}
      <div className="flex gap-0 items-start">

        {/* LEFT: Impact section + VRP */}
        <div className="flex-1 min-w-0 space-y-4">
          <Tabs defaultValue={defaultTab}>
            <div className="flex items-center gap-2 mb-2 border-b border-border pb-1.5">
              <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1 shrink-0">
                <Server className="h-3 w-3" /> Impacto
              </span>
              <TabsList className="h-6 bg-transparent p-0 gap-0.5 flex-wrap">
                {k8sWorkloads.length > 0 && <TabsTrigger value="k8s" className="h-6 text-[10px] px-2 rounded data-[state=active]:bg-cyan-500/15 data-[state=active]:text-cyan-400 data-[state=active]:shadow-none">K8s ({k8sWorkloads.length})</TabsTrigger>}
                {infraCount > 0 && <TabsTrigger value="infra" className="h-6 text-[10px] px-2 rounded data-[state=active]:bg-orange-500/15 data-[state=active]:text-orange-400 data-[state=active]:shadow-none">Infra ({infraCount})</TabsTrigger>}
                {serviceCount > 0 && <TabsTrigger value="services" className="h-6 text-[10px] px-2 rounded data-[state=active]:bg-blue-500/15 data-[state=active]:text-blue-400 data-[state=active]:shadow-none">Serviços ({serviceCount})</TabsTrigger>}
                {impactedEntities.length > 0 && <TabsTrigger value="propagation" className="h-6 text-[10px] px-2 rounded data-[state=active]:bg-violet-500/15 data-[state=active]:text-violet-400 data-[state=active]:shadow-none">Propagação ({impactedEntities.length})</TabsTrigger>}
                {otherCount > 0 && <TabsTrigger value="other" className="h-6 text-[10px] px-2 rounded data-[state=active]:shadow-none">Outras ({otherCount})</TabsTrigger>}
                {(hasOwnership || hasDevOps || envs.length > 0) && <TabsTrigger value="ownership" className="h-6 text-[10px] px-2 rounded data-[state=active]:bg-green-500/15 data-[state=active]:text-green-400 data-[state=active]:shadow-none">Ownership</TabsTrigger>}
              </TabsList>
            </div>

            <TabsContent value="k8s" className="mt-0 space-y-2">
              {k8sWorkloads.map((w, i) => (
                <div key={i} className="rounded-lg border bg-cyan-500/5 border-cyan-500/20 px-3 py-2.5 text-xs space-y-1.5">
                  <div className="flex items-center gap-1.5 flex-wrap">
                    {w.Cluster && <><span className="font-mono text-[10px] bg-muted/50 px-1.5 py-0.5 rounded text-muted-foreground">{w.Cluster}</span><ArrowRight className="h-2.5 w-2.5 text-muted-foreground/40" /></>}
                    <span className="font-mono text-[10px] bg-muted/50 px-1.5 py-0.5 rounded text-muted-foreground">{w.Namespace}</span>
                    <ArrowRight className="h-2.5 w-2.5 text-muted-foreground/40" />
                    <span className="font-semibold font-mono text-cyan-300">{w.Workload || w.AppName}</span>
                    <div className="ml-auto flex items-center gap-1.5">
                      {w.AppVersion && <Badge className="text-[10px] font-mono h-5 bg-blue-500/15 text-blue-400 border border-blue-500/30">v{w.AppVersion}</Badge>}
                      {w.Environment && <EnvBadge env={w.Environment} />}
                    </div>
                  </div>
                  {(w.PodName || w.GitHubRepoID) && (
                    <div className="flex items-center gap-3 text-[10px] text-muted-foreground border-t border-border/30 pt-1.5">
                      {w.PodName && <span className="flex items-center gap-1"><Cpu className="h-2.5 w-2.5" /><span className="font-mono">{w.PodName}</span></span>}
                      {w.GitHubRepoID && <span className="flex items-center gap-1"><GitBranch className="h-2.5 w-2.5" /><span className="font-mono text-blue-400/70">{w.GitHubRepoID}</span></span>}
                    </div>
                  )}
                </div>
              ))}
            </TabsContent>

            <TabsContent value="infra" className="mt-0 space-y-1.5">
              {infraTypes.flatMap(t => entityGroups[t] ?? []).map((e, i) => <EntityCard key={i} entity={e} />)}
            </TabsContent>

            <TabsContent value="services" className="mt-0 space-y-1.5">
              {serviceTypes.flatMap(t => entityGroups[t] ?? []).map((e, i) => <EntityCard key={i} entity={e} />)}
            </TabsContent>

            <TabsContent value="propagation" className="mt-0 space-y-1.5">
              {impactedEntities.map((e, i) => <EntityCard key={i} entity={e} />)}
            </TabsContent>

            <TabsContent value="other" className="mt-0 space-y-3">
              {otherGroups.map(([type, entities]) => (
                <div key={type}>
                  <p className="text-[10px] font-semibold text-muted-foreground/70 uppercase tracking-wider mb-1.5">{entityTypeLabel(type)} ({entities.length})</p>
                  <div className="space-y-1.5">{entities.map((e, i) => <EntityCard key={i} entity={e} />)}</div>
                </div>
              ))}
            </TabsContent>

            <TabsContent value="ownership" className="mt-0">
              <div className="rounded-lg border border-border bg-muted/10 px-4 py-3 space-y-3">
                <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-xs">
                  {envs.length > 0 && (
                    <div className="flex items-center gap-2 flex-wrap col-span-2">
                      <span className="text-muted-foreground text-[10px] w-20 shrink-0">Ambiente:</span>
                      {envs.map(e => <EnvBadge key={e} env={e!} />)}
                    </div>
                  )}
                  {squads.length > 0 && (
                    <div className="flex items-start gap-2 col-span-2">
                      <Users className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                      <div><span className="text-muted-foreground text-[10px]">Squad(s): </span>{squads.map(s => <Badge key={s} variant="outline" className="text-[10px] mr-1 border-green-500/30 text-green-400">{s}</Badge>)}</div>
                    </div>
                  )}
                  {journeys.length > 0 && (
                    <div className="flex items-start gap-2 col-span-2">
                      <Tag className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                      <div><span className="text-muted-foreground text-[10px]">Jornada(s): </span>{journeys.map(j => <Badge key={j} variant="outline" className="text-[10px] mr-1 border-purple-500/30 text-purple-400">{j}</Badge>)}</div>
                    </div>
                  )}
                  {tribes.length > 0 && <div className="flex items-center gap-2"><Globe className="h-3 w-3 text-muted-foreground shrink-0" /><span className="text-muted-foreground text-[10px]">Tribo: </span><span className="text-[11px]">{tribes.join(", ")}</span></div>}
                  {stages.length > 0 && <div className="flex items-center gap-2"><Shield className="h-3 w-3 text-muted-foreground shrink-0" /><span className="text-muted-foreground text-[10px]">Estágio: </span><span className="text-[11px]">{stages.join(", ")}</span></div>}
                  {deployedBys.length > 0 && <div className="flex items-center gap-2"><Rocket className="h-3 w-3 text-muted-foreground shrink-0" /><span className="text-muted-foreground text-[10px]">Deploy via: </span><span className="text-[11px] font-mono">{deployedBys.join(", ")}</span></div>}
                  {gitRepos.length > 0 && (
                    <div className="flex items-start gap-2 col-span-2">
                      <GitBranch className="h-3 w-3 text-muted-foreground mt-0.5 shrink-0" />
                      <div className="min-w-0"><span className="text-muted-foreground text-[10px]">Repos GitHub: </span>{gitRepos.map(r => <span key={r} className="inline-block font-mono text-[10px] text-blue-400 mr-2">{r}</span>)}</div>
                    </div>
                  )}
                </div>
                {mgmtZones.length > 0 && (
                  <div className="border-t border-border/30 pt-2 flex flex-wrap gap-1.5 items-center">
                    <span className="text-[10px] text-muted-foreground flex items-center gap-1"><MapPin className="h-2.5 w-2.5" /> MZ:</span>
                    {mgmtZones.map(z => <Badge key={z.id} variant="secondary" className="text-[10px]">{z.name}</Badge>)}
                  </div>
                )}
              </div>
            </TabsContent>
          </Tabs>

          {/* Visual Resolution Path — mesma largura do card Impacto */}
          <VisualResolutionPath problem={problem} />
        </div>

        <DTResizeDivider onDrag={d => setRightWidth(w => Math.max(260, Math.min(700, w - d)))} />

        {/* RIGHT: Root Cause + Investigation */}
        <div className="flex-shrink-0 space-y-3" style={{ width: rightWidth }}>
          {/* Root Cause */}
          <div className="rounded-lg border border-border bg-muted/10 overflow-hidden">
            <div className="flex items-center gap-1.5 px-3 py-1.5 bg-muted/20 border-b border-border/50">
              <Target className="h-3 w-3 text-orange-400" />
              <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Root cause</span>
            </div>
            <div className="p-2">
              {problem.rootCauseEntity ? (
                <EntityCard entity={problem.rootCauseEntity as any} isRoot />
              ) : (
                <div className="text-center py-5 text-[11px] text-muted-foreground space-y-1">
                  <p className="font-medium">No root cause</p>
                  <p className="text-[10px] px-4">Possible causes include the expiration of their retention period or missing permissions.</p>
                </div>
              )}
            </div>
          </div>

          {/* Quick Investigation */}
          <QuickInvestigation
            severityType={problem.severityLevel}
            mgmtZones={problem.managementZones ?? []}
            entityNames={(problem.affectedEntities ?? []).map(e => e.displayName || e.name || "").filter(Boolean)}
          />
        </div>
      </div>
    </div>
  );
}

// ── Aba Diagnóstico IA ──────────────────────────────────────────────────────────

// ── Painel de contexto do problem para a aba IA ────────────────────────────────

function DiagProblemContext({ problem }: { problem: DynatraceProblem }) {
  const sevColor = SEV_NODE_COLOR[problem.severityLevel] ?? "#6b7280";
  const affectedEntities = problem.affectedEntities ?? [];
  const rootCause = problem.rootCauseEntity;
  const k8sWorkloads = (problem.k8sWorkloads ?? []).filter(w => w.Workload || w.AppName);
  const mgmtZones = problem.managementZones ?? [];
  const hasVRP = affectedEntities.length > 0 || !!rootCause;

  const start = problem.startTime ? new Date(problem.startTime) : null;
  const end   = problem.endTime   ? new Date(problem.endTime)   : null;
  const durationMin = start && end ? Math.round((end.getTime() - start.getTime()) / 60000) : null;
  const fmtTime = (d: Date) => d.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });

  return (
    <div className="space-y-3 rounded-lg border border-border/50 bg-muted/5 p-3">
      {/* Linha 1: badges + duração */}
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline" style={{ borderColor: sevColor, color: sevColor }} className="text-[10px] font-semibold">
          {problem.severityLevel}
        </Badge>
        <Badge variant="outline" className="text-[10px]">{problem.impactLevel}</Badge>
        <Badge variant={problem.status === "OPEN" ? "destructive" : "secondary"} className="text-[10px]">
          {problem.status}
        </Badge>
        {durationMin != null && start && (
          <span className="flex items-center gap-1 text-[10px] text-muted-foreground ml-1">
            <Clock className="h-3 w-3" />
            {fmtTime(start)}{end ? ` → ${fmtTime(end)}` : ""} ({durationMin} min)
          </span>
        )}
        {mgmtZones.map(z => (
          <Badge key={z.id} variant="secondary" className="text-[10px] gap-1">
            <Users className="h-2.5 w-2.5" />{z.name}
          </Badge>
        ))}
      </div>

      {/* Linha 2: causa raiz */}
      {rootCause && (
        <div className="flex items-center gap-2 text-xs bg-red-500/10 border border-red-500/20 rounded-md px-2.5 py-1.5">
          <Target className="h-3 w-3 text-red-400 shrink-0" />
          <span className="text-red-300 font-medium truncate">{rootCause.displayName || rootCause.name || rootCause.entityId.id}</span>
          <span className="text-[9px] text-red-400/70 font-mono ml-auto shrink-0">{rootCause.entityId.type}</span>
          <Badge variant="outline" className="text-[9px] border-red-500/30 text-red-400 shrink-0">causa raiz</Badge>
        </div>
      )}

      {/* Linha 3: entidades afetadas */}
      {affectedEntities.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {affectedEntities.slice(0, 6).map(e => {
            const name = e.displayName || e.name || e.entityId.id;
            const k8s = [e.k8sNamespace, e.k8sWorkload].filter(Boolean).join("/");
            return (
              <div key={e.entityId.id} className="flex items-center gap-1.5 text-[10px] bg-muted/20 border border-border/40 rounded px-2 py-1">
                <Server className="h-2.5 w-2.5 text-muted-foreground shrink-0" />
                <span className="truncate max-w-[110px]" title={name}>{name}</span>
                {k8s && <span className="text-muted-foreground/50 font-mono truncate max-w-[80px]">{k8s}</span>}
              </div>
            );
          })}
          {affectedEntities.length > 6 && (
            <span className="text-[10px] text-muted-foreground px-2 py-1">+{affectedEntities.length - 6} mais</span>
          )}
        </div>
      )}

      {/* Linha 4: K8s workloads */}
      {k8sWorkloads.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {k8sWorkloads.slice(0, 5).map((w, i) => (
            <div key={i} className="flex items-center gap-1 text-[10px] bg-blue-500/10 border border-blue-500/20 rounded px-2 py-1">
              <Layers className="h-2.5 w-2.5 text-blue-400 shrink-0" />
              <span className="font-mono text-blue-300 truncate max-w-[140px]">{w.Namespace}/{w.Workload || w.AppName}</span>
              {w.AppVersion && <span className="text-blue-400/50 font-mono">v{w.AppVersion}</span>}
            </div>
          ))}
        </div>
      )}

      {/* Mini VRP read-only */}
      {hasVRP && (
        <div className="rounded-md border border-border/40 bg-muted/10 overflow-hidden">
          <div className="flex items-center gap-1.5 px-2.5 py-1 bg-muted/20 border-b border-border/30">
            <Network className="h-3 w-3 text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground font-medium">Fluxo de propagação</span>
          </div>
          <div className="overflow-auto max-h-[200px] p-2">
            <div style={{ transform: "scale(0.82)", transformOrigin: "top left", display: "inline-block" }}>
              <VRPGraph
                problem={problem}
                scale={1}
                readonly
                onNodeHover={() => {}}
                onNodeLeave={() => {}}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Parse + render resultado da IA em seções visuais ──────────────────────────

interface AIParsedSection { key: string; heading: string; content: string; }
interface AIParseResult { intro: string; sections: AIParsedSection[]; }

interface ActionItem {
  urgency: string;      // "IMEDIATA" | "ALTA" | "MONITORAR"
  app_section: string;  // "HPA" | "Deployments" | "Resource Explorer" | "Health Check"
  workload?: string;
  namespace?: string;
  cluster?: string;
  action: string;
  reason: string;
}

function ActionPlanCard({ items }: { items: ActionItem[] }) {
  if (!items || items.length === 0) return null;

  const urgencyMeta: Record<string, { color: string; bg: string; border: string; dot: string }> = {
    IMEDIATA: { color: "text-red-700 dark:text-red-300",    bg: "bg-red-50 dark:bg-red-950/30",    border: "border-red-200 dark:border-red-800",    dot: "bg-red-500" },
    ALTA:     { color: "text-orange-700 dark:text-orange-300", bg: "bg-orange-50 dark:bg-orange-950/30", border: "border-orange-200 dark:border-orange-800", dot: "bg-orange-500" },
    MONITORAR:{ color: "text-blue-700 dark:text-blue-300",   bg: "bg-blue-50 dark:bg-blue-950/30",   border: "border-blue-200 dark:border-blue-800",   dot: "bg-blue-500" },
  };

  return (
    <Card className="border-amber-200 dark:border-amber-800 bg-amber-50/30 dark:bg-amber-950/10">
      <CardHeader className="pb-2 pt-3 px-4">
        <div className="flex items-center gap-2">
          <ListChecks className="h-4 w-4 text-amber-600 dark:text-amber-400" />
          <span className="text-sm font-semibold text-amber-800 dark:text-amber-300">Plano de Ação</span>
          <Badge variant="outline" className="text-[10px] text-amber-700 dark:text-amber-400 border-amber-300 dark:border-amber-700 ml-auto">
            {items.length} {items.length === 1 ? "ação" : "ações"}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="px-4 pb-4 space-y-2">
        {items.map((item, i) => {
          const meta = urgencyMeta[item.urgency] ?? urgencyMeta["ALTA"];
          return (
            <div key={i} className={`rounded-md border px-3 py-2 space-y-1.5 ${meta.bg} ${meta.border}`}>
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <span className={`inline-block w-1.5 h-1.5 rounded-full shrink-0 mt-0.5 ${meta.dot}`} />
                  <span className={`text-xs font-semibold ${meta.color}`}>{item.action}</span>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <Badge variant="outline" className={`text-[9px] px-1 py-0 ${meta.color} border-current`}>
                    {item.urgency}
                  </Badge>
                  <Badge variant="secondary" className="text-[9px] px-1 py-0">
                    {item.app_section}
                  </Badge>
                </div>
              </div>
              <p className="text-[11px] text-muted-foreground pl-3.5">{item.reason}</p>
              {(item.namespace || item.workload) && (
                <div className="flex items-center gap-1 pl-3.5 flex-wrap">
                  {item.namespace && (
                    <span className="text-[10px] bg-muted/60 px-1.5 py-0.5 rounded font-mono">{item.namespace}</span>
                  )}
                  {item.workload && (
                    <span className="text-[10px] bg-muted/60 px-1.5 py-0.5 rounded font-mono">{item.workload}</span>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}

const AI_SECTION_PATTERNS: { key: string; re: RegExp }[] = [
  { key: "origem",      re: /\n((?:\*\*)?1\.\s+ORIGEM[^\n]*)/i },
  { key: "propagacao",  re: /\n((?:\*\*)?2\.\s+PROPAGA[ÇC][ÃA]O[^\n]*)/i },
  { key: "k8s",         re: /\n((?:\*\*)?3\.\s+(?:ANÁLISE|ANALISE|COMPONENTES)[^\n]*)/i },
  { key: "externas",    re: /\n((?:\*\*)?4\.\s+DEPEND[EÊ]NCIAS[^\n]*)/i },
  { key: "proximos",    re: /\n((?:\*\*)?5\.\s+(?:A[ÇC][ÕO]ES\s+CORRETIVAS|PR[OÓ]XIMOS)[^\n]*)/i },
];

function parseAISections(text: string): AIParseResult {
  type M = { idx: number; key: string; heading: string };
  const matches: M[] = [];
  for (const { key, re } of AI_SECTION_PATTERNS) {
    const m = text.match(re);
    if (m && m.index != null) matches.push({ idx: m.index, key, heading: m[1].replace(/\*\*/g, "").trim() });
  }
  matches.sort((a, b) => a.idx - b.idx);
  if (matches.length === 0) return { intro: text, sections: [] };
  const intro = text.slice(0, matches[0].idx).trim();
  const sections: AIParsedSection[] = matches.map((m, i) => ({
    key: m.key,
    heading: m.heading,
    content: text.slice(m.idx + 1, i + 1 < matches.length ? matches[i + 1].idx : text.length).trim(),
  }));
  return { intro, sections };
}

type SectionMeta = { Icon: React.ComponentType<{ className?: string }>; color: string; border: string; bg: string; label: string };
const SECTION_META: Record<string, SectionMeta> = {
  origem:      { Icon: Target,     color: "text-red-400",    border: "border-red-500/30",    bg: "bg-red-500/5",    label: "Origem / Causa Raiz" },
  propagacao:  { Icon: Share2,     color: "text-orange-400", border: "border-orange-500/30", bg: "bg-orange-500/5", label: "Propagação" },
  k8s:         { Icon: Layers,     color: "text-blue-400",   border: "border-blue-500/30",   bg: "bg-blue-500/5",   label: "Análise K8s" },
  externas:    { Icon: Globe,      color: "text-purple-400", border: "border-purple-500/30", bg: "bg-purple-500/5", label: "Dependências Externas" },
  proximos:    { Icon: ListChecks, color: "text-green-400",  border: "border-green-500/30",  bg: "bg-green-500/5",  label: "Ações Corretivas" },
};

function AIAnalysisResult({ text, actionItems }: { text: string; actionItems?: ActionItem[] }) {
  const { intro, sections } = parseAISections(text);

  // Fallback: sem seções parseadas
  if (sections.length === 0) {
    return (
      <div className="space-y-3">
        <ActionPlanCard items={actionItems ?? []} />
        <Card className="border-violet-500/20 overflow-hidden">
          <CardHeader className="pb-3 pt-4 px-5 bg-gradient-to-r from-violet-500/10 to-blue-500/5 border-b border-violet-500/20">
            <div className="flex items-center gap-2">
              <div className="p-1.5 bg-violet-500/10 rounded-md">
                <Bot className="h-4 w-4 text-violet-400" />
              </div>
              <span className="text-sm font-semibold text-violet-300">Análise IA</span>
            </div>
          </CardHeader>
          <CardContent className="pt-4 pb-4 px-5">
            <div className="prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown>{text}</ReactMarkdown>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  const defaultOpen = sections.map(s => s.key);

  return (
    <div className="space-y-3">
      {/* Plano de ação determinístico */}
      <ActionPlanCard items={actionItems ?? []} />

      {/* Header da análise AI */}
      <div className="flex items-center gap-2 px-1">
        <div className="p-1.5 bg-violet-500/10 rounded-md border border-violet-500/20">
          <Bot className="h-4 w-4 text-violet-400" />
        </div>
        <span className="text-sm font-semibold text-violet-300">Análise IA — Diagnóstico Dynatrace</span>
      </div>

      {/* Sumário introdutório */}
      {intro && (
        <Card className="border-border/40 bg-muted/10">
          <CardContent className="pt-3 pb-3 px-4">
            <p className="text-sm text-muted-foreground italic leading-relaxed">
              {intro.length > 500 ? intro.slice(0, 497) + "…" : intro}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Seções como Accordion — estilo AIAnalysisModal */}
      <div className="space-y-2">
        <Accordion type="multiple" defaultValue={defaultOpen} className="space-y-2">
          {sections.map(section => {
            const meta = SECTION_META[section.key] ?? {
              Icon: Bot, color: "text-foreground", border: "border-border", bg: "bg-muted/5", label: section.heading,
            };
            const { Icon, color, border, bg, label } = meta;

            return (
              <AccordionItem
                key={section.key}
                value={section.key}
                className={`border ${border} rounded-lg overflow-hidden`}
              >
                <AccordionTrigger
                  className={`px-4 py-3 ${bg} hover:no-underline hover:brightness-110 [&[data-state=open]]:border-b ${border}`}
                >
                  <div className="flex items-center gap-2 text-left">
                    <div className={`p-1.5 rounded-md ${bg} border ${border}`}>
                      <Icon className={`h-3.5 w-3.5 ${color}`} />
                    </div>
                    <span className={`text-sm font-semibold ${color}`}>{label}</span>
                  </div>
                </AccordionTrigger>
                <AccordionContent className="px-4 pt-3 pb-4">
                  <div className="prose prose-sm dark:prose-invert max-w-none">
                    <ReactMarkdown>{section.content}</ReactMarkdown>
                  </div>
                </AccordionContent>
              </AccordionItem>
            );
          })}
        </Accordion>
      </div>
    </div>
  );
}

function VRPEmbed({ problem }: { problem: DynatraceProblem }) {
  return (
    <div className="overflow-auto max-h-[220px] p-3">
      <div style={{ transform: "scale(0.82)", transformOrigin: "top left", display: "inline-block" }}>
        <VRPGraph problem={problem} scale={1} readonly onNodeHover={() => {}} onNodeLeave={() => {}} />
      </div>
    </div>
  );
}

// ── Helpers de export ────────────────────────────────────────────────────────────

function removeEmojis(text: string): string {
  return text.replace(/[\u{1F300}-\u{1FFFF}]|[\u2600-\u27FF]|⭐|🔴|🟡|🟢|⚠️|✅|❌/gu, "").trim();
}

/**
 * Sanitiza texto para uso em jsPDF com fonte helvetica (WinAnsi).
 * jsPDF renderiza caracteres fora do WinAnsi como "%P" ou similar.
 * Substitui os mais comuns por equivalentes ASCII.
 */
function sanitizePDF(text: string): string {
  return removeEmojis(text)
    // Box-drawing e separadores de bloco (comuns em respostas de IA)
    .replace(/[═─━╌╍╎╏┄┅┆┇┈┉┊┋]/g, "-")
    .replace(/[║│┃]/g, "|")
    .replace(/[╔╗╚╝╠╣╦╩╬┌┐└┘├┤┬┴┼]/g, "+")
    // Setas
    .replace(/→/g, "->")
    .replace(/←/g, "<-")
    .replace(/↑/g, "^")
    .replace(/↓/g, "v")
    .replace(/↔/g, "<->")
    .replace(/⇒/g, "=>")
    .replace(/⇐/g, "<=")
    // Travessões e hifens especiais
    .replace(/[—–]/g, "-")
    .replace(/…/g, "...")
    // Aspas tipográficas
    .replace(/[""]/g, '"')
    .replace(/['']/g, "'")
    // Bullets e símbolos de lista
    .replace(/[•◦▪▫◾◽▸▹►▻]/g, "-")
    .replace(/[✓✔]/g, "OK")
    .replace(/[✗✘]/g, "X")
    // Outros símbolos matemáticos/técnicos fora do WinAnsi
    .replace(/[×]/g, "x")
    .replace(/[÷]/g, "/")
    .replace(/[≥]/g, ">=")
    .replace(/[≤]/g, "<=")
    .replace(/[≠]/g, "!=")
    .replace(/[∞]/g, "inf")
    // Remove qualquer caractere acima de U+00FF que ainda reste (fallback)
    .replace(/[^\x00-\xFF]/g, "")
    .trim();
}

function buildInvestigationMarkdown(problem: DynatraceProblem, inv: any, quickAnalysis: string): string {
  const lines: string[] = [];
  lines.push(`# Diagnóstico Dynatrace — ${problem.displayId}`);
  lines.push(`**${problem.title}**`);
  lines.push(`Severidade: ${problem.severityLevel} | Impacto: ${problem.impactLevel} | Status: ${problem.status}`);
  lines.push(`Início: ${new Date(problem.startTime).toLocaleString("pt-BR")}`);
  if (problem.endTime) lines.push(`Fim: ${new Date(problem.endTime).toLocaleString("pt-BR")}`);
  lines.push("");

  if (inv) {
    lines.push("## Entidades Identificadas");
    if (inv.root_cause_entity_name) lines.push(`- **Causa Raiz (DT):** ${inv.root_cause_entity_name} [${inv.root_cause_entity_type}]`);
    if (inv.identified_cluster) lines.push(`- **Cluster K8s:** ${inv.identified_cluster} / node pool: ${inv.identified_node_pool}`);
    if (inv.identified_namespace) lines.push(`- **Namespace:** ${inv.identified_namespace}`);
    if (inv.identified_workload) lines.push(`- **Workload:** ${inv.identified_workload}`);
    lines.push("");

    const entities = (inv.dt_metrics?.entities ?? []).filter((e: any) =>
      (e.metrics ?? []).some((m: any) => (m.points ?? []).some((p: any) => !isNaN(p.v)))
    );
    if (entities.length > 0) {
      lines.push("## Métricas Dynatrace");
      for (const ed of entities) {
        lines.push(`\n### ${ed.entityName} [${ed.entityType}]${ed.isRootCause ? " ⭐ Causa Raiz" : ""}`);
        for (const m of ed.metrics ?? []) {
          const vals = (m.points ?? []).map((p: any) => p.v).filter((v: number) => !isNaN(v) && v >= 0);
          if (!vals.length) continue;
          const max = Math.max(...vals), avg = vals.reduce((a: number, b: number) => a + b, 0) / vals.length;
          lines.push(`- ${m.label}: avg=${avg.toFixed(2)} max=${max.toFixed(2)} ${m.unit}`);
        }
      }
      lines.push("");
    }

    if (inv.health_check_result) {
      const hc = inv.health_check_result as any;
      lines.push("## Health Check K8s");
      lines.push(`Status: **${hc.overall_status}** | Total: ${hc.total_checks} | Críticos: ${hc.severity_counts?.critical ?? 0}`);
      for (const d of hc.deployment_results ?? []) {
        lines.push(`\n### Deployment ${d.namespace}/${d.name} [${d.status}]`);
        lines.push(`Pods: ${d.replicas_ready}/${d.replicas_desired} | Crashes: ${d.containers_crash}`);
        if (d.cpu_usage_percent > 0) lines.push(`CPU: ${d.cpu_usage_percent.toFixed(1)}% | Mem: ${d.memory_usage_percent.toFixed(1)}%`);
        for (const cr of d.container_resources ?? []) {
          lines.push(`Container ${cr.name}: cpu ${cr.cpu_request || "—"}/${cr.cpu_limit || "—"} | mem ${cr.memory_request || "—"}/${cr.memory_limit || "—"}`);
        }
      }
      for (const h of (hc.hpa_results ?? []).filter((h: any) => h.status !== "healthy")) {
        lines.push(`\n### HPA ${h.namespace}/${h.name} [${h.status}]`);
        lines.push(`Réplicas: atual=${h.current_replicas} desejado=${h.desired_replicas} min=${h.min_replicas} max=${h.max_replicas}`);
      }
      lines.push("");
    }

    if ((inv.dependencies ?? []).length > 0) {
      lines.push("## Dependências Externas");
      const seen = new Set<string>();
      for (const dep of inv.dependencies) {
        const k = `${dep.service_type}:${dep.service_name}`;
        if (seen.has(k)) continue; seen.add(k);
        lines.push(`- [${dep.service_type}] ${dep.service_name}${dep.topic_name ? ` (${dep.topic_name})` : ""}`);
      }
      lines.push("");
    }

    if (inv.ai_analysis) {
      lines.push("## Análise AI — Investigação Profunda");
      lines.push(inv.ai_analysis);
      lines.push("");
    }
  }

  if (quickAnalysis && !inv?.ai_analysis) {
    lines.push("## Análise AI");
    lines.push(quickAnalysis);
    lines.push("");
  }

  lines.push(`---\n*Gerado pelo HPA Manager em ${new Date().toLocaleString("pt-BR")}*`);
  return lines.join("\n");
}

function exportMarkdown(problem: DynatraceProblem, inv: any, quickAnalysis: string) {
  const md = buildInvestigationMarkdown(problem, inv, quickAnalysis);
  const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `dt-diagnostico-${problem.displayId}-${new Date().toISOString().slice(0, 10)}.md`;
  a.click();
  URL.revokeObjectURL(url);
}

async function exportPDF(problem: DynatraceProblem, inv: any, quickAnalysis: string) {
  const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
  const pageWidth = doc.internal.pageSize.getWidth();
  const margin = 14;
  const contentWidth = pageWidth - margin * 2;

  // Header com logo (igual às outras ferramentas)
  let y = await addLogoHeaderToPDF(
    doc,
    "DIAGNOSTICO DYNATRACE",
    `${sanitizePDF(problem.title)} | ${problem.displayId}`,
    40,
  );

  const checkPage = (needed = 10) => {
    if (y + needed > 270) { doc.addPage(); y = 15; }
  };

  const h1 = (text: string) => {
    checkPage(14);
    doc.setFillColor(30, 64, 175);
    doc.rect(0, y, pageWidth, 12, "F");
    doc.setFontSize(13); doc.setFont("helvetica", "bold"); doc.setTextColor(255, 255, 255);
    doc.text(sanitizePDF(text), margin, y + 8);
    doc.setTextColor(0, 0, 0);
    y += 16;
  };

  const h2 = (text: string) => {
    checkPage(10);
    doc.setFontSize(11); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 64, 175);
    doc.text(sanitizePDF(text), margin, y);
    doc.setTextColor(0, 0, 0);
    y += 7;
  };

  const line = (text: string, indent = 0) => {
    checkPage(6);
    doc.setFontSize(9); doc.setFont("helvetica", "normal");
    const wrapped = doc.splitTextToSize(sanitizePDF(text), contentWidth - indent);
    doc.text(wrapped, margin + indent, y);
    y += wrapped.length * 5;
  };

  line(`Severidade: ${problem.severityLevel} | Impacto: ${problem.impactLevel} | Status: ${problem.status}`);
  line(`Inicio: ${new Date(problem.startTime).toLocaleString("pt-BR")}`);
  if (problem.endTime) line(`Fim: ${new Date(problem.endTime).toLocaleString("pt-BR")}`);
  y += 4;

  if (inv) {
    h1("ENTIDADES IDENTIFICADAS");
    if (inv.root_cause_entity_name) line(`Causa Raiz (DT): ${inv.root_cause_entity_name} [${inv.root_cause_entity_type}]`);
    if (inv.identified_cluster) line(`Cluster K8s: ${inv.identified_cluster} / ${inv.identified_node_pool}`);
    if (inv.identified_namespace) line(`Namespace: ${inv.identified_namespace}  Workload: ${inv.identified_workload}`);
    y += 4;

    const entities = (inv.dt_metrics?.entities ?? []).filter((e: any) =>
      (e.metrics ?? []).some((m: any) => (m.points ?? []).some((p: any) => !isNaN(p.v)))
    );
    if (entities.length > 0) {
      h1("METRICAS DYNATRACE");
      for (const ed of entities) {
        h2(`${ed.entityName} [${ed.entityType}]${ed.isRootCause ? " - CAUSA RAIZ" : ""}`);
        for (const m of ed.metrics ?? []) {
          const vals = (m.points ?? []).map((p: any) => p.v).filter((v: number) => !isNaN(v) && v >= 0);
          if (!vals.length) continue;
          const mx = Math.max(...vals), avg = vals.reduce((a: number, b: number) => a + b, 0) / vals.length;
          line(`${m.label}: avg=${avg.toFixed(2)} max=${mx.toFixed(2)} ${m.unit}`, 4);
        }
        y += 2;
      }
    }

    if (inv.health_check_result) {
      const hc = inv.health_check_result as any;
      h1("HEALTH CHECK K8S");
      line(`Status: ${hc.overall_status} | Total: ${hc.total_checks} | Criticos: ${hc.severity_counts?.critical ?? 0}`);
      y += 2;
      for (const d of hc.deployment_results ?? []) {
        h2(`Deployment: ${d.namespace}/${d.name} [${d.status}]`);
        line(`Pods: ${d.replicas_ready}/${d.replicas_desired} | Crashes: ${d.containers_crash}`, 4);
        if (d.cpu_usage_percent > 0) line(`CPU: ${d.cpu_usage_percent.toFixed(1)}% | Mem: ${d.memory_usage_percent.toFixed(1)}%`, 4);
        for (const cr of d.container_resources ?? []) {
          line(`Container ${cr.name}: cpu req=${cr.cpu_request || "n/d"} lim=${cr.cpu_limit || "n/d"} | mem req=${cr.memory_request || "n/d"} lim=${cr.memory_limit || "n/d"}`, 6);
        }
      }
      for (const h of (hc.hpa_results ?? []).filter((h: any) => h.status !== "healthy")) {
        h2(`HPA: ${h.namespace}/${h.name} [${h.status}]`);
        line(`Replicas: atual=${h.current_replicas} desejado=${h.desired_replicas} min=${h.min_replicas} max=${h.max_replicas}`, 4);
      }
    }

    if ((inv.dependencies ?? []).length > 0) {
      h1("DEPENDENCIAS EXTERNAS");
      const seen = new Set<string>();
      for (const dep of inv.dependencies) {
        const k = `${dep.service_type}:${dep.service_name}`;
        if (seen.has(k)) continue; seen.add(k);
        line(`[${dep.service_type}] ${dep.service_name}${dep.topic_name ? ` (${dep.topic_name})` : ""}`, 4);
      }
      y += 4;
    }

    const aiText = inv.ai_analysis || quickAnalysis;
    if (aiText) {
      h1("ANALISE AI");
      const stripped = sanitizePDF(aiText.replace(/[#*`]/g, ""));
      const paras = stripped.split("\n").filter(Boolean);
      for (const p of paras) {
        checkPage(8);
        const wrapped = doc.splitTextToSize(p, contentWidth);
        doc.setFontSize(9); doc.setFont("helvetica", "normal");
        doc.text(wrapped, margin, y);
        y += wrapped.length * 5 + 1;
      }
    }
  }

  doc.save(`dt-diagnostico-${problem.displayId}-${new Date().toISOString().slice(0, 10)}.pdf`);
}

// ── DTMetricSparkline — mini chart de série temporal ─────────────────────────────

function DTMetricSparkline({ metric, color = "#6366f1" }: { metric: { label: string; key: string; unit: string; points: { t: number; v: number }[] }; color?: string }) {
  const data = (metric.points ?? [])
    .filter(p => !isNaN(p.v) && p.v >= 0)
    .map(p => ({ t: p.t, v: parseFloat(p.v.toFixed(2)) }));

  if (data.length < 2) return null;
  const max = Math.max(...data.map(d => d.v));
  const avg = data.reduce((s, d) => s + d.v, 0) / data.length;
  const isCritical = (metric.key === "error_rate" && max > 20) || (metric.key === "pods_ready_pct" && max < 80) || (metric.key === "cpu_throttle" && max > 500) || (metric.key === "pod_restarts" && max > 5);
  const isWarn = !isCritical && ((metric.key === "error_rate" && max > 5) || (metric.key === "response_p95" && max > 1000) || (metric.key === "pod_restarts" && max > 1));
  const chartColor = isCritical ? "#ef4444" : isWarn ? "#f97316" : color;

  // Ticks a cada 5 minutos
  const FIVE_MIN = 5 * 60 * 1000;
  const firstT = data[0].t;
  const lastT = data[data.length - 1].t;
  const firstTick = Math.ceil(firstT / FIVE_MIN) * FIVE_MIN;
  const xTicks: number[] = [];
  for (let t = firstTick; t <= lastT; t += FIVE_MIN) xTicks.push(t);

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className={`font-medium ${isCritical ? "text-red-400" : isWarn ? "text-orange-400" : "text-muted-foreground"}`}>{metric.label}</span>
        <span className="font-mono text-[10px] text-muted-foreground">avg {avg.toFixed(1)} · max {max.toFixed(1)} <span className="opacity-60">{metric.unit}</span></span>
      </div>
      <ResponsiveContainer width="100%" height={70}>
        <AreaChart data={data} margin={{ top: 2, right: 4, bottom: 0, left: 4 }}>
          <defs>
            <linearGradient id={`grad-${metric.key}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={chartColor} stopOpacity={0.3} />
              <stop offset="95%" stopColor={chartColor} stopOpacity={0} />
            </linearGradient>
          </defs>
          <XAxis
            dataKey="t"
            type="number"
            domain={["dataMin", "dataMax"]}
            ticks={xTicks}
            tickFormatter={t => new Date(t).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" })}
            tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
            tickLine={false}
            axisLine={false}
            scale="time"
          />
          <YAxis hide domain={["auto", "auto"]} />
          <Area type="monotone" dataKey="v" stroke={chartColor} strokeWidth={1.5} fill={`url(#grad-${metric.key})`} dot={false} />
          <RechartsTooltip
            contentStyle={{ fontSize: "10px", padding: "2px 6px", background: "hsl(var(--popover))", border: "1px solid hsl(var(--border))" }}
            formatter={(v: number) => [`${v.toFixed(2)} ${metric.unit}`, metric.label]}
            labelFormatter={(t: number) => new Date(t).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" })}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

// ── DiagnosticoTab — Diagnóstico unificado (Análise Rápida + Investigação Profunda) ──

function DiagnosticoTab({
  problem,
  aiEmail,
  uiBaseUrl,
  onQuickAnalyze,
  quickAnalyzing,
  quickAnalysisResult,
  quickActionItems,
  onCancelQuickAnalyze,
  onInvestigate,
  investigating,
  investigationResult,
  onCancelInvestigate,
}: {
  problem: DynatraceProblem;
  aiEmail: string;
  uiBaseUrl?: string;
  onQuickAnalyze: () => void;
  quickAnalyzing: boolean;
  quickAnalysisResult: string;
  quickActionItems?: ActionItem[];
  onCancelQuickAnalyze: () => void;
  onInvestigate: () => void;
  investigating: boolean;
  investigationResult: any;
  onCancelInvestigate: () => void;
}) {
  const isWorking = quickAnalyzing || investigating;
  const hasContent = !!investigationResult || !!quickAnalysisResult;
  const aiText = investigationResult?.ai_analysis || quickAnalysisResult;

  return (
    <div className="space-y-4">
      <ProblemHeader problem={problem} uiBaseUrl={uiBaseUrl} />

      {/* Barra de ações */}
      <div className="flex items-center gap-2 flex-wrap">
        {investigating ? (
          <Button
            size="sm"
            variant="destructive"
            onClick={onCancelInvestigate}
            title="Cancelar investigação"
          >
            <X className="h-3 w-3 mr-1.5" />Cancelar
          </Button>
        ) : (
          <Button
            size="sm"
            onClick={onInvestigate}
            disabled={quickAnalyzing}
            title="Identifica cluster K8s, executa Health Check direcionado e analisa com IA"
          >
            <Microscope className="h-3 w-3 mr-1.5" />{investigationResult ? "Re-investigar" : "Investigar Profundo"}
          </Button>
        )}
        {quickAnalyzing ? (
          <Button
            size="sm"
            variant="outline"
            onClick={onCancelQuickAnalyze}
            title="Cancelar análise"
          >
            <X className="h-3 w-3 mr-1.5" />Cancelar
          </Button>
        ) : (
          <Button
            size="sm"
            variant="outline"
            onClick={onQuickAnalyze}
            disabled={investigating}
            title="Analisa apenas métricas DT + IA (sem Health Check K8s — mais rápido)"
          >
            <Bot className="h-3 w-3 mr-1.5" />{quickAnalysisResult ? "Re-analisar" : "Análise Rápida"}
          </Button>
        )}
        {hasContent && (
          <div className="flex items-center gap-1.5 ml-auto">
            <Button
              size="sm" variant="ghost"
              className="h-7 px-2 text-xs gap-1 text-muted-foreground"
              onClick={() => exportMarkdown(problem, investigationResult, quickAnalysisResult)}
              title="Exportar como Markdown"
            >
              <FileText className="h-3 w-3" />MD
            </Button>
            <Button
              size="sm" variant="ghost"
              className="h-7 px-2 text-xs gap-1 text-muted-foreground"
              onClick={() => void exportPDF(problem, investigationResult, quickAnalysisResult)}
              title="Exportar como PDF"
            >
              <FileDown className="h-3 w-3" />PDF
            </Button>
          </div>
        )}
      </div>

      {/* Estado de loading */}
      {investigating && (
        <div className="flex flex-col items-center gap-2 py-10 text-center">
          <Microscope className="h-7 w-7 animate-pulse text-blue-400" />
          <p className="text-sm font-medium">Investigando...</p>
          <p className="text-xs text-muted-foreground">Node Pool Registry → Health Check K8s → Métricas DT → Análise AI</p>
        </div>
      )}
      {quickAnalyzing && !investigating && (
        <div className="flex flex-col items-center gap-2 py-10 text-center">
          <RefreshCw className="h-7 w-7 animate-spin text-muted-foreground" />
          <p className="text-sm font-medium">Analisando com IA...</p>
          <p className="text-xs text-muted-foreground">Coletando métricas, evidências Davis e correlações</p>
        </div>
      )}

      {/* Placeholder */}
      {!isWorking && !hasContent && (
        <div className="flex flex-col items-center gap-3 py-12 text-center text-muted-foreground">
          <Microscope className="h-10 w-10 opacity-20" />
          <div className="space-y-1">
            <p className="text-sm font-medium">Nenhum diagnóstico disponível</p>
            <p className="text-xs">
              <strong>Investigar Profundo</strong> — identifica cluster, executa HC K8s, analisa dependências e IA<br />
              <strong>Análise Rápida</strong> — só métricas DT + IA (mais rápido)
            </p>
          </div>
        </div>
      )}

      {/* Resultado da Investigação */}
      {!isWorking && investigationResult && (
        <div className="space-y-3">
          {/* Identificação */}
          <Card className="border-blue-500/30 bg-blue-500/5">
            <CardHeader className="py-2 px-3">
              <p className="text-xs font-semibold text-blue-400 flex items-center gap-1.5">
                <MapPin className="h-3 w-3" />Entidades Identificadas
              </p>
            </CardHeader>
            <CardContent className="px-3 pb-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
              {investigationResult.root_cause_entity_name && (<>
                <span className="text-muted-foreground">Causa Raiz (DT)</span>
                <span className="font-mono">{investigationResult.root_cause_entity_name} <span className="text-muted-foreground">({investigationResult.root_cause_entity_type})</span></span>
              </>)}
              {investigationResult.identified_cluster ? (<>
                <span className="text-muted-foreground">Cluster K8s</span>
                <span className="font-mono">{investigationResult.identified_cluster} <span className="text-muted-foreground">/ {investigationResult.identified_node_pool}</span></span>
              </>) : (<>
                <span className="text-muted-foreground">Cluster K8s</span>
                <span className="text-yellow-500">Não identificado — execute "Escanear Clusters"</span>
              </>)}
              {investigationResult.identified_namespace && (<>
                <span className="text-muted-foreground">Namespace</span>
                <span className="font-mono">{investigationResult.identified_namespace}</span>
              </>)}
              {investigationResult.identified_workload && (<>
                <span className="text-muted-foreground">Workload</span>
                <span className="font-mono">{investigationResult.identified_workload}</span>
              </>)}
            </CardContent>
          </Card>

          {/* Métricas DT com charts — igual à tab Métricas (agrupado por família) */}
          {(() => {
            const entities = (investigationResult.dt_metrics?.entities ?? []).filter((e: any) =>
              (e.metrics ?? []).some((m: any) => (m.points ?? []).some((p: any) => !isNaN(p.v) && p.v >= 0))
            );
            if (!entities.length) return null;
            const hasVRP = (problem.affectedEntities?.length ?? 0) > 0 || !!problem.rootCauseEntity;
            const problemStartMs = new Date(problem.startTime).getTime();

            return (
              <Card>
                <CardHeader className="py-2 px-3">
                  <p className="text-xs font-semibold flex items-center gap-1.5">
                    <BarChart3 className="h-3 w-3 text-purple-400" />
                    Métricas Dynatrace
                    <Badge variant="outline" className="text-[9px] ml-auto">{entities.length} {entities.length === 1 ? "entidade" : "entidades"}</Badge>
                  </p>
                </CardHeader>
                <CardContent className="px-3 pb-3 space-y-8">
                  {entities.map((ed: any, ei: number) => (
                    <div key={ei}>
                      <EntityMetricsSection entity={ed} problemStartMs={problemStartMs} columns={2} />
                      {ei < entities.length - 1 && (
                        <div className="border-t border-dashed border-border/40 mt-4" />
                      )}
                    </div>
                  ))}
                  {hasVRP && (
                    <div className="rounded-lg border border-orange-500/30 bg-orange-500/5 overflow-hidden">
                      <p className="text-[10px] font-semibold text-orange-400 flex items-center gap-1 px-2 pt-1.5 pb-0.5">
                        <Network className="h-3 w-3" />Fluxo de Propagação
                      </p>
                      <VRPEmbed problem={problem} />
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })()}

          {/* Health Check K8s */}
          {investigationResult.health_check_error && (
            <Card className="border-red-500/30 bg-red-500/5">
              <CardContent className="px-3 py-2 text-xs text-red-400">
                Health Check falhou: {investigationResult.health_check_error}
              </CardContent>
            </Card>
          )}
          {investigationResult.health_check_result && (() => {
            const hc = investigationResult.health_check_result as any;
            const allDeploys = hc.deployment_results ?? [];
            const hpaProblems = (hc.hpa_results ?? []).filter((h: any) => h.status !== "healthy");
            const events = hc.event_results ?? [];
            const statusColor = hc.overall_status === "healthy" ? "green" : hc.overall_status === "critical" ? "red" : "yellow";
            return (
              <Card className={`border-${statusColor}-500/30 bg-${statusColor}-500/5`}>
                <CardHeader className="py-2 px-3">
                  <p className="text-xs font-semibold flex items-center gap-1.5">
                    <Activity className="h-3 w-3" />Health Check K8s — {investigationResult.identified_cluster}
                    <Badge variant="outline" className={`text-[9px] ml-auto border-${statusColor}-500 text-${statusColor}-400`}>
                      {hc.overall_status}
                    </Badge>
                  </p>
                </CardHeader>
                <CardContent className="px-3 pb-3 space-y-3">
                  <div className="flex gap-3 text-xs text-muted-foreground">
                    <span>Total: <strong className="text-foreground">{hc.total_checks}</strong></span>
                    {hc.severity_counts?.critical > 0 && <span className="text-red-400">Críticos: <strong>{hc.severity_counts.critical}</strong></span>}
                    {hc.severity_counts?.high > 0 && <span className="text-orange-400">Altos: <strong>{hc.severity_counts.high}</strong></span>}
                  </div>
                  {allDeploys.length > 0 && (
                    <div className="space-y-2">
                      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Deployments</p>
                      {allDeploys.map((d: any, i: number) => (
                        <div key={i} className={`text-xs rounded px-2 py-1.5 space-y-1 ${d.status !== "healthy" ? "bg-red-500/10" : "bg-background/50"}`}>
                          <div className="flex justify-between">
                            <span className={`font-mono font-medium ${d.status !== "healthy" ? "text-red-300" : ""}`}>
                              {d.namespace}/{d.name}
                            </span>
                            <span className="text-muted-foreground">{d.replicas_ready}/{d.replicas_desired} pods</span>
                          </div>
                          {(d.containers_crash > 0 || d.image_pull_errors > 0) && (
                            <div className="text-red-400 text-[10px]">
                              {d.containers_crash > 0 && <span>Crashes: {d.containers_crash} </span>}
                              {d.image_pull_errors > 0 && <span>ImagePull errors: {d.image_pull_errors}</span>}
                            </div>
                          )}
                          {(d.cpu_usage_percent > 0 || d.memory_usage_percent > 0) && (
                            <div className="text-[10px] text-muted-foreground">
                              CPU: {d.cpu_usage_percent.toFixed(1)}% | Mem: {d.memory_usage_percent.toFixed(1)}% | QoS: {d.qos_class}
                            </div>
                          )}
                          {(d.container_resources ?? []).map((cr: any, ci: number) => (
                            <div key={ci} className="text-[10px] font-mono text-muted-foreground/70 pl-2">
                              {cr.name}: cpu {cr.cpu_request || "—"}/{cr.cpu_limit || "—"} · mem {cr.memory_request || "—"}/{cr.memory_limit || "—"}
                            </div>
                          ))}
                        </div>
                      ))}
                    </div>
                  )}
                  {hpaProblems.length > 0 && (
                    <div className="space-y-1">
                      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">HPAs com problema</p>
                      {hpaProblems.map((h: any, i: number) => (
                        <div key={i} className="text-xs bg-orange-500/10 rounded px-2 py-1 flex justify-between">
                          <span className="font-mono">{h.namespace}/{h.name}</span>
                          <span className="text-muted-foreground">{h.current_replicas}/{h.desired_replicas} (max {h.max_replicas})</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {events.length > 0 && (
                    <div className="space-y-1">
                      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Eventos críticos</p>
                      {events.slice(0, 5).map((e: any, i: number) => (
                        <div key={i} className="text-xs bg-background/50 rounded px-2 py-1">
                          <span className="text-orange-400">[{e.severity}]</span> {e.namespace}/{e.name} — {e.reason}
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })()}

          {/* Dependências */}
          {(investigationResult.dependencies ?? []).length > 0 && (
            <Card>
              <CardHeader className="py-2 px-3">
                <p className="text-xs font-semibold flex items-center gap-1.5">
                  <Network className="h-3 w-3" />Dependências Externas ({investigationResult.dependencies!.length})
                </p>
              </CardHeader>
              <CardContent className="px-3 pb-3 space-y-1">
                {Array.from(new Map(investigationResult.dependencies!.map((d: any) => [`${d.service_type}:${d.service_name}`, d])).values()).slice(0, 12).map((dep: any, i: number) => (
                  <div key={i} className="text-xs flex items-center gap-2">
                    <Badge variant="outline" className="text-[9px] shrink-0">{dep.service_type}</Badge>
                    <span className="font-mono text-muted-foreground truncate">{dep.service_name}</span>
                    {dep.topic_name && <span className="text-[9px] text-muted-foreground/60">({dep.topic_name})</span>}
                  </div>
                ))}
              </CardContent>
            </Card>
          )}
        </div>
      )}

      {/* Análise AI — usa AIAnalysisResult para formatação bela (seções colapsáveis, ícones) */}
      {!isWorking && aiText && (
        <AIAnalysisResult
          text={aiText}
          actionItems={investigationResult ? undefined : quickActionItems}
        />
      )}

      {/* ActionPlanCard para análise rápida (sem investigação) */}
      {!isWorking && !investigationResult && quickActionItems && quickActionItems.length > 0 && !quickAnalysisResult && (
        <ActionPlanCard items={quickActionItems} />
      )}

      {investigationResult?.ai_error && !aiText && (
        <Card className="border-red-500/30">
          <CardContent className="px-3 py-2 text-xs text-red-400">
            Análise AI falhou: {investigationResult.ai_error}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// ── ProblemDetailPanel — wrapper com abas ───────────────────────────────────────

function ProblemDetailPanel({
  problem,
  aiEmail,
  uiBaseUrl,
  onQuickAnalyze,
  quickAnalyzing,
  quickAnalysisResult,
  quickActionItems,
  onCancelQuickAnalyze,
  onInvestigate,
  investigating,
  investigationResult,
  onCancelInvestigate,
}: {
  problem: DynatraceProblem;
  aiEmail: string;
  uiBaseUrl?: string;
  onQuickAnalyze: () => void;
  quickAnalyzing: boolean;
  quickAnalysisResult: string;
  quickActionItems?: ActionItem[];
  onCancelQuickAnalyze: () => void;
  onInvestigate: () => void;
  investigating: boolean;
  investigationResult: any;
  onCancelInvestigate: () => void;
}) {
  const hasWorkloads = (problem.k8sWorkloads ?? []).some(w => w.AppName);
  const [metricsRightWidth, setMetricsRightWidth] = useState(380);
  const hasActivity = investigating || quickAnalyzing || !!investigationResult || !!quickAnalysisResult;

  return (
    <Tabs defaultValue="details" className="w-full">
      <TabsList className="w-full grid grid-cols-4 h-8 mb-4">
        <TabsTrigger value="details" className="text-xs gap-1.5">
          <Info className="h-3 w-3" />Visão Geral
        </TabsTrigger>
        <TabsTrigger value="metrics" className="text-xs gap-1.5">
          <BarChart3 className="h-3 w-3" />Métricas
        </TabsTrigger>
        <TabsTrigger value="github" className="text-xs gap-1.5">
          <GitBranch className="h-3 w-3" />
          <span>GitHub</span>
          {hasWorkloads && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
        </TabsTrigger>
        <TabsTrigger value="diagnostico" className="text-xs gap-1.5">
          <Microscope className="h-3 w-3" />Diagnóstico
          {(investigating || quickAnalyzing) && <RefreshCw className="h-2.5 w-2.5 animate-spin" />}
          {hasActivity && !investigating && !quickAnalyzing && <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-500" />}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="details" className="mt-0">
        <DetailsTab problem={problem} uiBaseUrl={uiBaseUrl} />
      </TabsContent>

      <TabsContent value="metrics" className="mt-0">
        <div className="flex gap-0 items-start">
          <div className="flex-1 min-w-0">
            <DynatraceMetricsPanel
              problemId={problem.problemId}
              aiEmail={aiEmail}
              problemTitle={problem.title}
            />
          </div>
          <DTResizeDivider onDrag={d => setMetricsRightWidth(w => Math.max(260, Math.min(700, w - d)))} />
          <div className="flex-shrink-0" style={{ width: metricsRightWidth }}>
            <DynatraceContextPanel
              problemId={problem.problemId}
              aiEmail={aiEmail}
              uiBaseUrl={uiBaseUrl}
            />
          </div>
        </div>
      </TabsContent>

      <TabsContent value="github" className="mt-0">
        <DynatraceGitHubSection problem={problem} />
      </TabsContent>

      <TabsContent value="diagnostico" className="mt-0">
        <DiagnosticoTab
          problem={problem}
          aiEmail={aiEmail}
          uiBaseUrl={uiBaseUrl}
          onQuickAnalyze={onQuickAnalyze}
          quickAnalyzing={quickAnalyzing}
          quickAnalysisResult={quickAnalysisResult}
          quickActionItems={quickActionItems}
          onCancelQuickAnalyze={onCancelQuickAnalyze}
          onInvestigate={onInvestigate}
          investigating={investigating}
          investigationResult={investigationResult}
          onCancelInvestigate={onCancelInvestigate}
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
  const [quickAnalysisResult, setQuickAnalysisResult] = useState<string>("");
  const [quickActionItems, setQuickActionItems] = useState<ActionItem[]>([]);
  const [quickAnalyzingId, setQuickAnalyzingId] = useState<string | null>(null);
  const [investigatingId, setInvestigatingId] = useState<string | null>(null);
  const [investigationResult, setInvestigationResult] = useState<any>(null);

  const analyzeAbortRef = useRef<AbortController | null>(null);
  const investigateAbortRef = useRef<AbortController | null>(null);

  // Filtro por management zone (alert profile) / tag
  const [mzFilter, setMzFilter] = useState("");      // management zone selecionada no dropdown
  const [tagInput, setTagInput] = useState("");       // filtro livre: "tag:valor" ou texto
  const [activeFilter, setActiveFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("OPEN"); // OPEN | CLOSED | ALL
  const [dateFrom, setDateFrom] = useState("");       // ISO date para busca histórica
  const [dateTo, setDateTo] = useState("");

  // Intervalo de atualização automática
  const REFRESH_OPTIONS = [
    { label: "Nunca",    value: 0 },
    { label: "30s",      value: 30_000 },
    { label: "1 min",    value: 60_000 },
    { label: "5 min",    value: 5 * 60_000 },
    { label: "10 min",   value: 10 * 60_000 },
  ] as const;
  const [refreshInterval, setRefreshInterval] = useState<number>(60_000);

  // Management zones disponíveis no ambiente DT (alert profiles)
  const { data: mzData } = useQuery({
    queryKey: ["dynatrace-mz", aiEmail],
    queryFn: () => apiClient.getDynatraceManagementZones(aiEmail),
    enabled: !!aiEmail,
    staleTime: 5 * 60_000,
  });
  const managementZones = mzData?.zones ?? [];

  // Filtro efetivo: MZ dropdown tem prioridade sobre texto livre
  const effectiveFilter = mzFilter || tagInput.trim();

  const applyFilter = () => {
    setActiveFilter(effectiveFilter);
    setSelectedProblem(null);
    setQuickAnalysisResult("");
    setInvestigationResult(null);
    queryClient.invalidateQueries({ queryKey: ["dynatrace-problems", aiEmail, effectiveFilter, statusFilter, dateFrom, dateTo] });
  };

  const clearFilter = () => {
    setMzFilter("");
    setTagInput("");
    setActiveFilter("");
    setDateFrom("");
    setDateTo("");
    setSelectedProblem(null);
    setQuickAnalysisResult("");
  };

  const { data, isLoading, refetch, isRefetching } = useQuery({
    queryKey: ["dynatrace-problems", aiEmail, activeFilter, statusFilter, dateFrom, dateTo],
    queryFn: () => apiClient.getDynatraceProblems(
      aiEmail,
      activeFilter || undefined,
      statusFilter,
      dateFrom || undefined,
      dateTo || undefined,
    ),
    enabled: !!aiEmail,
    // Auto-refresh só faz sentido para problems abertos — fechados são histórico estático
    refetchInterval: statusFilter === "OPEN" && refreshInterval > 0 ? refreshInterval : false,
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
    mutationFn: (p: DynatraceProblem) => {
      analyzeAbortRef.current = new AbortController();
      return apiClient.analyzeDynatraceProblem(p.problemId, aiEmail, analyzeAbortRef.current.signal);
    },
    onMutate: (p) => {
      setQuickAnalyzingId(p.problemId);
      setQuickAnalysisResult("");
      setQuickActionItems([]);
    },
    onSuccess: (result) => {
      setQuickAnalysisResult(result.analysis);
      setQuickActionItems(result.action_items ?? []);
      setQuickAnalyzingId(null);
    },
    onError: (err: any) => {
      if (err?.name === "AbortError") return; // cancelado pelo usuário — não exibe erro
      setQuickAnalysisResult(`**Erro na análise:** ${err.message ?? "Erro desconhecido"}`);
      setQuickAnalyzingId(null);
    },
  });

  const investigateMutation = useMutation({
    mutationFn: (p: DynatraceProblem) => {
      investigateAbortRef.current = new AbortController();
      return apiClient.investigateDynatraceProblem(p.problemId, aiEmail, investigateAbortRef.current.signal);
    },
    onMutate: (p) => {
      setInvestigatingId(p.problemId);
      setInvestigationResult(null);
    },
    onSuccess: (result) => {
      setInvestigationResult(result);
      setInvestigatingId(null);
    },
    onError: (err: any) => {
      if (err?.name === "AbortError") return; // cancelado pelo usuário — não exibe erro
      setInvestigationResult({ ai_error: err.message ?? "Erro desconhecido" });
      setInvestigatingId(null);
    },
  });

  const handleCancelInvestigate = () => {
    investigateAbortRef.current?.abort();
    investigateAbortRef.current = null;
    setInvestigatingId(null);
    investigateMutation.reset();
  };

  const handleCancelQuickAnalyze = () => {
    analyzeAbortRef.current?.abort();
    analyzeAbortRef.current = null;
    setQuickAnalyzingId(null);
    analyzeMutation.reset();
  };

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
              // Limpar datas ao voltar para OPEN (datas só fazem sentido em CLOSED/ALL)
              if (s === "OPEN") { setDateFrom(""); setDateTo(""); }
              queryClient.invalidateQueries({ queryKey: ["dynatrace-problems", aiEmail, activeFilter, s, dateFrom, dateTo] });
            }}
          >
            {s === "OPEN" ? "Abertos" : s === "CLOSED" ? "Fechados" : "Todos"}
          </Button>
        ))}
      </div>

      {/* Filtro por Alert Profile (Management Zone) */}
      <div className="space-y-2">
        {/* Dropdown de management zones */}
        <div className="flex gap-1.5">
          <Select value={mzFilter} onValueChange={v => { setMzFilter(v === "__all__" ? "" : v); setTagInput(""); }}>
            <SelectTrigger className="h-8 text-xs flex-1 min-w-0">
              <SelectValue placeholder={
                managementZones.length > 0
                  ? "Alert Profile (Management Zone)..."
                  : "Carregando alert profiles..."
              } />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__" className="text-xs text-muted-foreground">— Todos os profiles —</SelectItem>
              {managementZones.map(mz => (
                <SelectItem key={mz.id} value={mz.name} className="text-xs">{mz.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {(mzFilter || tagInput.trim() || dateFrom || dateTo || activeFilter) && (
            <Button size="sm" variant="ghost" className="h-8 w-8 p-0 shrink-0" onClick={clearFilter} title="Limpar filtros">
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>

        {/* Filtro personalizado por tag (opcional, secundário) */}
        <div className="flex gap-1.5">
          <div className="relative flex-1">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
            <Input
              placeholder="tag:valor ou texto livre..."
              value={tagInput}
              onChange={e => { setTagInput(e.target.value); if (e.target.value) setMzFilter(""); }}
              onKeyDown={e => e.key === "Enter" && applyFilter()}
              className="pl-7 h-8 text-xs"
              disabled={!!mzFilter}
            />
          </div>
          <Button size="sm" variant="secondary" className="h-8 shrink-0 text-xs px-2.5" onClick={applyFilter}>
            Buscar
          </Button>
        </div>

        {/* Período de busca — relevante para CLOSED/ALL */}
        {statusFilter !== "OPEN" && (
          <div className="space-y-1.5">
            <p className="text-[10px] text-muted-foreground font-medium">Período de busca</p>
            {/* Atalhos rápidos */}
            <div className="flex flex-wrap gap-1">
              {[
                { label: "6h",     value: "now-6h"  },
                { label: "24h",    value: "now-24h" },
                { label: "3 dias", value: "now-3d"  },
                { label: "7 dias", value: "now-7d"  },
                { label: "14 dias",value: "now-14d" },
                { label: "30 dias",value: "now-30d" },
              ].map(opt => (
                <Button
                  key={opt.value}
                  size="sm"
                  variant={dateFrom === opt.value ? "default" : "outline"}
                  className="h-6 text-[10px] px-2"
                  onClick={() => { setDateFrom(opt.value); setDateTo(""); }}
                >
                  {opt.label}
                </Button>
              ))}
            </div>
            {/* Data customizada */}
            <div className="flex gap-1.5 items-center">
              <Input
                type="date"
                className="h-7 text-xs flex-1"
                value={dateFrom.startsWith("now") ? "" : dateFrom}
                onChange={e => { setDateFrom(e.target.value); }}
                title="Data inicial"
              />
              <span className="text-[10px] text-muted-foreground shrink-0">até</span>
              <Input
                type="date"
                className="h-7 text-xs flex-1"
                value={dateTo}
                onChange={e => setDateTo(e.target.value)}
                title="Data final (vazio = agora)"
              />
              {(dateFrom || dateTo) && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 w-7 p-0 shrink-0"
                  onClick={() => { setDateFrom(""); setDateTo(""); }}
                  title="Limpar datas"
                >
                  <X className="h-3 w-3" />
                </Button>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Status da busca */}
      {data && !data.dt_not_configured && (
        <div className="text-[10px] text-muted-foreground flex items-center gap-1.5 flex-wrap">
          {activeFilter && (
            <Badge variant="outline" className="text-[10px] gap-1">
              <MapPin className="h-2.5 w-2.5" />{activeFilter}
            </Badge>
          )}
          {dateFrom && (
            <Badge variant="outline" className="text-[10px] gap-1">
              <Clock className="h-2.5 w-2.5" />
              {dateFrom.startsWith("now-") ? `últimos ${dateFrom.replace("now-", "")}` : dateFrom}
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
          <div className="text-center py-16 text-sm text-muted-foreground space-y-1">
            <p>{activeFilter ? `Nenhum problem para "${activeFilter}"` : `Nenhum problem ${statusFilter === "OPEN" ? "aberto" : statusFilter === "CLOSED" ? "fechado" : ""} encontrado`}</p>
            {statusFilter !== "OPEN" && !dateFrom && !dateTo && (
              <p className="text-xs">Tente definir um período de busca acima</p>
            )}
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
                setQuickAnalysisResult("");
                setQuickActionItems([]);
                setInvestigationResult(null);
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
        quickAnalyzing={quickAnalyzingId === selectedProblem.problemId}
        quickAnalysisResult={quickAnalysisResult}
        quickActionItems={quickActionItems}
        onQuickAnalyze={() => analyzeMutation.mutate(selectedProblem)}
        onCancelQuickAnalyze={handleCancelQuickAnalyze}
        investigating={investigatingId === selectedProblem.problemId}
        investigationResult={investigationResult}
        onInvestigate={() => investigateMutation.mutate(selectedProblem)}
        onCancelInvestigate={handleCancelInvestigate}
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
              {/* Split button: atualizar agora + seletor de intervalo */}
              <div className="flex items-center border rounded h-6 overflow-hidden">
                <button
                  onClick={() => refetch()}
                  disabled={isRefetching || isLoading}
                  title="Atualizar agora"
                  className="flex items-center justify-center w-6 h-full hover:bg-accent disabled:opacity-50 transition-colors"
                >
                  <RefreshCw className={`h-3 w-3 ${isRefetching ? "animate-spin" : ""}`} />
                </button>
                <div className="w-px h-full bg-border" />
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button className="flex items-center justify-center w-4 h-full hover:bg-accent transition-colors">
                      <ChevronDown className="h-2.5 w-2.5" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="min-w-[130px]">
                    <DropdownMenuLabel className="text-[11px]">Auto-atualizar</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    {REFRESH_OPTIONS.map(opt => (
                      <DropdownMenuItem
                        key={opt.value}
                        className="text-xs gap-2"
                        onClick={() => setRefreshInterval(opt.value)}
                      >
                        {refreshInterval === opt.value && <span className="text-primary">✓</span>}
                        {refreshInterval !== opt.value && <span className="w-3" />}
                        {opt.label}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
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
