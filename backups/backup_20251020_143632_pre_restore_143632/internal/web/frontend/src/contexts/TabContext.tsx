import * as React from 'react';
import { createContext, useContext, useReducer, useCallback, useEffect } from 'react';
import { TabState, TabManager, TabAction, MultiTabAppState } from '../types/tabs';

// Estado inicial
const initialTabState: TabState = {
  id: '',
  name: '',
  clusterContext: '',
  active: false,
  modified: false,
  createdAt: new Date(),
  lastAccessedAt: new Date(),
  pageState: {
    activeTab: 'dashboard',
    selectedCluster: '',
    selectedNamespace: '',
    selectedHPA: null,
    selectedNodePool: null,
    showApplyModal: false,
    hpasToApply: [],
    showNodePoolApplyModal: false,
    nodePoolsToApply: [],
    showSaveSessionModal: false,
    showLoadSessionModal: false,
    isContextSwitching: false,
  },
  pendingChanges: {
    total: 0,
    hpas: 0,
    nodePools: 0,
  },
};

const initialState: MultiTabAppState = {
  tabManager: {
    tabs: [],
    activeTabIndex: 0,
    maxTabs: 10,
  },
  sessionManager: {
    availableTemplates: [],
    savedSessions: [],
  },
  clusters: {
    available: [],
    connecting: [],
  },
  ui: {
    sidebarOpen: true,
    showBatchOperations: false,
    loading: false,
  },
};

// Reducer para gerenciar as abas
function tabReducer(state: MultiTabAppState, action: TabAction): MultiTabAppState {
  switch (action.type) {
    case 'ADD_TAB': {
      const { name, clusterContext } = action.payload;
      
      // Verificar se já chegou no limite
      if (state.tabManager.tabs.length >= state.tabManager.maxTabs) {
        return state;
      }
      
      // Criar nova aba
      const newTab: TabState = {
        ...initialTabState,
        id: `tab-${Date.now()}-${state.tabManager.tabs.length}`,
        name,
        clusterContext,
        active: false,
        createdAt: new Date(),
        lastAccessedAt: new Date(),
      };
      
      // Desativar aba atual
      const updatedTabs = state.tabManager.tabs.map(tab => ({ ...tab, active: false }));
      
      // Adicionar nova aba e ativá-la
      const newTabs = [...updatedTabs, { ...newTab, active: true }];
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: newTabs,
          activeTabIndex: newTabs.length - 1,
        },
      };
    }
    
    case 'CLOSE_TAB': {
      const { index } = action.payload;
      
      // Não permitir fechar se só tem uma aba
      if (state.tabManager.tabs.length <= 1) {
        return state;
      }
      
      // Remover aba
      const newTabs = state.tabManager.tabs.filter((_, i) => i !== index);
      
      // Ajustar índice ativo
      let newActiveIndex = state.tabManager.activeTabIndex;
      if (index === state.tabManager.activeTabIndex) {
        // Se fechou a aba ativa, ativar a anterior ou próxima
        newActiveIndex = Math.min(index, newTabs.length - 1);
      } else if (index < state.tabManager.activeTabIndex) {
        // Se fechou uma aba antes da ativa, decrementar índice
        newActiveIndex = state.tabManager.activeTabIndex - 1;
      }
      
      // Garantir que existe uma aba ativa
      const finalTabs = newTabs.map((tab, i) => ({
        ...tab,
        active: i === newActiveIndex,
        lastAccessedAt: i === newActiveIndex ? new Date() : tab.lastAccessedAt,
      }));
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: finalTabs,
          activeTabIndex: newActiveIndex,
        },
      };
    }
    
    case 'SWITCH_TAB': {
      const { index } = action.payload;
      
      if (index < 0 || index >= state.tabManager.tabs.length) {
        return state;
      }
      
      const updatedTabs = state.tabManager.tabs.map((tab, i) => ({
        ...tab,
        active: i === index,
        lastAccessedAt: i === index ? new Date() : tab.lastAccessedAt,
      }));
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: updatedTabs,
          activeTabIndex: index,
        },
      };
    }
    
    case 'UPDATE_TAB_STATE': {
      const { index, pageState } = action.payload;
      
      if (index < 0 || index >= state.tabManager.tabs.length) {
        return state;
      }
      
      const updatedTabs = state.tabManager.tabs.map((tab, i) => 
        i === index ? { 
          ...tab, 
          pageState: { ...tab.pageState, ...pageState },
          lastAccessedAt: new Date() 
        } : tab
      );
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: updatedTabs,
        },
      };
    }
    
    case 'SET_TAB_MODIFIED': {
      const { index, modified } = action.payload;
      
      if (index < 0 || index >= state.tabManager.tabs.length) {
        return state;
      }
      
      const updatedTabs = state.tabManager.tabs.map((tab, i) => 
        i === index ? { ...tab, modified } : tab
      );
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: updatedTabs,
        },
      };
    }
    
    case 'UPDATE_TAB_CHANGES': {
      const { index, changes } = action.payload;
      
      if (index < 0 || index >= state.tabManager.tabs.length) {
        return state;
      }
      
      const updatedTabs = state.tabManager.tabs.map((tab, i) => 
        i === index ? {
          ...tab,
          pendingChanges: changes,
          modified: changes.total > 0,
          lastAccessedAt: new Date(),
        } : tab
      );
      
      return {
        ...state,
        tabManager: {
          ...state.tabManager,
          tabs: updatedTabs,
        },
      };
    }
    
    default:
      return state;
  }
}

