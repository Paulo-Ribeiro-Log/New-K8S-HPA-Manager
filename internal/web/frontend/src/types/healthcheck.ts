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

  // Timeout por check (segundos)
  timeout: number;

  // Paralelismo máximo (opcional)
  max_parallel?: number;
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

// Progress de health check (SSE)
export interface HealthCheckProgress {
  session_id: string;
  phase: string;     // "deployments" | "services" | "configs" | "complete" | "error"
  message: string;
  progress: number;  // 0-100
  status: HealthStatus;
  timestamp: string; // ISO timestamp
}

// Response da API
export interface HealthCheckRunResponse {
  success: boolean;
  session_id: string;
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

// Erro genérico da API
export interface HealthCheckErrorResponse {
  success: false;
  error: {
    message: string;
    details?: string;
  };
}
