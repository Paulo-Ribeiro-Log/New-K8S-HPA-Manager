import React, { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "sonner";
import { Session, HPAChange, NodePoolChange } from "@/lib/api/types";
import { Trash2, Save, Plus, AlertCircle, Edit2 } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";

interface EditSessionModalProps {
  session: Session | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: () => void;
}

export function EditSessionModal({ session, open, onOpenChange, onSave }: EditSessionModalProps) {
  const [editedSession, setEditedSession] = useState<Session | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [selectedHPAIndex, setSelectedHPAIndex] = useState<number | null>(null);
  const [selectedNodePoolIndex, setSelectedNodePoolIndex] = useState<number | null>(null);

  useEffect(() => {
    if (session) {
      // Deep copy da sessão para edição
      setEditedSession(JSON.parse(JSON.stringify(session)));
      setSelectedHPAIndex(null);
      setSelectedNodePoolIndex(null);
    }
  }, [session]);

  if (!editedSession) return null;

  const handleSave = async () => {
    if (!editedSession) return;

    setIsSaving(true);
    try {
      const folderQuery = editedSession.folder 
        ? `?folder=${encodeURIComponent(editedSession.folder)}` 
        : '';

      const response = await fetch(
        `/api/v1/sessions/${encodeURIComponent(editedSession.name)}${folderQuery}`,
        {
          method: 'PUT',
          headers: {
            'Authorization': `Bearer poc-token-123`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(editedSession),
        }
      );

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Erro ao salvar sessão');
      }

      toast.success('Sessão atualizada com sucesso');
      
      // Recarregar a sessão do servidor para pegar valores atualizados
      try {
        const reloadResponse = await fetch(
          `/api/v1/sessions/${encodeURIComponent(editedSession.name)}${folderQuery}`,
          {
            headers: {
              'Authorization': `Bearer poc-token-123`,
              'Content-Type': 'application/json',
            },
          }
        );
        
        if (reloadResponse.ok) {
          const reloadData = await reloadResponse.json();
          if (reloadData.success && reloadData.data) {
            // Atualizar sessão editada com dados frescos do servidor
            setEditedSession(JSON.parse(JSON.stringify(reloadData.data)));
            setSelectedHPAIndex(null);
            setSelectedNodePoolIndex(null);
            toast.success('Dados recarregados do servidor');
          }
        }
      } catch (reloadError) {
        console.warn('Erro ao recarregar sessão:', reloadError);
        // Não exibir erro para o usuário, apenas log
      }
      
      onSave();
      // Não fechar o modal para permitir edições adicionais
      // onOpenChange(false);
    } catch (error) {
      console.error('Erro ao salvar sessão:', error);
      toast.error(error instanceof Error ? error.message : 'Erro ao salvar sessão');
    } finally {
      setIsSaving(false);
    }
  };

  const updateHPAChange = (index: number, field: string, value: any) => {
    if (!editedSession) return;
    
    const updatedChanges = [...editedSession.changes];
    const change = updatedChanges[index];
    
    if (field.startsWith('new_values.')) {
      const subField = field.split('.')[1];
      change.new_values = {
        ...change.new_values,
        [subField]: value,
      };
    } else {
      (change as any)[field] = value;
    }
    
    setEditedSession({
      ...editedSession,
      changes: updatedChanges,
    });
  };

  const updateNodePoolChange = (index: number, field: string, value: any) => {
    if (!editedSession) return;
    
    const updatedChanges = [...editedSession.node_pool_changes];
    const change = updatedChanges[index];
    
    if (field.startsWith('new_values.')) {
      const subField = field.split('.')[1];
      change.new_values = {
        ...change.new_values,
        [subField]: value === '' ? 0 : (typeof value === 'string' ? parseInt(value) : value),
      };
    } else {
      (change as any)[field] = value;
    }
    
    setEditedSession({
      ...editedSession,
      node_pool_changes: updatedChanges,
    });
  };

  const deleteHPAChange = (index: number) => {
    if (!editedSession) return;
    
    const updatedChanges = editedSession.changes.filter((_, i) => i !== index);
    setEditedSession({
      ...editedSession,
      changes: updatedChanges,
    });
    setSelectedHPAIndex(null);
    toast.info('HPA removido da sessão');
  };

  const deleteNodePoolChange = (index: number) => {
    if (!editedSession) return;
    
    const updatedChanges = editedSession.node_pool_changes.filter((_, i) => i !== index);
    setEditedSession({
      ...editedSession,
      node_pool_changes: updatedChanges,
    });
    setSelectedNodePoolIndex(null);
    toast.info('Node Pool removido da sessão');
  };

  const renderHPAEditor = (change: HPAChange, index: number) => {
    const isSelected = selectedHPAIndex === index;

    return (
      <div
        key={index}
        className={`p-4 border rounded-lg transition-colors ${
          isSelected ? 'border-blue-500 bg-blue-50 dark:bg-blue-950' : 'border-gray-200 dark:border-gray-700'
        }`}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-2">
              <span className="font-semibold">{change.hpa_name}</span>
              <Badge variant="outline">{change.namespace}</Badge>
              <Badge variant="secondary">{change.cluster}</Badge>
            </div>
            {isSelected && (
              <Button
                variant="default"
                size="sm"
                className="mt-2"
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedHPAIndex(null);
                }}
              >
                OK
              </Button>
            )}
            
            {!isSelected && (
              <div className="mt-2 text-xs text-muted-foreground">
                Min: {change.new_values?.min_replicas} | Max: {change.new_values?.max_replicas}
                {change.new_values?.target_cpu && ` | CPU: ${change.new_values.target_cpu}%`}
                {change.new_values?.target_memory && ` | Mem: ${change.new_values.target_memory}%`}
              </div>
            )}

            {isSelected && (
              <div className="mt-4 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor={`min-replicas-${index}`}>Min Replicas</Label>
                    <Input
                      id={`min-replicas-${index}`}
                      type="number"
                      value={change.new_values?.min_replicas ?? 0}
                      onChange={(e) => updateHPAChange(index, 'new_values.min_replicas', parseInt(e.target.value))}
                      min={0}
                    />
                  </div>
                  <div>
                    <Label htmlFor={`max-replicas-${index}`}>Max Replicas</Label>
                    <Input
                      id={`max-replicas-${index}`}
                      type="number"
                      value={change.new_values?.max_replicas ?? 1}
                      onChange={(e) => updateHPAChange(index, 'new_values.max_replicas', parseInt(e.target.value))}
                      min={1}
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor={`cpu-target-${index}`}>Target CPU (%)</Label>
                    <Input
                      id={`cpu-target-${index}`}
                      type="number"
                      value={change.new_values?.target_cpu ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.target_cpu', e.target.value ? parseInt(e.target.value) : null)}
                      placeholder="Opcional"
                      min={1}
                      max={100}
                    />
                  </div>
                  <div>
                    <Label htmlFor={`memory-target-${index}`}>Target Memory (%)</Label>
                    <Input
                      id={`memory-target-${index}`}
                      type="number"
                      value={change.new_values?.target_memory ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.target_memory', e.target.value ? parseInt(e.target.value) : null)}
                      placeholder="Opcional"
                      min={1}
                      max={100}
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor={`cpu-request-${index}`}>CPU Request</Label>
                    <Input
                      id={`cpu-request-${index}`}
                      value={change.new_values?.cpu_request ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.cpu_request', e.target.value)}
                      placeholder="Ex: 100m"
                    />
                  </div>
                  <div>
                    <Label htmlFor={`cpu-limit-${index}`}>CPU Limit</Label>
                    <Input
                      id={`cpu-limit-${index}`}
                      value={change.new_values?.cpu_limit ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.cpu_limit', e.target.value)}
                      placeholder="Ex: 500m"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor={`memory-request-${index}`}>Memory Request</Label>
                    <Input
                      id={`memory-request-${index}`}
                      value={change.new_values?.memory_request ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.memory_request', e.target.value)}
                      placeholder="Ex: 256Mi"
                    />
                  </div>
                  <div>
                    <Label htmlFor={`memory-limit-${index}`}>Memory Limit</Label>
                    <Input
                      id={`memory-limit-${index}`}
                      value={change.new_values?.memory_limit ?? ''}
                      onChange={(e) => updateHPAChange(index, 'new_values.memory_limit', e.target.value)}
                      placeholder="Ex: 512Mi"
                    />
                  </div>
                </div>

                <div className="flex justify-end items-center">
                  <Button
                    variant="destructive"
                    size="sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      deleteHPAChange(index);
                    }}
                  >
                    <Trash2 className="w-4 h-4 mr-2" />
                    Remover HPA
                  </Button>
                </div>
              </div>
            )}
          </div>
          
          {!isSelected && (
            <Button
              variant="outline"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                setSelectedHPAIndex(index);
              }}
            >
              <Edit2 className="w-4 h-4 mr-2" />
              Editar
            </Button>
          )}
        </div>
      </div>
    );
  };

  const renderNodePoolEditor = (change: NodePoolChange, index: number) => {
    const isSelected = selectedNodePoolIndex === index;

    return (
      <div
        key={index}
        className={`p-4 border rounded-lg transition-colors ${
          isSelected ? 'border-blue-500 bg-blue-50 dark:bg-blue-950' : 'border-gray-200 dark:border-gray-700'
        }`}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-2">
              <span className="font-semibold">{change.node_pool_name}</span>
              <Badge variant="outline">{change.cluster}</Badge>
              <Badge variant="secondary">{change.resource_group}</Badge>
            </div>
            {isSelected && (
              <Button
                variant="default"
                size="sm"
                className="mt-2"
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedNodePoolIndex(null);
                }}
              >
                OK
              </Button>
            )}
            
            {!isSelected && (
              <div className="mt-2 text-xs text-muted-foreground">
                Nodes: {change.new_values.node_count}
                {change.new_values.autoscaling_enabled && ` | Autoscaling: ${change.new_values.min_node_count}-${change.new_values.max_node_count}`}
                {!change.new_values.autoscaling_enabled && ' | Autoscaling: Desabilitado'}
              </div>
            )}

            {isSelected && (
              <div className="mt-4 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label htmlFor={`node-count-${index}`}>Node Count</Label>
                    <Input
                      id={`node-count-${index}`}
                      type="number"
                      value={change.new_values.node_count}
                      onChange={(e) => updateNodePoolChange(index, 'new_values.node_count', e.target.value)}
                      min={0}
                    />
                  </div>
                  <div className="flex items-center gap-2 pt-8">
                    <input
                      type="checkbox"
                      id={`autoscaling-${index}`}
                      checked={change.new_values.autoscaling_enabled}
                      onChange={(e) => updateNodePoolChange(index, 'new_values.autoscaling_enabled', e.target.checked)}
                      className="w-4 h-4"
                    />
                    <Label htmlFor={`autoscaling-${index}`} className="cursor-pointer">
                      Autoscaling Enabled
                    </Label>
                  </div>
                </div>

                {change.new_values.autoscaling_enabled && (
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <Label htmlFor={`min-nodes-${index}`}>Min Node Count</Label>
                      <Input
                        id={`min-nodes-${index}`}
                        type="number"
                        value={change.new_values.min_node_count}
                        onChange={(e) => updateNodePoolChange(index, 'new_values.min_node_count', e.target.value)}
                        min={0}
                      />
                    </div>
                    <div>
                      <Label htmlFor={`max-nodes-${index}`}>Max Node Count</Label>
                      <Input
                        id={`max-nodes-${index}`}
                        type="number"
                        value={change.new_values.max_node_count}
                        onChange={(e) => updateNodePoolChange(index, 'new_values.max_node_count', e.target.value)}
                        min={1}
                      />
                    </div>
                  </div>
                )}

                <div className="flex justify-end items-center">
                  <Button
                    variant="destructive"
                    size="sm"
                    onClick={(e) => {
                      e.stopPropagation();
                      deleteNodePoolChange(index);
                    }}
                  >
                    <Trash2 className="w-4 h-4 mr-2" />
                    Remover Node Pool
                  </Button>
                </div>
              </div>
            )}
          </div>
          
          {!isSelected && (
            <Button
              variant="outline"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                setSelectedNodePoolIndex(index);
              }}
            >
              <Edit2 className="w-4 h-4 mr-2" />
              Editar
            </Button>
          )}
        </div>
      </div>
    );
  };

  const hpaCount = editedSession.changes.length;
  const nodePoolCount = editedSession.node_pool_changes.length;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Editar Sessão: {editedSession.name}</DialogTitle>
          <DialogDescription>
            Modifique os valores salvos na sessão. As alterações serão salvas no arquivo JSON.
          </DialogDescription>
        </DialogHeader>

        <Alert className="my-2">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>
            <strong>Atenção:</strong> Esta operação modifica diretamente o arquivo de sessão salvo.
            Certifique-se de revisar todas as alterações antes de salvar.
          </AlertDescription>
        </Alert>

        <Tabs defaultValue="hpas" className="flex-1 overflow-hidden flex flex-col">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="hpas">
              HPAs ({hpaCount})
            </TabsTrigger>
            <TabsTrigger value="nodepools">
              Node Pools ({nodePoolCount})
            </TabsTrigger>
          </TabsList>

          <TabsContent value="hpas" className="flex-1 overflow-hidden">
            <ScrollArea className="h-[500px] pr-4">
              {hpaCount === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  Nenhum HPA nesta sessão
                </div>
              ) : (
                <div className="space-y-3">
                  {editedSession.changes.map((change, index) => renderHPAEditor(change, index))}
                </div>
              )}
            </ScrollArea>
          </TabsContent>

          <TabsContent value="nodepools" className="flex-1 overflow-hidden">
            <ScrollArea className="h-[500px] pr-4">
              {nodePoolCount === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  Nenhum Node Pool nesta sessão
                </div>
              ) : (
                <div className="space-y-3">
                  {editedSession.node_pool_changes.map((change, index) => renderNodePoolEditor(change, index))}
                </div>
              )}
            </ScrollArea>
          </TabsContent>
        </Tabs>

        <DialogFooter className="mt-4">
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isSaving}
          >
            Fechar
          </Button>
          <Button onClick={handleSave} disabled={isSaving}>
            {isSaving ? (
              <>Salvando...</>
            ) : (
              <>
                <Save className="w-4 h-4 mr-2" />
                Salvar e Recarregar
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
