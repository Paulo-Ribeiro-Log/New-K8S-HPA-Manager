// API Types - matching Go backend structures

export interface Cluster {
  name: string;
  context: string;
  status: "online" | "offline";
  region?: string;
  resourceGroup?: string;
  subscription?: string;
}

export interface ClusterInfo {
  cluster: string;
  context: string;
  server: string;
  namespace: string;
  kubernetesVersion: string;
  cpuUsagePercent: number;
  memoryUsagePercent: number;
  nodeCount: number;
  podCount: number;
}

export interface Namespace {
  name: string;
  cluster: string;
  hpaCount?: number;
  isSystem?: boolean;
}

export interface HPA {
  name: string;
  namespace: string;
  cluster: string;
  min_replicas: number | null;
  max_replicas: number;
  current_replicas: number;
  desired_replicas?: number;
  target_cpu?: number | null;
  target_memory?: number | null;
  last_scale_time?: string;
  conditions?: HPACondition[];
  perform_rollout?: boolean;
  perform_daemonset_rollout?: boolean;
  perform_statefulset_rollout?: boolean;

  // Deployment information
  deployment_name?: string;

  // Target values (editable)
  target_cpu_request?: string;
  target_cpu_limit?: string;
  target_memory_request?: string;
  target_memory_limit?: string;

  // Original values from deployment (from original_values object)
  original_values?: {
    min_replicas?: number;
    max_replicas?: number;
    target_cpu?: number;
    target_memory?: number;
    cpu_request?: string;
    cpu_limit?: string;
    memory_request?: string;
    memory_limit?: string;
    deployment_name?: string;
    perform_rollout?: boolean;
    perform_daemonset_rollout?: boolean;
    perform_statefulset_rollout?: boolean;
  };

  resources_modified?: boolean;
}

export interface HPACondition {
  type: string;
  status: string;
  lastTransitionTime: string;
  reason: string;
  message: string;
}

export interface NodePool {
  name: string;
  vm_size: string;
  node_count: number;
  min_node_count: number;
  max_node_count: number;
  autoscaling_enabled: boolean;
  status: string;
  is_system_pool: boolean;
  cluster_name: string;
  resource_group: string;
  subscription: string;
  modified: boolean;
  selected: boolean;
  applied_count: number;
  sequence_order: number;
  sequence_status: string;
  original_values: {
    node_count: number;
    min_node_count: number;
    max_node_count: number;
    autoscaling_enabled: boolean;
  };
}

export interface CronJob {
  name: string;
  namespace: string;
  schedule: string;
  schedule_description: string;
  suspend: boolean | null;
  last_schedule_time?: string;
  active_jobs: number;
  successful_jobs: number;
  failed_jobs: number;
}

export interface CronJobUpdate {
  suspend: boolean;
}

export interface PrometheusResource {
  name: string;
  namespace: string;
  type: string; // Deployment, StatefulSet, DaemonSet
  component: string; // prometheus-server, grafana, etc.
  replicas: number;
  current_cpu_request: string;
  current_memory_request: string;
  current_cpu_limit: string;
  current_memory_limit: string;
  cpu_usage?: string;
  memory_usage?: string;
}

export interface PrometheusResourceUpdate {
  cpu_request: string;
  memory_request: string;
  cpu_limit: string;
  memory_limit: string;
  replicas?: number;
}

export interface ValidationStatus {
  vpnConnected: boolean;
  azureCliAvailable: boolean;
  kubectlAvailable: boolean;
  message: string;
  lastCheck: string;
}

export interface APIError {
  error: string;
  details?: string;
}

export interface APIResponse<T> {
  data?: T;
  error?: string;
  message?: string;
}

// Session Management Types
export interface Session {
  name: string;
  created_at: string;
  created_by: string;
  description?: string;
  template_used: string;
  metadata?: SessionMetadata;
  changes: HPAChange[];
  node_pool_changes: NodePoolChange[];
  resource_changes: ClusterResourceChange[];
  rollback_data?: RollbackData;
}

export interface SessionMetadata {
  clusters_affected: string[];
  namespaces_count: number;
  hpa_count: number;
  node_pool_count: number;
  resource_count: number;
  total_changes: number;
}

export interface RollbackData {
  original_state_captured: boolean;
  can_rollback: boolean;
  rollback_script_generated: boolean;
}

export interface HPAChange {
  cluster: string;
  namespace: string;
  hpa_name: string;
  original_values?: HPAValues;
  new_values?: HPAValues;
  applied: boolean;
  applied_at?: string;
  rollout_triggered: boolean;
  daemonset_rollout_triggered: boolean;
  statefulset_rollout_triggered: boolean;
}

export interface HPAValues {
  min_replicas?: number;
  max_replicas?: number;
  target_cpu?: number;
  target_memory?: number;
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;
  deployment_name?: string;
  perform_rollout?: boolean;
  perform_daemonset_rollout?: boolean;
  perform_statefulset_rollout?: boolean;
}

export interface NodePoolChange {
  cluster: string;
  resource_group: string;
  subscription: string;
  node_pool_name: string;
  original_values: NodePoolValues;
  new_values: NodePoolValues;
  applied: boolean;
  applied_at?: string;
  error?: string;
  sequence_order: number;
  sequence_status: string;
}

export interface NodePoolValues {
  node_count: number;
  min_node_count: number;
  max_node_count: number;
  autoscaling_enabled: boolean;
}

export interface ClusterResourceChange {
  cluster: string;
  namespace: string;
  resource_name: string;
  resource_type: string;
  original_values: ResourceValues;
  new_values: ResourceValues;
  applied: boolean;
  applied_at?: string;
  error?: string;
}

export interface ResourceValues {
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;
  replicas?: number;
}

export interface SessionFolder {
  name: string;
  type: "hpa" | "nodepool";
  action: "upscale" | "downscale";
  description: string;
}

export interface SessionTemplate {
  name: string;
  description: string;
  pattern: string;
  variables: string[];
  example: string;
}
