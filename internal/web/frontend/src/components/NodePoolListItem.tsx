import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Server, HardDrive, TrendingUp, TrendingDown, Loader2, CheckCircle2 } from "lucide-react";
import type { NodePool } from "@/lib/api/types";

interface NodePoolListItemProps {
  nodePool: NodePool;
  isSelected: boolean;
  isApplying?: boolean;
  applyResult?: "success" | "error" | null;
  onClick: () => void;
}

export const NodePoolListItem = ({
  nodePool,
  isSelected,
  isApplying = false,
  applyResult = null,
  onClick,
}: NodePoolListItemProps) => {
  return (
    <Card
      className={`relative p-4 cursor-pointer transition-all hover:shadow-md ${
        isSelected ? "border-primary bg-accent" : "border-border"
      } ${isApplying ? "opacity-80" : ""}`}
      onClick={onClick}
    >
      {/* Overlay de progresso */}
      {isApplying && (
        <div className="absolute inset-0 rounded-lg bg-background/60 backdrop-blur-[1px] flex items-center justify-center z-10">
          <div className="flex items-center gap-2 bg-background border border-border rounded-md px-3 py-1.5 shadow-sm">
            <Loader2 className="w-4 h-4 animate-spin text-primary" />
            <span className="text-xs font-medium text-foreground">Aplicando...</span>
          </div>
        </div>
      )}

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
          <div className="flex gap-1 items-center">
            {applyResult === "success" && (
              <CheckCircle2 className="w-4 h-4 text-green-500" />
            )}
            {applyResult === "error" && (
              <Badge variant="destructive" className="text-xs">Erro</Badge>
            )}
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
