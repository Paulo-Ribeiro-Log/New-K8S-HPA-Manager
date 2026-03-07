import { useState, useEffect, useCallback, useRef } from "react";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { SplitView } from "@/components/SplitView";
import { ProtectedAction } from "@/components/rbac";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Clock,
  Search,
  X,
  Eye,
  EyeOff,
  RefreshCcw,
  Loader2,
  Undo2,
  Redo2,
  SplitSquareHorizontal,
  FileDiff,
  CheckCircle2,
  Maximize2,
  Minimize2,
  FileText,
  Play,
  Pause,
  MoreVertical,
  Trash2,
  AlertTriangle,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { CronJob } from "@/lib/api/types";
import type { Namespace } from "@/lib/api/types";
import { usePersistedTabState } from "@/hooks/usePersistedTabState";

interface CronJobsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

const systemNamespaces = new Set([
  "kube-system", "kube-public", "kube-node-lease", "istio-system",
  "cert-manager", "monitoring", "flux-system", "gatekeeper-system",
  "elastic-system", "logging", "dynatrace",
]);

const MAX_HISTORY = 50;

export function CronJobsTab({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: CronJobsTabProps) {
  // ── State ─────────────────────────────────────────────────────────────────
  const [cronJobs, setCronJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  const [selectedCronJob, setSelectedCronJob] = usePersistedTabState<CronJob | null>("cronjobs-selected", null);
  const [originalYaml, setOriginalYaml] = useState("");
  const [editorValue, setEditorValue] = useState("");
  const [manifestLoading, setManifestLoading] = useState(false);
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");

  // History (Undo/Redo)
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const isUndoRedoRef = useRef(false);

  // Modal states
  const [diffHtml, setDiffHtml] = useState("");
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [describeOpen, setDescribeOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");
  const [errorOpen, setErrorOpen] = useState(false);
  const [triggerConfirmOpen, setTriggerConfirmOpen] = useState(false);
  const [isTriggering, setIsTriggering] = useState(false);
  const [isSuspending, setIsSuspending] = useState(false);

  // ── Computed ──────────────────────────────────────────────────────────────
  const hasChanges = editorValue !== originalYaml && editorValue !== "";

  const filteredNamespaces = namespaces.filter(
    (ns) => showSystemNamespaces || !systemNamespaces.has(ns.name)
  );

  const isAllNamespaces = !selectedNamespace || selectedNamespace === "__all__";

  const filteredCronJobs = cronJobs.filter((cj) => {
    const matchesNs = isAllNamespaces || cj.namespace === selectedNamespace;
    const matchesSearch =
      !searchQuery ||
      cj.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      cj.namespace.toLowerCase().includes(searchQuery.toLowerCase()) ||
      cj.schedule.toLowerCase().includes(searchQuery.toLowerCase());
    return matchesNs && matchesSearch;
  });

  // ── Data fetching ─────────────────────────────────────────────────────────
  const fetchCronJobs = useCallback(async () => {
    if (!cluster) return;
    setLoading(true);
    try {
      const nsList = !isAllNamespaces ? [selectedNamespace] : [];
      const data = await apiClient.getCronJobs(cluster, nsList.length ? nsList : undefined);
      setCronJobs(data);
    } catch (err) {
      toast.error("Erro ao carregar CronJobs", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setLoading(false);
    }
  }, [cluster, selectedNamespace]);

  useEffect(() => {
    fetchCronJobs();
  }, [fetchCronJobs]);

  // ── Manifest loading ───────────────────────────────────────────────────────
  const loadManifest = useCallback(async (cj: CronJob) => {
    setManifestLoading(true);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
    setViewMode("editor");
    try {
      const result = await apiClient.getCronJobManifest(cj.cluster || cluster, cj.namespace, cj.name);
      setOriginalYaml(result.yaml);
      setEditorValue(result.yaml);
      setHistory([result.yaml]);
      setHistoryIndex(0);
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  }, [cluster]);

  const handleSelectCronJob = (cj: CronJob) => {
    setSelectedCronJob(cj);
    loadManifest(cj);
  };

  // ── History ────────────────────────────────────────────────────────────────
  const addToHistory = (value: string) => {
    setHistory((prev) => {
      const newHistory = prev.slice(0, historyIndex + 1);
      newHistory.push(value);
      if (newHistory.length > MAX_HISTORY) newHistory.shift();
      return newHistory;
    });
    setHistoryIndex((prev) => Math.min(prev + 1, MAX_HISTORY - 1));
  };

  const handleEditorChange = (value: string) => {
    if (isUndoRedoRef.current) return;
    setEditorValue(value);
    addToHistory(value);
  };

  const handleUndo = () => {
    if (historyIndex <= 0) return;
    isUndoRedoRef.current = true;
    const newIndex = historyIndex - 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
    setTimeout(() => { isUndoRedoRef.current = false; }, 0);
  };

  const handleRedo = () => {
    if (historyIndex >= history.length - 1) return;
    isUndoRedoRef.current = true;
    const newIndex = historyIndex + 1;
    setHistoryIndex(newIndex);
    setEditorValue(history[newIndex]);
    setTimeout(() => { isUndoRedoRef.current = false; }, 0);
  };

  // ── Actions ────────────────────────────────────────────────────────────────
  const handleShowDiff = async () => {
    if (!selectedCronJob) return;
    setIsDiffLoading(true);
    try {
      const result = await apiClient.diffCronJob({
        originalYaml,
        updatedYaml: editorValue,
        fileName: `${selectedCronJob.namespace}/${selectedCronJob.name}`,
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

  const handleValidate = async () => {
    if (!selectedCronJob) return;
    setIsValidating(true);
    try {
      await apiClient.validateCronJob({
        cluster: selectedCronJob.cluster || cluster,
        namespace: selectedCronJob.namespace,
        name: selectedCronJob.name,
        yaml: editorValue,
      });
      toast.success("YAML válido", { description: "Dry-run executado com sucesso" });
    } catch (err) {
      toast.error("YAML inválido", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleApply = async () => {
    if (!selectedCronJob) return;
    setIsApplying(true);
    try {
      await apiClient.applyCronJob(selectedCronJob.cluster || cluster, selectedCronJob.namespace, selectedCronJob.name, {
        yaml: editorValue,
      });
      toast.success("CronJob aplicado com sucesso");
      setOriginalYaml(editorValue);
      addToHistory(editorValue);
      setApplyConfirmOpen(false);
      fetchCronJobs();
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Erro desconhecido");
      setApplyConfirmOpen(false);
      setErrorOpen(true);
    } finally {
      setIsApplying(false);
    }
  };

  const handleCancel = () => {
    setEditorValue(originalYaml);
    addToHistory(originalYaml);
    setViewMode("editor");
  };

  const handleDescribe = async () => {
    if (!selectedCronJob) return;
    setDescribeLoading(true);
    setDescribeOpen(true);
    try {
      const result = await apiClient.describeCronJob(
        selectedCronJob.cluster || cluster,
        selectedCronJob.namespace,
        selectedCronJob.name
      );
      setDescribeContent(result.describe);
    } catch (err) {
      setDescribeContent(`Erro: ${err instanceof Error ? err.message : "Erro desconhecido"}`);
    } finally {
      setDescribeLoading(false);
    }
  };

  const handleTrigger = async () => {
    if (!selectedCronJob) return;
    setIsTriggering(true);
    try {
      const result = await apiClient.triggerCronJob(
        selectedCronJob.cluster || cluster,
        selectedCronJob.namespace,
        selectedCronJob.name
      );
      toast.success("Job disparado com sucesso", {
        description: `Job criado: ${result.jobName}`,
      });
      setTriggerConfirmOpen(false);
      fetchCronJobs();
    } catch (err) {
      toast.error("Erro ao disparar Job", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
      setTriggerConfirmOpen(false);
    } finally {
      setIsTriggering(false);
    }
  };

  const handleToggleSuspend = async (suspend: boolean) => {
    if (!selectedCronJob) return;
    setIsSuspending(true);
    try {
      await apiClient.updateCronJob(
        selectedCronJob.cluster || cluster,
        selectedCronJob.namespace,
        selectedCronJob.name,
        { suspend }
      );
      toast.success(suspend ? "CronJob suspenso" : "CronJob ativado", {
        description: `${selectedCronJob.namespace}/${selectedCronJob.name}`,
      });
      fetchCronJobs();
      // Reload manifest to reflect suspend change
      loadManifest({ ...selectedCronJob, suspend });
    } catch (err) {
      toast.error("Erro ao atualizar CronJob", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsSuspending(false);
    }
  };

  const handleReloadYaml = () => {
    if (!selectedCronJob) return;
    loadManifest(selectedCronJob);
    toast.info("YAML recarregado");
  };

  // ── Status helpers ─────────────────────────────────────────────────────────
  const getStatusBadge = (cj: CronJob) => {
    if (cj.suspend) {
      return <Badge variant="secondary" className="bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400 text-xs">Suspenso</Badge>;
    }
    if (cj.active_jobs > 0) {
      return <Badge variant="secondary" className="bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400 text-xs">Executando</Badge>;
    }
    return <Badge variant="secondary" className="bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400 text-xs">Ativo</Badge>;
  };

  const isSuspended = selectedCronJob?.suspend === true;

  // ── LEFT PANEL ─────────────────────────────────────────────────────────────
  const leftTitleAction = (
    <div className="flex items-center gap-1">
      <Button
        variant="ghost" size="sm" className="h-7 px-2 text-xs"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="ghost" size="sm" className="h-7 px-2" onClick={fetchCronJobs} title="Atualizar">
        <RefreshCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
      </Button>
    </div>
  );

  const leftContent = (
    <div className="flex flex-col h-full gap-2 p-1">
      {/* Namespace Select */}
      <Select value={selectedNamespace || "__all__"} onValueChange={onNamespaceChange}>
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

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Buscar CronJob..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="pl-8 h-8"
        />
        {searchQuery && (
          <Button variant="ghost" size="sm" className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6" onClick={() => setSearchQuery("")}>
            <X className="h-3 w-3" />
          </Button>
        )}
      </div>

      {/* List */}
      <ScrollArea className="flex-1">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : filteredCronJobs.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 gap-2 text-muted-foreground">
            <Clock className="h-8 w-8 opacity-30" />
            <p className="text-sm">Nenhum CronJob encontrado</p>
          </div>
        ) : (
          <div className="space-y-1 pr-1">
            {filteredCronJobs.map((cj) => {
              const isSelected =
                selectedCronJob?.name === cj.name &&
                selectedCronJob?.namespace === cj.namespace;
              return (
                <div
                  key={`${cj.namespace}/${cj.name}`}
                  onClick={() => handleSelectCronJob(cj)}
                  className={`
                    p-2.5 rounded-lg cursor-pointer border transition-all
                    ${isSelected
                      ? "border-primary bg-accent shadow-sm"
                      : "border-border/50 hover:border-primary/50 hover:bg-muted/50"
                    }
                  `}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <p className="font-medium text-sm truncate">{cj.name}</p>
                      <p className="text-xs text-muted-foreground truncate">{cj.namespace}</p>
                    </div>
                    {getStatusBadge(cj)}
                  </div>
                  <div className="mt-1.5 flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock className="h-3 w-3 flex-shrink-0" />
                    <span className="font-mono truncate">{cj.schedule}</span>
                  </div>
                  {cj.last_schedule_time && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      Última: {cj.last_schedule_time}
                    </p>
                  )}
                  {cj.active_jobs > 0 && (
                    <Badge variant="outline" className="mt-1 text-xs h-4">
                      {cj.active_jobs} ativo{cj.active_jobs > 1 ? "s" : ""}
                    </Badge>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </ScrollArea>
    </div>
  );

  // ── RIGHT PANEL ────────────────────────────────────────────────────────────
  const rightTitleAction = selectedCronJob ? (
    <div className="flex items-center gap-1 flex-wrap">
      {/* Suspend / Ativar */}
      <ProtectedAction>
        {isSuspended ? (
          <Button
            variant="outline" size="sm" className="h-7 text-xs text-green-600 border-green-300"
            onClick={() => handleToggleSuspend(false)}
            disabled={isSuspending}
            title="Ativar CronJob"
          >
            {isSuspending ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : <Play className="h-3 w-3 mr-1" />}
            Ativar
          </Button>
        ) : (
          <Button
            variant="outline" size="sm" className="h-7 text-xs text-orange-600 border-orange-300"
            onClick={() => handleToggleSuspend(true)}
            disabled={isSuspending}
            title="Suspender CronJob"
          >
            {isSuspending ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : <Pause className="h-3 w-3 mr-1" />}
            Suspender
          </Button>
        )}
      </ProtectedAction>

      {/* Trigger */}
      <ProtectedAction>
        <Button
          variant="outline" size="sm" className="h-7 text-xs"
          onClick={() => setTriggerConfirmOpen(true)}
          title="Disparar Job manualmente"
        >
          <Play className="h-3 w-3 mr-1" />
          Trigger
        </Button>
      </ProtectedAction>

      {/* Describe */}
      <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={handleDescribe}>
        <FileText className="h-3 w-3 mr-1" />
        Describe
      </Button>

      {/* Reload */}
      <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={handleReloadYaml} title="Recarregar YAML">
        <RefreshCcw className="h-3 w-3" />
      </Button>

      {/* 3-dot menu */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="h-7 w-7 p-0">
            <MoreVertical className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <ProtectedAction>
            <DropdownMenuItem
              className="text-red-600 cursor-pointer"
              onClick={() => setDeleteConfirmOpen(true)}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Deletar CronJob
            </DropdownMenuItem>
          </ProtectedAction>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  ) : null;

  const renderManifestPanel = () => {
    if (!selectedCronJob) {
      return (
        <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground">
          <Clock className="h-12 w-12 opacity-30" />
          <p className="text-sm">Selecione um CronJob para visualizar o manifesto</p>
        </div>
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
            onClick={handleCancel}
            disabled={!hasChanges || isApplying}
          >
            Cancelar
          </Button>
          <ProtectedAction>
            <Button
              size="sm"
              onClick={() => setApplyConfirmOpen(true)}
              disabled={!hasChanges || isApplying}
            >
              {isApplying && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Aplicar
            </Button>
          </ProtectedAction>
        </div>
      </div>
    );
  };

  // ── RENDER ─────────────────────────────────────────────────────────────────
  return (
    <>
      <SplitView
        leftPanel={{ title: "CronJobs", titleAction: leftTitleAction, content: leftContent }}
        rightPanel={{
          title: selectedCronJob ? `${selectedCronJob.namespace}/${selectedCronJob.name}` : "Visualização",
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {/* Modal Diff */}
      <Dialog open={diffModalOpen} onOpenChange={setDiffModalOpen}>
        <DialogContent className="max-w-5xl w-[90vw] max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Diff — {selectedCronJob?.name}</DialogTitle>
          </DialogHeader>
          <ScrollArea className="flex-1 overflow-auto">
            <div
              className="diff2html-container text-xs"
              dangerouslySetInnerHTML={{ __html: diffHtml }}
            />
          </ScrollArea>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDiffModalOpen(false)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Confirmar Apply */}
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar aplicação</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja aplicar as alterações no CronJob{" "}
              <strong>{selectedCronJob?.name}</strong>?
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

      {/* Modal Confirmar Trigger */}
      <Dialog open={triggerConfirmOpen} onOpenChange={setTriggerConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Disparar Job manualmente</DialogTitle>
            <DialogDescription>
              Criar um Job manualmente a partir do CronJob{" "}
              <strong>{selectedCronJob?.name}</strong> ({selectedCronJob?.namespace})?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setTriggerConfirmOpen(false)} disabled={isTriggering}>
              Cancelar
            </Button>
            <Button onClick={handleTrigger} disabled={isTriggering}>
              {isTriggering ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Play className="h-4 w-4 mr-2" />}
              Disparar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Confirmar Delete */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-red-600">
              <AlertTriangle className="h-5 w-5" />
              Deletar CronJob
            </DialogTitle>
            <DialogDescription>
              Essa ação é irreversível. Deseja deletar o CronJob{" "}
              <strong>{selectedCronJob?.name}</strong> do namespace{" "}
              <strong>{selectedCronJob?.namespace}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteConfirmOpen(false)} disabled={isDeleting}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              disabled={isDeleting}
              onClick={async () => {
                if (!selectedCronJob) return;
                setIsDeleting(true);
                try {
                  // Usar update com delete via kubectl — não há endpoint delete ainda,
                  // mas podemos reutilizar o padrão via kubectl describe / apply
                  // Por ora usamos o updateCronJob para sinalizar (placeholder — adicionar endpoint delete se necessário)
                  toast.info("Delete não implementado ainda via UI. Use kubectl.");
                  setDeleteConfirmOpen(false);
                } finally {
                  setIsDeleting(false);
                }
              }}
            >
              {isDeleting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Deletar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Describe */}
      <Dialog open={describeOpen} onOpenChange={setDescribeOpen}>
        <DialogContent className="max-w-3xl w-[85vw] max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>kubectl describe — {selectedCronJob?.name}</DialogTitle>
          </DialogHeader>
          <ScrollArea className="flex-1 max-h-[60vh]">
            {describeLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap break-words p-2">
                {describeContent}
              </pre>
            )}
          </ScrollArea>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDescribeOpen(false)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Editor Fullscreen */}
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent className="max-w-[98vw] w-[98vw] h-[95vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center justify-between">
              <span>{selectedCronJob?.namespace}/{selectedCronJob?.name}</span>
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
      <Dialog open={errorOpen} onOpenChange={setErrorOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-red-600">
              <TriangleAlert className="h-5 w-5" />
              Erro ao aplicar
            </DialogTitle>
            <DialogDescription className="text-sm font-mono whitespace-pre-wrap break-words">
              {errorMessage}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setErrorOpen(false)}>Fechar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
