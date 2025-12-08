import { useEffect, useMemo, useState } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Search, RefreshCcw, Eye, EyeOff, PanelLeftClose, PanelLeftOpen, BarChart3, Package, Activity, X, MoreVertical, Trash2, FileText, Copy, Maximize2, Minimize2, Loader2, Plus } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { Namespace, TopNamespacesResponse, NamespaceManifest } from "@/lib/api/types";
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

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

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

    setManifestLoading(true);
    try {
      const manifest = await apiClient.getNamespace(cluster, selectedNamespace.name);
      setNamespaceManifest(manifest);
      setEditorValue(manifest.yaml || "");
    } catch (err) {
      toast.error("Erro ao carregar manifesto do namespace", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleCopyYaml = () => {
    navigator.clipboard.writeText(editorValue);
    toast.success("YAML copiado para a área de transferência");
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

  const handleDelete = async () => {
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
  };

  const handleCreate = async () => {
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
  };

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

  // Carregar manifest quando namespace é selecionado
  useEffect(() => {
    if (selectedNamespace && cluster) {
      loadManifest();
    } else {
      setNamespaceManifest(null);
      setEditorValue("");
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
          </div>
          
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm">
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setDeleteConfirmOpen(true)} className="text-destructive focus:text-destructive">
                <Trash2 className="w-4 h-4 mr-2" />
                Deletar Namespace
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {manifestLoading ? (
          <div className="flex items-center justify-center h-64">
            <Loader2 className="w-6 h-6 animate-spin" />
          </div>
        ) : (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Manifesto YAML</p>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleDescribe}>
                  <FileText className="w-4 h-4 mr-1" />
                  Describe
                </Button>
                <Button variant="outline" size="sm" onClick={handleCopyYaml}>
                  <Copy className="w-4 h-4 mr-1" />
                  Copiar YAML
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditorFullScreen(true)}
                  title="Expandir editor"
                >
                  <Maximize2 className="w-4 h-4" />
                </Button>
              </div>
            </div>
            <MonacoYamlEditor
              value={editorValue}
              onChange={setEditorValue}
              height={450}
              readOnly={false}
            />
          </div>
        )}

        {/* Modal Describe */}
        <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
          <DialogContent className="max-w-6xl max-h-[90vh]">
            <DialogHeader>
              <DialogTitle>Kubectl Describe - {selectedNamespace.name}</DialogTitle>
              <DialogDescription className="text-sm text-muted-foreground">
                Namespace: {selectedNamespace.name} • Cluster: {cluster}
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
              <Button onClick={handleCreate} disabled={!newNamespaceName.trim() || isCreating}>
                {isCreating ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Plus className="w-4 h-4 mr-2" />}
                Criar
              </Button>
            </div>
          </DialogContent>
        </Dialog>

        {/* Modal Delete Confirmation */}
        <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="text-destructive">Confirmar Deleção</DialogTitle>
              <DialogDescription>
                Tem certeza que deseja deletar o namespace <strong>{selectedNamespace.name}</strong>?
                <br />
                Esta ação não pode ser desfeita e irá remover todos os recursos dentro do namespace.
              </DialogDescription>
            </DialogHeader>
            <div className="flex justify-end gap-2 pt-4">
              <Button variant="ghost" onClick={() => setDeleteConfirmOpen(false)}>
                Cancelar
              </Button>
              <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
                {isDeleting ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Trash2 className="w-4 h-4 mr-2" />}
                Deletar
              </Button>
            </div>
          </DialogContent>
        </Dialog>

        {/* Modal Editor Full Screen */}
        <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
          <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0">
            <div className="h-full flex flex-col">
              <DialogHeader className="border-b border-border px-6 py-4 pr-16">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <DialogTitle className="text-xl font-semibold text-primary">
                      Editor YAML - Tela Cheia
                    </DialogTitle>
                    <DialogDescription className="text-sm text-muted-foreground">
                      {selectedNamespace.name} • {cluster}
                    </DialogDescription>
                  </div>
                  <Button variant="outline" size="sm" onClick={handleCopyYaml} className="mr-8">
                    <Copy className="w-4 h-4 mr-1" />
                    Copiar YAML
                  </Button>
                </div>
              </DialogHeader>
              <div className="flex-1 p-4">
                <MonacoYamlEditor
                  value={editorValue}
                  onChange={setEditorValue}
                  height="calc(100vh - 140px)"
                  readOnly={false}
                />
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
              <Button variant="outline" size="sm" onClick={() => setCreateModalOpen(true)}>
                <Plus className="w-4 h-4 mr-1" />
                Criar Namespace
              </Button>
            </div>
            <div className="flex-1 overflow-auto min-h-0">
              {renderMetricsPanel()}
            </div>
          </div>
        </div>
      </div>
    );
  }

  const rightTitleAction = (
    <Button variant="outline" size="sm" onClick={() => setCreateModalOpen(true)}>
      <Plus className="w-4 h-4 mr-1" />
      Criar Namespace
    </Button>
  );

  return (
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
  );
};
