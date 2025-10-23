import React, { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog';
import { Loader2, Activity, Server, Edit, AlertTriangle, Save, X, RefreshCcw } from 'lucide-react';
import { usePrometheusResources, useUpdatePrometheusResource } from '@/hooks/useAPI';
import type { PrometheusResource } from '@/lib/api/types';

interface PrometheusPageProps {
  selectedCluster: string;
}

interface EditingResource {
  name: string;
  namespace: string;
  type: string;
  component: string;
  replicas: number;
  cpu_request: string;
  memory_request: string;
  cpu_limit: string;
  memory_limit: string;
  cpu_usage?: string;
  memory_usage?: string;
}

export function PrometheusPage({ selectedCluster }: PrometheusPageProps) {
  const [selectedResources, setSelectedResources] = useState<Set<string>>(new Set());
  const [editingResource, setEditingResource] = useState<EditingResource | null>(null);
  const [isEditing, setIsEditing] = useState(false);
  const [rollingOutResource, setRollingOutResource] = useState<string | null>(null);

  const { data: resources = [], isLoading, error, refetch } = usePrometheusResources(selectedCluster);
  const updateResourceMutation = useUpdatePrometheusResource();

  if (!selectedCluster) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <Activity className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <p className="text-muted-foreground">Selecione um cluster para ver recursos do Prometheus</p>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-8 w-8 animate-spin" />
        <span className="ml-2">Carregando recursos do Prometheus...</span>
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle className="h-4 w-4" />
        <AlertDescription>
          Erro ao carregar recursos: {String(error)}
          <Button variant="outline" size="sm" className="ml-2" onClick={() => refetch()}>
            Tentar novamente
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  if (resources.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-center">
          <Activity className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <p className="text-muted-foreground">
            Nenhum recurso do Prometheus encontrado no cluster
          </p>
          <p className="text-xs text-muted-foreground mt-2">
            Procurando por deployments, statefulsets e daemonsets com "prometheus" no nome
          </p>
        </div>
      </div>
    );
  }

  const handleToggleResource = (resource: PrometheusResource) => {
    const key = `${resource.type}/${resource.name}`;
    setSelectedResources(prev => {
      const newSet = new Set(prev);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      return newSet;
    });
  };

  const handleEditResource = (resource: PrometheusResource) => {
    setEditingResource({
      name: resource.name,
      namespace: resource.namespace,
      type: resource.type,
      component: resource.component,
      replicas: resource.replicas,
      cpu_request: resource.current_cpu_request || '',
      memory_request: resource.current_memory_request || '',
      cpu_limit: resource.current_cpu_limit || '',
      memory_limit: resource.current_memory_limit || '',
      cpu_usage: resource.cpu_usage,
      memory_usage: resource.memory_usage,
    });
    setIsEditing(true);
  };

  const handleSaveResource = async () => {
    if (!editingResource) return;

    try {
      await updateResourceMutation.mutateAsync({
        cluster: selectedCluster,
        namespace: editingResource.namespace,
        type: editingResource.type.toLowerCase(),
        name: editingResource.name,
        data: {
          cpu_request: editingResource.cpu_request,
          memory_request: editingResource.memory_request,
          cpu_limit: editingResource.cpu_limit,
          memory_limit: editingResource.memory_limit,
          replicas: editingResource.replicas,
        }
      });

      setIsEditing(false);
      setEditingResource(null);
    } catch (error) {
      console.error('Error updating resource:', error);
    }
  };

  const handleRollout = async (resource: PrometheusResource) => {
    const key = `${resource.type}/${resource.name}`;
    setRollingOutResource(key);

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

      const data = await response.json();
      console.log('Rollout success:', data);

      // Aguardar um pouco e recarregar recursos para mostrar nova timestamp
      setTimeout(() => {
        refetch();
      }, 2000);
    } catch (error) {
      console.error('Error rolling out resource:', error);
      alert(`Erro ao executar rollout: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    } finally {
      setRollingOutResource(null);
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

  const getComponentBadge = (component: string) => {
    const variants: Record<string, string> = {
      'Prometheus': 'bg-red-100 text-red-800',
      'Prometheus Server': 'bg-red-100 text-red-800',
      'Grafana': 'bg-orange-100 text-orange-800',
      'Alertmanager': 'bg-yellow-100 text-yellow-800',
      'Node Exporter': 'bg-green-100 text-green-800',
      'Kube State Metrics': 'bg-blue-100 text-blue-800',
    };
    
    const className = variants[component] || 'bg-gray-100 text-gray-800';
    
    return (
      <Badge className={className}>
        {component}
      </Badge>
    );
  };

  const selectedResourcesCount = selectedResources.size;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">Recursos Prometheus</h2>
          <p className="text-muted-foreground">
            {resources.length} recursos encontrados no cluster <strong>{selectedCluster}</strong>
          </p>
        </div>
        
        {selectedResourcesCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {selectedResourcesCount} selecionados
            </span>
            <Button variant="outline" size="sm" disabled>
              <Edit className="h-4 w-4 mr-1" />
              Editar em lote (em breve)
            </Button>
          </div>
        )}
      </div>

      {/* Lista de recursos */}
      <div className="grid gap-4">
        {resources.map((resource) => {
          const key = `${resource.type}/${resource.name}`;
          const isSelected = selectedResources.has(key);

          return (
            <Card 
              key={key}
              className={`cursor-pointer transition-colors ${
                isSelected ? 'border-primary bg-accent' : 'hover:bg-accent/50'
              }`}
              onClick={() => handleToggleResource(resource)}
            >
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-3">
                    <div className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => handleToggleResource(resource)}
                        className="h-4 w-4"
                        onClick={(e) => e.stopPropagation()}
                      />
                      <span className="text-lg">{getTypeIcon(resource.type)}</span>
                      <CardTitle className="text-lg">{resource.name}</CardTitle>
                    </div>
                    <div className="flex items-center space-x-2">
                      {getComponentBadge(resource.component)}
                      <Badge variant="outline">{resource.type}</Badge>
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleRollout(resource);
                      }}
                      disabled={rollingOutResource === key}
                    >
                      {rollingOutResource === key ? (
                        <>
                          <Loader2 className="h-4 w-4 mr-1 animate-spin" />
                          Executando...
                        </>
                      ) : (
                        <>
                          <RefreshCcw className="h-4 w-4 mr-1" />
                          Rollout
                        </>
                      )}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleEditResource(resource);
                      }}
                    >
                      <Edit className="h-4 w-4 mr-1" />
                      Editar
                    </Button>
                  </div>
                </div>
              </CardHeader>
              
              <CardContent>
                <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4 text-sm">
                  {resource.type !== 'DaemonSet' && (
                    <div>
                      <div className="text-muted-foreground mb-1">Replicas</div>
                      <div className="font-semibold">{resource.replicas}</div>
                    </div>
                  )}
                  
                  <div>
                    <div className="text-muted-foreground mb-1">CPU Request</div>
                    <div className="font-mono text-xs">
                      {resource.current_cpu_request || '—'}
                    </div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">Memory Request</div>
                    <div className="font-mono text-xs">
                      {resource.current_memory_request || '—'}
                    </div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">CPU Limit</div>
                    <div className="font-mono text-xs">
                      {resource.current_cpu_limit || '—'}
                    </div>
                  </div>
                  
                  <div>
                    <div className="text-muted-foreground mb-1">Memory Limit</div>
                    <div className="font-mono text-xs">
                      {resource.current_memory_limit || '—'}
                    </div>
                  </div>
                </div>
                
                {(resource.cpu_usage || resource.memory_usage) && (
                  <div className="mt-3 pt-3 border-t">
                    <div className="grid grid-cols-2 gap-4 text-sm">
                      {resource.cpu_usage && (
                        <div>
                          <div className="text-muted-foreground mb-1">CPU Usage</div>
                          <div className="font-mono text-xs text-blue-600">{resource.cpu_usage}</div>
                        </div>
                      )}
                      {resource.memory_usage && (
                        <div>
                          <div className="text-muted-foreground mb-1">Memory Usage</div>
                          <div className="font-mono text-xs text-blue-600">{resource.memory_usage}</div>
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>

      {/* Modal de edição */}
      <Dialog open={isEditing} onOpenChange={setIsEditing}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center space-x-2">
              <Server className="h-5 w-5" />
              <span>Editar Recursos: {editingResource?.name}</span>
            </DialogTitle>
          </DialogHeader>
          
          {editingResource && (
            <div className="space-y-4">
              {editingResource.type !== 'DaemonSet' && (
                <div>
                  <Label htmlFor="replicas">Replicas</Label>
                  <Input
                    id="replicas"
                    type="number"
                    value={editingResource.replicas || ''}
                    onChange={(e) => setEditingResource({
                      ...editingResource,
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
                  value={editingResource.cpu_request}
                  onChange={(e) => setEditingResource({
                    ...editingResource,
                    cpu_request: e.target.value
                  })}
                  placeholder="ex: 100m, 0.5, 1"
                />
              </div>
              
              <div>
                <Label htmlFor="memory_request">Memory Request</Label>
                <Input
                  id="memory_request"
                  value={editingResource.memory_request}
                  onChange={(e) => setEditingResource({
                    ...editingResource,
                    memory_request: e.target.value
                  })}
                  placeholder="ex: 256Mi, 1Gi, 512M"
                />
              </div>
              
              <div>
                <Label htmlFor="cpu_limit">CPU Limit</Label>
                <Input
                  id="cpu_limit"
                  value={editingResource.cpu_limit}
                  onChange={(e) => setEditingResource({
                    ...editingResource,
                    cpu_limit: e.target.value
                  })}
                  placeholder="ex: 500m, 1, 2"
                />
              </div>
              
              <div>
                <Label htmlFor="memory_limit">Memory Limit</Label>
                <Input
                  id="memory_limit"
                  value={editingResource.memory_limit}
                  onChange={(e) => setEditingResource({
                    ...editingResource,
                    memory_limit: e.target.value
                  })}
                  placeholder="ex: 512Mi, 2Gi, 1G"
                />
              </div>
            </div>
          )}
          
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setIsEditing(false)}
              disabled={updateResourceMutation.isPending}
            >
              <X className="h-4 w-4 mr-1" />
              Cancelar
            </Button>
            <Button
              onClick={handleSaveResource}
              disabled={updateResourceMutation.isPending}
            >
              {updateResourceMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              <Save className="h-4 w-4 mr-1" />
              Salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}