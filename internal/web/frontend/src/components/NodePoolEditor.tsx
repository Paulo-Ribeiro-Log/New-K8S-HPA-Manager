import { useState, useEffect, useRef, useCallback } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { NodePool, NodeInfo } from "@/lib/api/types";
import { Save, RotateCcw, Server, Cpu, HardDrive, ArrowDownUp, Loader2, Zap, Shield, Info, Eye, Settings, Database, RefreshCcw, Tag, Tags, AlertTriangle, Copy, TrendingUp, History, Activity, Search } from "lucide-react";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import CordonDrainConfigModal, { CordonDrainConfig } from "./CordonDrainConfigModal";
import NodePoolDiskDetailsModal from "./NodePoolDiskDetailsModal";
import { formatVMSpecs, formatDiskSpecs, getVMSpecs } from "@/lib/azure-vm-specs";
import { useNodePoolDiskMetrics } from "@/hooks/useNodePoolDiskMetrics";
import { useNodes, useNodeDetails } from "@/hooks/useNodes";
import NodeDetailsModal from "./NodeDetailsModal";
import { ProtectedAction } from "@/components/rbac";
import { NodePoolPredictionModal } from "./NodePoolPredictionModal";
import { NodePoolPredictionHistoryModal } from "./NodePoolPredictionHistoryModal";
import { useAnalyzeNodePool } from "@/hooks/useNodePoolPredictions";
import { ConntrackTab } from "./ConntrackTab";

interface NodePoolEditorProps {
  nodePool: NodePool | null;
  onApply?: (nodePool: NodePool, original: NodePool) => void;
  onApplied?: () => void;
}

