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
  HealthStatus,
} from "@/types/healthcheck";

interface HealthCheckCardProps {
  health: DeploymentHealth | ServiceHealth | ConfigHealth;
  type: "deployment" | "service" | "config";
  selectionMode?: boolean;
  isSelected?: boolean;
  onToggleSelect?: () => void;
}

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
