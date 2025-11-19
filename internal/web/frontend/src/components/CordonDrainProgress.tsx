import { useSSE, type ProgressEvent } from '@/hooks/useSSE';
import { Progress } from '@/components/ui/progress';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { CheckCircle2, XCircle, Loader2, Server, HardDrive, Cloud } from 'lucide-react';

interface CordonDrainProgressProps {
  operationId: string;
  onComplete?: () => void;
  onError?: (error: string) => void;
}

/**
 * Componente de progress bar para operações Cordon/Drain
 *
 * Exibe progresso em tempo real via SSE com feedback visual detalhado:
 * - CORDON: 0-20%
 * - DRAIN: 20-80%
 * - AZURE: 80-95%
 * - COMPLETE: 100%
 */
export function CordonDrainProgress({ operationId, onComplete, onError }: CordonDrainProgressProps) {
  const { lastEvent, getProgress, getCurrentPhase, isComplete, hasError, isInProgress } = useSSE({
    operationId,
    enabled: true,
    onEvent: (event: ProgressEvent) => {
      console.log('[CordonDrainProgress] Evento recebido:', event);
    },
    onComplete: () => {
      console.log('[CordonDrainProgress] Operação concluída!');
      if (onComplete) {
        onComplete();
      }
    },
    onError: (error) => {
      console.error('[CordonDrainProgress] Erro SSE:', error);
      if (onError) {
        onError('Erro na conexão SSE');
      }
    },
  });

  const progress = getProgress();
  const currentPhase = getCurrentPhase();

  // Determinar ícone da fase atual
  const getPhaseIcon = () => {
    if (hasError()) {
      return <XCircle className="h-5 w-5 text-red-500" />;
    }
    if (isComplete()) {
      return <CheckCircle2 className="h-5 w-5 text-green-500" />;
    }

    switch (lastEvent?.type) {
      case 'cordon':
        return <Server className="h-5 w-5 text-blue-500 animate-pulse" />;
      case 'drain':
        return <HardDrive className="h-5 w-5 text-orange-500 animate-pulse" />;
      case 'azure':
        return <Cloud className="h-5 w-5 text-purple-500 animate-pulse" />;
      default:
        return <Loader2 className="h-5 w-5 animate-spin text-gray-500" />;
    }
  };

  // Determinar cor da progress bar
  const getProgressColor = () => {
    if (hasError()) return 'bg-red-500';
    if (isComplete()) return 'bg-green-500';
    if (progress >= 80) return 'bg-purple-500';
    if (progress >= 20) return 'bg-orange-500';
    return 'bg-blue-500';
  };

  return (
    <div className="space-y-4">
      {/* Header com fase atual */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {getPhaseIcon()}
          <span className="font-medium">{currentPhase}</span>
        </div>
        <span className="text-sm text-muted-foreground">{progress}%</span>
      </div>

      {/* Progress bar */}
      <Progress value={progress} className="h-2">
        <div
          className={`h-full transition-all ${getProgressColor()}`}
          style={{ width: `${progress}%` }}
        />
      </Progress>

      {/* Mensagem de status */}
      {lastEvent && (
        <div className="text-sm text-muted-foreground">
          {lastEvent.message}
          {lastEvent.details && (
            <div className="text-xs text-muted-foreground/70 mt-1">{lastEvent.details}</div>
          )}
        </div>
      )}

      {/* Detalhes de node e pods (se disponível) */}
      {lastEvent?.node_name && (
        <div className="text-xs text-muted-foreground/70">
          Node: <span className="font-mono">{lastEvent.node_name}</span>
          {lastEvent.pods_count !== undefined && ` | Pods: ${lastEvent.pods_count}`}
        </div>
      )}

      {/* Alert de erro */}
      {hasError() && lastEvent?.error && (
        <Alert variant="destructive">
          <XCircle className="h-4 w-4" />
          <AlertDescription>{lastEvent.error}</AlertDescription>
        </Alert>
      )}

      {/* Alert de sucesso */}
      {isComplete() && (
        <Alert>
          <CheckCircle2 className="h-4 w-4 text-green-500" />
          <AlertDescription className="text-green-600">
            Operação concluída com sucesso!
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
