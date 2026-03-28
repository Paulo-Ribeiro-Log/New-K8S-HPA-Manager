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
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, ChevronLeft, FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, X, FileText, Server, MoreVertical, Trash2, RotateCw, SplitSquareHorizontal, AlertCircle, Copy } from "lucide-react";
import { toast } from "sonner";
import yaml from "js-yaml";

import type {
  Namespace,
  DaemonSetSummary,
  DaemonSetManifest,
} from "@/lib/api/types";
import { useDaemonSets } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { setHistoryCacheEntry } from "@/lib/historyCache";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
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
import { DaemonSetMonitorTable } from "@/components/DaemonSetMonitorTable";

// Função para formatar versão de x-x-x-x para x.x.x-x (semver)
const formatVersion = (version: string | undefined): string => {
  if (!version) return '';
  const parts = version.split('-');
  if (parts.length === 4 && parts.every(p => /^\d+$/.test(p))) {
    return `${parts[0]}.${parts[1]}.${parts[2]}-${parts[3]}`;
  }
  if (parts.length >= 3 && parts.slice(0, 3).every(p => /^\d+$/.test(p))) {
    const semver = `${parts[0]}.${parts[1]}.${parts[2]}`;
    if (parts.length > 3) {
      return `${semver}-${parts.slice(3).join('-')}`;
    }
    return semver;
  }
  return version;
};

interface DaemonSetsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onOpenCompare?: (initial: { type: "daemonset"; namespace: string; name: string }) => void;
}

