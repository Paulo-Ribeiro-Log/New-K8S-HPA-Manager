import { Badge } from "@/components/ui/badge";
import { Server } from "lucide-react";
import { useClusterVersion } from "@/hooks/useClusterInfo";

interface ClusterInfoBadgeProps {
  cluster: string;
  showVersion?: boolean;
  className?: string;
}

export function ClusterInfoBadge({
  cluster,
  showVersion = true,
  className = "",
}: ClusterInfoBadgeProps) {
  const { data: versionInfo, isLoading } = useClusterVersion(cluster, showVersion);

  if (!showVersion) {
    // Modo simples: apenas nome do cluster
    return (
      <Badge variant="outline" className={className}>
        <Server className="h-3 w-3 mr-1" />
        <span className="font-medium">{cluster}</span>
      </Badge>
    );
  }

  // Modo com versão do Kubernetes
  return (
    <Badge variant="outline" className={className}>
      <Server className="h-3 w-3 mr-1" />
      <span className="font-medium">{cluster}</span>
      {isLoading && <span className="ml-1 text-muted-foreground">...</span>}
      {versionInfo && (
        <span className="ml-1 text-muted-foreground">
          • K8s {versionInfo.version}
        </span>
      )}
    </Badge>
  );
}
