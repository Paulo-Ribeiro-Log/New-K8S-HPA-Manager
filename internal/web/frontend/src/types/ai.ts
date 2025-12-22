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
  provider: "gemini" | "ollama";
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
  provider: "gemini" | "ollama";
  available: boolean;
  model: string;
  error?: string;
}

export interface AIStats {
  totalAnalyses: number;
  analysesByResource: Record<ResourceType, number>;
  analysesByProvider: Record<string, number>;
  avgResponseTime: number;
  totalTokensUsed: number;
}

export interface HistoryFilter {
  cluster?: string;
  namespace?: string;
  resourceType?: ResourceType;
  provider?: "gemini" | "ollama";
  limit?: number;
  offset?: number;
}
