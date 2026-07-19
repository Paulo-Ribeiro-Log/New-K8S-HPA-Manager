import { useState, useEffect, useCallback, useRef } from "react";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { SplitView } from "@/components/SplitView";
import { ProtectedAction } from "@/components/rbac";
import { useK8sPermissions } from "@/hooks/useK8sPermissions";
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
  Copy,
  ChevronLeft,
  Plus,
  GitBranch,
  ExternalLink,
  ChevronDown,
  ChevronUp,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { CronJob } from "@/lib/api/types";
import type { Namespace } from "@/lib/api/types";
import { usePersistedTabState } from "@/hooks/usePersistedTabState";
import { useRevealOnKeyChange } from "@/hooks/useRevealOnKeyChange";
import { CronJobMonitorTable } from "@/components/CronJobMonitorTable";

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
  const { permissions: k8sPerms } = useK8sPermissions(cluster, selectedNamespace || '');
  const canWriteCronJobs = selectedNamespace && selectedNamespace !== '__all__' ? k8sPerms.canWriteCronJobs : undefined;

  // ── State ─────────────────────────────────────────────────────────────────
  const [cronJobs, setCronJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  const [selectedCronJob, setSelectedCronJob] = usePersistedTabState<CronJob | null>("cronjobs", "selected", null);
  const leftListRef = useRef<HTMLDivElement>(null);
  useRevealOnKeyChange(
    leftListRef,
    selectedCronJob ? `${selectedCronJob.namespace}/${selectedCronJob.name}` : null
  );
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

  // Novo recurso batch modal (Job ou CronJob)
  const [newJobOpen, setNewJobOpen] = useState(false);
  const [newJobType, setNewJobType] = useState<"job" | "cronjob">("job");
  const [newJobYaml, setNewJobYaml] = useState("");
  const [newJobNamespace, setNewJobNamespace] = useState("");
  const [isCreatingJob, setIsCreatingJob] = useState(false);
  const [isLoadingJobTemplate, setIsLoadingJobTemplate] = useState(false);

  // GitHub commit section (dentro do modal Novo Job)
  const [ghSectionOpen, setGhSectionOpen] = useState(false);
  const [ghFolderUrl, setGhFolderUrl] = useState(() => localStorage.getItem("newjob_gh_folder") || "");
  const [ghFilename, setGhFilename] = useState("job.yaml");
  const [ghMessage, setGhMessage] = useState("");
  const [isCommitting, setIsCommitting] = useState(false);
  const [ghCommitResult, setGhCommitResult] = useState<{ fileUrl: string; commitUrl: string; created: boolean } | null>(null);

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

  const silentFetchCronJobs = useCallback(async () => {
    if (!cluster) return;
    try {
      const nsList = !isAllNamespaces ? [selectedNamespace] : [];
      const data = await apiClient.getCronJobs(cluster, nsList.length ? nsList : undefined);
      setCronJobs(data);
    } catch { /* ignore */ }
  }, [cluster, selectedNamespace, isAllNamespaces]);

  const handleClearSelection = () => {
    setSelectedCronJob(null);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
  };

  // ── Manifest loading ───────────────────────────────────────────────────────
  const loadManifest = useCallback(async (cj: CronJob) => {
    setManifestLoading(true);
    setEditorValue("");
    setOriginalYaml("");
    setHistory([]);
    setHistoryIndex(-1);
    setViewMode("editor");
    try {
      const result = await apiClient.getCronJobManifest(cluster, cj.namespace, cj.name);
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
        cluster: cluster,
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
      await apiClient.applyCronJob(cluster, selectedCronJob.namespace, selectedCronJob.name, {
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

  const fetchDescribe = useCallback(async () => {
    if (!selectedCronJob) return;
    setDescribeLoading(true);
    try {
      const result = await apiClient.describeCronJob(
        cluster,
        selectedCronJob.namespace,
        selectedCronJob.name
      );
      setDescribeContent(result.describe);
    } catch (err) {
      setDescribeContent(`Erro: ${err instanceof Error ? err.message : "Erro desconhecido"}`);
    } finally {
      setDescribeLoading(false);
    }
  }, [cluster, selectedCronJob]);

  const handleViewDescribe = async () => {
    setDescribeOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const handleTrigger = async () => {
    if (!selectedCronJob) return;
    setIsTriggering(true);
    try {
      const result = await apiClient.triggerCronJob(
        cluster,
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

  const defaultJobTemplate = () => `apiVersion: batch/v1
kind: Job
metadata:
  generateName: job-
spec:
  template:
    spec:
      containers:
        - name: job
          image: busybox:latest
          command: ["echo", "Hello from Job"]
      restartPolicy: Never
  backoffLimit: 3
`;

  const defaultCronJobTemplate = () => `apiVersion: batch/v1
kind: CronJob
metadata:
  name: meu-cronjob
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: job
              image: busybox:latest
              command: ["echo", "Hello from CronJob"]
          restartPolicy: Never
      backoffLimit: 3
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
`;

  const handleOpenNewJob = (type: "job" | "cronjob" = "job") => {
    const ns = selectedNamespace && selectedNamespace !== "__all__" ? selectedNamespace : "";
    setNewJobType(type);
    setNewJobNamespace(ns);
    setNewJobYaml(type === "cronjob" ? defaultCronJobTemplate() : defaultJobTemplate());
    setNewJobOpen(true);
  };

  const handleLoadJobTemplate = async () => {
    if (!selectedCronJob) return;
    setIsLoadingJobTemplate(true);
    try {
      const result = await apiClient.getJobTemplate(cluster, selectedCronJob.namespace, selectedCronJob.name);
      setNewJobYaml(result.yaml);
      setNewJobNamespace(selectedCronJob.namespace);
    } catch (err) {
      toast.error("Erro ao carregar template", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsLoadingJobTemplate(false);
    }
  };

  const handleCreateResource = async (dryRun = false) => {
    setIsCreatingJob(true);
    try {
      if (newJobType === "cronjob") {
        const result = await apiClient.createCronJob(cluster, newJobNamespace, newJobYaml, dryRun);
        if (dryRun) {
          toast.success("Validação OK", {
            description: `CronJob "${result.name}" seria criado em ${result.namespace} (schedule: ${result.schedule})`,
          });
        } else {
          toast.success("CronJob criado com sucesso", {
            description: `${result.namespace}/${result.name} · ${result.schedule}`,
          });
          setNewJobOpen(false);
          fetchCronJobs();
        }
      } else {
        const result = await apiClient.createJob(cluster, newJobNamespace, newJobYaml, dryRun);
        if (dryRun) {
          toast.success("Validação OK", {
            description: `Job "${result.name || "(gerado no apply)"}" seria criado em ${result.namespace}`,
          });
        } else {
          toast.success("Job criado com sucesso", {
            description: `${result.namespace}/${result.name}`,
          });
          setNewJobOpen(false);
          fetchCronJobs();
        }
      }
    } catch (err) {
      const label = newJobType === "cronjob" ? "CronJob" : "Job";
      toast.error(dryRun ? "Erro na validação" : `Erro ao criar ${label}`, {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCreatingJob(false);
    }
  };

  // Parseia URL de pasta do GitHub:
  // https://github.com/{owner}/{repo}/tree/{branch}/{path}
  const parseGitHubFolderUrl = (url: string) => {
    const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/tree\/([^/]+)\/?(.*)$/);
    if (!m) return null;
    return { owner: m[1], repo: m[2], branch: m[3], basePath: m[4] || "" };
  };

  const handleCommitToGitHub = async () => {
    const parsed = parseGitHubFolderUrl(ghFolderUrl);
    if (!parsed) {
      toast.error("URL de pasta inválida", {
        description: "Use o formato: https://github.com/org/repo/tree/branch/pasta",
      });
      return;
    }
    if (!ghFilename.trim()) {
      toast.error("Informe o nome do arquivo");
      return;
    }
    setIsCommitting(true);
    setGhCommitResult(null);
    try {
      const result = await apiClient.commitFileToGitHub({
        ...parsed,
        filename: ghFilename.trim(),
        content: newJobYaml,
        message: ghMessage.trim() || `chore: adiciona job em ${newJobNamespace || "kubernetes"}`,
      });
      setGhCommitResult({ fileUrl: result.file_url, commitUrl: result.commit_url, created: result.created });
      localStorage.setItem("newjob_gh_folder", ghFolderUrl);
      toast.success(result.created ? "Arquivo criado no GitHub" : "Arquivo atualizado no GitHub", {
        description: ghFilename,
      });
    } catch (err) {
      toast.error("Erro ao commitar no GitHub", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCommitting(false);
    }
  };

  const handleToggleSuspend = async (suspend: boolean) => {
    if (!selectedCronJob) return;
    setIsSuspending(true);
    try {
      await apiClient.updateCronJob(
        cluster,
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
      <ProtectedAction>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="h-7 px-2 text-xs gap-1" title="Criar novo recurso batch">
              <Plus className="w-3.5 h-3.5" />
              <span>Novo</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => handleOpenNewJob("job")}>
              <Play className="w-3.5 h-3.5 mr-2" />
              Job (execução única)
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleOpenNewJob("cronjob")}>
              <Clock className="w-3.5 h-3.5 mr-2" />
              CronJob (agendado)
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </ProtectedAction>
    </div>
  );

  const leftContent = (
    <div className="flex flex-col h-full gap-2 p-1">
      {selectedCronJob && (
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-primary/10 border border-primary/20 text-xs text-primary font-medium">
          <Clock className="w-3.5 h-3.5 flex-shrink-0" />
          <span className="truncate flex-1">{selectedCronJob.namespace}/{selectedCronJob.name}</span>
          <button onClick={handleClearSelection} className="flex-shrink-0 hover:text-foreground transition-colors" title="Limpar seleção">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
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
          <div className="space-y-1 pr-1" ref={leftListRef}>
            {filteredCronJobs.map((cj) => {
              const isSelected =
                selectedCronJob?.name === cj.name &&
                selectedCronJob?.namespace === cj.namespace;
              const itemKey = `${cj.namespace}/${cj.name}`;
              return (
                <div
                  key={itemKey}
                  data-item-key={itemKey}
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
  const rightTitlePrefix = selectedCronJob ? (
    <button
      onClick={handleClearSelection}
      className="flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 hover:bg-primary/40 active:bg-primary/60 border border-primary/30 text-primary transition-colors flex-shrink-0"
      title="Voltar para lista"
    >
      <ChevronLeft className="w-4 h-4" />
    </button>
  ) : undefined;

  const rightTitleAction = selectedCronJob ? (
    <div className="flex items-center gap-1 flex-wrap">
      {/* Suspend / Ativar */}
      <ProtectedAction allowed={canWriteCronJobs}>
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
      <ProtectedAction allowed={canWriteCronJobs}>
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
      <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={handleViewDescribe}>
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
          <ProtectedAction allowed={canWriteCronJobs}>
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
        <CronJobMonitorTable
          cluster={cluster}
          cronJobs={filteredCronJobs}
          loading={loading}
          headerLabel={`${filteredCronJobs.length} cronjob(s)`}
          onOpenEditor={handleSelectCronJob}
          onRequestRefresh={silentFetchCronJobs}
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
            onClick={handleCancel}
            disabled={!hasChanges || isApplying}
          >
            Cancelar
          </Button>
          <ProtectedAction allowed={canWriteCronJobs}>
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
          titlePrefix: rightTitlePrefix,
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
          <ScrollArea className="h-[70vh]">
            {describeLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono bg-muted p-4 rounded whitespace-pre-wrap">
                {describeContent}
              </pre>
            )}
          </ScrollArea>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDescribeOpen(false)}>Fechar</Button>
            <Button
              variant="outline"
              onClick={handleRefreshDescribe}
              disabled={describeLoading}
            >
              <RefreshCcw className={`h-4 w-4 mr-2 ${describeLoading ? "animate-spin" : ""}`} />
              Atualizar
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                navigator.clipboard.writeText(describeContent);
                toast.success("Conteúdo copiado para a área de transferência");
              }}
            >
              <Copy className="h-4 w-4 mr-2" />
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
            <ProtectedAction allowed={canWriteCronJobs}>
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

      {/* Modal Novo Job / CronJob */}
      <Dialog open={newJobOpen} onOpenChange={setNewJobOpen}>
        <DialogContent className="max-w-3xl flex flex-col" style={{ maxHeight: "90vh" }}>
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center gap-2">
              <Plus className="h-5 w-5" />
              Novo {newJobType === "cronjob" ? "CronJob" : "Job"}
            </DialogTitle>
            <DialogDescription>
              Cria {newJobType === "cronjob" ? "um CronJob agendado" : "um Job de execução única"} diretamente via API Kubernetes.
            </DialogDescription>
          </DialogHeader>

          {/* Seletor de tipo */}
          <div className="flex border-b border-border flex-shrink-0 -mx-6 px-6">
            {(["job", "cronjob"] as const).map(t => (
              <button
                key={t}
                onClick={() => {
                  setNewJobType(t);
                  setNewJobYaml(t === "cronjob" ? defaultCronJobTemplate() : defaultJobTemplate());
                }}
                className={`px-4 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                  newJobType === t
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                {t === "job" ? "Job (execução única)" : "CronJob (agendado)"}
              </button>
            ))}
          </div>

          {/* Aviso CronJob + Helm */}
          {newJobType === "cronjob" && (
            <div className="flex-shrink-0 flex items-start gap-2 px-3 py-2 rounded-md bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 text-xs text-amber-800 dark:text-amber-300">
              <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
              <span>CronJobs criados diretamente podem ser removidos pelo Helm no próximo <code>helm upgrade</code>. Considere commitar o YAML no GitHub antes de criar.</span>
            </div>
          )}

          {/* Namespace + ações de template */}
          <div className="flex items-center gap-2 flex-shrink-0">
            <Select value={newJobNamespace || "__none__"} onValueChange={(v) => setNewJobNamespace(v === "__none__" ? "" : v)}>
              <SelectTrigger className="h-8 flex-1">
                <SelectValue placeholder="Namespace (obrigatório)" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">— Selecione o namespace —</SelectItem>
                {namespaces
                  .filter(ns => showSystemNamespaces || !systemNamespaces.has(ns.name))
                  .map(ns => <SelectItem key={ns.name} value={ns.name}>{ns.name}</SelectItem>)
                }
              </SelectContent>
            </Select>
            {selectedCronJob && newJobType === "job" && (
              <Button
                variant="outline" size="sm" className="h-8 text-xs gap-1 flex-shrink-0"
                onClick={handleLoadJobTemplate}
                disabled={isLoadingJobTemplate}
                title={`Usar spec do CronJob ${selectedCronJob.name}`}
              >
                {isLoadingJobTemplate ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <FileText className="w-3.5 h-3.5" />}
                Template de {selectedCronJob.name}
              </Button>
            )}
          </div>

          <div className="flex-1 overflow-hidden min-h-0" style={{ minHeight: 300 }}>
            <MonacoYamlEditor
              value={newJobYaml}
              onChange={(v) => setNewJobYaml(v ?? "")}
              height={400}
            />
          </div>

          {/* Seção: Salvar no GitHub */}
          <div className="flex-shrink-0 border border-border rounded-md overflow-hidden">
            <button
              className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
              onClick={() => { setGhSectionOpen(v => !v); setGhCommitResult(null); }}
            >
              <span className="flex items-center gap-1.5">
                <GitBranch className="w-3.5 h-3.5" />
                Salvar YAML no GitHub
              </span>
              {ghSectionOpen ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
            </button>

            {ghSectionOpen && (
              <div className="px-3 pb-3 pt-1 flex flex-col gap-2 border-t border-border/50 bg-muted/20">
                <p className="text-xs text-muted-foreground">
                  Cole a URL da pasta no GitHub onde o arquivo será criado (ex: <code className="text-xs bg-muted px-1 rounded">github.com/org/repo/tree/main/jobs</code>)
                </p>
                <Input
                  placeholder="https://github.com/org/repo/tree/main/jobs/pasta"
                  value={ghFolderUrl}
                  onChange={e => { setGhFolderUrl(e.target.value); setGhCommitResult(null); }}
                  className="h-8 text-xs font-mono"
                />
                <div className="flex gap-2">
                  <Input
                    placeholder="Nome do arquivo (ex: relatorio-job.yaml)"
                    value={ghFilename}
                    onChange={e => setGhFilename(e.target.value)}
                    className="h-8 text-xs flex-1"
                  />
                  <Input
                    placeholder="Mensagem do commit (opcional)"
                    value={ghMessage}
                    onChange={e => setGhMessage(e.target.value)}
                    className="h-8 text-xs flex-1"
                  />
                </div>
                {/* Validação inline da URL */}
                {ghFolderUrl && !parseGitHubFolderUrl(ghFolderUrl) && (
                  <p className="text-xs text-red-500 flex items-center gap-1">
                    <AlertTriangle className="w-3 h-3" />
                    URL inválida — use o formato: https://github.com/org/repo/tree/branch/pasta
                  </p>
                )}
                {ghFolderUrl && parseGitHubFolderUrl(ghFolderUrl) && (() => {
                  const p = parseGitHubFolderUrl(ghFolderUrl)!;
                  return (
                    <p className="text-xs text-muted-foreground">
                      → <span className="text-foreground font-mono">{p.owner}/{p.repo}</span> · branch <span className="text-foreground font-mono">{p.branch}</span> · pasta <span className="text-foreground font-mono">{p.basePath || "/"}</span>
                    </p>
                  );
                })()}
                {ghCommitResult && (
                  <div className="flex items-center gap-2 text-xs text-green-600 dark:text-green-400">
                    <CheckCircle2 className="w-3.5 h-3.5 flex-shrink-0" />
                    <span>{ghCommitResult.created ? "Arquivo criado" : "Arquivo atualizado"}:</span>
                    <a href={ghCommitResult.fileUrl} target="_blank" rel="noopener noreferrer"
                       className="flex items-center gap-0.5 underline truncate">
                      {ghFilename} <ExternalLink className="w-3 h-3 flex-shrink-0" />
                    </a>
                  </div>
                )}
                <div className="flex justify-end">
                  <Button
                    size="sm" variant="outline" className="h-7 text-xs gap-1"
                    onClick={handleCommitToGitHub}
                    disabled={isCommitting || !ghFolderUrl || !ghFilename || !parseGitHubFolderUrl(ghFolderUrl)}
                  >
                    {isCommitting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <GitBranch className="w-3.5 h-3.5" />}
                    {isCommitting ? "Commitando…" : "Commitar no GitHub"}
                  </Button>
                </div>
              </div>
            )}
          </div>

          <DialogFooter className="flex-shrink-0 gap-2">
            <Button variant="ghost" onClick={() => setNewJobOpen(false)}>Cancelar</Button>
            <Button
              variant="outline"
              onClick={() => handleCreateResource(true)}
              disabled={isCreatingJob || !newJobNamespace}
            >
              {isCreatingJob ? <Loader2 className="w-4 h-4 animate-spin mr-1" /> : <CheckCircle2 className="w-4 h-4 mr-1" />}
              Validar (dry-run)
            </Button>
            <ProtectedAction>
              <Button
                onClick={() => handleCreateResource(false)}
                disabled={isCreatingJob || !newJobNamespace}
              >
                {isCreatingJob ? <Loader2 className="w-4 h-4 animate-spin mr-1" /> : <Play className="w-4 h-4 mr-1" />}
                Criar {newJobType === "cronjob" ? "CronJob" : "Job"}
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
