import { useState, useCallback, useEffect, useRef } from 'react';
import { Layers } from 'lucide-react';
import { apiClient } from '@/lib/api/client';
import { toast } from 'sonner';

interface ClusterTabCache {
  selectedNamespace: string;
  selectedHPA: any;
  selectedNodePool: any;
  hpas: any[];
  namespaces: any[];
  nodePools: any[];
}

interface ClusterTab {
  id: string;
  label: string;
  icon: any;
  cluster: string;
  // Cache de dados por aba
  cache?: ClusterTabCache;
}

export const useClusterTabs = (clusters: string[]) => {
  const [clusterTabs, setClusterTabs] = useState<ClusterTab[]>([]);
  const [activeClusterTab, setActiveClusterTab] = useState<string>('');
  const [isContextSwitching, setIsContextSwitching] = useState(false);
  const lastSwitchedContext = useRef<string>('');
  const switchTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Criar primeira aba automaticamente quando há clusters disponíveis
  const initializeFirstTab = useCallback(() => {
    if (clusters.length > 0 && clusterTabs.length === 0) {
      const firstCluster = clusters[0];
      const firstTab: ClusterTab = {
        id: `cluster-tab-1`,
        label: `Cluster 1`,
        icon: Layers,
        cluster: firstCluster,
        cache: {
          selectedNamespace: '',
          selectedHPA: null,
          selectedNodePool: null,
          hpas: [],
          namespaces: [],
          nodePools: []
        }
      };
      setClusterTabs([firstTab]);
      setActiveClusterTab(firstTab.id);
    }
  }, [clusters, clusterTabs.length]);

  // Switch de contexto com debouncing e prevenção de duplicatas
  const switchToTabContext = useCallback(async (tabId: string, force = false) => {
    const tab = clusterTabs.find(t => t.id === tabId);
    if (!tab || !tab.cluster) return;

    // Prevenir chamadas duplicadas
    if (!force && (isContextSwitching || lastSwitchedContext.current === tab.cluster)) {
      console.log(`[ClusterTabs] Skipping duplicate switch to: ${tab.cluster}`);
      return;
    }

    // Cancelar switch anterior se existir
    if (switchTimeoutRef.current) {
      clearTimeout(switchTimeoutRef.current);
    }

    // Debounce de 300ms
    switchTimeoutRef.current = setTimeout(async () => {
      console.log(`[ClusterTabs] Switching to tab context: ${tab.cluster}`);
      setIsContextSwitching(true);
      lastSwitchedContext.current = tab.cluster;

      try {
        await apiClient.switchContext(tab.cluster);
        console.log(`[ClusterTabs] Context switched successfully to: ${tab.cluster}`);
        toast.success(`Contexto alterado para: ${tab.cluster}`);
        
        // Emit event para notificar outros componentes sobre mudança de contexto
        window.dispatchEvent(new CustomEvent('clusterChanged', {
          detail: { cluster: tab.cluster, tabId }
        }));
        
        // Emit event para forçar rescan após mudança de contexto
        window.dispatchEvent(new CustomEvent('forceRescan', {
          detail: { cluster: tab.cluster, tabId, reason: 'context-switch' }
        }));
        
      } catch (error) {
        console.error(`[ClusterTabs] Error switching context:`, error);
        toast.error(`Erro ao alterar contexto: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
        lastSwitchedContext.current = ''; // Reset em caso de erro
      } finally {
        setIsContextSwitching(false);
        switchTimeoutRef.current = null;
      }
    }, 300);
  }, [clusterTabs, isContextSwitching]);

  // Handler para mudança de aba com switch automático
  const handleTabChange = useCallback(async (tabId: string) => {
    if (tabId === activeClusterTab) return;
    
    setActiveClusterTab(tabId);
    await switchToTabContext(tabId);
  }, [activeClusterTab, switchToTabContext]);

  // Adicionar nova aba
  const addClusterTab = useCallback(() => {
    const newTabNumber = clusterTabs.length + 1;
    const defaultCluster = clusters.length > 0 ? clusters[0] : '';
    const newTab: ClusterTab = {
      id: `cluster-tab-${newTabNumber}`,
      label: `Cluster ${newTabNumber}`,
      icon: Layers,
      cluster: defaultCluster,
      cache: {
        selectedNamespace: '',
        selectedHPA: null,
        selectedNodePool: null,
        hpas: [],
        namespaces: [],
        nodePools: []
      }
    };
    setClusterTabs(prev => [...prev, newTab]);
    handleTabChange(newTab.id); // Muda para a nova aba e faz switch de contexto
  }, [clusterTabs.length, clusters, handleTabChange]);

  // Remover aba
  const removeClusterTab = useCallback((tabId: string) => {
    setClusterTabs(prev => {
      const filtered = prev.filter(tab => tab.id !== tabId);
      
      // Se removemos a aba ativa, selecionar outra e fazer switch
      if (tabId === activeClusterTab && filtered.length > 0) {
        const currentIndex = prev.findIndex(tab => tab.id === tabId);
        const newActiveIndex = currentIndex > 0 ? currentIndex - 1 : 0;
        const newActiveTab = filtered[newActiveIndex];
        handleTabChange(newActiveTab.id);
      }
      
      return filtered;
    });
  }, [activeClusterTab, handleTabChange]);

  // Mudar cluster de uma aba específica (com switch se for a aba ativa)
  const changeTabCluster = useCallback(async (tabId: string, newCluster: string) => {
    // Prevenir mudança se é o mesmo cluster
    const currentTab = clusterTabs.find(tab => tab.id === tabId);
    if (currentTab?.cluster === newCluster) return;

    setClusterTabs(prev => 
      prev.map(tab => 
        tab.id === tabId 
          ? { ...tab, cluster: newCluster }
          : tab
      )
    );

    // Se é a aba ativa, fazer switch imediatamente
    if (tabId === activeClusterTab) {
      console.log(`[ClusterTabs] Active tab cluster changed, switching context to: ${newCluster}`);
      await switchToTabContext(tabId, true); // Force switch
      
      // Disparar evento customizado para o Index.tsx saber que precisa resetar estados
      window.dispatchEvent(new CustomEvent('clusterChanged', { 
        detail: { cluster: newCluster, tabId } 
      }));
    }
  }, [activeClusterTab, clusterTabs, switchToTabContext]);

  // Obter cluster da aba ativa
  const getActiveCluster = useCallback(() => {
    const activeTab = clusterTabs.find(tab => tab.id === activeClusterTab);
    return activeTab?.cluster || '';
  }, [clusterTabs, activeClusterTab]);

    // Salvar cache para uma aba específica
  const saveTabCache = useCallback((tabId: string, cacheData: Partial<ClusterTabCache>) => {
    console.log(`[useClusterTabs] Saving cache for tab ${tabId}:`, cacheData);
    setClusterTabs(prev =>
      prev.map(tab =>
        tab.id === tabId
          ? { ...tab, cache: { ...tab.cache, ...cacheData } }
          : tab
      )
    );
  }, []);

  // Obter cache da aba ativa
  const getActiveTabCache = useCallback(() => {
    const activeTab = clusterTabs.find(tab => tab.id === activeClusterTab);
    const cache = activeTab?.cache || {
      selectedNamespace: '',
      selectedHPA: null,
      selectedNodePool: null,
      hpas: [],
      namespaces: [],
      nodePools: []
    };
    console.log(`[useClusterTabs] Getting cache for active tab ${activeClusterTab}:`, cache);
    return cache;
  }, [clusterTabs, activeClusterTab]);

  // Switch automático apenas na primeira vez
  useEffect(() => {
    if (clusterTabs.length === 1 && activeClusterTab && lastSwitchedContext.current === '') {
      switchToTabContext(activeClusterTab, true);
    }
  }, [clusterTabs.length, activeClusterTab, switchToTabContext]);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (switchTimeoutRef.current) {
        clearTimeout(switchTimeoutRef.current);
      }
    };
  }, []);

  return {
    clusterTabs,
    activeClusterTab,
    handleTabChange, // Usar este em vez de setActiveClusterTab
    addClusterTab,
    removeClusterTab,
    changeTabCluster,
    getActiveCluster,
    saveTabCache,
    getActiveTabCache,
    initializeFirstTab,
    isContextSwitching
  };
};