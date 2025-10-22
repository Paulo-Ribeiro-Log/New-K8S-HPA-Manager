import { useTabManager } from '@/contexts/TabContext';
import { TabState } from '@/types/tabs';

// Importar os componentes necessários da página original
import { StatsCard } from "@/components/StatsCard";
import { TabNavigation } from "@/components/TabNavigation";
import { DashboardCharts } from "@/components/DashboardCharts";
import { SplitView } from "@/components/SplitView";
import { HPAListItem } from "@/components/HPAListItem";
import { HPAEditor } from "@/components/HPAEditor";
import { ApplyAllModal } from "@/components/ApplyAllModal";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { NodePoolApplyModal } from "@/components/NodePoolApplyModal";
import { SaveSessionModal } from "@/components/SaveSessionModal";
import { LoadSessionModal } from "@/components/LoadSessionModal";
import { CronJobsPage } from "@/pages/CronJobsPage";
import { PrometheusPage } from "@/pages/PrometheusPage";
import {
  LayoutDashboard,
  Scale,
  Server,
  Clock,
  Activity,
  Layers,
  Package,
  Database
} from "lucide-react";
import { useClusters, useNamespaces, useHPAs, useNodePools } from "@/hooks/useAPI";
import type { HPA, NodePool } from "@/lib/api/types";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

