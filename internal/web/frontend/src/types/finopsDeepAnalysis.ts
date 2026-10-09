// Tipos da Deep Analysis de node pool — espelham internal/finops/deep_analysis*.go
// (GET /api/v1/finops/deep-analysis). Ver FINOPS-DEEP-ANALYSIS-PLAN.md.

export interface DeepCoverage {
  reservation: number;
  savings_plan: number;
  on_demand: number;
  spot: number;
  effective_monthly_brl: number;
  reserved_monthly_brl: number;
  savings_plan_monthly_brl: number;
  on_demand_monthly_brl: number;
  reserved_nodes: number;
  reserved_node_monthly_brl: number;
  table_discount_pct: number;
  currency?: string;
  window_days: number;
  fetched_at: string;
  computable: boolean;
  note?: string;
}

export interface DeepCoverageScenario {
  basis: "same_sku" | "same_series" | "other_series";
  worst_monthly_brl: number;
  best_monthly_brl: number;
  worst_savings_brl: number;
  best_savings_brl: number;
  idle_reserved_nodes: number;
  note: string;
}

export interface SKUCoverageHint {
  kind: "reservation" | "savings_plan";
  scope: "sku" | "series";
  note: string;
}

export interface DeepPoolOverview {
  vm_size: string;
  vcpu: number;
  mem_gb: number;
  cpu?: string;
  smt: boolean;
  nodes: number;
  zones: string[];
  region?: string;
  priority: string;
  os_disk?: string;
  max_pods: number;
  pods_running: number;
  price_usd_hour: number;
  price_source?: string;
  monthly_cost_brl: number;
  exchange_rate: number;
  coverage?: DeepCoverage;
}

export interface DeepResource {
  allocatable: number;
  requests: number;
  limits: number;
  usage_live: number;
  has_live: boolean;
  usage_p95: number;
  has_p95: boolean;
  request_pct: number;
  limit_pct: number;
  usage_live_pct: number;
  usage_p95_pct: number;
}

export interface DeepAllocation {
  cpu: DeepResource;
  mem: DeepResource;
  scheduling_bound: "cpu" | "memory";
  real_bottleneck: "cpu" | "memory" | "";
  mismatch: boolean;
  explanation: string;
  nodes_mem_above_90: number;
  nodes_mem_above_95: number;
  nodes_cpu_above_90: number;
}

export interface DeepNodeRow {
  name: string;
  zone?: string;
  cpu_alloc_millis: number;
  mem_alloc_mi: number;
  pods: number;
  cpu_req_pct: number;
  mem_req_pct: number;
  cpu_lim_pct: number;
  mem_lim_pct: number;
  cpu_usage_pct: number;
  mem_usage_pct: number;
  has_usage: boolean;
  created_at?: string;
}

export interface DeepDaemonSetRow {
  namespace: string;
  name: string;
  pods: number;
  cpu_req_millis: number;
  mem_req_mi: number;
  mem_lim_mi: number;
  cpu_usage_avg_millis: number;
  cpu_usage_max_millis: number;
  mem_usage_avg_mi: number;
  mem_usage_max_mi: number;
  has_usage: boolean;
  flags: string[];
}

export interface DeepDaemonSetOverhead {
  per_node_cpu_req_millis: number;
  per_node_mem_req_mi: number;
  per_node_cpu_usage_millis: number;
  per_node_mem_usage_mi: number;
  per_node_pods: number;
  has_usage: boolean;
  cpu_overhead_pct: number;
  mem_overhead_pct: number;
  monthly_cost_brl: number;
  items: DeepDaemonSetRow[];
}

export interface DeepHPAMetric {
  type: string;
  name: string;
  target_type: string;
  target_utilization?: number;
  current_utilization?: number;
  target_value?: string;
}

export interface DeepHPAProjection {
  resource: "cpu" | "memory";
  target: number;
  current_pct: number;
  plain_projected_pct: number;
  projected_pct: number;
  would_trigger_scale_up: boolean;
  adjusted_for_hpa: boolean;
}

export interface DeepHPAView {
  name: string;
  min: number;
  max: number;
  current: number;
  metrics: DeepHPAMetric[];
  state: "fixed" | "pinned_min" | "pinned_max" | "scaling";
  avg_replicas?: number;
  max_observed?: number;
  min_observed?: number;
  scale_events?: number;
  never_scaled?: boolean;
  projections: DeepHPAProjection[];
}

export interface DeepWorkloadRow {
  namespace: string;
  workload: string;
  kind: string;
  pods_on_pool: number;
  cpu_req_millis: number;
  mem_req_mi: number;
  cpu_lim_millis: number;
  mem_lim_mi: number;
  cpu_usage_avg_millis: number;
  cpu_usage_max_millis: number;
  mem_usage_avg_mi: number;
  mem_usage_max_mi: number;
  has_live_usage: boolean;
  cpu_p95_millis?: number;
  cpu_peak_millis?: number;
  mem_p95_mi?: number;
  mem_peak_mi?: number;
  has_history: boolean;
  usage_basis: "p95" | "live" | "";
  cpu_usage_vs_request_pct: number;
  mem_usage_vs_request_pct: number;
  cpu_request_share_pct: number;
  cpu_rec_millis: number;
  mem_rec_mi: number;
  mem_limit_rec_mi: number;
  throttle_p95_pct?: number;
  throttle_current_pct?: number;
  has_throttle: boolean;
  cpu_limit_rec_millis?: number;
  hpa?: DeepHPAView;
  flags: string[];
}

export interface DeepSimulation {
  vm_size: string;
  vcpu: number;
  mem_gb: number;
  cpu?: string;
  smt: boolean;
  series?: string;
  is_current: boolean;
  alloc_cpu_millis: number;
  alloc_mem_mi: number;
  alloc_estimated: boolean;
  price_usd_hour: number;
  price_source?: string;
  nodes_current_req: number;
  cost_current_req_brl: number;
  limiting_current_req: string;
  nodes_recommended: number;
  cost_recommended_brl: number;
  limiting_recommended: string;
  savings_recommended_brl: number;
  node_loss_impact_pct: number;
  feasible: boolean;
  catalog_known: boolean;
  available: boolean;
  ephemeral_os_disk?: boolean;
  zones?: string[];
  vcpus_per_core?: number;
  coverage?: DeepCoverageScenario;
  reserved_hint?: SKUCoverageHint;
  notes: string[];
}

export interface DeepFinding {
  severity: "critical" | "warning" | "info";
  code: string;
  title: string;
  detail: string;
}

export interface DeepHistoryMeta {
  available: boolean;
  generated_at?: string;
  window_days?: number;
  covered: number;
  total: number;
}

export interface PoolDeepAnalysis {
  cluster: string;
  pool: string;
  generated_at: string;
  overview: DeepPoolOverview;
  allocation: DeepAllocation;
  nodes: DeepNodeRow[];
  daemonsets: DeepDaemonSetOverhead;
  workloads: DeepWorkloadRow[];
  simulation: DeepSimulation[];
  headroom: number;
  findings: DeepFinding[];
  history: DeepHistoryMeta;
  metrics_live: boolean;
  throttle_window_days: number;
  sku_catalog_status?: "fresh" | "stale" | "loading" | "unavailable";
  sku_catalog_fetched_at?: string;
  warnings: string[];
}
