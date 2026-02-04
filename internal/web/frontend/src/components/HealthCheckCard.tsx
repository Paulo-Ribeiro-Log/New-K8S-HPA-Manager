import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import {
  CheckCircle2,
  AlertCircle,
  XCircle,
  HelpCircle,
  Clock,
  Zap,
} from "lucide-react";
import type {
  DeploymentHealth,
  ServiceHealth,
  ConfigHealth,
  EventHealth,
  HPAHealth,
  PVCHealth,
  HealthStatus,
  Severity,
} from "@/types/healthcheck";
import {
  SeverityColors,
  SeverityBgColors,
  SeverityLabels,
  SeverityWeight,
} from "@/types/healthcheck";

interface HealthCheckCardProps {
  health: DeploymentHealth | ServiceHealth | ConfigHealth | EventHealth | HPAHealth | PVCHealth;
  type: "deployment" | "service" | "config" | "event" | "hpa" | "pvc";
  selectionMode?: boolean;
  isSelected?: boolean;
  onToggleSelect?: () => void;
}

// Helper function para renderizar badge de severidade
const SeverityBadge = ({ severity }: { severity: Severity }) => {
  const colorClass = SeverityColors[severity] || SeverityColors.info;
  const bgClass = SeverityBgColors[severity] || SeverityBgColors.info;
  const label = SeverityLabels[severity] || severity;

  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium border ${bgClass} ${colorClass}`}>
      {label}
    </span>
  );
};

export const HealthCheckCard = ({
  health,
  type,
  selectionMode = false,
  isSelected = false,
  onToggleSelect,
}: HealthCheckCardProps) => {
  const { name, namespace, status, message, suggestions, checked_at } = health;

  // Status colors
  const statusColors: Record<HealthStatus, string> = {
    healthy: "border-green-200 bg-green-50 dark:bg-green-950/20",
    warning: "border-yellow-200 bg-yellow-50 dark:bg-yellow-950/20",
    critical: "border-red-200 bg-red-50 dark:bg-red-950/20",
    unknown: "border-gray-200 bg-gray-50 dark:bg-gray-950/20",
  };

  // Status icons
  const statusIcons: Record<HealthStatus, JSX.Element> = {
    healthy: <CheckCircle2 className="h-4 w-4 text-green-600" />,
    warning: <AlertCircle className="h-4 w-4 text-yellow-600" />,
    critical: <XCircle className="h-4 w-4 text-red-600" />,
    unknown: <HelpCircle className="h-4 w-4 text-gray-600" />,
  };

  // Status badges
  const statusBadges: Record<HealthStatus, JSX.Element> = {
    healthy: <Badge className="bg-green-600">HEALTHY</Badge>,
    warning: <Badge className="bg-yellow-600">WARNING</Badge>,
    critical: <Badge className="bg-red-600">CRITICAL</Badge>,
    unknown: <Badge variant="outline">UNKNOWN</Badge>,
  };

  // Type-specific details
  const renderDetails = () => {
    if (type === "deployment") {
      const deployment = health as DeploymentHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div>
            <span className="text-muted-foreground">Réplicas:</span>{" "}
            <span className="font-medium">
              {deployment.replicas_ready}/{deployment.replicas_desired}
            </span>
          </div>
          {deployment.containers_crash > 0 && (
            <div className="text-red-600">
              <span className="text-muted-foreground">Crashes:</span>{" "}
              <span className="font-medium">{deployment.containers_crash}</span>
            </div>
          )}
          {deployment.image_pull_errors > 0 && (
            <div className="text-red-600">
              <span className="text-muted-foreground">Image Errors:</span>{" "}
              <span className="font-medium">{deployment.image_pull_errors}</span>
            </div>
          )}
          {deployment.cpu_usage_percent > 0 && (
            <div>
              <span className="text-muted-foreground">CPU:</span>{" "}
              <span className="font-medium">{deployment.cpu_usage_percent.toFixed(1)}%</span>
            </div>
          )}
          {deployment.memory_usage_percent > 0 && (
            <div>
              <span className="text-muted-foreground">Memory:</span>{" "}
              <span className="font-medium">{deployment.memory_usage_percent.toFixed(1)}%</span>
            </div>
          )}
          {deployment.has_liveness_probe !== undefined && (
            <div>
              <span className="text-muted-foreground">Liveness Probe:</span>{" "}
              <span className={deployment.has_liveness_probe ? "text-green-600" : "text-yellow-600"}>
                {deployment.has_liveness_probe ? "✓" : "✗"}
              </span>
            </div>
          )}
          {deployment.has_readiness_probe !== undefined && (
            <div>
              <span className="text-muted-foreground">Readiness Probe:</span>{" "}
              <span className={deployment.has_readiness_probe ? "text-green-600" : "text-yellow-600"}>
                {deployment.has_readiness_probe ? "✓" : "✗"}
              </span>
            </div>
          )}
          {deployment.qos_class && (
            <div>
              <span className="text-muted-foreground">QoS:</span>{" "}
              <span className={`font-medium ${
                deployment.qos_class === "Guaranteed" ? "text-green-600" :
                deployment.qos_class === "Burstable" ? "text-yellow-600" : "text-red-600"
              }`}>
                {deployment.qos_class}
              </span>
            </div>
          )}
          {/* Probe Issues */}
          {deployment.probe_issues && deployment.probe_issues.length > 0 && (
            <div className="col-span-2 mt-2 space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Problemas de Probes:</span>
              {[...deployment.probe_issues]
                .sort((a, b) => (SeverityWeight[b.severity] || 0) - (SeverityWeight[a.severity] || 0))
                .map((issue, i) => (
                <div
                  key={i}
                  className={`text-xs px-2 py-1 rounded border flex items-start gap-2 ${SeverityBgColors[issue.severity] || SeverityBgColors.info}`}
                >
                  <SeverityBadge severity={issue.severity} />
                  <span className={SeverityColors[issue.severity] || SeverityColors.info}>
                    [{issue.container}/{issue.probe_type}] {issue.issue}
                  </span>
                </div>
              ))}
            </div>
          )}
          {/* Resource Issues */}
          {deployment.resource_issues && deployment.resource_issues.length > 0 && (
            <div className="col-span-2 mt-2 space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Problemas de Resources:</span>
              {[...deployment.resource_issues]
                .sort((a, b) => (SeverityWeight[b.severity] || 0) - (SeverityWeight[a.severity] || 0))
                .map((issue, i) => (
                <div
                  key={i}
                  className={`text-xs px-2 py-1 rounded border flex items-start gap-2 ${SeverityBgColors[issue.severity] || SeverityBgColors.info}`}
                >
                  <SeverityBadge severity={issue.severity} />
                  <span className={SeverityColors[issue.severity] || SeverityColors.info}>
                    [{issue.container}/{issue.resource_type}] {issue.issue}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      );
    }

    if (type === "service") {
      const service = health as ServiceHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div>
            <span className="text-muted-foreground">Tipo:</span>{" "}
            <span className="font-medium">{service.service_type}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Alcançável:</span>{" "}
            <span className={service.reachable ? "text-green-600" : "text-red-600"}>
              {service.reachable ? "✓ Sim" : "✗ Não"}
            </span>
          </div>
          {service.latency_ms > 0 && (
            <div>
              <span className="text-muted-foreground">Latência:</span>{" "}
              <span className="font-medium">{service.latency_ms}ms</span>
            </div>
          )}
          {service.config_source && (
            <div className="col-span-2">
              <span className="text-muted-foreground">Fonte:</span>{" "}
              <span className="font-mono text-xs">{service.config_source}</span>
            </div>
          )}
          {service.connection_error && (
            <div className="col-span-2 text-red-600 text-xs">
              <span className="font-medium">Erro:</span> {service.connection_error}
            </div>
          )}
        </div>
      );
    }

    if (type === "config") {
      const config = health as ConfigHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div>
            <span className="text-muted-foreground">Tipo:</span>{" "}
            <span className="font-medium">{config.resource_type}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Existe:</span>{" "}
            <span className={config.exists ? "text-green-600" : "text-red-600"}>
              {config.exists ? "✓ Sim" : "✗ Não"}
            </span>
          </div>
          {config.missing_keys && config.missing_keys.length > 0 && (
            <div className="col-span-2 text-yellow-600 text-xs">
              <span className="font-medium">Chaves Ausentes:</span> {config.missing_keys.join(", ")}
            </div>
          )}
          {config.invalid_values && config.invalid_values.length > 0 && (
            <div className="col-span-2 text-red-600 text-xs">
              <span className="font-medium">Valores Inválidos:</span> {config.invalid_values.join(", ")}
            </div>
          )}
        </div>
      );
    }

    if (type === "event") {
      const event = health as EventHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Razão:</span>{" "}
            <span className="font-medium">{event.reason}</span>
            <SeverityBadge severity={event.severity} />
          </div>
          <div>
            <span className="text-muted-foreground">Tipo:</span>{" "}
            <span className="font-medium">{event.type}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Recurso:</span>{" "}
            <span className="font-mono text-xs">{event.involved_kind}/{event.involved_name}</span>
          </div>
          {event.count > 1 && (
            <div>
              <span className="text-muted-foreground">Ocorrências:</span>{" "}
              <span className="font-bold text-red-600">{event.count}x</span>
            </div>
          )}
          {event.first_timestamp && event.last_timestamp && (
            <div className="col-span-2 text-xs text-muted-foreground">
              <span className="font-medium">Período:</span>{" "}
              {new Date(event.first_timestamp).toLocaleTimeString()} - {new Date(event.last_timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>
      );
    }

    if (type === "hpa") {
      const hpa = health as HPAHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div>
            <span className="text-muted-foreground">Target:</span>{" "}
            <span className="font-mono text-xs">{hpa.target_kind}/{hpa.target_name}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Target Existe:</span>{" "}
            <span className={hpa.target_exists ? "text-green-600" : "text-red-600"}>
              {hpa.target_exists ? "✓ Sim" : "✗ Não"}
            </span>
          </div>
          <div>
            <span className="text-muted-foreground">Réplicas:</span>{" "}
            <span className="font-medium">
              {hpa.current_replicas}/{hpa.desired_replicas}
            </span>
          </div>
          <div>
            <span className="text-muted-foreground">Min/Max:</span>{" "}
            <span className={hpa.is_min_equals_max ? "text-yellow-600 font-bold" : "font-medium"}>
              {hpa.min_replicas}/{hpa.max_replicas}
            </span>
            {hpa.is_min_equals_max && (
              <span className="text-yellow-600 text-xs ml-1">(não escala)</span>
            )}
          </div>
          <div>
            <span className="text-muted-foreground">Métricas:</span>{" "}
            <span className="font-medium">{hpa.metrics_count}</span>
            {hpa.metrics_errors > 0 && (
              <span className="text-red-600 text-xs ml-1">({hpa.metrics_errors} com erro)</span>
            )}
          </div>
          {hpa.is_at_max_replicas && (
            <div className="text-yellow-600">
              <span className="font-medium">⚠️ No limite máximo</span>
            </div>
          )}
          {hpa.is_max_too_low && (
            <div className="text-yellow-600">
              <span className="font-medium">⚠️ Max muito baixo ({`<`}3)</span>
            </div>
          )}
          {hpa.has_scaling_disabled && (
            <div className="col-span-2 text-yellow-600">
              <span className="font-medium">⚠️ Scaling pode estar desabilitado</span>
            </div>
          )}
          {hpa.issues && hpa.issues.length > 0 && (
            <div className="col-span-2 mt-2 space-y-1">
              {/* Ordenar issues por severidade (mais grave primeiro) */}
              {[...hpa.issues]
                .sort((a, b) => (SeverityWeight[b.severity] || 0) - (SeverityWeight[a.severity] || 0))
                .map((issue, i) => (
                <div
                  key={i}
                  className={`text-xs px-2 py-1 rounded border flex items-start gap-2 ${SeverityBgColors[issue.severity] || SeverityBgColors.info}`}
                >
                  <SeverityBadge severity={issue.severity} />
                  <span className={SeverityColors[issue.severity] || SeverityColors.info}>
                    [{issue.type}] {issue.description}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      );
    }

    if (type === "pvc") {
      const pvc = health as PVCHealth;
      return (
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div>
            <span className="text-muted-foreground">Status:</span>{" "}
            <span className={`font-medium ${
              pvc.phase === "Bound" ? "text-green-600" :
              pvc.phase === "Pending" ? "text-yellow-600" :
              pvc.phase === "Lost" ? "text-red-600" : ""
            }`}>
              {pvc.phase}
            </span>
          </div>
          <div>
            <span className="text-muted-foreground">Bound:</span>{" "}
            <span className={pvc.is_bound ? "text-green-600" : "text-yellow-600"}>
              {pvc.is_bound ? "✓ Sim" : "✗ Não"}
            </span>
          </div>
          {pvc.volume_name && (
            <div className="col-span-2">
              <span className="text-muted-foreground">PV:</span>{" "}
              <span className="font-mono text-xs">{pvc.volume_name}</span>
              {!pvc.volume_exists && (
                <span className="text-red-600 text-xs ml-1">(não existe!)</span>
              )}
            </div>
          )}
          <div>
            <span className="text-muted-foreground">StorageClass:</span>{" "}
            <span className={`font-medium ${pvc.storage_class_exists ? "" : "text-red-600"}`}>
              {pvc.storage_class_name || "(default)"}
            </span>
            {!pvc.storage_class_exists && pvc.storage_class_name && (
              <span className="text-red-600 text-xs ml-1">(não existe!)</span>
            )}
          </div>
          <div>
            <span className="text-muted-foreground">Storage:</span>{" "}
            <span className="font-medium">{pvc.requested_storage || "N/A"}</span>
            {pvc.actual_storage && pvc.actual_storage !== pvc.requested_storage && (
              <span className="text-xs text-muted-foreground ml-1">
                (real: {pvc.actual_storage})
              </span>
            )}
          </div>
          {pvc.access_modes && pvc.access_modes.length > 0 && (
            <div className="col-span-2">
              <span className="text-muted-foreground">Access Modes:</span>{" "}
              <span className="font-mono text-xs">
                {pvc.access_modes.join(", ")}
              </span>
              {pvc.access_modes.includes("ReadWriteMany") && (
                <span className="text-yellow-600 text-xs ml-1">(verificar compatibilidade)</span>
              )}
            </div>
          )}
          {pvc.reclaim_policy && (
            <div>
              <span className="text-muted-foreground">Reclaim:</span>{" "}
              <span className={`font-medium ${pvc.reclaim_policy === "Delete" ? "text-yellow-600" : ""}`}>
                {pvc.reclaim_policy}
              </span>
            </div>
          )}
          {pvc.usage_percent > 0 && (
            <div>
              <span className="text-muted-foreground">Uso:</span>{" "}
              <span className={`font-medium ${
                pvc.usage_percent > 90 ? "text-red-600" :
                pvc.usage_percent > 70 ? "text-yellow-600" : "text-green-600"
              }`}>
                {pvc.usage_percent.toFixed(1)}%
              </span>
            </div>
          )}
          {pvc.issues && pvc.issues.length > 0 && (
            <div className="col-span-2 mt-2 space-y-1">
              {/* Ordenar issues por severidade (mais grave primeiro) */}
              {[...pvc.issues]
                .sort((a, b) => (SeverityWeight[b.severity] || 0) - (SeverityWeight[a.severity] || 0))
                .map((issue, i) => (
                <div
                  key={i}
                  className={`text-xs px-2 py-1 rounded border flex items-start gap-2 ${SeverityBgColors[issue.severity] || SeverityBgColors.info}`}
                >
                  <SeverityBadge severity={issue.severity} />
                  <span className={SeverityColors[issue.severity] || SeverityColors.info}>
                    [{issue.type}] {issue.description}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      );
    }

    return null;
  };

  return (
    <Card className={`${statusColors[status]} border-l-4`}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            {/* Checkbox (só aparece em modo seleção) */}
            {selectionMode && (
              <Checkbox
                checked={isSelected}
                onCheckedChange={onToggleSelect}
                onClick={(e) => e.stopPropagation()} // Evitar propagar clique para card
              />
            )}
            {statusIcons[status]}
            <CardTitle className="text-base">{name}</CardTitle>
          </div>
          {statusBadges[status]}
        </div>
        <div className="text-xs text-muted-foreground">
          Namespace: <span className="font-mono">{namespace}</span>
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        {/* Message */}
        <div className="text-sm">
          <p className="font-medium">{message}</p>
        </div>

        {/* Type-specific details */}
        {renderDetails()}

        {/* Suggestions */}
        {suggestions && suggestions.length > 0 && (
          <Accordion type="single" collapsible>
            <AccordionItem value="suggestions" className="border-0">
              <AccordionTrigger className="text-sm py-2 hover:no-underline">
                <span className="flex items-center gap-2">
                  <Zap className="h-4 w-4" />
                  Sugestões ({suggestions.length})
                </span>
              </AccordionTrigger>
              <AccordionContent>
                <ul className="list-disc list-inside space-y-1 text-sm">
                  {suggestions.map((suggestion, i) => (
                    <li key={i} className="text-muted-foreground">
                      {suggestion}
                    </li>
                  ))}
                </ul>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        )}

        {/* Timestamp */}
        <div className="flex items-center gap-2 text-xs text-muted-foreground pt-2 border-t">
          <Clock className="h-3 w-3" />
          Verificado em: {new Date(checked_at).toLocaleString()}
        </div>
      </CardContent>
    </Card>
  );
};
