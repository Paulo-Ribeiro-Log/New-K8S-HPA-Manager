import { useEffect, useRef, useState, useMemo } from "react";
import type { CronJob } from "@/lib/api/types";
import { Pencil, Loader2, Search, X, RefreshCw, Play, Pause, ArrowUpDown, ChevronUp, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 10000;

// SEL(fixed) | NAME/NS | SCHEDULE | STATUS | ACTIVE | LAST RUN | EDIT(fixed)
const INITIAL_WIDTHS = [28, 400, 150, 85, 55, 130, 28];

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

type CJSortKey = "name" | "active" | "schedule";

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: CJSortKey; sortKey: CJSortKey | null; sortDir: "asc" | "desc"; onSort: (k: CJSortKey) => void;
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

interface CronJobMonitorTableProps {
  cluster: string;
  cronJobs: CronJob[];
  loading: boolean;
  headerLabel: string;
  onOpenEditor: (cj: CronJob) => void;
  onRequestRefresh: () => void;
}

export const CronJobMonitorTable = ({
  cluster,
  cronJobs,
  loading,
  headerLabel,
  onOpenEditor,
  onRequestRefresh,
}: CronJobMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey] = useState<CJSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"trigger" | "suspend" | "resume" | null>(null);
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

  useEffect(() => { setLastUpdated(new Date()); }, [cronJobs]);

  useEffect(() => {
    setSelectedKeys((prev) => {
      if (prev.size === 0) return prev;
      const keys = new Set(cronJobs.map((cj) => `${cj.namespace}/${cj.name}`));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [cronJobs]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") { e.preventDefault(); setSelectedKeys(new Set()); }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const handleSort = (key: CJSortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else { setSortKey(key); setSortDir("asc"); }
  };

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    cronJobs.forEach((d) => { if (d.namespace) s.add(d.namespace); });
    return Array.from(s).sort();
  }, [cronJobs]);

  const filtered = useMemo(() => {
    let result = cronJobs;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((cj) =>
        cj.name.toLowerCase().includes(q) ||
        cj.namespace.toLowerCase().includes(q) ||
        cj.schedule.toLowerCase().includes(q)
      );
    }
    if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: number | string = 0, vb: number | string = 0;
        switch (sortKey) {
          case "name":     va = a.name; vb = b.name; break;
          case "schedule": va = a.schedule; vb = b.schedule; break;
          case "active":   va = a.active_jobs; vb = b.active_jobs; break;
        }
        if (typeof va === "string") return sortDir === "asc" ? va.localeCompare(vb as string) : (vb as string).localeCompare(va);
        return sortDir === "asc" ? (va as number) - (vb as number) : (vb as number) - (va as number);
      });
    }
    return result;
  }, [cronJobs, searchQuery, sortKey, sortDir]);

  const cjKey = (cj: CronJob) => `${cj.namespace}/${cj.name}`;
  const allSelected = filtered.length > 0 && filtered.every((cj) => selectedKeys.has(cjKey(cj)));
  const someSelected = filtered.some((cj) => selectedKeys.has(cjKey(cj)));

  const toggleAll = () => {
    if (allSelected) {
      const next = new Set(selectedKeys);
      filtered.forEach((cj) => next.delete(cjKey(cj)));
      setSelectedKeys(next);
    } else {
      const next = new Set(selectedKeys);
      filtered.forEach((cj) => next.add(cjKey(cj)));
      setSelectedKeys(next);
    }
  };

  const toggleCj = (cj: CronJob) => {
    const key = cjKey(cj);
    const next = new Set(selectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedKeys(next);
  };

  const selectedObjects = useMemo(
    () => cronJobs.filter((cj) => selectedKeys.has(cjKey(cj))),
    [cronJobs, selectedKeys]
  );

  const executeBulkAction = async () => {
    if (!bulkAction || bulkProcessing || selectedObjects.length === 0) return;
    setBulkProcessing(true);
    let succeeded = 0, failed = 0;
    try {
      await Promise.all(
        selectedObjects.map(async (cj) => {
          try {
            if (bulkAction === "trigger") {
              await apiClient.triggerCronJob(cluster, cj.namespace, cj.name);
            } else {
              await apiClient.updateCronJob(cluster, cj.namespace, cj.name, { suspend: bulkAction === "suspend" });
            }
            succeeded++;
          } catch { failed++; }
        })
      );
      const label = bulkAction === "trigger" ? "disparado(s)" : bulkAction === "suspend" ? "suspenso(s)" : "ativado(s)";
      if (succeeded > 0 && failed === 0) toast.success(`${succeeded} CronJob(s) ${label} com sucesso.`);
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

  const bulkActionLabel = bulkAction === "trigger"
    ? `Disparar ${selectedObjects.length} CronJob(s) manualmente?`
    : bulkAction === "suspend"
    ? `Suspender ${selectedObjects.length} CronJob(s)?`
    : `Ativar ${selectedObjects.length} CronJob(s)?`;

  const bulkActionColor = bulkAction === "suspend" ? "bg-orange-500/10 border-orange-500/30" : "bg-blue-500/10 border-b border-blue-500/30";

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
          <span className="relative overflow-hidden pr-4">
            <SortBtn label="NAME/NS" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(1, d)} />
          </span>
          <span className="relative overflow-hidden pr-4">
            <SortBtn label="SCHEDULE" colKey="schedule" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-4">
            STATUS
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className="relative overflow-hidden pr-4">
            <SortBtn label="ACTIVE" colKey="active" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-4">
            ÚLTIMO RUN
            <ResizeHandle onResize={(d) => resize(5, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery ? "Nenhum CronJob encontrado para a busca" : "Nenhum CronJob encontrado"}
          </div>
        )}
        {filtered.map((cj, index) => {
          const isSuspended = cj.suspend === true;
          const hasFailed = cj.failed_jobs > 0;
          const rowColor = isSuspended
            ? "text-muted-foreground"
            : hasFailed
            ? "text-orange-400"
            : "text-green-400";
          const isSelected = selectedKeys.has(cjKey(cj));

          return (
            <div
              key={cjKey(cj)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${rowColor} ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenEditor(cj)}
              onKeyDown={(e) => {
                if (e.key === " ") { e.preventDefault(); toggleCj(cj); }
                else if (e.key === "Enter") onOpenEditor(cj);
                else if (e.key === "ArrowDown") { e.preventDefault(); focusRow(index + 1); }
                else if (e.key === "ArrowUp") { e.preventDefault(); if (index === 0) searchInputRef.current?.focus(); else focusRow(index - 1); }
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              <span className="flex items-center" onClick={(e) => { e.stopPropagation(); toggleCj(cj); }}>
                <Checkbox checked={isSelected} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{cj.namespace}</span>
                )}
                <span className="truncate block" title={`${cj.namespace}/${cj.name}`}>{cj.name}</span>
              </span>
              <span className="font-mono text-[10px] text-muted-foreground truncate">{cj.schedule}</span>
              <span>
                {isSuspended ? (
                  <span className="px-1 py-0.5 rounded text-[9px] bg-muted/60 text-muted-foreground">Suspenso</span>
                ) : hasFailed ? (
                  <span className="px-1 py-0.5 rounded text-[9px] bg-orange-500/20 text-orange-400">Falhou</span>
                ) : (
                  <span className="px-1 py-0.5 rounded text-[9px] bg-green-500/20 text-green-400">Ativo</span>
                )}
              </span>
              <span className={cj.active_jobs > 0 ? "text-blue-400" : ""}>{cj.active_jobs}</span>
              <span className="text-muted-foreground truncate text-[10px]">
                {cj.last_schedule_time || "—"}
              </span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(cj); }}
                  title="Abrir detalhes"
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
            <div className={`flex items-center gap-2 px-3 py-2 text-xs border-b ${bulkActionColor}`}>
              <span className="flex-1">{bulkActionLabel}</span>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setBulkAction(null)} disabled={bulkProcessing}>Cancelar</Button>
              <Button
                size="sm"
                className={`h-6 px-3 text-xs gap-1 text-white ${bulkAction === "suspend" ? "bg-orange-600 hover:bg-orange-700" : "bg-blue-600 hover:bg-blue-700"}`}
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
                <span className="font-medium text-foreground">{selectedKeys.size}</span> CronJob(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-blue-500 border-blue-500/40 hover:bg-blue-500/10 hover:border-blue-500 gap-1" onClick={() => setBulkAction("trigger")}>
                  <Play className="w-3 h-3" /> Trigger ({selectedKeys.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-orange-500 border-orange-500/40 hover:bg-orange-500/10 hover:border-orange-500 gap-1" onClick={() => setBulkAction("suspend")}>
                  <Pause className="w-3 h-3" /> Suspender ({selectedKeys.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-green-500 border-green-500/40 hover:bg-green-500/10 hover:border-green-500 gap-1" onClick={() => setBulkAction("resume")}>
                  <Play className="w-3 h-3" /> Ativar ({selectedKeys.size})
                </Button>
              </ProtectedAction>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
