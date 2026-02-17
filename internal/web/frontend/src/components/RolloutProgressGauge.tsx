import { X, Clock, CheckCircle2, Loader2, AlertCircle, Circle, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import { useEffect, useState, useRef } from "react";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";

export interface PodStatus {
  name: string;
  phase: "Running" | "Pending" | "Terminating" | "ContainerCreating" | "CrashLoopBackOff" | "Error" | "Unknown";
  ready: boolean;
  isNew: boolean;
  age?: string;
  restarts?: number;
  errorReason?: string; // ex: ImagePullBackOff, OOMKilled, ErrImagePull, etc.
}

interface RolloutProgressGaugeProps {
  deploymentName: string;
  progress: number;
  updated: number;
  newReady: number;
  desired: number;
  oldPods: number;
  unavailable: number;
  status: "running" | "completed";
  message: string;
  startTime?: number;
  pods?: PodStatus[];
  onClose?: () => void;
}

const formatDuration = (ms: number): string => {
  const totalSeconds = Math.floor(ms / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours.toString().padStart(2, "0")}:${minutes.toString().padStart(2, "0")}:${seconds.toString().padStart(2, "0")}`;
  }
  return `${minutes.toString().padStart(2, "0")}:${seconds.toString().padStart(2, "0")}`;
};

const getPodStatusInfo = (pod: PodStatus) => {
  if (pod.phase === "Running" && pod.ready)
    return { icon: CheckCircle2, color: "text-green-500", bgColor: "bg-green-500/10", label: "Pronto", animate: false };
  if (pod.phase === "Running" && !pod.ready)
    return { icon: Loader2, color: "text-yellow-500", bgColor: "bg-yellow-500/10", label: "Iniciando", animate: true };
  if (pod.phase === "Pending" || pod.phase === "ContainerCreating")
    return { icon: Loader2, color: "text-blue-500", bgColor: "bg-blue-500/10", label: "Criando", animate: true };
  if (pod.phase === "Terminating")
    return { icon: RefreshCw, color: "text-orange-500", bgColor: "bg-orange-500/10", label: "Terminando", animate: true };
  if (pod.phase === "CrashLoopBackOff" || pod.phase === "Error")
    return { icon: AlertCircle, color: "text-red-500", bgColor: "bg-red-500/10", label: "Erro", animate: false };
  return { icon: Circle, color: "text-muted-foreground", bgColor: "bg-muted/50", label: "Aguardando", animate: false };
};

// Formata o errorReason para exibição compacta
const formatErrorReason = (reason: string): string => {
  const map: Record<string, string> = {
    ImagePullBackOff: "ImagePullBackOff",
    ErrImagePull: "ErrImagePull",
    OOMKilled: "OOMKilled",
    CrashLoopBackOff: "CrashLoopBackOff",
    ContainerCannotRun: "ContainerCannotRun",
    RunContainerError: "RunContainerError",
    CreateContainerError: "CreateContainerError",
    CreateContainerConfigError: "ConfigError",
    InvalidImageName: "InvalidImageName",
    BackOff: "BackOff",
  };
  return map[reason] ?? reason;
};

// Cor do badge de motivo de erro
const getReasonBadgeClass = (reason: string): string => {
  const lower = reason.toLowerCase();
  if (lower.includes("imagepull") || lower.includes("errimagepull") || lower.includes("invalidimagename"))
    return "bg-purple-500/20 text-purple-400 border-purple-500/30";
  if (lower.includes("oomkilled"))
    return "bg-red-500/20 text-red-400 border-red-500/30";
  if (lower.includes("crashloop") || lower.includes("backoff"))
    return "bg-red-500/20 text-red-400 border-red-500/30";
  return "bg-orange-500/20 text-orange-400 border-orange-500/30";
};

export const RolloutProgressGauge = ({
  deploymentName,
  progress,
  updated,
  newReady,
  desired,
  oldPods,
  unavailable,
  status,
  message,
  startTime,
  pods = [],
  onClose,
}: RolloutProgressGaugeProps) => {
  const normalizedProgress = Math.min(Math.max(progress, 0), 100);
  const isCompleted = status === "completed";

  const [elapsedTime, setElapsedTime] = useState(0);
  const [finalTime, setFinalTime] = useState<number | null>(null);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  useEffect(() => {
    if (!startTime) return;
    if (isCompleted && finalTime !== null) { setElapsedTime(finalTime); return; }
    const initialElapsed = Date.now() - startTime;
    setElapsedTime(initialElapsed);
    if (timerRef.current) clearInterval(timerRef.current);
    if (!isCompleted) {
      timerRef.current = setInterval(() => setElapsedTime(Date.now() - startTime), 1000);
    }
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [startTime, isCompleted, finalTime]);

  useEffect(() => {
    if (isCompleted && startTime && finalTime === null) {
      const final = Date.now() - startTime;
      setFinalTime(final);
      setElapsedTime(final);
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null; }
    }
  }, [isCompleted, startTime, finalTime]);

  const newPods = pods.filter((p) => p.isNew);
  const oldPodsList = pods.filter((p) => !p.isNew);
  const allPodsSorted = [...newPods, ...oldPodsList];

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3">
        <div className={cn(
          "flex items-center justify-center w-10 h-10 rounded-full shrink-0",
          isCompleted ? "bg-green-500/20" : "bg-primary/20"
        )}>
          {isCompleted
            ? <CheckCircle2 className="w-5 h-5 text-green-500" />
            : <Loader2 className="w-5 h-5 text-primary animate-spin" />
          }
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-foreground truncate">{deploymentName}</p>
          <p className="text-xs text-muted-foreground truncate">{message}</p>
        </div>
        {startTime && (
          <div className={cn(
            "flex items-center gap-1.5 px-2.5 py-1.5 rounded-md font-mono text-sm shrink-0",
            isCompleted ? "bg-green-500/10 text-green-500" : "bg-primary/10 text-primary"
          )}>
            <Clock className="w-3.5 h-3.5" />
            <span className="tabular-nums font-semibold">{formatDuration(elapsedTime)}</span>
          </div>
        )}
        <div className={cn(
          "px-2 py-1 rounded-md text-sm font-mono font-bold shrink-0",
          isCompleted ? "bg-green-500/10 text-green-500" : "bg-primary/10 text-primary"
        )}>
          {normalizedProgress.toFixed(0)}%
        </div>
        {onClose && (
          <button type="button" onClick={onClose}
            className="rounded-md border border-border/50 p-1.5 text-muted-foreground transition hover:bg-muted hover:text-foreground shrink-0"
            aria-label="Fechar">
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Barra de progresso */}
      <Progress value={normalizedProgress} className={cn("h-2.5", isCompleted && "[&>div]:bg-green-500")} />

      {/* Cards de métricas */}
      <div className="grid grid-cols-4 gap-2 text-center">
        {[
          { value: `${updated}/${desired}`, label: "Atualizados", color: "text-primary" },
          { value: `${newReady}/${desired}`, label: "Prontos", color: "text-green-500" },
          { value: String(oldPods), label: "Terminando", color: oldPods > 0 ? "text-orange-500" : "text-muted-foreground" },
          { value: String(unavailable), label: "Indispon.", color: unavailable > 0 ? "text-red-500" : "text-muted-foreground" },
        ].map(({ value, label, color }) => (
          <div key={label} className="rounded-md bg-muted/50 p-2.5 border border-border/30">
            <p className={cn("text-lg font-mono font-bold", color)}>{value}</p>
            <p className="text-[10px] uppercase text-muted-foreground mt-0.5">{label}</p>
          </div>
        ))}
      </div>

      {/* Tabela de pods */}
      {allPodsSorted.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Pods ({allPodsSorted.length})
          </p>
          <ScrollArea className={cn(allPodsSorted.length > 6 ? "h-52" : "")}>
            <div className="space-y-1">
              {/* Header: mesma grade das linhas */}
              <div className="grid grid-cols-[1fr_80px_48px_62px] px-2 py-1 text-[10px] uppercase text-muted-foreground font-medium border-b border-border/30 mb-1">
                <span>Pod</span>
                <span className="text-center">Status</span>
                <span className="text-center">Ready</span>
                <span className="text-right pr-1">Restarts</span>
              </div>

              {allPodsSorted.map((pod) => {
                const info = getPodStatusInfo(pod);
                const Icon = info.icon;
                const shortName = pod.name.split("-").slice(-2).join("-");
                const hasError = !!pod.errorReason && (pod.phase === "Error" || pod.phase === "CrashLoopBackOff" || pod.phase === "Pending" || pod.phase === "ContainerCreating");

                return (
                  <div key={pod.name} className={cn("rounded-md text-xs", info.bgColor, !pod.isNew && "opacity-70")}>
                    {/* Linha principal */}
                    <div
                      className="grid grid-cols-[1fr_80px_48px_62px] px-2 py-1.5 items-center"
                      title={pod.name}
                    >
                      {/* Nome */}
                      <div className="flex items-center gap-1.5 min-w-0">
                        {!pod.isNew && (
                          <span className="text-[9px] px-1 py-0.5 rounded bg-orange-500/20 text-orange-400 font-medium shrink-0">
                            OLD
                          </span>
                        )}
                        <span className={cn("font-mono truncate", info.color)}>…{shortName}</span>
                      </div>

                      {/* Status */}
                      <div className="flex items-center justify-center gap-1">
                        <Icon className={cn("w-3.5 h-3.5 shrink-0", info.color, info.animate && "animate-spin")} />
                        <span className={cn("font-medium", info.color)}>{info.label}</span>
                      </div>

                      {/* Ready */}
                      <span className={cn(
                        "text-center font-mono",
                        pod.ready ? "text-green-500" : "text-muted-foreground"
                      )}>
                        {pod.ready ? "Sim" : "Não"}
                      </span>

                      {/* Restarts */}
                      <span className={cn(
                        "text-right font-mono pr-1",
                        (pod.restarts ?? 0) > 0 ? "text-orange-400" : "text-muted-foreground"
                      )}>
                        {pod.restarts ?? 0}
                      </span>
                    </div>

                    {/* Linha de motivo de erro — exibida abaixo quando há erro */}
                    {hasError && pod.errorReason && (
                      <div className="px-2 pb-1.5">
                        <span className={cn(
                          "inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-0.5 rounded border font-medium",
                          getReasonBadgeClass(pod.errorReason)
                        )}>
                          <AlertCircle className="w-3 h-3 shrink-0" />
                          {formatErrorReason(pod.errorReason)}
                        </span>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </ScrollArea>
        </div>
      )}

      {/* Mensagem de conclusão */}
      {isCompleted && (
        <div className="flex items-center gap-2 p-2.5 rounded-md bg-green-500/10 border border-green-500/20 text-green-500 text-sm">
          <CheckCircle2 className="w-4 h-4 shrink-0" />
          <span>
            Rollout concluído com sucesso em{" "}
            <span className="font-mono font-semibold">{formatDuration(elapsedTime)}</span>
          </span>
        </div>
      )}
    </div>
  );
};
