import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Server, HardDrive, TrendingUp, TrendingDown, Loader2, CheckCircle2, AlertTriangle, RotateCw, XCircle } from "lucide-react";
import type { NodePool } from "@/lib/api/types";

interface NodePoolListItemProps {
  nodePool: NodePool;
  isSelected: boolean;
  isApplying?: boolean;
  applyResult?: "success" | "error" | null;
  applyError?: string;
  onClick: () => void;
  onProgressClick?: () => void;
  onReconcile?: () => void;
  onAbort?: () => void;
}

export const NodePoolListItem = ({
  nodePool,
  isSelected,
  isApplying = false,
  applyResult = null,
  applyError,
  onClick,
  onProgressClick,
  onReconcile,
  onAbort,
}: NodePoolListItemProps) => {
  const handleClick = () => {
    if (isApplying && onProgressClick) {
      onProgressClick();
    } else {
      onClick();
    }
  };

  return (
    <Card
      className={`relative p-4 cursor-pointer transition-all hover:shadow-md ${
        isSelected ? "border-primary bg-accent" : "border-border"
      } ${isApplying ? "border-blue-400 dark:border-blue-500 opacity-90" : ""}`}
      onClick={handleClick}
    >
      {/* Overlay de progresso — clicável para reabrir modal */}
      {isApplying && (
        <div className="absolute inset-0 rounded-lg bg-background/50 backdrop-blur-[1px] flex flex-col items-center justify-center gap-2 z-10">
          <div className="flex items-center gap-2 bg-background border border-blue-400 dark:border-blue-500 rounded-md px-3 py-1.5 shadow-sm">
            <Loader2 className="w-4 h-4 animate-spin text-blue-500" />
            <span className="text-xs font-medium text-foreground">Aplicando...</span>
            {onProgressClick && (
              <span className="text-xs text-blue-500 underline ml-1">ver detalhes</span>
            )}
          </div>
          {onAbort && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs border-red-400 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/30 bg-background shadow-sm"
              onClick={(e) => { e.stopPropagation(); onAbort(); }}
            >
              <XCircle className="w-3 h-3 mr-1.5" />
              Abortar
            </Button>
          )}
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

        {/* Mensagem de erro + botão Reconcile */}
        {applyResult === "error" && (
          <div className="space-y-1.5 mt-1">
            {applyError && (
              <div className="flex items-start gap-1.5 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-800 rounded p-2">
                <AlertTriangle className="w-3 h-3 mt-0.5 flex-shrink-0" />
                <span className="break-all">{applyError}</span>
              </div>
            )}
            {onReconcile && (
              <Button
                size="sm"
                variant="outline"
                className="w-full h-7 text-xs border-amber-400 text-amber-600 dark:text-amber-400 hover:bg-amber-50 dark:hover:bg-amber-950/30"
                onClick={(e) => { e.stopPropagation(); onReconcile(); }}
              >
                <RotateCw className="w-3 h-3 mr-1.5" />
                Reconcile — Tentar novamente
              </Button>
            )}
          </div>
        )}
      </div>
    </Card>
  );
};
