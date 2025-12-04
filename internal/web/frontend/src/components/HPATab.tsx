import { useState, useEffect } from "react";
import { 
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SplitView } from "@/components/SplitView";
import { HPAListItem } from "@/components/HPAListItem";
import { HPAEditor } from "@/components/HPAEditor";
import { HPATableView } from "@/components/HPATableView";
import { useClusters, useNamespaces, useHPAs } from "@/hooks/useAPI";
import type { HPA } from "@/lib/api/types";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface HPATabProps {
  onHPAModified?: () => void;
}

export const HPATab = ({ onHPAModified }: HPATabProps) => {
  const [selectedCluster, setSelectedCluster] = useState("");
  const [selectedNamespace, setSelectedNamespace] = useState("");
  const [selectedHPA, setSelectedHPA] = useState<HPA | null>(null);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [hpaToApply, setHpaToApply] = useState<{ hpa: HPA; original: HPA } | null>(null);
  const [isApplying, setIsApplying] = useState(false);

  const staging = useStaging();
  
  // API hooks - só executam quando cluster está selecionado
  const { clusters } = useClusters();
  const { namespaces } = useNamespaces(selectedCluster);
  // Buscar TODOS os HPAs do cluster (sem filtro de namespace)
  const { hpas, loading: hpasLoading, refetch: refetchHPAs } = useHPAs(selectedCluster, undefined, false);

  // Auto-select first cluster
  useEffect(() => {
    if (clusters.length > 0 && !selectedCluster) {
      setSelectedCluster(clusters[0].context);
    }
  }, [clusters, selectedCluster]);

  // Reset namespace when cluster changes
  useEffect(() => {
    setSelectedNamespace("");
    setSelectedHPA(null);
  }, [selectedCluster]);

  // Auto-select first namespace
  useEffect(() => {
    if (namespaces.length > 0 && !selectedNamespace) {
      const nonSystemNamespaces = namespaces.filter(ns => !ns.isSystem);
      if (nonSystemNamespaces.length > 0) {
        setSelectedNamespace(nonSystemNamespaces[0].name);
      } else if (namespaces.length > 0) {
        setSelectedNamespace(namespaces[0].name);
      }
    }
  }, [namespaces, selectedNamespace]);

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

  const handleApplySingle = async (hpa: HPA, original: HPA) => {
    setHpaToApply({ hpa, original });
    setApplyConfirmOpen(true);
  };

  const confirmApplyHPA = async () => {
    if (!hpaToApply) return;

    const { hpa, original } = hpaToApply;
    setIsApplying(true);

    try {
      // Adicionar sufixo -admin ao nome do cluster para a API
      const clusterWithAdmin = hpa.cluster.endsWith('-admin') 
        ? hpa.cluster 
        : `${hpa.cluster}-admin`;
      
      // Update the HPA object's cluster property to match
      const hpaWithCorrectCluster = {
        ...hpa,
        cluster: clusterWithAdmin
      };
      
      toast.info(`Aplicando HPA ${hpa.name}...`);
      
      await apiClient.updateHPA(
        clusterWithAdmin,
        hpa.namespace,
        hpa.name,
        hpaWithCorrectCluster
      );

      toast.success(`✅ HPA ${hpa.name} aplicado com sucesso`);
      
      setApplyConfirmOpen(false);
      setHpaToApply(null);
      
      // Refresh HPAs list
      await refetchHPAs();
      onHPAModified?.();
      
      // Reselect the updated HPA to show new values
      const updatedHPAs = await apiClient.getHPAs(selectedCluster, undefined, true);
      const updatedHPA = updatedHPAs.find(h => 
        h.name === hpa.name && h.namespace === hpa.namespace
      );
      if (updatedHPA) {
        setSelectedHPA(updatedHPA);
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
      toast.error(`❌ Erro ao aplicar ${hpa.name}: ${errorMessage}`);
    } finally {
      setIsApplying(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Controles específicos da aba HPAs */}
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

        <div className="flex items-center gap-2">
          <label className="text-sm font-medium">Namespace:</label>
          <Select value={selectedNamespace} onValueChange={setSelectedNamespace}>
            <SelectTrigger className="w-[200px]">
              <SelectValue placeholder="Selecionar namespace..." />
            </SelectTrigger>
            <SelectContent>
              {namespaces.map((namespace) => (
                <SelectItem key={namespace.name} value={namespace.name}>
                  {namespace.name}
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
            title: "Available HPAs",
            content: hpasLoading ? (
              <div className="flex items-center justify-center h-64 text-muted-foreground">
                Loading HPAs...
              </div>
            ) : hpas.length === 0 ? (
              <div className="flex items-center justify-center h-64 text-muted-foreground">
                {selectedCluster
                  ? "No HPAs found in this cluster/namespace"
                  : "Select a cluster to view HPAs"}
              </div>
            ) : (
              <div className="space-y-2">
                {hpas.map((hpa) => (
                  <HPAListItem
                    key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
                    name={hpa.name}
                    namespace={hpa.namespace}
                    currentReplicas={hpa.current_replicas ?? 0}
                    minReplicas={hpa.min_replicas ?? 0}
                    maxReplicas={hpa.max_replicas ?? 1}
                    isSelected={
                      selectedHPA?.name === hpa.name &&
                      selectedHPA?.namespace === hpa.namespace
                    }
                    onClick={() => setSelectedHPA(hpa)}
                  />
                ))}
              </div>
            ),
          }}
          rightPanel={{
            title: "HPA Editor",
            content: selectedHPA ? (
              <HPAEditor
                hpa={selectedHPA}
                onApply={handleApplySingle}
                onApplied={() => {
                  refetchHPAs();
                  onHPAModified?.();
                }}
              />
            ) : (
              <div className="p-4 overflow-auto">
                <HPATableView
                  hpas={hpas}
                  onSelectHPA={(hpa) => setSelectedHPA(hpa)}
                />
              </div>
            ),
          }}
        />
      </div>

      {/* Modal de confirmação */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent className="max-w-2xl max-h-[90vh] bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">
              Confirmar aplicação de HPA
            </DialogTitle>
            <DialogDescription>
              Essa ação vai aplicar as alterações do HPA diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          {hpaToApply && (
            <div className="space-y-3 text-sm">
              <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
                <p><span className="text-muted-foreground">Cluster:</span> {hpaToApply.hpa.cluster}</p>
                <p><span className="text-muted-foreground">Namespace:</span> {hpaToApply.hpa.namespace}</p>
                <p><span className="text-muted-foreground">HPA:</span> {hpaToApply.hpa.name}</p>
              </div>
              
              <div className="space-y-2">
                <p className="font-semibold text-sm">Mudanças:</p>
                <div className="max-h-[400px] overflow-y-auto space-y-2 border rounded-lg p-3 bg-muted/10">
                  {hpaToApply.original.min_replicas !== hpaToApply.hpa.min_replicas && (
                    <div className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">Min Replicas</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="text-[11px] text-red-300">{hpaToApply.original.min_replicas ?? 'N/A'}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="text-[11px] text-green-300">{hpaToApply.hpa.min_replicas ?? 'N/A'}</pre>
                        </div>
                      </div>
                    </div>
                  )}
                  {hpaToApply.original.max_replicas !== hpaToApply.hpa.max_replicas && (
                    <div className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">Max Replicas</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="text-[11px] text-red-300">{hpaToApply.original.max_replicas}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="text-[11px] text-green-300">{hpaToApply.hpa.max_replicas}</pre>
                        </div>
                      </div>
                    </div>
                  )}
                  {hpaToApply.original.target_cpu !== hpaToApply.hpa.target_cpu && (
                    <div className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">Target CPU (%)</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="text-[11px] text-red-300">{hpaToApply.original.target_cpu ?? 'N/A'}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="text-[11px] text-green-300">{hpaToApply.hpa.target_cpu ?? 'N/A'}</pre>
                        </div>
                      </div>
                    </div>
                  )}
                  {hpaToApply.original.target_memory !== hpaToApply.hpa.target_memory && (
                    <div className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">Target Memory (%)</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="text-[11px] text-red-300">{hpaToApply.original.target_memory ?? 'N/A'}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="text-[11px] text-green-300">{hpaToApply.hpa.target_memory ?? 'N/A'}</pre>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              </div>
              
              <p className="text-muted-foreground">
                Esta operação não possui rollback automático. Confirme que as mudanças estão corretas.
              </p>
            </div>
          )}
          <div className="flex justify-end gap-2 pt-4">
            <Button
              variant="ghost"
              onClick={() => {
                setApplyConfirmOpen(false);
                setHpaToApply(null);
              }}
              disabled={isApplying}
            >
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={confirmApplyHPA}
              disabled={isApplying}
            >
              {isApplying ? "Aplicando..." : "Confirmar e Aplicar"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
};