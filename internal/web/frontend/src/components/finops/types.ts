// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — tipos compartilhados entre as abas do FinOps, extraídos de
// FinOpsTab.tsx (que tinha 4589 linhas num único arquivo) pro mesmo padrão já usado por
// RightsizingTab.tsx/DataResourcesPanel.tsx (componentes próprios) e útil pros consumidores destas
// abas não precisarem importar o arquivo inteiro só pra ler um tipo.


import type { PoolCoverage } from "./PricingCoverage";

export interface FinOpsPool {
  name: string;
  vm_size: string;
  vm_cpu_cores: number;
  vm_memory_gb: number;
  vm_price_usd_hour: number;
  price_source: string;
  node_count: number;
  mode: string;
  monthly_cost_usd: number;
  monthly_cost_brl: number;
  total_cpu_millicores: number;
  total_memory_mi: number;
  // Alocável somado dos nodes vivos (status.allocatable); ausente em relatório antigo.
  allocatable_cpu_millicores?: number;
  allocatable_memory_mi?: number;
  allocatable_nodes?: number;
  // Reserva / Savings Plan / sob demanda / spot no custo efetivo do pool (Cost Management, só AKS).
  coverage?: PoolCoverage;
  // Storage — disco OS por nó
  os_disk_sku?: string;
  os_disk_tier?: string;
  os_disk_gb?: number;
  os_disk_cost_usd?: number;
  os_disk_cost_brl?: number;
  os_disk_effective_brl?: number; // custo efetivo do disco do SO (Cost Management)
  total_cost_usd?: number;
  total_cost_brl?: number;
}

export interface FinOpsNamespace {
  namespace: string;
  workload_count: number;
  monthly_cost_usd: number;
  monthly_cost_brl: number;
}

export interface FinOpsWorkload {
  namespace: string;
  workload: string;
  pods: number;
  cpu_request_millis: number;
  mem_request_mi: number;
  cpu_limit_millis?: number;
  mem_limit_mi?: number;
  cpu_limit_recommended_millis?: number;
  mem_limit_recommended_mi?: number;
  kind?: string; // Deployment | StatefulSet | DaemonSet | Job | ReplicaSet | Pod
  // Pool com mais pods do workload (best-effort, pela label do node; vazio se não reconhecida).
  node_pool?: string;
  pool_pods?: Record<string, number>; // pods em cada pool (DaemonSet tem em vários)
  // Só no frontend (toEffectiveReport): valores em reais no custo efetivo do pool, e pool sob
  // reserva/Savings Plan/Spot (economia nele não é certa).
  cost_basis?: "effective" | "table";
  committed?: boolean;
  cost_share_usd: number;
  cost_share_brl: number;
  hpa_min: number;
  hpa_max: number;
  hpa_current: number;
  hpa_name?: string; // nome do HPA (pode diferir do workload)
  hpa_cost_min_brl: number;
  hpa_cost_max_brl: number;
  hpa_cost_current_brl: number;
  verdict: "superprovisioned" | "ok" | "oom_risk" | "no_request" | "hpa_removable" | "fixed_high_cost" | "sem_dados";
  // Prometheus — uso real CPU/Mem (últimos N dias)
  cpu_avg_millis?: number;
  cpu_p95_millis?: number;
  cpu_recommended_millis?: number;
  mem_avg_mi?: number;
  mem_p95_mi?: number;
  mem_recommended_mi?: number;
  // Prometheus — histórico HPA
  hpa_avg_replicas?: number;
  hpa_max_observed?: number;
  hpa_min_observed?: number;
  hpa_scale_events?: number;
  hpa_never_scaled?: boolean;
  // Isenção (aplicada pelo backend na leitura do relatório — FINOPS-EXEMPTIONS-PLAN.md).
  // exempt_active: dentro do limite de réplicas → verdict "ok" e waste 0; os originais vêm em exempt_original_*.
  is_exempt?: boolean;
  exempt_reason?: string;
  exempt_threshold?: number;
  exempt_active?: boolean;
  exempt_replicas?: number;
  exempt_original_verdict?: string;
  exempt_original_waste_brl?: number;
  avg_replicas_cost_brl?: number;
  waste_brl?: number;
  metrics_source?: "dynatrace" | "prometheus" | "";
  // Storage — PVCs correlacionados
  storage_cost_usd?: number;
  storage_cost_brl?: number;
  pvc_count?: number;
  pvc_capacity_gb?: number;
}

