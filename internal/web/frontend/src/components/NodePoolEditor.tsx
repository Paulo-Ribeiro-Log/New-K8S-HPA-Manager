import { useState, useEffect, useRef } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { NodePool } from "@/lib/api/types";
import { Save, RotateCcw, Server, Cpu, HardDrive, ArrowDownUp, Loader2, Zap } from "lucide-react";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";

interface NodePoolEditorProps {
  nodePool: NodePool | null;
  onApply?: (nodePool: NodePool, original: NodePool) => void;
  onApplied?: () => void;
}

export const NodePoolEditor = ({ nodePool, onApply, onApplied }: NodePoolEditorProps) => {
  const staging = useStaging();

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

  // Initialize form when nodePool changes
  useEffect(() => {
    if (nodePool) {
      setNodeCount(String(nodePool.node_count));
      setMinNodeCount(String(nodePool.min_node_count));
      setMaxNodeCount(String(nodePool.max_node_count));
      setAutoscalingEnabled(nodePool.autoscaling_enabled);

      // Check if this pool is already in staging
      const stagedPool = staging.stagedNodePools.find(
        np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
      );
      
      if (stagedPool?.sequence_order) {
        setSequenceOrder(stagedPool.sequence_order.toString());
      } else {
        setSequenceOrder("none");
      }

      setHasChanges(false);
    }
  }, [nodePool, staging.stagedNodePools]);

  // Check for changes whenever form values update
  useEffect(() => {
    if (!nodePool) return;

    const changed =
      (parseInt(nodeCount) || 0) !== nodePool.node_count ||
      (parseInt(minNodeCount) || 0) !== nodePool.min_node_count ||
      (parseInt(maxNodeCount) || 1) !== nodePool.max_node_count ||
      autoscalingEnabled !== nodePool.autoscaling_enabled;

    setHasChanges(changed);
  }, [nodeCount, minNodeCount, maxNodeCount, autoscalingEnabled, nodePool]);

  const handleReset = () => {
    if (nodePool) {
      setNodeCount(String(nodePool.node_count));
      setMinNodeCount(String(nodePool.min_node_count));
      setMaxNodeCount(String(nodePool.max_node_count));
      setAutoscalingEnabled(nodePool.autoscaling_enabled);
      setHasChanges(false);
    }
  };

  const handleApply = () => {
    if (!nodePool) return;

    // Parse string values to numbers
    const nodeCountNum = parseInt(nodeCount) || 0;
    const minNodeCountNum = parseInt(minNodeCount) || 0;
    const maxNodeCountNum = parseInt(maxNodeCount) || 1;

    // First add to staging if not already there
    staging.addNodePoolToStaging(nodePool);

    // Then update with new values
    const updates: Partial<NodePool> = {
      node_count: nodeCountNum,
      min_node_count: minNodeCountNum,
      max_node_count: maxNodeCountNum,
      autoscaling_enabled: autoscalingEnabled,
    };

    // Add sequence order if specified
    if (sequenceOrder !== "none") {
      updates.sequence_order = parseInt(sequenceOrder);
    }

    staging.updateNodePoolInStaging(nodePool.cluster_name, nodePool.name, updates);
    setHasChanges(false);

    // Call optional callback
    onApplied?.();
  };

  const handleApplyNow = async () => {
    if (!nodePool) return;

    // Parse string values to numbers
    const nodeCountNum = parseInt(nodeCount) || 0;
    const minNodeCountNum = parseInt(minNodeCount) || 0;
    const maxNodeCountNum = parseInt(maxNodeCount) || 1;

    setIsApplying(true);

    try {
      // Prepare updated node pool data
      const updatedNodePool: NodePool = {
        ...nodePool,
        node_count: nodeCountNum,
        min_node_count: minNodeCountNum,
        max_node_count: maxNodeCountNum,
        autoscaling_enabled: autoscalingEnabled,
      };

      // Log changes
      console.log('⚙️ Applying Node Pool changes:', {
        name: nodePool.name,
        cluster: nodePool.cluster_name,
        changes: {
          node_count: nodePool.node_count !== nodeCountNum ? `${nodePool.node_count} → ${nodeCountNum}` : 'unchanged',
          min_node_count: nodePool.min_node_count !== minNodeCountNum ? `${nodePool.min_node_count} → ${minNodeCountNum}` : 'unchanged',
          max_node_count: nodePool.max_node_count !== maxNodeCountNum ? `${nodePool.max_node_count} → ${maxNodeCountNum}` : 'unchanged',
          autoscaling_enabled: nodePool.autoscaling_enabled !== autoscalingEnabled ? `${nodePool.autoscaling_enabled} → ${autoscalingEnabled}` : 'unchanged',
        }
      });

      // Call API to update node pool
      await apiClient.updateNodePool(
        nodePool.cluster_name,
        nodePool.resource_group,
        nodePool.name,
        {
          node_count: nodeCountNum,
          min_node_count: minNodeCountNum,
          max_node_count: maxNodeCountNum,
          autoscaling_enabled: autoscalingEnabled,
        }
      );

      toast.success(`✅ Node Pool ${nodePool.name} aplicado com sucesso`);
      setHasChanges(false);

      // Call optional callback
      onApplied?.();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Erro desconhecido";
      console.error('❌ Error applying node pool:', errorMessage);
      toast.error(`❌ Erro ao aplicar ${nodePool.name}: ${errorMessage}`);
    } finally {
      setIsApplying(false);
    }
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
    <div className="space-y-4">
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
        <p className="text-sm text-muted-foreground">
          {nodePool.cluster_name} • {nodePool.resource_group}
        </p>
      </div>

      <Separator />

      {/* VM Information */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Cpu className="w-4 h-4" />
            VM Configuration
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
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
        <CardContent>
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
        </CardContent>
      </Card>

      {/* Original Values */}
      {hasChanges && (
        <Card className="border-yellow-500/50 bg-yellow-50 dark:bg-yellow-950/20">
          <CardHeader>
            <CardTitle className="text-sm">Original Values</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            {(parseInt(nodeCount) || 0) !== nodePool.node_count && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Node Count:</span>
                <span className="line-through">{nodePool.node_count}</span>
              </div>
            )}
            {(parseInt(minNodeCount) || 0) !== nodePool.min_node_count && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Min Nodes:</span>
                <span className="line-through">{nodePool.min_node_count}</span>
              </div>
            )}
            {(parseInt(maxNodeCount) || 1) !== nodePool.max_node_count && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Max Nodes:</span>
                <span className="line-through">{nodePool.max_node_count}</span>
              </div>
            )}
            {autoscalingEnabled !== nodePool.autoscaling_enabled && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Autoscaling:</span>
                <span className="line-through">
                  {nodePool.autoscaling_enabled ? "Enabled" : "Disabled"}
                </span>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Action Buttons */}
      <div className="flex gap-3 pt-3 border-t border-border">
        <Button
          onClick={handleApply}
          disabled={!hasChanges || isApplying}
          className="flex-1 bg-gradient-primary h-9"
        >
          <Save className="w-4 h-4 mr-2" />
          💾 Salvar (Staging)
        </Button>

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
    </div>
  );
};
