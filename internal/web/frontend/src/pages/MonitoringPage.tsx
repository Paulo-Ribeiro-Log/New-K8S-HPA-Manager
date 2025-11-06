import { useState, useEffect } from "react";
import { ChevronDown, ChevronRight, Activity, Trash2, Circle, PanelLeftClose, PanelLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MetricsPanel } from "@/components/MetricsPanel";
import { AlertsPanel } from "@/components/AlertsPanel";
import { apiClient } from "@/lib/api/client";
import type { HPA, MonitoringStatus } from "@/lib/api/types";

interface MonitoredHPA {
  cluster: string;
  namespace: string;
  name: string;
  hpa: HPA;
}

interface MonitoringPageProps {
  // HPAs serão gerenciados via localStorage
}

export const MonitoringPage = ({}: MonitoringPageProps) => {
  const [monitoredHPAs, setMonitoredHPAs] = useState<MonitoredHPA[]>([]);
  const [selectedHPA, setSelectedHPA] = useState<MonitoredHPA | null>(null);
  const [expandedClusters, setExpandedClusters] = useState<Set<string>>(new Set());
  const [monitoringStatus, setMonitoringStatus] = useState<MonitoringStatus | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  // Carregar HPAs monitorados do localStorage
  useEffect(() => {
    const stored = localStorage.getItem("monitored_hpas");
    console.log("[MonitoringPage] localStorage data:", stored);
    if (stored) {
      try {
        const parsed = JSON.parse(stored) as MonitoredHPA[];
        console.log("[MonitoringPage] Parsed HPAs:", parsed);
        setMonitoredHPAs(parsed);
        // Auto-expandir todos os clusters ao carregar
        const clusters = new Set<string>(parsed.map((h) => h.cluster));
        setExpandedClusters(clusters);
      } catch (e) {
        console.error("Failed to parse monitored HPAs:", e);
      }
    }
  }, []);

  // Buscar status do monitoring engine periodicamente
  useEffect(() => {
    const fetchStatus = async () => {
      try {
        const status = await apiClient.getMonitoringStatus();
        console.log("[MonitoringPage] Status do engine:", status);
        setMonitoringStatus(status);
      } catch (error) {
        console.error("[MonitoringPage] Erro ao buscar status:", error);
      }
    };

    // Buscar status inicial
    fetchStatus();

    // Atualizar a cada 10 segundos
    const interval = setInterval(fetchStatus, 10000);

    return () => clearInterval(interval);
  }, []);

  // Agrupar HPAs por cluster
  const hpasByCluster = monitoredHPAs.reduce((acc, hpa) => {
    if (!acc[hpa.cluster]) {
      acc[hpa.cluster] = [];
    }
    acc[hpa.cluster].push(hpa);
    return acc;
  }, {} as Record<string, MonitoredHPA[]>);

  const toggleCluster = (cluster: string) => {
    setExpandedClusters(prev => {
      const next = new Set(prev);
      if (next.has(cluster)) {
        next.delete(cluster);
      } else {
        next.add(cluster);
      }
      return next;
    });
  };

  const removeHPA = (cluster: string, namespace: string, name: string) => {
    const updated = monitoredHPAs.filter(
      h => !(h.cluster === cluster && h.namespace === namespace && h.name === name)
    );
    setMonitoredHPAs(updated);
    localStorage.setItem("monitored_hpas", JSON.stringify(updated));

    // Se o HPA removido estava selecionado, limpar seleção
    if (selectedHPA?.cluster === cluster && selectedHPA?.namespace === namespace && selectedHPA?.name === name) {
      setSelectedHPA(null);
    }
  };

  console.log("[MonitoringPage] Rendering. HPAs count:", monitoredHPAs.length);
  console.log("[MonitoringPage] HPAs by cluster:", Object.keys(hpasByCluster));

  return (
    <div className="h-full flex relative">
      {/* Sidebar - HPAs monitorados */}
      <div
        className={`
          h-full bg-background border-r border-border transition-all duration-300 ease-in-out
          ${sidebarOpen ? "w-80" : "w-0"}
          overflow-hidden flex flex-col
        `}
      >
        {/* Sidebar Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-muted/30">
          <div className="flex items-center gap-2">
            <Activity className="w-4 h-4 text-primary" />
            <h2 className="font-semibold text-sm">HPAs monitorados</h2>
          </div>
          <div className="flex items-center gap-3">
            {monitoredHPAs.length > 0 && (
              <span className="text-xs text-muted-foreground">
                {monitoredHPAs.length} {monitoredHPAs.length === 1 ? "HPA" : "HPAs"}
              </span>
            )}
            {monitoringStatus && (
              <div className="flex items-center gap-1.5">
                <Circle
                  className={`w-2 h-2 ${
                    monitoringStatus.running
                      ? "fill-green-500 text-green-500 animate-pulse"
                      : "fill-gray-400 text-gray-400"
                  }`}
                />
                <span
                  className={`text-xs font-medium ${
                    monitoringStatus.running ? "text-green-600" : "text-muted-foreground"
                  }`}
                >
                  {monitoringStatus.running ? "Ativo" : "Parado"}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Sidebar Content */}
        <div className="flex-1 overflow-hidden">
          {Object.keys(hpasByCluster).length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-16 px-4 text-center text-muted-foreground">
              <Activity className="w-10 h-10 opacity-20" />
              <div className="space-y-1">
                <p className="text-sm font-medium leading-tight">Nenhum HPA monitorado</p>
                <p className="text-xs leading-relaxed">
                  Use o botão &quot;Monitorar&quot; na aba HPAs
                </p>
              </div>
            </div>
          ) : (
            <ScrollArea className="h-full">
              <div className="space-y-3 p-4">
                {Object.entries(hpasByCluster).map(([cluster, hpas]) => (
                  <Card
                    key={cluster}
                    className="border border-border/60 bg-background/80 shadow-sm overflow-hidden"
                  >
                    <button
                      onClick={() => toggleCluster(cluster)}
                      className="w-full flex items-center gap-2 px-4 py-3 transition-colors hover:bg-accent/60"
                    >
                      {expandedClusters.has(cluster) ? (
                        <ChevronDown className="w-4 h-4 text-muted-foreground" />
                      ) : (
                        <ChevronRight className="w-4 h-4 text-muted-foreground" />
                      )}
                      <span className="flex-1 font-medium text-sm truncate text-left">
                        {cluster}
                      </span>
                      <span className="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded-full">
                        {hpas.length}
                      </span>
                    </button>

                    {expandedClusters.has(cluster) && (
                      <div className="px-4 pb-3 space-y-2">
                        {hpas.map((hpa) => {
                          const isSelected =
                            selectedHPA?.cluster === hpa.cluster &&
                            selectedHPA?.namespace === hpa.namespace &&
                            selectedHPA?.name === hpa.name;

                          return (
                            <Card
                              key={`${hpa.namespace}/${hpa.name}`}
                              className={`
                                group flex items-center gap-3 px-3 py-2 cursor-pointer transition-all duration-200 border
                                ${
                                  isSelected
                                    ? "border-primary bg-primary/10 shadow-sm"
                                    : "border-border/60 hover:border-primary/40 hover:bg-accent/40"
                                }
                              `}
                              onClick={() => setSelectedHPA(hpa)}
                            >
                              <Activity
                                className={`w-4 h-4 flex-shrink-0 ${
                                  isSelected ? "text-primary" : "text-muted-foreground"
                                }`}
                              />
                              <div className="flex-1 min-w-0">
                                <div
                                  className={`text-xs font-semibold truncate ${
                                    isSelected ? "text-primary" : "text-foreground"
                                  }`}
                                >
                                  {hpa.name}
                                </div>
                                <div className="text-[11px] text-muted-foreground truncate">
                                  {hpa.namespace}
                                </div>
                              </div>
                              <Button
                                variant="ghost"
                                size="icon"
                                className={`h-7 w-7 rounded-full transition-opacity ${
                                  isSelected ? "opacity-100" : "opacity-0 group-hover:opacity-100"
                                } hover:bg-destructive/10 hover:text-destructive`}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  removeHPA(hpa.cluster, hpa.namespace, hpa.name);
                                }}
                                title="Remover do monitoramento"
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            </Card>
                          );
                        })}
                      </div>
                    )}
                  </Card>
                ))}
              </div>
            </ScrollArea>
          )}
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col h-full overflow-hidden">
        {/* Main Header com botão toggle */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-muted/30">
          <div className="flex items-center gap-3">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="h-8 w-8"
              title={sidebarOpen ? "Esconder sidebar" : "Mostrar sidebar"}
            >
              {sidebarOpen ? (
                <PanelLeftClose className="h-4 w-4" />
              ) : (
                <PanelLeft className="h-4 w-4" />
              )}
            </Button>
            <h2 className="font-semibold text-sm">
              {selectedHPA ? selectedHPA.name : "Selecione um HPA"}
            </h2>
          </div>
          {selectedHPA && (
            <span className="text-xs text-muted-foreground">
              {selectedHPA.cluster} · {selectedHPA.namespace}
            </span>
          )}
        </div>

        {/* Main Content */}
        <div className="flex-1 overflow-auto p-4">
          {selectedHPA ? (
            <div className="space-y-6">
              <MetricsPanel
                cluster={selectedHPA.cluster}
                namespace={selectedHPA.namespace}
                hpaName={selectedHPA.name}
              />
              <AlertsPanel cluster={selectedHPA.cluster} />
            </div>
          ) : (
            <div className="flex h-full items-center justify-center text-muted-foreground">
              <div className="text-center max-w-md px-4 space-y-2">
                <Activity className="w-14 h-14 mx-auto opacity-20" />
                <h3 className="text-lg font-semibold">Nenhum HPA selecionado</h3>
                <p className="text-sm">
                  Escolha um HPA na lista para visualizar métricas em tempo real,
                  gráficos históricos e alertas de anomalias.
                </p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
