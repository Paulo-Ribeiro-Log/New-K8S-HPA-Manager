// Health Checking Types - Mirror do backend Go

// Tipos de recursos suportados
export type ResourceType = "Deployment" | "Service" | "ConfigMap" | "Secret" | "HPA" | "PVC";

// Tipos de serviços externos
export type ServiceType =
  | "MongoDB"
  | "Redis"
  | "PostgreSQL"
  | "Kafka"
  | "EventHub"
  | "RabbitMQ"
  | "HTTP";

// Status de health
export type HealthStatus = "healthy" | "warning" | "critical" | "unknown";

// Níveis de severidade (5 níveis)
export type Severity = "critical" | "high" | "medium" | "low" | "info";

// Severidade de eventos K8s (deprecated, usar Severity)
export type EventSeverity = Severity;

// Peso numérico da severidade para ordenação (maior = mais grave)
export const SeverityWeight: Record<Severity, number> = {
  critical: 5,
  high: 4,
  medium: 3,
  low: 2,
  info: 1,
};

// Cores para cada nível de severidade
export const SeverityColors: Record<Severity, string> = {
  critical: "text-red-500",
  high: "text-orange-500",
  medium: "text-yellow-500",
  low: "text-blue-500",
  info: "text-gray-500",
};

// Background colors para cada nível de severidade
export const SeverityBgColors: Record<Severity, string> = {
  critical: "bg-red-500/10 border-red-500/30",
  high: "bg-orange-500/10 border-orange-500/30",
  medium: "bg-yellow-500/10 border-yellow-500/30",
  low: "bg-blue-500/10 border-blue-500/30",
  info: "bg-gray-500/10 border-gray-500/30",
};

// Labels para exibição
export const SeverityLabels: Record<Severity, string> = {
  critical: "Crítico",
  high: "Alto",
  medium: "Médio",
  low: "Baixo",
  info: "Info",
};

// Contadores por severidade
export interface SeverityCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  info: number;
}

// Request de health check
export interface HealthCheckRequest {
  // Modo 1: Filtro por ambiente (prioritário)
  environment?: string; // "prod" | "hlg" | "all"

  // Modo 2: Seleção manual de clusters
  clusters?: string[];

  // Namespaces a verificar (vazio = todos)
  namespaces?: string[];

  // Opções de verificação
  check_deployments: boolean;
  check_services: boolean;
  check_configs: boolean;
  check_events: boolean; // Verificar eventos K8s (FailedScheduling, etc.)
  check_hpas: boolean;       // Verificar HPAs (min=max, métricas, scaling)
  check_pvcs: boolean;       // Verificar PVCs (status, StorageClass, access modes)
  check_dynatrace?: boolean;        // Verificar problems OPEN no Dynatrace
  check_oneagent_signals?: boolean; // Escanear métricas OneAgent (sem problem ativo)

  // Email do analista (para buscar credenciais Dynatrace)
  ai_email?: string;

  // Timeout geral (segundos) - usado como fallback
  timeout: number;

  // Timeouts específicos por tipo de check (opcional)
  timeout_deployments?: number; // Padrão: 60s
  timeout_services?: number;    // Padrão: 45s
  timeout_configs?: number;     // Padrão: 30s
  timeout_events?: number;      // Padrão: 30s
  timeout_hpas?: number;        // Padrão: 45s
  timeout_pvcs?: number;        // Padrão: 30s

  // Paralelismo máximo (opcional)
  max_parallel?: number;

  // Aplicar filtros de falsos positivos (opcional, padrão: true)
  apply_filters?: boolean;
}

// Resultado de health check (por cluster)
export interface HealthCheckResult {
  id: string;          // UUID da sessão
  cluster: string;
  namespace: string;
  started_at: string;  // ISO timestamp
  finished_at: string; // ISO timestamp
  duration_ms: number; // Duração em milissegundos

  // Resultados detalhados
  deployment_results: DeploymentHealth[];
  service_results: ServiceHealth[];
  config_results: ConfigHealth[];
  event_results: EventHealth[];          // Eventos K8s críticos
  hpa_results: HPAHealth[];              // HPAs com problemas de configuração
  pvc_results: PVCHealth[];              // PVCs com problemas de configuração
  dynatrace_results?: DynatraceHealth[]; // Problems Dynatrace correlacionados
  correlated_items?: CorrelatedHealthItem[]; // Correlação K8s ↔ Dynatrace
  oneagent_signals?: OneAgentSignal[];   // Sinais de risco detectados via OneAgent (sem problem ativo)

