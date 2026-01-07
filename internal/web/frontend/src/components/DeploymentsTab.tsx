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
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, PanelLeftClose, PanelLeftOpen, FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, X, FileText, Brain, TrendingUp, BarChart3, Download, History, Server, MoreVertical, Trash2, RotateCw } from "lucide-react";
import { toast } from "sonner";
import yaml from "js-yaml";
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts';
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";

import type {
  Namespace,
  DeploymentSummary,
  DeploymentManifest,
} from "@/lib/api/types";
import { useDeployments } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { AITriggerButton } from "@/components/AITriggerButton";
import { PredictionHistoryModal } from "@/components/PredictionHistoryModal";
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

interface DeploymentsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const DeploymentsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: DeploymentsTabProps) => {
  // Estados com persistência entre trocas de aba
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>('deployments', 'searchQuery', "");
  const [selectedDeployment, setSelectedDeployment] = usePersistedTabState<DeploymentSummary | null>('deployments', 'selectedDeployment', null);
  const [showLabels, setShowLabels] = usePersistedTabState<boolean>('deployments', 'showLabels', false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = usePersistedTabState<boolean>('deployments', 'isSidebarCollapsed', false);
  const [viewMode, setViewMode] = usePersistedTabState<"editor" | "diff">('deployments', 'viewMode', "editor");

  // Estados locais (não persistidos)
  const [manifest, setManifest] = useState<DeploymentManifest | null>(null);
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
  const [predictionModalOpen, setPredictionModalOpen] = useState(false);
  const [predictionLoading, setPredictionLoading] = useState(false);
  const [predictionResult, setPredictionResult] = useState<any>(null);
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportFormat, setExportFormat] = useState<"pdf" | "markdown" | "json">("markdown");
  const [isExporting, setIsExporting] = useState(false);
  const [historyModalOpen, setHistoryModalOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [rolloutConfirmOpen, setRolloutConfirmOpen] = useState(false);
  const [isRestarting, setIsRestarting] = useState(false);

  // Debug: monitor modal state changes
  useEffect(() => {
    console.log("[PredictiveAnalysis] predictionModalOpen changed to:", predictionModalOpen);
  }, [predictionModalOpen]);

  useEffect(() => {
    console.log("[PredictiveAnalysis] predictionResult changed:", predictionResult);
  }, [predictionResult]);

  // Helper: Detectar deployment problemático
  const isDeploymentProblematic = (dep: DeploymentSummary): boolean => {
    // availableReplicas < desiredReplicas
    if (dep.availableReplicas < dep.replicas) return true;
    
    // readyReplicas < desiredReplicas
    if (dep.readyReplicas < dep.replicas) return true;
    
    return false;
  };

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
  const { deployments, loading, error, refetch } = useDeployments(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar Deployments", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedDeployment(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(false);
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
  }, [cluster, selectedNamespace]);

