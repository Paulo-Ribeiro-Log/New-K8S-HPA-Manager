import { createContext, useContext, ReactNode, useState } from 'react';
import type { ReleaseSummary, ReleaseDetail, RevisionEntry, HelmActionResponse } from '../types/helm';

interface HelmFilters {
  namespace: string;
  search: string;
  statuses: string[];
}

interface HelmState {
  // Releases
  releases: ReleaseSummary[];
  releasesLoading: boolean;
  releasesError: string | null;

  // Selected release
  selectedRelease: string | null;
  selectedReleaseNamespace: string | null;
  releaseDetail: ReleaseDetail | null;
  releaseDetailLoading: boolean;
  releaseDetailError: string | null;

  // History
  revisions: RevisionEntry[];
  revisionsLoading: boolean;
  revisionsError: string | null;

  // Operations
  operations: Map<string, HelmActionResponse>;
  activeOperationId: string | null;

  // Filters
  filters: HelmFilters;

  // Actions
  setReleases: (releases: ReleaseSummary[]) => void;
  setReleasesLoading: (loading: boolean) => void;
  setReleasesError: (error: string | null) => void;

  selectRelease: (releaseName: string | null, namespace?: string | null) => void;
  setReleaseDetail: (detail: ReleaseDetail | null) => void;
  setReleaseDetailLoading: (loading: boolean) => void;
  setReleaseDetailError: (error: string | null) => void;

  setRevisions: (revisions: RevisionEntry[]) => void;
  setRevisionsLoading: (loading: boolean) => void;
  setRevisionsError: (error: string | null) => void;

  addOperation: (operationId: string, response: HelmActionResponse) => void;
  updateOperation: (operationId: string, response: HelmActionResponse) => void;
  setActiveOperationId: (operationId: string | null) => void;

  setFilters: (filters: Partial<HelmFilters>) => void;
  resetFilters: () => void;

  reset: () => void;
}

const initialFilters: HelmFilters = {
  namespace: '',
  search: '',
  statuses: [],
};

const HelmContext = createContext<HelmState | undefined>(undefined);

export const useHelmStore = () => {
  const context = useContext(HelmContext);
  if (!context) {
    throw new Error('useHelmStore must be used within HelmProvider');
  }
  return context;
};

export const HelmProvider = ({ children }: { children: ReactNode }) => {
  const [releases, setReleases] = useState<ReleaseSummary[]>([]);
  const [releasesLoading, setReleasesLoading] = useState(false);
  const [releasesError, setReleasesError] = useState<string | null>(null);

  const [selectedRelease, setSelectedRelease] = useState<string | null>(null);
  const [selectedReleaseNamespace, setSelectedReleaseNamespace] = useState<string | null>(null);
  const [releaseDetail, setReleaseDetail] = useState<ReleaseDetail | null>(null);
  const [releaseDetailLoading, setReleaseDetailLoading] = useState(false);
  const [releaseDetailError, setReleaseDetailError] = useState<string | null>(null);

  const [revisions, setRevisions] = useState<RevisionEntry[]>([]);
  const [revisionsLoading, setRevisionsLoading] = useState(false);
  const [revisionsError, setRevisionsError] = useState<string | null>(null);

  const [operations, setOperations] = useState<Map<string, HelmActionResponse>>(new Map());
  const [activeOperationId, setActiveOperationId] = useState<string | null>(null);

  const [filters, setFiltersState] = useState<HelmFilters>(initialFilters);

  const selectRelease = (releaseName: string | null, namespace?: string | null) => {
    setSelectedRelease(releaseName);
    setSelectedReleaseNamespace(namespace || null);
  };

  const addOperation = (operationId: string, response: HelmActionResponse) => {
    setOperations((prev) => {
      const newMap = new Map(prev);
      newMap.set(operationId, response);
      return newMap;
    });
  };

  const updateOperation = (operationId: string, response: HelmActionResponse) => {
    setOperations((prev) => {
      const newMap = new Map(prev);
      newMap.set(operationId, response);
      return newMap;
    });
  };

  const setFilters = (newFilters: Partial<HelmFilters>) => {
    setFiltersState((prev) => ({ ...prev, ...newFilters }));
  };

  const resetFilters = () => {
    setFiltersState(initialFilters);
  };

  const reset = () => {
    setReleases([]);
    setReleasesLoading(false);
    setReleasesError(null);
    selectRelease(null);
    setReleaseDetail(null);
    setReleaseDetailLoading(false);
    setReleaseDetailError(null);
    setRevisions([]);
    setRevisionsLoading(false);
    setRevisionsError(null);
    setOperations(new Map());
    setActiveOperationId(null);
    setFiltersState(initialFilters);
  };

  const value: HelmState = {
    releases,
    releasesLoading,
    releasesError,
    selectedRelease,
    selectedReleaseNamespace,
    releaseDetail,
    releaseDetailLoading,
    releaseDetailError,
    revisions,
    revisionsLoading,
    revisionsError,
    operations,
    activeOperationId,
    filters,
    setReleases,
    setReleasesLoading,
    setReleasesError,
    selectRelease,
    setReleaseDetail,
    setReleaseDetailLoading,
    setReleaseDetailError,
    setRevisions,
    setRevisionsLoading,
    setRevisionsError,
    addOperation,
    updateOperation,
    setActiveOperationId,
    setFilters,
    resetFilters,
    reset,
  };

  return <HelmContext.Provider value={value}>{children}</HelmContext.Provider>;
};
