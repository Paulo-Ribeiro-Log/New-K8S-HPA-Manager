import { useEffect, useRef, useState, useMemo } from "react";
import type { PodSummary, BatchPodMetrics } from "@/lib/api/types";
import { formatAge, formatBytes, formatMillicores, parseCpuToMillicores, parseMemoryToBytes, podRowColor, podDotColor } from "@/lib/monitorUtils";
import { Loader2, ChevronLeft, ChevronRight, Search, X, ListFilter, Check, RefreshCw, ChevronUp, ChevronDown, ArrowUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac/ProtectedAction";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 5000;

export interface BreadcrumbSegment {
  label: string;
  onClick?: () => void;
}

interface PodMonitorTableProps {
  cluster: string;
  pods: PodSummary[];
  loading: boolean;
  metrics: BatchPodMetrics | null;
  metricsLoading: boolean;
  onOpenDetail: (pod: PodSummary) => void;
  headerLabel: string;
  breadcrumb?: BreadcrumbSegment[];
  onRequestRefresh: () => void;
  onBack?: () => void;
  backLabel?: string;
}

function ColumnFilter({
  label,
  options,
  selected,
  onChange,
}: {
  label: string;
  options: string[];
  selected: Set<string>;
  onChange: (v: Set<string>) => void;
}) {
  const active = selected.size > 0;
  const toggle = (val: string) => {
    const next = new Set(selected);
    if (next.has(val)) next.delete(val);
    else next.add(val);
    onChange(next);
  };
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className={`flex items-center gap-0.5 uppercase hover:text-foreground transition-colors ${active ? "text-primary" : "text-muted-foreground"}`}
          title={`Filtrar por ${label}`}
        >
          {label}
          <ListFilter className="w-2.5 h-2.5 ml-0.5" />
          {active && (
            <span className="ml-0.5 bg-primary text-primary-foreground rounded-full text-[9px] w-3.5 h-3.5 flex items-center justify-center font-bold">
              {selected.size}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-max min-w-[160px] max-w-[520px] p-2" align="start">
        <div className="flex items-center justify-between gap-4 mb-2">
          <span className="text-xs font-medium whitespace-nowrap">{label}</span>
          {active && <button onClick={() => onChange(new Set())} className="text-xs text-muted-foreground hover:text-foreground whitespace-nowrap">Limpar</button>}
        </div>
        <ScrollArea className="max-h-72">
          <div className="space-y-1">
            {options.map((opt) => (
              <label key={opt} className="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-muted/50 text-xs">
                <Checkbox checked={selected.has(opt)} onCheckedChange={() => toggle(opt)} className="w-3.5 h-3.5 rounded-full flex-shrink-0" />
                <span className="whitespace-nowrap" title={opt}>{opt}</span>
                {selected.has(opt) && <Check className="w-3 h-3 text-primary flex-shrink-0" />}
              </label>
            ))}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  );
}

function useSecondsTick(date: Date | null): string {
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);
  if (!date) return "";
  const secs = Math.floor((Date.now() - date.getTime()) / 1000);
  if (secs < 5) return "agora";
  if (secs < 60) return `${secs}s atrás`;
  return `${Math.floor(secs / 60)}m atrás`;
}

// Colunas: SEL | NAME/NS | VERSION | dot | READY | STATUS | REST. | CPU | MEM | NODE | AGE
// SEL(32) e dot(22) são fixos e não têm ResizeHandle por serem muito pequenos.
const INITIAL_WIDTHS = [32, 400, 120, 22, 60, 140, 50, 110, 110, 180, 60];

function extractImageVersion(image?: string): string {
  if (!image) return "-";
  const tag = image.split(":").pop();
  if (!tag || tag === image) return "-";
  // Converter x-x-x-x → x.x.x-x (tags semver com hífens no lugar de pontos)
  const parts = tag.split("-");
  if (parts.length === 4 && parts.every((p) => /^\d+$/.test(p))) {
    return `${parts[0]}.${parts[1]}.${parts[2]}-${parts[3]}`;
  }
  if (parts.length >= 3 && parts.slice(0, 3).every((p) => /^\d+$/.test(p))) {
    const semver = `${parts[0]}.${parts[1]}.${parts[2]}`;
    return parts.length > 3 ? `${semver}-${parts.slice(3).join("-")}` : semver;
  }
  return tag;
}

type PodSortKey = "name" | "ready" | "restarts" | "cpu" | "mem" | "age" | "node";
type StatusSortMode = null | "running" | "error" | "completed";

const STATUS_CYCLE: StatusSortMode[] = [null, "running", "error", "completed"];
const STATUS_PRIORITY: Record<NonNullable<StatusSortMode>, (status: string) => number> = {
  running:   (s) => (/running/i.test(s) ? 0 : 1),
  error:     (s) => (/error|failed|crash|oom|evict/i.test(s) ? 0 : 1),
  completed: (s) => (/completed|succeed/i.test(s) ? 0 : 1),
};

function SortIcon({ colKey, sortKey, sortDir, onSort }: {
  colKey: PodSortKey; sortKey: PodSortKey | null; sortDir: "asc" | "desc"; onSort: (k: PodSortKey) => void;
}) {
  const active = sortKey === colKey;
  return (
    <button
      onClick={(e) => { e.stopPropagation(); onSort(colKey); }}
      className={`flex items-center ml-0.5 transition-colors ${active ? "text-primary" : "text-muted-foreground/30 hover:text-muted-foreground"}`}
      title={active ? (sortDir === "asc" ? "Crescente — clique para decrescente" : "Decrescente — clique para remover") : "Ordenar"}
    >
      {active && sortDir === "asc" ? <ChevronUp className="w-2.5 h-2.5" /> : active && sortDir === "desc" ? <ChevronDown className="w-2.5 h-2.5" /> : <ArrowUpDown className="w-2.5 h-2.5" />}
    </button>
  );
}

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: PodSortKey; sortKey: PodSortKey | null; sortDir: "asc" | "desc"; onSort: (k: PodSortKey) => void;
}) {
  const active = sortKey === colKey;
  return (
    <button
      onClick={() => onSort(colKey)}
      className={`flex items-center gap-0.5 uppercase transition-colors ${active ? "text-primary hover:text-primary/80" : "text-muted-foreground hover:text-foreground"}`}
      title={active ? (sortDir === "asc" ? "Crescente — clique para decrescente" : "Decrescente — clique para remover") : "Ordenar por " + label}
    >
      {label}
      {active && sortDir === "asc" ? <ChevronUp className="w-2.5 h-2.5" /> : active && sortDir === "desc" ? <ChevronDown className="w-2.5 h-2.5" /> : <ArrowUpDown className="w-2.5 h-2.5 opacity-30" />}
    </button>
  );
}

