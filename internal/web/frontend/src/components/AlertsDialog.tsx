// AlertsDialog - Dialog modal para visualizar alertas sem sair da página
import { useState, useMemo } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useAllAlerts, useHPASpecificAlerts } from "@/hooks/useAlerts";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertTriangle,
  AlertCircle,
  Bell,
  CheckCircle,
  WrenchIcon,
  Info,
  Clock,
  X,
} from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";
import { ScrollArea } from "@/components/ui/scroll-area";

type AlertFilter = "all" | "critical" | "warning" | "info";

interface AlertsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace?: string;
  hpaName?: string;
}

export function AlertsDialog({
  open,
  onOpenChange,
  cluster,
  namespace,
  hpaName,
}: AlertsDialogProps) {
  const [activeFilter, setActiveFilter] = useState<AlertFilter>("all");

  // Se houver namespace e hpaName, buscar alertas específicos do HPA
  const isHPASpecific = !!namespace && !!hpaName;
  const { data: hpaSpecificAlerts, isLoading: loadingSpecific } =
    useHPASpecificAlerts(cluster, namespace || "", hpaName || "", isHPASpecific && open);

  // Caso contrário, buscar todos os alertas do cluster
  const {
    hpaAlerts,
    nodePoolAlerts,
    summary,
    loading: loadingAll,
  } = useAllAlerts(cluster, !isHPASpecific && open);

  const loading = loadingSpecific || loadingAll;
  const alerts = isHPASpecific ? hpaSpecificAlerts || [] : hpaAlerts;

  // Filtrar alertas baseado no filtro ativo
  const filteredAlerts = useMemo(() => {
    if (activeFilter === "all") return alerts;
    return alerts.filter(alert => alert.severity === activeFilter);
  }, [alerts, activeFilter]);

  const filteredNodeAlerts = useMemo(() => {
    if (activeFilter === "all") return nodePoolAlerts || [];
    return (nodePoolAlerts || []).filter(alert => alert.severity === activeFilter);
  }, [nodePoolAlerts, activeFilter]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl max-h-[90vh] p-0">
        <DialogHeader className="px-6 pt-6 pb-4 border-b">
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle className="text-2xl">
                {isHPASpecific ? "Alertas do HPA" : "Alertas do Cluster"}
              </DialogTitle>
              <p className="text-sm text-muted-foreground mt-1">
                {isHPASpecific ? (
                  <>
                    {hpaName} • {namespace} • {cluster}
                  </>
                ) : (
                  <>{cluster}</>
                )}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => onOpenChange(false)}
              className="h-8 w-8"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </DialogHeader>

        <ScrollArea className="max-h-[calc(90vh-120px)]">
          <div className="px-6 pb-6 space-y-6">
            {/* Summary Cards - Clicáveis para filtrar */}
            {!isHPASpecific && !loading && summary && (
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                <Card 
                  className={`cursor-pointer transition-all hover:shadow-md ${
                    activeFilter === "all" ? "ring-2 ring-primary shadow-md" : ""
                  }`}
                  onClick={() => setActiveFilter("all")}
                >
                  <CardContent className="p-4">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-sm text-muted-foreground">Total</p>
                        <p className="text-2xl font-bold">{summary.total}</p>
                      </div>
                      <Bell className="h-8 w-8 text-muted-foreground" />
                    </div>
                  </CardContent>
                </Card>
                <Card 
                  className={`cursor-pointer transition-all hover:shadow-md border-red-300 bg-red-50 dark:bg-red-950/20 ${
                    activeFilter === "critical" ? "ring-2 ring-red-500 shadow-md" : ""
                  }`}
                  onClick={() => setActiveFilter("critical")}
                >
                  <CardContent className="p-4">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-sm text-red-700 dark:text-red-300">Críticos</p>
                        <p className="text-2xl font-bold text-red-900 dark:text-red-100">
                          {summary.critical}
                        </p>
                      </div>
                      <AlertCircle className="h-8 w-8 text-red-600" />
                    </div>
                  </CardContent>
                </Card>
                <Card 
                  className={`cursor-pointer transition-all hover:shadow-md border-yellow-300 bg-yellow-50 dark:bg-yellow-950/20 ${
                    activeFilter === "warning" ? "ring-2 ring-yellow-500 shadow-md" : ""
                  }`}
                  onClick={() => setActiveFilter("warning")}
                >
                  <CardContent className="p-4">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-sm text-yellow-700 dark:text-yellow-300">
                          Avisos
                        </p>
                        <p className="text-2xl font-bold text-yellow-900 dark:text-yellow-100">
                          {summary.warning}
                        </p>
                      </div>
                      <AlertTriangle className="h-8 w-8 text-yellow-600" />
                    </div>
                  </CardContent>
                </Card>
                <Card 
                  className={`cursor-pointer transition-all hover:shadow-md border-blue-300 bg-blue-50 dark:bg-blue-950/20 ${
                    activeFilter === "info" ? "ring-2 ring-blue-500 shadow-md" : ""
                  }`}
                  onClick={() => setActiveFilter("info")}
                >
                  <CardContent className="p-4">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-sm text-blue-700 dark:text-blue-300">Info</p>
                        <p className="text-2xl font-bold text-blue-900 dark:text-blue-100">
                          {summary.info}
                        </p>
                      </div>
                      <Info className="h-8 w-8 text-blue-600" />
                    </div>
                  </CardContent>
                </Card>
              </div>
            )}

            {/* Loading State */}
            {loading && (
              <div className="space-y-4">
                <Skeleton className="h-32 w-full" />
                <Skeleton className="h-32 w-full" />
                <Skeleton className="h-32 w-full" />
              </div>
            )}

            {/* HPA Alerts List */}
            {!loading && filteredAlerts && filteredAlerts.length > 0 && (
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <AlertTriangle className="h-5 w-5" />
                    Alertas de HPA ({filteredAlerts.length})
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  {filteredAlerts.map((alert, idx) => (
                    <AlertCard key={idx} alert={alert} type="hpa" />
                  ))}
                </CardContent>
              </Card>
            )}

            {/* Node Pool Alerts List */}
            {!loading && !isHPASpecific && filteredNodeAlerts && filteredNodeAlerts.length > 0 && (
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <AlertTriangle className="h-5 w-5" />
                    Alertas de Node Pool ({filteredNodeAlerts.length})
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  {filteredNodeAlerts.map((alert, idx) => (
                    <AlertCard key={idx} alert={alert} type="nodepool" />
                  ))}
                </CardContent>
              </Card>
            )}

            {/* No Alerts */}
            {!loading && filteredAlerts.length === 0 && (!filteredNodeAlerts || filteredNodeAlerts.length === 0) && (
              <Card>
                <CardContent className="p-12 text-center">
                  <CheckCircle className="h-16 w-16 text-green-500 mx-auto mb-4" />
                  <h3 className="text-xl font-semibold mb-2">Nenhum Alerta Ativo</h3>
                  <p className="text-muted-foreground">
                    Todos os sistemas estão funcionando normalmente.
                  </p>
                </CardContent>
              </Card>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

// Componente de Card individual para cada alerta
interface AlertCardProps {
  alert: any;
  type: "hpa" | "nodepool";
}

function AlertCard({ alert, type }: AlertCardProps) {
  const severityConfig = {
    critical: {
      icon: AlertCircle,
      color: "text-red-600",
      bgColor: "bg-red-50 dark:bg-red-950/20",
      borderColor: "border-red-300",
      badgeVariant: "destructive" as const,
    },
    warning: {
      icon: AlertTriangle,
      color: "text-yellow-600",
      bgColor: "bg-yellow-50 dark:bg-yellow-950/20",
      borderColor: "border-yellow-300",
      badgeVariant: "secondary" as const,
    },
    info: {
      icon: Bell,
      color: "text-blue-600",
      bgColor: "bg-blue-50 dark:bg-blue-950/20",
      borderColor: "border-blue-300",
      badgeVariant: "secondary" as const,
    },
  };

  const config = severityConfig[alert.severity as keyof typeof severityConfig] || severityConfig.info;
  const Icon = config.icon;

  const formattedDate = alert.activeAt
    ? format(new Date(alert.activeAt), "dd/MM/yyyy 'às' HH:mm", { locale: ptBR })
    : "N/A";

  return (
    <div
      className={`p-4 rounded-lg border-2 ${config.borderColor} ${config.bgColor}`}
    >
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-start gap-3 flex-1">
          <Icon className={`h-6 w-6 ${config.color} mt-0.5`} />
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1">
              <h4 className="font-semibold text-lg">{alert.alertName}</h4>
              <Badge variant={config.badgeVariant}>{alert.severity}</Badge>
              {alert.mitigation?.autoMitigable && (
                <Badge variant="outline" className="gap-1">
                  <WrenchIcon className="h-3 w-3" />
                  Auto-Mitigável
                </Badge>
              )}
            </div>
            <p className="text-sm text-muted-foreground mb-2">{alert.description}</p>
            <div className="flex items-center gap-4 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                Ativo desde: {formattedDate}
              </span>
              {type === "hpa" && alert.namespace && (
                <span>Namespace: {alert.namespace}</span>
              )}
              {type === "hpa" && alert.deployment && (
                <span>Deployment: {alert.deployment}</span>
              )}
              {type === "nodepool" && alert.node && (
                <span>Node: {alert.node}</span>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Mitigation Section */}
      {alert.mitigation && (
        <>
          <Separator className="my-3" />
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <WrenchIcon className="h-4 w-4 text-muted-foreground" />
              <span className="font-medium text-sm">Ação Sugerida:</span>
              <span className="text-sm">{alert.mitigation.action}</span>
              <Badge variant="outline" className="text-xs">
                Prioridade: {alert.mitigation.priority}
              </Badge>
            </div>
            {alert.mitigation.steps && alert.mitigation.steps.length > 0 && (
              <div className="ml-6 space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Passos:</p>
                <ol className="list-decimal list-inside text-sm space-y-1">
                  {alert.mitigation.steps.map((step: string, idx: number) => (
                    <li key={idx} className="text-muted-foreground">
                      {step}
                    </li>
                  ))}
                </ol>
              </div>
            )}
            {alert.mitigation.autoMitigable && (
              <Button size="sm" className="mt-2 gap-1">
                <WrenchIcon className="h-4 w-4" />
                Aplicar Mitigação Automática
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  );
}
