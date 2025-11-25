// CriticalAlertsBanner - Barra de alerta sempre visível quando há alertas críticos
// Aparece no topo da aplicação com animação pulsante

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { AlertTriangle, X, ChevronRight } from "lucide-react";
import { useAlertSummary, useAllAlerts } from "@/hooks/useAlerts";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { cn } from "@/lib/utils";

interface CriticalAlertsBannerProps {
  cluster: string;
}

export function CriticalAlertsBanner({ cluster }: CriticalAlertsBannerProps) {
  const [dismissed, setDismissed] = useState(false);
  const navigate = useNavigate();
  const { summary, hpaAlerts, nodePoolAlerts, loading } = useAllAlerts(cluster);

  // Não mostrar se não há alertas críticos ou foi dispensado
  if (loading || !summary || summary.critical === 0 || dismissed) {
    return null;
  }

  const criticalHPAAlerts = hpaAlerts.filter((a) => a.severity === "critical");
  const criticalNodeAlerts = nodePoolAlerts.filter((a) => a.severity === "critical");

  const handleViewDetails = () => {
    // Navegar para painel de alertas
    navigate(`/alerts/${cluster}`);
  };

  const handleDismiss = () => {
    setDismissed(true);
    // O banner volta a aparecer quando o componente for remontado ou quando
    // o número de alertas críticos mudar
  };

  return (
    <div className="w-full animate-in slide-in-from-top-5 duration-500">
      <Alert
        className={cn(
          "border-red-500 bg-red-50 dark:bg-red-950/20 mb-4 relative",
          "animate-pulse-slow" // Animação pulsante sutil
        )}
      >
        <AlertTriangle className="h-5 w-5 text-red-600 dark:text-red-400" />
        <AlertDescription className="flex items-center justify-between">
          <div className="flex flex-col gap-1">
            <div className="font-semibold text-red-900 dark:text-red-100">
              🚨 {summary.critical} Alerta{summary.critical > 1 ? "s" : ""} Crítico
              {summary.critical > 1 ? "s" : ""} Ativo{summary.critical > 1 ? "s" : ""}!
            </div>
            <div className="text-sm text-red-800 dark:text-red-200">
              {criticalHPAAlerts.length > 0 && (
                <span>
                  {criticalHPAAlerts.length} HPA{criticalHPAAlerts.length > 1 ? "s" : ""}
                </span>
              )}
              {criticalHPAAlerts.length > 0 && criticalNodeAlerts.length > 0 && (
                <span className="mx-2">•</span>
              )}
              {criticalNodeAlerts.length > 0 && (
                <span>
                  {criticalNodeAlerts.length} Node Pool{criticalNodeAlerts.length > 1 ? "s" : ""}
                </span>
              )}
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="destructive"
              size="sm"
              onClick={handleViewDetails}
              className="gap-1"
            >
              Ver Detalhes
              <ChevronRight className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 text-red-600 hover:text-red-700 hover:bg-red-100"
              onClick={handleDismiss}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </AlertDescription>
      </Alert>
    </div>
  );
}

// Componente de notificação flutuante (toast) para alertas críticos novos
interface CriticalAlertToastProps {
  alerts: Array<{
    alertName: string;
    severity: string;
    description: string;
  }>;
  onDismiss: () => void;
}

export function CriticalAlertToast({
  alerts,
  onDismiss,
}: CriticalAlertToastProps) {
  const criticalAlerts = alerts.filter((a) => a.severity === "critical");

  if (criticalAlerts.length === 0) {
    return null;
  }

  return (
    <div className="fixed top-20 right-4 z-50 max-w-md animate-in slide-in-from-right-5 duration-300">
      <Alert className="border-red-500 bg-red-50 dark:bg-red-950 shadow-lg">
        <AlertTriangle className="h-5 w-5 text-red-600 animate-pulse" />
        <AlertDescription>
          <div className="flex items-start justify-between gap-2">
            <div className="flex-1">
              <div className="font-semibold text-red-900 dark:text-red-100 mb-2">
                🚨 Novo Alerta Crítico!
              </div>
              <div className="text-sm text-red-800 dark:text-red-200 space-y-1">
                {criticalAlerts.slice(0, 3).map((alert, idx) => (
                  <div key={idx} className="flex items-start gap-1">
                    <span className="font-medium">{alert.alertName}:</span>
                    <span className="text-xs">{alert.description}</span>
                  </div>
                ))}
                {criticalAlerts.length > 3 && (
                  <div className="text-xs opacity-75">
                    +{criticalAlerts.length - 3} mais...
                  </div>
                )}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-red-600 hover:text-red-700"
              onClick={onDismiss}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </AlertDescription>
      </Alert>
    </div>
  );
}
