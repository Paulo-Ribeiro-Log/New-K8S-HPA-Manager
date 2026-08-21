import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, RefreshCw, Copy, Check, FileText, Search, X as XIcon, Braces, AlertTriangle, Bell, Network } from "lucide-react";
import { PortForwardModal } from "@/components/PortForwardModal";
import type { PodSummary, PodMetricsSingle, EventSummary } from "@/lib/api/types";
import { formatAge, formatMillicores, formatBytes, formatPercent } from "@/lib/monitorUtils";
import { describeExitCode } from "@/lib/exitCodes";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useK8sPermissions } from "@/hooks/useK8sPermissions";
import { toast } from "sonner";
import { useJsonInspector } from "@/hooks/useJsonInspector";
import { JsonInspectorModal, JsonFloatingButton } from "@/components/JsonInspectorModal";
import { DeploymentBehaviorChart } from "@/components/DeploymentBehaviorChart";
import { usePodLogStream, type PodLogStreamTarget } from "@/hooks/usePodLogStream";

// Gauge duplo concêntrico: anel externo = MEM, anel interno = CPU
function DualGauge({ cpuPct, memPct, cpuVal, memVal }: {
  cpuPct: number; memPct: number; cpuVal: string; memVal: string;
}) {
  const CX = 52, CY = 52;
  const R_OUT = 42, R_IN = 28, STROKE = 7;
  const clamp = (v: number) => Math.max(0, Math.min(100, isNaN(v) ? 0 : v));
  const cp = clamp(cpuPct), mp = clamp(memPct);

  const arc = (r: number, pct: number) => {
    const circ = 2 * Math.PI * r;
    return { dash: circ, offset: circ * (1 - pct / 100) };
  };
  const memArc = arc(R_OUT, mp);
  const cpuArc = arc(R_IN, cp);

  const cpuColor = cp >= 90 ? "#ef4444" : cp >= 70 ? "#f97316" : "#22c55e";
  const memColor = mp >= 90 ? "#ef4444" : mp >= 70 ? "#f97316" : "#8b5cf6";

  return (
    <div className="flex flex-col items-center flex-shrink-0">
      <svg width="104" height="104" viewBox="0 0 104 104">
        <circle cx={CX} cy={CY} r={R_OUT} fill="none" stroke="rgba(255,255,255,0.07)" strokeWidth={STROKE} />
        <circle cx={CX} cy={CY} r={R_IN} fill="none" stroke="rgba(255,255,255,0.07)" strokeWidth={STROKE} />
        <circle cx={CX} cy={CY} r={R_OUT} fill="none"
          stroke={memColor} strokeWidth={STROKE}
          strokeDasharray={memArc.dash} strokeDashoffset={memArc.offset}
          strokeLinecap="round" transform={`rotate(-90 ${CX} ${CY})`}
          style={{ transition: "stroke-dashoffset 0.6s ease" }}
        />
        <circle cx={CX} cy={CY} r={R_IN} fill="none"
          stroke={cpuColor} strokeWidth={STROKE}
          strokeDasharray={cpuArc.dash} strokeDashoffset={cpuArc.offset}
          strokeLinecap="round" transform={`rotate(-90 ${CX} ${CY})`}
          style={{ transition: "stroke-dashoffset 0.6s ease" }}
        />
        <text x={CX} y={CY - 5} textAnchor="middle" style={{ fontSize: 13, fontWeight: 700, fill: cpuColor }}>
          {cp < 1 && cpuPct > 0 ? "<1%" : `${Math.round(cp)}%`}
        </text>
        <text x={CX} y={CY + 8} textAnchor="middle" style={{ fontSize: 8, fill: "rgba(255,255,255,0.45)" }}>
          CPU
        </text>
      </svg>
      <div className="flex gap-3 text-[9px] font-mono mt-0.5">
        <span style={{ color: cpuColor }}>● CPU {cpuVal}</span>
        <span style={{ color: memColor }}>● MEM {memVal}</span>
      </div>
    </div>
  );
}

// Exportados pra reuso em AllPodsLogsModal.tsx — mesmos recursos (filtro de nível, coloração por
// severidade) reaproveitados no modal de "logs de todos os pods", não uma versão reduzida.
export type LogLevel = "error" | "warn" | "info" | "debug";

export function getLogLevel(line: string): LogLevel | null {
  const l = line.toUpperCase();
  if (/\b(ERROR|FATAL|EXCEPTION|PANIC)\b/.test(l)) return "error";
  if (/\b(WARN|WARNING)\b/.test(l)) return "warn";
  if (/\b(INFO)\b/.test(l)) return "info";
  if (/\b(DEBUG|TRACE)\b/.test(l)) return "debug";
  return null;
}

export function logLineColor(line: string): string {
  const level = getLogLevel(line);
  if (level === "error") return "text-red-400";
  if (level === "warn") return "text-yellow-400";
  if (level === "debug") return "text-purple-400";
  if (level === "info") return "text-blue-400";
  if (/\s[45]\d{2}\s/.test(line)) return "text-orange-400";
  if (/\s2\d{2}\s/.test(line)) return "text-green-400";
  return "";
}

export const LOG_LEVEL_CONFIG: Record<LogLevel, { label: string; active: string; inactive: string }> = {
  error: { label: "ERR",   active: "bg-red-500/20 text-red-400 border-red-500/50",    inactive: "text-muted-foreground border-border hover:border-red-500/40 hover:text-red-400" },
  warn:  { label: "WARN",  active: "bg-yellow-500/20 text-yellow-400 border-yellow-500/50", inactive: "text-muted-foreground border-border hover:border-yellow-500/40 hover:text-yellow-400" },
  info:  { label: "INFO",  active: "bg-blue-500/20 text-blue-400 border-blue-500/50",  inactive: "text-muted-foreground border-border hover:border-blue-500/40 hover:text-blue-400" },
  debug: { label: "DEBUG", active: "bg-purple-500/20 text-purple-400 border-purple-500/50", inactive: "text-muted-foreground border-border hover:border-purple-500/40 hover:text-purple-400" },
};

interface DescribeBlock {
  start: number;
  end: number;
}

// Localiza os blocos "Last State:" no texto do `kubectl describe pod` — cada um
// mostra a causa do reinício anterior de um container (Reason/Exit Code/Started/Finished).
// O bloco termina quando a indentação volta ao nível da própria linha "Last State:".
function findLastStateBlocks(lines: string[]): DescribeBlock[] {
  const getIndent = (line: string) => line.length - line.trimStart().length;
  const blocks: DescribeBlock[] = [];
  lines.forEach((line, i) => {
    if (!/Last State:/.test(line)) return;
    const baseIndent = getIndent(line);
    let end = i;
    for (let j = i + 1; j < lines.length; j++) {
      if (lines[j].trim() === "") break;
      if (getIndent(lines[j]) <= baseIndent) break;
      end = j;
    }
    blocks.push({ start: i, end });
  });
  return blocks;
}

