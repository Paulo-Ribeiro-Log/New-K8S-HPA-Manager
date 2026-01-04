import { useState, useEffect, useMemo } from "react";
import { DateRange } from "react-day-picker";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Calendar } from "@/components/ui/calendar";
import { Clock, Server, CheckCircle2, AlertCircle, XCircle, Eye, Loader2, Search, Filter, X, CalendarIcon, Download } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import type { HealthCheckResult } from "@/types/healthcheck";
import { cn } from "@/lib/utils";
import { ExportReportModal } from "@/components/ExportReportModal";
import { HealthCheckAlertsExportModal } from "@/components/HealthCheckAlertsExportModal";

interface HealthCheckHistoryModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSelectHistory: (result: HealthCheckResult) => void;
}

export const HealthCheckHistoryModal = ({
  open,
  onOpenChange,
  onSelectHistory,
}: HealthCheckHistoryModalProps) => {
  const [history, setHistory] = useState<HealthCheckResult[]>([]);
  const [loading, setLoading] = useState(false);

  // Filtros
  const [searchText, setSearchText] = useState("");
  const [filterCluster, setFilterCluster] = useState<string>("all");
  const [filterStatus, setFilterStatus] = useState<string>("all");
  const [filterDateRange, setFilterDateRange] = useState<string>("all"); // all, today, week, month, custom
  const [customDateRange, setCustomDateRange] = useState<DateRange | undefined>();
  const [isDatePickerOpen, setIsDatePickerOpen] = useState(false);
  const [showExportModal, setShowExportModal] = useState(false);
  const [alertsExportModalOpen, setAlertsExportModalOpen] = useState(false);
  const [alertsToExport, setAlertsToExport] = useState<{cluster: string, events: any[]}>({ cluster: "", events: [] });

  // Carregar histórico ao abrir modal
  useEffect(() => {
    if (open) {
      loadHistory();
    }
  }, [open]);

  const loadHistory = async () => {
    setLoading(true);
    try {
      const response = await apiClient.getHealthCheckHistory();

      if (response.success && response.data) {
        setHistory(response.data);
      } else {
        toast.error("Erro ao carregar histórico");
      }
    } catch (error) {
      console.error("Failed to load history:", error);
      toast.error("Erro ao carregar histórico de análises");
    } finally {
      setLoading(false);
    }
  };

  // Extrair clusters únicos do histórico
  const uniqueClusters = useMemo(() => {
    const clusters = new Set(history.map((h) => h.cluster));
    return Array.from(clusters).sort();
  }, [history]);

  // Filtrar histórico
  const filteredHistory = useMemo(() => {
    let filtered = [...history];

    // Filtro por texto (cluster ou id)
    if (searchText.trim()) {
      const search = searchText.toLowerCase();
      filtered = filtered.filter(
        (h) =>
          h.cluster.toLowerCase().includes(search) ||
          h.id.toLowerCase().includes(search)
      );
    }

    // Filtro por cluster
    if (filterCluster !== "all") {
      filtered = filtered.filter((h) => h.cluster === filterCluster);
    }

    // Filtro por status
    if (filterStatus !== "all") {
      filtered = filtered.filter((h) => h.overall_status === filterStatus);
    }

    // Filtro por data
    if (filterDateRange !== "all") {
      const now = new Date();
      const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate());

      filtered = filtered.filter((h) => {
        const date = new Date(h.started_at);
        switch (filterDateRange) {
          case "today":
            return date >= startOfDay;
          case "week":
            const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            return date >= weekAgo;
          case "month":
            const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
            return date >= monthAgo;
          case "custom":
            // Filtro personalizado com date range
            if (!customDateRange) return true;

            // Se apenas "from" está definido, filtra a partir dessa data
            if (customDateRange.from && !customDateRange.to) {
              const fromDate = new Date(customDateRange.from);
              fromDate.setHours(0, 0, 0, 0);
              return date >= fromDate;
            }

            // Se ambos "from" e "to" estão definidos, filtra o range
            if (customDateRange.from && customDateRange.to) {
              const fromDate = new Date(customDateRange.from);
              fromDate.setHours(0, 0, 0, 0);
              const toDate = new Date(customDateRange.to);
              toDate.setHours(23, 59, 59, 999);
              return date >= fromDate && date <= toDate;
            }

            return true;
          default:
            return true;
        }
      });
    }

    return filtered;
  }, [history, searchText, filterCluster, filterStatus, filterDateRange, customDateRange]);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const formatDuration = (ms: number) => {
    const seconds = Math.floor(ms / 1000);
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;
    return `${minutes}m ${remainingSeconds}s`;
  };

  const formatDateRange = (range: DateRange | undefined): string => {
    if (!range) return "Selecionar período";

    const formatShort = (date: Date) => {
      return date.toLocaleDateString("pt-BR", {
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
      });
    };

    if (range.from && range.to) {
      return `${formatShort(range.from)} - ${formatShort(range.to)}`;
    } else if (range.from) {
      return `A partir de ${formatShort(range.from)}`;
    }

    return "Selecionar período";
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "healthy":
        return (
          <Badge variant="outline" className="border-green-500 bg-green-50 text-green-700">
            <CheckCircle2 className="mr-1 h-3 w-3" />
            Healthy
          </Badge>
        );
      case "warning":
        return (
          <Badge variant="outline" className="border-yellow-500 bg-yellow-50 text-yellow-700">
            <AlertCircle className="mr-1 h-3 w-3" />
            Warning
          </Badge>
        );
      case "critical":
        return (
          <Badge variant="destructive">
            <XCircle className="mr-1 h-3 w-3" />
            Critical
          </Badge>
        );
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  const clearFilters = () => {
    setSearchText("");
    setFilterCluster("all");
    setFilterStatus("all");
    setFilterDateRange("all");
    setCustomDateRange(undefined);
  };

  const hasActiveFilters =
    searchText.trim() !== "" ||
    filterCluster !== "all" ||
    filterStatus !== "all" ||
    filterDateRange !== "all";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-w-5xl max-h-[90vh]"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Clock className="h-5 w-5 text-blue-600" />
              <DialogTitle>Histórico de Análises</DialogTitle>
            </div>
            <div className="flex items-center gap-2">
              {/* Botão Exportar - Aparece quando há resultados filtrados */}
              {filteredHistory.length > 0 && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowExportModal(true)}
                  className="h-7"
                >
                  <Download className="mr-1.5 h-3.5 w-3.5" />
                  <span className="text-xs">Exportar</span>
                </Button>
              )}
              <Badge variant="secondary">{filteredHistory.length} resultado(s)</Badge>
            </div>
          </div>
          <DialogDescription>
            Visualize e filtre análises anteriores de Health Check
          </DialogDescription>
        </DialogHeader>

        {/* Filtros */}
        <div className="space-y-3 border-b pb-4">
          <div className="flex items-center gap-2">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">Filtros</span>
            {hasActiveFilters && (
              <Button
                variant="ghost"
                size="sm"
                onClick={clearFilters}
                className="h-6 text-xs"
              >
                <X className="mr-1 h-3 w-3" />
                Limpar
              </Button>
            )}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
            {/* Busca por texto */}
            <div className="space-y-1.5">
              <Label htmlFor="search" className="text-xs">Buscar</Label>
              <div className="relative">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  id="search"
                  placeholder="Cluster ou ID..."
                  value={searchText}
                  onChange={(e) => setSearchText(e.target.value)}
                  className="pl-8 h-9 text-sm"
                />
              </div>
            </div>

            {/* Filtro por cluster */}
            <div className="space-y-1.5">
              <Label htmlFor="cluster" className="text-xs">Cluster</Label>
              <Select value={filterCluster} onValueChange={setFilterCluster}>
                <SelectTrigger id="cluster" className="h-9 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos os clusters</SelectItem>
                  {uniqueClusters.map((cluster) => (
                    <SelectItem key={cluster} value={cluster}>
                      {cluster}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Filtro por status */}
            <div className="space-y-1.5">
              <Label htmlFor="status" className="text-xs">Status</Label>
              <Select value={filterStatus} onValueChange={setFilterStatus}>
                <SelectTrigger id="status" className="h-9 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos os status</SelectItem>
                  <SelectItem value="healthy">Healthy</SelectItem>
                  <SelectItem value="warning">Warning</SelectItem>
                  <SelectItem value="critical">Critical</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Filtro por data */}
            <div className="space-y-1.5">
              <Label htmlFor="date" className="text-xs">Período</Label>
              <Select
                value={filterDateRange}
                onValueChange={(value) => {
                  setFilterDateRange(value);
                  if (value !== "custom") {
                    setCustomDateRange(undefined);
                  } else {
                    setIsDatePickerOpen(true);
                  }
                }}
              >
                <SelectTrigger id="date" className="h-9 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todas as datas</SelectItem>
                  <SelectItem value="today">Hoje</SelectItem>
                  <SelectItem value="week">Última semana</SelectItem>
                  <SelectItem value="month">Último mês</SelectItem>
                  <SelectItem value="custom">Personalizado</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {/* Date Picker para período personalizado */}
          {filterDateRange === "custom" && (
            <div className="mt-3 pt-3 border-t">
              <div className="flex items-center gap-2">
                <Label className="text-xs">Período personalizado:</Label>
                <Popover open={isDatePickerOpen} onOpenChange={setIsDatePickerOpen}>
                  <PopoverTrigger asChild>
                    <Button
                      variant="outline"
                      size="sm"
                      className={cn(
                        "h-9 text-sm justify-start text-left font-normal",
                        !customDateRange && "text-muted-foreground"
                      )}
                    >
                      <CalendarIcon className="mr-2 h-4 w-4" />
                      {formatDateRange(customDateRange)}
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-auto p-0" align="start">
                    <div className="p-3 space-y-3">
                      <div className="text-sm font-medium">Selecione o período</div>
                      <Calendar
                        mode="range"
                        selected={customDateRange}
                        onSelect={setCustomDateRange}
                        numberOfMonths={2}
                        disabled={(date) => date > new Date()}
                      />
                      <div className="flex items-center gap-2 pt-2 border-t">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setCustomDateRange(undefined);
                            setIsDatePickerOpen(false);
                          }}
                          className="flex-1"
                        >
                          Limpar
                        </Button>
                        <Button
                          size="sm"
                          onClick={() => setIsDatePickerOpen(false)}
                          className="flex-1"
                        >
                          Aplicar
                        </Button>
                      </div>
                    </div>
                  </PopoverContent>
                </Popover>
                {customDateRange && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setCustomDateRange(undefined);
                    }}
                    className="h-7 px-2"
                  >
                    <X className="h-3 w-3" />
                  </Button>
                )}
              </div>
            </div>
          )}
        </div>

        {/* Lista de resultados */}
        <ScrollArea className="h-[500px] pr-4">
          {loading ? (
            <div className="flex items-center justify-center h-40">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : filteredHistory.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-40 text-center">
              {hasActiveFilters ? (
                <>
                  <Search className="h-12 w-12 text-muted-foreground mb-2" />
                  <p className="text-sm text-muted-foreground">
                    Nenhuma análise encontrada com os filtros aplicados
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={clearFilters}
                    className="mt-3"
                  >
                    <X className="mr-1.5 h-3.5 w-3.5" />
                    Limpar filtros
                  </Button>
                </>
              ) : (
                <>
                  <Clock className="h-12 w-12 text-muted-foreground mb-2" />
                  <p className="text-sm text-muted-foreground">
                    Nenhuma análise encontrada
                  </p>
                  <p className="text-xs text-muted-foreground mt-1">
                    Execute um Health Check para ver o histórico aqui
                  </p>
                </>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {filteredHistory.map((result) => (
                <Card key={result.id} className="hover:border-blue-500 transition-colors">
                  <CardHeader className="pb-3">
                    <div className="flex items-start justify-between">
                      <div className="space-y-1 flex-1">
                        <div className="flex items-center gap-2">
                          <Server className="h-4 w-4 text-muted-foreground" />
                          <CardTitle className="text-sm font-semibold">
                            {result.cluster}
                          </CardTitle>
                          {getStatusBadge(result.overall_status)}
                        </div>
                        <div className="flex items-center gap-4 text-xs text-muted-foreground">
                          <span className="flex items-center gap-1">
                            <Clock className="h-3 w-3" />
                            {formatDate(result.started_at)}
                          </span>
                          <span>Duração: {formatDuration(result.duration_ms)}</span>
                        </div>
                      </div>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            onSelectHistory(result);
                            onOpenChange(false);
                          }}
                        >
                          <Eye className="mr-1.5 h-3.5 w-3.5" />
                          Visualizar
                        </Button>
                        {/* Botão Exportar Alertas - só aparece se houver warnings/criticals */}
                        {(result.warning_count > 0 || result.critical_count > 0) && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={async () => {
                              try {
                                toast.loading("Carregando alertas...", { id: "export-alerts" });

                                // Buscar eventos do banco
                                const response = await apiClient.getHealthCheckEvents(result.id);

                                if (!response.success || !response.events) {
                                  toast.error("Nenhum alerta encontrado", { id: "export-alerts" });
                                  return;
                                }

                                // Filtrar apenas warnings e criticals
                                const alerts = response.events.filter(
                                  (e: any) => e.status === "warning" || e.status === "critical"
                                );

                                if (alerts.length === 0) {
                                  toast.info("Nenhum alerta encontrado", { id: "export-alerts" });
                                  return;
                                }

                                toast.dismiss("export-alerts");

                                // Preparar dados para exportação
                                setAlertsToExport({
                                  cluster: result.cluster,
                                  events: alerts
                                });
                                setAlertsExportModalOpen(true);

                              } catch (error) {
                                console.error("Erro ao exportar alertas:", error);
                                toast.error("Erro ao carregar alertas", { id: "export-alerts" });
                              }
                            }}
                            className="text-xs"
                          >
                            <Download className="mr-1.5 h-3.5 w-3.5" />
                            Exportar Alertas
                          </Button>
                        )}
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent className="pt-0">
                    <div className="flex items-center gap-4 text-sm">
                      <div className="flex items-center gap-1.5">
                        <CheckCircle2 className="h-4 w-4 text-green-600" />
                        <span className="font-medium text-green-700">{result.healthy_count}</span>
                        <span className="text-muted-foreground">Healthy</span>
                      </div>
                      <div className="flex items-center gap-1.5">
                        <AlertCircle className="h-4 w-4 text-yellow-600" />
                        <span className="font-medium text-yellow-700">{result.warning_count}</span>
                        <span className="text-muted-foreground">Warning</span>
                      </div>
                      <div className="flex items-center gap-1.5">
                        <XCircle className="h-4 w-4 text-red-600" />
                        <span className="font-medium text-red-700">{result.critical_count}</span>
                        <span className="text-muted-foreground">Critical</span>
                      </div>
                      <div className="flex items-center gap-1.5 ml-auto">
                        <span className="text-muted-foreground">Total:</span>
                        <span className="font-semibold">{result.total_checks}</span>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </ScrollArea>
      </DialogContent>

      {/* Export Report Modal */}
      <ExportReportModal
        open={showExportModal}
        onOpenChange={setShowExportModal}
        results={filteredHistory.reduce((acc, result) => {
          acc[result.cluster] = result;
          return acc;
        }, {} as Record<string, HealthCheckResult>)}
      />

      {/* Alerts Export Modal */}
      {alertsExportModalOpen && alertsToExport.events.length > 0 && (
        <HealthCheckAlertsExportModal
          open={alertsExportModalOpen}
          onOpenChange={setAlertsExportModalOpen}
          cluster={alertsToExport.cluster}
          alerts={alertsToExport.events}
        />
      )}
    </Dialog>
  );
};
