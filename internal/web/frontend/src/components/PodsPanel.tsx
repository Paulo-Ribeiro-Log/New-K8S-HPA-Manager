import { useEffect, useMemo, useState, useCallback } from "react";
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
import { Search, RefreshCcw, RefreshCw, Eye, EyeOff, Trash2, Terminal, ChevronDown, ChevronRight, AlertCircle, Copy, Check, RotateCw, Download, X, PanelLeftClose, PanelLeftOpen, MoreVertical, Maximize2, FileText, Loader2, Brain, Undo2, Redo2, CheckCircle2, TriangleAlert, FileDiff, Skull, XSquare } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { ProtectedAction } from "@/components/rbac";
import { PodTerminal } from "@/components/PodTerminal";
import { PodFileTransferModal } from "@/components/PodFileTransferModal";
import ResourceGauge from "@/components/ResourceGauge";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useAIDiagnostics } from "@/hooks/useAIDiagnostics";
import { usePersistedTabState } from "@/hooks/usePersistedTabState";
import { createTwoFilesPatch } from "diff";
import { html } from "diff2html";
import * as yaml from "js-yaml";

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
  const { analyzeResource, isAnalyzing, cancelAnalysis } = useAIDiagnostics();

  // ✅ Estados com persistência entre trocas de aba
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>('pods', 'searchQuery', "");
  const [selectedPod, setSelectedPod] = usePersistedTabState<PodSummary | null>('pods', 'selectedPod', null);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = usePersistedTabState<boolean>('pods', 'isSidebarCollapsed', false);
  const [expandedLabels, setExpandedLabels] = usePersistedTabState<boolean>('pods', 'expandedLabels', false);

  // Estados locais (não persistidos)
  const [pods, setPods] = useState<PodSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [podYaml, setPodYaml] = useState("");
  const [yamlLoading, setYamlLoading] = useState(false);
  const [podLogs, setPodLogs] = useState<Record<string, string>>({});
  const [logsLoading, setLogsLoading] = useState<Record<string, boolean>>({});
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deletingPod, setDeletingPod] = useState<PodSummary | null>(null);
  const [restartConfirmOpen, setRestartConfirmOpen] = useState(false);
  const [restartingPod, setRestartingPod] = useState<PodSummary | null>(null);
  const [killConfirmOpen, setKillConfirmOpen] = useState(false);
  const [killingPod, setKillingPod] = useState<PodSummary | null>(null);

  // Estados para seleção em lote
  const [selectedPods, setSelectedPods] = useState<Set<string>>(new Set());
  const [batchActionType, setBatchActionType] = useState<"delete" | "kill" | "restart" | null>(null);
  const [batchConfirmOpen, setBatchConfirmOpen] = useState(false);
  const [batchOperationLoading, setBatchOperationLoading] = useState(false);
  const [batchResults, setBatchResults] = useState<Array<{ namespace: string; name: string; success: boolean; message: string; error?: string }> | null>(null);
  const [batchResultsOpen, setBatchResultsOpen] = useState(false);

  const [yamlCopied, setYamlCopied] = useState(false);
  const [logsModalOpen, setLogsModalOpen] = useState(false);
  const [selectedContainerForLogs, setSelectedContainerForLogs] = useState<string>("");
  const [isAutoRefreshingLogs, setIsAutoRefreshingLogs] = useState(false);
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [shellModalOpen, setShellModalOpen] = useState(false);
  const [selectedShellContainer, setSelectedShellContainer] = useState("");
  const [selectedShellType, setSelectedShellType] = useState("/bin/bash");
  const [useEphemeralDebug, setUseEphemeralDebug] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [terminalFullscreen, setTerminalFullscreen] = useState(false);
  const [showFileTransferModal, setShowFileTransferModal] = useState(false);

  // Estados para edição de YAML
  const [editedYaml, setEditedYaml] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [isApplying, setIsApplying] = useState(false);
  const [isValidating, setIsValidating] = useState(false);
  const [viewMode, setViewMode] = useState<"editor" | "diff">("editor");
  const [diffHtml, setDiffHtml] = useState<string>("");
  const [showDiffModal, setShowDiffModal] = useState(false);
  const [yamlHistory, setYamlHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [editorFullScreen, setEditorFullScreen] = useState(false);

  // Métricas em tempo real
  const [metrics, setMetrics] = useState<{
    available: boolean;
    cpu?: { current: number; percent: number; limit: number; request: number };
    memory?: { current: number; percent: number; limit: number; request: number };
  } | null>(null);

  // Controle de undo/redo
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < yamlHistory.length - 1;
  const hasChanges = editedYaml !== originalYaml;

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  // Helper: Detectar pods problemáticos para AI analysis
  const isPodProblematic = (pod: PodSummary): boolean => {
    // TEMPORÁRIO: Mostrar botão em TODOS os pods para teste
    return true;
    
    /* LÓGICA ORIGINAL (comentada para debug):
    const problematicStatuses = [
      'CrashLoopBackOff',
      'Error',
      'ImagePullBackOff',
      'ErrImagePull',
      'CreateContainerConfigError',
      'InvalidImageName',
      'CreateContainerError',
    ];

    // Status problemático
    if (problematicStatuses.includes(pod.status)) return true;

    // Muitos restarts
    if (pod.restarts > 3) return true;

    // Pending por muito tempo (assumindo que age está em seconds ou similar)
    // Se o pod está Pending e não é recente
    if (pod.status === 'Pending') {
      // age format pode ser "5m", "2h", "1d", etc
      // Simplificação: considerar problemático se não tem "s" (segundos)
      if (!pod.age.includes('s') && pod.age !== '0s') return true;
    }

    return false;
    */
  };

  // Helper: Extrair versão da imagem do container
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

  // ===== Funções de Edição YAML =====

  // Adicionar à história de edição
  const addToHistory = useCallback((yamlContent: string) => {
    setYamlHistory(prev => {
      const newHistory = prev.slice(0, historyIndex + 1);
      newHistory.push(yamlContent);
      if (newHistory.length > 50) newHistory.shift(); // Limite de 50 versões
      return newHistory;
    });
    setHistoryIndex(prev => Math.min(prev + 1, 49));
  }, [historyIndex]);

  // Undo
  const handleUndo = useCallback(() => {
    if (canUndo) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditedYaml(yamlHistory[newIndex]);
    }
  }, [canUndo, historyIndex, yamlHistory]);

  // Redo
  const handleRedo = useCallback(() => {
    if (canRedo) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditedYaml(yamlHistory[newIndex]);
    }
  }, [canRedo, historyIndex, yamlHistory]);

  // Toggle entre editor e diff
  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && hasChanges) {
      const diff = createTwoFilesPatch(
        "original.yaml",
        "edited.yaml",
        originalYaml,
        editedYaml,
        "",
        ""
      );
      const diffHtmlContent = html(diff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(diffHtmlContent);
    }
    setViewMode(mode);
  };

  // Validar YAML (dry-run)
  const handleValidate = async () => {
    if (!selectedPod) return;

    setIsValidating(true);
    try {
      await apiClient.applyPod(
        cluster,
        selectedPod.namespace,
        selectedPod.name,
        {
          yaml: editedYaml,
          fieldManager: "web-pod-editor",
          dryRun: true,
        }
      );
      toast.success("✅ YAML válido!", {
        description: "Validação dry-run passou sem erros",
      });
    } catch (err) {
      toast.error("❌ YAML inválido", {
        description: err instanceof Error ? err.message : "Erro na validação",
      });
    } finally {
      setIsValidating(false);
    }
  };

  // Cancelar edições
  const handleCancelEdit = () => {
    setEditedYaml(originalYaml);
    setViewMode("editor");
    toast.info("Edições canceladas");
  };

  // Recarregar YAML do cluster
  const handleReloadYaml = async () => {
    if (!selectedPod) return;

    try {
      setYamlLoading(true);
      const manifest = await apiClient.getPod(selectedPod.cluster, selectedPod.namespace, selectedPod.name);
      setPodYaml(manifest.yaml);
      setEditedYaml(manifest.yaml);
      setOriginalYaml(manifest.yaml);
      setYamlHistory([manifest.yaml]);
      setHistoryIndex(0);
      toast.success("YAML recarregado do cluster");
    } catch (err) {
      toast.error("Erro ao recarregar YAML", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setYamlLoading(false);
    }
  };

  // Aplicar mudanças
  const handleApplyChanges = async () => {
    if (!selectedPod) return;

    setIsApplying(true);
    try {
      await apiClient.applyPod(
        cluster,
        selectedPod.namespace,
        selectedPod.name,
        {
          yaml: editedYaml,
          fieldManager: "web-pod-editor",
          dryRun: false,
          force: true,
        }
      );
      toast.success("✅ Pod atualizado com sucesso!");
      setOriginalYaml(editedYaml);
      setYamlHistory([editedYaml]);
      setHistoryIndex(0);
      // Recarregar pod
      await loadPods();
      if (selectedPod) {
        await loadPodYaml(selectedPod);
      }
    } catch (err) {
      toast.error("Erro ao aplicar mudanças", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsApplying(false);
    }
  };

  // Visualizar diff em modal
  const handleViewDiff = () => {
    if (!hasChanges) {
      toast.info("Nenhuma mudança para visualizar");
      return;
    }
    const diff = createTwoFilesPatch(
      "original.yaml",
      "edited.yaml",
      originalYaml,
      editedYaml,
      "",
      ""
    );
    const diffHtmlContent = html(diff, {
      drawFileList: false,
      matching: "lines",
      outputFormat: "side-by-side",
    });
    setDiffHtml(diffHtmlContent);
    setShowDiffModal(true);
  };

  // Helper: Processar e colorir logs
  const processLogLine = (line: string, index: number) => {
    const lowerLine = line.toLowerCase();

    // Detectar nível do log e aplicar estilo inline para garantir
    let style: React.CSSProperties = { color: '#d1d5db' }; // Default gray
    let bgStyle: React.CSSProperties = {};
    let icon = null;

    // ERROR - Vermelho intenso com background
    if (lowerLine.includes("error") || lowerLine.includes("fatal") || lowerLine.includes("exception") || lowerLine.includes("fail")) {
      style = { color: '#f87171', fontWeight: 600 };
      bgStyle = { backgroundColor: 'rgba(127, 29, 29, 0.3)', borderLeft: '4px solid #ef4444', paddingLeft: '8px' };
      icon = "❌";
    }
    // WARN - Amarelo/Laranja
    else if (lowerLine.includes("warn") || lowerLine.includes("warning") || lowerLine.includes("deprecated")) {
      style = { color: '#fbbf24', fontWeight: 500 };
      bgStyle = { backgroundColor: 'rgba(113, 63, 18, 0.2)', borderLeft: '4px solid #f59e0b', paddingLeft: '8px' };
      icon = "⚠️";
    }
    // INFO - Azul claro
    else if (lowerLine.includes("info") || lowerLine.includes("information")) {
      style = { color: '#60a5fa' };
      icon = "ℹ️";
    }
    // DEBUG - Roxo/Magenta
    else if (lowerLine.includes("debug") || lowerLine.includes("trace")) {
      style = { color: '#c084fc', fontSize: '10px' };
      icon = "🔍";
    }
    // SUCCESS - Verde
    else if (lowerLine.includes("success") || lowerLine.includes("completed") || lowerLine.includes("done")) {
      style = { color: '#4ade80' };
      icon = "✅";
    }
    // HTTP Status codes
    else if (lowerLine.match(/\b[45]\d{2}\b/)) { // 4xx ou 5xx
      style = { color: '#fb923c' };
      bgStyle = { backgroundColor: 'rgba(124, 45, 18, 0.2)', borderLeft: '2px solid #f97316', paddingLeft: '8px' };
      icon = "🔴";
    }
    else if (lowerLine.match(/\b[23]\d{2}\b/)) { // 2xx ou 3xx
      style = { color: '#86efac' };
    }

    return (
      <div key={index} style={{ ...bgStyle, paddingTop: '2px', paddingBottom: '2px', lineHeight: '1.6' }}>
        <span style={style}>
          {icon && <span style={{ marginRight: '8px', display: 'inline-block', width: '16px' }}>{icon}</span>}
          {line}
        </span>
      </div>
    );
  };

  // Renderizar logs com highlight
  const renderHighlightedLogs = (logs: string) => {
    if (!logs) return null;
    const lines = logs.split('\n');
    return (
      <div className="text-xs font-mono">
        {lines.map((line, index) => processLogLine(line, index))}
      </div>
    );
  };

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) {
      onNamespaceChange("");
    }
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  // Helper: Converter CPU string para millicores
  const parseCPU = (cpu: string | undefined): number => {
    if (!cpu) return 0;
    const match = cpu.match(/^(\d+)m?$/);
    if (!match) return 0;
    return parseInt(match[1], 10);
  };

  // Helper: Converter Memory string para bytes
  const parseMemory = (mem: string | undefined): number => {
    if (!mem) return 0;
    const units: Record<string, number> = {
      'Ki': 1024,
      'Mi': 1024 * 1024,
      'Gi': 1024 * 1024 * 1024,
      'K': 1000,
      'M': 1000 * 1000,
      'G': 1000 * 1000 * 1000,
    };
    const match = mem.match(/^(\d+(?:\.\d+)?)(Ki|Mi|Gi|K|M|G)?$/);
    if (!match) return 0;
    const value = parseFloat(match[1]);
    const unit = match[2] || '';
    return value * (units[unit] || 1);
  };

  // Carregar métricas do Pod
  const loadMetrics = async (pod: PodSummary) => {
    try {
      const response = await apiClient.getPodMetrics(pod.cluster, pod.namespace, pod.name);
      if (response.success && response.data?.available) {
        setMetrics({
          available: true,
          cpu: response.data.cpu,
          memory: response.data.memory,
        });
      } else {
        setMetrics({ available: false });
      }
    } catch (error) {
      setMetrics({ available: false });
    }
  };

  // Auto-refresh de métricas a cada 30 segundos
  useEffect(() => {
    if (!selectedPod) {
      setMetrics(null);
      return;
    }

    // Carregar imediatamente
    loadMetrics(selectedPod);

    // Refresh a cada 30 segundos
    const interval = setInterval(() => {
      if (selectedPod) {
        loadMetrics(selectedPod);
      }
    }, 30000);

    return () => clearInterval(interval);
  }, [selectedPod]);

  const fetchPods = async (silent = false) => {
    if (!cluster) return;

    if (!silent) {
      setLoading(true);
    }
    try {
      const namespaceFilter = selectedNamespace && selectedNamespace !== "__all__" ? [selectedNamespace] : undefined;
      const data = await apiClient.getPods(cluster, namespaceFilter, undefined, showSystemNamespaces, true);
      setPods(data);

      // Se há um pod selecionado, atualizar seus dados sem perder a seleção
      if (silent && selectedPod) {
        const updatedPod = data.find(
          (p) => p.name === selectedPod.name && p.namespace === selectedPod.namespace
        );
        if (updatedPod) {
          setSelectedPod(updatedPod);

          // Atualizar YAML silenciosamente também
          try {
            const manifest = await apiClient.getPod(updatedPod.cluster, updatedPod.namespace, updatedPod.name);
            setPodYaml(manifest.yaml);
          } catch (err) {
            // Erro silencioso - não mostra toast
          }
        }
      }
    } catch (err) {
      if (!silent) {
        toast.error("Erro ao carregar Pods", {
          description: err instanceof Error ? err.message : "Erro desconhecido",
        });
      }
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    fetchPods();
    // Auto-refresh silencioso a cada 30 segundos
    const interval = setInterval(() => fetchPods(true), 30000);
    return () => clearInterval(interval);
  }, [cluster, selectedNamespace, showSystemNamespaces]);

  useEffect(() => {
    setSelectedPod(null);
    setPodYaml("");
    setPodLogs({});
    setExpandedLabels(false);
  }, [cluster, selectedNamespace]);

  const filteredPods = useMemo(() => {
    let result = pods;
    
    // Filtrar por namespaces permitidos (remove system namespaces quando ocultos)
    if (!showSystemNamespaces) {
      const allowedNamespaces = new Set(filteredNamespaces.map(ns => ns.name));
      result = result.filter(pod => allowedNamespaces.has(pod.namespace));
    }
    
    // Filtrar por busca
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      result = result.filter((pod) => {
        return (
          pod.name.toLowerCase().includes(query) ||
          pod.namespace.toLowerCase().includes(query) ||
          pod.nodeName?.toLowerCase().includes(query) ||
          pod.phase.toLowerCase().includes(query) ||
          (pod.statusReason && pod.statusReason.toLowerCase().includes(query)) ||
          pod.containers.some(c => c.name.toLowerCase().includes(query))
        );
      });
    }
    
    return result;
  }, [pods, searchQuery, showSystemNamespaces, filteredNamespaces]);

  const handleSelectPod = async (pod: PodSummary) => {
    setSelectedPod(pod);
    setYamlLoading(true);
    setPodYaml("");
    setPodLogs({});
    setExpandedLabels(false);
    setYamlCopied(false);

    // Inicia auto-refresh de logs automaticamente
    if (pod.containers.length > 0) {
      setSelectedContainerForLogs(pod.containers[0].name);
      setIsAutoRefreshingLogs(true);
    }

    try {
      const manifest = await apiClient.getPod(pod.cluster, pod.namespace, pod.name);
      setPodYaml(manifest.yaml);
      
      // Inicializar estados de edição
      setOriginalYaml(manifest.yaml);
      setEditedYaml(manifest.yaml);
      setYamlHistory([manifest.yaml]);
      setHistoryIndex(0);
      setViewMode("editor");
    } catch (err) {
      setPodYaml("Erro ao carregar manifest");
      toast.error("Erro ao carregar manifest do Pod");
    } finally {
      setYamlLoading(false);
    }
  };

  const loadPodYaml = async (pod: PodSummary) => {
    try {
      const manifest = await apiClient.getPod(pod.cluster, pod.namespace, pod.name);
      setPodYaml(manifest.yaml);
      setOriginalYaml(manifest.yaml);
      setEditedYaml(manifest.yaml);
      setYamlHistory([manifest.yaml]);
      setHistoryIndex(0);
    } catch (err) {
      toast.error("Erro ao recarregar manifest do Pod");
    }
  };

  const loadPods = async () => {
    await fetchPods(false);
  };

  const handleCopyYaml = () => {
    if (!podYaml) return;

    navigator.clipboard.writeText(podYaml);
    setYamlCopied(true);
    toast.success("YAML copiado para a área de transferência");

    setTimeout(() => {
      setYamlCopied(false);
    }, 2000);
  };

  const handleLoadLogs = async (containerName: string, silent = false) => {
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
      // Só exibir toast se não estiver em modo silencioso (auto-refresh)
      if (!silent) {
        toast.error(`Erro ao carregar logs do container ${containerName}`);
      }
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

  const handleRestartPod = async () => {
    if (!restartingPod) return;

    try {
      const result = await apiClient.restartPod(restartingPod.cluster, restartingPod.namespace, restartingPod.name);

      if (!result.hasOwner) {
        toast.warning("Pod reiniciado (sem owner)", {
          description: "ATENÇÃO: Este pod não tem owner e NÃO será recriado automaticamente.",
        });
      } else {
        toast.success(result.message);
      }

      setRestartConfirmOpen(false);
      setRestartingPod(null);
      if (selectedPod?.name === restartingPod.name && selectedPod?.namespace === restartingPod.namespace) {
        setSelectedPod(null);
      }
      fetchPods();
    } catch (err) {
      toast.error("Erro ao reiniciar Pod", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const handleKillPod = async () => {
    if (!killingPod) return;

    try {
      const result = await apiClient.killPod(killingPod.cluster, killingPod.namespace, killingPod.name);
      toast.success(result.message);
      setKillConfirmOpen(false);
      setKillingPod(null);
      if (selectedPod?.name === killingPod.name && selectedPod?.namespace === killingPod.namespace) {
        setSelectedPod(null);
      }
      fetchPods();
    } catch (err) {
      toast.error("Erro ao matar Pod", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  // === Funções de Seleção em Lote ===
  const getPodKey = (pod: PodSummary) => `${pod.namespace}/${pod.name}`;

  const togglePodSelection = (pod: PodSummary, e: React.MouseEvent) => {
    e.stopPropagation();
    const key = getPodKey(pod);
    setSelectedPods(prev => {
      const newSet = new Set(prev);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      return newSet;
    });
  };

  const selectAllPods = () => {
    const allKeys = filteredPods.map(getPodKey);
    setSelectedPods(new Set(allKeys));
  };

  const clearSelection = () => {
    setSelectedPods(new Set());
  };

  const getSelectedPodsData = () => {
    return filteredPods.filter(pod => selectedPods.has(getPodKey(pod)));
  };

  const handleBatchAction = (action: "delete" | "kill" | "restart") => {
    setBatchActionType(action);
    setBatchConfirmOpen(true);
  };

  const executeBatchAction = async () => {
    if (!batchActionType || selectedPods.size === 0) return;

    setBatchOperationLoading(true);
    const podsToProcess = getSelectedPodsData().map(p => ({ namespace: p.namespace, name: p.name }));

    try {
      let result;
      switch (batchActionType) {
        case "delete":
          result = await apiClient.batchDeletePods(cluster, podsToProcess);
          break;
        case "kill":
          result = await apiClient.batchKillPods(cluster, podsToProcess);
          break;
        case "restart":
          result = await apiClient.batchRestartPods(cluster, podsToProcess);
          break;
      }

      setBatchResults(result.results);
      setBatchConfirmOpen(false);
      setBatchResultsOpen(true);

      if (result.success_count === result.total) {
        toast.success(`${result.success_count} pod(s) processado(s) com sucesso`);
      } else if (result.success_count > 0) {
        toast.warning(`${result.success_count}/${result.total} pod(s) processado(s)`, {
          description: `${result.failed_count} falha(s)`,
        });
      } else {
        toast.error(`Falha ao processar ${result.total} pod(s)`);
      }

      clearSelection();
      setSelectedPod(null);
      fetchPods();
    } catch (err) {
      toast.error("Erro na operação em lote", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setBatchOperationLoading(false);
    }
  };

  const getBatchActionConfig = () => {
    switch (batchActionType) {
      case "delete":
        return { label: "Deletar", icon: Trash2, color: "bg-red-600 hover:bg-red-700", description: "Os pods serão deletados permanentemente." };
      case "kill":
        return { label: "Kill (Forçar)", icon: Skull, color: "bg-orange-600 hover:bg-orange-700", description: "Os pods serão terminados imediatamente sem graceful shutdown." };
      case "restart":
        return { label: "Reiniciar", icon: RotateCw, color: "bg-blue-600 hover:bg-blue-700", description: "Os pods serão reiniciados. Pods gerenciados por controllers serão recriados automaticamente." };
      default:
        return { label: "", icon: RotateCw, color: "", description: "" };
    }
  };

  const handleDownloadLogs = (containerName: string) => {
    const logs = podLogs[containerName];
    if (!logs || !selectedPod) return;

    const blob = new Blob([logs], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${selectedPod.name}_${containerName}_logs.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    toast.success(`Logs do container ${containerName} baixados`);
  };

  const handleViewDescribe = async () => {
    if (!selectedPod) return;

    setDescribeLoading(true);
    setDescribeModalOpen(true);
    try {
      const result = await apiClient.describePod(selectedPod.cluster, selectedPod.namespace, selectedPod.name);
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

  // Auto-refresh de logs a cada 3 segundos quando habilitado
  useEffect(() => {
    if (!isAutoRefreshingLogs || !selectedContainerForLogs || !selectedPod) return;

    // Carrega imediatamente ao iniciar (silencioso para evitar toasts repetidos)
    handleLoadLogs(selectedContainerForLogs, true);

    const interval = setInterval(() => {
      // Auto-refresh em modo silencioso (não exibe toasts de erro)
      handleLoadLogs(selectedContainerForLogs, true);
    }, 3000);

    return () => clearInterval(interval);
  }, [isAutoRefreshingLogs, selectedContainerForLogs, selectedPod]);

  const getPhaseColor = (phase: string) => {
    switch (phase.toLowerCase()) {
      case "running":
        return "bg-green-500/10 text-green-700 dark:text-green-400 border-green-500/20";
      case "pending":
        return "bg-yellow-500/10 text-yellow-700 dark:text-yellow-400 border-yellow-500/20";
      case "succeeded":
      case "completed":
        return "bg-blue-500/10 text-blue-700 dark:text-blue-400 border-blue-500/20";
      case "failed":
      case "error":
      case "oomkilled":
        return "bg-red-500/10 text-red-700 dark:text-red-400 border-red-500/20";
      case "crashloopbackoff":
      case "imagepullbackoff":
      case "errimagepull":
      case "createcontainerconfigerror":
        return "bg-red-600/10 text-red-800 dark:text-red-300 border-red-600/20";
      default:
        // Init container errors (Init:Error, Init:CrashLoopBackOff, etc)
        if (phase.toLowerCase().startsWith("init:")) {
          return "bg-red-500/10 text-red-700 dark:text-red-400 border-red-500/20";
        }
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

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Pods" : "Ocultar painel de Pods"}
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
        title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={() => fetchPods()} disabled={!cluster || loading}>
        <RefreshCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
      </Button>
      {collapseButton}
    </div>
  );

  const rightTitleAction = selectedPod && (
    <div className="flex items-center gap-2">
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
      <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon">
          <MoreVertical className="w-4 h-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          onClick={() => {
            if (selectedPod.containers.length > 0) {
              setSelectedShellContainer(selectedPod.containers[0].name);
            }
            setShellModalOpen(true);
          }}
        >
          <Terminal className="w-4 h-4 mr-2" />
          Abrir Shell
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setShowFileTransferModal(true)}
        >
          <Download className="w-4 h-4 mr-2" />
          Transferir Arquivos
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <ProtectedAction showWarning={false}>
          <DropdownMenuItem
            onClick={() => {
              setRestartingPod(selectedPod);
              setRestartConfirmOpen(true);
            }}
          >
            <RotateCw className="w-4 h-4 mr-2" />
            Restart Pod
          </DropdownMenuItem>
        </ProtectedAction>
        <ProtectedAction showWarning={false}>
          <DropdownMenuItem
            onClick={() => {
              setKillingPod(selectedPod);
              setKillConfirmOpen(true);
            }}
            className="text-orange-600 focus:text-orange-600"
          >
            <Skull className="w-4 h-4 mr-2" />
            Kill Pod (Forçar)
          </DropdownMenuItem>
        </ProtectedAction>
        <ProtectedAction showWarning={false}>
          <DropdownMenuItem
            onClick={() => {
              setDeletingPod(selectedPod);
              setDeleteConfirmOpen(true);
            }}
            className="text-destructive focus:text-destructive"
          >
            <Trash2 className="w-4 h-4 mr-2" />
            Deletar Pod
          </DropdownMenuItem>
        </ProtectedAction>
      </DropdownMenuContent>
    </DropdownMenu>
    </div>
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

    const allSelected = filteredPods.length > 0 && filteredPods.every(pod => selectedPods.has(getPodKey(pod)));
    const someSelected = selectedPods.size > 0 && !allSelected;

    return (
      <div className="space-y-2">
        {/* Header com seleção em lote */}
        <div className="flex items-center gap-2 pb-2 border-b border-border/40">
          <Checkbox
            checked={allSelected ? true : someSelected ? "indeterminate" : false}
            onCheckedChange={(checked) => {
              if (checked) {
                selectAllPods();
              } else {
                clearSelection();
              }
            }}
            className="data-[state=checked]:bg-primary data-[state=indeterminate]:bg-primary"
          />
          <span className="text-xs text-muted-foreground">
            {selectedPods.size > 0 ? `${selectedPods.size} selecionado(s)` : "Selecionar todos"}
          </span>
          {selectedPods.size > 0 && (
            <div className="flex items-center gap-1 ml-auto">
              <ProtectedAction>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-xs text-blue-600 hover:text-blue-700 hover:bg-blue-50 dark:hover:bg-blue-950"
                  onClick={() => handleBatchAction("restart")}
                >
                  Reiniciar
                </Button>
              </ProtectedAction>
              <ProtectedAction>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-xs text-orange-600 hover:text-orange-700 hover:bg-orange-50 dark:hover:bg-orange-950"
                  onClick={() => handleBatchAction("kill")}
                >
                  Kill
                </Button>
              </ProtectedAction>
              <ProtectedAction>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-xs text-red-600 hover:text-red-700 hover:bg-red-50 dark:hover:bg-red-950"
                  onClick={() => handleBatchAction("delete")}
                >
                  Deletar
                </Button>
              </ProtectedAction>
              <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={clearSelection}>
                <X className="w-3 h-3 mr-1" />
                Limpar
              </Button>
            </div>
          )}
        </div>

        {/* Lista de pods */}
        {filteredPods.map((pod) => {
          const isSelected =
            selectedPod?.name === pod.name &&
            selectedPod?.namespace === pod.namespace;
          const isChecked = selectedPods.has(getPodKey(pod));
          const hasWarning = pod.restarts > 3;

          return (
            <div
              key={`${pod.cluster}-${pod.namespace}-${pod.name}`}
              className={`flex items-start gap-2 p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10"
                  : isChecked
                  ? "border-primary/50 bg-primary/5"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              {/* Checkbox */}
              <Checkbox
                checked={isChecked}
                onCheckedChange={() => {
                  const key = getPodKey(pod);
                  setSelectedPods(prev => {
                    const newSet = new Set(prev);
                    if (newSet.has(key)) {
                      newSet.delete(key);
                    } else {
                      newSet.add(key);
                    }
                    return newSet;
                  });
                }}
                onClick={(e) => e.stopPropagation()}
                className="mt-0.5"
              />

              {/* Pod info */}
              <button
                onClick={() => handleSelectPod(pod)}
                className="flex-1 text-left"
              >
                <div className="flex items-center gap-2 mb-1">
                  <div className="font-semibold text-sm truncate flex-1">{pod.name}</div>
                  <Badge variant="outline" className={`text-xs ${getPhaseColor(pod.statusReason || pod.phase)}`}>
                    {pod.statusReason || pod.phase}
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
            </div>
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

    // Calcular AGE
    const calculateAge = (createdAt: string) => {
      if (!createdAt) return "N/A";
      const created = new Date(createdAt);
      const now = new Date();
      const diffMs = now.getTime() - created.getTime();
      const diffMinutes = Math.floor(diffMs / 60000);
      const diffHours = Math.floor(diffMinutes / 60);
      const diffDays = Math.floor(diffHours / 24);

      if (diffDays > 0) return `${diffDays}d`;
      if (diffHours > 0) return `${diffHours}h`;
      if (diffMinutes > 0) return `${diffMinutes}m`;
      return "agora";
    };

    const age = calculateAge(selectedPod.createdAt);

    return (
      <div className="flex flex-col h-full overflow-hidden">
        {/* Header Compacto */}
        <div className="border-b border-border px-4 py-2 space-y-2 flex-shrink-0 relative">
          {/* Gauges - Container Isolado (Posicionamento Absoluto) */}
          {metrics && metrics.available && (
            <div className="absolute top-2 right-4 flex items-center gap-2">
              <ResourceGauge
                title="CPU"
                current={metrics.cpu?.current || 0}
                request={metrics.cpu?.request || 0}
                limit={metrics.cpu?.limit || 0}
                unit="m"
                formatValue={(v) => v.toFixed(0)}
              />
              <ResourceGauge
                title="Memory"
                current={(metrics.memory?.current || 0) / (1024 * 1024)}
                request={(metrics.memory?.request || 0) / (1024 * 1024)}
                limit={(metrics.memory?.limit || 0) / (1024 * 1024)}
                unit="Mi"
                formatValue={(v) => v.toFixed(0)}
              />
            </div>
          )}

          {/* Linha 1: Nome do Pod */}
          <div className="pr-48">
            <h3 className="font-semibold text-base">{selectedPod.name}</h3>
          </div>

          {/* Linha 2: Badges de Namespace, Versão e Status */}
          <div className="flex items-center gap-2 pr-48">
            <Badge variant="secondary" className="text-xs">
              NS: {selectedPod.namespace}
            </Badge>
            {selectedPod.containers.length > 0 && extractImageVersion(selectedPod.containers[0].image) && (
              <Badge variant="secondary" className="text-xs">
                v{extractImageVersion(selectedPod.containers[0].image)}
              </Badge>
            )}
            <Badge variant="outline" className={`text-xs ${getPhaseColor(selectedPod.statusReason || selectedPod.phase)}`}>
              {selectedPod.statusReason || selectedPod.phase}
            </Badge>
            <Badge
              variant="outline"
              className={`text-xs ${
                selectedPod.readyContainers === selectedPod.totalContainers
                  ? "bg-green-500/10 text-green-700 dark:text-green-400"
                  : "bg-red-500/10 text-red-700 dark:text-red-400"
              }`}
            >
              {selectedPod.readyContainers}/{selectedPod.totalContainers}
            </Badge>
          </div>

          {/* Linha 3: Node, IP, Age, Restarts */}
          <div className="flex items-center gap-4 text-xs text-muted-foreground">
            <span>Node: <span className="font-mono text-foreground">{selectedPod.nodeName || "N/A"}</span></span>
            <span>IP: <span className="font-mono text-foreground">{selectedPod.podIP || "N/A"}</span></span>
            <span>Age: <span className="text-foreground">{age}</span></span>
            <span>Restarts: <span className="text-foreground">{selectedPod.restarts}</span></span>
          </div>

          {/* Linha 4: CPU e Memória com cores */}
          <div className="flex items-center gap-4 text-xs">
            <span className="text-muted-foreground">CPU/R: <span className="font-mono font-semibold text-blue-600 dark:text-blue-400">{selectedPod.cpuRequest || "N/A"}</span></span>
            <span className="text-muted-foreground">CPU/L: <span className="font-mono font-semibold text-slate-600 dark:text-slate-400">{selectedPod.cpuLimit || "N/A"}</span></span>
            <span className="text-muted-foreground">MEM/R: <span className="font-mono font-semibold text-blue-600 dark:text-blue-400">{selectedPod.memoryRequest || "N/A"}</span></span>
            <span className="text-muted-foreground">MEM/L: <span className="font-mono font-semibold text-slate-600 dark:text-slate-400">{selectedPod.memoryLimit || "N/A"}</span></span>
          </div>

          {/* Containers - Compacto */}
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-muted-foreground">Containers:</span>
            {selectedPod.containers.map((container) => (
              <Badge
                key={container.name}
                variant="outline"
                className={`text-xs ${getContainerStateColor(container.state)}`}
              >
                {container.name}
                {container.restartCount > 0 && ` (${container.restartCount}↻)`}
              </Badge>
            ))}
          </div>

          {/* Labels - Colapsável */}
          {selectedPod.labels && Object.keys(selectedPod.labels).length > 0 && (
            <div>
              <button
                onClick={() => setExpandedLabels(!expandedLabels)}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-primary transition-colors"
              >
                {expandedLabels ? (
                  <ChevronDown className="w-3 h-3" />
                ) : (
                  <ChevronRight className="w-3 h-3" />
                )}
                Labels ({Object.keys(selectedPod.labels).length})
              </button>
              {expandedLabels && (
                <div className="mt-1 flex flex-wrap gap-1">
                  {Object.entries(selectedPod.labels).map(([key, value]) => (
                    <Badge
                      key={key}
                      variant="outline"
                      className="text-xs font-mono"
                    >
                      {key}={value}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* YAML Manifest + Editor */}
        <div className="flex-1 overflow-auto">
          <div className="p-4">
            {yamlLoading ? (
              <div className="flex items-center justify-center h-64">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            ) : (
              <div className="space-y-3">
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium">Manifesto YAML</p>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={handleReloadYaml}
                      disabled={yamlLoading}
                      className="h-6 w-6 p-0"
                      title="Recarregar YAML do cluster"
                    >
                      <RefreshCw className={`w-3.5 h-3.5 ${yamlLoading ? "animate-spin" : ""}`} />
                    </Button>
                    {hasChanges && (
                      <Badge variant="outline" className="text-xs bg-orange-500/10 text-orange-700 dark:text-orange-400">
                        Não salvo
                      </Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    {/* Undo/Redo */}
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

                    {/* View Mode Toggle */}
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

                    {/* Botão AI (se pod problemático) */}
                    {selectedPod && isPodProblematic(selectedPod) && (
                      <Button
                        variant="default"
                        size="sm"
                        onClick={async () => {
                          console.log("[PodsPanel] Starting AI analysis...");
                          try {
                            const analysis = await analyzeResource({
                              resourceType: "Pod",
                              cluster,
                              namespace: selectedPod.namespace,
                              resourceName: selectedPod.name,
                              includeDescribe: true,
                              includeMetrics: true,
                              includeLogs: true,
                            });

                            console.log("[PodsPanel] Analysis result:", analysis);

                            if (analysis) {
                              console.log("[PodsPanel] Saving to sessionStorage with key:", `ai-analysis-${analysis.id}`);
                              sessionStorage.setItem(`ai-analysis-${analysis.id}`, JSON.stringify(analysis));

                              // Verificar se foi salvo
                              const saved = sessionStorage.getItem(`ai-analysis-${analysis.id}`);
                              console.log("[PodsPanel] Verification - saved data exists:", !!saved);

                              console.log("[PodsPanel] Opening new window:", `/ai-analysis/${analysis.id}`);
                              const newWindow = window.open(`/ai-analysis/${analysis.id}`, '_blank');
                              if (newWindow) {
                                toast.success('Análise aberta em nova aba');
                              } else {
                                toast.error('Popup bloqueado! Permita popups para este site.');
                              }
                            } else {
                              console.warn("[PodsPanel] Analysis is null or undefined");
                              toast.error('Análise retornou vazia');
                            }
                          } catch (error) {
                            console.error("[PodsPanel] Analysis failed:", error);
                            toast.error('Falha ao analisar recurso');
                          }
                        }}
                        disabled={isAnalyzing}
                        className="bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-700 hover:to-blue-700"
                      >
                        {isAnalyzing ? (
                          <>
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                            Analisando...
                          </>
                        ) : (
                          <>
                            <Brain className="w-4 h-4 mr-2" />
                            Analisar com AI
                          </>
                        )}
                      </Button>
                    )}

                    {/* Botão Cancelar (apenas durante análise) */}
                    {isAnalyzing && (
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => {
                          cancelAnalysis();
                          toast.info('Cancelando análise...');
                        }}
                      >
                        <X className="w-4 h-4 mr-2" />
                        Cancelar
                      </Button>
                    )}

                    {/* Botões auxiliares */}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setLogsModalOpen(true)}
                      disabled={!selectedPod}
                    >
                      <Terminal className="w-4 h-4 mr-2" />
                      Ver Logs
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleCopyYaml}
                      disabled={!podYaml || yamlLoading}
                    >
                      {yamlCopied ? <Check className="w-4 h-4 mr-2" /> : <Copy className="w-4 h-4 mr-2" />}
                      {yamlCopied ? "Copiado!" : "Copiar YAML"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setEditorFullScreen(true)}
                      title="Abrir editor em tela cheia"
                      disabled={!podYaml || yamlLoading}
                    >
                      <Maximize2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </div>

                {/* Editor / Diff View */}
                {viewMode === "editor" && (
                  <MonacoYamlEditor
                    key={`pod-manifest-${selectedPod?.name}`}
                    value={editedYaml}
                    onChange={(value) => {
                      setEditedYaml(value || "");
                      if (value && value !== yamlHistory[historyIndex]) {
                        addToHistory(value);
                      }
                    }}
                    readOnly={false}
                    height={1200}
                  />
                )}
                {viewMode === "diff" && (
                  <MonacoYamlEditor
                    mode="diff"
                    originalValue={originalYaml}
                    value={editedYaml}
                    height={1200}
                    readOnly
                  />
                )}
              </div>

              {/* Botões de ação na parte inferior */}
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleViewDiff}
                  disabled={!hasChanges}
                >
                  <FileDiff className="w-4 h-4 mr-2" />
                  Visualizar diff
                </Button>

                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleCancelEdit}
                  disabled={!hasChanges}
                >
                  <X className="w-4 h-4 mr-2" />
                  Cancelar
                </Button>

                <ProtectedAction>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={handleValidate}
                    disabled={!selectedPod || isValidating}
                  >
                    {isValidating ? (
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <CheckCircle2 className="w-4 h-4 mr-2" />
                    )}
                    Dry-run
                  </Button>
                </ProtectedAction>

                <ProtectedAction>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={handleApplyChanges}
                    disabled={!hasChanges || isApplying}
                  >
                    {isApplying ? (
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <Check className="w-4 h-4 mr-2" />
                    )}
                    Aplicar
                  </Button>
                  </ProtectedAction>
                </div>
              </div>
            )}
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
          placeholder="Buscar por nome, namespace, node..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderPodList()}
    </div>
  );

  // Renderização quando sidebar está recolhido
  if (isSidebarCollapsed) {
    return (
      <>
        <div className="p-4 h-full">
          <div className="grid grid-cols-1 h-full">
            <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
              <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
                <div className="flex items-center gap-3 flex-wrap">
                  {collapseButton}
                  <p className="text-base font-semibold text-primary">Detalhes do Pod</p>
                </div>
                {rightTitleAction}
              </div>
              <div className="flex-1 overflow-auto min-h-0">
                {renderPodDetails()}
              </div>
            </div>
          </div>
        </div>

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
              <ProtectedAction>
                <Button variant="destructive" onClick={handleDeletePod}>
                  Deletar Pod
                </Button>
              </ProtectedAction>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Modal de Confirmação de Restart */}
        <Dialog open={restartConfirmOpen} onOpenChange={setRestartConfirmOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Confirmar Restart do Pod</DialogTitle>
              <DialogDescription>
                Tem certeza que deseja reiniciar o pod <strong>{restartingPod?.name}</strong>?
                <br />
                <br />
                O pod será deletado e recriado automaticamente pelo controller (Deployment/StatefulSet).
                {restartingPod && (
                  <div className="mt-2 p-2 bg-muted rounded text-sm">
                    <strong>Namespace:</strong> {restartingPod.namespace}
                  </div>
                )}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setRestartConfirmOpen(false)}>
                Cancelar
              </Button>
              <ProtectedAction>
                <Button onClick={handleRestartPod}>
                  <RotateCw className="w-4 h-4 mr-2" />
                  Restart Pod
                </Button>
              </ProtectedAction>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Modal de Confirmação de Kill */}
        <Dialog open={killConfirmOpen} onOpenChange={setKillConfirmOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2 text-orange-600">
                <Skull className="w-5 h-5" />
                Confirmar Kill do Pod
              </DialogTitle>
              <DialogDescription>
                Tem certeza que deseja <strong className="text-orange-600">FORÇAR</strong> a terminação imediata do pod <strong>{killingPod?.name}</strong>?
                <br />
                <br />
                <div className="p-3 bg-orange-500/10 border border-orange-500/20 rounded-lg text-sm">
                  <strong className="text-orange-600">Atenção:</strong> Esta ação força a terminação imediata do pod (gracePeriod=0),
                  sem aguardar o shutdown gracioso. O container será encerrado abruptamente.
                </div>
                {killingPod && (
                  <div className="mt-2 p-2 bg-muted rounded text-sm">
                    <strong>Namespace:</strong> {killingPod.namespace}
                  </div>
                )}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setKillConfirmOpen(false)}>
                Cancelar
              </Button>
              <ProtectedAction>
                <Button variant="destructive" className="bg-orange-600 hover:bg-orange-700" onClick={handleKillPod}>
                  <Skull className="w-4 h-4 mr-2" />
                  Kill Pod
                </Button>
              </ProtectedAction>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Modal de Confirmação de Batch */}
        <Dialog open={batchConfirmOpen} onOpenChange={setBatchConfirmOpen}>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                {(() => {
                  const config = getBatchActionConfig();
                  const Icon = config.icon;
                  return Icon ? <Icon className="w-5 h-5" /> : null;
                })()}
                {getBatchActionConfig().label} {selectedPods.size} Pod(s)
              </DialogTitle>
              <DialogDescription>
                <div className="mt-2">
                  {getBatchActionConfig().description}
                </div>
                <div className="mt-4 p-3 bg-muted rounded-lg max-h-48 overflow-y-auto">
                  <div className="text-xs font-medium mb-2">Pods selecionados:</div>
                  <div className="space-y-1">
                    {getSelectedPodsData().map(pod => (
                      <div key={getPodKey(pod)} className="text-xs flex items-center gap-2">
                        <Badge variant="outline" className={`text-[10px] ${getPhaseColor(pod.statusReason || pod.phase)}`}>
                          {pod.statusReason || pod.phase}
                        </Badge>
                        <span className="text-muted-foreground">{pod.namespace}/</span>
                        <span className="font-medium">{pod.name}</span>
                      </div>
                    ))}
                  </div>
                </div>
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setBatchConfirmOpen(false)} disabled={batchOperationLoading}>
                Cancelar
              </Button>
              <ProtectedAction>
                <Button
                  className={getBatchActionConfig().color}
                  onClick={executeBatchAction}
                  disabled={batchOperationLoading}
                >
                  {batchOperationLoading ? (
                    <>
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                      Processando...
                    </>
                  ) : (
                    (() => {
                      const config = getBatchActionConfig();
                      const Icon = config.icon;
                      return (
                        <>
                          {Icon && <Icon className="w-4 h-4 mr-2" />}
                          {config.label} {selectedPods.size} Pod(s)
                        </>
                      );
                    })()
                  )}
                </Button>
              </ProtectedAction>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Modal de Resultados do Batch */}
        <Dialog open={batchResultsOpen} onOpenChange={setBatchResultsOpen}>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>Resultado da Operação</DialogTitle>
              <DialogDescription>
                {batchResults && (
                  <div className="mt-2">
                    <div className="flex items-center gap-4 mb-4">
                      <Badge variant="outline" className="bg-green-500/10 text-green-700 border-green-500/20">
                        <CheckCircle2 className="w-3 h-3 mr-1" />
                        {batchResults.filter(r => r.success).length} sucesso
                      </Badge>
                      {batchResults.filter(r => !r.success).length > 0 && (
                        <Badge variant="outline" className="bg-red-500/10 text-red-700 border-red-500/20">
                          <XSquare className="w-3 h-3 mr-1" />
                          {batchResults.filter(r => !r.success).length} falha(s)
                        </Badge>
                      )}
                    </div>
                    <ScrollArea className="max-h-64">
                      <div className="space-y-2">
                        {batchResults.map((result, idx) => (
                          <div
                            key={idx}
                            className={`p-2 rounded-lg text-xs ${
                              result.success
                                ? "bg-green-500/10 border border-green-500/20"
                                : "bg-red-500/10 border border-red-500/20"
                            }`}
                          >
                            <div className="flex items-center gap-2">
                              {result.success ? (
                                <CheckCircle2 className="w-4 h-4 text-green-600" />
                              ) : (
                                <XSquare className="w-4 h-4 text-red-600" />
                              )}
                              <span className="font-medium">{result.namespace}/{result.name}</span>
                            </div>
                            <div className="ml-6 mt-1 text-muted-foreground">
                              {result.message}
                              {result.error && <span className="text-red-600"> - {result.error}</span>}
                            </div>
                          </div>
                        ))}
                      </div>
                    </ScrollArea>
                  </div>
                )}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button onClick={() => setBatchResultsOpen(false)}>
                Fechar
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Modal de Logs */}
        <Dialog open={logsModalOpen} onOpenChange={(open) => {
          setLogsModalOpen(open);
          if (open && selectedPod && selectedPod.containers.length > 0) {
            if (!selectedContainerForLogs) {
              setSelectedContainerForLogs(selectedPod.containers[0].name);
            }
            // Inicia auto-refresh automaticamente ao abrir
            setIsAutoRefreshingLogs(true);
          } else {
            // Para auto-refresh ao fechar
            setIsAutoRefreshingLogs(false);
          }
        }}>
          <DialogContent className="max-w-6xl max-h-[90vh] flex flex-col p-0">
            <DialogHeader className="border-b border-border px-6 py-4 flex-shrink-0">
              <div className="flex items-center justify-between gap-4 pr-8">
                <div>
                  <DialogTitle className="text-xl font-semibold">
                    Logs do Pod
                  </DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground mt-1">
                    {selectedPod?.namespace}/{selectedPod?.name}
                  </DialogDescription>
                </div>
                <div className="flex items-center gap-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">Container:</span>
                    <Select
                      value={selectedContainerForLogs}
                      onValueChange={setSelectedContainerForLogs}
                    >
                      <SelectTrigger className="w-[200px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {selectedPod?.containers.map((container) => (
                          <SelectItem key={container.name} value={container.name}>
                            {container.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <Button
                    variant={isAutoRefreshingLogs ? "destructive" : "default"}
                    size="sm"
                    onClick={() => setIsAutoRefreshingLogs(!isAutoRefreshingLogs)}
                  >
                    {isAutoRefreshingLogs ? (
                      <>
                        <X className="w-4 h-4 mr-2" />
                        Parar
                      </>
                    ) : (
                      <>
                        <RefreshCcw className="w-4 h-4 mr-2" />
                        Iniciar
                      </>
                    )}
                  </Button>
                  {podLogs[selectedContainerForLogs] && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleDownloadLogs(selectedContainerForLogs)}
                    >
                      <Download className="w-4 h-4 mr-2" />
                      Download
                    </Button>
                  )}
                </div>
              </div>
            </DialogHeader>

            <div className="flex-1 overflow-hidden p-6">
              {podLogs[selectedContainerForLogs] ? (
                <ScrollArea className="h-[calc(90vh-200px)] border rounded-lg bg-black/95">
                  <div className="p-4">
                    {renderHighlightedLogs(podLogs[selectedContainerForLogs])}
                  </div>
                </ScrollArea>
              ) : (
                <div className="flex items-center justify-center h-[calc(90vh-200px)] border border-dashed rounded-lg text-muted-foreground text-sm">
                  {isAutoRefreshingLogs ? (
                    <div className="flex flex-col items-center gap-2">
                      <RefreshCcw className="w-6 h-6 animate-spin" />
                      <span>Carregando logs automaticamente...</span>
                    </div>
                  ) : (
                    <span>Clique em "Iniciar" para carregar os logs</span>
                  )}
                </div>
              )}
            </div>
          </DialogContent>
        </Dialog>
      </>
    );
  }

  return (
    <>
      {/* Modal de Configuração do Shell */}
      <Dialog open={shellModalOpen} onOpenChange={setShellModalOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Abrir Shell no Container</DialogTitle>
            <DialogDescription>
              Escolha o container e o tipo de shell para conectar
            </DialogDescription>
          </DialogHeader>
          
          <div className="space-y-4 py-4">
            {/* Seleção de Container */}
            <div className="space-y-2">
              <Label htmlFor="container-select">Container</Label>
              <Select
                value={selectedShellContainer}
                onValueChange={setSelectedShellContainer}
              >
                <SelectTrigger id="container-select">
                  <SelectValue placeholder="Selecione um container" />
                </SelectTrigger>
                <SelectContent>
                  {selectedPod?.containers.map((container) => (
                    <SelectItem key={container.name} value={container.name}>
                      <div className="flex items-center gap-2">
                        <span>{container.name}</span>
                        <Badge variant="outline" className="text-xs">
                          {container.state}
                        </Badge>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Seleção de Shell */}
            <div className="space-y-3">
              <Label>Tipo de Shell</Label>
              <RadioGroup 
                value={useEphemeralDebug ? "ephemeral" : selectedShellType} 
                onValueChange={(value) => {
                  if (value === "ephemeral") {
                    setUseEphemeralDebug(true);
                    setSelectedShellType("/bin/bash");
                  } else {
                    setUseEphemeralDebug(false);
                    setSelectedShellType(value);
                  }
                }}
              >
                <div className="flex items-start space-x-2 p-2 rounded hover:bg-muted/50 transition-colors">
                  <RadioGroupItem value="/bin/bash" id="bash" className="mt-0.5" />
                  <Label htmlFor="bash" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium">/bin/bash</div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      Shell padrão mais comum • Recursos avançados • Presente na maioria dos containers
                    </div>
                  </Label>
                </div>
                <div className="flex items-start space-x-2 p-2 rounded hover:bg-muted/50 transition-colors">
                  <RadioGroupItem value="/bin/sh" id="sh" className="mt-0.5" />
                  <Label htmlFor="sh" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium">/bin/sh</div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      Shell minimalista POSIX • Comum em imagens Alpine e efêmeras • Sempre disponível
                    </div>
                  </Label>
                </div>
                <div className="flex items-start space-x-2 p-2 rounded bg-blue-500/10 border border-blue-500/20 hover:bg-blue-500/20 transition-colors">
                  <RadioGroupItem value="ephemeral" id="ephemeral" className="mt-0.5" />
                  <Label htmlFor="ephemeral" className="font-normal cursor-pointer flex-1">
                    <div className="font-medium flex items-center gap-2">
                      <span>🛠️ Ephemeral Debug Container</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-600 dark:text-blue-400 font-semibold">RECOMENDADO</span>
                    </div>
                    <div className="text-xs text-muted-foreground mt-0.5">
                      <span className="font-medium">nicolaka/netshoot</span> • Container temporário com arsenal completo de debug
                    </div>
                    <div className="text-xs text-muted-foreground mt-1 flex flex-wrap gap-1">
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">tcpdump</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">curl</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">nslookup</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">dig</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">netstat</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">iperf</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">mtr</code>
                      <code className="px-1 py-0.5 bg-black/20 rounded text-[10px]">ethtool</code>
                    </div>
                    <div className="text-xs mt-2 p-2 bg-yellow-500/10 border border-yellow-500/20 rounded">
                      <span className="text-yellow-300">ℹ️ Nota:</span> Ephemeral containers persistem até o pod reiniciar. Containers existentes serão reutilizados automaticamente.
                    </div>
                  </Label>
                </div>
              </RadioGroup>
            </div>

            {/* Info sobre o pod */}
            {selectedPod && (
              <div className="mt-4 p-3 bg-muted rounded-lg text-sm space-y-1">
                <div><span className="font-medium">Pod:</span> {selectedPod.name}</div>
                <div><span className="font-medium">Namespace:</span> {selectedPod.namespace}</div>
                <div><span className="font-medium">Status:</span> <Badge variant="outline" className={getPhaseColor(selectedPod.statusReason || selectedPod.phase)}>{selectedPod.statusReason || selectedPod.phase}</Badge>{selectedPod.statusReason && <span className="ml-2 text-muted-foreground">({selectedPod.phase})</span>}</div>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShellModalOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              onClick={() => {
                if (!selectedShellContainer) {
                  toast.error("Selecione um container");
                  return;
                }
                setShellModalOpen(false);
                setTerminalOpen(true);
              }}
              disabled={!selectedShellContainer}
            >
              <Terminal className="w-4 h-4 mr-2" />
              Conectar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal do Terminal */}
      <Dialog open={terminalOpen} onOpenChange={(open) => {
        setTerminalOpen(open);
        if (!open) {
          // Resetar fullscreen ao fechar
          setTerminalFullscreen(false);
        }
      }}>
        <DialogContent className={terminalFullscreen 
          ? "w-screen h-screen max-w-none max-h-none p-0 m-0 rounded-none"
          : "max-w-6xl h-[85vh] p-0"
        }>
          {selectedPod && selectedShellContainer && (
            <PodTerminal
              cluster={selectedPod.cluster}
              namespace={selectedPod.namespace}
              pod={selectedPod.name}
              container={selectedShellContainer}
              shell={selectedShellType}
              ephemeral={useEphemeralDebug}
              isFullscreen={terminalFullscreen}
              onToggleFullscreen={() => setTerminalFullscreen(!terminalFullscreen)}
              onClose={() => setTerminalOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>

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
            <ProtectedAction>
              <Button variant="destructive" onClick={handleDeletePod}>
                Deletar Pod
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Confirmação de Restart */}
      <Dialog open={restartConfirmOpen} onOpenChange={setRestartConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar Restart do Pod</DialogTitle>
            <DialogDescription>
              Tem certeza que deseja reiniciar o pod <strong>{restartingPod?.name}</strong>?
              <br />
              <br />
              O pod será deletado e recriado automaticamente pelo controller (Deployment/StatefulSet).
              {restartingPod && (
                <div className="mt-2 p-2 bg-muted rounded text-sm">
                  <strong>Namespace:</strong> {restartingPod.namespace}
                </div>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRestartConfirmOpen(false)}>
              Cancelar
            </Button>
            <ProtectedAction>
              <Button onClick={handleRestartPod}>
                <RotateCw className="w-4 h-4 mr-2" />
                Restart Pod
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Confirmação de Kill */}
      <Dialog open={killConfirmOpen} onOpenChange={setKillConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-orange-600">
              <Skull className="w-5 h-5" />
              Confirmar Kill do Pod
            </DialogTitle>
            <DialogDescription>
              Tem certeza que deseja <strong className="text-orange-600">FORÇAR</strong> a terminação imediata do pod <strong>{killingPod?.name}</strong>?
              <br />
              <br />
              <div className="p-3 bg-orange-500/10 border border-orange-500/20 rounded-lg text-sm">
                <strong className="text-orange-600">Atenção:</strong> Esta ação força a terminação imediata do pod (gracePeriod=0),
                sem aguardar o shutdown gracioso. O container será encerrado abruptamente.
              </div>
              {killingPod && (
                <div className="mt-2 p-2 bg-muted rounded text-sm">
                  <strong>Namespace:</strong> {killingPod.namespace}
                </div>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setKillConfirmOpen(false)}>
              Cancelar
            </Button>
            <ProtectedAction>
              <Button variant="destructive" className="bg-orange-600 hover:bg-orange-700" onClick={handleKillPod}>
                <Skull className="w-4 h-4 mr-2" />
                Kill Pod
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Confirmação de Batch */}
      <Dialog open={batchConfirmOpen} onOpenChange={setBatchConfirmOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {(() => {
                const config = getBatchActionConfig();
                const Icon = config.icon;
                return Icon ? <Icon className="w-5 h-5" /> : null;
              })()}
              {getBatchActionConfig().label} {selectedPods.size} Pod(s)
            </DialogTitle>
            <DialogDescription>
              <div className="mt-2">
                {getBatchActionConfig().description}
              </div>
              <div className="mt-4 p-3 bg-muted rounded-lg max-h-48 overflow-y-auto">
                <div className="text-xs font-medium mb-2">Pods selecionados:</div>
                <div className="space-y-1">
                  {getSelectedPodsData().map(pod => (
                    <div key={getPodKey(pod)} className="text-xs flex items-center gap-2">
                      <Badge variant="outline" className={`text-[10px] ${getPhaseColor(pod.statusReason || pod.phase)}`}>
                        {pod.statusReason || pod.phase}
                      </Badge>
                      <span className="text-muted-foreground">{pod.namespace}/</span>
                      <span className="font-medium">{pod.name}</span>
                    </div>
                  ))}
                </div>
              </div>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBatchConfirmOpen(false)} disabled={batchOperationLoading}>
              Cancelar
            </Button>
            <ProtectedAction>
              <Button
                className={getBatchActionConfig().color}
                onClick={executeBatchAction}
                disabled={batchOperationLoading}
              >
                {batchOperationLoading ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Processando...
                  </>
                ) : (
                  (() => {
                    const config = getBatchActionConfig();
                    const Icon = config.icon;
                    return (
                      <>
                        {Icon && <Icon className="w-4 h-4 mr-2" />}
                        {config.label} {selectedPods.size} Pod(s)
                      </>
                    );
                  })()
                )}
              </Button>
            </ProtectedAction>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Resultados do Batch */}
      <Dialog open={batchResultsOpen} onOpenChange={setBatchResultsOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Resultado da Operação</DialogTitle>
            <DialogDescription>
              {batchResults && (
                <div className="mt-2">
                  <div className="flex items-center gap-4 mb-4">
                    <Badge variant="outline" className="bg-green-500/10 text-green-700 border-green-500/20">
                      <CheckCircle2 className="w-3 h-3 mr-1" />
                      {batchResults.filter(r => r.success).length} sucesso
                    </Badge>
                    {batchResults.filter(r => !r.success).length > 0 && (
                      <Badge variant="outline" className="bg-red-500/10 text-red-700 border-red-500/20">
                        <XSquare className="w-3 h-3 mr-1" />
                        {batchResults.filter(r => !r.success).length} falha(s)
                      </Badge>
                    )}
                  </div>
                  <ScrollArea className="max-h-64">
                    <div className="space-y-2">
                      {batchResults.map((result, idx) => (
                        <div
                          key={idx}
                          className={`p-2 rounded-lg text-xs ${
                            result.success
                              ? "bg-green-500/10 border border-green-500/20"
                              : "bg-red-500/10 border border-red-500/20"
                          }`}
                        >
                          <div className="flex items-center gap-2">
                            {result.success ? (
                              <CheckCircle2 className="w-4 h-4 text-green-600" />
                            ) : (
                              <XSquare className="w-4 h-4 text-red-600" />
                            )}
                            <span className="font-medium">{result.namespace}/{result.name}</span>
                          </div>
                          <div className="mt-1 text-muted-foreground">{result.message}</div>
                          {result.error && (
                            <div className="mt-1 text-red-600">{result.error}</div>
                          )}
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                </div>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button onClick={() => setBatchResultsOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Logs */}
      <Dialog open={logsModalOpen} onOpenChange={(open) => {
        setLogsModalOpen(open);
        if (open && selectedPod && selectedPod.containers.length > 0) {
          if (!selectedContainerForLogs) {
            setSelectedContainerForLogs(selectedPod.containers[0].name);
          }
          // Inicia auto-refresh automaticamente ao abrir
          setIsAutoRefreshingLogs(true);
        } else {
          // Para auto-refresh ao fechar
          setIsAutoRefreshingLogs(false);
        }
      }}>
        <DialogContent className="max-w-6xl max-h-[90vh] flex flex-col p-0">
          <DialogHeader className="border-b border-border px-6 py-4 flex-shrink-0">
            <div className="flex items-center justify-between gap-4 pr-8">
              <div>
                <DialogTitle className="text-xl font-semibold">
                  Logs do Pod
                </DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground mt-1">
                  {selectedPod?.namespace}/{selectedPod?.name}
                </DialogDescription>
              </div>
              <div className="flex items-center gap-3">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">Container:</span>
                  <Select
                    value={selectedContainerForLogs}
                    onValueChange={setSelectedContainerForLogs}
                  >
                    <SelectTrigger className="w-[200px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedPod?.containers.map((container) => (
                        <SelectItem key={container.name} value={container.name}>
                          {container.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <Button
                  variant={isAutoRefreshingLogs ? "destructive" : "default"}
                  size="sm"
                  onClick={() => setIsAutoRefreshingLogs(!isAutoRefreshingLogs)}
                >
                  {isAutoRefreshingLogs ? (
                    <>
                      <X className="w-4 h-4 mr-2" />
                      Parar
                    </>
                  ) : (
                    <>
                      <RefreshCcw className="w-4 h-4 mr-2" />
                      Iniciar
                    </>
                  )}
                </Button>
                {podLogs[selectedContainerForLogs] && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleDownloadLogs(selectedContainerForLogs)}
                  >
                    <Download className="w-4 h-4 mr-2" />
                    Download
                  </Button>
                )}
              </div>
            </div>
          </DialogHeader>

          <div className="flex-1 overflow-hidden p-6">
            {podLogs[selectedContainerForLogs] ? (
              <ScrollArea className="h-[calc(90vh-200px)] border rounded-lg bg-black/95">
                <pre className="p-4 text-xs font-mono text-green-400 whitespace-pre-wrap break-words">
                  <code>{podLogs[selectedContainerForLogs]}</code>
                </pre>
              </ScrollArea>
            ) : (
              <div className="flex items-center justify-center h-[calc(90vh-200px)] border border-dashed rounded-lg text-muted-foreground text-sm">
                {isAutoRefreshingLogs ? (
                  <div className="flex flex-col items-center gap-2">
                    <RefreshCcw className="w-6 h-6 animate-spin" />
                    <span>Carregando logs automaticamente...</span>
                  </div>
                ) : (
                  <span>Clique em "Iniciar" para carregar os logs</span>
                )}
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {selectedPod?.name}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {selectedPod?.namespace}/{selectedPod?.name}
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

      {/* Modal Fullscreen de Edição */}
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent 
          className="max-w-[98vw] max-h-[98vh] w-full h-full p-0"
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setEditorFullScreen(false);
            }
          }}
        >
          <div className="h-full flex flex-col">
            <DialogHeader className="border-b border-border px-6 py-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <DialogTitle className="text-xl font-semibold text-primary">
                    Editor YAML - Tela Cheia
                  </DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground">
                    {selectedPod?.name} • {selectedPod?.namespace} • {cluster}
                  </DialogDescription>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  {/* Undo/Redo */}
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

                  {/* View Mode Toggle */}
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
                      disabled={!selectedPod || isValidating}
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
                    onClick={handleCancelEdit}
                    disabled={!hasChanges}
                  >
                    <X className="w-4 h-4 mr-2" />
                    Cancelar
                  </Button>

                  <ProtectedAction>
                    <Button
                      variant="default"
                      size="sm"
                      onClick={handleApplyChanges}
                      disabled={!hasChanges || isApplying}
                    >
                      {isApplying ? (
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                      ) : (
                        <Check className="w-4 h-4 mr-2" />
                      )}
                      Aplicar
                    </Button>
                  </ProtectedAction>

                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditorFullScreen(false)}
                  >
                    <X className="w-4 h-4" />
                  </Button>
                </div>
              </div>
            </DialogHeader>

            <div className="flex-1 p-6 overflow-hidden">
              <div className="h-full">
                {viewMode === "editor" ? (
                  <MonacoYamlEditor
                    value={editedYaml}
                    onChange={(value) => {
                      setEditedYaml(value || "");
                      if (value && value !== yamlHistory[historyIndex]) {
                        addToHistory(value);
                      }
                    }}
                    readOnly={false}
                    height="calc(98vh - 180px)"
                  />
                ) : (
                  <MonacoYamlEditor
                    mode="diff"
                    originalValue={originalYaml}
                    value={editedYaml}
                    height="calc(98vh - 180px)"
                    readOnly
                  />
                )}
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Modal de Diff */}
      <Dialog open={showDiffModal} onOpenChange={setShowDiffModal}>
        <DialogContent className="max-w-6xl max-h-[90vh] overflow-hidden">
          <DialogHeader>
            <DialogTitle>Visualizar Mudanças (Diff)</DialogTitle>
            <DialogDescription>
              Comparação entre o YAML original e o editado
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="h-[70vh]">
            <div
              className="diff-container text-xs"
              dangerouslySetInnerHTML={{ __html: diffHtml }}
            />
          </ScrollArea>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDiffModal(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Transferência de Arquivos */}
      {selectedPod && (
        <PodFileTransferModal
          open={showFileTransferModal}
          onOpenChange={setShowFileTransferModal}
          cluster={cluster}
          namespace={selectedPod.namespace}
          podName={selectedPod.name}
          containers={selectedPod.containers.map(c => c.name)}
        />
      )}
    </>
  );
};
