import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import type { AnalysisResult } from "@/types/ai";
import {
  Brain,
  Clock,
  Cpu,
  AlertCircle,
  CheckCircle2,
  ArrowLeft,
  ExternalLink,
} from "lucide-react";
import { AIAnalysisView } from "@/components/AIAnalysisView";

export function AIAnalysisPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    console.log("[AIAnalysisPage] Loading analysis with ID:", id);

    // Tentar obter análise do sessionStorage primeiro
    const cachedAnalysis = sessionStorage.getItem(`ai-analysis-${id}`);
    console.log("[AIAnalysisPage] SessionStorage cache:", cachedAnalysis ? "FOUND" : "NOT FOUND");

    if (cachedAnalysis) {
      try {
        const parsed = JSON.parse(cachedAnalysis);
        console.log("[AIAnalysisPage] Parsed analysis from cache:", parsed);
        console.log("[AIAnalysisPage] analysis.analysis field:", parsed.analysis);
        console.log("[AIAnalysisPage] analysis.analysis length:", parsed.analysis?.length || 0);
        console.log("[AIAnalysisPage] executive_summary exists?", !!parsed.executive_summary);
        setAnalysis(parsed);
        setLoading(false);
        return;
      } catch (err) {
        console.error("[AIAnalysisPage] Failed to parse cached analysis:", err);
      }
    }

    // Se não estiver no cache, buscar do backend
    const fetchAnalysis = async () => {
      try {
        console.log("[AIAnalysisPage] Fetching from backend:", `/api/v1/ai/history/${id}`);
        const response = await fetch(`/api/v1/ai/history/${id}`, {
          headers: {
            Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
          },
        });

        console.log("[AIAnalysisPage] Backend response status:", response.status);

        if (!response.ok) {
          const errorText = await response.text();
          console.error("[AIAnalysisPage] Backend error:", errorText);
          throw new Error(`Falha ao carregar análise (${response.status})`);
        }

        const data = await response.json();
        console.log("[AIAnalysisPage] Analysis loaded from backend:", data);
        setAnalysis(data);
      } catch (err) {
        console.error("[AIAnalysisPage] Fetch error:", err);
        setError(err instanceof Error ? err.message : "Erro desconhecido");
      } finally {
        setLoading(false);
      }
    };

    fetchAnalysis();
  }, [id]);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-center space-y-4">
          <Brain className="h-12 w-12 text-purple-600 animate-pulse mx-auto" />
          <p className="text-muted-foreground">Carregando análise...</p>
        </div>
      </div>
    );
  }

  if (error || !analysis) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-center space-y-4">
          <AlertCircle className="h-12 w-12 text-red-600 mx-auto" />
          <p className="text-lg font-semibold">Erro ao carregar análise</p>
          <p className="text-muted-foreground">{error}</p>
          <Button onClick={() => navigate(-1)} variant="outline">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Voltar
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header Fixo */}
      <div className="sticky top-0 z-10 border-b bg-gradient-to-r from-purple-50 to-blue-50 dark:from-purple-950/20 dark:to-blue-950/20 backdrop-blur-sm">
        <div className="container max-w-7xl mx-auto px-4 sm:px-6 md:px-8 py-4 sm:py-6">
          <div className="flex items-start justify-between gap-4 mb-4">
            <div className="space-y-2 flex-1 min-w-0">
              <div className="flex items-center gap-3">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => navigate(-1)}
                  className="flex-shrink-0"
                >
                  <ArrowLeft className="h-4 w-4 mr-2" />
                  Voltar
                </Button>
              </div>
              <h1 className="flex items-center gap-3 text-2xl sm:text-3xl font-bold">
                <div className="p-2 bg-purple-100 dark:bg-purple-900/30 rounded-lg flex-shrink-0">
                  <Brain className="h-6 w-6 sm:h-8 sm:w-8 text-purple-600 dark:text-purple-400" />
                </div>
                <span className="truncate">Análise AI - {analysis.resource_type}</span>
              </h1>
              <div className="flex items-center gap-2 flex-wrap text-sm">
                <Badge variant="outline" className="font-mono text-xs px-3 py-1">
                  <span className="truncate max-w-[300px] sm:max-w-[500px]">
                    {analysis.cluster}/{analysis.namespace}/{analysis.resource_name}
                  </span>
                </Badge>
                <Badge variant="secondary" className="text-xs px-3 py-1">
                  {analysis.provider} • {analysis.model || "default"}
                </Badge>
              </div>
            </div>
          </div>

          {/* Metadados */}
          <div className="flex items-center gap-4 md:gap-6 flex-wrap text-xs text-muted-foreground pt-3 border-t border-purple-200/50 dark:border-purple-800/50">
            {analysis.response_time && (
              <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
                <Clock className="h-4 w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.response_time.toFixed(1)}s</span>
              </div>
            )}
            {analysis.tokens_used && (
              <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
                <Cpu className="h-4 w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.tokens_used} tokens</span>
              </div>
            )}
            <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
              <CheckCircle2 className="h-4 w-4 flex-shrink-0" />
              <span className="font-medium">{new Date(analysis.analyzed_at).toLocaleString("pt-BR")}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Conteúdo */}
      <ScrollArea className="h-[calc(100vh-200px)]">
        <div className="container max-w-7xl mx-auto px-4 sm:px-6 md:px-8 py-6 space-y-6">
          {/* Usar componente reutilizável AIAnalysisView */}
          <AIAnalysisView analysis={analysis} showExportButtons={true} />

          {/* Ações */}
          <div className="flex justify-center gap-4 pb-8">
            <Button variant="outline" onClick={() => navigate("/ai-diagnostics")} className="gap-2">
              <ExternalLink className="h-4 w-4" />
              Ver Histórico Completo
            </Button>
            <Button variant="default" onClick={() => navigate(-1)} className="gap-2">
              <ArrowLeft className="h-4 w-4" />
              Voltar para Interface
            </Button>
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}
