// AutoDiscoverDialog - Modal de auto-descoberta de clusters AKS + EKS + GKE com SSE progress
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
import { Input } from "@/components/ui/input";
import {
  Loader2,
  CheckCircle2,
  AlertCircle,
  Search,
  Save,
  XCircle,
  KeyRound,
  Copy,
} from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "sonner";

interface AutoDiscoverProgress {
  phase: string;       // discovering, processing, saving, completed, error
  message: string;
  current: number;
  total: number;
  clusterName?: string;
  success: number;
  errors: number;
  provider?: string;   // "aks" | "eks" | "gke"
}

interface LogEntry {
  message: string;
  provider?: string;
  isError: boolean;
  isSuccess: boolean;
}

interface ProviderStats {
  success: number;
  errors: number;
  done: boolean;
}

interface GCPAuthStatus {
  authenticated: boolean;
  account?: string;
  has_gcloud: boolean;
  has_adc: boolean;
}

interface GCPLoginSession {
  session_id: string;
  user_code: string;
  verify_url: string;
  expires_at: string;
  interval_sec: number;
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
  const [gkeStats, setGkeStats] = useState<ProviderStats>({ success: 0, errors: 0, done: false });
  const logsEndRef = useRef<HTMLDivElement>(null);

  // GCP auth state
  const [gcpStatus, setGcpStatus] = useState<GCPAuthStatus | null>(null);
  const [gcpLoginSession, setGcpLoginSession] = useState<GCPLoginSession | null>(null);
  const [gcpPolling, setGcpPolling] = useState(false);
  const gcpPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (open) {
      setLogs([]);
      setHasCompleted(false);
      setHasError(false);
      setAksStats({ success: 0, errors: 0, done: false });
      setEksStats({ success: 0, errors: 0, done: false });
      setGkeStats({ success: 0, errors: 0, done: false });
      setGcpLoginSession(null);
      setGcpPolling(false);
      checkGCPAuth();
    } else {
      stopGCPPoll();
    }
  }, [open]);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs]);

  // Cleanup GCP poll on unmount
  useEffect(() => () => stopGCPPoll(), []);

  const stopGCPPoll = () => {
    if (gcpPollRef.current) {
      clearInterval(gcpPollRef.current);
      gcpPollRef.current = null;
    }
  };

  const checkGCPAuth = async () => {
    try {
      const resp = await fetch("/api/v1/gcp/auth/status", {
        headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
      });
      if (resp.ok) {
        const data: GCPAuthStatus = await resp.json();
        setGcpStatus(data);
      }
    } catch {
      // Ignorar — GCP auth é opcional
    }
  };

  const startGCPLogin = async () => {
    try {
      const resp = await fetch("/api/v1/gcp/auth/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data: GCPLoginSession = await resp.json();
      setGcpLoginSession(data);
      setGcpPolling(true);

      // Iniciar polling
      const interval = Math.max((data.interval_sec || 5), 5) * 1000;
      gcpPollRef.current = setInterval(() => pollGCPLogin(data.session_id), interval);
    } catch (err) {
      toast.error("Erro ao iniciar autenticação GCP", {
        description: err instanceof Error ? err.message : String(err),
      });
    }
  };

  const pollGCPLogin = async (sessionID: string) => {
    try {
      const resp = await fetch(`/api/v1/gcp/auth/poll?session_id=${sessionID}`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
      });
      if (!resp.ok) return;
      const data = await resp.json();
      if (data.done) {
        stopGCPPoll();
        setGcpPolling(false);
        setGcpLoginSession(null);
        if (data.success) {
          toast.success("GCP autenticado com sucesso!");
          await checkGCPAuth();
        } else {
          toast.error("Autenticação GCP falhou ou expirou");
        }
      }
    } catch {
      // ignorar erros de polling
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => toast.success("Copiado!"));
  };

  const startAutoDiscover = async () => {
    setIsRunning(true);
    setLogs([]);
    setHasCompleted(false);
    setHasError(false);
    setAksStats({ success: 0, errors: 0, done: false });
    setEksStats({ success: 0, errors: 0, done: false });
    setGkeStats({ success: 0, errors: 0, done: false });

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

            if (data.provider === "aks" && data.success > 0) {
              setAksStats((prev) => ({ ...prev, success: data.success, errors: data.errors }));
            }
            if (data.provider === "eks" && data.success > 0) {
              setEksStats((prev) => ({ ...prev, success: data.success, errors: data.errors }));
            }
            if (data.provider === "gke" && data.success > 0) {
              setGkeStats((prev) => ({ ...prev, success: data.success, errors: data.errors }));
            }

            if (data.phase === "completed") {
              setHasCompleted(true);
              const aksMatch = data.message.match(/AKS:\s*(\d+)/);
              const eksMatch = data.message.match(/EKS:\s*(\d+)/);
              const gkeMatch = data.message.match(/GKE:\s*(\d+)/);
              if (aksMatch) setAksStats((p) => ({ ...p, success: +aksMatch[1], done: true }));
              if (eksMatch) setEksStats((p) => ({ ...p, success: +eksMatch[1], done: true }));
              if (gkeMatch) setGkeStats((p) => ({ ...p, success: +gkeMatch[1], done: true }));
              // Atualizar status GCP após discovery
              checkGCPAuth();
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
    const variants: Record<string, string> = {
      aks: "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300",
      eks: "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300",
      gke: "bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300",
    };
    const cls = variants[provider];
    if (!cls) return null;
    return (
      <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold ${cls} mr-1.5 shrink-0`}>
        {provider.toUpperCase()}
      </span>
    );
  };

  const StatCard = ({
    provider,
    stats,
    label,
  }: {
    provider: "aks" | "eks" | "gke";
    stats: ProviderStats;
    label: string;
  }) => {
    const colorMap: Record<string, { border: string; badge: string }> = {
      aks: {
        border: "border-blue-200 dark:border-blue-800",
        badge: "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300",
      },
      eks: {
        border: "border-orange-200 dark:border-orange-800",
        badge: "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300",
      },
      gke: {
        border: "border-green-200 dark:border-green-800",
        badge: "bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300",
      },
    };
    const { border, badge } = colorMap[provider];

    return (
      <div className={`flex-1 p-3 rounded-lg border ${border} bg-card`}>
        <div className="flex items-center gap-2 mb-2">
          <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${badge}`}>{label}</span>
          {stats.done && stats.errors === 0 && <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />}
          {isRunning && !stats.done && <Loader2 className="h-3.5 w-3.5 text-muted-foreground animate-spin" />}
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

  const totalSuccess = aksStats.success + eksStats.success + gkeStats.success;
  const totalErrors = aksStats.errors + eksStats.errors + gkeStats.errors;

  const gcpNeedsAuth = gcpStatus && !gcpStatus.authenticated;
  const gcpLoginInProgress = gcpPolling && gcpLoginSession;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {getPhaseIcon()}
            Auto-Descoberta de Clusters
          </DialogTitle>
          <DialogDescription>
            Descobre configurações AKS (Azure), EKS (AWS) e GKE (GCP) em paralelo a partir do kubeconfig local
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 min-h-0 flex-1">

          {/* Aviso e fluxo de autenticação GCP */}
          {gcpNeedsAuth && !isRunning && !hasCompleted && (
            <Alert className="border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20 shrink-0">
              <KeyRound className="h-4 w-4 text-yellow-600 shrink-0" />
              <AlertDescription className="text-yellow-800 dark:text-yellow-300 w-full">
                {gcpLoginInProgress ? (
                  <div className="flex flex-col gap-2">
                    <span className="font-medium">Autenticação GCP em andamento</span>
                    <span className="text-xs">
                      Acesse{" "}
                      <a
                        href={gcpLoginSession.verify_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="underline font-medium"
                      >
                        accounts.google.com/device
                      </a>{" "}
                      e insira o código abaixo:
                    </span>
                    <div className="flex items-center gap-2">
                      <Input
                        readOnly
                        value={gcpLoginSession.user_code}
                        className="font-mono text-center text-lg tracking-widest h-10 bg-background border-yellow-400 max-w-[160px]"
                      />
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => copyToClipboard(gcpLoginSession.user_code)}
                        className="gap-1"
                      >
                        <Copy className="h-3.5 w-3.5" /> Copiar
                      </Button>
                      <Loader2 className="h-4 w-4 animate-spin text-yellow-600" />
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center justify-between gap-3 flex-wrap">
                    <span>
                      <strong>GCP não autenticado.</strong> Autentique para incluir clusters GKE na descoberta.
                    </span>
                    <Button size="sm" variant="outline" onClick={startGCPLogin} className="gap-1 shrink-0">
                      <KeyRound className="h-3.5 w-3.5" />
                      Autenticar GCP
                    </Button>
                  </div>
                )}
              </AlertDescription>
            </Alert>
          )}

          {/* GCP autenticado */}
          {gcpStatus?.authenticated && !isRunning && !hasCompleted && (
            <Alert className="border-green-500 bg-green-50 dark:bg-green-950/20 shrink-0 py-2">
              <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" />
              <AlertDescription className="text-green-800 dark:text-green-300 text-xs">
                GCP autenticado{gcpStatus.account ? ` como ${gcpStatus.account}` : ""}
                {gcpStatus.has_adc && !gcpStatus.has_gcloud ? " (via ADC)" : ""}
              </AlertDescription>
            </Alert>
          )}

          {/* Stats por provider — aparece assim que a descoberta inicia */}
          {(isRunning || hasCompleted) && (
            <div className="flex gap-3">
              <StatCard provider="aks" stats={aksStats} label="AKS" />
              <StatCard provider="eks" stats={eksStats} label="EKS" />
              <StatCard provider="gke" stats={gkeStats} label="GKE" />
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
                Concluído com erros.{totalSuccess > 0 && <> <strong>{totalSuccess}</strong> cluster{totalSuccess !== 1 ? "s" : ""} configurado{totalSuccess !== 1 ? "s" : ""} com sucesso.</>}{" "}
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
                    setGkeStats({ success: 0, errors: 0, done: false });
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

// Remove prefixos "[AKS] ", "[EKS] ", "[GKE] " da mensagem — o badge visual já indica o provider
function stripProviderPrefix(msg: string): string {
  return msg.replace(/^\[(AKS|EKS|GKE)\]\s*/i, "");
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
