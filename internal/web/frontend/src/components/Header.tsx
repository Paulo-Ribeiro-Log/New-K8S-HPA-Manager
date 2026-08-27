import { useState, useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { CheckCircle, Zap, Save, FolderOpen, FileText, ChevronsUpDown, Check, History, AlertCircle } from "lucide-react";
import { NotificationBell } from "@/components/NotificationBell";
import { NotificationDrawer } from "@/components/NotificationDrawer";
import { AlertsDialog } from "@/components/AlertsDialog";
import { UserProfileMenu } from "@/components/UserProfileMenu";
import { useNotifications } from "@/hooks/useNotifications";
import type { InAppNotification } from "@/hooks/useNotifications";
import { cn } from "@/lib/utils";
import { apiClient } from "@/lib/api/client";
import type { VersionInfo } from "@/lib/api/types";
import { toast } from "sonner";
import { cloudProviderBadge } from "@/hooks/useCloudProvider";
import { Loader2 } from "lucide-react";
import { isProdClusterName, isHlgClusterName } from "@/lib/clusterSafety";

interface HeaderProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
  /** Mapa de context → cloud_provider para exibir badges */
  clusterProviders?: Record<string, string>;
  /** Mapa de context → nome de exibição (normaliza ARNs EKS) */
  clusterDisplayNames?: Record<string, string>;
  modifiedCount: number;
  onApplyAll: () => void;
  onApplySequential?: () => void;
  onSaveSession?: () => void;
  onLoadSession?: () => void;
  onViewLogs?: () => void;
  onViewHistory?: () => void;
  onLogout: () => void;
}

