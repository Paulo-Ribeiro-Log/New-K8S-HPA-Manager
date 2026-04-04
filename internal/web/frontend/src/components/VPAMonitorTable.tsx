import { useEffect, useRef, useState, useMemo } from "react";
import type { VPASummary } from "@/lib/api/types";
import { Pencil, Loader2, Search, X, RefreshCw, Trash2, ArrowUpDown, ChevronUp, ChevronDown, ListFilter, Check, CheckCircle2, Circle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 10000;

// SEL(fixed) | NAME/NS | TARGET | MODE | RECS | EDIT(fixed)
const INITIAL_WIDTHS = [28, 200, 160, 80, 50, 28];

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

function updateModeBadge(mode: string): string {
  switch (mode?.toLowerCase()) {
    case "auto":      return "bg-green-500/20 text-green-400 border border-green-500/30";
    case "off":       return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
    case "initial":   return "bg-blue-500/20 text-blue-400 border border-blue-500/30";
    case "recreate":  return "bg-orange-500/20 text-orange-400 border border-orange-500/30";
    default:          return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
  }
}

type SortKey = "name" | "mode" | "target";

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: SortKey; sortKey: SortKey | null; sortDir: "asc" | "desc"; onSort: (k: SortKey) => void;
}) {
  const active = sortKey === colKey;
  return (
    <button
      onClick={() => onSort(colKey)}
      className={`flex items-center gap-0.5 uppercase transition-colors ${active ? "text-primary hover:text-primary/80" : "text-muted-foreground hover:text-foreground"}`}
    >
      {label}
      {active && sortDir === "asc" ? <ChevronUp className="w-2.5 h-2.5" /> : active && sortDir === "desc" ? <ChevronDown className="w-2.5 h-2.5" /> : <ArrowUpDown className="w-2.5 h-2.5 opacity-30" />}
    </button>
  );
}

