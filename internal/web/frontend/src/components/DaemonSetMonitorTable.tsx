import { useEffect, useRef, useState, useMemo } from "react";
import type { DaemonSetSummary } from "@/lib/api/types";
import { formatAge } from "@/lib/monitorUtils";
import { Pencil, Loader2, Search, X, RefreshCw, RotateCw, Trash2, ArrowUpDown, ChevronUp, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 10000;

// SEL(fixed) | NAME/NS | READY | DESIRED | AVAILABLE | AGE | EDIT(fixed)
const INITIAL_WIDTHS = [28, 200, 80, 80, 90, 70, 28];

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

type DSSortKey = "name" | "ready" | "desired" | "available" | "age";

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: DSSortKey; sortKey: DSSortKey | null; sortDir: "asc" | "desc"; onSort: (k: DSSortKey) => void;
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

interface DaemonSetMonitorTableProps {
  daemonsets: DaemonSetSummary[];
  loading: boolean;
  headerLabel: string;
  onOpenEditor: (ds: DaemonSetSummary) => void;
  onRequestRefresh: () => void;
}

export const DaemonSetMonitorTable = ({
  daemonsets,
  loading,
  headerLabel,
  onOpenEditor,
  onRequestRefresh,
}: DaemonSetMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey] = useState<DSSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"delete" | "restart" | null>(null);
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

  useEffect(() => { setLastUpdated(new Date()); }, [daemonsets]);

  useEffect(() => {
    setSelectedKeys((prev) => {
      if (prev.size === 0) return prev;
      const keys = new Set(daemonsets.map((d) => `${d.namespace}/${d.name}`));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [daemonsets]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") { e.preventDefault(); setSelectedKeys(new Set()); }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const handleSort = (key: DSSortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else { setSortKey(key); setSortDir("asc"); }
  };

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    daemonsets.forEach((d) => { if (d.namespace) s.add(d.namespace); });
    return Array.from(s).sort();
  }, [daemonsets]);

  const filtered = useMemo(() => {
    let result = daemonsets;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((d) =>
        d.name.toLowerCase().includes(q) || (d.namespace ?? "").toLowerCase().includes(q)
      );
    }
    if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: number | string = 0, vb: number | string = 0;
        switch (sortKey) {
          case "name":      va = a.name; vb = b.name; break;
          case "ready":     va = a.numberReady / Math.max(a.desiredNumberScheduled, 1); vb = b.numberReady / Math.max(b.desiredNumberScheduled, 1); break;
          case "desired":   va = a.desiredNumberScheduled; vb = b.desiredNumberScheduled; break;
          case "available": va = a.numberAvailable; vb = b.numberAvailable; break;
          case "age":       va = a.updatedAt ? new Date(a.updatedAt).getTime() : 0; vb = b.updatedAt ? new Date(b.updatedAt).getTime() : 0; break;
        }
        if (typeof va === "string") return sortDir === "asc" ? va.localeCompare(vb as string) : (vb as string).localeCompare(va);
        return sortDir === "asc" ? (va as number) - (vb as number) : (vb as number) - (va as number);
      });
    }
    return result;
  }, [daemonsets, searchQuery, sortKey, sortDir]);

  const dsKey = (d: DaemonSetSummary) => `${d.namespace}/${d.name}`;
  const allSelected = filtered.length > 0 && filtered.every((d) => selectedKeys.has(dsKey(d)));
  const someSelected = filtered.some((d) => selectedKeys.has(dsKey(d)));

  const toggleAll = () => {
    if (allSelected) {
      const next = new Set(selectedKeys);
      filtered.forEach((d) => next.delete(dsKey(d)));
      setSelectedKeys(next);
    } else {
      const next = new Set(selectedKeys);
      filtered.forEach((d) => next.add(dsKey(d)));
      setSelectedKeys(next);
    }
  };

  const toggleDs = (d: DaemonSetSummary) => {
    const key = dsKey(d);
    const next = new Set(selectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedKeys(next);
  };

  const selectedObjects = useMemo(
    () => daemonsets.filter((d) => selectedKeys.has(dsKey(d))),
    [daemonsets, selectedKeys]
  );

  const executeBulkAction = async () => {
    if (!bulkAction || bulkProcessing || selectedObjects.length === 0) return;
    setBulkProcessing(true);
    let succeeded = 0, failed = 0;
    try {
      await Promise.all(
        selectedObjects.map(async (d) => {
          try {
            if (bulkAction === "restart") await apiClient.restartDaemonSet(d.cluster, d.namespace, d.name);
            else await apiClient.deleteDaemonSet(d.cluster, d.namespace, d.name);
            succeeded++;
          } catch { failed++; }
        })
      );
      const label = bulkAction === "restart" ? "reiniciado(s)" : "deletado(s)";
      if (succeeded > 0 && failed === 0) toast.success(`${succeeded} DaemonSet(s) ${label} com sucesso.`);
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
          {searchQuery && ` — ${filtered.length} resultado(s)`}
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

      {/* Column headers + rows (scroll together) */}
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
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="READY" colKey="ready" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="DESIRED" colKey="desired" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="AVAILABLE" colKey="available" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="AGE" colKey="age" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(5, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery ? "Nenhum DaemonSet encontrado para a busca" : "Nenhum DaemonSet encontrado"}
          </div>
        )}
        {filtered.map((ds, index) => {
          const isHealthy = ds.numberReady === ds.desiredNumberScheduled && ds.numberAvailable === ds.desiredNumberScheduled;
          const rowColor = isHealthy ? "text-green-600 dark:text-green-400" : "text-orange-600 dark:text-orange-400";
          const readyColor = ds.numberReady < ds.desiredNumberScheduled ? "text-orange-600 dark:text-orange-400" : "text-green-600 dark:text-green-400";
          const isSelected = selectedKeys.has(dsKey(ds));

          return (
            <div
              key={dsKey(ds)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenEditor(ds)}
              onKeyDown={(e) => {
                if (e.key === " ") { e.preventDefault(); toggleDs(ds); }
                else if (e.key === "ArrowDown") { e.preventDefault(); focusRow(index + 1); }
                else if (e.key === "ArrowUp") { e.preventDefault(); if (index === 0) searchInputRef.current?.focus(); else focusRow(index - 1); }
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              <span className="flex items-center" onClick={(e) => { e.stopPropagation(); toggleDs(ds); }}>
                <Checkbox checked={isSelected} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{ds.namespace}</span>
                )}
                <span className="truncate block" title={`${ds.namespace}/${ds.name}`}>{ds.name}</span>
              </span>
              <span className={readyColor}>{ds.numberReady}/{ds.desiredNumberScheduled}</span>
              <span>{ds.desiredNumberScheduled}</span>
              <span>{ds.numberAvailable}</span>
              <span className="text-muted-foreground">{ds.updatedAt ? formatAge(ds.updatedAt) : "-"}</span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(ds); }}
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
          {bulkAction && (
            <div className={`flex items-center gap-2 px-3 py-2 text-xs ${bulkAction === "delete" ? "bg-destructive/10 border-b border-destructive/30" : "bg-blue-500/10 border-b border-blue-500/30"}`}>
              <span className="flex-1">
                {bulkAction === "restart"
                  ? `Reiniciar ${selectedObjects.length} DaemonSet(s) via rollout restart?`
                  : `Deletar ${selectedObjects.length} DaemonSet(s) permanentemente?`}
              </span>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setBulkAction(null)} disabled={bulkProcessing}>Cancelar</Button>
              <Button
                size="sm"
                className={`h-6 px-3 text-xs gap-1 text-white ${bulkAction === "delete" ? "bg-destructive hover:bg-destructive/90" : "bg-blue-600 hover:bg-blue-700"}`}
                onClick={executeBulkAction}
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
                <span className="font-medium text-foreground">{selectedKeys.size}</span> DaemonSet(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-blue-500 border-blue-500/40 hover:bg-blue-500/10 hover:border-blue-500 gap-1" onClick={() => setBulkAction("restart")}>
                  <RotateCw className="w-3 h-3" /> Restart ({selectedKeys.size})
                </Button>
              </ProtectedAction>
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