// Componente de página completa dentro de uma aba
const TabPageContent = ({ tab }: { tab: TabState }) => {
  const { updateActiveTabState, updateActiveTabChanges } = useTabManager();
  
  // Use the tab's cluster context
  const selectedCluster = tab.pageState.selectedCluster || tab.clusterContext;
  
  // API hooks usando o cluster da aba - com controle para evitar loops
  const { data: clusters = [], isLoading: clustersLoading } = useClusters();
  const { data: namespaces = [], isLoading: namespacesLoading, refetch: refetchNamespaces } = useNamespaces(selectedCluster);
  
  // Só carrega HPAs se a aba HPAs está ativa para evitar calls desnecessários
  const shouldLoadHPAs = tab.id === 'hpas' || tab.activeView === 'hpas';
  const { hpas = [], loading: hpasLoading, refetch: refetchHPAs } = useHPAs(
    shouldLoadHPAs ? selectedCluster : '', 
    shouldLoadHPAs ? tab.pageState.selectedNamespace : undefined
  );
  
  const { data: nodePools = [], isLoading: nodePoolsLoading, refetch: refetchNodePools } = useNodePools(selectedCluster);
  
  // Staging context
  const staging = useStaging();
  
  // Stats calculados
  const stats = {
    clusters: clusters.length,
    namespaces: namespaces.length,
    hpas: shouldLoadHPAs ? hpas.length : 0,
    nodePools: nodePools.length,
  };
  
  // Tabs da navegação interna
  const tabs = [
    { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
    { id: "hpas", label: "HPAs", icon: Scale },
    { id: "nodepools", label: "Node Pools", icon: Server },
    { id: "prometheus", label: "Prometheus", icon: Activity },
    { id: "cronjobs", label: "CronJobs", icon: Clock },
  ];
  
  // Handlers para atualizar estado da aba
  const handleClusterChange = (cluster: string) => {
    updateActiveTabState({ 
      selectedCluster: cluster,
      selectedNamespace: '',
      selectedHPA: null,
      selectedNodePool: null,
      isContextSwitching: true
    });
  };
  
  const handleTabChange = (tabId: string) => {
    updateActiveTabState({ activeTab: tabId });
  };
  
  const handleApplySingle = (hpa: HPA) => {
    // Lógica para aplicar HPA individual
    console.log("Aplicando HPA:", hpa);
  };
  
  // Atualizar contador de mudanças
  const updateChangesCount = () => {
    const changesCount = staging.getChangesCount();
    updateActiveTabChanges({
      total: changesCount.total,
      hpas: changesCount.hpas,
      nodePools: changesCount.nodePools,
    });
  };
  
  // Renderizar conteúdo da tab interna
  const renderTabContent = () => {
    switch (tab.pageState.activeTab) {
      case "dashboard":
        return (
          <div className="p-6 space-y-6">
            <DashboardCharts 
              clusters={clusters}
              selectedCluster={selectedCluster}
            />
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mt-6">
              <div className="p-4 bg-card rounded-lg border">
                <h3 className="font-semibold mb-2">� Cluster Ativo</h3>
                <p className="text-muted-foreground">{selectedCluster || 'Nenhum selecionado'}</p>
              </div>
              <div className="p-4 bg-card rounded-lg border">
                <h3 className="font-semibold mb-2">📦 Namespaces</h3>
                <p className="text-2xl font-bold">{stats.namespaces}</p>
              </div>
              <div className="p-4 bg-card rounded-lg border">
                <h3 className="font-semibold mb-2">📊 HPAs</h3>
                <p className="text-2xl font-bold">{stats.hpas}</p>
              </div>
              <div className="p-4 bg-card rounded-lg border">
                <h3 className="font-semibold mb-2">🖥️ Node Pools</h3>
                <p className="text-2xl font-bold">{stats.nodePools}</p>
              </div>
            </div>
          </div>
        );
      
      case "hpas":
        return (
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
                    ? "No HPAs found in this cluster"
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
                        tab.pageState.selectedHPA?.name === hpa.name &&
                        tab.pageState.selectedHPA?.namespace === hpa.namespace
                      }
                      onClick={() => updateActiveTabState({ selectedHPA: hpa })}
                    />
                  ))}
                </div>
              ),
            }}
            rightPanel={{
              title: "HPA Editor",
              content: (
                <HPAEditor
                  hpa={tab.pageState.selectedHPA}
                  onApply={handleApplySingle}
                  onApplied={() => {
                    refetchHPAs();
                    updateChangesCount();
                  }}
                />
              ),
            }}
          />
        );
      
      case "nodepools":
        return (
          <SplitView
            leftPanel={{
              title: "Available Node Pools",
              content: nodePoolsLoading ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  Loading Node Pools...
                </div>
              ) : nodePools.length === 0 ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  {selectedCluster
                    ? "No Node Pools found in this cluster"
                    : "Select a cluster to view Node Pools"}
                </div>
              ) : (
                <div className="space-y-2">
                  {nodePools.map((nodePool) => (
                    <NodePoolListItem
                      key={`${nodePool.cluster_name}-${nodePool.name}`}
                      nodePool={nodePool}
                      isSelected={
                        tab.pageState.selectedNodePool?.name === nodePool.name
                      }
                      onClick={() => updateActiveTabState({ selectedNodePool: nodePool })}
                    />
                  ))}
                </div>
              ),
            }}
            rightPanel={{
              title: "Node Pool Editor",
              content: (
                <NodePoolEditor
                  nodePool={tab.pageState.selectedNodePool}
                  onApplied={() => {
                    refetchNodePools();
                    updateChangesCount();
                  }}
                />
              ),
            }}
          />
        );
      
      case "prometheus":
        return <PrometheusPage />;
      
      case "cronjobs":
        return <CronJobsPage />;
      
      default:
        return null;
    }
  };
  
  return (
    <div className="flex flex-col h-full bg-background overflow-hidden">
      {/* Header da aba com cluster info */}
      <div className="flex items-center justify-between p-4 border-b bg-muted/20">
        <div className="flex items-center gap-3">
          <h2 className="font-semibold">{tab.name}</h2>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span>📍 {selectedCluster || 'No cluster selected'}</span>
            {tab.pendingChanges.total > 0 && (
              <span className="text-amber-600 dark:text-amber-400">
                ⚠️ {tab.pendingChanges.total} mudanças pendentes
              </span>
            )}
          </div>
        </div>
        
        {/* Seletor de cluster para esta aba */}
        <div className="flex items-center gap-2">
          <select
            value={selectedCluster}
            onChange={(e) => handleClusterChange(e.target.value)}
            className="px-3 py-1 border rounded text-sm bg-background"
          >
            <option value="">Selecionar cluster...</option>
            {clusters.map((cluster) => (
              <option key={cluster.context} value={cluster.context}>
                {cluster.context}
              </option>
            ))}
          </select>
        </div>
      </div>
      
      {/* Stats cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 px-6 py-3 flex-shrink-0">
        <StatsCard
          icon={Layers}
          value={clustersLoading ? "..." : String(stats.clusters)}
          label="Clusters"
        />
        <StatsCard
          icon={Package}
          value={namespacesLoading ? "..." : String(stats.namespaces)}
          label="Namespaces"
        />
        <StatsCard
          icon={Scale}
          value={shouldLoadHPAs && hpasLoading ? "..." : String(stats.hpas)}
          label="HPAs"
        />
        <StatsCard
          icon={Database}
          value={nodePoolsLoading ? "..." : String(stats.nodePools)}
          label="Node Pools"
        />
      </div>

      {/* Tab Navigation */}
      <TabNavigation
        tabs={tabs}
        activeTab={tab.pageState.activeTab}
        onTabChange={handleTabChange}
      />

      {/* Content */}
      <div className="flex-1 min-h-0 overflow-auto">
        {renderTabContent()}
      </div>

      {/* Modals */}
      <ApplyAllModal
        open={tab.pageState.showApplyModal}
        onOpenChange={(open) => updateActiveTabState({ showApplyModal: open })}
        modifiedHPAs={tab.pageState.hpasToApply}
        onApplied={() => {
          refetchHPAs();
          updateChangesCount();
        }}
        onClear={() => {
          staging.clearStaging();
          updateChangesCount();
        }}
      />

      <NodePoolApplyModal
        open={tab.pageState.showNodePoolApplyModal}
        onOpenChange={(open) => updateActiveTabState({ showNodePoolApplyModal: open })}
        modifiedNodePools={tab.pageState.nodePoolsToApply}
        onApplied={() => {
          refetchNodePools();
          updateChangesCount();
        }}
        onClear={() => {
          staging.clearStaging();
          updateChangesCount();
        }}
      />

      <SaveSessionModal
        open={tab.pageState.showSaveSessionModal}
        onOpenChange={(open) => updateActiveTabState({ showSaveSessionModal: open })}
        onSuccess={() => {
          console.log("Sessão salva com sucesso!");
        }}
      />

      <LoadSessionModal
        open={tab.pageState.showLoadSessionModal}
        onOpenChange={(open) => updateActiveTabState({ showLoadSessionModal: open })}
        onSessionLoaded={() => {
          console.log("Sessão carregada com sucesso!");
          updateChangesCount();
        }}
      />
    </div>
  );
};

// Componente principal que renderiza a aba ativa
export const ActiveTabContent = () => {
  const { getActiveTab, state, addTab } = useTabManager();
  const activeTab = getActiveTab();
  
  // Fallback para criar aba se necessário
  const handleCreateTab = () => {
    addTab('New Cluster', 'default');
  };
  
  if (!activeTab) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-center space-y-4">
          <h3 className="text-lg font-semibold">Nenhuma aba ativa</h3>
          <p className="text-muted-foreground">
            Crie uma nova aba para começar a trabalhar com clusters.
          </p>
          <button 
            onClick={handleCreateTab}
            className="px-4 py-2 bg-primary text-primary-foreground rounded hover:bg-primary/90"
          >
            Criar Primeira Aba
          </button>
          <div className="text-xs text-muted-foreground mt-2">
            Debug: Total abas = {state.tabManager.tabs.length}
          </div>
        </div>
      </div>
    );
  }
  
  return <TabPageContent tab={activeTab} />;
};

export default ActiveTabContent;