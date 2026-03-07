import { useState, useEffect, useCallback } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { RefreshCcw, Loader2, CheckCircle2, XCircle } from "lucide-react";
import { SplitView } from "@/components/SplitView";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { useClusters, useNodePools } from "@/hooks/useAPI";
import type { NodePool } from "@/lib/api/types";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface NodePoolTabProps {
  onNodePoolModified?: () => void;
}

export const NodePoolTab = ({ onNodePoolModified }: NodePoolTabProps) => {
  const [selectedCluster, setSelectedCluster] = useState("");
  const [selectedNodePool, setSelectedNodePool] = useState<NodePool | null>(null);
  const [applyingPools, setApplyingPools] = useState<Set<string>>(new Set());
  const [poolResults, setPoolResults] = useState<Record<string, "success" | "error">>({});
  const [poolErrors, setPoolErrors] = useState<Record<string, string>>({});
  // Modal de status individual por pool (permite reabrir ao clicar no spinner)
  const [statusModalPool, setStatusModalPool] = useState<string | null>(null);
  
  // API hooks - só executam quando cluster está selecionado
  const { clusters } = useClusters();
  const { nodePools, loading: nodePoolsLoading, refetch: refetchNodePools } = useNodePools(selectedCluster);

  // Auto-select first cluster
  useEffect(() => {
    if (clusters.length > 0 && !selectedCluster) {
      setSelectedCluster(clusters[0].context);
    }
  }, [clusters, selectedCluster]);

  // Reset selection when cluster changes
  useEffect(() => {
    setSelectedNodePool(null);
  }, [selectedCluster]);

  // Escutar eventos de progresso de aplicação de node pools
  const handleApplyingEvent = useCallback((e: Event) => {
    const detail = (e as CustomEvent<{ poolName: string; status: "start" | "end"; result?: "success" | "error"; errorMessage?: string }>).detail;
    const { poolName, status } = detail;

    if (status === "start") {
      setApplyingPools((prev) => new Set(prev).add(poolName));
      setPoolResults((prev) => { const next = { ...prev }; delete next[poolName]; return next; });
      setPoolErrors((prev) => { const next = { ...prev }; delete next[poolName]; return next; });
    } else {
      setApplyingPools((prev) => { const next = new Set(prev); next.delete(poolName); return next; });
      if (detail.result) {
        setPoolResults((prev) => ({ ...prev, [poolName]: detail.result! }));
        if (detail.result === "error" && detail.errorMessage) {
          setPoolErrors((prev) => ({ ...prev, [poolName]: detail.errorMessage! }));
        }
        if (detail.result === "success") {
          setTimeout(() => {
            setPoolResults((prev) => { const next = { ...prev }; delete next[poolName]; return next; });
          }, 8000);
        }
      }
    }
  }, []);

  useEffect(() => {
    window.addEventListener("nodePoolApplying", handleApplyingEvent);
    return () => window.removeEventListener("nodePoolApplying", handleApplyingEvent);
  }, [handleApplyingEvent]);

  const handleClusterChange = async (newCluster: string) => {
    if (newCluster === selectedCluster) return;

    try {
      await apiClient.switchContext(newCluster);
      setSelectedCluster(newCluster);
      toast.success(`Contexto alterado para: ${newCluster}`);
    } catch (error) {
      toast.error(`Erro ao alterar contexto: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Controles específicos da aba Node Pools */}
      <div className="flex items-center gap-4 p-4 border-b bg-muted/20">
        <div className="flex items-center gap-2">
          <label className="text-sm font-medium">Cluster:</label>
          <Select value={selectedCluster} onValueChange={handleClusterChange}>
            <SelectTrigger className="w-[280px]">
              <SelectValue placeholder="Selecionar cluster..." />
            </SelectTrigger>
            <SelectContent>
              {clusters.map((cluster) => (
                <SelectItem key={cluster.context} value={cluster.context}>
                  {cluster.context}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Conteúdo da aba */}
      <div className="flex-1 min-h-0">
        <SplitView
          leftPanel={{
            title: "Available Node Pools",
            titleAction: (
              <Button
                variant="outline"
                size="sm"
                onClick={() => refetchNodePools()}
                disabled={!selectedCluster || nodePoolsLoading}
                title="Atualizar lista de Node Pools"
              >
                <RefreshCcw className={`w-4 h-4 ${nodePoolsLoading ? 'animate-spin' : ''}`} />
              </Button>
            ),
            content: nodePoolsLoading ? (
              <div className="flex items-center justify-center h-64 text-muted-foreground">
                Loading Node Pools...
              </div>
            ) : nodePools.length === 0 ? (
              <div className="flex items-center justify-center h-64 text-muted-foreground">
                {selectedCluster
                  ? "No Node Pools found in this cluster"
                  : "Select a cluster to view Node Pools"}
              </div>
            ) : (
              <div className="space-y-2">
                {nodePools.map((nodePool) => (
                  <NodePoolListItem
                    key={`${nodePool.cluster_name}-${nodePool.name}`}
                    nodePool={nodePool}
                    isSelected={selectedNodePool?.name === nodePool.name}
                    isApplying={applyingPools.has(nodePool.name)}
                    applyResult={poolResults[nodePool.name] ?? null}
                    applyError={poolErrors[nodePool.name]}
                    onClick={() => setSelectedNodePool(nodePool)}
                    onProgressClick={() => setStatusModalPool(nodePool.name)}
                  />
                ))}
              </div>
            ),
          }}
          rightPanel={{
            title: "Node Pool Editor",
            content: (
              <NodePoolEditor
                nodePool={selectedNodePool}
                onApplied={() => {
                  refetchNodePools();
                  onNodePoolModified?.();
                }}
              />
            ),
          }}
        />
      </div>

      {/* Modal de status individual de operação */}
      {statusModalPool && (() => {
        const isApplying = applyingPools.has(statusModalPool);
        const result = poolResults[statusModalPool];
        const errorMsg = poolErrors[statusModalPool];
        const pool = nodePools.find(np => np.name === statusModalPool);
        return (
          <Dialog open={!!statusModalPool} onOpenChange={(open) => { if (!open) setStatusModalPool(null); }}>
            <DialogContent className="max-w-md">
              <DialogHeader>
                <DialogTitle>Status da Operação</DialogTitle>
              </DialogHeader>
              <div className="space-y-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="font-semibold">{statusModalPool}</span>
                  <Badge variant="secondary">{pool?.cluster_name}</Badge>
                </div>
                <div className="text-xs text-muted-foreground">{pool?.resource_group}</div>
                {pool && (
                  <div className="text-sm text-muted-foreground">
                    {pool.autoscaling_enabled
                      ? `Autoscaling: ${pool.min_node_count}–${pool.max_node_count} nodes`
                      : `Manual: ${pool.node_count} node(s)`}
                  </div>
                )}
                <div className="flex items-center gap-2 pt-2">
                  {isApplying && (
                    <>
                      <Loader2 className="w-5 h-5 animate-spin text-blue-500" />
                      <span className="text-sm font-medium">Aguardando resposta do Azure...</span>
                    </>
                  )}
                  {!isApplying && result === "success" && (
                    <>
                      <CheckCircle2 className="w-5 h-5 text-green-500" />
                      <span className="text-sm font-medium text-green-600 dark:text-green-400">Aplicado com sucesso</span>
                    </>
                  )}
                  {!isApplying && result === "error" && (
                    <div className="space-y-2 w-full">
                      <div className="flex items-center gap-2">
                        <XCircle className="w-5 h-5 text-red-500 flex-shrink-0" />
                        <span className="text-sm font-medium text-red-600 dark:text-red-400">Falha na operação</span>
                      </div>
                      {errorMsg && (
                        <div className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-800 rounded p-2 break-all">
                          {errorMsg}
                        </div>
                      )}
                    </div>
                  )}
                  {!isApplying && !result && (
                    <span className="text-sm text-muted-foreground">Nenhuma operação em andamento</span>
                  )}
                </div>
              </div>
              <div className="flex justify-end">
                <Button variant="outline" size="sm" onClick={() => setStatusModalPool(null)}>Fechar</Button>
              </div>
            </DialogContent>
          </Dialog>
        );
      })()}
    </div>
  );
};