import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import { AIHistoryPanel } from "./AIHistoryPanel";
import { AIAnalysisCard } from "./AIAnalysisCard";
import { AISettingsTab } from "./AISettingsTab";
import type { AnalysisResult } from "@/types/ai";
import { Brain, RefreshCw, Settings } from "lucide-react";

// Mock data para testar AIAnalysisCard
const mockAnalysis: AnalysisResult = {
  id: "test-123",
  resourceType: "Pod",
  cluster: "test-cluster",
  namespace: "default",
  resourceName: "test-pod",
  analysis: "## Análise de Teste\n\nEste é um exemplo de análise AI.\n\n- Item 1\n- Item 2",
  suggestions: [
    {
      type: "investigate",
      description: "Verificar logs do container",
      command: "kubectl logs test-pod -n default",
      priority: "high"
    }
  ],
  provider: "gemini",
  analyzedAt: new Date().toISOString(),
  responseTime: 2.5,
  tokensUsed: 150,
  model: "gemini-pro"
};

export function AIDiagnosticsTab() {
  const [showDebug, setShowDebug] = useState(false);
  const [showHistoryPanel, setShowHistoryPanel] = useState(false);
  const [showAnalysisCard, setShowAnalysisCard] = useState(false);

  // Testar hook sem disparar fetches automáticos
  const {
    currentAnalysis,
    providerStatus,
    stats,
    history,
    isLoadingStatus,
    isLoadingHistory,
    fetchProviderStatus,
    fetchHistory,
    fetchStats,
    deleteAnalysis,
  } = useAIDiagnostics();

  return (
    <div className="space-y-6 p-6">
      <Tabs defaultValue="diagnostics" className="w-full">
        <TabsList>
          <TabsTrigger value="diagnostics">
            <Brain className="h-4 w-4 mr-2" />
            Diagnósticos
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings className="h-4 w-4 mr-2" />
            Configurações
          </TabsTrigger>
        </TabsList>

        <TabsContent value="diagnostics" className="space-y-6">
          {/* Exibir análise atual se existir */}
          {currentAnalysis && (
            <AIAnalysisCard
              analysis={currentAnalysis}
              onClose={() => {
                // Limpar análise atual se necessário
                console.log("Fechando análise atual");
              }}
            />
          )}

          <Card>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="flex items-center gap-2">
                    <Brain className="h-6 w-6" />
                    AI-Powered Diagnostics (Debug Mode)
                  </CardTitle>
                  <CardDescription>
                    Testando hook useAIDiagnostics gradualmente
                  </CardDescription>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowDebug(!showDebug)}
                >
                  {showDebug ? "Ocultar Debug" : "Mostrar Debug"}
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {/* Botões de teste manual */}
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    onClick={() => fetchProviderStatus()}
                    disabled={isLoadingStatus}
                  >
                    <RefreshCw className={`h-4 w-4 mr-2 ${isLoadingStatus ? "animate-spin" : ""}`} />
                    Testar Provider Status
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => fetchHistory()}
                    disabled={isLoadingHistory}
                  >
                    <RefreshCw className={`h-4 w-4 mr-2 ${isLoadingHistory ? "animate-spin" : ""}`} />
                    Testar History
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => fetchStats()}
                  >
                    Testar Stats
                  </Button>
                  <Button
                    size="sm"
                    variant={showHistoryPanel ? "secondary" : "outline"}
                    onClick={() => setShowHistoryPanel(!showHistoryPanel)}
                  >
                    {showHistoryPanel ? "Ocultar" : "Mostrar"} History Panel
                  </Button>
                  <Button
                    size="sm"
                    variant={showAnalysisCard ? "secondary" : "outline"}
                    onClick={() => setShowAnalysisCard(!showAnalysisCard)}
                  >
                    {showAnalysisCard ? "Ocultar" : "Mostrar"} Analysis Card
                  </Button>
                </div>

                {/* Debug info */}
                {showDebug && (
                  <div className="rounded-lg border p-4 bg-muted/50 font-mono text-xs">
                    <div className="space-y-2">
                      <div>
                        <strong>Provider Status:</strong>{" "}
                        {providerStatus ? (
                          <span className={providerStatus.available ? "text-green-500" : "text-red-500"}>
                            {providerStatus.provider} - {providerStatus.available ? "Online" : "Offline"}
                          </span>
                        ) : (
                          <span className="text-gray-500">Not loaded</span>
                        )}
                      </div>
                      {providerStatus?.model && (
                        <div>
                          <strong>Model:</strong>{" "}
                          <span className="text-blue-500">{providerStatus.model}</span>
                        </div>
                      )}
                      <div>
                        <strong>Loading Status:</strong> {isLoadingStatus ? "Yes" : "No"}
                      </div>
                      <div>
                        <strong>Loading History:</strong> {isLoadingHistory ? "Yes" : "No"}
                      </div>
                      <div>
                        <strong>History Items:</strong> {history?.length ?? 0}
                      </div>
                      <div>
                        <strong>Stats:</strong> {stats ? JSON.stringify(stats) : "Not loaded"}
                      </div>
                    </div>
                  </div>
                )}

                {/* Status atual */}
                <div className="text-sm text-muted-foreground">
                  ✅ Hook inicializado com sucesso (sem erros)
                  <br />
                  🔍 Clique nos botões acima para testar cada função manualmente
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Testar AIHistoryPanel */}
          {showHistoryPanel && (
            <AIHistoryPanel
              history={history || []}
              isLoading={isLoadingHistory}
              onRefresh={fetchHistory}
              onViewAnalysis={(analysis) => {
                console.log("View analysis:", analysis);
                alert(`Análise selecionada: ${analysis.resourceName}`);
              }}
              onDeleteAnalysis={deleteAnalysis}
            />
          )}

          {/* Testar AIAnalysisCard */}
          {showAnalysisCard && (
            <AIAnalysisCard
              analysis={mockAnalysis}
              onClose={() => setShowAnalysisCard(false)}
            />
          )}
        </TabsContent>

        <TabsContent value="settings">
          <AISettingsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
