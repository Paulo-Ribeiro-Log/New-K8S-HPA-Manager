import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import {
  CheckCircle2,
  AlertCircle,
  XCircle,
  Activity,
  Server,
  Database,
  Settings,
  Loader2,
  Eye,
  Clock,
  ChevronDown,
  ChevronRight,
  ListChecks,
  X,
  Filter,
} from "lucide-react";
import type { HealthCheckResult } from "@/types/healthcheck";
import { HealthCheckCard } from "@/components/HealthCheckCard";

interface HealthCheckResultsPanelProps {
  results: HealthCheckResult[];
  isRunning?: boolean;
  runningClusters?: string[];
  onShowProgress?: (cluster: string, result: HealthCheckResult) => void;
  onAddToWhitelist?: (alerts: SelectedAlert[]) => void;
}

// Tipo para alerta selecionado
export interface SelectedAlert {
  namespace: string;
  name: string;
  resource_type: "Deployment" | "ConfigMap" | "Secret";
  message: string;
}

export const HealthCheckResultsPanel = ({
  results,
  isRunning = false,
  runningClusters = [],
  onShowProgress,
  onAddToWhitelist,
}: HealthCheckResultsPanelProps) => {
  const [expandedCluster, setExpandedCluster] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedAlerts, setSelectedAlerts] = useState<Set<string>>(new Set());

  // Funções de seleção
  const toggleAlert = (alertKey: string) => {
    setSelectedAlerts((prev) => {
      const next = new Set(prev);
      if (next.has(alertKey)) {
        next.delete(alertKey);
      } else {
        next.add(alertKey);
      }
      return next;
    });
  };

  const cancelSelection = () => {
    setSelectionMode(false);
    setSelectedAlerts(new Set());
  };

  const addToWhitelist = () => {
    if (!onAddToWhitelist || selectedAlerts.size === 0) return;

    // Construir array de alertas a partir das chaves selecionadas
    const alerts: SelectedAlert[] = [];

    results.forEach((result) => {
      result.deployment_results.forEach((dep) => {
        const key = `deployment-${result.cluster}-${dep.namespace}-${dep.name}`;
        if (selectedAlerts.has(key)) {
          alerts.push({
            namespace: dep.namespace,
            name: dep.name,
            resource_type: "Deployment",
            message: dep.message,
          });
        }
      });

      result.config_results.forEach((cfg) => {
        const key = `config-${result.cluster}-${cfg.namespace}-${cfg.name}`;
        if (selectedAlerts.has(key)) {
          alerts.push({
            namespace: cfg.namespace,
            name: cfg.name,
            resource_type: cfg.resource_type === "ConfigMap" ? "ConfigMap" : "Secret",
            message: cfg.message,
          });
        }
      });

      result.service_results.forEach((svc) => {
        const key = `service-${result.cluster}-${svc.namespace}-${svc.name}`;
        if (selectedAlerts.has(key)) {
          // Nota: Services não têm mapeamento direto para filtros atuais
          // Por ora, pular services
        }
      });
    });

    onAddToWhitelist(alerts);
    cancelSelection();
  };

  // Agrupar resultados por cluster (pegar o mais recente)
  const clusterResults = results.reduce((acc, result) => {
    const existing = acc[result.cluster];
    if (!existing || new Date(result.finished_at) > new Date(existing.finished_at)) {
      acc[result.cluster] = result;
    }
    return acc;
  }, {} as Record<string, HealthCheckResult>);

  // Clusters finalizados
  const completedClusters = Object.keys(clusterResults);

  // Clusters em execução (que não estão nos resultados ainda)
  const activeClusters = runningClusters.filter(c => !completedClusters.includes(c));

  // Total de clusters sendo monitorados
  const allClusters = [...new Set([...completedClusters, ...activeClusters])];

  // Se não há resultados E não está rodando, mostrar mensagem vazia
  if (!isRunning && results.length === 0) {
    return (
      <div className="p-8 text-center">
        <Activity className="h-12 w-12 mx-auto text-muted-foreground opacity-50 mb-4" />
        <p className="text-sm text-muted-foreground">Nenhum resultado disponível</p>
      </div>
    );
  }

  // Status badge color
  const getStatusBadge = (status: string) => {
    switch (status) {
      case "healthy":
        return (
          <Badge className="bg-green-600">
            <CheckCircle2 className="mr-1 h-3 w-3" />
            Healthy
          </Badge>
        );
      case "warning":
        return (
          <Badge className="bg-yellow-600">
            <AlertCircle className="mr-1 h-3 w-3" />
            Warning
          </Badge>
        );
      case "critical":
        return (
          <Badge className="bg-red-600">
            <XCircle className="mr-1 h-3 w-3" />
            Critical
          </Badge>
        );
      case "running":
        return (
          <Badge className="bg-blue-600">
            <Loader2 className="mr-1 h-3 w-3 animate-spin" />
            Em Progresso
          </Badge>
        );
      default:
        return (
          <Badge variant="outline">
            <Activity className="mr-1 h-3 w-3" />
            Unknown
          </Badge>
        );
    }
  };

  return (
    <div className="p-4 space-y-4">
      {/* Header com botão de progresso */}
      {isRunning && onShowProgress && (
        <div className="flex items-center justify-between bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
          <div className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin text-blue-600" />
            <span className="text-sm font-medium">
              Health Check em Progresso ({activeClusters.length}/{allClusters.length} clusters)
            </span>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={onShowProgress}
            className="gap-2"
          >
            <Eye className="h-4 w-4" />
            Ver Progresso
          </Button>
        </div>
      )}

      {/* Resumo Geral */}
      {allClusters.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="h-5 w-5" />
              Resumo Geral
            </CardTitle>
            <CardDescription>
              {completedClusters.length} de {allClusters.length} cluster(s) analisado(s)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              <div className="text-center p-3 border rounded-lg">
                <div className="text-2xl font-bold">
                  {allClusters.length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Total Clusters</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-green-50 dark:bg-green-950/20">
                <div className="text-2xl font-bold text-green-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "healthy").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Healthy</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-yellow-50 dark:bg-yellow-950/20">
                <div className="text-2xl font-bold text-yellow-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "warning").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Warning</div>
              </div>
              <div className="text-center p-3 border rounded-lg bg-red-50 dark:bg-red-950/20">
                <div className="text-2xl font-bold text-red-600">
                  {completedClusters.filter(c => clusterResults[c].overall_status === "critical").length}
                </div>
                <div className="text-xs text-muted-foreground mt-1">Critical</div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Clusters em Progresso */}
      {activeClusters.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-semibold flex items-center gap-2">
            <Clock className="h-4 w-4" />
            Em Execução
          </h3>
          {activeClusters.map((cluster) => (
            <Card key={cluster} className="border-blue-200 dark:border-blue-800">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Server className="h-4 w-4" />
                    <CardTitle className="text-sm font-mono">{cluster}</CardTitle>
                  </div>
                  {getStatusBadge("running")}
                </div>
                <CardDescription className="text-xs">
                  Análise em andamento... Clique em "Ver Progresso" para acompanhar
                </CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      )}

      {/* Resultados por Cluster (Colapsáveis) */}
      {completedClusters.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4" />
              Clusters Analisados
            </h3>

            {/* Botões de controle de seleção */}
            <div className="flex items-center gap-2">
              {!selectionMode ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSelectionMode(true)}
                  className="gap-2"
                >
                  <ListChecks className="h-4 w-4" />
                  Selecionar Alertas
                </Button>
              ) : (
                <>
                  <Badge variant="secondary" className="gap-1">
                    {selectedAlerts.size} selecionado(s)
                  </Badge>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={cancelSelection}
                    className="gap-2"
                  >
                    <X className="h-4 w-4" />
                    Cancelar
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={addToWhitelist}
                    disabled={selectedAlerts.size === 0}
                    className="gap-2 bg-blue-600 hover:bg-blue-700"
                  >
                    <Filter className="h-4 w-4" />
                    Adicionar à Whitelist
                  </Button>
                </>
              )}
            </div>
          </div>
          <ScrollArea className="h-[600px]">
            <div className="space-y-3">
              {completedClusters.map((cluster) => {
                const result = clusterResults[cluster];
                const isExpanded = expandedCluster === cluster;

                return (
                  <Collapsible
                    key={cluster}
                    open={isExpanded}
                    onOpenChange={() =>
                      setExpandedCluster(isExpanded ? null : cluster)
                    }
                  >
                    <Card>
                      <CollapsibleTrigger className="w-full">
                        <CardHeader className="pb-3 cursor-pointer hover:bg-muted/50 transition-colors">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              {isExpanded ? (
                                <ChevronDown className="h-4 w-4 text-muted-foreground" />
                              ) : (
                                <ChevronRight className="h-4 w-4 text-muted-foreground" />
                              )}
                              <Server className="h-4 w-4" />
                              <CardTitle className="text-sm font-mono">{cluster}</CardTitle>
                            </div>
                            {getStatusBadge(result.overall_status)}
                          </div>
                          <CardDescription className="text-xs text-left ml-6">
                            {result.namespace ? `Namespace: ${result.namespace}` : "Todos os namespaces"} •{" "}
                            Tempo: {(result.duration_ms / 1000).toFixed(2)}s
                          </CardDescription>
                        </CardHeader>
                      </CollapsibleTrigger>

                      {/* Métricas Resumidas com Badges Clicáveis */}
                      <CardContent className="pt-0 pb-3">
                        <div className="flex items-center gap-4 ml-6 flex-wrap">
                          {/* Badge Healthy */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-green-500 hover:bg-green-50 dark:hover:bg-green-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <CheckCircle2 className="h-4 w-4 text-green-600" />
                              <span className="text-sm font-medium text-green-700 dark:text-green-500">
                                Healthy
                              </span>
                              {/* Círculo com contador */}
                              {result.healthy_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-green-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.healthy_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Warning */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-yellow-500 hover:bg-yellow-50 dark:hover:bg-yellow-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <AlertCircle className="h-4 w-4 text-yellow-600" />
                              <span className="text-sm font-medium text-yellow-700 dark:text-yellow-500">
                                Warning
                              </span>
                              {result.warning_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-yellow-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.warning_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Critical */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-red-500 hover:bg-red-50 dark:hover:bg-red-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <XCircle className="h-4 w-4 text-red-600" />
                              <span className="text-sm font-medium text-red-700 dark:text-red-500">
                                Critical
                              </span>
                              {result.critical_count > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-red-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.critical_count}
                                </div>
                              )}
                            </div>
                          </Button>

                          {/* Badge Total */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onShowProgress && onShowProgress(cluster, result)}
                            className="relative h-auto py-2.5 px-4 border-gray-400 hover:bg-gray-50 dark:hover:bg-gray-950/20 cursor-pointer"
                            title="Clique para ver logs detalhados"
                          >
                            <div className="flex items-center gap-2">
                              <Activity className="h-4 w-4" />
                              <span className="text-sm font-medium">Total</span>
                              {result.total_checks > 0 && (
                                <div className="absolute -top-2 -right-2 h-6 w-6 rounded-full bg-gray-600 text-white text-xs flex items-center justify-center font-bold shadow-lg">
                                  {result.total_checks}
                                </div>
                              )}
                            </div>
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground mt-2 ml-6">
                          💡 Clique nos badges para ver logs detalhados da análise
                        </p>
                      </CardContent>

                      {/* Detalhes Expandidos */}
                      <CollapsibleContent>
                        <CardContent className="pt-0 border-t">
                          <Tabs defaultValue="deployments" className="mt-4">
                            <TabsList className="grid w-full grid-cols-3">
                              <TabsTrigger value="deployments" className="gap-1 text-xs">
                                <Server className="h-3 w-3" />
                                Deployments ({result.deployment_results.length})
                              </TabsTrigger>
                              <TabsTrigger value="services" className="gap-1 text-xs">
                                <Database className="h-3 w-3" />
                                Services ({result.service_results.length})
                              </TabsTrigger>
                              <TabsTrigger value="configs" className="gap-1 text-xs">
                                <Settings className="h-3 w-3" />
                                Configs ({result.config_results.length})
                              </TabsTrigger>
                            </TabsList>

                            <TabsContent value="deployments" className="space-y-2 mt-3">
                              {result.deployment_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum deployment verificado
                                </div>
                              ) : (
                                result.deployment_results.map((deployment, i) => {
                                  const alertKey = `deployment-${result.cluster}-${deployment.namespace}-${deployment.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={deployment}
                                      type="deployment"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="services" className="space-y-2 mt-3">
                              {result.service_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum serviço externo testado
                                </div>
                              ) : (
                                result.service_results.map((service, i) => {
                                  const alertKey = `service-${result.cluster}-${service.namespace}-${service.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={service}
                                      type="service"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>

                            <TabsContent value="configs" className="space-y-2 mt-3">
                              {result.config_results.length === 0 ? (
                                <div className="text-center py-4 text-xs text-muted-foreground">
                                  Nenhum ConfigMap/Secret validado
                                </div>
                              ) : (
                                result.config_results.map((config, i) => {
                                  const alertKey = `config-${result.cluster}-${config.namespace}-${config.name}`;
                                  return (
                                    <HealthCheckCard
                                      key={i}
                                      health={config}
                                      type="config"
                                      selectionMode={selectionMode}
                                      isSelected={selectedAlerts.has(alertKey)}
                                      onToggleSelect={() => toggleAlert(alertKey)}
                                    />
                                  );
                                })
                              )}
                            </TabsContent>
                          </Tabs>
                        </CardContent>
                      </CollapsibleContent>
                    </Card>
                  </Collapsible>
                );
              })}
            </div>
          </ScrollArea>
        </div>
      )}
    </div>
  );
};
