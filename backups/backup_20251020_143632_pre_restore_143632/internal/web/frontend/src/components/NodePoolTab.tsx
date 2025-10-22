import { useState, useEffect } from "react";
import { 
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SplitView } from "@/components/SplitView";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { useClusters, useNodePools } from "@/hooks/useAPI";
import type { NodePool } from "@/lib/api/types";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface NodePoolTabProps {
  onNodePoolModified?: () => void;
}

export const NodePoolTab = ({ onNodePoolModified }: NodePoolTabProps) => {
  const [selectedCluster, setSelectedCluster] = useState("");
  const [selectedNodePool, setSelectedNodePool] = useState<NodePool | null>(null);

  const staging = useStaging();
  
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
                    isSelected={
                      selectedNodePool?.name === nodePool.name
                    }
                    onClick={() => setSelectedNodePool(nodePool)}
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
    </div>
  );
};