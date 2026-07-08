import { useEffect, useRef, useState, useMemo } from "react";
import type { PodSummary, ContainerStatus } from "@/lib/api/types";
import { formatAge } from "@/lib/monitorUtils";
import { Loader2, Search, X, ListFilter, Check, RefreshCw, ChevronUp, ChevronDown, ArrowUpDown } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 5000;

interface ContainerRow {
  pod: PodSummary;
  container: ContainerStatus;
}

interface ContainerMonitorTableProps {
  pods: PodSummary[];
  loading: boolean;
  headerLabel: string;
  onOpenDetail: (pod: PodSummary, containerName: string) => void;
  onRequestRefresh: () => void;
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

// Colunas: NAME | TYPE | POD | VERSION | dot | READY | STATE | REST. | NODE | AGE
const INITIAL_WIDTHS = [260, 65, 240, 110, 22, 55, 130, 50, 160, 60];

function extractImageVersion(image?: string): string {
  if (!image) return "-";
  const tag = image.split(":").pop();
  if (!tag || tag === image) return "-";
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

// "Completed"/"succeeded" é o motivo NORMAL de um init container ou de um ephemeral container
// (ex: kafka-test rodando `sleep 300`) terminar depois de cumprir o que tinha que fazer — não é
// erro, tem que ficar cinza/neutro. Só razões que indicam falha de verdade (crash loop, erro,
// OOM) são vermelhas.
function isBenignTerminalReason(reason: string): boolean {
  return reason === "completed" || reason === "succeeded";
}

// Cores próprias pro estado de CONTAINER (Running/Waiting/Terminated) — não dá pra reaproveitar
// podRowColor/podDotColor porque aqueles esperam fase de POD (Running/Pending/Failed/Succeeded),
// vocabulário diferente do State de container.
function containerRowColor(container: ContainerStatus): string {
  const reason = (container.stateReason ?? "").toLowerCase();
  const state = container.state.toLowerCase();
  if (reason.includes("crashloop") || reason.includes("error") || reason.includes("oomkilled")) return "text-red-400";
  if (state === "terminated") return reason && !isBenignTerminalReason(reason) ? "text-red-400" : "text-gray-400";
  if (state === "waiting") return "text-orange-400";
  if (state === "running" && !container.ready) return "text-orange-400";
  if (state === "running") return "text-green-400";
  return "text-gray-300";
}

function containerDotColor(container: ContainerStatus): string {
  const reason = (container.stateReason ?? "").toLowerCase();
  const state = container.state.toLowerCase();
  if (reason.includes("crashloop") || reason.includes("error") || reason.includes("oomkilled")) return "bg-red-500";
  if (state === "terminated") return reason && !isBenignTerminalReason(reason) ? "bg-red-500" : "bg-gray-500";
  if (state === "waiting") return "bg-orange-500";
  if (state === "running" && !container.ready) return "bg-orange-500";
  if (state === "running") return "bg-green-500";
  return "bg-gray-600";
}

function typeBadge(type: ContainerStatus["type"]) {
  if (type === "init") {
    return <Badge variant="outline" className="text-[9px] px-1 py-0 border-blue-400 text-blue-600 dark:text-blue-400">init</Badge>;
  }
  if (type === "ephemeral") {
    return <Badge variant="outline" className="text-[9px] px-1 py-0 border-purple-400 text-purple-600 dark:text-purple-400">efêm.</Badge>;
  }
  return <Badge variant="outline" className="text-[9px] px-1 py-0">main</Badge>;
}

type ContainerSortKey = "name" | "type" | "pod" | "ready" | "restarts" | "node" | "age";

function SortIcon({ colKey, sortKey, sortDir, onSort }: {
  colKey: ContainerSortKey; sortKey: ContainerSortKey | null; sortDir: "asc" | "desc"; onSort: (k: ContainerSortKey) => void;
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
  label: string; colKey: ContainerSortKey; sortKey: ContainerSortKey | null; sortDir: "asc" | "desc"; onSort: (k: ContainerSortKey) => void;
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

export const ContainerMonitorTable = ({
  pods,
  loading,
  headerLabel,
  onOpenDetail,
  onRequestRefresh,
}: ContainerMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState<Set<string>>(new Set());
  const [stateFilter, setStateFilter] = useState<Set<string>>(new Set());
  const [nodeFilter, setNodeFilter] = useState<Set<string>>(new Set());
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [sortKey, setSortKey] = useState<ContainerSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);

  const refreshRef = useRef(onRequestRefresh);
  useEffect(() => { refreshRef.current = onRequestRefresh; }, [onRequestRefresh]);

  const rowsContainerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  const focusRow = (idx: number) => {
    const el = rowsContainerRef.current?.querySelector<HTMLElement>(`[data-row-index="${idx}"]`);
    if (el) { el.focus(); el.scrollIntoView({ block: "nearest" }); }
  };

  useEffect(() => {
    const id = setInterval(() => {
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

  const rows = useMemo<ContainerRow[]>(
    () => pods.flatMap((pod) => pod.containers.map((container) => ({ pod, container }))),
    [pods]
  );

  const handleSort = (key: ContainerSortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  const uniqueTypes = useMemo(() => {
    const s = new Set<string>();
    rows.forEach((r) => s.add(r.container.type));
    return Array.from(s).sort();
  }, [rows]);

  const uniqueStates = useMemo(() => {
    const s = new Set<string>();
    rows.forEach((r) => { if (r.container.state) s.add(r.container.state); });
    return Array.from(s).sort();
  }, [rows]);

  const uniqueNodes = useMemo(() => {
    const s = new Set<string>();
    pods.forEach((p) => { if (p.nodeName) s.add(p.nodeName); });
    return Array.from(s).sort();
  }, [pods]);

  const hasFilters = typeFilter.size > 0 || stateFilter.size > 0 || nodeFilter.size > 0;
  const activeFilterCount = typeFilter.size + stateFilter.size + nodeFilter.size;

  const clearAllFilters = () => {
    setTypeFilter(new Set());
    setStateFilter(new Set());
    setNodeFilter(new Set());
    setSearchQuery("");
  };

  const filtered = useMemo(() => {
    let result = rows;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((r) =>
        r.container.name.toLowerCase().includes(q) ||
        r.pod.name.toLowerCase().includes(q) ||
        r.pod.namespace.toLowerCase().includes(q) ||
        r.container.image.toLowerCase().includes(q) ||
        (r.pod.nodeName ?? "").toLowerCase().includes(q)
      );
    }
    if (typeFilter.size > 0) result = result.filter((r) => typeFilter.has(r.container.type));
    if (stateFilter.size > 0) result = result.filter((r) => stateFilter.has(r.container.state));
    if (nodeFilter.size > 0) result = result.filter((r) => nodeFilter.has(r.pod.nodeName ?? ""));

    if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: number | string = 0, vb: number | string = 0;
        switch (sortKey) {
          case "name":     va = a.container.name; vb = b.container.name; break;
          case "type":     va = a.container.type; vb = b.container.type; break;
          case "pod":      va = a.pod.name; vb = b.pod.name; break;
          case "ready":    va = a.container.ready ? 1 : 0; vb = b.container.ready ? 1 : 0; break;
          case "restarts": va = a.container.restartCount; vb = b.container.restartCount; break;
          case "node":     va = a.pod.nodeName ?? ""; vb = b.pod.nodeName ?? ""; break;
          case "age":      va = a.pod.createdAt ? new Date(a.pod.createdAt).getTime() : 0; vb = b.pod.createdAt ? new Date(b.pod.createdAt).getTime() : 0; break;
        }
        if (typeof va === "string") return sortDir === "asc" ? va.localeCompare(vb as string) : (vb as string).localeCompare(va);
        return sortDir === "asc" ? (va as number) - (vb as number) : (vb as number) - (va as number);
      });
    }

    return result;
  }, [rows, searchQuery, typeFilter, stateFilter, nodeFilter, sortKey, sortDir]);

  return (
    <div className="flex flex-col h-full border border-border rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border bg-muted/30 flex-shrink-0">
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
          {Array.from(typeFilter).map((v) => (
            <Badge key={`t-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(typeFilter); n.delete(v); setTypeFilter(n); }}>
              tipo: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
          {Array.from(stateFilter).map((v) => (
            <Badge key={`s-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(stateFilter); n.delete(v); setStateFilter(n); }}>
              state: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
          {Array.from(nodeFilter).map((v) => (
            <Badge key={`n-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(nodeFilter); n.delete(v); setNodeFilter(n); }}>
              node: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
        </div>
      )}

      {/* Cabeçalho das colunas */}
      <div
        className="grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20 flex-shrink-0"
        style={{ gridTemplateColumns: gridTemplate }}
      >
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="NAME" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(0, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          {uniqueTypes.length > 1
            ? <><ColumnFilter label="TIPO" options={uniqueTypes} selected={typeFilter} onChange={setTypeFilter} /><SortIcon colKey="type" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} /></>
            : <SortBtn label="TIPO" colKey="type" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />}
          <ResizeHandle onResize={(d) => resize(1, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="POD" colKey="pod" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(2, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 text-muted-foreground uppercase text-[10px]">
          VERSION
          <ResizeHandle onResize={(d) => resize(3, d)} />
        </span>
        {/* dot — fixo, sem resize */}
        <span></span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="READY" colKey="ready" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(5, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          <ColumnFilter label="STATE" options={uniqueStates} selected={stateFilter} onChange={setStateFilter} />
          <ResizeHandle onResize={(d) => resize(6, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="REST." colKey="restarts" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(7, d)} />
        </span>
        <span className="relative overflow-hidden pr-4 flex items-center">
          <ColumnFilter label="NODE" options={uniqueNodes} selected={nodeFilter} onChange={setNodeFilter} />
          <SortIcon colKey="node" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(8, d)} />
        </span>
        <span className="relative overflow-hidden pr-4">
          <SortBtn label="AGE" colKey="age" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
          <ResizeHandle onResize={(d) => resize(9, d)} />
        </span>
      </div>

      {/* Linhas */}
      <div ref={rowsContainerRef} className="rows-container flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum container encontrado para os filtros aplicados" : "Nenhum container encontrado"}
          </div>
        )}
        {filtered.map(({ pod, container }, index) => {
          const rowColor = containerRowColor(container);
          const dotColor = containerDotColor(container);

          return (
            <button
              key={`${pod.namespace}/${pod.name}/${container.name}`}
              data-row-index={index}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenDetail(pod, container.name)}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  focusRow(index + 1);
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  if (index === 0) searchInputRef.current?.focus();
                  else focusRow(index - 1);
                }
              }}
              title="Enter para detalhes • ↑↓ para navegar"
            >
              {/* NAME */}
              <span className="min-w-0 truncate" title={container.name}>{container.name}</span>

              {/* TYPE */}
              <span className="flex items-center">{typeBadge(container.type)}</span>

              {/* POD */}
              <span className="min-w-0">
                <span className="text-muted-foreground text-[9px] block leading-tight">{pod.namespace}</span>
                <span className="truncate block" title={pod.name}>{pod.name}</span>
              </span>

              {/* VERSION */}
              <span className="truncate text-muted-foreground text-[10px] flex items-center" title={container.image}>
                {extractImageVersion(container.image)}
              </span>

              {/* dot */}
              <span className="flex items-center justify-center">
                <span className={`w-2 h-2 rounded-full ${dotColor}`} />
              </span>

              {/* READY */}
              <span className={container.ready ? "text-green-400" : "text-muted-foreground"}>
                {container.ready ? "true" : "false"}
              </span>

              {/* STATE */}
              <span className="truncate" title={container.stateReason || container.state}>
                {container.state}{container.stateReason ? ` (${container.stateReason})` : ""}
              </span>

              {/* REST. */}
              <span>{container.restartCount}</span>

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
