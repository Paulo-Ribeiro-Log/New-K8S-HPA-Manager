import { useEffect, useMemo, useState, useRef, useCallback } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Search, RefreshCcw, Eye, EyeOff, PanelLeftClose, PanelLeftOpen, BarChart3, Package, Activity, X, MoreVertical, Trash2, FileText, Copy, Maximize2, Minimize2, Loader2, Plus, Undo2, Redo2, CheckCircle2, TriangleAlert, FileDiff, ChevronDown, ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { Namespace, TopNamespacesResponse, NamespaceManifest, DeploymentSummary } from "@/lib/api/types";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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

interface NamespacesTabProps {
  cluster: string;
  namespaces: Namespace[];
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onNamespaceChange: (namespace: string) => void;
  onRefresh: () => void;
}

export const NamespacesTab = ({
  cluster,
  namespaces,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onNamespaceChange,
  onRefresh,
}: NamespacesTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedNamespace, setSelectedNamespace] = useState<Namespace | null>(null);
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
  const [isCreating, setIsCreating] = useState(false);

  // Estados para deployments
  const [deployments, setDeployments] = useState<DeploymentSummary[]>([]);
  const [deploymentsLoading, setDeploymentsLoading] = useState(false);
  const [showDeployments, setShowDeployments] = useState(false);

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
      historyCache.current.set(cacheKey, {
        history: [...history],
        index: historyIndex,
      });
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

  const handleCopyYaml = () => {
    navigator.clipboard.writeText(editorValue);
    toast.success("YAML copiado para a área de transferência");
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
        }
      );
      toast.success("Namespace aplicado", {
        description: selectedNamespace.name,
      });
      
      // Recarregar manifest do servidor após aplicar
      await refreshManifest();
    } catch (err) {
      toast.error("Falha ao aplicar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
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

  const handleDescribe = async () => {
    if (!selectedNamespace || !cluster) return;

    setDescribeLoading(true);
    setDescribeModalOpen(true);
    try {
      const result = await apiClient.describeNamespace(cluster, selectedNamespace.name);
      setDescribeContent(result.describe);
    } catch (err) {
      toast.error("Erro ao buscar describe", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
      setDescribeContent("Error loading describe");
    } finally {
      setDescribeLoading(false);
    }
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

  const handleCreate = useCallback(async () => {
    if (!newNamespaceName.trim() || !cluster) return;

    setIsCreating(true);
    try {
      await apiClient.createNamespace(cluster, newNamespaceName.trim());
      toast.success(`Namespace ${newNamespaceName.trim()} criado com sucesso`);
      setCreateModalOpen(false);
      setNewNamespaceName("");
      onRefresh();
    } catch (err) {
      toast.error("Erro ao criar namespace", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCreating(false);
    }
  }, [newNamespaceName, cluster, onRefresh]);

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
    } else {
      setNamespaceManifest(null);
      setEditorValue("");
      setDeployments([]);
      setShowDeployments(false);
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

  const refreshNamespaces = () => {
    if (!cluster) return;
    onRefresh();
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
        {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
      </Button>
      <Button variant="outline" size="sm" onClick={refreshNamespaces} disabled={!cluster}>
        <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
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
      <div className="space-y-2">
        {searchedNamespaces.map((ns) => {
          const isSelected = selectedNamespace?.name === ns.name;
          return (
            <button
              key={`${ns.cluster}-${ns.name}`}
              onClick={() => handleSelectNamespace(ns)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="font-semibold text-sm">{ns.name}</div>
              <div className="text-[11px] text-muted-foreground mt-1 flex items-center gap-2">
                {ns.isSystem && (
                  <span className="px-1.5 py-0.5 bg-yellow-500/20 text-yellow-300 rounded text-[10px]">
                    Sistema
                  </span>
                )}
                <span>{ns.hpaCount} HPAs</span>
              </div>
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
      <div className="space-y-3">
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

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* CPU Chart */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Activity className="w-4 h-4 text-blue-500" />
                Top 5 Namespaces - CPU
              </CardTitle>
              <CardDescription className="text-xs">Requisição de CPU (millicores)</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer
                config={{
                  value: {
                    label: "CPU",
                    color: "hsl(var(--chart-1))",
                  },
                }}
                className="h-[140px]"
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
              <div className="mt-2 space-y-1">
                {metrics.top_cpu.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between">
                    <span className="font-medium">{m.namespace}</span>
                    <span className="text-muted-foreground">{m.cpu_request_millis} m</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Memory Chart */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <BarChart3 className="w-4 h-4 text-green-500" />
                Top 5 Namespaces - Memória
              </CardTitle>
              <CardDescription className="text-xs">Requisição de Memória (GB)</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer
                config={{
                  value: {
                    label: "Memória",
                    color: "hsl(var(--chart-2))",
                  },
                }}
                className="h-[140px]"
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
              <div className="mt-2 space-y-1">
                {metrics.top_memory.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between">
                    <span className="font-medium">{m.namespace}</span>
                    <span className="text-muted-foreground">{m.memory_request_gb.toFixed(2)} GB</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Pods Chart */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Package className="w-4 h-4 text-purple-500" />
                Top 5 Namespaces - Pods
              </CardTitle>
              <CardDescription className="text-xs">Quantidade de Pods</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer
                config={{
                  value: {
                    label: "Pods",
                    color: "hsl(var(--chart-3))",
                  },
                }}
                className="h-[140px]"
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
              <div className="mt-2 space-y-1">
                {metrics.top_pods.map((m, idx) => (
                  <div key={idx} className="text-xs flex justify-between">
                    <span className="font-medium">{m.namespace}</span>
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
              <ProtectedAction showWarning={false}>
                <DropdownMenuItem onClick={() => setDeleteConfirmOpen(true)} className="text-destructive focus:text-destructive">
                  <Trash2 className="w-4 h-4 mr-2" />
                  Deletar Namespace
                </DropdownMenuItem>
              </ProtectedAction>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

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
                onClick={handleDescribe}
                disabled={!selectedNamespace}
              >
                <FileText className="w-4 h-4 mr-1" />
                Describe
              </Button>
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
        </DialogContent>
      </Dialog>

      {/* Modal Create Namespace */}
      <Dialog open={createModalOpen} onOpenChange={setCreateModalOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Criar Novo Namespace</DialogTitle>
            <DialogDescription>
              Digite o nome do namespace que deseja criar no cluster <strong>{cluster}</strong>.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label htmlFor="namespace-name" className="text-sm font-medium">
                Nome do Namespace
              </label>
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

  if (isSidebarCollapsed) {
    return (
      <div className="p-4 h-full">
        <div className="grid grid-cols-1 h-full">
          <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
            <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
              <div className="flex items-center gap-3 flex-wrap">
                {collapseButton}
                <p className="text-base font-semibold text-primary">Visualização</p>
              </div>
              <ProtectedAction>
                <Button variant="outline" size="sm" onClick={() => setCreateModalOpen(true)}>
                  <Plus className="w-4 h-4 mr-1" />
                  Criar Namespace
                </Button>
              </ProtectedAction>
            </div>
            <div className="flex-1 overflow-auto min-h-0">
              {renderMetricsPanel()}
            </div>
          </div>
        </div>
        {renderGlobalModals()}
      </div>
    );
  }

  const rightTitleAction = (
    <ProtectedAction>
      <Button variant="outline" size="sm" onClick={() => setCreateModalOpen(true)}>
        <Plus className="w-4 h-4 mr-1" />
        Criar Namespace
      </Button>
    </ProtectedAction>
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
        
        const changes: Array<{ path: string; before: any; after: any }> = [];
        
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
              changes.push({
                path: currentPath,
                before: val1 === undefined ? '(não existe)' : typeof val1 === 'object' ? JSON.stringify(val1, null, 2) : String(val1),
                after: val2 === undefined ? '(removido)' : typeof val2 === 'object' ? JSON.stringify(val2, null, 2) : String(val2)
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

  return (
    <>
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

      {renderDiffDialog()}
      {renderApplyConfirmDialog()}
      {renderGlobalModals()}
    </>
  );
};
