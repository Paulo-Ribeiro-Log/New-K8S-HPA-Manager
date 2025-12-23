import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { AnalysisResult } from "@/types/ai";
import ReactMarkdown from "react-markdown";
import {
  Brain,
  Clock,
  Cpu,
  Sparkles,
  AlertCircle,
  CheckCircle2,
  TrendingUp,
  Wrench,
  ArrowLeft,
  ExternalLink,
} from "lucide-react";

const suggestionIcons = {
  investigate: AlertCircle,
  fix: Wrench,
  optimize: TrendingUp,
  scale: Sparkles,
};

const priorityColors = {
  critical: "border-red-500 bg-red-50 dark:bg-red-950/20",
  high: "border-orange-500 bg-orange-50 dark:bg-orange-950/20",
  medium: "border-yellow-500 bg-yellow-50 dark:bg-yellow-950/20",
  low: "border-blue-500 bg-blue-50 dark:bg-blue-950/20",
};

export function AIAnalysisPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Tentar obter análise do sessionStorage primeiro
    const cachedAnalysis = sessionStorage.getItem(`ai-analysis-${id}`);
    if (cachedAnalysis) {
      setAnalysis(JSON.parse(cachedAnalysis));
      setLoading(false);
      return;
    }

    // Se não estiver no cache, buscar do backend
    const fetchAnalysis = async () => {
      try {
        const response = await fetch(`/api/v1/ai/history/${id}`, {
          headers: {
            Authorization: `Bearer ${localStorage.getItem("token")}`,
          },
        });

        if (!response.ok) {
          throw new Error("Falha ao carregar análise");
        }

        const data = await response.json();
        setAnalysis(data);
      } catch (err) {
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
                <span className="truncate">Análise AI - {analysis.resourceType}</span>
              </h1>
              <div className="flex items-center gap-2 flex-wrap text-sm">
                <Badge variant="outline" className="font-mono text-xs px-3 py-1">
                  <span className="truncate max-w-[300px] sm:max-w-[500px]">
                    {analysis.cluster}/{analysis.namespace}/{analysis.resourceName}
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
            {analysis.responseTime && (
              <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
                <Clock className="h-4 w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.responseTime.toFixed(1)}s</span>
              </div>
            )}
            {analysis.tokensUsed && (
              <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
                <Cpu className="h-4 w-4 flex-shrink-0" />
                <span className="font-medium">{analysis.tokensUsed} tokens</span>
              </div>
            )}
            <div className="flex items-center gap-2 bg-white/50 dark:bg-black/20 px-3 py-1.5 rounded-md">
              <CheckCircle2 className="h-4 w-4 flex-shrink-0" />
              <span className="font-medium">{new Date(analysis.analyzedAt).toLocaleString("pt-BR")}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Conteúdo */}
      <ScrollArea className="h-[calc(100vh-200px)]">
        <div className="container max-w-7xl mx-auto px-4 sm:px-6 md:px-8 py-6 space-y-6">
          {/* Análise Principal */}
          <Card className="border-2 shadow-sm">
            <CardHeader className="bg-gradient-to-r from-purple-50 to-transparent dark:from-purple-950/20 dark:to-transparent border-b">
              <CardTitle className="text-lg flex items-center gap-2 font-semibold">
                <Sparkles className="h-5 w-5 text-purple-600 flex-shrink-0" />
                Análise Detalhada
              </CardTitle>
            </CardHeader>
            <CardContent className="pt-6">
              <div className="prose prose-sm dark:prose-invert max-w-none break-words overflow-wrap-anywhere">
                <ReactMarkdown
                  components={{
                    code: ({ node, className, ...props }) => {
                      const isInline = !className?.includes('language-');
                      return isInline ? (
                        <code className="break-all whitespace-pre-wrap" {...props} />
                      ) : (
                        <code className="block overflow-x-auto whitespace-pre-wrap break-words" {...props} />
                      );
                    },
                    pre: ({ node, ...props }) => (
                      <pre className="overflow-x-auto whitespace-pre-wrap break-words bg-muted p-4 rounded-lg" {...props} />
                    ),
                    p: ({ node, ...props }) => (
                      <p className="break-words whitespace-pre-wrap leading-relaxed my-4" {...props} />
                    ),
                    ul: ({ node, ...props }) => <ul className="space-y-2 my-4" {...props} />,
                    ol: ({ node, ...props }) => <ol className="space-y-2 my-4" {...props} />,
                    li: ({ node, ...props }) => <li className="break-words ml-4" {...props} />,
                    h1: ({ node, ...props }) => (
                      <h1 className="text-2xl font-bold mt-8 mb-4 pb-2 border-b" {...props} />
                    ),
                    h2: ({ node, ...props }) => (
                      <h2 className="text-xl font-bold mt-6 mb-3 pb-2 border-b border-muted" {...props} />
                    ),
                    h3: ({ node, ...props }) => (
                      <h3 className="text-lg font-semibold mt-4 mb-2" {...props} />
                    ),
                  }}
                >
                  {analysis.analysis}
                </ReactMarkdown>
              </div>
            </CardContent>
          </Card>

          {/* Sugestões */}
          {analysis.suggestions && analysis.suggestions.length > 0 && (
            <Card className="border-2 shadow-sm">
              <CardHeader className="bg-gradient-to-r from-orange-50 to-transparent dark:from-orange-950/20 dark:to-transparent border-b">
                <CardTitle className="text-lg flex items-center gap-2 font-semibold">
                  <AlertCircle className="h-5 w-5 text-orange-600 flex-shrink-0" />
                  Sugestões de Ação ({analysis.suggestions.length})
                </CardTitle>
              </CardHeader>
              <CardContent className="pt-6">
                <div className="space-y-4">
                  {analysis.suggestions.map((suggestion, idx) => {
                    const Icon =
                      suggestionIcons[suggestion.type as keyof typeof suggestionIcons] || AlertCircle;
                    const priorityColor =
                      priorityColors[suggestion.priority as keyof typeof priorityColors] || priorityColors.low;

                    return (
                      <div
                        key={idx}
                        className={`p-4 rounded-lg border-l-4 shadow-sm ${priorityColor} transition-all hover:shadow-md`}
                      >
                        <div className="flex items-start gap-4">
                          <div className="p-2 bg-white dark:bg-black/20 rounded-lg flex-shrink-0">
                            <Icon className="h-5 w-5" />
                          </div>
                          <div className="flex-1 min-w-0 space-y-2">
                            <div className="flex items-center gap-2 flex-wrap">
                              <Badge variant="outline" className="text-xs font-semibold">
                                {suggestion.type}
                              </Badge>
                              <Badge
                                variant="secondary"
                                className={`text-xs font-semibold ${
                                  suggestion.priority === "critical"
                                    ? "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-200"
                                    : suggestion.priority === "high"
                                    ? "bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-200"
                                    : suggestion.priority === "medium"
                                    ? "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-200"
                                    : "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-200"
                                }`}
                              >
                                {suggestion.priority}
                              </Badge>
                            </div>
                            <p className="text-sm leading-relaxed break-words whitespace-pre-wrap">
                              {suggestion.description}
                            </p>
                            {suggestion.command && (
                              <div className="mt-3 bg-muted/50 rounded-lg p-3 border">
                                <code className="text-xs font-mono block break-all whitespace-pre-wrap leading-relaxed">
                                  {suggestion.command}
                                </code>
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          )}

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
