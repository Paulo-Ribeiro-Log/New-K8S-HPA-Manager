import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Server, Cpu, HardDrive, TrendingUp, TrendingDown } from "lucide-react";
import type { NodePool } from "@/lib/api/types";

interface NodePoolListItemProps {
  nodePool: NodePool;
  isSelected: boolean;
  onClick: () => void;
}

export const NodePoolListItem = ({ nodePool, isSelected, onClick }: NodePoolListItemProps) => {
  return (
    <Card
      className={`p-4 cursor-pointer transition-all hover:shadow-md ${
        isSelected ? "border-primary bg-accent" : "border-border"
      }`}
      onClick={onClick}
    >
      <div className="space-y-3">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-2">
            <Server className={`w-5 h-5 ${isSelected ? "text-primary" : "text-muted-foreground"}`} />
            <div>
              <h3 className="font-semibold">{nodePool.name}</h3>
              <p className="text-xs text-muted-foreground">{nodePool.vm_size}</p>
            </div>
          </div>
          <div className="flex gap-1">
            <Badge variant={nodePool.is_system_pool ? "default" : "secondary"} className="text-xs">
              {nodePool.is_system_pool ? "System" : "User"}
            </Badge>
            {nodePool.status === "Succeeded" ? (
              <Badge variant="outline" className="text-xs">
                Active
              </Badge>
            ) : (
              <Badge variant="destructive" className="text-xs">
                {nodePool.status}
              </Badge>
            )}
          </div>
        </div>

        {/* Scaling Info */}
        <div className="flex items-center gap-4 text-sm">
          <div className="flex items-center gap-1.5">
            {nodePool.autoscaling_enabled ? (
              <>
                <TrendingUp className="w-4 h-4 text-green-500" />
                <span className="text-muted-foreground">Auto:</span>
                <span className="font-medium">
                  {nodePool.min_node_count}-{nodePool.max_node_count}
                </span>
              </>
            ) : (
              <>
                <TrendingDown className="w-4 h-4 text-blue-500" />
                <span className="text-muted-foreground">Manual:</span>
                <span className="font-medium">{nodePool.node_count}</span>
              </>
            )}
          </div>

          <div className="flex items-center gap-1.5">
            <HardDrive className="w-4 h-4 text-muted-foreground" />
            <span className="text-muted-foreground">Current:</span>
            <span className="font-medium">{nodePool.node_count}</span>
          </div>
        </div>

        {/* Resource Group */}
        <div className="text-xs text-muted-foreground border-t pt-2">
          {nodePool.resource_group}
        </div>
      </div>
    </Card>
  );
};
