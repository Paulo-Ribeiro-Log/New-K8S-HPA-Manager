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
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, PanelLeftClose, PanelLeftOpen, FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, X, FileText, Network, MoreVertical, Trash2, SplitSquareHorizontal, AlertCircle, Copy } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import yaml from "js-yaml";

import type {
  Namespace,
  IngressSummary,
  IngressManifest,
} from "@/lib/api/types";
import { useIngresses } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { setHistoryCacheEntry } from "@/lib/historyCache";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac";

interface IngressTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onOpenCompare?: (initial: { type: "ingress"; namespace: string; name: string }) => void;
}

export const IngressTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onOpenCompare,
}: IngressTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedIngress, setSelectedIngress] = useState<IngressSummary | null>(null);
  const [manifest, setManifest] = useState<IngressManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [showLabels, setShowLabels] = useState(false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
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

  // Error Dialog para exibir erros de apply de forma mais proeminente
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  // Undo/Redo history with persistent cache
  const historyCache = useRef<Map<string, { history: string[], index: number }>>(new Map());
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

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
  const { ingresses, loading, error, refetch } = useIngresses(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar Ingresses", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedIngress(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(false);
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedNamespace]);

  const filteredIngresses = useMemo(() => {
    let filtered = ingresses;
    
    // Filtrar ingresses de sistema quando Sistema está desligado
    if (!showSystemNamespaces) {
      filtered = filtered.filter((ing) => {
        const name = ing.name.toLowerCase();
        return !name.startsWith("istio-") &&
               !name.startsWith("kube-") &&
               !name.includes("prometheus");
      });
    }
    
    // Aplicar busca por query
    if (!searchQuery) return filtered;
    const query = searchQuery.toLowerCase();
    return filtered.filter((ing) => {
      return (
        ing.name.toLowerCase().includes(query) ||
        ing.namespace.toLowerCase().includes(query) ||
        ing.hosts?.some(h => h.toLowerCase().includes(query)) ||
        (ing.ingressClass && ing.ingressClass.toLowerCase().includes(query)) ||
        Object.entries(ing.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [ingresses, searchQuery, showSystemNamespaces]);

  const handleSelectIngress = async (summary: IngressSummary) => {
    // Salvar histórico atual no cache antes de trocar
    if (selectedIngress && history.length > 0) {
      const cacheKey = `${selectedIngress.namespace}/${selectedIngress.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedIngress(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getIngress(
        summary.cluster,
        summary.namespace,
        summary.name
      );
      setManifest(detail);
      const initialYaml = detail.yaml || "";
      setOriginalYaml(initialYaml);
      setShowLabels(false);
      setViewMode("editor");
      
      // Restaurar histórico do cache se existir
      const cacheKey = `${summary.namespace}/${summary.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setHistory(cached.history);
        setHistoryIndex(cached.index);
        // Atualizar editor com valor do histórico atual
        if (cached.index >= 0 && cached.index < cached.history.length) {
          setEditorValue(cached.history[cached.index]);
        }
      } else {
        // Inicializar histórico com valor inicial
        setHistory([initialYaml]);
        setHistoryIndex(0);
        setEditorValue(initialYaml);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // Atualizar histórico quando o editor muda (simples, sem adicionar ao histórico automaticamente)
  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
  }, []);

  // Adicionar ao histórico manualmente quando necessário
  const addToHistory = useCallback((value: string) => {
    setHistoryIndex((currentIndex) => {
      setHistory((prev) => {
        // Remover itens futuros se estamos no meio do histórico
        const newHistory = prev.slice(0, currentIndex + 1);
        
        // Só adicionar se for diferente do último item
        if (newHistory.length === 0 || newHistory[newHistory.length - 1] !== value) {
          newHistory.push(value);
          // Limitar histórico a 50 itens
          if (newHistory.length > 50) {
            newHistory.shift();
            return newHistory;
          }
        }
        return newHistory;
      });
      return Math.min(currentIndex + 1, 49);
    });
  }, []);

  // Undo
  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  // Redo
  const handleRedo = useCallback(() => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  const refreshIngresses = () => {
    if (!cluster) return;
    refetch();
  };

  const refreshManifest = async () => {
    if (!selectedIngress) return;
    
    setManifestLoading(true);
    try {
      // Buscar ingress atualizado do servidor
      const detail = await apiClient.getIngress(
        selectedIngress.cluster,
        selectedIngress.namespace,
        selectedIngress.name
      );
      setManifest(detail);
      const freshYaml = detail.yaml || "";
      
      // Atualizar com YAML fresco do servidor (ignorar cache)
      setOriginalYaml(freshYaml);
      setEditorValue(freshYaml);
      
      // Resetar histórico com o novo YAML
      setHistory([freshYaml]);
      setHistoryIndex(0);
      
      toast.success("Manifest recarregado do servidor");
    } catch (err) {
      toast.error("Falha ao recarregar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && !hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setViewMode(mode);
  };

  const handleShowDiffModal = async (fullscreen = false) => {
    if (!selectedIngress) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setIsDiffLoading(true);
    try {
      const diffResponse = await apiClient.diffIngress(originalYaml, editorValue, selectedIngress?.name);
      const unifiedDiff = diffResponse.unifiedDiff || "";
      const html = diff2html(unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffFullScreen(fullscreen);
      setDiffModalOpen(true);
    } catch (error) {
      toast.error("Erro ao gerar diff visual", {
        description: error instanceof Error ? error.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleDiffModalChange = (open: boolean) => {
    setDiffModalOpen(open);
    if (!open) {
      setDiffFullScreen(false);
    }
  };

  const toggleDiffFullScreen = () => {
    setDiffFullScreen((prev) => !prev);
  };

  const handleValidate = async () => {
    if (!selectedIngress) return;
    setIsValidating(true);
    try {
      await apiClient.validateIngress({
        cluster: selectedIngress.cluster,
        namespace: selectedIngress.namespace,
        yaml: editorValue,
        fieldManager: "web-ingress-editor",
      });
      toast.success("Dry-run bem-sucedido", {
        description: `${selectedIngress.namespace}/${selectedIngress.name}`,
      });
    } catch (err) {
      toast.error("Dry-run falhou", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleCancel = () => {
    // Restaurar o YAML original, descartando alterações
    setEditorValue(originalYaml);
    setViewMode("editor");
    setEditorFullScreen(false);
    toast.info("Alterações descartadas");
  };

  const handleApply = async () => {
    if (!selectedIngress) return;
    setIsApplying(true);
    try {
      await apiClient.applyIngress(
        selectedIngress.cluster,
        selectedIngress.namespace,
        selectedIngress.name,
        {
          yaml: editorValue,
          fieldManager: "web-ingress-editor",
          dryRun: false,
          force: true,
        }
      );
      toast.success("Ingress aplicado", {
        description: `${selectedIngress.namespace}/${selectedIngress.name}`,
      });
      
      // Recarregar manifest do servidor após aplicar
      await refreshManifest();
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      const resourceFullName = `Ingress ${selectedNamespace}/${selectedIngress.name}`;

      setErrorTitle(`Falha ao aplicar ${resourceFullName}`);
      setErrorMessage(errorMsg);
      setErrorDialogOpen(true);

      toast.error("Falha ao aplicar Ingress", { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  const openApplyConfirm = () => {
    if (!selectedIngress) return;
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
    if (!selectedIngress) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeIngress(selectedIngress.cluster, selectedIngress.namespace, selectedIngress.name);
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
    if (!selectedIngress) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
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

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Ingresses" : "Ocultar painel de Ingresses"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {namespaceSelector}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Mostrar namespaces e ingresses de sistema" : "Ocultar namespaces e ingresses de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={refreshIngresses} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4" />
      </Button>
      {collapseButton}
    </div>
  );

  const handleDeleteIngress = async () => {
    if (!selectedIngress) return;
    setIsDeleting(true);
    try {
      const response = await fetch(
        `/api/v1/ingresses/${selectedIngress.cluster}/${selectedIngress.namespace}/${selectedIngress.name}`,
        { method: "DELETE", headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` } }
      );
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || `HTTP ${response.status}`);
      }
      toast.success("Ingress deletado com sucesso!", { description: `${selectedIngress.namespace}/${selectedIngress.name}` });
      setSelectedIngress(null);
      setManifest(null);
      setEditorValue("");
      setOriginalYaml("");
      setDeleteConfirmOpen(false);
      await refetch();
    } catch (err) {
      toast.error("Erro ao deletar Ingress", { description: err instanceof Error ? err.message : "Erro desconhecido" });
    } finally {
      setIsDeleting(false);
    }
  };

  const rightTitleAction = (
    <div className="flex items-center gap-2">
      {selectedIngress && onOpenCompare && (
        <Button
          variant="ghost"
          size="sm"
          title="Abrir em Edição Lado a Lado"
          onClick={() => onOpenCompare({ type: "ingress", namespace: selectedIngress.namespace, name: selectedIngress.name })}
        >
          <SplitSquareHorizontal className="w-4 h-4" />
        </Button>
      )}
      {selectedIngress && (
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
      <Button
        variant="outline"
        size="sm"
        onClick={refreshManifest}
        disabled={!selectedIngress || manifestLoading}
      >
        <RefreshCcw className="w-4 h-4 mr-2" />
        Recarregar YAML
      </Button>
      {selectedIngress && (
        <ProtectedAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={manifestLoading}>
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => setDeleteConfirmOpen(true)}
                disabled={isDeleting}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Deletar Ingress
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ProtectedAction>
      )}
    </div>
  );

  const renderIngressList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Ingresses
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando Ingresses...
        </div>
      );
    }

    if (filteredIngresses.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {ingresses.length === 0
            ? "Nenhum Ingress encontrado"
            : "Nenhum Ingress corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredIngresses.map((ing) => {
          const isSelected =
            selectedIngress?.name === ing.name &&
            selectedIngress?.namespace === ing.namespace;
          return (
            <button
              key={`${ing.cluster}-${ing.namespace}-${ing.name}`}
              onClick={() => handleSelectIngress(ing)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="font-semibold text-sm flex items-center gap-2">
                <Network className="w-4 h-4" />
                {ing.name}
              </div>
              <div className="text-xs text-muted-foreground">{ing.namespace}</div>
              {ing.hosts && ing.hosts.length > 0 && (
                <div className="text-[11px] text-muted-foreground mt-1 flex items-center gap-1">
                  <span className="font-semibold">Hosts:</span> {ing.hosts.join(", ")}
                </div>
              )}
              {ing.ingressClass && (
                <div className="text-[11px] text-muted-foreground">
                  <span className="font-semibold">Class:</span> {ing.ingressClass}
                </div>
              )}
            </button>
          );
        })}
      </div>
    );
  };

  const hasChanges = editorValue !== originalYaml;

  const renderManifestPanel = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar Ingresses
        </div>
      );
    }

    if (!selectedIngress) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um Ingress para visualizar o manifesto
        </div>
      );
    }

    const updatedAt = selectedIngress.updatedAt
      ? new Date(selectedIngress.updatedAt).toLocaleString()
      : "--";

    // Keyboard shortcuts for normal editor
    const handleEditorKeyDown = (e: React.KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 'z' && !e.shiftKey) {
          e.preventDefault();
          handleUndo();
        } else if ((e.key === 'y') || (e.key === 'z' && e.shiftKey)) {
          e.preventDefault();
          handleRedo();
        } else if (e.key === 's') {
          e.preventDefault();
          // Ctrl+S: Salvar checkpoint local (não aplica)
          if (hasChanges) {
            // Adicionar checkpoint ao histórico
            addToHistory(editorValue);
            toast.success("Checkpoint salvo localmente", {
              description: "Alterações mantidas no histórico local. Use 'Aplicar' para confirmar no cluster.",
              style: {
                background: '#dcfce7',
                border: '1px solid #86efac',
                color: '#166534',
              },
            });
          }
        }
      } else if (e.key === 'Escape') {
        handleCancel();
      }
    };

    return (
      <div className="space-y-3" onKeyDown={handleEditorKeyDown} tabIndex={-1}>
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedIngress.cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
            <span className="font-medium">{selectedIngress.namespace}</span>
          </div>
          {selectedIngress.ingressClass && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Ingress Class</span>
              <span className="font-mono text-primary">{selectedIngress.ingressClass}</span>
            </div>
          )}
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">ResourceVersion</span>
            <span className="font-mono">{selectedIngress.resourceVersion || "--"}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Atualizado</span>
            <span className="font-medium">{updatedAt}</span>
          </div>
          {selectedIngress.labels && Object.keys(selectedIngress.labels).length > 0 && (
            <div className="flex flex-col">
              <button
                type="button"
                onClick={() => setShowLabels((prev) => !prev)}
                className="flex items-center gap-1 text-muted-foreground uppercase mb-0.5 hover:text-foreground"
              >
                {showLabels ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                <span>Labels</span>
              </button>
              {showLabels && (
                <div className="flex flex-wrap gap-1 mt-1">
                  {Object.entries(selectedIngress.labels).map(([key, value]) => (
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
        </div>

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
                  disabled={!selectedIngress}
                >
                  <Maximize2 className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
            {viewMode === "editor" && (
              <MonacoYamlEditor
                value={editorValue}
                onChange={handleEditorChange}
                height={520}
              />
            )}
            {viewMode === "diff" && (
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalYaml}
                value={editorValue}
                height={520}
                readOnly
              />
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal(false)}
              disabled={!selectedIngress || !hasChanges || isDiffLoading}
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
              disabled={!selectedIngress || !hasChanges || isDiffLoading}
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
                disabled={!selectedIngress || isValidating}
              >
                <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
              </Button>
            </ProtectedAction>
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={!selectedIngress || !hasChanges}
            >
              <X className="w-4 h-4 mr-2" /> Cancelar
            </Button>
            <ProtectedAction>
              <Button
                variant="default"
                size="sm"
                onClick={openApplyConfirm}
                disabled={!selectedIngress || isApplying || !hasChanges}
              >
                <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
              </Button>
            </ProtectedAction>
          </div>

        </div>
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
          placeholder="Buscar por nome, host, label..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderIngressList()}
    </div>
  );

  const renderDiffDialog = () => {
    const dialogSizeClass = diffFullScreen
      ? "w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none"
      : "max-w-6xl max-h-[85vh]";
    const scrollAreaHeight = diffFullScreen ? "h-[calc(100vh-8rem)]" : "h-[calc(85vh-8rem)]";

    return (
      <Dialog open={diffModalOpen} onOpenChange={handleDiffModalChange}>
        <DialogContent className={`bg-background border-border ${dialogSizeClass}`}>
          <DialogHeader className="border-b border-border pb-4 pr-12">
            <div className="flex items-start justify-between gap-4">
              <div>
                <DialogTitle className="text-xl font-semibold text-primary">
                  Diff Visual
                </DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground">
                  Comparação lado a lado entre o YAML original e a versão editada
                  {selectedIngress && ` • ${selectedIngress.namespace}/${selectedIngress.name}`}
                </DialogDescription>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={toggleDiffFullScreen}
                title={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                aria-label={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                className="gap-2"
              >
                {diffFullScreen ? (
                  <>
                    <Minimize2 className="w-4 h-4" />
                    <span>Sair de tela cheia</span>
                  </>
                ) : (
                  <>
                    <Maximize2 className="w-4 h-4" />
                    <span>Tela cheia</span>
                  </>
                )}
              </Button>
            </div>
          </DialogHeader>
          <ScrollArea className={`${scrollAreaHeight} w-full`}>
            <div className="p-4">
              {diffHtml ? (
                <div className="diff2html-dark" dangerouslySetInnerHTML={{ __html: diffHtml }} />
              ) : (
                <div className="flex items-center justify-center h-32 text-muted-foreground">
                  <p>Nenhum diff disponível.</p>
                </div>
              )}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    );
  };

  const renderEditorFullScreen = () => {
    if (!selectedIngress) return null;

    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 'z' && !e.shiftKey) {
          e.preventDefault();
          handleUndo();
        } else if ((e.key === 'y') || (e.key === 'z' && e.shiftKey)) {
          e.preventDefault();
          handleRedo();
        } else if (e.key === 's') {
          e.preventDefault();
          if (hasChanges) {
            addToHistory(editorValue);
            toast.success("Checkpoint salvo localmente", {
              description: "Alterações mantidas no histórico local. Use 'Aplicar' para confirmar no cluster.",
              style: {
                background: '#dcfce7',
                border: '1px solid #86efac',
                color: '#166534',
              },
            });
          }
        }
      } else if (e.key === 'Escape') {
        handleCancel();
      }
    };

    return (
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent 
          className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0"
          onKeyDown={handleKeyDown}
        >
          <div className="h-full flex flex-col">
            <DialogHeader className="border-b border-border px-6 py-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <DialogTitle className="text-xl font-semibold text-primary">
                    Editor YAML - Tela Cheia
                  </DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground">
                    {selectedIngress.namespace}/{selectedIngress.name}
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
                      disabled={!selectedIngress || isValidating}
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
                      disabled={!selectedIngress || isApplying || !hasChanges}
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
    );
  };

  const renderApplyConfirmDialog = () => {
    if (!selectedIngress) return null;

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
              Essa ação vai aplicar o Ingress diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {selectedIngress.cluster}</p>
              <p><span className="text-muted-foreground">Namespace:</span> {selectedIngress.namespace}</p>
              <p><span className="text-muted-foreground">Ingress:</span> {selectedIngress.name}</p>
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

  if (isSidebarCollapsed) {
    return (
      <>
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
                {renderManifestPanel()}
              </div>
            </div>
          </div>
        </div>

        {renderDiffDialog()}
        {renderEditorFullScreen()}
        {renderApplyConfirmDialog()}
      </>
    );
  }

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Ingresses",
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: "Visualização",
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {renderDiffDialog()}
      {renderEditorFullScreen()}
      {renderApplyConfirmDialog()}

      {/* Modal Delete Confirm */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <Trash2 className="w-5 h-5" />
              Deletar Ingress
            </DialogTitle>
            <DialogDescription>
              Esta acao e permanente e nao pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4 my-4">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Cluster:</span>
                <span className="text-sm text-muted-foreground">{selectedIngress?.cluster}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Namespace:</span>
                <span className="text-sm text-muted-foreground">{selectedIngress?.namespace}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Ingress:</span>
                <span className="text-sm font-semibold">{selectedIngress?.name}</span>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)} disabled={isDeleting}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDeleteIngress} disabled={isDeleting}>
              {isDeleting ? (<><Loader2 className="w-4 h-4 mr-2 animate-spin" />Deletando...</>) : (<><Trash2 className="w-4 h-4 mr-2" />Deletar Ingress</>)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {selectedIngress?.name}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {selectedIngress?.namespace}/{selectedIngress?.name}
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

    </>
  );
};
