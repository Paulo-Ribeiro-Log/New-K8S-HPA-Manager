// AlertHealthBadge - Badge que combina Health + Alertas do Prometheus
// Substitui o HealthBadge antigo com informações de alertas do Prometheus

import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import {
  Activity,
  AlertCircle,
  AlertTriangle,
  CheckCircle,
  Loader2,
  Bell,
} from "lucide-react";
import { useHPAHealth } from "@/hooks/useMonitoring";
import { useHPASpecificAlerts } from "@/hooks/useAlerts";
import { cn } from "@/lib/utils";
import { AlertsDialog } from "@/components/AlertsDialog";

interface AlertHealthBadgeProps {
  cluster: string;
  namespace: string;
  hpaName: string;
  showIcon?: boolean;
  className?: string;
  onClick?: () => void;
}

export function AlertHealthBadge({
  cluster,
  namespace,
  hpaName,
  showIcon = true,
  className = "",
  onClick,
}: AlertHealthBadgeProps) {
  const [alertsDialogOpen, setAlertsDialogOpen] = useState(false);
  const { health, loading: healthLoading } = useHPAHealth(
    cluster,
    namespace,
    hpaName
  );
  const { data: alerts, isLoading: alertsLoading } = useHPASpecificAlerts(
    cluster,
    namespace,
    hpaName
  );

  // Contar alertas por severidade
  const criticalCount = alerts?.filter((a) => a.severity === "critical").length || 0;
  const warningCount = alerts?.filter((a) => a.severity === "warning").length || 0;
  const totalAlerts = alerts?.length || 0;

  // Apenas mostrar loading se não houver dados ainda (primeira carga)
  // Durante refetch, manter os dados anteriores visíveis
  const isInitialLoading = (healthLoading || alertsLoading) && !health && !alerts;

  // Loading state - apenas na primeira carga
  if (isInitialLoading) {
    return (
      <Badge variant="outline" className={cn("gap-1", className)}>
        <Loader2 className="h-3 w-3 animate-spin" />
        <span>Verificando...</span>
      </Badge>
    );
  }

  // Determinar status final (alertas têm prioridade sobre health)
  let finalStatus: "critical" | "warning" | "healthy" | "unknown";
  let icon;
  let label;
  let badgeColor;
  let badgeVariant: "default" | "secondary" | "destructive" | "outline";

  if (criticalCount > 0) {
    // CRÍTICO - Há alertas críticos
    finalStatus = "critical";
    icon = AlertCircle;
    label = `${criticalCount} Crítico${criticalCount > 1 ? "s" : ""}`;
    badgeColor = "bg-red-100 text-red-700 border-red-300 hover:bg-red-200";
    badgeVariant = "destructive";
  } else if (warningCount > 0) {
    // WARNING - Há alertas de warning
    finalStatus = "warning";
    icon = AlertTriangle;
    label = `${warningCount} Aviso${warningCount > 1 ? "s" : ""}`;
    badgeColor = "bg-yellow-100 text-yellow-700 border-yellow-300 hover:bg-yellow-200";
    badgeVariant = "secondary";
  } else if (totalAlerts > 0) {
    // INFO - Há alertas info
    finalStatus = "warning";
    icon = Bell;
    label = `${totalAlerts} Alerta${totalAlerts > 1 ? "s" : ""}`;
    badgeColor = "bg-blue-100 text-blue-700 border-blue-300 hover:bg-blue-200";
    badgeVariant = "secondary";
  } else if (health?.status === "critical") {
    // Health crítico (sem alertas do Prometheus)
    finalStatus = "critical";
    icon = AlertCircle;
    label = "Crítico";
    badgeColor = "bg-red-100 text-red-700 border-red-300 hover:bg-red-200";
    badgeVariant = "destructive";
  } else if (health?.status === "warning") {
    // Health warning (sem alertas do Prometheus)
    finalStatus = "warning";
    icon = AlertTriangle;
    label = "Aviso";
    badgeColor = "bg-yellow-100 text-yellow-700 border-yellow-300 hover:bg-yellow-200";
    badgeVariant = "secondary";
  } else if (health?.status === "healthy") {
    // HEALTHY - Tudo OK
    finalStatus = "healthy";
    icon = CheckCircle;
    label = "Healthy";
    badgeColor = "bg-green-100 text-green-700 border-green-300 hover:bg-green-200";
    badgeVariant = "default";
  } else {
    // UNKNOWN
    finalStatus = "unknown";
    icon = Activity;
    label = "Desconhecido";
    badgeColor = "bg-gray-100 text-gray-700 border-gray-300";
    badgeVariant = "outline";
  }

  const Icon = icon;

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onClick) {
      onClick();
    } else if (totalAlerts > 0) {
      // Abrir dialog de alertas
      setAlertsDialogOpen(true);
    }
  };

  const isClickable = onClick || totalAlerts > 0;

  return (
    <>
      <Badge
        variant={badgeVariant}
        className={cn(
          "gap-1 border",
          badgeColor,
          isClickable && "cursor-pointer transition-all hover:scale-105",
          className
        )}
        onClick={isClickable ? handleClick : undefined}
        title={
          totalAlerts > 0
            ? `${criticalCount} crítico(s), ${warningCount} aviso(s) - Clique para ver detalhes`
            : health?.message || `Status: ${label}`
        }
      >
        {showIcon && <Icon className="h-3 w-3" />}
        <span>{label}</span>
      </Badge>

      {/* Dialog de Alertas */}
      <AlertsDialog
        open={alertsDialogOpen}
        onOpenChange={setAlertsDialogOpen}
        cluster={cluster}
        namespace={namespace}
        hpaName={hpaName}
      />
    </>
  );
}

