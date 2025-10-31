import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckCircle, XCircle, ArrowRight, Loader2, AlertCircle, Trash2, MoreVertical, Edit } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import type { HPA } from "@/lib/api/types";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { useStaging } from "@/contexts/StagingContext";

interface ApplyAllModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modifiedHPAs: Array<{ key: string; current: HPA; original: HPA }>;
  onApplied: () => void;
  onClear: () => void;
}

type HPAStatus = 'idle' | 'applying' | 'success' | 'error' | 'warning';

interface HPAApplyState {
  status: HPAStatus;
  message?: string;
}

export const ApplyAllModal = ({
  open,
  onOpenChange,
  modifiedHPAs,
  onApplied,
  onClear,
}: ApplyAllModalProps) => {
  const staging = useStaging();
  const [isApplying, setIsApplying] = useState(false);
  const [applyingIndividual, setApplyingIndividual] = useState<string | null>(null);
  const [hpaStates, setHpaStates] = useState<Record<string, HPAApplyState>>({});
  const [removedKeys, setRemovedKeys] = useState<Set<string>>(new Set());

  // Edit modal state
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editMinReplicas, setEditMinReplicas] = useState(0);
  const [editMaxReplicas, setEditMaxReplicas] = useState(1);
  const [editTargetCPU, setEditTargetCPU] = useState<number | undefined>(undefined);
  const [editTargetMemory, setEditTargetMemory] = useState<number | undefined>(undefined);
  const [editTargetCpuRequest, setEditTargetCpuRequest] = useState("");
  const [editTargetCpuLimit, setEditTargetCpuLimit] = useState("");
  const [editTargetMemoryRequest, setEditTargetMemoryRequest] = useState("");
  const [editTargetMemoryLimit, setEditTargetMemoryLimit] = useState("");
  const [editPerformRollout, setEditPerformRollout] = useState(false);
  const [editPerformDaemonSetRollout, setEditPerformDaemonSetRollout] = useState(false);
  const [editPerformStatefulSetRollout, setEditPerformStatefulSetRollout] = useState(false);

  const handleOpenEdit = (key: string, current: HPA) => {
    setEditingKey(key);
    setEditMinReplicas(current.min_replicas ?? 0);
    setEditMaxReplicas(current.max_replicas ?? 1);
    setEditTargetCPU(current.target_cpu ?? undefined);
    setEditTargetMemory(current.target_memory ?? undefined);
    setEditTargetCpuRequest(current.target_cpu_request ?? "");
    setEditTargetCpuLimit(current.target_cpu_limit ?? "");
    setEditTargetMemoryRequest(current.target_memory_request ?? "");
    setEditTargetMemoryLimit(current.target_memory_limit ?? "");
    setEditPerformRollout(current.perform_rollout ?? false);
    setEditPerformDaemonSetRollout(current.perform_daemonset_rollout ?? false);
    setEditPerformStatefulSetRollout(current.perform_statefulset_rollout ?? false);
  };

  const handleSaveEdit = () => {
    if (!editingKey) return;

    // Validate
    if (editMinReplicas > editMaxReplicas) {
      toast.error("Min Replicas não pode ser maior que Max Replicas");
      return;
    }
    if (editTargetCPU !== undefined && (editTargetCPU < 1 || editTargetCPU > 100)) {
      toast.error("Target CPU deve estar entre 1 e 100%");
      return;
    }
    if (editTargetMemory !== undefined && (editTargetMemory < 1 || editTargetMemory > 100)) {
      toast.error("Target Memory deve estar entre 1 e 100%");
      return;
    }

    // Find the HPA being edited
    const hpaToEdit = modifiedHPAs.find(({ key }) => key === editingKey);
    if (!hpaToEdit) return;

    // Update in staging
    const updates: Partial<HPA> = {
      min_replicas: editMinReplicas,
      max_replicas: editMaxReplicas,
      target_cpu: editTargetCPU ?? null,
      target_memory: editTargetMemory ?? null,
      target_cpu_request: editTargetCpuRequest || undefined,
      target_cpu_limit: editTargetCpuLimit || undefined,
      target_memory_request: editTargetMemoryRequest || undefined,
      target_memory_limit: editTargetMemoryLimit || undefined,
      perform_rollout: editPerformRollout,
      perform_daemonset_rollout: editPerformDaemonSetRollout,
      perform_statefulset_rollout: editPerformStatefulSetRollout,
    };

    staging?.updateHPAInStaging(
      hpaToEdit.current.cluster,
      hpaToEdit.current.namespace,
      hpaToEdit.current.name,
      updates
    );

    toast.success(`HPA ${hpaToEdit.current.name} atualizado no staging`);

    // Close modal - parent component will re-render with updated staging data
    setEditingKey(null);

    // Force parent to refresh modified HPAs list
    // This triggers a re-render with the new staging data
    onOpenChange(false);
    setTimeout(() => onOpenChange(true), 50);
  };

  const handleRemoveIndividual = (key: string, current: HPA) => {
    // Remove from staging
    staging?.removeHPAFromStaging(current.cluster, current.namespace, current.name);

    // Add to removed set
    setRemovedKeys(prev => new Set(prev).add(key));

    toast.info(`HPA ${current.name} removido da lista`);
  };

  const handleApplyIndividual = async (key: string, current: HPA) => {
    setApplyingIndividual(key);
    setHpaStates(prev => ({ ...prev, [key]: { status: 'applying' } }));

    try {
      // Adicionar sufixo -admin ao nome do cluster para a API
      const clusterWithAdmin = current.cluster.endsWith('-admin') 
        ? current.cluster 
        : `${current.cluster}-admin`;
      
      // Update the HPA object's cluster property to match
      const hpaWithCorrectCluster = {
        ...current,
        cluster: clusterWithAdmin
      };
      
      await apiClient.updateHPA(
        clusterWithAdmin,
        current.namespace,
        current.name,
        hpaWithCorrectCluster
      );

      setHpaStates(prev => ({ ...prev, [key]: { status: 'success', message: 'Aplicado com sucesso' } }));
      toast.success(`✅ HPA ${current.name} aplicado com sucesso`);
      onApplied();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
      setHpaStates(prev => ({ ...prev, [key]: { status: 'error', message: errorMessage } }));
      toast.error(`❌ Erro ao aplicar ${current.name}`);
    } finally {
      setApplyingIndividual(null);
    }
  };

  const handleApplyAll = async () => {
    setIsApplying(true);

    for (const { key, current } of modifiedHPAs) {
      setHpaStates(prev => ({ ...prev, [key]: { status: 'applying' } }));
      
      try {
        // Adicionar sufixo -admin ao nome do cluster para a API
        const clusterWithAdmin = current.cluster.endsWith('-admin') 
          ? current.cluster 
          : `${current.cluster}-admin`;
        
        // Update the HPA object's cluster property to match
        const hpaWithCorrectCluster = {
          ...current,
          cluster: clusterWithAdmin
        };
        
        await apiClient.updateHPA(
          clusterWithAdmin,
          current.namespace,
          current.name,
          hpaWithCorrectCluster
        );
        
        setHpaStates(prev => ({ ...prev, [key]: { status: 'success', message: 'Aplicado com sucesso' } }));
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
        setHpaStates(prev => ({ ...prev, [key]: { status: 'error', message: errorMessage } }));
      }
    }

    setIsApplying(false);
    onApplied();
    
    const successCount = Object.values(hpaStates).filter(s => s.status === 'success').length;
    const errorCount = Object.values(hpaStates).filter(s => s.status === 'error').length;
    
    if (errorCount === 0) {
      toast.success(`✅ ${successCount} HPA(s) aplicado(s) com sucesso`);
    } else {
      toast.error(`⚠️ ${errorCount} erro(s) ao aplicar HPAs`);
    }
  };

  const handleClose = () => {
    if (!isApplying && !applyingIndividual) {
      onOpenChange(false);
      setHpaStates({});
    }
  };

  const renderStatusIcon = (status: HPAStatus) => {
    switch (status) {
      case 'applying':
        return <Loader2 className="w-4 h-4 animate-spin text-blue-500" />;
      case 'success':
        return <CheckCircle className="w-4 h-4 text-green-500" />;
      case 'error':
        return <XCircle className="w-4 h-4 text-red-500" />;
      case 'warning':
        return <AlertCircle className="w-4 h-4 text-yellow-500" />;
      default:
        return null;
    }
  };

  const renderChange = (label: string, before: any, after: any) => {
    // Normalizar valores undefined, null e string vazia para null
    const normalizedBefore = (before === undefined || before === null || before === "") ? null : before;
    const normalizedAfter = (after === undefined || after === null || after === "") ? null : after;

    // Se ambos são null/undefined/vazio, não mostrar
    if (normalizedBefore === null && normalizedAfter === null) {
      return null;
    }

    // Se os valores são iguais, não mostrar
    if (normalizedBefore === normalizedAfter) {
      return null;
    }

    return (
      <div className="flex items-center gap-2 text-sm py-1">
        <span className="text-muted-foreground min-w-[140px]">{label}:</span>
        <span className="text-red-500 line-through">{normalizedBefore ?? "—"}</span>
        <ArrowRight className="w-4 h-4 text-muted-foreground" />
        <span className="text-green-500 font-medium">{normalizedAfter ?? "—"}</span>
      </div>
    );
  };

  const renderHPAChanges = (current: HPA, original: HPA) => {
    const changes = [];

    // Sempre mostrar min/max replicas e targets que existem
    if (current.min_replicas !== undefined || original.min_replicas !== undefined) {
      changes.push(renderChange("Min Replicas", original.min_replicas, current.min_replicas));
    }
    if (current.max_replicas !== undefined || original.max_replicas !== undefined) {
      changes.push(renderChange("Max Replicas", original.max_replicas, current.max_replicas));
    }
    if (current.target_cpu !== undefined || original.target_cpu !== undefined) {
      changes.push(renderChange("Target CPU (%)", original.target_cpu, current.target_cpu));
    }
    if (current.target_memory !== undefined || original.target_memory !== undefined) {
      changes.push(renderChange("Target Memory (%)", original.target_memory, current.target_memory));
    }
    
    // Só mostrar recursos se pelo menos um dos valores existir
    if (current.target_cpu_request || original.target_cpu_request) {
      changes.push(renderChange("CPU Request", original.target_cpu_request, current.target_cpu_request));
    }
    if (current.target_cpu_limit || original.target_cpu_limit) {
      changes.push(renderChange("CPU Limit", original.target_cpu_limit, current.target_cpu_limit));
    }
    if (current.target_memory_request || original.target_memory_request) {
      changes.push(renderChange("Memory Request", original.target_memory_request, current.target_memory_request));
    }
    if (current.target_memory_limit || original.target_memory_limit) {
      changes.push(renderChange("Memory Limit", original.target_memory_limit, current.target_memory_limit));
    }

    // Rollout options
    if (current.perform_rollout && !original.perform_rollout) {
      changes.push(
        <div key="rollout-deployment" className="flex items-center gap-2 text-sm py-1">
          <span className="text-muted-foreground min-w-[140px]">Rollout Deployment:</span>
          <span className="text-primary font-medium">✓ Ativado</span>
        </div>
      );
    }
    if (current.perform_daemonset_rollout && !original.perform_daemonset_rollout) {
      changes.push(
        <div key="rollout-daemonset" className="flex items-center gap-2 text-sm py-1">
          <span className="text-muted-foreground min-w-[140px]">Rollout DaemonSet:</span>
          <span className="text-primary font-medium">✓ Ativado</span>
        </div>
      );
    }
    if (current.perform_statefulset_rollout && !original.perform_statefulset_rollout) {
      changes.push(
        <div key="rollout-statefulset" className="flex items-center gap-2 text-sm py-1">
          <span className="text-muted-foreground min-w-[140px]">Rollout StatefulSet:</span>
          <span className="text-primary font-medium">✓ Ativado</span>
        </div>
      );
    }

    return changes.filter((c) => c !== null);
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-3xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle>Confirmar Alterações</DialogTitle>
          <DialogDescription>
            {modifiedHPAs.length} HPA{modifiedHPAs.length > 1 ? "s" : ""} será{modifiedHPAs.length > 1 ? "ão" : ""} modificado{modifiedHPAs.length > 1 ? "s" : ""} no cluster
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
            {modifiedHPAs
              .filter(({ key }) => !removedKeys.has(key))
              .map(({ key, current, original }) => {
              const changes = renderHPAChanges(current, original);

              // Se não há mudanças visíveis, mostrar uma mensagem mínima mas não esconder o HPA
              const hpaState = hpaStates[key];
              const hasBeenApplied = hpaState && (hpaState.status === 'success' || hpaState.status === 'error' || hpaState.status === 'warning');

              return (
                <div key={key} className="border rounded-lg p-4 space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-3">
                      <div className="flex items-center gap-2">
                        <h4 className="font-semibold text-base">{current.name}</h4>
                        <span className="text-xs text-muted-foreground">
                          {current.namespace}
                        </span>
                      </div>
                      {hpaState && (
                        <div className="flex items-center gap-1.5">
                          {renderStatusIcon(hpaState.status)}
                          {hpaState.message && hpaState.status !== 'applying' && (
                            <span className="text-xs text-muted-foreground">
                              {hpaState.message}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="default"
                        onClick={() => handleApplyIndividual(key, current)}
                        disabled={isApplying || applyingIndividual !== null}
                        className="h-8"
                      >
                        {applyingIndividual === key ? (
                          <>
                            <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                            Aplicando...
                          </>
                        ) : (
                          <>
                            <CheckCircle className="h-4 w-4 mr-2" />
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
                  <Separator />
                  <div className="space-y-1">
                    {changes.length > 0 ? (
                      changes
                    ) : (
                      <div className="text-sm text-muted-foreground italic">
                        Nenhuma mudança visível (valores idênticos)
                      </div>
                    )}
                  </div>
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
              }} 
              disabled={isApplying || applyingIndividual !== null}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              Cancelar e Limpar
            </Button>
            <div className="flex gap-2">
              <Button variant="outline" onClick={handleClose} disabled={isApplying || applyingIndividual !== null}>
                {isApplying || applyingIndividual !== null ? "Aguarde..." : "Fechar"}
              </Button>
              <Button
                onClick={handleApplyAll}
                disabled={isApplying || applyingIndividual !== null || modifiedHPAs.filter(({ key }) => !removedKeys.has(key)).length === 0}
                className="bg-success hover:bg-success/90"
              >
                {isApplying ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Aplicando todos...
                  </>
                ) : (
                  `✅ Aplicar Todos (${modifiedHPAs.filter(({ key }) => !removedKeys.has(key)).length})`
                )}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>

      {/* Edit HPA Modal */}
      <Dialog open={editingKey !== null} onOpenChange={(open) => !open && setEditingKey(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Editar HPA</DialogTitle>
            <DialogDescription>
              Modifique os valores do HPA {modifiedHPAs.find(({ key }) => key === editingKey)?.current.name}
            </DialogDescription>
          </DialogHeader>

          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="space-y-6">
              {/* HPA Config */}
              <div className="space-y-4">
                <h3 className="text-sm font-semibold">Configuração HPA</h3>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor="edit-min-replicas">Min Replicas</Label>
                    <Input
                      id="edit-min-replicas"
                      type="number"
                      value={editMinReplicas}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "") {
                          setEditMinReplicas(0);
                        } else {
                          setEditMinReplicas(parseInt(val));
                        }
                      }}
                      min="0"
                    />
                  </div>
                  <div>
                    <Label htmlFor="edit-max-replicas">Max Replicas</Label>
                    <Input
                      id="edit-max-replicas"
                      type="number"
                      value={editMaxReplicas}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "") {
                          setEditMaxReplicas(1);
                        } else {
                          setEditMaxReplicas(parseInt(val));
                        }
                      }}
                      min="1"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor="edit-target-cpu">Target CPU (%)</Label>
                    <Input
                      id="edit-target-cpu"
                      type="number"
                      value={editTargetCPU ?? ""}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "") {
                          setEditTargetCPU(undefined);
                        } else {
                          setEditTargetCPU(parseInt(val));
                        }
                      }}
                      placeholder="Ex: 80"
                      min="1"
                      max="100"
                    />
                  </div>
                  <div>
                    <Label htmlFor="edit-target-memory">Target Memory (%)</Label>
                    <Input
                      id="edit-target-memory"
                      type="number"
                      value={editTargetMemory ?? ""}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "") {
                          setEditTargetMemory(undefined);
                        } else {
                          setEditTargetMemory(parseInt(val));
                        }
                      }}
                      placeholder="Ex: 80"
                      min="1"
                      max="100"
                    />
                  </div>
                </div>
              </div>

              {/* Resources */}
              <div className="space-y-4">
                <h3 className="text-sm font-semibold">Recursos (Target Deployment)</h3>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor="edit-cpu-request">CPU Request</Label>
                    <Input
                      id="edit-cpu-request"
                      type="text"
                      value={editTargetCpuRequest}
                      onChange={(e) => setEditTargetCpuRequest(e.target.value)}
                      placeholder="Ex: 100m"
                    />
                  </div>
                  <div>
                    <Label htmlFor="edit-cpu-limit">CPU Limit</Label>
                    <Input
                      id="edit-cpu-limit"
                      type="text"
                      value={editTargetCpuLimit}
                      onChange={(e) => setEditTargetCpuLimit(e.target.value)}
                      placeholder="Ex: 200m"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor="edit-memory-request">Memory Request</Label>
                    <Input
                      id="edit-memory-request"
                      type="text"
                      value={editTargetMemoryRequest}
                      onChange={(e) => setEditTargetMemoryRequest(e.target.value)}
                      placeholder="Ex: 128Mi"
                    />
                  </div>
                  <div>
                    <Label htmlFor="edit-memory-limit">Memory Limit</Label>
                    <Input
                      id="edit-memory-limit"
                      type="text"
                      value={editTargetMemoryLimit}
                      onChange={(e) => setEditTargetMemoryLimit(e.target.value)}
                      placeholder="Ex: 256Mi"
                    />
                  </div>
                </div>
              </div>

              {/* Rollout Options */}
              <div className="space-y-4">
                <h3 className="text-sm font-semibold">Opções de Rollout</h3>
                <p className="text-xs text-muted-foreground">
                  Reinicia os pods do workload ao aplicar as alterações de recursos
                </p>

                <div className="space-y-3">
                  <div className="flex items-center space-x-2">
                    <Checkbox
                      id="edit-rollout-deployment"
                      checked={editPerformRollout}
                      onCheckedChange={(checked) => setEditPerformRollout(checked as boolean)}
                    />
                    <Label
                      htmlFor="edit-rollout-deployment"
                      className="text-sm font-normal cursor-pointer"
                    >
                      Rollout Deployment
                    </Label>
                  </div>

                  <div className="flex items-center space-x-2">
                    <Checkbox
                      id="edit-rollout-daemonset"
                      checked={editPerformDaemonSetRollout}
                      onCheckedChange={(checked) => setEditPerformDaemonSetRollout(checked as boolean)}
                    />
                    <Label
                      htmlFor="edit-rollout-daemonset"
                      className="text-sm font-normal cursor-pointer"
                    >
                      Rollout DaemonSet
                    </Label>
                  </div>

                  <div className="flex items-center space-x-2">
                    <Checkbox
                      id="edit-rollout-statefulset"
                      checked={editPerformStatefulSetRollout}
                      onCheckedChange={(checked) => setEditPerformStatefulSetRollout(checked as boolean)}
                    />
                    <Label
                      htmlFor="edit-rollout-statefulset"
                      className="text-sm font-normal cursor-pointer"
                    >
                      Rollout StatefulSet
                    </Label>
                  </div>
                </div>
              </div>
            </div>
          </ScrollArea>

          <DialogFooter>
            <Button variant="outline" onClick={() => setEditingKey(null)}>
              Cancelar
            </Button>
            <Button onClick={handleSaveEdit}>
              <CheckCircle className="h-4 w-4 mr-2" />
              Salvar Alterações
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Dialog>
  );
};
