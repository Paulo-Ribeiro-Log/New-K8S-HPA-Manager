// AutoDiscoverDialog - Modal de auto-descoberta de clusters AKS + EKS com SSE progress
import { useState, useEffect, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Loader2,
  CheckCircle2,
  AlertCircle,
  Search,
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
  provider?: string;   // "aks" | "eks" | undefined
}

interface LogEntry {
  message: string;
  provider?: string;   // "aks" | "eks" | undefined
  isError: boolean;
  isSuccess: boolean;
}

interface ProviderStats {
  success: number;
  errors: number;
  done: boolean;
}

interface AutoDiscoverDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete?: () => void;
}

export function AutoDiscoverDialog({
  open,
  onOpenChange,
  onComplete,
}: AutoDiscoverDialogProps) {
  const [isRunning, setIsRunning] = useState(false);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [hasCompleted, setHasCompleted] = useState(false);
  const [hasError, setHasError] = useState(false);
  const [aksStats, setAksStats] = useState<ProviderStats>({ success: 0, errors: 0, done: false });
  const [eksStats, setEksStats] = useState<ProviderStats>({ success: 0, errors: 0, done: false });
  const logsEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      setLogs([]);
      setHasCompleted(false);
      setHasError(false);
      setAksStats({ success: 0, errors: 0, done: false });
      setEksStats({ success: 0, errors: 0, done: false });
    }
  }, [open]);

  // Auto-scroll ao final dos logs
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs]);

  const startAutoDiscover = async () => {
    setIsRunning(true);
    setLogs([]);
    setHasCompleted(false);
    setHasError(false);
    setAksStats({ success: 0, errors: 0, done: false });
    setEksStats({ success: 0, errors: 0, done: false });

    try {
      const response = await fetch("/api/v1/clusters/autodiscover", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });

      if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

      const reader = response.body?.getReader();
      const decoder = new TextDecoder();
      if (!reader) throw new Error("No response body");

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value);
        for (const line of chunk.split("\n")) {
          if (!line.startsWith("data: ")) continue;
          try {
            const data = JSON.parse(line.substring(6)) as AutoDiscoverProgress;

            const entry: LogEntry = {
              message: data.message,
              provider: data.provider,
              isError: data.phase === "error" || data.message.includes("❌"),
              isSuccess: data.message.includes("✅"),
            };
            setLogs((prev) => [...prev, entry]);

            // Atualizar contadores por provider nos eventos de saving/completed
            if (data.provider === "aks" && data.success > 0) {
              setAksStats((prev) => ({ ...prev, success: data.success, errors: data.errors }));
            }
            if (data.provider === "eks" && data.success > 0) {
              setEksStats((prev) => ({ ...prev, success: data.success, errors: data.errors }));
            }

            if (data.phase === "completed") {
              setHasCompleted(true);
              // Parsear contagens finais da mensagem: "AKS: X ok / Y erro(s) | EKS: X ok / Y erro(s)"
              const aksMatch = data.message.match(/AKS:\s*(\d+)\s*ok\s*\/\s*(\d+)/);
              const eksMatch = data.message.match(/EKS:\s*(\d+)\s*ok\s*\/\s*(\d+)/);
              if (aksMatch) setAksStats({ success: +aksMatch[1], errors: +aksMatch[2], done: true });
              if (eksMatch) setEksStats({ success: +eksMatch[1], errors: +eksMatch[2], done: true });
              if (onComplete) onComplete();
            }
            if (data.phase === "error") setHasError(true);
          } catch {
            // linha malformada — ignorar
          }
        }
      }
    } catch (error) {
      setLogs((prev) => [
        ...prev,
        {
          message: `❌ Erro na auto-descoberta: ${error instanceof Error ? error.message : String(error)}`,
          isError: true,
          isSuccess: false,
        },
      ]);
      setHasError(true);
    } finally {
      setIsRunning(false);
    }
  };

  const getPhaseIcon = () => {
    if (hasCompleted && !hasError) return <CheckCircle2 className="h-6 w-6 text-green-500" />;
    if (hasError) return <AlertCircle className="h-6 w-6 text-yellow-500" />;
    if (isRunning) return <Loader2 className="h-6 w-6 text-blue-500 animate-spin" />;
    return <Search className="h-6 w-6 text-blue-500" />;
  };

  const ProviderBadge = ({ provider }: { provider?: string }) => {
    if (!provider) return null;
    if (provider === "aks")
      return (
        <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300 mr-1.5 shrink-0">
          AKS
        </span>
      );
    if (provider === "eks")
      return (
        <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300 mr-1.5 shrink-0">
          EKS
        </span>
      );
    return null;
  };

  const StatCard = ({
    provider,
    stats,
    label,
  }: {
    provider: "aks" | "eks";
    stats: ProviderStats;
    label: string;
  }) => {
    const isAks = provider === "aks";
    const color = isAks
      ? "border-blue-200 dark:border-blue-800"
      : "border-orange-200 dark:border-orange-800";
    const badgeBg = isAks
      ? "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300"
      : "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300";

    return (
      <div className={`flex-1 p-3 rounded-lg border ${color} bg-card`}>
        <div className="flex items-center gap-2 mb-2">
          <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${badgeBg}`}>
            {label}
          </span>
          {stats.done && stats.errors === 0 && (
            <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
          )}
          {isRunning && !stats.done && (
            <Loader2 className="h-3.5 w-3.5 text-muted-foreground animate-spin" />
          )}
        </div>
        <div className="flex gap-3 text-xs">
          <span className="text-green-600 dark:text-green-400">
            ✓ {stats.success} configurado{stats.success !== 1 ? "s" : ""}
          </span>
          {stats.errors > 0 && (
            <span className="text-red-500">✗ {stats.errors} erro{stats.errors !== 1 ? "s" : ""}</span>
          )}
          {stats.success === 0 && stats.errors === 0 && !stats.done && (
            <span className="text-muted-foreground">aguardando...</span>
          )}
        </div>
      </div>
    );
  };

  const totalSuccess = aksStats.success + eksStats.success;
  const totalErrors = aksStats.errors + eksStats.errors;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {getPhaseIcon()}
            Auto-Descoberta de Clusters
          </DialogTitle>
          <DialogDescription>
            Descobre configurações AKS (Azure) e EKS (AWS) em paralelo a partir do kubeconfig local
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 min-h-0 flex-1">
          {/* Stats por provider — aparece assim que a descoberta inicia */}
          {(isRunning || hasCompleted) && (
            <div className="flex gap-3">
              <StatCard provider="aks" stats={aksStats} label="AKS" />
              <StatCard provider="eks" stats={eksStats} label="EKS" />
              {hasCompleted && (
                <div className="flex-none flex flex-col justify-center items-center px-3 rounded-lg border bg-card text-center min-w-[80px]">
                  <span className="text-lg font-bold text-foreground">{totalSuccess}</span>
                  <span className="text-[10px] text-muted-foreground">total</span>
                  {totalErrors > 0 && (
                    <span className="text-[10px] text-red-500 mt-0.5">{totalErrors} erro{totalErrors !== 1 ? "s" : ""}</span>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Log de progresso */}
          {logs.length > 0 && (
            <div className="flex flex-col gap-1.5 min-h-0 flex-1">
              <h4 className="text-sm font-medium shrink-0">Progresso:</h4>
              <ScrollArea className="flex-1 rounded-md border bg-muted/10 p-3" style={{ maxHeight: "280px" }}>
                <div className="space-y-0.5 font-mono text-xs">
                  {logs.map((log, idx) => (
                    <div
                      key={idx}
                      className={`flex items-start gap-0 leading-5 ${
                        log.isError
                          ? "text-red-500 dark:text-red-400"
                          : log.isSuccess
                          ? "text-green-600 dark:text-green-400"
                          : "text-foreground/80"
                      }`}
                    >
                      <ProviderBadge provider={log.provider} />
                      <span className="break-all">{stripProviderPrefix(log.message)}</span>
                    </div>
                  ))}
                  <div ref={logsEndRef} />
                </div>
              </ScrollArea>
            </div>
          )}

          {/* Alerta de conclusão */}
          {hasCompleted && !hasError && (
            <Alert className="border-green-500 bg-green-50 dark:bg-green-950/20 shrink-0">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertDescription className="text-green-700 dark:text-green-400">
                Auto-descoberta concluída!{" "}
                <strong>{totalSuccess} cluster{totalSuccess !== 1 ? "s" : ""}</strong> configurado{totalSuccess !== 1 ? "s" : ""}.
                Feche e recarregue a página para usar os novos clusters.
              </AlertDescription>
            </Alert>
          )}

          {hasCompleted && hasError && (
            <Alert className="border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20 shrink-0">
              <AlertCircle className="h-4 w-4 text-yellow-600" />
              <AlertDescription className="text-yellow-700 dark:text-yellow-400">
                Concluído com erros. {totalSuccess > 0 && <><strong>{totalSuccess}</strong> cluster{totalSuccess !== 1 ? "s" : ""} configurado{totalSuccess !== 1 ? "s" : ""} com sucesso. </>}
                Verifique os logs acima para detalhes.
              </AlertDescription>
            </Alert>
          )}

          {/* Ações */}
          <div className="flex justify-end gap-2 pt-1 shrink-0">
            {!isRunning && !hasCompleted && (
              <Button onClick={startAutoDiscover} className="gap-2">
                <Search className="h-4 w-4" />
                Iniciar Auto-Descoberta
              </Button>
            )}

            {isRunning && (
              <Button disabled className="gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Descobrindo clusters...
              </Button>
            )}

            {(hasCompleted || hasError) && !isRunning && (
              <>
                <Button
                  variant="outline"
                  onClick={() => {
                    setLogs([]);
                    setHasCompleted(false);
                    setHasError(false);
                    setAksStats({ success: 0, errors: 0, done: false });
                    setEksStats({ success: 0, errors: 0, done: false });
                  }}
                  className="gap-2"
                >
                  <Search className="h-4 w-4" />
                  Executar Novamente
                </Button>
                <Button
                  onClick={() => {
                    onOpenChange(false);
                    if (hasCompleted && onComplete) window.location.reload();
                  }}
                >
                  {hasCompleted ? "Fechar e Recarregar" : "Fechar"}
                </Button>
              </>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// Remove prefixos "[AKS] " e "[EKS] " da mensagem — o badge visual já indica o provider
function stripProviderPrefix(msg: string): string {
  return msg.replace(/^\[(AKS|EKS)\]\s*/i, "");
}

// Ícone de fase mantido para uso externo (VPNWarningBanner, etc.)
export function AutoDiscoverPhaseIcon({ phase, className }: { phase: string; className?: string }) {
  switch (phase) {
    case "discovering": return <Search className={className} />;
    case "processing":  return <Loader2 className={`${className} animate-spin`} />;
    case "saving":      return <Save className={className} />;
    case "completed":   return <CheckCircle2 className={className} />;
    case "error":       return <XCircle className={className} />;
    default:            return <Loader2 className={`${className} animate-spin`} />;
  }
}
