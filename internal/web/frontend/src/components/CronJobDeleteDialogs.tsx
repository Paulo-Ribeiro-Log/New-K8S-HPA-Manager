import { useCallback, useEffect, useState } from "react";
import { AlertTriangle, CheckCircle2, Loader2, Play, RefreshCcw, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { apiClient, type CronJobDeletePreview, type CronJobJobInfo } from "@/lib/api/client";
import { formatAge } from "@/lib/monitorUtils";
import { ProtectedAction } from "@/components/rbac";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

// Exclusão de CronJob e de Jobs na aba CronJobs. As proteções de verdade ficam no backend
// (precondition de UID: só apaga o objeto mostrado na prévia); aqui ficam as confirmações.

function usePreview(cluster: string, namespace: string, name: string) {
  const [preview, setPreview] = useState<CronJobDeletePreview | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setPreview(await apiClient.getCronJobDeletePreview(cluster, namespace, name));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [cluster, namespace, name]);
  useEffect(() => { load(); }, [load]);
  return { preview, error, loading, reload: load };
}

function JobStatusIcon({ status }: { status: CronJobJobInfo["status"] }) {
  if (status === "Running") return <Play className="w-3.5 h-3.5 text-blue-500" />;
  if (status === "Failed") return <XCircle className="w-3.5 h-3.5 text-red-500" />;
  return <CheckCircle2 className="w-3.5 h-3.5 text-green-500" />;
}

function ManagedByWarning({ managedBy, what }: { managedBy?: string; what: string }) {
  if (!managedBy) return null;
  return (
    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs space-y-1">
      <p className="font-semibold text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
        <AlertTriangle className="w-3.5 h-3.5" />
        Gerenciado por {managedBy}
      </p>
      <p className="text-muted-foreground">
        Apagar aqui não é definitivo: o {what} volta no próximo deploy/sync. Para removê-lo de vez, tire-o do chart/repositório de origem.
      </p>
    </div>
  );
}

// ── Deletar CronJob ──────────────────────────────────────────────────────────