  // Resumo
  total_checks: number;
  healthy_count: number;
  warning_count: number;   // Deprecated: usar severity_counts
  critical_count: number;  // Deprecated: usar severity_counts
  overall_status: HealthStatus;

  // Contadores por severidade (novo sistema)
  severity_counts?: SeverityCounts;
}

// Probe Issue (problema na configuração de probes)
export interface ProbeIssue {
  container: string;
  probe_type: string;  // "liveness", "readiness", "startup"
  issue: string;
  severity: Severity;
}

// Resource Issue (problema na configuração de recursos)
export interface ResourceIssue {
  container: string;
  resource_type: string;  // "cpu", "memory"
  issue: string;
  severity: Severity;
}

// Container Resources (recursos configurados de um container)
export interface ContainerResources {
  name: string;
  cpu_request?: string;
  memory_request?: string;
  cpu_limit?: string;
  memory_limit?: string;
  has_cpu_request: boolean;
  has_memory_request: boolean;
  has_cpu_limit: boolean;
  has_memory_limit: boolean;
}

// QoS Class
export type QoSClass = "Guaranteed" | "Burstable" | "BestEffort";

// Health de Deployment
export interface DeploymentHealth {
  name: string;
  namespace: string;
  status: HealthStatus;

  // Detalhes de réplicas
  replicas_ready: number;
  replicas_desired: number;
  containers_crash: number;
  image_pull_errors: number;

  // Recursos - Uso atual
  cpu_usage_percent: number;    // 0-100
  memory_usage_percent: number; // 0-100

  // Recursos - Configuração
  qos_class?: QoSClass;
  container_resources?: ContainerResources[];
  resource_issues?: ResourceIssue[];

  // Probes
  has_liveness_probe: boolean;
  has_readiness_probe: boolean;
  has_startup_probe?: boolean;
  liveness_probe_failures?: number;
  readiness_probe_failures?: number;
  liveness_failing: boolean;
  readiness_failing: boolean;
  probe_issues?: ProbeIssue[];

  // Mensagem e sugestões
  message: string;
  suggestions: string[];
  checked_at: string; // ISO timestamp
}

// Health de Serviço Externo
export interface ServiceHealth {
  name: string;
  namespace: string;
  service_type: ServiceType;
  status: HealthStatus;

  // Conectividade
  reachable: boolean;
  latency_ms: number;
  connection_error?: string;

  // Detalhes específicos do serviço (opcional)
  details?: Record<string, any>;

  // Fonte da configuração
  config_source: string; // "configmap:my-config/key" ou "secret:my-secret/key"

  // Mensagem e sugestões
  message: string;
  suggestions: string[];
  checked_at: string; // ISO timestamp
}

// Health de ConfigMap/Secret
export interface ConfigHealth {
  name: string;
  namespace: string;
  resource_type: ResourceType; // "ConfigMap" ou "Secret"
  status: HealthStatus;

  // Validações
  exists: boolean;
  has_required_keys: boolean;
  missing_keys?: string[];
  invalid_values?: string[];

  // Mensagem e sugestões
  message: string;
  suggestions: string[];
  checked_at: string; // ISO timestamp
}

// Health de Evento K8s (FailedScheduling, CrashLoopBackOff, etc.)
export interface EventHealth {
  name: string;
  namespace: string;
  reason: string;           // FailedScheduling, BackOff, etc.
  message: string;          // Mensagem completa do evento
  type: string;             // Warning, Normal
  severity: Severity;       // critical, high, medium, low, info
  count: number;            // Número de ocorrências
  first_timestamp: string;  // ISO timestamp - Primeira ocorrência
  last_timestamp: string;   // ISO timestamp - Última ocorrência

  // Recurso relacionado
  involved_kind: string;    // Pod, Node, Deployment, etc.
  involved_name: string;    // Nome do recurso

  // Sugestões de correção
  suggestions: string[];
  status: HealthStatus;     // Para compatibilidade com outros checkers
  checked_at: string;       // ISO timestamp
}

// HPA Scaling Issue
export interface HPAScalingIssue {
  type: string;        // "config", "metric", "scaling", "target"
  description: string;
  severity: Severity;  // critical, high, medium, low, info
}

// HPA Metric Config
export interface HPAMetricConfig {
  type: string;              // "Resource", "Pods", "Object", "External"
  name: string;              // "cpu", "memory", "custom-metric"
  target_type: string;       // "Utilization", "Value", "AverageValue"
  target_value: string;      // "80%", "100m", "1000"
  current_value?: string;    // Valor atual se disponível
  is_healthy: boolean;
  error_message?: string;
}

