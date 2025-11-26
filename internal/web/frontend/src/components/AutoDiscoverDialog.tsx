// AutoDiscoverDialog - Modal de auto-descoberta de clusters com SSE progress
import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Loader2,
  CheckCircle2,
  AlertCircle,
  Search,
  Database,
  Save,
  XCircle,
} from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";

interface AutoDiscoverProgress {
  phase: string;       // discovering, processing, saving, completed, error
  message: string;
  current: number;
  total: number;
  clusterName?: string;
  success: number;
  errors: number;
}

interface AutoDiscoverDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete?: () => void;  // Callback quando descoberta completa
}

export function AutoDiscoverDialog({
  open,
  onOpenChange,
  onComplete,
}: AutoDiscoverDialogProps) {
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState<AutoDiscoverProgress | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [hasCompleted, setHasCompleted] = useState(false);
  const [hasError, setHasError] = useState(false);

  // Resetar estado ao abrir o modal
  useEffect(() => {
    if (open) {
      setProgress(null);
      setLogs([]);
      setHasCompleted(false);
      setHasError(false);
    }
  }, [open]);

  const startAutoDiscover = async () => {
    setIsRunning(true);
    setProgress(null);
    setLogs([]);
    setHasCompleted(false);
    setHasError(false);

    try {
      const response = await fetch("/api/v1/clusters/autodiscover", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const reader = response.body?.getReader();
      const decoder = new TextDecoder();

      if (!reader) {
        throw new Error("No response body");
      }

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value);
        const lines = chunk.split("\n");

        for (const line of lines) {
          if (line.startsWith("data: ")) {
            try {
              const data = JSON.parse(line.substring(6)) as AutoDiscoverProgress;
              setProgress(data);
              setLogs((prev) => [...prev, data.message]);

              // Detectar conclusão
              if (data.phase === "completed") {
                setHasCompleted(true);
                if (onComplete) {
                  onComplete();
                }
              }

              // Detectar erro
              if (data.phase === "error") {
                setHasError(true);
              }
            } catch (err) {
              console.error("Failed to parse SSE data:", err);
            }
          }
        }
      }
    } catch (error) {
      console.error("Auto-discover failed:", error);
      setLogs((prev) => [
        ...prev,
        `❌ Erro na auto-descoberta: ${error instanceof Error ? error.message : String(error)}`,
      ]);
      setHasError(true);
    } finally {
      setIsRunning(false);
    }
  };

  const getPhaseIcon = () => {
    if (hasCompleted && !hasError) {
      return <CheckCircle2 className="h-6 w-6 text-green-500" />;
    }
    if (hasError) {
      return <AlertCircle className="h-6 w-6 text-red-500" />;
    }
    if (!progress) {
      return <Search className="h-6 w-6 text-blue-500" />;
    }

    switch (progress.phase) {
      case "discovering":
        return <Search className="h-6 w-6 text-blue-500 animate-pulse" />;
      case "processing":
        return <Loader2 className="h-6 w-6 text-blue-500 animate-spin" />;
      case "saving":
        return <Save className="h-6 w-6 text-green-500 animate-pulse" />;
      case "completed":
        return <CheckCircle2 className="h-6 w-6 text-green-500" />;
      case "error":
        return <XCircle className="h-6 w-6 text-red-500" />;
      default:
        return <Loader2 className="h-6 w-6 text-blue-500 animate-spin" />;
    }
  };

  const getProgressPercentage = () => {
    if (!progress || progress.total === 0) return 0;
    return Math.round((progress.current / progress.total) * 100);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {getPhaseIcon()}
            Auto-Descoberta de Clusters
          </DialogTitle>
          <DialogDescription>
            Descobrindo automaticamente configurações de todos os clusters AKS do kubeconfig
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Status Card */}
          {progress && (
            <div className="p-4 rounded-lg border bg-card">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm font-medium">
                    {progress.current} / {progress.total} clusters processados
                  </span>
                </div>
                <div className="flex gap-4 text-sm">
                  <span className="text-green-600">✓ {progress.success} sucesso</span>
                  {progress.errors > 0 && (
                    <span className="text-red-600">✗ {progress.errors} erros</span>
                  )}
                </div>
              </div>

              {/* Progress Bar */}
              <Progress value={getProgressPercentage()} className="h-2" />
              <p className="text-xs text-muted-foreground mt-2">
                {getProgressPercentage()}% completo
              </p>
            </div>
          )}

          {/* Logs */}
          {logs.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">Progresso:</h4>
              <ScrollArea className="h-64 rounded-md border bg-muted/10 p-4">
                <div className="space-y-1 font-mono text-xs">
                  {logs.map((log, idx) => (
                    <div
                      key={idx}
                      className={`${
                        log.includes("❌") || log.includes("⚠️")
                          ? "text-red-500"
                          : log.includes("✅")
                          ? "text-green-500"
                          : "text-foreground"
                      }`}
                    >
                      {log}
                    </div>
                  ))}
                </div>
              </ScrollArea>
            </div>
          )}

          {/* Completion Alert */}
          {hasCompleted && !hasError && (
            <Alert className="border-green-500 bg-green-50 dark:bg-green-950/20">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertDescription className="text-green-700 dark:text-green-400">
                Auto-descoberta concluída com sucesso! Todos os clusters foram configurados.
                Você pode fechar este dialog e recarregar a página.
              </AlertDescription>
            </Alert>
          )}

          {/* Error Alert */}
          {hasError && (
            <Alert className="border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20">
              <AlertCircle className="h-4 w-4 text-yellow-600" />
              <AlertDescription className="text-yellow-700 dark:text-yellow-400">
                Auto-descoberta concluída com alguns erros. Verifique os logs acima para
                mais detalhes.
              </AlertDescription>
            </Alert>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4">
            {!isRunning && !hasCompleted && (
              <Button onClick={startAutoDiscover} className="gap-2">
                <Search className="h-4 w-4" />
                Iniciar Auto-Descoberta
              </Button>
            )}

            {(hasCompleted || hasError) && (
              <Button
                variant="outline"
                onClick={() => {
                  onOpenChange(false);
                  if (hasCompleted && onComplete) {
                    // Recarregar página para pegar novos clusters
                    window.location.reload();
                  }
                }}
              >
                Fechar
              </Button>
            )}

            {isRunning && (
              <Button disabled className="gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Processando...
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
