import { useState, useEffect } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Save, CheckCircle, RotateCcw } from "lucide-react";
import { useStaging } from "@/contexts/StagingContext";
import type { HPA } from "@/lib/api/types";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface HPAEditorProps {
  hpa: HPA | null;
  onApplied?: () => void;
  onApply?: (hpa: HPA, original: HPA) => void;
}

export const HPAEditor = ({ hpa, onApplied, onApply }: HPAEditorProps) => {
  const staging = useStaging();

  // Form state - HPA Config
  const [minReplicas, setMinReplicas] = useState(0);
  const [maxReplicas, setMaxReplicas] = useState(1);
  const [targetCPU, setTargetCPU] = useState<number | undefined>(undefined);
  const [targetMemory, setTargetMemory] = useState<number | undefined>(undefined);

  // Form state - Resources (target values - editable)
  const [targetCpuRequest, setTargetCpuRequest] = useState("");
  const [targetCpuLimit, setTargetCpuLimit] = useState("");
  const [targetMemoryRequest, setTargetMemoryRequest] = useState("");
  const [targetMemoryLimit, setTargetMemoryLimit] = useState("");

  // Form state - Rollouts
  const [performRollout, setPerformRollout] = useState(false);
  const [performDaemonSetRollout, setPerformDaemonSetRollout] = useState(false);
  const [performStatefulSetRollout, setPerformStatefulSetRollout] = useState(false);

  // Loading states
  const [isSaving, setIsSaving] = useState(false);

  // Initialize form when HPA changes
  // Use a combination of hpa reference + key fields to detect updates
  useEffect(() => {
    if (hpa) {
      console.log('[HPAEditor] Resetting form with HPA:', hpa.name);
      setMinReplicas(hpa.min_replicas ?? 0);
      setMaxReplicas(hpa.max_replicas ?? 1);
      setTargetCPU(hpa.target_cpu ?? undefined);
      setTargetMemory(hpa.target_memory ?? undefined);

      // Initialize target values from original_values (current deployment values)
      setTargetCpuRequest(hpa.target_cpu_request ?? hpa.original_values?.cpu_request ?? "");
      setTargetCpuLimit(hpa.target_cpu_limit ?? hpa.original_values?.cpu_limit ?? "");
      setTargetMemoryRequest(hpa.target_memory_request ?? hpa.original_values?.memory_request ?? "");
      setTargetMemoryLimit(hpa.target_memory_limit ?? hpa.original_values?.memory_limit ?? "");

      setPerformRollout(false);
      setPerformDaemonSetRollout(false);
      setPerformStatefulSetRollout(false);
    }
  }, [hpa, hpa?.min_replicas, hpa?.max_replicas, hpa?.target_cpu, hpa?.target_memory]);

  if (!hpa) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        Selecione um HPA para editar
      </div>
    );
  }

  const handleSave = () => {
    // Validate
    if (minReplicas > maxReplicas) {
      toast.error("Min Replicas não pode ser maior que Max Replicas");
      return;
    }
    if (targetCPU !== undefined && (targetCPU < 1 || targetCPU > 100)) {
      toast.error("Target CPU deve estar entre 1 e 100%");
      return;
    }
    if (targetMemory !== undefined && (targetMemory < 1 || targetMemory > 100)) {
      toast.error("Target Memory deve estar entre 1 e 100%");
      return;
    }

    setIsSaving(true);

    // First add to staging if not already there
    staging.addHPAToStaging(hpa);

    // Then update with modified values
    const updates: Partial<HPA> = {
      min_replicas: minReplicas,
      max_replicas: maxReplicas,
      target_cpu: targetCPU ?? null,
      target_memory: targetMemory ?? null,
      target_cpu_request: targetCpuRequest || undefined,
      target_cpu_limit: targetCpuLimit || undefined,
      target_memory_request: targetMemoryRequest || undefined,
      target_memory_limit: targetMemoryLimit || undefined,
      perform_rollout: performRollout,
      perform_daemonset_rollout: performDaemonSetRollout,
      perform_statefulset_rollout: performStatefulSetRollout,
    };

    staging.updateHPAInStaging(hpa.cluster, hpa.namespace, hpa.name, updates);

    const changesCount = staging.getChangesCount();
    toast.success(`HPA salvo no staging (${changesCount.total} alteraç${changesCount.total === 1 ? 'ão' : 'ões'} pendente${changesCount.total === 1 ? '' : 's'})`);
    setIsSaving(false);
  };

  const handleApply = () => {
    // Validate
    if (minReplicas > maxReplicas) {
      toast.error("Min Replicas não pode ser maior que Max Replicas");
      return;
    }

    // Create modified HPA
    const modifiedHPA: HPA = {
      ...hpa,
      min_replicas: minReplicas,
      max_replicas: maxReplicas,
      target_cpu: targetCPU ?? null,
      target_memory: targetMemory ?? null,
      target_cpu_request: targetCpuRequest || undefined,
      target_cpu_limit: targetCpuLimit || undefined,
      target_memory_request: targetMemoryRequest || undefined,
      target_memory_limit: targetMemoryLimit || undefined,
      perform_rollout: performRollout,
      perform_daemonset_rollout: performDaemonSetRollout,
      perform_statefulset_rollout: performStatefulSetRollout,
    };

    // Call the callback to open modal
    if (onApply) {
      onApply(modifiedHPA, hpa);
    }
  };

  const handleReset = () => {
    setMinReplicas(hpa.min_replicas ?? 0);
    setMaxReplicas(hpa.max_replicas ?? 1);
    setTargetCPU(hpa.target_cpu ?? undefined);
    setTargetMemory(hpa.target_memory ?? undefined);
    setTargetCpuRequest(hpa.target_cpu_request ?? hpa.original_values?.cpu_request ?? "");
    setTargetCpuLimit(hpa.target_cpu_limit ?? hpa.original_values?.cpu_limit ?? "");
    setTargetMemoryRequest(hpa.target_memory_request ?? hpa.original_values?.memory_request ?? "");
    setTargetMemoryLimit(hpa.target_memory_limit ?? hpa.original_values?.memory_limit ?? "");
    setPerformRollout(false);
    setPerformDaemonSetRollout(false);
    setPerformStatefulSetRollout(false);
    toast.info("Alterações descartadas");
  };

  const isModified =
    minReplicas !== (hpa.min_replicas ?? 0) ||
    maxReplicas !== (hpa.max_replicas ?? 1) ||
    targetCPU !== (hpa.target_cpu ?? undefined) ||
    targetMemory !== (hpa.target_memory ?? undefined) ||
    targetCpuRequest !== (hpa.target_cpu_request ?? hpa.original_values?.cpu_request ?? "") ||
    targetCpuLimit !== (hpa.target_cpu_limit ?? hpa.original_values?.cpu_limit ?? "") ||
    targetMemoryRequest !== (hpa.target_memory_request ?? hpa.original_values?.memory_request ?? "") ||
    targetMemoryLimit !== (hpa.target_memory_limit ?? hpa.original_values?.memory_limit ?? "") ||
    performRollout !== false ||
    performDaemonSetRollout !== false ||
    performStatefulSetRollout !== false;

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Header */}
      <div className="space-y-1">
        <h4 className="font-semibold text-base text-foreground">{hpa.name}</h4>
        <p className="text-xs text-muted-foreground">{hpa.namespace}</p>
        {isModified && (
          <p className="text-xs text-warning">⚠️ Modificado (não aplicado)</p>
        )}
      </div>

      {/* Form Fields */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="minReplicas" className="text-sm">Minimum Replicas</Label>
          <Input
            id="minReplicas"
            type="number"
            min={0}
            value={minReplicas}
            onChange={(e) => {
              const val = e.target.value;
              setMinReplicas(val === "" ? 0 : parseInt(val));
            }}
            className="bg-background h-9"
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="maxReplicas" className="text-sm">Maximum Replicas</Label>
          <Input
            id="maxReplicas"
            type="number"
            min={1}
            value={maxReplicas}
            onChange={(e) => {
              const val = e.target.value;
              setMaxReplicas(val === "" ? 1 : parseInt(val));
            }}
            className="bg-background h-9"
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="targetCPU" className="text-sm">Target CPU (%)</Label>
          <Input
            id="targetCPU"
            type="number"
            min={1}
            max={100}
            value={targetCPU ?? ""}
            onChange={(e) => {
              const val = e.target.value;
              setTargetCPU(val === "" ? undefined : parseInt(val));
            }}
            placeholder="Não configurado"
            className="bg-background h-9"
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="targetMemory" className="text-sm">Target Memory (%)</Label>
          <Input
            id="targetMemory"
            type="number"
            min={1}
            max={100}
            value={targetMemory ?? ""}
            onChange={(e) => {
              const val = e.target.value;
              setTargetMemory(val === "" ? undefined : parseInt(val));
            }}
            placeholder="Não configurado"
            className="bg-background h-9"
          />
        </div>

        <div className="space-y-1.5">
          <Label className="text-sm">Current Replicas</Label>
          <Input
            value={hpa.current_replicas ?? 0}
            disabled
            className="bg-muted h-9"
          />
        </div>
      </div>

      {/* Deployment Resources */}
      <div className="space-y-3 pt-3 border-t border-border">
        <Label className="text-sm font-semibold">Deployment Resources</Label>
        <p className="text-xs text-muted-foreground">
          💡 Configure requests e limits de CPU/Memory do deployment
        </p>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="cpuRequest" className="text-sm">CPU Request</Label>
            <Input
              id="cpuRequest"
              type="text"
              value={targetCpuRequest}
              onChange={(e) => setTargetCpuRequest(e.target.value)}
              placeholder="ex: 100m, 0.5, 1"
              className="bg-background h-9"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cpuLimit" className="text-sm">CPU Limit</Label>
            <Input
              id="cpuLimit"
              type="text"
              value={targetCpuLimit}
              onChange={(e) => setTargetCpuLimit(e.target.value)}
              placeholder="ex: 200m, 1, 2"
              className="bg-background h-9"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="memoryRequest" className="text-sm">Memory Request</Label>
            <Input
              id="memoryRequest"
              type="text"
              value={targetMemoryRequest}
              onChange={(e) => setTargetMemoryRequest(e.target.value)}
              placeholder="ex: 128Mi, 256Mi, 1Gi"
              className="bg-background h-9"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="memoryLimit" className="text-sm">Memory Limit</Label>
            <Input
              id="memoryLimit"
              type="text"
              value={targetMemoryLimit}
              onChange={(e) => setTargetMemoryLimit(e.target.value)}
              placeholder="ex: 512Mi, 1Gi, 2Gi"
              className="bg-background h-9"
            />
          </div>
        </div>
      </div>

      {/* Rollout Controls */}
      <div className="space-y-3 pt-3 border-t border-border">
        <Label className="text-sm font-semibold">Rollout Options</Label>
        <p className="text-xs text-muted-foreground">
          💡 Reinicia os pods do tipo selecionado após aplicar alterações
        </p>

        <div className="space-y-2">
          <div className="flex items-center space-x-2">
            <Checkbox
              id="deploymentRollout"
              checked={performRollout}
              onCheckedChange={(checked) => setPerformRollout(checked as boolean)}
            />
            <label
              htmlFor="deploymentRollout"
              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
            >
              🔄 Deployment Rollout
            </label>
          </div>

          <div className="flex items-center space-x-2">
            <Checkbox
              id="daemonsetRollout"
              checked={performDaemonSetRollout}
              onCheckedChange={(checked) => setPerformDaemonSetRollout(checked as boolean)}
            />
            <label
              htmlFor="daemonsetRollout"
              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
            >
              🔄 DaemonSet Rollout
            </label>
          </div>

          <div className="flex items-center space-x-2">
            <Checkbox
              id="statefulsetRollout"
              checked={performStatefulSetRollout}
              onCheckedChange={(checked) => setPerformStatefulSetRollout(checked as boolean)}
            />
            <label
              htmlFor="statefulsetRollout"
              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
            >
              🔄 StatefulSet Rollout
            </label>
          </div>
        </div>
      </div>

      {/* Action Buttons */}
      <div className="flex gap-3 pt-3 border-t border-border">
        <Button
          onClick={handleSave}
          disabled={!isModified || isSaving}
          className="flex-1 bg-gradient-primary h-9"
        >
          <Save className="w-4 h-4 mr-2" />
          {isSaving ? "Salvando..." : "💾 Salvar (Staging)"}
        </Button>

        <Button
          onClick={handleApply}
          variant="default"
          className="flex-1 bg-success hover:bg-success/90 h-9"
        >
          <CheckCircle className="w-4 h-4 mr-2" />
          ✅ Aplicar Agora
        </Button>

        <Button
          onClick={handleReset}
          disabled={!isModified}
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