export const DaemonSetsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onOpenCompare,
}: DaemonSetsTabProps) => {
  // Estados com persistência entre trocas de aba
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>('daemonsets', 'searchQuery', "");
  const [selectedDaemonSet, setSelectedDaemonSet] = usePersistedTabState<DaemonSetSummary | null>('daemonsets', 'selectedDaemonSet', null);
  const [showLabels, setShowLabels] = usePersistedTabState<boolean>('daemonsets', 'showLabels', false);
  const [viewMode, setViewMode] = usePersistedTabState<"editor" | "diff">('daemonsets', 'viewMode', "editor");

  // Estados locais (não persistidos)
  const [manifest, setManifest] = useState<DaemonSetManifest | null>(null);
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
  const [rolloutConfirmOpen, setRolloutConfirmOpen] = useState(false);
  const [isRestarting, setIsRestarting] = useState(false);

  // Error Dialog para exibir erros de apply de forma mais proeminente
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  // Helper: Detectar daemonset problemático
  const isDaemonSetProblematic = (ds: DaemonSetSummary): boolean => {
    if (ds.numberReady < ds.desiredNumberScheduled) return true;
    if (ds.numberAvailable < ds.desiredNumberScheduled) return true;
    return false;
  };

  // Helper: Retorna info de status para o card
  const getDaemonSetStatusInfo = (ds: DaemonSetSummary): { label: string; color: string } | null => {
    if (ds.numberMisscheduled > 0) {
      return { label: `Misscheduled: ${ds.numberMisscheduled}`, color: "bg-orange-500/20 text-orange-400" };
    }
    if (ds.numberReady < ds.desiredNumberScheduled) {
      return { label: `${ds.desiredNumberScheduled - ds.numberReady} pods pendentes`, color: "bg-yellow-500/20 text-yellow-400" };
    }
    if (ds.numberAvailable < ds.desiredNumberScheduled) {
      return { label: `${ds.desiredNumberScheduled - ds.numberAvailable} indisponíveis`, color: "bg-yellow-500/20 text-yellow-400" };
    }
    return null;
  };

  // Undo/Redo history with persistent cache
  const historyCache = useRef<Map<string, { history: string[], index: number }>>(new Map());
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
    if (!exists) {
      onNamespaceChange("");
    }
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { daemonsets, loading, error, refetch, silentRefetch } = useDaemonSets(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar DaemonSets", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedDaemonSet(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(false);
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedNamespace]);

  useEffect(() => {
    if (!selectedDaemonSet || !daemonsets) return;
    const latest = daemonsets.find(
      (ds) =>
        ds.cluster === selectedDaemonSet.cluster &&
        ds.namespace === selectedDaemonSet.namespace &&
        ds.name === selectedDaemonSet.name
    );
    if (!latest) return;
    const hasChanged =
      latest.resourceVersion !== selectedDaemonSet.resourceVersion ||
      latest.numberReady !== selectedDaemonSet.numberReady ||
      latest.numberAvailable !== selectedDaemonSet.numberAvailable;
    if (hasChanged) {
      setSelectedDaemonSet(latest);
    }
  }, [daemonsets, selectedDaemonSet, setSelectedDaemonSet]);

  const filteredDaemonSets = useMemo(() => {
    if (!daemonsets) return [];
    if (!searchQuery) return daemonsets;
    const query = searchQuery.toLowerCase();
    return daemonsets.filter((ds) => {
      return (
        ds.name.toLowerCase().includes(query) ||
        ds.namespace.toLowerCase().includes(query) ||
        Object.entries(ds.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [daemonsets, searchQuery]);

  const handleSelectDaemonSet = async (summary: DaemonSetSummary) => {
    // Salvar histórico atual no cache antes de trocar
    if (selectedDaemonSet && history.length > 0) {
      const cacheKey = `${selectedDaemonSet.namespace}/${selectedDaemonSet.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedDaemonSet(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getDaemonSet(
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
        if (cached.index >= 0 && cached.index < cached.history.length) {
          setEditorValue(cached.history[cached.index]);
        }
      } else {
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

  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
  }, []);

  const addToHistory = useCallback((value: string) => {
    setHistoryIndex((currentIndex) => {
      setHistory((prev) => {
        const newHistory = prev.slice(0, currentIndex + 1);
        if (newHistory.length === 0 || newHistory[newHistory.length - 1] !== value) {
          newHistory.push(value);
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

  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  const handleRedo = useCallback(() => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  const refreshDaemonSets = () => {
    if (!cluster) return;
    refetch();
  };

  const handleClearSelection = () => {
    setSelectedDaemonSet(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
  };

  const handleValidate = async () => {
    if (!selectedDaemonSet || !editorValue) return;
    setIsValidating(true);
    try {
      yaml.load(editorValue);
      await apiClient.validateDaemonSet({
        cluster: selectedDaemonSet.cluster,
        namespace: selectedDaemonSet.namespace,
        yaml: editorValue,
      });
      toast.success("YAML válido", {
        description: "O manifesto é válido e pode ser aplicado.",
      });
    } catch (err) {
      toast.error("YAML inválido", {
        description: err instanceof Error ? err.message : "Erro de validação",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleShowDiff = async () => {
    if (!selectedDaemonSet) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffDaemonSet({
        originalYaml,
        updatedYaml: editorValue,
        fileName: selectedDaemonSet.name,
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
    if (!selectedDaemonSet || !editorValue) return;
    setIsApplying(true);
    try {
      await apiClient.applyDaemonSet(
        selectedDaemonSet.cluster,
        selectedDaemonSet.namespace,
        selectedDaemonSet.name,
        { yaml: editorValue, dryRun: false, force: true }
      );
      toast.success("DaemonSet aplicado com sucesso");
      setApplyConfirmOpen(false);
      setOriginalYaml(editorValue);
      addToHistory(editorValue);
      refetch();
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      const resourceFullName = `DaemonSet ${selectedNamespace}/${selectedDaemonSet.name}`;

      setErrorTitle(`Falha ao aplicar ${resourceFullName}`);
      setErrorMessage(errorMsg);
      setErrorDialogOpen(true);

      toast.error("Falha ao aplicar DaemonSet", { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  const fetchDescribe = async () => {
    if (!selectedDaemonSet) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeDaemonSet(
        selectedDaemonSet.cluster,
        selectedDaemonSet.namespace,
        selectedDaemonSet.name
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
    if (!selectedDaemonSet) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const handleDelete = async () => {
    if (!selectedDaemonSet) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteDaemonSet(
        selectedDaemonSet.cluster,
        selectedDaemonSet.namespace,
        selectedDaemonSet.name
      );
      toast.success("DaemonSet deletado com sucesso");
      setDeleteConfirmOpen(false);
      setSelectedDaemonSet(null);
      setManifest(null);
      refetch();
    } catch (err) {
      toast.error("Erro ao deletar DaemonSet", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  const handleRolloutRestart = async () => {
    if (!selectedDaemonSet) return;
    setIsRestarting(true);
    try {
      await apiClient.restartDaemonSet(
        selectedDaemonSet.cluster,
        selectedDaemonSet.namespace,
        selectedDaemonSet.name
      );
      toast.success("Rollout restart iniciado", {
        description: "Os pods serão recriados gradualmente.",
      });
      setRolloutConfirmOpen(false);
      refetch();
    } catch (err) {
      toast.error("Erro ao executar rollout restart", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsRestarting(false);
    }
  };

  const handleCancel = () => {
    setEditorValue(originalYaml);
    setHistory([originalYaml]);
    setHistoryIndex(0);
  };

  const hasChanges = editorValue !== originalYaml;

  // Namespace Selector
  const namespaceSelector = (
    <Select value={selectedNamespace || "__all__"} onValueChange={(v) => onNamespaceChange(v === "__all__" ? "" : v)}>
      <SelectTrigger className="h-8 w-[180px]">
        <SelectValue placeholder="Todos os namespaces" />
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
  );

  // Left Panel Title Action (ações no header da sidebar)
  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {namespaceSelector}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={refreshDaemonSets} disabled={loading}>
        <RefreshCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
      </Button>
    </div>
  );

  // Left Panel Content (lista de daemonsets)
  const leftContent = (
    <div className="space-y-3">
      {/* Breadcrumb de seleção atual */}
      {selectedDaemonSet && (
        <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-primary/10 border border-primary/20 text-xs">
          <Server className="h-3 w-3 text-primary flex-shrink-0" />
          <span className="flex-1 truncate font-medium text-primary">{selectedDaemonSet.name}</span>
          <button
            onClick={handleClearSelection}
            className="text-primary/60 hover:text-primary flex-shrink-0"
            title="Voltar para lista geral"
          >
            <X className="h-3 w-3" />
          </button>
        </div>
      )}
      {/* Search */}
      <div className="relative">
        <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Buscar..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-8 h-8"
        />
        {searchQuery && (
          <Button
            variant="ghost"
            size="icon"
            className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6"
            onClick={() => setSearchQuery("")}
          >
            <X className="h-3 w-3" />
          </Button>
        )}
      </div>

      {/* Contador */}
      {!loading && daemonsets && (
        <div className="text-xs text-muted-foreground">
          {filteredDaemonSets.length} DaemonSet(s) encontrado(s)
        </div>
      )}

      {/* List */}
      {loading ? (
        <div className="text-center text-muted-foreground py-4">
          <Loader2 className="h-6 w-6 animate-spin mx-auto mb-2" />
          Carregando...
        </div>
      ) : filteredDaemonSets.length === 0 ? (
        <div className="text-center text-muted-foreground py-4">
          Nenhum DaemonSet encontrado
        </div>
      ) : (
        <div className="space-y-1.5">
          {filteredDaemonSets.map((ds) => {
            const isSelected = selectedDaemonSet?.name === ds.name && selectedDaemonSet?.namespace === ds.namespace;
            const isProblematic = isDaemonSetProblematic(ds);
            const version = formatVersion(ds.labels?.["app.kubernetes.io/version"]);
            const statusInfo = getDaemonSetStatusInfo(ds);
            const replicaColor = isProblematic ? "text-red-400" : "text-green-400";

            return (
              <div
                key={`${ds.namespace}/${ds.name}`}
                className={`flex items-start gap-2 p-3 rounded-lg border transition-colors relative cursor-pointer ${
                  isSelected
                    ? "border-primary bg-primary/10"
                    : isProblematic
                    ? "border-red-500/40 hover:border-red-500/60 bg-red-500/5"
                    : "border-border/60 hover:border-primary/40"
                }`}
                onClick={() => handleSelectDaemonSet(ds)}
              >
                {isProblematic && (
                  <div className="absolute top-2 right-2 w-2 h-2 bg-red-500 rounded-full animate-pulse" title="DaemonSet com problemas" />
                )}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <Server className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                    <span className="font-semibold text-sm truncate">{ds.name}</span>
                  </div>
                  <div className="text-xs text-muted-foreground mt-0.5">{ds.namespace}</div>
                  {version && (
                    <div className="text-[10px] mt-0.5">
                      <span className="text-blue-400/80">{version}</span>
                    </div>
                  )}
                  <div className={`text-[11px] mt-1 font-medium ${replicaColor}`}>
                    {ds.numberReady}/{ds.desiredNumberScheduled} ready • {ds.numberAvailable}/{ds.desiredNumberScheduled} available
                  </div>
                  {statusInfo && (
                    <div className={`text-[10px] mt-1 px-1.5 py-0.5 rounded inline-block font-medium ${statusInfo.color}`}>
                      {statusInfo.label}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );

  // Função para recarregar o manifesto do DaemonSet selecionado
  const refreshManifest = async () => {
    if (!selectedDaemonSet) return;
    setManifestLoading(true);
    try {
      const detail = await apiClient.getDaemonSet(
        selectedDaemonSet.cluster,
        selectedDaemonSet.namespace,
        selectedDaemonSet.name
      );
      setManifest(detail);
      const newYaml = detail.yaml || "";
      setOriginalYaml(newYaml);
      setEditorValue(newYaml);
      setHistory([newYaml]);
      setHistoryIndex(0);
      toast.success("Manifesto recarregado");
    } catch (err) {
      toast.error("Erro ao recarregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const rightTitlePrefix = selectedDaemonSet ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  // Right Panel Title Action (ações no header do editor)
  const rightTitleAction = (
    <div className="flex gap-2">
      {selectedDaemonSet && onOpenCompare && (
        <Button
          variant="ghost"
          size="sm"
          title="Abrir em Edição Lado a Lado"
          onClick={() => onOpenCompare({ type: "daemonset", namespace: selectedDaemonSet.namespace, name: selectedDaemonSet.name })}
        >
          <SplitSquareHorizontal className="w-4 h-4" />
        </Button>
      )}
      {selectedDaemonSet && (
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
        disabled={!selectedDaemonSet || manifestLoading}
      >
        <RefreshCcw className="w-4 h-4 mr-2" />
        Recarregar YAML
      </Button>
      {selectedDaemonSet && (
        <ProtectedAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                size="sm"
                disabled={manifestLoading}
              >
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => setRolloutConfirmOpen(true)}
                disabled={isRestarting}
              >
                <RotateCw className="w-4 h-4 mr-2" />
                Rollout Restart
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() => setDeleteConfirmOpen(true)}
                disabled={isDeleting}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Deletar DaemonSet
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ProtectedAction>
      )}
    </div>
  );

  // Right Panel Content (editor de manifesto)
  const renderManifestPanel = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar DaemonSets
        </div>
      );
    }

    if (!selectedDaemonSet) {
      return (
        <div className="flex flex-col h-full p-2">
          <DaemonSetMonitorTable
            daemonsets={filteredDaemonSets}
            loading={loading}
            headerLabel={selectedNamespace ? `${selectedNamespace} — daemonsets (${filteredDaemonSets.length})` : `daemonsets (${filteredDaemonSets.length})`}
            onOpenEditor={(ds) => handleSelectDaemonSet(ds)}
            onRequestRefresh={silentRefetch}
          />
        </div>
      );
    }

    // Extrair versão dos labels
    const appVersion = selectedDaemonSet.labels?.["app.kubernetes.io/version"] ||
                       selectedDaemonSet.labels?.["version"] ||
                       selectedDaemonSet.labels?.["app.version"];

    // Formatar data de atualização
    const updatedAt = manifest?.metadata?.creationTimestamp
      ? new Date(manifest.metadata.creationTimestamp).toLocaleString()
      : "--";

    return (
      <div className="space-y-3">
        {/* Card de informações detalhadas */}
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedDaemonSet.cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
            <span className="font-medium">{selectedDaemonSet.namespace}</span>
          </div>
          {appVersion && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Versão</span>
              <span className="font-mono text-primary">{formatVersion(appVersion)}</span>
            </div>
          )}
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Ready</span>
            <span className="font-mono">
              {selectedDaemonSet.numberReady} / {selectedDaemonSet.desiredNumberScheduled}
            </span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Atualizado</span>
            <span className="font-medium">{updatedAt}</span>
          </div>
          {selectedDaemonSet.labels && Object.keys(selectedDaemonSet.labels).length > 0 && (
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
                  {Object.entries(selectedDaemonSet.labels).map(([key, value]) => (
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

        {/* Seção Manifesto YAML */}
        <div className="space-y-3">
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Manifesto YAML</p>
              <div className="flex items-center gap-2">
                {manifestLoading && (
                  <span className="text-xs text-muted-foreground">Carregando...</span>
                )}
                {/* Undo/Redo buttons */}
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
                {/* Editor/Diff toggle */}
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={() => setViewMode("editor")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    }`}
                  >
                    Editor
                  </button>
                  <button
                    type="button"
                    onClick={() => setViewMode("diff")}
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
                  disabled={!selectedDaemonSet}
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

          {/* Botões de ação */}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleShowDiff}
              disabled={!selectedDaemonSet || !hasChanges || isDiffLoading}
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
              onClick={() => setDiffFullScreen(true)}
              disabled={!selectedDaemonSet || !hasChanges || isDiffLoading}
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
                disabled={!selectedDaemonSet || isValidating}
              >
                <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
              </Button>
            </ProtectedAction>
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={!selectedDaemonSet || !hasChanges}
            >
              <X className="w-4 h-4 mr-2" /> Cancelar
            </Button>
            <ProtectedAction>
              <Button
                variant="default"
                size="sm"
                onClick={() => setApplyConfirmOpen(true)}
                disabled={!selectedDaemonSet || isApplying || !hasChanges}
              >
                {isApplying && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
              </Button>
            </ProtectedAction>
          </div>
        </div>
      </div>
    );
  };

  return (
    <>
      <SplitView
        leftPanel={{
          title: "DaemonSets",
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedDaemonSet ? `${selectedDaemonSet.namespace}/${selectedDaemonSet.name}` : "Visualização",
          titlePrefix: rightTitlePrefix,
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {/* Diff Modal */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className={diffFullScreen ? "max-w-[95vw] max-h-[95vh] w-[95vw] h-[95vh]" : "max-w-4xl max-h-[80vh]"}>
          <DialogHeader>
            <div className="flex items-center justify-between">
              <DialogTitle>Diferenças</DialogTitle>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setDiffFullScreen(!diffFullScreen)}
              >
                {diffFullScreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
              </Button>
            </div>
          </DialogHeader>
          <ScrollArea className={diffFullScreen ? "h-[calc(95vh-100px)]" : "h-[60vh]"}>
            <div dangerouslySetInnerHTML={{ __html: diffHtml }} />
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Describe Modal */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-4xl max-h-[80vh]">
          <DialogHeader>
            <DialogTitle>kubectl describe daemonset {selectedDaemonSet?.name}</DialogTitle>
          </DialogHeader>
          <ScrollArea className="h-[60vh]">
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

      {/* Apply Confirm Modal */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Aplicação</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja aplicar as alterações no DaemonSet{" "}
              <strong>{selectedDaemonSet?.name}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setApplyConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button onClick={handleApply} disabled={isApplying}>
              {isApplying && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
              Aplicar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirm Modal */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Exclusão</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o DaemonSet{" "}
              <strong>{selectedDaemonSet?.name}</strong>? Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
              {isDeleting && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
              Deletar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rollout Restart Confirm Modal */}
      <Dialog open={rolloutConfirmOpen} onOpenChange={setRolloutConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Rollout Restart</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja executar rollout restart no DaemonSet{" "}
              <strong>{selectedDaemonSet?.name}</strong>? Os pods serão recriados gradualmente.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRolloutConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button onClick={handleRolloutRestart} disabled={isRestarting}>
              {isRestarting && <Loader2 className="h-4 w-4 animate-spin mr-2" />}
              Reiniciar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Fullscreen Editor Modal */}
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent className="max-w-[95vw] max-h-[95vh] w-[95vw] h-[95vh]">
          <DialogHeader>
            <div className="flex items-center justify-between">
              <DialogTitle>{selectedDaemonSet?.name}</DialogTitle>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={handleUndo}
                  disabled={!canUndo}
                  title="Desfazer"
                  className="h-7 w-7"
                >
                  <Undo2 className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={handleRedo}
                  disabled={!canRedo}
                  title="Refazer"
                  className="h-7 w-7"
                >
                  <Redo2 className="h-4 w-4" />
                </Button>
                <Button
                  variant={viewMode === "editor" ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => setViewMode("editor")}
                  className="h-7"
                >
                  Editor
                </Button>
                <Button
                  variant={viewMode === "diff" ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => setViewMode("diff")}
                  disabled={!hasChanges}
                  className="h-7"
                >
                  Diff
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleCancel}
                  disabled={!hasChanges}
                  className="h-7"
                >
                  Cancelar
                </Button>
                <ProtectedAction>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => setApplyConfirmOpen(true)}
                    disabled={!hasChanges || isApplying}
                    className="h-7"
                  >
                    Aplicar
                  </Button>
                </ProtectedAction>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setEditorFullScreen(false)}
                  className="h-7 w-7"
                >
                  <Minimize2 className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </DialogHeader>
          <div className="flex-1 p-4">
            {viewMode === "editor" && (
              <MonacoYamlEditor
                value={editorValue}
                onChange={handleEditorChange}
                height="calc(95vh - 140px)"
              />
            )}
            {viewMode === "diff" && (
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalYaml}
                value={editorValue}
                height="calc(95vh - 140px)"
                readOnly
              />
            )}
          </div>
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

export default DaemonSetsTab;
