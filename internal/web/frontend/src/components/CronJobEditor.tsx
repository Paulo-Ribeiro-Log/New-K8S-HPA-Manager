import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Loader2, Clock, Play, Pause, Edit2, Check, X, HelpCircle } from "lucide-react";
import type { CronJob } from "@/lib/api/types";
import { useUpdateCronJob } from "@/hooks/useAPI";
import { toast } from "sonner";
import { explainCronExpression, isValidCronExpression } from "@/lib/cronParser";

interface CronJobEditorProps {
  cronJob: CronJob | null;
  selectedCluster: string;
  onRefetch: () => void;
}

export const CronJobEditor = ({ cronJob, selectedCluster, onRefetch }: CronJobEditorProps) => {
  const [isApplying, setIsApplying] = useState(false);
  const [isEditingSchedule, setIsEditingSchedule] = useState(false);
  const [scheduleValue, setScheduleValue] = useState("");
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

  const handleEditSchedule = () => {
    setScheduleValue(cronJob.schedule);
    setIsEditingSchedule(true);
  };

  const handleSaveSchedule = async () => {
    if (!isValidCronExpression(scheduleValue)) {
      toast.error("Expressão cron inválida", {
        description: "Verifique o formato: minuto hora dia mês dia-da-semana"
      });
      return;
    }

    setIsApplying(true);

    try {
      await updateCronJobMutation.mutateAsync({
        cluster: selectedCluster,
        namespace: cronJob.namespace,
        name: cronJob.name,
        data: { schedule: scheduleValue }
      });

      toast.success("Schedule atualizado com sucesso", {
        description: `${cronJob.namespace}/${cronJob.name}`
      });

      setIsEditingSchedule(false);

      // Recarregar dados para refletir novo estado
      setTimeout(() => {
        onRefetch();
      }, 500);
    } catch (error) {
      console.error("Error updating schedule:", error);
      toast.error("Erro ao atualizar schedule", {
        description: error instanceof Error ? error.message : "Erro desconhecido"
      });
    } finally {
      setIsApplying(false);
    }
  };

  const handleCancelEdit = () => {
    setIsEditingSchedule(false);
    setScheduleValue("");
  };

  const isSuspended = cronJob.suspend === true;

  // Explicar o schedule atual
  const scheduleExplanation = explainCronExpression(cronJob.schedule);
  const editScheduleExplanation = scheduleValue ? explainCronExpression(scheduleValue) : null;

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
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <Clock className="w-4 h-4 text-muted-foreground" />
              <Label className="text-xs text-muted-foreground">Schedule</Label>
            </div>
            {!isEditingSchedule && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleEditSchedule}
                className="h-7 px-2"
              >
                <Edit2 className="w-3 h-3 mr-1" />
                Editar
              </Button>
            )}
          </div>

          {!isEditingSchedule ? (
            <>
              {/* Descrição legível em destaque */}
              <p className="text-base font-semibold text-foreground mb-2">
                {scheduleExplanation?.readable || cronJob.schedule}
              </p>
              {/* Expressão cron em fonte mono menor */}
              <p className="font-mono text-xs text-muted-foreground">
                {cronJob.schedule}
              </p>
            </>
          ) : (
            /* Modo de edição */
            <div className="space-y-3">
              <div>
                <Input
                  value={scheduleValue}
                  onChange={(e) => setScheduleValue(e.target.value)}
                  placeholder="Ex: 0 5 * * * (todos os dias às 05:00)"
                  className="font-mono"
                />

                {/* Preview da expressão */}
                {editScheduleExplanation && isValidCronExpression(scheduleValue) && (
                  <div className="mt-2 p-2 bg-green-50 dark:bg-green-950/20 rounded text-sm">
                    <p className="font-semibold text-green-700 dark:text-green-400">
                      {editScheduleExplanation.readable}
                    </p>
                  </div>
                )}

                {scheduleValue && !isValidCronExpression(scheduleValue) && (
                  <div className="mt-2 p-2 bg-red-50 dark:bg-red-950/20 rounded text-sm">
                    <p className="text-red-700 dark:text-red-400">
                      Expressão cron inválida
                    </p>
                  </div>
                )}
              </div>

              {/* Guia rápido */}
              <div className="p-2 bg-blue-50 dark:bg-blue-950/20 rounded">
                <div className="flex items-start gap-1 mb-1">
                  <HelpCircle className="w-3 h-3 text-blue-600 mt-0.5 flex-shrink-0" />
                  <p className="text-xs font-semibold text-blue-700 dark:text-blue-400">
                    Formato: minuto hora dia mês dia-da-semana
                  </p>
                </div>
                <div className="grid grid-cols-5 gap-1 text-[10px] text-blue-600 dark:text-blue-300 font-mono">
                  <div>0-59</div>
                  <div>0-23</div>
                  <div>1-31</div>
                  <div>1-12</div>
                  <div>0-6</div>
                </div>
                <p className="text-[10px] text-blue-600 dark:text-blue-300 mt-1">
                  Use * para "qualquer valor"
                </p>
              </div>

              {/* Botões de ação */}
              <div className="flex gap-2">
                <Button
                  variant="default"
                  size="sm"
                  onClick={handleSaveSchedule}
                  disabled={isApplying || !scheduleValue || !isValidCronExpression(scheduleValue)}
                  className="flex-1"
                >
                  {isApplying ? (
                    <Loader2 className="w-3 h-3 mr-1 animate-spin" />
                  ) : (
                    <Check className="w-3 h-3 mr-1" />
                  )}
                  Salvar
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleCancelEdit}
                  disabled={isApplying}
                  className="flex-1"
                >
                  <X className="w-3 h-3 mr-1" />
                  Cancelar
                </Button>
              </div>
            </div>
          )}
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
