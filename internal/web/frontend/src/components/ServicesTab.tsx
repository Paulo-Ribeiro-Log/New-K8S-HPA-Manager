import { useEffect, useMemo, useState, useCallback, useRef } from "react";
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
import {
  Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert,
  FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, X,
  FileText, MoreVertical, Trash2, SplitSquareHorizontal, AlertCircle,
  Network, Plus, Copy, ChevronLeft
} from "lucide-react";
import { toast } from "sonner";

import type { Namespace, ServiceSummary, ServiceManifest } from "@/lib/api/types";
import { useServices } from "@/hooks/useAPI";
import { ServiceMonitorTable } from "@/components/ServiceMonitorTable";
import { apiClient } from "@/lib/api/client";
import { setHistoryCacheEntry } from "@/lib/historyCache";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { usePersistedTabState } from "@/hooks/usePersistedTabState";

const SERVICE_TEMPLATE = `apiVersion: v1
kind: Service
metadata:
  name: my-service
  namespace: default
  labels:
    app: my-app
spec:
  selector:
    app: my-app
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: 8080
  type: ClusterIP
`;

interface ServicesTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

const serviceTypeBadgeClass = (type: string) => {
  switch (type) {
    case "ClusterIP":    return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
    case "NodePort":     return "bg-blue-500/20 text-blue-400 border border-blue-500/30";
    case "LoadBalancer": return "bg-green-500/20 text-green-400 border border-green-500/30";
    case "ExternalName": return "bg-orange-500/20 text-orange-400 border border-orange-500/30";
    default:             return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
  }
};

