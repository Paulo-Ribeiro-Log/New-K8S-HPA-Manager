// AlertsPanel - Painel de exibição de anomalias detectadas

import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { AlertCircle, AlertTriangle, Info, RefreshCw, CheckCircle, Clock } from "lucide-react";
import { useAnomalies } from "@/hooks/useMonitoring";
import type { Anomaly } from "@/lib/api/types";
import { formatDistanceToNow } from "date-fns";
import { ptBR } from "date-fns/locale";

interface AlertsPanelProps {
  cluster?: string;
  className?: string;
}

const severityConfig = {
  low: {
    icon: Info,
    color: "text-blue-600",
    bgColor: "bg-blue-50",
    borderColor: "border-blue-200",
    variant: "outline" as const,
    label: "Baixa",
  },
  medium: {
    icon: AlertCircle,
    color: "text-yellow-600",
    bgColor: "bg-yellow-50",
    borderColor: "border-yellow-200",
    variant: "secondary" as const,
    label: "Média",
  },
  high: {
    icon: AlertTriangle,
    color: "text-orange-600",
    bgColor: "bg-orange-50",
    borderColor: "border-orange-200",
    variant: "destructive" as const,
    label: "Alta",
  },
  critical: {
    icon: AlertCircle,
    color: "text-red-600",
    bgColor: "bg-red-50",
    borderColor: "border-red-200",
    variant: "destructive" as const,
    label: "Crítica",
  },
};

const anomalyTypeLabels: Record<string, string> = {
  cpu_spike: "Pico de CPU",
  cpu_sustained_high: "CPU Alta Sustentada",
  memory_spike: "Pico de Memória",
  memory_sustained_high: "Memória Alta Sustentada",
  memory_leak: "Vazamento de Memória",
  rapid_scaling: "Escalonamento Rápido",
  oscillation: "Oscilação",
  underutilization: "Subutilização",
  near_limit: "Próximo do Limite",
  metric_unavailable: "Métrica Indisponível",
};

export function AlertsPanel({ cluster, className = "" }: AlertsPanelProps) {
  const [severityFilter, setSeverityFilter] = useState<string>("all");
  const { anomalies, count, loading, error, refetch } = useAnomalies(cluster, severityFilter === "all" ? undefined : severityFilter);

  // Filtrar anomalias não resolvidas
  const activeAnomalies = anomalies.filter(a => !a.resolved);
  const resolvedAnomalies = anomalies.filter(a => a.resolved);

  const formatDuration = (seconds: number): string => {
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}min`;
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}min`;
  };

  const renderAnomaly = (anomaly: Anomaly) => {
    const config = severityConfig[anomaly.severity];
    const Icon = config.icon;
    const typeLabel = anomalyTypeLabels[anomaly.type] || anomaly.type;

    return (
      <div
        key={anomaly.id}
        className={`p-4 rounded-lg border ${config.borderColor} ${config.bgColor} space-y-2`}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-start gap-2 flex-1">
            <Icon className={`h-5 w-5 ${config.color} mt-0.5`} />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <h4 className="font-semibold text-sm">{typeLabel}</h4>
                <Badge variant={config.variant} className="text-xs">
                  {config.label}
                </Badge>
                {anomaly.resolved && (
                  <Badge variant="outline" className="text-xs gap-1">
                    <CheckCircle className="h-3 w-3" />
                    Resolvido
                  </Badge>
                )}
              </div>
              <p className="text-xs text-muted-foreground mt-1">
                {anomaly.cluster} / {anomaly.namespace} / {anomaly.hpa_name}
              </p>
            </div>
          </div>
        </div>

        {/* Message */}
        <p className="text-sm">{anomaly.message}</p>

        {/* Metadata */}
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <div className="flex items-center gap-1">
            <Clock className="h-3 w-3" />
            <span>
              {formatDistanceToNow(new Date(anomaly.detected_at), {
                addSuffix: true,
                locale: ptBR,
              })}
            </span>
          </div>
          <div>
            Duração: {formatDuration(anomaly.duration_seconds)}
          </div>
          {anomaly.resolved && anomaly.resolved_at && (
            <div>
              Resolvido: {formatDistanceToNow(new Date(anomaly.resolved_at), {
                addSuffix: true,
                locale: ptBR,
              })}
            </div>
          )}
        </div>

        {/* Details */}
        {Object.keys(anomaly.details).length > 0 && (
          <details className="text-xs">
            <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
              Ver detalhes técnicos
            </summary>
            <pre className="mt-2 p-2 bg-background rounded border overflow-x-auto">
              {JSON.stringify(anomaly.details, null, 2)}
            </pre>
          </details>
        )}
      </div>
    );
  };

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <AlertCircle className="h-5 w-5" />
              Anomalias Detectadas
            </CardTitle>
            <CardDescription>
              {cluster ? `Cluster: ${cluster}` : "Todas os clusters"}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Select value={severityFilter} onValueChange={setSeverityFilter}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todas</SelectItem>
                <SelectItem value="critical">Crítica</SelectItem>
                <SelectItem value="high">Alta</SelectItem>
                <SelectItem value="medium">Média</SelectItem>
                <SelectItem value="low">Baixa</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="icon"
              onClick={() => refetch()}
              disabled={loading}
            >
              <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {error && (
          <div className="p-4 border border-red-200 bg-red-50 rounded-lg text-sm text-red-600">
            <AlertCircle className="h-4 w-4 inline mr-2" />
            {error}
          </div>
        )}

        {loading && !error && (
          <div className="text-center py-8 text-muted-foreground">
            <RefreshCw className="h-6 w-6 animate-spin mx-auto mb-2" />
            <p>Carregando anomalias...</p>
          </div>
        )}

        {!loading && !error && count === 0 && (
          <div className="text-center py-8">
            <CheckCircle className="h-12 w-12 text-green-600 mx-auto mb-2" />
            <p className="text-lg font-semibold">Nenhuma anomalia detectada</p>
            <p className="text-sm text-muted-foreground">
              Todos os HPAs estão operando normalmente
            </p>
          </div>
        )}

        {!loading && !error && count > 0 && (
          <div className="space-y-6">
            {/* Active Anomalies */}
            {activeAnomalies.length > 0 && (
              <div className="space-y-3">
                <h3 className="text-sm font-semibold text-muted-foreground">
                  Ativas ({activeAnomalies.length})
                </h3>
                <div className="space-y-3">
                  {activeAnomalies.map(renderAnomaly)}
                </div>
              </div>
            )}

            {/* Resolved Anomalies */}
            {resolvedAnomalies.length > 0 && (
              <div className="space-y-3">
                <h3 className="text-sm font-semibold text-muted-foreground">
                  Resolvidas ({resolvedAnomalies.length})
                </h3>
                <div className="space-y-3">
                  {resolvedAnomalies.map(renderAnomaly)}
                </div>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
