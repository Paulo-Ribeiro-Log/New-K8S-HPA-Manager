import { useMemo, useState } from "react";
import type { PrometheusResource } from "@/lib/api/types";
import { Pencil, Search, X, ArrowUpDown, ChevronUp, ChevronDown, RotateCw, Loader2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";

// SEL(fixed) | NAME/NS | TYPE | COMPONENT | REP. | CPU REQ | MEM REQ | EDIT(fixed)
const INITIAL_WIDTHS = [28, 200, 95, 140, 55, 85, 95, 28];

type PromSortKey = "name" | "type" | "component" | "replicas";

function SortBtn({ label, colKey, sortKey, sortDir, onSort }: {
  label: string; colKey: PromSortKey; sortKey: PromSortKey | null; sortDir: "asc" | "desc"; onSort: (k: PromSortKey) => void;
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

interface PrometheusMonitorTableProps {
  cluster: string;
  resources: PrometheusResource[];
  headerLabel: string;
  onOpenEditor: (res: PrometheusResource) => void;
}

export const PrometheusMonitorTable = ({
  cluster,
  resources,
  headerLabel,
  onOpenEditor,
}: PrometheusMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey] = useState<PromSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const { resize, gridTemplate } = useResizableColumns(INITIAL_WIDTHS);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"restart" | null>(null);
  const [bulkProcessing, setBulkProcessing] = useState(false);

  const handleSort = (key: PromSortKey) => {
    if (sortKey === key) {
      if (sortDir === "asc") setSortDir("desc");
      else { setSortKey(null); setSortDir("asc"); }
    } else { setSortKey(key); setSortDir("asc"); }
  };

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    resources.forEach((r) => { if (r.namespace) s.add(r.namespace); });
    return Array.from(s).sort();
  }, [resources]);

  const filtered = useMemo(() => {
    let result = resources;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((r) =>
        r.name.toLowerCase().includes(q) ||
        r.namespace.toLowerCase().includes(q) ||
        r.component.toLowerCase().includes(q) ||
        r.type.toLowerCase().includes(q)
      );
    }
    if (sortKey) {
      result = [...result].sort((a, b) => {
        let va: number | string = 0, vb: number | string = 0;
        switch (sortKey) {
          case "name":      va = a.name; vb = b.name; break;
          case "type":      va = a.type; vb = b.type; break;
          case "component": va = a.component; vb = b.component; break;
          case "replicas":  va = a.replicas; vb = b.replicas; break;
        }
        if (typeof va === "string") return sortDir === "asc" ? va.localeCompare(vb as string) : (vb as string).localeCompare(va);
        return sortDir === "asc" ? (va as number) - (vb as number) : (vb as number) - (va as number);
      });
    }
    return result;
  }, [resources, searchQuery, sortKey, sortDir]);

  const resKey = (r: PrometheusResource) => `${r.type}/${r.namespace}/${r.name}`;
  const allSelected = filtered.length > 0 && filtered.every((r) => selectedKeys.has(resKey(r)));
  const someSelected = filtered.some((r) => selectedKeys.has(resKey(r)));

  const toggleAll = () => {
    if (allSelected) {
      const next = new Set(selectedKeys);
      filtered.forEach((r) => next.delete(resKey(r)));
      setSelectedKeys(next);
    } else {
      const next = new Set(selectedKeys);
      filtered.forEach((r) => next.add(resKey(r)));
      setSelectedKeys(next);
    }
  };

  const toggleRes = (r: PrometheusResource) => {
    const key = resKey(r);
    const next = new Set(selectedKeys);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedKeys(next);
  };

  const selectedObjects = useMemo(
    () => resources.filter((r) => selectedKeys.has(resKey(r))),
    [resources, selectedKeys]
  );

  const executeBulkRestart = async () => {
    if (bulkProcessing || selectedObjects.length === 0) return;
    setBulkProcessing(true);
    let succeeded = 0, failed = 0;
    try {
      const deployments = selectedObjects.filter((r) => r.type === "Deployment").map((r) => ({ namespace: r.namespace, name: r.name }));
      const statefulsets = selectedObjects.filter((r) => r.type === "StatefulSet");
      const daemonsets = selectedObjects.filter((r) => r.type === "DaemonSet");

      const results = await Promise.allSettled([
        ...(deployments.length > 0
          ? [apiClient.batchRestartDeployments(cluster, deployments).then((r) => { succeeded += r.success_count; failed += r.failed_count; })]
          : []),
        ...statefulsets.map((r) => apiClient.restartStatefulSet(cluster, r.namespace, r.name).then(() => succeeded++).catch(() => failed++)),
        ...daemonsets.map((r) => apiClient.restartDaemonSet(cluster, r.namespace, r.name).then(() => succeeded++).catch(() => failed++)),
      ]);
      void results;
      if (succeeded > 0 && failed === 0) toast.success(`${succeeded} recurso(s) reiniciado(s) com sucesso.`);
      else if (succeeded > 0) toast.warning(`${succeeded} sucesso(s), ${failed} falha(s).`);
      else toast.error("Falha na operação em lote.");
    } catch (err) {
      toast.error("Erro na operação em lote", { description: err instanceof Error ? err.message : "Erro desconhecido" });
    } finally {
      setBulkProcessing(false);
      setBulkAction(null);
      setSelectedKeys(new Set());
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
        <div className="flex-1" />
        <div className="relative w-44">
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

      {/* Column headers + rows (scroll together) */}
      <div className="flex-1 overflow-auto">
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
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="NAME/NS" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(1, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="TYPE" colKey="type" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="COMPONENT" colKey="component" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className="relative overflow-hidden pr-3">
            <SortBtn label="REP." colKey="replicas" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-3">
            CPU REQ
            <ResizeHandle onResize={(d) => resize(5, d)} />
          </span>
          <span className="relative uppercase text-muted-foreground overflow-hidden pr-3">
            MEM REQ
            <ResizeHandle onResize={(d) => resize(6, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery ? "Nenhum recurso encontrado para a busca" : "Nenhum recurso Prometheus encontrado"}
          </div>
        )}
        {filtered.map((res, index) => {
          const isSelected = selectedKeys.has(resKey(res));
          return (
            <div
              key={resKey(res)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs text-foreground cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: gridTemplate }}
              onClick={() => onOpenEditor(res)}
              onKeyDown={(e) => {
                if (e.key === " ") { e.preventDefault(); toggleRes(res); }
                else if (e.key === "Enter") onOpenEditor(res);
              }}
              title="Enter para detalhes • Espaço para selecionar"
            >
              <span className="flex items-center" onClick={(e) => { e.stopPropagation(); toggleRes(res); }}>
                <Checkbox checked={isSelected} onCheckedChange={() => {}} className="w-3.5 h-3.5 rounded-full pointer-events-none" />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{res.namespace}</span>
                )}
                <span className="truncate block" title={`${res.namespace}/${res.name}`}>{res.name}</span>
              </span>
              <span className="text-blue-400/80">{res.type}</span>
              <span className="text-muted-foreground truncate">{res.component}</span>
              <span className="text-center">{res.type === "DaemonSet" ? "—" : String(res.replicas)}</span>
              <span className="text-yellow-400/80 font-mono">{res.current_cpu_request || "—"}</span>
              <span className="text-purple-400/80 font-mono">{res.current_memory_request || "—"}</span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(res); }}
                  title="Editar recurso"
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
          {bulkAction === "restart" && (
            <div className="flex items-center gap-2 px-3 py-2 text-xs bg-blue-500/10 border-b border-blue-500/30">
              <span className="flex-1">Reiniciar {selectedObjects.length} recurso(s) via rollout restart?</span>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setBulkAction(null)} disabled={bulkProcessing}>Cancelar</Button>
              <Button
                size="sm"
                className="h-6 px-3 text-xs gap-1 text-white bg-blue-600 hover:bg-blue-700"
                onClick={executeBulkRestart}
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
                <span className="font-medium text-foreground">{selectedKeys.size}</span> recurso(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button variant="outline" size="sm" className="h-7 text-xs text-blue-500 border-blue-500/40 hover:bg-blue-500/10 hover:border-blue-500 gap-1" onClick={() => setBulkAction("restart")}>
                  <RotateCw className="w-3 h-3" /> Restart ({selectedKeys.size})
                </Button>
              </ProtectedAction>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
