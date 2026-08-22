import { useEffect, useRef, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { ptBR } from "date-fns/locale";
import { Check, CheckCheck, Trash2, AlertCircle, AlertTriangle, Info } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { InAppNotification } from "@/hooks/useNotifications";

interface NotificationDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  notifications: InAppNotification[];
  unreadCount: number;
  onMarkAsRead: (id: string) => void;
  onMarkAllAsRead: () => void;
  onClearAll: () => void;
  onNotificationClick?: (notification: InAppNotification) => void;
}

function getSeverityIcon(severity: string) {
  switch (severity) {
    case "critical":
      return <AlertCircle className="h-5 w-5 text-red-500" />;
    case "warning":
      return <AlertTriangle className="h-5 w-5 text-yellow-500" />;
    default:
      return <Info className="h-5 w-5 text-blue-500" />;
  }
}

function getSeverityBadge(severity: string) {
  const variants = {
    critical: "destructive",
    warning: "default",
    info: "secondary",
  } as const;

  return (
    <Badge variant={variants[severity as keyof typeof variants] || "secondary"} className="text-xs">
      {severity.toUpperCase()}
    </Badge>
  );
}

// Largura arrastável — pedido explícito do usuário. Limites e default escolhidos em torno do
// max-w-md (28rem=448px) que era fixo antes; persistida em localStorage (mesmo padrão de
// ce_font_size/ce_word_wrap em CodeEditorTab.tsx) pra lembrar a preferência entre sessões.
const NOTIFICATION_DRAWER_MIN_WIDTH = 380;
const NOTIFICATION_DRAWER_MAX_WIDTH = 900;
const NOTIFICATION_DRAWER_DEFAULT_WIDTH = 448;
const NOTIFICATION_DRAWER_WIDTH_KEY = "notification_drawer_width";