// HPA Scaling Event
export interface HPAScalingEvent {
  timestamp: string;    // ISO timestamp
  type: string;         // "ScaledUp", "ScaledDown", "FailedScaling"
  old_replicas: number;
  new_replicas: number;
  reason: string;
  message: string;
}

// Health de HPA (HorizontalPodAutoscaler)
export interface HPAHealth {
  name: string;
  namespace: string;
  status: HealthStatus;

  // Target Reference
  target_kind: string;    // "Deployment", "StatefulSet", etc
  target_name: string;
  target_exists: boolean;

  // Configuração de Réplicas
  min_replicas: number;
  max_replicas: number;
  current_replicas: number;
  desired_replicas: number;

  // Flags de Problemas
  is_min_equals_max: boolean;      // min == max (não escala)
  is_max_too_low: boolean;         // max < 3 (pouca flexibilidade)
  is_at_max_replicas: boolean;     // current == max (pode precisar escalar mais)
  is_at_min_replicas: boolean;     // current == min
  has_scaling_disabled: boolean;   // Annotations que desabilitam scaling

  // Métricas Configuradas
  metrics: HPAMetricConfig[];
  metrics_count: number;
  metrics_errors: number;

  // Comportamento de Scaling
  scale_up_stabilization_seconds?: number;
  scale_down_stabilization_seconds?: number;

  // Eventos Recentes de Scaling
  recent_scaling_events?: HPAScalingEvent[];
  last_scale_time?: string; // ISO timestamp

  // Problemas Detectados
  issues: HPAScalingIssue[];

  // Mensagem e Sugestões
  message: string;
  suggestions: string[];
  checked_at: string; // ISO timestamp
}

// PV Issue (problema detectado em PVC)
export interface PVIssue {
  type: string;        // "volume", "status", "config"
  description: string;
  severity: Severity;  // critical, high, medium, low, info
}

// Health de PVC (PersistentVolumeClaim)
export interface PVCHealth {
  name: string;
  namespace: string;
  status: HealthStatus;

  // Status do PVC
  phase: string;              // "Pending", "Bound", "Lost"
  is_bound: boolean;
  is_pending: boolean;

  // Volume associado
  volume_name: string;        // Nome do PV vinculado
  volume_exists: boolean;

  // Storage
  storage_class_name: string;
  storage_class_exists: boolean;
  requested_storage: string;  // "10Gi"
  actual_storage: string;     // Capacidade real do PV
  requested_bytes: number;
  actual_bytes: number;
  usage_percent: number;      // 0-100 (se disponível)

  // Access Modes
  access_modes: string[];     // ["ReadWriteOnce", "ReadWriteMany", etc]

  // Reclaim Policy
  reclaim_policy: string;     // "Delete", "Retain", "Recycle"
  volume_mode: string;        // "Filesystem", "Block"

  // Problemas Detectados
  issues: PVIssue[];

  // Mensagem e Sugestões
  message: string;
  suggestions: string[];
  checked_at: string; // ISO timestamp
}

// Progress de health check (SSE)
export interface HealthCheckProgress {
  id: string;        // Session ID (pode vir como 'ID' do backend Go)
  ID?: string;       // Alias para compatibilidade com backend Go (capitalizado)
  type: string;      // "init" | "deployments" | "services" | "configs" | "events" | "summary" | "complete" | "error"
  phase: string;     // Mantido por compatibilidade (usado em cordon/drain, não em health checking)
  message: string;
  progress: number;  // 0-100
  status: HealthStatus;
  timestamp: string; // ISO timestamp
  details?: string;  // Campo adicional para multiplexing SSE (formato: "cluster:nome") - lowercase da tag JSON do backend
  Details?: string;  // Fallback para compatibilidade (se backend mudar para uppercase)
}

// Response da API
export interface HealthCheckRunResponse {
  success: boolean;
  session_id: string;             // Base session ID
  cluster_sessions: Record<string, string>; // Map: cluster -> sessionID único
  message: string;
}

export interface HealthCheckHistoryResponse {
  success: boolean;
  data: HealthCheckResult[];
  count: number;
}

export interface HealthCheckStatsResponse {
  success: boolean;
  data: {
    total_runs: number;
    total_checks: number;
    total_healthy: number;
    total_warnings: number;
    total_critical: number;
    avg_duration_ms: number;
    healthy_runs: number;
    warning_runs: number;
    critical_runs: number;
    days: number;
    since: string; // ISO timestamp
  };
}

export interface HealthCheckGetResponse {
  success: boolean;
  data: HealthCheckResult;
}

export interface HealthCheckDeleteResponse {
  success: boolean;
  message: string;
}

