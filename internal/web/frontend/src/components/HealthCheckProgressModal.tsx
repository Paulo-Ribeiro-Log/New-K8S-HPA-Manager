import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
} from "lucide-react";
import { useHealthCheckProgress } from "@/hooks/useHealthCheckProgress";
import type { HealthCheckResult, HealthCheckProgress } from "@/types/healthcheck";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface HealthCheckProgressModalProps {
  sessionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete: (result: HealthCheckResult) => void;
}

export const HealthCheckProgressModal = ({
  sessionId,
  open,
  onOpenChange,
  onComplete,
}: HealthCheckProgressModalProps) => {
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
    enabled: open,
    onComplete: async () => {
      // Fetch final result
      try {
        const response = await apiClient.getHealthCheckResult(sessionId);
        if (response.success && response.data) {
          setResult(response.data);
          onComplete(response.data);
        }
      } catch (error) {
        console.error("Failed to fetch final result:", error);
        toast.error("Erro ao buscar resultado final");
      }
    },
    onError: (error) => {
      console.error("SSE Error:", error);
      toast.error("Erro na conexão SSE");
    },
  });

  // ✅ Resetar estado quando modal fechar
  useEffect(() => {
    if (!open) {
      setResult(null);
      clearEvents();
      setFilter("all");
    }
  }, [open, clearEvents]);

  // Filtrar eventos baseado no filtro selecionado
  const filteredEvents = events.filter((event) => {
    if (filter === "all") return true;
    return event.status === filter;
  });

  // Handle cancel
  const handleCancel = () => {
    disconnect();
    toast.info("Health check cancelado");
    onOpenChange(false);
  };

  const progress = getProgress();
  const phase = getCurrentPhase();
  const message = getCurrentMessage();

  // Contar eventos em tempo real (para exibir contadores durante progresso)
  const liveHealthy = events.filter(e => e.status === "healthy").length;
  const liveWarning = events.filter(e => e.status === "warning").length;
  const liveCritical = events.filter(e => e.status === "critical").length;
  const liveTotal = events.length;

  // Status icon based on current state
  const getStatusIcon = () => {
    if (hasError()) {
      return <XCircle className="h-6 w-6 text-red-600" />;
    }
    if (isComplete()) {
      return <CheckCircle2 className="h-6 w-6 text-green-600" />;
    }
    return <Loader2 className="h-6 w-6 text-blue-600 animate-spin" />;
  };

  // Event badge variant with custom colors
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

  // Event badge label
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[80vh] overflow-hidden">
        <DialogHeader>
          <div className="flex items-center gap-2">
            {getStatusIcon()}
            <DialogTitle>
              {isComplete() ? "Health Check Concluído" : "Health Check em Progresso"}
            </DialogTitle>
          </div>
          <DialogDescription>
            {isComplete()
              ? "Análise completa do cluster"
              : "Verificando saúde dos recursos do cluster..."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 overflow-x-hidden">
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

          {/* Live Summary Filters (durante progresso) */}
          {!result && liveTotal > 0 && (
            <div className="grid grid-cols-4 gap-2">
              <Button
                variant={filter === "healthy" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-green-600">
                  {liveHealthy}
                </div>
                <div className="text-xs">Healthy</div>
              </Button>
              <Button
                variant={filter === "warning" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-yellow-600">
                  {liveWarning}
                </div>
                <div className="text-xs">Warnings</div>
              </Button>
              <Button
                variant={filter === "critical" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-red-600">
                  {liveCritical}
                </div>
                <div className="text-xs">Critical</div>
              </Button>
              <Button
                variant={filter === "all" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter("all")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold">
                  {liveTotal}
                </div>
                <div className="text-xs">Total</div>
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
            <ScrollArea className="h-[400px] rounded-md border p-4">
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
            <div className="grid grid-cols-4 gap-2">
              <Button
                variant={filter === "healthy" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-green-600">
                  {result.healthy_count}
                </div>
                <div className="text-xs">Healthy</div>
              </Button>
              <Button
                variant={filter === "warning" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-yellow-600">
                  {result.warning_count}
                </div>
                <div className="text-xs">Warnings</div>
              </Button>
              <Button
                variant={filter === "critical" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold text-red-600">
                  {result.critical_count}
                </div>
                <div className="text-xs">Critical</div>
              </Button>
              <Button
                variant={filter === "all" ? "default" : "outline"}
                size="sm"
                onClick={() => setFilter("all")}
                className="flex flex-col h-auto py-3"
              >
                <div className="text-2xl font-bold">
                  {result.total_checks}
                </div>
                <div className="text-xs">Total</div>
              </Button>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-between gap-2">
            {/* Cancel/Stop button - only show when in progress */}
            {!isComplete() && !hasError() && (
              <Button
                variant="destructive"
                onClick={handleCancel}
                disabled={!isConnected}
              >
                <X className="mr-2 h-4 w-4" />
                Cancelar
              </Button>
            )}

            {/* Close/Minimize button */}
            <div className="flex gap-2 ml-auto">
              {isComplete() ? (
                <Button onClick={() => onOpenChange(false)}>
                  <CheckCircle2 className="mr-2 h-4 w-4" />
                  Fechar
                </Button>
              ) : (
                <Button variant="outline" onClick={() => onOpenChange(false)}>
                  <X className="mr-2 h-4 w-4" />
                  Minimizar
                </Button>
              )}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
