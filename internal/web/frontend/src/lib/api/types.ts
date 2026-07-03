// API Types - matching Go backend structures

export interface VersionInfo {
  current_version: string;
  update_available: boolean;
  latest_version?: string;
  download_url?: string;
}

export interface Cluster {
  name: string;
  context: string;
  status: "online" | "offline";
  cloud_provider?: "aks" | "eks" | "gke" | "unknown";
  region?: string;
  resourceGroup?: string;
  subscription?: string;
  aws_profile?: string; // EKS: perfil AWS real do kubeconfig (não inferido)
}

export interface ClusterInfo {
  cluster: string;
  context: string;
  server: string;
  namespace: string;
  kubernetesVersion: string;
  cpuUsagePercent: number;
  memoryUsagePercent: number;
  cpuCapacityPercent: number;    // % de Allocatable em relação ao Capacity
  memoryCapacityPercent: number; // % de Allocatable em relação ao Capacity
  nodeCount: number;
  podCount: number;
}

export interface Namespace {
  name: string;
  cluster: string;
  hpaCount?: number;
  isSystem?: boolean;
}

export interface NamespaceMetadata {
  uid?: string;
  resourceVersion?: string;
  creationTimestamp?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface NamespaceManifest {
  cluster: string;
  name: string;
  yaml: string;
  status: string;
  age: string;
  metadata: NamespaceMetadata;
}

export interface ConfigMapSummary {
  cluster: string;
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  dataKeys: string[];
  binaryKeys: string[];
  resourceVersion?: string;
  updatedAt: string;
}

export interface ConfigMapMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface ConfigMapManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: ConfigMapMetadata;
}

export interface ConfigMapDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface ConfigMapValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface ConfigMapApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// Secrets Types
export interface SecretSummary {
  cluster: string;
  namespace: string;
  name: string;
  type: string;
  labels?: Record<string, string>;
  dataKeys: string[];
  resourceVersion?: string;
  updatedAt: string;
  serviceClusterIPs?: string[];
  serviceExternalIPs?: string[];
}

export interface SecretMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface SecretManifest {
  cluster: string;
  namespace: string;
  name: string;
  type: string;
  yaml: string;
  metadata: SecretMetadata;
}

export interface SecretDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface SecretValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface SecretApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

export interface AkvResyncResult {
  success: boolean;
  command: string;
  output: string;
  resourceName?: string;
  namespace?: string;
  cluster?: string;
  timestamp?: number;
}

// Deployment Types
export interface DeploymentSummary {
  cluster: string;
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  replicas: number;
  readyReplicas: number;
  availableReplicas: number;
  updatedReplicas: number;
  unavailableReplicas: number;
  currentReplicas: number;
  statusCondition?: string;
  statusReason?: string;
  statusMessage?: string;
  resourceVersion?: string;
  updatedAt: string;
  serviceClusterIPs?: string[];
  serviceExternalIPs?: string[];
}

// Nota: statusCondition/statusReason/statusMessage vêm das Conditions do Deployment K8s

export interface DeploymentMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface DeploymentManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: DeploymentMetadata;
}

export interface DeploymentDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface DeploymentValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface DeploymentApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// DaemonSet Types
export interface DaemonSetSummary {
  cluster: string;
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  desiredNumberScheduled: number;
  currentNumberScheduled: number;
  numberReady: number;
  numberAvailable: number;
  numberMisscheduled: number;
  updatedNumberScheduled: number;
  resourceVersion?: string;
  updatedAt: string;
}

export interface DaemonSetMetadata {
  uid?: string;
  resourceVersion?: string;
  creationTimestamp?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface DaemonSetManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: DaemonSetMetadata;
}

export interface DaemonSetDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface DaemonSetValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface DaemonSetApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// StatefulSet Types
export interface StatefulSetSummary {
  cluster: string;
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  replicas: number;
  readyReplicas: number;
  currentReplicas: number;
  updatedReplicas: number;
  availableReplicas: number;
  resourceVersion?: string;
  updatedAt: string;
}

export interface StatefulSetMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface StatefulSetManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: StatefulSetMetadata;
}

