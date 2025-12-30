import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  CheckCircle2,
  AlertCircle,
  XCircle,
  Loader2,
  Activity,
  X,
  Server,
} from "lucide-react";
import { useHealthCheckProgress } from "@/hooks/useHealthCheckProgress";
import type { HealthCheckResult, HealthCheckProgress } from "@/types/healthcheck";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface HealthCheckProgressModalProps {
  sessionId: string;                           // Base session ID
  clusterSessions: Record<string, string>;     // Map: cluster -> sessionID único
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete: (result: HealthCheckResult) => void;
}

// Componente filho para cada cluster tab
interface ClusterTabContentProps {
  cluster: string;
  sessionId: string;
  enabled: boolean;
  onComplete: (result: HealthCheckResult) => void;
  onError: (error: string) => void;
}

const ClusterTabContent = ({
  cluster,
  sessionId,
  enabled,
  onComplete,
  onError,
}: ClusterTabContentProps) => {
  const [result, setResult] = useState<HealthCheckResult | null>(null);
  const [filter, setFilter] = useState<"all" | "healthy" | "warning" | "critical">("all");

  const {
    events,
    isConnected,
    getProgress,
    getCurrentPhase,
    getCurrentMessage,
    isComplete,
    hasError,
    disconnect,
    clearEvents,
  } = useHealthCheckProgress({
    sessionId,
    enabled,
    onComplete: async () => {
      // Fetch final result
      try {
        const response = await apiClient.getHealthCheckResult(sessionId);
        if (response.success && response.data) {
          setResult(response.data);
          onComplete(response.data);
        }
      } catch (error) {
        console.error(`[${cluster}] Failed to fetch final result:`, error);
        toast.error(`[${cluster}] Erro ao buscar resultado final`);
      }
    },
    onError: (error) => {
      console.error(`[${cluster}] SSE Error:`, error);
      onError(error);
    },
  });

  // ❌ REMOVIDO: Não limpar eventos ao trocar de tab
  // Eventos devem persistir entre trocas de tab
  // Só limpar quando modal fechar completamente (gerenciado pelo componente pai)

  // Filtrar eventos baseado no filtro selecionado
  const filteredEvents = events.filter((event) => {
    if (filter === "all") return true;
    return event.status === filter;
  });

  const progress = getProgress();
  const phase = getCurrentPhase();
  const message = getCurrentMessage();

  // Contar eventos em tempo real
  const liveHealthy = events.filter(e => e.status === "healthy").length;
  const liveWarning = events.filter(e => e.status === "warning").length;
  const liveCritical = events.filter(e => e.status === "critical").length;
  const liveTotal = events.length;

  // Status icon
  const getStatusIcon = () => {
    if (hasError()) {
      return <XCircle className="h-5 w-5 text-red-600" />;
    }
    if (isComplete()) {
      return <CheckCircle2 className="h-5 w-5 text-green-600" />;
    }
    return <Loader2 className="h-5 w-5 text-blue-600 animate-spin" />;
  };

  // Event badge
  const getEventBadgeProps = (status: string): { variant: "default" | "secondary" | "destructive" | "outline", className?: string } => {
    switch (status) {
      case "healthy":
        return { variant: "outline", className: "border-green-500 bg-green-50 text-green-700" };
      case "warning":
        return { variant: "outline", className: "border-yellow-500 bg-yellow-50 text-yellow-700" };
      case "critical":
        return { variant: "destructive" };
      default:
        return { variant: "outline" };
    }
  };

  const getEventBadgeLabel = (status: string): string => {
    switch (status) {
      case "healthy":
        return "Healthy";
      case "warning":
        return "Warning";
      case "critical":
        return "Critical";
      default:
        return "Unknown";
    }
  };

  return (
    <div className="space-y-4">
      {/* Status Header */}
      <div className="flex items-center gap-2">
        {getStatusIcon()}
        <span className="text-sm font-medium">
          {isComplete() ? `Concluído - ${cluster}` : `Em Progresso - ${cluster}`}
        </span>
      </div>

      {/* Progress Bar */}
      {!isComplete() && !hasError() && (
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="font-medium">{phase}</span>
            <span className="text-muted-foreground">{progress}%</span>
          </div>
          <Progress value={progress} className="h-2" />
        </div>
      )}

      {/* Current Message */}
      {!isComplete() && (
        <div className="text-sm text-muted-foreground break-words whitespace-normal">
          {message}
        </div>
      )}

      {/* Live Summary Filters */}
      {!result && liveTotal > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <Button
            variant={filter === "healthy" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-green-600">
              {liveHealthy}
            </div>
            <div className="text-[10px] sm:text-xs">Healthy</div>
          </Button>
          <Button
            variant={filter === "warning" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-yellow-600">
              {liveWarning}
            </div>
            <div className="text-[10px] sm:text-xs">Warnings</div>
          </Button>
          <Button
            variant={filter === "critical" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-red-600">
              {liveCritical}
            </div>
            <div className="text-[10px] sm:text-xs">Critical</div>
          </Button>
          <Button
            variant={filter === "all" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter("all")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold">
              {liveTotal}
            </div>
            <div className="text-[10px] sm:text-xs">Total</div>
          </Button>
        </div>
      )}

      {/* Event Log */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">Log de Eventos</h3>
          {filter !== "all" && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setFilter("all")}
              className="h-6 text-xs"
            >
              Limpar filtro
            </Button>
          )}
        </div>
        <ScrollArea className="h-[250px] sm:h-[350px] rounded-md border p-2 sm:p-4">
          <div className="space-y-2 max-w-full overflow-hidden">
            {filteredEvents.length === 0 && events.length === 0 && (
              <div className="text-sm text-muted-foreground text-center py-4">
                Aguardando eventos...
              </div>
            )}

            {filteredEvents.length === 0 && events.length > 0 && (
              <div className="text-sm text-muted-foreground text-center py-4">
                Nenhum evento encontrado para o filtro selecionado
              </div>
            )}

            {filteredEvents.map((event, i) => {
              const badgeProps = getEventBadgeProps(event.status);
              return (
                <div key={i} className="flex items-start justify-between gap-2 text-sm border-b pb-2">
                  <div className="flex-1 space-y-1 min-w-0" style={{ maxWidth: "calc(100% - 80px)" }}>
                    <div
                      className="font-medium"
                      style={{
                        wordBreak: "break-word",
                        overflowWrap: "break-word",
                        whiteSpace: "normal",
                        maxWidth: "100%"
                      }}
                    >
                      {event.message}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {new Date(event.timestamp).toLocaleTimeString()}
                    </div>
                  </div>
                  <Badge
                    variant={badgeProps.variant}
                    className={`text-xs shrink-0 ${badgeProps.className || ''}`}
                  >
                    {getEventBadgeLabel(event.status)}
                  </Badge>
                </div>
              );
            })}
          </div>
        </ScrollArea>
      </div>

      {/* Summary Filters (when complete) */}
      {result && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <Button
            variant={filter === "healthy" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-green-600">
              {result.healthy_count}
            </div>
            <div className="text-[10px] sm:text-xs">Healthy</div>
          </Button>
          <Button
            variant={filter === "warning" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-yellow-600">
              {result.warning_count}
            </div>
            <div className="text-[10px] sm:text-xs">Warnings</div>
          </Button>
          <Button
            variant={filter === "critical" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold text-red-600">
              {result.critical_count}
            </div>
            <div className="text-[10px] sm:text-xs">Critical</div>
          </Button>
          <Button
            variant={filter === "all" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter("all")}
            className="flex flex-col h-auto py-1.5 sm:py-2"
          >
            <div className="text-lg sm:text-xl font-bold">
              {result.total_checks}
            </div>
            <div className="text-[10px] sm:text-xs">Total</div>
          </Button>
        </div>
      )}
    </div>
  );
};

// Componente principal do modal
export const HealthCheckProgressModal = ({
  sessionId,
  clusterSessions,
  open,
  onOpenChange,
  onComplete,
}: HealthCheckProgressModalProps) => {
  const [activeTab, setActiveTab] = useState<string>("");
  const [completedClusters, setCompletedClusters] = useState<Set<string>>(new Set());
  const [failedClusters, setFailedClusters] = useState<Set<string>>(new Set());

  // Extrair clusters do clusterSessions
  const clusters = Object.keys(clusterSessions);

  // ✅ Resetar estado quando modal fechar
  useEffect(() => {
    if (!open) {
      setCompletedClusters(new Set());
      setFailedClusters(new Set());
      setActiveTab("");
    } else {
      // Auto-select primeiro cluster ao abrir
      if (clusters.length > 0 && !activeTab) {
        setActiveTab(clusters[0]);
      }
    }
  }, [open, clusters, activeTab]);

  // Handle complete de um cluster específico
  const handleClusterComplete = (cluster: string) => (result: HealthCheckResult) => {
    console.log(`[${cluster}] Health check completed:`, result);
    setCompletedClusters(prev => new Set(prev).add(cluster));
    onComplete(result);
  };

  // Handle error de um cluster específico
  const handleClusterError = (cluster: string) => (error: string) => {
    console.error(`[${cluster}] Health check error:`, error);
    setFailedClusters(prev => new Set(prev).add(cluster));
    toast.error(`[${cluster}] Erro na conexão SSE`);
  };

  // Handle cancel
  const handleCancel = () => {
    toast.info("Health check cancelado");
    onOpenChange(false);
  };

  // Status geral
  const allCompleted = completedClusters.size === clusters.length;
  const hasFailures = failedClusters.size > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:max-w-4xl max-h-[90vh] sm:max-h-[85vh] overflow-hidden">
        <DialogHeader>
          <div className="flex items-center gap-2">
            {allCompleted ? (
              <CheckCircle2 className="h-5 w-5 sm:h-6 sm:w-6 text-green-600" />
            ) : hasFailures ? (
              <XCircle className="h-5 w-5 sm:h-6 sm:w-6 text-red-600" />
            ) : (
              <Loader2 className="h-5 w-5 sm:h-6 sm:w-6 text-blue-600 animate-spin" />
            )}
            <DialogTitle className="text-base sm:text-lg">
              {allCompleted
                ? "Health Check Concluído"
                : `Health Check em Progresso (${completedClusters.size}/${clusters.length})`}
            </DialogTitle>
          </div>
          <DialogDescription className="text-xs sm:text-sm">
            {allCompleted
              ? "Análise completa de todos os clusters"
              : "Verificando saúde dos recursos dos clusters..."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 overflow-x-hidden">
          {/* Tabs para cada cluster */}
          {clusters.length === 1 ? (
            // Caso especial: 1 cluster apenas - não mostrar tabs
            <ClusterTabContent
              cluster={clusters[0]}
              sessionId={clusterSessions[clusters[0]]}
              enabled={open}
              onComplete={handleClusterComplete(clusters[0])}
              onError={handleClusterError(clusters[0])}
            />
          ) : (
            <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
              <TabsList className="inline-flex w-full overflow-x-auto" style={{ gridTemplateColumns: clusters.length <= 3 ? `repeat(${clusters.length}, 1fr)` : undefined }}>
                {clusters.map((cluster) => (
                  <TabsTrigger key={cluster} value={cluster} className="relative flex-shrink-0">
                    <div className="flex items-center gap-1 sm:gap-2">
                      <Server className="h-3 w-3 sm:h-4 sm:w-4" />
                      <span className="truncate max-w-[80px] sm:max-w-[150px] text-xs sm:text-sm">{cluster}</span>
                      {completedClusters.has(cluster) && (
                        <CheckCircle2 className="h-3 w-3 text-green-600 absolute -top-1 -right-1" />
                      )}
                      {failedClusters.has(cluster) && (
                        <XCircle className="h-3 w-3 text-red-600 absolute -top-1 -right-1" />
                      )}
                    </div>
                  </TabsTrigger>
                ))}
              </TabsList>

              {/* ✅ IMPORTANTE: Renderizar TODOS os ClusterTabContent simultaneamente (não desmontar ao trocar tab)
                  Usar className para ocultar visualmente as tabs inativas (display: none)
                  Isso mantém o estado de eventos persistente entre trocas de tab */}
              <div className="mt-4">
                {clusters.map((cluster) => (
                  <div
                    key={cluster}
                    className={activeTab === cluster ? "block" : "hidden"}
                  >
                    <ClusterTabContent
                      cluster={cluster}
                      sessionId={clusterSessions[cluster]}
                      enabled={open}
                      onComplete={handleClusterComplete(cluster)}
                      onError={handleClusterError(cluster)}
                    />
                  </div>
                ))}
              </div>
            </Tabs>
          )}

          {/* Actions */}
          <div className="flex flex-col sm:flex-row justify-between gap-2 pt-4 border-t">
            {/* Cancel/Stop button - only show when in progress */}
            {!allCompleted && (
              <Button
                variant="destructive"
                onClick={handleCancel}
                size="sm"
                className="w-full sm:w-auto"
              >
                <X className="mr-2 h-3 w-3 sm:h-4 sm:w-4" />
                <span className="text-xs sm:text-sm">Cancelar</span>
              </Button>
            )}

            {/* Close button */}
            <div className="flex gap-2 sm:ml-auto w-full sm:w-auto">
              {allCompleted ? (
                <Button onClick={() => onOpenChange(false)} size="sm" className="w-full sm:w-auto">
                  <CheckCircle2 className="mr-2 h-3 w-3 sm:h-4 sm:w-4" />
                  <span className="text-xs sm:text-sm">Fechar</span>
                </Button>
              ) : (
                <Button variant="outline" onClick={() => onOpenChange(false)} size="sm" className="w-full sm:w-auto">
                  <X className="mr-2 h-3 w-3 sm:h-4 sm:w-4" />
                  <span className="text-xs sm:text-sm">Minimizar</span>
                </Button>
              )}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