function ColumnFilter({ label, options, selected, onChange }: {
  label: string; options: string[]; selected: Set<string>; onChange: (v: Set<string>) => void;
}) {
  const active = selected.size > 0;
  const toggle = (val: string) => {
    const next = new Set(selected);
    if (next.has(val)) next.delete(val); else next.add(val);
    onChange(next);
  };
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button className={`flex items-center gap-0.5 uppercase hover:text-foreground transition-colors ${active ? "text-primary" : "text-muted-foreground"}`} title={`Filtrar por ${label}`}>
          {label}<ListFilter className="w-2.5 h-2.5 ml-0.5" />
          {active && <span className="ml-0.5 bg-primary text-primary-foreground rounded-full text-[9px] w-3.5 h-3.5 flex items-center justify-center font-bold">{selected.size}</span>}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-max min-w-[140px] p-2" align="start">
        <div className="flex items-center justify-between gap-4 mb-2">
          <span className="text-xs font-medium">{label}</span>
          {active && <button onClick={() => onChange(new Set())} className="text-xs text-muted-foreground hover:text-foreground">Limpar</button>}
        </div>
        <ScrollArea className="max-h-48">
          <div className="space-y-1">
            {options.map((opt) => (
              <label key={opt} className="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-muted/50 text-xs">
                <Checkbox checked={selected.has(opt)} onCheckedChange={() => toggle(opt)} className="w-3.5 h-3.5 rounded-full flex-shrink-0" />
                <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${updateModeBadge(opt)}`}>{opt}</span>
                {selected.has(opt) && <Check className="w-3 h-3 text-primary flex-shrink-0" />}
              </label>
            ))}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  );
}

interface VPAMonitorTableProps {
  items: VPASummary[];
  loading: boolean;
  headerLabel: string;
  onOpenEditor: (item: VPASummary) => void;
  onRequestRefresh: () => void;
}

export const VPAMonitorTable = ({
  items,
  loading,
  headerLabel,
  onOpenEditor,
  onRequestRefresh,
}: VPAMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey] = useState<SortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [modeFilter, setModeFilter] = useState<Set<string>>(new Set());
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"delete" | null>(null);
  const [bulkProcessing, setBulkProcessing] = useState(false);

  const refreshRef = useRef(onRequestRefresh);
  useEffect(() => { refreshRef.current = onRequestRefresh; }, [onRequestRefresh]);

  const rowsContainerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  const focusRow = (idx: number) => {
    const el = rowsContainerRef.current?.querySelector<HTMLElement>(`[data-row-index="${idx}"]`);
    if (el) { el.focus(); el.scrollIntoView({ block: "nearest" }); }
  };

  useEffect(() => {
    const id = setTimeout(() => {
      const first = rowsContainerRef.current?.querySelector<HTMLElement>("[data-row-index=\"0\"]");
      if (first) first.focus();
      else searchInputRef.current?.focus();
    }, 50);
    return () => clearTimeout(id);
  }, []);

  useEffect(() => {
    const id = setInterval(() => {
      setRefreshing(true);
      refreshRef.current();
      setTimeout(() => setRefreshing(false), 600);
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => { setLastUpdated(new Date()); }, [items]);

  useEffect(() => {
    setSelectedKeys((prev) => {
      if (prev.size === 0) return prev;
      const keys = new Set(items.map((i) => itemKey(i)));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [items]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") { e.preventDefault(); setSelectedKeys(new Set()); }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else { setSortKey(key); setSortDir("asc"); }
  };

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    items.forEach((d) => { if (d.namespace) s.add(d.namespace); });
    return Array.from(s).sort();
  }, [items]);

  const uniqueModes = useMemo(() => {
    const s = new Set<string>();
    items.forEach((i) => { if (i.updateMode) s.add(i.updateMode); });
    return Array.from(s).sort();
  }, [items]);

  const hasFilters = modeFilter.size > 0;

  const filtered = useMemo(() => {
    let result = items;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((i) =>
        i.name.toLowerCase().includes(q) ||
        i.namespace.toLowerCase().includes(q) ||
        i.targetRefName.toLowerCase().includes(q) ||
        i.updateMode.toLowerCase().includes(q)
      );
    }
    if (modeFilter.size > 0) {
      result = result.filter((i) => modeFilter.has(i.updateMode));
    }
    if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: string, vb: string;
        if (sortKey === "mode") { va = a.updateMode; vb = b.updateMode; }
        else if (sortKey === "target") { va = a.targetRefName; vb = b.targetRefName; }
        else { va = a.name; vb = b.name; }
        return sortDir === "asc" ? va.localeCompare(vb) : vb.localeCompare(va);
      });
    }
    return result;
  }, [items, searchQuery, sortKey, sortDir, modeFilter]);

  const itemKey = (i: VPASummary) => `${i.cluster}/${i.namespace}/${i.name}`;
  const allSelected = filtered.length > 0 && filtered.every((i) => selectedKeys.has(itemKey(i)));
  const someSelected = filtered.some((i) => selectedKeys.has(itemKey(i)));

  const toggleAll = () => {
    if (allSelected) {
      const next = new Set(selectedKeys);
      filtered.forEach((i) => next.delete(itemKey(i)));
      setSelectedKeys(next);
    } else {
      const next = new Set(selectedKeys);
      filtered.forEach((i) => next.add(itemKey(i)));
      setSelectedKeys(next);
    }
  };

  const toggleItem = (i: VPASummary) => {
    const key = itemKey(i);
    const next = new Set(selectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedKeys(next);
  };

  const selectedObjects = useMemo(
    () => items.filter((i) => selectedKeys.has(itemKey(i))),
    [items, selectedKeys]
  );

  const executeBulkDelete = async () => {
    if (bulkProcessing || selectedObjects.length === 0) return;
    setBulkProcessing(true);
    let succeeded = 0, failed = 0;
    try {
      await Promise.all(
        selectedObjects.map(async (i) => {
          try {
            await apiClient.deleteVPA(i.cluster, i.namespace, i.name);
            succeeded++;
          } catch { failed++; }
        })
      );
      if (succeeded > 0 && failed === 0) toast.success(`${succeeded} VPA(s) deletado(s) com sucesso.`);
      else if (succeeded > 0) toast.warning(`${succeeded} sucesso(s), ${failed} falha(s).`);
      else toast.error("Falha na operação em lote.");
    } catch (err) {
      toast.error("Erro na operação em lote", { description: err instanceof Error ? err.message : "Erro desconhecido" });
    } finally {
      setBulkProcessing(false);
      setBulkAction(null);
      setSelectedKeys(new Set());
      onRequestRefresh();
    }
  };

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
        <div className="relative w-40">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
          <Input
            ref={searchInputRef}
            placeholder="Buscar..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-7 text-xs pl-6 pr-6"
            onKeyDown={(e) => { if (e.key === "ArrowDown") { e.preventDefault(); focusRow(0); } }}
          />
          {searchQuery && (
            <button onClick={() => setSearchQuery("")} className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      </div>

      {/* Column headers + rows */}
      <div ref={rowsContainerRef} className="flex-1 overflow-auto">
        <div className="sticky top-0 z-10 grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20" style={{ gridTemplateColumns: gridTemplate }}>
          <span className="flex items-center">
            <Checkbox
              checked={allSelected}
              data-state={someSelected && !allSelected ? "indeterminate" : undefined}
              onCheckedChange={toggleAll}
              className="w-3.5 h-3.5 rounded-full"
              disabled={filtered.length === 0}
            />
          </span>
          <span className="relative flex items-center overflow-hidden pr-3">
            <SortBtn label="NAME/NS" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(1, d)} />
          </span>
          <span className="relative flex items-center overflow-hidden pr-3">
            <SortBtn label="TARGET" colKey="target" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative flex items-center overflow-hidden pr-3">
            <ColumnFilter label="MODE" options={uniqueModes} selected={modeFilter} onChange={setModeFilter} />
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-3">
            RECS
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum VPA encontrado para os filtros aplicados" : "Nenhum VPA encontrado"}
          </div>
        )}
        {filtered.map((item, index) => {
          const isSelected = selectedKeys.has(itemKey(item));

          return (
            <div
              key={itemKey(item)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs text-foreground cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenEditor(item)}
              onKeyDown={(e) => {
                if (e.key === " ") { e.preventDefault(); toggleItem(item); }
                else if (e.key === "Enter") onOpenEditor(item);
                else if (e.key === "ArrowDown") { e.preventDefault(); focusRow(index + 1); }
                else if (e.key === "ArrowUp") { e.preventDefault(); if (index === 0) searchInputRef.current?.focus(); else focusRow(index - 1); }
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              <span className="flex items-center" onClick={(e) => { e.stopPropagation(); toggleItem(item); }}>
                <Checkbox checked={isSelected} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{item.namespace}</span>
                )}
                <span className="truncate block" title={`${item.namespace}/${item.name}`}>{item.name}</span>
              </span>
              <span className="truncate text-muted-foreground" title={`${item.targetRefKind}/${item.targetRefName}`}>
                <span className="text-muted-foreground/60 text-[10px]">{item.targetRefKind}/</span>{item.targetRefName}
              </span>
              <span>
                <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${updateModeBadge(item.updateMode)}`}>
                  {item.updateMode}
                </span>
              </span>
              <span className="flex items-center">
                {item.hasRecommendation
                  ? <CheckCircle2 className="w-3.5 h-3.5 text-green-400" title="Tem recomendação" />
                  : <Circle className="w-3.5 h-3.5 text-muted-foreground/40" title="Sem recomendação" />
                }
              </span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(item); }}
                  title="Abrir editor YAML"
                >
                  <Pencil className="w-3 h-3" />
                </span>
              </span>
            </div>
          );
        })}
      </div>

      {/* Barra de ações em massa */}
      {selectedKeys.size > 0 && (
        <div className="flex-shrink-0 border-t border-border">
          {bulkAction === "delete" && (
            <div className="flex items-center gap-2 px-3 py-2 text-xs bg-destructive/10 border-b border-destructive/30">
              <span className="flex-1">Deletar {selectedObjects.length} VPA(s) permanentemente?</span>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setBulkAction(null)} disabled={bulkProcessing}>Cancelar</Button>
              <Button
                size="sm"
                className="h-6 px-3 text-xs gap-1 text-white bg-destructive hover:bg-destructive/90"
                onClick={executeBulkDelete}
                disabled={bulkProcessing}
              >
                {bulkProcessing ? <Loader2 className="w-3 h-3 animate-spin" /> : "Confirmar"}
              </Button>
            </div>
          )}
          {!bulkAction && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-muted/30">
              <Button variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground" onClick={() => setSelectedKeys(new Set())} title="Desmarcar todos (Ctrl+\)">
                Desmarcar tudo
              </Button>
              <span className="text-xs text-muted-foreground">
                <span className="font-medium text-foreground">{selectedKeys.size}</span> VPA(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-destructive border-destructive/40 hover:bg-destructive/10 hover:border-destructive gap-1" onClick={() => setBulkAction("delete")}>
                  <Trash2 className="w-3 h-3" /> Deletar ({selectedKeys.size})
                </Button>
              </ProtectedAction>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