// Localiza a seção "Events:" (tabela de eventos recentes do pod) no texto do describe.
// Retorna null se a seção não existe ou está vazia ("Events:  <none>").
function findEventsBlock(lines: string[]): DescribeBlock | null {
  const getIndent = (line: string) => line.length - line.trimStart().length;
  const idx = lines.findIndex(line => /^Events:/.test(line));
  if (idx === -1) return null;
  if (/<none>/i.test(lines[idx])) return null;
  let end = idx;
  for (let j = idx + 1; j < lines.length; j++) {
    if (lines[j].trim() === "") break;
    if (getIndent(lines[j]) <= 0) break;
    end = j;
  }
  return { start: idx, end };
}

// Exportadas pra reuso em AllPodsLogsModal.tsx (mesma lógica de destaque de busca em logs).
export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Destaca (case-insensitive) todas as ocorrências de `query` dentro de uma linha de log,
// espelhando o match de filterLogLines (mesmo .toLowerCase().includes) — só que aqui
// precisamos das posições exatas pra fatiar a string em spans normais + <mark>.
export function highlightMatches(line: string, query: string): React.ReactNode {
  const text = line || " ";
  const trimmed = query.trim();
  if (!trimmed) return text;
  const escaped = escapeRegExp(trimmed);
  const parts = text.split(new RegExp(`(${escaped})`, "gi"));
  if (parts.length === 1) return text;
  return parts.map((part, idx) =>
    part.toLowerCase() === trimmed.toLowerCase() ? (
      <mark key={idx} className="bg-yellow-400/80 text-black rounded-sm px-0.5">{part}</mark>
    ) : (
      <span key={idx}>{part}</span>
    )
  );
}

function filterLogLines(rawLogs: string, levelFilter: Set<LogLevel>, search: string): string[] {
  const lines = (rawLogs || "").split("\n");
  let result = lines;
  if (levelFilter.size > 0) {
    result = result.filter(line => {
      const level = getLogLevel(line);
      return level !== null && levelFilter.has(level);
    });
  }
  if (search.trim()) {
    const q = search.toLowerCase();
    result = result.filter(line => line.toLowerCase().includes(q));
  }
  return result;
}

// Mostra a causa do reinício ANTERIOR do container selecionado (Exit Code/Reason/Signal/quando),
// sourced de cs.LastTerminationState — só existe enquanto o Pod object atual não tiver sido
// deletado (ex: sobrevive a restarts em CrashLoopBackOff, mas não a um rollout que troca o Pod).
function LastStateBanner({ lastState }: { lastState?: import("@/lib/api/types").ContainerLastState }) {
  if (!lastState) return null;
  const { label, severity } = describeExitCode(lastState.exitCode);
  const colorClass =
    severity === "critical" ? "border-red-500/30 bg-red-500/10 text-red-300"
    : severity === "warning" ? "border-orange-500/30 bg-orange-500/10 text-orange-300"
    : "border-border bg-muted/30 text-muted-foreground";
  return (
    <div className={`mx-4 mt-3 px-3 py-2 rounded border text-xs flex items-start gap-2 flex-shrink-0 ${colorClass}`}>
      <AlertTriangle className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />
      <div>
        <div className="font-medium">
          Última finalização — Exit Code {lastState.exitCode} ({label})
        </div>
        <div className="text-[11px] opacity-80 mt-0.5 flex flex-wrap gap-x-3">
          {lastState.reason && <span>Reason: {lastState.reason}</span>}
          {!!lastState.signal && <span>Signal: {lastState.signal}</span>}
          {lastState.finishedAt && <span>Em: {formatAge(lastState.finishedAt)} atrás</span>}
        </div>
        {lastState.message && (
          <div className="text-[11px] opacity-70 mt-0.5 break-words">{lastState.message}</div>
        )}
      </div>
    </div>
  );
}

