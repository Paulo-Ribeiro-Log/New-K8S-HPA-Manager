import { useEffect, useMemo, useState, useRef, useCallback } from "react";
import { SplitView } from "@/components/SplitView";
import { useRevealOnKeyChange } from "@/hooks/useRevealOnKeyChange";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Search, RefreshCcw, Eye, EyeOff, PanelLeftClose, PanelLeftOpen, BarChart3, Package, Activity, X, MoreVertical, Trash2, FileText, Copy, Maximize2, Minimize2, Loader2, Plus, Undo2, Redo2, CheckCircle2, TriangleAlert, FileDiff, ChevronDown, ChevronRight, Network, Shield, AlertCircle, Info, AlertTriangle, Terminal, SplitSquareHorizontal, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { setHistoryCacheEntry } from "@/lib/historyCache";
import type { Namespace, TopNamespacesResponse, NamespaceManifest, DeploymentSummary, EventSummary, ResourceQuotaSummary, NetworkPolicySummary, ServiceSummary, PodsSummary, PodSummary } from "@/lib/api/types";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { PodTerminal } from "@/components/PodTerminal";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { PieChart, Pie, Cell, ResponsiveContainer, Legend } from "recharts";
import { createTwoFilesPatch } from "diff";
import { html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import * as yaml from "js-yaml";
import { ProtectedAction } from "@/components/rbac";
import { AWXCertModal } from "@/components/AWXCertModal";

interface NamespacesTabProps {
  cluster: string;
  namespaces: Namespace[];
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onNamespaceChange: (namespace: string) => void;
  onRefresh: () => Promise<void> | void;
  onOpenCompare?: (initial: { type: "namespace"; namespace: string; name: string }) => void;
}

export const NamespacesTab = ({
  cluster,
  namespaces,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onNamespaceChange,
  onRefresh,
  onOpenCompare,
}: NamespacesTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [selectedNamespace, setSelectedNamespace] = useState<Namespace | null>(null);
  const leftListRef = useRef<HTMLDivElement>(null);
  useRevealOnKeyChange(
    leftListRef,
    selectedNamespace ? `${selectedNamespace.cluster}-${selectedNamespace.name}` : null
  );
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const [metrics, setMetrics] = useState<TopNamespacesResponse | null>(null);
  const [metricsLoading, setMetricsLoading] = useState(false);
  const [namespaceManifest, setNamespaceManifest] = useState<NamespaceManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [newNamespaceName, setNewNamespaceName] = useState("");
  const [isSpotInstance, setIsSpotInstance] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [showAnnotationsEditor, setShowAnnotationsEditor] = useState(false);
  const [annotationsYaml, setAnnotationsYaml] = useState("");

  // Estados para deployments
  const [deployments, setDeployments] = useState<DeploymentSummary[]>([]);
  const [deploymentsLoading, setDeploymentsLoading] = useState(false);
  const [showDeployments, setShowDeployments] = useState(false);

  // Estados para observabilidade
  const [events, setEvents] = useState<EventSummary[]>([]);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [resourceQuotas, setResourceQuotas] = useState<ResourceQuotaSummary[]>([]);
  const [quotasLoading, setQuotasLoading] = useState(false);
  const [networkPolicies, setNetworkPolicies] = useState<NetworkPolicySummary[]>([]);
  const [policiesLoading, setPoliciesLoading] = useState(false);
  const [services, setServices] = useState<ServiceSummary[]>([]);
  const [servicesLoading, setServicesLoading] = useState(false);
  const [podsSummary, setPodsSummary] = useState<PodsSummary | null>(null);
  const [podsSummaryLoading, setPodsSummaryLoading] = useState(false);

  // Modais de observabilidade
  const [eventsModalOpen, setEventsModalOpen] = useState(false);
  const [quotasModalOpen, setQuotasModalOpen] = useState(false);
  const [policiesModalOpen, setPoliciesModalOpen] = useState(false);
  const [servicesModalOpen, setServicesModalOpen] = useState(false);

  // Estados para shell
  const [namespacePods, setNamespacePods] = useState<PodSummary[]>([]);
  const [podsListLoading, setPodsListLoading] = useState(false);
  const [selectedPodForShell, setSelectedPodForShell] = useState<PodSummary | null>(null);
  const [shellModalOpen, setShellModalOpen] = useState(false);
  const [selectedShellContainer, setSelectedShellContainer] = useState("");
  const [selectedShellType, setSelectedShellType] = useState("/bin/bash");
  const [useEphemeralDebug, setUseEphemeralDebug] = useState(false);
  const [useStandaloneDebugPod, setUseStandaloneDebugPod] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [terminalFullscreen, setTerminalFullscreen] = useState(false);
  const [creatingDebugPod, setCreatingDebugPod] = useState(false);
  const [debugPodName, setDebugPodName] = useState("");
  const [isStandalonePod, setIsStandalonePod] = useState(false);

  // AWX Integration
  const [awxConfigured, setAwxConfigured] = useState(false);
  const [awxCertOpen, setAwxCertOpen] = useState(false);

  // Estados de edição (copiado de ConfigMapsTab)
  const historyCache = useRef<Map<string, { history: string[], index: number }>>(new Map());
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [originalYaml, setOriginalYaml] = useState("");
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);

  // Error Dialog para exibir erros de apply de forma mais proeminente
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  // Computed values para edição
  const hasChanges = useMemo(() => editorValue !== originalYaml, [editorValue, originalYaml]);
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  const loadOverviewMetrics = async () => {
    if (!cluster) return;
    
    setMetricsLoading(true);
    try {
      const response = await apiClient.getNamespaceMetrics(cluster, 5);
      setMetrics(response);
    } catch (err) {
      toast.error("Erro ao carregar métricas do cluster", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setMetricsLoading(false);
    }
  };

  const loadManifest = async () => {
    if (!selectedNamespace || !cluster) return;

    // Salvar histórico antes de trocar
    if (selectedNamespace && history.length > 0) {
      const cacheKey = `${cluster}/${selectedNamespace.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...history], index: historyIndex });
    }

    setManifestLoading(true);
    try {
      const manifest = await apiClient.getNamespace(cluster, selectedNamespace.name);
      setNamespaceManifest(manifest);
      const yamlContent = manifest.yaml || "";
      setEditorValue(yamlContent);
      setOriginalYaml(yamlContent);
      setViewMode("editor");

      // Restaurar histórico do cache ou inicializar
      const cacheKey = `${cluster}/${selectedNamespace.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setHistory(cached.history);
        setHistoryIndex(cached.index);
      } else {
        setHistory([yamlContent]);
        setHistoryIndex(0);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto do namespace", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleReloadYaml = async () => {
    if (!selectedNamespace || !cluster) return;
    setManifestLoading(true);
    try {
      const manifest = await apiClient.getNamespace(cluster, selectedNamespace.name);
      const yamlContent = manifest.yaml || "";
      setNamespaceManifest(manifest);
      setEditorValue(yamlContent);
      setOriginalYaml(yamlContent);
      setViewMode("editor");
      setHistory([yamlContent]);
      setHistoryIndex(0);
      const cacheKey = `${cluster}/${selectedNamespace.name}`;
      historyCache.current.delete(cacheKey);
      toast.success("YAML recarregado do cluster");
    } catch (err) {
      toast.error("Erro ao recarregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // Helper para verificar se Istio está habilitado
  const isIstioEnabled = useMemo(() => {
    if (!namespaceManifest?.yaml) return false;
    try {
      const manifest = yaml.load(namespaceManifest.yaml) as any;
      return manifest?.metadata?.labels?.["istio-injection"] === "enabled";
    } catch {
      return false;
    }
  }, [namespaceManifest]);

  const loadDeployments = async () => {
    if (!selectedNamespace || !cluster) return;

    setDeploymentsLoading(true);
    try {
      const deploymentsList = await apiClient.getDeployments(
        cluster,
        [selectedNamespace.name],
        undefined,
        false,
        true
      );
      setDeployments(deploymentsList);
      setShowDeployments(false); // Inicialmente recolhido
    } catch (err) {
      toast.error("Erro ao carregar deployments", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
      setDeployments([]);
    } finally {
      setDeploymentsLoading(false);
    }
  };

  // Funções de carregamento para observabilidade
  const loadEvents = async () => {
    if (!selectedNamespace || !cluster) return;
    setEventsLoading(true);
    try {
      const data = await apiClient.getEvents(
        cluster,
        [selectedNamespace.name],
        undefined,
        undefined,
        false,
        true
      );
      setEvents(data.slice(0, 10));
    } catch (err) {
      console.error("Erro ao carregar events:", err);
      setEvents([]);
    } finally {
      setEventsLoading(false);
    }
  };

  const loadResourceQuotas = async () => {
    if (!selectedNamespace || !cluster) return;
    setQuotasLoading(true);
    try {
      const data = await apiClient.getResourceQuotas(cluster, [selectedNamespace.name]);
      setResourceQuotas(data);
    } catch (err) {
      console.error("Erro ao carregar resource quotas:", err);
      setResourceQuotas([]);
    } finally {
      setQuotasLoading(false);
    }
  };

  const loadNetworkPolicies = async () => {
    if (!selectedNamespace || !cluster) return;
    setPoliciesLoading(true);
    try {
      const data = await apiClient.getNetworkPolicies(cluster, [selectedNamespace.name]);
      setNetworkPolicies(data);
    } catch (err) {
      console.error("Erro ao carregar network policies:", err);
      setNetworkPolicies([]);
    } finally {
      setPoliciesLoading(false);
    }
  };

  const loadServices = async () => {
    if (!selectedNamespace || !cluster) return;
    setServicesLoading(true);
    try {
      const data = await apiClient.getServices(cluster, [selectedNamespace.name]);
      setServices(data);
    } catch (err) {
      console.error("Erro ao carregar services:", err);
      setServices([]);
    } finally {
      setServicesLoading(false);
    }
  };

  const loadPodsSummary = async () => {
    if (!selectedNamespace || !cluster) return;
    setPodsSummaryLoading(true);
    try {
      const data = await apiClient.getPodsSummary(cluster, selectedNamespace.name);
      setPodsSummary(data);
    } catch (err) {
      console.error("Erro ao carregar pods summary:", err);
      setPodsSummary(null);
    } finally {
      setPodsSummaryLoading(false);
    }
  };

  const loadNamespacePods = async () => {
    if (!selectedNamespace || !cluster) return;
    setPodsListLoading(true);
    try {
      const pods = await apiClient.getPods(cluster, [selectedNamespace.name]);
      setNamespacePods(pods);
    } catch (err) {
      console.error("Erro ao carregar pods:", err);
      setNamespacePods([]);
    } finally {
      setPodsListLoading(false);
    }
  };

  const handleCopyYaml = () => {
    navigator.clipboard.writeText(editorValue);
    toast.success("YAML copiado para a área de transferência");
  };

  // Helper para cor do status do pod
  const getPhaseColor = (phase: string) => {
    switch (phase) {
      case "Running":
        return "bg-green-500/20 text-green-300 border-green-500/30";
      case "Pending":
        return "bg-yellow-500/20 text-yellow-300 border-yellow-500/30";
      case "Succeeded":
        return "bg-blue-500/20 text-blue-300 border-blue-500/30";
      case "Failed":
        return "bg-red-500/20 text-red-300 border-red-500/30";
      default:
        return "bg-muted/20 text-muted-foreground border-muted/30";
    }
  };

  // Funções de edição (copiado de ConfigMapsTab)
  const handleEditorChange = (value: string | undefined) => {
    setEditorValue(value || "");
  };

  const addToHistory = useCallback(() => {
    const newHistory = history.slice(0, historyIndex + 1);
    newHistory.push(editorValue);
    if (newHistory.length > 50) {
      newHistory.shift();
    } else {
      setHistoryIndex((prev) => prev + 1);
    }
    setHistory(newHistory);
  }, [editorValue, history, historyIndex]);

  const handleUndo = () => {
    if (canUndo) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  };

  const handleRedo = () => {
    if (canRedo) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    setViewMode(mode);
  };

  const refreshManifest = async () => {
    if (!selectedNamespace || !cluster) return;
    try {
      const manifest = await apiClient.getNamespace(cluster, selectedNamespace.name);
      const yamlContent = manifest.yaml || "";
      setEditorValue(yamlContent);
      setOriginalYaml(yamlContent);
      setHistory([yamlContent]);
      setHistoryIndex(0);
      setNamespaceManifest(manifest);
      toast.success("Manifesto recarregado");
    } catch (err) {
      toast.error("Erro ao recarregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const handleShowDiffModal = async (fullScreen: boolean) => {
    if (!selectedNamespace || !hasChanges) return;
    
    setIsDiffLoading(true);
    setDiffModalOpen(true);
    setDiffFullScreen(fullScreen);

    try {
      const patch = createTwoFilesPatch(
        `${selectedNamespace.name} (original)`,
        `${selectedNamespace.name} (editado)`,
        originalYaml,
        editorValue,
        "",
        ""
      );

      const diffHtmlOutput = html(patch, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });

      setDiffHtml(diffHtmlOutput);
    } catch (err) {
      toast.error("Erro ao gerar diff", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleValidate = async () => {
    if (!selectedNamespace) return;
    setIsValidating(true);
    try {
      await apiClient.applyNamespace(
        cluster,
        selectedNamespace.name,
        {
          yaml: editorValue,
          fieldManager: "web-namespace-editor",
          dryRun: true,
        }
      );
      toast.success("Validação bem-sucedida (dry-run)", {
        description: "O YAML está válido e pode ser aplicado",
      });
    } catch (err) {
      toast.error("Falha na validação", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleCancel = () => {
    setEditorValue(originalYaml);
    setHistory([originalYaml]);
    setHistoryIndex(0);
    setViewMode("editor");
    toast.info("Alterações descartadas");
  };

  const handleApply = async () => {
    if (!selectedNamespace) return;
    setIsApplying(true);
    try {
      await apiClient.applyNamespace(
        cluster,
        selectedNamespace.name,
        {
          yaml: editorValue,
          fieldManager: "web-namespace-editor",
          dryRun: false,
          force: true,
        }
      );
      toast.success("Namespace aplicado", {
        description: selectedNamespace.name,
      });
      
      // Recarregar manifest do servidor após aplicar
      await refreshManifest();
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      const resourceFullName = `Namespace ${selectedNamespace.name}`;

      setErrorTitle(`Falha ao aplicar ${resourceFullName}`);
      setErrorMessage(errorMsg);
      setErrorDialogOpen(true);

      toast.error("Falha ao aplicar Namespace", { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  const openApplyConfirm = () => {
    if (!selectedNamespace) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para aplicar");
      return;
    }
    setApplyConfirmOpen(true);
  };

  const confirmApplyChanges = async () => {
    setApplyConfirmOpen(false);
    await handleApply();
  };

  const fetchDescribe = async () => {
    if (!selectedNamespace || !cluster) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeNamespace(cluster, selectedNamespace.name);
      setDescribeContent(result.describe);
    } catch (err) {
      toast.error("Erro ao buscar describe", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
      setDescribeContent("Error loading describe");
    } finally {
      setDescribeLoading(false);
    }
  };

  const handleViewDescribe = async () => {
    if (!selectedNamespace || !cluster) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const handleDelete = useCallback(async () => {
    if (!selectedNamespace || !cluster) return;

    setIsDeleting(true);
    try {
      await apiClient.deleteNamespace(cluster, selectedNamespace.name);
      toast.success(`Namespace ${selectedNamespace.name} deletado com sucesso`);
      setDeleteConfirmOpen(false);
      setSelectedNamespace(null);
      onNamespaceChange("");
      onRefresh();
    } catch (err) {
      toast.error("Erro ao deletar namespace", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDeleting(false);
    }
  }, [selectedNamespace, cluster, onNamespaceChange, onRefresh]);

  const getAnnotationsTemplate = useCallback((spotInstance: boolean): string => {
    const lines: string[] = [];
    if (spotInstance) {
      lines.push("# AVISO: A annotation de Spot Instance já é adicionada automaticamente.");
      lines.push("#");
    }
    lines.push("annotations:");
    lines.push("  # Exemplo: app.kubernetes.io/managed-by: terraform");
    lines.push("labels:");
    lines.push("  # Exemplo: environment: production");
    return lines.join("\n");
  }, []);

  const handleCreate = useCallback(async () => {
    if (!newNamespaceName.trim() || !cluster) return;

    // Parsear annotations e labels do editor YAML (se habilitado)
    let annotations: Record<string, string> | undefined;
    let labels: Record<string, string> | undefined;
    if (showAnnotationsEditor && annotationsYaml.trim()) {
      try {
        const parsed = yaml.load(annotationsYaml.trim());
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
          const obj = parsed as Record<string, unknown>;
          // Formato estruturado: { annotations: {}, labels: {} }
          if (obj.annotations || obj.labels) {
            if (obj.annotations && typeof obj.annotations === "object") {
              annotations = Object.fromEntries(
                Object.entries(obj.annotations as Record<string, unknown>)
                  .filter(([, v]) => v !== null && v !== undefined)
                  .map(([k, v]) => [k, String(v)])
              );
            }
            if (obj.labels && typeof obj.labels === "object") {
              labels = Object.fromEntries(
                Object.entries(obj.labels as Record<string, unknown>)
                  .filter(([, v]) => v !== null && v !== undefined)
                  .map(([k, v]) => [k, String(v)])
              );
            }
          } else {
            // Formato legado: mapa plano (apenas annotations)
            annotations = Object.fromEntries(
              Object.entries(obj)
                .filter(([, v]) => v !== null && v !== undefined)
                .map(([k, v]) => [k, String(v)])
            );
          }
          // Limpar mapas vazios
          if (annotations && Object.keys(annotations).length === 0) annotations = undefined;
          if (labels && Object.keys(labels).length === 0) labels = undefined;
        } else {
          toast.error("YAML inválido", { description: "O YAML deve conter as seções 'annotations' e/ou 'labels'" });
          return;
        }
      } catch {
        toast.error("YAML inválido");
        return;
      }
    }

    setIsCreating(true);
    try {
      await apiClient.createNamespace(cluster, newNamespaceName.trim(), isSpotInstance, annotations, labels);
      const spotMsg = isSpotInstance ? " (Spot Instance)" : "";
      toast.success(`Namespace ${newNamespaceName.trim()}${spotMsg} criado com sucesso`);
      setCreateModalOpen(false);
      setNewNamespaceName("");
      setIsSpotInstance(false);
      setShowAnnotationsEditor(false);
      setAnnotationsYaml("");
      onRefresh();
    } catch (err) {
      toast.error("Erro ao criar namespace", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCreating(false);
    }
  }, [newNamespaceName, cluster, isSpotInstance, showAnnotationsEditor, annotationsYaml, onRefresh, getAnnotationsTemplate]);

  // Verificar se AWX está configurado ao montar o componente
  useEffect(() => {
    apiClient.getAWXStatus()
      .then((s) => setAwxConfigured(s.configured && s.reachable))
      .catch(() => setAwxConfigured(false));
  }, []);

  useEffect(() => {
    setSelectedNamespace(null);
    setMetrics(null);
    setNamespaceManifest(null);
    setEditorValue("");

    // Carregar métricas de overview quando cluster muda
    if (cluster) {
      loadOverviewMetrics();
    }
  }, [cluster]);

  // Carregar manifest e deployments quando namespace é selecionado
  useEffect(() => {
    if (selectedNamespace && cluster) {
      loadManifest();
      loadDeployments();
      loadEvents();
      loadResourceQuotas();
      loadNetworkPolicies();
      loadServices();
      loadPodsSummary();
      loadNamespacePods();
    } else {
      setNamespaceManifest(null);
      setEditorValue("");
      setDeployments([]);
      setShowDeployments(false);
      setNamespacePods([]);
    }
  }, [selectedNamespace, cluster]);

  const searchedNamespaces = useMemo(() => {
    if (!searchQuery) return filteredNamespaces;
    const query = searchQuery.toLowerCase();
    return filteredNamespaces.filter((ns) => ns.name.toLowerCase().includes(query));
  }, [filteredNamespaces, searchQuery]);

  const handleSelectNamespace = (ns: Namespace) => {
    setSelectedNamespace(ns);
    onNamespaceChange(ns.name);
  };

  const refreshNamespaces = async () => {
    if (!cluster || isRefreshing) return;
    setIsRefreshing(true);
    try {
      await onRefresh();
    } finally {
      setIsRefreshing(false);
    }
  };

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Namespaces" : "Ocultar painel de Namespaces"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {selectedNamespace && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setSelectedNamespace(null);
            onNamespaceChange("");
          }}
          title="Desmarcar namespace e ver overview"
        >
          <X className="w-4 h-4 mr-1" />
          Desmarcar
        </Button>
      )}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Mostrar namespaces de sistema" : "Ocultar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={refreshNamespaces} disabled={!cluster || isRefreshing}>
        {isRefreshing
          ? <Loader2 className="w-4 h-4 mr-2 animate-spin" />
          : <RefreshCcw className="w-4 h-4 mr-2" />}
        Atualizar
      </Button>
      {collapseButton}
    </div>
  );

  const renderNamespaceList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Namespaces
        </div>
      );
    }

    if (searchedNamespaces.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {filteredNamespaces.length === 0
            ? "Nenhum Namespace encontrado"
            : "Nenhum Namespace corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2" ref={leftListRef}>
        {searchedNamespaces.map((ns) => {
          const isSelected = selectedNamespace?.name === ns.name;
          const itemKey = `${ns.cluster}-${ns.name}`;
          return (
            <button
              key={itemKey}
              data-item-key={itemKey}
              onClick={() => handleSelectNamespace(ns)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="font-semibold text-sm">{ns.name}</div>
              {ns.isSystem && (
                <div className="text-[11px] text-muted-foreground mt-1">
                  <span className="px-1.5 py-0.5 bg-yellow-500/20 text-yellow-300 rounded text-[10px]">
                    Sistema
                  </span>
                </div>
              )}
            </button>
          );
        })}
      </div>
    );
  };

  const renderMetricsPanel = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar métricas
        </div>
      );
    }

    // Mostrar overview apenas quando nenhum namespace está selecionado
    if (!selectedNamespace) {
      if (metricsLoading) {
        return (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            Carregando métricas...
          </div>
        );
      }

      if (!metrics) {
        return (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            Carregando overview do cluster...
          </div>
        );
      }

      // Renderizar gráficos de overview (Top 5)
      return renderOverviewCharts();
    }

    // Quando um namespace está selecionado, mostrar detalhes dele
    return renderNamespaceDetails();
  };

  const renderOverviewCharts = () => {
    if (!metrics) return null;

    // Preparar dados para os gráficos de pizza (Top 5 + Others)
    const cpuChartData = [
      ...metrics.top_cpu.map((m) => ({
        name: m.namespace,
        value: m.cpu_request_millis,
        percent: m.cpu_percent_of_cluster,
      })),
      {
        name: "Outros",
        value: metrics.cpu_others.cpu_request_millis,
        percent: metrics.cpu_others.cpu_percent_of_cluster,
      },
    ];

    const memoryChartData = [
      ...metrics.top_memory.map((m) => ({
        name: m.namespace,
        value: m.memory_request_gb,
        percent: m.memory_percent_of_cluster,
      })),
      {
        name: "Outros",
        value: metrics.memory_others.memory_request_gb,
        percent: metrics.memory_others.memory_percent_of_cluster,
      },
    ];

    const podsChartData = [
      ...metrics.top_pods.map((m) => ({
        name: m.namespace,
        value: m.pod_count,
        percent: m.pod_percent_of_cluster,
      })),
      {
        name: "Outros",
        value: metrics.pods_others.pod_count,
        percent: metrics.pods_others.pod_percent_of_cluster,
      },
    ];

    // Cores para os gráficos
    const COLORS = ["#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#6b7280"];

    return (
      <div className="space-y-3 w-full overflow-hidden">
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Total Namespaces</span>
            <span className="font-medium">{metrics.total_namespaces}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Exibindo</span>
            <span className="font-medium">Overview Top 5</span>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 w-full">
          {/* CPU Chart */}
          <Card className="min-w-0 w-full">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Activity className="w-4 h-4 text-blue-500" />
                Top 5 Namespaces - CPU
              </CardTitle>
              <CardDescription className="text-xs">Requisição de CPU (millicores)</CardDescription>
            </CardHeader>
            <CardContent className="overflow-hidden">
              <ChartContainer
                config={{
                  value: {
                    label: "CPU",
                    color: "hsl(var(--chart-1))",
                  },
                }}
                className="h-[200px] w-full max-w-full"
              >
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={cpuChartData}
                      cx="50%"
                      cy="50%"
                      labelLine={false}
                      label={({ percent }) => `${(percent).toFixed(1)}%`}
                      outerRadius={50}
                      fill="#8884d8"
                      dataKey="value"
                    >
                      {cpuChartData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <ChartTooltip content={<ChartTooltipContent />} />
                  </PieChart>
                </ResponsiveContainer>
              </ChartContainer>
              <div className="mt-3 space-y-1">
                {metrics.top_cpu.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between items-center">
                    <div className="flex items-center gap-2">
                      <div 
                        className="w-3 h-3 rounded-sm flex-shrink-0" 
                        style={{ backgroundColor: COLORS[idx % COLORS.length] }}
                      />
                      <span className="font-medium">{m.namespace}</span>
                    </div>
                    <span className="text-muted-foreground">{m.cpu_request_millis} m</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Memory Chart */}
          <Card className="min-w-0 w-full">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <BarChart3 className="w-4 h-4 text-green-500" />
                Top 5 Namespaces - Memória
              </CardTitle>
              <CardDescription className="text-xs">Requisição de Memória (GB)</CardDescription>
            </CardHeader>
            <CardContent className="overflow-hidden">
              <ChartContainer
                config={{
                  value: {
                    label: "Memória",
                    color: "hsl(var(--chart-2))",
                  },
                }}
                className="h-[200px] w-full max-w-full"
              >
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={memoryChartData}
                      cx="50%"
                      cy="50%"
                      labelLine={false}
                      label={({ percent }) => `${(percent).toFixed(1)}%`}
                      outerRadius={50}
                      fill="#82ca9d"
                      dataKey="value"
                    >
                      {memoryChartData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <ChartTooltip content={<ChartTooltipContent />} />
                  </PieChart>
                </ResponsiveContainer>
              </ChartContainer>
              <div className="mt-3 space-y-1">
                {metrics.top_memory.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between items-center">
                    <div className="flex items-center gap-2">
                      <div 
                        className="w-3 h-3 rounded-sm flex-shrink-0" 
                        style={{ backgroundColor: COLORS[idx % COLORS.length] }}
                      />
                      <span className="font-medium">{m.namespace}</span>
                    </div>
                    <span className="text-muted-foreground">{m.memory_request_gb.toFixed(2)} GB</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Pods Chart */}
          <Card className="min-w-0 w-full">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Package className="w-4 h-4 text-purple-500" />
                Top 5 Namespaces - Pods
              </CardTitle>
              <CardDescription className="text-xs">Quantidade de Pods</CardDescription>
            </CardHeader>
            <CardContent className="overflow-hidden">
              <ChartContainer
                config={{
                  value: {
                    label: "Pods",
                    color: "hsl(var(--chart-3))",
                  },
                }}
                className="h-[200px] w-full max-w-full"
              >
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={podsChartData}
                      cx="50%"
                      cy="50%"
                      labelLine={false}
                      label={({ percent }) => `${(percent).toFixed(1)}%`}
                      outerRadius={50}
                      fill="#ffc658"
                      dataKey="value"
                    >
                      {podsChartData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <ChartTooltip content={<ChartTooltipContent />} />
                  </PieChart>
                </ResponsiveContainer>
              </ChartContainer>
              <div className="mt-3 space-y-1">
                {metrics.top_pods.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between items-center">
                    <div className="flex items-center gap-2">
                      <div 
                        className="w-3 h-3 rounded-sm flex-shrink-0" 
                        style={{ backgroundColor: COLORS[idx % COLORS.length] }}
                      />
                      <span className="font-medium">{m.namespace}</span>
                    </div>
                    <span className="text-muted-foreground">{m.pod_count} pods</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    );
  };

  const renderNamespaceDetails = () => {
    if (!selectedNamespace) return null;

    return (
      <div className="space-y-3">
        <div className="flex items-start justify-between gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex items-start gap-4">
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Nome</span>
              <span className="font-medium">{selectedNamespace.name}</span>
            </div>
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
              <span className="font-medium">{cluster}</span>
            </div>
            {namespaceManifest && (
              <>
                <div className="flex flex-col">
                  <span className="text-muted-foreground uppercase mb-0.5">Status</span>
                  <span className="font-medium">{namespaceManifest.status}</span>
                </div>
                <div className="flex flex-col">
                  <span className="text-muted-foreground uppercase mb-0.5">Age</span>
                  <span className="font-medium">{namespaceManifest.age}</span>
                </div>
                {isIstioEnabled && (
                  <Badge variant="outline" className="h-fit px-2 py-1 bg-purple-500/10 text-purple-400 border-purple-500/30">
                    <Network className="w-3 h-3 mr-1" />
                    Istio Enabled
                  </Badge>
                )}
              </>
            )}
            {selectedNamespace.isSystem && (
              <span className="px-2 py-1 bg-yellow-500/20 text-yellow-300 rounded text-xs font-medium">
                Sistema
              </span>
            )}
            {deployments.length > 0 && (
              <div className="flex flex-col">
                <button
                  type="button"
                  onClick={() => setShowDeployments((prev) => !prev)}
                  className="flex items-center gap-1 text-muted-foreground uppercase mb-0.5 hover:text-foreground"
                >
                  {showDeployments ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                  <span>Deployments ({deployments.length})</span>
                </button>
                {showDeployments && (
                  <div className="flex flex-wrap gap-1 mt-1">
                    {deployments.map((dep) => (
                      <span
                        key={dep.name}
                        className="px-2 py-1 bg-secondary/60 rounded text-[10px] font-mono flex items-center gap-1"
                        title={`${dep.readyReplicas}/${dep.replicas} réplicas prontas`}
                      >
                        <Package className="w-3 h-3" />
                        {dep.name}
                        <span className="text-muted-foreground ml-0.5">
                          ({dep.readyReplicas}/{dep.replicas})
                        </span>
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm">
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem 
                onClick={() => {
                  if (namespacePods.length === 0) {
                    toast.info("Nenhum pod encontrado neste namespace");
                    return;
                  }
                  const runningPod = namespacePods.find(p => p.phase === "Running");
                  const podToUse = runningPod || namespacePods[0];
                  setSelectedPodForShell(podToUse);
                  setSelectedShellContainer(podToUse.containers[0]?.name || "");
                  setShellModalOpen(true);
                }}
                disabled={podsListLoading || namespacePods.length === 0}
              >
                <Terminal className="w-4 h-4 mr-2" />
                Shell
                {namespacePods.length > 0 && (
                  <Badge variant="secondary" className="text-xs ml-2">
                    {namespacePods.length}
                  </Badge>
                )}
              </DropdownMenuItem>
              <ProtectedAction showWarning={false}>
                <DropdownMenuItem onClick={() => setDeleteConfirmOpen(true)} className="text-destructive focus:text-destructive">
                  <Trash2 className="w-4 h-4 mr-2" />
                  Deletar Namespace
                </DropdownMenuItem>
              </ProtectedAction>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {/* Observabilidade - Pods Summary + Botões */}
        {selectedNamespace && (
          <div className="w-full space-y-3">
            {/* Pods Summary - Sempre Visível */}
            <Card className="border-primary/30">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm flex items-center gap-2">
                    <Package className="w-4 h-4 text-primary" />
                    Pods Status
                  </CardTitle>
                  {podsSummaryLoading && <Loader2 className="w-4 h-4 animate-spin" />}
                </div>
              </CardHeader>
              <CardContent>
                {podsSummary ? (
                  <div className="grid grid-cols-4 gap-3">
                    <div className="p-3 bg-green-500/10 border border-green-500/30 rounded-lg text-center">
                      <p className="text-3xl font-bold text-green-400">{podsSummary.running}</p>
                      <p className="text-xs text-muted-foreground mt-1">Running</p>
                    </div>
                    <div className="p-3 bg-yellow-500/10 border border-yellow-500/30 rounded-lg text-center">
                      <p className="text-3xl font-bold text-yellow-400">{podsSummary.pending}</p>
                      <p className="text-xs text-muted-foreground mt-1">Pending</p>
                    </div>
                    <div className="p-3 bg-red-500/10 border border-red-500/30 rounded-lg text-center">
                      <p className="text-3xl font-bold text-red-400">{podsSummary.failed}</p>
                      <p className="text-xs text-muted-foreground mt-1">Failed</p>
                    </div>
                    <div className="p-3 bg-blue-500/10 border border-blue-500/30 rounded-lg text-center">
                      <p className="text-3xl font-bold text-blue-400">{podsSummary.total}</p>
                      <p className="text-xs text-muted-foreground mt-1">Total</p>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center justify-center py-4">
                    <p className="text-xs text-muted-foreground">Carregando status dos pods...</p>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Botões para Modais de Observabilidade */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEventsModalOpen(true)}
                className="flex items-center justify-between gap-2"
              >
                <div className="flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4" />
                  <span>Events</span>
                </div>
                <Badge variant="secondary" className="text-xs">
                  {events.length}
                </Badge>
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={() => setQuotasModalOpen(true)}
                className="flex items-center justify-between gap-2"
              >
                <div className="flex items-center gap-2">
                  <BarChart3 className="w-4 h-4" />
                  <span>Quotas</span>
                </div>
                <Badge variant="secondary" className="text-xs">
                  {resourceQuotas.length}
                </Badge>
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={() => setPoliciesModalOpen(true)}
                className="flex items-center justify-between gap-2"
              >
                <div className="flex items-center gap-2">
                  <Shield className="w-4 h-4" />
                  <span>Policies</span>
                </div>
                <Badge variant="secondary" className="text-xs">
                  {networkPolicies.length}
                </Badge>
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={() => setServicesModalOpen(true)}
                className="flex items-center justify-between gap-2"
              >
                <div className="flex items-center gap-2">
                  <Network className="w-4 h-4" />
                  <span>Services</span>
                </div>
                <Badge variant="secondary" className="text-xs">
                  {services.length}
                </Badge>
              </Button>
            </div>
          </div>
        )}

        {manifestLoading ? (
          <div className="flex items-center justify-center h-64">
            <Loader2 className="w-6 h-6 animate-spin" />
          </div>
        ) : (
          <div className="space-y-3">
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium">Manifesto YAML</p>
                <div className="flex items-center gap-2">
                  {manifestLoading && (
                    <span className="text-xs text-muted-foreground">Carregando...</span>
                  )}
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={handleUndo}
                      disabled={!canUndo}
                      className={`px-2 py-1 text-xs font-medium ${
                        canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Desfazer (Ctrl+Z)"
                    >
                      <Undo2 className="w-3.5 h-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={handleRedo}
                      disabled={!canRedo}
                      className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                        canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Refazer (Ctrl+Y)"
                    >
                      <Redo2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={() => handleToggleView("editor")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      }`}
                    >
                      Editor
                    </button>
                    <button
                      type="button"
                      onClick={() => handleToggleView("diff")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                      disabled={!hasChanges}
                    >
                      Diff
                    </button>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setEditorFullScreen(true)}
                    title="Abrir editor em tela cheia"
                    disabled={!selectedNamespace}
                  >
                    <Maximize2 className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
              {viewMode === "editor" && (
                <MonacoYamlEditor
                  value={editorValue}
                  onChange={handleEditorChange}
                  height={450}
                />
              )}
              {viewMode === "diff" && (
                <MonacoYamlEditor
                  mode="diff"
                  originalValue={originalYaml}
                  value={editorValue}
                  height={450}
                  readOnly
                />
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleShowDiffModal(false)}
                disabled={!selectedNamespace || !hasChanges || isDiffLoading}
              >
                {isDiffLoading ? (
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                ) : (
                  <FileDiff className="w-4 h-4 mr-2" />
                )}
                Visualizar diff
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleShowDiffModal(true)}
                disabled={!selectedNamespace || !hasChanges || isDiffLoading}
                className="gap-2"
                title="Abrir diff ocupando toda a tela"
              >
                <Maximize2 className="w-4 h-4" />
                Tela cheia
              </Button>
              <ProtectedAction>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleValidate}
                  disabled={!selectedNamespace || isValidating}
                >
                  <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
                </Button>
              </ProtectedAction>
              <Button
                variant="outline"
                size="sm"
                onClick={handleCancel}
                disabled={!selectedNamespace || !hasChanges}
              >
                <X className="w-4 h-4 mr-2" /> Cancelar
              </Button>
              <ProtectedAction>
                <Button
                  variant="default"
                  size="sm"
                  onClick={openApplyConfirm}
                  disabled={!selectedNamespace || isApplying || !hasChanges}
                >
                  <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
                </Button>
              </ProtectedAction>
            </div>
          </div>
        )}

        {/* Modal Editor Full Screen */}
        <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
          <DialogContent 
            className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0"
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                setEditorFullScreen(false);
              }
            }}
          >
            <div className="h-full flex flex-col">
              <DialogHeader className="border-b border-border px-6 py-4">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <DialogTitle className="text-xl font-semibold text-primary">
                      Editor YAML - Tela Cheia
                    </DialogTitle>
                    <DialogDescription className="text-sm text-muted-foreground">
                      {selectedNamespace.name} • {cluster}
                    </DialogDescription>
                  </div>
                  <div className="flex items-center gap-2 flex-wrap">
                    <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                      <button
                        type="button"
                        onClick={handleUndo}
                        disabled={!canUndo}
                        className={`px-2 py-1 text-xs font-medium ${
                          canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                        }`}
                        title="Desfazer (Ctrl+Z)"
                      >
                        <Undo2 className="w-3.5 h-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={handleRedo}
                        disabled={!canRedo}
                        className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                          canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                        }`}
                        title="Refazer (Ctrl+Y)"
                      >
                        <Redo2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                    <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                      <button
                        type="button"
                        onClick={() => handleToggleView("editor")}
                        className={`px-3 py-1 text-xs font-medium ${
                          viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                        }`}
                      >
                        Editor
                      </button>
                      <button
                        type="button"
                        onClick={() => handleToggleView("diff")}
                        className={`px-3 py-1 text-xs font-medium ${
                          viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                        } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                        disabled={!hasChanges}
                      >
                        Diff
                      </button>
                    </div>
                    <ProtectedAction>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={handleValidate}
                        disabled={!selectedNamespace || isValidating}
                      >
                        {isValidating ? (
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                          <CheckCircle2 className="w-4 h-4 mr-2" />
                        )}
                        Dry-run
                      </Button>
                    </ProtectedAction>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleCancel}
                      title="Descartar alterações e sair (Esc)"
                    >
                      Cancelar
                    </Button>
                    <ProtectedAction>
                      <Button
                        variant="default"
                        size="sm"
                        onClick={() => {
                          openApplyConfirm();
                          setEditorFullScreen(false);
                        }}
                        disabled={!selectedNamespace || isApplying || !hasChanges}
                      >
                        <TriangleAlert className="w-4 h-4 mr-2" />
                        Aplicar
                      </Button>
                    </ProtectedAction>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditorFullScreen(false)}
                      title="Minimizar tela cheia (Esc)"
                    >
                      <Minimize2 className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </DialogHeader>
              <div className="flex-1 p-4">
                {viewMode === "editor" && (
                  <MonacoYamlEditor
                    value={editorValue}
                    onChange={handleEditorChange}
                    height="calc(100vh - 140px)"
                  />
                )}
                {viewMode === "diff" && (
                  <MonacoYamlEditor
                    mode="diff"
                    originalValue={originalYaml}
                    value={editorValue}
                    height="calc(100vh - 140px)"
                    readOnly
                  />
                )}
              </div>
            </div>
          </DialogContent>
        </Dialog>
      </div>
    );
  };

  const leftContent = (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        {searchQuery && (
          <button
            type="button"
            onClick={() => setSearchQuery("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            aria-label="Limpar busca"
          >
            ×
          </button>
        )}
        <Input
          placeholder="Buscar namespace..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderNamespaceList()}
    </div>
  );

  // Modais globais (sempre disponíveis)
  const renderGlobalModals = () => (
    <>
      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {selectedNamespace?.name}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              Namespace: {selectedNamespace?.name} • Cluster: {cluster}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="h-[70vh]">
            {describeLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono bg-muted p-4 rounded whitespace-pre-wrap">{describeContent}</pre>
            )}
          </ScrollArea>
          <DialogFooter className="gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleRefreshDescribe}
              disabled={describeLoading}
            >
              <RefreshCcw className={`w-3 h-3 mr-1 ${describeLoading ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigator.clipboard.writeText(describeContent)}
              disabled={!describeContent || describeContent === "Error loading describe"}
              className="text-xs"
            >
              <Copy className="w-3 h-3 mr-1" />
              Copiar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Create Namespace */}
      <Dialog open={createModalOpen} onOpenChange={(open) => {
        setCreateModalOpen(open);
        if (!open) {
          setNewNamespaceName("");
          setIsSpotInstance(false);
          setShowAnnotationsEditor(false);
          setAnnotationsYaml("");
        }
      }}>
        <DialogContent className={showAnnotationsEditor ? "max-w-xl" : "max-w-md"}>
          <DialogHeader>
            <DialogTitle>Criar Novo Namespace</DialogTitle>
            <DialogDescription>
              Digite o nome do namespace que deseja criar no cluster <strong>{cluster}</strong>.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="namespace-name">Nome do Namespace</Label>
              <Input
                id="namespace-name"
                value={newNamespaceName}
                onChange={(e) => setNewNamespaceName(e.target.value)}
                placeholder="meu-namespace"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && newNamespaceName.trim()) {
                    handleCreate();
                  }
                }}
              />
            </div>
            <div className="space-y-2">
              <Label>Tipo de Node</Label>
              <RadioGroup
                value={isSpotInstance ? "spot" : "normal"}
                onValueChange={(value) => setIsSpotInstance(value === "spot")}
                className="flex flex-col gap-2"
              >
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="normal" id="node-normal" />
                  <Label htmlFor="node-normal" className="cursor-pointer font-normal">
                    Normal
                  </Label>
                </div>
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="spot" id="node-spot" />
                  <Label htmlFor="node-spot" className="cursor-pointer font-normal">
                    Spot Instance
                  </Label>
                </div>
              </RadioGroup>
              {isSpotInstance && (
                <p className="text-xs text-muted-foreground mt-1">
                  Adiciona tolerations para pods rodarem em nodes Spot do Azure
                </p>
              )}
            </div>

            {/* Switch + Editor de Annotations e Labels */}
            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <Switch
                  id="annotations-switch"
                  checked={showAnnotationsEditor}
                  onCheckedChange={(checked) => {
                    setShowAnnotationsEditor(checked);
                    if (checked && !annotationsYaml.trim()) {
                      setAnnotationsYaml(getAnnotationsTemplate(isSpotInstance));
                    }
                  }}
                  disabled={isCreating}
                />
                <Label htmlFor="annotations-switch" className="cursor-pointer">
                  Adicionar Annotations e Labels
                </Label>
              </div>
              {showAnnotationsEditor && (
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">
                    Edite as seções <code className="font-mono">annotations</code> e <code className="font-mono">labels</code> conforme necessário. Linhas com <code className="font-mono">#</code> são comentários e serão ignoradas.
                  </p>
                  <div className="rounded border overflow-hidden">
                    <MonacoYamlEditor
                      value={annotationsYaml}
                      onChange={setAnnotationsYaml}
                      readOnly={isCreating}
                      height={200}
                    />
                  </div>
                </div>
              )}
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setCreateModalOpen(false)} disabled={isCreating}>
              Cancelar
            </Button>
            <ProtectedAction>
              <Button onClick={handleCreate} disabled={!newNamespaceName.trim() || isCreating}>
                {isCreating ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Plus className="w-4 h-4 mr-2" />}
                Criar
              </Button>
            </ProtectedAction>
          </div>
        </DialogContent>
      </Dialog>

      {/* Modal Delete Confirmation */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="text-destructive">Confirmar Deleção</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o namespace <strong>{selectedNamespace?.name}</strong>?
              <br />
              Esta ação não pode ser desfeita e irá remover todos os recursos dentro do namespace.
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2 pt-4">
            <Button variant="ghost" onClick={() => setDeleteConfirmOpen(false)}>
              Cancelar
            </Button>
            <ProtectedAction>
              <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
                {isDeleting ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Trash2 className="w-4 h-4 mr-2" />}
                Deletar
              </Button>
            </ProtectedAction>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );


  const rightTitleAction = (
    <div className="flex gap-2">
      {selectedNamespace && onOpenCompare && (
        <Button
          variant="ghost"
          size="sm"
          title="Abrir em Edição Lado a Lado"
          onClick={() => onOpenCompare({ type: "namespace", namespace: selectedNamespace.name, name: selectedNamespace.name })}
        >
          <SplitSquareHorizontal className="w-4 h-4" />
        </Button>
      )}
      {selectedNamespace && (
        <Button
          variant="outline"
          size="sm"
          onClick={handleReloadYaml}
          disabled={manifestLoading}
        >
          {manifestLoading ? (
            <Loader2 className="w-4 h-4 mr-1 animate-spin" />
          ) : (
            <RefreshCcw className="w-4 h-4 mr-1" />
          )}
          Recarregar YAML
        </Button>
      )}
      {selectedNamespace && (
        <Button
          variant="outline"
          size="sm"
          onClick={handleViewDescribe}
          disabled={describeLoading}
        >
          {describeLoading ? (
            <Loader2 className="w-4 h-4 mr-1 animate-spin" />
          ) : (
            <FileText className="w-4 h-4 mr-1" />
          )}
          Describe
        </Button>
      )}
      {selectedNamespace && awxConfigured && (
        <ProtectedAction showWarning={false}>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setAwxCertOpen(true)}
            title="Instalar ou renovar certificado TLS via AWX"
          >
            <ShieldCheck className="w-4 h-4 mr-1" />
            Certificado TLS
          </Button>
        </ProtectedAction>
      )}
      <ProtectedAction>
        <Button variant="outline" size="sm" onClick={() => setCreateModalOpen(true)}>
          <Plus className="w-4 h-4 mr-1" />
          Criar Namespace
        </Button>
      </ProtectedAction>
    </div>
  );

  // Render modais
  const renderDiffDialog = () => {
    if (!selectedNamespace) return null;

    const maxWidth = diffFullScreen ? "max-w-[95vw]" : "max-w-5xl";
    const maxHeight = diffFullScreen ? "max-h-[95vh]" : "max-h-[85vh]";

    return (
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className={`${maxWidth} ${maxHeight}`}>
          <DialogHeader>
            <DialogTitle>Comparação de Alterações (Diff)</DialogTitle>
            <DialogDescription>
              {selectedNamespace.name} • {cluster}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="h-[70vh] w-full">
            {isDiffLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            ) : (
              <div 
                className="diff-content"
                dangerouslySetInnerHTML={{ __html: diffHtml }}
              />
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    );
  };

  const renderApplyConfirmDialog = () => {
    if (!selectedNamespace) return null;

    // Gerar diff compacto apenas com mudanças
    const generateCompactDiff = () => {
      try {
        const originalObj = yaml.load(originalYaml) as any;
        const updatedObj = yaml.load(editorValue) as any;

        const changes: Array<{ path: string; before: string; after: string }> = [];

        // Função auxiliar para formatar valores de forma legível
        const formatValue = (val: any, isAfter: boolean = false): string => {
          if (val === undefined) return isAfter ? '(removido)' : '(não existe)';
          if (val === null) return 'null';
          if (val === '') return '""';  // String vazia - exibe aspas para indicar valor vazio
          if (typeof val === 'object') return JSON.stringify(val, null, 2);
          return String(val);
        };

        const compareObjects = (obj1: any, obj2: any, path: string = '') => {
          const allKeys = new Set([...Object.keys(obj1 || {}), ...Object.keys(obj2 || {})]);

          for (const key of allKeys) {
            const currentPath = path ? `${path}.${key}` : key;
            const val1 = obj1?.[key];
            const val2 = obj2?.[key];

            if (val1 === val2) continue;

            if (typeof val1 === 'object' && typeof val2 === 'object' && val1 !== null && val2 !== null && !Array.isArray(val1) && !Array.isArray(val2)) {
              compareObjects(val1, val2, currentPath);
            } else if (JSON.stringify(val1) !== JSON.stringify(val2)) {
              // Formatar o valor "depois" incluindo a chave quando é uma adição
              let afterDisplay = formatValue(val2, true);
              if (val1 === undefined && val2 !== undefined) {
                // Chave foi ADICIONADA - mostrar chave: valor
                const formattedVal = val2 === '' ? '""' :
                                     typeof val2 === 'object' ? JSON.stringify(val2, null, 2) :
                                     JSON.stringify(val2);
                afterDisplay = `${key}: ${formattedVal}`;
              }

              // Formatar o valor "antes" incluindo a chave quando é uma remoção
              let beforeDisplay = formatValue(val1, false);
              if (val2 === undefined && val1 !== undefined) {
                // Chave foi REMOVIDA - mostrar chave: valor
                const formattedVal = val1 === '' ? '""' :
                                     typeof val1 === 'object' ? JSON.stringify(val1, null, 2) :
                                     JSON.stringify(val1);
                beforeDisplay = `${key}: ${formattedVal}`;
              }

              changes.push({
                path: currentPath,
                before: beforeDisplay,
                after: afterDisplay
              });
            }
          }
        };

        compareObjects(originalObj, updatedObj);

        return changes;
      } catch {
        return [];
      }
    };

    const changes = generateCompactDiff();

    return (
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent className="max-w-4xl max-h-[90vh] bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">
              Confirmar aplicação
            </DialogTitle>
            <DialogDescription>
              Essa ação vai aplicar o Namespace diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {cluster}</p>
              <p><span className="text-muted-foreground">Namespace:</span> {selectedNamespace.name}</p>
            </div>
            
            {changes.length > 0 && (
              <div className="space-y-2">
                <p className="font-semibold text-sm">Mudanças detectadas ({changes.length}):</p>
                <div className="max-h-[400px] overflow-y-auto space-y-2 border rounded-lg p-3 bg-muted/10">
                  {changes.map((change, idx) => (
                    <div key={idx} className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">{change.path}</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-red-300">{change.before}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-green-300">{change.after}</pre>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
            
            <p className="text-muted-foreground">
              Esta operação não possui rollback automático. Confirme que as mudanças estão corretas.
            </p>
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Button
              variant="ghost"
              onClick={() => setApplyConfirmOpen(false)}
            >
              Cancelar
            </Button>
            <ProtectedAction>
              <Button
                variant="destructive"
                onClick={confirmApplyChanges}
                disabled={isApplying}
              >
                {isApplying ? (
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                ) : (
                  <TriangleAlert className="w-4 h-4 mr-2" />
                )}
                Confirmar
              </Button>
            </ProtectedAction>
          </div>
        </DialogContent>
      </Dialog>
    );
  };

  // Modais de Observabilidade
  const renderObservabilityModals = () => {
    if (!selectedNamespace) return null;

    return (
      <>
        {/* Modal Events */}
        <Dialog open={eventsModalOpen} onOpenChange={setEventsModalOpen}>
          <DialogContent className="max-w-4xl max-h-[85vh]">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <AlertTriangle className="w-5 h-5" />
                Events - {selectedNamespace.name}
              </DialogTitle>
              <DialogDescription>
                Eventos recentes do namespace • Cluster: {cluster}
              </DialogDescription>
            </DialogHeader>
            <ScrollArea className="h-[60vh]">
              {eventsLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-6 h-6 animate-spin" />
                </div>
              ) : events.length === 0 ? (
                <div className="flex items-center justify-center py-12">
                  <p className="text-muted-foreground">Nenhum evento recente</p>
                </div>
              ) : (
                <div className="space-y-2 p-2">
                  {events.map((event, idx) => (
                    <div
                      key={`${event.name}-${idx}`}
                      className={`p-4 rounded-lg border ${
                        event.type === "Warning"
                          ? "bg-destructive/5 border-destructive/30"
                          : "bg-blue-500/5 border-blue-500/30"
                      }`}
                    >
                      <div className="flex items-start gap-3">
                        {event.type === "Warning" ? (
                          <AlertTriangle className="w-5 h-5 text-destructive flex-shrink-0 mt-0.5" />
                        ) : (
                          <Info className="w-5 h-5 text-blue-400 flex-shrink-0 mt-0.5" />
                        )}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 mb-2">
                            <Badge
                              variant={event.type === "Warning" ? "destructive" : "default"}
                              className="text-xs"
                            >
                              {event.reason}
                            </Badge>
                            <span className="text-xs text-muted-foreground">{event.age}</span>
                          </div>
                          <p className="text-sm break-words">{event.message}</p>
                          {event.name && (
                            <p className="text-xs text-muted-foreground mt-2 font-mono">
                              Object: {event.name}
                            </p>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </DialogContent>
        </Dialog>

        {/* Modal Resource Quotas */}
        <Dialog open={quotasModalOpen} onOpenChange={setQuotasModalOpen}>
          <DialogContent className="max-w-4xl max-h-[85vh]">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <BarChart3 className="w-5 h-5" />
                Resource Quotas - {selectedNamespace.name}
              </DialogTitle>
              <DialogDescription>
                Limites de recursos configurados • Cluster: {cluster}
              </DialogDescription>
            </DialogHeader>
            <ScrollArea className="h-[60vh]">
              {quotasLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-6 h-6 animate-spin" />
                </div>
              ) : resourceQuotas.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 gap-2">
                  <AlertCircle className="w-12 h-12 text-muted-foreground" />
                  <p className="text-muted-foreground">Nenhuma quota configurada</p>
                  <p className="text-xs text-muted-foreground">
                    Este namespace não possui limites de recursos definidos
                  </p>
                </div>
              ) : (
                <div className="space-y-4 p-2">
                  {resourceQuotas.map((quota) => (
                    <Card key={quota.name}>
                      <CardHeader className="pb-3">
                        <CardTitle className="text-base font-mono">{quota.name}</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                          {quota.hard.map((limit) => {
                            const percent = limit.percent || 0;
                            const isHigh = percent > 80;
                            const isMedium = percent > 60 && percent <= 80;

                            return (
                              <div
                                key={limit.resource}
                                className={`p-3 rounded-lg border ${
                                  isHigh
                                    ? "bg-destructive/10 border-destructive/30"
                                    : isMedium
                                    ? "bg-yellow-500/10 border-yellow-500/30"
                                    : "bg-green-500/10 border-green-500/30"
                                }`}
                              >
                                <div className="flex justify-between items-start mb-2">
                                  <span className="text-sm font-medium">{limit.resource}</span>
                                  {limit.percent !== undefined && (
                                    <Badge
                                      variant={isHigh ? "destructive" : isMedium ? "default" : "secondary"}
                                      className="text-xs"
                                    >
                                      {limit.percent.toFixed(0)}%
                                    </Badge>
                                  )}
                                </div>
                                <div className="flex items-center justify-between text-xs">
                                  <span className="text-muted-foreground">Used:</span>
                                  <span className="font-mono font-semibold">{limit.used}</span>
                                </div>
                                <div className="flex items-center justify-between text-xs mt-1">
                                  <span className="text-muted-foreground">Limit:</span>
                                  <span className="font-mono">{limit.hard}</span>
                                </div>
                                {limit.percent !== undefined && (
                                  <div className="mt-2 w-full bg-secondary/30 rounded-full h-2">
                                    <div
                                      className={`h-2 rounded-full transition-all ${
                                        isHigh
                                          ? "bg-destructive"
                                          : isMedium
                                          ? "bg-yellow-500"
                                          : "bg-green-500"
                                      }`}
                                      style={{ width: `${Math.min(percent, 100)}%` }}
                                    />
                                  </div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              )}
            </ScrollArea>
          </DialogContent>
        </Dialog>

        {/* Modal Network Policies */}
        <Dialog open={policiesModalOpen} onOpenChange={setPoliciesModalOpen}>
          <DialogContent className="max-w-4xl max-h-[85vh]">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Shield className="w-5 h-5" />
                Network Policies - {selectedNamespace.name}
              </DialogTitle>
              <DialogDescription>
                Políticas de rede e segurança • Cluster: {cluster}
              </DialogDescription>
            </DialogHeader>
            <ScrollArea className="h-[60vh]">
              {policiesLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-6 h-6 animate-spin" />
                </div>
              ) : networkPolicies.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-12 gap-3">
                  <div className="p-4 bg-yellow-500/10 border border-yellow-500/30 rounded-lg">
                    <AlertCircle className="w-12 h-12 text-yellow-500 mx-auto mb-3" />
                    <p className="text-yellow-300 text-center font-medium mb-2">
                      Nenhuma Network Policy configurada
                    </p>
                    <p className="text-xs text-yellow-300/70 text-center max-w-md">
                      Todos os pods neste namespace podem se comunicar livremente com qualquer outro pod
                      ou serviço sem restrições de rede.
                    </p>
                  </div>
                </div>
              ) : (
                <div className="space-y-3 p-2">
                  {networkPolicies.map((policy) => (
                    <Card key={policy.name}>
                      <CardHeader className="pb-3">
                        <div className="flex items-center justify-between">
                          <CardTitle className="text-base font-mono">{policy.name}</CardTitle>
                          <Badge variant="outline" className="text-xs">
                            {policy.policyTypes.join(" + ")}
                          </Badge>
                        </div>
                      </CardHeader>
                      <CardContent className="space-y-3">
                        <div className="p-3 bg-secondary/30 rounded-lg">
                          <p className="text-xs text-muted-foreground mb-1">Pod Selector:</p>
                          <p className="text-sm font-mono">{policy.podSelector || "Todos os pods"}</p>
                        </div>
                        {policy.ingress && (
                          <div className="p-3 bg-green-500/10 border border-green-500/30 rounded-lg">
                            <p className="text-xs text-green-400 mb-1 font-semibold">
                              Ingress Rules:
                            </p>
                            <p className="text-sm">{policy.ingress}</p>
                          </div>
                        )}
                        {policy.egress && (
                          <div className="p-3 bg-blue-500/10 border border-blue-500/30 rounded-lg">
                            <p className="text-xs text-blue-400 mb-1 font-semibold">Egress Rules:</p>
                            <p className="text-sm">{policy.egress}</p>
                          </div>
                        )}
                      </CardContent>
                    </Card>
                  ))}
                </div>
              )}
            </ScrollArea>
          </DialogContent>
        </Dialog>

        {/* Modal Services */}
        <Dialog open={servicesModalOpen} onOpenChange={setServicesModalOpen}>
          <DialogContent className="max-w-4xl max-h-[85vh]">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Network className="w-5 h-5" />
                Services - {selectedNamespace.name}
              </DialogTitle>
              <DialogDescription>
                Serviços de rede expostos • Cluster: {cluster}
              </DialogDescription>
            </DialogHeader>
            <ScrollArea className="h-[60vh]">
              {servicesLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-6 h-6 animate-spin" />
                </div>
              ) : services.length === 0 ? (
                <div className="flex items-center justify-center py-12">
                  <p className="text-muted-foreground">Nenhum serviço configurado</p>
                </div>
              ) : (
                <div className="space-y-3 p-2">
                  {services.map((svc) => (
                    <Card key={svc.name}>
                      <CardHeader className="pb-3">
                        <div className="flex items-center justify-between">
                          <CardTitle className="text-base font-mono">{svc.name}</CardTitle>
                          <Badge
                            variant={
                              svc.type === "LoadBalancer"
                                ? "default"
                                : svc.type === "NodePort"
                                ? "secondary"
                                : "outline"
                            }
                            className="text-xs"
                          >
                            {svc.type}
                          </Badge>
                        </div>
                      </CardHeader>
                      <CardContent className="space-y-3">
                        <div className="grid grid-cols-2 gap-3">
                          <div className="p-3 bg-secondary/30 rounded-lg">
                            <p className="text-xs text-muted-foreground mb-1">Cluster IP:</p>
                            <p className="text-sm font-mono">{svc.clusterIP}</p>
                          </div>
                          <div className="p-3 bg-secondary/30 rounded-lg">
                            <p className="text-xs text-muted-foreground mb-1">Ports:</p>
                            <p className="text-sm font-mono">{svc.ports.join(", ")}</p>
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              )}
            </ScrollArea>
          </DialogContent>
        </Dialog>
      </>
    );
  };

  return (
    <>
      {/* Layout: painel único (colapsado) ou split view */}
      {isSidebarCollapsed ? (
        <div className="p-4 h-full">
          <div className="grid grid-cols-1 h-full">
            <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
              <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
                <div className="flex items-center gap-3 flex-wrap">
                  {collapseButton}
                  <p className="text-base font-semibold text-primary">Visualização</p>
                </div>
                {rightTitleAction}
              </div>
              <div className="flex-1 overflow-auto min-h-0">
                {renderMetricsPanel()}
              </div>
            </div>
          </div>
        </div>
      ) : (
        <SplitView
          leftPanel={{
            title: "Namespaces",
            titleAction: leftTitleAction,
            content: leftContent,
          }}
          rightPanel={{
            title: "Visualização",
            titleAction: rightTitleAction,
            content: renderMetricsPanel(),
          }}
        />
      )}

      {renderDiffDialog()}
      {renderApplyConfirmDialog()}
      {renderGlobalModals()}
      {renderObservabilityModals()}

      {/* Modal Shell Selection */}
      <Dialog open={shellModalOpen} onOpenChange={(open) => {
        setShellModalOpen(open);
        if (!open) {
          // Reset standalone flag ao fechar modal sem criar pod
          if (!terminalOpen) {
            setUseStandaloneDebugPod(false);
          }
        }
      }}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center gap-2">
              <Terminal className="w-5 h-5" />
              Conectar ao Shell
            </DialogTitle>
            <DialogDescription>
              Selecione o pod, container e tipo de shell para conectar
            </DialogDescription>
          </DialogHeader>
          
          <div className="flex-1 overflow-y-auto min-h-0">
            <div className="space-y-4 py-4 pr-2">
            {/* Seletor de Pod */}
            <div className="space-y-2">
              <Label>Pod ({namespacePods.length} disponíveis)</Label>
              <Select
                value={selectedPodForShell?.name || ""}
                onValueChange={(podName) => {
                  const pod = namespacePods.find(p => p.name === podName);
                  if (pod) {
                    setSelectedPodForShell(pod);
                    setSelectedShellContainer(pod.containers[0]?.name || "");
                  }
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Selecione um pod" />
                </SelectTrigger>
                <SelectContent>
                  {namespacePods.map((pod) => (
                    <SelectItem key={pod.name} value={pod.name}>
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className={getPhaseColor(pod.phase)}>
                          {pod.phase}
                        </Badge>
                        <span className="font-mono text-sm">{pod.name}</span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Seletor de Container */}
            {selectedPodForShell && selectedPodForShell.containers.length > 0 && (
              <div className="space-y-2">
                <Label>
                  Container
                  {selectedPodForShell.containers.length > 1 && (
                    <span className="text-muted-foreground ml-2">
                      ({selectedPodForShell.containers.length} containers)
                    </span>
                  )}
                </Label>
                <Select
                  value={selectedShellContainer}
                  onValueChange={setSelectedShellContainer}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {selectedPodForShell.containers.map((container) => (
                      <SelectItem key={container.name} value={container.name}>
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-sm">{container.name}</span>
                          {container.ready && (
                            <Badge variant="outline" className="bg-green-500/20 text-green-300 border-green-500/30">
                              Ready
                            </Badge>
                          )}
                        </div>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {/* Seletor de Shell Type */}
            <div className="space-y-2">
              <Label>Tipo de Shell</Label>
              <RadioGroup
                value={useStandaloneDebugPod ? "standalone" : (useEphemeralDebug ? "ephemeral" : selectedShellType)}
                onValueChange={(value) => {
                  if (value === "standalone") {
                    setUseStandaloneDebugPod(true);
                    setUseEphemeralDebug(false);
                    setSelectedShellType("/bin/bash");
                  } else if (value === "ephemeral") {
                    setUseEphemeralDebug(true);
                    setUseStandaloneDebugPod(false);
                    setSelectedShellType("/bin/bash");
                  } else {
                    setUseEphemeralDebug(false);
                    setUseStandaloneDebugPod(false);
                    setSelectedShellType(value);
                  }
                }}
              >
                <div className="flex items-start space-x-2 p-2 rounded hover:bg-secondary/50 transition-colors">
                  <RadioGroupItem value="/bin/bash" id="bash" className="mt-0.5" />
                  <Label htmlFor="bash" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium">/bin/bash</div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      Shell padrão • Disponível na maioria dos containers • Boa compatibilidade
                    </div>
                  </Label>
                </div>
                <div className="flex items-start space-x-2 p-2 rounded hover:bg-secondary/50 transition-colors">
                  <RadioGroupItem value="/bin/sh" id="sh" className="mt-0.5" />
                  <Label htmlFor="sh" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium">/bin/sh</div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      Shell mínimo • Presente em quase todos os containers • Alpine, busybox
                    </div>
                  </Label>
                </div>
                <div className="flex items-start space-x-2 p-2 rounded bg-green-500/10 border border-green-500/20 hover:bg-green-500/20 transition-colors">
                  <RadioGroupItem value="standalone" id="standalone" className="mt-0.5" />
                  <Label htmlFor="standalone" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium flex items-center gap-2">
                      <span>🚀 Debug Pod Standalone</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-green-500/20 text-green-600 dark:text-green-400 font-semibold">NOVO</span>
                    </div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      <span className="font-medium">nicolaka/netshoot</span> • Cria um pod dedicado de debug no namespace
                    </div>
                    <div className="text-xs text-muted-foreground mt-1">
                      Ideal para testes de rede, DNS e conectividade sem afetar pods existentes
                    </div>
                    <div className="text-xs mt-2 p-2 bg-green-500/10 border border-green-500/20 rounded space-y-1">
                      <div><span className="text-green-300">✓ Auto-limpeza:</span> Pod é removido automaticamente ao fechar o terminal</div>
                      <div><span className="text-green-300">✓ Independente:</span> Não afeta outros pods do namespace</div>
                    </div>
                  </Label>
                </div>
                <div className="flex items-start space-x-2 p-2 rounded bg-blue-500/10 border border-blue-500/20 hover:bg-blue-500/20 transition-colors">
                  <RadioGroupItem value="ephemeral" id="ephemeral" className="mt-0.5" />
                  <Label htmlFor="ephemeral" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium flex items-center gap-2">
                      <span>🛠️ Ephemeral Debug Container</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-600 dark:text-blue-400 font-semibold">RECOMENDADO</span>
                    </div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      <span className="font-medium">nicolaka/netshoot</span> • Container temporário com arsenal completo de debug
                    </div>
                    <div className="text-xs text-muted-foreground mt-1 flex flex-wrap gap-1">
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">tcpdump</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">curl</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">nslookup</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">dig</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">netstat</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">iperf</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">mtr</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">ethtool</code>
                    </div>
                    <div className="text-xs mt-2 p-2 bg-yellow-500/10 border border-yellow-500/20 rounded">
                      <span className="text-yellow-300">ℹ️ Nota:</span> Ephemeral containers persistem até o pod reiniciar. Containers existentes serão reutilizados automaticamente.
                    </div>
                  </Label>
                </div>
              </RadioGroup>
            </div>

            {/* Info sobre o pod */}
            {selectedPodForShell && (
              <div className="mt-4 p-3 bg-muted rounded-lg text-sm space-y-1">
                <div><span className="font-medium">Pod:</span> {selectedPodForShell.name}</div>
                <div><span className="font-medium">Namespace:</span> {selectedPodForShell.namespace}</div>
                <div><span className="font-medium">Status:</span> <Badge variant="outline" className={getPhaseColor(selectedPodForShell.phase)}>{selectedPodForShell.phase}</Badge></div>
              </div>
            )}
            </div>
          </div>

          <DialogFooter className="flex-shrink-0 pt-4">
            <Button
              variant="outline"
              onClick={() => setShellModalOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              onClick={async () => {
                if (useStandaloneDebugPod) {
                  // Criar debug pod standalone
                  setCreatingDebugPod(true);
                  try {
                    const podName = `debug-${selectedNamespace.name}-${Date.now()}`;
                    await apiClient.createDebugPod(cluster, selectedNamespace.name, podName);
                    setDebugPodName(podName);
                    setShellModalOpen(false);
                    
                    toast.info("Aguardando pod estar pronto...", { duration: 3000 });
                    
                    // Aguardar pod estar pronto com polling
                    let attempts = 0;
                    const maxAttempts = 30; // 30 segundos
                    let createdPod: PodSummary | undefined;
                    
                    while (attempts < maxAttempts) {
                      await new Promise(resolve => setTimeout(resolve, 1000));
                      await loadNamespacePods();
                      
                      // Precisamos aguardar a próxima atualização do estado
                      const pods = await apiClient.getPods(cluster, [selectedNamespace.name]);
                      createdPod = pods.find(p => p.name === podName);
                      
                      if (createdPod && createdPod.phase === "Running") {
                        break;
                      }
                      attempts++;
                    }
                    
                    if (createdPod && createdPod.phase === "Running") {
                      setSelectedPodForShell(createdPod);
                      setSelectedShellContainer("netshoot");
                      setIsStandalonePod(true);
                      setTerminalOpen(true);
                      toast.success("Debug pod pronto e conectado!");
                    } else {
                      toast.warning("Debug pod criado mas ainda não está Running. Verifique na aba Pods.");
                    }
                  } catch (err) {
                    toast.error("Erro ao criar debug pod", {
                      description: err instanceof Error ? err.message : "Erro desconhecido"
                    });
                  } finally {
                    setCreatingDebugPod(false);
                  }
                } else {
                  if (!selectedPodForShell || !selectedShellContainer) {
                    toast.error("Selecione um pod e container");
                    return;
                  }
                  setIsStandalonePod(false);
                  setShellModalOpen(false);
                  setTerminalOpen(true);
                }
              }}
              disabled={creatingDebugPod || (!useStandaloneDebugPod && (!selectedPodForShell || !selectedShellContainer))}
            >
              {creatingDebugPod ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <Terminal className="w-4 h-4 mr-2" />
              )}
              {creatingDebugPod ? "Criando..." : "Conectar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal do Terminal */}
      <Dialog open={terminalOpen} onOpenChange={async (open) => {
        if (!open && isStandalonePod && selectedPodForShell && selectedNamespace) {
          // Deletar pod standalone ao fechar
          try {
            await apiClient.deletePod(cluster, selectedNamespace.name, selectedPodForShell.name);
            toast.success("Debug pod removido automaticamente");
          } catch (err) {
            console.error("Erro ao remover debug pod:", err);
          }
          setIsStandalonePod(false);
          setSelectedPodForShell(null);
        }
        setTerminalOpen(open);
        if (!open) {
          setTerminalFullscreen(false);
        }
      }}>
        <DialogContent className={terminalFullscreen 
          ? "w-screen h-screen max-w-none max-h-none p-0 m-0 rounded-none"
          : "max-w-6xl h-[85vh] p-0"
        }>
          {selectedPodForShell && selectedShellContainer && selectedNamespace && (
            <PodTerminal
              cluster={cluster}
              namespace={selectedNamespace.name}
              pod={selectedPodForShell.name}
              container={selectedShellContainer}
              shell={selectedShellType}
              ephemeral={useEphemeralDebug}
              isFullscreen={terminalFullscreen}
              onToggleFullscreen={() => setTerminalFullscreen(!terminalFullscreen)}
              onClose={async () => {
                if (isStandalonePod) {
                  try {
                    await apiClient.deletePod(cluster, selectedNamespace.name, selectedPodForShell.name);
                    toast.success("Debug pod removido automaticamente");
                  } catch (err) {
                    console.error("Erro ao remover debug pod:", err);
                  }
                  setIsStandalonePod(false);
                  setSelectedPodForShell(null);
                }
                setTerminalOpen(false);
              }}
            />
          )}
        </DialogContent>
      </Dialog>

      {/* ── Modal de erro (destaque para analistas) ───────── */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh] bg-background border-destructive/50 border-2">
          <DialogHeader className="border-b border-destructive/30 pb-4">
            <div className="flex items-start gap-3">
              <div className="shrink-0 w-10 h-10 rounded-full bg-destructive/10 flex items-center justify-center">
                <AlertCircle className="w-6 h-6 text-destructive" />
              </div>
              <div className="flex-1 min-w-0">
                <DialogTitle className="text-destructive text-lg font-semibold">
                  {errorTitle}
                </DialogTitle>
                <DialogDescription className="text-muted-foreground text-sm mt-1">
                  Detalhes técnicos do erro abaixo. Verifique conflitos de field managers ou validação de YAML.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="space-y-4">
              {/* Mensagem de erro formatada */}
              <div className="bg-destructive/5 border border-destructive/20 rounded-lg p-4">
                <div className="flex items-center gap-2 mb-3">
                  <TriangleAlert className="w-4 h-4 text-destructive" />
                  <span className="text-sm font-semibold text-destructive">Erro Detalhado</span>
                </div>
                <pre className="text-xs font-mono text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">
                  {errorMessage}
                </pre>
              </div>

              {/* Dicas de resolução (se erro contiver palavras-chave conhecidas) */}
              {errorMessage.includes("conflicts with") && (
                <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-semibold text-blue-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Este erro indica conflito de <strong>field manager</strong> (Server-Side Apply).
                    O recurso foi previamente aplicado com <code className="bg-muted px-1 rounded">kubectl apply</code> (client-side)
                    e agora está sendo aplicado via SSA com field manager diferente.
                  </p>
                  <p className="text-xs text-muted-foreground mt-2 leading-relaxed">
                    <strong>Ação recomendada:</strong> O backend já utiliza <code className="bg-muted px-1 rounded">--force=true</code>.
                    Se o erro persistir, verifique se há anotações <code className="bg-muted px-1 rounded">kubectl.kubernetes.io/*</code>
                    que precisam ser removidas manualmente do YAML antes de aplicar.
                  </p>
                </div>
              )}
            </div>
          </ScrollArea>

          <DialogFooter className="border-t border-border/50 pt-4">
            <Button variant="outline" onClick={() => setErrorDialogOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* AWX Certificado TLS Modal */}
      <AWXCertModal
        open={awxCertOpen}
        onOpenChange={setAwxCertOpen}
        cluster={cluster}
        namespace={selectedNamespace?.name ?? ""}
      />

    </>
  );
};