export interface StatefulSetDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface StatefulSetValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface StatefulSetApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// Ingress Types
export interface IngressSummary {
  cluster: string;
  namespace: string;
  name: string;
  labels?: Record<string, string>;
  ingressClass?: string;
  hosts?: string[];
  addresses?: string[];
  resourceVersion?: string;
  updatedAt: string;
}

export interface IngressMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface IngressManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: IngressMetadata;
}

export interface IngressDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface IngressValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface IngressApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// Gateway API Types (gateway.networking.k8s.io)
export interface GatewaySummary {
  cluster: string;
  namespace: string;
  name: string;
  kind: string;
  gatewayClass?: string;
  addresses?: string[];
  listeners?: number;
  parentRefs?: string[];
  hostnames?: string[];
  labels?: Record<string, string>;
  updatedAt: string;
}

export interface GatewayMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface GatewayManifest {
  cluster: string;
  namespace: string;
  name: string;
  kind: string;
  yaml: string;
  metadata: GatewayMetadata;
}

export interface GatewayDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface GatewayApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  kind: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// Pod/Container Types
export interface ContainerStatus {
  name: string;
  image: string;
  ready: boolean;
  restartCount: number;
  state: string;
  stateReason?: string;
  started?: boolean;
}

export interface PodSummary {
  cluster: string;
  namespace: string;
  name: string;
  podIP?: string;
  nodeName?: string;
  phase: string;
  status?: string;
  statusReason?: string;
  labels?: Record<string, string>;
  containers: ContainerStatus[];
  readyContainers: number;
  totalContainers: number;
  cpuRequest?: string;
  memoryRequest?: string;
  cpuLimit?: string;
  memoryLimit?: string;
  resourceVersion?: string;
  createdAt: string;
  restarts: number;
}

export interface PodMetricsSingle {
  cpuMillicores: number;
  memoryBytes: number;
  cpuPercentRequest: number;   // -1 se não disponível
  cpuPercentLimit: number;
  memPercentRequest: number;
  memPercentLimit: number;
}

export interface BatchPodMetrics {
  available: boolean;
  pods: Record<string, PodMetricsSingle>;  // key: podName
}

export interface PodMetadata {
  uid?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface PodManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
  metadata: PodMetadata;
}

export interface InvolvedObjectRef {
  kind: string;
  name: string;
  namespace: string;
  uid?: string;
}

export interface EventSummary {
  cluster: string;
  namespace: string;
  name: string;
  type: string; // "Normal" ou "Warning"
  reason: string;
  message: string;
  count: number;
  firstTimestamp: string;
  lastTimestamp: string;
  involvedObject: InvolvedObjectRef;
  sourceComponent: string;
  sourceHost?: string;
  age: string;
}

export interface ResourceQuotaSummary {
  cluster: string;
  namespace: string;
  name: string;
  hard: ResourceLimit[];
}

export interface ResourceLimit {
  resource: string;
  hard: string;
  used: string;
  percent?: number;
}

export interface NetworkPolicySummary {
  cluster: string;
  namespace: string;
  name: string;
  podSelector: string;
  policyTypes: string[];
  ingress?: string;
  egress?: string;
}

export interface ServiceSummary {
  cluster: string;
  namespace: string;
  name: string;
  type: string;
  clusterIP: string;
  externalIP?: string;
  ports: string[];
  selector?: Record<string, string>;
  age?: string;
}

export interface ServiceManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
}

export interface ServiceDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface ServiceValidateResult {
  name: string;
  namespace: string;
  resourceVersion?: string;
}

export interface ServiceApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resourceVersion?: string;
  dryRun?: boolean;
  appliedAt?: string;
}

// VPA Types
export interface VPASummary {
  cluster: string;
  namespace: string;
  name: string;
  updateMode: string;
  targetRefName: string;
  targetRefKind: string;
  containerCount: number;
  hasRecommendation: boolean;
}

export interface VPAManifest {
  cluster: string;
  namespace: string;
  name: string;
  yaml: string;
}

export interface VPADiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface VPAValidateResult {
  valid: boolean;
}

export interface VPAApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  dryRun?: boolean;
}