// Componente simplificado apenas para exibir alertas (sem health check)
interface SimpleAlertBadgeProps {
  alerts: Array<{ severity: string }>;
  showIcon?: boolean;
  className?: string;
  onClick?: () => void;
}

export function SimpleAlertBadge({
  alerts,
  showIcon = true,
  className = "",
  onClick,
}: SimpleAlertBadgeProps) {
  const criticalCount = alerts.filter((a) => a.severity === "critical").length;
  const warningCount = alerts.filter((a) => a.severity === "warning").length;
  const totalAlerts = alerts.length;

  if (totalAlerts === 0) {
    return (
      <Badge
        variant="default"
        className={cn(
          "gap-1 border bg-green-100 text-green-700 border-green-300",
          className
        )}
      >
        {showIcon && <CheckCircle className="h-3 w-3" />}
        <span>OK</span>
      </Badge>
    );
  }

  let icon;
  let label;
  let badgeColor;

  if (criticalCount > 0) {
    icon = AlertCircle;
    label = `${criticalCount} Crítico${criticalCount > 1 ? "s" : ""}`;
    badgeColor = "bg-red-100 text-red-700 border-red-300 hover:bg-red-200";
  } else if (warningCount > 0) {
    icon = AlertTriangle;
    label = `${warningCount} Aviso${warningCount > 1 ? "s" : ""}`;
    badgeColor = "bg-yellow-100 text-yellow-700 border-yellow-300 hover:bg-yellow-200";
  } else {
    icon = Bell;
    label = `${totalAlerts} Info`;
    badgeColor = "bg-blue-100 text-blue-700 border-blue-300 hover:bg-blue-200";
  }

  const Icon = icon;

  return (
    <Badge
      variant="secondary"
      className={cn(
        "gap-1 border cursor-pointer hover:scale-105 transition-all",
        badgeColor,
        className
      )}
      onClick={onClick}
      title={`${criticalCount} crítico(s), ${warningCount} aviso(s), ${totalAlerts - criticalCount - warningCount} info - Clique para ver detalhes`}
    >
      {showIcon && <Icon className="h-3 w-3" />}
      <span>{label}</span>
    </Badge>
  );
}
