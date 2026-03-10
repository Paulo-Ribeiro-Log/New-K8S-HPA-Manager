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
import { ProtectedAction } from "@/components/rbac/ProtectedAction";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

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

// Colunas: SEL | NAME/NS | dot | READY | STATUS | REST. | CPU | MEM | NODE | AGE
const GRID = "32px minmax(180px,1fr) 22px 56px 100px 50px 64px 72px minmax(130px,1fr) 56px";

export const PodMonitorTable = ({
  cluster,
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
          <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onBack}>
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
        style={{ gridTemplateColumns: GRID }}
      >
        {/* Select-all circular */}
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
      <div ref={rowsContainerRef} className="rows-container flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum pod encontrado para os filtros aplicados" : "Nenhum pod encontrado"}
          </div>
        )}
        {filtered.map((pod, index) => {
          const m = metrics?.pods[pod.name];
          const rowColor = podRowColor(pod.phase ?? "", pod.statusReason);
          const dotColor = podDotColor(pod.phase ?? "", pod.statusReason);
          const isSelected = selectedPods.has(podKey(pod));

          return (
            <button
              key={`${pod.namespace}/${pod.name}`}
              data-row-index={index}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: GRID }}
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