// Lista de eventos persistidos (backend SQLite)
export interface HealthCheckEventsResponse {
  success: boolean;
  events: HealthCheckProgress[];
  count: number;
}

// Erro genérico da API
export interface HealthCheckErrorResponse {
  success: false;
  error: {
    message: string;
    details?: string;
  };
}

// Tags ricas do OneAgent — DevOps, versão, squad, GitHub repo
export interface DTLabels {
  // app.kubernetes.io/*
  appName?: string;
  appVersion?: string;
  appEnvironment?: string; // prd | hlg | dev
  appInstance?: string;
  releaseName?: string;
  stage?: string;          // stable | canary
  deployedBy?: string;     // spinnaker-helm
  isCanary?: string;       // "true" | "false"
  // K8s infra
  namespace?: string;
  hostGroup?: string;      // dt.host_group.id → AKS cluster (ex: akspriv-busca-prd)
  // DevOps / rastreabilidade
  githubRepoId?: string;
  componentName?: string;
  componentSquad?: string;
  componentJourney?: string;
  componentTribe?: string;
  helmChart?: string;
}

// Problem Dynatrace retornado pela API /api/v1/dynatrace/problems (ProblemSummary — camelCase)
export interface DynatraceEntityStub {
  entityId: { id: string; type: string };
  displayName?: string; // da Entity API
  name?: string;        // da Problems API (campo "name" nos stubs)
  k8sCluster?: string;
  k8sNamespace?: string;
  k8sWorkload?: string;
  labels?: DTLabels;    // tags ricas do OneAgent (squad, journey, versão, GitHub, etc.)
  callsTo?: { id: string; type: string }[];   // entidades downstream (esta chama)
  calledBy?: { id: string; type: string }[];  // entidades upstream (chamam esta)
}

export interface DynatraceProblem {
  problemId: string;
  displayId: string;
  title: string;
  status: string;           // "OPEN" | "CLOSED"
  severityLevel: string;    // "AVAILABILITY" | "ERROR" | "PERFORMANCE" | "RESOURCE_CONTENTION" | "CUSTOM_ALERT"
  impactLevel: string;      // "APPLICATION" | "ENVIRONMENT" | "INFRASTRUCTURE" | "SERVICE"
  startTime: string;        // ISO timestamp
  endTime?: string;
  affectedEntities: DynatraceEntityStub[];
  impactedEntities?: DynatraceEntityStub[];
  rootCauseEntity?: DynatraceEntityStub;
  managementZones?: { id: string; name: string }[];
  k8sWorkloads?: {
    Cluster: string;
    Namespace: string;
    Workload: string;
    PodName?: string;
    AppName?: string;
    AppVersion?: string;
    GitHubRepoID?: string;
    Environment?: string;
  }[];
}

// ─── Métricas de performance por entidade ─────────────────────────────────────

export interface DTMetricPoint {
  t: number; // epoch ms
  v: number;
}

export interface DTMetricSeries {
  key: string;
  label: string;
  unit: string;
  points: DTMetricPoint[];
}

export interface DTEntityMetrics {
  entityId: string;
  entityName: string;
  entityType: string;
  isRootCause: boolean;
  metrics: DTMetricSeries[];
}

export interface DTProblemMetrics {
  problemId: string;
  title: string;
  timeFrom: string;
  timeTo: string;
  problemStart: string;
  entities: DTEntityMetrics[];
}

// ─── Contexto rico de um problem (evidências, topologia, traces, eventos) ──────

export interface DTEvidenceItem {
  evidenceType: string;   // "EVENT" | "METRIC" | "TRANSACTIONAL" | "LOG" | "MAINTENANCE_WINDOW"
  displayName: string;
  entityId: string;
  entityName: string;
  entityType: string;
  rootCause: boolean;
  startTime: string; // ISO
}

export interface DTTopoRelation {
  entityId: string;
  entityName: string;
  entityType: string;
  direction: string; // "outgoing" | "incoming"
}

export interface DTTraceEntry {
  traceId: string;
  startTime: string; // ISO
  durationMs: number;
  name: string;
  httpMethod?: string;
  httpStatus?: number;
  hasError: boolean;
}

export interface DTContextEventEntry {
  eventId: string;
  eventType: string;
  title: string;
  startTime: string; // ISO
  entityId: string;
  entityName: string;
}

export interface DTProblemContext {
  problemId: string;
  timeFrom: string;
  timeTo: string;
  problemStart: string;
  evidence: DTEvidenceItem[];
  events: DTContextEventEntry[];
  topology: DTTopoRelation[];
  traces: DTTraceEntry[];
  tracesError?: string; // erro ao buscar traces (403 = falta escopo DataExport no token)
}

