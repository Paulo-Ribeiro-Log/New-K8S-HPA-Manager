import { useState, useEffect } from "react";
import { Header } from "@/components/Header";
import { StatsCard } from "@/components/StatsCard";
import { TabNavigation } from "@/components/TabNavigation";
import { DashboardCharts } from "@/components/DashboardCharts";
import { SplitView } from "@/components/SplitView";
import { HPAListItem } from "@/components/HPAListItem";
import { HPAEditor } from "@/components/HPAEditor";
import { ApplyAllModal } from "@/components/ApplyAllModal";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { NodePoolApplyModal } from "@/components/NodePoolApplyModal";
import { SaveSessionModal } from "@/components/SaveSessionModal";
import { LoadSessionModal } from "@/components/LoadSessionModal";
import { LogViewer } from "@/components/LogViewer";
import { HistoryViewer } from "@/components/HistoryViewer";
import { StagingPanel } from "@/components/StagingPanel";
import { CronJobsPage } from "./CronJobsPage";
import { PrometheusPage } from "./PrometheusPage";
import {
  LayoutDashboard,
  Scale,
  Server,
  Clock,
  Activity,
  Layers,
  Package,
  Database,
  FileText,
  Search,
  Eye,
  EyeOff
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { useClusters, useNamespaces, useHPAs, useNodePools } from "@/hooks/useAPI";
import type { HPA, NodePool } from "@/lib/api/types";
import { useStaging } from "@/contexts/StagingContext";
import { useTabManager } from "@/contexts/TabContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface IndexProps {
  onLogout?: () => void;
}

