import { useState, useEffect } from "react";
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
import { Loader2 } from "lucide-react";

interface HeaderProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
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
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);
  const [updating, setUpdating] = useState(false);

  const handleUpdate = async () => {
    setUpdating(true);
    try {
      await apiClient.post("/version/update");
      toast.success("Atualização iniciada", {
        description: "O servidor será reiniciado em breve. Aguarde e recarregue a página.",
        duration: 8000,
      });
    } catch {
      toast.error("Falha ao iniciar atualização");
      setUpdating(false);
    }
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
                onClick={handleUpdate}
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
              {selectedCluster
                ? clusters.find((cluster) => cluster === selectedCluster)
                : "Selecione ou busque um cluster..."}
              <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[300px] xl:w-[400px] p-0">
            <Command>
              <CommandInput placeholder="Buscar cluster..." />
              <CommandList>
                <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                <CommandGroup>
                  {clusters.map((cluster) => (
                    <CommandItem
                      key={cluster}
                      value={cluster}
                      onSelect={(currentValue) => {
                        onClusterChange(currentValue === selectedCluster ? "" : currentValue);
                        setOpen(false);
                      }}
                    >
                      <Check
                        className={cn(
                          "mr-2 h-4 w-4",
                          selectedCluster === cluster ? "opacity-100" : "opacity-0"
                        )}
                      />
                      {cluster}
                    </CommandItem>
                  ))}
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
    </header>
  );
};
