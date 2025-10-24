import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { useStaging } from "@/contexts/StagingContext";
import { Edit2, Trash2, FileText } from "lucide-react";
import { HPAEditor } from "./HPAEditor";
import { NodePoolEditor } from "./NodePoolEditor";
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

  const changesCount = staging.getChangesCount();

  if (changesCount.total === 0) {
    return (
      <Card className="h-full">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FileText className="h-5 w-5" />
            Alterações Pendentes
          </CardTitle>
          <CardDescription>
            Nenhuma alteração no staging
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col items-center justify-center h-48 text-muted-foreground">
          <p className="text-sm">Adicione HPAs ou Node Pools para começar</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card className="h-full flex flex-col">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                <FileText className="h-5 w-5" />
                Alterações Pendentes
              </CardTitle>
              <CardDescription>
                {changesCount.total} {changesCount.total === 1 ? 'item' : 'itens'} no staging
              </CardDescription>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={staging.clearStaging}
              className="text-destructive hover:text-destructive"
            >
              Limpar Tudo
            </Button>
          </div>
        </CardHeader>

        <Separator />

        <CardContent className="flex-1 overflow-hidden p-0">
          <ScrollArea className="h-full">
            <div className="p-4 space-y-4">
              {/* HPAs Section */}
              {staging.stagedHPAs.length > 0 && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950">
                      {staging.stagedHPAs.length} HPAs
                    </Badge>
                  </div>

                  <div className="space-y-2">
                    {staging.stagedHPAs.map((hpa) => (
                      <Card
                        key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
                        className="border-l-4 border-l-blue-500"
                      >
                        <CardContent className="p-3">
                          <div className="flex items-start justify-between">
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-2 mb-1">
                                <span className="font-semibold truncate">{hpa.name}</span>
                                {hpa.isModified && (
                                  <Badge variant="secondary" className="text-xs">
                                    Modificado
                                  </Badge>
                                )}
                              </div>
                              <div className="text-xs text-muted-foreground space-y-0.5">
                                <div>📦 {hpa.namespace}</div>
                                <div>🎯 {hpa.cluster}</div>
                                {hpa.isModified && (
                                  <div className="mt-1 text-xs font-mono">
                                    Min: {hpa.originalValues.min_replicas} → {hpa.min_replicas} |
                                    Max: {hpa.originalValues.max_replicas} → {hpa.max_replicas}
                                  </div>
                                )}
                              </div>
                            </div>

                            <div className="flex gap-1 ml-2">
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7"
                                onClick={() => setEditingHPA(hpa)}
                              >
                                <Edit2 className="h-3 w-3" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 text-destructive hover:text-destructive"
                                onClick={() => staging.removeHPAFromStaging(hpa.cluster, hpa.namespace, hpa.name)}
                              >
                                <Trash2 className="h-3 w-3" />
                              </Button>
                            </div>
                          </div>
                        </CardContent>
                      </Card>
                    ))}
                  </div>
                </div>
              )}

              {/* Node Pools Section */}
              {staging.stagedNodePools.length > 0 && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="bg-green-50 dark:bg-green-950">
                      {staging.stagedNodePools.length} Node Pools
                    </Badge>
                  </div>

                  <div className="space-y-2">
                    {staging.stagedNodePools.map((nodePool) => (
                      <Card
                        key={`${nodePool.cluster_name}-${nodePool.name}`}
                        className="border-l-4 border-l-green-500"
                      >
                        <CardContent className="p-3">
                          <div className="flex items-start justify-between">
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-2 mb-1">
                                <span className="font-semibold truncate">{nodePool.name}</span>
                                {nodePool.isModified && (
                                  <Badge variant="secondary" className="text-xs">
                                    Modificado
                                  </Badge>
                                )}
                              </div>
                              <div className="text-xs text-muted-foreground space-y-0.5">
                                <div>🎯 {nodePool.cluster_name}</div>
                                <div>📁 {nodePool.resource_group}</div>
                                {nodePool.isModified && (
                                  <div className="mt-1 text-xs font-mono">
                                    Nodes: {nodePool.originalValues.node_count} → {nodePool.node_count}
                                    {nodePool.autoscaling_enabled && ` (${nodePool.min_node_count}-${nodePool.max_node_count})`}
                                  </div>
                                )}
                              </div>
                            </div>

                            <div className="flex gap-1 ml-2">
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7"
                                onClick={() => setEditingNodePool(nodePool)}
                              >
                                <Edit2 className="h-3 w-3" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 text-destructive hover:text-destructive"
                                onClick={() => staging.removeNodePoolFromStaging(nodePool.cluster_name, nodePool.name)}
                              >
                                <Trash2 className="h-3 w-3" />
                              </Button>
                            </div>
                          </div>
                        </CardContent>
                      </Card>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </ScrollArea>
        </CardContent>
      </Card>

      {/* HPA Edit Modal */}
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

      {/* Node Pool Edit Modal */}
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
