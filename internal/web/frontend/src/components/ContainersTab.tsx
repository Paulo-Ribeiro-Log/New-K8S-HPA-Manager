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
import { Search, RefreshCcw, Eye, EyeOff, Terminal, Trash2, FileText, AlertCircle, CheckCircle2, XCircle, Loader2, Download, ChevronDown, ChevronRight, Maximize2, X } from "lucide-react";
import { toast } from "sonner";

import type {
  Namespace,
  PodSummary,
  ContainerStatus,
} from "@/lib/api/types";
import { usePods } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";

interface ContainersTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const ContainersTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: ContainersTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedPod, setSelectedPod] = useState<PodSummary | null>(null);
  const [selectedContainer, setSelectedContainer] = useState<string | null>(null);
  const [logs, setLogs] = useState<string>("");
  const [logsLoading, setLogsLoading] = useState(false);
  const [showLabelsInDetails, setShowLabelsInDetails] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deletingPod, setDeletingPod] = useState(false);
  const [manifestModalOpen, setManifestModalOpen] = useState(false);
  const [manifest, setManifest] = useState<string>("");
  const [manifestLoading, setManifestLoading] = useState(false);

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

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { pods, loading, error, refetch } = usePods(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar Pods", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedPod(null);
    setSelectedContainer(null);
    setLogs("");
  }, [cluster, selectedNamespace]);

  const filteredPods = useMemo(() => {
    if (!searchQuery) return pods;
    const query = searchQuery.toLowerCase();
    return pods.filter(
      (pod) =>
        pod.name.toLowerCase().includes(query) ||
        pod.namespace.toLowerCase().includes(query) ||
        pod.nodeName?.toLowerCase().includes(query) ||
        pod.containers.some(c => c.name.toLowerCase().includes(query))
    );
  }, [pods, searchQuery]);

  const refreshPods = () => {
    if (!cluster) return;
    refetch();
  };

  const fetchLogs = async (pod: PodSummary, containerName: string) => {
    setLogsLoading(true);
    try {
      const result = await apiClient.getPodLogs(pod.cluster, pod.namespace, pod.name, containerName, 1000);
      setLogs(result.logs || "No logs available");
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : "Failed to fetch logs";
      toast.error("Erro ao buscar logs", { description: errorMsg });
      setLogs(`Error fetching logs: ${errorMsg}`);
    } finally {
      setLogsLoading(false);
    }
  };

  const handlePodSelect = async (pod: PodSummary) => {
    setSelectedPod(pod);
    setSelectedContainer(null);
    setLogs("");
    setManifestLoading(true);
    setManifest("");
    
    // Auto-select first container
    if (pod.containers.length > 0) {
      const firstContainer = pod.containers[0].name;
      setSelectedContainer(firstContainer);
      fetchLogs(pod, firstContainer);
    }

    // Load manifest
    try {
      const result = await apiClient.getPod(pod.cluster, pod.namespace, pod.name);
      setManifest(result.yaml);
    } catch (err) {
      setManifest("Error loading manifest");
      toast.error("Erro ao carregar manifest do Pod");
    } finally {
      setManifestLoading(false);
    }
  };

  const handleContainerSelect = (containerName: string) => {
    setSelectedContainer(containerName);
    if (selectedPod) {
      fetchLogs(selectedPod, containerName);
    }
  };

  const handleDeletePod = async () => {
    if (!selectedPod) return;
    
    setDeletingPod(true);
    try {
      await apiClient.deletePod(selectedPod.cluster, selectedPod.namespace, selectedPod.name);
      toast.success("Pod deletado com sucesso");
      setDeleteConfirmOpen(false);
      setSelectedPod(null);
      refetch();
    } catch (err) {
      toast.error("Erro ao deletar Pod", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
    } finally {
      setDeletingPod(false);
    }
  };

  const handleViewManifest = async () => {
    if (!selectedPod) return;
    
    setManifestLoading(true);
    try {
      const result = await apiClient.getPod(selectedPod.cluster, selectedPod.namespace, selectedPod.name);
      setManifest(result.yaml);
      // Open modal after manifest is loaded
      setTimeout(() => setManifestModalOpen(true), 100);
    } catch (err) {
      toast.error("Erro ao buscar manifest", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
      setManifest("Error loading manifest");
      setManifestModalOpen(true);
    } finally {
      setManifestLoading(false);
    }
  };

  const downloadLogs = () => {
    if (!selectedPod || !selectedContainer || !logs) return;
    
    const blob = new Blob([logs], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${selectedPod.name}-${selectedContainer}-logs.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast.success("Logs salvos");
  };

  const getPhaseColor = (phase: string) => {
    switch (phase.toLowerCase()) {
      case 'running': return 'default';
      case 'succeeded': return 'secondary';
      case 'failed': return 'destructive';
      case 'pending': return 'outline';
      default: return 'outline';
    }
  };

  const getContainerStateIcon = (state: string) => {
    switch (state.toLowerCase()) {
      case 'running': return <CheckCircle2 className="w-3 h-3 text-green-500" />;
      case 'terminated': return <XCircle className="w-3 h-3 text-red-500" />;
      case 'waiting': return <Loader2 className="w-3 h-3 text-yellow-500 animate-spin" />;
      default: return <AlertCircle className="w-3 h-3 text-gray-500" />;
    }
  };

  const extractImageVersion = (image: string): string => {
    // Extract version from image string (e.g., "nginx:1.21.0" -> "1.21.0")
    const parts = image.split(':');
    if (parts.length > 1) {
      // Get the part after the last ':'
      const versionPart = parts[parts.length - 1];
      // If it contains '/', get the part after '/' (for registry paths like gcr.io/project/image:v1.0.0)
      const slashIndex = versionPart.lastIndexOf('/');
      return slashIndex >= 0 ? versionPart.substring(slashIndex + 1) : versionPart;
    }
    return 'latest';
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
      <Button variant="outline" size="sm" onClick={refreshPods} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
      </Button>
    </div>
  );

  const leftPanel = (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Buscar pods, containers..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-8"
          />
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="space-y-2">
          {loading && (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
            </div>
          )}

          {!loading && filteredPods.length === 0 && (
            <div className="text-center py-8 text-muted-foreground text-sm">
              {searchQuery ? "Nenhum pod encontrado" : "Nenhum pod disponível"}
            </div>
          )}

          {filteredPods.map((pod) => (
            <div
              key={`${pod.namespace}/${pod.name}`}
              className={`border rounded-lg p-3 cursor-pointer transition-colors ${
                selectedPod?.name === pod.name && selectedPod?.namespace === pod.namespace
                  ? "border-primary bg-accent"
                  : "hover:bg-accent"
              }`}
              onClick={() => handlePodSelect(pod)}
            >
              <div className="flex items-start justify-between mb-2">
                <div className="flex-1 min-w-0">
                  <h4 className="font-medium text-sm truncate">{pod.name}</h4>
                  <p className="text-xs text-muted-foreground">{pod.namespace}</p>
                </div>
                <Badge variant={getPhaseColor(pod.phase)} className="text-xs ml-2">
                  {pod.phase}
                </Badge>
              </div>

              <div className="grid grid-cols-2 gap-2 text-xs mb-2">
                <div>
                  <span className="text-muted-foreground">Containers:</span>
                  <span className="ml-1 font-medium">{pod.readyContainers}/{pod.totalContainers}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Restarts:</span>
                  <span className="ml-1 font-medium">{pod.restarts}</span>
                </div>
                {pod.nodeName && (
                  <div className="col-span-2">
                    <span className="text-muted-foreground">Node:</span>
                    <span className="ml-1 font-medium text-xs">{pod.nodeName}</span>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      </ScrollArea>
    </div>
  );

  const rightPanel = selectedPod ? (
    <div className="flex flex-col h-full">
      <div className="flex items-start justify-between mb-4 pb-4 border-b">
        <div className="flex-1">
          <h3 className="text-lg font-semibold">{selectedPod.name}</h3>
          <p className="text-sm text-muted-foreground">{selectedPod.namespace}</p>
          <div className="flex items-center gap-2 mt-2">
            <Badge variant={getPhaseColor(selectedPod.phase)}>{selectedPod.phase}</Badge>
            {selectedPod.podIP && <Badge variant="outline">IP: {selectedPod.podIP}</Badge>}
          </div>
        </div>
        
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={handleViewManifest}>
            <FileText className="w-4 h-4 mr-1" />
            Manifest
          </Button>
        </div>
      </div>

      {selectedPod.labels && Object.keys(selectedPod.labels).length > 0 && (
        <div className="mb-4 pb-3 border-b border-border/50">
          <button
            type="button"
            onClick={() => setShowLabelsInDetails((prev) => !prev)}
            className="flex items-center gap-1 text-xs text-muted-foreground uppercase mb-2 hover:text-foreground transition-colors"
          >
            {showLabelsInDetails ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
            <span>Labels</span>
          </button>
          {showLabelsInDetails && (
            <div className="flex flex-wrap gap-1">
              {Object.entries(selectedPod.labels).map(([key, value]) => (
                <span
                  key={`${key}-${value}`}
                  className="px-1.5 py-0.5 bg-secondary/60 rounded text-[10px] font-mono"
                >
                  {key}={value}
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      <Tabs defaultValue="logs" className="flex-1 flex flex-col">
        <TabsList className="w-full justify-start mb-4">
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="details">Details</TabsTrigger>
        </TabsList>

        <TabsContent value="logs" className="flex-1 flex flex-col mt-0">
          {selectedPod.containers.length > 0 && (
            <div className="flex items-center gap-2 mb-3">
              <Select value={selectedContainer || ""} onValueChange={handleContainerSelect}>
                <SelectTrigger className="w-[250px]">
                  <SelectValue placeholder="Select container" />
                </SelectTrigger>
                <SelectContent>
                  {selectedPod.containers.map((container) => (
                    <SelectItem key={container.name} value={container.name}>
                      <div className="flex items-center gap-2">
                        {getContainerStateIcon(container.state)}
                        {container.name}
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Button
                variant="outline"
                size="sm"
                onClick={() => selectedContainer && fetchLogs(selectedPod, selectedContainer)}
                disabled={!selectedContainer || logsLoading}
              >
                <RefreshCcw className={`w-4 h-4 ${logsLoading ? 'animate-spin' : ''}`} />
              </Button>

              <Button
                variant="outline"
                size="sm"
                onClick={downloadLogs}
                disabled={!logs}
              >
                <Download className="w-4 h-4" />
              </Button>
            </div>
          )}

          <ScrollArea className="flex-1 border rounded-lg bg-black text-green-400 p-4">
            {logsLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap">{logs || "Select a container to view logs"}</pre>
            )}
          </ScrollArea>
        </TabsContent>

        <TabsContent value="details" className="flex-1 mt-0">
          <ScrollArea className="h-full">
            <div className="space-y-4">
              <div>
                <h4 className="font-medium mb-2">Pod Information</h4>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <span className="text-muted-foreground">Name:</span>
                    <p className="font-medium">{selectedPod.name}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Namespace:</span>
                    <p className="font-medium">{selectedPod.namespace}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Phase:</span>
                    <p className="font-medium">{selectedPod.phase}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Pod IP:</span>
                    <p className="font-medium">{selectedPod.podIP || 'N/A'}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Node:</span>
                    <p className="font-medium">{selectedPod.nodeName || 'N/A'}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Created:</span>
                    <p className="font-medium">{new Date(selectedPod.createdAt).toLocaleString()}</p>
                  </div>
                </div>
              </div>

              <div>
                <h4 className="font-medium mb-2">Resource Requests & Limits</h4>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  {selectedPod.cpuRequest && (
                    <div>
                      <span className="text-muted-foreground">CPU Request:</span>
                      <p className="font-medium">{selectedPod.cpuRequest}</p>
                    </div>
                  )}
                  {selectedPod.cpuLimit && (
                    <div>
                      <span className="text-muted-foreground">CPU Limit:</span>
                      <p className="font-medium">{selectedPod.cpuLimit}</p>
                    </div>
                  )}
                  {selectedPod.memoryRequest && (
                    <div>
                      <span className="text-muted-foreground">Memory Request:</span>
                      <p className="font-medium">{selectedPod.memoryRequest}</p>
                    </div>
                  )}
                  {selectedPod.memoryLimit && (
                    <div>
                      <span className="text-muted-foreground">Memory Limit:</span>
                      <p className="font-medium">{selectedPod.memoryLimit}</p>
                    </div>
                  )}
                </div>
              </div>

              <div>
                <h4 className="font-medium mb-2">Containers ({selectedPod.containers.length})</h4>
                <div className="space-y-2">
                  {selectedPod.containers.map((container) => (
                    <div key={container.name} className="border rounded-lg p-3">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          {getContainerStateIcon(container.state)}
                          <span className="font-medium">{container.name}</span>
                          <Badge variant="outline" className="text-xs">
                            v{extractImageVersion(container.image)}
                          </Badge>
                        </div>
                        <Badge variant={container.ready ? "default" : "destructive"} className="text-xs">
                          {container.ready ? "Ready" : "Not Ready"}
                        </Badge>
                      </div>
                      <div className="grid grid-cols-2 gap-2 text-xs">
                        <div>
                          <span className="text-muted-foreground">Image:</span>
                          <p className="font-medium text-[11px] break-all">{container.image}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">State:</span>
                          <p className="font-medium">{container.state}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Restarts:</span>
                          <p className="font-medium">{container.restartCount}</p>
                        </div>
                        {container.stateReason && (
                          <div>
                            <span className="text-muted-foreground">Reason:</span>
                            <p className="font-medium">{container.stateReason}</p>
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              {/* YAML Manifest Section */}
              <div className="flex-1 flex flex-col min-h-0 space-y-2">
                <div className="flex items-center justify-between flex-shrink-0">
                  <p className="text-sm font-medium">Manifesto YAML (Read-only)</p>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (manifest) {
                          navigator.clipboard.writeText(manifest);
                          toast.success("YAML copiado");
                        }
                      }}
                      disabled={!manifest || manifestLoading}
                    >
                      <FileText className="w-4 h-4 mr-2" />
                      {manifestLoading ? "Carregando..." : "Copiar YAML"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setManifestModalOpen(true)}
                      title="Abrir YAML em tela cheia"
                      disabled={!manifest || manifestLoading}
                    >
                      <Maximize2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </div>

                {manifestLoading ? (
                  <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
                    Carregando manifest...
                  </div>
                ) : (
                  <div className="flex-1 min-h-0">
                    <MonacoYamlEditor
                      value={manifest || ""}
                      readOnly={true}
                      height="400px"
                    />
                  </div>
                )}
              </div>
            </div>
          </ScrollArea>
        </TabsContent>
      </Tabs>

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Exclusão</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o pod <strong>{selectedPod.name}</strong>?
              Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2 mt-4">
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)} disabled={deletingPod}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDeletePod} disabled={deletingPod}>
              {deletingPod ? <Loader2 className="w-4 h-4 animate-spin mr-2" /> : null}
              Deletar
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Manifest Dialog */}
      <Dialog open={manifestModalOpen} onOpenChange={setManifestModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh] flex flex-col p-0">
          <DialogHeader className="border-b border-border px-6 py-4 flex-shrink-0">
            <div className="flex items-center justify-between gap-4 pr-8">
              <div>
                <DialogTitle className="text-xl font-semibold">
                  Manifesto YAML (Read-only)
                </DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground mt-1">
                  {selectedPod.namespace}/{selectedPod.name}
                </DialogDescription>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    if (manifest) {
                      navigator.clipboard.writeText(manifest);
                      toast.success("YAML copiado para área de transferência");
                    }
                  }}
                  disabled={!manifest || manifestLoading}
                >
                  <FileText className="w-4 h-4 mr-2" />
                  Copiar YAML
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setManifestModalOpen(false)}
                  className="h-8 w-8 p-0"
                >
                  <X className="w-4 h-4" />
                </Button>
              </div>
            </div>
          </DialogHeader>

          {/* Monaco Editor */}
          <div className="flex-1 overflow-hidden px-6 pb-6 pt-4" style={{ minHeight: '500px' }}>
            {manifestLoading ? (
              <div className="flex items-center justify-center h-full">
                <div className="flex flex-col items-center gap-2">
                  <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
                  <span className="text-sm text-muted-foreground">Carregando manifest...</span>
                </div>
              </div>
            ) : manifest && manifest !== "Error loading manifest" ? (
              <MonacoYamlEditor
                key={`manifest-modal-${selectedPod.name}`}
                value={manifest}
                readOnly={true}
                height="calc(90vh - 150px)"
              />
            ) : (
              <div className="flex items-center justify-center h-full text-muted-foreground">
                <p>{manifest === "Error loading manifest" ? "Erro ao carregar manifest" : "Nenhum manifest disponível"}</p>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  ) : (
    <div className="flex items-center justify-center h-full text-muted-foreground">
      <div className="text-center">
        <Terminal className="w-12 h-12 mx-auto mb-2 opacity-50" />
        <p>Selecione um pod para ver detalhes e logs</p>
      </div>
    </div>
  );

  const rightTitleAction = undefined;

  return (
    <SplitView
      leftPanel={{
        title: "Pods & Containers",
        titleAction: leftTitleAction,
        content: leftPanel,
      }}
      rightPanel={{
        title: selectedPod ? "Container Logs & Details" : "Container Logs & Details",
        titleAction: rightTitleAction,
        content: rightPanel,
      }}
    />
  );
};
