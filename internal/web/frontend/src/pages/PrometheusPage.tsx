import React, { useState } from 'react';
import { Input } from '@/components/ui/input';
import { Loader2, Activity, Search } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { SplitView } from '@/components/SplitView';
import { PrometheusListItem } from '@/components/PrometheusListItem';
import { PrometheusEditor } from '@/components/PrometheusEditor';
import { usePrometheusResources } from '@/hooks/useAPI';
import type { PrometheusResource } from '@/lib/api/types';

interface PrometheusPageProps {
  selectedCluster: string;
}

export function PrometheusPage({ selectedCluster }: PrometheusPageProps) {
  const [selectedResource, setSelectedResource] = useState<PrometheusResource | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const { data: resources = [], isLoading, error, refetch } = usePrometheusResources(selectedCluster);

  // Atualizar selectedResource quando resources mudar (após refetch)
  React.useEffect(() => {
    if (selectedResource && resources.length > 0) {
      const updated = resources.find(
        res => res.name === selectedResource.name &&
               res.namespace === selectedResource.namespace &&
               res.type === selectedResource.type
      );
      if (updated) {
        setSelectedResource(updated);
      }
    }
  }, [resources]);

  // Filter resources based on search query (name, namespace, or component)
  const filteredResources = resources.filter(resource =>
    resource.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    resource.namespace.toLowerCase().includes(searchQuery.toLowerCase()) ||
    resource.component.toLowerCase().includes(searchQuery.toLowerCase())
  );

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

  if (error) {
    return (
      <div className="p-4">
        <Alert variant="destructive">
          <Activity className="h-4 w-4" />
          <AlertDescription>
            Erro ao carregar recursos: {String(error)}
            <Button variant="outline" size="sm" className="ml-2" onClick={() => refetch()}>
              Tentar novamente
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <SplitView
      leftPanel={{
        title: "Available Prometheus Resources",
        content: isLoading ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <Loader2 className="h-8 w-8 animate-spin mr-2" />
            Loading Prometheus resources...
          </div>
        ) : resources.length === 0 ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <div className="text-center">
              <Activity className="h-12 w-12 mx-auto mb-4 opacity-20" />
              <p className="text-sm">Nenhum recurso do Prometheus encontrado no cluster</p>
              <p className="text-xs mt-2 opacity-70">
                Procurando por deployments, statefulsets e daemonsets com "prometheus" no nome
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
                  aria-label="Limpar busca do Prometheus"
                >
                  ×
                </button>
              )}
              <Input
                type="text"
                placeholder="Buscar por nome, namespace ou componente..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-10 pr-8"
              />
            </div>

            {/* Resources list */}
            <div className="space-y-2">
              {filteredResources.length === 0 ? (
                <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                  Nenhum recurso encontrado para "{searchQuery}"
                </div>
              ) : (
                filteredResources.map((resource) => (
                  <PrometheusListItem
                    key={`${resource.type}-${resource.namespace}-${resource.name}`}
                    name={resource.name}
                    namespace={resource.namespace}
                    type={resource.type}
                    component={resource.component}
                    replicas={resource.replicas}
                    cpuRequest={resource.current_cpu_request}
                    memoryRequest={resource.current_memory_request}
                    isSelected={
                      selectedResource?.name === resource.name &&
                      selectedResource?.namespace === resource.namespace &&
                      selectedResource?.type === resource.type
                    }
                    onClick={() => setSelectedResource(resource)}
                  />
                ))
              )}
            </div>
          </div>
        ),
      }}
      rightPanel={{
        title: "Prometheus Resource Editor",
        content: (
          <PrometheusEditor
            resource={selectedResource}
            selectedCluster={selectedCluster}
            onRefetch={refetch}
          />
        ),
      }}
    />
  );
}
