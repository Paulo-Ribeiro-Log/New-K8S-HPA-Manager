// CriticalAlertsBanner - Card de alerta compacto para exibir alertas críticos
// Aparece junto aos outros cards de estatísticas

import { Card } from "@/components/ui/card";
import { AlertTriangle } from "lucide-react";
import { useAllAlerts } from "@/hooks/useAlerts";
import { usePrometheusHealthAlerts } from "@/hooks/usePrometheusHealth";
import { cn } from "@/lib/utils";

interface CriticalAlertsBannerProps {
  cluster: string;
  /** Leva à aba Prometheus: usado quando o único alerta crítico é a integridade do Prometheus. */
  onOpenPrometheus?: () => void;
}

export function CriticalAlertsBanner({ cluster, onOpenPrometheus }: CriticalAlertsBannerProps) {
  console.log(`[CriticalAlertsBanner] Rendering with cluster: ${cluster}`);
  const { summary, hpaAlerts, nodePoolAlerts, loading } = useAllAlerts(cluster);
  // Integridade do Prometheus (independente do Alertmanager): soma ao número de críticos.
  const prom = usePrometheusHealthAlerts(cluster);
  const promCritical = prom.items.filter((i) => i.severity === "critical").length;

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
  const alertmanagerCritical = summary.critical > 0;
  const totalCritical = summary.critical + promCritical;
  const hasCritical = totalCritical > 0;

  const handleClick = () => {
    if (alertmanagerCritical) {
      window.open(`/alerts/${cluster}`, '_blank');
    } else if (promCritical > 0) {
      onOpenPrometheus?.();
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
              {totalCritical}
            </p>
          </div>
          {hasCritical && (
            <p className="text-xs text-red-600 dark:text-red-400 mt-1">
              {criticalHPAAlerts.length > 0 && `${criticalHPAAlerts.length} HPA${criticalHPAAlerts.length > 1 ? "s" : ""}`}
              {criticalHPAAlerts.length > 0 && criticalNodeAlerts.length > 0 && " • "}
              {criticalNodeAlerts.length > 0 && `${criticalNodeAlerts.length} Node${criticalNodeAlerts.length > 1 ? "s" : ""}`}
              {(criticalHPAAlerts.length > 0 || criticalNodeAlerts.length > 0) && promCritical > 0 && " • "}
              {promCritical > 0 && "Prometheus"}
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