export interface DynatraceHistoryRecord {
  id: string;
  resource_name: string;   // "P-XXXXX — título do problem"
  namespace: string;       // management zones (csv)
  analysis: string;
  analyzed_at: string;
  user_email: string;
  resource_metadata?: string; // JSON: { display_id, severity, impact_level, management_zones, start_time }
}

// Problem Dynatrace correlacionado com recursos K8s do cluster
export interface DynatraceHealth {
  problem_id: string;
  display_id: string;
  title: string;
  dt_severity: string;      // "AVAILABILITY" | "ERROR" | "PERFORMANCE" | "RESOURCE_CONTENTION" | "CUSTOM_ALERT"
  impact_level: string;     // "APPLICATION" | "ENVIRONMENT" | "INFRASTRUCTURE" | "SERVICE"
  status: HealthStatus;
  severity: Severity;
  start_time: string;       // ISO timestamp
  k8s_namespaces?: string[];
  k8s_workloads?: string[]; // "namespace/workload"
  affected_entities?: string[];
  message: string;
  suggestions: string[];
  checked_at: string;       // ISO timestamp
  // Campos enriquecidos via DTLabels (OneAgent tags)
  app_versions?: Record<string, string>; // appName → version
  github_repos?: string[];               // IDs dos repos afetados
  squads?: string[];                     // times donos das apps
  journeys?: string[];                   // jornadas/domínios
  environments?: string[];               // prd/hlg/dev
  // Campos enriquecidos via Davis AI context (Top 5 problems por severidade)
  evidence?: string[];                   // Evidências Davis AI, prefixadas com "[Root Cause]" quando root cause
  recent_events?: string[];              // Top 3 eventos recentes: "TIPO: título"
  metrics_summary?: Record<string, number>; // ex: { error_rate: 12.5, response_p90_ms: 2300 }
  context_fetched?: boolean;             // true se GetProblemContext foi chamado com sucesso
}

// Issue K8s de um workload específico para correlação bidirecional K8s ↔ Dynatrace
export interface CorrelatedK8sIssue {
  resource_kind: string;   // "Deployment" | "HPA" | "Event"
  resource_name: string;
  status: HealthStatus;
  message: string;
  severity: Severity;
  suggestions?: string[];
}

// Item correlacionado: une sintomas K8s com problems Dynatrace do mesmo workload
export interface CorrelatedHealthItem {
  workload_name: string;
  namespace: string;
  cluster: string;

  // K8s side (pode existir sem match DT)
  k8s_issues?: CorrelatedK8sIssue[];
  k8s_severity: Severity;

  // DT side (pode existir sem match K8s)
  dt_problems?: DynatraceHealth[];
  dt_severity: Severity;

  // Combined
  final_severity: Severity; // pior dos dois; escalada para critical se ambos >= high
  correlated: boolean;      // true = match encontrado dos dois lados

  // AI Analysis (preenchida sob demanda via POST /healthcheck/correlated/analyze)
  ai_analysis?: string;
  ai_analyzed_at?: string; // ISO timestamp
}

// Node Pool summary para correlação dentro do OneAgentSignal
export interface NodePoolSummary {
  node_pool: string;
  vm_size: string;
  mode: string;
  node_count: number;
}

// Sinal de risco detectado via OneAgent — sem precisar de um Problem DT ativo
export interface OneAgentSignal {
  entity_id?: string;
  entity_type?: string;
  cluster: string;
  namespace: string;
  workload_name: string;
  app_version?: string;
  squad?: string;

  // Métricas (máximo da última hora; podem ser zero se não encontrado no DT)
  error_rate?: number;       // %
  response_p90_ms?: number;  // ms
  pod_restarts?: number;     // count/hora
  cpu_throttle_pct?: number; // %
  pods_ready_pct?: number;   // % (0-100)

  // Avaliação de risco
  risk_level: Severity;
  risk_reasons: string[];

  // Flags
  has_dt_problem: boolean;   // true = workload já coberto por Problem DT ativo
  from_k8s_issue?: boolean;  // true = Fase 1 (detectado via K8s sem match DT)
  k8s_issues?: string[];     // mensagens dos problemas K8s (Fase 1)

  // Correlação
  cluster_pools?: NodePoolSummary[];
  depended_by?: string[];   // serviços que dependem deste workload
  depends_on?: string[];    // dependências deste workload

  // AI Analysis (preenchida sob demanda)
  ai_analysis?: string;
  checked_at: string;       // ISO timestamp
}