  const filteredDeployments = useMemo(() => {
    if (!searchQuery) return deployments;
    const query = searchQuery.toLowerCase();
    return deployments.filter((dep) => {
      return (
        dep.name.toLowerCase().includes(query) ||
        dep.namespace.toLowerCase().includes(query) ||
        Object.entries(dep.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [deployments, searchQuery]);

  const handleSelectDeployment = async (summary: DeploymentSummary) => {
    // Salvar histórico atual no cache antes de trocar
    if (selectedDeployment && history.length > 0) {
      const cacheKey = `${selectedDeployment.namespace}/${selectedDeployment.name}`;
      historyCache.current.set(cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedDeployment(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getDeployment(
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

  const refreshDeployments = () => {
    if (!cluster) return;
    refetch();
  };

  const refreshManifest = async () => {
    if (!selectedDeployment) return;
    
    setManifestLoading(true);
    try {
      // Buscar deployment atualizado do servidor
      const detail = await apiClient.getDeployment(
        selectedDeployment.cluster,
        selectedDeployment.namespace,
        selectedDeployment.name
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

  const handleShowDiffModal = async (options?: { fullscreen?: boolean }) => {
    if (!selectedDeployment) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setIsDiffLoading(true);
    try {
      const diffResponse = await apiClient.diffDeployment(originalYaml, editorValue, selectedDeployment?.name);
      const unifiedDiff = diffResponse.unifiedDiff || "";
      const html = diff2html(unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffFullScreen(!!options?.fullscreen);
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
    if (!selectedDeployment) return;
    setIsValidating(true);
    try {
      await apiClient.validateDeployment({
        cluster: selectedDeployment.cluster,
        namespace: selectedDeployment.namespace,
        yaml: editorValue,
        fieldManager: "web-deployment-editor",
      });
      toast.success("Dry-run bem-sucedido", {
        description: `${selectedDeployment.namespace}/${selectedDeployment.name}`,
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
    if (!selectedDeployment) return;
    setIsApplying(true);
    try {
      await apiClient.applyDeployment(
        selectedDeployment.cluster,
        selectedDeployment.namespace,
        selectedDeployment.name,
        {
          yaml: editorValue,
          fieldManager: "web-deployment-editor",
          dryRun: false,
        }
      );
      toast.success("Deployment aplicado", {
        description: `${selectedDeployment.namespace}/${selectedDeployment.name}`,
      });
      
      // Recarregar manifest do servidor após aplicar
      await refreshManifest();
    } catch (err) {
      toast.error("Falha ao aplicar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsApplying(false);
    }
  };

  const openApplyConfirm = () => {
    if (!selectedDeployment) return;
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

  const handleViewDescribe = async () => {
    if (!selectedDeployment) return;

    setDescribeLoading(true);
    setDescribeModalOpen(true);
    try {
      const result = await apiClient.describeDeployment(selectedDeployment.cluster, selectedDeployment.namespace, selectedDeployment.name);
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

  // Handler customizado para controlar quando o modal de predição pode fechar
  const handlePredictionModalChange = useCallback((open: boolean) => {
    console.log("[PredictiveAnalysis] onOpenChange called with:", open);
    console.log("[PredictiveAnalysis] Current states:", { predictionLoading, predictionResult });

    // Só permitir fechar o modal se não estiver carregando
    if (!open && predictionLoading) {
      console.log("[PredictiveAnalysis] Prevented closing while loading");
      return;
    }

    console.log("[PredictiveAnalysis] Setting predictionModalOpen to:", open);
    setPredictionModalOpen(open);
  }, [predictionLoading, predictionResult]);

  // Nova função para análise preditiva
  const handlePredictiveAnalysis = async () => {
    console.log("[PredictiveAnalysis] Button clicked");
    console.log("[PredictiveAnalysis] selectedDeployment:", selectedDeployment);
    console.log("[PredictiveAnalysis] Current predictionModalOpen:", predictionModalOpen);

    if (!selectedDeployment) {
      console.error("[PredictiveAnalysis] No deployment selected!");
      return;
    }

    // Show loading toast
    toast.info("Iniciando análise preditiva...", {
      description: "Coletando métricas e executando análise com IA",
      duration: 5000,
    });

    setPredictionLoading(true);
    setPredictionResult(null);

    // Usar setTimeout para garantir que o modal abre DEPOIS do estado ser atualizado
    setTimeout(() => {
      console.log("[PredictiveAnalysis] Opening modal via setTimeout");
      setPredictionModalOpen(true);
    }, 50);

    console.log("[PredictiveAnalysis] State after set:", { predictionLoading: true });
    console.log("[PredictiveAnalysis] Sending request...");

    try {
      const requestBody = {
        cluster: selectedDeployment.cluster,
        namespace: selectedDeployment.namespace,
        deployment: selectedDeployment.name,
      };

      console.log("[PredictiveAnalysis] Request body:", requestBody);

      const response = await fetch("/api/v1/predictions/analyze", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
        body: JSON.stringify(requestBody),
      });

      console.log("[PredictiveAnalysis] Response status:", response.status);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const result = await response.json();
      console.log("[PredictiveAnalysis] Result:", result);
      console.log("[PredictiveAnalysis] raw_metrics:", result.raw_metrics);
      console.log("[PredictiveAnalysis] competing_apps:", result.raw_metrics?.competing_apps);
      console.log("[PredictiveAnalysis] node_metrics:", result.raw_metrics?.node_metrics);
      console.log("[PredictiveAnalysis] executive_summary:", result.executive_summary);
      console.log("[PredictiveAnalysis] predictions:", result.predictions);
      console.log("[PredictiveAnalysis] recommendations:", result.recommendations);
      setPredictionResult(result);

      toast.success("Análise preditiva concluída!", {
        description: `Health Score: ${result.health_score.overall}/100 (${result.health_score.category})`,
      });
    } catch (err) {
      console.error("[PredictiveAnalysis] Error:", err);
      toast.error("Erro na análise preditiva", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
      setPredictionResult({ error: err instanceof Error ? err.message : "Unknown error" });
    } finally {
      setPredictionLoading(false);
    }
  };

  // Handler para deletar deployment
  const handleDeleteDeployment = async () => {
    if (!selectedDeployment) return;

    setIsDeleting(true);

    try {
      const response = await fetch(
        `/api/v1/deployments/${selectedDeployment.cluster}/${selectedDeployment.namespace}/${selectedDeployment.name}`,
        {
          method: "DELETE",
          headers: {
            Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
          },
        }
      );

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || `HTTP ${response.status}`);
      }

      toast.success("Deployment deletado com sucesso!", {
        description: `${selectedDeployment.namespace}/${selectedDeployment.name}`,
      });

      // Limpar seleção e recarregar lista
      setSelectedDeployment(null);
      setManifest(null);
      setEditorValue("");
      setOriginalYaml("");
      setDeleteConfirmOpen(false);
      await refetch();
    } catch (err) {
      console.error("[DeleteDeployment] Error:", err);
      toast.error("Erro ao deletar deployment", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  // Handler para rollout restart deployment
  const handleRolloutRestart = async () => {
    if (!selectedDeployment) return;

    setIsRestarting(true);

    try {
      const response = await fetch(
        `/api/v1/deployments/${selectedDeployment.cluster}/${selectedDeployment.namespace}/${selectedDeployment.name}/restart`,
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
          },
        }
      );

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || `HTTP ${response.status}`);
      }

      toast.success("Rollout restart iniciado com sucesso!", {
        description: `${selectedDeployment.namespace}/${selectedDeployment.name}`,
      });

      setRolloutConfirmOpen(false);

      // Recarregar manifest após alguns segundos para ver o restart
      setTimeout(async () => {
        await refreshManifest();
      }, 2000);
    } catch (err) {
      console.error("[RolloutRestart] Error:", err);
      toast.error("Erro ao reiniciar deployment", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
    } finally {
      setIsRestarting(false);
    }
  };

  // Função para gerar PDF profissional usando jsPDF
  const generatePredictionPDF = () => {
    if (!predictionResult || !selectedDeployment) return;

    const doc = new jsPDF({
      orientation: "portrait",
      unit: "mm",
      format: "a4",
    });

    const pageWidth = doc.internal.pageSize.getWidth();
    const pageHeight = doc.internal.pageSize.getHeight();
    let yPosition = 20;

    // Helper para quebra de página
    const checkPageBreak = (neededSpace: number) => {
      if (yPosition + neededSpace > pageHeight - 20) {
        doc.addPage();
        yPosition = 20;
        return true;
      }
      return false;
    };

    // Header azul/roxo gradiente (simulated)
    doc.setFillColor(79, 70, 229); // Indigo
    doc.rect(0, 0, pageWidth, 40, "F");

    // Título
    doc.setFontSize(20);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(255, 255, 255);
    doc.text("ANALISE PREDITIVA", pageWidth / 2, 15, { align: "center" });

    // Subtítulo - Deployment
    doc.setFontSize(14);
    doc.setFont("helvetica", "normal");
    doc.text(selectedDeployment.name, pageWidth / 2, 24, { align: "center" });

    // Info adicional
    doc.setFontSize(9);
    doc.text(`Cluster: ${selectedDeployment.cluster} | Namespace: ${selectedDeployment.namespace}`, pageWidth / 2, 30, { align: "center" });
    doc.text(`Gerado em: ${new Date().toLocaleString("pt-BR")}`, pageWidth / 2, 35, { align: "center" });

    // Reset cor do texto
    doc.setTextColor(0, 0, 0);
    yPosition = 50;

    // ===== DADOS ANALISADOS =====
    checkPageBreak(40);
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(79, 70, 229);
    doc.text("DADOS ANALISADOS", 14, yPosition);
    yPosition += 8;

    doc.setFontSize(9);
    doc.setFont("helvetica", "normal");
    doc.setTextColor(0, 0, 0);
    const introText = "Esta análise foi baseada nas seguintes métricas e observações do deployment:";
    doc.text(introText, 14, yPosition);
    yPosition += 8;

    // Métricas de Réplicas
    checkPageBreak(20);
    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.text("Métricas de Réplicas", 14, yPosition);
    yPosition += 6;
    doc.setFontSize(9);
    doc.setFont("helvetica", "normal");
    const replicas = predictionResult.raw_metrics;
    if (replicas) {
      doc.text(`Réplicas Desejadas: ${replicas.desired_replicas || 0}`, 14, yPosition);
      yPosition += 4;
      doc.text(`Réplicas Disponíveis: ${replicas.available_replicas || 0}`, 14, yPosition);
      yPosition += 4;
      doc.text(`Réplicas Prontas: ${replicas.ready_replicas || 0}`, 14, yPosition);
      yPosition += 4;
      const disponibilidade = replicas.desired_replicas > 0 ? (replicas.available_replicas / replicas.desired_replicas * 100).toFixed(1) : 0;
      doc.text(`Taxa de Disponibilidade: ${disponibilidade}%`, 14, yPosition);
      yPosition += 8;
    }

    // Consumo de Recursos
    checkPageBreak(20);
    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.text("Consumo de Recursos", 14, yPosition);
    yPosition += 6;
    doc.setFontSize(9);
    doc.setFont("helvetica", "normal");
    if (replicas?.current) {
      doc.text(`CPU Média: ${(replicas.current.cpu_usage_avg || 0).toFixed(2)} cores (P95: ${(replicas.current.cpu_usage_p95 || 0).toFixed(2)} cores)`, 14, yPosition);
      yPosition += 4;
      const memAvgGB = (replicas.current.memory_usage_avg || 0) / (1024 * 1024 * 1024);
      const memP95GB = (replicas.current.memory_usage_p95 || 0) / (1024 * 1024 * 1024);
      doc.text(`Memória Média: ${memAvgGB.toFixed(2)} GB (P95: ${memP95GB.toFixed(2)} GB)`, 14, yPosition);
      yPosition += 4;
    }
    if (replicas?.trends) {
      doc.text(`Tendência CPU (7 dias): ${(replicas.trends.cpu_change_7d || 0).toFixed(1)}%`, 14, yPosition);
      yPosition += 4;
      doc.text(`Tendência Memória (7 dias): ${(replicas.trends.memory_change_7d || 0).toFixed(1)}%`, 14, yPosition);
      yPosition += 8;
    }

    // Capacidade do Cluster
    checkPageBreak(15);
    doc.setFontSize(10);
    doc.setFont("helvetica", "bold");
    doc.text("Capacidade do Cluster", 14, yPosition);
    yPosition += 6;
    doc.setFontSize(9);
    doc.setFont("helvetica", "normal");
    if (replicas?.node_metrics?.total_capacity) {
      const capacity = replicas.node_metrics.total_capacity;
      doc.text(`CPU Total Disponível: ${(capacity.cpu_total || 0).toFixed(2)} cores (Utilização: ${(capacity.cpu_utilization || 0).toFixed(1)}%)`, 14, yPosition);
      yPosition += 4;
      doc.text(`Memória Total: ${(capacity.mem_total || 0).toFixed(2)} GB (Utilização: ${(capacity.mem_utilization || 0).toFixed(1)}%)`, 14, yPosition);
      yPosition += 4;
      doc.text(`Nodes: ${replicas.node_metrics.nodes_used || 0}/${replicas.node_metrics.total_nodes_in_cluster || 0} em uso`, 14, yPosition);
      yPosition += 12;
    }

    // Tipo de VM/Instance
    if (replicas?.node_metrics?.vm_sizing?.predominant_instance_type) {
      checkPageBreak(20);
      doc.setFontSize(10);
      doc.setFont("helvetica", "bold");
      doc.text("Tipo de VM/Instance", 14, yPosition);
      yPosition += 6;
      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      const vmSizing = replicas.node_metrics.vm_sizing;
      doc.text(`Tipo Predominante: ${vmSizing.predominant_instance_type}`, 14, yPosition);
      yPosition += 4;
      if (vmSizing.cpu_per_vm_cores > 0) {
        doc.text(`CPU por VM: ${vmSizing.cpu_per_vm_cores} cores`, 14, yPosition);
        yPosition += 4;
      }
      if (vmSizing.memory_per_vm_gb > 0) {
        doc.text(`Memória por VM: ${vmSizing.memory_per_vm_gb} GB`, 14, yPosition);
        yPosition += 4;
      }
      if (vmSizing.max_pods_per_node > 0) {
        doc.text(`Máximo de Pods por Node: ${vmSizing.max_pods_per_node}`, 14, yPosition);
        yPosition += 4;
      }
      if (vmSizing.recommended_instance_type) {
        doc.text(`Tipo Recomendado: ${vmSizing.recommended_instance_type}`, 14, yPosition);
        yPosition += 4;
        if (vmSizing.recommendation_reason) {
          const reasonLines = doc.splitTextToSize(`Razão: ${vmSizing.recommendation_reason}`, pageWidth - 28);
          reasonLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
        }
      }
      yPosition += 8;
    }

    // Aplicações Concorrentes
    if (replicas?.competing_apps?.length > 0) {
      checkPageBreak(25);
      doc.setFontSize(10);
      doc.setFont("helvetica", "bold");
      doc.text("Aplicações Concorrentes nas Mesmas VMs", 14, yPosition);
      yPosition += 6;
      doc.setFontSize(9);
      doc.setFont("helvetica", "bold");
      doc.text("IMPORTANTE:", 14, yPosition);
      yPosition += 4;
      doc.setFont("helvetica", "normal");
      const importantText = doc.splitTextToSize("As VMs/Nodes não dispõem de recursos totais para este deployment. Os recursos são compartilhados e há concorrência com outras aplicações:", pageWidth - 28);
      importantText.forEach((line: string) => {
        doc.text(line, 14, yPosition);
        yPosition += 4;
      });
      yPosition += 4;

      const highImpact = replicas.competing_apps.filter((app: any) => app.impact_level === "high");
      const mediumImpact = replicas.competing_apps.filter((app: any) => app.impact_level === "medium");
      const lowImpact = replicas.competing_apps.filter((app: any) => app.impact_level === "low" || !app.impact_level);

      if (highImpact.length > 0) {
        checkPageBreak(15);
        doc.setFont("helvetica", "bold");
        doc.text("Impacto Alto:", 14, yPosition);
        yPosition += 5;
        doc.setFont("helvetica", "normal");
        highImpact.forEach((app: any) => {
          checkPageBreak(8);
          doc.text(`• ${app.name} (${app.namespace})`, 14, yPosition);
          yPosition += 4;
          doc.text(`  CPU: ${app.cpu_usage_cores.toFixed(2)} cores | Mem: ${app.memory_usage_gb.toFixed(2)} GB`, 14, yPosition);
          yPosition += 5;
        });
      }

      if (mediumImpact.length > 0) {
        checkPageBreak(15);
        doc.setFont("helvetica", "bold");
        doc.text("Impacto Médio:", 14, yPosition);
        yPosition += 5;
        doc.setFont("helvetica", "normal");
        mediumImpact.forEach((app: any) => {
          checkPageBreak(6);
          doc.text(`• ${app.name} (${app.namespace}): CPU ${app.cpu_usage_cores.toFixed(2)} | Mem ${app.memory_usage_gb.toFixed(2)} GB`, 14, yPosition);
          yPosition += 4;
        });
        yPosition += 2;
      }

      if (lowImpact.length > 0 && lowImpact.length <= 5) {
        checkPageBreak(12);
        doc.setFont("helvetica", "bold");
        doc.text("Impacto Baixo:", 14, yPosition);
        yPosition += 5;
        doc.setFont("helvetica", "normal");
        lowImpact.slice(0, 5).forEach((app: any) => {
          checkPageBreak(5);
          doc.text(`• ${app.name}: CPU ${app.cpu_usage_cores.toFixed(2)} | Mem ${app.memory_usage_gb.toFixed(2)} GB`, 14, yPosition);
          yPosition += 4;
        });
        yPosition += 2;
      } else if (lowImpact.length > 5) {
        doc.text(`... e mais ${lowImpact.length} aplicações de baixo impacto`, 14, yPosition);
        yPosition += 6;
      }

      // Totais
      const totalCPU = replicas.competing_apps.reduce((sum: number, app: any) => sum + (app.cpu_usage_cores || 0), 0);
      const totalMem = replicas.competing_apps.reduce((sum: number, app: any) => sum + (app.memory_usage_gb || 0), 0);
      checkPageBreak(6);
      doc.setFont("helvetica", "bold");
      doc.text(`Total Consumido: ${totalCPU.toFixed(2)} cores CPU | ${totalMem.toFixed(2)} GB Mem`, 14, yPosition);
      yPosition += 10;
    }

    // ===== HEALTH SCORE =====
    checkPageBreak(30);
    doc.setFontSize(16);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(79, 70, 229);
    doc.text("HEALTH SCORE", 14, yPosition);
    yPosition += 8;

    doc.setFontSize(9);
    doc.setFont("helvetica", "normal");
    doc.setTextColor(0, 0, 0);
    doc.text("O Health Score é calculado com base em 4 dimensões principais:", 14, yPosition);
    yPosition += 8;

    // Box com score
    const score = predictionResult.health_score?.overall || 0;
    const scoreColor = score >= 75 ? [34, 197, 94] : score >= 50 ? [234, 179, 8] : [239, 68, 68];
    doc.setFillColor(...(scoreColor as [number, number, number]));
    doc.roundedRect(14, yPosition, 40, 20, 3, 3, "F");
    doc.setFontSize(28);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(255, 255, 255);
    doc.text(`${score}`, 34, yPosition + 14, { align: "center" });
    
    // Breakdown com interpretação
    doc.setFontSize(9);
    doc.setTextColor(0, 0, 0);
    doc.setFont("helvetica", "normal");
    const breakdown = predictionResult.health_score?.breakdown;
    if (breakdown) {
      const getInterpretation = (score: number) => {
        if (score >= 90) return "Excelente";
        if (score >= 75) return "Bom, mas melhorável";
        return "Precisa atenção";
      };
      doc.text(`Availability: ${breakdown.availability} - ${getInterpretation(breakdown.availability)}`, 60, yPosition + 5);
      doc.text(`Performance: ${breakdown.performance} - ${getInterpretation(breakdown.performance)}`, 60, yPosition + 10);
      doc.text(`Stability: ${breakdown.stability} - ${getInterpretation(breakdown.stability)}`, 60, yPosition + 15);
      doc.text(`Efficiency: ${breakdown.efficiency} - ${getInterpretation(breakdown.efficiency)}`, 60, yPosition + 20);
    }
    yPosition += 28;

    // Explicação das dimensões
    checkPageBreak(25);
    doc.setFontSize(9);
    doc.setFont("helvetica", "bold");
    doc.text("Como interpretamos estes scores:", 14, yPosition);
    yPosition += 5;
    doc.setFont("helvetica", "normal");
    doc.text("• Availability: Mede a taxa de réplicas disponíveis vs. desejadas e histórico de downtime", 14, yPosition);
    yPosition += 4;
    doc.text("• Performance: Avalia utilização de CPU/memória, latência e capacidade de resposta", 14, yPosition);
    yPosition += 4;
    doc.text("• Stability: Considera restarts, crashloops, erros de health checks e variação de réplicas", 14, yPosition);
    yPosition += 4;
    doc.text("• Efficiency: Analisa otimização de recursos, desperdício e relação requests/limits", 14, yPosition);
    yPosition += 10;

    // ===== RESUMO EXECUTIVO =====
    checkPageBreak(40);
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(79, 70, 229);
    doc.text("RESUMO EXECUTIVO", 14, yPosition);
    yPosition += 8;

    // Risk Level badge
    const riskLevel = predictionResult.executive_summary?.risk_level || "unknown";
    const riskColors: Record<string, number[]> = {
      critical: [239, 68, 68],
      high: [249, 115, 22],
      medium: [234, 179, 8],
      low: [34, 197, 94],
    };
    const riskColor = riskColors[riskLevel] || [100, 100, 100];
    doc.setFillColor(...(riskColor as [number, number, number]));
    doc.roundedRect(14, yPosition, 30, 6, 2, 2, "F");
    doc.setFontSize(9);
    doc.setFont("helvetica", "bold");
    doc.setTextColor(255, 255, 255);
    doc.text(riskLevel.toUpperCase(), 29, yPosition + 4.5, { align: "center" });
    yPosition += 10;

    // Current state
    doc.setFontSize(10);
    doc.setTextColor(0, 0, 0);
    doc.setFont("helvetica", "normal");
    const currentState = predictionResult.executive_summary?.current_state || "";
    const stateLines = doc.splitTextToSize(currentState, pageWidth - 28);
    stateLines.forEach((line: string) => {
      checkPageBreak(6);
      doc.text(line, 14, yPosition);
      yPosition += 5;
    });
    yPosition += 5;

    // Key findings
    if (predictionResult.executive_summary?.key_findings?.length > 0) {
      checkPageBreak(15);
      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text("Principais Descobertas:", 14, yPosition);
      yPosition += 6;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      predictionResult.executive_summary.key_findings.forEach((finding: string) => {
        checkPageBreak(6);
        const findingLines = doc.splitTextToSize(`• ${finding}`, pageWidth - 28);
        findingLines.forEach((line: string) => {
          doc.text(line, 14, yPosition);
          yPosition += 5;
        });
      });
      yPosition += 5;
    }

    // ===== PREVISOES =====
    const hasShortTerm = predictionResult.predictions?.short_term?.length > 0;
    const hasMediumTerm = predictionResult.predictions?.medium_term?.length > 0;
    const hasLongTerm = predictionResult.predictions?.long_term?.length > 0;

    if (hasShortTerm || hasMediumTerm || hasLongTerm) {
      checkPageBreak(20);
      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(79, 70, 229);
      doc.text("PREVISOES", 14, yPosition);
      yPosition += 8;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(0, 0, 0);
      const predIntro = doc.splitTextToSize("Com base nos dados coletados e padrões identificados, a IA prevê os seguintes eventos. Cada previsão inclui o nível de severidade, probabilidade de ocorrência e os indicadores que levaram à conclusão.", pageWidth - 28);
      predIntro.forEach((line: string) => {
        doc.text(line, 14, yPosition);
        yPosition += 4;
      });
      yPosition += 4;

      const addPredictions = (predictions: any[], title: string) => {
        if (predictions && predictions.length > 0) {
          checkPageBreak(10);
          doc.setFontSize(11);
          doc.setFont("helvetica", "bold");
          doc.setTextColor(0, 0, 0);
          doc.text(title, 14, yPosition);
          yPosition += 6;

          predictions.forEach((pred: any) => {
            checkPageBreak(18);
            doc.setFontSize(9);
            doc.setFont("helvetica", "bold");
            const severityText = pred.severity ? `[${pred.severity.toUpperCase()}]` : "";
            const probText = pred.probability ? ` (${(pred.probability * 100).toFixed(0)}%)` : "";
            doc.text(`• ${severityText} ${pred.event || pred.timeframe}${probText}`, 14, yPosition);
            yPosition += 5;
            
            doc.setFont("helvetica", "normal");
            if (pred.timestamp) {
              doc.text(`  Timestamp: ${new Date(pred.timestamp).toLocaleDateString("pt-BR")}`, 14, yPosition);
              yPosition += 4;
            }
            if (pred.impact) {
              const impactLines = doc.splitTextToSize(`  Impacto: ${pred.impact}`, pageWidth - 28);
              impactLines.forEach((line: string) => {
                checkPageBreak(5);
                doc.text(line, 14, yPosition);
                yPosition += 4;
              });
            }
            if (pred.indicators && pred.indicators.length > 0) {
              doc.text("  Indicadores:", 14, yPosition);
              yPosition += 4;
              pred.indicators.forEach((ind: string) => {
                checkPageBreak(5);
                const indLines = doc.splitTextToSize(`    - ${ind}`, pageWidth - 28);
                indLines.forEach((line: string) => {
                  doc.text(line, 14, yPosition);
                  yPosition += 4;
                });
              });
            }
            yPosition += 3;
          });
        }
      };

      if (hasShortTerm) addPredictions(predictionResult.predictions.short_term, "Curto Prazo (4h)");
      if (hasMediumTerm) addPredictions(predictionResult.predictions.medium_term, "Medio Prazo (24h)");
      if (hasLongTerm) addPredictions(predictionResult.predictions.long_term, "Longo Prazo (7d)");
      
      yPosition += 5;
    }

    // ===== ANALISE DE CAUSA RAIZ =====
    if (predictionResult.root_cause_analysis?.identified_causes?.length > 0) {
      checkPageBreak(20);
      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(79, 70, 229);
      doc.text("ANALISE DE CAUSA RAIZ", 14, yPosition);
      yPosition += 8;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(0, 0, 0);
      const rcaIntro = doc.splitTextToSize("A análise identificou as seguintes causas com base em evidências concretas das métricas coletadas. O nível de certeza indica a confiança da IA na identificação da causa.", pageWidth - 28);
      rcaIntro.forEach((line: string) => {
        doc.text(line, 14, yPosition);
        yPosition += 4;
      });
      yPosition += 4;

      if (predictionResult.root_cause_analysis.primary_factor) {
        checkPageBreak(8);
        doc.setFont("helvetica", "bold");
        doc.text("Fator Primário Identificado:", 14, yPosition);
        yPosition += 5;
        doc.setFont("helvetica", "normal");
        const primaryLines = doc.splitTextToSize(predictionResult.root_cause_analysis.primary_factor, pageWidth - 28);
        primaryLines.forEach((line: string) => {
          checkPageBreak(5);
          doc.text(line, 14, yPosition);
          yPosition += 4;
        });
        yPosition += 4;
      }

      predictionResult.root_cause_analysis.identified_causes.forEach((cause: any, idx: number) => {
        checkPageBreak(20);
        doc.setFontSize(10);
        doc.setFont("helvetica", "bold");
        const certainty = cause.certainty ? ` (Certeza: ${(cause.certainty * 100).toFixed(0)}%)` : "";
        doc.text(`Causa ${idx + 1}: ${cause.cause}${certainty}`, 14, yPosition);
        yPosition += 6;

        doc.setFontSize(9);
        doc.setFont("helvetica", "normal");
        if (cause.category) {
          doc.text(`Categoria: ${cause.category}`, 14, yPosition);
          yPosition += 5;
        }

        if (cause.evidence && cause.evidence.length > 0) {
          doc.setFont("helvetica", "bold");
          doc.text("Evidências:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          cause.evidence.forEach((ev: string) => {
            checkPageBreak(5);
            const evLines = doc.splitTextToSize(`• ${ev}`, pageWidth - 28);
            evLines.forEach((line: string) => {
              doc.text(line, 14, yPosition);
              yPosition += 4;
            });
          });
          yPosition += 2;
        }

        if (cause.remediation) {
          checkPageBreak(8);
          doc.setFont("helvetica", "bold");
          doc.text("Remediação:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          const remLines = doc.splitTextToSize(cause.remediation, pageWidth - 28);
          remLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 4;
        }
      });

      if (predictionResult.root_cause_analysis.contributing_factors?.length > 0) {
        checkPageBreak(12);
        doc.setFont("helvetica", "bold");
        doc.text("Fatores Contribuintes:", 14, yPosition);
        yPosition += 5;
        doc.setFont("helvetica", "normal");
        predictionResult.root_cause_analysis.contributing_factors.forEach((factor: string) => {
          checkPageBreak(5);
          const factorLines = doc.splitTextToSize(`• ${factor}`, pageWidth - 28);
          factorLines.forEach((line: string) => {
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
        });
        yPosition += 5;
      }
    }

    // ===== ANALISE DE IMPACTO =====
    if (predictionResult.impact_analysis) {
      checkPageBreak(20);
      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(79, 70, 229);
      doc.text("ANALISE DE IMPACTO", 14, yPosition);
      yPosition += 8;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(0, 0, 0);
      doc.text("Esta seção compara dois cenários para ajudar na tomada de decisão:", 14, yPosition);
      yPosition += 8;

      // Cenário 1: Se Nenhuma Ação
      if (predictionResult.impact_analysis.if_no_action) {
        checkPageBreak(15);
        doc.setFontSize(11);
        doc.setFont("helvetica", "bold");
        doc.text("Cenário 1: Se Nenhuma Ação For Tomada", 14, yPosition);
        yPosition += 6;
        doc.setFontSize(9);
        doc.setFont("helvetica", "italic");
        doc.text("Análise do que acontecerá se o estado atual for mantido:", 14, yPosition);
        yPosition += 6;

        doc.setFont("helvetica", "normal");
        const noAction = predictionResult.impact_analysis.if_no_action;
        if (noAction.user_impact) {
          const userLines = doc.splitTextToSize(`Impacto nos Usuários: ${noAction.user_impact}`, pageWidth - 28);
          userLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 2;
        }
        if (noAction.infrastructure_impact) {
          const infraLines = doc.splitTextToSize(`Impacto na Infraestrutura: ${noAction.infrastructure_impact}`, pageWidth - 28);
          infraLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 2;
        }
        if (noAction.risks?.length > 0) {
          doc.setFont("helvetica", "bold");
          doc.text("Riscos:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          noAction.risks.forEach((risk: string) => {
            checkPageBreak(5);
            const riskLines = doc.splitTextToSize(`• ${risk}`, pageWidth - 28);
            riskLines.forEach((line: string) => {
              doc.text(line, 14, yPosition);
              yPosition += 4;
            });
          });
        }
        yPosition += 6;
      }

      // Cenário 2: Se Otimizações
      if (predictionResult.impact_analysis.if_optimizations_applied) {
        checkPageBreak(15);
        doc.setFontSize(11);
        doc.setFont("helvetica", "bold");
        doc.text("Cenário 2: Se Otimizações Forem Aplicadas", 14, yPosition);
        yPosition += 6;
        doc.setFontSize(9);
        doc.setFont("helvetica", "italic");
        doc.text("Análise dos benefícios esperados ao implementar as recomendações:", 14, yPosition);
        yPosition += 6;

        doc.setFont("helvetica", "normal");
        const withAction = predictionResult.impact_analysis.if_optimizations_applied;
        if (withAction.user_impact) {
          const userLines = doc.splitTextToSize(`Impacto nos Usuários: ${withAction.user_impact}`, pageWidth - 28);
          userLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 2;
        }
        if (withAction.infrastructure_impact) {
          const infraLines = doc.splitTextToSize(`Impacto na Infraestrutura: ${withAction.infrastructure_impact}`, pageWidth - 28);
          infraLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 2;
        }
        if (withAction.benefits?.length > 0) {
          doc.setFont("helvetica", "bold");
          doc.text("Benefícios:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          withAction.benefits.forEach((benefit: string) => {
            checkPageBreak(5);
            const benefitLines = doc.splitTextToSize(`• ${benefit}`, pageWidth - 28);
            benefitLines.forEach((line: string) => {
              doc.text(line, 14, yPosition);
              yPosition += 4;
            });
          });
        }
        yPosition += 6;
      }

      if (predictionResult.impact_analysis.recommended_action_priority) {
        checkPageBreak(8);
        doc.setFont("helvetica", "bold");
        doc.text(`Prioridade de Ação: ${predictionResult.impact_analysis.recommended_action_priority.toUpperCase()}`, 14, yPosition);
        yPosition += 5;
      }
      if (predictionResult.impact_analysis.timeline_to_action) {
        doc.text(`Timeline para Ação: ${predictionResult.impact_analysis.timeline_to_action}`, 14, yPosition);
        yPosition += 8;
      }
    }

    // ===== RECOMENDACOES =====
    if (predictionResult.recommendations?.length > 0) {
      checkPageBreak(20);
      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.setTextColor(79, 70, 229);
      doc.text("RECOMENDACOES", 14, yPosition);
      yPosition += 8;

      doc.setFontSize(9);
      doc.setFont("helvetica", "normal");
      doc.setTextColor(0, 0, 0);
      const recIntro = doc.splitTextToSize("As recomendações abaixo são ordenadas por prioridade e foram geradas considerando: o estado atual do deployment e suas métricas, os problemas identificados na análise de causa raiz, o impacto esperado vs. complexidade de implementação, as previsões de eventos futuros, e oportunidades de economia de custos.", pageWidth - 28);
      recIntro.forEach((line: string) => {
        doc.text(line, 14, yPosition);
        yPosition += 4;
      });
      yPosition += 4;

      // Verificar se há recomendações de custo
      const hasCostOptimization = predictionResult.recommendations.some(
        (rec: any) => rec.category === 'cost-optimization' || rec.category === 'downsizing'
      );

      // Alerta de economia de custos
      if (hasCostOptimization) {
        checkPageBreak(20);
        doc.setFillColor(255, 235, 59); // Amarelo
        doc.roundedRect(14, yPosition, pageWidth - 28, 18, 2, 2, "F");
        doc.setFontSize(10);
        doc.setFont("helvetica", "bold");
        doc.setTextColor(0, 0, 0);
        doc.text("💰 OPORTUNIDADE DE ECONOMIA DE CUSTOS IDENTIFICADA", pageWidth / 2, yPosition + 6, { align: "center" });
        yPosition += 10;
        doc.setFontSize(8);
        doc.setFont("helvetica", "normal");
        const alertText = doc.splitTextToSize("Há recursos sobreprovisionados que podem ser reduzidos sem impacto negativo, resultando em economia significativa.", pageWidth - 32);
        alertText.forEach((line: string) => {
          doc.text(line, pageWidth / 2, yPosition, { align: "center" });
          yPosition += 3.5;
        });
        yPosition += 8;
      }

      predictionResult.recommendations.slice(0, 5).forEach((rec: any, idx: number) => {
        checkPageBreak(20);
        doc.setFontSize(10);
        doc.setFont("helvetica", "bold");
        doc.setTextColor(0, 0, 0);
        
        // Destacar economia de custos
        if (rec.category === 'cost-optimization' || rec.category === 'downsizing') {
          doc.setTextColor(255, 152, 0); // Laranja
          doc.text(`💰 ${idx + 1}. ${rec.title}`, 14, yPosition);
          doc.setTextColor(0, 0, 0);
        } else {
          doc.text(`${idx + 1}. ${rec.title}`, 14, yPosition);
        }
        yPosition += 6;

        if (rec.category) {
          doc.setFontSize(9);
          doc.setFont("helvetica", "normal");
          doc.text(`Categoria: ${rec.category}`, 14, yPosition);
          yPosition += 5;
        }

        doc.setFont("helvetica", "bold");
        doc.text("Por que esta recomendação?", 14, yPosition);
        yPosition += 4;
        doc.setFont("helvetica", "normal");
        const descLines = doc.splitTextToSize(rec.description, pageWidth - 28);
        descLines.forEach((line: string) => {
          checkPageBreak(5);
          doc.text(line, 14, yPosition);
          yPosition += 4;
        });
        yPosition += 2;

        if (rec.expected_impact) {
          checkPageBreak(8);
          doc.setFont("helvetica", "bold");
          doc.text("Impacto Esperado:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          const impactLines = doc.splitTextToSize(rec.expected_impact, pageWidth - 28);
          impactLines.forEach((line: string) => {
            checkPageBreak(5);
            doc.text(line, 14, yPosition);
            yPosition += 4;
          });
          yPosition += 2;
        }

        if (rec.actions?.length > 0) {
          checkPageBreak(10);
          doc.setFont("helvetica", "bold");
          doc.text("Ações:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          rec.actions.forEach((action: string, actIdx: number) => {
            checkPageBreak(5);
            const actionLines = doc.splitTextToSize(`${actIdx + 1}. ${action}`, pageWidth - 28);
            actionLines.forEach((line: string) => {
              doc.text(line, 14, yPosition);
              yPosition += 4;
            });
          });
          yPosition += 2;
        }

        if (rec.implementation_estimate) {
          checkPageBreak(12);
          doc.setFont("helvetica", "bold");
          doc.text("Estimativa de Implementação:", 14, yPosition);
          yPosition += 4;
          doc.setFont("helvetica", "normal");
          const est = rec.implementation_estimate;
          if (est.time_required) {
            doc.text(`Tempo: ${est.time_required}`, 14, yPosition);
            yPosition += 4;
          }
          if (est.complexity) {
            doc.text(`Complexidade: ${est.complexity}`, 14, yPosition);
            yPosition += 4;
          }
          if (est.risk_level) {
            doc.text(`Risco: ${est.risk_level}`, 14, yPosition);
            yPosition += 4;
          }
          doc.text(`Requer Downtime: ${est.requires_downtime ? "SIM" : "NAO"}`, 14, yPosition);
          yPosition += 4;
          if (est.resource_efficiency_gain > 0) {
            doc.text(`Ganho de Eficiência: ${est.resource_efficiency_gain.toFixed(1)}%`, 14, yPosition);
            yPosition += 4;
          }
        }
        yPosition += 6;
      });
    }

    // Footer
    doc.setFontSize(8);
    doc.setTextColor(128, 128, 128);
    doc.text("Gerado por K8s HPA Manager - Analise Preditiva com IA", pageWidth / 2, pageHeight - 10, { align: "center" });

    // Salvar
    doc.save(`prediction-${selectedDeployment.name}-${Date.now()}.pdf`);
  };

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Deployments" : "Ocultar painel de Deployments"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

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
      <Button variant="outline" size="sm" onClick={refreshDeployments} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4 mr-2" /> Atualizar
      </Button>
      {collapseButton}
    </div>
  );

  const rightTitleAction = (
    <div className="flex gap-2">
      {selectedDeployment && (
        <>
          <Button
            variant="default"
            size="sm"
            onClick={handlePredictiveAnalysis}
            disabled={manifestLoading || predictionLoading}
            className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
          >
            {predictionLoading ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                Analisando...
              </>
            ) : (
              <>
                <TrendingUp className="w-4 h-4 mr-2" />
                Análise Preditiva
              </>
            )}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setHistoryModalOpen(true)}
            disabled={!selectedDeployment}
          >
            <History className="w-4 h-4 mr-2" />
            Histórico de Análises
          </Button>
          {isDeploymentProblematic(selectedDeployment) && (
            <AITriggerButton
              resourceType="Deployment"
              cluster={cluster}
              namespace={selectedDeployment.namespace}
              resourceName={selectedDeployment.name}
              size="sm"
              variant="outline"
            />
          )}
        </>
      )}
      <Button
        variant="outline"
        size="sm"
        onClick={refreshManifest}
        disabled={!selectedDeployment || manifestLoading}
      >
        <RefreshCcw className="w-4 h-4 mr-2" />
        Recarregar YAML
      </Button>
      {selectedDeployment && (
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
                Deletar Deployment
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ProtectedAction>
      )}
    </div>
  );

  const renderDeploymentList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Deployments
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando Deployments...
        </div>
      );
    }

    if (filteredDeployments.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {deployments.length === 0
            ? "Nenhum Deployment encontrado"
            : "Nenhum Deployment corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredDeployments.map((dep) => {
          const isSelected =
            selectedDeployment?.name === dep.name &&
            selectedDeployment?.namespace === dep.namespace;
          const hasProblems = isDeploymentProblematic(dep);
          const statusColor = hasProblems ? "text-red-400" : "text-green-400";
          
          return (
            <button
              key={`${dep.cluster}-${dep.namespace}-${dep.name}`}
              onClick={() => handleSelectDeployment(dep)}
              className={`w-full text-left p-3 rounded-lg border transition-colors relative ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : hasProblems
                  ? "border-red-500/40 hover:border-red-500/60 bg-red-500/5"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              {hasProblems && (
                <div className="absolute top-2 right-2 w-2 h-2 bg-red-500 rounded-full animate-pulse" title="Deployment com problemas" />
              )}
              <div className="flex items-center gap-2">
                <div className="font-semibold text-sm">{dep.name}</div>
                {hasProblems && (
                  <span className="px-1.5 py-0.5 bg-red-500/20 text-red-400 text-[10px] font-medium rounded">
                    !
                  </span>
                )}
              </div>
              <div className="text-xs text-muted-foreground">{dep.namespace}</div>
              <div className={`text-[11px] mt-1 font-medium ${statusColor}`}>
                {dep.readyReplicas}/{dep.replicas} ready • {dep.availableReplicas}/{dep.replicas} available
              </div>
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
          Selecione um cluster para visualizar Deployments
        </div>
      );
    }

    if (!selectedDeployment) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um Deployment para visualizar o manifesto
        </div>
      );
    }

    const updatedAt = selectedDeployment.updatedAt
      ? new Date(selectedDeployment.updatedAt).toLocaleString()
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

    // Extrair versão dos labels (app.kubernetes.io/version ou version)
    const appVersion = selectedDeployment.labels?.["app.kubernetes.io/version"] ||
                       selectedDeployment.labels?.["version"] ||
                       selectedDeployment.labels?.["app.version"];

    return (
      <div className="space-y-3" onKeyDown={handleEditorKeyDown} tabIndex={-1}>
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedDeployment.cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
            <span className="font-medium">{selectedDeployment.namespace}</span>
          </div>
          {appVersion && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Versão</span>
              <span className="font-mono text-primary">{appVersion}</span>
            </div>
          )}
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Replicas</span>
            <span className="font-mono">
              {selectedDeployment.readyReplicas} / {selectedDeployment.replicas}
            </span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Atualizado</span>
            <span className="font-medium">{updatedAt}</span>
          </div>
          {selectedDeployment.labels && Object.keys(selectedDeployment.labels).length > 0 && (
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
                  {Object.entries(selectedDeployment.labels).map(([key, value]) => (
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
                  disabled={!selectedDeployment}
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
              onClick={handleViewDescribe}
              disabled={!selectedDeployment}
            >
              <FileText className="w-4 h-4 mr-1" />
              Describe
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal()}
              disabled={!selectedDeployment || !hasChanges || isDiffLoading}
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
              onClick={() => handleShowDiffModal({ fullscreen: true })}
              disabled={!selectedDeployment || !hasChanges || isDiffLoading}
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
                disabled={!selectedDeployment || isValidating}
              >
                <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
              </Button>
            </ProtectedAction>
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={!selectedDeployment || !hasChanges}
            >
              <X className="w-4 h-4 mr-2" /> Cancelar
            </Button>
            <ProtectedAction>
              <Button
                variant="default"
                size="sm"
                onClick={openApplyConfirm}
                disabled={!selectedDeployment || isApplying || !hasChanges}
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
          placeholder="Buscar por nome ou label..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderDeploymentList()}
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
                  {selectedDeployment && ` • ${selectedDeployment.namespace}/${selectedDeployment.name}`}
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
    if (!selectedDeployment) return null;

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
                    {selectedDeployment.namespace}/{selectedDeployment.name}
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
                      disabled={!selectedDeployment || isValidating}
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
                      disabled={!selectedDeployment || isApplying || !hasChanges}
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
    if (!selectedDeployment) return null;

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
              Essa ação vai aplicar o Deployment diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {selectedDeployment.cluster}</p>
              <p><span className="text-muted-foreground">Namespace:</span> {selectedDeployment.namespace}</p>
              <p><span className="text-muted-foreground">Deployment:</span> {selectedDeployment.name}</p>
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

        {/* Modal Describe */}
        <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
          <DialogContent className="max-w-6xl max-h-[90vh]">
            <DialogHeader>
              <DialogTitle>Kubectl Describe - {selectedDeployment?.name}</DialogTitle>
              <DialogDescription className="text-sm text-muted-foreground">
                {selectedDeployment?.namespace}/{selectedDeployment?.name}
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
      </>
    );
  }

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Deployments",
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

      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {selectedDeployment?.name}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {selectedDeployment?.namespace}/{selectedDeployment?.name}
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

      {/* Modal de Análise Preditiva */}
      <Dialog open={predictionModalOpen} onOpenChange={handlePredictionModalChange}>
        <DialogContent
          className="max-w-6xl h-[90vh] flex flex-col p-0"
          onInteractOutside={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => {
            if (predictionLoading) {
              e.preventDefault();
              console.log("[PredictiveAnalysis] Prevented ESC while loading");
            }
          }}
        >
          <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b">
            <div className="flex items-center justify-between">
              <div>
                <DialogTitle className="flex items-center gap-2">
                  <TrendingUp className="w-5 h-5 text-blue-500" />
                  Análise Preditiva - {selectedDeployment?.name}
                </DialogTitle>
                <DialogDescription>
                  Análise baseada em métricas históricas, tendências e IA
                </DialogDescription>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setHistoryModalOpen(true);
                  }}
                >
                  <History className="w-4 h-4 mr-2" />
                  Ver Histórico
                </Button>
                {predictionResult && !predictionResult.error && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setExportModalOpen(true)}
                  >
                    <Download className="w-4 h-4 mr-2" />
                    Exportar Relatório
                  </Button>
                )}
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="flex-1 px-6" style={{ height: 'calc(90vh - 140px)' }}>
            <div className="py-4">
              {predictionLoading ? (
                <div className="flex items-center justify-center py-20">
                  <Loader2 className="w-8 h-8 animate-spin text-primary" />
                  <span className="ml-3 text-muted-foreground">Analisando deployment...</span>
                </div>
              ) : predictionResult?.error ? (
                <div className="bg-destructive/10 border border-destructive/50 rounded-lg p-4">
                  <p className="text-destructive font-semibold">Erro na análise:</p>
                  <p className="text-sm text-muted-foreground mt-2">{predictionResult.error}</p>
                </div>
              ) : predictionResult ? (
                <div className="space-y-6">
                  {/* Health Score */}
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold text-lg mb-3 flex items-center gap-2">
                      <CheckCircle2 className="w-5 h-5 text-primary" />
                      Health Score
                    </h3>
                    <div className="flex items-center gap-4">
                      <div className={`text-5xl font-bold ${
                        predictionResult.health_score.overall >= 75 ? 'text-green-500' :
                        predictionResult.health_score.overall >= 50 ? 'text-yellow-500' :
                        'text-red-500'
                      }`}>
                        {predictionResult.health_score.overall}
                        <span className="text-2xl text-muted-foreground">/100</span>
                      </div>
                      <div className="flex-1">
                        <div className="text-sm text-muted-foreground mb-2">Categoria:
                          <span className="ml-2 font-semibold capitalize">{predictionResult.health_score.category}</span>
                        </div>
                        <div className="grid grid-cols-2 gap-2 text-xs">
                          <div>Availability: {predictionResult.health_score.breakdown.availability}/100</div>
                          <div>Performance: {predictionResult.health_score.breakdown.performance}/100</div>
                          <div>Stability: {predictionResult.health_score.breakdown.stability}/100</div>
                          <div>Efficiency: {predictionResult.health_score.breakdown.efficiency}/100</div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Executive Summary */}
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold text-lg mb-3">Resumo Executivo</h3>
                    <p className="text-sm mb-3">{predictionResult.executive_summary.current_state}</p>
                    <div className="mb-3">
                      <span className="text-xs font-semibold text-muted-foreground">Nível de Risco:</span>
                      <span className={`ml-2 px-2 py-1 rounded text-xs font-semibold ${
                        predictionResult.executive_summary.risk_level === 'critical' ? 'bg-red-500/20 text-red-400' :
                        predictionResult.executive_summary.risk_level === 'high' ? 'bg-orange-500/20 text-orange-400' :
                        predictionResult.executive_summary.risk_level === 'medium' ? 'bg-yellow-500/20 text-yellow-400' :
                        'bg-green-500/20 text-green-400'
                      }`}>
                        {predictionResult.executive_summary.risk_level}
                      </span>
                    </div>
                    <div>
                      <span className="text-xs font-semibold text-muted-foreground">Principais Descobertas:</span>
                      <ul className="list-disc list-inside text-sm mt-2 space-y-1">
                        {predictionResult.executive_summary?.key_findings?.map((finding: string, idx: number) => (
                          <li key={idx}>{finding}</li>
                        )) || <li className="text-muted-foreground">Nenhuma descoberta disponível</li>}
                      </ul>
                    </div>
                    {predictionResult.executive_summary?.business_impact && (
                      <div className="mt-3 pt-3 border-t border-border/50">
                        <span className="text-xs font-semibold text-muted-foreground">Impacto no Negócio:</span>
                        <p className="text-sm mt-1">{predictionResult.executive_summary.business_impact}</p>
                      </div>
                    )}
                  </div>

                  {/* DADOS ANALISADOS */}
                  {predictionResult.raw_metrics && (
                    <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                      <h3 className="font-semibold text-lg mb-3">Dados Analisados</h3>
                      <p className="text-xs text-muted-foreground mb-3">
                        Esta análise foi baseada nas seguintes métricas e observações do deployment:
                      </p>

                      {/* Métricas de Réplicas */}
                      <div className="mb-4">
                        <h4 className="font-semibold text-sm mb-2">Métricas de Réplicas</h4>
                        <div className="grid grid-cols-2 gap-2 text-xs">
                          <div className="bg-secondary/50 rounded p-2">
                            <div className="text-muted-foreground">Réplicas Desejadas</div>
                            <div className="text-lg font-semibold">{predictionResult.raw_metrics.desired_replicas}</div>
                          </div>
                          <div className="bg-secondary/50 rounded p-2">
                            <div className="text-muted-foreground">Réplicas Disponíveis</div>
                            <div className="text-lg font-semibold">{predictionResult.raw_metrics.available_replicas}</div>
                          </div>
                          <div className="bg-secondary/50 rounded p-2">
                            <div className="text-muted-foreground">Réplicas Prontas</div>
                            <div className="text-lg font-semibold">{predictionResult.raw_metrics.ready_replicas}</div>
                          </div>
                          <div className="bg-secondary/50 rounded p-2">
                            <div className="text-muted-foreground">Taxa de Disponibilidade</div>
                            <div className="text-lg font-semibold">
                              {((predictionResult.raw_metrics.available_replicas / predictionResult.raw_metrics.desired_replicas) * 100).toFixed(1)}%
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Consumo de Recursos */}
                      <div className="mb-4">
                        <h4 className="font-semibold text-sm mb-2">Consumo de Recursos</h4>
                        <div className="bg-secondary/50 rounded p-3 space-y-2 text-xs">
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">CPU Média:</span>
                            <span className="font-mono font-semibold">
                              {predictionResult.raw_metrics.current.cpu_usage_avg.toFixed(3)} cores
                              <span className="text-muted-foreground ml-2">(P95: {predictionResult.raw_metrics.current.cpu_usage_p95.toFixed(3)})</span>
                            </span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">Memória Média:</span>
                            <span className="font-mono font-semibold">
                              {(predictionResult.raw_metrics.current.memory_usage_avg / (1024*1024*1024)).toFixed(2)} GB
                              <span className="text-muted-foreground ml-2">(P95: {(predictionResult.raw_metrics.current.memory_usage_p95 / (1024*1024*1024)).toFixed(2)} GB)</span>
                            </span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">Tendência CPU (7 dias):</span>
                            <span className={`font-semibold ${
                              predictionResult.raw_metrics.trends.cpu_change_7d_percent > 10 ? 'text-orange-400' :
                              predictionResult.raw_metrics.trends.cpu_change_7d_percent < -10 ? 'text-green-400' :
                              'text-blue-400'
                            }`}>
                              {predictionResult.raw_metrics.trends.cpu_change_7d_percent.toFixed(1)}%
                            </span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">Tendência Memória (7 dias):</span>
                            <span className={`font-semibold ${
                              predictionResult.raw_metrics.trends.memory_change_7d_percent > 10 ? 'text-orange-400' :
                              predictionResult.raw_metrics.trends.memory_change_7d_percent < -10 ? 'text-green-400' :
                              'text-blue-400'
                            }`}>
                              {predictionResult.raw_metrics.trends.memory_change_7d_percent.toFixed(1)}%
                            </span>
                          </div>
                        </div>
                      </div>

                      {/* Capacidade do Cluster */}
                      <div>
                        <h4 className="font-semibold text-sm mb-2">Capacidade do Cluster</h4>
                        <div className="bg-secondary/50 rounded p-3 space-y-2 text-xs">
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">CPU Total Disponível:</span>
                            <span className="font-mono font-semibold">
                              {predictionResult.raw_metrics.node_metrics.total_capacity.cpu_total_cores.toFixed(2)} cores
                              <span className="text-muted-foreground ml-2">
                                (Utilização: {predictionResult.raw_metrics.node_metrics.total_capacity.cpu_utilization_percent.toFixed(1)}%)
                              </span>
                            </span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">Memória Total:</span>
                            <span className="font-mono font-semibold">
                              {predictionResult.raw_metrics.node_metrics.total_capacity.mem_total_gb.toFixed(2)} GB
                              <span className="text-muted-foreground ml-2">
                                (Utilização: {predictionResult.raw_metrics.node_metrics.total_capacity.mem_utilization_percent.toFixed(1)}%)
                              </span>
                            </span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-muted-foreground">Nodes em uso:</span>
                            <span className="font-mono font-semibold">
                              {predictionResult.raw_metrics.node_metrics.nodes_used}/{predictionResult.raw_metrics.node_metrics.total_nodes_in_cluster}
                            </span>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* VM Sizing e Aplicações Concorrentes */}
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold text-lg mb-3 flex items-center gap-2">
                      <Server className="w-5 h-5 text-primary" />
                      Contexto de Infraestrutura
                    </h3>
                    
                    {/* VM Sizing */}
                    {predictionResult.raw_metrics?.node_metrics?.vm_sizing?.predominant_instance_type && (
                      <div className="mb-4">
                        <h4 className="font-semibold text-sm mb-2">Tipo de VM/Instance</h4>
                        <div className="bg-secondary/50 rounded p-3 text-xs space-y-1">
                          <div><span className="text-muted-foreground">Tipo:</span> <span className="font-mono font-semibold">{predictionResult.raw_metrics.node_metrics.vm_sizing.predominant_instance_type}</span></div>
                          {predictionResult.raw_metrics.node_metrics.vm_sizing.cpu_per_vm_cores > 0 && (
                            <div><span className="text-muted-foreground">CPU por VM:</span> {predictionResult.raw_metrics.node_metrics.vm_sizing.cpu_per_vm_cores} cores</div>
                          )}
                          {predictionResult.raw_metrics.node_metrics.vm_sizing.memory_per_vm_gb > 0 && (
                            <div><span className="text-muted-foreground">Memória por VM:</span> {predictionResult.raw_metrics.node_metrics.vm_sizing.memory_per_vm_gb} GB</div>
                          )}
                          {predictionResult.raw_metrics.node_metrics.vm_sizing.max_pods_per_node > 0 && (
                            <div><span className="text-muted-foreground">Máx Pods/Node:</span> {predictionResult.raw_metrics.node_metrics.vm_sizing.max_pods_per_node}</div>
                          )}
                          {predictionResult.raw_metrics.node_metrics.vm_sizing.recommended_instance_type && (
                            <div className="mt-2 pt-2 border-t border-border/50">
                              <div className="text-yellow-500"><span className="text-muted-foreground">Recomendado:</span> {predictionResult.raw_metrics.node_metrics.vm_sizing.recommended_instance_type}</div>
                              {predictionResult.raw_metrics.node_metrics.vm_sizing.recommendation_reason && (
                                <div className="text-muted-foreground mt-1">{predictionResult.raw_metrics.node_metrics.vm_sizing.recommendation_reason}</div>
                              )}
                            </div>
                          )}
                        </div>
                      </div>
                    )}

                    {/* Aplicações Concorrentes */}
                    {predictionResult.raw_metrics?.competing_apps?.length > 0 && (
                      <div>
                        <h4 className="font-semibold text-sm mb-2">Aplicações Concorrentes nas Mesmas VMs</h4>
                        <div className="bg-yellow-500/10 border border-yellow-500/30 rounded p-3 mb-3">
                          <p className="text-xs text-yellow-600 dark:text-yellow-400 font-semibold">
                            IMPORTANTE: As VMs/Nodes não dispõem de recursos totais para este deployment.
                          </p>
                          <p className="text-xs text-muted-foreground mt-1">
                            Os recursos são compartilhados e há concorrência com {predictionResult.raw_metrics.competing_apps.length} aplicação(s).
                          </p>
                        </div>
                        <div className="space-y-3 max-h-60 overflow-y-auto">
                          {/* Impacto Alto */}
                          {predictionResult.raw_metrics.competing_apps.filter((app: any) => app.impact_level === 'high').length > 0 && (
                            <div>
                              <div className="text-xs font-semibold text-red-500 mb-1">Impacto Alto:</div>
                              {predictionResult.raw_metrics.competing_apps
                                .filter((app: any) => app.impact_level === 'high')
                                .map((app: any, idx: number) => (
                                  <div key={idx} className="bg-red-500/10 border border-red-500/30 rounded p-2 mb-1">
                                    <div className="text-xs font-semibold">{app.name}</div>
                                    <div className="text-xs text-muted-foreground">Namespace: {app.namespace}</div>
                                    <div className="text-xs mt-1">
                                      <span className="text-muted-foreground">CPU:</span> {app.cpu_usage_cores?.toFixed(2) || 0} cores | 
                                      <span className="text-muted-foreground ml-2">Mem:</span> {app.memory_usage_gb?.toFixed(2) || 0} GB
                                    </div>
                                  </div>
                              ))}
                            </div>
                          )}
                          {/* Impacto Médio */}
                          {predictionResult.raw_metrics.competing_apps.filter((app: any) => app.impact_level === 'medium').length > 0 && (
                            <div>
                              <div className="text-xs font-semibold text-yellow-500 mb-1">Impacto Médio:</div>
                              {predictionResult.raw_metrics.competing_apps
                                .filter((app: any) => app.impact_level === 'medium')
                                .map((app: any, idx: number) => (
                                  <div key={idx} className="bg-yellow-500/10 border border-yellow-500/30 rounded p-2 mb-1 text-xs">
                                    <span className="font-semibold">{app.name}</span> ({app.namespace}) - 
                                    CPU: {app.cpu_usage_cores?.toFixed(2) || 0} | Mem: {app.memory_usage_gb?.toFixed(2) || 0} GB
                                  </div>
                              ))}
                            </div>
                          )}
                          {/* Impacto Baixo (lista completa) */}
                          {predictionResult.raw_metrics.competing_apps.filter((app: any) => app.impact_level === 'low' || !app.impact_level).length > 0 && (
                            <div>
                              <div className="text-xs font-semibold text-green-500 mb-1">Impacto Baixo:</div>
                              <div className="space-y-1">
                                {predictionResult.raw_metrics.competing_apps
                                  .filter((app: any) => app.impact_level === 'low' || !app.impact_level)
                                  .map((app: any, idx: number) => (
                                    <div key={idx} className="bg-green-500/10 border border-green-500/30 rounded p-2 text-xs">
                                      <span className="font-semibold">{app.name}</span> ({app.namespace}) - 
                                      CPU: {app.cpu_usage_cores?.toFixed(2) || 0} | Mem: {app.memory_usage_gb?.toFixed(2) || 0} GB
                                    </div>
                                  ))}
                              </div>
                            </div>
                          )}
                          {/* Totais */}
                          <div className="bg-primary/10 border border-primary/30 rounded p-2">
                            <div className="text-xs font-semibold">Total Consumido por Concorrentes:</div>
                            <div className="text-xs mt-1">
                              CPU: {predictionResult.raw_metrics.competing_apps.reduce((sum: number, app: any) => sum + (app.cpu_usage_cores || 0), 0).toFixed(2)} cores | 
                              Memória: {predictionResult.raw_metrics.competing_apps.reduce((sum: number, app: any) => sum + (app.memory_usage_gb || 0), 0).toFixed(2)} GB
                            </div>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* ANÁLISE DE CAPACIDADE PARA CRESCIMENTO HORIZONTAL */}
                  {predictionResult.raw_metrics?.capacity_forecast?.growth_analysis && (
                    <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                      <h3 className="font-semibold text-lg mb-3 flex items-center gap-2">
                        <TrendingUp className="w-5 h-5 text-primary" />
                        Análise de Capacidade para Crescimento Horizontal
                      </h3>

                      {(() => {
                        const growth = predictionResult.raw_metrics.capacity_forecast.growth_analysis;
                        
                        return (
                          <div className="space-y-4">
                            {/* Node Pool Info */}
                            <div>
                              <h4 className="font-semibold text-sm mb-2">Configuração do Node Pool</h4>
                              <div className="grid grid-cols-3 gap-2 text-xs">
                                <div className="bg-secondary/50 rounded p-2">
                                  <div className="text-muted-foreground">Nodes Mínimos</div>
                                  <div className="text-lg font-semibold">{predictionResult.raw_metrics.node_metrics.vm_sizing.min_nodes}</div>
                                </div>
                                <div className="bg-secondary/50 rounded p-2">
                                  <div className="text-muted-foreground">Nodes Máximos</div>
                                  <div className="text-lg font-semibold">{predictionResult.raw_metrics.node_metrics.vm_sizing.max_nodes}</div>
                                </div>
                                <div className="bg-primary/20 rounded p-2">
                                  <div className="text-muted-foreground">Nodes Atuais</div>
                                  <div className="text-lg font-semibold text-primary">{predictionResult.raw_metrics.node_metrics.vm_sizing.current_nodes}</div>
                                </div>
                              </div>
                            </div>

                            {/* Aplicação em Análise */}
                            <div>
                              <h4 className="font-semibold text-sm mb-2">Aplicação em Análise</h4>
                              <div className="bg-blue-500/10 border border-blue-500/30 rounded p-3 text-xs space-y-1">
                                <div className="flex justify-between">
                                  <span className="text-muted-foreground">Réplicas Atuais:</span>
                                  <span className="font-semibold">{growth.target_app.replicas}</span>
                                </div>
                                <div className="flex justify-between">
                                  <span className="text-muted-foreground">CPU Total:</span>
                                  <span className="font-mono">
                                    {growth.target_app.usage.cpu_cores.toFixed(2)} cores
                                    <span className="text-muted-foreground ml-2">
                                      ({(growth.target_app.usage.cpu_cores / growth.target_app.replicas).toFixed(3)} cores/réplica)
                                    </span>
                                  </span>
                                </div>
                                <div className="flex justify-between">
                                  <span className="text-muted-foreground">Memória Total:</span>
                                  <span className="font-mono">
                                    {growth.target_app.usage.memory_gb.toFixed(2)} GB
                                    <span className="text-muted-foreground ml-2">
                                      ({(growth.target_app.usage.memory_gb / growth.target_app.replicas).toFixed(2)} GB/réplica)
                                    </span>
                                  </span>
                                </div>
                              </div>
                            </div>

                            {/* Aplicações Concorrentes - Tabela Resumida com Réplicas */}
                            {growth.competing_apps?.length > 0 && (
                              <div>
                                <h4 className="font-semibold text-sm mb-2">Resumo de Aplicações Concorrentes (com Réplicas)</h4>
                                <div className="bg-secondary/30 rounded overflow-hidden">
                                  <div className="overflow-x-auto max-h-48 overflow-y-auto">
                                    <table className="w-full text-xs">
                                      <thead className="bg-secondary/70 sticky top-0">
                                        <tr>
                                          <th className="text-left p-2">Aplicação</th>
                                          <th className="text-left p-2">Namespace</th>
                                          <th className="text-center p-2">Réplicas</th>
                                          <th className="text-right p-2">CPU Total</th>
                                          <th className="text-right p-2">Mem Total</th>
                                          <th className="text-right p-2">CPU/Répl</th>
                                          <th className="text-right p-2">Mem/Répl</th>
                                        </tr>
                                      </thead>
                                      <tbody>
                                        {growth.competing_apps.map((app: any, idx: number) => {
                                          const cpuPerReplica = app.replicas > 0 ? app.usage.cpu_cores / app.replicas : app.usage.cpu_cores;
                                          const memPerReplica = app.replicas > 0 ? app.usage.memory_gb / app.replicas : app.usage.memory_gb;
                                          return (
                                            <tr key={idx} className="border-t border-border/30">
                                              <td className="p-2 font-mono">{app.name}</td>
                                              <td className="p-2">{app.namespace}</td>
                                              <td className="p-2 text-center font-semibold">{app.replicas}</td>
                                              <td className="p-2 text-right font-mono">{app.usage.cpu_cores.toFixed(2)}</td>
                                              <td className="p-2 text-right font-mono">{app.usage.memory_gb.toFixed(2)}</td>
                                              <td className="p-2 text-right font-mono text-muted-foreground">{cpuPerReplica.toFixed(3)}</td>
                                              <td className="p-2 text-right font-mono text-muted-foreground">{memPerReplica.toFixed(2)}</td>
                                            </tr>
                                          );
                                        })}
                                      </tbody>
                                      <tfoot className="bg-primary/20 font-semibold border-t-2 border-primary">
                                        <tr>
                                          <td colSpan={3} className="p-2">Total Concorrentes</td>
                                          <td className="p-2 text-right font-mono">{growth.total_competing_usage.cpu_cores.toFixed(2)}</td>
                                          <td className="p-2 text-right font-mono">{growth.total_competing_usage.memory_gb.toFixed(2)}</td>
                                          <td colSpan={2}></td>
                                        </tr>
                                      </tfoot>
                                    </table>
                                  </div>
                                </div>
                              </div>
                            )}

                            {/* Capacidade do Cluster */}
                            <div>
                              <h4 className="font-semibold text-sm mb-2">Capacidade do Cluster</h4>
                              <div className="bg-secondary/30 rounded overflow-hidden">
                                <table className="w-full text-xs">
                                  <thead className="bg-secondary/70">
                                    <tr>
                                      <th className="text-left p-2">Cenário</th>
                                      <th className="text-center p-2">Nodes</th>
                                      <th className="text-right p-2">CPU Total</th>
                                      <th className="text-right p-2">Memória Total</th>
                                    </tr>
                                  </thead>
                                  <tbody>
                                    <tr className="border-t border-border/30">
                                      <td className="p-2">Atual</td>
                                      <td className="p-2 text-center font-semibold">{growth.current_capacity.nodes}</td>
                                      <td className="p-2 text-right font-mono">{growth.current_capacity.resources.cpu_cores.toFixed(2)} cores</td>
                                      <td className="p-2 text-right font-mono">{growth.current_capacity.resources.memory_gb.toFixed(2)} GB</td>
                                    </tr>
                                    <tr className="border-t border-border/30 bg-green-500/10">
                                      <td className="p-2">Máximo (se escalar)</td>
                                      <td className="p-2 text-center font-semibold text-green-400">{growth.max_capacity.nodes}</td>
                                      <td className="p-2 text-right font-mono text-green-400">{growth.max_capacity.resources.cpu_cores.toFixed(2)} cores</td>
                                      <td className="p-2 text-right font-mono text-green-400">{growth.max_capacity.resources.memory_gb.toFixed(2)} GB</td>
                                    </tr>
                                  </tbody>
                                </table>
                              </div>
                            </div>

                            {/* Disponível para Crescimento */}
                            <div>
                              <h4 className="font-semibold text-sm mb-2">Capacidade Disponível para Crescimento</h4>
                              <div className="bg-secondary/50 rounded p-3 space-y-2 text-xs">
                                <div className="flex justify-between">
                                  <span className="text-muted-foreground">CPU Disponível:</span>
                                  <span className="font-mono font-semibold">{growth.available_for_growth.cpu_cores.toFixed(2)} cores</span>
                                </div>
                                <div className="flex justify-between">
                                  <span className="text-muted-foreground">Memória Disponível:</span>
                                  <span className="font-mono font-semibold">{growth.available_for_growth.memory_gb.toFixed(2)} GB</span>
                                </div>
                                <div className="flex justify-between pt-2 border-t border-border/50">
                                  <span className="text-muted-foreground">Recurso Gargalo:</span>
                                  <span className={`font-semibold uppercase ${
                                    growth.bottleneck_resource === 'memory' ? 'text-orange-400' : 'text-yellow-400'
                                  }`}>{growth.bottleneck_resource}</span>
                                </div>
                              </div>
                            </div>

                            {/* Cenários de Escalabilidade */}
                            <div>
                              <h4 className="font-semibold text-sm mb-2">Cenários de Escalabilidade</h4>
                              <div className="bg-secondary/30 rounded overflow-hidden">
                                <table className="w-full text-xs">
                                  <thead className="bg-secondary/70">
                                    <tr>
                                      <th className="text-left p-2">Cenário</th>
                                      <th className="text-right p-2">Máximo de Réplicas</th>
                                    </tr>
                                  </thead>
                                  <tbody>
                                    <tr className="border-t border-border/30">
                                      <td className="p-2">Nodes Atuais ({growth.current_capacity.nodes})</td>
                                      <td className="p-2 text-right">
                                        <span className="font-bold text-blue-400 text-base">{growth.max_replicas_current_nodes}</span>
                                        <span className="text-muted-foreground ml-1">réplicas</span>
                                      </td>
                                    </tr>
                                    <tr className="border-t border-border/30 bg-green-500/10">
                                      <td className="p-2">Escalando para Max Nodes ({growth.max_capacity.nodes})</td>
                                      <td className="p-2 text-right">
                                        <span className="font-bold text-green-400 text-base">{growth.max_replicas_with_max_nodes}</span>
                                        <span className="text-muted-foreground ml-1">réplicas</span>
                                      </td>
                                    </tr>
                                    {growth.replicas_if_remove_competing > growth.max_replicas_current_nodes && (
                                      <tr className="border-t border-border/30 bg-purple-500/10">
                                        <td className="p-2">Se remover aplicações concorrentes</td>
                                        <td className="p-2 text-right">
                                          <span className="font-bold text-purple-400 text-base">{growth.replicas_if_remove_competing}</span>
                                          <span className="text-muted-foreground ml-1">réplicas</span>
                                        </td>
                                      </tr>
                                    )}
                                  </tbody>
                                </table>
                              </div>
                            </div>

                            {/* Recomendação Final */}
                            <div className="bg-primary/10 border border-primary/30 rounded-lg p-4">
                              <h4 className="font-semibold text-sm mb-2 flex items-center gap-2">
                                <CheckCircle2 className="w-4 h-4 text-primary" />
                                RECOMENDAÇÃO
                              </h4>
                              <p className="text-sm mb-2">{growth.growth_recommendation}</p>
                              <div className="flex items-center gap-2 mt-3 pt-3 border-t border-border/50">
                                <span className="text-xs text-muted-foreground">Máximo Recomendado:</span>
                                <span className="font-bold text-primary text-lg">{growth.recommended_max_replicas}</span>
                                <span className="text-muted-foreground">réplicas</span>
                              </div>
                            </div>
                          </div>
                        );
                      })()}
                    </div>
                  )}

                  {/* Gráficos de Tendências Temporais */}
                  {predictionResult.raw_metrics && (
                    <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                      <h3 className="font-semibold text-lg mb-3 flex items-center gap-2">
                        <BarChart3 className="w-5 h-5 text-primary" />
                        Tendências Temporais
                      </h3>
                      
                      {/* Gráfico de CPU */}
                      <div className="mb-6">
                        <h4 className="text-sm font-semibold mb-2">Uso de CPU (cores)</h4>
                        <ResponsiveContainer width="100%" height={200}>
                          <LineChart data={[
                            { periodo: '14d atrás', avg: predictionResult.raw_metrics.day_14_ago.cpu_usage_avg, p95: predictionResult.raw_metrics.day_14_ago.cpu_usage_p95 },
                            { periodo: '10d atrás', avg: predictionResult.raw_metrics.day_10_ago.cpu_usage_avg, p95: predictionResult.raw_metrics.day_10_ago.cpu_usage_p95 },
                            { periodo: '7d atrás', avg: predictionResult.raw_metrics.day_7_ago.cpu_usage_avg, p95: predictionResult.raw_metrics.day_7_ago.cpu_usage_p95 },
                            { periodo: '3d atrás', avg: predictionResult.raw_metrics.day_3_ago.cpu_usage_avg, p95: predictionResult.raw_metrics.day_3_ago.cpu_usage_p95 },
                            { periodo: 'Atual', avg: predictionResult.raw_metrics.current.cpu_usage_avg, p95: predictionResult.raw_metrics.current.cpu_usage_p95 }
                          ]}>
                            <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                            <XAxis dataKey="periodo" stroke="#888" style={{ fontSize: '12px' }} />
                            <YAxis stroke="#888" style={{ fontSize: '12px' }} tickFormatter={(value) => value >= 1 ? value.toFixed(2) : (value * 1000).toFixed(0) + 'm'} />
                            <Tooltip 
                              contentStyle={{ backgroundColor: '#1a1a1a', border: '1px solid #333' }} 
                              formatter={(value: any) => [value >= 1 ? `${value.toFixed(3)} cores` : `${(value * 1000).toFixed(0)} milicores`, '']}
                            />
                            <Legend />
                            <Line type="monotone" dataKey="avg" stroke="#8884d8" name="Média" strokeWidth={2} />
                            <Line type="monotone" dataKey="p95" stroke="#82ca9d" name="P95" strokeWidth={2} />
                          </LineChart>
                        </ResponsiveContainer>
                        <div className="text-xs text-muted-foreground mt-2">
                          Tendência: <span className={`font-semibold ${
                            predictionResult.raw_metrics.trends.cpu_trend === 'increasing' ? 'text-orange-400' :
                            predictionResult.raw_metrics.trends.cpu_trend === 'decreasing' ? 'text-green-400' :
                            'text-blue-400'
                          }`}>{predictionResult.raw_metrics.trends.cpu_trend}</span>
                          {' | '}
                          Mudança 7d: <span className={`font-semibold ${
                            predictionResult.raw_metrics.trends.cpu_change_7d_percent > 10 ? 'text-orange-400' :
                            predictionResult.raw_metrics.trends.cpu_change_7d_percent < -10 ? 'text-green-400' :
                            'text-blue-400'
                          }`}>{predictionResult.raw_metrics.trends.cpu_change_7d_percent.toFixed(1)}%</span>
                        </div>
                      </div>

                      {/* Gráfico de Memória */}
                      <div className="mb-6">
                        <h4 className="text-sm font-semibold mb-2">Uso de Memória (GB)</h4>
                        <ResponsiveContainer width="100%" height={200}>
                          <LineChart data={[
                            { periodo: '14d atrás', avg: (predictionResult.raw_metrics.day_14_ago.memory_usage_avg / (1024*1024*1024)).toFixed(2), p95: (predictionResult.raw_metrics.day_14_ago.memory_usage_p95 / (1024*1024*1024)).toFixed(2) },
                            { periodo: '10d atrás', avg: (predictionResult.raw_metrics.day_10_ago.memory_usage_avg / (1024*1024*1024)).toFixed(2), p95: (predictionResult.raw_metrics.day_10_ago.memory_usage_p95 / (1024*1024*1024)).toFixed(2) },
                            { periodo: '7d atrás', avg: (predictionResult.raw_metrics.day_7_ago.memory_usage_avg / (1024*1024*1024)).toFixed(2), p95: (predictionResult.raw_metrics.day_7_ago.memory_usage_p95 / (1024*1024*1024)).toFixed(2) },
                            { periodo: '3d atrás', avg: (predictionResult.raw_metrics.day_3_ago.memory_usage_avg / (1024*1024*1024)).toFixed(2), p95: (predictionResult.raw_metrics.day_3_ago.memory_usage_p95 / (1024*1024*1024)).toFixed(2) },
                            { periodo: 'Atual', avg: (predictionResult.raw_metrics.current.memory_usage_avg / (1024*1024*1024)).toFixed(2), p95: (predictionResult.raw_metrics.current.memory_usage_p95 / (1024*1024*1024)).toFixed(2) }
                          ]}>
                            <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                            <XAxis dataKey="periodo" stroke="#888" style={{ fontSize: '12px' }} />
                            <YAxis stroke="#888" style={{ fontSize: '12px' }} />
                            <Tooltip contentStyle={{ backgroundColor: '#1a1a1a', border: '1px solid #333' }} />
                            <Legend />
                            <Line type="monotone" dataKey="avg" stroke="#ffc658" name="Média" strokeWidth={2} />
                            <Line type="monotone" dataKey="p95" stroke="#ff7c7c" name="P95" strokeWidth={2} />
                          </LineChart>
                        </ResponsiveContainer>
                        <div className="text-xs text-muted-foreground mt-2">
                          Tendência: <span className={`font-semibold ${
                            predictionResult.raw_metrics.trends.memory_trend === 'increasing' ? 'text-orange-400' :
                            predictionResult.raw_metrics.trends.memory_trend === 'decreasing' ? 'text-green-400' :
                            'text-blue-400'
                          }`}>{predictionResult.raw_metrics.trends.memory_trend}</span>
                          {' | '}
                          Mudança 7d: <span className={`font-semibold ${
                            predictionResult.raw_metrics.trends.memory_change_7d_percent > 10 ? 'text-orange-400' :
                            predictionResult.raw_metrics.trends.memory_change_7d_percent < -10 ? 'text-green-400' :
                            'text-blue-400'
                          }`}>{predictionResult.raw_metrics.trends.memory_change_7d_percent.toFixed(1)}%</span>
                        </div>
                      </div>

                      {/* Gráfico de Restarts */}
                      <div>
                        <h4 className="text-sm font-semibold mb-2">Contagem de Restarts</h4>
                        {(() => {
                          const maxRestarts = Math.max(
                            predictionResult.raw_metrics.day_14_ago.restart_count,
                            predictionResult.raw_metrics.day_10_ago.restart_count,
                            predictionResult.raw_metrics.day_7_ago.restart_count,
                            predictionResult.raw_metrics.day_3_ago.restart_count,
                            predictionResult.raw_metrics.current.restart_count
                          );
                          
                          return maxRestarts === 0 ? (
                            <div className="bg-green-500/10 border border-green-500/30 rounded-lg p-8 text-center">
                              <p className="text-green-400 font-semibold">Nenhum restart detectado nos últimos 14 dias</p>
                              <p className="text-sm text-muted-foreground mt-2">Deployment está estável</p>
                            </div>
                          ) : (
                            <ResponsiveContainer width="100%" height={180}>
                              <BarChart data={[
                                { periodo: '14d atrás', restarts: predictionResult.raw_metrics.day_14_ago.restart_count },
                                { periodo: '10d atrás', restarts: predictionResult.raw_metrics.day_10_ago.restart_count },
                                { periodo: '7d atrás', restarts: predictionResult.raw_metrics.day_7_ago.restart_count },
                                { periodo: '3d atrás', restarts: predictionResult.raw_metrics.day_3_ago.restart_count },
                                { periodo: 'Atual', restarts: predictionResult.raw_metrics.current.restart_count }
                              ]}>
                                <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                                <XAxis dataKey="periodo" stroke="#888" style={{ fontSize: '12px' }} />
                                <YAxis stroke="#888" style={{ fontSize: '12px' }} domain={[0, 'auto']} allowDecimals={false} />
                                <Tooltip contentStyle={{ backgroundColor: '#1a1a1a', border: '1px solid #333' }} />
                                <Bar dataKey="restarts" fill="#ff6b6b" name="Restarts" />
                              </BarChart>
                            </ResponsiveContainer>
                          );
                        })()}
                      </div>
                    </div>
                  )}

                  {/* Predictions */}
                  {((Array.isArray(predictionResult.predictions?.short_term) && predictionResult.predictions.short_term.length > 0) ||
                    (Array.isArray(predictionResult.predictions?.medium_term) && predictionResult.predictions.medium_term.length > 0) ||
                    (Array.isArray(predictionResult.predictions?.long_term) && predictionResult.predictions.long_term.length > 0)) && (
                    <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                      <h3 className="font-semibold text-lg mb-3">Previsões</h3>

                      {Array.isArray(predictionResult.predictions?.short_term) && predictionResult.predictions.short_term.length > 0 && (
                        <div className="mb-4">
                          <h4 className="font-semibold text-sm mb-2 text-orange-400">Curto Prazo (4h)</h4>
                          {predictionResult.predictions.short_term.map((pred: any, idx: number) => (
                            <div key={idx} className="bg-background/50 rounded p-3 mb-2">
                              <div className="flex items-start justify-between mb-1">
                                <span className="font-semibold text-sm">{pred.event}</span>
                                <span className={`text-xs px-2 py-1 rounded ${
                                  pred.severity === 'critical' ? 'bg-red-500/20 text-red-400' :
                                  pred.severity === 'high' ? 'bg-orange-500/20 text-orange-400' :
                                  pred.severity === 'medium' ? 'bg-yellow-500/20 text-yellow-400' :
                                  'bg-blue-500/20 text-blue-400'
                                }`}>{pred.severity}</span>
                              </div>
                              <p className="text-xs text-muted-foreground mb-2">{pred.impact}</p>
                              <div className="text-xs">Probabilidade: {Math.round(pred.probability * 100)}%</div>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Recommendations */}
                  {Array.isArray(predictionResult.recommendations) && predictionResult.recommendations.length > 0 && (
                    <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                      <h3 className="font-semibold text-lg mb-3">Recomendações</h3>
                      
                      {/* Alerta de Economia de Custos */}
                      {predictionResult.recommendations.some((rec: any) => 
                        rec.category === 'cost-optimization' || rec.category === 'downsizing'
                      ) && (
                        <div className="bg-yellow-500/10 border border-yellow-500/30 rounded-lg p-4 mb-4">
                          <div className="flex items-start gap-3">
                            <div className="text-2xl">💰</div>
                            <div className="flex-1">
                              <h4 className="font-semibold text-yellow-600 dark:text-yellow-400 mb-1">
                                Oportunidade de Economia de Custos Identificada
                              </h4>
                              <p className="text-sm text-muted-foreground">
                                Há recursos sobreprovisionados que podem ser reduzidos sem impacto negativo, 
                                resultando em economia significativa de custos de infraestrutura.
                              </p>
                            </div>
                          </div>
                        </div>
                      )}

                      {predictionResult.recommendations.map((rec: any, idx: number) => (
                        <div 
                          key={idx} 
                          className={`bg-background/50 rounded p-3 mb-3 border-l-4 ${
                            rec.category === 'cost-optimization' || rec.category === 'downsizing'
                              ? 'border-yellow-500'
                              : 'border-primary'
                          }`}
                        >
                          <div className="flex items-start justify-between mb-2">
                            <span className="font-semibold">
                              {(rec.category === 'cost-optimization' || rec.category === 'downsizing') && '💰 '}
                              #{rec.priority} - {rec.title}
                            </span>
                            <span className={`text-xs px-2 py-1 rounded ${
                              rec.category === 'cost-optimization' || rec.category === 'downsizing'
                                ? 'bg-yellow-500/20 text-yellow-600 dark:text-yellow-400'
                                : 'bg-primary/20'
                            }`}>
                              {rec.category}
                            </span>
                          </div>
                          <p className="text-sm text-muted-foreground mb-2">{rec.description}</p>
                          <div className="flex gap-4 text-xs text-muted-foreground">
                            <span>Tempo: {rec.implementation_estimate.time_required}</span>
                            <span>Complexidade: {rec.implementation_estimate.complexity}</span>
                            <span>Risco: {rec.implementation_estimate.risk_level}</span>
                            {rec.implementation_estimate.resource_efficiency_gain_percent > 0 && (
                              <span className="text-green-500 font-semibold">
                                Economia: {rec.implementation_estimate.resource_efficiency_gain_percent.toFixed(0)}%
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ) : null}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* Modal de Exportação de Relatório */}
      <Dialog open={exportModalOpen} onOpenChange={setExportModalOpen}>
        <DialogContent 
          className="max-w-md"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Download className="h-5 w-5 text-blue-600" />
              Exportar Relatório de Análise Preditiva
            </DialogTitle>
            <DialogDescription>
              Escolha o formato para exportar o relatório da análise
            </DialogDescription>
          </DialogHeader>

          {predictionResult && (
            <div className="bg-muted/50 rounded-lg p-3 space-y-1 text-sm">
              <div className="font-medium">Resumo da análise:</div>
              <div className="text-muted-foreground">
                • Deployment: {selectedDeployment?.name}
              </div>
              <div className="text-muted-foreground">
                • Namespace: {selectedDeployment?.namespace}
              </div>
              <div className="text-muted-foreground">
                • Health Score: {predictionResult.health_score?.overall}/100
              </div>
              <div className="text-muted-foreground">
                • Recomendações: {predictionResult.recommendations?.length || 0}
              </div>
            </div>
          )}

          <div className="space-y-3">
            <label className="text-sm font-medium">Formato de exportação:</label>
            <div className="space-y-2">
              <div 
                className={`flex items-start space-x-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                  exportFormat === "markdown" ? "border-primary bg-primary/5" : "border-border hover:border-primary/50"
                }`}
                onClick={() => setExportFormat("markdown")}
              >
                <input
                  type="radio"
                  checked={exportFormat === "markdown"}
                  onChange={() => setExportFormat("markdown")}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="flex items-center gap-2 font-medium">
                    <FileText className="h-4 w-4 text-blue-600" />
                    Markdown (.md)
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    Formato texto com marcação. Ideal para documentação e versionamento.
                  </p>
                </div>
              </div>

              <div 
                className={`flex items-start space-x-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                  exportFormat === "json" ? "border-primary bg-primary/5" : "border-border hover:border-primary/50"
                }`}
                onClick={() => setExportFormat("json")}
              >
                <input
                  type="radio"
                  checked={exportFormat === "json"}
                  onChange={() => setExportFormat("json")}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="flex items-center gap-2 font-medium">
                    <FileText className="h-4 w-4 text-green-600" />
                    JSON (JavaScript Object Notation)
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    Formato estruturado. Ideal para integração com outras ferramentas e scripts.
                  </p>
                </div>
              </div>

              <div 
                className={`flex items-start space-x-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                  exportFormat === "pdf" ? "border-primary bg-primary/5" : "border-border hover:border-primary/50"
                }`}
                onClick={() => setExportFormat("pdf")}
              >
                <input
                  type="radio"
                  checked={exportFormat === "pdf"}
                  onChange={() => setExportFormat("pdf")}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="flex items-center gap-2 font-medium">
                    <FileText className="h-4 w-4 text-red-600" />
                    PDF (Portable Document Format)
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    Documento formatado. Ideal para apresentações e impressão.
                  </p>
                </div>
              </div>
            </div>
          </div>

          <div className="flex justify-end gap-2 mt-4">
            <Button variant="outline" onClick={() => setExportModalOpen(false)} disabled={isExporting}>
              Cancelar
            </Button>
            <Button onClick={async () => {
              setIsExporting(true);
              try {
                if (exportFormat === "pdf") {
                  // Gerar PDF localmente com jsPDF
                  generatePredictionPDF();
                  toast.success("Relatório PDF gerado com sucesso!");
                  setExportModalOpen(false);
                } else {
                  // Markdown e JSON via backend
                  const response = await fetch(`/api/v1/predictions/export?format=${exportFormat}`, {
                    method: "POST",
                    headers: {
                      "Content-Type": "application/json",
                      Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
                    },
                    body: JSON.stringify(predictionResult),
                  });
                  
                  if (!response.ok) throw new Error("Falha ao exportar");
                  
                  const blob = await response.blob();
                  const url = window.URL.createObjectURL(blob);
                  const a = document.createElement('a');
                  a.href = url;
                  const extension = exportFormat === "json" ? "json" : "md";
                  a.download = `prediction-${selectedDeployment?.name}-${Date.now()}.${extension}`;
                  a.click();
                  window.URL.revokeObjectURL(url);
                  
                  toast.success(`Relatório ${exportFormat.toUpperCase()} exportado com sucesso!`);
                  setExportModalOpen(false);
                }
              } catch (err) {
                console.error("Export error:", err);
                toast.error("Erro ao exportar relatório");
              } finally {
                setIsExporting(false);
              }
            }} disabled={isExporting}>
              {isExporting ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Exportando...
                </>
              ) : (
                <>
                  <Download className="w-4 h-4 mr-2" />
                  Exportar
                </>
              )}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Modal de Histórico de Análises */}
      <PredictionHistoryModal
        open={historyModalOpen}
        onOpenChange={setHistoryModalOpen}
        cluster={cluster}
        namespace={selectedDeployment?.namespace}
        deployment={selectedDeployment?.name}
        onSelectRecord={(record) => {
          setPredictionResult(record);
          setPredictionModalOpen(true);
          setHistoryModalOpen(false);
        }}
      />

      {/* Modal de Confirmação - Delete Deployment */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <Trash2 className="w-5 h-5" />
              Deletar Deployment
            </DialogTitle>
            <DialogDescription>
              Você está prestes a deletar o deployment permanentemente. Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Cluster:</span>
                  <span className="text-sm text-muted-foreground">{selectedDeployment?.cluster}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Namespace:</span>
                  <span className="text-sm text-muted-foreground">{selectedDeployment?.namespace}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Deployment:</span>
                  <span className="text-sm font-semibold">{selectedDeployment?.name}</span>
                </div>
              </div>
            </div>
            <p className="text-sm text-muted-foreground">
              <strong>Atenção:</strong> Todos os pods associados serão terminados e o deployment será removido do cluster.
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeleteConfirmOpen(false)}
              disabled={isDeleting}
            >
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteDeployment}
              disabled={isDeleting}
            >
              {isDeleting ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Deletando...
                </>
              ) : (
                <>
                  <Trash2 className="w-4 h-4 mr-2" />
                  Deletar Deployment
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Confirmação - Rollout Restart */}
      <Dialog open={rolloutConfirmOpen} onOpenChange={setRolloutConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <RotateCw className="w-5 h-5" />
              Rollout Restart
            </DialogTitle>
            <DialogDescription>
              Reiniciar o deployment forçará a recriação de todos os pods.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="bg-blue-500/10 border border-blue-500/20 rounded-md p-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Cluster:</span>
                  <span className="text-sm text-muted-foreground">{selectedDeployment?.cluster}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Namespace:</span>
                  <span className="text-sm text-muted-foreground">{selectedDeployment?.namespace}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Deployment:</span>
                  <span className="text-sm font-semibold">{selectedDeployment?.name}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Réplicas:</span>
                  <span className="text-sm text-muted-foreground">{selectedDeployment?.replicas}</span>
                </div>
              </div>
            </div>
            <div className="text-sm text-muted-foreground space-y-2">
              <p><strong>O que vai acontecer:</strong></p>
              <ul className="list-disc list-inside space-y-1 ml-2">
                <li>Uma annotation de restart será adicionada ao deployment</li>
                <li>Todos os pods serão recriados com a estratégia de rolling update</li>
                <li>O downtime será mínimo (respeitando readiness probes)</li>
              </ul>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRolloutConfirmOpen(false)}
              disabled={isRestarting}
            >
              Cancelar
            </Button>
            <Button
              onClick={handleRolloutRestart}
              disabled={isRestarting}
            >
              {isRestarting ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Reiniciando...
                </>
              ) : (
                <>
                  <RotateCw className="w-4 h-4 mr-2" />
                  Reiniciar Deployment
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </>
  );
};
