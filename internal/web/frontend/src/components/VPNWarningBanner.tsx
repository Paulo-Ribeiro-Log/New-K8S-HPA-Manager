import { AlertTriangle, RefreshCw, X } from "lucide-react";
import { useState, useEffect } from "react";
import { Button } from "./ui/button";
import { apiClient } from "@/lib/api/client";

interface VPNWarningBannerProps {
  onDismiss?: () => void;
}

export function VPNWarningBanner({ onDismiss }: VPNWarningBannerProps) {
  const [isRetrying, setIsRetrying] = useState(false);

  const handleRetry = async () => {
    setIsRetrying(true);
    try {
      // Tentar buscar clusters para validar conexão
      await apiClient.getClusters();
      // Se conseguir, dismissar o banner
      if (onDismiss) {
        onDismiss();
      }
    } catch (err) {
      console.error("VPN ainda desconectada:", err);
    } finally {
      setIsRetrying(false);
    }
  };

  return (
    <div className="bg-red-900/20 border-l-4 border-red-500 p-4 mb-4 rounded-r-lg">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-3 flex-1">
          <AlertTriangle className="w-6 h-6 text-red-500 flex-shrink-0 mt-0.5" />
          <div className="flex-1">
            <h3 className="text-red-500 font-semibold text-base mb-1">
              VPN Desconectada - Kubernetes Inacessível
            </h3>
            <p className="text-sm text-muted-foreground mb-3">
              Não foi possível conectar aos clusters Kubernetes. Verifique se você está conectado à VPN corporativa.
            </p>
            <div className="flex flex-col gap-2 text-sm text-muted-foreground">
              <div className="flex items-center gap-2">
                <span className="text-red-400">•</span>
                <span>Conecte-se à VPN e clique em "Tentar Novamente"</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-red-400">•</span>
                <span>
                  Após conectar, execute:{" "}
                  <code className="bg-muted px-1 py-0.5 rounded text-xs">
                    new-k8s-hpa autodiscover
                  </code>
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-red-400">•</span>
                <span>Verifique se o kubectl está configurado corretamente</span>
              </div>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2 flex-shrink-0">
          <Button
            variant="outline"
            size="sm"
            onClick={handleRetry}
            disabled={isRetrying}
            className="border-red-500/30 hover:bg-red-500/10"
          >
            {isRetrying ? (
              <>
                <RefreshCw className="w-4 h-4 mr-2 animate-spin" />
                Verificando...
              </>
            ) : (
              <>
                <RefreshCw className="w-4 h-4 mr-2" />
                Tentar Novamente
              </>
            )}
          </Button>

          {onDismiss && (
            <Button
              variant="ghost"
              size="icon"
              onClick={onDismiss}
              className="hover:bg-red-500/10"
            >
              <X className="w-4 h-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