export interface FinOpsSummary {
  total_monthly_cost_brl: number;
  total_monthly_cost_usd: number;
  top_namespace: string;
  potential_savings_brl: number;
  hpa_savings_if_min_brl: number;
  workloads_analyzed: number;
  superprovisioned_count: number;
  oom_risk_count: number;
  no_request_count: number;
  hpa_removable_count: number;
  fixed_high_cost_count: number;
  // no_data_count — verdict "sem_dados": workloads que nunca receberam enriquecimento de uso
  // real (Prometheus/DT indisponíveis pra ele especificamente), distintos de "ok" (verificado e
  // saudável) — ver internal/finops/models.go.
  no_data_count?: number;
  // Storage
  storage_monthly_cost_brl?: number;
  storage_monthly_cost_usd?: number;
  os_disk_cost_brl?: number;
  orphaned_storage_cost_brl?: number;
  total_with_storage_brl?: number;
  // Cobertura de métricas reais (Dynatrace/Prometheus) — usado pra distinguir "cluster sem
  // desperdício" de "falha silenciosa de coleta" (ver internal/finops/models.go).
  metrics_attempted?: boolean;
  metrics_workloads_enriched?: number;
  // metrics_collection_error — SEMPRE presente (mesmo "") num relatório fresco; AUSENTE
  // (undefined) só num relatório antigo já persistido/cacheado de antes deste campo existir.
  // "" = consultas tiveram sucesso mas sem nenhum dado real (cluster sem cobertura de
  // monitoramento); não-vazia = pelo menos uma consulta falhou de verdade (falha transitória).
  // Ver comentário completo em internal/finops/models.go.
  metrics_collection_error?: string;
  // true quando a ÚNICA causa foram timeouts das queries pesadas de container do Prometheus (as
  // leves de HPA responderam) — o Prometheus está no ar; a orientação é uma janela menor.
  metrics_collection_timeout?: boolean;
  prometheus_url?: string; // endpoint consultado no scan (vazio = sem Prometheus)
  prometheus_unavailable?: string; // motivo quando o teste de presença falhou e o scan seguiu sem Prometheus
}

export interface PVCCostItem {
  namespace: string;
  name: string;
  bound_pv: string;
  storage_class: string;
  capacity_gb: number;
  azure_disk_type: string;
  azure_disk_tier: string;
  price_per_month: number;
  monthly_cost_usd: number;
  monthly_cost_brl: number;
  phase: string;
  reclaim_policy: string;
  workload_ref: string;
  is_orphaned: boolean;
  effective_cost_brl?: number; // custo efetivo no mês (Cost Management, disco pvc-<uid>)
  price_source: string;
}

export interface StorageClassBreakdown {
  storage_class: string;
  azure_type: string;
  pvc_count: number;
  total_gb: number;
  monthly_cost_brl: number;
}

export interface StorageSummary {
  total_monthly_cost_usd: number;
  total_monthly_cost_brl: number;
  os_disk_cost_usd: number;
  os_disk_cost_brl: number;
  pvc_count: number;
  bound_pvc_count: number;
  orphaned_pvc_count: number;
  orphaned_cost_brl: number;
  total_capacity_gb: number;
  by_storage_class: StorageClassBreakdown[];
  by_namespace: Record<string, number>;
}

export interface FinOpsReport {
  cluster: string;
  generated_at: string;
  exchange_rate: number;
  exchange_date: string;
  window_days: number;
  node_pools: FinOpsPool[];
  namespaces: FinOpsNamespace[];
  workloads: FinOpsWorkload[];
  summary: FinOpsSummary;
  pvcs?: PVCCostItem[];
  storage?: StorageSummary;
}

// ─── Tipos: Timeline ──────────────────────────────────────────────────────────

export interface HPADayPoint {
  date: string;         // "2026-02-24"
  max_replicas: number;
  avg_replicas: number;
  min_replicas: number;
}

export interface HPATimeline {
  namespace: string;
  workload: string;
  hpa_min: number;
  hpa_max: number;
  series: HPADayPoint[];
}

export interface CPUDayPoint {
  date: string;
  used_millis: number;
  req_millis: number;
}

export interface MemDayPoint {
  date: string;
  used_mi: number;
  req_mi: number;
}

export interface TimelineReport {
  cluster: string;
  start_date: string;
  end_date: string;
  hpas: HPATimeline[];
  nodes: { date: string; node_count: number }[];
  cpu: CPUDayPoint[];
  mem: MemDayPoint[];
}

export interface TimelineCompareResponse {
  cluster: string;
  days: number;
  current: TimelineReport;
  previous?: TimelineReport;
  has_previous: boolean;
  previous_saved_at?: string;
}

export interface TimelineSnapshotMeta {
  id: string;
  cluster: string;
  start_date: string;
  end_date: string;
  days: number;
  saved_at: string;
}

