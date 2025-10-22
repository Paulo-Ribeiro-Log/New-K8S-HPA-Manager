import { useTabManager } from '@/contexts/TabContext';
import { TabState } from '@/types/tabs';

// Componente simples de teste para cada aba
const SimpleTabContent = ({ tab }: { tab: TabState }) => {
  const { updateActiveTabState } = useTabManager();
  
  return (
    <div className="flex flex-col h-full p-6">
      <div className="border rounded-lg p-4 mb-4">
        <h2 className="text-xl font-bold mb-2">{tab.name}</h2>
        <p className="text-muted-foreground mb-4">
          Cluster: {tab.clusterContext}
        </p>
        
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <div className="bg-card p-4 rounded border">
            <h3 className="font-semibold text-sm mb-1">Dashboard</h3>
            <button 
              onClick={() => updateActiveTabState({ activeTab: 'dashboard' })}
              className={`w-full px-3 py-2 text-sm rounded ${
                tab.pageState.activeTab === 'dashboard' 
                  ? 'bg-primary text-primary-foreground' 
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              📊 Overview
            </button>
          </div>
          
          <div className="bg-card p-4 rounded border">
            <h3 className="font-semibold text-sm mb-1">HPAs</h3>
            <button 
              onClick={() => updateActiveTabState({ activeTab: 'hpas' })}
              className={`w-full px-3 py-2 text-sm rounded ${
                tab.pageState.activeTab === 'hpas' 
                  ? 'bg-primary text-primary-foreground' 
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              📈 HPAs
            </button>
          </div>
          
          <div className="bg-card p-4 rounded border">
            <h3 className="font-semibold text-sm mb-1">Node Pools</h3>
            <button 
              onClick={() => updateActiveTabState({ activeTab: 'nodepools' })}
              className={`w-full px-3 py-2 text-sm rounded ${
                tab.pageState.activeTab === 'nodepools' 
                  ? 'bg-primary text-primary-foreground' 
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              🖥️ Node Pools
            </button>
          </div>
          
          <div className="bg-card p-4 rounded border">
            <h3 className="font-semibold text-sm mb-1">Monitoring</h3>
            <button 
              onClick={() => updateActiveTabState({ activeTab: 'prometheus' })}
              className={`w-full px-3 py-2 text-sm rounded ${
                tab.pageState.activeTab === 'prometheus' 
                  ? 'bg-primary text-primary-foreground' 
                  : 'bg-muted hover:bg-muted/80'
              }`}
            >
              📊 Prometheus
            </button>
          </div>
        </div>
        
        <div className="bg-muted/20 p-4 rounded">
          <h3 className="font-semibold mb-2">View Ativa: {tab.pageState.activeTab}</h3>
          <div className="space-y-2">
            {tab.pageState.activeTab === 'dashboard' && (
              <div>
                <h4 className="font-medium">📊 Dashboard</h4>
                <p className="text-sm text-muted-foreground">Visão geral do cluster {tab.clusterContext}</p>
                <div className="mt-2 p-2 bg-card rounded text-sm">
                  Aqui seria exibido o dashboard completo com métricas, gráficos e estatísticas do cluster.
                </div>
              </div>
            )}
            
            {tab.pageState.activeTab === 'hpas' && (
              <div>
                <h4 className="font-medium">📈 HPAs</h4>
                <p className="text-sm text-muted-foreground">Gerenciamento de HPAs do cluster {tab.clusterContext}</p>
                <div className="mt-2 p-2 bg-card rounded text-sm">
                  Aqui seria exibido a interface completa de HPAs: lista à esquerda, editor à direita, modals de aplicação.
                </div>
              </div>
            )}
            
            {tab.pageState.activeTab === 'nodepools' && (
              <div>
                <h4 className="font-medium">🖥️ Node Pools</h4>
                <p className="text-sm text-muted-foreground">Gerenciamento de Node Pools do cluster {tab.clusterContext}</p>
                <div className="mt-2 p-2 bg-card rounded text-sm">
                  Aqui seria exibido a interface completa de Node Pools: lista à esquerda, editor à direita, controles de scaling.
                </div>
              </div>
            )}
            
            {tab.pageState.activeTab === 'prometheus' && (
              <div>
                <h4 className="font-medium">📊 Prometheus</h4>
                <p className="text-sm text-muted-foreground">Monitoramento do cluster {tab.clusterContext}</p>
                <div className="mt-2 p-2 bg-card rounded text-sm">
                  Aqui seria exibido a interface do Prometheus com métricas, alertas e dashboards.
                </div>
              </div>
            )}
          </div>
        </div>
        
        <div className="mt-4 p-2 bg-blue-50 dark:bg-blue-950/20 rounded text-xs">
          <strong>Status da Aba:</strong><br/>
          • ID: {tab.id}<br/>
          • Ativa: {tab.active ? 'Sim' : 'Não'}<br/>
          • Modificada: {tab.modified ? 'Sim' : 'Não'}<br/>
          • Mudanças Pendentes: {tab.pendingChanges.total}<br/>
          • View Atual: {tab.pageState.activeTab}<br/>
          • Cluster: {tab.pageState.selectedCluster || 'Não selecionado'}
        </div>
      </div>
    </div>
  );
};

// Componente principal que renderiza a aba ativa
export const SimpleActiveTabContent = () => {
  const { getActiveTab, state, addTab } = useTabManager();
  const activeTab = getActiveTab();
  
  const handleCreateTab = () => {
    addTab('New Cluster', 'cluster-new');
  };
  
  if (!activeTab) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-center space-y-4">
          <h3 className="text-lg font-semibold">Nenhuma aba ativa</h3>
          <p className="text-muted-foreground">
            Total de abas: {state.tabManager.tabs.length}
          </p>
          <button 
            onClick={handleCreateTab}
            className="px-4 py-2 bg-primary text-primary-foreground rounded hover:bg-primary/90"
          >
            Criar Primeira Aba
          </button>
        </div>
      </div>
    );
  }
  
  return <SimpleTabContent tab={activeTab} />;
};

export default SimpleActiveTabContent;