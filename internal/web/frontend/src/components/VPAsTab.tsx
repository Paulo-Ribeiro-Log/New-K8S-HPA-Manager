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
  FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, X, ChevronLeft,
  FileText, MoreVertical, Trash2, SplitSquareHorizontal, AlertCircle,
  TrendingUp, Info, Copy
} from "lucide-react";
import { toast } from "sonner";

import type { Namespace, VPASummary, VPAManifest } from "@/lib/api/types";
import { useVPAs } from "@/hooks/useAPI";
import { VPAMonitorTable } from "@/components/VPAMonitorTable";
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

interface VPAsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

const updateModeBadgeClass = (mode: string) => {
  switch (mode?.toLowerCase()) {
    case "auto":      return "bg-green-500/20 text-green-400 border border-green-500/30";
    case "off":       return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
    case "initial":   return "bg-blue-500/20 text-blue-400 border border-blue-500/30";
    case "recreate":  return "bg-orange-500/20 text-orange-400 border border-orange-500/30";
    default:          return "bg-gray-500/20 text-gray-400 border border-gray-500/30";
  }
};

export const VPAsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: VPAsTabProps) => {
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>("vpas", "searchQuery", "");
  const [selectedVPA, setSelectedVPA] = usePersistedTabState<VPASummary | null>("vpas", "selectedVPA", null);
  const [viewMode, setViewMode] = usePersistedTabState<"editor" | "diff">("vpas", "viewMode", "editor");

  const [manifest, setManifest] = useState<VPAManifest | null>(null);
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
  const { vpas, crdNotInstalled, loading, error, refetch, silentRefetch } = useVPAs(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) toast.error("Erro ao carregar VPAs", { description: error });
  }, [error]);

  useEffect(() => {
    setSelectedVPA(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedNamespace]);

  const filteredVPAs = useMemo(() => {
    if (!vpas) return [];
    if (!searchQuery) return vpas;
    const query = searchQuery.toLowerCase();
    return vpas.filter(
      (v) =>
        v.name.toLowerCase().includes(query) ||
        v.namespace.toLowerCase().includes(query) ||
        v.targetRefName.toLowerCase().includes(query) ||
        v.updateMode.toLowerCase().includes(query)
    );
  }, [vpas, searchQuery]);

  const handleSelectVPA = async (summary: VPASummary) => {
    if (selectedVPA && history.length > 0) {
      const cacheKey = `${selectedVPA.namespace}/${selectedVPA.name}`;
      setHistoryCacheEntry(historyCache.current, cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedVPA(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getVPAManifest(summary.cluster, summary.namespace, summary.name);
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
      toast.error("Erro ao carregar manifesto VPA", {
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
    if (!selectedVPA) return;
    setIsValidating(true);
    try {
      await apiClient.validateVPA({
        cluster: selectedVPA.cluster,
        namespace: selectedVPA.namespace,
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
    if (!selectedVPA) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffVPA({
        originalYaml,
        updatedYaml: editorValue,
        fileName: `${selectedVPA.namespace}/${selectedVPA.name}`,
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
    if (!selectedVPA) return;
    setIsApplying(true);
    try {
      await apiClient.applyVPA(selectedVPA.cluster, selectedVPA.namespace, selectedVPA.name, {
        yaml: editorValue,
        force: true,
      });
      toast.success("VPA aplicado com sucesso");
      setOriginalYaml(editorValue);
      addToHistory(editorValue);
      setApplyConfirmOpen(false);
      refetch();
    } catch (err) {
      setApplyConfirmOpen(false);
      const msg = err instanceof Error ? err.message : String(err);
      setErrorTitle("Erro ao aplicar VPA");
      setErrorMessage(msg);
      setErrorDialogOpen(true);
    } finally {
      setIsApplying(false);
    }
  };

  const fetchDescribe = async () => {
    if (!selectedVPA) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeVPA(
        selectedVPA.cluster,
        selectedVPA.namespace,
        selectedVPA.name
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
    if (!selectedVPA) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const handleDelete = async () => {
    if (!selectedVPA) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteVPA(selectedVPA.cluster, selectedVPA.namespace, selectedVPA.name);
      toast.success(`VPA ${selectedVPA.name} deletado com sucesso`);
      setDeleteConfirmOpen(false);
      setSelectedVPA(null);
      setManifest(null);
      setEditorValue("");
      refetch();
    } catch (err) {
      toast.error("Erro ao deletar VPA", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  const handleReloadYaml = async () => {
    if (!selectedVPA) return;
    setManifestLoading(true);
    try {
      const detail = await apiClient.getVPAManifest(selectedVPA.cluster, selectedVPA.namespace, selectedVPA.name);
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
    </div>
  );

  const leftContent = (
    <div className="space-y-3">
      {selectedVPA && (
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary font-medium">
          <TrendingUp className="w-3.5 h-3.5 flex-shrink-0" />
          <span className="truncate flex-1">{selectedVPA.namespace}/{selectedVPA.name}</span>
          <button onClick={handleClearSelection} className="flex-shrink-0 hover:text-foreground transition-colors" title="Voltar para lista">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
      {/* Namespace select + busca */}
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
          placeholder="Buscar VPAs..."
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
          {filteredVPAs.length} VPA(s) encontrado(s)
        </div>
      )}

      {crdNotInstalled ? (
        <div className="flex flex-col items-center gap-2 py-6 text-center text-muted-foreground">
          <Info className="h-8 w-8 opacity-50" />
          <p className="text-sm font-medium">VPA não instalado</p>
          <p className="text-xs">O CRD VerticalPodAutoscaler não está instalado neste cluster.</p>
        </div>
      ) : loading ? (
        <div className="flex justify-center py-6">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : filteredVPAs.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-6 text-muted-foreground">
          <TrendingUp className="h-8 w-8 opacity-40" />
          <p className="text-sm">Nenhum VPA encontrado</p>
        </div>
      ) : (
        <div className="space-y-1">
          {filteredVPAs.map((vpa) => {
            const isSelected =
              selectedVPA?.cluster === vpa.cluster &&
              selectedVPA?.namespace === vpa.namespace &&
              selectedVPA?.name === vpa.name;
            return (
              <div
                key={`${vpa.cluster}/${vpa.namespace}/${vpa.name}`}
                className={`p-2 rounded-md cursor-pointer transition-colors ${
                  isSelected ? "bg-accent" : "hover:bg-muted"
                }`}
                onClick={() => handleSelectVPA(vpa)}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="flex-1 min-w-0">
                    <span className="font-medium text-sm truncate block">{vpa.name}</span>
                    <span className="text-xs text-muted-foreground">{vpa.namespace}</span>
                  </div>
                  <div className="flex flex-col items-end gap-1 shrink-0">
                    <span className={`px-1.5 py-0.5 rounded text-xs font-medium ${updateModeBadgeClass(vpa.updateMode)}`}>
                      {vpa.updateMode || "N/A"}
                    </span>
                    {vpa.hasRecommendation && (
                      <span className="px-1.5 py-0.5 rounded text-xs bg-purple-500/20 text-purple-400 border border-purple-500/30">
                        Recomendação
                      </span>
                    )}
                  </div>
                </div>
                {vpa.targetRefName && (
                  <p className="text-xs text-muted-foreground mt-1 truncate">
                    → {vpa.targetRefKind}/{vpa.targetRefName}
                  </p>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );

  // ─── Right panel ─────────────────────────────────────────────────────────
  const handleClearSelection = () => {
    setSelectedVPA(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  };

  const rightTitlePrefix = selectedVPA ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  const rightTitleAction = selectedVPA ? (
    <div className="flex items-center gap-1">
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
              Deletar VPA
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </ProtectedAction>
    </div>
  ) : null;

  const renderManifestPanel = () => {
    if (!selectedVPA) {
      return (
        <VPAMonitorTable
          items={vpas ?? []}
          loading={loading}
          headerLabel={`${(vpas ?? []).length} VPA(s)`}
          onOpenEditor={handleSelectVPA}
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
              title={viewMode === "editor" ? "Modo Diff" : "Modo Editor"}
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

        {/* Botões de ação */}
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
          title: "VPAs",
          titleAction: leftTitleAction,
          content: leftContent,
        }}
        rightPanel={{
          title: selectedVPA ? `${selectedVPA.namespace}/${selectedVPA.name}` : "Visualização",
          titlePrefix: rightTitlePrefix,
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {/* Modal Diff */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className={diffFullScreen ? "max-w-[98vw] w-[98vw] h-[95vh]" : "max-w-4xl w-full"}>
          <DialogHeader>
            <DialogTitle className="flex items-center justify-between">
              Diff do VPA
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
              Tem certeza que deseja aplicar as alterações no VPA{" "}
              <strong>{selectedVPA?.name}</strong>?
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
              Deletar VPA
            </DialogTitle>
            <DialogDescription>
              Tem certeza que deseja deletar o VPA{" "}
              <strong>{selectedVPA?.name}</strong> no namespace{" "}
              <strong>{selectedVPA?.namespace}</strong>? Esta ação não pode ser desfeita.
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
            <DialogTitle>kubectl describe vpa {selectedVPA?.name}</DialogTitle>
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
              <span>{selectedVPA?.namespace}/{selectedVPA?.name}</span>
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
