import { useState, useEffect, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Calendar as CalendarComponent } from "@/components/ui/calendar";
import {
  History,
  RefreshCw,
  Download,
  ChevronDown,
  ChevronRight,
  Calendar,
  Clock,
  Server,
  User,
  Activity,
  CheckCircle2,
  XCircle,
  AlertCircle,
  X,
} from "lucide-react";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface HistoryEntry {
  id: string;
  timestamp: string;
  user_email?: string;
  user_name?: string;
  action: string;
  resource: string;
  cluster: string;
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
  status: string;
  error_msg?: string;
  duration_ms: number;
  session_name?: string;
}

interface HistoryPage {
  entries: HistoryEntry[] | null;
  total: number;
  has_more: boolean;
}

interface HistoryStats {
  total: number;
  success: number;
  failed: number;
  by_action: Record<string, number>;
  by_cluster: Record<string, number>;
  by_user: Record<string, number>;
  retention_days?: number;
}

interface HistoryViewerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Entradas por página — a paginação é no servidor (SQLite); o diff de cada entrada é DOM pesado.
const PAGE_SIZE = 200;

// ── Diff por chave ─────────────────────────────────────────────────────────────
// Antes comparava o JSON linha a linha pela posição: uma chave a mais no meio desalinhava todo o
// resto. Agora achata before/after em caminhos ("spec.replicas") e compara chave a chave.
type DiffRow = { path: string; kind: "added" | "removed" | "changed"; before?: string; after?: string };

function flatten(value: unknown, prefix: string, out: Record<string, string>) {
  if (value !== null && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length > 0) {
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) flatten(v, prefix ? `${prefix}.${k}` : k, out);
    return;
  }
  out[prefix || "(valor)"] = JSON.stringify(value);
}

function diffEntries(before: Record<string, unknown> | null, after: Record<string, unknown> | null): DiffRow[] {
  const b: Record<string, string> = {};
  const a: Record<string, string> = {};
  if (before) flatten(before, "", b);
  if (after) flatten(after, "", a);
  const rows: DiffRow[] = [];
  for (const path of Array.from(new Set([...Object.keys(b), ...Object.keys(a)])).sort()) {
    if (!(path in a)) rows.push({ path, kind: "removed", before: b[path] });
    else if (!(path in b)) rows.push({ path, kind: "added", after: a[path] });
    else if (a[path] !== b[path]) rows.push({ path, kind: "changed", before: b[path], after: a[path] });
  }
  return rows;
}

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e));

// Célula de CSV sem injeção de fórmula: Excel/LibreOffice executam células que começam com
// = + - @ (ou tab/CR) — prefixa com aspa simples para virarem texto.
function csvCell(value: unknown): string {
  let s = value === null || value === undefined ? "" : String(value);
  if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
  return `"${s.replace(/"/g, '""')}"`;
}

