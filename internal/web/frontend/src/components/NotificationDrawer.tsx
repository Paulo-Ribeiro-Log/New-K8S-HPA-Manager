import { formatDistanceToNow } from "date-fns";
import { ptBR } from "date-fns/locale";
import { X, Check, CheckCheck, Trash2, AlertCircle, AlertTriangle, Info } from "lucide-react";
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

export function NotificationDrawer({
  open,
  onOpenChange,
  notifications,
  unreadCount,
  onMarkAsRead,
  onMarkAllAsRead,
  onClearAll,
}: NotificationDrawerProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-md">
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
                  className={cn(
                    "border rounded-lg p-3 transition-all hover:shadow-md",
                    !notification.read && "bg-blue-50/50 dark:bg-blue-950/20 border-blue-200 dark:border-blue-800"
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
      </SheetContent>
    </Sheet>
  );
}
