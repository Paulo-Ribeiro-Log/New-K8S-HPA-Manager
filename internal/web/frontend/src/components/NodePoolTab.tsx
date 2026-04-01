import { useState, useEffect, useCallback, useRef } from "react";
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
import { RefreshCcw, Loader2, CheckCircle2, XCircle, RotateCw, Ban } from "lucide-react";
import { SplitView } from "@/components/SplitView";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { useClusters, useNodePools } from "@/hooks/useAPI";
import type { NodePool } from "@/lib/api/types";
import { apiClient } from "@/lib/api/client";
import { useStaging } from "@/contexts/StagingContext";
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
  const [reconcilingPool, setReconcilingPool] = useState<string | null>(null);
  const reconcilingAbortRef = useRef<AbortController | null>(null);
  // Chave para forçar remount do NodePoolEditor com dados frescos após Atualizar
  const [editorKey, setEditorKey] = useState(0);
  // Flag para sinalizar que o remount deve ocorrer após o próximo update de nodePools
  const pendingEditorRemountRef = useRef(false);
  const [isSwitchingContext, setIsSwitchingContext] = useState(false);

  // API hooks - só executam quando cluster está selecionado
  const { clusters } = useClusters();
  const { nodePools, loading: nodePoolsLoading, refetch: refetchNodePools } = useNodePools(selectedCluster);
  const staging = useStaging();

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

  // Sincroniza selectedNodePool com dados frescos após refetchNodePools
  useEffect(() => {
    if (!selectedNodePool || nodePools.length === 0) return;
    const updated = nodePools.find(
      (np) => np.name === selectedNodePool.name && np.cluster_name === selectedNodePool.cluster_name
    );
    if (!updated) return;
    setSelectedNodePool(updated);
    if (pendingEditorRemountRef.current) {
      setEditorKey((k) => k + 1);
      pendingEditorRemountRef.current = false;
    }
  }, [nodePools, selectedNodePool]);

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

  const handleAbort = useCallback(async (poolName: string) => {
    // 1. Abortar reconcile local se estiver ativo para este pool
    if (reconcilingPool === poolName && reconcilingAbortRef.current) {
      reconcilingAbortRef.current.abort();
    }
    // 2. Abortar apply do NodePoolApplyModal via CustomEvent (cancela o fetch)
    window.dispatchEvent(new CustomEvent("nodePoolAbort", { detail: { poolName } }));

    // 3. Chamar backend para matar o processo az CLI em execução
    const pool = nodePools.find(np => np.name === poolName);
    if (pool) {
      try {
        const result = await apiClient.abortNodePoolOperation(pool.cluster_name, pool.resource_group, poolName);
        if (result.success) {
          toast.warning("Operação abortada no Azure. Aguarde antes de tentar Reconcile.");
        }
      } catch {
        // Pool pode não ter operação ativa no backend (ex: já terminou)
        toast.info("Nenhuma operação ativa encontrada para abortar.");
      }
    }
  }, [reconcilingPool, nodePools]);

  const handleClusterChange = async (newCluster: string) => {
    if (newCluster === selectedCluster || isSwitchingContext) return;

    setIsSwitchingContext(true);
    try {
      await apiClient.switchContext(newCluster);
      setSelectedCluster(newCluster);
      toast.success(`Contexto alterado para: ${newCluster}`);
    } catch (error) {
      toast.error(`Erro ao alterar contexto: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    } finally {
      setIsSwitchingContext(false);
    }
  };

  const handleReconcile = async (poolName: string) => {
    // Buscar pool no staging pelo nome + cluster
    const staged = staging?.stagedNodePools.find(
      (np) => np.name === poolName && np.cluster_name === selectedCluster && np.isModified
    );

    if (!staged) {
      toast.error("Alterações não encontradas no staging", {
        description: "Re-edite o Node Pool e aplique novamente.",
      });
      return;
    }

    setReconcilingPool(poolName);
    setApplyingPools((prev) => new Set(prev).add(poolName));
    setPoolResults((prev) => { const next = { ...prev }; delete next[poolName]; return next; });
    setPoolErrors((prev) => { const next = { ...prev }; delete next[poolName]; return next; });

    let success = false;
    let capturedError: string | undefined;
    try {
      await apiClient.updateNodePool(
        staged.cluster_name,
        staged.resource_group,
        staged.name,
        {
          node_count: staged.node_count,
          min_node_count: staged.min_node_count,
          max_node_count: staged.max_node_count,
          autoscaling_enabled: staged.autoscaling_enabled,
        }
      );
      success = true;
      setPoolResults((prev) => ({ ...prev, [poolName]: "success" }));
      toast.success(`Node Pool ${poolName} reconciliado com sucesso`);
      refetchNodePools();
      onNodePoolModified?.();
    } catch (error) {
      capturedError = error instanceof Error ? error.message : "Erro desconhecido";
      setPoolResults((prev) => ({ ...prev, [poolName]: "error" }));
      setPoolErrors((prev) => ({ ...prev, [poolName]: capturedError! }));
      toast.error(`Falha ao reconciliar ${poolName}`, { description: capturedError });
    } finally {
      setApplyingPools((prev) => { const next = new Set(prev); next.delete(poolName); return next; });
      setReconcilingPool(null);
      // Limpa resultado de sucesso após 8s
      if (success) {
        setTimeout(() => {
          setPoolResults((prev) => { const next = { ...prev }; delete next[poolName]; return next; });
        }, 8000);
      }
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Controles específicos da aba Node Pools */}
      <div className="flex items-center gap-4 p-4 border-b bg-muted/20">
        <div className="flex items-center gap-2">
          <label className="text-sm font-medium">Cluster:</label>
          <Select value={selectedCluster} onValueChange={handleClusterChange} disabled={isSwitchingContext}>
            <SelectTrigger className="w-[280px]">
              <SelectValue placeholder={isSwitchingContext ? "Alternando..." : "Selecionar cluster..."} />
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
                    onAbort={applyingPools.has(nodePool.name) ? () => handleAbort(nodePool.name) : undefined}
                    onReconcile={poolResults[nodePool.name] === "error" ? () => handleReconcile(nodePool.name) : undefined}
                  />
                ))}
              </div>
            ),
          }}
          rightPanel={{
            title: "Node Pool Editor",
            titleAction: selectedNodePool ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  pendingEditorRemountRef.current = true;
                  refetchNodePools();
                }}
                disabled={nodePoolsLoading}
                title="Atualizar dados do Node Pool"
              >
                <RefreshCcw className={`w-4 h-4 ${nodePoolsLoading ? "animate-spin" : ""}`} />
              </Button>
            ) : undefined,
            content: (
              <NodePoolEditor
                key={editorKey}
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
                    <div className="space-y-2 w-full">
                      <div className="flex items-center gap-2">
                        <Loader2 className="w-5 h-5 animate-spin text-blue-500" />
                        <span className="text-sm font-medium">Aguardando resposta do Azure...</span>
                      </div>
                      <Button
                        size="sm"
                        variant="outline"
                        className="w-full border-red-400 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/30"
                        onClick={() => handleAbort(statusModalPool!)}
                      >
                        <Ban className="w-4 h-4 mr-2" />
                        Abortar operação
                      </Button>
                    </div>
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
                      <Button
                        size="sm"
                        variant="outline"
                        className="w-full border-amber-400 text-amber-600 dark:text-amber-400 hover:bg-amber-50 dark:hover:bg-amber-950/30"
                        disabled={reconcilingPool === statusModalPool}
                        onClick={() => handleReconcile(statusModalPool!)}
                      >
                        {reconcilingPool === statusModalPool ? (
                          <><Loader2 className="w-4 h-4 mr-2 animate-spin" />Reconciliando...</>
                        ) : (
                          <><RotateCw className="w-4 h-4 mr-2" />Reconcile — Tentar novamente</>
                        )}
                      </Button>
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