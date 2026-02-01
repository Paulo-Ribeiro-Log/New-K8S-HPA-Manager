import { useEffect, useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import { AIHistoryPanel } from "./AIHistoryPanel";
import { AIAnalysisModal } from "./AIAnalysisModal";
import { AISettingsTab } from "./AISettingsTab";
import { AIStatsCard } from "./AIStatsCard";
import { AIProviderStatusCard } from "./AIProviderStatusCard";
import { AIQuickStartGuide } from "./AIQuickStartGuide";
import { Brain, Settings } from "lucide-react";
import type { AnalysisResult } from "@/types/ai";

export function AIDiagnosticsTab() {
  const [activeSubTab, setActiveSubTab] = useState("diagnostics");
  const [selectedAnalysis, setSelectedAnalysis] = useState<AnalysisResult | null>(null);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const {
    providerStatus,
    stats,
    history,
    isLoadingStatus,
    isLoadingHistory,
    fetchProviderStatus,
    fetchHistory,
    fetchStats,
    deleteAnalysis,
    getAnalysisById,
  } = useAIDiagnostics();

  // Auto-load data on mount (APENAS no mount - não fazer polling automático)
  useEffect(() => {
    fetchProviderStatus();
    fetchStats();
    fetchHistory();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Executar apenas no mount

  /**
   * REMOVIDO: Auto-refresh provider status a cada 30 segundos
   *
   * MOTIVO: Polling automático pode fazer chamadas desnecessárias ao backend
   * quando a aba está aberta mas não está sendo ativamente monitorada.
   * Alguns providers podem fazer chamadas de teste que consomem quota.
   *
   * SOLUÇÃO: Status é carregado apenas no mount e pode ser atualizado
   * manualmente através do botão de refresh.
   */
  // useEffect(() => {
  //   const interval = setInterval(() => {
  //     fetchProviderStatus();
  //   }, 30000); // 30 segundos
  //   return () => clearInterval(interval);
  // }, [fetchProviderStatus]);

  return (
    <div className="space-y-6 p-6">
      <Tabs value={activeSubTab} onValueChange={setActiveSubTab} className="w-full">
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
          {/* Modal de análise detalhada */}
          <AIAnalysisModal
            analysis={selectedAnalysis}
            open={isModalOpen}
            onOpenChange={(open) => {
              setIsModalOpen(open);
              if (!open) setSelectedAnalysis(null);
            }}
          />

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
              onNavigateToSettings={() => setActiveSubTab("settings")}
            />
          </div>

          {/* Quick Start Guide (se não houver histórico) */}
          {(!history || history.length === 0) && <AIQuickStartGuide />}

          {/* Histórico completo */}
          <AIHistoryPanel
            history={history || []}
            isLoading={isLoadingHistory}
            onRefresh={fetchHistory}
            onViewAnalysis={async (analysis) => {
              // Carregar análise completa pelo ID e abrir no modal
              const fullAnalysis = await getAnalysisById(analysis.id);
              if (fullAnalysis) {
                setSelectedAnalysis(fullAnalysis);
                setIsModalOpen(true);
              }
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
