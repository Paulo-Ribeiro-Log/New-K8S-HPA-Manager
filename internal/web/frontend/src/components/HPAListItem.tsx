import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { HealthBadge } from "@/components/HealthBadge";
import { BarChart3 } from "lucide-react";

interface HPAListItemProps {
  name: string;
  namespace: string;
  cluster: string;
  currentReplicas: number;
  minReplicas: number;
  maxReplicas: number;
  isSelected: boolean;
  isModified?: boolean;
  onClick: () => void;
  onMonitor?: () => void;
}

export const HPAListItem = ({
  name,
  namespace,
  cluster,
  currentReplicas,
  minReplicas,
  maxReplicas,
  isSelected,
  isModified,
  onClick,
  onMonitor,
}: HPAListItemProps) => {
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
      <div className="flex items-start justify-between mb-2">
        <div className="flex-1">
          <h4 className="font-semibold text-sm text-foreground">{name}</h4>
          <p className="text-xs text-muted-foreground">{namespace}</p>
        </div>
        <div className="flex items-center gap-2">
          {isModified && (
            <Badge variant="secondary" className="bg-warning/20 text-warning border-warning/30 text-xs py-0">
              Modified
            </Badge>
          )}
          {onMonitor && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={(e) => {
                e.stopPropagation();
                onMonitor();
              }}
              title="Monitorar este HPA"
            >
              <BarChart3 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span>Current: <span className="font-medium text-foreground">{currentReplicas}</span></span>
          <span>Min: <span className="font-medium text-foreground">{minReplicas}</span></span>
          <span>Max: <span className="font-medium text-foreground">{maxReplicas}</span></span>
        </div>
        <HealthBadge
          cluster={cluster}
          namespace={namespace}
          hpaName={name}
          showIcon={true}
          className="text-xs"
        />
      </div>
    </Card>
  );
};
