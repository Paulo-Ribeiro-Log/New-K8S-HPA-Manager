import { useEffect, useRef, useState, useMemo } from "react";
import type { DeploymentSummary } from "@/lib/api/types";
import { formatAge } from "@/lib/monitorUtils";
import { Pencil, Loader2, ChevronLeft, Search, X, ListFilter, Check, RefreshCw, RotateCw, Trash2, ArrowUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";

const REFRESH_INTERVAL_MS = 10000;

// Colunas: SEL | NAME/NS | READY | UP-TO-DATE | AVAILABLE | AGE | EDIT
const GRID = "28px 1fr 80px 90px 80px 64px 28px";

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

interface DeploymentMonitorTableProps {
  deployments: DeploymentSummary[];
  loading: boolean;
  headerLabel: string;
  onSelectDeployment: (dep: DeploymentSummary) => void;
  onOpenEditor: (dep: DeploymentSummary) => void;
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
          className={`flex items-center gap-0.5 uppercase hover:text-foreground transition-colors ${
            active ? "text-primary" : "text-muted-foreground"
          }`}
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
      <PopoverContent className="w-44 p-2" align="start">
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs font-medium">{label}</span>
          {active && (
            <button
              onClick={() => onChange(new Set())}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              Limpar
            </button>
          )}
        </div>
        <div className="space-y-1">
          {options.map((opt) => (
            <label
              key={opt}
              className="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-muted/50 text-xs"
            >
              <Checkbox
                checked={selected.has(opt)}
                onCheckedChange={() => toggle(opt)}
                className="w-3.5 h-3.5"
              />
              <span className="truncate flex-1">{opt}</span>
              {selected.has(opt) && <Check className="w-3 h-3 text-primary flex-shrink-0" />}
            </label>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export const DeploymentMonitorTable = ({
  deployments,
  loading,
  headerLabel,
  onSelectDeployment,
  onOpenEditor,
  onRequestRefresh,
  onBack,
  backLabel,
}: DeploymentMonitorTableProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<Set<string>>(new Set());
  const [namespaceFilter, setNamespaceFilter] = useState<Set<string>>(new Set());
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Seleção em lote
  const [selectedDeps, setSelectedDeps] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<"delete" | "restart" | "scale" | null>(null);
  const [bulkProcessing, setBulkProcessing] = useState(false);
  const [scaleReplicasValue, setScaleReplicasValue] = useState(1);

  // Ref estável — evita recriar o interval a cada render
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
    const id = setInterval(() => {
      setRefreshing(true);
      refreshRef.current();
      setTimeout(() => setRefreshing(false), 600);
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    setLastUpdated(new Date());
  }, [deployments]);

  // Limpar seleção de deployments que sumiram
  useEffect(() => {
    setSelectedDeps((prev) => {
      if (prev.size === 0) return prev;
      const keys = new Set(deployments.map((d) => `${d.namespace}/${d.name}`));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size !== prev.size ? next : prev;
    });
  }, [deployments]);

  // Ctrl+\ desmarca todos
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "\\") {
        e.preventDefault();
        setSelectedDeps(new Set());
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const lastUpdatedLabel = useSecondsTick(lastUpdated);

  // "Saudável" / "Degradado" como opções de status
  const uniqueStatuses = ["Saudável", "Degradado"];

  const uniqueNamespaces = useMemo(() => {
    const s = new Set<string>();
    deployments.forEach((d) => { if (d.namespace) s.add(d.namespace); });
    return Array.from(s).sort();
  }, [deployments]);

  const hasFilters = statusFilter.size > 0 || namespaceFilter.size > 0;

  const clearAllFilters = () => {
    setStatusFilter(new Set());
    setNamespaceFilter(new Set());
    setSearchQuery("");
  };

  const filtered = useMemo(() => {
    let result = deployments;

    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((d) =>
        d.name.toLowerCase().includes(q) ||
        (d.namespace ?? "").toLowerCase().includes(q)
      );
    }

    if (statusFilter.size > 0) {
      result = result.filter((d) => {
        const ready = d.readyReplicas ?? 0;
        const desired = d.replicas ?? 0;
        const available = d.availableReplicas ?? 0;
        const isHealthy = ready === desired && available > 0;
        return statusFilter.has(isHealthy ? "Saudável" : "Degradado");
      });
    }

    if (namespaceFilter.size > 0) {
      result = result.filter((d) => namespaceFilter.has(d.namespace ?? ""));
    }

    return result;
  }, [deployments, searchQuery, statusFilter, namespaceFilter]);

  const activeFilterCount = statusFilter.size + namespaceFilter.size;

  // Helpers de seleção
  const depKey = (d: DeploymentSummary) => `${d.namespace}/${d.name}`;
  const allFilteredSelected = filtered.length > 0 && filtered.every((d) => selectedDeps.has(depKey(d)));
  const someFilteredSelected = filtered.some((d) => selectedDeps.has(depKey(d)));

  const toggleSelectAll = () => {
    if (allFilteredSelected) {
      const next = new Set(selectedDeps);
      filtered.forEach((d) => next.delete(depKey(d)));
      setSelectedDeps(next);
    } else {
      const next = new Set(selectedDeps);
      filtered.forEach((d) => next.add(depKey(d)));
      setSelectedDeps(next);
    }
  };

  const toggleDep = (d: DeploymentSummary) => {
    const key = depKey(d);
    const next = new Set(selectedDeps);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setSelectedDeps(next);
  };

  const selectedDepObjects = useMemo(
    () => deployments.filter((d) => selectedDeps.has(depKey(d))),
    [deployments, selectedDeps]
  );

  // Executa ação em massa
  const executeBulkAction = async () => {
    if (!bulkAction || bulkProcessing || selectedDepObjects.length === 0) return;
    setBulkProcessing(true);

    try {
      if (bulkAction === "delete" || bulkAction === "restart") {
        // Agrupar por cluster (normalmente um único cluster)
        const byCluster = new Map<string, Array<{ namespace: string; name: string }>>();
        for (const dep of selectedDepObjects) {
          const list = byCluster.get(dep.cluster) ?? [];
          list.push({ namespace: dep.namespace, name: dep.name });
          byCluster.set(dep.cluster, list);
        }

        let successCount = 0;
        let failedCount = 0;

        for (const [cluster, deps] of byCluster) {
          const result = bulkAction === "delete"
            ? await apiClient.batchDeleteDeployments(cluster, deps)
            : await apiClient.batchRestartDeployments(cluster, deps);
          successCount += result.success_count;
          failedCount += result.failed_count;
        }

        const actionLabel = bulkAction === "delete" ? "Delete" : "Rollout Restart";
        const successMsg = bulkAction === "delete" ? "deletado(s)" : "reiniciado(s)";

        if (successCount > 0 && failedCount === 0) {
          toast.success(`${actionLabel} concluído`, {
            description: `${successCount} deployment(s) ${successMsg} com sucesso.`,
          });
        } else if (successCount > 0 && failedCount > 0) {
          toast.warning("Parcialmente concluído", {
            description: `${successCount} sucesso(s), ${failedCount} falha(s).`,
          });
        } else {
          toast.error("Falha na operação", {
            description: `Não foi possível executar ${actionLabel}.`,
          });
        }
      } else if (bulkAction === "scale") {
        let succeeded = 0;
        let failed = 0;
        for (const dep of selectedDepObjects) {
          try {
            await apiClient.scaleDeployment(dep.cluster, dep.namespace, dep.name, scaleReplicasValue);
            succeeded++;
          } catch {
            failed++;
          }
        }

        if (succeeded > 0 && failed === 0) {
          toast.success("Scale concluído", {
            description: `${succeeded} deployment(s) escalado(s) para ${scaleReplicasValue} réplica(s).`,
          });
        } else if (succeeded > 0 && failed > 0) {
          toast.warning("Parcialmente concluído", {
            description: `${succeeded} sucesso(s), ${failed} falha(s).`,
          });
        } else {
          toast.error("Falha no scale");
        }
      }
    } catch (err) {
      toast.error("Erro na operação em lote", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setBulkProcessing(false);
      setBulkAction(null);
      setSelectedDeps(new Set());
      onRequestRefresh();
    }
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
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground gap-1"
            onClick={clearAllFilters}
            title="Limpar todos os filtros"
          >
            <X className="w-3 h-3" />
            {activeFilterCount > 0 && (
              <Badge variant="secondary" className="text-[10px] h-4 px-1">
                {activeFilterCount}
              </Badge>
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
            <button
              onClick={() => setSearchQuery("")}
              className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      </div>

      {/* Chips de filtros ativos */}
      {hasFilters && (
        <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 flex-shrink-0 flex-wrap">
          {Array.from(statusFilter).map((v) => (
            <Badge
              key={`s-${v}`}
              variant="secondary"
              className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(statusFilter); n.delete(v); setStatusFilter(n); }}
            >
              status: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
          {Array.from(namespaceFilter).map((v) => (
            <Badge
              key={`ns-${v}`}
              variant="secondary"
              className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
              onClick={() => { const n = new Set(namespaceFilter); n.delete(v); setNamespaceFilter(n); }}
            >
              ns: {v} <X className="w-2.5 h-2.5" />
            </Badge>
          ))}
        </div>
      )}

      {/* Column headers */}
      <div
        className="grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20 flex-shrink-0"
        style={{ gridTemplateColumns: GRID }}
      >
        {/* Select-all */}
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
        {/* NAME / NAMESPACE */}
        <span>
          {uniqueNamespaces.length > 1 ? (
            <ColumnFilter
              label="NAME/NS"
              options={uniqueNamespaces}
              selected={namespaceFilter}
              onChange={setNamespaceFilter}
            />
          ) : (
            <span className="text-muted-foreground uppercase">NAME</span>
          )}
        </span>
        {/* READY com filtro */}
        <span>
          <ColumnFilter
            label="READY"
            options={uniqueStatuses}
            selected={statusFilter}
            onChange={setStatusFilter}
          />
        </span>
        <span className="text-muted-foreground uppercase">UP-TO-DATE</span>
        <span className="text-muted-foreground uppercase">AVAILABLE</span>
        <span className="text-muted-foreground uppercase">AGE</span>
        <span></span>
      </div>

      {/* Rows */}
      <div ref={rowsContainerRef} className="flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters
              ? "Nenhum deployment encontrado para os filtros aplicados"
              : "Nenhum deployment encontrado"}
          </div>
        )}
        {filtered.map((dep, index) => {
          const ready = dep.readyReplicas ?? 0;
          const desired = dep.replicas ?? 0;
          const available = dep.availableReplicas ?? 0;
          const updated = dep.updatedReplicas ?? 0;
          const isHealthy = ready === desired && available > 0;
          const rowColor = isHealthy
            ? "text-green-600 dark:text-green-400"
            : "text-orange-600 dark:text-orange-400";
          const readyColor = ready < desired
            ? "text-orange-600 dark:text-orange-400"
            : "text-green-600 dark:text-green-400";
          const isSelected = selectedDeps.has(depKey(dep));

          return (
            <button
              key={`${dep.namespace}/${dep.name}`}
              data-row-index={index}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor} cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
              style={{ gridTemplateColumns: GRID }}
              onClick={() => onSelectDeployment(dep)}
              onKeyDown={(e) => {
                if (e.key === " ") {
                  e.preventDefault();
                  toggleDep(dep);
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
                // Enter aciona onClick naturalmente (abre detalhe)
              }}
              title="Enter para detalhes • Espaço para selecionar • ↑↓ para navegar"
            >
              {/* Checkbox — clique no span faz toggle sem propagar para onSelectDeployment */}
              <span
                className="flex items-center"
                onClick={(e) => { e.stopPropagation(); toggleDep(dep); }}
              >
                <Checkbox
                  checked={isSelected}
                  onCheckedChange={() => {}}
                  className="w-3.5 h-3.5 rounded-full pointer-events-none"
                />
              </span>
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{dep.namespace}</span>
                )}
                <span className="truncate block" title={`${dep.namespace}/${dep.name}`}>{dep.name}</span>
              </span>
              <span className={readyColor}>{ready}/{desired}</span>
              <span>{updated}</span>
              <span>{available}</span>
              <span className="text-muted-foreground">
                {dep.updatedAt ? formatAge(dep.updatedAt) : "-"}
              </span>
              <span className="flex items-center justify-center">
                <span
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(dep); }}
                  title="Abrir editor YAML"
                >
                  <Pencil className="w-3 h-3" />
                </span>
              </span>
            </button>
          );
        })}
      </div>

      {/* Barra de ações em massa — visível quando há seleção */}
      {selectedDeps.size > 0 && (
        <div className="flex-shrink-0 border-t border-border">
          {/* Confirmação da ação */}
          {bulkAction && (
            <div className={`flex items-center gap-2 px-3 py-2 text-xs ${
              bulkAction === "delete"
                ? "bg-destructive/10 border-b border-destructive/30"
                : bulkAction === "restart"
                ? "bg-blue-500/10 border-b border-blue-500/30"
                : "bg-amber-500/10 border-b border-amber-500/30"
            }`}>
              {bulkAction === "scale" ? (
                <span className="flex items-center gap-2 flex-1">
                  Escalar {selectedDepObjects.length} deployment(s) para
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={scaleReplicasValue}
                    onChange={(e) => setScaleReplicasValue(Math.max(0, parseInt(e.target.value) || 0))}
                    className="h-6 w-16 text-xs px-2"
                  />
                  réplica(s)?
                </span>
              ) : (
                <span className="flex-1">
                  {bulkAction === "restart"
                    ? `Reiniciar ${selectedDepObjects.length} deployment(s) via rollout restart?`
                    : `Deletar ${selectedDepObjects.length} deployment(s) permanentemente?`}
                </span>
              )}
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
                    : "bg-amber-600 hover:bg-amber-700"
                }`}
                onClick={executeBulkAction}
                disabled={bulkProcessing}
              >
                {bulkProcessing ? <Loader2 className="w-3 h-3 animate-spin" /> : "Confirmar"}
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
                onClick={() => setSelectedDeps(new Set())}
                title="Desmarcar todos (Ctrl+\)"
              >
                Desmarcar tudo
              </Button>
              <span className="text-xs text-muted-foreground">
                <span className="font-medium text-foreground">{selectedDeps.size}</span> deployment(s) selecionado(s)
              </span>
              <div className="flex-1" />
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-amber-500 border-amber-500/40 hover:bg-amber-500/10 hover:border-amber-500 gap-1"
                  onClick={() => setBulkAction("scale")}
                  title="Escalar deployments selecionados"
                >
                  <ArrowUpDown className="w-3 h-3" />
                  Scale ({selectedDeps.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-blue-500 border-blue-500/40 hover:bg-blue-500/10 hover:border-blue-500 gap-1"
                  onClick={() => setBulkAction("restart")}
                  title="Rollout restart nos deployments selecionados"
                >
                  <RotateCw className="w-3 h-3" />
                  Restart ({selectedDeps.size})
                </Button>
              </ProtectedAction>
              <ProtectedAction showWarning={false}>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs text-destructive border-destructive/40 hover:bg-destructive/10 hover:border-destructive gap-1"
                  onClick={() => setBulkAction("delete")}
                  title="Deletar deployments selecionados"
                >
                  <Trash2 className="w-3 h-3" />
                  Deletar ({selectedDeps.size})
                </Button>
              </ProtectedAction>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