export function HistoryViewer({ open, onOpenChange }: HistoryViewerProps) {
  const [entries, setEntries] = useState<HistoryEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [stats, setStats] = useState<HistoryStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [expandedEntries, setExpandedEntries] = useState<Set<string>>(new Set());
  // Descarta respostas antigas quando o filtro muda no meio de uma busca
  const requestSeq = useRef(0);

  // Filtros
  const [filterAction, setFilterAction] = useState<string>("all");
  const [filterCluster, setFilterCluster] = useState<string>("all");
  const [filterUser, setFilterUser] = useState<string>("all");
  const [filterStatus, setFilterStatus] = useState<string>("all");
  const [filterResource, setFilterResource] = useState<string>("");
  const [filterStartDate, setFilterStartDate] = useState<Date | undefined>(undefined);
  const [datePickerOpen, setDatePickerOpen] = useState(false);

  useEffect(() => {
    if (open) {
      fetchStats();
    }
  }, [open]);

  // Buscar histórico quando filtros mudarem
  useEffect(() => {
    if (open) {
      const timer = setTimeout(() => {
        fetchHistory(0);
      }, 300); // Debounce de 300ms para evitar muitas requisições
      return () => clearTimeout(timer);
    }
  }, [open, filterAction, filterCluster, filterUser, filterStatus, filterResource, filterStartDate]);

  const buildParams = (offset: number, limit: number) => {
    const params = new URLSearchParams();
    if (filterAction !== "all") params.append("action", filterAction);
    if (filterCluster !== "all") params.append("cluster", filterCluster);
    if (filterUser !== "all") params.append("user_email", filterUser);
    if (filterStatus !== "all") params.append("status", filterStatus);
    if (filterResource) params.append("resource", filterResource);
    if (filterStartDate) {
      // Formatar data como YYYY-MM-DD e enviar start e end para filtrar apenas o dia específico
      const dateStr = format(filterStartDate, "yyyy-MM-dd");
      params.append("start_date", dateStr);
      params.append("end_date", dateStr);
    }
    params.append("limit", String(limit));
    params.append("offset", String(offset));
    return params.toString();
  };

  const fetchHistory = async (offset: number) => {
    const seq = ++requestSeq.current;
    if (offset === 0) setLoading(true);
    else setLoadingMore(true);
    try {
      const data = await apiClient.get<HistoryPage>(`/history?${buildParams(offset, PAGE_SIZE)}`);
      if (seq !== requestSeq.current) return;
      const page = data.entries || [];
      setEntries((prev) => (offset === 0 ? page : [...prev, ...page]));
      setTotal(data.total ?? page.length);
      setHasMore(!!data.has_more);
    } catch (error: unknown) {
      if (seq !== requestSeq.current) return;
      console.error("Failed to fetch history:", error);
      toast.error("Erro ao carregar histórico", {
        description: errorMessage(error),
        style: {
          background: '#fee2e2',
          border: '1px solid #fca5a5',
          color: '#991b1b',
        },
      });
    } finally {
      if (seq === requestSeq.current) {
        setLoading(false);
        setLoadingMore(false);
      }
    }
  };

  const fetchStats = async () => {
    try {
      const data = await apiClient.get<HistoryStats>("/history/stats");
      setStats(data);
    } catch (error: unknown) {
      console.error("Failed to fetch stats:", error);
    }
  };

  const handleRefresh = () => {
    fetchHistory(0);
    fetchStats();
  };

  // Exporta TODAS as entradas do filtro atual (não só as carregadas na tela), em lotes de 1000.
  const handleExportCSV = async () => {
    if (total === 0) {
      toast.error("Nenhum dado para exportar");
      return;
    }
    setExporting(true);
    try {
      const all: HistoryEntry[] = [];
      for (let offset = 0; ; offset += 1000) {
        const data = await apiClient.get<HistoryPage>(`/history?${buildParams(offset, 1000)}`);
        all.push(...(data.entries || []));
        if (!data.has_more) break;
      }

      const headers = [
        "ID",
        "Timestamp",
        "User",
        "Action",
        "Resource",
        "Cluster",
        "Status",
        "Duration (ms)",
        "Session",
        "Error",
      ];
      const rows = all.map((entry) => [
        entry.id,
        new Date(entry.timestamp).toLocaleString(),
        entry.user_email || "",
        entry.action || "",
        entry.resource || "",
        entry.cluster || "",
        entry.status || "",
        String(entry.duration_ms ?? 0),
        entry.session_name || "",
        entry.error_msg || "",
      ]);
      const csvContent = [
        headers.map(csvCell).join(","),
        ...rows.map((row) => row.map(csvCell).join(",")),
      ].join("\n");

      // Download
      const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
      const link = document.createElement("a");
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute("download", `history_${new Date().toISOString()}.csv`);
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      toast.success(`CSV exportado (${all.length} entradas)`);
    } catch (error: unknown) {
      toast.error("Erro ao exportar CSV", { description: errorMessage(error) });
    } finally {
      setExporting(false);
    }
  };

  const toggleExpanded = (id: string) => {
    const newExpanded = new Set(expandedEntries);
    if (newExpanded.has(id)) {
      newExpanded.delete(id);
    } else {
      newExpanded.add(id);
    }
    setExpandedEntries(newExpanded);
  };

  const formatDuration = (ms: number): string => {
    if (!ms) return "-";
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  };

  const getActionBadgeColor = (action: string): string => {
    action = action || "";
    if (action.includes("update") || action.includes("apply") || action.includes("scale")) return "bg-blue-500";
    if (action.includes("delete") || action.includes("remove") || action.includes("kill") || action.includes("uninstall")) return "bg-red-500";
    if (action.includes("rollout") || action.includes("restart") || action.includes("rollback")) return "bg-purple-500";
    if (action.includes("suspend") || action.includes("cordon") || action.includes("drain") || action.includes("abort")) return "bg-orange-500";
    if (action.includes("resume") || action.includes("create") || action.includes("install")) return "bg-green-500";
    return "bg-gray-500";
  };

  const getStatusIcon = (status: string) => {
    if (status === "success") return <CheckCircle2 className="w-4 h-4 text-green-600" />;
    if (status === "failed") return <XCircle className="w-4 h-4 text-red-600" />;
    return <AlertCircle className="w-4 h-4 text-yellow-600" />;
  };

  // Opções dos filtros vêm das estatísticas do histórico inteiro (não só da página carregada).
  // Vazios ficam de fora: <SelectItem value=""> lança exceção no Radix e derrubava a página.
  const optionsFrom = (m?: Record<string, number>) =>
    Object.keys(m || {}).filter(Boolean).sort((a, b) => a.localeCompare(b));
  const uniqueActions = optionsFrom(stats?.by_action);
  const uniqueClusters = optionsFrom(stats?.by_cluster);
  const uniqueUsers = optionsFrom(stats?.by_user);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-7xl h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <History className="w-5 h-5" />
            History Tracker
          </DialogTitle>
        </DialogHeader>

        {/* Stats Cards */}
        {stats && (
          <div className="grid grid-cols-4 gap-3 mb-4">
            <div className="bg-slate-50 dark:bg-slate-800 rounded-lg p-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-slate-600 dark:text-slate-400">Total</span>
                <Activity className="w-4 h-4 text-slate-500" />
              </div>
              <p className="text-2xl font-bold mt-1">{stats.total}</p>
            </div>
            <div className="bg-green-50 dark:bg-green-900/20 rounded-lg p-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-green-600 dark:text-green-400">Sucesso</span>
                <CheckCircle2 className="w-4 h-4 text-green-600" />
              </div>
              <p className="text-2xl font-bold mt-1 text-green-700 dark:text-green-300">{stats.success}</p>
            </div>
            <div className="bg-red-50 dark:bg-red-900/20 rounded-lg p-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-red-600 dark:text-red-400">Falhas</span>
                <XCircle className="w-4 h-4 text-red-600" />
              </div>
              <p className="text-2xl font-bold mt-1 text-red-700 dark:text-red-300">{stats.failed}</p>
            </div>
            <div className="bg-blue-50 dark:bg-blue-900/20 rounded-lg p-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-blue-600 dark:text-blue-400">Taxa Sucesso</span>
                <Activity className="w-4 h-4 text-blue-600" />
              </div>
              <p className="text-2xl font-bold mt-1 text-blue-700 dark:text-blue-300">
                {stats.total > 0 ? Math.round((stats.success / stats.total) * 100) : 0}%
              </p>
            </div>
          </div>
        )}

        {/* Filtros */}
        <div className="grid grid-cols-6 gap-2 mb-4">
          <div>
            <Label className="text-xs">Ação</Label>
            <Select value={filterAction} onValueChange={setFilterAction}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todas</SelectItem>
                {uniqueActions.map((action) => (
                  <SelectItem key={action} value={action}>
                    {action.replace(/_/g, " ")}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <Label className="text-xs">Cluster</Label>
            <Select value={filterCluster} onValueChange={setFilterCluster}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos</SelectItem>
                {uniqueClusters.map((cluster) => (
                  <SelectItem key={cluster} value={cluster}>
                    {cluster}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <Label className="text-xs">Usuário</Label>
            <Select value={filterUser} onValueChange={setFilterUser}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos</SelectItem>
                {uniqueUsers.map((user) => (
                  <SelectItem key={user} value={user}>
                    {user}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <Label className="text-xs">Status</Label>
            <Select value={filterStatus} onValueChange={setFilterStatus}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos</SelectItem>
                <SelectItem value="success">Sucesso</SelectItem>
                <SelectItem value="failed">Falha</SelectItem>
                <SelectItem value="partial">Parcial</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div>
            <Label className="text-xs">Recurso</Label>
            <Input
              placeholder="namespace/name"
              value={filterResource}
              onChange={(e) => setFilterResource(e.target.value)}
              className="h-8 text-xs"
            />
          </div>

          <div>
            <Label className="text-xs">Data</Label>
            <Popover open={datePickerOpen} onOpenChange={setDatePickerOpen}>
              <PopoverTrigger asChild>
                <Button
                  variant="outline"
                  className={`h-8 w-full justify-start text-left font-normal text-xs ${
                    !filterStartDate && "text-muted-foreground"
                  }`}
                >
                  <Calendar className="mr-2 h-3 w-3" />
                  {filterStartDate ? format(filterStartDate, "dd/MM/yyyy") : "Selecionar data"}
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-auto p-0" align="start">
                <CalendarComponent
                  mode="single"
                  selected={filterStartDate}
                  onSelect={(date) => {
                    setFilterStartDate(date);
                    setDatePickerOpen(false);
                  }}
                  initialFocus
                  locale={ptBR}
                  formatters={{
                    formatCaption: (date) => format(date, "MMMM yyyy", { locale: ptBR }),
                  }}
                />
                {filterStartDate && (
                  <div className="p-2 border-t">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="w-full text-xs"
                      onClick={() => {
                        setFilterStartDate(undefined);
                        setDatePickerOpen(false);
                      }}
                    >
                      <X className="w-3 h-3 mr-1" />
                      Limpar
                    </Button>
                  </div>
                )}
              </PopoverContent>
            </Popover>
          </div>
        </div>

        {/* Ações */}
        <div className="flex items-center gap-2 mb-2">
          <Button
            onClick={handleRefresh}
            disabled={loading}
            size="sm"
            variant="outline"
          >
            <RefreshCw className={`w-4 h-4 mr-2 ${loading ? "animate-spin" : ""}`} />
            Atualizar
          </Button>
          <Button onClick={handleExportCSV} size="sm" variant="outline" disabled={exporting || total === 0}>
            <Download className={`w-4 h-4 mr-2 ${exporting ? "animate-pulse" : ""}`} />
            Exportar CSV
          </Button>
          <span className="ml-auto text-xs text-muted-foreground">
            {total > 0 && `Mostrando ${entries.length} de ${total}`}
            {stats?.retention_days ? ` · retenção de ${stats.retention_days} dias, somente leitura` : ""}
          </span>
        </div>

        {/* Entries List */}
        <div className="flex-1 overflow-y-auto border rounded-lg bg-slate-50 dark:bg-slate-900">
          {loading ? (
            <div className="flex items-center justify-center h-full">
              <RefreshCw className="w-6 h-6 animate-spin text-slate-400" />
            </div>
          ) : entries.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-slate-500">
              <History className="w-12 h-12 mb-2 opacity-50" />
              <p>Nenhum histórico encontrado</p>
            </div>
          ) : (
            <div className="space-y-1 p-2">
              {entries.map((entry) => {
                const isExpanded = expandedEntries.has(entry.id);
                return (
                  <div
                    key={entry.id}
                    className="bg-white dark:bg-slate-800 rounded border hover:border-blue-400 transition-colors"
                  >
                    {/* Header */}
                    <div
                      className="flex items-center gap-3 p-3 cursor-pointer"
                      onClick={() => toggleExpanded(entry.id)}
                    >
                      {isExpanded ? (
                        <ChevronDown className="w-4 h-4 text-slate-400 flex-shrink-0" />
                      ) : (
                        <ChevronRight className="w-4 h-4 text-slate-400 flex-shrink-0" />
                      )}

                      {getStatusIcon(entry.status)}

                      <div className="flex-1 grid grid-cols-8 gap-2 text-sm min-w-0">
                        <div className="flex items-center gap-2">
                          <Calendar className="w-3 h-3 text-slate-400" />
                          <span className="font-mono text-xs">
                            {new Date(entry.timestamp).toLocaleString()}
                          </span>
                        </div>

                        <div>
                          <Badge className={`${getActionBadgeColor(entry.action)} text-white text-xs`}>
                            {(entry.action || "-").replace(/_/g, " ")}
                          </Badge>
                        </div>

                        <div className="flex items-center gap-1 text-xs truncate" title={entry.user_email || ""}>
                          <User className="w-3 h-3 text-slate-400 flex-shrink-0" />
                          <span className="truncate">{entry.user_name || entry.user_email || "-"}</span>
                        </div>

                        <div className="flex items-center gap-1 text-xs truncate">
                          <Server className="w-3 h-3 text-slate-400 flex-shrink-0" />
                          <span className="truncate">{entry.cluster || "-"}</span>
                        </div>

                        <div className="col-span-3 text-xs font-mono truncate" title={entry.resource}>
                          {entry.resource}
                        </div>

                        <div className="flex items-center gap-1 text-xs text-slate-500">
                          <Clock className="w-3 h-3" />
                          {formatDuration(entry.duration_ms)}
                        </div>
                      </div>
                    </div>

                    {/* Expanded Details */}
                    {isExpanded && (
                      <div className="border-t p-3 bg-slate-50 dark:bg-slate-900/50">
                        <div className="border rounded font-mono text-xs bg-white dark:bg-slate-950">
                          <div className="bg-slate-100 dark:bg-slate-800 px-3 py-1 text-slate-600 dark:text-slate-400 font-semibold border-b">
                            Changes
                          </div>
                          {(() => {
                            const rows = diffEntries(entry.before, entry.after);
                            if (rows.length === 0) {
                              return <div className="px-3 py-2 text-slate-500">Sem detalhes de antes/depois</div>;
                            }
                            return rows.map((row) => (
                              <div key={row.path} className="border-b last:border-b-0">
                                {row.before !== undefined && (
                                  <div className="flex bg-red-50 dark:bg-red-950/30">
                                    <span className="w-8 flex-shrink-0 text-center text-red-600 dark:text-red-400 select-none bg-red-100 dark:bg-red-900/30 font-bold">
                                      -
                                    </span>
                                    <pre className="flex-1 px-3 py-1 text-red-700 dark:text-red-300 overflow-x-auto whitespace-pre-wrap break-all">
                                      {row.path}: {row.before}
                                    </pre>
                                  </div>
                                )}
                                {row.after !== undefined && (
                                  <div className="flex bg-green-50 dark:bg-green-950/30">
                                    <span className="w-8 flex-shrink-0 text-center text-green-600 dark:text-green-400 select-none bg-green-100 dark:bg-green-900/30 font-bold">
                                      +
                                    </span>
                                    <pre className="flex-1 px-3 py-1 text-green-700 dark:text-green-300 overflow-x-auto whitespace-pre-wrap break-all">
                                      {row.path}: {row.after}
                                    </pre>
                                  </div>
                                )}
                              </div>
                            ));
                          })()}
                        </div>

                        {/* Error Message */}
                        {entry.error_msg && (
                          <div className="mt-3">
                            <h4 className="font-semibold mb-1 text-red-600 dark:text-red-400 text-xs">
                              Erro
                            </h4>
                            <p className="text-xs bg-red-50 dark:bg-red-900/20 p-2 rounded border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300">
                              {entry.error_msg}
                            </p>
                          </div>
                        )}

                        {/* Usuário */}
                        {entry.user_email && (
                          <div className="mt-2 text-xs text-slate-600 dark:text-slate-400">
                            <span className="font-semibold">Usuário:</span> {entry.user_name ? `${entry.user_name} <${entry.user_email}>` : entry.user_email}
                          </div>
                        )}

                        {/* Session Name */}
                        {entry.session_name && (
                          <div className="mt-2 text-xs text-slate-600 dark:text-slate-400">
                            <span className="font-semibold">Sessão:</span> {entry.session_name}
                          </div>
                        )}

                        {/* ID */}
                        <div className="mt-2 text-xs text-slate-500 font-mono">
                          ID: {entry.id}
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
              {hasMore && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="w-full"
                  disabled={loadingMore}
                  onClick={() => fetchHistory(entries.length)}
                >
                  {loadingMore ? "Carregando..." : `Mostrar mais (${total - entries.length} restantes)`}
                </Button>
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
