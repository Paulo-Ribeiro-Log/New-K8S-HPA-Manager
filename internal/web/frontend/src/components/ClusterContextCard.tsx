import { Server } from "lucide-react";
import { Card } from "@/components/ui/card";
import { useClusterVersion } from "@/hooks/useClusterInfo";

interface ClusterContextCardProps {
  cluster: string | null;
}

export const ClusterContextCard = ({ cluster }: ClusterContextCardProps) => {
  const { data: versionInfo, isLoading } = useClusterVersion(cluster || "", !!cluster);

  return (
    <Card className="p-4 bg-gradient-card hover:shadow-lg transition-all duration-300 hover:-translate-y-1 border-border/50">
      <div className="flex items-start justify-between">
        <div className="flex flex-col gap-1 min-w-0 flex-1 mr-2">
          <p className="text-xs font-medium text-muted-foreground">Contexto</p>
          <div className="flex flex-col gap-0.5">
            {/* Nome do contexto com fonte ajustada para não quebrar */}
            <p className="text-lg font-bold text-primary truncate" title={cluster || "—"}>
              {cluster || "—"}
            </p>
            {/* Versão do Kubernetes com fonte menor mas elegante */}
            {cluster && (
              <p className="text-xs font-medium text-muted-foreground/80">
                {isLoading ? "Carregando..." : versionInfo?.version || "Desconhecida"}
              </p>
            )}
          </div>
        </div>
        <div className="p-2 bg-primary/10 rounded-lg flex-shrink-0">
          <Server className="w-5 h-5 text-primary" />
        </div>
      </div>
    </Card>
  );
};
