import { useEffect, useMemo, useState } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Search, RefreshCcw, Eye, EyeOff, Trash2, FileText, Terminal, ChevronDown, ChevronRight, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card } from "@/components/ui/card";

import type { Namespace, PodSummary, ContainerStatus } from "@/lib/api/types";
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
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [podYaml, setPodYaml] = useState("");
  const [podLogs, setPodLogs] = useState<Record<string, string>>({});
  const [logsLoading, setLogsLoading] = useState<Record<string, boolean>>({});
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deletingPod, setDeletingPod] = useState<PodSummary | null>(null);
  const [expandedLabels, setExpandedLabels] = useState<Set<string>>(new Set());

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

  const handleViewDetails = async (pod: PodSummary) => {
    setSelectedPod(pod);
    setDetailsOpen(true);
    setPodYaml("Carregando...");
    setPodLogs({});

    try {
      const manifest = await apiClient.getPod(pod.cluster, pod.namespace, pod.name);
      setPodYaml(manifest.yaml);
    } catch (err) {
      setPodYaml("Erro ao carregar manifest");
      toast.error("Erro ao carregar manifest do Pod");
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

  const toggleLabels = (podKey: string) => {
    const newExpanded = new Set(expandedLabels);
    if (newExpanded.has(podKey)) {
      newExpanded.delete(podKey);
    } else {
      newExpanded.add(podKey);
    }
    setExpandedLabels(newExpanded);
  };

  return (
    <div className="h-full flex flex-col bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800">
      {/* Header com Filtros */}
      <div className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border-b border-slate-200/60 dark:border-slate-700/60 shadow-sm">
        <div className="p-4 space-y-3">
          <div className="flex items-center gap-3 flex-wrap">
            {/* Select Namespace */}
            <Select value={selectedNamespace || "__all__"} onValueChange={(value) => onNamespaceChange(value === "__all__" ? "" : value)}>
              <SelectTrigger className="w-64 bg-white dark:bg-slate-900">
                <SelectValue placeholder="Selecione um namespace" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">Todos os namespaces</SelectItem>
                {filteredNamespaces.map((ns) => (
                  <SelectItem key={ns.name} value={ns.name}>
                    {ns.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Search */}
            <div className="flex-1 min-w-64 relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
              <Input
                placeholder="Buscar por nome, namespace, node, container..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-10 bg-white dark:bg-slate-900"
              />
            </div>

            {/* Botões */}
            <Button
              variant="outline"
              size="sm"
              onClick={onToggleSystemNamespaces}
              className="gap-2"
            >
              {showSystemNamespaces ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              {showSystemNamespaces ? "Ocultar Sistema" : "Mostrar Sistema"}
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={fetchPods}
              disabled={loading}
              className="gap-2"
            >
              <RefreshCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
          </div>

          {/* Contador */}
          <div className="text-sm text-slate-600 dark:text-slate-400">
            {loading ? "Carregando..." : `${filteredPods.length} pod(s) encontrado(s)`}
          </div>
        </div>
      </div>

      {/* Lista de Pods */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          {filteredPods.map((pod) => {
            const podKey = `${pod.namespace}/${pod.name}`;
            const isExpanded = expandedLabels.has(podKey);
            const hasWarning = pod.restarts > 3;

            return (
              <Card
                key={podKey}
                className="p-4 bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm border border-slate-200/60 dark:border-slate-700/60 hover:shadow-lg transition-all duration-200"
              >
                <div className="space-y-3">
                  {/* Header do Pod */}
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-2">
                        <h3 className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-100 truncate">
                          {pod.name}
                        </h3>
                        <Badge variant="outline" className={getPhaseColor(pod.phase)}>
                          {pod.phase}
                        </Badge>
                        {hasWarning && (
                          <Badge variant="outline" className="bg-orange-500/10 text-orange-700 dark:text-orange-400 border-orange-500/20">
                            <AlertCircle className="w-3 h-3 mr-1" />
                            {pod.restarts} restarts
                          </Badge>
                        )}
                      </div>

                      <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-xs text-slate-600 dark:text-slate-400">
                        <div>
                          <span className="font-medium">Namespace:</span> {pod.namespace}
                        </div>
                        <div>
                          <span className="font-medium">Node:</span> {pod.nodeName || "N/A"}
                        </div>
                        <div>
                          <span className="font-medium">IP:</span> {pod.podIP || "N/A"}
                        </div>
                        <div>
                          <span className="font-medium">Containers:</span> {pod.readyContainers}/{pod.totalContainers}
                        </div>
                      </div>

                      {/* Containers */}
                      <div className="mt-3 space-y-1">
                        {pod.containers.map((container) => (
                          <div
                            key={container.name}
                            className="flex items-center gap-2 text-xs bg-slate-50 dark:bg-slate-900/50 p-2 rounded"
                          >
                            <span className={`font-medium ${getContainerStateColor(container.state)}`}>
                              {container.state}
                            </span>
                            <span className="font-mono">{container.name}</span>
                            {container.restartCount > 0 && (
                              <Badge variant="outline" className="text-xs">
                                {container.restartCount} restarts
                              </Badge>
                            )}
                            {container.stateReason && (
                              <span className="text-slate-500 dark:text-slate-500 italic">
                                ({container.stateReason})
                              </span>
                            )}
                          </div>
                        ))}
                      </div>

                      {/* Labels (colapsável) */}
                      {pod.labels && Object.keys(pod.labels).length > 0 && (
                        <div className="mt-3">
                          <button
                            onClick={() => toggleLabels(podKey)}
                            className="flex items-center gap-1 text-xs text-slate-600 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 transition-colors"
                          >
                            {isExpanded ? (
                              <ChevronDown className="w-3 h-3" />
                            ) : (
                              <ChevronRight className="w-3 h-3" />
                            )}
                            Labels ({Object.keys(pod.labels).length})
                          </button>
                          {isExpanded && (
                            <div className="mt-2 flex flex-wrap gap-1">
                              {Object.entries(pod.labels).map(([key, value]) => (
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

                    {/* Ações */}
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleViewDetails(pod)}
                        className="gap-2"
                      >
                        <FileText className="w-4 h-4" />
                        Detalhes
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setDeletingPod(pod);
                          setDeleteConfirmOpen(true);
                        }}
                        className="gap-2 text-red-600 hover:text-red-700 hover:bg-red-50 dark:hover:bg-red-950/30"
                      >
                        <Trash2 className="w-4 h-4" />
                        Deletar
                      </Button>
                    </div>
                  </div>
                </div>
              </Card>
            );
          })}

          {filteredPods.length === 0 && !loading && (
            <div className="text-center py-12 text-slate-500 dark:text-slate-400">
              Nenhum pod encontrado
            </div>
          )}
        </div>
      </ScrollArea>

      {/* Modal de Detalhes (YAML + Logs) */}
      <Dialog open={detailsOpen} onOpenChange={setDetailsOpen}>
        <DialogContent className="max-w-5xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>
              Pod: {selectedPod?.name}
            </DialogTitle>
            <DialogDescription>
              Namespace: {selectedPod?.namespace} | Node: {selectedPod?.nodeName}
            </DialogDescription>
          </DialogHeader>

          <Tabs defaultValue="yaml" className="flex-1">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="yaml">
                <FileText className="w-4 h-4 mr-2" />
                YAML Manifest
              </TabsTrigger>
              <TabsTrigger value="logs">
                <Terminal className="w-4 h-4 mr-2" />
                Logs
              </TabsTrigger>
            </TabsList>

            <TabsContent value="yaml" className="flex-1">
              <ScrollArea className="h-[500px] w-full border rounded-lg bg-slate-50 dark:bg-slate-900">
                <pre className="p-4 text-xs font-mono">
                  <code>{podYaml}</code>
                </pre>
              </ScrollArea>
            </TabsContent>

            <TabsContent value="logs" className="flex-1">
              <div className="space-y-4">
                {selectedPod?.containers.map((container) => (
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
                      <ScrollArea className="h-[200px] w-full border rounded-lg bg-slate-50 dark:bg-slate-900">
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
        </DialogContent>
      </Dialog>

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
    </div>
  );
};
