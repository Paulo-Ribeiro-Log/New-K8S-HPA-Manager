import { useEffect, useRef, useState, useMemo } from "react";
import type { DeploymentSummary } from "@/lib/api/types";
import { formatAge } from "@/lib/monitorUtils";
import { Pencil, Loader2, ChevronLeft, Search, X, ListFilter, Check, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";

const REFRESH_INTERVAL_MS = 10000;

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

  // Ref estável — evita recriar o interval a cada render
  const refreshRef = useRef(onRequestRefresh);
  useEffect(() => { refreshRef.current = onRequestRefresh; }, [onRequestRefresh]);

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
            placeholder="Buscar..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-7 text-xs pl-6 pr-6"
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

      {/* Column headers com filtros */}
      <div
        className="grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20 flex-shrink-0"
        style={{ gridTemplateColumns: "1fr 80px 90px 80px 64px 28px" }}
      >
        {/* NAME / NAMESPACE com filtro */}
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
        {/* STATUS com filtro */}
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
      <div className="flex-1 overflow-auto">
        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters
              ? "Nenhum deployment encontrado para os filtros aplicados"
              : "Nenhum deployment encontrado"}
          </div>
        )}
        {filtered.map((dep) => {
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

          return (
            <button
              key={`${dep.namespace}/${dep.name}`}
              className={`grid w-full px-3 py-1.5 hover:bg-muted/40 text-left transition-colors border-b border-border/40 font-mono text-xs ${rowColor}`}
              style={{ gridTemplateColumns: "1fr 80px 90px 80px 64px 28px" }}
              onClick={() => onSelectDeployment(dep)}
            >
              <span className="truncate pr-1" title={`${dep.namespace}/${dep.name}`}>
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px]">{dep.namespace}/</span>
                )}
                {dep.name}
              </span>
              <span className={readyColor}>{ready}/{desired}</span>
              <span>{updated}</span>
              <span>{available}</span>
              <span className="text-muted-foreground">
                {dep.updatedAt ? formatAge(dep.updatedAt) : "-"}
              </span>
              <span className="flex items-center justify-center">
                <button
                  className="text-muted-foreground hover:text-foreground p-0.5 rounded"
                  onClick={(e) => { e.stopPropagation(); onOpenEditor(dep); }}
                  title="Abrir editor YAML"
                >
                  <Pencil className="w-3 h-3" />
                </button>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
};
