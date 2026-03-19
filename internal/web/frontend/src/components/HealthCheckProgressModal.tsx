import { useEffect, useState, useRef, useMemo } from "react";
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
  FileText,
} from "lucide-react";
import { useHealthCheckProgressMultiplexed } from "@/hooks/useHealthCheckProgressMultiplexed";
import type { HealthCheckResult, HealthCheckProgress } from "@/types/healthcheck";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import { HealthCheckAlertsReport } from "@/components/HealthCheckAlertsReport";

interface HealthCheckProgressModalProps {
  sessionId: string;                           // Base session ID
  clusterSessions: Record<string, string>;     // Map: cluster -> sessionID único
  open: boolean;
  preloadedEvents?: any[];                     // Eventos pré-carregados (modo visualização)
  preloadedResult?: HealthCheckResult;         // ✅ Resultado completo pré-carregado (modo visualização)
  viewMode?: boolean;                          // Modo visualização (não conecta SSE)
  onOpenChange: (open: boolean) => void;
  onComplete: (result: HealthCheckResult) => void;
  onCancel?: () => void;
}

// Componente filho para cada cluster tab (REFATORADO - sem hooks, recebe eventos via props)
interface ClusterTabContentProps {
  cluster: string;
  sessionId: string;
  events: HealthCheckProgress[];                          // ✅ Eventos passados via props
  progress: number;                                       // ✅ Progresso passado via props (0-100)
  isComplete: boolean;                                    // ✅ Status passado via props
  hasError: boolean;                                      // ✅ Erro passado via props
  preloadedResult?: HealthCheckResult;                    // Resultado pré-carregado (modo visualização)
  viewMode?: boolean;                                     // Modo visualização
  onComplete: (result: HealthCheckResult) => void;
}

