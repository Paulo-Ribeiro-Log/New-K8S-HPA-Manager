import { useEffect, useMemo, useState } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Search, RefreshCcw, Eye, EyeOff, Trash2, Terminal, ChevronDown, ChevronRight, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import type { Namespace, PodSummary } from "@/lib/api/types";
import { apiClient } from "@/lib/api/client";

interface PodsPanelProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const PodsPanel = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: PodsPanelProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [pods, setPods] = useState<PodSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedPod, setSelectedPod] = useState<PodSummary | null>(null);
  const [podYaml, setPodYaml] = useState("");
  const [yamlLoading, setYamlLoading] = useState(false);
  const [podLogs, setPodLogs] = useState<Record<string, string>>({});
  const [logsLoading, setLogsLoading] = useState<Record<string, boolean>>({});
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deletingPod, setDeletingPod] = useState<PodSummary | null>(null);
  const [expandedLabels, setExpandedLabels] = useState(false);

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) {
      onNamespaceChange("");
    }
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  const fetchPods = async () => {
    if (!cluster) return;

    setLoading(true);
    try {
      const namespaceFilter = selectedNamespace && selectedNamespace !== "__all__" ? [selectedNamespace] : undefined;
      const data = await apiClient.getPods(cluster, namespaceFilter, undefined, showSystemNamespaces, true);
      setPods(data);
    } catch (err) {
      toast.error("Erro ao carregar Pods", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchPods();
    // Auto-refresh a cada 30 segundos
    const interval = setInterval(fetchPods, 30000);
    return () => clearInterval(interval);
  }, [cluster, selectedNamespace, showSystemNamespaces]);

  useEffect(() => {
    setSelectedPod(null);
    setPodYaml("");
    setPodLogs({});
    setExpandedLabels(false);
  }, [cluster, selectedNamespace]);

  const filteredPods = useMemo(() => {
    if (!searchQuery) return pods;
    const query = searchQuery.toLowerCase();
    return pods.filter((pod) => {
      return (
        pod.name.toLowerCase().includes(query) ||
        pod.namespace.toLowerCase().includes(query) ||
        pod.nodeName?.toLowerCase().includes(query) ||
        pod.phase.toLowerCase().includes(query) ||
        pod.containers.some(c => c.name.toLowerCase().includes(query))
      );
    });
  }, [pods, searchQuery]);

  const handleSelectPod = async (pod: PodSummary) => {
    setSelectedPod(pod);
    setYamlLoading(true);
    setPodYaml("");
    setPodLogs({});
    setExpandedLabels(false);

    try {
      const manifest = await apiClient.getPod(pod.cluster, pod.namespace, pod.name);
      setPodYaml(manifest.yaml);
    } catch (err) {
      setPodYaml("Erro ao carregar manifest");
      toast.error("Erro ao carregar manifest do Pod");
    } finally {
      setYamlLoading(false);
    }
  };

  const handleLoadLogs = async (containerName: string) => {
    if (!selectedPod) return;

    setLogsLoading({ ...logsLoading, [containerName]: true });
    try {
      const result = await apiClient.getPodLogs(
        selectedPod.cluster,
        selectedPod.namespace,
        selectedPod.name,
        containerName,
        500
      );
      setPodLogs({ ...podLogs, [containerName]: result.logs });
    } catch (err) {
      setPodLogs({ ...podLogs, [containerName]: "Erro ao carregar logs" });
      toast.error(`Erro ao carregar logs do container ${containerName}`);
    } finally {
      setLogsLoading({ ...logsLoading, [containerName]: false });
    }
  };

  const handleDeletePod = async () => {
    if (!deletingPod) return;

    try {
      await apiClient.deletePod(deletingPod.cluster, deletingPod.namespace, deletingPod.name);
      toast.success(`Pod ${deletingPod.name} deletado com sucesso`);
      setDeleteConfirmOpen(false);
      setDeletingPod(null);
      if (selectedPod?.name === deletingPod.name && selectedPod?.namespace === deletingPod.namespace) {
        setSelectedPod(null);
      }
      fetchPods();
    } catch (err) {
      toast.error("Erro ao deletar Pod", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const getPhaseColor = (phase: string) => {
    switch (phase.toLowerCase()) {
      case "running":
        return "bg-green-500/10 text-green-700 dark:text-green-400 border-green-500/20";
      case "pending":
        return "bg-yellow-500/10 text-yellow-700 dark:text-yellow-400 border-yellow-500/20";
      case "succeeded":
        return "bg-blue-500/10 text-blue-700 dark:text-blue-400 border-blue-500/20";
      case "failed":
        return "bg-red-500/10 text-red-700 dark:text-red-400 border-red-500/20";
      case "crashloopbackoff":
        return "bg-red-600/10 text-red-800 dark:text-red-300 border-red-600/20";
      default:
        return "bg-slate-500/10 text-slate-700 dark:text-slate-400 border-slate-500/20";
    }
  };

  const getContainerStateColor = (state: string) => {
    switch (state.toLowerCase()) {
      case "running":
        return "text-green-600 dark:text-green-400";
      case "waiting":
        return "text-yellow-600 dark:text-yellow-400";
      case "terminated":
        return "text-red-600 dark:text-red-400";
      default:
        return "text-slate-600 dark:text-slate-400";
    }
  };

  const namespaceSelector = (
    <Select
      value={selectedNamespace || "__all__"}
      onValueChange={(value) => onNamespaceChange(value === "__all__" ? "" : value)}
      disabled={!cluster || filteredNamespaces.length === 0}
    >
      <SelectTrigger className="w-[140px]">
        <SelectValue />
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
  );

  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {namespaceSelector}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4 mr-2" /> : <EyeOff className="w-4 h-4 mr-2" />}Sistema
      </Button>
      <Button variant="outline" size="sm" onClick={fetchPods} disabled={!cluster || loading}>
        <RefreshCcw className={`w-4 h-4 mr-2 ${loading ? "animate-spin" : ""}`} /> Atualizar
      </Button>
    </div>
  );

  const rightTitleAction = selectedPod && (
    <Button
      variant="destructive"
      size="sm"
      onClick={() => {
        setDeletingPod(selectedPod);
        setDeleteConfirmOpen(true);
      }}
    >
      <Trash2 className="w-4 h-4 mr-2" />
      Deletar Pod
    </Button>
  );

  const renderPodList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Pods
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando Pods...
        </div>
      );
    }

    if (filteredPods.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {pods.length === 0
            ? "Nenhum Pod encontrado"
            : "Nenhum Pod corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredPods.map((pod) => {
          const isSelected =
            selectedPod?.name === pod.name &&
            selectedPod?.namespace === pod.namespace;
          const hasWarning = pod.restarts > 3;

          return (
            <button
              key={`${pod.cluster}-${pod.namespace}-${pod.name}`}
              onClick={() => handleSelectPod(pod)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="flex items-center gap-2 mb-1">
                <div className="font-semibold text-sm truncate flex-1">{pod.name}</div>
                <Badge variant="outline" className={`text-xs ${getPhaseColor(pod.phase)}`}>
                  {pod.phase}
                </Badge>
              </div>
              <div className="text-xs text-muted-foreground">{pod.namespace}</div>
              <div className="flex items-center gap-2 mt-1 text-[11px] text-muted-foreground">
                <span>{pod.readyContainers}/{pod.totalContainers} ready</span>
                {hasWarning && (
                  <Badge variant="outline" className="text-xs bg-orange-500/10 text-orange-700 dark:text-orange-400 border-orange-500/20">
                    <AlertCircle className="w-3 h-3 mr-1" />
                    {pod.restarts} restarts
                  </Badge>
                )}
              </div>
            </button>
          );
        })}
      </div>
    );
  };

  const renderPodDetails = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar Pods
        </div>
      );
    }

    if (!selectedPod) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um Pod para visualizar os detalhes
        </div>
      );
    }

    const createdAt = selectedPod.createdAt
      ? new Date(selectedPod.createdAt).toLocaleString()
      : "--";

    return (
      <div className="flex flex-col h-full">
        {/* Header */}
        <div className="border-b border-border p-4 space-y-3">
          <div>
            <h3 className="font-semibold text-lg">{selectedPod.name}</h3>
            <div className="flex items-center gap-2 mt-1">
              <span className="text-sm text-muted-foreground">Namespace:</span>
              <span className="text-sm font-mono">{selectedPod.namespace}</span>
            </div>
          </div>

          {/* Informações Gerais */}
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <span className="text-muted-foreground">Status:</span>
              <Badge variant="outline" className={`ml-2 ${getPhaseColor(selectedPod.phase)}`}>
                {selectedPod.phase}
              </Badge>
            </div>
            <div>
              <span className="text-muted-foreground">Node:</span>
              <span className="ml-2 font-mono text-xs">{selectedPod.nodeName || "N/A"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">IP:</span>
              <span className="ml-2 font-mono text-xs">{selectedPod.podIP || "N/A"}</span>
            </div>
            <div>
              <span className="text-muted-foreground">Criado em:</span>
              <span className="ml-2 text-xs">{createdAt}</span>
            </div>
          </div>

          {/* Containers */}
          <div>
            <div className="text-sm font-medium mb-2">Containers ({selectedPod.containers.length})</div>
            <div className="space-y-1">
              {selectedPod.containers.map((container) => (
                <div
                  key={container.name}
                  className="flex items-center gap-2 text-xs bg-muted/50 p-2 rounded"
                >
                  <span className={`font-medium ${getContainerStateColor(container.state)}`}>
                    {container.state}
                  </span>
                  <span className="font-mono flex-1">{container.name}</span>
                  {container.restartCount > 0 && (
                    <Badge variant="outline" className="text-xs">
                      {container.restartCount} restarts
                    </Badge>
                  )}
                  {container.stateReason && (
                    <span className="text-muted-foreground italic">
                      ({container.stateReason})
                    </span>
                  )}
                </div>
              ))}
            </div>
          </div>

          {/* Labels */}
          {selectedPod.labels && Object.keys(selectedPod.labels).length > 0 && (
            <div>
              <button
                onClick={() => setExpandedLabels(!expandedLabels)}
                className="flex items-center gap-1 text-sm font-medium hover:text-primary transition-colors"
              >
                {expandedLabels ? (
                  <ChevronDown className="w-4 h-4" />
                ) : (
                  <ChevronRight className="w-4 h-4" />
                )}
                Labels ({Object.keys(selectedPod.labels).length})
              </button>
              {expandedLabels && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {Object.entries(selectedPod.labels).map(([key, value]) => (
                    <Badge
                      key={key}
                      variant="outline"
                      className="text-xs font-mono bg-blue-50 dark:bg-blue-950/30 text-blue-700 dark:text-blue-400"
                    >
                      {key}={value}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Tabs: YAML + Logs */}
        <Tabs defaultValue="yaml" className="flex-1 flex flex-col">
          <TabsList className="mx-4 mt-2">
            <TabsTrigger value="yaml">YAML Manifest</TabsTrigger>
            <TabsTrigger value="logs">
              <Terminal className="w-4 h-4 mr-2" />
              Logs
            </TabsTrigger>
          </TabsList>

          <TabsContent value="yaml" className="flex-1 m-4 mt-2">
            <ScrollArea className="h-full border rounded-lg bg-muted/50">
              <pre className="p-4 text-xs font-mono">
                <code>{yamlLoading ? "Carregando..." : podYaml}</code>
              </pre>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="logs" className="flex-1 m-4 mt-2 overflow-y-auto">
            <div className="space-y-4">
              {selectedPod.containers.map((container) => (
                <div key={container.name} className="space-y-2">
                  <div className="flex items-center justify-between">
                    <h4 className="font-medium text-sm">Container: {container.name}</h4>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleLoadLogs(container.name)}
                      disabled={logsLoading[container.name]}
                    >
                      {logsLoading[container.name] ? "Carregando..." : "Carregar Logs"}
                    </Button>
                  </div>
                  {podLogs[container.name] && (
                    <ScrollArea className="h-[250px] border rounded-lg bg-muted/50">
                      <pre className="p-4 text-xs font-mono">
                        <code>{podLogs[container.name]}</code>
                      </pre>
                    </ScrollArea>
                  )}
                </div>
              ))}
            </div>
          </TabsContent>
        </Tabs>
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
          placeholder="Buscar por nome, namespace, node..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderPodList()}
    </div>
  );

  return (
    <>
      <SplitView
        leftPanel={{
          title: `Pods (${filteredPods.length})`,
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedPod ? selectedPod.name : "Detalhes do Pod",
          titleAction: rightTitleAction,
          content: renderPodDetails(),
        }}
      />

      {/* Modal de Confirmação de Deleção */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Deleção</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o pod <strong>{deletingPod?.name}</strong>?
              <br />
              Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDeletePod}>
              Deletar Pod
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