export const Header = ({
  selectedCluster,
  onClusterChange,
  clusters,
  clusterProviders,
  clusterDisplayNames,
  modifiedCount,
  onApplyAll,
  onApplySequential,
  onSaveSession,
  onLoadSession,
  onViewLogs,
  onViewHistory,
  onLogout,
}: HeaderProps) => {
  const [open, setOpen] = useState(false);
  // Correção de segurança pedida pelo usuário: um analista sobrecarregado pode selecionar o
  // cluster errado (PRD em vez de HLG) sem perceber — filtro Todos/HLG/PRD reduz a lista visível
  // na hora de escolher, e clusters PRD ganham destaque em laranja (na lista E no botão do
  // cluster já selecionado, que fica sempre visível — o sinal mais importante pra não esquecer em
  // qual ambiente está trabalhando). isProdClusterName/isHlgClusterName (lib/clusterSafety.ts)
  // — detecção AMPLA do lado PRD (pedido explícito: pega qualquer "produ*" no nome, não só o
  // sufixo "-prd" exato, pra cobrir clusters como um EKS "asaplog-production").
  const [envFilter, setEnvFilter] = useState<"all" | "hlg" | "prd">("all");
  const filteredClusters = clusters.filter((c) => {
    if (envFilter === "all") return true;
    if (envFilter === "prd") return isProdClusterName(c);
    return isHlgClusterName(c);
  });
  const selectedIsProd = isProdClusterName(selectedCluster);
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);
  const [updating, setUpdating] = useState(false);
  const [confirmUpdateOpen, setConfirmUpdateOpen] = useState(false);

  // Polling pós-update: o processo mata e reinicia o próprio servidor (install-from-github.sh),
  // então não dá pra acompanhar via SSE — a conexão cairia junto. Reaproveita o mesmo padrão de
  // polling já usado no Device Auth Grant do GCP (SNATPortWidget.tsx): useRef pro handle do
  // interval, catch silencioso em erro de rede (esperado enquanto o servidor está fora do ar).
  const updatePollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const updateTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const stopUpdatePolling = () => {
    if (updatePollRef.current) {
      clearInterval(updatePollRef.current);
      updatePollRef.current = null;
    }
    if (updateTimeoutRef.current) {
      clearTimeout(updateTimeoutRef.current);
      updateTimeoutRef.current = null;
    }
  };

  useEffect(() => stopUpdatePolling, []);

  const handleUpdate = async () => {
    setConfirmUpdateOpen(false);
    setUpdating(true);
    const versionBeforeUpdate = versionInfo?.current_version;

    try {
      await apiClient.post("/version/update");
    } catch {
      toast.error("Falha ao iniciar atualização");
      setUpdating(false);
      return;
    }

    toast.loading("Atualizando servidor...", {
      id: "server-update",
      description: "O servidor vai reiniciar sozinho. Isso pode levar até 2 minutos — não feche esta aba.",
    });

    stopUpdatePolling();
    updatePollRef.current = setInterval(async () => {
      try {
        const info = await apiClient.getVersion();
        // Servidor ainda pode responder com a versão antiga por alguns segundos (download/instalação
        // acontecem antes do kill do processo atual) — só considerar concluído quando a versão mudar.
        if (info.current_version !== versionBeforeUpdate) {
          stopUpdatePolling();
          toast.success(`Atualizado para v${info.current_version}! Recarregando...`, { id: "server-update" });
          setTimeout(() => window.location.reload(), 1500);
        }
      } catch {
        // Servidor fora do ar durante o restart — esperado, tenta de novo no próximo tick.
      }
    }, 4000);

    updateTimeoutRef.current = setTimeout(() => {
      stopUpdatePolling();
      setUpdating(false);
      toast.error("Atualização demorando mais que o esperado", {
        id: "server-update",
        description: "Verifique o servidor manualmente ou recarregue a página em alguns instantes.",
        duration: 15000,
      });
    }, 3 * 60 * 1000);
  };
  const [notificationDrawerOpen, setNotificationDrawerOpen] = useState(false);
  const [alertsDialogOpen, setAlertsDialogOpen] = useState(false);
  const [alertsDialogContext, setAlertsDialogContext] = useState<{
    cluster: string;
    namespace?: string;
    hpaName?: string;
  } | null>(null);

  // Hook de notificações (polling a cada 10 segundos)
  const {
    notifications,
    unreadCount,
    markAsRead,
    markAllAsRead,
    clearAll,
  } = useNotifications(10000);

  useEffect(() => {
    // Buscar versão ao montar componente
    apiClient.getVersion().then(setVersionInfo).catch(console.error);
  }, []);

  const handleNotificationClick = (notification: InAppNotification) => {
    if (notification.cluster) {
      // Configurar contexto do AlertsDialog
      setAlertsDialogContext({
        cluster: notification.cluster,
        namespace: notification.namespace,
        hpaName: notification.hpaName,
      });

      // Fechar drawer de notificações
      setNotificationDrawerOpen(false);

      // Abrir dialog de alertas
      setAlertsDialogOpen(true);
    }
  };

  return (
    <header className="h-16 bg-gradient-primary flex items-center justify-between px-6 shadow-lg flex-shrink-0">
      <div className="flex items-center gap-4">
        <div className="flex flex-col">
          <div className="flex items-center gap-2">
            <h1 className="text-xl font-semibold text-white tracking-tight">
              k8s-hpa-manager
            </h1>
            {versionInfo?.update_available && (
              <button
                onClick={() => setConfirmUpdateOpen(true)}
                disabled={updating}
                className="flex items-center gap-1 px-2 py-0.5 bg-amber-500 hover:bg-amber-600 disabled:opacity-60 text-white text-xs font-medium rounded-full transition-colors"
                title={`Nova versão disponível: ${versionInfo.latest_version} — clique para atualizar`}
              >
                {updating ? <Loader2 className="w-3 h-3 animate-spin" /> : <AlertCircle className="w-3 h-3" />}
                {updating ? "Atualizando..." : "Update"}
              </button>
            )}
          </div>
          {versionInfo && (
            <span className="text-xs text-white/60">
              v{versionInfo.current_version}
            </span>
          )}
        </div>

        {/* Combobox de cluster com busca integrada */}
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              role="combobox"
              aria-expanded={open}
              className="w-[180px] sm:w-[240px] lg:w-[300px] xl:w-[400px] justify-between bg-white/20 border-white/30 text-white hover:bg-white/25 hover:text-white"
            >
              <span
                className={cn(
                  "truncate",
                  selectedIsProd && "text-amber-300 font-semibold"
                )}
              >
                {selectedCluster
                  ? (clusterDisplayNames?.[selectedCluster] ?? selectedCluster)
                  : "Selecione ou busque um cluster..."}
              </span>
              <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[300px] xl:w-[400px] p-0">
            {/* Filtro Todos/HLG/PRD — reduz a lista antes mesmo da busca por texto, pra um
                analista sobrecarregado não escolher o ambiente errado por engano. */}
            <div className="flex items-center gap-1 px-2 pt-2 pb-1.5 border-b border-border">
              {(["all", "hlg", "prd"] as const).map((f) => (
                <button
                  key={f}
                  type="button"
                  onClick={() => setEnvFilter(f)}
                  className={cn(
                    "text-xs px-2 py-1 rounded",
                    envFilter === f
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted"
                  )}
                >
                  {f === "all" ? "Todos" : f.toUpperCase()}
                </button>
              ))}
            </div>
            <Command>
              <CommandInput placeholder="Buscar cluster..." />
              <CommandList>
                <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                <CommandGroup>
                  {filteredClusters.map((cluster) => {
                    const badge = clusterProviders
                      ? cloudProviderBadge(clusterProviders[cluster])
                      : null;
                    const isProd = isProdClusterName(cluster);
                    return (
                      <CommandItem
                        key={cluster}
                        value={clusterDisplayNames?.[cluster] ?? cluster}
                        onSelect={() => {
                          onClusterChange(cluster === selectedCluster ? "" : cluster);
                          setOpen(false);
                        }}
                      >
                        <Check
                          className={cn(
                            "mr-2 h-4 w-4",
                            selectedCluster === cluster ? "opacity-100" : "opacity-0"
                          )}
                        />
                        {badge && (
                          <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded mr-1.5 ${badge.className}`}>
                            {badge.label}
                          </span>
                        )}
                        <span className={isProd ? "text-amber-600 dark:text-amber-400 font-medium" : undefined}>
                          {clusterDisplayNames?.[cluster] ?? cluster}
                        </span>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>

      <div className="flex items-center gap-3">
        {/* Session Management Buttons */}
        {onLoadSession && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onLoadSession}
            title="Load Session"
          >
            <FolderOpen className="w-4 h-4 xl:mr-2" />
            <span className="hidden xl:inline">Load Session</span>
          </Button>
        )}
        
        {onSaveSession && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onSaveSession}
            title="Save Session"
          >
            <Save className="w-4 h-4 xl:mr-2" />
            <span className="hidden xl:inline">Save Session</span>
          </Button>
        )}
        
        {onApplySequential && (
          <Button
            variant="secondary"
            className="bg-warning hover:bg-warning/90 text-white border-0"
            onClick={onApplySequential}
          >
            <Zap className="w-4 h-4 mr-2" />
            Apply Sequential
          </Button>
        )}
        
        {modifiedCount > 0 && (
          <Button
            variant="secondary"
            className="bg-success hover:bg-success/90 text-white border-0"
            onClick={onApplyAll}
          >
            <CheckCircle className="w-4 h-4 mr-2" />
            Apply All
            <span className="ml-2 px-2 py-0.5 bg-white/20 rounded-full text-xs">
              {modifiedCount}
            </span>
          </Button>
        )}
        
        {onViewLogs && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onViewLogs}
            title="View System Logs"
          >
            <FileText className="w-4 h-4" />
          </Button>
        )}

        {onViewHistory && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onViewHistory}
            title="View Change History"
          >
            <History className="w-4 h-4" />
          </Button>
        )}

        {/* Notification Bell */}
        <NotificationBell
          unreadCount={unreadCount}
          onClick={() => setNotificationDrawerOpen(true)}
        />

        {/* User Profile Menu - substitui userInfo, SREBadge, ModeToggle e Logout */}
        <UserProfileMenu onLogout={onLogout} />
      </div>

      {/* Notification Drawer */}
      <NotificationDrawer
        open={notificationDrawerOpen}
        onOpenChange={setNotificationDrawerOpen}
        notifications={notifications}
        unreadCount={unreadCount}
        onMarkAsRead={markAsRead}
        onMarkAllAsRead={markAllAsRead}
        onClearAll={clearAll}
        onNotificationClick={handleNotificationClick}
      />

      {/* Alerts Dialog */}
      {alertsDialogContext && (
        <AlertsDialog
          open={alertsDialogOpen}
          onOpenChange={setAlertsDialogOpen}
          cluster={alertsDialogContext.cluster}
          namespace={alertsDialogContext.namespace}
          hpaName={alertsDialogContext.hpaName}
        />
      )}

      {/* Confirmação de Update */}
      <AlertDialog open={confirmUpdateOpen} onOpenChange={setConfirmUpdateOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Atualizar para v{versionInfo?.latest_version}?</AlertDialogTitle>
            <AlertDialogDescription>
              O servidor vai reiniciar automaticamente durante a atualização — a conexão cai por
              alguns instantes (até ~2 minutos) e a página recarrega sozinha quando terminar. Não
              feche esta aba enquanto isso.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleUpdate}>Atualizar agora</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </header>
  );
};