export function DeleteCronJobDialog({ cluster, namespace, name, onClose, onDeleted }: {
  cluster: string; namespace: string; name: string; onClose: () => void; onDeleted: () => void;
}) {
  const { preview, error, loading, reload } = usePreview(cluster, namespace, name);
  const [jobsChoice, setJobsChoice] = useState<"" | "delete" | "keep">("");
  const [confirmText, setConfirmText] = useState("");
  const [deleting, setDeleting] = useState(false);

  const jobsCount = preview?.jobs.length ?? 0;
  // Sem Jobs não há o que escolher; com Jobs, a escolha é obrigatória (sem padrão pré-marcado).
  const choiceOk = jobsCount === 0 || jobsChoice !== "";
  const canDelete = !!preview && choiceOk && confirmText === name && !deleting;

  const doDelete = async () => {
    if (!preview) return;
    setDeleting(true);
    try {
      await apiClient.deleteCronJob(cluster, namespace, name, preview.uid, jobsCount === 0 ? "delete" : (jobsChoice as "delete" | "keep"));
      toast.success(`CronJob ${namespace}/${name} apagado`, {
        description: jobsCount === 0 ? undefined : jobsChoice === "delete" ? `${jobsCount} Job(s) e seus pods também` : `${jobsCount} Job(s) mantidos (órfãos)`,
      });
      onDeleted();
      onClose();
    } catch (e) {
      toast.error("Erro ao apagar CronJob", { description: e instanceof Error ? e.message : String(e) });
      reload(); // o objeto pode ter mudado: atualiza a prévia
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !deleting && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-destructive">
            <Trash2 className="w-5 h-5" />
            Deletar CronJob
          </DialogTitle>
          <DialogDescription className="font-mono break-all">{namespace}/{name} • {cluster}</DialogDescription>
        </DialogHeader>

        {loading && !preview ? (
          <div className="flex items-center justify-center py-8"><Loader2 className="w-5 h-5 animate-spin" /></div>
        ) : error ? (
          <p className="text-sm text-red-500">{error}</p>
        ) : preview && (
          <div className="space-y-3 text-sm">
            <p className="text-xs text-muted-foreground">
              Schedule <span className="font-mono text-foreground">{preview.schedule}</span>
              {preview.suspended && <span className="ml-1">(suspenso)</span>}. Esta ação não pode ser desfeita pela tela — o manifesto fica salvo no histórico para recriar, se preciso.
            </p>

            <ManagedByWarning managedBy={preview.managed_by} what="CronJob" />

            {jobsCount > 0 ? (
              <div className="rounded-md border border-border/60 p-3 space-y-2">
                <p className="text-xs">
                  O CronJob tem <strong>{jobsCount} Job(s)</strong> no histórico
                  {preview.active_jobs > 0 && <> — <strong className="text-blue-500">{preview.active_jobs} rodando agora</strong></>}.
                  O que fazer com eles?
                </p>
                <RadioGroup value={jobsChoice} onValueChange={(v) => setJobsChoice(v as "delete" | "keep")} className="gap-2">
                  <label className="flex items-start gap-2 text-xs cursor-pointer">
                    <RadioGroupItem value="delete" className="mt-0.5" />
                    <span>
                      <span className="font-medium">Apagar junto</span> — remove os Jobs e os pods deles
                      {preview.active_jobs > 0 && <span className="text-red-500"> (interrompe o que está rodando)</span>}.
                    </span>
                  </label>
                  <label className="flex items-start gap-2 text-xs cursor-pointer">
                    <RadioGroupItem value="keep" className="mt-0.5" />
                    <span>
                      <span className="font-medium">Manter os Jobs</span> — ficam órfãos no namespace
                      {preview.active_jobs > 0 && " (os que estão rodando terminam normalmente)"}; apague-os depois, se quiser.
                    </span>
                  </label>
                </RadioGroup>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">Nenhum Job no histórico deste CronJob.</p>
            )}

            <div>
              <Label className="text-xs">Digite o nome do CronJob para confirmar</Label>
              <Input value={confirmText} onChange={(e) => setConfirmText(e.target.value)} placeholder={name} className="font-mono h-8 mt-1" autoFocus />
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={deleting}>Cancelar</Button>
          <ProtectedAction>
            <Button variant="destructive" onClick={doDelete} disabled={!canDelete} title={!choiceOk ? "Escolha o que fazer com os Jobs" : confirmText !== name ? "Digite o nome do CronJob" : undefined}>
              {deleting ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Trash2 className="w-4 h-4 mr-2" />}
              Deletar CronJob
            </Button>
          </ProtectedAction>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Jobs do CronJob (listar e apagar) ───────────────────────────────────────────

export function CronJobJobsDialog({ cluster, namespace, name, onClose, onChanged }: {
  cluster: string; namespace: string; name: string; onClose: () => void; onChanged: () => void;
}) {
  const { preview, error, loading, reload } = usePreview(cluster, namespace, name);
  const [confirming, setConfirming] = useState<string | null>(null); // nome do Job em confirmação
  const [interruptAck, setInterruptAck] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const startConfirm = (job: string) => { setConfirming(job); setInterruptAck(false); };

  const doDelete = async (job: CronJobJobInfo) => {
    setDeleting(true);
    try {
      await apiClient.deleteJob(cluster, namespace, job.name, job.uid);
      toast.success(`Job ${job.name} apagado`, { description: job.status === "Running" ? "Os pods em execução foram interrompidos." : undefined });
      setConfirming(null);
      onChanged();
    } catch (e) {
      toast.error("Erro ao apagar Job", { description: e instanceof Error ? e.message : String(e) });
    } finally {
      setDeleting(false);
      reload();
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !deleting && onClose()}>
      <DialogContent className="max-w-3xl max-h-[85vh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            Jobs do CronJob
            <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={reload} disabled={loading} title="Atualizar">
              <RefreshCcw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </DialogTitle>
          <DialogDescription className="font-mono break-all">{namespace}/{name} • {cluster}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-auto space-y-2">
          {preview && <ManagedByWarning managedBy={preview.jobs.find((j) => j.managed_by)?.managed_by} what="Job" />}
          {loading && !preview ? (
            <div className="flex items-center justify-center py-8"><Loader2 className="w-5 h-5 animate-spin" /></div>
          ) : error ? (
            <p className="text-sm text-red-500">{error}</p>
          ) : preview && preview.jobs.length === 0 ? (
            <p className="text-sm text-muted-foreground py-6 text-center">Nenhum Job no histórico deste CronJob.</p>
          ) : preview && (
            <div className="border border-border/60 rounded-md divide-y divide-border/50 text-xs">
              {preview.jobs.map((j) => (
                <div key={j.uid} className="px-3 py-2">
                  <div className="flex items-center gap-3">
                    <JobStatusIcon status={j.status} />
                    <span className="font-mono flex-1 min-w-0 truncate" title={j.name}>{j.name}</span>
                    <span className="text-muted-foreground whitespace-nowrap" title={j.start_time ? new Date(j.start_time).toLocaleString("pt-BR") : undefined}>
                      {j.start_time ? `há ${formatAge(j.start_time)}` : "—"}
                    </span>
                    <span className="text-muted-foreground whitespace-nowrap w-16 text-right" title="Duração (até agora, se rodando)">
                      {j.duration_seconds < 60 ? `${j.duration_seconds}s` : formatAge(new Date(Date.now() - j.duration_seconds * 1000))}
                    </span>
                    <span className={`whitespace-nowrap w-20 ${j.status === "Running" ? "text-blue-500" : j.status === "Failed" ? "text-red-500" : "text-green-600"}`}>
                      {j.status === "Running" ? `rodando (${j.active_pods})` : j.status === "Failed" ? "falhou" : "sucesso"}
                    </span>
                    <ProtectedAction showWarning={false}>
                      <Button variant="ghost" size="sm" className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive" onClick={() => startConfirm(j.name)} disabled={deleting} title="Apagar este Job">
                        <Trash2 className="w-3.5 h-3.5" />
                      </Button>
                    </ProtectedAction>
                  </div>
                  {confirming === j.name && (
                    <div className="mt-2 ml-6 rounded-md border border-destructive/40 bg-destructive/5 p-2 space-y-2">
                      <p>Apagar o Job <span className="font-mono">{j.name}</span> e os pods dele?</p>
                      {j.status === "Running" && (
                        <label className="flex items-center gap-2 cursor-pointer text-red-600 dark:text-red-400">
                          <Checkbox checked={interruptAck} onCheckedChange={(v) => setInterruptAck(v === true)} className="w-3.5 h-3.5" />
                          Está rodando ({j.active_pods} pod(s)): entendo que a execução será interrompida
                        </label>
                      )}
                      <div className="flex justify-end gap-2">
                        <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={() => setConfirming(null)} disabled={deleting}>Cancelar</Button>
                        <Button variant="destructive" size="sm" className="h-6 text-xs" onClick={() => doDelete(j)} disabled={deleting || (j.status === "Running" && !interruptAck)}>
                          {deleting ? <Loader2 className="w-3 h-3 mr-1 animate-spin" /> : <Trash2 className="w-3 h-3 mr-1" />}
                          Apagar Job
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={onClose} disabled={deleting}>Fechar</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
