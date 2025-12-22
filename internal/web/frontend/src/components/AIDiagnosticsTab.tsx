import { useEffect, useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import { AIAnalysisCard } from "./AIAnalysisCard";
import { AIHistoryPanel } from "./AIHistoryPanel";
import type { AnalysisResult } from "@/types/ai";
import { Brain, RefreshCw, AlertTriangle, CheckCircle2, XCircle } from "lucide-react";

export function AIDiagnosticsTab() {
  const {
    currentAnalysis,
    history,
    providerStatus,
    stats,
    isLoadingHistory,
    isLoadingStatus,
    fetchHistory,
    fetchProviderStatus,
    fetchStats,
    deleteAnalysis,
    clearCurrentAnalysis,
  } = useAIDiagnostics();

  const [selectedView, setSelectedView] = useState<"current" | "history">("history");

  // Load initial data
  useEffect(() => {
    fetchProviderStatus();
    fetchHistory();
    fetchStats();
  }, []);

  // Auto-refresh history every 60 seconds
  useEffect(() => {
    const interval = setInterval(() => {
      fetchHistory();
    }, 60000);

    return () => clearInterval(interval);
  }, [fetchHistory]);

  // Switch to current view when new analysis arrives
  useEffect(() => {
    if (currentAnalysis) {
      setSelectedView("current");
    }
  }, [currentAnalysis]);

  const handleViewAnalysis = (analysis: AnalysisResult) => {
    // Set as current analysis and switch to current view
    // Note: This is handled by getAnalysisById in the hook
    setSelectedView("current");
  };

  return (
    <div className="space-y-6 p-6">
      {/* Header com status do provider */}
      <Card>
        <CardHeader>
          <div className="flex items-start justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2">
                <Brain className="h-6 w-6" />
                AI-Powered Diagnostics
              </CardTitle>
              <CardDescription>
                Análise inteligente de recursos Kubernetes usando IA
              </CardDescription>
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                fetchProviderStatus();
                fetchHistory();
                fetchStats();
              }}
              disabled={isLoadingStatus}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoadingStatus ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
          </div>
        </CardHeader>

        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {/* Provider Status */}
            <div className="border rounded-lg p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Provider AI</span>
                {providerStatus ? (
                  providerStatus.available ? (
                    <Badge className="bg-green-500 hover:bg-green-600">
                      <CheckCircle2 className="h-3 w-3 mr-1" />
                      Online
                    </Badge>
                  ) : (
                    <Badge variant="destructive">
                      <XCircle className="h-3 w-3 mr-1" />
                      Offline
                    </Badge>
                  )
                ) : (
                  <Badge variant="outline">Carregando...</Badge>
                )}
              </div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Provider: <span className="font-mono">{providerStatus?.provider || "—"}</span>
                </div>
                <div>
                  Modelo: <span className="font-mono">{providerStatus?.model || "—"}</span>
                </div>
              </div>
              {providerStatus && !providerStatus.available && providerStatus.error && (
                <Alert variant="destructive" className="mt-2">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertDescription className="text-xs">
                    {providerStatus.error}
                  </AlertDescription>
                </Alert>
              )}
            </div>

            {/* Statistics */}
            <div className="border rounded-lg p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Estatísticas</span>
              </div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Total de análises: <span className="font-mono">{stats?.totalAnalyses || 0}</span>
                </div>
                <div>
                  Tempo médio: <span className="font-mono">
                    {stats?.avgResponseTime ? `${stats.avgResponseTime.toFixed(2)}s` : "—"}
                  </span>
                </div>
                <div>
                  Tokens usados: <span className="font-mono">
                    {stats?.totalTokensUsed || 0}
                  </span>
                </div>
              </div>
            </div>

            {/* Quick Stats by Resource Type */}
            <div className="border rounded-lg p-4">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Por tipo de recurso</span>
              </div>
              <div className="space-y-1 text-xs text-muted-foreground">
                {stats?.analysesByResource ? (
                  Object.entries(stats.analysesByResource).map(([type, count]) => (
                    <div key={type}>
                      {type}: <span className="font-mono">{count}</span>
                    </div>
                  ))
                ) : (
                  <div>Nenhuma análise ainda</div>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Main Content */}
      <Tabs value={selectedView} onValueChange={(v) => setSelectedView(v as "current" | "history")}>
        <TabsList className="grid w-full grid-cols-2 max-w-md">
          <TabsTrigger value="current">
            {currentAnalysis ? "Análise Atual" : "Nova Análise"}
          </TabsTrigger>
          <TabsTrigger value="history">
            Histórico ({history.length})
          </TabsTrigger>
        </TabsList>

        <TabsContent value="current" className="space-y-4">
          {currentAnalysis ? (
            <AIAnalysisCard
              analysis={currentAnalysis}
              onClose={clearCurrentAnalysis}
            />
          ) : (
            <Card>
              <CardContent className="flex items-center justify-center h-[400px]">
                <div className="text-center space-y-4">
                  <Brain className="h-16 w-16 mx-auto text-muted-foreground" />
                  <div>
                    <h3 className="text-lg font-semibold mb-2">
                      Nenhuma análise em andamento
                    </h3>
                    <p className="text-sm text-muted-foreground max-w-md mx-auto">
                      Para iniciar uma análise AI, clique no botão "Analisar com AI" em um
                      recurso nas abas Pods, Deployments, HPAs ou Nodes.
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="history" className="space-y-4">
          <AIHistoryPanel
            history={history}
            isLoading={isLoadingHistory}
            onRefresh={fetchHistory}
            onViewAnalysis={handleViewAnalysis}
            onDeleteAnalysis={deleteAnalysis}
          />
        </TabsContent>
      </Tabs>

      {/* Alert de provider indisponível */}
      {providerStatus && !providerStatus.available && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>
            <strong>Provider AI indisponível:</strong> {providerStatus.error}
            <br />
            <span className="text-xs">
              Verifique se a variável de ambiente GEMINI_API_KEY está configurada ou se o Ollama
              está rodando em {providerStatus.provider === "ollama" ? "http://localhost:11434" : "—"}.
            </span>
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
