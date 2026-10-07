import { useEffect, useRef, useState, useMemo } from "react";
import type { CronJob } from "@/lib/api/types";
import { Pencil, Loader2, Search, X, RefreshCw, Play, Pause, ArrowUpDown, ChevronUp, ChevronDown, ListFilter, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { useResizableColumns, ResizeHandle } from "@/lib/resizableColumns";
import { formatAge } from "@/lib/monitorUtils";

const REFRESH_INTERVAL_MS = 10000;

// SEL | NAME/NS | SCHEDULE | STATUS | PRÓXIMA | ÚLTIMA | ÚLT. JOB | ÚLT. SUCESSO | HISTÓRICO | ATIVOS | CONC. | AGE | EDIT
const INITIAL_WIDTHS = [28, 260, 190, 92, 110, 100, 130, 110, 84, 58, 70, 60, 28];

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

// ── Estado derivado do CronJob ─────────────────────────────────────────────────

// Status para o analista, em ordem de prioridade. "Falhou" vem do último Job real — antes vinha de
// failed_jobs > 0, que é o LIMITE de histórico (padrão 1) e marcava quase todo CronJob como falho.
type CJStatus = "Suspenso" | "Rodando" | "Atrasado" | "Falhou" | "OK";
function cronJobStatus(cj: CronJob): CJStatus {
  if (cj.suspend === true) return "Suspenso";
  if (cj.active_jobs > 0) return "Rodando";
  if (cj.missed) return "Atrasado";
  if (cj.last_job?.status === "Failed") return "Falhou";
  return "OK";
}
const STATUS_STYLE: Record<CJStatus, { row: string; badge: string; title: string }> = {
  Suspenso: { row: "text-muted-foreground", badge: "bg-muted/60 text-muted-foreground", title: "spec.suspend = true: não dispara novas execuções" },
  Rodando: { row: "text-blue-600 dark:text-blue-400", badge: "bg-blue-500/20 text-blue-600 dark:text-blue-400", title: "Há Job em execução agora" },
  Atrasado: { row: "text-orange-600 dark:text-orange-400", badge: "bg-orange-500/20 text-orange-600 dark:text-orange-400", title: "A execução esperada já passou e nada rodou (controller parado, startingDeadline estourado, Forbid com job preso...)" },
  Falhou: { row: "text-red-600 dark:text-red-400", badge: "bg-red-500/20 text-red-600 dark:text-red-400", title: "O último Job terminou com falha" },
  OK: { row: "text-green-600 dark:text-green-400", badge: "bg-green-500/20 text-green-600 dark:text-green-400", title: "Ativo; o último Job não falhou" },
};

const toMs = (iso?: string) => (iso ? new Date(iso).getTime() || 0 : 0);
const fmtLocal = (iso?: string) => (iso ? new Date(iso).toLocaleString("pt-BR") : "");

// "em 12m", "em 3h20m" — mesmo formato curto do formatAge, para o futuro.
function formatIn(iso?: string): string {
  if (!iso) return "—";
  const secs = Math.round((toMs(iso) - Date.now()) / 1000);
  if (secs <= 0) return "agora";
  return `em ${formatAge(new Date(Date.now() - secs * 1000))}`;
}
const formatAgo = (iso?: string) => (iso ? `há ${formatAge(iso)}` : "—");

function formatDuration(secs: number): string {
  if (secs < 60) return `${secs}s`;
  return formatAge(new Date(Date.now() - secs * 1000));
}

type CJSortKey = "name" | "schedule" | "status" | "next" | "last" | "lastJob" | "lastSuccess" | "history" | "active" | "age";

const STATUS_ORDER: Record<CJStatus, number> = { Falhou: 0, Atrasado: 1, Rodando: 2, OK: 3, Suspenso: 4 };

function sortValue(cj: CronJob, k: CJSortKey): number | string {
  switch (k) {
    case "name": return cj.name;
    case "schedule": return cj.schedule;
    case "status": return STATUS_ORDER[cronJobStatus(cj)];
    case "next": return toMs(cj.next_schedule_time) || Number.MAX_SAFE_INTEGER;
    case "last": return toMs(cj.last_schedule_at);
    case "lastJob": return cj.last_job?.duration_seconds ?? -1;
    case "lastSuccess": return toMs(cj.last_successful_time);
    case "history": return cj.history_failed ?? 0;
    case "active": return cj.active_jobs;
    case "age": return toMs(cj.created_at);
  }
}

function SortBtn({ label, title, colKey, sortKey, sortDir, onSort }: {
  label: string; title?: string; colKey: CJSortKey; sortKey: CJSortKey | null; sortDir: "asc" | "desc"; onSort: (k: CJSortKey) => void;
}) {
  const active = sortKey === colKey;
  return (
    <button
      onClick={() => onSort(colKey)}
      title={title}
      className={`flex items-center gap-0.5 uppercase transition-colors ${active ? "text-primary hover:text-primary/80" : "text-muted-foreground hover:text-foreground"}`}
    >
      {label}
      {active && sortDir === "asc" ? <ChevronUp className="w-2.5 h-2.5" /> : active && sortDir === "desc" ? <ChevronDown className="w-2.5 h-2.5" /> : <ArrowUpDown className="w-2.5 h-2.5 opacity-30" />}
    </button>
  );
}

function SortIcon({ colKey, sortKey, sortDir, onSort }: {
  colKey: CJSortKey; sortKey: CJSortKey | null; sortDir: "asc" | "desc"; onSort: (k: CJSortKey) => void;
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

// Filtro de coluna — mesmo componente/visual do PodMonitorTable.
function ColumnFilter({ label, options, selected, onChange }: {
  label: string; options: string[]; selected: Set<string>; onChange: (v: Set<string>) => void;
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

interface CronJobMonitorTableProps {
  cluster: string;
  cronJobs: CronJob[];
  loading: boolean;
  headerLabel: string;
  onOpenEditor: (cj: CronJob) => void;
  onRequestRefresh: () => void;
  // searchQuery/onSearchQueryChange — controlados pelo painel esquerdo (Tab) quando informados,
  // pra manter a busca sincronizada entre os dois painéis. Opcionais: sem eles, cai no estado
  // interno de sempre.
  searchQuery?: string;
  onSearchQueryChange?: (query: string) => void;
}

export const CronJobMonitorTable = ({
  cluster,
  cronJobs,
  loading,
  headerLabel,
  onOpenEditor,
  onRequestRefresh,
  searchQuery: controlledSearchQuery,
  onSearchQueryChange,
}: CronJobMonitorTableProps) => {
  const [uncontrolledSearchQuery, setUncontrolledSearchQuery] = useState("");
  const searchQuery = controlledSearchQuery ?? uncontrolledSearchQuery;
  const setSearchQuery = onSearchQueryChange ?? setUncontrolledSearchQuery;
  const [sortKey, setSortKey] = useState<CJSortKey | null>(null);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [statusFilter, setStatusFilter] = useState<Set<string>>(new Set());
  const [namespaceFilter, setNamespaceFilter] = useState<Set<string>>(new Set());
  const [concurrencyFilter, setConcurrencyFilter] = useState<Set<string>>(new Set());
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
  const uniqueStatuses = useMemo(() => [...new Set(cronJobs.map(cronJobStatus))].sort((a, b) => STATUS_ORDER[a] - STATUS_ORDER[b]), [cronJobs]);
  const uniqueConcurrency = useMemo(() => [...new Set(cronJobs.map((cj) => cj.concurrency_policy || "Allow"))].sort(), [cronJobs]);

  const hasFilters = statusFilter.size + namespaceFilter.size + concurrencyFilter.size > 0;
  const activeFilterCount = statusFilter.size + namespaceFilter.size + concurrencyFilter.size;
  const clearAllFilters = () => {
    setStatusFilter(new Set());
    setNamespaceFilter(new Set());
    setConcurrencyFilter(new Set());
    setSearchQuery("");
  };

  const filtered = useMemo(() => {
    let result = cronJobs;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter((cj) =>
        cj.name.toLowerCase().includes(q) ||
        cj.namespace.toLowerCase().includes(q) ||
        cj.schedule.toLowerCase().includes(q) ||
        (cj.schedule_description ?? "").toLowerCase().includes(q) ||
        (cj.image ?? "").toLowerCase().includes(q)
      );
    }
    if (statusFilter.size > 0) result = result.filter((cj) => statusFilter.has(cronJobStatus(cj)));
    if (namespaceFilter.size > 0) result = result.filter((cj) => namespaceFilter.has(cj.namespace));
    if (concurrencyFilter.size > 0) result = result.filter((cj) => concurrencyFilter.has(cj.concurrency_policy || "Allow"));
    if (sortKey) {
      result = [...result].sort((a, b) => {
        const va = sortValue(a, sortKey);
        const vb = sortValue(b, sortKey);
        const cmp = typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb));
        return sortDir === "asc" ? cmp : -cmp;
      });
    }
    return result;
  }, [cronJobs, searchQuery, statusFilter, namespaceFilter, concurrencyFilter, sortKey, sortDir]);

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

  const head = "relative overflow-hidden pr-4 flex items-center";

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
            {activeFilterCount > 0 && <Badge variant="secondary" className="text-[10px] h-4 px-1">{activeFilterCount}</Badge>}
            Limpar filtros
          </Button>
        )}
        <div className="relative w-40">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
          <Input
            ref={searchInputRef}
            placeholder="Buscar..."
            title="Busca por nome, namespace, schedule, descrição ou imagem"
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

      {/* Chips de filtros ativos */}
      {hasFilters && (
        <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-border bg-muted/10 flex-shrink-0 flex-wrap">
          {([
            ["status", statusFilter, setStatusFilter],
            ["ns", namespaceFilter, setNamespaceFilter],
            ["conc.", concurrencyFilter, setConcurrencyFilter],
          ] as [string, Set<string>, (v: Set<string>) => void][]).flatMap(([prefix, set, setter]) =>
            [...set].map((v) => (
              <Badge key={`${prefix}-${v}`} variant="secondary" className="text-[10px] h-5 gap-1 cursor-pointer hover:bg-destructive/20"
                onClick={() => { const n = new Set(set); n.delete(v); setter(n); }}>
                {prefix}: {v} <X className="w-2.5 h-2.5" />
              </Badge>
            )),
          )}
        </div>
      )}

      {/* Column headers + rows (scroll together) */}
      <div ref={rowsContainerRef} className="flex-1 overflow-auto">
        <div className="sticky top-0 z-10 grid font-mono text-[10px] px-3 py-1.5 border-b border-border bg-muted/20 w-max min-w-full" style={{ gridTemplateColumns: gridTemplate }}>
          <span className="flex items-center">
            <Checkbox
              checked={allSelected}
              data-state={someSelected && !allSelected ? "indeterminate" : undefined}
              onCheckedChange={toggleAll}
              className="w-3.5 h-3.5 rounded-full"
              disabled={filtered.length === 0}
            />
          </span>
          <span className={head}>
            {uniqueNamespaces.length > 1
              ? <><ColumnFilter label="NAME/NS" options={uniqueNamespaces} selected={namespaceFilter} onChange={setNamespaceFilter} /><SortIcon colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} /></>
              : <SortBtn label="NAME" colKey="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />}
            <ResizeHandle onResize={(d) => resize(1, d)} />
          </span>
          <span className={head}>
            <SortBtn label="SCHEDULE" title="Expressão cron, descrição e fuso (sem spec.timeZone, o controller usa UTC)" colKey="schedule" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(2, d)} />
          </span>
          <span className={head}>
            <ColumnFilter label="STATUS" options={uniqueStatuses} selected={statusFilter} onChange={setStatusFilter} />
            <SortIcon colKey="status" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(3, d)} />
          </span>
          <span className={head}>
            <SortBtn label="PRÓXIMA" title="Próxima execução prevista pelo schedule" colKey="next" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(4, d)} />
          </span>
          <span className={head}>
            <SortBtn label="ÚLTIMA" title="Último disparo (status.lastScheduleTime)" colKey="last" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(5, d)} />
          </span>
          <span className={head}>
            <SortBtn label="ÚLT. JOB" title="Resultado e duração do Job mais recente (ordena pela duração)" colKey="lastJob" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(6, d)} />
          </span>
          <span className={head}>
            <SortBtn label="ÚLT. SUCESSO" title="Último Job concluído com sucesso (status.lastSuccessfulTime)" colKey="lastSuccess" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(7, d)} />
          </span>
          <span className={head}>
            <SortBtn label="HISTÓRICO" title="Jobs retidos no cluster: ✓ sucesso / ✗ falha (limitados por successful/failedJobsHistoryLimit) — ordena pelas falhas" colKey="history" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(8, d)} />
          </span>
          <span className={head}>
            <SortBtn label="ATIVOS" title="Jobs em execução agora" colKey="active" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(9, d)} />
          </span>
          <span className={head} title="concurrencyPolicy: Allow (sobrepõe), Forbid (pula se o anterior ainda roda), Replace (substitui o anterior)">
            <ColumnFilter label="CONC." options={uniqueConcurrency} selected={concurrencyFilter} onChange={setConcurrencyFilter} />
            <ResizeHandle onResize={(d) => resize(10, d)} />
          </span>
          <span className={head}>
            <SortBtn label="AGE" colKey="age" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
            <ResizeHandle onResize={(d) => resize(11, d)} />
          </span>
          <span></span>
        </div>

        {filtered.length === 0 && !loading && (
          <div className="text-muted-foreground text-xs text-center py-6">
            {searchQuery || hasFilters ? "Nenhum CronJob encontrado para a busca/filtros" : "Nenhum CronJob encontrado"}
          </div>
        )}
        {filtered.map((cj, index) => {
          const status = cronJobStatus(cj);
          const style = STATUS_STYLE[status];
          const isSelected = selectedKeys.has(cjKey(cj));
          const lj = cj.last_job;

          return (
            <div
              key={cjKey(cj)}
              data-row-index={index}
              tabIndex={0}
              className={`grid w-max min-w-full px-3 py-1.5 hover:bg-muted/40 transition-colors border-b border-border/40 font-mono text-xs cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary/60 ${style.row} ${isSelected ? "bg-primary/10 hover:bg-primary/15 ring-inset ring-1 ring-primary/30" : ""}`}
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

              {/* NAME/NS + imagem */}
              <span className="truncate pr-1 min-w-0">
                {uniqueNamespaces.length > 1 && (
                  <span className="text-muted-foreground text-[10px] block leading-tight">{cj.namespace}</span>
                )}
                <span className="truncate block" title={`${cj.namespace}/${cj.name}${cj.image ? `\nimagem: ${cj.image}` : ""}`}>{cj.name}</span>
              </span>

              {/* SCHEDULE: cron + descrição + fuso */}
              <span className="min-w-0 pr-1" title={cj.schedule_error ? `Expressão não interpretada: ${cj.schedule_error}` : `${cj.schedule} (${cj.time_zone || "UTC"})`}>
                <span className="block truncate">{cj.schedule}</span>
                <span className="block truncate text-[10px] text-muted-foreground leading-tight">
                  {cj.schedule_error ? "expressão inválida" : `${cj.schedule_description && cj.schedule_description !== cj.schedule ? `${cj.schedule_description} · ` : ""}${cj.time_zone || "UTC"}`}
                </span>
              </span>

              {/* STATUS */}
              <span title={status === "Atrasado" && cj.missed_since ? `${style.title}\nEsperada em: ${fmtLocal(cj.missed_since)}` : style.title}>
                <span className={`px-1 py-0.5 rounded text-[9px] ${style.badge}`}>{status}</span>
              </span>

              {/* PRÓXIMA */}
              <span className={cj.suspend ? "text-muted-foreground line-through" : ""} title={cj.next_schedule_time ? `${fmtLocal(cj.next_schedule_time)}${cj.suspend ? " — suspenso, não vai disparar" : ""}` : undefined}>
                {cj.next_schedule_time ? formatIn(cj.next_schedule_time) : "—"}
              </span>

              {/* ÚLTIMA */}
              <span className="text-muted-foreground" title={fmtLocal(cj.last_schedule_at)}>{formatAgo(cj.last_schedule_at)}</span>

              {/* ÚLT. JOB: resultado + duração */}
              <span
                className={lj?.status === "Failed" ? "text-red-600 dark:text-red-400" : lj?.status === "Running" ? "text-blue-600 dark:text-blue-400" : lj ? "text-green-600 dark:text-green-400" : "text-muted-foreground"}
                title={lj ? `${lj.name}\ninício: ${fmtLocal(lj.start_time)}${lj.completion_time ? `\nfim: ${fmtLocal(lj.completion_time)}` : ""}` : "Nenhum Job retido no histórico"}
              >
                {lj ? `${lj.status === "Failed" ? "✗" : lj.status === "Running" ? "▶" : "✓"} ${formatDuration(lj.duration_seconds)}` : "—"}
              </span>

              {/* ÚLT. SUCESSO */}
              <span className="text-muted-foreground" title={fmtLocal(cj.last_successful_time)}>{formatAgo(cj.last_successful_time)}</span>

              {/* HISTÓRICO */}
              <span title={`Retidos no cluster: ${cj.history_succeeded ?? 0} com sucesso, ${cj.history_failed ?? 0} com falha\nLimites: ${cj.successful_jobs} sucesso / ${cj.failed_jobs} falha`}>
                <span className="text-green-600 dark:text-green-400">✓{cj.history_succeeded ?? 0}</span>{" "}
                <span className={(cj.history_failed ?? 0) > 0 ? "text-red-600 dark:text-red-400" : "text-muted-foreground"}>✗{cj.history_failed ?? 0}</span>
              </span>

              {/* ATIVOS */}
              <span className={cj.active_jobs > 0 ? "text-blue-600 dark:text-blue-400" : "text-muted-foreground"}>{cj.active_jobs}</span>

              {/* CONC. */}
              <span className="text-muted-foreground" title={cj.starting_deadline_seconds ? `startingDeadlineSeconds: ${cj.starting_deadline_seconds}` : undefined}>
                {cj.concurrency_policy || "Allow"}
              </span>

              {/* AGE */}
              <span className="text-muted-foreground" title={fmtLocal(cj.created_at)}>{cj.created_at ? formatAge(cj.created_at) : "—"}</span>

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
