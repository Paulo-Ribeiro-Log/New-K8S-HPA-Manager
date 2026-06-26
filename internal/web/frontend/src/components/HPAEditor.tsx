import { useState, useEffect, useRef } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Save, CheckCircle, RotateCcw, AlertCircle } from "lucide-react";
import { useStaging } from "@/contexts/StagingContext";
import type { HPA } from "@/lib/api/types";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { validateHPAUpdate, formatValidationErrors, type ValidationError } from "@/lib/validation";
import { ProtectedAction } from "@/components/rbac";
import { AITriggerButton } from "@/components/AITriggerButton";
import { useK8sPermissions } from "@/hooks/useK8sPermissions";

interface HPAEditorProps {
  hpa: HPA | null;
  onApplied?: () => void;
  onApply?: (hpa: HPA, original: HPA) => void;
}

export const HPAEditor = ({ hpa, onApplied, onApply }: HPAEditorProps) => {
  const staging = useStaging();

  // Permissões reais do K8s — determina se botões de escrita são exibidos
  const { permissions } = useK8sPermissions(hpa?.cluster ?? '', hpa?.namespace ?? '');
  const canWrite = permissions.canUpdateHPA;

  // Refs for input fields to enable select-all behavior
  const minReplicasRef = useRef<HTMLInputElement>(null);
  const maxReplicasRef = useRef<HTMLInputElement>(null);
  const targetCPURef = useRef<HTMLInputElement>(null);
  const targetMemoryRef = useRef<HTMLInputElement>(null);

  // Form state - HPA Config (usando string para permitir campo vazio)
  const [minReplicas, setMinReplicas] = useState<string>("0");
  const [maxReplicas, setMaxReplicas] = useState<string>("1");
  const [targetCPU, setTargetCPU] = useState<string>("");
  const [targetMemory, setTargetMemory] = useState<string>("");

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

  // Validation state
  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({});

  // Helper: Detectar HPA problemático (maxed out ou flapping)
  const isHPAProblematic = (): boolean => {
    // TEMPORÁRIO: Sempre mostrar botão para teste
    return true;
    
    /* LÓGICA ORIGINAL (comentada para debug):
    if (!hpa) return false;
    
    // HPA maxed out (currentReplicas == maxReplicas e maxReplicas definido)
    if (hpa.current_replicas && hpa.max_replicas && hpa.current_replicas >= hpa.max_replicas) {
      return true;
    }

    // Flapping detection: muitas mudanças recentes (se tiver essa informação)
    // Por enquanto, apenas maxed out
    
    return false;
    */
  };

  // Initialize form when HPA changes
  // Use a combination of hpa reference + key fields to detect updates
  useEffect(() => {
    if (hpa) {
      console.log('[HPAEditor] Resetting form with HPA:', hpa.name);

      // 🔧 FIX: Check if HPA exists in staging - use staging values as base reference
      const stagedHPA = staging.stagedHPAs.find(
        h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
      );

      // Use staged values if available, otherwise use original HPA props
      const baseHPA = stagedHPA || hpa;
      console.log('[HPAEditor] Using base HPA:', stagedHPA ? 'FROM STAGING' : 'FROM PROPS', baseHPA);

      setMinReplicas(String(baseHPA.min_replicas ?? 0));
      setMaxReplicas(String(baseHPA.max_replicas ?? 1));
      setTargetCPU(baseHPA.target_cpu !== null && baseHPA.target_cpu !== undefined ? String(baseHPA.target_cpu) : "");
      setTargetMemory(baseHPA.target_memory !== null && baseHPA.target_memory !== undefined ? String(baseHPA.target_memory) : "");

      // Initialize target values from original_values (current deployment values)
      setTargetCpuRequest(baseHPA.target_cpu_request ?? baseHPA.original_values?.cpu_request ?? "");
      setTargetCpuLimit(baseHPA.target_cpu_limit ?? baseHPA.original_values?.cpu_limit ?? "");
      setTargetMemoryRequest(baseHPA.target_memory_request ?? baseHPA.original_values?.memory_request ?? "");
      setTargetMemoryLimit(baseHPA.target_memory_limit ?? baseHPA.original_values?.memory_limit ?? "");

      setPerformRollout(baseHPA.perform_rollout ?? false);
      setPerformDaemonSetRollout(baseHPA.perform_daemonset_rollout ?? false);
      setPerformStatefulSetRollout(baseHPA.perform_statefulset_rollout ?? false);
    }
  }, [hpa, hpa?.min_replicas, hpa?.max_replicas, hpa?.target_cpu, hpa?.target_memory, staging.stagedHPAs]);

  if (!hpa) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        Selecione um HPA para editar
      </div>
    );
  }

  const handleSave = () => {
    // Parse string values to numbers
    const minReplicasNum = parseInt(minReplicas) || 0;
    const maxReplicasNum = parseInt(maxReplicas) || 1;
    const targetCPUNum = targetCPU ? parseInt(targetCPU) : undefined;
    const targetMemoryNum = targetMemory ? parseInt(targetMemory) : undefined;

    // Validate using comprehensive validation
    const validationResult = validateHPAUpdate({
      minReplicas: minReplicasNum,
      maxReplicas: maxReplicasNum,
      targetCPU: targetCPUNum,
      targetMemory: targetMemoryNum,
      cpuRequest: targetCpuRequest,
      memoryRequest: targetMemoryRequest,
      cpuLimit: targetCpuLimit,
      memoryLimit: targetMemoryLimit,
    });

    if (!validationResult.valid) {
      // Build error map for visual feedback
      const errorMap: Record<string, string> = {};
      validationResult.errors.forEach(err => {
        errorMap[err.field] = err.message;
      });
      setValidationErrors(errorMap);

      toast.error("Erro de validação", {
        description: formatValidationErrors(validationResult.errors),
        style: {
          background: '#fee2e2',
          border: '1px solid #fca5a5',
          color: '#991b1b',
        },
      });
      return;
    }

    // Clear validation errors on success
    setValidationErrors({});
    setIsSaving(true);

    // First add to staging if not already there
    staging.addHPAToStaging(hpa);

    // Then update with modified values
    const updates: Partial<HPA> = {
      min_replicas: minReplicasNum,
      max_replicas: maxReplicasNum,
      target_cpu: targetCPUNum ?? null,
      target_memory: targetMemoryNum ?? null,
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
    toast.success(`HPA salvo no staging (${changesCount.total} alteraç${changesCount.total === 1 ? 'ão' : 'ões'} pendente${changesCount.total === 1 ? '' : 's'})`, {
      style: {
        background: '#dcfce7',
        border: '1px solid #86efac',
        color: '#166534',
      },
    });
    setIsSaving(false);
  };

  const handleApply = () => {
    // Parse string values to numbers
    const minReplicasNum = parseInt(minReplicas) || 0;
    const maxReplicasNum = parseInt(maxReplicas) || 1;
    const targetCPUNum = targetCPU ? parseInt(targetCPU) : undefined;
    const targetMemoryNum = targetMemory ? parseInt(targetMemory) : undefined;

    // Validate using comprehensive validation
    const validationResult = validateHPAUpdate({
      minReplicas: minReplicasNum,
      maxReplicas: maxReplicasNum,
      targetCPU: targetCPUNum,
      targetMemory: targetMemoryNum,
      cpuRequest: targetCpuRequest,
      memoryRequest: targetMemoryRequest,
      cpuLimit: targetCpuLimit,
      memoryLimit: targetMemoryLimit,
    });

    if (!validationResult.valid) {
      // Build error map for visual feedback
      const errorMap: Record<string, string> = {};
      validationResult.errors.forEach(err => {
        errorMap[err.field] = err.message;
      });
      setValidationErrors(errorMap);

      toast.error("Erro de validação", {
        description: formatValidationErrors(validationResult.errors),
        style: {
          background: '#fee2e2',
          border: '1px solid #fca5a5',
          color: '#991b1b',
        },
      });
      return;
    }

    // Clear validation errors on success
    setValidationErrors({});

    // Create modified HPA
    const modifiedHPA: HPA = {
      ...hpa,
      min_replicas: minReplicasNum,
      max_replicas: maxReplicasNum,
      target_cpu: targetCPUNum ?? null,
      target_memory: targetMemoryNum ?? null,
      target_cpu_request: targetCpuRequest || undefined,
      target_cpu_limit: targetCpuLimit || undefined,
      target_memory_request: targetMemoryRequest || undefined,
      target_memory_limit: targetMemoryLimit || undefined,
      perform_rollout: performRollout,
      perform_daemonset_rollout: performDaemonSetRollout,
      perform_statefulset_rollout: performStatefulSetRollout,
    };

    // 🔧 FIX: Reconstruir valores originais do cluster a partir de original_values
    // Isso garante que o modal de confirmação sempre compare com o estado atual do cluster,
    // independente de já ter sido salvo no staging antes

    const actualValuesHPA: HPA = {
      ...hpa,
      // Se original_values existe, usar ele; senão, usar valores atuais do HPA (fallback)
      min_replicas: hpa.original_values?.min_replicas ?? hpa.min_replicas,
      max_replicas: hpa.original_values?.max_replicas ?? hpa.max_replicas,
      target_cpu: hpa.original_values?.target_cpu ?? hpa.target_cpu,
      target_memory: hpa.original_values?.target_memory ?? hpa.target_memory,
      target_cpu_request: hpa.original_values?.cpu_request ?? hpa.target_cpu_request,
      target_cpu_limit: hpa.original_values?.cpu_limit ?? hpa.target_cpu_limit,
      target_memory_request: hpa.original_values?.memory_request ?? hpa.target_memory_request,
      target_memory_limit: hpa.original_values?.memory_limit ?? hpa.target_memory_limit,
      perform_rollout: hpa.original_values?.perform_rollout ?? false,
      perform_daemonset_rollout: hpa.original_values?.perform_daemonset_rollout ?? false,
      perform_statefulset_rollout: hpa.original_values?.perform_statefulset_rollout ?? false,
    };

    // Call the callback to open modal with correct original values
    if (onApply) {
      onApply(modifiedHPA, actualValuesHPA);
    }
  };

  const handleReset = () => {
    // 🔧 FIX: Reset to staged values if exists, otherwise to original props
    const stagedHPA = staging.stagedHPAs.find(
      h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
    );
    const baseHPA = stagedHPA || hpa;

    setMinReplicas(String(baseHPA.min_replicas ?? 0));
    setMaxReplicas(String(baseHPA.max_replicas ?? 1));
    setTargetCPU(baseHPA.target_cpu !== null && baseHPA.target_cpu !== undefined ? String(baseHPA.target_cpu) : "");
    setTargetMemory(baseHPA.target_memory !== null && baseHPA.target_memory !== undefined ? String(baseHPA.target_memory) : "");
    setTargetCpuRequest(baseHPA.target_cpu_request ?? baseHPA.original_values?.cpu_request ?? "");
    setTargetCpuLimit(baseHPA.target_cpu_limit ?? baseHPA.original_values?.cpu_limit ?? "");
    setTargetMemoryRequest(baseHPA.target_memory_request ?? baseHPA.original_values?.memory_request ?? "");
    setTargetMemoryLimit(baseHPA.target_memory_limit ?? baseHPA.original_values?.memory_limit ?? "");
    setPerformRollout(baseHPA.perform_rollout ?? false);
    setPerformDaemonSetRollout(baseHPA.perform_daemonset_rollout ?? false);
    setPerformStatefulSetRollout(baseHPA.perform_statefulset_rollout ?? false);
    toast.info("Alterações descartadas", {
      style: {
        background: '#dbeafe',
        border: '1px solid #93c5fd',
        color: '#1e40af',
      },
    });
  };

  // 🔧 FIX: Use staged HPA as base for comparison if it exists
  const stagedHPA = staging.stagedHPAs.find(
    h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
  );
  const baseHPA = stagedHPA || hpa;

  const isModified =
    (parseInt(minReplicas) || 0) !== (baseHPA.min_replicas ?? 0) ||
    (parseInt(maxReplicas) || 1) !== (baseHPA.max_replicas ?? 1) ||
    (targetCPU ? parseInt(targetCPU) : undefined) !== (baseHPA.target_cpu ?? undefined) ||
    (targetMemory ? parseInt(targetMemory) : undefined) !== (baseHPA.target_memory ?? undefined) ||
    targetCpuRequest !== (baseHPA.target_cpu_request ?? baseHPA.original_values?.cpu_request ?? "") ||
    targetCpuLimit !== (baseHPA.target_cpu_limit ?? baseHPA.original_values?.cpu_limit ?? "") ||
    targetMemoryRequest !== (baseHPA.target_memory_request ?? baseHPA.original_values?.memory_request ?? "") ||
    targetMemoryLimit !== (baseHPA.target_memory_limit ?? baseHPA.original_values?.memory_limit ?? "") ||
    performRollout !== (baseHPA.perform_rollout ?? false) ||
    performDaemonSetRollout !== (baseHPA.perform_daemonset_rollout ?? false) ||
    performStatefulSetRollout !== (baseHPA.perform_statefulset_rollout ?? false);

  // Keyboard shortcuts
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey) {
      if (e.key === 's' || e.key === 'S') {
        e.preventDefault();
        if (e.shiftKey) {
          // Ctrl+Shift+S: Apply directly
          if (isModified && !isSaving) {
            handleApply();
          }
        } else {
          // Ctrl+S: Save to staging
          if (isModified && !isSaving) {
            handleSave();
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

  // Staging direto — sem exigir modificações
  const isInStaging = staging.stagedHPAs.some(
    h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
  );

  const handleToggleStaging = (checked: boolean) => {
    if (checked) {
      staging.addHPAToStaging(hpa);
      const total = staging.getChangesCount().total + 1;
      toast.success(`${hpa.name} incluído no staging`, {
        description: `${total} HPA${total !== 1 ? "s" : ""} no staging`,
      });
    } else {
      staging.removeHPAFromStaging(hpa.cluster, hpa.namespace, hpa.name);
      toast.info(`${hpa.name} removido do staging`);
    }
  };

  return (
    <div className="space-y-4 animate-fade-in" onKeyDown={handleKeyDown} tabIndex={-1}>
      {/* Header */}
      <div className="space-y-1">
        <h4 className="font-semibold text-base text-foreground">{hpa.name}</h4>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{hpa.namespace}</span>
          {hpa.image_version && (
            <>
              <span>•</span>
              <code className="bg-muted px-2 py-0.5 rounded text-xs">
                {hpa.image_version}
              </code>
            </>
          )}
        </div>

        {/* Checkbox: inclui no staging sem alterar valores */}
        <div className="flex items-center gap-2 pt-1">
          <Checkbox
            id={`staging-${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
            checked={isInStaging}
            onCheckedChange={handleToggleStaging}
          />
          <label
            htmlFor={`staging-${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
            className="text-xs cursor-pointer select-none text-muted-foreground"
          >
            Incluir no staging com valores atuais
          </label>
          {isInStaging && (
            <span className="text-xs font-medium text-emerald-500">● no staging</span>
          )}
        </div>

        {isModified && (
          <p className="text-xs text-warning">⚠️ Modificado (não aplicado)</p>
        )}
      </div>

      {/* Form Fields */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="minReplicas" className="text-sm">Minimum Replicas</Label>
          <Input
            ref={minReplicasRef}
            id="minReplicas"
            type="text"
            value={minReplicas}
            onChange={(e) => {
              const val = e.target.value;
              // Allow empty or digits only
              if (val === "" || /^\d+$/.test(val)) {
                setMinReplicas(val);
                // Clear validation error when user types
                if (validationErrors.min_replicas) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.min_replicas;
                  setValidationErrors(newErrors);
                }
              }
            }}
            onClick={() => minReplicasRef.current?.select()}
            onFocus={() => minReplicasRef.current?.select()}
            className={`bg-background h-9 ${validationErrors.min_replicas ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
          />
          {validationErrors.min_replicas && (
            <p className="text-xs text-red-500 flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              {validationErrors.min_replicas}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="maxReplicas" className="text-sm">Maximum Replicas</Label>
          <Input
            ref={maxReplicasRef}
            id="maxReplicas"
            type="text"
            value={maxReplicas}
            onChange={(e) => {
              const val = e.target.value;
              // Allow empty or digits only
              if (val === "" || /^\d+$/.test(val)) {
                setMaxReplicas(val);
                // Clear validation error when user types
                if (validationErrors.max_replicas) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.max_replicas;
                  setValidationErrors(newErrors);
                }
              }
            }}
            onClick={() => maxReplicasRef.current?.select()}
            onFocus={() => maxReplicasRef.current?.select()}
            className={`bg-background h-9 ${validationErrors.max_replicas ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
          />
          {validationErrors.max_replicas && (
            <p className="text-xs text-red-500 flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              {validationErrors.max_replicas}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="targetCPU" className="text-sm">Target CPU (%)</Label>
          <Input
            ref={targetCPURef}
            id="targetCPU"
            type="text"
            value={targetCPU}
            onChange={(e) => {
              const val = e.target.value;
              // Allow empty or digits only (no default on empty for optional fields)
              if (val === "" || /^\d+$/.test(val)) {
                setTargetCPU(val);
                // Clear validation error when user types
                if (validationErrors.target_cpu) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.target_cpu;
                  setValidationErrors(newErrors);
                }
              }
            }}
            onClick={() => targetCPURef.current?.select()}
            onFocus={() => targetCPURef.current?.select()}
            placeholder="Não configurado"
            className={`bg-background h-9 ${validationErrors.target_cpu ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
          />
          {validationErrors.target_cpu && (
            <p className="text-xs text-red-500 flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              {validationErrors.target_cpu}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="targetMemory" className="text-sm">Target Memory (%)</Label>
          <Input
            ref={targetMemoryRef}
            id="targetMemory"
            type="text"
            value={targetMemory}
            onChange={(e) => {
              const val = e.target.value;
              // Allow empty or digits only (no default on empty for optional fields)
              if (val === "" || /^\d+$/.test(val)) {
                setTargetMemory(val);
                // Clear validation error when user types
                if (validationErrors.target_memory) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.target_memory;
                  setValidationErrors(newErrors);
                }
              }
            }}
            onClick={() => targetMemoryRef.current?.select()}
            onFocus={() => targetMemoryRef.current?.select()}
            placeholder="Não configurado"
            className={`bg-background h-9 ${validationErrors.target_memory ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
          />
          {validationErrors.target_memory && (
            <p className="text-xs text-red-500 flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              {validationErrors.target_memory}
            </p>
          )}
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
              onChange={(e) => {
                setTargetCpuRequest(e.target.value);
                // Clear validation error when user types
                if (validationErrors.cpu_request) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.cpu_request;
                  setValidationErrors(newErrors);
                }
              }}
              placeholder="ex: 100m, 0.5, 1"
              className={`bg-background h-9 ${validationErrors.cpu_request ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
            />
            {validationErrors.cpu_request && (
              <p className="text-xs text-red-500 flex items-center gap-1">
                <AlertCircle className="w-3 h-3" />
                {validationErrors.cpu_request}
              </p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cpuLimit" className="text-sm">CPU Limit</Label>
            <Input
              id="cpuLimit"
              type="text"
              value={targetCpuLimit}
              onChange={(e) => {
                setTargetCpuLimit(e.target.value);
                // Clear validation error when user types
                if (validationErrors.cpu_limit) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.cpu_limit;
                  setValidationErrors(newErrors);
                }
              }}
              placeholder="ex: 200m, 1, 2"
              className={`bg-background h-9 ${validationErrors.cpu_limit ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
            />
            {validationErrors.cpu_limit && (
              <p className="text-xs text-red-500 flex items-center gap-1">
                <AlertCircle className="w-3 h-3" />
                {validationErrors.cpu_limit}
              </p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="memoryRequest" className="text-sm">Memory Request</Label>
            <Input
              id="memoryRequest"
              type="text"
              value={targetMemoryRequest}
              onChange={(e) => {
                setTargetMemoryRequest(e.target.value);
                // Clear validation error when user types
                if (validationErrors.memory_request) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.memory_request;
                  setValidationErrors(newErrors);
                }
              }}
              placeholder="ex: 128Mi, 256Mi, 1Gi"
              className={`bg-background h-9 ${validationErrors.memory_request ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
            />
            {validationErrors.memory_request && (
              <p className="text-xs text-red-500 flex items-center gap-1">
                <AlertCircle className="w-3 h-3" />
                {validationErrors.memory_request}
              </p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="memoryLimit" className="text-sm">Memory Limit</Label>
            <Input
              id="memoryLimit"
              type="text"
              value={targetMemoryLimit}
              onChange={(e) => {
                setTargetMemoryLimit(e.target.value);
                // Clear validation error when user types
                if (validationErrors.memory_limit) {
                  const newErrors = { ...validationErrors };
                  delete newErrors.memory_limit;
                  setValidationErrors(newErrors);
                }
              }}
              placeholder="ex: 512Mi, 1Gi, 2Gi"
              className={`bg-background h-9 ${validationErrors.memory_limit ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
            />
            {validationErrors.memory_limit && (
              <p className="text-xs text-red-500 flex items-center gap-1">
                <AlertCircle className="w-3 h-3" />
                {validationErrors.memory_limit}
              </p>
            )}
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
        {isHPAProblematic() && hpa && (
          <AITriggerButton
            resourceType="HPA"
            cluster={hpa.cluster}
            namespace={hpa.namespace}
            resourceName={hpa.name}
            size="sm"
            variant="outline"
            className="flex-1"
          />
        )}
        <ProtectedAction allowed={canWrite}>
          <Button
            onClick={handleSave}
            disabled={!isModified || isSaving}
            className="flex-1 bg-gradient-primary h-9"
          >
            <Save className="w-4 h-4 mr-2" />
            {isSaving ? "Salvando..." : "💾 Salvar (Staging)"}
          </Button>
        </ProtectedAction>

        <ProtectedAction allowed={canWrite}>
          <Button
            onClick={handleApply}
            variant="default"
            className="flex-1 bg-success hover:bg-success/90 h-9"
          >
            <CheckCircle className="w-4 h-4 mr-2" />
            ✅ Aplicar Agora
          </Button>
        </ProtectedAction>

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
