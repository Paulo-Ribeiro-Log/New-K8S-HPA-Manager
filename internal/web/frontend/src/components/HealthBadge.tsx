// HealthBadge - Badge de status de health do HPA

import { Badge } from "@/components/ui/badge";
import { Activity, AlertCircle, CheckCircle, Loader2 } from "lucide-react";
import { useHPAHealth } from "@/hooks/useMonitoring";
import type { HPAHealth } from "@/lib/api/types";

interface HealthBadgeProps {
  cluster: string;
  namespace: string;
  hpaName: string;
  showIcon?: boolean;
  className?: string;
}

const healthConfig = {
  healthy: {
    variant: "default" as const,
    icon: CheckCircle,
    color: "text-green-600",
    bgColor: "bg-green-50 border-green-200",
    label: "Healthy",
  },
  warning: {
    variant: "secondary" as const,
    icon: AlertCircle,
    color: "text-yellow-600",
    bgColor: "bg-yellow-50 border-yellow-200",
    label: "Warning",
  },
  critical: {
    variant: "destructive" as const,
    icon: AlertCircle,
    color: "text-red-600",
    bgColor: "bg-red-50 border-red-200",
    label: "Critical",
  },
};

export function HealthBadge({
  cluster,
  namespace,
  hpaName,
  showIcon = true,
  className = "",
}: HealthBadgeProps) {
  const { health, loading, error } = useHPAHealth(cluster, namespace, hpaName);

  // Loading state
  if (loading) {
    return (
      <Badge variant="outline" className={`${className} gap-1`}>
        <Loader2 className="h-3 w-3 animate-spin" />
        <span>Checking...</span>
      </Badge>
    );
  }

  // Error state
  if (error || !health) {
    return (
      <Badge variant="outline" className={`${className} gap-1 text-muted-foreground`}>
        <Activity className="h-3 w-3" />
        <span>Unknown</span>
      </Badge>
    );
  }

  const config = healthConfig[health.status];
  const Icon = config.icon;

  return (
    <Badge
      variant={config.variant}
      className={`${className} ${config.bgColor} ${config.color} gap-1 border`}
      title={health.message || `Health status: ${config.label}`}
    >
      {showIcon && <Icon className="h-3 w-3" />}
      <span>{config.label}</span>
      {health.score !== undefined && (
        <span className="ml-1 opacity-75">({health.score}%)</span>
      )}
    </Badge>
  );
}

/**
 * Componente simplificado que apenas exibe o status sem fazer fetch
 * Útil quando o health já foi carregado em outro lugar
 */
interface SimpleHealthBadgeProps {
  health: HPAHealth;
  showIcon?: boolean;
  showScore?: boolean;
  className?: string;
}

export function SimpleHealthBadge({
  health,
  showIcon = true,
  showScore = false,
  className = "",
}: SimpleHealthBadgeProps) {
  const config = healthConfig[health.status];
  const Icon = config.icon;

  return (
    <Badge
      variant={config.variant}
      className={`${className} ${config.bgColor} ${config.color} gap-1 border`}
      title={health.message || `Health status: ${config.label}`}
    >
      {showIcon && <Icon className="h-3 w-3" />}
      <span>{config.label}</span>
      {showScore && health.score !== undefined && (
        <span className="ml-1 opacity-75">({health.score}%)</span>
      )}
    </Badge>
  );
}