export interface PodsSummary {
  total: number;
  running: number;
  pending: number;
  failed: number;
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
  image_version?: string;

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
  subscription: string; // Valor da config (pode ser nome ou UUID)
  subscription_name?: string; // Nome legível da subscription
  subscription_uuid?: string; // UUID real resolvido via az account show
  cluster_tags?: Record<string, string>; // Tags do cluster AKS
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
  cordon_drain_config?: CordonDrainConfig; // Configuração de Cordon/Drain (opcional)
}

// ============================================================================
// Node Management Types
// ============================================================================

export interface NodeInfo {
  name: string;
  status: string; // "Ready" | "NotReady" | "SchedulingDisabled"
  node_pool_name: string;
  cluster_name: string;
  resource_group: string; // Azure Resource Group
  subscription: string; // Subscription ID (UUID)
  subscription_name?: string; // Nome legível da subscription
  cluster_tags?: Record<string, string>; // Tags do cluster AKS
  kubernetes_version: string;
  provider_id: string;
  internal_ip: string;
  external_ip?: string;
  hostname: string;
  age: string;
  created_at: string;

  // Capacity e Allocatable
  cpu_capacity: string;
  memory_capacity: string;
  pods_capacity: number;
  cpu_allocatable: string;
  memory_allocatable: string;
  pods_allocatable: number;

  // Usage
  cpu_used: string;
  memory_used: string;
  cpu_usage_percent: number;
  memory_usage_percent: number;
  disk_usage_percent: number;

  // Pods count
  pods_running: number;
  pods_total: number;

  // Conditions
  conditions: NodeCondition[];

  // Taints and Labels
  taints?: NodeTaint[];
  labels: Record<string, string>;
  annotations?: Record<string, string>;

  // Flags
  unschedulable: boolean;
}

export interface NodeCondition {
  type: string;
  status: string;
  last_transition_time: string;
  reason?: string;
  message?: string;
}

export interface NodeTaint {
  key: string;
  value?: string;
  effect: string; // "NoSchedule" | "PreferNoSchedule" | "NoExecute"
  timeAdded?: string;
}

export interface NodeEvent {
  type: string; // "Normal" | "Warning"
  reason: string;
  message: string;
  count: number;
  first_timestamp: string;
  last_timestamp: string;
  source_component: string;
  source_host?: string;
}

export interface PendingWorkload {
  namespace: string;
  workload: string;
  kind: string;
  running: number;
  not_ready: number;
  oldest_age: string;
  reason: string;
  source: "dynatrace" | "k8s";
}

export interface PendingWorkloadsResponse {
  workloads: PendingWorkload[];
  source: "dynatrace" | "k8s";
  total: number;
}

export interface NodeResourceInfo {
  node_name: string;
  cpu_allocatable_m: number;
  cpu_requested_m: number;
  cpu_pct: number;
  mem_allocatable_bytes: number;
  mem_requested_bytes: number;
  mem_pct: number;
  pod_count: number;
  pod_capacity: number;
}

export interface NodeResourcesResponse {
  nodes: NodeResourceInfo[];
  total: number;
}

export interface AutoscalerNodeGroup {
  name: string;
  health: string;
  scale_up: string;
  scale_down: string;
  min: number;
  max: number;
  current: number;
}

export interface NodeDiskStats {
  node_name: string;
  disk_pressure: boolean;
  memory_pressure: boolean;
  pid_pressure: boolean;
  inodes_total: number;
  inodes_free: number;
  inodes_pct: number;
  read_bytes_per_sec: number;
  write_bytes_per_sec: number;
  io_util_pct: number;
  prometheus_available: boolean;
  error?: string;
}

export interface NodeDiskStatsResponse {
  nodes: NodeDiskStats[];
  total: number;
  prometheus_available: boolean;
}

export interface AutoscalerStatus {
  available: boolean;
  health: string;
  scale_up: string;
  scale_down: string;
  node_groups: AutoscalerNodeGroup[];
  fetched_at: string;
}