// Lista compacta de eventos de Warning do workload dono — Events sobrevivem à deleção do Pod
// (até expirarem pelo TTL do cluster), diferente de logs/describe que só existem enquanto o
// Pod object atual não for substituído (ex: por um rollout).
function WorkloadEventsList({ events, loading, workloadLabel }: {
  events: EventSummary[];
  loading: boolean;
  workloadLabel: string;
}) {
  return (
    <div className="mx-4 mt-3 flex-shrink-0">
      <div className="text-[10px] font-medium text-muted-foreground uppercase mb-1.5 flex items-center gap-1">
        <Bell className="w-3 h-3" />
        Eventos de aviso do workload{workloadLabel ? ` (${workloadLabel})` : ""}
      </div>
      <div className="text-[10px] text-muted-foreground mb-2">
        Pode incluir pods anteriores a este (Events sobrevivem à deleção do Pod), sujeito ao TTL de eventos do cluster.
      </div>
      {loading ? (
        <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
          <Loader2 className="w-3 h-3 animate-spin" /> Buscando eventos...
        </div>
      ) : events.length === 0 ? (
        <div className="text-xs text-muted-foreground py-1">Nenhum evento de aviso recente encontrado.</div>
      ) : (
        <div className="space-y-1 max-h-40 overflow-y-auto pr-1">
          {events.map((ev, i) => (
            <div key={`${ev.name}-${i}`} className="bg-muted/20 rounded px-2 py-1.5 text-[11px]">
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="text-[9px] h-4 px-1 text-orange-400 border-orange-400/40">
                  {ev.reason}
                </Badge>
                <span className="text-muted-foreground">{ev.age} atrás</span>
                {ev.count > 1 && <span className="text-muted-foreground">×{ev.count}</span>}
              </div>
              <div className="mt-0.5 break-words">{ev.message}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

interface LogsViewerProps {
  loading: boolean;
  logs: string;
  errorMessage?: string | null;
  emptyMessage: string;
  filteredLines: string[];
  containerNames: string[];
  selectedContainer: string;
  onContainerChange: (v: string) => void;
  tailLines: string;
  onTailLinesChange: (v: string) => void;
  showAutoRefresh?: boolean;
  autoRefresh?: boolean;
  onToggleAutoRefresh?: () => void;
  onManualRefresh: () => void;
  logLevelFilter: Set<LogLevel>;
  onToggleLevelFilter: (level: LogLevel) => void;
  logSearch: string;
  onLogSearchChange: (v: string) => void;
  onCopy: () => void;
  copied: boolean;
  onOpenJsonInspector: () => void;
  onJsonMouseUp: () => void;
  logsEndRef: React.RefObject<HTMLDivElement>;
  onClearFilters: () => void;
}

function LogsViewer({
  loading, logs, errorMessage, emptyMessage, filteredLines,
  containerNames, selectedContainer, onContainerChange, tailLines, onTailLinesChange,
  showAutoRefresh, autoRefresh, onToggleAutoRefresh, onManualRefresh,
  logLevelFilter, onToggleLevelFilter, logSearch, onLogSearchChange,
  onCopy, copied, onOpenJsonInspector, onJsonMouseUp, logsEndRef, onClearFilters,
}: LogsViewerProps) {
  return (
    <div className="flex-1 flex flex-col min-h-0">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0">
        <Select value={selectedContainer} onValueChange={onContainerChange}>
          <SelectTrigger className="h-7 text-xs w-44">
            <SelectValue placeholder="Container" />
          </SelectTrigger>
          <SelectContent>
            {containerNames.map(c => (
              <SelectItem key={c} value={c} className="text-xs">{c}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={tailLines} onValueChange={onTailLinesChange}>
          <SelectTrigger className="h-7 text-xs w-24">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {["100", "500", "1000", "5000"].map(n => (
              <SelectItem key={n} value={n} className="text-xs">{n} linhas</SelectItem>
            ))}
          </SelectContent>
        </Select>

        {showAutoRefresh && (
          <Button
            size="sm" variant={autoRefresh ? "default" : "outline"}
            className="h-7 text-xs gap-1"
            onClick={onToggleAutoRefresh}
          >
            <RefreshCw className={`w-3 h-3 ${autoRefresh ? "animate-spin" : ""}`} />
            Auto
          </Button>
        )}

        <Button
          size="sm" variant="outline" className="h-7 text-xs"
          onClick={onManualRefresh} disabled={loading}
        >
          {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
        </Button>

        <div className="flex-1" />

        <Button
          size="sm" variant="ghost" className="h-7 text-xs gap-1"
          onClick={onCopy} disabled={!logs}
        >
          {copied ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
          {copied ? "Copiado" : "Copiar"}
        </Button>
        <Button
          size="sm" variant="ghost"
          className="h-7 text-xs gap-1 text-blue-400 hover:bg-blue-400/10"
          onClick={onOpenJsonInspector}
          title="Selecione texto no log para inspecionar JSON"
        >
          <Braces className="w-3 h-3" />
          JSON
        </Button>
      </div>

      {/* Barra de filtros de nível + busca */}
      <div className="flex items-center gap-2 px-4 py-1.5 border-b border-border/50 flex-shrink-0 bg-muted/10">
        {(["error", "warn", "info", "debug"] as LogLevel[]).map(level => {
          const cfg = LOG_LEVEL_CONFIG[level];
          const active = logLevelFilter.has(level);
          return (
            <button
              key={level}
              onClick={() => onToggleLevelFilter(level)}
              className={`h-6 px-2 rounded text-[10px] font-mono font-medium border transition-colors ${active ? cfg.active : cfg.inactive}`}
            >
              {cfg.label}
            </button>
          );
        })}
        <div className="w-px h-4 bg-border/50 mx-0.5" />
        <div className="relative flex-1 max-w-56">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground pointer-events-none" />
          <input
            type="text"
            value={logSearch}
            onChange={e => onLogSearchChange(e.target.value)}
            placeholder="Buscar nos logs..."
            className="w-full h-6 pl-6 pr-6 text-[11px] bg-background border border-border rounded focus:outline-none focus:border-primary font-mono"
          />
          {logSearch && (
            <button
              onClick={() => onLogSearchChange("")}
              className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <XIcon className="w-3 h-3" />
            </button>
          )}
        </div>
        {(logLevelFilter.size > 0 || logSearch.trim()) && (
          <span className="text-[10px] text-muted-foreground ml-auto font-mono">
            {filteredLines.length} linha{filteredLines.length !== 1 ? "s" : ""}
          </span>
        )}
        {(logLevelFilter.size > 0 || logSearch.trim()) && (
          <button
            onClick={onClearFilters}
            className="text-[10px] text-muted-foreground hover:text-foreground flex items-center gap-0.5"
          >
            <XIcon className="w-3 h-3" /> Limpar
          </button>
        )}
      </div>

      {/* Área de scroll de logs */}
      <div className="flex-1 min-h-0 overflow-auto bg-black/50" onMouseUp={onJsonMouseUp}>
        <div className="p-3 font-mono text-xs leading-5">
          {loading && !logs ? (
            <div className="text-muted-foreground flex items-center gap-2">
              <Loader2 className="w-3 h-3 animate-spin" /> Carregando logs...
            </div>
          ) : errorMessage ? (
            <span className="text-muted-foreground">{errorMessage}</span>
          ) : logs ? (
            filteredLines.length > 0 ? (
              filteredLines.map((line, i) => (
                <div key={i} className={`whitespace-pre-wrap break-all ${logLineColor(line)}`}>
                  {highlightMatches(line, logSearch)}
                </div>
              ))
            ) : (
              <span className="text-muted-foreground">Nenhuma linha corresponde ao filtro.</span>
            )
          ) : (
            <span className="text-muted-foreground">{emptyMessage}</span>
          )}
          <div ref={logsEndRef} />
        </div>
      </div>
    </div>
  );
}

type PendingAction = "restart" | "kill" | "delete" | null;

interface Props {
  pod: PodSummary | null;
  cluster: string;
  metrics?: PodMetricsSingle | null;
  onClose: () => void;
  onRefresh?: () => void;
}

export function PodQuickViewModal({ pod, cluster, metrics, onClose, onRefresh }: Props) {
  const { permissions: k8sPerms } = useK8sPermissions(cluster, pod?.namespace || '');
  const canWritePods = pod?.namespace ? k8sPerms.canWritePods : undefined;

  const [activeTab, setActiveTab] = useState("details");
  const [selectedContainer, setSelectedContainer] = useState("");
  const [tailLines, setTailLines] = useState("500");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [copied, setCopied] = useState(false);

  // Logs anteriores (--previous, do container que reiniciou)
  const [previousLogs, setPreviousLogs] = useState("");
  const [previousLogsLoading, setPreviousLogsLoading] = useState(false);
  const [previousLogsError, setPreviousLogsError] = useState<string | null>(null);
  const [previousCopied, setPreviousCopied] = useState(false);

  // Eventos de Warning do workload dono (sobrevivem à deleção do Pod, ex: antes de um rollout)
  const [workloadEvents, setWorkloadEvents] = useState<EventSummary[]>([]);
  const [workloadEventsLoading, setWorkloadEventsLoading] = useState(false);

  // Busca de outras aplicações no cluster que usam a mesma imagem do container selecionado
  const [sameImagePods, setSameImagePods] = useState<PodSummary[]>([]);
  const [sameImageLoading, setSameImageLoading] = useState(false);
  const [sameImageSearched, setSameImageSearched] = useState(false);
  const sameImageCache = useRef<Map<string, PodSummary[]>>(new Map());

  // Describe state
  const [describe, setDescribe] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [showDescribe, setShowDescribe] = useState(false);
  const [lastStateCursor, setLastStateCursor] = useState(-1);
  const [eventsHighlighted, setEventsHighlighted] = useState(false);
  const describeLineRefs = useRef<(HTMLDivElement | null)[]>([]);

  // Log filter state
  const [logLevelFilter, setLogLevelFilter] = useState<Set<LogLevel>>(new Set());
  const [logSearch, setLogSearch] = useState("");

  // Action state
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const [showPortForward, setShowPortForward] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);

  const logsEndRef = useRef<HTMLDivElement>(null);
  const previousLogsEndRef = useRef<HTMLDivElement>(null);
  const jsonInspector = useJsonInspector();

  // Resize state
  const [modalSize, setModalSize] = useState({ width: 900, height: 680 });
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize(prev => ({
        width:  resizeDir.current !== "s" ? Math.max(480, prev.width  + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(400, prev.height + dy) : prev.height,
      }));
    };
    const onUp = () => {
      if (!resizing.current) return;
      resizing.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => { window.removeEventListener("mousemove", onMove); window.removeEventListener("mouseup", onUp); };
  }, []);

  const containerNames = pod?.containers?.map(c => c.name) ?? [];
  const selectedContainerRestartCount =
    pod?.containers?.find(c => c.name === selectedContainer)?.restartCount ?? 0;
  const selectedContainerLastState =
    pod?.containers?.find(c => c.name === selectedContainer)?.lastState;

  useEffect(() => {
    if (!pod) return;
    setActiveTab("details");
    // logLines não precisa de reset manual aqui — usePodLogStream já reabre/zera o buffer sozinho
    // quando pod.namespace/pod.name mudam (target do stream muda, closeStream+openStream roda de
    // novo).
    setAutoRefresh(true);
    setPendingAction(null);
    setSelectedContainer(pod.containers?.[0]?.name ?? "");
    setDescribe("");
    setShowDescribe(false);
    setLogLevelFilter(new Set());
    setLogSearch("");
    setPreviousLogs("");
    setPreviousLogsError(null);
    setWorkloadEvents([]);
    setSameImagePods([]);
    setSameImageSearched(false);
  }, [pod?.namespace, pod?.name]);

  const selectedContainerImage = pod?.containers?.find(c => c.name === selectedContainer)?.image ?? "";

  const fetchSameImagePods = useCallback(async () => {
    if (!cluster || !selectedContainerImage) return;
    const cacheKey = `${cluster}::${selectedContainerImage}`;
    const cached = sameImageCache.current.get(cacheKey);
    if (cached) {
      setSameImagePods(cached);
      setSameImageSearched(true);
      return;
    }
    setSameImageLoading(true);
    try {
      const allPods = await apiClient.getPods(cluster, undefined, undefined, true, true);
      const matches = allPods.filter(p =>
        !(p.namespace === pod?.namespace && p.name === pod?.name) &&
        p.containers?.some(c => c.image === selectedContainerImage)
      );
      sameImageCache.current.set(cacheKey, matches);
      setSameImagePods(matches);
    } catch {
      setSameImagePods([]);
    } finally {
      setSameImageLoading(false);
      setSameImageSearched(true);
    }
  }, [cluster, selectedContainerImage, pod?.namespace, pod?.name]);

  useEffect(() => {
    if (activeTab !== "same-image") return;
    fetchSameImagePods();
  }, [activeTab, selectedContainerImage, fetchSameImagePods]);

  // Streaming ao vivo (Follow=true, mesmo `kubectl logs -f` do k9s) — substitui o polling antigo
  // (getPodLogs a cada 2s, substituindo o buffer inteiro a cada resposta, causando tanto a
  // lentidão de refazer a busca completa do zero quanto o "log some por um instante" quando uma
  // resposta vinha vazia/diferente). Ver hooks/usePodLogStream.ts — mesma infra SSE já usada pelo
  // AllPodsLogsModal (vários pods), aqui com um único pod. "Auto" controla a conexão em si (mesmo
  // padrão do AllPodsLogsModal): desligado = stream fechado, buffer já recebido continua visível.
  const logsStreamTarget = useMemo<PodLogStreamTarget | null>(() => {
    if (!pod || !cluster) return null;
    return { cluster, namespace: pod.namespace, name: pod.name, container: selectedContainer || pod.containers?.[0]?.name };
  }, [pod, cluster, selectedContainer]);

  const { lines: logLines, loading: logsLoading, refetch: fetchLogs } = usePodLogStream(
    logsStreamTarget,
    parseInt(tailLines),
    activeTab === "logs" && autoRefresh
  );

  const logs = useMemo(() => logLines.join("\n"), [logLines]);

  useEffect(() => {
    if (activeTab !== "logs" || logLines.length === 0) return;
    setTimeout(() => logsEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);
  }, [activeTab, logLines]);

  const filteredLines = useMemo(
    () => filterLogLines(logs, logLevelFilter, logSearch),
    [logs, logLevelFilter, logSearch]
  );

  const toggleLevelFilter = (level: LogLevel) => {
    setLogLevelFilter(prev => {
      const next = new Set(prev);
      if (next.has(level)) next.delete(level);
      else next.add(level);
      return next;
    });
  };

  const clearLogFilters = () => {
    setLogLevelFilter(new Set());
    setLogSearch("");
  };

  const copyLogs = () => {
    const content = (logLevelFilter.size > 0 || logSearch.trim())
      ? filteredLines.join("\n")
      : logs;
    navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Logs da execução anterior do container (kubectl logs --previous)
  const fetchPreviousLogs = useCallback(async () => {
    if (!pod || !cluster) return;
    setPreviousLogsLoading(true);
    setPreviousLogsError(null);
    try {
      const res = await apiClient.getPodLogs(
        cluster, pod.namespace, pod.name,
        selectedContainer || pod.containers?.[0]?.name,
        parseInt(tailLines),
        true
      );
      setPreviousLogs(res.logs ?? "");
      setTimeout(() => previousLogsEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);
    } catch (err: unknown) {
      setPreviousLogs("");
      const code = (err as { code?: string })?.code;
      setPreviousLogsError(
        code === "PREVIOUS_LOGS_NOT_FOUND"
          ? "Não há logs de uma execução anterior — este container ainda não reiniciou ou os logs já foram descartados."
          : "Erro ao carregar logs anteriores."
      );
    } finally {
      setPreviousLogsLoading(false);
    }
  }, [pod, cluster, selectedContainer, tailLines]);

  useEffect(() => {
    if (activeTab !== "previous-logs") return;
    if (selectedContainerRestartCount === 0) {
      // Este Pod object específico nunca reiniciou — não há log bruto de "--previous" pra buscar
      // (isso é o comportamento normal logo após um rollout criar um Pod novo). O banner de Last
      // State e a lista de eventos do workload abaixo continuam tentando mostrar algo útil.
      setPreviousLogs("");
      setPreviousLogsError("Este container ainda não reiniciou neste Pod — não há log bruto de uma execução anterior. Veja o resumo e os eventos abaixo.");
      return;
    }
    fetchPreviousLogs();
  }, [activeTab, selectedContainer, tailLines, selectedContainerRestartCount, fetchPreviousLogs]);

  // Eventos de Warning do workload dono — sobrevivem à deleção do Pod (até expirarem pelo TTL do
  // cluster), então cobrem também pods de ANTES de um rollout que este Pod object não alcança.
  const workloadSearchTerm = (pod?.ownerWorkload?.split("/")[1] || pod?.name || "").trim();
  // Aba "Comportamento" só faz sentido pra pods donos de um Deployment — o gráfico busca
  // histórico via kube_deployment_* (Prometheus) / CLOUD_APPLICATION (Dynatrace), nenhum dos
  // dois existe pra DaemonSet/StatefulSet/Job.
  const isDeploymentOwned = (pod?.ownerWorkload ?? "").startsWith("Deployment/");
  useEffect(() => {
    if (activeTab !== "previous-logs" || !cluster || !pod || !workloadSearchTerm) return;
    let cancelled = false;
    setWorkloadEventsLoading(true);
    apiClient.getEvents(cluster, [pod.namespace], workloadSearchTerm, "Warning", true)
      .then(events => { if (!cancelled) setWorkloadEvents(events); })
      .catch(() => { if (!cancelled) setWorkloadEvents([]); })
      .finally(() => { if (!cancelled) setWorkloadEventsLoading(false); });
    return () => { cancelled = true; };
  }, [activeTab, cluster, pod?.namespace, workloadSearchTerm]);

  const filteredPreviousLines = useMemo(
    () => filterLogLines(previousLogs, logLevelFilter, logSearch),
    [previousLogs, logLevelFilter, logSearch]
  );

  const copyPreviousLogs = () => {
    const content = (logLevelFilter.size > 0 || logSearch.trim())
      ? filteredPreviousLines.join("\n")
      : previousLogs;
    navigator.clipboard.writeText(content);
    setPreviousCopied(true);
    setTimeout(() => setPreviousCopied(false), 2000);
  };

  const fetchDescribe = useCallback(async () => {
    if (!pod || !cluster) return;
    setDescribeLoading(true);
    setLastStateCursor(-1);
    setEventsHighlighted(false);
    try {
      const res = await apiClient.describePod(cluster, pod.namespace, pod.name);
      setDescribe(res.describe || "");
    } catch {
      setDescribe("Erro ao carregar describe.");
    } finally {
      setDescribeLoading(false);
    }
  }, [pod, cluster]);

  const handleDescribeClick = () => {
    if (!showDescribe) {
      fetchDescribe();
    }
    setShowDescribe(!showDescribe);
  };

  const describeLines = useMemo(() => describe.split("\n"), [describe]);
  const lastStateBlocks = useMemo(() => findLastStateBlocks(describeLines), [describeLines]);
  const eventsBlock = useMemo(() => findEventsBlock(describeLines), [describeLines]);

  const jumpToLastState = () => {
    if (lastStateBlocks.length === 0) return;
    setEventsHighlighted(false);
    const nextCursor = (lastStateCursor + 1) % lastStateBlocks.length;
    setLastStateCursor(nextCursor);
    const target = lastStateBlocks[nextCursor];
    describeLineRefs.current[target.start]?.scrollIntoView({ behavior: "smooth", block: "center" });
  };

  const jumpToEvents = () => {
    if (!eventsBlock) return;
    setLastStateCursor(-1);
    setEventsHighlighted(true);
    describeLineRefs.current[eventsBlock.start]?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  const executeAction = async () => {
    if (!pod || !pendingAction) return;
    setActionLoading(true);
    try {
      if (pendingAction === "restart") {
        const res = await apiClient.restartPod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod reiniciado com sucesso");
        onRefresh?.();
        setPendingAction(null);
      } else if (pendingAction === "kill") {
        const res = await apiClient.killPod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod finalizado com sucesso");
        onRefresh?.();
        setPendingAction(null);
      } else if (pendingAction === "delete") {
        const res = await apiClient.deletePod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod deletado com sucesso");
        onClose();
        onRefresh?.();
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Erro ao executar ação";
      toast.error(msg);
      setPendingAction(null);
    } finally {
      setActionLoading(false);
    }
  };

  if (!pod) return null;

  const isHealthy = pod.phase === "Running" && pod.readyContainers === pod.totalContainers;
  const statusVariant = isHealthy ? "default" : (pod.phase === "Pending" ? "secondary" : "destructive");

  // % relativo ao LIMIT (não ao request) — mesma base de cálculo da coluna CPU/MEM em
  // PodMonitorTable.tsx (`m.cpuMillicores / limitM * 100`), pra não divergir do que a lista já
  // mostra. Request costuma ser bem menor que limit, então usar %request aqui inflava o gauge
  // (ex: lista mostrando 68% do limite mas o gauge batendo 100% por já ter estourado o request).
  const cpuPct = metrics && metrics.cpuPercentLimit >= 0 ? metrics.cpuPercentLimit : 0;
  const memPct = metrics && metrics.memPercentLimit >= 0 ? metrics.memPercentLimit : 0;

  const actionConfig = {
    restart: {
      label: "Reiniciar Pod",
      desc: "O pod será reiniciado (deletado e recriado pelo controller).",
      confirmLabel: "Reiniciar",
      color: "bg-blue-600 hover:bg-blue-700 text-white",
    },
    kill: {
      label: "Kill Pod (Forçar)",
      desc: "O pod será encerrado imediatamente com SIGKILL.",
      confirmLabel: "Forçar Kill",
      color: "bg-orange-600 hover:bg-orange-700 text-white",
    },
    delete: {
      label: "Deletar Pod",
      desc: "O pod será deletado permanentemente do cluster.",
      confirmLabel: "Deletar",
      color: "bg-destructive hover:bg-destructive/90 text-white",
    },
  } as const;

  return (
    <Dialog open={!!pod} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent
        className="flex flex-col p-0 gap-0 overflow-hidden"
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader className="px-4 pt-4 pb-3 border-b border-border flex-shrink-0">
          <DialogTitle className="font-mono text-sm leading-snug">
            <span className="text-muted-foreground text-xs">{pod.namespace}/</span>
            <span className="text-foreground">{pod.name}</span>
          </DialogTitle>
          <div className="flex items-center justify-between mt-1">
            <div className="flex items-center gap-2 flex-wrap min-w-0">
              {/* pod.status é sempre curto (reason/phase, ex: "CrashLoopBackOff") — pod.statusReason
                  pode ser uma mensagem longa de evento (ex: "back-off 5m0s restarting failed
                  container=..."), por isso vai só no title/tooltip, nunca dentro do badge. */}
              <Badge
                variant={statusVariant}
                className="text-[10px] h-4 px-1.5 max-w-[320px] truncate"
                title={pod.statusReason || undefined}
              >
                {pod.status || pod.phase}
              </Badge>
            </div>
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs gap-1"
              onClick={handleDescribeClick}
              disabled={describeLoading}
            >
              {describeLoading ? (
                <Loader2 className="w-3 h-3 animate-spin" />
              ) : (
                <FileText className="w-3 h-3" />
              )}
              Describe
            </Button>
          </div>
        </DialogHeader>

        <div className="flex-1 flex flex-col min-h-0">
          {/* Tab bar manual */}
          <div className="flex items-center justify-between border-b border-border px-4 pt-3 gap-2 flex-shrink-0">
            <div className="flex gap-1">
              {(["details", "logs", "previous-logs", "same-image", "behavior"] as const).map(tab => {
                const disabled = tab === "behavior" && !isDeploymentOwned;
                return (
                  <button
                    key={tab}
                    onClick={() => { if (!disabled) setActiveTab(tab); }}
                    disabled={disabled}
                    title={disabled ? "Disponível apenas para pods de um Deployment" : undefined}
                    className={`px-3 py-1.5 text-xs font-medium transition-colors border-b-2 -mb-px ${
                      disabled
                        ? "border-transparent text-muted-foreground/40 cursor-not-allowed"
                        : activeTab === tab
                          ? "border-primary text-foreground"
                          : "border-transparent text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    {tab === "details" ? "Detalhes" : tab === "logs" ? "Logs" : tab === "previous-logs" ? "Logs Anteriores" : tab === "same-image" ? "Mesma Imagem" : "Comportamento"}
                  </button>
                );
              })}
            </div>

            {activeTab === "same-image" && (
              <div className="flex items-center gap-2 pb-1.5">
                <Select value={selectedContainer} onValueChange={setSelectedContainer}>
                  <SelectTrigger className="h-7 text-xs w-40 flex-shrink-0">
                    <SelectValue placeholder="Container" />
                  </SelectTrigger>
                  <SelectContent>
                    {containerNames.map(c => (
                      <SelectItem key={c} value={c} className="text-xs">{c}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  size="sm"
                  variant="outline"
                  className="h-7 text-xs gap-1 flex-shrink-0"
                  onClick={fetchSameImagePods}
                  disabled={sameImageLoading || !selectedContainerImage}
                >
                  {sameImageLoading ? <Loader2 className="w-3 h-3 animate-spin" /> : <Search className="w-3 h-3" />}
                  Atualizar
                </Button>
              </div>
            )}

            {activeTab === "details" && (
              <div className="flex items-center gap-2 pb-1.5 flex-wrap justify-end">
                <Button
                  size="sm" variant="outline"
                  className="h-7 text-xs gap-1"
                  onClick={() => setShowPortForward(true)}
                  title="Abrir um túnel de port-forward pra este pod"
                >
                  <Network className="w-3 h-3" /> Port Forward
                </Button>
                <ProtectedAction showWarning={false} allowed={canWritePods}>
                  <Button
                    size="sm" variant="outline"
                    className="h-7 text-xs text-blue-400 border-blue-400/30 hover:bg-blue-400/10"
                    onClick={() => setPendingAction(pendingAction === "restart" ? null : "restart")}
                  >
                    Rollout Restart
                  </Button>
                </ProtectedAction>
                <ProtectedAction showWarning={false} allowed={canWritePods}>
                  <Button
                    size="sm" variant="outline"
                    className="h-7 text-xs text-orange-400 border-orange-400/30 hover:bg-orange-400/10"
                    onClick={() => setPendingAction(pendingAction === "kill" ? null : "kill")}
                  >
                    Kill (Forçar)
                  </Button>
                </ProtectedAction>
                <ProtectedAction showWarning={false} allowed={canWritePods}>
                  <Button
                    size="sm" variant="outline"
                    className="h-7 text-xs text-destructive border-destructive/30 hover:bg-destructive/10"
                    onClick={() => setPendingAction(pendingAction === "delete" ? null : "delete")}
                  >
                    Deletar Pod
                  </Button>
                </ProtectedAction>
              </div>
            )}
          </div>

          {/* Barra de confirmação inline (fixa, fora da área que rola) */}
          {activeTab === "details" && pendingAction && (
            <div className="flex items-center gap-2 px-4 py-2 border-b border-border bg-muted/30 text-xs flex-shrink-0">
              <span className="flex-1 text-muted-foreground">
                <strong className="text-foreground">{actionConfig[pendingAction].label}</strong>
                {" — "}{actionConfig[pendingAction].desc}
              </span>
              <Button
                size="sm"
                className={`h-6 text-xs ${actionConfig[pendingAction].color}`}
                onClick={executeAction}
                disabled={actionLoading}
              >
                {actionLoading
                  ? <Loader2 className="w-3 h-3 animate-spin mr-1" />
                  : null}
                {actionConfig[pendingAction].confirmLabel}
              </Button>
              <Button
                size="sm" variant="ghost"
                className="h-6 text-xs"
                onClick={() => setPendingAction(null)}
                disabled={actionLoading}
              >
                Cancelar
              </Button>
            </div>
          )}

          {/* ── DETALHES ── */}
          {activeTab === "details" && (
            <div className="flex-1 min-h-0 overflow-y-auto">
            <div className="p-4 space-y-4">

              {/* Gauge + Info lado a lado */}
              <div className="flex gap-5 items-start">
                {metrics ? (
                  <DualGauge
                    cpuPct={cpuPct}
                    memPct={memPct}
                    cpuVal={formatMillicores(metrics.cpuMillicores)}
                    memVal={formatBytes(metrics.memoryBytes)}
                  />
                ) : (
                  <div className="w-[104px] h-[104px] flex items-center justify-center flex-shrink-0">
                    <span className="text-[10px] text-muted-foreground text-center leading-tight">
                      Métricas<br/>indisponíveis
                    </span>
                  </div>
                )}

                {/* Info grid ao lado do gauge */}
                <div className="flex-1 grid grid-cols-2 gap-x-6 gap-y-1.5 text-xs">
                  {[
                    { label: "Namespace",   value: pod.namespace },
                    { label: "Restarts",    value: String(pod.restarts) },
                    { label: "Ready",       value: `${pod.readyContainers}/${pod.totalContainers}` },
                    { label: "IP",          value: pod.podIP || "-" },
                    { label: "CPU Request", value: pod.cpuRequest || "-" },
                    { label: "CPU Limit",   value: pod.cpuLimit || "-" },
                    { label: "Mem Request", value: pod.memoryRequest || "-" },
                    { label: "Mem Limit",   value: pod.memoryLimit || "-" },
                    { label: "Age",         value: pod.createdAt ? formatAge(pod.createdAt) : "-" },
                  ].map(({ label, value }) => (
                    <div key={label} className="flex gap-2">
                      <span className="text-muted-foreground w-24 flex-shrink-0">{label}:</span>
                      <span className="font-mono font-medium">{value}</span>
                    </div>
                  ))}
                  {/* Node — ocupa coluna inteira para não truncar */}
                  <div className="flex gap-2 col-span-2">
                    <span className="text-muted-foreground w-24 flex-shrink-0">Node:</span>
                    <span className="font-mono font-medium break-all">{pod.nodeName || "-"}</span>
                  </div>
                  {/* Percentuais vs limits (só com métricas) */}
                  {metrics && (
                    <>
                      <div className="flex gap-2 col-span-2 mt-0.5 pt-1.5 border-t border-border/40">
                        <span className="text-muted-foreground w-24 flex-shrink-0">CPU vs Req:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.cpuPercentRequest)}</span>
                        <span className="text-muted-foreground ml-3 w-20">vs Limit:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.cpuPercentLimit)}</span>
                      </div>
                      <div className="flex gap-2 col-span-2">
                        <span className="text-muted-foreground w-24 flex-shrink-0">MEM vs Req:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.memPercentRequest)}</span>
                        <span className="text-muted-foreground ml-3 w-20">vs Limit:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.memPercentLimit)}</span>
                      </div>
                    </>
                  )}
                </div>
              </div>

              {/* Containers */}
              <div>
                <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">Containers</div>
                <div className="space-y-1.5">
                  {pod.containers.map(c => (
                    <div key={c.name} className="flex items-start gap-2 bg-muted/20 rounded px-3 py-2 text-xs font-mono">
                      <span className={`w-2 h-2 rounded-full mt-0.5 flex-shrink-0 ${c.ready ? "bg-green-500" : "bg-orange-500"}`} />
                      <div className="min-w-0">
                        <div className="font-medium text-foreground">{c.name}</div>
                        <div className="text-muted-foreground text-[10px] truncate">{c.image}</div>
                        <div className="text-[10px] mt-0.5 flex gap-2">
                          <span className={c.ready ? "text-green-400" : "text-orange-400"}>
                            {c.state}{c.stateReason ? ` (${c.stateReason})` : ""}
                          </span>
                          {c.restartCount > 0 && (
                            <span className="text-orange-400">{c.restartCount} restarts</span>
                          )}
                        </div>
                        {c.lastState && (
                          <div className="text-[10px] mt-0.5 text-amber-400/90">
                            Última finalização: Exit {c.lastState.exitCode} ({describeExitCode(c.lastState.exitCode).label})
                            {c.lastState.reason ? ` • ${c.lastState.reason}` : ""}
                            {c.lastState.finishedAt ? ` • ${formatAge(c.lastState.finishedAt)} atrás` : ""}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              {/* Labels */}
              {pod.labels && Object.keys(pod.labels).length > 0 && (
                <div>
                  <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">Labels</div>
                  <div className="flex flex-wrap gap-1">
                    {Object.entries(pod.labels).map(([k, v]) => (
                      <Badge key={k} variant="secondary" className="text-[9px] font-mono px-1.5 h-4">
                        {k}={v}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}

            </div>
            </div>
          )}

          {/* ── LOGS ── */}
          {activeTab === "logs" && (
            <LogsViewer
              loading={logsLoading}
              logs={logs}
              emptyMessage="Nenhum log disponível."
              filteredLines={filteredLines}
              containerNames={containerNames}
              selectedContainer={selectedContainer}
              onContainerChange={setSelectedContainer}
              tailLines={tailLines}
              onTailLinesChange={setTailLines}
              showAutoRefresh
              autoRefresh={autoRefresh}
              onToggleAutoRefresh={() => setAutoRefresh(v => !v)}
              onManualRefresh={fetchLogs}
              logLevelFilter={logLevelFilter}
              onToggleLevelFilter={toggleLevelFilter}
              logSearch={logSearch}
              onLogSearchChange={setLogSearch}
              onCopy={copyLogs}
              copied={copied}
              onOpenJsonInspector={() => jsonInspector.setOpen(true)}
              onJsonMouseUp={jsonInspector.handleMouseUp}
              logsEndRef={logsEndRef}
              onClearFilters={clearLogFilters}
            />
          )}

          {/* ── LOGS ANTERIORES (--previous) ── */}
          {activeTab === "previous-logs" && (
            <div className="flex-1 flex flex-col min-h-0">
              <LastStateBanner lastState={selectedContainerLastState} />
              {selectedContainerRestartCount === 0 && (
                <WorkloadEventsList
                  events={workloadEvents}
                  loading={workloadEventsLoading}
                  workloadLabel={pod.ownerWorkload ?? ""}
                />
              )}
              <LogsViewer
                loading={previousLogsLoading}
                logs={previousLogs}
                errorMessage={previousLogsError}
                emptyMessage="Nenhum log anterior disponível."
                filteredLines={filteredPreviousLines}
                containerNames={containerNames}
                selectedContainer={selectedContainer}
                onContainerChange={setSelectedContainer}
                tailLines={tailLines}
                onTailLinesChange={setTailLines}
                onManualRefresh={fetchPreviousLogs}
                logLevelFilter={logLevelFilter}
                onToggleLevelFilter={toggleLevelFilter}
                logSearch={logSearch}
                onLogSearchChange={setLogSearch}
                onCopy={copyPreviousLogs}
                copied={previousCopied}
                onOpenJsonInspector={() => jsonInspector.setOpen(true)}
                onJsonMouseUp={jsonInspector.handleMouseUp}
                logsEndRef={previousLogsEndRef}
                onClearFilters={clearLogFilters}
              />
            </div>
          )}

          {/* ── MESMA IMAGEM (outras aplicações do cluster usando o mesmo container) ── */}
          {activeTab === "same-image" && (
            <div className="flex-1 flex flex-col min-h-0">
              <div className="px-4 py-2 border-b border-border flex-shrink-0">
                <span className="text-[10px] text-muted-foreground font-mono truncate block" title={selectedContainerImage}>
                  {selectedContainerImage || "—"}
                </span>
              </div>
              <div className="flex-1 min-h-0 overflow-y-auto p-4">
                {sameImageLoading ? (
                  <div className="flex items-center justify-center h-32 text-muted-foreground text-xs gap-2">
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Buscando em todos os namespaces do cluster...
                  </div>
                ) : !sameImageSearched ? (
                  <div className="flex items-center justify-center h-32 text-muted-foreground text-xs text-center px-8">
                    Mostra outros pods deste cluster (em qualquer namespace) que usam a mesma imagem do container selecionado.
                  </div>
                ) : sameImagePods.length === 0 ? (
                  <div className="flex items-center justify-center h-32 text-muted-foreground text-xs">
                    Nenhum outro pod neste cluster usa esta imagem.
                  </div>
                ) : (
                  <>
                    <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">
                      {sameImagePods.length} pod(s) em {new Set(sameImagePods.map(p => p.namespace)).size} namespace(s)
                    </div>
                    <div className="space-y-1.5">
                      {sameImagePods
                        .slice()
                        .sort((a, b) => (a.namespace + a.name).localeCompare(b.namespace + b.name))
                        .map(p => {
                          const matchedContainer = p.containers?.find(c => c.image === selectedContainerImage);
                          return (
                            <div key={`${p.namespace}/${p.name}`} className="flex items-start gap-2 bg-muted/20 rounded px-3 py-2 text-xs font-mono">
                              <span className={`w-2 h-2 rounded-full mt-0.5 flex-shrink-0 ${matchedContainer?.ready ? "bg-green-500" : "bg-orange-500"}`} />
                              <div className="min-w-0 flex-1">
                                <div className="text-muted-foreground text-[10px]">{p.namespace}</div>
                                <div className="font-medium text-foreground truncate">{p.name}</div>
                                {matchedContainer && (
                                  <div className="text-[10px] text-muted-foreground">container: {matchedContainer.name}</div>
                                )}
                              </div>
                            </div>
                          );
                        })}
                    </div>
                  </>
                )}
              </div>
            </div>
          )}

          {activeTab === "behavior" && isDeploymentOwned && cluster && pod && (
            <div className="flex-1 min-h-0 overflow-y-auto">
              <DeploymentBehaviorChart cluster={cluster} namespace={pod.namespace} deployment={workloadSearchTerm} podName={pod.name} />
            </div>
          )}
        </div>

        {jsonInspector.floatingPos && (
          <JsonFloatingButton pos={jsonInspector.floatingPos} onClick={jsonInspector.openInspector} />
        )}
        <JsonInspectorModal
          open={jsonInspector.open}
          onClose={() => jsonInspector.setOpen(false)}
          initialText={jsonInspector.text}
        />

        {/* Port Forward — instância própria pré-preenchida com este pod (independente da
            instância global de Index.tsx, que abre sem pré-preenchimento) */}
        <PortForwardModal
          open={showPortForward}
          onOpenChange={setShowPortForward}
          initialCluster={cluster}
          initialNamespace={pod.namespace}
          initialPod={pod.name}
          initialWorkload={pod.ownerWorkload || workloadSearchTerm}
        />

        {/* Handles de resize */}
        {/* Borda direita */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "e";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "e-resize";
            document.body.style.userSelect = "none";
          }}
        />
        {/* Borda inferior */}
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "s";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "s-resize";
            document.body.style.userSelect = "none";
          }}
        />
        {/* Canto inferior direito (se-resize) */}
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "se";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "se-resize";
            document.body.style.userSelect = "none";
          }}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round"/>
            <path d="M9 5 L9 9 L5 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round"/>
          </svg>
        </div>
      </DialogContent>

      {/* Modal Describe */}
      <Dialog open={showDescribe} onOpenChange={setShowDescribe}>
        <DialogContent className="max-w-4xl w-[90vw] h-[85vh] flex flex-col overflow-hidden">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center gap-2 text-sm">
              <FileText className="w-4 h-4" />
              kubectl describe pod {pod.name}
              <Badge variant="secondary" className="text-xs font-mono">
                {pod.namespace}
              </Badge>
            </DialogTitle>
          </DialogHeader>

          {pod.containers.some(c => c.lastState) && (
            <div className="border rounded overflow-hidden flex-shrink-0 max-h-[180px] overflow-y-auto">
              <div className="px-3 py-1.5 text-[10px] font-medium text-muted-foreground uppercase bg-muted/40 border-b border-border sticky top-0">
                Códigos de saída encontrados
              </div>
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-[10px] text-muted-foreground text-left">
                    <th className="px-3 py-1.5 font-medium">Container</th>
                    <th className="px-3 py-1.5 font-medium">Exit Code</th>
                    <th className="px-3 py-1.5 font-medium">Significado</th>
                    <th className="px-3 py-1.5 font-medium">Reason</th>
                    <th className="px-3 py-1.5 font-medium">Quando</th>
                  </tr>
                </thead>
                <tbody>
                  {pod.containers.filter(c => c.lastState).map(c => {
                    const info = describeExitCode(c.lastState!.exitCode);
                    const colorClass =
                      info.severity === "critical" ? "text-red-400"
                      : info.severity === "warning" ? "text-orange-400"
                      : "text-muted-foreground";
                    return (
                      <tr key={c.name} className="border-t border-border/50">
                        <td className="px-3 py-1.5 font-mono">{c.name}</td>
                        <td className={`px-3 py-1.5 font-mono font-medium ${colorClass}`}>{c.lastState!.exitCode}</td>
                        <td className="px-3 py-1.5">{info.label}</td>
                        <td className="px-3 py-1.5 font-mono">{c.lastState!.reason || "-"}</td>
                        <td className="px-3 py-1.5 text-muted-foreground">
                          {c.lastState!.finishedAt ? `${formatAge(c.lastState!.finishedAt)} atrás` : "-"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          <ScrollArea className="flex-1 min-h-0 w-full border rounded">
            <div className="p-4">
              {describeLoading ? (
                <div className="flex items-center justify-center py-8">
                  <Loader2 className="w-6 h-6 animate-spin mr-2" />
                  <span className="text-sm text-muted-foreground">Carregando describe...</span>
                </div>
              ) : describe ? (
                <div className="text-xs font-mono whitespace-pre-wrap leading-relaxed">
                  {describeLines.map((line, i) => {
                    const activeBlock = lastStateCursor >= 0 ? lastStateBlocks[lastStateCursor] : null;
                    const inLastState = !!activeBlock && i >= activeBlock.start && i <= activeBlock.end;
                    const inEvents = eventsHighlighted && !!eventsBlock && i >= eventsBlock.start && i <= eventsBlock.end;
                    const highlightClass = inLastState
                      ? "bg-yellow-500/20 -mx-1 px-1 rounded"
                      : inEvents
                      ? "bg-blue-500/20 -mx-1 px-1 rounded"
                      : undefined;
                    return (
                      <div
                        key={i}
                        ref={(el) => (describeLineRefs.current[i] = el)}
                        className={highlightClass}
                      >
                        {line || " "}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="text-center py-8 text-muted-foreground text-sm">
                  Nenhuma informação de describe disponível.
                </div>
              )}
            </div>
          </ScrollArea>

          <div className="flex justify-between items-center pt-2 flex-shrink-0">
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={fetchDescribe}
                disabled={describeLoading}
                className="text-xs"
              >
                {describeLoading ? (
                  <Loader2 className="w-3 h-3 animate-spin mr-1" />
                ) : (
                  <RefreshCw className="w-3 h-3 mr-1" />
                )}
                Atualizar
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={jumpToLastState}
                disabled={lastStateBlocks.length === 0}
                title={lastStateBlocks.length === 0 ? "Nenhum reinício registrado neste pod" : "Ir para a causa do reinício anterior"}
                className="text-xs gap-1 text-orange-400 border-orange-400/30 hover:bg-orange-400/10 disabled:text-muted-foreground disabled:border-border"
              >
                <AlertTriangle className="w-3 h-3" />
                Causa do reinício
                {lastStateBlocks.length > 1 && (
                  <Badge variant="secondary" className="text-[10px] h-4 px-1 ml-0.5">
                    {(lastStateCursor < 0 ? 0 : lastStateCursor + 1)}/{lastStateBlocks.length}
                  </Badge>
                )}
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={jumpToEvents}
                disabled={!eventsBlock}
                title={!eventsBlock ? "Nenhum evento registrado neste pod" : "Ir para a seção de Eventos"}
                className="text-xs gap-1 text-blue-400 border-blue-400/30 hover:bg-blue-400/10 disabled:text-muted-foreground disabled:border-border"
              >
                <Bell className="w-3 h-3" />
                Eventos
              </Button>
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(describe);
                toast.success("Describe copiado para a área de transferência!");
              }}
              disabled={!describe}
              className="text-xs"
            >
              <Copy className="w-3 h-3 mr-1" />
              Copiar
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </Dialog>
  );
}
