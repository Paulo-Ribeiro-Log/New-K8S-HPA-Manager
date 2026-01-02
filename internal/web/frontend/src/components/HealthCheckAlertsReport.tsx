import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { AlertCircle, XCircle, Download, Server, CheckCircle2 } from "lucide-react";
import type { HealthCheckProgress } from "@/types/healthcheck";

interface HealthCheckAlertsReportProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventsPerCluster: Record<string, HealthCheckProgress[]>;
  clusters: string[];
}

export const HealthCheckAlertsReport = ({
  open,
  onOpenChange,
  eventsPerCluster,
  clusters,
}: HealthCheckAlertsReportProps) => {
  // Extrair apenas warnings e criticals de cada cluster
  const getAlertsForCluster = (cluster: string) => {
    const events = eventsPerCluster[cluster] || [];
    return events.filter(
      (event) => event.status === "warning" || event.status === "critical"
    );
  };

  // Agrupar alertas por cluster
  const clusterAlerts = clusters.map((cluster) => ({
    cluster,
    alerts: getAlertsForCluster(cluster),
  }));

  // Contar totais
  const totalWarnings = clusterAlerts.reduce(
    (sum, ca) => sum + ca.alerts.filter((a) => a.status === "warning").length,
    0
  );
  const totalCriticals = clusterAlerts.reduce(
    (sum, ca) => sum + ca.alerts.filter((a) => a.status === "critical").length,
    0
  );

  // Download relatório como texto
  const handleDownload = () => {
    const lines: string[] = [];
    lines.push("=".repeat(80));
    lines.push("RELATÓRIO DE ALERTAS - HEALTH CHECK");
    lines.push("=".repeat(80));
    lines.push("");
    lines.push(`Data: ${new Date().toLocaleString("pt-BR")}`);
    lines.push(`Total de Clusters: ${clusters.length}`);
    lines.push(`Total de Warnings: ${totalWarnings}`);
    lines.push(`Total de Criticals: ${totalCriticals}`);
    lines.push("");
    lines.push("=".repeat(80));
    lines.push("");

    clusterAlerts.forEach(({ cluster, alerts }) => {
      if (alerts.length === 0) return;

      lines.push(`CLUSTER: ${cluster}`);
      lines.push("-".repeat(80));
      lines.push(
        `Warnings: ${alerts.filter((a) => a.status === "warning").length} | ` +
        `Criticals: ${alerts.filter((a) => a.status === "critical").length}`
      );
      lines.push("");

      alerts.forEach((alert, idx) => {
        const icon = alert.status === "critical" ? "❌" : "⚠️";
        lines.push(`${idx + 1}. ${icon} [${alert.status.toUpperCase()}]`);
        lines.push(`   ${alert.message}`);
        lines.push("");
      });

      lines.push("");
    });

    const content = lines.join("\n");
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `health-check-alerts-${Date.now()}.txt`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh]">
        <DialogHeader>
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle>Relatório de Alertas</DialogTitle>
              <DialogDescription>
                Warnings e Criticals detectados durante o Health Check
              </DialogDescription>
            </div>
            <Button
              onClick={handleDownload}
              variant="outline"
              size="sm"
              className="gap-2"
            >
              <Download className="h-4 w-4" />
              Baixar Relatório
            </Button>
          </div>
        </DialogHeader>

        {/* Resumo */}
        <div className="flex gap-4 pb-4 border-b">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">
              {clusters.length} clusters
            </span>
          </div>
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-yellow-500" />
            <span className="text-sm font-medium">{totalWarnings} warnings</span>
          </div>
          <div className="flex items-center gap-2">
            <XCircle className="h-4 w-4 text-red-500" />
            <span className="text-sm font-medium">{totalCriticals} criticals</span>
          </div>
        </div>

        {/* Lista de alertas por cluster */}
        <ScrollArea className="h-[600px] pr-4">
          {clusterAlerts.map(({ cluster, alerts }) => {
            if (alerts.length === 0) return null;

            const warnings = alerts.filter((a) => a.status === "warning");
            const criticals = alerts.filter((a) => a.status === "critical");

            return (
              <div key={cluster} className="mb-6 pb-6 border-b last:border-0">
                {/* Header do cluster */}
                <div className="flex items-center gap-3 mb-3">
                  <Server className="h-5 w-5 text-blue-500" />
                  <h3 className="font-semibold text-lg">{cluster}</h3>
                  <div className="flex gap-2 ml-auto">
                    {warnings.length > 0 && (
                      <Badge variant="outline" className="bg-yellow-50 text-yellow-700 border-yellow-300">
                        {warnings.length} warnings
                      </Badge>
                    )}
                    {criticals.length > 0 && (
                      <Badge variant="outline" className="bg-red-50 text-red-700 border-red-300">
                        {criticals.length} criticals
                      </Badge>
                    )}
                  </div>
                </div>

                {/* Alertas */}
                <div className="space-y-3 pl-8">
                  {alerts.map((alert, idx) => (
                    <div
                      key={idx}
                      className={`flex items-start gap-3 p-3 rounded-lg border ${
                        alert.status === "critical"
                          ? "bg-red-50 border-red-200"
                          : "bg-yellow-50 border-yellow-200"
                      }`}
                    >
                      {alert.status === "critical" ? (
                        <XCircle className="h-5 w-5 text-red-500 flex-shrink-0 mt-0.5" />
                      ) : (
                        <AlertCircle className="h-5 w-5 text-yellow-600 flex-shrink-0 mt-0.5" />
                      )}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <Badge
                            variant={alert.status === "critical" ? "destructive" : "default"}
                            className={
                              alert.status === "warning"
                                ? "bg-yellow-600 hover:bg-yellow-700"
                                : ""
                            }
                          >
                            {alert.status.toUpperCase()}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {alert.type}
                          </span>
                        </div>
                        <p className="text-sm break-words whitespace-normal">
                          {alert.message}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}

          {/* Mensagem quando não há alertas */}
          {clusterAlerts.every((ca) => ca.alerts.length === 0) && (
            <div className="flex flex-col items-center justify-center h-[400px] text-center">
              <CheckCircle2 className="h-16 w-16 text-green-500 mb-4" />
              <h3 className="text-lg font-semibold mb-2">
                Nenhum alerta encontrado!
              </h3>
              <p className="text-sm text-muted-foreground max-w-md">
                Todos os clusters estão saudáveis. Não foram detectados warnings
                ou criticals durante o health check.
              </p>
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
};