const Index = ({ onLogout }: IndexProps) => {
  const [activeTab, setActiveTab] = useState("dashboard");
  const [selectedCluster, setSelectedCluster] = useState("");
  const [selectedNamespace, setSelectedNamespace] = useState("");
  const [selectedHPA, setSelectedHPA] = useState<HPA | null>(null);
  const [selectedNodePool, setSelectedNodePool] = useState<NodePool | null>(null);
  const [showApplyModal, setShowApplyModal] = useState(false);
  const [hpasToApply, setHpasToApply] = useState<Array<{ key: string; current: HPA; original: HPA }>>([]);
  const [showNodePoolApplyModal, setShowNodePoolApplyModal] = useState(false);
  const [nodePoolsToApply, setNodePoolsToApply] = useState<Array<{ key: string; current: NodePool; original: NodePool }>>([]);
  const [showSaveSessionModal, setShowSaveSessionModal] = useState(false);
  const [showLoadSessionModal, setShowLoadSessionModal] = useState(false);
  const [showLogViewer, setShowLogViewer] = useState(false);
  const [showHistoryViewer, setShowHistoryViewer] = useState(false);
  const [isContextSwitching, setIsContextSwitching] = useState(false);

  // Search filters
  const [hpaSearchQuery, setHpaSearchQuery] = useState("");
  const [nodePoolSearchQuery, setNodePoolSearchQuery] = useState("");

  // Toggle para mostrar namespaces de sistema (default: false)
  const [showSystemNamespaces, setShowSystemNamespaces] = useState(false);

  // TabManager para sincronizar estado com abas
  const { updateActiveTabState } = useTabManager();

  // Staging context
  const staging = useStaging();

  // API Hooks
  const { clusters, loading: clustersLoading } = useClusters();
  const { namespaces, loading: namespacesLoading } = useNamespaces(selectedCluster);
  // Para HPAs: sempre buscar de TODOS os namespaces (passar undefined ao invés de selectedNamespace)
  const { hpas, loading: hpasLoading, refetch: refetchHPAs } = useHPAs(selectedCluster, undefined, showSystemNamespaces);
  const { nodePools, loading: nodePoolsLoading } = useNodePools(selectedCluster);

  // Auto-select first cluster (using context instead of name)
  useEffect(() => {
    if (clusters.length > 0 && !selectedCluster) {
      setSelectedCluster(clusters[0].context);
    }
  }, [clusters, selectedCluster]);

  // 🔧 FIX: Handler para mudança de cluster com switch de contexto
  const handleClusterChange = async (newCluster: string) => {
    if (newCluster === selectedCluster) return;

    console.log(`[ClusterSwitch] Switching from ${selectedCluster} to ${newCluster}`);
    setIsContextSwitching(true);

    try {
      // 1. Chamar endpoint de switch context no backend
      await apiClient.switchContext(newCluster);
      console.log(`[ClusterSwitch] Context switched successfully to ${newCluster}`);
      
      // 2. Atualizar estado do frontend local
      setSelectedCluster(newCluster);
      setSelectedNamespace(""); // Reset namespace selection
      setSelectedHPA(null); // Reset HPA selection  
      setSelectedNodePool(null); // Reset NodePool selection
      
      // 3. Sincronizar com TabManager (CRÍTICO para SaveSessionModal)
      updateActiveTabState({
        selectedCluster: newCluster,
        selectedNamespace: "",
        selectedHPA: null,
        selectedNodePool: null,
        isContextSwitching: false
      });
      
      // 4. Mostrar toast de sucesso
      toast.success(`Contexto alterado para: ${newCluster}`);
      
    } catch (error) {
      console.error(`[ClusterSwitch] Error switching context:`, error);
      toast.error(`Erro ao alterar contexto: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
      
      // Não alterar o cluster selecionado se houve erro
      return;
    } finally {
      setIsContextSwitching(false);
    }
  };

  // Reset namespace when cluster changes
  useEffect(() => {
    setSelectedNamespace("");
  }, [selectedCluster]);

  // Auto-select first namespace for CronJobs and Prometheus
  useEffect(() => {
    if (namespaces.length > 0 && !selectedNamespace) {
      // Filter out system namespaces and pick first non-system one
      const nonSystemNamespaces = namespaces.filter(ns => !ns.isSystem);
      if (nonSystemNamespaces.length > 0) {
        setSelectedNamespace(nonSystemNamespaces[0].name);
      } else if (namespaces.length > 0) {
        setSelectedNamespace(namespaces[0].name);
      }
    }
  }, [namespaces, selectedNamespace]);

  // Calculate stats
  const stats = {
    clusters: clusters.length,
    namespaces: namespaces.length,
    hpas: hpas.length,
    nodePools: nodePools.length,
  };

  const tabs = [
    { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
    { id: "hpas", label: "HPAs", icon: Scale },
    { id: "nodepools", label: "Node Pools", icon: Server },
    { id: "staging", label: "Staging", icon: FileText, badge: staging.getChangesCount().total },
    { id: "cronjobs", label: "CronJobs", icon: Clock },
    { id: "prometheus", label: "Prometheus", icon: Activity },
  ];

  // Filter functions
  const filteredHPAs = hpas.filter(hpa => {
    if (!hpaSearchQuery) return true;
    const query = hpaSearchQuery.toLowerCase();
    return (
      hpa.name.toLowerCase().includes(query) ||
      hpa.namespace.toLowerCase().includes(query)
    );
  });

  const filteredNodePools = nodePools.filter(pool => {
    if (!nodePoolSearchQuery) return true;
    const query = nodePoolSearchQuery.toLowerCase();
    return (
      pool.name.toLowerCase().includes(query) ||
      pool.cluster_name.toLowerCase().includes(query)
    );
  });

  // Handler para aplicar HPA individual (via "Aplicar Agora")
  const handleApplySingle = (current: HPA, original: HPA) => {
    // Salvar no temp staging para permitir edição no modal
    staging?.setTempHPA(current, original);

    const key = `${current.cluster}/${current.namespace}/${current.name}`;
    setHpasToApply([{ key, current, original }]);
    setShowApplyModal(true);
  };

  const renderTabContent = () => {
    switch (activeTab) {
      case "dashboard":
        return <DashboardCharts selectedCluster={selectedCluster} />;
      
      case "hpas":
        return (
          <SplitView
            leftPanel={{
              title: "Available HPAs",
              titleAction: (
                <Button
                  variant="outline"
                  size="sm"
                  className={`${
                    showSystemNamespaces
                      ? "bg-primary/10 border-primary/30 text-primary"
                      : "bg-muted/50 border-muted-foreground/20"
                  } transition-colors`}
                  onClick={() => {
                    console.log('[Toggle] Atual:', showSystemNamespaces, '→ Novo:', !showSystemNamespaces);
                    setShowSystemNamespaces(!showSystemNamespaces);
                  }}
                  title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
                >
                  {showSystemNamespaces ? (
                    <>
                      <Eye className="w-4 h-4 mr-2" />
                      Sistema: ON
                    </>
                  ) : (
                    <>
                      <EyeOff className="w-4 h-4 mr-2" />
                      Sistema: OFF
                    </>
                  )}
                </Button>
              ),
              content: hpasLoading ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  Loading HPAs...
                </div>
              ) : hpas.length === 0 ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  {selectedCluster
                    ? "No HPAs found in this cluster"
                    : "Select a cluster to view HPAs"}
                </div>
              ) : (
                <div className="space-y-3">
                  {/* Search input */}
                  <div className="relative">
                    <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                      type="text"
                      placeholder="Buscar por nome ou namespace..."
                      value={hpaSearchQuery}
                      onChange={(e) => setHpaSearchQuery(e.target.value)}
                      className="pl-10"
                    />
                  </div>

                  {/* HPAs list */}
                  <div className="space-y-2">
                    {filteredHPAs.length === 0 ? (
                      <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                        Nenhum HPA encontrado
                      </div>
                    ) : (
                      filteredHPAs.map((hpa) => (
                        <HPAListItem
                          key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
                          name={hpa.name}
                          namespace={hpa.namespace}
                          currentReplicas={hpa.current_replicas ?? 0}
                          minReplicas={hpa.min_replicas ?? 0}
                          maxReplicas={hpa.max_replicas ?? 1}
                          isSelected={
                            selectedHPA?.name === hpa.name &&
                            selectedHPA?.namespace === hpa.namespace
                          }
                          onClick={() => setSelectedHPA(hpa)}
                        />
                      ))
                    )}
                  </div>
                </div>
              ),
            }}
            rightPanel={{
              title: "HPA Editor",
              content: (
                <HPAEditor
                  hpa={selectedHPA}
                  onApply={handleApplySingle}
                />
              ),
            }}
          />
        );

      case "nodepools":
        return (
          <SplitView
            leftPanel={{
              title: "Available Node Pools",
              content: nodePoolsLoading ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  Loading Node Pools...
                </div>
              ) : nodePools.length === 0 ? (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  {selectedCluster
                    ? "No node pools found in this cluster"
                    : "Select a cluster to view node pools"}
                </div>
              ) : (
                <div className="space-y-3">
                  {/* Search input */}
                  <div className="relative">
                    <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                      type="text"
                      placeholder="Buscar por nome ou cluster..."
                      value={nodePoolSearchQuery}
                      onChange={(e) => setNodePoolSearchQuery(e.target.value)}
                      className="pl-10"
                    />
                  </div>

                  {/* Node Pools list */}
                  <div className="space-y-2">
                    {filteredNodePools.length === 0 ? (
                      <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                        Nenhum Node Pool encontrado
                      </div>
                    ) : (
                      filteredNodePools.map((pool) => (
                        <NodePoolListItem
                          key={`${pool.cluster_name}-${pool.name}`}
                          nodePool={pool}
                          isSelected={
                            selectedNodePool?.name === pool.name &&
                            selectedNodePool?.cluster_name === pool.cluster_name
                          }
                          onClick={() => setSelectedNodePool(pool)}
                        />
                      ))
                    )}
                  </div>
                </div>
              ),
            }}
            rightPanel={{
              title: "Node Pool Editor",
              content: <NodePoolEditor nodePool={selectedNodePool} />,
            }}
          />
        );
      
      case "staging":
        return <StagingPanel />;

      case "cronjobs":
        return (
          <CronJobsPage
            selectedCluster={selectedCluster}
            onClusterChange={handleClusterChange}
            clusters={clusters}
          />
        );

      case "prometheus":
        return (
          <PrometheusPage
            selectedCluster={selectedCluster}
            onClusterChange={handleClusterChange}
            clusters={clusters}
          />
        );

      default:
        return null;
    }
  };

  return (
    <div className="flex flex-col h-screen bg-background overflow-hidden">
      <Header
        selectedCluster={selectedCluster}
        onClusterChange={handleClusterChange}
        clusters={clusters.map((c) => c.context)}
        modifiedCount={staging.getChangesCount().total}
        onApplyAll={() => {
          const changesCount = staging.getChangesCount();
          const totalChanges = changesCount.total;

          if (totalChanges === 0) {
            toast.error("Nenhuma alteração pendente");
            return;
          }

          // HPAs
          if (changesCount.hpas > 0) {
            const modifiedHPAs = staging.stagedHPAs
              .filter(hpa => hpa.isModified)
              .map(hpa => ({
                key: `${hpa.cluster}/${hpa.namespace}/${hpa.name}`,
                current: hpa,
                original: hpa.originalValues as HPA,
              }));
            setHpasToApply(modifiedHPAs);
            setShowApplyModal(true);
          }

          // Node Pools
          if (changesCount.nodePools > 0) {
            const modifiedNodePools = staging.stagedNodePools
              .filter(np => np.isModified)
              .map(np => ({
                key: `${np.cluster_name}/${np.name}`,
                current: np,
                original: { ...np, ...np.originalValues } as NodePool,
              }));
            setNodePoolsToApply(modifiedNodePools);
            setShowNodePoolApplyModal(true);
          }
        }}
        onSaveSession={() => setShowSaveSessionModal(true)}
        onLoadSession={() => setShowLoadSessionModal(true)}
        onViewLogs={() => setShowLogViewer(true)}
        onViewHistory={() => setShowHistoryViewer(true)}
        userInfo="admin@k8s.local"
        onLogout={onLogout || (() => console.log("Logout"))}
      />

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 px-6 py-3 flex-shrink-0">
        <StatsCard
          icon={Layers}
          value={clustersLoading ? "..." : String(stats.clusters)}
          label="Clusters"
        />
        <StatsCard
          icon={Package}
          value={namespacesLoading ? "..." : String(stats.namespaces)}
          label="Namespaces"
        />
        <StatsCard
          icon={Scale}
          value={hpasLoading ? "..." : String(stats.hpas)}
          label="HPAs"
        />
        <StatsCard
          icon={Database}
          value={nodePoolsLoading ? "..." : String(stats.nodePools)}
          label="Node Pools"
        />
      </div>

      <TabNavigation
        tabs={tabs}
        activeTab={activeTab}
        onTabChange={setActiveTab}
      />

      <div className="flex-1 min-h-0 overflow-auto">
        {renderTabContent()}
      </div>

      {/* Modal de Confirmação - HPAs */}
      <ApplyAllModal
        open={showApplyModal}
        onOpenChange={setShowApplyModal}
        modifiedHPAs={hpasToApply}
        onApplied={() => {
          // Disparar evento global de rescan para recarregar HPAs do cluster correto
          if (typeof window !== "undefined" && selectedCluster) {
            window.dispatchEvent(new CustomEvent("rescanHPAs", {
              detail: { cluster: selectedCluster }
            }));
          }
        }}
        onClear={() => {
          // Limpar staging area
          staging.clearStaging();
        }}
      />

      {/* Modal de Confirmação - Node Pools */}
      <NodePoolApplyModal
        open={showNodePoolApplyModal}
        onOpenChange={setShowNodePoolApplyModal}
        modifiedNodePools={nodePoolsToApply}
        onApplied={() => {
          // Disparar evento global de rescan para recarregar Node Pools do cluster correto
          if (typeof window !== "undefined" && selectedCluster) {
            window.dispatchEvent(new CustomEvent("rescanNodePools", {
              detail: { cluster: selectedCluster }
            }));
          }
        }}
        onClear={() => {
          // Limpar staging area
          staging.clearStaging();
        }}
      />

      {/* Modal de Salvar Sessão */}
      <SaveSessionModal
        open={showSaveSessionModal}
        onOpenChange={setShowSaveSessionModal}
        onSuccess={() => {
          // Opcional: mostrar toast de sucesso
          console.log("Sessão salva com sucesso!");
        }}
      />

      {/* Modal de Carregar Sessão */}
      <LoadSessionModal
        open={showLoadSessionModal}
        onOpenChange={setShowLoadSessionModal}
        onSessionLoaded={(clusterName) => {
          console.log("Sessão carregada com sucesso!");
          // Resetar namespace ANTES de trocar o cluster para evitar busca em namespace inexistente
          if (clusterName) {
            setSelectedNamespace(""); // Reset namespace PRIMEIRO
            setSelectedCluster(`${clusterName}-admin`); // Depois troca o cluster
          }
        }}
      />

      {/* Modal de Visualização de Logs */}
      <LogViewer
        open={showLogViewer}
        onOpenChange={setShowLogViewer}
      />

      <HistoryViewer
        open={showHistoryViewer}
        onOpenChange={setShowHistoryViewer}
      />
    </div>
  );
};

export default Index;
