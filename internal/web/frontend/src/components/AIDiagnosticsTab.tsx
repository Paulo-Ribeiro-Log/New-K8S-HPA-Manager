import { useEffect } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import { AIHistoryPanel } from "./AIHistoryPanel";
import { AIAnalysisCard } from "./AIAnalysisCard";
import { AISettingsTab } from "./AISettingsTab";
import { AIStatsCard } from "./AIStatsCard";
import { AIProviderStatusCard } from "./AIProviderStatusCard";
import { AIQuickStartGuide } from "./AIQuickStartGuide";
import { Brain, Settings } from "lucide-react";

export function AIDiagnosticsTab() {
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
    clearCurrentAnalysis,
    getAnalysisById,
  } = useAIDiagnostics();

  // Auto-load data on mount
  useEffect(() => {
    fetchProviderStatus();
    fetchStats();
    fetchHistory();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Executar apenas no mount

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
          {/* Análise atual (se existir) */}
          {currentAnalysis && (
            <AIAnalysisCard analysis={currentAnalysis} onClose={clearCurrentAnalysis} />
          )}

          {/* Grid 2 colunas: Stats + Provider Status */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <AIStatsCard
              stats={stats}
              isLoading={false}
              onRefresh={fetchStats}
            />
            <AIProviderStatusCard
              providerStatus={providerStatus}
              isLoading={isLoadingStatus}
              onRefresh={fetchProviderStatus}
            />
          </div>

          {/* Quick Start Guide (se não houver histórico) */}
          {(!history || history.length === 0) && <AIQuickStartGuide />}

          {/* Histórico completo */}
          <AIHistoryPanel
            history={history || []}
            isLoading={isLoadingHistory}
            onRefresh={fetchHistory}
            onViewAnalysis={(analysis) => {
              // Carregar análise completa e scroll até o topo
              getAnalysisById(analysis.id);
              window.scrollTo({ top: 0, behavior: "smooth" });
            }}
            onDeleteAnalysis={deleteAnalysis}
          />
        </TabsContent>

        <TabsContent value="settings">
          <AISettingsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
