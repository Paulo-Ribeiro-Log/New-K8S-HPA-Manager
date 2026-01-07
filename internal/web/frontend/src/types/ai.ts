// AI Diagnostics Types

export type ResourceType = "Pod" | "Deployment" | "HPA" | "Node";

export type SuggestionType = "investigate" | "fix" | "optimize" | "scale";

export type Priority = "critical" | "high" | "medium" | "low";

export interface Suggestion {
  type: SuggestionType;
  description: string;
  command?: string; // kubectl command (se aplicável)
  priority: Priority;
}

export type AIProvider = "gemini" | "ollama" | "claude" | "openai" | "copilot";

// Estruturas novas - Análise Estruturada (FASE 3)

export interface ExecutiveSummary {
  severity: "critical" | "high" | "medium" | "low";
  status: "unhealthy" | "degraded" | "healthy";
  quick_summary: string;
  time_to_resolve: string;
}

export interface RootCauseAnalysis {
  symptom: string;
  probable_causes: string[];
  evidence: string[];
  confidence: "high" | "medium" | "low";
}

export interface ImpactAssessment {
  severity: string;
  affected_users: string;
  downtime_estimate: string;
  sla_breach: boolean;
  business_impact: string;
}

export interface ActionableRecommendation {
  priority: number; // 1-5 (1 = mais crítico)
  title: string;
  description: string;
  commands: string[];
  time_estimate: string;
  risk_level: string;
  impact_level: string;
}

export interface ResourceMetrics {
  cpu_usage: number;
  cpu_request: number;
  cpu_limit: number;
  memory_usage: number;
  memory_request: number;
  memory_limit: number;
  restart_count: number;
  ready_replicas: number;
  desired_replicas: number;
}

export interface ResourceDetail {
  current: string;
  request: string;
  limit: string;
}

export interface ResourceSummary {
  cpu: ResourceDetail;
  memory: ResourceDetail;
}

export interface ReplicaInfo {
  current: number;
  ready: number;
  desired: number;
}

export interface ResourceMetadata {
  name: string;
  namespace: string;
  cluster: string;
  type: string;
  version?: string;
  status: string;
  phase: string;
  age: string;
  restart_count: number;
  node_name?: string;
  container_images?: string[];
  labels?: Record<string, string>;
  resources: ResourceSummary;
  replicas?: ReplicaInfo;
  collected_at: string;
}

export interface AnalysisResult {
  id: string;
  resource_type: ResourceType;
  cluster: string;
  namespace: string;
  resource_name: string;

  // Estrutura nova (4 seções) - FASE 3
  executive_summary?: ExecutiveSummary;
  root_cause_analysis?: RootCauseAnalysis;
  impact_assessment?: ImpactAssessment;
  recommendations?: ActionableRecommendation[];

  // Campos legados (backward compatibility)
  analysis: string; // Markdown formatado
  suggestions: Suggestion[];

  // Métricas (FASE 2)
  current_metrics?: ResourceMetrics;

  // Metadados do Recurso (FASE 3.5)
  resource_metadata?: ResourceMetadata;

  // Metadados
  provider: AIProvider;
  model?: string;
  analyzed_at: string; // ISO timestamp
  tokens_used?: number;
  response_time?: number; // segundos
  user_email?: string;
  error?: string;
}

export interface AnalyzeRequest {
  resourceType: ResourceType;
  cluster: string;
  namespace: string;
  resourceName: string;
  includeLogs?: boolean;
  includeMetrics?: boolean;
  includeDescribe?: boolean;
}

export interface ProviderStatus {
  provider: AIProvider;
  available: boolean;
  model: string;
  error?: string;
  lastCheck?: string; // ISO timestamp
  total_calls: number;
  successful_calls: number;
  failed_calls: number;
}

export interface AIStats {
  total_analyses: number;
  by_resource_type: Record<ResourceType, number>;
  by_provider: Record<string, number>;
  avg_response_time: number;
  total_tokens_used: number;
  last_analysis_at?: string;
}

export interface HistoryFilter {
  cluster?: string;
  namespace?: string;
  resourceType?: ResourceType;
  provider?: AIProvider;
  limit?: number;
  offset?: number;
}
