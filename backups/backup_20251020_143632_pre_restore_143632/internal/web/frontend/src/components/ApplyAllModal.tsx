import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Progress } from "@/components/ui/progress";
import { CheckCircle, XCircle, ArrowRight, Loader2, RefreshCw } from "lucide-react";
import type { HPA } from "@/lib/api/types";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface ApplyAllModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modifiedHPAs: Array<{ key: string; current: HPA; original: HPA }>;
  onApplied: () => void;
  onClear: () => void;
}

interface RolloutProgress {
  type: "deployment" | "daemonset" | "statefulset";
  status: "pending" | "in_progress" | "completed" | "failed";
  progress: number; // 0-100
  message?: string;
}

interface ApplyResult {
  key: string;
  hpa: HPA;
  success: boolean;
  error?: string;
  rollouts?: RolloutProgress[];
}

export const ApplyAllModal = ({
  open,
  onOpenChange,
  modifiedHPAs,
  onApplied,
  onClear,
}: ApplyAllModalProps) => {
  const [isApplying, setIsApplying] = useState(false);
  const [results, setResults] = useState<ApplyResult[]>([]);
  const [showResults, setShowResults] = useState(false);

  const simulateRolloutProgress = async (
    key: string,
    rolloutType: "deployment" | "daemonset" | "statefulset"
  ): Promise<void> => {
    return new Promise((resolve) => {
      let progress = 0;
      const interval = setInterval(() => {
        progress += 10;

        setResults((prev) =>
          prev.map((r) =>
            r.key === key
              ? {
                  ...r,
                  rollouts: r.rollouts?.map((ro) =>
                    ro.type === rolloutType
                      ? {
                          ...ro,
                          status: progress >= 100 ? "completed" : "in_progress",
                          progress,
                          message:
                            progress >= 100
                              ? "Rollout concluído"
                              : `Reiniciando pods... ${progress}%`,
                        }
                      : ro
                  ),
                }
              : r
          )
        );

        if (progress >= 100) {
          clearInterval(interval);
          resolve();
        }
      }, 300);
    });
  };

  const handleApplyAll = async () => {
    setIsApplying(true);
    setShowResults(true);
    const newResults: ApplyResult[] = [];

    for (const { key, current, original } of modifiedHPAs) {
      try {
        // Build update payload
        const updates: any = {};

        // HPA config changes
        if (current.min_replicas !== original.min_replicas) {
          updates.min_replicas = current.min_replicas;
        }
        if (current.max_replicas !== original.max_replicas) {
          updates.max_replicas = current.max_replicas;
        }
        if (current.target_cpu !== original.target_cpu) {
          updates.target_cpu = current.target_cpu;
        }
        if (current.target_memory !== original.target_memory) {
          updates.target_memory = current.target_memory;
        }

        // Resource changes
        if (current.target_cpu_request !== original.target_cpu_request) {
          updates.target_cpu_request = current.target_cpu_request;
        }
        if (current.target_cpu_limit !== original.target_cpu_limit) {
          updates.target_cpu_limit = current.target_cpu_limit;
        }
        if (current.target_memory_request !== original.target_memory_request) {
          updates.target_memory_request = current.target_memory_request;
        }
        if (current.target_memory_limit !== original.target_memory_limit) {
          updates.target_memory_limit = current.target_memory_limit;
        }

        // Rollout options
        updates.perform_rollout = current.perform_rollout || false;
        updates.perform_daemonset_rollout = current.perform_daemonset_rollout || false;
        updates.perform_statefulset_rollout = current.perform_statefulset_rollout || false;

        // Initialize rollouts array
        const rollouts: RolloutProgress[] = [];
        if (updates.perform_rollout) {
          rollouts.push({
            type: "deployment",
            status: "pending",
            progress: 0,
            message: "Aguardando início...",
          });
        }
        if (updates.perform_daemonset_rollout) {
          rollouts.push({
            type: "daemonset",
            status: "pending",
            progress: 0,
            message: "Aguardando início...",
          });
        }
        if (updates.perform_statefulset_rollout) {
          rollouts.push({
            type: "statefulset",
            status: "pending",
            progress: 0,
            message: "Aguardando início...",
          });
        }

        // Add to results with pending rollouts
        const result: ApplyResult = {
          key,
          hpa: current,
          success: true,
          rollouts: rollouts.length > 0 ? rollouts : undefined,
        };
        newResults.push(result);
        setResults([...newResults]);

        // Apply HPA changes - Send complete HPA object instead of partial updates
        // The backend expects a full HPA object via c.ShouldBindJSON(&hpa)
        await apiClient.updateHPA(
          current.cluster,
          current.namespace,
          current.name,
          current
        );

        // Simulate rollout progress for each type
        if (updates.perform_rollout) {
          await simulateRolloutProgress(key, "deployment");
        }
        if (updates.perform_daemonset_rollout) {
          await simulateRolloutProgress(key, "daemonset");
        }
        if (updates.perform_statefulset_rollout) {
          await simulateRolloutProgress(key, "statefulset");
        }
      } catch (error) {
        newResults.push({
          key,
          hpa: current,
          success: false,
          error: error instanceof Error ? error.message : "Unknown error",
        });
        setResults([...newResults]);
      }
    }

    setIsApplying(false);

    const successCount = newResults.filter((r) => r.success).length;
    const failCount = newResults.filter((r) => !r.success).length;

    console.log(`🔍 [ApplyAllModal] Results: success=${successCount}, fail=${failCount}`);
    console.log('🔍 [ApplyAllModal] Detailed results:', newResults);

    if (failCount === 0) {
      console.log('✅ [ApplyAllModal] All successful - calling onApplied()');
      toast.success(`✅ ${successCount} HPAs aplicados com sucesso`);
      onClear();
      onApplied();
    } else {
      console.log('❌ [ApplyAllModal] Some failures - NOT calling onApplied()');
      toast.error(`❌ ${failCount} HPAs falharam, ${successCount} aplicados`);
    }
    
    setTimeout(() => {
      onOpenChange(false);
      setShowResults(false);
      setResults([]);
    }, 2000);
  };

  const renderChange = (label: string, before: any, after: any) => {
    // Normalize null/undefined to null for comparison
    const normalizedBefore = before ?? null;
    const normalizedAfter = after ?? null;

    // Don't show if both are null/undefined (no real change)
    if (normalizedBefore === normalizedAfter) return null;

    // Don't show if both are empty/null (— → —)
    if ((normalizedBefore === null || normalizedBefore === "") &&
        (normalizedAfter === null || normalizedAfter === "")) {
      return null;
    }

    return (
      <div className="flex items-center gap-2 text-sm py-1">
        <span className="text-muted-foreground min-w-[140px]">{label}:</span>
        <span className="text-red-500 line-through">{before ?? "—"}</span>
        <ArrowRight className="w-4 h-4 text-muted-foreground" />
        <span className="text-green-500 font-medium">{after ?? "—"}</span>
      </div>
    );
  };

  const renderHPAChanges = (current: HPA, original: HPA) => {
    const changes = [];

    changes.push(renderChange("Min Replicas", original.min_replicas, current.min_replicas));
    changes.push(renderChange("Max Replicas", original.max_replicas, current.max_replicas));
    changes.push(
      renderChange("Target CPU (%)", original.target_cpu, current.target_cpu)
    );
    changes.push(
      renderChange("Target Memory (%)", original.target_memory, current.target_memory)
    );
    changes.push(
      renderChange("CPU Request", original.target_cpu_request, current.target_cpu_request)
    );
    changes.push(
      renderChange("CPU Limit", original.target_cpu_limit, current.target_cpu_limit)
    );
    changes.push(
      renderChange(
        "Memory Request",
        original.target_memory_request,
        current.target_memory_request
      )
    );
    changes.push(
      renderChange("Memory Limit", original.target_memory_limit, current.target_memory_limit)
    );

    return changes.filter((c) => c !== null);
  };

  const handleClose = () => {
    if (!isApplying) {
      onOpenChange(false);
      setShowResults(false);
      setResults([]);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-3xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle>
            {showResults ? "Resultados da Aplicação" : "Confirmar Alterações"}
          </DialogTitle>
          <DialogDescription>
            {showResults
              ? "Progresso da aplicação das alterações"
              : `${modifiedHPAs.length} HPA${modifiedHPAs.length > 1 ? "s" : ""} será${modifiedHPAs.length > 1 ? "ão" : ""} modificado${modifiedHPAs.length > 1 ? "s" : ""} no cluster`}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[50vh] pr-4">
          {!showResults ? (
            // Preview mode - show all changes
            <div className="space-y-4">
              {modifiedHPAs.map(({ key, current, original }) => {
                const changes = renderHPAChanges(current, original);
                if (changes.length === 0) return null;

                return (
                  <div key={key} className="border rounded-lg p-4 space-y-2">
                    <div className="flex items-center gap-2">
                      <h4 className="font-semibold text-base">{current.name}</h4>
                      <span className="text-xs text-muted-foreground">
                        {current.namespace}
                      </span>
                    </div>
                    <Separator />
                    <div className="space-y-1">{changes}</div>

                    {/* Rollout options */}
                    {(current.perform_rollout ||
                      current.perform_daemonset_rollout ||
                      current.perform_statefulset_rollout) && (
                      <>
                        <Separator />
                        <div className="text-sm text-muted-foreground">
                          <span className="font-medium">Rollouts:</span>
                          {current.perform_rollout && " 🔄 Deployment"}
                          {current.perform_daemonset_rollout && " 🔄 DaemonSet"}
                          {current.perform_statefulset_rollout && " 🔄 StatefulSet"}
                        </div>
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          ) : (
            // Results mode - show progress
            <div className="space-y-3">
              {modifiedHPAs.map(({ key, current }) => {
                const result = results.find((r) => r.key === key);
                const isPending = !result;
                const isSuccess = result?.success;
                const isError = result && !result.success;
                const hasRollouts = result?.rollouts && result.rollouts.length > 0;

                return (
                  <div
                    key={key}
                    className={`p-3 rounded-lg border ${
                      isSuccess
                        ? "bg-green-50 border-green-200"
                        : isError
                          ? "bg-red-50 border-red-200"
                          : "bg-muted"
                    }`}
                  >
                    <div className="flex items-center gap-3">
                      {isPending && (
                        <Loader2 className="w-5 h-5 text-blue-500 animate-spin flex-shrink-0" />
                      )}
                      {isSuccess && <CheckCircle className="w-5 h-5 text-green-500 flex-shrink-0" />}
                      {isError && <XCircle className="w-5 h-5 text-red-500 flex-shrink-0" />}

                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-sm truncate">
                          {current.namespace}/{current.name}
                        </div>
                        {isError && (
                          <div className="text-xs text-red-600 mt-1">
                            {result.error}
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Rollout Progress Bars */}
                    {hasRollouts && (
                      <div className="mt-3 space-y-2 pl-8">
                        {result.rollouts!.map((rollout, idx) => {
                          const icon =
                            rollout.type === "deployment"
                              ? "🚀"
                              : rollout.type === "daemonset"
                                ? "⚙️"
                                : "📦";
                          const label =
                            rollout.type === "deployment"
                              ? "Deployment"
                              : rollout.type === "daemonset"
                                ? "DaemonSet"
                                : "StatefulSet";

                          return (
                            <div key={idx} className="space-y-1">
                              <div className="flex items-center justify-between text-xs">
                                <span className="flex items-center gap-1.5">
                                  {rollout.status === "in_progress" && (
                                    <RefreshCw className="w-3 h-3 animate-spin" />
                                  )}
                                  {rollout.status === "completed" && (
                                    <CheckCircle className="w-3 h-3 text-green-600" />
                                  )}
                                  {rollout.status === "pending" && (
                                    <Loader2 className="w-3 h-3 text-gray-400" />
                                  )}
                                  <span className="font-medium">
                                    {icon} {label}
                                  </span>
                                </span>
                                <span className="text-muted-foreground">
                                  {rollout.message || `${rollout.progress}%`}
                                </span>
                              </div>
                              <Progress
                                value={rollout.progress}
                                className="h-2"
                              />
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </ScrollArea>

        <DialogFooter>
          {!showResults ? (
            <>
              <Button variant="outline" onClick={handleClose} disabled={isApplying}>
                Cancelar
              </Button>
              <Button
                onClick={handleApplyAll}
                disabled={isApplying}
                className="bg-success hover:bg-success/90"
              >
                {isApplying ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Aplicando...
                  </>
                ) : (
                  `✅ Aplicar ${modifiedHPAs.length} HPA${modifiedHPAs.length > 1 ? "s" : ""}`
                )}
              </Button>
            </>
          ) : (
            <Button onClick={handleClose} disabled={isApplying}>
              {isApplying ? "Aguarde..." : "Fechar"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
