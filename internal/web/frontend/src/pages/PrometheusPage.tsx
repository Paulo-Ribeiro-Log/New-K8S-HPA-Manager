import React, { useState } from 'react';
import { Input } from '@/components/ui/input';
import { Loader2, Activity, Search, ChevronLeft, X } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { SplitView } from '@/components/SplitView';
import { PrometheusListItem } from '@/components/PrometheusListItem';
import { PrometheusEditor } from '@/components/PrometheusEditor';
import { PrometheusMonitorTable } from '@/components/PrometheusMonitorTable';
import { usePrometheusResources } from '@/hooks/useAPI';
import type { PrometheusResource } from '@/lib/api/types';

interface PrometheusPageProps {
  selectedCluster: string;
}

export function PrometheusPage({ selectedCluster }: PrometheusPageProps) {
  const [selectedResource, setSelectedResource] = useState<PrometheusResource | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const { data: resources = [], isLoading, error, refetch } = usePrometheusResources(selectedCluster);

  const handleClearSelection = () => setSelectedResource(null);

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

  const leftTitleAction = selectedResource ? (
    <div className="flex items-center gap-2 px-2 py-1 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary font-medium">
      <Activity className="w-3.5 h-3.5 flex-shrink-0" />
      <span className="truncate max-w-[140px]">{selectedResource.namespace}/{selectedResource.name}</span>
      <button onClick={handleClearSelection} className="flex-shrink-0 hover:text-foreground transition-colors" title="Limpar seleção">
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  ) : undefined;

  const rightTitlePrefix = selectedResource ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  return (
    <SplitView
      leftPanel={{
        title: "Recursos Prometheus",
        titleAction: leftTitleAction,
        content: isLoading ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <Loader2 className="h-8 w-8 animate-spin mr-2" />
            Carregando recursos Prometheus...
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
        title: selectedResource ? `${selectedResource.namespace}/${selectedResource.name}` : "Visualização",
        titlePrefix: rightTitlePrefix,
        content: selectedResource ? (
          <PrometheusEditor
            resource={selectedResource}
            selectedCluster={selectedCluster}
            onRefetch={refetch}
          />
        ) : (
          <PrometheusMonitorTable
            cluster={selectedCluster}
            resources={filteredResources}
            headerLabel={`${filteredResources.length} recurso(s)`}
            onOpenEditor={setSelectedResource}
          />
        ),
      }}
    />
  );
}
