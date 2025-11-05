import { useState, useEffect } from "react";
import { ChevronDown, ChevronRight, Activity, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MetricsPanel } from "@/components/MetricsPanel";
import { AlertsPanel } from "@/components/AlertsPanel";
import type { HPA } from "@/lib/api/types";

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
    <div className="flex h-full bg-background">
      {/* Sidebar com Tree View */}
      <div className="w-72 border-r border-border flex-shrink-0 flex flex-col">
        <div className="h-14 border-b border-border flex items-center px-4">
          <h3 className="font-semibold">HPAs Monitorados</h3>
          {monitoredHPAs.length > 0 && (
            <span className="ml-auto text-xs text-muted-foreground">
              {monitoredHPAs.length} {monitoredHPAs.length === 1 ? 'HPA' : 'HPAs'}
            </span>
          )}
        </div>
        <ScrollArea className="flex-1">
          <div className="p-4 space-y-2">
            {Object.keys(hpasByCluster).length === 0 ? (
              <div className="text-center text-sm text-muted-foreground p-4">
                Nenhum HPA monitorado.
                <br />
                <span className="text-xs">
                  Use o botão "Monitorar" na aba HPAs
                </span>
              </div>
            ) : (
              Object.entries(hpasByCluster).map(([cluster, hpas]) => (
                <div key={cluster} className="space-y-1">
                  {/* Cluster header */}
                  <button
                    onClick={() => toggleCluster(cluster)}
                    className="w-full flex items-center gap-2 px-3 py-2 hover:bg-accent rounded-lg transition-colors"
                  >
                    {expandedClusters.has(cluster) ? (
                      <ChevronDown className="w-4 h-4 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="w-4 h-4 text-muted-foreground" />
                    )}
                    <span className="flex-1 font-medium text-sm truncate">{cluster}</span>
                    <span className="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded-full">
                      {hpas.length}
                    </span>
                  </button>

                  {/* HPAs list */}
                  {expandedClusters.has(cluster) && (
                    <div className="ml-6 space-y-1">
                      {hpas.map((hpa) => {
                        const isSelected =
                          selectedHPA?.cluster === hpa.cluster &&
                          selectedHPA?.namespace === hpa.namespace &&
                          selectedHPA?.name === hpa.name;

                        return (
                          <div
                            key={`${hpa.namespace}/${hpa.name}`}
                            className={`
                              group flex items-center gap-2 px-3 py-2 rounded-lg transition-all cursor-pointer
                              ${isSelected
                                ? "bg-primary/10 border border-primary/20 shadow-sm"
                                : "hover:bg-accent border border-transparent"
                              }
                            `}
                            onClick={() => setSelectedHPA(hpa)}
                          >
                            <Activity className={`w-3.5 h-3.5 flex-shrink-0 ${isSelected ? "text-primary" : "text-muted-foreground"}`} />
                            <div className="flex-1 min-w-0">
                              <div className={`text-xs font-medium truncate ${isSelected ? "text-primary" : ""}`}>
                                {hpa.name}
                              </div>
                              <div className="text-xs text-muted-foreground truncate">
                                {hpa.namespace}
                              </div>
                            </div>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity hover:bg-destructive/10 hover:text-destructive"
                              onClick={(e) => {
                                e.stopPropagation();
                                removeHPA(hpa.cluster, hpa.namespace, hpa.name);
                              }}
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Main frame - Métricas */}
      <div className="flex-1 overflow-hidden flex flex-col">
        {selectedHPA ? (
          <>
            {/* Header do HPA selecionado */}
            <div className="h-14 border-b border-border flex items-center px-6 gap-3 bg-muted/30">
              <Activity className="w-5 h-5 text-primary" />
              <div className="flex-1">
                <h2 className="font-semibold text-base">{selectedHPA.name}</h2>
                <p className="text-xs text-muted-foreground">
                  {selectedHPA.cluster} → {selectedHPA.namespace}
                </p>
              </div>
            </div>

            {/* Painéis de métricas e alertas */}
            <ScrollArea className="flex-1">
              <div className="p-6 space-y-6">
                <MetricsPanel
                  cluster={selectedHPA.cluster}
                  namespace={selectedHPA.namespace}
                  hpaName={selectedHPA.name}
                />
                <AlertsPanel cluster={selectedHPA.cluster} />
              </div>
            </ScrollArea>
          </>
        ) : (
          <div className="h-full flex items-center justify-center bg-muted/10">
            <div className="text-center text-muted-foreground max-w-md px-4">
              <Activity className="w-16 h-16 mx-auto mb-4 opacity-30" />
              <h3 className="text-lg font-semibold mb-2">Nenhum HPA selecionado</h3>
              <p className="text-sm">
                Selecione um HPA no sidebar para visualizar métricas em tempo real,
                gráficos históricos e alertas de anomalias
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