// ─── Recomendações concretas ──────────────────────────────────────────────────

export interface Recommendation {
  lines: { text: string; highlight?: boolean }[];
  safeMin?: number;        // novo min de réplicas sugerido
  safeMax?: number;        // novo max de réplicas sugerido
  savingBRL: number;       // economia imediata estimada (réplicas atuais sendo reduzidas)
  exposureBRL: number;     // exposição de custo máximo (se escalar ao máximo configurado)
  needsPrometheus: boolean; // verdadeiro quando saving = 0 só por falta de dados
  kubectlList: string[];   // lista de comandos kubectl aplicáveis (pode ter HPA + resources)
}

// ─── Discos desatachados (GET /api/v1/finops/unattached-disks) ─────────────────
// Shape de finops.UnattachedDisksReport (internal/finops/unattached_disks.go) — só leitura: a app
// nunca exclui disco, cada item traz o comando pra copiar.

export type UnattachedDiskVerdict = "candidate" | "review" | "in_use_by_pv";
export type UnattachedDiskOrigin = "cluster_pv" | "k8s_tags" | "external";

export interface UnattachedDiskItem {
  provider: "azure" | "gcp" | "aws";
  id: string;
  name: string;
  resource_group?: string;
  subscription_id?: string; // Azure, escopo por jornada
  journey?: string;         // Azure, escopo por jornada
  journey_source?: JourneySource;
  cluster?: string;         // cluster contra o qual o veredito foi calculado (escopo por jornada)
  location?: string;
  zone?: string;
  size_gb: number;
  disk_type: string;
  disk_state?: string;
  provisioned_iops?: number;  // só Azure Ultra/Premium SSD v2 e GCP Hyperdisk
  provisioned_mbps?: number;
  created_at?: string;
  unattached_since?: string;
  tags?: Record<string, string>;
  k8s_pv_name?: string;
  k8s_pvc_name?: string;
  k8s_pvc_namespace?: string;
  k8s_cluster_hint?: string;

  monthly_cost_usd: number;
  monthly_cost_brl: number;
  price_source: "api" | "fallback" | "table" | "unsupported" | "unpriced";

  age_days: number;
  age_basis: "unattached" | "created" | "";
  origin: UnattachedDiskOrigin;
  verdict: UnattachedDiskVerdict;
  reason: string;
  cluster_match: boolean;

  pv_name?: string;
  pv_phase?: string;
  pvc?: string;
  reclaim_policy?: string;
  storage_class?: string;
  delete_command?: string;
}

export interface UnattachedDisksSummary {
  total_count: number;
  total_size_gb: number;
  total_cost_usd: number;
  total_cost_brl: number;
  unpriced_count: number;
  candidate_count: number;
  candidate_cost_brl: number;
  review_count: number;
  review_cost_brl: number;
  in_use_by_pv_count: number;
  in_use_by_pv_cost_brl: number;
}

export interface UnattachedDisksReport {
  cluster: string;
  provider: "azure" | "gcp" | "aws";
  scope: string;
  disks: UnattachedDiskItem[];
  summary: UnattachedDisksSummary;
  exchange_rate: number;
  exchange_date: string;
  pv_cross_ref: boolean;
  warnings: string[];
  scanned_at: string;
  from_cache: boolean;
}

// ─── Recursos órfãos por jornada (GET /finops/orphan-resources, só Azure) ────
// De onde veio a jornada de um recurso: tag do recurso, tag do RG ou node RG (MC_*) do cluster.
export type JourneySource = "resource_tag" | "rg_tag" | "node_rg" | "none";

export interface ScopedResourceGroup {
  subscription_id: string;
  resource_group: string;
  journey?: string;
  journey_tag?: string; // valor da tag "jornada" do próprio RG
  source: "tag" | "cluster" | "node" | "data";
  cluster?: string;
  environment?: string;     // prd/hlg/... (tag de ambiente ou nome do RG)
  env_tag?: string;
  excluded_reason?: string; // só nos RGs ignorados
}

export interface OrphanResource {
  id: string;
  name: string;
  type: string; // tipo ARM em minúsculas
  kind?: string;
  location?: string;
  resource_group: string;
  subscription_id: string;
  journey?: string;
  journey_source?: JourneySource;
  reason: string;
  sku?: string;
  tags?: Record<string, string>;
  last_change?: string;
  since_basis: "last_change" | "no_change_14d" | "unknown";
  age_days: number; // -1 = desconhecida
  recent: boolean;  // alterado há menos de min_age_days
  monthly_cost_usd: number;
  monthly_cost_brl: number;
  price_note?: string;
  delete_command?: string;
}

