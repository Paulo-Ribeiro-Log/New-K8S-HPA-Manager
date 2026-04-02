import { AlertTriangle, RefreshCw, X, Search } from "lucide-react";
import { useState } from "react";
import { Button } from "./ui/button";
import { AutoDiscoverDialog } from "@/components/AutoDiscoverDialog";

interface VPNWarningBannerProps {
  cloudProvider?: string; // "aks" | "eks" | "unknown"
  clusterName?: string;
  onDismiss?: () => void;
  onRetry?: () => Promise<boolean>;
}

function vpnMessage(cloud?: string, cluster?: string) {
  const clusterLabel = cluster ? ` (${cluster})` : "";
  if (cloud === "eks") {
    return {
      title: "Cluster AWS EKS inacessível",
      detail: `Não foi possível conectar ao cluster EKS${clusterLabel}. Verifique se a VPN AWS está ativa.`,
      tips: [
        "Conecte-se à VPN AWS",
        "Se a VPN Azure estiver ativa, pode haver conflito — considere desativá-la",
        'Clique em "Tentar Novamente" após conectar',
      ],
    };
  }
  if (cloud === "aks") {
    return {
      title: "Cluster Azure AKS inacessível",
      detail: `Não foi possível conectar ao cluster AKS${clusterLabel}. Verifique se a VPN Azure está ativa.`,
      tips: [
        "Conecte-se à VPN Azure",
        "Se a VPN AWS estiver ativa, pode haver conflito — considere desativá-la",
        'Clique em "Tentar Novamente" após conectar',
      ],
    };
  }
  return {
    title: "Cluster Kubernetes inacessível",
    detail: `Não foi possível conectar ao cluster${clusterLabel}. Verifique a VPN e tente novamente.`,
    tips: [
      "Verifique se a VPN correta está ativa",
      'Clique em "Tentar Novamente" após conectar',
    ],
  };
}

export function VPNWarningBanner({ cloudProvider, clusterName, onDismiss, onRetry }: VPNWarningBannerProps) {
  const [isRetrying, setIsRetrying] = useState(false);
  const [showAutoDiscover, setShowAutoDiscover] = useState(false);

  const msg = vpnMessage(cloudProvider, clusterName);

  const handleRetry = async () => {
    setIsRetrying(true);
    try {
      const ok = await onRetry?.();
      if (ok) onDismiss?.();
    } finally {
      setIsRetrying(false);
    }
  };

  return (
    <>
      <AutoDiscoverDialog
        open={showAutoDiscover}
        onOpenChange={setShowAutoDiscover}
        onComplete={handleRetry}
      />

      <div className="bg-red-900/20 border-l-4 border-red-500 p-4 mb-4 rounded-r-lg">
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-start gap-3 flex-1">
            <AlertTriangle className="w-6 h-6 text-red-500 flex-shrink-0 mt-0.5" />
            <div className="flex-1">
              <h3 className="text-red-500 font-semibold text-base mb-1">{msg.title}</h3>
              <p className="text-sm text-muted-foreground mb-3">{msg.detail}</p>
              <div className="flex flex-col gap-2 text-sm text-muted-foreground">
                {msg.tips.map((tip, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <span className="text-red-400">•</span>
                    <span>{tip}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 flex-shrink-0">
            <Button
              variant="default"
              size="sm"
              onClick={() => setShowAutoDiscover(true)}
              className="bg-blue-600 hover:bg-blue-700"
            >
              <Search className="w-4 h-4 mr-2" />
              Auto-Descobrir Clusters
            </Button>

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
    </>
  );
}
