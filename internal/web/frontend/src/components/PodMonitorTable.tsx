import { useEffect, useRef, useState, useMemo } from "react";
import type { PodSummary, BatchPodMetrics } from "@/lib/api/types";
import { formatAge, formatBytes, formatMillicores, podRowColor, podDotColor } from "@/lib/monitorUtils";
import { Loader2, ChevronLeft, Search, X, ListFilter, Check, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";

const REFRESH_INTERVAL_MS = 5000;

interface PodMonitorTableProps {
  cluster: string;
  pods: PodSummary[];
  loading: boolean;
  metrics: BatchPodMetrics | null;
  metricsLoading: boolean;
  onOpenDetail: (pod: PodSummary) => void;
  headerLabel: string;
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
      <PopoverContent className="w-48 p-2" align="start">
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs font-medium">{label}</span>
          {active && <button onClick={() => onChange(new Set())} className="text-xs text-muted-foreground hover:text-foreground">Limpar</button>}
        </div>
        <ScrollArea className="max-h-48">
          <div className="space-y-1">
            {options.map((opt) => (
              <label key={opt} className="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-muted/50 text-xs">
                <Checkbox checked={selected.has(opt)} onCheckedChange={() => toggle(opt)} className="w-3.5 h-3.5" />
                <span className="truncate flex-1" title={opt}>{opt}</span>
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

// Colunas: NAME/NS | dot | READY | STATUS | REST. | CPU | MEM | NODE | AGE
const GRID = "minmax(180px,1fr) 22px 56px 100px 50px 64px 72px minmax(130px,1fr) 56px";

export const PodMonitorTable = ({
  pods,
  loading,
  metrics,
  onOpenDetail,
  headerLabel,
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

  const refreshRef = useRef(onRequestRefresh);
  useEffect(() => { refreshRef.current = onRequestRefresh; }, [onRequestRefresh]);

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

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueStatuses = useMemo(() => {
    const s = new Set<string>();
    pods.forEach((p) => { const v = p.statusReason || p.phase; if (v) s.add(v); });
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
      result = result.filter((p) => statusFilter.has(p.statusReason || p.phase || ""));
    if (nodeFilter.size > 0)
      result = result.filter((p) => nodeFilter.has(p.nodeName ?? ""));
    if (namespaceFilter.size > 0)
      result = result.filter((p) => namespaceFilter.has(p.namespace ?? ""));
    return result;
  }, [pods, searchQuery, statusFilter, nodeFilter, namespaceFilter]);

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
        <span className="text-xs font-medium text-muted-foreground truncate">
          {headerLabel}
          {(searchQuery || hasFilters) && ` — ${filtered.length} resultado(s)`}
        </span>
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
            placeholder="Buscar..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-7 text-xs pl-6 pr-6"
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
        style={{ gridTemplateColumns: GRID }}
      >
        <span>
          {uniqueNamespaces.length > 1
            ? <ColumnFilter label="NAME/NS" options={uniqueNamespaces} selected={namespaceFilter} onChange={setNamespaceFilter} />
            : <span className="text-muted-foreground uppercase">NAME</span>}
        </span>
        <span></span>
        <span className="text-muted-foreground uppercase">READY</span>
        <span>
          <ColumnFilter label="STATUS" options={uniqueStatuses} selected={statusFilter} onChange={setStatusFilter} />
        </span>
        <span className="text-muted-foreground uppercase">REST.</span>
        <span className="text-muted-foreground uppercase">CPU</span>
        <span className="text-muted-foreground uppercase">MEM</span>
        <span>
          <ColumnFilter label="NODE" options={uniqueNodes} selected={nodeFilter} onChange={setNodeFilter} />
        </span>
        <span className="text-muted-foreground uppercase">AGE</span>
      </div>

      {/* Linhas */}
      <div className="flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum pod encontrado para os filtros aplicados" : "Nenhum pod encontrado"}
          </div>
        )}
        {filtered.map((pod) => {
          const m = metrics?.pods[pod.name];
          const rowColor = podRowColor(pod.phase ?? "", pod.statusReason);
          const dotColor = podDotColor(pod.phase ?? "", pod.statusReason);

          return (
            <button
              key={`${pod.namespace}/${pod.name}`}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer`}
              style={{ gridTemplateColumns: GRID }}
              onClick={() => onOpenDetail(pod)}
              title="Clique para ver detalhes e logs"
            >
              {/* NAME / NS */}
              <span className="min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[9px] block leading-tight">{pod.namespace}</span>
                )}
                <span className="truncate block" title={pod.name}>{pod.name}</span>
              </span>

              {/* dot */}
              <span className="flex items-center justify-center">
                <span className={`w-2 h-2 rounded-full ${dotColor}`} />
              </span>

              {/* READY */}
              <span>{pod.readyContainers}/{pod.totalContainers}</span>

              {/* STATUS */}
              <span className="truncate" title={pod.statusReason || pod.phase}>{pod.statusReason || pod.phase || "-"}</span>

              {/* REST. */}
              <span>{pod.restarts}</span>

              {/* CPU */}
              <span>{m ? formatMillicores(m.cpuMillicores) : "n/a"}</span>

              {/* MEM */}
              <span>{m ? formatBytes(m.memoryBytes) : "n/a"}</span>

              {/* NODE */}
              <span className="truncate" title={pod.nodeName}>{pod.nodeName || "-"}</span>

              {/* AGE */}
              <span className="text-muted-foreground">{pod.createdAt ? formatAge(pod.createdAt) : "-"}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
};
