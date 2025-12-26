import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
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
} from "lucide-react";
import type { HealthCheckResult } from "@/types/healthcheck";
import { HealthCheckCard } from "@/components/HealthCheckCard";

interface HealthCheckResultsPanelProps {
  results: HealthCheckResult[];
}

export const HealthCheckResultsPanel = ({ results }: HealthCheckResultsPanelProps) => {
  const [activeTab, setActiveTab] = useState("deployments");

  // Use the latest result (most recent)
  const latestResult = results[results.length - 1];

  if (!latestResult) {
    return (
      <div className="p-8 text-center">
        <Activity className="h-12 w-12 mx-auto text-muted-foreground opacity-50 mb-4" />
        <p className="text-sm text-muted-foreground">Nenhum resultado disponível</p>
      </div>
    );
  }

  const {
    cluster,
    namespace,
    deployment_results,
    service_results,
    config_results,
    healthy_count,
    warning_count,
    critical_count,
    total_checks,
    overall_status,
    duration_ms,
  } = latestResult;

  // Status badge color
  const getStatusBadge = () => {
    switch (overall_status) {
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
      {/* Summary Header */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="text-lg">Health Check Summary</CardTitle>
              <CardDescription>
                Cluster: <span className="font-mono">{cluster}</span>
                {namespace && (
                  <>
                    {" • "}
                    Namespace: <span className="font-mono">{namespace}</span>
                  </>
                )}
              </CardDescription>
            </div>
            {getStatusBadge()}
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-4 gap-4">
            <div className="text-center p-4 border rounded-lg bg-green-50 dark:bg-green-950/20">
              <div className="text-3xl font-bold text-green-600">
                {healthy_count}
              </div>
              <div className="text-sm text-muted-foreground mt-1">Healthy</div>
            </div>
            <div className="text-center p-4 border rounded-lg bg-yellow-50 dark:bg-yellow-950/20">
              <div className="text-3xl font-bold text-yellow-600">
                {warning_count}
              </div>
              <div className="text-sm text-muted-foreground mt-1">Warnings</div>
            </div>
            <div className="text-center p-4 border rounded-lg bg-red-50 dark:bg-red-950/20">
              <div className="text-3xl font-bold text-red-600">
                {critical_count}
              </div>
              <div className="text-sm text-muted-foreground mt-1">Critical</div>
            </div>
            <div className="text-center p-4 border rounded-lg">
              <div className="text-3xl font-bold">
                {total_checks}
              </div>
              <div className="text-sm text-muted-foreground mt-1">Total Checks</div>
            </div>
          </div>

          {/* Duration */}
          <div className="mt-4 text-sm text-muted-foreground text-center">
            Tempo de execução: <span className="font-medium">{(duration_ms / 1000).toFixed(2)}s</span>
          </div>
        </CardContent>
      </Card>

      {/* Detailed Results Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="deployments" className="gap-2">
            <Server className="h-4 w-4" />
            Deployments ({deployment_results.length})
          </TabsTrigger>
          <TabsTrigger value="services" className="gap-2">
            <Database className="h-4 w-4" />
            Services ({service_results.length})
          </TabsTrigger>
          <TabsTrigger value="configs" className="gap-2">
            <Settings className="h-4 w-4" />
            Configs ({config_results.length})
          </TabsTrigger>
        </TabsList>

        <TabsContent value="deployments" className="space-y-2 mt-4">
          {deployment_results.length === 0 ? (
            <div className="text-center py-8 text-sm text-muted-foreground">
              Nenhum deployment verificado
            </div>
          ) : (
            deployment_results.map((deployment, i) => (
              <HealthCheckCard
                key={i}
                health={deployment}
                type="deployment"
              />
            ))
          )}
        </TabsContent>

        <TabsContent value="services" className="space-y-2 mt-4">
          {service_results.length === 0 ? (
            <div className="text-center py-8 text-sm text-muted-foreground">
              Nenhum serviço externo testado
            </div>
          ) : (
            service_results.map((service, i) => (
              <HealthCheckCard
                key={i}
                health={service}
                type="service"
              />
            ))
          )}
        </TabsContent>

        <TabsContent value="configs" className="space-y-2 mt-4">
          {config_results.length === 0 ? (
            <div className="text-center py-8 text-sm text-muted-foreground">
              Nenhum ConfigMap/Secret validado
            </div>
          ) : (
            config_results.map((config, i) => (
              <HealthCheckCard
                key={i}
                health={config}
                type="config"
              />
            ))
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
};
