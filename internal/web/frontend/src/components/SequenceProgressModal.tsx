import React, { useEffect, useState, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CheckCircle2, Circle, AlertCircle, Loader2, XCircle } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";

// Event types do SSE
interface ProgressEvent {
  phase: number;
  phase_name: string;
  status: "running" | "completed" | "error";
  message: string;
  progress: number;
  node_name?: string;
  node_index?: number;
  node_total?: number;
  timestamp: string;
  error?: string;
}

interface SequenceProgressModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  clusterName: string;
  originName: string;
  destName: string;
}

const PHASES = [
  { id: 1, name: "PRE-DRAIN", description: "Scale UP destination" },
  { id: 2, name: "CORDON", description: "Mark nodes unschedulable" },
  { id: 3, name: "DRAIN", description: "Migrate pods" },
  { id: 4, name: "POST-DRAIN", description: "Scale DOWN origin" },
  { id: 5, name: "FINALIZAÇÃO", description: "Cleanup" },
];

export default function SequenceProgressModal({
  open,
  onOpenChange,
  sessionId,
  clusterName,
  originName,
  destName,
}: SequenceProgressModalProps) {
  const [currentPhase, setCurrentPhase] = useState(0);
  const [overallProgress, setOverallProgress] = useState(0);
  const [events, setEvents] = useState<ProgressEvent[]>([]);
  const [isComplete, setIsComplete] = useState(false);
  const [hasError, setHasError] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open || !sessionId) return;

    // Conectar ao endpoint SSE
    const eventSource = new EventSource(
      `/api/v1/nodepools/sequence/progress?session_id=${sessionId}`
    );
    eventSourceRef.current = eventSource;

    // Listener para eventos de progresso
    eventSource.addEventListener("progress", (e: MessageEvent) => {
      try {
        const event: ProgressEvent = JSON.parse(e.data);
        setEvents((prev) => [...prev, event]);
        setCurrentPhase(event.phase);
        setOverallProgress(event.progress);

        if (event.status === "error") {
          setHasError(true);
        }

        // Auto-scroll para o último evento
        if (scrollRef.current) {
          scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
      } catch (error) {
        console.error("Failed to parse SSE event:", error);
      }
    });

    // Listener para evento de fechamento
    eventSource.addEventListener("close", () => {
      setIsComplete(true);
      eventSource.close();
    });

    // Listener de erro
    eventSource.onerror = (error) => {
      console.error("SSE error:", error);
      eventSource.close();
    };

    // Cleanup ao desmontar
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
    };
  }, [open, sessionId]);

  // Renderizar ícone de status da fase
  const getPhaseIcon = (phaseId: number) => {
    if (phaseId < currentPhase) {
      return <CheckCircle2 className="h-5 w-5 text-green-500" />;
    } else if (phaseId === currentPhase) {
      if (hasError) {
        return <XCircle className="h-5 w-5 text-red-500" />;
      }
      return <Loader2 className="h-5 w-5 text-blue-500 animate-spin" />;
    } else {
      return <Circle className="h-5 w-5 text-gray-300" />;
    }
  };

  // Renderizar badge de status do evento
  const getEventBadge = (status: string) => {
    switch (status) {
      case "running":
        return <Badge variant="default">Running</Badge>;
      case "completed":
        return <Badge variant="outline" className="bg-green-50">Completed</Badge>;
      case "error":
        return <Badge variant="destructive">Error</Badge>;
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden">
        <DialogHeader>
          <DialogTitle>Node Pool Sequencing Progress</DialogTitle>
          <DialogDescription>
            Cluster: {clusterName} | Origin: {originName} → Dest: {destName}
          </DialogDescription>
        </DialogHeader>

        {/* Progress bar geral */}
        <div className="space-y-2">
          <div className="flex justify-between text-sm">
            <span className="font-medium">Overall Progress</span>
            <span className="text-muted-foreground">{Math.round(overallProgress)}%</span>
          </div>
          <Progress value={overallProgress} className="h-3" />
        </div>

        {/* Indicadores de fases */}
        <div className="grid grid-cols-5 gap-2">
          {PHASES.map((phase) => (
            <div
              key={phase.id}
              className={`flex flex-col items-center p-3 rounded-lg border ${
                phase.id === currentPhase
                  ? "border-blue-500 bg-blue-50"
                  : phase.id < currentPhase
                  ? "border-green-500 bg-green-50"
                  : "border-gray-200 bg-gray-50"
              }`}
            >
              {getPhaseIcon(phase.id)}
              <span className="text-xs font-medium mt-1">{phase.name}</span>
              <span className="text-xs text-muted-foreground text-center">
                {phase.description}
              </span>
            </div>
          ))}
        </div>

        {/* Log de eventos */}
        <div className="space-y-2">
          <h3 className="text-sm font-semibold">Execution Log</h3>
          <ScrollArea className="h-[300px] border rounded-lg p-3" ref={scrollRef}>
            <div className="space-y-2">
              {events.map((event, idx) => (
                <div
                  key={idx}
                  className="flex items-start gap-3 text-sm border-b pb-2 last:border-0"
                >
                  <span className="text-xs text-muted-foreground whitespace-nowrap">
                    {new Date(event.timestamp).toLocaleTimeString()}
                  </span>
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-blue-600">
                        [{event.phase}] {event.phase_name}
                      </span>
                      {getEventBadge(event.status)}
                      {event.node_index && event.node_total && (
                        <span className="text-xs text-muted-foreground">
                          Node {event.node_index}/{event.node_total}
                        </span>
                      )}
                    </div>
                    <p className="text-xs">{event.message}</p>
                    {event.node_name && (
                      <p className="text-xs text-muted-foreground font-mono">
                        Node: {event.node_name}
                      </p>
                    )}
                    {event.error && (
                      <Alert variant="destructive" className="mt-2">
                        <AlertCircle className="h-4 w-4" />
                        <AlertDescription className="text-xs">
                          {event.error}
                        </AlertDescription>
                      </Alert>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
        </div>

        {/* Botão de fechar (só aparece quando completa) */}
        {isComplete && (
          <div className="flex justify-end">
            <Button onClick={() => onOpenChange(false)}>
              {hasError ? "Close" : "Done"}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
