import { createContext, useContext, ReactNode, useState, useEffect, useCallback } from 'react';
import type { ValuesFileRequest, NexusStatus } from '../types/nexus';

// Chave para localStorage
const STORAGE_KEY = 'nexus-values-state';

interface NexusState {
  // Status
  status: NexusStatus | null;
  statusLoading: boolean;

  // File 1
  file1: ValuesFileRequest;

  // File 2
  file2: ValuesFileRequest;

  // Results
  file1Content: string;
  file2Content: string;
  file1Url: string;
  file2Url: string;
  compareError: string | null;

  // UI State
  showDiffModal: boolean;
  showConfigPanel: boolean;
  comparing: boolean;

  // Actions
  setStatus: (status: NexusStatus | null) => void;
  setStatusLoading: (loading: boolean) => void;
  setFile1: (file: ValuesFileRequest) => void;
  setFile2: (file: ValuesFileRequest) => void;
  setFile1Content: (content: string) => void;
  setFile2Content: (content: string) => void;
  setFile1Url: (url: string) => void;
  setFile2Url: (url: string) => void;
  setCompareError: (error: string | null) => void;
  setShowDiffModal: (show: boolean) => void;
  setShowConfigPanel: (show: boolean) => void;
  setComparing: (comparing: boolean) => void;

  // Bulk actions
  setCompareResults: (results: {
    file1Content: string;
    file2Content: string;
    file1Url: string;
    file2Url: string;
  }) => void;
  clearCompareResults: () => void;
  reset: () => void;
}

const initialFile: ValuesFileRequest = {
  release: '',
  version: '',
  environment: '' as any,
  type: '' as any,
};

// Interface para o estado persistido (exclui funções e loading states)
interface PersistedState {
  file1: ValuesFileRequest;
  file2: ValuesFileRequest;
  file1Content: string;
  file2Content: string;
  file1Url: string;
  file2Url: string;
  showDiffModal: boolean;
}

// Função para carregar estado do localStorage
const loadPersistedState = (): Partial<PersistedState> => {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as PersistedState;
      console.log('[NexusStore] Loaded persisted state:', {
        file1Release: parsed.file1?.release,
        file2Release: parsed.file2?.release,
        hasContent1: !!parsed.file1Content,
        hasContent2: !!parsed.file2Content,
      });
      return parsed;
    }
  } catch (error) {
    console.error('[NexusStore] Error loading persisted state:', error);
  }
  return {};
};

// Função para salvar estado no localStorage
const savePersistedState = (state: PersistedState) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    console.log('[NexusStore] State saved to localStorage');
  } catch (error) {
    console.error('[NexusStore] Error saving state:', error);
  }
};

const NexusContext = createContext<NexusState | undefined>(undefined);

export const useNexusStore = () => {
  const context = useContext(NexusContext);
  if (!context) {
    throw new Error('useNexusStore must be used within NexusProvider');
  }
  return context;
};

export const NexusProvider = ({ children }: { children: ReactNode }) => {
  // Carregar estado persistido
  const persisted = loadPersistedState();

  // Status (não persistido - sempre recarrega)
  const [status, setStatus] = useState<NexusStatus | null>(null);
  const [statusLoading, setStatusLoading] = useState(true);

  // File 1 (persistido)
  const [file1, setFile1Internal] = useState<ValuesFileRequest>(
    persisted.file1 || { ...initialFile }
  );

  // File 2 (persistido)
  const [file2, setFile2Internal] = useState<ValuesFileRequest>(
    persisted.file2 || { ...initialFile }
  );

  // Results (persistidos)
  const [file1Content, setFile1ContentInternal] = useState(persisted.file1Content || '');
  const [file2Content, setFile2ContentInternal] = useState(persisted.file2Content || '');
  const [file1Url, setFile1UrlInternal] = useState(persisted.file1Url || '');
  const [file2Url, setFile2UrlInternal] = useState(persisted.file2Url || '');
  const [compareError, setCompareError] = useState<string | null>(null);

  // UI State (showDiffModal persistido, outros não)
  const [showDiffModal, setShowDiffModalInternal] = useState(persisted.showDiffModal || false);
  const [showConfigPanel, setShowConfigPanel] = useState(false);
  const [comparing, setComparing] = useState(false);

  // Wrappers que atualizam o localStorage
  const setFile1 = useCallback((file: ValuesFileRequest) => {
    setFile1Internal(file);
  }, []);

  const setFile2 = useCallback((file: ValuesFileRequest) => {
    setFile2Internal(file);
  }, []);

  const setFile1Content = useCallback((content: string) => {
    setFile1ContentInternal(content);
  }, []);

  const setFile2Content = useCallback((content: string) => {
    setFile2ContentInternal(content);
  }, []);

  const setFile1Url = useCallback((url: string) => {
    setFile1UrlInternal(url);
  }, []);

  const setFile2Url = useCallback((url: string) => {
    setFile2UrlInternal(url);
  }, []);

  const setShowDiffModal = useCallback((show: boolean) => {
    setShowDiffModalInternal(show);
  }, []);

  // Persistir quando o estado mudar
  useEffect(() => {
    const stateToSave: PersistedState = {
      file1,
      file2,
      file1Content,
      file2Content,
      file1Url,
      file2Url,
      showDiffModal,
    };
    savePersistedState(stateToSave);
  }, [file1, file2, file1Content, file2Content, file1Url, file2Url, showDiffModal]);

  const setCompareResults = (results: {
    file1Content: string;
    file2Content: string;
    file1Url: string;
    file2Url: string;
  }) => {
    setFile1Content(results.file1Content);
    setFile2Content(results.file2Content);
    setFile1Url(results.file1Url);
    setFile2Url(results.file2Url);
  };

  const clearCompareResults = () => {
    setFile1Content('');
    setFile2Content('');
    setFile1Url('');
    setFile2Url('');
    setCompareError(null);
  };

  const reset = () => {
    setStatus(null);
    setStatusLoading(true);
    setFile1({ ...initialFile });
    setFile2({ ...initialFile });
    setFile1Content('');
    setFile2Content('');
    setFile1Url('');
    setFile2Url('');
    setCompareError(null);
    setShowDiffModal(false);
    setShowConfigPanel(false);
    setComparing(false);
    // Limpar localStorage também
    try {
      localStorage.removeItem(STORAGE_KEY);
      console.log('[NexusStore] State cleared from localStorage');
    } catch (error) {
      console.error('[NexusStore] Error clearing state:', error);
    }
  };

  const value: NexusState = {
    status,
    statusLoading,
    file1,
    file2,
    file1Content,
    file2Content,
    file1Url,
    file2Url,
    compareError,
    showDiffModal,
    showConfigPanel,
    comparing,
    setStatus,
    setStatusLoading,
    setFile1,
    setFile2,
    setFile1Content,
    setFile2Content,
    setFile1Url,
    setFile2Url,
    setCompareError,
    setShowDiffModal,
    setShowConfigPanel,
    setComparing,
    setCompareResults,
    clearCompareResults,
    reset,
  };

  return <NexusContext.Provider value={value}>{children}</NexusContext.Provider>;
};
