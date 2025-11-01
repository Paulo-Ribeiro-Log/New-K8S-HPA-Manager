import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Loader2, Clock, Play, Pause } from "lucide-react";
import type { CronJob } from "@/lib/api/types";
import { useUpdateCronJob } from "@/hooks/useAPI";
import { toast } from "sonner";

interface CronJobEditorProps {
  cronJob: CronJob | null;
  selectedCluster: string;
  onRefetch: () => void;
}

export const CronJobEditor = ({ cronJob, selectedCluster, onRefetch }: CronJobEditorProps) => {
  const [isApplying, setIsApplying] = useState(false);
  const updateCronJobMutation = useUpdateCronJob();

  if (!cronJob) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
        <Clock className="w-16 h-16 mb-4 opacity-20" />
        <p className="text-sm">Selecione um CronJob para editar</p>
      </div>
    );
  }

  const handleToggleSuspend = async (suspend: boolean) => {
    setIsApplying(true);

    try {
      await updateCronJobMutation.mutateAsync({
        cluster: selectedCluster,
        namespace: cronJob.namespace,
        name: cronJob.name,
        data: { suspend }
      });

      toast.success(
        suspend ? "CronJob suspenso com sucesso" : "CronJob ativado com sucesso",
        {
          description: `${cronJob.namespace}/${cronJob.name}`
        }
      );

      // Recarregar dados para refletir novo estado
      setTimeout(() => {
        onRefetch();
      }, 500);
    } catch (error) {
      console.error("Error updating CronJob:", error);
      toast.error("Erro ao atualizar CronJob", {
        description: error instanceof Error ? error.message : "Erro desconhecido"
      });
    } finally {
      setIsApplying(false);
    }
  };

  const isSuspended = cronJob.suspend === true;

  return (
    <div className="space-y-6">
      {/* CronJob Info */}
      <div className="space-y-4">
        <div>
          <h3 className="text-lg font-semibold text-foreground mb-1">{cronJob.name}</h3>
          <p className="text-sm text-muted-foreground">{cronJob.namespace}</p>
        </div>

        {/* Schedule */}
        <div className="p-4 bg-muted/30 rounded-lg">
          <div className="flex items-center gap-2 mb-2">
            <Clock className="w-4 h-4 text-muted-foreground" />
            <Label className="text-xs text-muted-foreground">Schedule</Label>
          </div>
          <p className="font-mono text-sm font-semibold">{cronJob.schedule}</p>
          <p className="text-xs text-muted-foreground mt-1">{cronJob.schedule_description}</p>
        </div>

        {/* Status Info */}
        <div className="grid grid-cols-3 gap-3">
          <div className="p-3 bg-muted/30 rounded-lg">
            <p className="text-xs text-muted-foreground mb-1">Jobs Ativos</p>
            <p className="text-lg font-bold text-foreground">{cronJob.active_jobs}</p>
          </div>
          <div className="p-3 bg-green-50 dark:bg-green-950/20 rounded-lg">
            <p className="text-xs text-muted-foreground mb-1">Sucessos</p>
            <p className="text-lg font-bold text-green-600">{cronJob.successful_jobs}</p>
          </div>
          <div className="p-3 bg-red-50 dark:bg-red-950/20 rounded-lg">
            <p className="text-xs text-muted-foreground mb-1">Falhas</p>
            <p className="text-lg font-bold text-red-600">{cronJob.failed_jobs}</p>
          </div>
        </div>

        {/* Last Schedule Time */}
        {cronJob.last_schedule_time && (
          <div className="p-3 bg-muted/30 rounded-lg">
            <p className="text-xs text-muted-foreground mb-1">Última Execução</p>
            <p className="text-sm font-medium">
              {new Date(cronJob.last_schedule_time).toLocaleString('pt-BR')}
            </p>
          </div>
        )}

        {/* Suspend Control - Compacto */}
        <div className="grid grid-cols-2 gap-3">
          <Button
            variant={isSuspended ? "default" : "outline"}
            onClick={() => handleToggleSuspend(false)}
            disabled={isApplying || !isSuspended}
            className="w-full"
          >
            {isApplying && !isSuspended ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Play className="w-4 h-4 mr-2" />
            )}
            Ativar
          </Button>
          <Button
            variant={isSuspended ? "outline" : "destructive"}
            onClick={() => handleToggleSuspend(true)}
            disabled={isApplying || isSuspended}
            className="w-full"
          >
            {isApplying && isSuspended ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Pause className="w-4 h-4 mr-2" />
            )}
            Suspender
          </Button>
        </div>
      </div>
    </div>
  );
};
