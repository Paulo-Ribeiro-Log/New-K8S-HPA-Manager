// CriticalAlertsBanner - Card de alerta compacto para exibir alertas críticos
// Aparece junto aos outros cards de estatísticas

import { Card } from "@/components/ui/card";
import { AlertTriangle } from "lucide-react";
import { useAllAlerts } from "@/hooks/useAlerts";
import { cn } from "@/lib/utils";

interface CriticalAlertsBannerProps {
  cluster: string;
}

export function CriticalAlertsBanner({ cluster }: CriticalAlertsBannerProps) {
  console.log(`[CriticalAlertsBanner] Rendering with cluster: ${cluster}`);
  const { summary, hpaAlerts, nodePoolAlerts, loading } = useAllAlerts(cluster);

  // Não mostrar se carregando ou sem dados
  if (loading || !summary) {
    return (
      <Card className="p-4 bg-gradient-card border-border/50">
        <div className="flex items-start justify-between">
          <div className="flex flex-col gap-1">
            <p className="text-xs font-medium text-muted-foreground">Alertas Críticos</p>
            <p className="text-2xl font-bold text-primary">...</p>
          </div>
          <div className="p-2 bg-primary/10 rounded-lg">
            <AlertTriangle className="w-5 h-5 text-primary" />
          </div>
        </div>
      </Card>
    );
  }

  const criticalHPAAlerts = hpaAlerts.filter((a) => a.severity === "critical");
  const criticalNodeAlerts = nodePoolAlerts.filter((a) => a.severity === "critical");
  const hasCritical = summary.critical > 0;

  const handleClick = () => {
    if (hasCritical) {
      window.open(`/alerts/${cluster}`, '_blank');
    }
  };

  return (
    <Card 
      className={cn(
        "p-4 transition-all duration-300 border-border/50",
        hasCritical 
          ? "bg-red-50 dark:bg-red-950/20 border-red-500 hover:shadow-lg hover:-translate-y-1 cursor-pointer animate-pulse-slow" 
          : "bg-gradient-card hover:shadow-lg hover:-translate-y-1"
      )}
      onClick={handleClick}
    >
      <div className="flex items-start justify-between">
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium text-muted-foreground">
            {hasCritical ? "🚨 Alertas Críticos" : "Alertas Críticos"}
          </p>
          <div className="flex items-baseline gap-2">
            <p className={cn(
              "text-2xl font-bold",
              hasCritical ? "text-red-600 dark:text-red-400" : "text-primary"
            )}>
              {summary.critical}
            </p>
          </div>
          {hasCritical && (
            <p className="text-xs text-red-600 dark:text-red-400 mt-1">
              {criticalHPAAlerts.length > 0 && `${criticalHPAAlerts.length} HPA${criticalHPAAlerts.length > 1 ? "s" : ""}`}
              {criticalHPAAlerts.length > 0 && criticalNodeAlerts.length > 0 && " • "}
              {criticalNodeAlerts.length > 0 && `${criticalNodeAlerts.length} Node${criticalNodeAlerts.length > 1 ? "s" : ""}`}
            </p>
          )}
        </div>
        <div className={cn(
          "p-2 rounded-lg",
          hasCritical ? "bg-red-600/20" : "bg-primary/10"
        )}>
          <AlertTriangle className={cn(
            "w-5 h-5",
            hasCritical ? "text-red-600 dark:text-red-400" : "text-primary"
          )} />
        </div>
      </div>
    </Card>
  );
}

// Componente de notificação flutuante (toast) para alertas críticos novos
interface CriticalAlertToastProps {
  alerts: Array<{
    alertName: string;
    severity: string;
    description: string;
  }>;
  onDismiss: () => void;
}

export function CriticalAlertToast({
  alerts,
  onDismiss,
}: CriticalAlertToastProps) {
  const criticalAlerts = alerts.filter((a) => a.severity === "critical");

  if (criticalAlerts.length === 0) {
    return null;
  }

  return (
    <div className="fixed top-20 right-4 z-50 max-w-md animate-in slide-in-from-right-5 duration-300">
      <Alert className="border-red-500 bg-red-50 dark:bg-red-950 shadow-lg">
        <AlertTriangle className="h-5 w-5 text-red-600 animate-pulse" />
        <AlertDescription>
          <div className="flex items-start justify-between gap-2">
            <div className="flex-1">
              <div className="font-semibold text-red-900 dark:text-red-100 mb-2">
                🚨 Novo Alerta Crítico!
              </div>
              <div className="text-sm text-red-800 dark:text-red-200 space-y-1">
                {criticalAlerts.slice(0, 3).map((alert, idx) => (
                  <div key={idx} className="flex items-start gap-1">
                    <span className="font-medium">{alert.alertName}:</span>
                    <span className="text-xs">{alert.description}</span>
                  </div>
                ))}
                {criticalAlerts.length > 3 && (
                  <div className="text-xs opacity-75">
                    +{criticalAlerts.length - 3} mais...
                  </div>
                )}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-red-600 hover:text-red-700"
              onClick={onDismiss}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </AlertDescription>
      </Alert>
    </div>
  );
}