export function NotificationDrawer({
  open,
  onOpenChange,
  notifications,
  unreadCount,
  onMarkAsRead,
  onMarkAllAsRead,
  onClearAll,
  onNotificationClick,
}: NotificationDrawerProps) {
  const [width, setWidth] = useState(() => {
    const saved = Number(localStorage.getItem(NOTIFICATION_DRAWER_WIDTH_KEY));
    return saved >= NOTIFICATION_DRAWER_MIN_WIDTH && saved <= NOTIFICATION_DRAWER_MAX_WIDTH
      ? saved
      : NOTIFICATION_DRAWER_DEFAULT_WIDTH;
  });
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      // O painel abre a partir da borda direita da tela (Sheet side="right", padrão) — arrastar
      // pra ESQUERDA (o mouse anda pra um clientX menor) precisa AUMENTAR a largura, o oposto do
      // sinal usado num painel que abre da esquerda (ex: ResizeDivider em SplitView.tsx).
      const delta = lastX.current - e.clientX;
      lastX.current = e.clientX;
      setWidth((w) => Math.max(NOTIFICATION_DRAWER_MIN_WIDTH, Math.min(NOTIFICATION_DRAWER_MAX_WIDTH, w + delta)));
    };
    const onUp = () => {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      setWidth((w) => {
        localStorage.setItem(NOTIFICATION_DRAWER_WIDTH_KEY, String(w));
        return w;
      });
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, []);

  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    dragging.current = true;
    lastX.current = e.clientX;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  };

  const handleNotificationClick = (notification: InAppNotification) => {
    // Marcar como lida automaticamente ao clicar
    if (!notification.read) {
      onMarkAsRead(notification.id);
    }

    // Chamar callback se existir e tiver metadados
    if (onNotificationClick && notification.cluster) {
      onNotificationClick(notification);
    }
  };
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      {/* sm:max-w-md removido de propósito — um max-width fixo capa a largura mesmo com o
          `width` inline abaixo tentando crescer além dele. maxWidth: 95vw é só uma rede de
          segurança pra não estourar telas estreitas.
          NUNCA adicionar `relative`/`absolute`/etc. na className do SheetContent em si — a
          classe base (ui/sheet.tsx) já define `fixed` (mais `inset-y-0 right-0 h-full` pro lado
          direito); cn()/tailwind-merge trata utilitários de position como um grupo conflitante
          (só o último sobrevive), então `relative` aqui REMOVERIA o `fixed` da base, quebrando o
          posicionamento inteiro do painel (bug real reproduzido: painel virou algo solto no
          fluxo normal do documento, "tomando a tela toda e sem informação" — mesma classe de bug
          já documentada no CLAUDE.md pro ServiceNowImportModal). O `relative` que o handle de
          resize precisa fica no wrapper INTERNO abaixo, não aqui. */}
      <SheetContent className="w-full" style={{ maxWidth: "95vw", width: `${width}px` }}>
        <div className="relative h-full">
          {/* Handle de resize na borda esquerda (painel abre da direita) — mesmo padrão de drag
              de ResizeDivider (SplitView.tsx) e dos handles de PodQuickViewModal.tsx,
              reimplementado aqui em vez de importado porque nenhum dos dois é um componente
              genérico exportado pra esse tipo de painel lateral fixo (Sheet), só pra
              split-view/dialog flutuante. */}
          {/* -left-6 (não left-0): SheetContent tem padding p-6 (24px) — o wrapper `relative`
              acima já nasce inset 24px da borda visual real do painel; -left-6 compensa
              exatamente esse padding pra o handle ficar em cima da borda esquerda de verdade,
              não 24px pra dentro dela. */}
          <div
            className="absolute -left-6 top-0 h-full w-1.5 cursor-col-resize hover:bg-primary/40 active:bg-primary/60 transition-colors z-10"
            onMouseDown={startResize}
            title="Arrastar para redimensionar"
          />
          <SheetHeader>
            <SheetTitle className="flex items-center justify-between">
              <span>Notificações</span>
              {unreadCount > 0 && (
                <Badge variant="destructive" className="ml-2">
                  {unreadCount} não lidas
                </Badge>
              )}
            </SheetTitle>
            <SheetDescription>
              Central de notificações do sistema
            </SheetDescription>
          </SheetHeader>

          {/* Actions */}
          <div className="flex gap-2 mt-4 mb-4">
            <Button
              variant="outline"
              size="sm"
              onClick={onMarkAllAsRead}
              disabled={unreadCount === 0}
              className="flex-1"
            >
              <CheckCheck className="h-4 w-4 mr-2" />
              Marcar todas lidas
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={onClearAll}
              disabled={notifications.length === 0}
              className="flex-1"
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Limpar todas
            </Button>
          </div>

          {/* Notifications List */}
          <ScrollArea className="h-[calc(100vh-12rem)]">
            {notifications.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                <Info className="h-12 w-12 mb-4 opacity-50" />
                <p className="text-sm">Nenhuma notificação</p>
              </div>
            ) : (
              <div className="space-y-2 pr-4">
                {notifications.map((notification) => (
                  <div
                    key={notification.id}
                    onClick={() => handleNotificationClick(notification)}
                    className={cn(
                      "border rounded-lg p-3 transition-all hover:shadow-md",
                      !notification.read && "bg-blue-50/50 dark:bg-blue-950/20 border-blue-200 dark:border-blue-800",
                      notification.cluster && "cursor-pointer hover:scale-[1.01]"
                    )}
                  >
                    <div className="flex items-start gap-3">
                      {/* Icon */}
                      <div className="flex-shrink-0 mt-0.5">
                        {getSeverityIcon(notification.severity)}
                      </div>

                      {/* Content */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-start justify-between gap-2 mb-1">
                          <h4 className="text-sm font-semibold truncate">
                            {notification.title}
                          </h4>
                          {getSeverityBadge(notification.severity)}
                        </div>

                        <p className="text-xs text-muted-foreground whitespace-pre-wrap mb-2">
                          {notification.message}
                        </p>

                        <div className="flex items-center justify-between">
                          <span className="text-xs text-muted-foreground">
                            {formatDistanceToNow(new Date(notification.timestamp), {
                              addSuffix: true,
                              locale: ptBR,
                            })}
                          </span>

                          {!notification.read && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => onMarkAsRead(notification.id)}
                              className="h-7 text-xs"
                            >
                              <Check className="h-3 w-3 mr-1" />
                              Marcar lida
                            </Button>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </ScrollArea>
        </div>
      </SheetContent>
    </Sheet>
  );
}
