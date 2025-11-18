import { useState, useEffect, useMemo } from "react";
import { Header } from "@/components/Header";
import { StatsCard } from "@/components/StatsCard";
import { TabNavigation } from "@/components/TabNavigation";
import { DashboardCharts } from "@/components/DashboardCharts";
import { SplitView } from "@/components/SplitView";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { HPAListItem } from "@/components/HPAListItem";
import { HPAEditor } from "@/components/HPAEditor";
import { HPATableView } from "@/components/HPATableView";
import { ApplyAllModal } from "@/components/ApplyAllModal";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { NodePoolApplyModal } from "@/components/NodePoolApplyModal";
import NodePoolSequencingModal from "@/components/NodePoolSequencingModal";
import SequenceProgressModal from "@/components/SequenceProgressModal";
import { ConfigMapsTab } from "@/components/ConfigMapsTab";
import { SecretsTab } from "@/components/SecretsTab";
import { DeploymentsTab } from "@/components/DeploymentsTab";
import ErrorBoundary from "@/components/ErrorBoundary";
import { SaveSessionModal } from "@/components/SaveSessionModal";
import { LoadSessionModal } from "@/components/LoadSessionModal";
import { LogViewer } from "@/components/LogViewer";
import { HistoryViewer } from "@/components/HistoryViewer";
import { StagingPanel } from "@/components/StagingPanel";
import { VPNWarningBanner } from "@/components/VPNWarningBanner";
import { CronJobsPage } from "./CronJobsPage";
import { PrometheusPage } from "./PrometheusPage";
import { MonitoringPage } from "./MonitoringPage";
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
  EyeOff,
  BarChart3,
  FileCode,
  Settings,
  Key,
  X
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { useClusters, useNamespaces, useHPAs, useNodePools } from "@/hooks/useAPI";
import type { HPA, NodePool } from "@/lib/api/types";
import { useStaging } from "@/contexts/StagingContext";
import { useTabManager } from "@/contexts/TabContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import { useVPNMonitor } from "@/hooks/useVPNMonitor";

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
  const [showSequencingModal, setShowSequencingModal] = useState(false);
  const [sequencedNodePools, setSequencedNodePools] = useState<NodePool[]>([]);
  const [showProgressModal, setShowProgressModal] = useState(false);
  const [progressSessionId, setProgressSessionId] = useState("");
  const [progressCluster, setProgressCluster] = useState("");
  const [progressOrigin, setProgressOrigin] = useState("");
  const [progressDest, setProgressDest] = useState("");
  const [showSaveSessionModal, setShowSaveSessionModal] = useState(false);
  const [showLoadSessionModal, setShowLoadSessionModal] = useState(false);
  const [showLogViewer, setShowLogViewer] = useState(false);
  const [showHistoryViewer, setShowHistoryViewer] = useState(false);
  const [isContextSwitching, setIsContextSwitching] = useState(false);
  const [showVPNWarning, setShowVPNWarning] = useState(false);

  // Search filters
  const [hpaSearchQuery, setHpaSearchQuery] = useState("");
  const [nodePoolSearchQuery, setNodePoolSearchQuery] = useState("");

  // Toggle para mostrar namespaces de sistema (default: false)
  const [showSystemNamespaces, setShowSystemNamespaces] = useState(false);

  // TabManager para sincronizar estado com abas
  const { updateActiveTabState } = useTabManager();

  // Staging context
  const staging = useStaging();

  // VPN Monitor - polling a cada 2 minutos
  const { isConnected: vpnConnected, checkVPN } = useVPNMonitor({
    pollingInterval: 120000, // 2 minutos
    showToastOnDisconnect: false, // Vamos usar banner ao invés de toast
    checkOnMount: true,
  });

  // Mostrar/ocultar banner de VPN
  useEffect(() => {
    setShowVPNWarning(!vpnConnected);
  }, [vpnConnected]);

  // Wrapper para setActiveTab que dispara evento e verifica VPN
  const handleTabChange = async (newTab: string) => {
    // Disparar evento de mudança de tab
    if (typeof window !== "undefined") {
      window.dispatchEvent(new Event("tabChanged"));
    }

    // Verificar VPN ao mudar de tab
    await checkVPN();

    // Mudar tab
    setActiveTab(newTab);
  };

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
    { id: "monitoring", label: "Monitoramento", icon: BarChart3 },
    { id: "configmaps", label: "ConfigMaps", icon: FileCode },
    { id: "secrets", label: "Secrets", icon: Key },
    { id: "deployments", label: "Deployments", icon: Package },
  ];

  // Filtrar namespaces
  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  // Filter functions
  const filteredHPAs = hpas.filter(hpa => {
    // Filtro por namespace selecionado
    if (selectedNamespace && hpa.namespace !== selectedNamespace) {
      return false;
    }
    
    // Filtro por busca
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
                <div className="flex items-center gap-2 flex-wrap">
                  {selectedHPA && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setSelectedHPA(null)}
                      title="Desmarcar HPA selecionado"
                    >
                      <X className="w-4 h-4 mr-1" />
                      Desmarcar
                    </Button>
                  )}
                  <Select
                    value={selectedNamespace || "__all__"}
                    onValueChange={(value) => setSelectedNamespace(value === "__all__" ? "" : value)}
                    disabled={!selectedCluster || filteredNamespaces.length === 0}
                  >
                    <SelectTrigger className="w-[140px]">
                      <SelectValue>
                        {selectedNamespace ? filteredNamespaces.find(ns => ns.name === selectedNamespace)?.name : "Todos"}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__all__">Todos</SelectItem>
                      {filteredNamespaces.map((ns) => (
                        <SelectItem key={ns.name} value={ns.name}>
                          {ns.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    variant={showSystemNamespaces ? "secondary" : "outline"}
                    size="sm"
                    onClick={() => {
                      console.log('[Toggle] Atual:', showSystemNamespaces, '→ Novo:', !showSystemNamespaces);
                      setShowSystemNamespaces(!showSystemNamespaces);
                    }}
                    title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
                  >
                    {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
                  </Button>
                </div>
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
                    {hpaSearchQuery && (
                      <button
                        type="button"
                        onClick={() => setHpaSearchQuery("")}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        aria-label="Limpar busca de HPAs"
                      >
                        ×
                      </button>
                    )}
                    <Input
                      type="text"
                      placeholder="Buscar por nome ou namespace..."
                      value={hpaSearchQuery}
                      onChange={(e) => setHpaSearchQuery(e.target.value)}
                      className="pl-10 pr-8"
                    />
                  </div>

                  {/* HPAs list */}
                  <div className="space-y-2">
                    {filteredHPAs.length === 0 ? (
                      <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                        Nenhum HPA encontrado
                      </div>
                    ) : (
                      filteredHPAs.map((hpa) => {
                        // 🔧 FIX: Buscar versão do staging se existir
                        const stagedHPA = staging?.stagedHPAs.find(
                          h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
                        );
                        const displayHPA = stagedHPA || hpa;

                        return (
                          <HPAListItem
                            key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
                            name={displayHPA.name}
                            namespace={displayHPA.namespace}
                            cluster={displayHPA.cluster}
                            currentReplicas={displayHPA.current_replicas ?? 0}
                            minReplicas={displayHPA.min_replicas ?? 0}
                            maxReplicas={displayHPA.max_replicas ?? 1}
                            isSelected={
                              selectedHPA?.name === hpa.name &&
                              selectedHPA?.namespace === hpa.namespace
                            }
                            onClick={() => {
                              // Toggle: se clicar no item já selecionado, desmarca
                              if (selectedHPA?.name === hpa.name && selectedHPA?.namespace === hpa.namespace) {
                                setSelectedHPA(null);
                              } else {
                                setSelectedHPA(displayHPA);
                              }
                            }}
                            onMonitor={async () => {
                              try {
                                // Verificar se já existe no localStorage
                                const stored = localStorage.getItem("monitored_hpas");
                                const current = stored ? JSON.parse(stored) : [];

                                const exists = current.some((h: any) =>
                                  h.cluster === displayHPA.cluster &&
                                  h.namespace === displayHPA.namespace &&
                                  h.name === displayHPA.name
                                );

                                if (exists) {
                                  toast.info(`HPA ${displayHPA.name} já está sendo monitorado`);
                                  handleTabChange("monitoring");
                                  return;
                                }

                                // 1. Adicionar HPA ao monitoring engine (backend)
                                console.log("[onMonitor] Adding HPA to monitoring engine:", {
                                  cluster: displayHPA.cluster,
                                  namespace: displayHPA.namespace,
                                  name: displayHPA.name,
                                });

                                await apiClient.addHPAToMonitoring(
                                  displayHPA.cluster,
                                  displayHPA.namespace,
                                  displayHPA.name
                                );

                                // 2. Verificar se monitoring engine está rodando
                                const status = await apiClient.getMonitoringStatus();
                                console.log("[onMonitor] Monitoring status:", status);

                                // 3. Iniciar monitoring engine se não estiver rodando
                                if (!status.running) {
                                  console.log("[onMonitor] Starting monitoring engine...");
                                  await apiClient.startMonitoring();
                                  toast.success("Monitoring engine iniciado");
                                }

                                // 4. Salvar no localStorage (para UI)
                                current.push({
                                  cluster: displayHPA.cluster,
                                  namespace: displayHPA.namespace,
                                  name: displayHPA.name,
                                  hpa: displayHPA
                                });
                                localStorage.setItem("monitored_hpas", JSON.stringify(current));

                                toast.success(`HPA ${displayHPA.name} adicionado ao monitoramento`);
                                handleTabChange("monitoring");
                              } catch (error) {
                                console.error("[onMonitor] Error:", error);
                                toast.error(
                                  `Erro ao adicionar HPA ao monitoramento: ${
                                    error instanceof Error ? error.message : "Erro desconhecido"
                                  }`
                                );
                              }
                            }}
                            isModified={!!stagedHPA}
                          />
                        );
                      })
                    )}
                  </div>
                </div>
              ),
            }}
            rightPanel={{
              title: "HPA Editor",
              content: selectedHPA ? (
                <div className="h-full overflow-auto">
                  <HPAEditor
                    hpa={selectedHPA}
                    onApply={handleApplySingle}
                  />
                </div>
              ) : (
                <div className="p-4 overflow-auto">
                  <HPATableView
                    hpas={filteredHPAs}
                    onSelectHPA={(hpa) => setSelectedHPA(hpa)}
                  />
                </div>
              ),
            }}
          />
        );
      
      case "configmaps":
        return (
          <ConfigMapsTab
            cluster={selectedCluster}
            namespaces={namespaces}
            selectedNamespace={selectedNamespace}
            onNamespaceChange={setSelectedNamespace}
            showSystemNamespaces={showSystemNamespaces}
            onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
          />
        );

      case "secrets":
        return (
          <ErrorBoundary componentName="Secrets Tab">
            <SecretsTab
              cluster={selectedCluster}
              namespaces={namespaces}
              selectedNamespace={selectedNamespace}
              onNamespaceChange={setSelectedNamespace}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
            />
          </ErrorBoundary>
        );

      case "deployments":
        return (
          <ErrorBoundary componentName="Deployments Tab">
            <DeploymentsTab
              cluster={selectedCluster}
              namespaces={namespaces}
              selectedNamespace={selectedNamespace}
              onNamespaceChange={setSelectedNamespace}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
            />
          </ErrorBoundary>
        );

      case "nodepools":
        const markedNodePools = staging?.stagedNodePools.filter(
          np => np.sequence_order > 0
        ) || [];

        return (
          <SplitView
            leftPanel={{
              title: "Available Node Pools",
              titleAction: markedNodePools.length === 2 ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="bg-primary/10 border-primary/30 text-primary hover:bg-primary/20"
                  onClick={() => {
                    setSequencedNodePools(markedNodePools);
                    setShowSequencingModal(true);
                  }}
                  title="Configurar cordon/drain para sequenciamento"
                >
                  <Settings className="h-4 w-4 mr-2" />
                  Configure Sequencing ({markedNodePools.length})
                </Button>
              ) : null,
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
                    {nodePoolSearchQuery && (
                      <button
                        type="button"
                        onClick={() => setNodePoolSearchQuery("")}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        aria-label="Limpar busca de Node Pools"
                      >
                        ×
                      </button>
                    )}
                    <Input
                      type="text"
                      placeholder="Buscar por nome ou cluster..."
                      value={nodePoolSearchQuery}
                      onChange={(e) => setNodePoolSearchQuery(e.target.value)}
                      className="pl-10 pr-8"
                    />
                  </div>

                  {/* Node Pools list */}
                  <div className="space-y-2">
                    {filteredNodePools.length === 0 ? (
                      <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
                        Nenhum Node Pool encontrado
                      </div>
                    ) : (
                      filteredNodePools.map((pool) => {
                        // 🔧 FIX: Buscar versão do staging se existir
                        const stagedPool = staging?.stagedNodePools.find(
                          np => np.cluster_name === pool.cluster_name && np.name === pool.name
                        );
                        const displayPool = stagedPool || pool;

                        return (
                          <NodePoolListItem
                            key={`${pool.cluster_name}-${pool.name}`}
                            nodePool={displayPool}
                            isSelected={
                              selectedNodePool?.name === pool.name &&
                              selectedNodePool?.cluster_name === pool.cluster_name
                            }
                            onClick={() => setSelectedNodePool(displayPool)}
                          />
                        );
                      })
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
          />
        );

      case "prometheus":
        return (
          <PrometheusPage
            selectedCluster={selectedCluster}
          />
        );

      case "monitoring":
        return <MonitoringPage />;

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

      {/* Ocultar cards de estatísticas nas abas Monitoramento, ConfigMaps, Secrets e Deployments */}
      {activeTab !== "monitoring" && activeTab !== "configmaps" && activeTab !== "secrets" && activeTab !== "deployments" && (
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
      )}

      {/* Banner de VPN desconectada */}
      {showVPNWarning && (
        <VPNWarningBanner onDismiss={() => setShowVPNWarning(false)} />
      )}

      <TabNavigation
        tabs={tabs}
        activeTab={activeTab}
        onTabChange={handleTabChange}
      />

      <div className="flex-1 min-h-0 overflow-auto">
        {renderTabContent()}
      </div>

      {/* Modal de Confirmação - HPAs */}
      <ApplyAllModal
        open={showApplyModal}
        onOpenChange={setShowApplyModal}
        modifiedHPAs={hpasToApply}
        checkVPN={checkVPN} // Passar função de validação VPN
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
        checkVPN={checkVPN} // Passar função de validação VPN
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

      {/* Modal de Configuração de Sequenciamento - Cordon/Drain */}
      <NodePoolSequencingModal
        open={showSequencingModal}
        onOpenChange={setShowSequencingModal}
        nodePools={sequencedNodePools}
        onConfirm={async (config) => {
          try {
            // Chamar API para executar sequenciamento
            const response = await apiClient.executeNodePoolSequence(config);

            // Fechar modal de configuração
            setShowSequencingModal(false);

            // Extrair informações para o modal de progresso
            const sessionId = response.data?.session_id;
            const origin = config.node_pools.find((np) => np.sequence_order === 1);
            const dest = config.node_pools.find((np) => np.sequence_order === 2);

            if (sessionId && origin && dest) {
              // Configurar estados do modal de progresso
              setProgressSessionId(sessionId);
              setProgressCluster(config.cluster);
              setProgressOrigin(origin.name);
              setProgressDest(dest.name);

              // Abrir modal de progresso
              setShowProgressModal(true);

              toast.success(response.message || "Sequenciamento iniciado com sucesso!");
            } else {
              toast.error("Resposta inválida do servidor - sessão não encontrada");
            }

            // Limpar seleção de node pools
            setSequencedNodePools([]);
          } catch (error: any) {
            toast.error(error.message || "Erro ao executar sequenciamento");
          }
        }}
        onCancel={() => {
          setShowSequencingModal(false);
          setSequencedNodePools([]);
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

      {/* Sequence Progress Modal */}
      <SequenceProgressModal
        open={showProgressModal}
        onOpenChange={setShowProgressModal}
        sessionId={progressSessionId}
        clusterName={progressCluster}
        originName={progressOrigin}
        destName={progressDest}
      />
    </div>
  );
};

export default Index;