export interface PodOnNode {
  name: string;
  namespace: string;
  phase: string;
  cpu_request: string;
  memory_request: string;
  cpu_limit: string;
  memory_limit: string;
  restart_count: number;
  // Métricas em tempo real via Metrics Server (opcionais)
  cpu_usage?: string;
  memory_usage?: string;
  cpu_usage_pct?: number;
  memory_usage_pct?: number;
}

export interface NodeDetailsResponse {
  node: NodeInfo;
  pods: PodOnNode[];
  events: NodeEvent[];
  kubectl_describe?: string;
}

export interface NodesListResponse {
  nodes: NodeInfo[];
  count: number;
  node_pool_name: string;
  cluster: string;
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
  folder?: string;
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
  cordon_drain_config?: CordonDrainConfig; // Configuração de Cordon/Drain (opcional)

}

export interface NodePoolValues {
  node_count: number;
  min_node_count: number;
  max_node_count: number;
  autoscaling_enabled: boolean;
}

// Node Pool Cordon/Drain Types
export interface NodePoolChanges {
  autoscaling: boolean;
  node_count: number;
  min_nodes: number;
  max_nodes: number;
}

export interface DrainOptions {
  // Essentials
  ignore_daemonsets: boolean;
  delete_emptydir_data: boolean;
  force: boolean;
  grace_period: number;
  timeout: string;
  // Advanced
  disable_eviction: boolean;
  skip_wait_for_delete_timeout: number;
  pod_selector: string;
  dry_run: boolean;
  chunk_size: number;
}

// Configuração de Cordon/Drain para Node Pools (salvável em sessões)
export interface CordonDrainConfig {
  cordon_enabled: boolean;      // Habilitar CORDON (marca nodes como unschedulable)
  drain_enabled: boolean;        // Habilitar DRAIN (evacua pods dos nodes)
  grace_period: number;          // Tempo de espera antes de forçar término (padrão: 300s)
  timeout: number;               // Timeout máximo para drain (padrão: 600s)
  force_delete: boolean;         // ⚠️ Ignora PodDisruptionBudget (perigoso!)
  ignore_daemonsets: boolean;    // Ignora DaemonSets durante drain (padrão: true)
  delete_emptydir: boolean;      // Deleta volumes EmptyDir durante drain
  chunk_size: number;            // Pods evacuados simultaneamente (padrão: 5)
}

export interface NodePoolSequenceConfig {
  name: string;
  resource_group: string;
  subscription: string;
  sequence_order: number; // 1 or 2
  pre_drain_changes?: NodePoolChanges;
  post_drain_changes?: NodePoolChanges;
}

export interface SequenceExecuteRequest {
  cluster: string;
  node_pools: NodePoolSequenceConfig[];
  cordon_enabled: boolean;
  drain_enabled: boolean;
  drain_options: DrainOptions;
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

// Monitoring Types
export interface MonitoringStatus {
  running: boolean;
  mode: string;
  interval: string;
  clusters: number;
  last_scan: string | null;
  total_scans: number;
  port_info?: Record<string, number>; // cluster -> porta
}

export interface HPASnapshot {
  cluster: string;
  namespace: string;
  hpa_name: string;
  timestamp: string;
  cpu_current: number;
  cpu_target: number;
  memory_current: number;
  memory_target: number;
  replicas_current: number;
  replicas_desired: number;
  replicas_ready?: number;
  replicas_min: number;
  replicas_max: number;
  // Resource Request/Limit do deployment (vem do K8s API)
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;

