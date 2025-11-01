import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

interface PrometheusListItemProps {
  name: string;
  namespace: string;
  type: string;
  component: string;
  replicas: number;
  cpuRequest?: string;
  memoryRequest?: string;
  isSelected: boolean;
  onClick: () => void;
}

export const PrometheusListItem = ({
  name,
  namespace,
  type,
  component,
  replicas,
  cpuRequest,
  memoryRequest,
  isSelected,
  onClick,
}: PrometheusListItemProps) => {
  const getTypeIcon = (type: string) => {
    switch (type.toLowerCase()) {
      case 'deployment':
        return '🚀';
      case 'statefulset':
        return '💽';
      case 'daemonset':
        return '🔄';
      default:
        return '📦';
    }
  };

  const getComponentBadge = (component: string) => {
    const variants: Record<string, string> = {
      'Prometheus': 'bg-red-100 text-red-800',
      'Prometheus Server': 'bg-red-100 text-red-800',
      'Grafana': 'bg-orange-100 text-orange-800',
      'Alertmanager': 'bg-yellow-100 text-yellow-800',
      'Node Exporter': 'bg-green-100 text-green-800',
      'Kube State Metrics': 'bg-blue-100 text-blue-800',
    };

    const className = variants[component] || 'bg-gray-100 text-gray-800';

    return (
      <Badge className={`${className} text-xs`}>
        {component}
      </Badge>
    );
  };

  return (
    <Card
      className={`
        p-3 mb-2 cursor-pointer transition-all duration-200
        hover:shadow-md hover:-translate-y-0.5
        ${
          isSelected
            ? "border-2 border-primary bg-accent shadow-md"
            : "border border-border/50 hover:border-primary/50"
        }
      `}
      onClick={onClick}
    >
      <div className="flex items-start justify-between mb-1">
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <span className="text-base">{getTypeIcon(type)}</span>
            <h4 className="font-semibold text-sm text-foreground">{name}</h4>
          </div>
          <p className="text-xs text-muted-foreground">{namespace}</p>
        </div>
        {getComponentBadge(component)}
      </div>
      <div className="flex items-center gap-2 mt-1 mb-1">
        <Badge variant="outline" className="text-xs">{type}</Badge>
      </div>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        {type !== 'DaemonSet' && (
          <span>Replicas: <span className="font-medium text-foreground">{replicas}</span></span>
        )}
        {cpuRequest && (
          <span>CPU: <span className="font-mono font-medium text-foreground">{cpuRequest}</span></span>
        )}
        {memoryRequest && (
          <span>Mem: <span className="font-mono font-medium text-foreground">{memoryRequest}</span></span>
        )}
      </div>
    </Card>
  );
};
