import { useState, useEffect } from "react";
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
  History,
  RefreshCw,
  Trash2,
  Download,
  ChevronDown,
  ChevronRight,
  Calendar,
  Clock,
  Server,
  Activity,
  CheckCircle2,
  XCircle,
  AlertCircle,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface HistoryEntry {
  id: string;
  timestamp: string;
  action: string;
  resource: string;
  cluster: string;
  before: Record<string, any>;
  after: Record<string, any>;
  status: string;
  error_msg?: string;
  duration_ms: number;
  session_name?: string;
}

interface HistoryStats {
  total: number;
  success: number;
  failed: number;
  by_action: Record<string, number>;
  by_cluster: Record<string, number>;
}

interface HistoryViewerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function HistoryViewer({ open, onOpenChange }: HistoryViewerProps) {
  const [entries, setEntries] = useState<HistoryEntry[]>([]);
  const [stats, setStats] = useState<HistoryStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [expandedEntries, setExpandedEntries] = useState<Set<string>>(new Set());

  // Filtros
  const [filterAction, setFilterAction] = useState<string>("all");
  const [filterCluster, setFilterCluster] = useState<string>("all");
  const [filterStatus, setFilterStatus] = useState<string>("all");
  const [filterResource, setFilterResource] = useState<string>("");
  const [filterStartDate, setFilterStartDate] = useState<string>("");
  const [filterEndDate, setFilterEndDate] = useState<string>("");

  useEffect(() => {
    if (open) {
      fetchHistory();
      fetchStats();
    }
  }, [open]);