export interface OrphanSummary {
  total_count: number;
  aged_count: number;
  recent_count: number;
  total_cost_brl: number;
  aged_cost_brl: number;
  by_type: Record<string, number>;
}

export interface OrphanResourcesResponse {
  journeys: string[];
  scope: string;
  resource_groups: ScopedResourceGroup[];
  excluded_resource_groups: ScopedResourceGroup[] | null; // da jornada, mas de outro ambiente / sem ambiente
  environment: string; // ambiente do cluster analisado ("" = não identificado)
  ignored: { other_journey: number; no_journey: number }; // órfãos deixados de fora pela jornada
  clusters: number;
  disks: UnattachedDisksReport;
  orphans: OrphanResource[];
  orphan_summary: OrphanSummary;
  min_age_days: number;
  exchange_rate: number;
  scanned_at: string;
  from_cache: boolean;
  warnings: string[];
}

// Custo faturado (Cost Management, amortizado) dos RGs do cluster AKS — GET /finops/billed-cost.
export interface BilledService {
  service: string;
  cost: number;
}

export interface BilledCostResponse {
  available: boolean;
  reason?: string;
  cluster: string;
  provider?: "aks" | "eks" | "gke";
  currency?: string;
  window_days?: number;
  from?: string;
  to?: string;
  fetched_at?: string;
  total: number;
  services?: BilledService[];
  resource_groups?: { name: string; role: "cluster" | "nodes" | "data"; total: number; services: BilledService[] }[];
}

// Isenção por workload — GET/POST/DELETE /finops/exemptions.
export interface FinOpsExemption {
  id: number;
  cluster: string;
  namespace: string;
  workload: string;
  max_replicas_threshold: number;
  reason: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

// Linha da visão multi-cluster — GET /finops/overview (internal/finops/overview.go).
export interface ClusterOverview {
  cluster: string;
  journey?: string;
  environment?: string;
  generated_at: string;
  compute_table_brl: number;
  compute_brl: number;
  storage_brl: number;
  total_brl: number;
  covered_pools: number;
  waste_brl: number;
  waste_committed_brl: number;
  orphan_storage_brl: number;
  idle_brl: number;
  workloads: number;
  nodes: number;
  pools: number;
  oom_risk: number;
  no_request: number;
  hpa_removable: number;
  superprovisioned: number;
  exempt: number;
  metrics_coverage_pct: number;
}

// Recomendação enviada para aplicar + estado no relatório mais recente — GET /finops/actions.
export interface FinOpsActionRow {
  id: number;
  cluster: string;
  namespace: string;
  workload: string;
  channel: "staging" | "values" | "kubectl";
  created_by?: string;
  created_at: string;
  baseline_report_at: string;
  baseline_pods: number;
  baseline_hpa_min: number;
  baseline_hpa_max: number;
  baseline_cpu_millis: number;
  baseline_mem_mi: number;
  baseline_cost_brl: number;
  baseline_waste_brl: number;
  expected_saving_brl: number;
  target_hpa_min?: number;
  target_hpa_max?: number;
  target_cpu_millis?: number;
  target_mem_mi?: number;
  evaluation: {
    status: "aguardando_scan" | "nao_encontrado" | "pendente" | "parcial" | "aplicada";
    report_at?: string;
    current_pods?: number;
    current_hpa_min?: number;
    current_hpa_max?: number;
    current_cpu_millis?: number;
    current_mem_mi?: number;
    current_cost_brl?: number;
    current_waste_brl?: number;
    realized_saving_brl: number;
    applied: number;
    targets: number;
  };
}

// Showback — GET /finops/showback (internal/finops/showback.go).
export interface ShowbackRow {
  cluster: string;
  namespace: string; // "(não alocado)" = capacidade ociosa + disco do SO
  owner?: string;
  owner_label?: string;
  workloads: number;
  compute_brl: number;
  storage_brl: number;
  waste_brl: number;
  total_brl: number;
}

// Reserva para a carga base — GET /finops/base-reservations (internal/finops/base_reservation.go).
export interface CommitmentOption {
  kind: "reservation" | "savings_plan";
  term: "1y" | "3y";
  monthly_node_brl: number;
  saving_month_brl: number;
  saving_pct: number;
  commitment_brl: number;
}

export interface BaseReservationSuggestion {
  pool: string;
  vm_size: string;
  nodes: number;
  autoscaling: boolean;
  min_nodes?: number;
  base_nodes: number;
  base_source: "hpa_min" | "autoscaler_min" | "fixed";
  nodes_at_hpa_min: number;
  covered_nodes: number;
  suggested_nodes: number;
  on_demand_node_brl: number;
  options?: CommitmentOption[];
  skip?: string;
}
