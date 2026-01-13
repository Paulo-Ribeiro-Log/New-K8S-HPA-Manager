/**
 * Helm API Types
 * Mirrors the backend Go structures for type safety
 */

export interface ReleaseSummary {
  name: string;
  namespace: string;
  chart: string;
  appVersion: string;
  revision: number;
  updatedAt: string;
  status: string;
  hasPendingUpgrade: boolean;
}

export interface Hook {
  name: string;
  kind: string;
  path: string;
  weight: number;
  deletePolicies: string[];
  lastRun: string;
}

export interface ChartMetadata {
  name: string;
  version: string;
  description: string;
  home?: string;
  sources?: string[];
  icon?: string;
  annotations?: Record<string, string>;
}

export interface ReleaseDetail extends ReleaseSummary {
  valuesRaw: string;
  valuesRendered: string;
  manifest: string;
  notes: string;
  hooks: Hook[];
  resources?: string[];
  chartMetadata?: ChartMetadata;
}

export interface RevisionEntry {
  revision: number;
  updatedAt: string;
  status: string;
  description: string;
  valuesDigest: string;
  executedBy: string;
}

export type HelmAction = 'install' | 'upgrade' | 'rollback' | 'uninstall';
export type OperationStatus = 'running' | 'succeeded' | 'failed' | 'pending-install' | 'pending-upgrade' | 'pending-rollback';

export interface HelmActionRequest {
  namespace: string;
  releaseName: string;
  action: HelmAction;
  chartRef?: string;
  version?: string;
  valuesYaml?: string;
  targetRevision?: number;
  force?: boolean;
  dryRun?: boolean;
}

export interface HelmActionResponse {
  operationId: string;
  status: OperationStatus;
  startedAt: string;
  completedAt?: string;
  logs: string[];
  warnings?: string[];
}

export interface OperationEvent {
  operationId: string;
  timestamp: string;
  phase: string;
  message: string;
}

export interface ListReleasesParams {
  cluster: string;
  namespace?: string;
  search?: string;
  statuses?: string[];
  limit?: number;
}

export interface GetReleaseParams {
  cluster: string;
  namespace?: string;
  release: string;
}

export interface HistoryParams {
  cluster: string;
  namespace?: string;
  release: string;
  limit?: number;
}

// API Response wrappers
export interface HelmListResponse {
  success: boolean;
  data: {
    releases: ReleaseSummary[];
    count: number;
  };
  error?: {
    code: string;
    message: string;
  };
}

export interface HelmDetailResponse {
  success: boolean;
  data: ReleaseDetail;
  error?: {
    code: string;
    message: string;
  };
}

export interface HelmHistoryResponse {
  success: boolean;
  data: {
    revisions: RevisionEntry[];
    count: number;
  };
  error?: {
    code: string;
    message: string;
  };
}

export interface HelmActionApiResponse {
  success: boolean;
  data: HelmActionResponse;
  error?: {
    code: string;
    message: string;
  };
}
