// Health Checking Types - Mirror do backend Go

// Tipos de recursos suportados
export type ResourceType = "Deployment" | "Service" | "ConfigMap" | "Secret";

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

// Severidade de eventos K8s
export type EventSeverity = "critical" | "warning" | "info";

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

  // Timeout geral (segundos) - usado como fallback
  timeout: number;

  // Timeouts específicos por tipo de check (opcional)
  timeout_deployments?: number; // Padrão: 60s
  timeout_services?: number;    // Padrão: 45s
  timeout_configs?: number;     // Padrão: 30s
  timeout_events?: number;      // Padrão: 30s

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
  event_results: EventHealth[]; // Eventos K8s críticos

  // Resumo
  total_checks: number;
  healthy_count: number;
  warning_count: number;
  critical_count: number;
  overall_status: HealthStatus;
}

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

  // Recursos
  cpu_usage_percent: number;    // 0-100
  memory_usage_percent: number; // 0-100

  // Probes
  has_liveness_probe: boolean;
  has_readiness_probe: boolean;
  liveness_failing: boolean;
  readiness_failing: boolean;

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
  severity: EventSeverity;  // critical, warning, info
  count: number;            // Número de ocorrências
  first_timestamp: string;  // ISO timestamp - Primeira ocorrência
  last_timestamp: string;   // ISO timestamp - Última ocorrência

  // Recurso relacionado
  involved_kind: string;    // Pod, Node, Deployment, etc.
  involved_name: string;    // Nome do recurso

  // Sugestões de correção
  suggestions: string[];
  status: HealthStatus;     // Para compatibilidade com outros checkers
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
