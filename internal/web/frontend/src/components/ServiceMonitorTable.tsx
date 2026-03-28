import { useEffect, useRef, useState, useMemo } from "react";
import type { ServiceSummary } from "@/lib/api/types";
import { Pencil, Loader2, Search, X, RefreshCw, Trash2, ArrowUpDown, ChevronUp, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

const REFRESH_INTERVAL_MS = 10000;

// SEL(fixed) | NAME/NS | TYPE | CLUSTER-IP | PORTS | EDIT(fixed)
const INITIAL_WIDTHS = [28, 200, 90, 160, 170, 28];

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

function serviceTypeColor(type: string): string {
  switch (type) {
    case "LoadBalancer": return "text-green-400";
    case "NodePort":     return "text-yellow-400";
    case "ExternalName": return "text-purple-400";
    default:             return "text-muted-foreground";
  }
}

type SvcSortKey = "name" | "type";

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: SvcSortKey; sortKey: SvcSortKey | null; sortDir: "asc" | "desc"; onSort: (k: SvcSortKey) => void;
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

interface ServiceMonitorTableProps {
  services: ServiceSummary[];
  loading: boolean;
  headerLabel: string;
  onOpenEditor: (svc: ServiceSummary) => void;
  onRequestRefresh: () => void;
}

export const ServiceMonitorTable = ({
  services,
  loading,
  headerLabel,
  onOpenEditor,
  onRequestRefresh,
}: ServiceMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey] = useState<SvcSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
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

  useEffect(() => { setLastUpdated(new Date()); }, [services]);

  useEffect(() => {
    setSelectedKeys((prev) => {
      if (prev.size === 0) return prev;
      const keys = new Set(services.map((s) => `${s.cluster}/${s.namespace}/${s.name}`));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [services]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") { e.preventDefault(); setSelectedKeys(new Set()); }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const handleSort = (key: SvcSortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else { setSortKey(key); setSortDir("asc"); }
  };

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    services.forEach((d) => { if (d.namespace) s.add(d.namespace); });
    return Array.from(s).sort();
  }, [services]);

  const filtered = useMemo(() => {
    let result = services;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((s) =>
        s.name.toLowerCase().includes(q) ||
        s.namespace.toLowerCase().includes(q) ||
        s.type.toLowerCase().includes(q) ||
        (s.clusterIP ?? "").toLowerCase().includes(q)
      );
    }
    if (sortKey) {
      result = [...result].sort((a, b) => {
        const va = sortKey === "type" ? a.type : a.name;
        const vb = sortKey === "type" ? b.type : b.name;
        return sortDir === "asc" ? va.localeCompare(vb) : vb.localeCompare(va);
      });
    }
    return result;
  }, [services, searchQuery, sortKey, sortDir]);

  const svcKey = (s: ServiceSummary) => `${s.cluster}/${s.namespace}/${s.name}`;
  const allSelected = filtered.length > 0 && filtered.every((s) => selectedKeys.has(svcKey(s)));
  const someSelected = filtered.some((s) => selectedKeys.has(svcKey(s)));

  const toggleAll = () => {
    if (allSelected) {
      const next = new Set(selectedKeys);
      filtered.forEach((s) => next.delete(svcKey(s)));
      setSelectedKeys(next);
    } else {
      const next = new Set(selectedKeys);
      filtered.forEach((s) => next.add(svcKey(s)));
      setSelectedKeys(next);
    }
  };

  const toggleSvc = (s: ServiceSummary) => {
    const key = svcKey(s);
    const next = new Set(selectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedKeys(next);
  };

  const selectedObjects = useMemo(
    () => services.filter((s) => selectedKeys.has(svcKey(s))),
    [services, selectedKeys]
  );

  const executeBulkDelete = async () => {
    if (bulkProcessing || selectedObjects.length === 0) return;
    setBulkProcessing(true);
    let succeeded = 0, failed = 0;
    try {
      await Promise.all(
        selectedObjects.map(async (s) => {
          try {
            await apiClient.deleteService(s.cluster, s.namespace, s.name);
            succeeded++;
          } catch { failed++; }
        })
      );
      if (succeeded > 0 && failed === 0) toast.success(`${succeeded} Service(s) deletado(s) com sucesso.`);
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
            <SortBtn label="TYPE" colKey="type" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-3">
            CLUSTER-IP
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-3">
            PORTS
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery ? "Nenhum Service encontrado para a busca" : "Nenhum Service encontrado"}
          </div>
        )}
        {filtered.map((svc, index) => {
          const hasExternal = !!svc.externalIP;
          const isSelected = selectedKeys.has(svcKey(svc));

          return (
            <div
              key={svcKey(svc)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs text-foreground cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenEditor(svc)}
              onKeyDown={(e) => {
                if (e.key === " ") { e.preventDefault(); toggleSvc(svc); }
                else if (e.key === "Enter") onOpenEditor(svc);
                else if (e.key === "ArrowDown") { e.preventDefault(); focusRow(index + 1); }
                else if (e.key === "ArrowUp") { e.preventDefault(); if (index === 0) searchInputRef.current?.focus(); else focusRow(index - 1); }
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              <span className="flex items-center" onClick={(e) => { e.stopPropagation(); toggleSvc(svc); }}>
                <Checkbox checked={isSelected} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{svc.namespace}</span>
                )}
                <span className="truncate block" title={`${svc.namespace}/${svc.name}`}>{svc.name}</span>
              </span>
              <span className={`text-[11px] font-medium ${serviceTypeColor(svc.type)}`}>{svc.type}</span>
              <span className="text-muted-foreground truncate">
                {svc.clusterIP && svc.clusterIP !== "None" ? svc.clusterIP : "—"}
                {hasExternal && <span className="text-green-400 ml-1">↗{svc.externalIP}</span>}
              </span>
              <span className="text-muted-foreground truncate">
                {svc.ports?.slice(0, 2).join(", ")}
                {(svc.ports?.length ?? 0) > 2 && <span className="text-muted-foreground/60"> +{svc.ports.length - 2}</span>}
              </span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(svc); }}
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
              <span className="flex-1">Deletar {selectedObjects.length} Service(s) permanentemente?</span>
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
                <span className="font-medium text-foreground">{selectedKeys.size}</span> Service(s) selecionado(s)
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