const ClusterTabContent = ({
  cluster,
  sessionId,
  events: eventsProp,        // ✅ Recebido via props (do hook multiplexado ou preloaded)
  progress,      // ✅ Recebido via props
  isComplete: isCompleteProp,
  hasError: hasErrorProp,
  preloadedResult,
  viewMode,
  onComplete,
}: ClusterTabContentProps) => {
  // Estado local
  const [result, setResult] = useState<HealthCheckResult | null>(preloadedResult || null);
  const [filter, setFilter] = useState<"all" | "healthy" | "warning" | "critical">("all");
  const [isFetchingResult, setIsFetchingResult] = useState(false);
  const [localEvents, setLocalEvents] = useState<HealthCheckProgress[]>([]);
  const [isLoadingEvents, setIsLoadingEvents] = useState(false);
  const [fetchAttempted, setFetchAttempted] = useState(false); // ✅ Flag para evitar loop infinito

  // ✅ Usar eventos locais (carregados do banco) ou eventos da prop
  const events = localEvents.length > 0 ? localEvents : eventsProp;

  // ✅ Carregar eventos do banco APENAS quando em viewMode explícito (com preloadedEvents vazios)
  useEffect(() => {
    // Só carregar se: viewMode=true, não há eventos, e ainda não tentou
    if (viewMode && eventsProp.length === 0 && localEvents.length === 0 && !isLoadingEvents && !fetchAttempted) {
      const loadEvents = async () => {
        setIsLoadingEvents(true);
        setFetchAttempted(true); // ✅ Marcar que já tentou (evita loop)
        try {
          console.log(`[${cluster}] Carregando eventos do banco para sessionId:`, sessionId);
          const response = await apiClient.getHealthCheckEvents(sessionId);

          if (response.success && response.events && response.events.length > 0) {
            const formattedEvents: HealthCheckProgress[] = response.events.map((event: any) => ({
              id: event.id,
              type: event.type,
              phase: event.phase,
              message: event.message,
              progress: event.progress,
              status: event.status,
              timestamp: event.timestamp, // Manter como string (tipo esperado por HealthCheckProgress)
            }));
            console.log(`[${cluster}] ${formattedEvents.length} eventos carregados do banco`);
            setLocalEvents(formattedEvents);
          } else {
            console.log(`[${cluster}] Nenhum evento encontrado no banco`);
          }
        } catch (error) {
          console.error(`[${cluster}] Erro ao carregar eventos:`, error);
        } finally {
          setIsLoadingEvents(false);
        }
      };
      loadEvents();
    }
  }, [viewMode, eventsProp, localEvents, isLoadingEvents, fetchAttempted, sessionId, cluster]);

  // ✅ Carregar resultado do banco APENAS quando em viewMode explícito
  useEffect(() => {
    // Só carregar se: viewMode=true, não há resultado, e ainda não tentou
    if (viewMode && !result && !isFetchingResult && !fetchAttempted) {
      const loadResult = async () => {
        setIsFetchingResult(true);
        setFetchAttempted(true); // ✅ Marcar que já tentou (evita loop)
        try {
          console.log(`[${cluster}] Carregando resultado do banco para sessionId:`, sessionId);
          const response = await apiClient.getHealthCheckResult(sessionId);

          if (response.success && response.data) {
            console.log(`[${cluster}] Resultado carregado do banco`);
            setResult(response.data);
          }
        } catch (error) {
          console.error(`[${cluster}] Erro ao carregar resultado:`, error);
        } finally {
          setIsFetchingResult(false);
        }
      };
      loadResult();
    }
  }, [viewMode, result, isFetchingResult, fetchAttempted, sessionId, cluster]);

  // ✅ Sincronizar result com preloadedResult quando mudar
  useEffect(() => {
    if (preloadedResult) {
      console.log(`[${cluster}] Usando resultado pré-carregado:`, preloadedResult);
      setResult(preloadedResult);
    }
  }, [preloadedResult, cluster]);

  // ✅ Buscar resultado final quando cluster completar
  useEffect(() => {
    if (!isCompleteProp || isFetchingResult || result || viewMode) {
      return;
    }

    console.log(`[${cluster}] ✅ Cluster completo! Buscando resultado final...`);

    const fetchResult = async () => {
      setIsFetchingResult(true);

      // ✅ Retry logic: tentar até 3 vezes com delay progressivo
      const maxRetries = 3;
      let lastError: Error | null = null;

      for (let attempt = 1; attempt <= maxRetries; attempt++) {
        try {
          console.log(`[${cluster}] Tentativa ${attempt}/${maxRetries} - Buscando resultado final para sessionId:`, sessionId);

          const response = await apiClient.getHealthCheckResult(sessionId);
          console.log(`[${cluster}] Resposta recebida (tentativa ${attempt}):`, response);

          if (response.success && response.data) {
            console.log(`[${cluster}] ✅ Resultado válido recebido!`);
            setResult(response.data);
            onComplete(response.data);
            setIsFetchingResult(false);
            return;
          } else {
            if (attempt < maxRetries) {
              const delay = attempt * 1000;
              await new Promise(resolve => setTimeout(resolve, delay));
              continue;
            } else {
              throw new Error('Resultado final não retornado pelo backend após 3 tentativas');
            }
          }
        } catch (error) {
          lastError = error instanceof Error ? error : new Error('Erro desconhecido');
          console.error(`[${cluster}] ❌ Erro na tentativa ${attempt}/${maxRetries}:`, lastError.message);

          if (attempt < maxRetries) {
            const delay = attempt * 1000;
            await new Promise(resolve => setTimeout(resolve, delay));
          }
        }
      }

      console.error(`[${cluster}] ❌ Todas as ${maxRetries} tentativas falharam`);
      toast.error(`[${cluster}] Erro ao buscar resultado final: ${lastError?.message || 'Erro desconhecido'}`);
      setIsFetchingResult(false);
    };

    fetchResult();
  }, [isCompleteProp, isFetchingResult, result, sessionId, cluster, viewMode, onComplete]);

  // Filtrar eventos baseado no filtro selecionado
  const filteredEvents = events.filter((event) => {
    if (filter === "all") return true;
    return event.status === filter;
  });

  // ✅ Calcular últimas estatísticas dos eventos
  const lastEvent = events.length > 0 ? events[events.length - 1] : null;

  const getCurrentPhase = (): string => {
    if (!lastEvent) return 'Aguardando...';

    const phaseMap: Record<string, string> = {
      init: 'Inicializando',
      deployments: 'Verificando Deployments',
      services: 'Testando Serviços Externos',
      configs: 'Validando ConfigMaps/Secrets',
      summary: 'Resumo',
      complete: 'Concluído',
      error: 'Erro',
    };

    return phaseMap[lastEvent.type] || lastEvent.type;
  };

  const getCurrentMessage = (): string => {
    return lastEvent?.message || 'Iniciando health check...';
  };

  const phase = getCurrentPhase();
  const message = getCurrentMessage();

  // Contar eventos em tempo real
  const liveHealthy = events.filter(e => e.status === "healthy").length;
  const liveWarning = events.filter(e => e.status === "warning").length;
  const liveCritical = events.filter(e => e.status === "critical").length;
  const liveTotal = events.length;

  // Status icon
  const getStatusIcon = () => {
    if (hasErrorProp) {
      return <XCircle className="h-5 w-5 text-red-600" />;
    }
    if (isCompleteProp) {
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
        {viewMode ? (
          // ✅ Modo visualização - apenas ícone de arquivo
          <FileText className="h-5 w-5 text-blue-600" />
        ) : (
          getStatusIcon()
        )}
        <span className="text-sm font-medium">
          {viewMode
            ? `Logs - ${cluster}`
            : isCompleteProp
            ? `Concluído - ${cluster}`
            : `Em Progresso - ${cluster}`}
        </span>
      </div>

      {/* Progress Bar - ocultar em viewMode */}
      {!viewMode && !isCompleteProp && !hasErrorProp && (
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="font-medium">{phase}</span>
            <span className="text-muted-foreground">{progress}%</span>
          </div>
          <Progress value={progress} className="h-2" />
        </div>
      )}

      {/* Current Message - ocultar em viewMode */}
      {!viewMode && !isCompleteProp && (
        <div className="text-sm text-muted-foreground break-words whitespace-normal">
          {message}
        </div>
      )}

      {/* Live Summary Filters */}
      {!result && liveTotal > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-1 sm:gap-2">
          <Button
            variant={filter === "healthy" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
            className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
          >
            <div className="text-sm sm:text-xl font-bold text-green-600 leading-none">
              {liveHealthy}
            </div>
            <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Healthy</div>
          </Button>
          <Button
            variant={filter === "warning" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
            className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
          >
            <div className="text-sm sm:text-xl font-bold text-yellow-600 leading-none">
              {liveWarning}
            </div>
            <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Warning</div>
          </Button>
          <Button
            variant={filter === "critical" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
            className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
          >
            <div className="text-sm sm:text-xl font-bold text-red-600 leading-none">
              {liveCritical}
            </div>
            <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Critical</div>
          </Button>
          <Button
            variant={filter === "all" ? "default" : "outline"}
            size="sm"
            onClick={() => setFilter("all")}
            className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
          >
            <div className="text-sm sm:text-xl font-bold leading-none">
              {liveTotal}
            </div>
            <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Total</div>
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
                {isLoadingEvents ? (
                  <div className="flex items-center justify-center gap-2">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Carregando eventos do banco...
                  </div>
                ) : viewMode ? (
                  "Nenhum evento encontrado no banco de dados"
                ) : (
                  "Aguardando eventos..."
                )}
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
        <div className="space-y-2">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-1 sm:gap-2">
            <Button
              variant={filter === "healthy" ? "default" : "outline"}
              size="sm"
              onClick={() => setFilter(filter === "healthy" ? "all" : "healthy")}
              className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
            >
              <div className="text-sm sm:text-xl font-bold text-green-600 leading-none">
                {result.healthy_count}
              </div>
              <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Healthy</div>
            </Button>
            <Button
              variant={filter === "warning" ? "default" : "outline"}
              size="sm"
              onClick={() => setFilter(filter === "warning" ? "all" : "warning")}
              className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
            >
              <div className="text-sm sm:text-xl font-bold text-yellow-600 leading-none">
                {result.warning_count}
              </div>
              <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Warning</div>
            </Button>
            <Button
              variant={filter === "critical" ? "default" : "outline"}
              size="sm"
              onClick={() => setFilter(filter === "critical" ? "all" : "critical")}
              className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
            >
              <div className="text-sm sm:text-xl font-bold text-red-600 leading-none">
                {result.critical_count}
              </div>
              <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Critical</div>
            </Button>
            <Button
              variant={filter === "all" ? "default" : "outline"}
              size="sm"
              onClick={() => setFilter("all")}
              className="flex flex-col h-auto py-1 sm:py-2 min-w-0 px-0.5 sm:px-3 overflow-hidden"
            >
              <div className="text-sm sm:text-xl font-bold leading-none">
                {result.total_checks}
              </div>
              <div className="text-[8px] sm:text-xs leading-none mt-1 truncate w-full">Total</div>
            </Button>
          </div>

          {/* Resumo Dynatrace (quando disponível) */}
          {((result.dynatrace_results && result.dynatrace_results.length > 0) ||
            (result.oneagent_signals && result.oneagent_signals.length > 0) ||
            (result.correlated_items && result.correlated_items.length > 0)) && (
            <div className="rounded-md border border-purple-500/30 bg-purple-500/5 px-3 py-2 space-y-1">
              <div className="text-[10px] sm:text-xs font-semibold text-purple-400 uppercase tracking-wide">Dynatrace</div>
              <div className="flex flex-wrap gap-2 text-xs">
                {result.dynatrace_results && result.dynatrace_results.length > 0 && (
                  <span className="text-orange-400">
                    ⚠ {result.dynatrace_results.length} problem{result.dynatrace_results.length !== 1 ? "s" : ""} aberto{result.dynatrace_results.length !== 1 ? "s" : ""}
                  </span>
                )}
                {result.oneagent_signals && result.oneagent_signals.length > 0 && (
                  <span className="text-blue-400">
                    ◉ {result.oneagent_signals.length} sinal{result.oneagent_signals.length !== 1 ? "is" : ""} OneAgent
                  </span>
                )}
                {result.correlated_items && result.correlated_items.length > 0 && (() => {
                  const correlated = result.correlated_items.filter(i => i.correlated).length;
                  return correlated > 0 ? (
                    <span className="text-red-400">
                      ✕ {correlated} correlacionado{correlated !== 1 ? "s" : ""} K8s↔DT
                    </span>
                  ) : null;
                })()}
              </div>
            </div>
          )}
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
  preloadedEvents,
  preloadedResult,
  viewMode,
  onOpenChange,
  onComplete,
  onCancel,
}: HealthCheckProgressModalProps) => {
  const [activeTab, setActiveTab] = useState<string>("");
  const [completedClusters, setCompletedClusters] = useState<Set<string>>(new Set());
  const [failedClusters, setFailedClusters] = useState<Set<string>>(new Set());
  const [showAlertsReport, setShowAlertsReport] = useState(false);

  // ✅ Estado persistente de eventos - NÃO é resetado quando modal fecha
  const [persistedEventsPerCluster, setPersistedEventsPerCluster] = useState<Record<string, HealthCheckProgress[]>>({});

  // ✅ Memoizar clusters para evitar loop infinito de reconexão SSE
  const clusters = useMemo(() => Object.keys(clusterSessions), [clusterSessions]);

  // ✅ Hook SSE Multiplexado - UMA conexão para TODOS os clusters
  const {
    eventsPerCluster,
    isConnected,
    disconnect,
    getProgress,
    isComplete: isClusterComplete,
    hasError: isClusterError,
  } = useHealthCheckProgressMultiplexed({
    sessionId,
    clusters,
    enabled: open && !viewMode,  // Somente se aberto e não em viewMode
    onComplete: (cluster) => {
      console.log(`[Modal] Cluster ${cluster} completou`);
      setCompletedClusters(prev => new Set(prev).add(cluster));
    },
    onEvent: (cluster, event) => {
      // Detectar erros
      if (event.type === 'error') {
        setFailedClusters(prev => new Set(prev).add(cluster));
      }
    },
  });

  // ✅ CRÍTICO: Sincronizar CONTINUAMENTE eventos do hook com estado persistente
  useEffect(() => {
    // Atualizar estado persistente sempre que eventsPerCluster mudar
    if (Object.keys(eventsPerCluster).length > 0) {
      console.log('[HealthCheckProgressModal] Sincronizando eventos para persistência:', eventsPerCluster);
      setPersistedEventsPerCluster(eventsPerCluster);
    }
  }, [eventsPerCluster]);

  // ✅ Resetar estado quando modal fechar (mas NÃO limpa eventos persistidos!)
  useEffect(() => {
    if (!open) {
      setCompletedClusters(new Set());
      setFailedClusters(new Set());
      setActiveTab("");
      // ⚠️ NÃO resetar persistedEventsPerCluster aqui!
    } else {
      // Auto-select primeiro cluster ao abrir
      if (clusters.length > 0 && !activeTab) {
        setActiveTab(clusters[0]);
      }
    }
  }, [open, clusters, activeTab]);

  // ✅ Handle complete de um cluster específico
  const handleClusterComplete = (cluster: string) => (result: HealthCheckResult) => {
    console.log(`[${cluster}] Health check completed:`, result);
    onComplete(result);
  };

  // ✅ Handle cancel - SIMPLIFICADO com SSE multiplexado
  const handleCancel = async () => {
    console.log("[Modal] Cancelando health check");

    // 1. Cancelar no backend
    try {
      await apiClient.cancelHealthCheck(sessionId);
      console.log(`[Modal] Cancelamento solicitado ao backend`);
    } catch (error) {
      console.error("[Modal] Erro ao cancelar no backend:", error);
    }

    // 2. Desconectar SSE multiplexado (uma única função!)
    disconnect();

    toast.info("Health check cancelado - análises interrompidas");
    if (onCancel) {
      onCancel();
    }
    onOpenChange(false);
  };

  // Status geral
  const allCompleted = completedClusters.size === clusters.length;
  const hasFailures = failedClusters.size > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="w-[98vw] sm:max-w-7xl max-h-[90vh] sm:max-h-[85vh] overflow-hidden"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              {viewMode ? (
                // ✅ Modo Visualização - ícone de arquivo/histórico
                <FileText className="h-5 w-5 sm:h-6 sm:w-6 text-blue-600" />
              ) : allCompleted ? (
                <CheckCircle2 className="h-5 w-5 sm:h-6 sm:w-6 text-green-600" />
              ) : hasFailures ? (
                <XCircle className="h-5 w-5 sm:h-6 sm:w-6 text-red-600" />
              ) : (
                <Loader2 className="h-5 w-5 sm:h-6 sm:w-6 text-blue-600 animate-spin" />
              )}
              <DialogTitle className="text-base sm:text-lg">
                {viewMode
                  ? "Resultados do Health Check"
                  : allCompleted
                  ? "Health Check Concluído"
                  : `Health Check em Progresso (${completedClusters.size}/${clusters.length})`}
              </DialogTitle>
            </div>
            <Button
              onClick={() => setShowAlertsReport(true)}
              variant="outline"
              size="sm"
              className="gap-2"
            >
              <AlertCircle className="h-4 w-4" />
              Relatório de Alertas
            </Button>
          </div>
          <DialogDescription className="text-xs sm:text-sm">
            {viewMode
              ? "Visualização de logs e alertas da análise anterior"
              : allCompleted
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
              events={viewMode ? preloadedEvents || [] : (eventsPerCluster[clusters[0]] || [])}
              progress={viewMode ? 100 : getProgress(clusters[0])}
              isComplete={viewMode ? true : isClusterComplete(clusters[0])}
              hasError={viewMode ? false : isClusterError(clusters[0])}
              preloadedResult={preloadedResult}
              viewMode={viewMode}
              onComplete={handleClusterComplete(clusters[0])}
            />
          ) : (
            <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
              <div className="relative">
                <div className="overflow-x-auto rounded-lg border border-zinc-800/80 bg-background/90 px-2 py-1 [&::-webkit-scrollbar]:h-[6px] [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-[#2f2f3d]">
                  <TabsList
                    className="flex w-max gap-2 rounded-md bg-transparent"
                    style={{ gridTemplateColumns: clusters.length <= 3 ? `repeat(${clusters.length}, 1fr)` : undefined }}
                  >
                    {clusters.map((cluster) => (
                      <TabsTrigger
                        key={cluster}
                        value={cluster}
                        className="relative flex-shrink-0 h-[41.8px] rounded-md border border-transparent bg-[#1f1f31] px-4 py-2 text-xs sm:text-sm font-medium text-muted-foreground transition-all hover:bg-[#27273a] hover:text-foreground data-[state=active]:border-primary data-[state=active]:bg-background data-[state=active]:text-primary data-[state=active]:shadow-md"
                      >
                        <div className="flex items-center gap-1 sm:gap-2">
                          <Server className="h-3 w-3 sm:h-4 sm:w-4" />
                          <span className="truncate max-w-[90px] sm:max-w-[150px]">
                            {cluster}
                          </span>
                          {completedClusters.has(cluster) && (
                            <CheckCircle2 className="h-3 w-3 text-green-600 absolute -top-1 -right-1 drop-shadow" />
                          )}
                          {failedClusters.has(cluster) && (
                            <XCircle className="h-3 w-3 text-red-600 absolute -top-1 -right-1 drop-shadow" />
                          )}
                        </div>
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </div>
              </div>

              {/* ✅ Renderizar TODOS os ClusterTabContent simultaneamente (não desmontar ao trocar tab)
                  Usar className para ocultar visualmente as tabs inativas (display: none)
                  Eventos persistem entre trocas de tab via hook multiplexado */}
              <div className="mt-4">
                {clusters.map((cluster) => (
                  <div
                    key={cluster}
                    className={activeTab === cluster ? "block" : "hidden"}
                  >
                    <ClusterTabContent
                      cluster={cluster}
                      sessionId={clusterSessions[cluster]}
                      events={viewMode ? preloadedEvents || [] : (eventsPerCluster[cluster] || [])}
                      progress={viewMode ? 100 : getProgress(cluster)}
                      isComplete={viewMode ? true : isClusterComplete(cluster)}
                      hasError={viewMode ? false : isClusterError(cluster)}
                      preloadedResult={preloadedResult}
                      viewMode={viewMode}
                      onComplete={handleClusterComplete(cluster)}
                    />
                  </div>
                ))}
              </div>
            </Tabs>
          )}

          {/* Actions */}
          <div className="flex flex-col sm:flex-row justify-between gap-2 pt-4 border-t">
            {/* Cancel/Stop button - only show when in progress and NOT in view mode */}
            {!viewMode && !allCompleted && (
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

      {/* Modal de Relatório de Alertas - Usa eventos persistidos + busca do banco */}
      <HealthCheckAlertsReport
        open={showAlertsReport}
        onOpenChange={setShowAlertsReport}
        eventsPerCluster={persistedEventsPerCluster}
        clusters={clusters}
        clusterSessions={clusterSessions}
      />
    </Dialog>
  );
};