export const NodePoolEditor = ({ nodePool, onApply, onApplied }: NodePoolEditorProps) => {
  const staging = useStaging();

  // O backend resolve automaticamente o contexto kubeconfig correto (com ou sem -admin)
  // via resolveContext em KubeConfigManager — não forçar sufixo no frontend.
  const clusterWithAdmin = nodePool?.cluster_name ?? "";

  const { metrics: diskMetrics, loading: diskMetricsLoading, refetch: refetchDiskMetrics } = useNodePoolDiskMetrics(
    clusterWithAdmin,
    nodePool?.name
  );

  // Refs for input fields to enable select-all behavior
  const nodeCountRef = useRef<HTMLInputElement>(null);
  const minNodeCountRef = useRef<HTMLInputElement>(null);
  const maxNodeCountRef = useRef<HTMLInputElement>(null);

  // State for editable fields (usando string para permitir campo vazio)
  const [nodeCount, setNodeCount] = useState<string>("0");
  const [minNodeCount, setMinNodeCount] = useState<string>("0");
  const [maxNodeCount, setMaxNodeCount] = useState<string>("1");
  const [autoscalingEnabled, setAutoscalingEnabled] = useState<boolean>(false);
  const [sequenceOrder, setSequenceOrder] = useState<string>("none");

  // Track if values have changed
  const [hasChanges, setHasChanges] = useState(false);

  // Track if applying changes
  const [isApplying, setIsApplying] = useState(false);

  // Cordon/Drain configuration states
  const [cordonDrainEnabled, setCordonDrainEnabled] = useState(false);
  const [showCordonDrainModal, setShowCordonDrainModal] = useState(false);
  const [cordonDrainConfig, setCordonDrainConfig] = useState<CordonDrainConfig | null>(null);
  const [modalContext, setModalContext] = useState<'applyNow' | 'saveStaging'>('saveStaging');

  // Disk details modal state
  const [showDiskDetailsModal, setShowDiskDetailsModal] = useState(false);

  // Predictive Analysis
  const { analyze: analyzeNodePool, loading: predictionLoading, result: predictionResult } = useAnalyzeNodePool();
  const [predictionModalOpen, setPredictionModalOpen] = useState(false);
  const [historyModalOpen, setHistoryModalOpen] = useState(false);
  const [historyResult, setHistoryResult] = useState<any>(null);

  // Nodes states
  const [selectedNode, setSelectedNode] = useState<string | null>(null);
  const [showNodeDetailsModal, setShowNodeDetailsModal] = useState(false);
  const [modalKey, setModalKey] = useState(0); // Force modal re-creation
  const [nodeSearch, setNodeSearch] = useState("");
  const [removedNodes, setRemovedNodes] = useState<Array<{
    name: string; removed_at: string; reason: string; source: string; details: string;
  }>>([]);
  const [removedLoading, setRemovedLoading] = useState(false);
  const [removedDetail, setRemovedDetail] = useState<{ name: string; details: string; reason: string; removed_at: string } | null>(null);

  // Fetch nodes from API (sem Azure CLI — resposta rápida)
  const { nodes, loading: nodesLoading, error: nodesError, refetch: refetchNodes } = useNodes(
    clusterWithAdmin,
    nodePool?.name || ""
  );

  // Busca de tags Azure e subscription name — endpoint assíncrono separado para não bloquear nodes
  const [azureInfo, setAzureInfo] = useState<{
    cluster_tags: Record<string, string>;
    subscription_name: string;
    resource_group: string;
    subscription: string;
  } | null>(null);
  const [azureInfoLoading, setAzureInfoLoading] = useState(false);

  const fetchAzureInfo = useCallback(async () => {
    if (!clusterWithAdmin || !nodePool?.name) return;
    setAzureInfoLoading(true);
    try {
      const info = await apiClient.getNodePoolAzureInfo(clusterWithAdmin, nodePool.name);
      setAzureInfo(info);
    } catch {
      // silencioso — tags são opcionais
    } finally {
      setAzureInfoLoading(false);
    }
  }, [clusterWithAdmin, nodePool?.name]);

  useEffect(() => {
    setAzureInfo(null);
    if (clusterWithAdmin && nodePool?.name) {
      fetchAzureInfo();
    }
  }, [clusterWithAdmin, nodePool?.name, fetchAzureInfo]);

  // Fetch selected node details
  const { nodeDetails, loading: loadingNodeDetails } = useNodeDetails(
    clusterWithAdmin,
    nodePool?.name || "",
    selectedNode || ""
  );

  // Initialize form when nodePool changes or staging updates
  useEffect(() => {
    if (nodePool) {
      // Check if this pool is already in staging - use staged values if available
      const stagedPool = staging.stagedNodePools.find(
        np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
      );

      // Use staged values if available, otherwise use original nodePool values
      if (stagedPool) {
        setNodeCount(String(stagedPool.node_count));
        setMinNodeCount(String(stagedPool.min_node_count));
        setMaxNodeCount(String(stagedPool.max_node_count));
        setAutoscalingEnabled(stagedPool.autoscaling_enabled);
        setSequenceOrder(stagedPool.sequence_order && stagedPool.sequence_order > 0 ? stagedPool.sequence_order.toString() : "none");
      } else {
        setNodeCount(String(nodePool.node_count));
        setMinNodeCount(String(nodePool.min_node_count));
        setMaxNodeCount(String(nodePool.max_node_count));
        setAutoscalingEnabled(nodePool.autoscaling_enabled);
        setSequenceOrder("none");
      }

      setHasChanges(false);
    }
  }, [nodePool, staging.stagedNodePools]);

  // Check for changes whenever form values update
  useEffect(() => {
    if (!nodePool) return;

    const stagedPool = staging.stagedNodePools.find(
      np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
    );

    const currentNodeCount = nodeCount === "" ? 0 : parseInt(nodeCount);
    const currentMinNodeCount = minNodeCount === "" ? 0 : parseInt(minNodeCount);
    const currentMaxNodeCount = maxNodeCount === "" ? 0 : parseInt(maxNodeCount);

    if (stagedPool) {
      // Compare against staged values
      const changed =
        currentNodeCount !== stagedPool.node_count ||
        currentMinNodeCount !== stagedPool.min_node_count ||
        currentMaxNodeCount !== stagedPool.max_node_count ||
        autoscalingEnabled !== stagedPool.autoscaling_enabled ||
        sequenceOrder !== (stagedPool.sequence_order && stagedPool.sequence_order > 0 ? stagedPool.sequence_order.toString() : "none");

      setHasChanges(changed);
    } else {
      // Compare against original nodePool
      const changed =
        currentNodeCount !== nodePool.node_count ||
        currentMinNodeCount !== nodePool.min_node_count ||
        currentMaxNodeCount !== nodePool.max_node_count ||
        autoscalingEnabled !== nodePool.autoscaling_enabled ||
        sequenceOrder !== "none";

      setHasChanges(changed);
    }
  }, [nodeCount, minNodeCount, maxNodeCount, autoscalingEnabled, sequenceOrder, nodePool, staging.stagedNodePools]);

  const handleReset = () => {
    if (nodePool) {
      // Check if this pool is in staging - reset to staged values if available
      const stagedPool = staging.stagedNodePools.find(
        np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
      );

      if (stagedPool) {
        // Reset to staged values
        setNodeCount(String(stagedPool.node_count));
        setMinNodeCount(String(stagedPool.min_node_count));
        setMaxNodeCount(String(stagedPool.max_node_count));
        setAutoscalingEnabled(stagedPool.autoscaling_enabled);
        setSequenceOrder(stagedPool.sequence_order ? stagedPool.sequence_order.toString() : "none");
      } else {
        // Reset to original nodePool values
        setNodeCount(String(nodePool.node_count));
        setMinNodeCount(String(nodePool.min_node_count));
        setMaxNodeCount(String(nodePool.max_node_count));
        setAutoscalingEnabled(nodePool.autoscaling_enabled);
        setSequenceOrder("none");
      }

      setHasChanges(false);
    }
  };

  const handleApply = () => {
    if (!nodePool) return;

    // Se Cordon/Drain habilitado e sequenceOrder != "none", abrir modal
    if (cordonDrainEnabled && sequenceOrder !== "none") {
      setModalContext('saveStaging');
      setShowCordonDrainModal(true);
      return;
    }

    // Salvar normalmente no staging se Cordon/Drain desabilitado
    executeSaveToStaging(null);
  };

  const executeSaveToStaging = (config: CordonDrainConfig | null) => {
    if (!nodePool) return;

    // Parse string values to numbers - handle empty strings
    const nodeCountNum = nodeCount === "" ? 0 : parseInt(nodeCount);
    const minNodeCountNum = minNodeCount === "" ? 0 : parseInt(minNodeCount);
    const maxNodeCountNum = maxNodeCount === "" ? 0 : parseInt(maxNodeCount);

    // First add to staging if not already there
    staging.addNodePoolToStaging(nodePool);

    // Then update with new values
    const updates: Partial<NodePool> = {
      node_count: nodeCountNum,
      min_node_count: minNodeCountNum,
      max_node_count: maxNodeCountNum,
      autoscaling_enabled: autoscalingEnabled,
      sequence_order: sequenceOrder !== "none" ? parseInt(sequenceOrder) : 0,
    };

    // Se config foi fornecido, adicionar ao updates (será salvo no staging)
    if (config) {
      (updates as any).cordon_drain_config = {
        cordon_enabled: config.cordonEnabled,
        drain_enabled: config.drainEnabled,
        grace_period: config.gracePeriod,
        timeout: config.timeout,
        force_delete: config.forceDelete,
        ignore_daemonsets: config.ignoreDaemonSets,
        delete_emptydir: config.deleteEmptyDir,
        chunk_size: config.chunkSize,
      };
    }

    staging.updateNodePoolInStaging(nodePool.cluster_name, nodePool.name, updates);
    setHasChanges(false);

    toast.success(`Node Pool ${nodePool.name} salvo no staging${config ? ' com Cordon/Drain config' : ''}`);

    // Call optional callback
    onApplied?.();
  };

  const handleApplyNow = async () => {
    if (!nodePool) return;

    // Se Cordon/Drain habilitado, abrir modal de configuração primeiro
    if (cordonDrainEnabled) {
      setModalContext('applyNow');
      setShowCordonDrainModal(true);
      return;
    }

    // Se callback de aprovação fornecido, abrir modal de aprovação
    if (onApply) {
      const nodeCountNum = nodeCount === "" ? 0 : parseInt(nodeCount);
      const minNodeCountNum = minNodeCount === "" ? 0 : parseInt(minNodeCount);
      const maxNodeCountNum = maxNodeCount === "" ? 0 : parseInt(maxNodeCount);
      const currentNodePool: NodePool = {
        ...nodePool,
        node_count: nodeCountNum,
        min_node_count: minNodeCountNum,
        max_node_count: maxNodeCountNum,
        autoscaling_enabled: autoscalingEnabled,
      };
      onApply(currentNodePool, nodePool);
      return;
    }

    // Fallback: aplicar diretamente (sem modal de aprovação)
    await executeApplyNow(null);
  };

  const executeApplyNow = async (config: CordonDrainConfig | null) => {
    if (!nodePool) return;

    // Parse string values to numbers - handle empty strings
    const nodeCountNum = nodeCount === "" ? 0 : parseInt(nodeCount);
    const minNodeCountNum = minNodeCount === "" ? 0 : parseInt(minNodeCount);
    const maxNodeCountNum = maxNodeCount === "" ? 0 : parseInt(maxNodeCount);

    setIsApplying(true);
    window.dispatchEvent(new CustomEvent("nodePoolApplying", {
      detail: { poolName: nodePool.name, status: "start" }
    }));

    let applySuccess = false;
    try {
      // Log changes
      console.log('⚙️ Applying Node Pool changes:', {
        name: nodePool.name,
        cluster: nodePool.cluster_name,
        cordonDrainConfig: config,
        changes: {
          node_count: nodePool.node_count !== nodeCountNum ? `${nodePool.node_count} → ${nodeCountNum}` : 'unchanged',
          min_node_count: nodePool.min_node_count !== minNodeCountNum ? `${nodePool.min_node_count} → ${minNodeCountNum}` : 'unchanged',
          max_node_count: nodePool.max_node_count !== maxNodeCountNum ? `${nodePool.max_node_count} → ${maxNodeCountNum}` : 'unchanged',
          autoscaling_enabled: nodePool.autoscaling_enabled !== autoscalingEnabled ? `${nodePool.autoscaling_enabled} → ${autoscalingEnabled}` : 'unchanged',
        }
      });

      // Call API to update node pool with optional cordon/drain config
      await apiClient.updateNodePool(
        nodePool.cluster_name,
        nodePool.resource_group,
        nodePool.name,
        {
          node_count: nodeCountNum,
          min_node_count: minNodeCountNum,
          max_node_count: maxNodeCountNum,
          autoscaling_enabled: autoscalingEnabled,
        },
        config || undefined
      );

      applySuccess = true;
      toast.success(`✅ Node Pool ${nodePool.name} aplicado com sucesso`);
      setHasChanges(false);

      // Call optional callback
      onApplied?.();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
      console.error('❌ Error applying node pool:', errorMessage);
      toast.error(`❌ Erro ao aplicar ${nodePool.name}: ${errorMessage}`);
      window.dispatchEvent(new CustomEvent("nodePoolApplying", {
        detail: { poolName: nodePool.name, status: "end", result: "error", errorMessage }
      }));
    } finally {
      setIsApplying(false);
      if (applySuccess) window.dispatchEvent(new CustomEvent("nodePoolApplying", {
        detail: { poolName: nodePool.name, status: "end", result: "success" }
      }));
    }
  };

  const handleCordonDrainConfirm = (config: CordonDrainConfig) => {
    setCordonDrainConfig(config);

    // Verificar o contexto: "Aplicar Agora" ou "Salvar (Staging)"
    if (modalContext === 'applyNow') {
      executeApplyNow(config);
    } else {
      // Foi "Salvar (Staging)"
      executeSaveToStaging(config);
    }
  };

  // Keyboard shortcuts
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey) {
      if (e.key === 's' || e.key === 'S') {
        e.preventDefault();
        if (e.shiftKey) {
          // Ctrl+Shift+S: Apply directly
          if (hasChanges && !isApplying) {
            handleApply();
          }
        } else {
          // Ctrl+S: Save to staging
          if (hasChanges) {
            handleApply();
          }
        }
      } else if (e.key === 'z') {
        e.preventDefault();
        handleReset();
      }
    } else if (e.key === 'Escape') {
      handleReset();
    }
  };

  // Handler: Predictive Analysis
  const handlePredictiveAnalysis = async () => {
    if (!nodePool) return;
    setHistoryResult(null);
    setPredictionModalOpen(true);
    await analyzeNodePool(clusterWithAdmin, nodePool.name);
  };

  // Handlers for nodes table
  const handleViewNodeDetails = (nodeName: string) => {
    console.log('🔍 [NodePoolEditor] Opening modal for node:', nodeName);
    // First close any existing modal and clear state
    setShowNodeDetailsModal(false);
    setSelectedNode(null);

    // Small delay to ensure state is cleared before opening new modal
    setTimeout(() => {
      setSelectedNode(nodeName);
      setModalKey(prev => prev + 1); // Force modal re-creation
      setShowNodeDetailsModal(true);
      console.log('✅ [NodePoolEditor] Modal opened for node:', nodeName, 'with modalKey:', modalKey + 1);
    }, 100);
  };

  const handleCloseNodeModal = () => {
    console.log('🔒 [NodePoolEditor] Closing modal and clearing state');
    setShowNodeDetailsModal(false);
    setSelectedNode(null);
  };

  const StatusBadge = ({ status }: { status: string }) => {
    const getVariant = () => {
      if (status === "Ready") return "success";
      if (status === "NotReady") return "destructive";
      return "secondary";
    };

    return (
      <Badge variant={getVariant() as any}>
        {status}
      </Badge>
    );
  };

  if (!nodePool) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-muted-foreground p-8">
        <Server className="w-16 h-16 mb-4 opacity-20" />
        <p className="text-lg">Select a node pool to edit</p>
        <p className="text-sm mt-2">Choose a node pool from the list to view and modify its configuration</p>
      </div>
    );
  }

  return (
    <div className="space-y-4" onKeyDown={handleKeyDown} tabIndex={-1}>
      {/* Header */}
      <div>
        <div className="flex items-center gap-2 mb-2">
          <h2 className="text-2xl font-bold">{nodePool.name}</h2>
          <Badge variant={nodePool.is_system_pool ? "default" : "secondary"}>
            {nodePool.is_system_pool ? "System" : "User"}
          </Badge>
          <Badge variant={nodePool.status === "Succeeded" ? "outline" : "destructive"}>
            {nodePool.status}
          </Badge>
        </div>
        <div className="flex items-center gap-3 flex-wrap">
          {/* Cluster Name */}
          <div className="flex items-center gap-1">
            <span className="text-sm text-muted-foreground">{nodePool.cluster_name}</span>
            <Button
              variant="ghost"
              size="sm"
              className="h-5 w-5 p-0"
              onClick={() => {
                navigator.clipboard.writeText(nodePool.cluster_name);
                toast.success("Cluster name copiado!");
              }}
            >
              <Copy className="h-3 w-3" />
            </Button>
          </div>

          <span className="text-sm text-muted-foreground">•</span>

          {/* Resource Group */}
          <div className="flex items-center gap-1">
            <span className="text-sm text-muted-foreground">{nodePool.resource_group}</span>
            <Button
              variant="ghost"
              size="sm"
              className="h-5 w-5 p-0"
              onClick={() => {
                navigator.clipboard.writeText(nodePool.resource_group);
                toast.success("Resource Group copiado!");
              }}
            >
              <Copy className="h-3 w-3" />
            </Button>
          </div>

          {nodePool.subscription && (
            <>
              <span className="text-sm text-muted-foreground">•</span>

              {/* Subscription Name + ID */}
              <div className="flex items-center gap-1">
                {nodePool.subscription_name ? (
                  <>
                    <span className="text-sm text-muted-foreground">{nodePool.subscription_name}</span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-5 w-5 p-0"
                      onClick={() => {
                        navigator.clipboard.writeText(nodePool.subscription_name || '');
                        toast.success("Subscription name copiado!");
                      }}
                    >
                      <Copy className="h-3 w-3" />
                    </Button>
                    {nodePool.subscription !== nodePool.subscription_name && (
                      <>
                        <span className="text-xs text-muted-foreground/60 font-mono">({nodePool.subscription})</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-5 w-5 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(nodePool.subscription);
                            toast.success("Subscription ID copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </>
                    )}
                    {nodePool.subscription_uuid && (
                      <>
                        <span className="text-xs text-muted-foreground/40 font-mono">{nodePool.subscription_uuid}</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-5 w-5 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(nodePool.subscription_uuid || '');
                            toast.success("UUID copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </>
                    )}
                  </>
                ) : (
                  <>
                    <span className="text-sm text-muted-foreground font-mono">{nodePool.subscription}</span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-5 w-5 p-0"
                      onClick={() => {
                        navigator.clipboard.writeText(nodePool.subscription);
                        toast.success("Subscription ID copiado!");
                      }}
                    >
                      <Copy className="h-3 w-3" />
                    </Button>
                    {nodePool.subscription_uuid && (
                      <>
                        <span className="text-xs text-muted-foreground/40 font-mono">{nodePool.subscription_uuid}</span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-5 w-5 p-0"
                          onClick={() => {
                            navigator.clipboard.writeText(nodePool.subscription_uuid || '');
                            toast.success("UUID copiado!");
                          }}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </>
                    )}
                  </>
                )}
              </div>
            </>
          )}

          {/* Node Labels Popover */}
          {nodes && nodes.length > 0 && (() => {
            // Coleta todas as labels únicas de todos os nodes
            const allNodeLabels = new Map<string, Set<string>>();
            nodes.forEach(node => {
              if (node.labels) {
                Object.entries(node.labels).forEach(([key, value]) => {
                  if (!allNodeLabels.has(key)) {
                    allNodeLabels.set(key, new Set());
                  }
                  allNodeLabels.get(key)!.add(value);
                });
              }
            });

            return allNodeLabels.size > 0 ? (
              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="outline" size="sm">
                    <Tags className="h-4 w-4 mr-2" />
                    Node Labels ({allNodeLabels.size})
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80">
                  <div className="space-y-1 max-h-60 overflow-y-auto">
                    {Array.from(allNodeLabels.entries()).map(([key, values]) => (
                      <div key={key} className="flex items-center justify-between px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground rounded-sm cursor-pointer">
                        <div className="flex items-center gap-2 min-w-0 flex-1">
                          <span className="font-mono text-xs">{key}</span>
                          <span className="text-xs text-muted-foreground truncate">
                            {Array.from(values).join(', ')}
                          </span>
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-4 w-4 p-0 shrink-0"
                          onClick={() => {
                            navigator.clipboard.writeText(key);
                            toast.success(`Label ${key} copiada!`);
                          }}
                        >
                          <Copy className="h-2.5 w-2.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                </PopoverContent>
              </Popover>
            ) : null;
          })()}
        </div>
      </div>

      {/* Predictive Analysis buttons */}
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          onClick={handlePredictiveAnalysis}
          disabled={predictionLoading}
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
        >
          <History className="w-4 h-4 mr-2" />
          Histórico de Análises
        </Button>
      </div>

      <Separator />

      {/* Tabs para organizar visualizações */}
      <Tabs defaultValue="configuration" className="w-full" onValueChange={(tab) => {
        if (tab === "nodes" && clusterWithAdmin && nodePool?.name && removedNodes.length === 0 && !removedLoading) {
          setRemovedLoading(true);
          apiClient.getRemovedNodes(clusterWithAdmin, nodePool.name)
            .then(r => setRemovedNodes(r.removed_nodes ?? []))
            .catch(() => {})
            .finally(() => setRemovedLoading(false));
        }
      }}>
        <TabsList className="grid w-full grid-cols-4">
          <TabsTrigger value="configuration">
            <Settings className="w-4 h-4 mr-2" />
            Configuration
          </TabsTrigger>
          <TabsTrigger value="nodes">
            <Server className="w-4 h-4 mr-2" />
            Nodes ({nodes.length})
          </TabsTrigger>
          <TabsTrigger value="disk">
            <Database className="w-4 h-4 mr-2" />
            Disk
          </TabsTrigger>
          <TabsTrigger value="conntrack">
            <Activity className="w-4 h-4 mr-2" />
            Conntrack
          </TabsTrigger>
        </TabsList>

        {/* Tab Configuration */}
        <TabsContent value="configuration" className="space-y-4 mt-4">

      {/* VM Information */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Cpu className="w-4 h-4" />
            VM Configuration
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <Label className="text-muted-foreground">VM Size</Label>
              <p className="font-medium">{nodePool.vm_size}</p>
            </div>
            <div>
              <Label className="text-muted-foreground">Current Nodes</Label>
              <p className="font-medium">{nodePool.node_count}</p>
            </div>
          </div>

          {/* VM Specs */}
          {(() => {
            const vmSpecs = getVMSpecs(nodePool.vm_size);
            const specsFormatted = formatVMSpecs(nodePool.vm_size);
            const diskSpecsFormatted = formatDiskSpecs(nodePool.vm_size);

            if (vmSpecs && specsFormatted) {
              return (
                <div className="pt-2 border-t">
                  <div className="flex items-start gap-2 text-sm">
                    <Info className="w-4 h-4 text-muted-foreground mt-0.5 flex-shrink-0" />
                    <div className="w-full">
                      <p className="text-muted-foreground mb-1">Specifications</p>
                      <p className="font-medium text-primary">{specsFormatted}</p>
                      {vmSpecs.family && (
                        <p className="text-xs text-muted-foreground mt-1">{vmSpecs.family}</p>
                      )}
                      {vmSpecs.description && (
                        <p className="text-xs text-muted-foreground mt-1">{vmSpecs.description}</p>
                      )}
                      {diskSpecsFormatted && (
                        <div className="mt-2 pt-2 border-t border-border/50">
                          <p className="text-xs text-muted-foreground mb-1">Disk Performance</p>
                          <p className="text-xs font-medium">{diskSpecsFormatted}</p>
                        </div>
                      )}
                      {diskMetrics && !diskMetricsLoading && (
                        <div className="mt-2 pt-2 border-t border-border/50">
                          <div className="flex items-center justify-between mb-2">
                            <p className="text-xs text-muted-foreground">Current Disk Usage ({diskMetrics.node_count} nodes)</p>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => setShowDiskDetailsModal(true)}
                              className="h-7 text-xs gap-1"
                            >
                              <HardDrive className="w-3 h-3" />
                              Details
                            </Button>
                          </div>
                          <div className="space-y-1">
                            <div className="flex items-center justify-between text-xs">
                              <span>Used: {(diskMetrics.used_bytes / (1024**3)).toFixed(1)} GiB</span>
                              <span className={`font-medium ${
                                diskMetrics.usage_percent > 80 ? 'text-destructive' :
                                diskMetrics.usage_percent > 60 ? 'text-warning' : 'text-primary'
                              }`}>
                                {diskMetrics.usage_percent.toFixed(1)}%
                              </span>
                            </div>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              );
            }
            return null;
          })()}
        </CardContent>
      </Card>

      {/* Scaling Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="w-4 h-4" />
            Scaling Configuration
          </CardTitle>
          <CardDescription>
            Configure manual or automatic scaling for this node pool
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Autoscaling Toggle */}
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="autoscaling">Autoscaling</Label>
              <p className="text-sm text-muted-foreground">
                Enable automatic scaling based on cluster load
              </p>
            </div>
            <Switch
              id="autoscaling"
              checked={autoscalingEnabled}
              onCheckedChange={setAutoscalingEnabled}
            />
          </div>

          <Separator />

          {/* Manual Node Count (only when autoscaling disabled) */}
          {!autoscalingEnabled && (
            <div className="space-y-2">
              <Label htmlFor="nodeCount">Node Count</Label>
              <Input
                ref={nodeCountRef}
                id="nodeCount"
                type="text"
                value={nodeCount}
                onChange={(e) => {
                  const val = e.target.value;
                  // Allow empty or digits only
                  if (val === "" || /^\d+$/.test(val)) {
                    setNodeCount(val);
                  }
                }}
                onClick={() => nodeCountRef.current?.select()}
                onFocus={() => nodeCountRef.current?.select()}
                className="w-full"
              />
              <p className="text-xs text-muted-foreground">
                Set to 0 for complete scale-down (useful for testing)
              </p>
            </div>
          )}

          {/* Min/Max Node Count (only when autoscaling enabled) */}
          {autoscalingEnabled && (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="minNodes">Min Nodes</Label>
                  <Input
                    ref={minNodeCountRef}
                    id="minNodes"
                    type="text"
                    value={minNodeCount}
                    onChange={(e) => {
                      const val = e.target.value;
                      // Allow empty or digits only
                      if (val === "" || /^\d+$/.test(val)) {
                        setMinNodeCount(val);
                      }
                    }}
                    onClick={() => minNodeCountRef.current?.select()}
                    onFocus={() => minNodeCountRef.current?.select()}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="maxNodes">Max Nodes</Label>
                  <Input
                    ref={maxNodeCountRef}
                    id="maxNodes"
                    type="text"
                    value={maxNodeCount}
                    onChange={(e) => {
                      const val = e.target.value;
                      // Allow empty or digits only
                      if (val === "" || /^\d+$/.test(val)) {
                        setMaxNodeCount(val);
                      }
                    }}
                    onClick={() => maxNodeCountRef.current?.select()}
                    onFocus={() => maxNodeCountRef.current?.select()}
                  />
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Cluster autoscaler will scale between {minNodeCount} and {maxNodeCount} nodes
              </p>
            </>
          )}
        </CardContent>
      </Card>

      {/* Sequential Execution */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ArrowDownUp className="w-4 h-4" />
            Sequential Execution
          </CardTitle>
          <CardDescription>
            Mark this node pool for sequential execution during batch operations
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="sequenceOrder">Execution Order</Label>
            <Select value={sequenceOrder} onValueChange={setSequenceOrder}>
              <SelectTrigger id="sequenceOrder">
                <SelectValue placeholder="No sequencing" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">No sequencing</SelectItem>
                <SelectItem value="1">*1 (Execute first)</SelectItem>
                <SelectItem value="2">*2 (Execute after *1)</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {sequenceOrder === "1" && "This pool will be executed first in sequential mode"}
              {sequenceOrder === "2" && "This pool will be executed after *1 completes"}
              {sequenceOrder === "none" && "This pool will be executed normally (not sequentially)"}
            </p>
          </div>

          {/* Cordon/Drain Config - só aparece quando sequenceOrder != "none" */}
          {sequenceOrder !== "none" && (
            <div className="pt-4 border-t space-y-3">
              <div className="flex items-center gap-3">
                <Checkbox
                  id="cordon-drain-enabled"
                  checked={cordonDrainEnabled}
                  onCheckedChange={(checked) => setCordonDrainEnabled(checked as boolean)}
                />
                <div className="flex-1">
                  <Label htmlFor="cordon-drain-enabled" className="cursor-pointer font-medium flex items-center gap-2">
                    <Shield className="w-4 h-4 text-orange-500" />
                    Cordon/Drain Config
                  </Label>
                  <p className="text-xs text-muted-foreground mt-1">
                    {cordonDrainEnabled
                      ? "Modal de configuração será exibido antes de salvar no staging"
                      : "Aplicação será feita sem evacuação de nodes"}
                  </p>
                </div>
              </div>

              {cordonDrainEnabled && cordonDrainConfig && (
                <div className="p-3 border rounded-md bg-muted/30 space-y-1 text-xs">
                  <p className="font-semibold text-primary">Configuração atual:</p>
                  {cordonDrainConfig.cordonEnabled && <p>✓ CORDON habilitado</p>}
                  {cordonDrainConfig.drainEnabled && (
                    <>
                      <p>✓ DRAIN habilitado</p>
                      <p className="text-muted-foreground ml-4">
                        Grace Period: {cordonDrainConfig.gracePeriod}s | Timeout: {cordonDrainConfig.timeout}s
                      </p>
                    </>
                  )}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Original Values */}
      {hasChanges && (
        <Card className="border-yellow-500/50 bg-yellow-50 dark:bg-yellow-950/20">
          <CardHeader>
            <CardTitle className="text-sm">Original Values</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            {(() => {
              // Get reference values (staged if exists, otherwise original)
              const stagedPool = staging.stagedNodePools.find(
                np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
              );
              const refPool = stagedPool || nodePool;

              return (
                <>
                  {(nodeCount === "" ? 0 : parseInt(nodeCount)) !== refPool.node_count && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Node Count:</span>
                      <span className="line-through">{refPool.node_count}</span>
                    </div>
                  )}
                  {(minNodeCount === "" ? 0 : parseInt(minNodeCount)) !== refPool.min_node_count && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Min Nodes:</span>
                      <span className="line-through">{refPool.min_node_count}</span>
                    </div>
                  )}
                  {(maxNodeCount === "" ? 0 : parseInt(maxNodeCount)) !== refPool.max_node_count && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Max Nodes:</span>
                      <span className="line-through">{refPool.max_node_count}</span>
                    </div>
                  )}
                  {autoscalingEnabled !== refPool.autoscaling_enabled && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Autoscaling:</span>
                      <span className="line-through">
                        {refPool.autoscaling_enabled ? "Enabled" : "Disabled"}
                      </span>
                    </div>
                  )}
                </>
              );
            })()}
          </CardContent>
        </Card>
      )}

        </TabsContent>

        {/* Tab Nodes */}
        <TabsContent value="nodes" className="space-y-4 mt-4">
          {nodesLoading ? (
            <div className="flex items-center justify-center py-12">
              <div className="flex items-center gap-2 text-muted-foreground">
                <RefreshCcw className="w-4 h-4 animate-spin" />
                Loading nodes...
              </div>
            </div>
          ) : nodesError ? (
            <div className="text-center text-destructive py-12">
              <p className="mb-4">Error loading nodes: {nodesError}</p>
              <Button onClick={() => refetchNodes()} variant="outline" size="sm">
                <RefreshCcw className="w-4 h-4 mr-2" />
                Retry
              </Button>
            </div>
          ) : nodes.length === 0 ? (
            <div className="text-center text-muted-foreground py-12">
              No nodes found in this pool
            </div>
          ) : (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <CardTitle className="flex items-center gap-2">
                      <Server className="w-5 h-5 flex-shrink-0" />
                      Nodes - {nodePool.name}
                    </CardTitle>
                    <CardDescription>
                      {(() => {
                        const q = nodeSearch.toLowerCase();
                        const activeCount = q ? nodes.filter(n => n.name.toLowerCase().includes(q)).length : nodes.length;
                        const removedCount = q ? removedNodes.filter(n => n.name.toLowerCase().includes(q)).length : removedNodes.length;
                        const base = q
                          ? `${activeCount} de ${nodes.length} node${nodes.length !== 1 ? "s" : ""}`
                          : `${nodes.length} node${nodes.length !== 1 ? "s" : ""} in this pool`;
                        return removedCount > 0 ? `${base} · ${removedCount} removido${removedCount !== 1 ? "s" : ""}` : base;
                      })()}
                    </CardDescription>
                  </div>
                  <div className="flex items-center gap-2 flex-shrink-0">
                    <div className="relative">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
                      <input
                        type="text"
                        placeholder="Buscar node..."
                        value={nodeSearch}
                        onChange={e => setNodeSearch(e.target.value)}
                        className="h-8 w-44 pl-7 pr-3 text-xs rounded-md border border-input bg-background focus:outline-none focus:ring-2 focus:ring-ring"
                      />
                    </div>
                    <Button onClick={() => refetchNodes()} variant="outline" size="sm">
                      <RefreshCcw className="w-4 h-4 mr-2" />
                      Atualizar
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="border rounded-lg overflow-hidden">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead>CPU</TableHead>
                        <TableHead>Memory</TableHead>
                        <TableHead>Pods</TableHead>
                        <TableHead>Metadados</TableHead>
                        <TableHead>Age</TableHead>
                        <TableHead className="text-right">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {nodes.filter(n =>
                        !nodeSearch || n.name.toLowerCase().includes(nodeSearch.toLowerCase())
                      ).map((node: NodeInfo) => (
                        <TableRow key={node.name} className="hover:bg-muted/50">
                          <TableCell className="font-medium font-mono text-sm">
                            {node.name}
                          </TableCell>
                          <TableCell>
                            <StatusBadge status={node.status} />
                            {node.unschedulable && (
                              <Badge variant="secondary" className="ml-2">
                                Cordoned
                              </Badge>
                            )}
                          </TableCell>
                          <TableCell>
                            {node.cpu_used ? (
                              <div className="space-y-1">
                                <div className="text-sm font-mono">
                                  {node.cpu_used} / {node.cpu_allocatable}
                                </div>
                                <Badge
                                  variant={node.cpu_usage_percent > 80 ? "destructive" : "secondary"}
                                  className="text-xs"
                                >
                                  {node.cpu_usage_percent.toFixed(1)}%
                                </Badge>
                              </div>
                            ) : (
                              <span className="text-muted-foreground text-sm">N/A</span>
                            )}
                          </TableCell>
                          <TableCell>
                            {node.memory_used ? (
                              <div className="space-y-1">
                                <div className="text-sm font-mono">
                                  {node.memory_used} / {node.memory_allocatable}
                                </div>
                                <Badge
                                  variant={node.memory_usage_percent > 80 ? "destructive" : "secondary"}
                                  className="text-xs"
                                >
                                  {node.memory_usage_percent.toFixed(1)}%
                                </Badge>
                              </div>
                            ) : (
                              <span className="text-muted-foreground text-sm">N/A</span>
                            )}
                          </TableCell>
                          <TableCell>
                            <div className="text-sm font-mono">
                              {node.pods_running} / {node.pods_total}
                            </div>
                            <div className="text-xs text-muted-foreground">
                              of {node.pods_capacity} max
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="flex flex-col gap-1">
                              {/* Node Pool Tags (Azure Level) — carregado assincronamente */}
                              {azureInfoLoading ? (
                                <div className="flex items-center gap-1">
                                  <Tag className="h-3 w-3 text-blue-300 animate-pulse" />
                                  <Badge variant="outline" className="text-[10px] px-1 py-0 bg-blue-50 text-blue-400 border-blue-200">
                                    Pool: …
                                  </Badge>
                                </div>
                              ) : azureInfo && Object.keys(azureInfo.cluster_tags).length > 0 ? (
                                <div className="flex items-center gap-1">
                                  <Tag className="h-3 w-3 text-blue-500" />
                                  <Badge variant="outline" className="text-[10px] px-1 py-0 bg-blue-50 text-blue-700 border-blue-300">
                                    Pool: {Object.keys(azureInfo.cluster_tags).length}
                                  </Badge>
                                </div>
                              ) : null}

                              {/* Node Labels (Kubernetes Individual) */}
                              {node.labels && Object.keys(node.labels).length > 0 && (
                                <div className="flex items-center gap-1">
                                  <Tags className="h-3 w-3 text-green-500" />
                                  <Badge variant="outline" className="text-[10px] px-1 py-0 bg-green-50 text-green-700 border-green-300">
                                    Node: {Object.keys(node.labels).length}
                                  </Badge>
                                </div>
                              )}

                              {/* Node Taints */}
                              {node.taints && node.taints.length > 0 && (
                                <div className="flex items-center gap-1">
                                  <AlertTriangle className="h-3 w-3 text-orange-500" />
                                  <Badge variant="outline" className="text-[10px] px-1 py-0 bg-orange-50 text-orange-700 border-orange-300">
                                    Taints: {node.taints.length}
                                  </Badge>
                                </div>
                              )}

                              {/* Empty state */}
                              {!azureInfoLoading &&
                               (!azureInfo || Object.keys(azureInfo.cluster_tags).length === 0) &&
                               (!node.labels || Object.keys(node.labels).length === 0) &&
                               (!node.taints || node.taints.length === 0) && (
                                <span className="text-xs text-muted-foreground">Nenhum</span>
                              )}
                            </div>
                          </TableCell>
                          <TableCell className="text-sm">{node.age}</TableCell>
                          <TableCell className="text-right">
                            <Button
                              onClick={() => handleViewNodeDetails(node.name)}
                              variant="ghost"
                              size="sm"
                            >
                              <Eye className="w-4 h-4 mr-2" />
                              View Details
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))}
                      {/* Nodes removidos — aparecem na busca com badge */}
                      {removedNodes
                        .filter(n => !nodeSearch || n.name.toLowerCase().includes(nodeSearch.toLowerCase()))
                        .map(n => (
                          <TableRow key={`removed-${n.name}`} className="opacity-60 hover:opacity-80">
                            <TableCell className="font-medium font-mono text-sm">
                              <div className="flex items-center gap-2">
                                {n.name}
                                <Badge variant="destructive" className="text-[10px] px-1.5 py-0 h-4 flex-shrink-0">
                                  Removido
                                </Badge>
                              </div>
                            </TableCell>
                            <TableCell>
                              <span className="text-muted-foreground text-xs">—</span>
                            </TableCell>
                            <TableCell><span className="text-muted-foreground text-xs">—</span></TableCell>
                            <TableCell><span className="text-muted-foreground text-xs">—</span></TableCell>
                            <TableCell><span className="text-muted-foreground text-xs">—</span></TableCell>
                            <TableCell><span className="text-muted-foreground text-xs">—</span></TableCell>
                            <TableCell>
                              {n.removed_at
                                ? <span className="text-xs text-muted-foreground">{new Date(n.removed_at).toLocaleString("pt-BR")}</span>
                                : <span className="text-muted-foreground text-xs">—</span>}
                            </TableCell>
                            <TableCell className="text-right">
                              <Button
                                variant="outline"
                                size="sm"
                                className="h-7 text-xs gap-1"
                                onClick={() => setRemovedDetail({ name: n.name, details: n.details, reason: n.reason, removed_at: n.removed_at })}
                              >
                                <Info className="w-3 h-3" />
                                Ver motivo
                              </Button>
                            </TableCell>
                          </TableRow>
                        ))}

                      {/* Empty state: busca sem resultados em nenhuma das listas */}
                      {nodeSearch &&
                        !nodes.some(n => n.name.toLowerCase().includes(nodeSearch.toLowerCase())) &&
                        !removedNodes.some(n => n.name.toLowerCase().includes(nodeSearch.toLowerCase())) && (
                          <TableRow>
                            <TableCell colSpan={8} className="text-center text-muted-foreground text-sm py-8">
                              Nenhum node encontrado para "{nodeSearch}"
                            </TableCell>
                          </TableRow>
                        )}
                    </TableBody>
                  </Table>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        {/* Tab Disk */}
        <TabsContent value="disk" className="space-y-4 mt-4">
          {diskMetricsLoading ? (
            <div className="flex items-center justify-center py-12">
              <div className="flex items-center gap-2 text-muted-foreground">
                <RefreshCcw className="w-4 h-4 animate-spin" />
                Loading disk metrics...
              </div>
            </div>
          ) : !diskMetrics ? (
            <div className="text-center text-muted-foreground py-12">
              No disk metrics available
            </div>
          ) : (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <CardTitle className="flex items-center gap-2">
                      <HardDrive className="w-5 h-5" />
                      Disk Usage - {nodePool.name}
                    </CardTitle>
                    <CardDescription>
                      {diskMetrics.node_count} node{diskMetrics.node_count !== 1 ? "s" : ""} in this pool
                    </CardDescription>
                  </div>
                  <Button onClick={() => refetchDiskMetrics()} variant="outline" size="sm" disabled={diskMetricsLoading}>
                    <RefreshCcw className={`w-4 h-4 mr-2 ${diskMetricsLoading ? "animate-spin" : ""}`} />
                    Atualizar
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <p className="text-sm text-muted-foreground">Total</p>
                    <p className="text-2xl font-bold">
                      {(diskMetrics.total_bytes / (1024**3)).toFixed(1)} GiB
                    </p>
                  </div>
                  <div>
                    <p className="text-sm text-muted-foreground">Used</p>
                    <p className="text-2xl font-bold">
                      {(diskMetrics.used_bytes / (1024**3)).toFixed(1)} GiB
                    </p>
                  </div>
                  <div>
                    <p className="text-sm text-muted-foreground">Usage</p>
                    <p className="text-2xl font-bold">
                      <Badge
                        variant={diskMetrics.usage_percent > 80 ? "destructive" : "secondary"}
                        className="text-lg"
                      >
                        {diskMetrics.usage_percent.toFixed(1)}%
                      </Badge>
                    </p>
                  </div>
                </div>
                <Separator />
                <div>
                  <Button
                    onClick={() => setShowDiskDetailsModal(true)}
                    variant="outline"
                    className="w-full"
                  >
                    <Info className="w-4 h-4 mr-2" />
                    View Detailed Metrics
                  </Button>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        {/* Tab Conntrack */}
        <TabsContent value="conntrack">
          {nodePool && (
            <ConntrackTab cluster={clusterWithAdmin} nodepool={nodePool.name} />
          )}
        </TabsContent>
      </Tabs>

      {/* Action Buttons */}
      <div className="flex gap-3 pt-3 border-t border-border">
        <ProtectedAction>
          <Button
            onClick={handleApply}
            disabled={!hasChanges || isApplying}
            className="flex-1 bg-gradient-primary h-9"
          >
            <Save className="w-4 h-4 mr-2" />
            💾 Salvar (Staging)
          </Button>
        </ProtectedAction>

        <ProtectedAction>
          <Button
            onClick={handleApplyNow}
            variant="default"
            disabled={!hasChanges || isApplying}
            className="flex-1 bg-success hover:bg-success/90 h-9"
          >
            {isApplying ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              Aplicando...
            </>
          ) : (
            <>
              <Zap className="w-4 h-4 mr-2" />
              ✅ Aplicar Agora
            </>
          )}
        </Button>
        </ProtectedAction>

        <Button
          onClick={handleReset}
          disabled={!hasChanges || isApplying}
          variant="outline"
          className="h-9"
        >
          <RotateCcw className="w-4 h-4 mr-2" />
          Cancelar
        </Button>
      </div>

      {/* Cordon/Drain Config Modal */}
      <CordonDrainConfigModal
        open={showCordonDrainModal}
        onOpenChange={setShowCordonDrainModal}
        onConfirm={handleCordonDrainConfirm}
        nodePoolName={nodePool.name}
      />

      {/* Disk Details Modal */}
      <NodePoolDiskDetailsModal
        open={showDiskDetailsModal}
        onOpenChange={setShowDiskDetailsModal}
        diskMetrics={diskMetrics}
        loading={diskMetricsLoading}
        vmSize={nodePool.vm_size}
        cluster={clusterWithAdmin}
      />

      {/* Node Details Modal */}
      <NodeDetailsModal
        key={`modal-${modalKey}`} // Force complete re-render with controlled key
        open={showNodeDetailsModal}
        onOpenChange={handleCloseNodeModal}
        nodeDetails={nodeDetails}
        loading={loadingNodeDetails}
        vmSize={nodePool?.vm_size}
        azureInfo={azureInfo}
      />

      {/* NodePool Prediction Modal */}
      <NodePoolPredictionModal
        open={predictionModalOpen}
        onOpenChange={setPredictionModalOpen}
        loading={predictionLoading && !historyResult}
        result={historyResult ?? predictionResult}
        nodepoolName={nodePool.name}
        onShowHistory={() => {
          setPredictionModalOpen(false);
          setHistoryModalOpen(true);
        }}
      />

      {/* NodePool Prediction History Modal */}
      <NodePoolPredictionHistoryModal
        open={historyModalOpen}
        onOpenChange={setHistoryModalOpen}
        cluster={clusterWithAdmin}
        nodepool={nodePool.name}
        onSelectRecord={(result) => {
          setHistoryResult(result);
          setHistoryModalOpen(false);
          setPredictionModalOpen(true);
        }}
      />

      {/* Modal: motivo de remoção do node */}
      {removedDetail && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setRemovedDetail(null)}>
          <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-2xl mx-4 flex flex-col max-h-[80vh]" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-4 py-3 border-b border-border flex-shrink-0">
              <div className="min-w-0">
                <p className="text-sm font-semibold font-mono truncate">{removedDetail.name}</p>
                <p className="text-[11px] text-muted-foreground mt-0.5">
                  {removedDetail.removed_at
                    ? `Removido em ${new Date(removedDetail.removed_at).toLocaleString("pt-BR")}`
                    : "Data de remoção desconhecida"}
                </p>
              </div>
              <button onClick={() => setRemovedDetail(null)} className="ml-4 text-muted-foreground hover:text-foreground text-lg leading-none flex-shrink-0">✕</button>
            </div>
            {removedDetail.reason && (
              <div className="px-4 py-2 bg-muted/30 border-b border-border flex-shrink-0">
                <p className="text-xs font-medium text-muted-foreground mb-0.5">Motivo</p>
                <p className="text-xs">{removedDetail.reason}</p>
              </div>
            )}
            <div className="flex-1 overflow-y-auto min-h-0 p-4">
              <p className="text-[11px] font-medium text-muted-foreground mb-2">Detalhes (logs / eventos)</p>
              <pre className="text-[11px] font-mono whitespace-pre-wrap break-all bg-muted/40 rounded p-3 leading-relaxed">
                {removedDetail.details || "Sem detalhes disponíveis"}
              </pre>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
