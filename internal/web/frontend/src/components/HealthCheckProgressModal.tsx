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

  const {
    events,
    isConnected,
    getProgress,
    getCurrentPhase,
    getCurrentMessage,
    isComplete,
    hasError,
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

  const progress = getProgress();
  const phase = getCurrentPhase();
  const message = getCurrentMessage();

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

  // Event type icons
  const getEventIcon = (event: HealthCheckProgress) => {
    switch (event.status) {
      case "healthy":
        return <CheckCircle2 className="h-4 w-4 text-green-600" />;
      case "warning":
        return <AlertCircle className="h-4 w-4 text-yellow-600" />;
      case "critical":
        return <XCircle className="h-4 w-4 text-red-600" />;
      default:
        return <Activity className="h-4 w-4 text-blue-600" />;
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh]">
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

        <div className="space-y-4">
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
            <div className="text-sm text-muted-foreground">
              {message}
            </div>
          )}

          {/* Event Log */}
          <div className="space-y-2">
            <h3 className="text-sm font-medium">Log de Eventos</h3>
            <ScrollArea className="h-[300px] rounded-md border p-4">
              <div className="space-y-2">
                {events.length === 0 && (
                  <div className="text-sm text-muted-foreground text-center py-4">
                    Aguardando eventos...
                  </div>
                )}

                {events.map((event, i) => (
                  <div key={i} className="flex items-start gap-2 text-sm">
                    <div className="mt-0.5">{getEventIcon(event)}</div>
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{event.message}</span>
                        {event.phase !== "complete" && event.phase !== "error" && (
                          <Badge variant="outline" className="text-xs">
                            {event.phase}
                          </Badge>
                        )}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {new Date(event.timestamp).toLocaleTimeString()}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </ScrollArea>
          </div>

          {/* Summary (when complete) */}
          {result && (
            <div className="grid grid-cols-4 gap-4 p-4 bg-muted rounded-lg">
              <div className="text-center">
                <div className="text-2xl font-bold text-green-600">
                  {result.healthy_count}
                </div>
                <div className="text-xs text-muted-foreground">Healthy</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-yellow-600">
                  {result.warning_count}
                </div>
                <div className="text-xs text-muted-foreground">Warnings</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-red-600">
                  {result.critical_count}
                </div>
                <div className="text-xs text-muted-foreground">Critical</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">
                  {result.total_checks}
                </div>
                <div className="text-xs text-muted-foreground">Total</div>
              </div>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2">
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
      </DialogContent>
    </Dialog>
  );
};
