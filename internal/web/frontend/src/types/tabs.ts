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
    selectedHPA: unknown | null;
    selectedNodePool: unknown | null;
    showApplyModal: boolean;
    hpasToApply: Array<{ key: string; current: unknown; original: unknown }>;
    showNodePoolApplyModal: boolean;
    nodePoolsToApply: Array<{ key: string; current: unknown; original: unknown }>;
    showSaveSessionModal: boolean;
    showLoadSessionModal: boolean;
    isContextSwitching: boolean;

    // Estados aninhados de cada aba do Workload (Pods, ConfigMaps, Deployments, Secrets, etc)
    // Estrutura: { [namespace]: { [key]: value } }
    // Ex: { pods: { selectedPod: {...}, showSystemPods: false }, configmaps: { selectedConfigMap: {...} } }
    workloadStates?: Record<string, Record<string, unknown>>;
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
    availableTemplates: unknown[];
    savedSessions: unknown[];
    currentSessionFolder?: string;
  };
  clusters: {
    available: unknown[];
    connecting: string[];
  };
  ui: {
    sidebarOpen: boolean;
    showBatchOperations: boolean;
    loading: boolean;
  };
}