import { useState, useEffect, useMemo, useRef } from "react";
import { Header } from "@/components/Header";
import { StatsCard } from "@/components/StatsCard";
import { TabNavigation } from "@/components/TabNavigation";
import { WorkloadMenu } from "@/components/WorkloadMenu";
import { ToolsMenu } from "@/components/ToolsMenu";
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
import { HPAExportButton } from "@/components/HPAExportButton";
import { ApplyAllModal } from "@/components/ApplyAllModal";
import { NodePoolListItem } from "@/components/NodePoolListItem";
import { NodePoolEditor } from "@/components/NodePoolEditor";
import { NodePoolApplyModal } from "@/components/NodePoolApplyModal";
import NodePoolSequencingModal from "@/components/NodePoolSequencingModal";
import SequenceProgressModal from "@/components/SequenceProgressModal";
import { ConfigMapsTab } from "@/components/ConfigMapsTab";
import { IngressTab } from "@/components/IngressTab";
import { NamespacesTab } from "@/components/NamespacesTab";
import { SecretsTab } from "@/components/SecretsTab";
import { DeploymentsTab } from "@/components/DeploymentsTab";
import { DaemonSetsTab } from "@/components/DaemonSetsTab";
import { StatefulSetsTab } from "@/components/StatefulSetsTab";
import { ContainersTab } from "@/components/ContainersTab";
import { PodsPanel } from "@/components/PodsPanel";
import { EventsTab } from "@/components/EventsTab";
import ErrorBoundary from "@/components/ErrorBoundary";
import { SaveSessionModal } from "@/components/SaveSessionModal";
import { LoadSessionModal } from "@/components/LoadSessionModal";
import { LogViewer } from "@/components/LogViewer";
import { HistoryViewer } from "@/components/HistoryViewer";
import { StagingPanel } from "@/components/StagingPanel";
import { VPNWarningBanner } from "@/components/VPNWarningBanner";
import { CriticalAlertsBanner } from "@/components/CriticalAlertsBanner";
import { CronJobsPage } from "./CronJobsPage";
import { PrometheusPage } from "./PrometheusPage";
import { MonitoringPage } from "./MonitoringPage";
import { ServiceMeshGraph } from "@/components/ServiceMeshGraph";
import { AIDiagnosticsTab } from "@/components/AIDiagnosticsTab";
import { HealthCheckingTab } from "@/components/HealthCheckingTab";
import { HelmTab } from "@/components/HelmTab";
import { GitHubReleasesTab } from "@/components/GitHubReleasesTab";
import { NexusValuesDiffPanel } from "@/components/NexusValuesDiffPanel";
import { DependenciesTab } from "@/components/DependenciesTab";
import CertificatesTab from "@/components/CertificatesTab";
import {
  LayoutDashboard,
  Scale,
  Server,
  Layers,
  Package,
  Database,
  FileText,
  Search,
  Eye,
  EyeOff,
  BarChart3,
  Settings,
  X,
  RefreshCcw,
  Network,
  Brain,
  Activity,
  PackageOpen,
  GitCompareArrows,
  FileCode,
  Link2,
  Shield,
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
import { ClusterContextCard } from "@/components/ClusterContextCard";

interface IndexProps {
  onLogout?: () => void;
}

const Index = ({ onLogout }: IndexProps) => {
  const [activeTab, setActiveTab] = useState("dashboard");
  const [selectedCluster, setSelectedCluster] = useState("");
  const [selectedNamespace, setSelectedNamespace] = useState(""); // Namespace global (HPAs, Namespaces tab)
  const [selectedHPA, setSelectedHPA] = useState<HPA | null>(null);
  const [selectedNodePool, setSelectedNodePool] = useState<NodePool | null>(null);

  // 🔄 Namespace independente por aba workload (evita interferência ao trocar namespaces em outras abas)
  const [podsNamespace, setPodsNamespace] = useState("");
  const [configMapsNamespace, setConfigMapsNamespace] = useState("");
  const [deploymentsNamespace, setDeploymentsNamespace] = useState("");
  const [secretsNamespace, setSecretsNamespace] = useState("");
  const [containersNamespace, setContainersNamespace] = useState("");
  const [ingressNamespace, setIngressNamespace] = useState("");
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

  // HPA Selection state
  const [hpaSelectionMode, setHpaSelectionMode] = useState(false);
  const [selectedHPAsForExport, setSelectedHPAsForExport] = useState<Set<string>>(new Set());
  const [selectedHPAObjects, setSelectedHPAObjects] = useState<HPA[]>([]);

  // Estado para forçar re-renderização quando HPAs monitorados mudam
  const [monitoringRefreshKey, setMonitoringRefreshKey] = useState(0);

  // Estado para navegação pendente de HPA (usado quando vindo do Monitoramento)
  const [pendingHPANavigation, setPendingHPANavigation] = useState<{
    cluster: string;
    namespace: string;
    hpaName: string;
  } | null>(null);

  // Função para navegar diretamente para um HPA no Monitoramento
  const navigateToHPAInMonitoring = async (cluster: string, namespace: string, hpaName: string) => {
    try {
      console.log("[NavigateToHPA] Iniciando navegação:", { cluster, namespace, hpaName });

      // 1. Toast informativo antes de iniciar
      toast.info(`Carregando ${hpaName}...`);

      // 2. Configurar navegação pendente PRIMEIRO (antes de qualquer mudança)
      console.log("[NavigateToHPA] Configurando navegação pendente");
      setPendingHPANavigation({ cluster, namespace, hpaName });

      // 3. Trocar para o cluster correto (se necessário)
      if (cluster !== selectedCluster) {
        console.log("[NavigateToHPA] Trocando cluster de", selectedCluster, "para", cluster);
        await handleClusterChange(cluster);
        await new Promise(resolve => setTimeout(resolve, 100));
      }

      // 4. Trocar para aba de monitoramento
      handleTabChange("monitoring");

      console.log("[NavigateToHPA] Navegação configurada com sucesso");
    } catch (error) {
      console.error("[NavigateToHPA] Erro ao navegar:", error);
      toast.error("Erro ao navegar para o HPA");
    }
  };

  // TabManager para sincronizar estado com abas
  const { updateActiveTabState } = useTabManager();

  // 🔄 Ref para rastrear cluster anterior (evitar reset ao restaurar aba)
  const previousClusterRef = useRef<string>("");

  // 🔄 Ref para rastrear quais componentes workload já foram montados (lazy loading)
  const hasBeenMounted = useRef({
    pods: false,
    configmaps: false,
    deployments: false,
    secrets: false,
    containers: false,
    ingresses: false,
    healthcheck: false,
  });

  // 🔄 Marcar componente como montado quando usuário acessa a aba
  useEffect(() => {
    const workloadTabs: Array<keyof typeof hasBeenMounted.current> = ["pods", "configmaps", "deployments", "secrets", "containers", "ingresses", "healthcheck"];
    if (workloadTabs.includes(activeTab as any)) {
      hasBeenMounted.current[activeTab as keyof typeof hasBeenMounted.current] = true;
    }
  }, [activeTab]);

  // 💾 PERSISTIR ESTADO no TabContext sempre que estados locais mudarem
  useEffect(() => {
    updateActiveTabState({
      activeTab,
      selectedCluster,
      selectedNamespace,
      selectedHPA,
      selectedNodePool,
      showApplyModal,
      hpasToApply,
      showNodePoolApplyModal,
      nodePoolsToApply,
      showSaveSessionModal,
      showLoadSessionModal,
      isContextSwitching,
    });
  }, [
    activeTab,
    selectedCluster,
    selectedNamespace,
    selectedHPA,
    selectedNodePool,
    showApplyModal,
    hpasToApply,
    showNodePoolApplyModal,
    nodePoolsToApply,
    showSaveSessionModal,
    showLoadSessionModal,
    isContextSwitching,
    updateActiveTabState,
  ]);

  // 🧹 RESET DE CONTEXTOS DEPENDENTES quando cluster mudar
  useEffect(() => {
    // Ao trocar de cluster, limpar estados dependentes do cluster anterior
    // Usa ref para evitar reset ao restaurar aba (apenas reset em mudanças manuais)

    if (previousClusterRef.current && previousClusterRef.current !== selectedCluster && selectedCluster) {
      console.log(`[ClusterChange] Cluster mudou de ${previousClusterRef.current} para ${selectedCluster} - limpando contextos`);

      // Resetar seleções específicas do cluster
      setSelectedNamespace("");
      setSelectedHPA(null);
      setSelectedNodePool(null);

      // Limpar modals que dependem do cluster
      setShowApplyModal(false);
      setHpasToApply([]);
      setShowNodePoolApplyModal(false);
      setNodePoolsToApply([]);

      toast.info(`Cluster alterado para ${selectedCluster}. Contexto de busca resetado.`);
    }

    // Atualizar ref para próxima comparação
    previousClusterRef.current = selectedCluster;
  }, [selectedCluster]); // Executar sempre que cluster mudar

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
  const { namespaces, loading: namespacesLoading, refetch: refetchNamespaces } = useNamespaces(selectedCluster);
  // Para HPAs: sempre buscar de TODOS os namespaces (passar undefined ao invés de selectedNamespace)
  const { hpas, loading: hpasLoading, refetch: refetchHPAs } = useHPAs(selectedCluster, undefined, showSystemNamespaces);
  const { nodePools, loading: nodePoolsLoading, refetch: refetchNodePools } = useNodePools(selectedCluster);

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

  // Reset namespace when cluster changes (exceto quando há navegação pendente)
  useEffect(() => {
    if (!pendingHPANavigation) {
      setSelectedNamespace("");
    }
  }, [selectedCluster, pendingHPANavigation]);

  // 🔧 useEffect para processar navegação pendente de HPA
  useEffect(() => {
    // Não processar se não há navegação pendente
    if (!pendingHPANavigation) return;

    // Debug: sempre logar estado atual quando há navegação pendente
    console.log("[PendingNavigation] Estado atual:", {
      pendingNavigation: pendingHPANavigation,
      hpasLoading,
      hpasCount: hpas.length,
      selectedCluster,
      selectedNamespace,
      clusterMatch: selectedCluster === pendingHPANavigation.cluster,
      namespaceMatch: selectedNamespace === pendingHPANavigation.namespace,
    });

    // Verificar se todas as condições estão satisfeitas
    const allConditionsMet =
      !hpasLoading &&
      hpas.length > 0 &&
      selectedCluster === pendingHPANavigation.cluster &&
      selectedNamespace === pendingHPANavigation.namespace;

    if (!allConditionsMet) {
      console.log("[PendingNavigation] ⏳ Aguardando condições serem satisfeitas...");
      return;
    }

    console.log("[PendingNavigation] ✅ Todas as condições satisfeitas, processando navegação pendente:", pendingHPANavigation);

    // Buscar o HPA específico
    const targetHPA = hpas.find(
      (h) =>
        h.cluster === pendingHPANavigation.cluster &&
        h.namespace === pendingHPANavigation.namespace &&
        h.name === pendingHPANavigation.hpaName
    );

    if (targetHPA) {
      console.log("[PendingNavigation] ✅ HPA encontrado, selecionando:", targetHPA.name);
      setSelectedHPA(targetHPA);
      toast.success(`HPA ${targetHPA.name} carregado para edição`);
    } else {
      const availableHPAs = hpas
        .filter(h => h.cluster === pendingHPANavigation.cluster && h.namespace === pendingHPANavigation.namespace)
        .map(h => h.name);
      console.warn(
        `[PendingNavigation] ❌ HPA "${pendingHPANavigation.hpaName}" não encontrado. HPAs disponíveis em ${pendingHPANavigation.namespace}:`,
        availableHPAs.length > 0 ? availableHPAs : "nenhum"
      );
      toast.error(`HPA ${pendingHPANavigation.hpaName} não encontrado no namespace ${pendingHPANavigation.namespace}`);
    }

    // Limpar navegação pendente
    console.log("[PendingNavigation] Limpando navegação pendente");
    setPendingHPANavigation(null);
  }, [pendingHPANavigation, hpas, hpasLoading, selectedCluster, selectedNamespace]);

  // Calculate stats
  const stats = {
    clusters: clusters.length,
    namespaces: namespaces.length,
    hpas: hpas.length,
    nodePools: nodePools.length,
  };

  // Abas que ficam no TabNavigation (topo) - SEM as abas workload e tools
  const tabs = [
    { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
    { id: "hpas", label: "HPAs", icon: Scale },
    { id: "nodepools", label: "Node Pools", icon: Server },
    { id: "staging", label: "Staging", icon: FileText, badge: staging.getChangesCount().total },
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

                        // Verificar se HPA está sendo monitorado
                        const stored = localStorage.getItem("monitored_hpas");
                        const monitoredHPAs = stored ? JSON.parse(stored) : [];
                        const isMonitored = monitoredHPAs.some((h: any) =>
                          h.cluster === displayHPA.cluster &&
                          h.namespace === displayHPA.namespace &&
                          h.name === displayHPA.name
                        );

                        return (
                          <HPAListItem
                            key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}-${monitoringRefreshKey}`}
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
                            isMonitored={isMonitored}
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

                                // Se já está sendo monitorado, navegar direto para o HPA no monitoramento
                                if (exists) {
                                  await navigateToHPAInMonitoring(
                                    displayHPA.cluster,
                                    displayHPA.namespace,
                                    displayHPA.name
                                  );
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

                                // 5. Forçar re-renderização para atualizar ícone imediatamente
                                setMonitoringRefreshKey(prev => prev + 1);

                                toast.success(`HPA ${displayHPA.name} adicionado ao monitoramento`);

                                // 🔧 FIX: Não trocar de aba automaticamente ao adicionar novo HPA
                                // O usuário pode continuar navegando e adicionando outros HPAs
                                // Apenas trocar se clicar em HPA já monitorado (comportamento acima)
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
              titleAction: !selectedHPA ? (
                <HPAExportButton
                  hpas={selectedHPAsForExport.size > 0 ? selectedHPAObjects : filteredHPAs}
                  selectionMode={hpaSelectionMode}
                  selectedHPAs={selectedHPAsForExport}
                  onToggleSelectionMode={() => {
                    setHpaSelectionMode(!hpaSelectionMode);
                    // Limpar seleção ao sair do modo
                    if (hpaSelectionMode) {
                      setSelectedHPAsForExport(new Set());
                      setSelectedHPAObjects([]);
                    }
                  }}
                  onClearSelection={() => {
                    setSelectedHPAsForExport(new Set());
                    setSelectedHPAObjects([]);
                  }}
                />
              ) : undefined,
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
                    selectionMode={hpaSelectionMode}
                    selectedHPAs={selectedHPAsForExport}
                    onToggleSelection={(hpa) => {
                      const hpaKey = `${hpa.cluster}-${hpa.namespace}-${hpa.name}`;
                      const newSet = new Set(selectedHPAsForExport);

                      if (newSet.has(hpaKey)) {
                        newSet.delete(hpaKey);
                        // Remover do array de objetos
                        setSelectedHPAObjects(prev => prev.filter(h => 
                          `${h.cluster}-${h.namespace}-${h.name}` !== hpaKey
                        ));
                      } else {
                        newSet.add(hpaKey);
                        // Adicionar ao array de objetos
                        setSelectedHPAObjects(prev => [...prev, hpa]);
                      }

                      setSelectedHPAsForExport(newSet);
                    }}
                  />
                </div>
              ),
            }}
          />
        );
      
      case "namespaces":
        return (
          <ErrorBoundary componentName="Namespaces Tab">
            <NamespacesTab
              cluster={selectedCluster}
              namespaces={namespaces}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              onNamespaceChange={setSelectedNamespace}
              onRefresh={refetchNamespaces}
            />
          </ErrorBoundary>
        );

      case "helm":
        return (
          <ErrorBoundary componentName="Helm Tab">
            <HelmTab selectedCluster={selectedCluster} />
          </ErrorBoundary>
        );

      case "events":
        return (
          <ErrorBoundary componentName="Events Tab">
            <EventsTab
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
              titleAction: (
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => refetchNodePools()}
                    disabled={!selectedCluster || nodePoolsLoading}
                    title="Atualizar lista de Node Pools"
                  >
                    <RefreshCcw className={`w-4 h-4 ${nodePoolsLoading ? 'animate-spin' : ''}`} />
                  </Button>
                  {markedNodePools.length === 2 && (
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
                  )}
                </div>
              ),
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
        return (
          <MonitoringPage
            preSelectedHPA={pendingHPANavigation}
            onHPASelected={() => {
              // Limpar navegação pendente após HPA ser selecionado
              console.log("[Index] HPA selecionado, limpando pendingHPANavigation");
              setPendingHPANavigation(null);
            }}
            onNavigateToHPA={async (cluster, namespace, hpaName) => {
              try {
                console.log("[NavigateToHPA] Iniciando navegação:", { cluster, namespace, hpaName });

                // 1. Toast informativo antes de iniciar
                toast.info(`Carregando ${hpaName}...`);

                // 2. Configurar navegação pendente PRIMEIRO (antes de qualquer mudança)
                console.log("[NavigateToHPA] Configurando navegação pendente");
                setPendingHPANavigation({ cluster, namespace, hpaName });

                // 3. Trocar para o cluster correto (se necessário)
                // Isso disparará o useHPAs automaticamente para buscar novos HPAs
                if (cluster !== selectedCluster) {
                  console.log("[NavigateToHPA] Trocando cluster de", selectedCluster, "para", cluster);
                  await handleClusterChange(cluster);
                  // Aguardar um pouco para o estado se estabilizar
                  await new Promise(resolve => setTimeout(resolve, 100));
                }

                // 4. Selecionar o namespace correto
                console.log("[NavigateToHPA] Selecionando namespace:", namespace);
                setSelectedNamespace(namespace);

                // 5. Trocar para a aba HPAs
                console.log("[NavigateToHPA] Trocando para aba HPAs");
                handleTabChange("hpas");

                // O useEffect processará quando os HPAs do novo cluster/namespace forem carregados
              } catch (error) {
                console.error("[NavigateToHPA] Error:", error);
                toast.error(`Erro ao navegar para o HPA: ${error instanceof Error ? error.message : "Erro desconhecido"}`);
                setPendingHPANavigation(null); // Limpar navegação pendente em caso de erro
              }
            }}
          />
        );

      case "servicemesh":
        return (
          <ServiceMeshGraph
            cluster={selectedCluster}
          />
        );

      case "ai-diagnostics":
        return (
          <ErrorBoundary>
            <AIDiagnosticsTab />
          </ErrorBoundary>
        );

      case "github-releases":
        return (
          <ErrorBoundary>
            <GitHubReleasesTab />
          </ErrorBoundary>
        );

      case "nexus-values":
        return (
          <ErrorBoundary>
            <NexusValuesDiffPanel />
          </ErrorBoundary>
        );

      case "dependencies":
        return (
          <ErrorBoundary componentName="Dependencies Tab">
            <DependenciesTab />
          </ErrorBoundary>
        );

      case "certificates":
        return (
          <ErrorBoundary componentName="Certificates Tab">
            <CertificatesTab selectedCluster={selectedCluster} />
          </ErrorBoundary>
        );

      case "daemonsets":
        return (
          <ErrorBoundary componentName="DaemonSets Tab">
            <DaemonSetsTab
              cluster={selectedCluster}
              namespaces={namespaces}
              selectedNamespace={selectedNamespace}
              onNamespaceChange={setSelectedNamespace}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
            />
          </ErrorBoundary>
        );

      case "statefulsets":
        return (
          <ErrorBoundary componentName="StatefulSets Tab">
            <StatefulSetsTab
              cluster={selectedCluster}
              namespaces={namespaces}
              selectedNamespace={selectedNamespace}
              onNamespaceChange={setSelectedNamespace}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
            />
          </ErrorBoundary>
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
        onLogout={onLogout || (() => console.log("Logout"))}
      />

      {/* Ocultar cards de estatísticas nas abas Monitoramento, Namespaces, ConfigMaps, Secrets, Deployments, DaemonSets, StatefulSets, Containers, Pods, CronJobs, Prometheus, Ingresses, Service Mesh, Health Checking, Helm, Nexus Values, AI Diagnostics, GitHub Releases, Dependencies e Certificados TLS */}
      {activeTab !== "monitoring" && activeTab !== "namespaces" && activeTab !== "configmaps" && activeTab !== "secrets" && activeTab !== "deployments" && activeTab !== "daemonsets" && activeTab !== "statefulsets" && activeTab !== "containers" && activeTab !== "pods" && activeTab !== "cronjobs" && activeTab !== "prometheus" && activeTab !== "ingresses" && activeTab !== "servicemesh" && activeTab !== "healthcheck" && activeTab !== "helm" && activeTab !== "nexus-values" && activeTab !== "ai-diagnostics" && activeTab !== "github-releases" && activeTab !== "dependencies" && activeTab !== "certificates" && (
        <div className="grid grid-cols-1 md:grid-cols-5 gap-4 px-6 py-3 flex-shrink-0">
          {/* Card de Cluster: mostra total na Dashboard, contexto+versão nas outras abas */}
          {activeTab === "dashboard" ? (
            <StatsCard
              icon={Layers}
              value={clustersLoading ? "..." : String(stats.clusters)}
              label="Clusters"
            />
          ) : (
            <ClusterContextCard cluster={selectedCluster} />
          )}
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
          {/* Card de Alertas Críticos */}
          {selectedCluster && (
            <CriticalAlertsBanner cluster={selectedCluster} />
          )}
        </div>
      )}

      {/* Banner de VPN desconectada */}
      {showVPNWarning && (
        <VPNWarningBanner onDismiss={() => setShowVPNWarning(false)} />
      )}

      {/* TabNavigation com WorkloadMenu */}
      <TabNavigation
        tabs={tabs}
        activeTab={activeTab}
        onTabChange={handleTabChange}
      >
        {/* Menu dropdown Workload */}
        <WorkloadMenu activeTab={activeTab} onTabChange={handleTabChange} />
        {/* Menu dropdown Tools */}
        <ToolsMenu activeTab={activeTab} onTabChange={handleTabChange} />
      </TabNavigation>

      {/* Conteúdo Principal */}
      <div className="flex-1 min-h-0 overflow-auto">
        {/* ✅ SOLUÇÃO: Renderizar componentes workload SEMPRE montados, mostrar apenas o ativo */}
        {/* Isso mantém o estado React naturalmente sem precisar persistir/restaurar */}

        {/* Pods - sempre montado */}
        <div style={{ display: activeTab === "pods" ? "block" : "none", height: "100%" }}>
          {(activeTab === "pods" || hasBeenMounted.current.pods) && (
            <ErrorBoundary componentName="Pods Panel">
              <PodsPanel
                cluster={selectedCluster}
                namespaces={namespaces}
                selectedNamespace={podsNamespace}
                onNamespaceChange={setPodsNamespace}
                showSystemNamespaces={showSystemNamespaces}
                onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* ConfigMaps - sempre montado */}
        <div style={{ display: activeTab === "configmaps" ? "block" : "none", height: "100%" }}>
          {(activeTab === "configmaps" || hasBeenMounted.current.configmaps) && (
            <ConfigMapsTab
              cluster={selectedCluster}
              namespaces={namespaces}
              selectedNamespace={configMapsNamespace}
              onNamespaceChange={setConfigMapsNamespace}
              showSystemNamespaces={showSystemNamespaces}
              onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
            />
          )}
        </div>

        {/* Deployments - sempre montado */}
        <div style={{ display: activeTab === "deployments" ? "block" : "none", height: "100%" }}>
          {(activeTab === "deployments" || hasBeenMounted.current.deployments) && (
            <ErrorBoundary componentName="Deployments Tab">
              <DeploymentsTab
                cluster={selectedCluster}
                namespaces={namespaces}
                selectedNamespace={deploymentsNamespace}
                onNamespaceChange={setDeploymentsNamespace}
                showSystemNamespaces={showSystemNamespaces}
                onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* Secrets - sempre montado */}
        <div style={{ display: activeTab === "secrets" ? "block" : "none", height: "100%" }}>
          {(activeTab === "secrets" || hasBeenMounted.current.secrets) && (
            <ErrorBoundary componentName="Secrets Tab">
              <SecretsTab
                cluster={selectedCluster}
                namespaces={namespaces}
                selectedNamespace={secretsNamespace}
                onNamespaceChange={setSecretsNamespace}
                showSystemNamespaces={showSystemNamespaces}
                onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* Containers - sempre montado */}
        <div style={{ display: activeTab === "containers" ? "block" : "none", height: "100%" }}>
          {(activeTab === "containers" || hasBeenMounted.current.containers) && (
            <ErrorBoundary componentName="Containers Tab">
              <ContainersTab
                cluster={selectedCluster}
                namespaces={namespaces}
                selectedNamespace={containersNamespace}
                onNamespaceChange={setContainersNamespace}
                showSystemNamespaces={showSystemNamespaces}
                onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* Ingresses - sempre montado */}
        <div style={{ display: activeTab === "ingresses" ? "block" : "none", height: "100%" }}>
          {(activeTab === "ingresses" || hasBeenMounted.current.ingresses) && (
            <ErrorBoundary componentName="Ingress Tab">
              <IngressTab
                cluster={selectedCluster}
                namespaces={namespaces}
                selectedNamespace={ingressNamespace}
                onNamespaceChange={setIngressNamespace}
                showSystemNamespaces={showSystemNamespaces}
                onToggleSystemNamespaces={() => setShowSystemNamespaces(!showSystemNamespaces)}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* Health Checking - sempre montado */}
        <div style={{ display: activeTab === "healthcheck" ? "block" : "none", height: "100%" }}>
          {(activeTab === "healthcheck" || hasBeenMounted.current.healthcheck) && (
            <ErrorBoundary componentName="Health Checking Tab">
              <HealthCheckingTab />
            </ErrorBoundary>
          )}
        </div>

        {/* Outras abas - renderização condicional normal (switch/case) */}
        {!["pods", "configmaps", "deployments", "secrets", "containers", "ingresses", "healthcheck"].includes(activeTab) && renderTabContent()}
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
        nodePools={sequencedNodePools.map(np => ({
          name: np.name,
          cluster: np.cluster_name,
          resourceGroup: np.resource_group || "",
          subscription: np.subscription || "",
          sequenceOrder: np.sequence_order || 0,
        }))}
        onConfirm={async (config) => {
          try {
            // Converter SequenceConfig para SequenceExecuteRequest
            const request = {
              cluster: selectedCluster,
              node_pools: sequencedNodePools.map(np => ({
                name: np.name,
                resource_group: np.resource_group || "",
                subscription: np.subscription || "",
                sequence_order: np.sequence_order || 0,
              })),
              cordon_enabled: config.cordonEnabled,
              drain_enabled: config.drainEnabled,
              drain_options: {
                ignore_daemonsets: config.drainOptions.ignoreDaemonsets,
                delete_emptydir_data: config.drainOptions.deleteEmptyDirData,
                force: config.drainOptions.force,
                grace_period: config.drainOptions.gracePeriod,
                timeout: config.drainOptions.timeout,
                disable_eviction: config.drainOptions.disableEviction,
                skip_wait_for_delete_timeout: config.drainOptions.skipWaitForDeleteTimeout,
                pod_selector: config.drainOptions.podSelector,
                dry_run: config.drainOptions.dryRun,
                chunk_size: config.drainOptions.chunkSize,
              },
            };

            // Chamar API para executar sequenciamento
            const response = await apiClient.executeNodePoolSequence(request);

            // Fechar modal de configuração
            setShowSequencingModal(false);

            // Extrair informações para o modal de progresso usando o estado sequencedNodePools
            const sessionId = response.data?.session_id;
            const origin = sequencedNodePools.find((np) => np.sequence_order === 1);
            const dest = sequencedNodePools.find((np) => np.sequence_order === 2);

            if (sessionId && origin && dest) {
              // Configurar estados do modal de progresso
              setProgressSessionId(sessionId);
              setProgressCluster(selectedCluster);
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