  // Extended metrics (Prometheus)
  request_rate?: number;
  error_rate?: number;
  p95_latency?: number;
  p99_latency?: number;
  network_rx_bytes?: number;
  network_tx_bytes?: number;
}

export interface HPAMetrics {
  cluster: string;
  namespace: string;
  hpa_name: string;
  duration: string;
  snapshots: HPASnapshot[];
  snapshots_yesterday?: HPASnapshot[];  // Dados de ontem para comparação D-1
  count: number;
  count_yesterday?: number;             // Contagem de snapshots de ontem
  message?: string;
}

export interface Anomaly {
  id: string;
  cluster: string;
  namespace: string;
  hpa_name: string;
  type: string;
  severity: "low" | "medium" | "high" | "critical";
  detected_at: string;
  duration_seconds: number;
  message: string;
  details: Record<string, any>;
  resolved: boolean;
  resolved_at?: string;
}

export interface Anomalies {
  cluster?: string;
  severity: string;
  anomalies: Anomaly[];
  count: number;
  message?: string;
}

export interface HPAHealth {
  cluster: string;
  namespace: string;
  hpa_name: string;
  status: "healthy" | "warning" | "critical";
  anomalies: Anomaly[];
  message?: string;
  score?: number; // 0-100
  recommendations?: string[];
}

// Namespace Metrics Types
export interface NamespaceMetrics {
  namespace: string;
  cpu_request_millis: number;
  cpu_usage_millis: number;
  cpu_percent_of_cluster: number;
  memory_request_gb: number;
  memory_usage_gb: number;
  memory_percent_of_cluster: number;
  pod_count: number;
  pod_percent_of_cluster: number;
}

export interface TopNamespacesResponse {
  top_cpu: NamespaceMetrics[];
  top_memory: NamespaceMetrics[];
  top_pods: NamespaceMetrics[];
  cpu_others: NamespaceMetrics;
  memory_others: NamespaceMetrics;
  pods_others: NamespaceMetrics;
  total_namespaces: number;
}

// GitHub Releases Types
export interface GitHubRepoInfo {
  owner: string;
  repo: string;
}

export interface DeploymentConfig {
  name: string;
  app_name: string;
  github_repo: GitHubRepoInfo;
  version: string;
  last_published: string;
  squad: string;
  servicenow_task: string;
  age: string;
}

export interface NamespaceConfig {
  name: string;
  deployments: DeploymentConfig[];
}

export interface ClusterConfig {
  name: string;
  namespaces: NamespaceConfig[];
}

export interface GitHubReposConfig {
  clusters: ClusterConfig[];
  total_clusters: number;
  total_namespaces: number;
  total_deployments: number;
}

export interface GitHubRelease {
  tag_name: string;
  name: string;
  body: string;
  created_at: string;
  published_at: string;
  prerelease: boolean;
  draft: boolean;
}

export interface GitHubReleasesResponse {
  owner: string;
  repo: string;
  releases: GitHubRelease[];
  total: number;
}

export interface GitHubCommit {
  sha: string;
  message: string;
  author: string;
  date: string;
  url: string;
}

export interface GitHubFile {
  filename: string;
  status: "added" | "modified" | "deleted" | "renamed";
  additions: number;
  deletions: number;
  extension: string;
  patch?: string;
}

export interface GitHubComparison {
  base_tag: string;
  head_tag: string;
  commits: GitHubCommit[];
  files_changed: GitHubFile[];
  ahead_by: number;
  behind_by: number;
  base_release_notes: string;
  head_release_notes: string;
}

// GitHub Deployment Search Types
export interface DeploymentRecord {
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  full_image: string;
  status: string;
  last_seen: string;
}

export interface DeploymentSearchResponse {
  app_name: string;
  deployments: DeploymentRecord[];
  total: number;
}

export interface ProductionDeploymentResponse {
  deployment: string;
  namespace: string;
  cluster: string;
  version: string;
  image: string;
  status: string;
  last_seen: string;
}

export interface VersionMap {
  [version: string]: DeploymentRecord[];
}

export interface AllVersionsResponse {
  app_name: string;
  versions: VersionMap;
  total_versions: number;
  total_deployments: number;
}

// GitHub Token Types
export interface TokenStatusResponse {
  valid: boolean;
  username?: string;
  email?: string;
  remaining?: number;
  limit?: number;
  reset_at?: string;
  configured: boolean;
  error?: string;
}

export interface SaveTokenRequest {
  token: string;
  email: string;
}

export interface SaveTokenResponse {
  success: boolean;
  message: string;
  github_user?: string;
  github_email?: string;
}

// ==================== ServiceNow Integration Types ====================

export interface ServiceNowExtractedData {
  application?: string;
  version?: string;
  github_repo?: string;
  squad?: string;
  branch?: string;
  jira_issues?: string[];
  product?: string;
  project?: string;
  xlrelease_url?: string;
  xlrelease_title?: string;
  severity?: string;
  confidence: number;
}

export interface ServiceNowChangeRequest {
  sys_id?: string;
  number?: string;
  short_description?: string;
  description?: string;
  state?: string;
}

export interface ServiceNowImportResponse {
  success: boolean;
  change_request?: ServiceNowChangeRequest;
  extracted_data?: ServiceNowExtractedData;
  error?: string;
}

export interface ServiceNowParseResponse {
  success: boolean;
  extracted_data?: ServiceNowExtractedData;
  error?: string;
}

export interface ServiceNowPlaywrightResponse {
  success: boolean;
  change_number?: string;
  short_description?: string;
  description?: string;
  state?: string;
  extracted_data?: ServiceNowExtractedData;
  error?: string;
}

export interface PlaywrightStatusResponse {
  playwright_configured: boolean;
  frontend_dir: string;
  script_exists: boolean;
  npx_available: boolean;
  ts_node_available: boolean;
  // Modo de browser
  wsl_mode: boolean;     // true = WSL sem display, usa Chrome Windows via CDP automaticamente
  is_wsl: boolean;       // true = rodando no WSL (com ou sem display)
  has_display: boolean;  // true = display gráfico disponível no WSL
}

export interface ServiceNowBrowserConfig {
  force_windows_browser: boolean;
  windows_session_dir: string;       // caminho configurado pelo usuário (vazio = auto)
  effective_session_dir: string;     // caminho efetivo em uso (após auto-detecção)
  needs_windows_browser: boolean;
  is_wsl: boolean;
  has_display: boolean;
  active_mode?: string;
}

export interface ServiceNowBatchItem {
  chg: string;
  url: string;
  approval_url?: string;
}

export interface ServiceNowBatchResultItem {
  chg: string;
  success: boolean;
  change_number?: string;
  short_description?: string;
  description?: string;
  state?: string;
  extracted_data?: ServiceNowExtractedData;
  error?: string;
}

export interface ServiceNowBatchResponse {
  success: boolean;
  results: ServiceNowBatchResultItem[];
  error?: string;
}

// ==================== Resource Explorer Types ====================

export interface APIResourceInfo {
  kind: string;
  name: string;       // plural (e.g. "externalsecrets")
  group: string;      // API group (e.g. "external-secrets.io"; vazio para core)
  version: string;    // e.g. "v1", "v1beta1"
  namespaced: boolean;
  verbs: string[];
}

export interface GenericResourceSummary {
  name: string;
  namespace: string;
  kind: string;
  apiVersion: string;
  age: string;
  labels: Record<string, string>;
  additionalColumns: Record<string, string>;
}

export interface GenericResourceManifest {
  cluster: string;
  namespace: string;
  kind: string;
  name: string;
  yaml: string;
}

export interface ExplorerDiffResult {
  unifiedDiff: string;
  hasChanges: boolean;
}

export interface ExplorerApplyResult {
  name: string;
  namespace: string;
  cluster: string;
  resource: string;
  dryRun: boolean;
}

// AWX Integration (certificados TLS via Ansible AWX/Tower)
export interface SSOProfile {
  configured: boolean;
  email?: string;
  matricula?: string;
  has_password?: boolean;
}

export interface AWXStatus {
  configured: boolean;
  reachable: boolean;
  base_url?: string;
  username?: string;
  use_sso_profile?: boolean;
  login_identifier?: string;  // "email" | "matricula"
  version?: string;
  error?: string;
}

export interface AWXCertificate {
  id: number;
  name: string;
}

export interface AWXJobLaunch {
  job_id: number;
}

// Command Runner
export interface CommandTarget {
  cluster: string;
  namespace: string;
}

export type CommandType = 'kubectl' | 'sh' | 'bash' | 'python' | 'go';

export interface ExecuteCommandRequest {
  targets: CommandTarget[];
  command: string;
  type: CommandType;
  timeout_sec?: number;
}

export interface ExecuteCommandResponse {
  session_id: string;
}

export interface GenerateCommandRequest {
  prompt: string;
  cluster: string;
  namespace: string;
  clusters?: string[];    // todos os clusters selecionados (contexto para AI)
  namespaces?: string[];  // todos os namespaces selecionados (contexto para AI)
  ai_email: string;
  cmd_type?: string;
  explain?: boolean;
}

export interface GenerateCommandResponse {
  command: string;
  type: CommandType;
  explanation?: string;
}

// SSE event do Command Runner (estende o ProgressEvent do backend)
// ─── NodePool Registry (correlação Dynatrace aks-<pool>-vmss*) ────────────────

export interface NodePoolRegistryEntry {
  cluster: string;
  nodepool: string;
  node_count: number;
  vm_size?: string;
  os_sku?: string;
  mode?: string;       // System | User
  last_scanned: string;
}

export interface NodePoolLookupResult {
  entity_name: string;
  nodepool: string;    // nome do pool extraído do entity_name
  matches: NodePoolRegistryEntry[];
  found: boolean;
}

// ─── Conntrack Stats ──────────────────────────────────────────────────────────

export interface ConntrackNodeStats {
  node_name: string;
  count: number;
  max: number;
  buckets: number;
  usage_pct: number;
  status: 'ok' | 'warning' | 'critical' | 'error';
  probe_method: string;
  error?: string;
}

export interface ConntrackResponse {
  node_pool: string;
  cluster: string;
  nodes: ConntrackNodeStats[];
  fetched_at: string;
}

export interface ConntrackHistoryPoint {
  ts: number;       // Unix timestamp (segundos)
  count: number;    // nf_conntrack_entries
  max: number;      // nf_conntrack_entries_limit (0 se indisponível)
  usage_pct: number;
}

export interface ConntrackNodeHistoryResponse {
  node_name: string;
  hours: number;
  step_minutes: number;
  points: ConntrackHistoryPoint[];
  prometheus_available: boolean;
  error?: string;
}

export interface CommandRunnerSSEEvent {
  id: string;
  type: 'init' | 'output' | 'output_error' | 'cluster_done' | 'complete' | 'error';
  phase: string;
  message: string;
  cluster?: string;
  details?: string; // namespace
  progress: number;
  timestamp: string;
  error?: string;
}

// Permissões reais do K8s — retornadas pelo SelfSubjectRulesReview.
// Refletem o que o RBAC do cluster permite para o usuário atual (não grupos AD).
export interface K8sNamespacePermissions {
  cluster: string;
  namespace: string;
  canListHPA: boolean;
  canGetHPA: boolean;
  canUpdateHPA: boolean;
  canListPods: boolean;
  canExecPods: boolean;
  canViewLogs: boolean;
  canListDeployments: boolean;
  canUpdateDeployment: boolean;
  canWriteSecrets: boolean;
  canWriteConfigMaps: boolean;
  canWriteCronJobs: boolean;
  canWriteServices: boolean;
  canWriteIngress: boolean;
  canWriteStatefulSets: boolean;
  canWriteDaemonSets: boolean;
  canWritePods: boolean;
  incomplete: boolean;
}

// Verificar Acesso — checa se um analista (e-mail) tem acesso a um cluster/namespace via
// impersonation K8s (SelfSubjectRulesReview/SelfSubjectAccessReview) + grupos AAD VV_CLOUD_*.
export interface AccessCheckMatchedGroup {
  id: string;
  displayName: string;
}

export interface AccessCheckResourceRule {
  verbs: string[];
  apiGroups?: string[];
  resources?: string[];
  resourceNames?: string[];
}

export interface AccessCheckNonResourceRule {
  verbs: string[];
  nonResourceURLs?: string[];
}

export interface AccessCheckRulesResult {
  resourceRules: AccessCheckResourceRule[];
  nonResourceRules: AccessCheckNonResourceRule[];
  incomplete: boolean;
  matchedGroups: AccessCheckMatchedGroup[];
  allGroups: AccessCheckMatchedGroup[];
  groupsResolutionError?: string;
}

export interface AccessCheckCanIResult {
  allowed: boolean;
  denied: boolean;
  reason?: string;
  matchedGroups: AccessCheckMatchedGroup[];
  allGroups: AccessCheckMatchedGroup[];
  groupsResolutionError?: string;
}
