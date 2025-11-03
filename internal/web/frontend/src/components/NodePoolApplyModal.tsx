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
import { Separator } from "@/components/ui/separator";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Loader2,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  TrendingUp,
  TrendingDown,
  Trash2,
  ArrowRight,
  MoreVertical,
  Edit
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { apiClient } from "@/lib/api/client";
import type { NodePool } from "@/lib/api/types";
import { toast } from "sonner";
import { useStaging } from "@/contexts/StagingContext";
import { guardVPNOperation } from "@/lib/vpnGuard";

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
  checkVPN?: () => Promise<boolean>; // Função de validação VPN (opcional)
}

type NodePoolStatus = 'idle' | 'applying' | 'success' | 'error';

interface NodePoolApplyState {
  status: NodePoolStatus;
  message?: string;
}

export const NodePoolApplyModal = ({
  open,
  onOpenChange,
  modifiedNodePools,
  onApplied,
  onClear,
  checkVPN,
}: NodePoolApplyModalProps) => {
  const staging = useStaging();
  const [isApplying, setIsApplying] = useState(false);
  const [applyingIndividual, setApplyingIndividual] = useState<string | null>(null);
  const [nodePoolStates, setNodePoolStates] = useState<Record<string, NodePoolApplyState>>({});
  const [removedKeys, setRemovedKeys] = useState<Set<string>>(new Set());
  const [refreshCounter, setRefreshCounter] = useState(0);

  // Estados para edição inline
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editNodeCount, setEditNodeCount] = useState("");
  const [editMinNodes, setEditMinNodes] = useState("");
  const [editMaxNodes, setEditMaxNodes] = useState("");
  const [editAutoscaling, setEditAutoscaling] = useState(false);

  // Detectar se há node pools com ordem sequencial
  const hasSequentialPools = modifiedNodePools.some((np) => np.order !== undefined);
  const sequentialPools = modifiedNodePools
    .filter((np) => np.order !== undefined)
    .sort((a, b) => (a.order || 0) - (b.order || 0));
  const normalPools = modifiedNodePools.filter((np) => np.order === undefined);

  const handleApplyIndividual = async (key: string, current: NodePool) => {
    setApplyingIndividual(key);
    setNodePoolStates(prev => ({ ...prev, [key]: { status: 'applying' } }));

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

      setNodePoolStates(prev => ({
        ...prev,
        [key]: { status: 'success', message: 'Aplicado com sucesso' }
      }));
      toast.success(`✅ Node Pool ${current.name} aplicado com sucesso`);
      onApplied?.();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
      setNodePoolStates(prev => ({
        ...prev,
        [key]: { status: 'error', message: errorMessage }
      }));
      toast.error(`❌ Erro ao aplicar ${current.name}`);
    } finally {
      setApplyingIndividual(null);
    }
  };

  const handleOpenEdit = (key: string, current: NodePool) => {
    setEditingKey(key);
    setEditNodeCount(String(current.node_count ?? 0));
    setEditMinNodes(String(current.min_node_count ?? 0));
    setEditMaxNodes(String(current.max_node_count ?? 1));
    setEditAutoscaling(current.autoscaling_enabled ?? false);
  };

  const handleSaveEdit = () => {
    // Validar campos
    const nodeCount = parseInt(editNodeCount);
    const minNodes = parseInt(editMinNodes);
    const maxNodes = parseInt(editMaxNodes);

    if (isNaN(nodeCount) || nodeCount < 0) {
      toast.error("Node Count deve ser um número válido");
      return;
    }

    if (editAutoscaling) {
      if (isNaN(minNodes) || minNodes < 0) {
        toast.error("Min Nodes deve ser um número válido");
        return;
      }
      if (isNaN(maxNodes) || maxNodes < minNodes) {
        toast.error("Max Nodes deve ser maior ou igual a Min Nodes");
        return;
      }
    }

    // Encontrar o node pool sendo editado
    const nodePoolToEdit = modifiedNodePools.find(({ key }) => key === editingKey);
    if (!nodePoolToEdit) return;

    const { current } = nodePoolToEdit;

    // Atualizar no staging
    const updates: Partial<NodePool> = {
      node_count: nodeCount,
      min_node_count: minNodes,
      max_node_count: maxNodes,
      autoscaling_enabled: editAutoscaling,
    };

    staging?.updateNodePoolInStaging(
      current.cluster_name,
      current.name,
      updates
    );

    toast.success(`Node Pool ${current.name} atualizado no staging`);

    // Fechar modal de edição e forçar refresh
    setEditingKey(null);
    setRefreshCounter(prev => prev + 1);
  };

  const handleRemoveIndividual = (key: string, current: NodePool) => {
    // Remover do staging
    staging?.removeNodePoolFromStaging(
      current.cluster_name,
      current.name
    );

    // Adicionar ao set de removidos
    setRemovedKeys(prev => new Set(prev).add(key));

    toast.info(`Node Pool ${current.name} removido da lista`);
  };

  const handleApplyAll = async () => {
    // Validar VPN antes de aplicar (se função fornecida)
    if (checkVPN) {
      const vpnOk = await guardVPNOperation(checkVPN, 'Aplicar Alterações de Node Pools');
      if (!vpnOk) {
        return; // Bloquear operação se VPN desconectada
      }
    }

    setIsApplying(true);

    try {
      if (hasSequentialPools && sequentialPools.length > 0) {
        // Execução SEQUENCIAL via endpoint dedicado
        for (const { key, current } of sequentialPools) {
          setNodePoolStates(prev => ({ ...prev, [key]: { status: 'applying' } }));
        }

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
              setNodePoolStates(prev => ({
                ...prev,
                [matchingPool.key]: {
                  status: result.success ? 'success' : 'error',
                  message: result.message || (result.success ? 'Aplicado com sucesso' : 'Erro na aplicação'),
                }
              }));
            }
          }
        } else {
          // Erro geral no endpoint
          sequentialPools.forEach((np) => {
            setNodePoolStates(prev => ({
              ...prev,
              [np.key]: {
                status: 'error',
                message: response.error?.message || 'Erro ao executar sequencialmente',
              }
            }));
          });
        }
      }

      // Executar node pools NORMAIS (sem ordem) em paralelo
      for (const { key, current } of normalPools) {
        setNodePoolStates(prev => ({ ...prev, [key]: { status: 'applying' } }));

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

          setNodePoolStates(prev => ({
            ...prev,
            [key]: { status: 'success', message: 'Aplicado com sucesso' }
          }));
        } catch (error) {
          const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
          setNodePoolStates(prev => ({
            ...prev,
            [key]: { status: 'error', message: errorMessage }
          }));
        }
      }

      setIsApplying(false);
      onApplied?.();

      const successCount = Object.values(nodePoolStates).filter(s => s.status === 'success').length;
      const errorCount = Object.values(nodePoolStates).filter(s => s.status === 'error').length;

      if (errorCount === 0) {
        toast.success(`✅ ${successCount} node pool(s) aplicado(s) com sucesso`);
      } else {
        toast.error(`⚠️ ${errorCount} erro(s) ao aplicar node pools`);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao aplicar alterações");
      setIsApplying(false);
    }
  };

  const handleClose = () => {
    if (!isApplying && !applyingIndividual) {
      onOpenChange(false);
      setNodePoolStates({});
    }
  };

  const renderStatusIcon = (status: NodePoolStatus) => {
    switch (status) {
      case 'applying':
        return <Loader2 className="w-4 h-4 animate-spin text-blue-500" />;
      case 'success':
        return <CheckCircle2 className="w-4 h-4 text-green-500" />;
      case 'error':
        return <XCircle className="w-4 h-4 text-red-500" />;
      default:
        return null;
    }
  };

  const renderChange = (label: string, before: any, after: any) => {
    // Se os valores são iguais, não mostrar
    if (before === after) {
      return null;
    }

    return (
      <div className="flex items-center gap-2 text-sm py-1">
        <span className="text-muted-foreground min-w-[140px]">{label}:</span>
        <span className="text-red-500 line-through">{before ?? "—"}</span>
        <ArrowRight className="w-4 h-4 text-muted-foreground" />
        <span className="text-green-500 font-medium">{after ?? "—"}</span>
      </div>
    );
  };

  const renderNodePoolChanges = (current: NodePool, original: NodePool) => {
    const changes = [];

    if (current.autoscaling_enabled !== original.autoscaling_enabled) {
      changes.push(
        renderChange(
          "Autoscaling",
          original.autoscaling_enabled ? "Ativado" : "Desativado",
          current.autoscaling_enabled ? "Ativado" : "Desativado"
        )
      );
    }

    if (current.node_count !== original.node_count) {
      changes.push(renderChange("Node Count", original.node_count, current.node_count));
    }

    if (current.min_node_count !== original.min_node_count) {
      changes.push(renderChange("Min Nodes", original.min_node_count, current.min_node_count));
    }

    if (current.max_node_count !== original.max_node_count) {
      changes.push(renderChange("Max Nodes", original.max_node_count, current.max_node_count));
    }

    return changes.filter((c) => c !== null);
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
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
          {staging?.loadedSessionInfo && (
            <div className="mt-3 p-3 bg-blue-50 dark:bg-blue-950 border border-blue-200 dark:border-blue-800 rounded-md">
              <div className="flex items-center gap-2 text-sm">
                <span className="font-semibold text-blue-700 dark:text-blue-300">📂 Sessão Carregada:</span>
                <span className="text-blue-900 dark:text-blue-100">{staging.loadedSessionInfo.sessionName}</span>
              </div>
              <div className="flex items-center gap-2 text-sm mt-1">
                <span className="font-semibold text-blue-700 dark:text-blue-300">🎯 Cluster{staging.loadedSessionInfo.clusters.length > 1 ? 's' : ''}:</span>
                <span className="text-blue-900 dark:text-blue-100">{staging.loadedSessionInfo.clusters.join(', ')}</span>
              </div>
            </div>
          )}
        </DialogHeader>

        <ScrollArea className="max-h-[50vh] pr-4">
          <div className="space-y-4">
            {modifiedNodePools.map(({ key, current, original, order }) => {
              const changes = renderNodePoolChanges(current, original);
              const nodePoolState = nodePoolStates[key];
              const hasBeenApplied = nodePoolState && (nodePoolState.status === 'success' || nodePoolState.status === 'error');

              return (
                <div
                  key={key}
                  className="border rounded-lg p-4 space-y-2"
                >
                  {/* Header with Apply Individual Button */}
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-3">
                      <div className="flex items-center gap-2">
                        {order !== undefined && (
                          <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950 text-blue-600 dark:text-blue-400">
                            *{order}
                          </Badge>
                        )}
                        <h4 className="font-semibold text-base">{current.name}</h4>
                        <Badge variant={current.is_system_pool ? "default" : "secondary"}>
                          {current.is_system_pool ? "System" : "User"}
                        </Badge>
                      </div>
                      {nodePoolState && (
                        <div className="flex items-center gap-1.5">
                          {renderStatusIcon(nodePoolState.status)}
                          {nodePoolState.message && nodePoolState.status !== 'applying' && (
                            <span className="text-xs text-muted-foreground">
                              {nodePoolState.message}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleApplyIndividual(key, current)}
                        disabled={isApplying || applyingIndividual !== null}
                        className="shrink-0"
                      >
                        {applyingIndividual === key ? (
                          <>
                            <Loader2 className="w-3 h-3 mr-1 animate-spin" />
                            Aplicando...
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="w-3 h-3 mr-1" />
                            {hasBeenApplied ? 'Re-aplicar' : 'Aplicar'}
                          </>
                        )}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={isApplying || applyingIndividual !== null}
                            className="h-8 w-8 p-0"
                          >
                            <MoreVertical className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onClick={() => handleOpenEdit(key, current)}
                            disabled={isApplying || applyingIndividual !== null}
                          >
                            <Edit className="h-4 w-4 mr-2" />
                            Editar Conteúdo
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            onClick={() => handleRemoveIndividual(key, current)}
                            disabled={isApplying || applyingIndividual !== null}
                            className="text-destructive focus:text-destructive"
                          >
                            <Trash2 className="h-4 w-4 mr-2" />
                            Remover da Lista
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </div>

                  <p className="text-sm text-muted-foreground">
                    {current.cluster_name} • {current.vm_size}
                  </p>

                  <Separator />

                  {/* Changes List with before/after arrows */}
                  <div className="space-y-1">
                    {changes.length > 0 ? (
                      changes
                    ) : (
                      <div className="text-sm text-muted-foreground italic">
                        Nenhuma mudança visível (valores idênticos)
                      </div>
                    )}
                  </div>

                  {/* Scaling Mode Indicator */}
                  <div className="flex items-center gap-2 text-sm pt-2">
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
                  {nodePoolState?.status === 'error' && nodePoolState.message && (
                    <div className="flex items-start gap-2 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/20 p-2 rounded mt-2">
                      <AlertTriangle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                      <span>{nodePoolState.message}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </ScrollArea>

        <DialogFooter>
          <div className="flex justify-between w-full">
            <Button
              variant="destructive"
              onClick={() => {
                staging?.clearStaging();
                toast.info("Staging limpo com sucesso");
                handleClose();
                onClear?.();
              }}
              disabled={isApplying || applyingIndividual !== null}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              Cancelar e Limpar
            </Button>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={handleClose}
                disabled={isApplying || applyingIndividual !== null}
              >
                {isApplying || applyingIndividual !== null ? "Aguarde..." : "Fechar"}
              </Button>
              <Button
                onClick={handleApplyAll}
                disabled={isApplying || applyingIndividual !== null}
                className="bg-success hover:bg-success/90"
              >
                {isApplying ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Aplicando todos...
                  </>
                ) : (
                  `Aplicar ${modifiedNodePools.length} Node Pool(s)`
                )}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>

      {/* Modal de Edição Inline */}
      {editingKey && (() => {
        const nodePoolToEdit = modifiedNodePools.find(({ key }) => key === editingKey);
        if (!nodePoolToEdit) return null;
        const { current } = nodePoolToEdit;

        return (
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>Editar Node Pool</DialogTitle>
              <DialogDescription>
                Editando: {current.name} ({current.cluster_name})
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4">
              {/* Autoscaling Toggle */}
              <div className="flex items-center gap-2">
                <Checkbox
                  id="edit-autoscaling"
                  checked={editAutoscaling}
                  onCheckedChange={(checked) => setEditAutoscaling(checked === true)}
                />
                <Label htmlFor="edit-autoscaling" className="cursor-pointer">
                  Autoscaling Habilitado
                </Label>
              </div>

              {/* Node Count (Manual Mode) */}
              {!editAutoscaling && (
                <div className="space-y-2">
                  <Label htmlFor="edit-node-count">Node Count</Label>
                  <Input
                    id="edit-node-count"
                    type="text"
                    value={editNodeCount}
                    onChange={(e) => setEditNodeCount(e.target.value)}
                    onClick={(e) => e.currentTarget.select()}
                    onFocus={(e) => e.currentTarget.select()}
                  />
                </div>
              )}

              {/* Min/Max Nodes (Autoscaling Mode) */}
              {editAutoscaling && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="edit-min-nodes">Min Nodes</Label>
                    <Input
                      id="edit-min-nodes"
                      type="text"
                      value={editMinNodes}
                      onChange={(e) => setEditMinNodes(e.target.value)}
                      onClick={(e) => e.currentTarget.select()}
                      onFocus={(e) => e.currentTarget.select()}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="edit-max-nodes">Max Nodes</Label>
                    <Input
                      id="edit-max-nodes"
                      type="text"
                      value={editMaxNodes}
                      onChange={(e) => setEditMaxNodes(e.target.value)}
                      onClick={(e) => e.currentTarget.select()}
                      onFocus={(e) => e.currentTarget.select()}
                    />
                  </div>
                </>
              )}
            </div>

            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => setEditingKey(null)}
              >
                Cancelar
              </Button>
              <Button onClick={handleSaveEdit}>
                Salvar Alterações
              </Button>
            </DialogFooter>
          </DialogContent>
        );
      })()}
    </Dialog>
  );
};