// Ícone de sort cíclico para STATUS (apenas ícone, sem label duplicado)
function StatusSortIcon({ mode, onCycle }: { mode: StatusSortMode; onCycle: () => void }) {
  const modeLabel: Record<NonNullable<StatusSortMode>, string> = { running: "Running primeiro", error: "Error primeiro", completed: "Completed primeiro" };
  const modeColor: Record<NonNullable<StatusSortMode>, string> = { running: "text-green-500", error: "text-red-500", completed: "text-blue-400" };
  return (
    <button
      onClick={(e) => { e.stopPropagation(); onCycle(); }}
      className={`flex items-center ml-0.5 transition-colors ${mode ? modeColor[mode] + " hover:opacity-80" : "text-muted-foreground/30 hover:text-muted-foreground"}`}
      title={mode ? `${modeLabel[mode]} — clique para próximo` : "Ordenar por status: Running → Error → Completed"}
    >
      {mode === "running" ? <ChevronUp className="w-2.5 h-2.5" /> : mode === "error" ? <ChevronDown className="w-2.5 h-2.5" /> : mode === "completed" ? <ChevronUp className="w-2.5 h-2.5" /> : <ArrowUpDown className="w-2.5 h-2.5" />}
    </button>
  );
}

export const PodMonitorTable = ({
  cluster,
  pods,
  loading,
  metrics,
  onOpenDetail,
  headerLabel,
  breadcrumb,
  onRequestRefresh,
  onBack,
  backLabel,
}: PodMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<Set<string>>(new Set());
  const [nodeFilter, setNodeFilter] = useState<Set<string>>(new Set());
  const [namespaceFilter, setNamespaceFilter] = useState<Set<string>>(new Set());
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [sortKey, setSortKey] = useState<PodSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [statusSortMode, setStatusSortMode] = useState<StatusSortMode>(null);
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);

  const handleSort = (key: PodSortKey) => {
    setStatusSortMode(null);
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  const cycleStatusSort = () => {
    setSortKey(null);
    const idx = STATUS_CYCLE.indexOf(statusSortMode);
    setStatusSortMode(STATUS_CYCLE[(idx + 1) % STATUS_CYCLE.length]);
  };

  // Seleção de pods
  const [selectedPods, setSelectedPods] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"kill" | "delete" | "restart" | null>(null);
  const [bulkProcessing, setBulkProcessing] = useState(false);

  const refreshRef = useRef(onRequestRefresh);
  useEffect(() => { refreshRef.current = onRequestRefresh; }, [onRequestRefresh]);

  const rowsContainerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  const focusRow = (idx: number) => {
    const el = rowsContainerRef.current?.querySelector<HTMLElement>(`[data-row-index="${idx}"]`);
    if (el) { el.focus(); el.scrollIntoView({ block: "nearest" }); }
  };

  // Foca a primeira linha (ou o input de busca) ao abrir o painel
  useEffect(() => {
    const id = setTimeout(() => {
      const first = rowsContainerRef.current?.querySelector<HTMLElement>("[data-row-index=\"0\"]");
      if (first) first.focus();
      else searchInputRef.current?.focus();
    }, 50);
    return () => clearTimeout(id);
  }, []);

  useEffect(() => {
    const id = setInterval(async () => {
      setRefreshing(true);
      refreshRef.current();
      setTimeout(() => setRefreshing(false), 600);
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    setLastUpdated(new Date());
  }, [pods]);

  // Ctrl+\ desmarca todos os pods selecionados
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") {
        e.preventDefault();
        setSelectedPods(new Set());
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  // Limpar seleção de pods que sumiram da lista
  useEffect(() => {
    setSelectedPods((prev) => {
      if (prev.size === 0) return prev;
      const podKeys = new Set(pods.map((p) => `${p.namespace}/${p.name}`));
      const next = new Set([...prev].filter((k) => podKeys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [pods]);

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueStatuses = useMemo(() => {
    const s = new Set<string>();
    pods.forEach((p) => {
      // Pods Running-mas-não-prontos aparecem como "NotReady" no filtro
      const v = p.statusReason === "NotReady" ? "NotReady" : (p.status || p.phase);
      if (v) s.add(v);
    });
    return Array.from(s).sort();
  }, [pods]);

  const uniqueNodes = useMemo(() => {
    const s = new Set<string>();
    pods.forEach((p) => { if (p.nodeName) s.add(p.nodeName); });
    return Array.from(s).sort();
  }, [pods]);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    pods.forEach((p) => { if (p.namespace) s.add(p.namespace); });
    return Array.from(s).sort();
  }, [pods]);

  const hasFilters = statusFilter.size > 0 || nodeFilter.size > 0 || namespaceFilter.size > 0;
  const activeFilterCount = statusFilter.size + nodeFilter.size + namespaceFilter.size;

  const clearAllFilters = () => {
    setStatusFilter(new Set());
    setNodeFilter(new Set());
    setNamespaceFilter(new Set());
    setSearchQuery("");
  };

  const filtered = useMemo(() => {
    let result = pods;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((p) =>
        p.name.toLowerCase().includes(q) ||
        (p.namespace ?? "").toLowerCase().includes(q) ||
        (p.phase ?? "").toLowerCase().includes(q) ||
        (p.statusReason ?? "").toLowerCase().includes(q) ||
        (p.nodeName ?? "").toLowerCase().includes(q) ||
        (p.podIP ?? "").toLowerCase().includes(q)
      );
    }
    if (statusFilter.size > 0)
      result = result.filter((p) => {
        const effectiveStatus = p.statusReason === "NotReady" ? "NotReady" : (p.status || p.phase || "");
        return statusFilter.has(effectiveStatus);
      });
    if (nodeFilter.size > 0)
      result = result.filter((p) => nodeFilter.has(p.nodeName ?? ""));
    if (namespaceFilter.size > 0)
      result = result.filter((p) => namespaceFilter.has(p.namespace ?? ""));

    if (statusSortMode) {
      const priority = STATUS_PRIORITY[statusSortMode];
      result = [...result].sort((a, b) => {
        const pa = priority(a.status || a.phase || "");
        const pb = priority(b.status || b.phase || "");
        return pa !== pb ? pa - pb : (a.name).localeCompare(b.name);
      });
    } else if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: number | string = 0, vb: number | string = 0;
        switch (sortKey) {
          case "name":     va = a.name; vb = b.name; break;
          case "ready":    va = (a.readyContainers ?? 0) / Math.max(a.totalContainers ?? 1, 1); vb = (b.readyContainers ?? 0) / Math.max(b.totalContainers ?? 1, 1); break;
          case "restarts": va = a.restarts ?? 0; vb = b.restarts ?? 0; break;
          case "cpu":      va = metrics?.pods[a.name]?.cpuMillicores ?? 0; vb = metrics?.pods[b.name]?.cpuMillicores ?? 0; break;
          case "mem":      va = metrics?.pods[a.name]?.memoryBytes ?? 0; vb = metrics?.pods[b.name]?.memoryBytes ?? 0; break;
          case "age":      va = a.createdAt ? new Date(a.createdAt).getTime() : 0; vb = b.createdAt ? new Date(b.createdAt).getTime() : 0; break;
          case "node":     va = a.nodeName ?? ""; vb = b.nodeName ?? ""; break;
        }
        if (typeof va === "string") return sortDir === "asc" ? va.localeCompare(vb as string) : (vb as string).localeCompare(va);
        return sortDir === "asc" ? (va as number) - (vb as number) : (vb as number) - (va as number);
      });
    }

    return result;
  }, [pods, searchQuery, statusFilter, nodeFilter, namespaceFilter, sortKey, sortDir, statusSortMode, metrics]);

  // Helpers de seleção
  const podKey = (p: PodSummary) => `${p.namespace}/${p.name}`;
  const allFilteredSelected = filtered.length > 0 && filtered.every((p) => selectedPods.has(podKey(p)));
  const someFilteredSelected = filtered.some((p) => selectedPods.has(podKey(p)));

  const toggleSelectAll = () => {
    if (allFilteredSelected) {
      const next = new Set(selectedPods);
      filtered.forEach((p) => next.delete(podKey(p)));
      setSelectedPods(next);
    } else {
      const next = new Set(selectedPods);
      filtered.forEach((p) => next.add(podKey(p)));
      setSelectedPods(next);
    }
  };

  const togglePod = (p: PodSummary) => {
    const key = podKey(p);
    const next = new Set(selectedPods);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedPods(next);
  };

  // Pods selecionados com dados completos
  const selectedPodObjects = useMemo(
    () => pods.filter((p) => selectedPods.has(podKey(p))),
    [pods, selectedPods]
  );

  // Executa ação em massa
  const executeBulkAction = async () => {
    if (!bulkAction || bulkProcessing) return;
    setBulkProcessing(true);
    const targets = selectedPodObjects;
    let succeeded = 0;
    let failed = 0;

    if (bulkAction === "restart") {
      // Batch restart via endpoint dedicado
      const result = await apiClient.batchRestartPods(
        cluster,
        targets.map((p) => ({ namespace: p.namespace, name: p.name }))
      );
      succeeded = result.success_count;
      failed = result.failed_count;
    } else {
      for (const pod of targets) {
        try {
          if (bulkAction === "kill") {
            await apiClient.killPod(cluster, pod.namespace, pod.name);
          } else {
            await apiClient.deletePod(cluster, pod.namespace, pod.name);
          }
          succeeded++;
        } catch {
          failed++;
        }
      }
    }

    const actionLabel = bulkAction === "kill" ? "Kill" : bulkAction === "restart" ? "Rollout Restart" : "Delete";
    const successMsg = bulkAction === "kill"
      ? "encerrado(s) forçadamente"
      : bulkAction === "restart"
      ? "reiniciado(s)"
      : "deletado(s)";

    if (succeeded > 0 && failed === 0) {
      toast.success(`${actionLabel} concluído`, {
        description: `${succeeded} pod(s) ${successMsg} com sucesso.`,
      });
    } else if (succeeded > 0 && failed > 0) {
      toast.warning("Parcialmente concluído", {
        description: `${succeeded} sucesso(s), ${failed} falha(s).`,
      });
    } else {
      toast.error("Falha na operação", {
        description: `Não foi possível executar ${actionLabel} em nenhum pod.`,
      });
    }

    setBulkProcessing(false);
    setBulkAction(null);
    setSelectedPods(new Set());
    onRequestRefresh();
  };

  return (
    <div className="flex flex-col h-full border border-border rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border bg-muted/30 flex-shrink-0">
        {onBack && (
          <Button variant="ghost" size="sm" className="h-7 px-2 text-xs gap-1" onClick={onBack}>
            <ChevronLeft className="w-3 h-3" />
            {backLabel ?? "Voltar"}
          </Button>
        )}
        {breadcrumb && breadcrumb.length > 0 ? (
          <div className="flex items-center gap-1 min-w-0 truncate">
            {breadcrumb.map((seg, i) => {
              const isLast = i === breadcrumb.length - 1;
              return (
                <span key={i} className="flex items-center gap-1 min-w-0">
                  {i > 0 && <ChevronRight className="w-3 h-3 text-muted-foreground/40 flex-shrink-0" />}
                  {seg.onClick ? (
                    <button
                      onClick={seg.onClick}
                      className="text-xs font-medium text-muted-foreground hover:text-foreground hover:underline truncate transition-colors"
                    >
                      {seg.label}
                    </button>
                  ) : (
                    <span className={`text-xs font-medium truncate ${isLast ? "text-foreground" : "text-muted-foreground"}`}>
                      {seg.label}
                    </span>
                  )}
                </span>
              );
            })}
            {(searchQuery || hasFilters) && (
              <span className="text-xs font-medium text-muted-foreground flex-shrink-0">
                {" "}— {filtered.length} resultado(s)
              </span>
            )}
          </div>
        ) : (
          <span className="text-xs font-medium text-muted-foreground truncate">
            {headerLabel}
            {(searchQuery || hasFilters) && ` — ${filtered.length} resultado(s)`}
          </span>
        )}
        {(loading || refreshing) && <Loader2 className="w-3 h-3 animate-spin text-muted-foreground flex-shrink-0" />}
        <div className="flex-1" />

        {lastUpdated && (
          <span className="text-[10px] text-muted-foreground/60 flex items-center gap-1 flex-shrink-0">
            <RefreshCw className={`w-2.5 h-2.5 ${refreshing ? "animate-spin" : ""}`} />
            {lastUpdatedLabel}
          </span>
        )}

        {(hasFilters || searchQuery) && (
          <Button
            variant="ghost" size="sm"
            className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground gap-1"
            onClick={clearAllFilters}
            title="Limpar todos os filtros"
          >
            <X className="w-3 h-3" />
            {activeFilterCount > 0 && (
              <Badge variant="secondary" className="text-[10px] h-4 px-1">{activeFilterCount}</Badge>
            )}
            Limpar filtros
          </Button>
        )}

        <div className="relative w-40">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
          <Input
            ref={searchInputRef}
            placeholder="Buscar..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-7 text-xs pl-6 pr-6"
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                focusRow(0);
              }
            }}
          />
          {searchQuery && (
            <button onClick={() => setSearchQuery("")} className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      </div>

      {/* Chips de filtros ativos */}
      {hasFilters && (
        <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 flex-shrink-0 flex-wrap">
          {Array.from(statusFilter).map((v) => (
            <Badge key={`s-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(statusFilter); n.delete(v); setStatusFilter(n); }}>
              status: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
          {Array.from(nodeFilter).map((v) => (
            <Badge key={`n-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(nodeFilter); n.delete(v); setNodeFilter(n); }}>
              node: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
          {Array.from(namespaceFilter).map((v) => (
            <Badge key={`ns-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(namespaceFilter); n.delete(v); setNamespaceFilter(n); }}>
              ns: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
        </div>
      )}

      {/* Cabeçalho das colunas */}
      <div
        className="grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20 flex-shrink-0"
        style={{ gridTemplateColumns: gridTemplate }}
      >
        {/* SEL — fixo, sem resize */}
        <span className="flex items-center">
          <Checkbox
            checked={allFilteredSelected}
            data-state={someFilteredSelected && !allFilteredSelected ? "indeterminate" : undefined}
            onCheckedChange={toggleSelectAll}
            className="w-3.5 h-3.5 rounded-full"
            title={allFilteredSelected ? "Desmarcar todos" : "Selecionar todos (ou use Espaço na linha)"}
            disabled={filtered.length === 0}
          />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          {uniqueNamespaces.length > 1
            ? <><ColumnFilter label="NAME/NS" options={uniqueNamespaces} selected={namespaceFilter} onChange={setNamespaceFilter} /><SortIcon colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} /></>
            : <SortBtn label="NAME" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />}
          <ResizeHandle onResize={(d) => resize(1, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 text-muted-foreground uppercase text-[10px]">
          VERSION
          <ResizeHandle onResize={(d) => resize(2, d)} />
        </span>
        {/* dot — fixo, sem resize */}
        <span></span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="READY" colKey="ready" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(4, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          <ColumnFilter label="STATUS" options={uniqueStatuses} selected={statusFilter} onChange={setStatusFilter} />
          <StatusSortIcon mode={statusSortMode} onCycle={cycleStatusSort} />
          <ResizeHandle onResize={(d) => resize(5, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="REST." colKey="restarts" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(6, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="CPU" colKey="cpu" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(7, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="MEM" colKey="mem" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(8, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          <ColumnFilter label="NODE" options={uniqueNodes} selected={nodeFilter} onChange={setNodeFilter} />
          <SortIcon colKey="node" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(9, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="AGE" colKey="age" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(10, d)} />
        </span>
      </div>

      {/* Linhas */}
      <div ref={rowsContainerRef} className="rows-container flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum pod encontrado para os filtros aplicados" : "Nenhum pod encontrado"}
          </div>
        )}
        {filtered.map((pod, index) => {
          const m = metrics?.pods[pod.name];
          // Prioridade da cor: "NotReady" (statusReason explícito) > pod.status (CrashLoopBackOff, Error, etc.)
          const effectiveReason = pod.statusReason === "NotReady" ? "NotReady" : (pod.status ?? "");
          const rowColor = podRowColor(pod.phase ?? "", effectiveReason);
          const dotColor = podDotColor(pod.phase ?? "", effectiveReason);
          const isSelected = selectedPods.has(podKey(pod));

          return (
            <button
              key={`${pod.namespace}/${pod.name}`}
              data-row-index={index}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenDetail(pod)}
              onKeyDown={(e) => {
                if (e.key === " ") {
                  e.preventDefault();
                  togglePod(pod);
                } else if (e.key === "ArrowDown") {
                  e.preventDefault();
                  focusRow(index + 1);
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  if (index === 0) {
                    searchInputRef.current?.focus();
                  } else {
                    focusRow(index - 1);
                  }
                }
                // Enter aciona onClick naturalmente (abre o modal)
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              {/* Checkbox circular — clique no span faz toggle sem propagar para onOpenDetail */}
              <span
                className="flex items-center"
                onClick={(e) => { e.stopPropagation(); togglePod(pod); }}
              >
                <Checkbox
                  checked={isSelected}
                  onCheckedChange={() => {}}
                  className="w-3.5 h-3.5 rounded-full pointer-events-none"
                />
              </span>

              {/* NAME / NS */}
              <span className="min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[9px] block leading-tight">{pod.namespace}</span>
                )}
                <span className="truncate block" title={pod.name}>{pod.name}</span>
              </span>

              {/* VERSION */}
              <span className="truncate text-muted-foreground text-[10px] flex items-center" title={pod.containers[0]?.image ?? ""}>
                {extractImageVersion(pod.containers[0]?.image)}
              </span>

              {/* dot */}
              <span className="flex items-center justify-center">
                <span className={`w-2 h-2 rounded-full ${dotColor}`} />
              </span>

              {/* READY */}
              <span>{pod.readyContainers}/{pod.totalContainers}</span>

              {/* STATUS */}
              <span className="truncate" title={pod.statusReason || pod.status || pod.phase}>
                {pod.statusReason === "NotReady" ? "NotReady" : (pod.status || pod.phase || "-")}
              </span>

              {/* REST. */}
              <span>{pod.restarts}</span>

              {/* CPU */}
              <div className="flex flex-col min-w-0 overflow-hidden">
                <span className="truncate">
                  {m ? (() => {
                    const cur = formatMillicores(m.cpuMillicores);
                    const limitM = parseCpuToMillicores(pod.cpuLimit || "");
                    const pct = limitM > 0 ? Math.round(m.cpuMillicores / limitM * 100) : null;
                    return pct !== null ? `${cur} / ${pct}%` : cur;
                  })() : "-"}
                </span>
                <span className="truncate text-muted-foreground/70 text-[10px] -mt-0.5">
                  {pod.cpuRequest || "0"} / {pod.cpuLimit || "∞"}
                </span>
              </div>

              {/* MEM */}
              <div className="flex flex-col min-w-0 overflow-hidden">
                <span className="truncate">
                  {m ? (() => {
                    const cur = formatBytes(m.memoryBytes);
                    const limitB = parseMemoryToBytes(pod.memoryLimit || "");
                    const pct = limitB > 0 ? Math.round(m.memoryBytes / limitB * 100) : null;
                    return pct !== null ? `${cur} / ${pct}%` : cur;
                  })() : "-"}
                </span>
                <span className="truncate text-muted-foreground/70 text-[10px] -mt-0.5">
                  {pod.memoryRequest || "0"} / {pod.memoryLimit || "∞"}
                </span>
              </div>

              {/* NODE */}
              <span className="truncate" title={pod.nodeName}>{pod.nodeName || "-"}</span>

              {/* AGE */}
              <span className="text-muted-foreground">{pod.createdAt ? formatAge(pod.createdAt) : "-"}</span>
            </button>
          );
        })}
      </div>

      {/* Barra de ações em massa — visível quando há seleção */}
      {selectedPods.size > 0 && (
        <div className="flex-shrink-0 border-t border-border">
          {/* Confirmação da ação */}
          {bulkAction && (
            <div className={`flex items-center gap-2 px-3 py-2 text-xs ${
              bulkAction === "delete"
                ? "bg-destructive/10 border-b border-destructive/30"
                : bulkAction === "restart"
                ? "bg-blue-500/10 border-b border-blue-500/30"
                : "bg-orange-500/10 border-b border-orange-500/30"
            }`}>
              <span className="flex-1">
                {bulkAction === "kill"
                  ? `Forçar encerramento de ${selectedPodObjects.length} pod(s)? Os pods serão reiniciados pelo controlador.`
                  : bulkAction === "restart"
                  ? `Reiniciar ${selectedPodObjects.length} pod(s) via rollout restart?`
                  : `Deletar ${selectedPodObjects.length} pod(s) permanentemente?`}
              </span>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs"
                onClick={() => setBulkAction(null)}
                disabled={bulkProcessing}
              >
                Cancelar
              </Button>
              <Button
                size="sm"
                className={`h-6 px-3 text-xs gap-1 text-white ${
                bulkAction === "delete"
                  ? "bg-destructive hover:bg-destructive/90"
                  : bulkAction === "restart"
                  ? "bg-blue-600 hover:bg-blue-700"
                  : "bg-orange-500 hover:bg-orange-600"
              }`}
                onClick={executeBulkAction}
                disabled={bulkProcessing}
              >
                Confirmar
              </Button>
            </div>
          )}

          {/* Toolbar principal */}
          {!bulkAction && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-muted/30">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
                onClick={() => setSelectedPods(new Set())}
                title="Desmarcar todos (Ctrl+\)"
              >
                Desmarcar tudo
              </Button>
              <span className="text-xs text-muted-foreground">
                <span className="font-medium text-foreground">{selectedPods.size}</span> pod(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-blue-500 border-blue-500/40 hover:bg-blue-500/10 hover:border-blue-500"
                  onClick={() => setBulkAction("restart")}
                  title="Rollout restart nos pods selecionados"
                >
                  Restart ({selectedPods.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-orange-500 border-orange-500/40 hover:bg-orange-500/10 hover:border-orange-500"
                  onClick={() => setBulkAction("kill")}
                  title="Forçar encerramento (SIGKILL)"
                >
                  Kill ({selectedPods.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-destructive border-destructive/40 hover:bg-destructive/10 hover:border-destructive"
                  onClick={() => setBulkAction("delete")}
                  title="Deletar pods selecionados"
                >
                  Deletar ({selectedPods.size})
                </Button>
              </ProtectedAction>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
