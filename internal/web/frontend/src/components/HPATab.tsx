import { useState, useEffect } from "react";
import { 
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SplitView } from "@/components/SplitView";
import { HPAListItem } from "@/components/HPAListItem";
import { HPAEditor } from "@/components/HPAEditor";
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

  const staging = useStaging();
  
  // API hooks - só executam quando cluster está selecionado
  const { clusters } = useClusters();
  const { namespaces } = useNamespaces(selectedCluster);
  const { hpas, loading: hpasLoading, refetch: refetchHPAs } = useHPAs(selectedCluster, selectedNamespace);

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

  const handleApplySingle = (hpa: HPA) => {
    console.log("Aplicando HPA:", hpa);
    onHPAModified?.();
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
            content: (
              <HPAEditor
                hpa={selectedHPA}
                onApply={handleApplySingle}
                onApplied={() => {
                  refetchHPAs();
                  onHPAModified?.();
                }}
              />
            ),
          }}
        />
      </div>
    </div>
  );
};