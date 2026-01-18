import { X, Clock, CheckCircle2, Loader2, AlertCircle, Circle } from "lucide-react";
import { cn } from "@/lib/utils";
import { useEffect, useState, useRef } from "react";
import { Progress } from "@/components/ui/progress";

export interface PodStatus {
  name: string;
  phase: "Running" | "Pending" | "Terminating" | "ContainerCreating" | "CrashLoopBackOff" | "Error" | "Unknown";
  ready: boolean;
  isNew: boolean; // true = pod novo (atualizado), false = pod antigo
  age?: string;
  restarts?: number;
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
  startTime?: number; // timestamp em ms do início do rollout
  pods?: PodStatus[]; // status individual dos pods
  onClose?: () => void;
}

// Formata duração em mm:ss ou hh:mm:ss
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

// Ícone e cor baseado no status do pod
const getPodStatusInfo = (pod: PodStatus) => {
  if (pod.phase === "Running" && pod.ready) {
    return {
      icon: CheckCircle2,
      color: "text-green-500",
      bgColor: "bg-green-500/10",
      label: "Pronto",
    };
  }
  if (pod.phase === "Running" && !pod.ready) {
    return {
      icon: Loader2,
      color: "text-yellow-500",
      bgColor: "bg-yellow-500/10",
      label: "Iniciando",
      animate: true,
    };
  }
  if (pod.phase === "Pending" || pod.phase === "ContainerCreating") {
    return {
      icon: Loader2,
      color: "text-blue-500",
      bgColor: "bg-blue-500/10",
      label: "Criando",
      animate: true,
    };
  }
  if (pod.phase === "Terminating") {
    return {
      icon: Circle,
      color: "text-orange-500",
      bgColor: "bg-orange-500/10",
      label: "Terminando",
    };
  }
  if (pod.phase === "CrashLoopBackOff" || pod.phase === "Error") {
    return {
      icon: AlertCircle,
      color: "text-red-500",
      bgColor: "bg-red-500/10",
      label: "Erro",
    };
  }
  return {
    icon: Circle,
    color: "text-muted-foreground",
    bgColor: "bg-muted/50",
    label: "Desconhecido",
  };
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

  // Timer state - atualiza a cada segundo
  const [elapsedTime, setElapsedTime] = useState(0);
  const [finalTime, setFinalTime] = useState<number | null>(null);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  useEffect(() => {
    if (!startTime) return;

    // Se já completou, não reiniciar o timer
    if (isCompleted && finalTime !== null) {
      setElapsedTime(finalTime);
      return;
    }

    // Calcula tempo inicial
    const initialElapsed = Date.now() - startTime;
    setElapsedTime(initialElapsed);

    // Limpar timer anterior se existir
    if (timerRef.current) {
      clearInterval(timerRef.current);
    }

    // Atualiza a cada segundo enquanto não completou
    if (!isCompleted) {
      timerRef.current = setInterval(() => {
        setElapsedTime(Date.now() - startTime);
      }, 1000);
    }

    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [startTime, isCompleted, finalTime]);

  // Salvar o tempo final quando completar
  useEffect(() => {
    if (isCompleted && startTime && finalTime === null) {
      const final = Date.now() - startTime;
      setFinalTime(final);
      setElapsedTime(final);
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
    }
  }, [isCompleted, startTime, finalTime]);

  // Separa pods novos e antigos
  const newPods = pods.filter((p) => p.isNew);
  const oldPodsList = pods.filter((p) => !p.isNew);

  return (
    <div className="rounded-lg border border-border/60 bg-muted/30 p-4 space-y-4">
      {/* Header com nome, timer e botão fechar - todos juntos */}
      <div className="flex items-center gap-3">
        <div
          className={cn(
            "flex items-center justify-center w-10 h-10 rounded-full shrink-0",
            isCompleted ? "bg-green-500/20" : "bg-primary/20"
          )}
        >
          {isCompleted ? (
            <CheckCircle2 className="w-5 h-5 text-green-500" />
          ) : (
            <Loader2 className="w-5 h-5 text-primary animate-spin" />
          )}
        </div>
        <div className="min-w-0">
          <p className="text-sm font-semibold text-foreground truncate">
            Rollout Restart: {deploymentName}
          </p>
          <p className="text-xs text-muted-foreground truncate">{message}</p>
        </div>

        {/* Timer/Cronômetro - logo após o título */}
        {startTime && (
          <div
            className={cn(
              "flex items-center gap-2 px-3 py-1.5 rounded-md font-mono text-sm shrink-0",
              isCompleted
                ? "bg-green-500/10 text-green-500"
                : "bg-primary/10 text-primary"
            )}
          >
            <Clock className="w-4 h-4" />
            <span className="tabular-nums font-semibold">
              {formatDuration(elapsedTime)}
            </span>
          </div>
        )}

        {/* Porcentagem inline */}
        <div
          className={cn(
            "px-2 py-1 rounded-md text-sm font-mono font-semibold shrink-0",
            isCompleted ? "bg-green-500/10 text-green-500" : "bg-primary/10 text-primary"
          )}
        >
          {normalizedProgress.toFixed(0)}%
        </div>

        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-border/50 p-2 text-muted-foreground transition hover:bg-muted hover:text-foreground shrink-0"
            aria-label="Fechar gauge de rollout"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Barra de progresso principal */}
      <div className="space-y-1">
        <Progress
          value={normalizedProgress}
          className={cn("h-2", isCompleted && "[&>div]:bg-green-500")}
        />
      </div>

      {/* Métricas em grid */}
      <div className="grid grid-cols-4 gap-3 text-center">
        <div className="rounded-md bg-background/50 p-2">
          <p className="text-lg font-mono font-bold text-primary">
            {updated}/{desired}
          </p>
          <p className="text-[10px] uppercase text-muted-foreground">
            Atualizados
          </p>
        </div>
        <div className="rounded-md bg-background/50 p-2">
          <p className="text-lg font-mono font-bold text-green-500">
            {newReady}/{desired}
          </p>
          <p className="text-[10px] uppercase text-muted-foreground">
            Prontos
          </p>
        </div>
        <div className="rounded-md bg-background/50 p-2">
          <p
            className={cn(
              "text-lg font-mono font-bold",
              oldPods > 0 ? "text-orange-500" : "text-muted-foreground"
            )}
          >
            {oldPods}
          </p>
          <p className="text-[10px] uppercase text-muted-foreground">
            Antigos
          </p>
        </div>
        <div className="rounded-md bg-background/50 p-2">
          <p
            className={cn(
              "text-lg font-mono font-bold",
              unavailable > 0 ? "text-red-500" : "text-muted-foreground"
            )}
          >
            {unavailable}
          </p>
          <p className="text-[10px] uppercase text-muted-foreground">
            Indispon.
          </p>
        </div>
      </div>

      {/* Status individual dos pods */}
      {pods.length > 0 && (
        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground uppercase">
            Status dos Pods
          </p>

          {/* Pods novos */}
          {newPods.length > 0 && (
            <div className="space-y-1">
              <p className="text-[10px] text-muted-foreground">
                Novos ({newPods.length})
              </p>
              <div className="flex flex-wrap gap-1">
                {newPods.map((pod) => {
                  const statusInfo = getPodStatusInfo(pod);
                  const Icon = statusInfo.icon;
                  return (
                    <div
                      key={pod.name}
                      className={cn(
                        "flex items-center gap-1.5 px-2 py-1 rounded-md text-xs",
                        statusInfo.bgColor
                      )}
                      title={`${pod.name} - ${statusInfo.label}${pod.restarts ? ` (${pod.restarts} restarts)` : ""}`}
                    >
                      <Icon
                        className={cn(
                          "w-3.5 h-3.5",
                          statusInfo.color,
                          statusInfo.animate && "animate-spin"
                        )}
                      />
                      <span
                        className={cn(
                          "font-mono truncate max-w-[120px]",
                          statusInfo.color
                        )}
                      >
                        {pod.name.split("-").slice(-2).join("-")}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Pods antigos (terminando) */}
          {oldPodsList.length > 0 && (
            <div className="space-y-1">
              <p className="text-[10px] text-muted-foreground">
                Terminando ({oldPodsList.length})
              </p>
              <div className="flex flex-wrap gap-1">
                {oldPodsList.map((pod) => (
                  <div
                    key={pod.name}
                    className="flex items-center gap-1.5 px-2 py-1 rounded-md text-xs bg-orange-500/10"
                    title={`${pod.name} - Terminando`}
                  >
                    <Circle className="w-3.5 h-3.5 text-orange-500" />
                    <span className="font-mono truncate max-w-[120px] text-orange-500">
                      {pod.name.split("-").slice(-2).join("-")}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Mensagem de conclusão */}
      {isCompleted && (
        <div className="flex items-center gap-2 p-2 rounded-md bg-green-500/10 text-green-500 text-sm">
          <CheckCircle2 className="w-4 h-4" />
          <span>
            Rollout concluído em {formatDuration(elapsedTime)}
          </span>
        </div>
      )}
    </div>
  );
};
