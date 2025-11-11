import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useStaging } from "@/contexts/StagingContext";
import { Edit2, Trash2, FileText, Search } from "lucide-react";
import { HPAEditor } from "./HPAEditor";
import { NodePoolEditor } from "./NodePoolEditor";
import { SplitView } from "./SplitView";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { HPA, NodePool } from "@/lib/api/types";

export function StagingPanel() {
  const staging = useStaging();
  const [editingHPA, setEditingHPA] = useState<HPA | null>(null);
  const [editingNodePool, setEditingNodePool] = useState<NodePool | null>(null);
  const [selectedItem, setSelectedItem] = useState<{ type: 'hpa' | 'nodepool'; item: HPA | NodePool } | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const changesCount = staging.getChangesCount();

  // Combinar HPAs e Node Pools em uma lista única
  const allItems = [
    ...staging.stagedHPAs.map(hpa => ({ type: 'hpa' as const, item: hpa })),
    ...staging.stagedNodePools.map(np => ({ type: 'nodepool' as const, item: np }))
  ];

  // Filtrar itens baseado na busca
  const filteredItems = allItems.filter(({ type, item }) => {
    const query = searchQuery.toLowerCase();
    if (type === 'hpa') {
      const hpa = item as HPA;
      return hpa.name.toLowerCase().includes(query) ||
             hpa.namespace.toLowerCase().includes(query) ||
             hpa.cluster.toLowerCase().includes(query);
    } else {
      const np = item as NodePool;
      return np.name.toLowerCase().includes(query) ||
             np.cluster_name.toLowerCase().includes(query);
    }
  });

  // Handler para remover item
  const handleRemove = (type: 'hpa' | 'nodepool', item: HPA | NodePool) => {
    if (type === 'hpa') {
      const hpa = item as HPA;
      staging.removeHPAFromStaging(hpa.cluster, hpa.namespace, hpa.name);
      if (selectedItem?.type === 'hpa' && (selectedItem.item as HPA).name === hpa.name) {
        setSelectedItem(null);
      }
    } else {
      const np = item as NodePool;
      staging.removeNodePoolFromStaging(np.cluster_name, np.name);
      if (selectedItem?.type === 'nodepool' && (selectedItem.item as NodePool).name === np.name) {
        setSelectedItem(null);
      }
    }
  };

  // Renderizar item da lista (compacto como CronJobListItem/PrometheusListItem)
  const renderListItem = ({ type, item }: { type: 'hpa' | 'nodepool'; item: HPA | NodePool }) => {
    const isSelected = selectedItem?.type === type &&
      (type === 'hpa'
        ? (selectedItem.item as HPA).name === (item as HPA).name
        : (selectedItem.item as NodePool).name === (item as NodePool).name
      );

    if (type === 'hpa') {
      const hpa = item as HPA;
      return (
        <div
          key={`hpa-${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
          className={`p-3 border rounded-lg cursor-pointer transition-all hover:border-primary/50 ${
            isSelected ? 'border-primary bg-primary/5' : 'border-border'
          }`}
          onClick={() => setSelectedItem({ type: 'hpa', item: hpa })}
        >
          <div className="flex items-start justify-between">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950 text-xs">
                  HPA
                </Badge>
                <span className="font-semibold truncate text-sm">{hpa.name}</span>
                {hpa.resources_modified && (
                  <Badge variant="secondary" className="text-xs">
                    Modified
                  </Badge>
                )}
              </div>
              <div className="text-xs text-muted-foreground space-y-0.5">
                <div>📦 {hpa.namespace}</div>
                <div>🎯 {hpa.cluster}</div>
                {hpa.original_values && (
                  <div className="mt-1 text-xs font-mono bg-muted/50 p-1 rounded">
                    Min: {hpa.original_values.min_replicas} → {hpa.min_replicas} |
                    Max: {hpa.original_values.max_replicas} → {hpa.max_replicas}
                  </div>
                )}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive ml-2"
              onClick={(e) => {
                e.stopPropagation();
                handleRemove('hpa', hpa);
              }}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        </div>
      );
    } else {
      const np = item as NodePool;
      return (
        <div
          key={`np-${np.cluster_name}-${np.name}`}
          className={`p-3 border rounded-lg cursor-pointer transition-all hover:border-primary/50 ${
            isSelected ? 'border-primary bg-primary/5' : 'border-border'
          }`}
          onClick={() => setSelectedItem({ type: 'nodepool', item: np })}
        >
          <div className="flex items-start justify-between">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-xs">
                  Node Pool
                </Badge>
                <span className="font-semibold truncate text-sm">{np.name}</span>
                {np.modified && (
                  <Badge variant="secondary" className="text-xs">
                    Modified
                  </Badge>
                )}
              </div>
              <div className="text-xs text-muted-foreground space-y-0.5">
                <div>🎯 {np.cluster_name}</div>
                <div>📁 {np.resource_group}</div>
                {np.modified && (
                  <div className="mt-1 text-xs font-mono bg-muted/50 p-1 rounded">
                    Nodes: {np.original_values.node_count} → {np.node_count}
                    {np.autoscaling_enabled && ` (${np.min_node_count}-${np.max_node_count})`}
                  </div>
                )}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive ml-2"
              onClick={(e) => {
                e.stopPropagation();
                handleRemove('nodepool', np);
              }}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        </div>
      );
    }
  };

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Staging Items",
          titleAction: changesCount.total > 0 && (
            <Button
              variant="outline"
              size="sm"
              onClick={staging.clearStaging}
              className="text-destructive hover:text-destructive"
            >
              Clear All
            </Button>
          ),
          content: changesCount.total === 0 ? (
            <div className="flex items-center justify-center h-64 text-muted-foreground">
              <div className="text-center">
                <FileText className="h-12 w-12 mx-auto mb-4 opacity-20" />
                <p className="text-sm">No items in staging</p>
                <p className="text-xs mt-2 opacity-70">
                  Add HPAs or Node Pools to begin
                </p>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              {/* Search input */}
              <div className="relative">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              {searchQuery && (
                <button
                  type="button"
                  onClick={() => setSearchQuery("")}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label="Limpar busca do staging"
                >
                  ×
                </button>
              )}
              <Input
                type="text"
                placeholder="Search by name, namespace, or cluster..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-10 pr-8"
              />
            </div>

              {/* Stats badges */}
              <div className="flex items-center gap-2 flex-wrap">
                <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950">
                  {staging.stagedHPAs.length} HPAs
                </Badge>
                <Badge variant="outline" className="bg-green-50 dark:bg-green-950">
                  {staging.stagedNodePools.length} Node Pools
                </Badge>
              </div>

              {/* Items list */}
              <div className="space-y-2">
                {filteredItems.length === 0 ? (
                  <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                    No items match your search
                  </div>
                ) : (
                  filteredItems.map(renderListItem)
                )}
              </div>
            </div>
          ),
        }}
        rightPanel={{
          title: selectedItem ? (
            selectedItem.type === 'hpa'
              ? `Edit HPA: ${(selectedItem.item as HPA).name}`
              : `Edit Node Pool: ${(selectedItem.item as NodePool).name}`
          ) : "Editor",
          content: !selectedItem ? (
            <div className="flex items-center justify-center h-64 text-muted-foreground">
              <div className="text-center">
                <Edit2 className="h-12 w-12 mx-auto mb-4 opacity-20" />
                <p className="text-sm">Select an item to edit</p>
              </div>
            </div>
          ) : selectedItem.type === 'hpa' ? (
            <HPAEditor
              hpa={selectedItem.item as HPA}
            />
          ) : (
            <NodePoolEditor
              nodePool={selectedItem.item as NodePool}
            />
          ),
        }}
      />

      {/* HPA Edit Modal (mantido para compatibilidade, mas não usado) */}
      <Dialog open={!!editingHPA} onOpenChange={(open) => !open && setEditingHPA(null)}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>Editar HPA no Staging</DialogTitle>
            <DialogDescription>
              Modifique os valores antes de aplicar as alterações
            </DialogDescription>
          </DialogHeader>

          <div className="flex-1 overflow-auto">
            {editingHPA && (
              <HPAEditor
                hpa={editingHPA}
                onApplied={() => setEditingHPA(null)}
              />
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* Node Pool Edit Modal (mantido para compatibilidade, mas não usado) */}
      <Dialog open={!!editingNodePool} onOpenChange={(open) => !open && setEditingNodePool(null)}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>Editar Node Pool no Staging</DialogTitle>
            <DialogDescription>
              Modifique os valores antes de aplicar as alterações
            </DialogDescription>
          </DialogHeader>

          <div className="flex-1 overflow-auto">
            {editingNodePool && (
              <NodePoolEditor
                nodePool={editingNodePool}
                onApplied={() => setEditingNodePool(null)}
              />
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
