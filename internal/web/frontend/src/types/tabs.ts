export interface TabState {
  // Identificação da aba
  id: string;
  name: string;
  clusterContext: string;
  
  // Controle de estado
  active: boolean;
  modified: boolean;
  createdAt: Date;
  lastAccessedAt: Date;
  
  // Estado completo da página original
  pageState: {
    activeTab: string;
    selectedCluster: string;
    selectedNamespace: string;
    selectedHPA: any | null;
    selectedNodePool: any | null;
    showApplyModal: boolean;
    hpasToApply: Array<{ key: string; current: any; original: any }>;
    showNodePoolApplyModal: boolean;
    nodePoolsToApply: Array<{ key: string; current: any; original: any }>;
    showSaveSessionModal: boolean;
    showLoadSessionModal: boolean;
    isContextSwitching: boolean;
  };
  
  // Status de mudanças pendentes
  pendingChanges: {
    total: number;
    hpas: number;
    nodePools: number;
  };
}

export interface TabManager {
  tabs: TabState[];
  activeTabIndex: number;
  maxTabs: number;
}

// Actions para gerenciar abas
export type TabAction = 
  | { type: 'ADD_TAB'; payload: { name: string; clusterContext: string } }
  | { type: 'CLOSE_TAB'; payload: { index: number } }
  | { type: 'SWITCH_TAB'; payload: { index: number } }
  | { type: 'UPDATE_TAB_STATE'; payload: { index: number; pageState: Partial<TabState['pageState']> } }
  | { type: 'UPDATE_TAB_CHANGES'; payload: { index: number; changes: TabState['pendingChanges'] } }
  | { type: 'SET_TAB_MODIFIED'; payload: { index: number; modified: boolean } };

// Estado global da aplicação com multi-abas
export interface MultiTabAppState {
  tabManager: TabManager;
  sessionManager: {
    availableTemplates: any[];
    savedSessions: any[];
    currentSessionFolder?: string;
  };
  clusters: {
    available: any[];
    connecting: string[];
  };
  ui: {
    sidebarOpen: boolean;
    showBatchOperations: boolean;
    loading: boolean;
  };
}