export const ServicesTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: ServicesTabProps) => {
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>("services", "searchQuery", "");
  const [selectedService, setSelectedService] = usePersistedTabState<ServiceSummary | null>("services", "selectedService", null);
  const [viewMode, setViewMode] = usePersistedTabState<"editor" | "diff">("services", "viewMode", "editor");

  const [manifest, setManifest] = useState<ServiceManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [createYaml, setCreateYaml] = useState(SERVICE_TEMPLATE);
  const [isCreating, setIsCreating] = useState(false);

  const historyCache = useRef<Map<string, { history: string[]; index: number }>>(new Map());
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const filteredNamespaces = useMemo(() => {
    if (!namespaces) return [];
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) onNamespaceChange("");
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { services, loading, error, refetch, silentRefetch } = useServices(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) toast.error("Erro ao carregar Services", { description: error });
  }, [error]);

  useEffect(() => {
    setSelectedService(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedNamespace]);

  const filteredServices = useMemo(() => {
    if (!services) return [];
    if (!searchQuery) return services;
    const query = searchQuery.toLowerCase();
    return services.filter(
      (s) =>
        s.name.toLowerCase().includes(query) ||
        s.namespace.toLowerCase().includes(query) ||
        s.type.toLowerCase().includes(query) ||
        (s.clusterIP || "").toLowerCase().includes(query) ||
        (s.ports || []).some((p) => p.toLowerCase().includes(query))
    );
  }, [services, searchQuery]);

  const handleSelectService = async (summary: ServiceSummary) => {
    if (selectedService && history.length > 0) {
      const cacheKey = `${selectedService.namespace}/${selectedService.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedService(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getServiceManifest(summary.cluster, summary.namespace, summary.name);
      setManifest(detail);
      const initialYaml = detail.yaml || "";
      setOriginalYaml(initialYaml);
      setViewMode("editor");

      const cacheKey = `${summary.namespace}/${summary.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setHistory(cached.history);
        setHistoryIndex(cached.index);
        setEditorValue(cached.history[cached.index]);
      } else {
        setHistory([initialYaml]);
        setHistoryIndex(0);
        setEditorValue(initialYaml);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto Service", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
  }, []);

  const addToHistory = useCallback((value: string) => {
    setHistory((prev) => {
      const currentIndex = historyIndex;
      const newHistory = prev.slice(0, currentIndex + 1);
      if (newHistory[newHistory.length - 1] === value) return prev;
      const updated = [...newHistory, value].slice(-50);
      setHistoryIndex(updated.length - 1);
      return updated;
    });
  }, [historyIndex]);

  const handleUndo = useCallback(() => {
    if (historyIndex <= 0) return;
    const newIndex = historyIndex - 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
  }, [history, historyIndex]);

  const handleRedo = useCallback(() => {
    if (historyIndex >= history.length - 1) return;
    const newIndex = historyIndex + 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
  }, [history, historyIndex]);

  const handleValidate = async () => {
    if (!selectedService) return;
    setIsValidating(true);
    try {
      await apiClient.validateService({
        cluster: selectedService.cluster,
        namespace: selectedService.namespace,
        name: selectedService.name,
        yaml: editorValue,
      });
      toast.success("YAML válido", { description: "Validação dry-run bem-sucedida" });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error("Erro de validação", { description: msg });
    } finally {
      setIsValidating(false);
    }
  };

  const handleShowDiff = async () => {
    if (!selectedService) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffService({
        originalYaml,
        updatedYaml: editorValue,
        fileName: `${selectedService.namespace}/${selectedService.name}`,
      });
      const html = diff2html(result.unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffModalOpen(true);
    } catch (err) {
      toast.error("Erro ao gerar diff", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleApply = async () => {
    if (!selectedService) return;
    setIsApplying(true);
    try {
      await apiClient.applyService(selectedService.cluster, selectedService.namespace, selectedService.name, {
        yaml: editorValue,
        force: true,
      });
      toast.success("Service aplicado com sucesso");
      setOriginalYaml(editorValue);
      addToHistory(editorValue);
      setApplyConfirmOpen(false);
      refetch();
    } catch (err) {
      setApplyConfirmOpen(false);
      const msg = err instanceof Error ? err.message : String(err);
      setErrorTitle("Erro ao aplicar Service");
      setErrorMessage(msg);
      setErrorDialogOpen(true);
    } finally {
      setIsApplying(false);
    }
  };

  const fetchDescribe = async () => {
    if (!selectedService) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeService(
        selectedService.cluster,
        selectedService.namespace,
        selectedService.name
      );
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
    if (!selectedService) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const handleDelete = async () => {
    if (!selectedService) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteService(selectedService.cluster, selectedService.namespace, selectedService.name);
      toast.success(`Service ${selectedService.name} deletado com sucesso`);
      setDeleteConfirmOpen(false);
      setSelectedService(null);
      setManifest(null);
      setEditorValue("");
      refetch();
    } catch (err) {
      toast.error("Erro ao deletar Service", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  const handleCreate = async () => {
    if (!cluster) return;
    setIsCreating(true);
    try {
      const ns = selectedNamespace || "default";
      const result = await apiClient.createService(cluster, ns, createYaml);
      toast.success(`Service ${result.name} criado com sucesso`);
      setCreateModalOpen(false);
      setCreateYaml(SERVICE_TEMPLATE);
      refetch();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error("Erro ao criar Service", { description: msg });
    } finally {
      setIsCreating(false);
    }
  };

  const handleClearSelection = () => {
    setSelectedService(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
  };

  const handleReloadYaml = async () => {
    if (!selectedService) return;
    setManifestLoading(true);
    try {
      const detail = await apiClient.getServiceManifest(selectedService.cluster, selectedService.namespace, selectedService.name);
      const freshYaml = detail.yaml || "";
      setManifest(detail);
      setOriginalYaml(freshYaml);
      setEditorValue(freshYaml);
      setHistory([freshYaml]);
      setHistoryIndex(0);
      toast.success("YAML recarregado do cluster");
    } catch (err) {
      toast.error("Falha ao recarregar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const hasChanges = editorValue !== originalYaml;
  void manifest;

  // ─── Left panel ──────────────────────────────────────────────────────────
  const leftTitleAction = (
    <div className="flex items-center gap-2">
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={refetch} disabled={loading}>
        <RefreshCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
      </Button>
      <ProtectedAction>
        <Button
          size="sm"
          onClick={() => { setCreateYaml(SERVICE_TEMPLATE); setCreateModalOpen(true); }}
        >
          <Plus className="w-4 h-4 mr-1" />
          Criar
        </Button>
      </ProtectedAction>
    </div>
  );

  const leftContent = (
    <div className="space-y-3">
      {selectedService && (
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary font-medium">
          <Network className="w-3.5 h-3.5 flex-shrink-0" />
          <span className="truncate flex-1">{selectedService.namespace}/{selectedService.name}</span>
          <button onClick={handleClearSelection} className="flex-shrink-0 hover:text-foreground transition-colors" title="Limpar seleção">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
      <Select value={selectedNamespace || "__all__"} onValueChange={(v) => onNamespaceChange(v === "__all__" ? "" : v)}>
        <SelectTrigger className="h-8 w-full">
          <SelectValue placeholder="Todos os namespaces" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="__all__">Todos os namespaces</SelectItem>
          {filteredNamespaces.map((ns) => (
            <SelectItem key={ns.name} value={ns.name}>{ns.name}</SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className="relative">
        <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Buscar Services..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-8 h-8"
        />
        {searchQuery && (
          <Button
            variant="ghost" size="icon"
            className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6"
            onClick={() => setSearchQuery("")}
          >
            <X className="h-3 w-3" />
          </Button>
        )}
      </div>

      {!loading && (
        <div className="text-xs text-muted-foreground">
          {filteredServices.length} service(s) encontrado(s)
        </div>
      )}

      {loading ? (
        <div className="flex justify-center py-6">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : filteredServices.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-6 text-muted-foreground">
          <Network className="h-8 w-8 opacity-40" />
          <p className="text-sm">Nenhum Service encontrado</p>
        </div>
      ) : (
        <div className="space-y-1">
          {filteredServices.map((svc) => {
            const isSelected =
              selectedService?.cluster === svc.cluster &&
              selectedService?.namespace === svc.namespace &&
              selectedService?.name === svc.name;
            return (
              <div
                key={`${svc.cluster}/${svc.namespace}/${svc.name}`}
                className={`flex items-start gap-2 p-3 rounded-lg border transition-colors relative cursor-pointer ${
                  isSelected
                    ? "border-primary bg-accent shadow-sm"
                    : "border-border/50 hover:border-primary/50 hover:bg-muted/50"
                }`}
                onClick={() => handleSelectService(svc)}
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <span className="font-medium text-sm truncate block">{svc.name}</span>
                      <span className="text-xs text-muted-foreground">{svc.namespace}</span>
                    </div>
                    <span className={`px-1.5 py-0.5 rounded text-[10px] mt-1 font-medium shrink-0 inline-block ${serviceTypeBadgeClass(svc.type)}`}>
                      {svc.type}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 mt-1 flex-wrap">
                    {svc.clusterIP && svc.clusterIP !== "None" && (
                      <span className="text-xs text-muted-foreground">{svc.clusterIP}</span>
                    )}
                    {svc.externalIP && (
                      <span className="text-xs text-green-400">{svc.externalIP}</span>
                    )}
                    {svc.ports && svc.ports.length > 0 && (
                      <span className="text-xs text-muted-foreground truncate">
                        {svc.ports.slice(0, 2).join(", ")}
                        {svc.ports.length > 2 && ` +${svc.ports.length - 2}`}
                      </span>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );

  // ─── Right panel ─────────────────────────────────────────────────────────
  const rightTitlePrefix = selectedService ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  const rightTitleAction = selectedService ? (
    <div className="flex items-center gap-1">
      {selectedService.type && (
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${serviceTypeBadgeClass(selectedService.type)}`}>
          {selectedService.type}
        </span>
      )}
      <Button variant="outline" size="sm" onClick={handleReloadYaml} disabled={manifestLoading}>
        {manifestLoading ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <RefreshCcw className="w-4 h-4 mr-1" />}
        Recarregar YAML
      </Button>
      <Button variant="outline" size="sm" onClick={handleViewDescribe}>
        <FileText className="w-4 h-4 mr-1" />
        Describe
      </Button>
      <ProtectedAction>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="h-8 w-8 p-0">
              <MoreVertical className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-red-500 focus:text-red-500"
              onClick={() => setDeleteConfirmOpen(true)}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Deletar Service
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </ProtectedAction>
    </div>
  ) : null;

  const renderManifestPanel = () => {
    if (!selectedService) {
      return (
        <ServiceMonitorTable
          services={services ?? []}
          loading={loading}
          headerLabel={`${(services ?? []).length} service(s)`}
          onOpenEditor={handleSelectService}
          onRequestRefresh={silentRefetch}
        />
      );
    }

    return (
      <div className="flex flex-col h-full gap-2">
        {/* Toolbar */}
        <div className="flex items-center justify-between flex-wrap gap-1">
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" onClick={handleUndo} disabled={historyIndex <= 0} title="Desfazer">
              <Undo2 className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="sm" onClick={handleRedo} disabled={historyIndex >= history.length - 1} title="Refazer">
              <Redo2 className="h-4 w-4" />
            </Button>
            <div className="w-px h-4 bg-border mx-1" />
            <Button
              variant="ghost" size="sm"
              onClick={() => setViewMode(viewMode === "editor" ? "diff" : "editor")}
            >
              <SplitSquareHorizontal className="h-4 w-4" />
              <span className="ml-1 text-xs">{viewMode === "editor" ? "Diff" : "Editor"}</span>
            </Button>
            <Button
              variant="ghost" size="sm"
              onClick={handleShowDiff}
              disabled={isDiffLoading || !hasChanges}
            >
              {isDiffLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <FileDiff className="h-4 w-4" />}
              <span className="ml-1 text-xs">Ver Diff</span>
            </Button>
            <Button variant="ghost" size="sm" onClick={handleValidate} disabled={isValidating}>
              {isValidating ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
              <span className="ml-1 text-xs">Validar</span>
            </Button>
          </div>
          <Button variant="ghost" size="sm" onClick={() => setEditorFullScreen(true)} title="Tela cheia">
            <Maximize2 className="h-4 w-4" />
          </Button>
        </div>

        {/* Editor */}
        <div className="flex-1 min-h-0">
          {manifestLoading ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <MonacoYamlEditor
              value={viewMode === "editor" ? editorValue : originalYaml}
              onChange={viewMode === "editor" ? handleEditorChange : undefined}
              readOnly={viewMode === "diff"}
              height={520}
            />
          )}
        </div>

        {/* Botões */}
        <div className="flex items-center justify-end gap-2 pt-2 border-t border-border/50">
          <Button
            variant="ghost" size="sm"
            onClick={() => { setEditorValue(originalYaml); setHistory([originalYaml]); setHistoryIndex(0); }}
            disabled={!hasChanges}
          >
            Cancelar
          </Button>
          <ProtectedAction>
            <Button size="sm" onClick={() => setApplyConfirmOpen(true)} disabled={!hasChanges || isApplying}>
              {isApplying && <Loader2 className="h-4 w-4 mr-1 animate-spin" />}
              Aplicar
            </Button>
          </ProtectedAction>
        </div>
      </div>
    );
  };

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Services",
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedService ? `${selectedService.namespace}/${selectedService.name}` : "Visualização",
          titlePrefix: rightTitlePrefix,
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {/* Modal Criar Service */}
      <Dialog open={createModalOpen} onOpenChange={setCreateModalOpen}>
        <DialogContent className="max-w-3xl w-full">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Plus className="h-5 w-5" />
              Criar novo Service
            </DialogTitle>
            <DialogDescription>
              Edite o YAML abaixo e clique em Criar para adicionar o Service ao cluster.
            </DialogDescription>
          </DialogHeader>
          <div>
            <MonacoYamlEditor
              value={createYaml}
              onChange={(v) => setCreateYaml(v)}
              height={400}
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateModalOpen(false)} disabled={isCreating}>
              Cancelar
            </Button>
            <Button onClick={handleCreate} disabled={isCreating}>
              {isCreating && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Criar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Diff */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className={diffFullScreen ? "max-w-[98vw] w-[98vw] h-[95vh]" : "max-w-4xl w-full"}>
          <DialogHeader>
            <DialogTitle className="flex items-center justify-between">
              Diff do Service
              <Button variant="ghost" size="sm" onClick={() => setDiffFullScreen(!diffFullScreen)}>
                {diffFullScreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
              </Button>
            </DialogTitle>
          </DialogHeader>
          <ScrollArea className={diffFullScreen ? "h-[calc(95vh-80px)]" : "h-[60vh]"}>
            <div className="text-xs" dangerouslySetInnerHTML={{ __html: diffHtml }} />
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Modal Apply Confirm */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Aplicar alterações</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja aplicar as alterações no Service{" "}
              <strong>{selectedService?.name}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setApplyConfirmOpen(false)} disabled={isApplying}>
              Cancelar
            </Button>
            <Button onClick={handleApply} disabled={isApplying}>
              {isApplying && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Aplicar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Delete Confirm */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-red-500">
              <TriangleAlert className="h-5 w-5" />
              Deletar Service
            </DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o Service{" "}
              <strong>{selectedService?.name}</strong> no namespace{" "}
              <strong>{selectedService?.namespace}</strong>? Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteConfirmOpen(false)} disabled={isDeleting}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
              {isDeleting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Deletar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-4xl w-full">
          <DialogHeader>
            <DialogTitle>kubectl describe service {selectedService?.name}</DialogTitle>
          </DialogHeader>
          <ScrollArea className="h-[60vh]">
            {describeLoading ? (
              <div className="flex items-center justify-center h-24">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono bg-muted p-4 rounded whitespace-pre-wrap">
                {describeContent}
              </pre>
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

      {/* Modal Editor Fullscreen */}
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent className="max-w-[98vw] w-[98vw] h-[95vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center justify-between">
              <span>{selectedService?.namespace}/{selectedService?.name}</span>
              <Button variant="ghost" size="sm" onClick={() => setEditorFullScreen(false)}>
                <Minimize2 className="h-4 w-4" />
              </Button>
            </DialogTitle>
          </DialogHeader>
          <div className="flex items-center gap-2 py-1 border-b border-border/50">
            <Button variant="ghost" size="sm" onClick={handleUndo} disabled={historyIndex <= 0}>
              <Undo2 className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="sm" onClick={handleRedo} disabled={historyIndex >= history.length - 1}>
              <Redo2 className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="sm" onClick={handleValidate} disabled={isValidating}>
              {isValidating ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
              <span className="ml-1 text-xs">Validar</span>
            </Button>
          </div>
          <div className="flex-1 overflow-hidden">
            <MonacoYamlEditor
              value={editorValue}
              onChange={handleEditorChange}
              height="calc(95vh - 140px)"
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setEditorFullScreen(false)}>Fechar</Button>
            <ProtectedAction>
              <Button
                onClick={() => { setEditorFullScreen(false); setApplyConfirmOpen(true); }}
                disabled={!hasChanges}
              >
                Aplicar
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Erro */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-red-500">
              <AlertCircle className="h-5 w-5" />
              {errorTitle}
            </DialogTitle>
          </DialogHeader>
          <ScrollArea className="max-h-64">
            <pre className="text-xs font-mono whitespace-pre-wrap break-words text-red-400 p-2">
              {errorMessage}
            </pre>
          </ScrollArea>
          <DialogFooter>
            <Button onClick={() => setErrorDialogOpen(false)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
