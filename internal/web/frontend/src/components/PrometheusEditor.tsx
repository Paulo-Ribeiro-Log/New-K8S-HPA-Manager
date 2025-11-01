import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, Activity, Save, X, RefreshCcw } from "lucide-react";
import type { PrometheusResource } from "@/lib/api/types";
import { useUpdatePrometheusResource } from "@/hooks/useAPI";
import { toast } from "sonner";

interface PrometheusEditorProps {
  resource: PrometheusResource | null;
  selectedCluster: string;
  onRefetch: () => void;
}

export const PrometheusEditor = ({ resource, selectedCluster, onRefetch }: PrometheusEditorProps) => {
  const [isEditing, setIsEditing] = useState(false);
  const [isRollingOut, setIsRollingOut] = useState(false);
  const [editedValues, setEditedValues] = useState<{
    replicas: number;
    cpu_request: string;
    memory_request: string;
    cpu_limit: string;
    memory_limit: string;
  } | null>(null);

  const updateResourceMutation = useUpdatePrometheusResource();

  if (!resource) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
        <Activity className="w-16 h-16 mb-4 opacity-20" />
        <p className="text-sm">Selecione um recurso Prometheus para editar</p>
      </div>
    );
  }

  const handleEdit = () => {
    setEditedValues({
      replicas: resource.replicas,
      cpu_request: resource.current_cpu_request || '',
      memory_request: resource.current_memory_request || '',
      cpu_limit: resource.current_cpu_limit || '',
      memory_limit: resource.current_memory_limit || '',
    });
    setIsEditing(true);
  };

  const handleCancel = () => {
    setEditedValues(null);
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!editedValues) return;

    try {
      await updateResourceMutation.mutateAsync({
        cluster: selectedCluster,
        namespace: resource.namespace,
        type: resource.type.toLowerCase(),
        name: resource.name,
        data: {
          cpu_request: editedValues.cpu_request,
          memory_request: editedValues.memory_request,
          cpu_limit: editedValues.cpu_limit,
          memory_limit: editedValues.memory_limit,
          replicas: editedValues.replicas,
        }
      });

      toast.success("Recurso atualizado com sucesso", {
        description: `${resource.namespace}/${resource.name}`
      });

      setIsEditing(false);
      setEditedValues(null);
    } catch (error) {
      console.error("Error updating resource:", error);
      toast.error("Erro ao atualizar recurso", {
        description: error instanceof Error ? error.message : "Erro desconhecido"
      });
    }
  };

  const handleRollout = async () => {
    setIsRollingOut(true);

    try {
      const response = await fetch(
        `/api/v1/prometheus/${encodeURIComponent(selectedCluster)}/${encodeURIComponent(resource.namespace)}/${encodeURIComponent(resource.type.toLowerCase())}/${encodeURIComponent(resource.name)}/rollout`,
        {
          method: 'POST',
          headers: {
            'Authorization': 'Bearer poc-token-123',
            'Content-Type': 'application/json',
          },
        }
      );

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Erro ao executar rollout');
      }

      toast.success("Rollout executado com sucesso", {
        description: `${resource.namespace}/${resource.name}`
      });

      // Aguardar um pouco e recarregar recursos
      setTimeout(() => {
        onRefetch();
      }, 2000);
    } catch (error) {
      console.error("Error rolling out resource:", error);
      toast.error("Erro ao executar rollout", {
        description: error instanceof Error ? error.message : "Erro desconhecido"
      });
    } finally {
      setIsRollingOut(false);
    }
  };

  const getTypeIcon = (type: string) => {
    switch (type.toLowerCase()) {
      case 'deployment':
        return '🚀';
      case 'statefulset':
        return '💽';
      case 'daemonset':
        return '🔄';
      default:
        return '📦';
    }
  };

  return (
    <div className="space-y-6">
      {/* Resource Info */}
      <div className="space-y-4">
        <div>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-2xl">{getTypeIcon(resource.type)}</span>
              <div>
                <h3 className="text-lg font-semibold text-foreground">{resource.name}</h3>
                <p className="text-sm text-muted-foreground">{resource.namespace}</p>
                <p className="text-xs text-muted-foreground">{resource.component} • {resource.type}</p>
              </div>
            </div>
            {/* Rollout Button - Ao lado do título */}
            <Button
              variant="outline"
              size="sm"
              className="border-orange-300 hover:bg-orange-100 dark:border-orange-700 dark:hover:bg-orange-900/30"
              onClick={handleRollout}
              disabled={isRollingOut}
            >
              {isRollingOut ? (
                <>
                  <Loader2 className="w-4 h-4 mr-1 animate-spin" />
                  Rollout...
                </>
              ) : (
                <>
                  <RefreshCcw className="w-4 h-4 mr-1" />
                  Rollout
                </>
              )}
            </Button>
          </div>
        </div>

        {/* Current Values (Read-only) */}
        {!isEditing && (
          <>
            {resource.type !== 'DaemonSet' && (
              <div className="p-3 bg-muted/30 rounded-lg">
                <Label className="text-xs text-muted-foreground">Replicas</Label>
                <p className="text-lg font-bold text-foreground">{resource.replicas}</p>
              </div>
            )}

            <div className="grid grid-cols-2 gap-3">
              <div className="p-3 bg-muted/30 rounded-lg">
                <Label className="text-xs text-muted-foreground">CPU Request</Label>
                <p className="font-mono text-sm font-semibold">{resource.current_cpu_request || '—'}</p>
              </div>
              <div className="p-3 bg-muted/30 rounded-lg">
                <Label className="text-xs text-muted-foreground">Memory Request</Label>
                <p className="font-mono text-sm font-semibold">{resource.current_memory_request || '—'}</p>
              </div>
              <div className="p-3 bg-muted/30 rounded-lg">
                <Label className="text-xs text-muted-foreground">CPU Limit</Label>
                <p className="font-mono text-sm font-semibold">{resource.current_cpu_limit || '—'}</p>
              </div>
              <div className="p-3 bg-muted/30 rounded-lg">
                <Label className="text-xs text-muted-foreground">Memory Limit</Label>
                <p className="font-mono text-sm font-semibold">{resource.current_memory_limit || '—'}</p>
              </div>
            </div>

            {/* Usage (if available) */}
            {(resource.cpu_usage || resource.memory_usage) && (
              <div className="grid grid-cols-2 gap-3">
                {resource.cpu_usage && (
                  <div className="p-3 bg-blue-50 dark:bg-blue-950/20 rounded-lg">
                    <Label className="text-xs text-muted-foreground">CPU Usage</Label>
                    <p className="font-mono text-sm font-semibold text-blue-600">{resource.cpu_usage}</p>
                  </div>
                )}
                {resource.memory_usage && (
                  <div className="p-3 bg-blue-50 dark:bg-blue-950/20 rounded-lg">
                    <Label className="text-xs text-muted-foreground">Memory Usage</Label>
                    <p className="font-mono text-sm font-semibold text-blue-600">{resource.memory_usage}</p>
                  </div>
                )}
              </div>
            )}

            {/* Action Button - Apenas Editar */}
            <Button
              className="w-full"
              onClick={handleEdit}
            >
              <Save className="w-4 h-4 mr-2" />
              Editar Recursos
            </Button>
          </>
        )}

        {/* Editing Mode */}
        {isEditing && editedValues && (
          <div className="space-y-4 p-4 border-2 border-primary/20 rounded-lg bg-accent/30">
            {resource.type !== 'DaemonSet' && (
              <div>
                <Label htmlFor="replicas">Replicas</Label>
                <Input
                  id="replicas"
                  type="number"
                  value={editedValues.replicas || ''}
                  onChange={(e) => setEditedValues({
                    ...editedValues,
                    replicas: parseInt(e.target.value) || 0
                  })}
                  placeholder="ex: 3"
                />
              </div>
            )}

            <div>
              <Label htmlFor="cpu_request">CPU Request</Label>
              <Input
                id="cpu_request"
                value={editedValues.cpu_request}
                onChange={(e) => setEditedValues({
                  ...editedValues,
                  cpu_request: e.target.value
                })}
                placeholder="ex: 100m, 0.5, 1"
              />
            </div>

            <div>
              <Label htmlFor="memory_request">Memory Request</Label>
              <Input
                id="memory_request"
                value={editedValues.memory_request}
                onChange={(e) => setEditedValues({
                  ...editedValues,
                  memory_request: e.target.value
                })}
                placeholder="ex: 256Mi, 1Gi, 512M"
              />
            </div>

            <div>
              <Label htmlFor="cpu_limit">CPU Limit</Label>
              <Input
                id="cpu_limit"
                value={editedValues.cpu_limit}
                onChange={(e) => setEditedValues({
                  ...editedValues,
                  cpu_limit: e.target.value
                })}
                placeholder="ex: 500m, 1, 2"
              />
            </div>

            <div>
              <Label htmlFor="memory_limit">Memory Limit</Label>
              <Input
                id="memory_limit"
                value={editedValues.memory_limit}
                onChange={(e) => setEditedValues({
                  ...editedValues,
                  memory_limit: e.target.value
                })}
                placeholder="ex: 512Mi, 2Gi, 1G"
              />
            </div>

            <div className="grid grid-cols-2 gap-3 pt-4">
              <Button
                onClick={handleSave}
                disabled={updateResourceMutation.isPending}
              >
                {updateResourceMutation.isPending ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Salvando...
                  </>
                ) : (
                  <>
                    <Save className="w-4 h-4 mr-2" />
                    Salvar Alterações
                  </>
                )}
              </Button>
              <Button
                variant="outline"
                onClick={handleCancel}
                disabled={updateResourceMutation.isPending}
              >
                <X className="w-4 h-4 mr-2" />
                Cancelar
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