  const fetchHistory = async () => {
    setLoading(true);
    try {
      // Construir query params com filtros
      const params = new URLSearchParams();
      if (filterAction !== "all") params.append("action", filterAction);
      if (filterCluster !== "all") params.append("cluster", filterCluster);
      if (filterStatus !== "all") params.append("status", filterStatus);
      if (filterResource) params.append("resource", filterResource);
      if (filterStartDate) params.append("start_date", filterStartDate);
      if (filterEndDate) params.append("end_date", filterEndDate);

      const queryString = params.toString();
      const url = `/api/v1/history${queryString ? `?${queryString}` : ""}`;

      const token = localStorage.getItem("auth_token") || "poc-token-123";
      const response = await fetch(url, {
        headers: {
          "Authorization": `Bearer ${token}`,
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const data = await response.json();
      setEntries(data.entries || []);
    } catch (error: any) {
      console.error("Failed to fetch history:", error);
      toast.error("Erro ao carregar histórico", {
        description: error.message,
        style: {
          background: '#fee2e2',
          border: '1px solid #fca5a5',
          color: '#991b1b',
        },
      });
    } finally {
      setLoading(false);
    }
  };

  const fetchStats = async () => {
    try {
      const token = localStorage.getItem("auth_token") || "poc-token-123";
      const response = await fetch("/api/v1/history/stats", {
        headers: {
          "Authorization": `Bearer ${token}`,
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const data = await response.json();
      setStats(data);
    } catch (error: any) {
      console.error("Failed to fetch stats:", error);
    }
  };

  const handleRefresh = () => {
    fetchHistory();
    fetchStats();
  };

  const handleClearHistory = async () => {
    if (!confirm("Tem certeza que deseja limpar TODO o histórico? Esta ação não pode ser desfeita.")) {
      return;
    }

    try {
      await apiClient.delete("/history");
      toast.success("Histórico limpo com sucesso");
      fetchHistory();
      fetchStats();
    } catch (error: any) {
      console.error("Failed to clear history:", error);
      toast.error("Erro ao limpar histórico", {
        description: error.response?.data?.error?.message || error.message,
      });
    }
  };

  const handleExportCSV = () => {
    if (entries.length === 0) {
      toast.error("Nenhum dado para exportar");
      return;
    }

    // Criar CSV
    const headers = [
      "ID",
      "Timestamp",
      "Action",
      "Resource",
      "Cluster",
      "Status",
      "Duration (ms)",
      "Session",
      "Error",
    ];

    const rows = entries.map((entry) => [
      entry.id,
      new Date(entry.timestamp).toLocaleString(),
      entry.action,
      entry.resource,
      entry.cluster,
      entry.status,
      entry.duration_ms.toString(),
      entry.session_name || "",
      entry.error_msg || "",
    ]);

    const csvContent = [
      headers.join(","),
      ...rows.map((row) =>
        row.map((cell) => `"${cell.replace(/"/g, '""')}"`).join(",")
      ),
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

    toast.success("CSV exportado com sucesso");
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
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  };

  const getActionBadgeColor = (action: string): string => {
    if (action.includes("update")) return "bg-blue-500";
    if (action.includes("delete")) return "bg-red-500";
    if (action.includes("rollout")) return "bg-purple-500";
    if (action.includes("suspend")) return "bg-orange-500";
    if (action.includes("resume")) return "bg-green-500";
    return "bg-gray-500";
  };

  const getStatusIcon = (status: string) => {
    if (status === "success") return <CheckCircle2 className="w-4 h-4 text-green-600" />;
    if (status === "failed") return <XCircle className="w-4 h-4 text-red-600" />;
    return <AlertCircle className="w-4 h-4 text-yellow-600" />;
  };

  // Extrair valores únicos para filtros
  const uniqueActions = Array.from(new Set(entries.map((e) => e.action)));
  const uniqueClusters = Array.from(new Set(entries.map((e) => e.cluster)));

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
            <Label className="text-xs">Status</Label>
            <Select value={filterStatus} onValueChange={setFilterStatus}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos</SelectItem>
                <SelectItem value="success">Sucesso</SelectItem>
                <SelectItem value="failed">Falha</SelectItem>
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
            <Label className="text-xs">Data Início</Label>
            <Input
              type="date"
              value={filterStartDate}
              onChange={(e) => setFilterStartDate(e.target.value)}
              className="h-8 text-xs"
            />
          </div>

          <div>
            <Label className="text-xs">Data Fim</Label>
            <Input
              type="date"
              value={filterEndDate}
              onChange={(e) => setFilterEndDate(e.target.value)}
              className="h-8 text-xs"
            />
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
          <Button onClick={handleExportCSV} size="sm" variant="outline">
            <Download className="w-4 h-4 mr-2" />
            Exportar CSV
          </Button>
          <Button
            onClick={handleClearHistory}
            size="sm"
            variant="destructive"
            className="ml-auto"
          >
            <Trash2 className="w-4 h-4 mr-2" />
            Limpar Histórico
          </Button>
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

                      <div className="flex-1 grid grid-cols-6 gap-2 text-sm">
                        <div className="flex items-center gap-2">
                          <Calendar className="w-3 h-3 text-slate-400" />
                          <span className="font-mono text-xs">
                            {new Date(entry.timestamp).toLocaleString()}
                          </span>
                        </div>

                        <div>
                          <Badge className={`${getActionBadgeColor(entry.action)} text-white text-xs`}>
                            {entry.action.replace(/_/g, " ")}
                          </Badge>
                        </div>

                        <div className="flex items-center gap-1 text-xs">
                          <Server className="w-3 h-3 text-slate-400" />
                          {entry.cluster}
                        </div>

                        <div className="col-span-2 text-xs font-mono truncate">
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
                        {/* GitHub-style Unified Diff */}
                        <div className="border rounded font-mono text-xs bg-white dark:bg-slate-950">
                          <div className="bg-slate-100 dark:bg-slate-800 px-3 py-1 text-slate-600 dark:text-slate-400 font-semibold border-b">
                            Changes
                          </div>
                          {(() => {
                            const beforeJSON = JSON.stringify(entry.before, null, 2);
                            const afterJSON = JSON.stringify(entry.after, null, 2);
                            const beforeLines = beforeJSON.split('\n');
                            const afterLines = afterJSON.split('\n');

                            // Simple line-by-line diff
                            const diffLines: Array<{ type: 'unchanged' | 'removed' | 'added'; content: string }> = [];

                            beforeLines.forEach((line, i) => {
                              if (afterLines[i] !== line) {
                                diffLines.push({ type: 'removed', content: line });
                                if (afterLines[i]) {
                                  diffLines.push({ type: 'added', content: afterLines[i] });
                                }
                              } else {
                                diffLines.push({ type: 'unchanged', content: line });
                              }
                            });

                            // Add remaining lines from after if it's longer
                            if (afterLines.length > beforeLines.length) {
                              afterLines.slice(beforeLines.length).forEach(line => {
                                diffLines.push({ type: 'added', content: line });
                              });
                            }

                            return diffLines.map((line, i) => {
                              if (line.type === 'unchanged') {
                                return (
                                  <div key={i} className="flex">
                                    <span className="w-8 flex-shrink-0 text-center text-slate-400 select-none bg-slate-50 dark:bg-slate-900/30">

                                    </span>
                                    <pre className="flex-1 px-3 py-1 text-slate-600 dark:text-slate-400 overflow-x-auto">
                                      {line.content}
                                    </pre>
                                  </div>
                                );
                              } else if (line.type === 'removed') {
                                return (
                                  <div key={i} className="flex bg-red-50 dark:bg-red-950/30">
                                    <span className="w-8 flex-shrink-0 text-center text-red-600 dark:text-red-400 select-none bg-red-100 dark:bg-red-900/30 font-bold">
                                      -
                                    </span>
                                    <pre className="flex-1 px-3 py-1 text-red-700 dark:text-red-300 overflow-x-auto">
                                      {line.content}
                                    </pre>
                                  </div>
                                );
                              } else {
                                return (
                                  <div key={i} className="flex bg-green-50 dark:bg-green-950/30">
                                    <span className="w-8 flex-shrink-0 text-center text-green-600 dark:text-green-400 select-none bg-green-100 dark:bg-green-900/30 font-bold">
                                      +
                                    </span>
                                    <pre className="flex-1 px-3 py-1 text-green-700 dark:text-green-300 overflow-x-auto">
                                      {line.content}
                                    </pre>
                                  </div>
                                );
                              }
                            });
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
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