// Context
const TabContext = createContext<{
  state: MultiTabAppState;
  dispatch: React.Dispatch<TabAction>;
  // Helper functions
  addTab: (name: string, clusterContext: string) => boolean;
  closeTab: (index: number) => boolean;
  switchTab: (index: number) => boolean;
  getActiveTab: () => TabState | null;
  canAddTab: () => boolean;
  getTabsWithChanges: () => TabState[];
  updateActiveTabState: (pageState: Partial<TabState['pageState']>) => void;
  updateActiveTabChanges: (changes: TabState['pendingChanges']) => void;
} | null>(null);

// Provider
export const TabProvider = ({ children }: { children: React.ReactNode }) => {
  const [state, dispatch] = useReducer(tabReducer, initialState);
  
  // Helper functions
  const addTab = useCallback((name: string, clusterContext: string): boolean => {
    if (state.tabManager.tabs.length >= state.tabManager.maxTabs) {
      return false;
    }
    dispatch({ type: 'ADD_TAB', payload: { name, clusterContext } });
    return true;
  }, [state.tabManager.tabs.length, state.tabManager.maxTabs]);
  
  const closeTab = useCallback((index: number): boolean => {
    if (state.tabManager.tabs.length <= 1) {
      return false;
    }
    dispatch({ type: 'CLOSE_TAB', payload: { index } });
    return true;
  }, [state.tabManager.tabs.length]);
  
  const switchTab = useCallback((index: number): boolean => {
    if (index < 0 || index >= state.tabManager.tabs.length) {
      return false;
    }
    dispatch({ type: 'SWITCH_TAB', payload: { index } });
    return true;
  }, [state.tabManager.tabs.length]);
  
  const getActiveTab = useCallback((): TabState | null => {
    const { activeTabIndex, tabs } = state.tabManager;
    if (activeTabIndex >= 0 && activeTabIndex < tabs.length) {
      return tabs[activeTabIndex];
    }
    return null;
  }, [state.tabManager.activeTabIndex, state.tabManager.tabs]);
  
  const canAddTab = useCallback((): boolean => {
    return state.tabManager.tabs.length < state.tabManager.maxTabs;
  }, [state.tabManager.tabs.length, state.tabManager.maxTabs]);
  
  const getTabsWithChanges = useCallback((): TabState[] => {
    return state.tabManager.tabs.filter((tab: TabState) => tab.modified && tab.pendingChanges.total > 0);
  }, [state.tabManager.tabs]);
  
  const updateActiveTabState = useCallback((pageState: Partial<TabState['pageState']>) => {
    const activeIndex = state.tabManager.activeTabIndex;
    if (activeIndex >= 0 && activeIndex < state.tabManager.tabs.length) {
      dispatch({ type: 'UPDATE_TAB_STATE', payload: { index: activeIndex, pageState } });
    }
  }, [state.tabManager.activeTabIndex, state.tabManager.tabs.length]);
  
  const updateActiveTabChanges = useCallback((changes: TabState['pendingChanges']) => {
    const activeIndex = state.tabManager.activeTabIndex;
    if (activeIndex >= 0 && activeIndex < state.tabManager.tabs.length) {
      dispatch({ type: 'UPDATE_TAB_CHANGES', payload: { index: activeIndex, changes } });
    }
  }, [state.tabManager.activeTabIndex, state.tabManager.tabs.length]);
  
  // Atalhos de teclado (Alt+1-9, Alt+0)
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.altKey && !event.ctrlKey && !event.shiftKey) {
        const key = event.key;
        
        // Alt+1 to Alt+9 (tabs 0-8)
        if (key >= '1' && key <= '9') {
          const tabIndex = parseInt(key) - 1;
          if (tabIndex < state.tabManager.tabs.length) {
            event.preventDefault();
            switchTab(tabIndex);
          }
        }
        // Alt+0 (tab 9, a 10ª aba)
        else if (key === '0') {
          const tabIndex = 9;
          if (tabIndex < state.tabManager.tabs.length) {
            event.preventDefault();
            switchTab(tabIndex);
          }
        }
        // Alt+T para nova aba
        else if (key.toLowerCase() === 't') {
          event.preventDefault();
          if (canAddTab()) {
            const clusterContext = 'new-cluster'; // TODO: pegar do seletor de cluster
            addTab(`Cluster ${state.tabManager.tabs.length + 1}`, clusterContext);
          }
        }
        // Alt+W para fechar aba
        else if (key.toLowerCase() === 'w') {
          event.preventDefault();
          const activeIndex = state.tabManager.activeTabIndex;
          closeTab(activeIndex);
        }
      }
    };
    
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [state.tabManager, switchTab, addTab, closeTab, canAddTab]);
  
  // Criar primeira aba quando o provider monta
  useEffect(() => {
    if (state.tabManager.tabs.length === 0) {
      const timer = setTimeout(() => {
        dispatch({ 
          type: 'ADD_TAB', 
          payload: { name: 'Main Cluster', clusterContext: 'default' } 
        });
      }, 10);
      return () => clearTimeout(timer);
    }
  }, []);
  
  const value = {
    state,
    dispatch,
    addTab,
    closeTab,
    switchTab,
    getActiveTab,
    canAddTab,
    getTabsWithChanges,
    updateActiveTabState,
    updateActiveTabChanges,
  };
  
  return <TabContext.Provider value={value}>{children}</TabContext.Provider>;
};

// Hook para usar o contexto
export const useTabManager = () => {
  const context = useContext(TabContext);
  if (!context) {
    throw new Error('useTabManager must be used within a TabProvider');
  }
  return context;
};

export default TabProvider;