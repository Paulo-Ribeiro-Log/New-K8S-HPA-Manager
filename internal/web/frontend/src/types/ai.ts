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

export interface AnalysisResult {
  id: string;
  resourceType: ResourceType;
  cluster: string;
  namespace: string;
  resourceName: string;

  // Análise da IA
  analysis: string; // Markdown formatado
  suggestions: Suggestion[];

  // Metadados
  provider: AIProvider;
  model?: string;
  analyzedAt: string; // ISO timestamp
  tokensUsed?: number;
  responseTime?: number; // segundos
  userEmail?: string;
}

export interface AnalyzeRequest {
  resourceType: ResourceType;
  cluster: string;
  namespace: string;
  resourceName: string;
  includeDescribe?: boolean;
  includeMetrics?: boolean;
}

export interface ProviderStatus {
  provider: AIProvider;
  available: boolean;
  model: string;
  error?: string;
  lastCheck?: string; // ISO timestamp
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
