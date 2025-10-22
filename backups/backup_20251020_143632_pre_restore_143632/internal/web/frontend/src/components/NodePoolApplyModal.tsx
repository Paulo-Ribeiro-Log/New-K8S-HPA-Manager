import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Loader2, AlertTriangle, CheckCircle2, XCircle, TrendingUp, TrendingDown } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { apiClient } from "@/lib/api/client";
import type { NodePool } from "@/lib/api/types";
import { toast } from "sonner";

interface NodePoolApplyModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modifiedNodePools: Array<{
    key: string;
    current: NodePool;
    original: NodePool;
    order?: number; // Ordem de execução sequencial (*1, *2)
  }>;
  onApplied?: () => void;
  onClear?: () => void;
}

export const NodePoolApplyModal = ({
  open,
  onOpenChange,
  modifiedNodePools,
  onApplied,
  onClear,
}: NodePoolApplyModalProps) => {
  const [applying, setApplying] = useState(false);
  const [results, setResults] = useState<
    Array<{ key: string; status: "pending" | "success" | "error"; message?: string }>
  >([]);

  // Detectar se há node pools com ordem sequencial
  const hasSequentialPools = modifiedNodePools.some((np) => np.order !== undefined);
  const sequentialPools = modifiedNodePools
    .filter((np) => np.order !== undefined)
    .sort((a, b) => (a.order || 0) - (b.order || 0));
  const normalPools = modifiedNodePools.filter((np) => np.order === undefined);

  const handleApply = async () => {
    setApplying(true);
    setResults(
      modifiedNodePools.map((np) => ({ key: np.key, status: "pending" }))
    );

    const newResults = [];

    try {
      if (hasSequentialPools && sequentialPools.length > 0) {
        // Execução SEQUENCIAL via endpoint dedicado
        const cluster = sequentialPools[0].current.cluster_name;

        const response = await apiClient.applyNodePoolsSequential(
          cluster,
          sequentialPools.map((np) => ({
            name: np.current.name,
            autoscaling_enabled: np.current.autoscaling_enabled,
            node_count: np.current.node_count,
            min_node_count: np.current.min_node_count,
            max_node_count: np.current.max_node_count,
            order: np.order || 0,
          }))
        );

        // Processar resultados do endpoint sequencial
        if (response.success && response.results) {
          for (const result of response.results) {
            const matchingPool = sequentialPools.find(
              (p) => p.current.name === result.pool_name
            );
            if (matchingPool) {
              newResults.push({
                key: matchingPool.key,
                status: result.success ? "success" : "error",
                message: result.message || (result.success ? "Aplicado com sucesso" : "Erro na aplicação"),
              });
            }
          }
        } else {
          // Erro geral no endpoint
          sequentialPools.forEach((np) => {
            newResults.push({
              key: np.key,
              status: "error",
              message: response.error?.message || "Erro ao executar sequencialmente",
            });
          });
        }
      }

      // Executar node pools NORMAIS (sem ordem) em paralelo
      for (const { key, current } of normalPools) {
        try {
          await apiClient.updateNodePool(
            current.cluster_name,
            current.resource_group,
            current.name,
            {
              node_count: current.node_count,
              min_node_count: current.min_node_count,
              max_node_count: current.max_node_count,
              autoscaling_enabled: current.autoscaling_enabled,
            }
          );

          newResults.push({
            key,
            status: "success" as const,
            message: "Aplicado com sucesso",
          });
        } catch (error) {
          newResults.push({
            key,
            status: "error" as const,
            message: error instanceof Error ? error.message : "Erro desconhecido",
          });
        }
      }

      setResults(newResults);

      // Verificar se todos foram bem-sucedidos
      const allSuccess = newResults.every((r) => r.status === "success");
      if (allSuccess) {
        toast.success(`${newResults.length} node pool(s) aplicado(s) com sucesso`);
        onApplied?.();
        onClear?.();
        setTimeout(() => onOpenChange(false), 2000);
      } else {
        const errorCount = newResults.filter((r) => r.status === "error").length;
        toast.error(`${errorCount} erro(s) ao aplicar node pools`);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao aplicar alterações");
    } finally {
      setApplying(false);
    }
  };

  const getChangesList = (current: NodePool, original: NodePool) => {
    const changes: string[] = [];

    if (current.autoscaling_enabled !== original.autoscaling_enabled) {
      changes.push(
        `Autoscaling: ${original.autoscaling_enabled ? "Ativado" : "Desativado"} → ${
          current.autoscaling_enabled ? "Ativado" : "Desativado"
        }`
      );
    }

    if (current.node_count !== original.node_count) {
      changes.push(`Node Count: ${original.node_count} → ${current.node_count}`);
    }

    if (current.min_node_count !== original.min_node_count) {
      changes.push(`Min Nodes: ${original.min_node_count} → ${current.min_node_count}`);
    }

    if (current.max_node_count !== original.max_node_count) {
      changes.push(`Max Nodes: ${original.max_node_count} → ${current.max_node_count}`);
    }

    return changes;
  };

  const getStatus = (key: string) => {
    return results.find((r) => r.key === key);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle>
            {hasSequentialPools ? "Executar Node Pools Sequencialmente" : "Confirmar Alterações de Node Pools"}
          </DialogTitle>
          <DialogDescription>
            {hasSequentialPools
              ? `Execução sequencial: ${sequentialPools.map(p => `*${p.order}`).join(" → ")} ${normalPools.length > 0 ? `+ ${normalPools.length} normal` : ""}`
              : `${modifiedNodePools.length} node pool(s) será(ão) modificado(s) no Azure`}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[50vh] pr-4">
          <div className="space-y-4">
            {modifiedNodePools.map(({ key, current, original, order }) => {
              const status = getStatus(key);
              const changes = getChangesList(current, original);

              return (
                <div
                  key={key}
                  className="border rounded-lg p-4 space-y-3 bg-card"
                >
                  {/* Header */}
                  <div className="flex items-start justify-between">
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-1">
                        {order !== undefined && (
                          <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950 text-blue-600 dark:text-blue-400">
                            *{order}
                          </Badge>
                        )}
                        <h3 className="font-semibold">{current.name}</h3>
                        <Badge variant={current.is_system_pool ? "default" : "secondary"}>
                          {current.is_system_pool ? "System" : "User"}
                        </Badge>
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {current.cluster_name} • {current.vm_size}
                      </p>
                    </div>

                    {/* Status Icon */}
                    {status && (
                      <div className="flex items-center gap-2">
                        {status.status === "pending" && (
                          <Loader2 className="w-5 h-5 animate-spin text-blue-500" />
                        )}
                        {status.status === "success" && (
                          <CheckCircle2 className="w-5 h-5 text-green-500" />
                        )}
                        {status.status === "error" && (
                          <XCircle className="w-5 h-5 text-red-500" />
                        )}
                      </div>
                    )}
                  </div>

                  {/* Changes List */}
                  <div className="space-y-1.5 text-sm bg-muted/50 p-3 rounded">
                    {changes.map((change, idx) => (
                      <div key={idx} className="flex items-center gap-2">
                        <span className="text-muted-foreground">•</span>
                        <span>{change}</span>
                      </div>
                    ))}
                  </div>

                  {/* Scaling Mode Indicator */}
                  <div className="flex items-center gap-2 text-sm">
                    {current.autoscaling_enabled ? (
                      <>
                        <TrendingUp className="w-4 h-4 text-green-500" />
                        <span className="text-green-600 dark:text-green-400">
                          Autoscaling: {current.min_node_count}-{current.max_node_count} nodes
                        </span>
                      </>
                    ) : (
                      <>
                        <TrendingDown className="w-4 h-4 text-blue-500" />
                        <span className="text-blue-600 dark:text-blue-400">
                          Manual: {current.node_count} node(s)
                        </span>
                      </>
                    )}
                  </div>

                  {/* Error Message */}
                  {status?.status === "error" && status.message && (
                    <div className="flex items-start gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 p-2 rounded">
                      <AlertTriangle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                      <span>{status.message}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={applying}
          >
            Cancelar
          </Button>
          <Button onClick={handleApply} disabled={applying}>
            {applying ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                Aplicando...
              </>
            ) : (
              `Aplicar ${modifiedNodePools.length} Node Pool(s)`
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
