import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Progress } from "@/components/ui/progress";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { AlertTriangle, CheckCircle2, Loader2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { PrometheusWALCleanupResult, PrometheusWALReport } from "@/lib/api/types";

interface PrometheusWalCleanupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  report: PrometheusWALReport;
  /** Chamado ao fechar depois de a limpeza terminar (com sucesso ou não). */
  onFinished: () => void;
}

type Phase = "confirm" | "running" | "done" | "failed";

interface ProgressEventPayload {
  type: string;
  message?: string;
  progress?: number;
  error?: string;
  result?: PrometheusWALCleanupResult;
}

const CONFIRM_WORD = "APAGAR";

const formatBytes = (b: number) => {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GiB`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(0)} MiB`;
  return `${Math.round(b / 1024)} KiB`;
};

export const PrometheusWalCleanupDialog = ({ open, onOpenChange, report, onFinished }: PrometheusWalCleanupDialogProps) => {
  const affected = report.affected_replicas ?? [];
  const [selected, setSelected] = useState<string[]>(affected);
  const [confirmWord, setConfirmWord] = useState("");
  const [confirmName, setConfirmName] = useState("");
  const [phase, setPhase] = useState<Phase>("confirm");
  const [starting, setStarting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [opId, setOpId] = useState<string | null>(null);
  const [lines, setLines] = useState<string[]>([]);
  const [progress, setProgress] = useState(0);
  const [result, setResult] = useState<PrometheusWALCleanupResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => () => sourceRef.current?.close(), []);

  const deepByPod = new Map((report.deep ?? []).map((d) => [d.pod, d]));
  const missingDeep = selected.filter((p) => !deepByPod.has(p));
  const canStart =
    phase === "confirm" &&
    !starting &&
    selected.length > 0 &&
    missingDeep.length === 0 &&
    confirmWord === CONFIRM_WORD &&
    confirmName === report.name;

  const watch = (id: string) => {
    const es = new EventSource(apiClient.getPrometheusWALProgressURL(id));
    sourceRef.current = es;
    es.onmessage = (ev) => {
      let e: ProgressEventPayload;
      try {
        e = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (e.message) setLines((l) => (l[l.length - 1] === e.message ? l : [...l, e.message!]));
      if (typeof e.progress === "number") setProgress(e.progress);
      if (e.result) setResult(e.result);
      if (e.type === "complete") {
        es.close();
        setPhase("done");
      } else if (e.type === "error") {
        es.close();
        setError(e.error ?? "Erro desconhecido");
        setPhase("failed");
      }
    };
    es.onerror = () => {
      // O EventSource reconecta sozinho e o servidor reenvia os eventos já emitidos; só avisamos.
      setLines((l) => [...l, "Conexão com o progresso instável — a limpeza continua no servidor."]);
    };
  };

  const start = async () => {
    setStarting(true);
    try {
      const { operation_id } = await apiClient.startPrometheusWALCleanup({
        analysis_id: report.analysis_id,
        replicas: selected,
        confirm: confirmWord,
        confirm_name: confirmName,
      });
      setOpId(operation_id);
      setPhase("running");
      watch(operation_id);
    } catch (err) {
      toast.error("Não foi possível iniciar a limpeza", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setStarting(false);
    }
  };

  const cancel = async () => {
    if (!opId) return;
    setCancelling(true);
    try {
      await apiClient.cancelPrometheusWALCleanup(opId);
      toast.info("Cancelando: o Prometheus será religado");
    } catch (err) {
      toast.error("Falha ao cancelar", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setCancelling(false);
    }
  };

  const finish = () => {
    onOpenChange(false);
    onFinished();
  };

  // Enquanto roda, o diálogo não fecha (a operação desliga o Prometheus; o usuário precisa ver o fim).
  const handleOpenChange = (next: boolean) => {
    if (!next && phase === "running") return;
    if (!next && (phase === "done" || phase === "failed")) return finish();
    onOpenChange(next);
  };

  const toggle = (pod: string, on: boolean) =>
    setSelected((s) => (on ? [...s, pod] : s.filter((p) => p !== pod)));

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Limpar WAL — {report.namespace}/{report.name}</DialogTitle>
          <DialogDescription>
            Apaga <code>wal/</code> e <code>chunks_head/</code> dos volumes selecionados. Os blocos compactados são preservados.
          </DialogDescription>
        </DialogHeader>

        {phase === "confirm" && (
          <div className="space-y-4 text-sm">
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>
                <b>Todas as réplicas do Prometheus ficam desligadas</b> durante a limpeza (o StatefulSet é gerenciado pelo
                operator), mesmo que só uma seja limpa. As métricas que ainda não viraram bloco compactado serão
                <b> perdidas</b>. Se o recurso é gerenciado por Helm/Argo CD, pause o sync antes.
              </AlertDescription>
            </Alert>

            {report.recurrence?.recurrent && (
              <Alert variant="destructive">
                <AlertTriangle className="h-4 w-4" />
                <AlertDescription>
                  <b>Problema recorrente:</b> este Prometheus já teve o WAL limpo {report.recurrence.count_30d} vezes nos últimos 30
                  dias. Limpar de novo trata o sintoma; a causa provável é cardinalidade, memory limit, disco ou retenção. Você
                  pode continuar, mas investigue a causa.
                </AlertDescription>
              </Alert>
            )}

            <div className="space-y-2">
              <p className="text-xs font-semibold">Réplicas a limpar</p>
              {affected.map((pod) => {
                const d = deepByPod.get(pod);
                return (
                  <label key={pod} className="flex items-start gap-2 rounded-md border p-2 cursor-pointer">
                    <Checkbox checked={selected.includes(pod)} onCheckedChange={(v) => toggle(pod, v === true)} className="mt-0.5" />
                    <span className="text-xs space-y-0.5">
                      <span className="font-mono block">{pod}</span>
                      {d ? (
                        <span className="text-muted-foreground block">
                          WAL {formatBytes(d.wal_bytes)} · {d.blocks} bloco(s) preservado(s) ·{" "}
                          {d.last_block_ends_at
                            ? `perde ~${d.lost_window_hours.toFixed(1)} h (desde ${new Date(d.last_block_ends_at).toLocaleString("pt-BR")})`
                            : "sem blocos compactados: perde TODO o histórico"}
                        </span>
                      ) : (
                        <span className="text-amber-600 block">Sem análise profunda: rode-a antes para ver o que seria perdido.</span>
                      )}
                    </span>
                  </label>
                );
              })}
            </div>

            <div className="space-y-2">
              <p className="text-xs">
                Para confirmar, digite <b>{CONFIRM_WORD}</b> e o nome do recurso Prometheus (<b>{report.name}</b>):
              </p>
              <Input placeholder={CONFIRM_WORD} value={confirmWord} onChange={(e) => setConfirmWord(e.target.value)} autoComplete="off" />
              <Input placeholder={report.name} value={confirmName} onChange={(e) => setConfirmName(e.target.value)} autoComplete="off" />
            </div>
          </div>
        )}

        {phase !== "confirm" && (
          <div className="space-y-3 text-sm">
            <Progress value={Math.round(progress * 100)} />
            <ul className="space-y-1 text-xs max-h-48 overflow-y-auto">
              {lines.map((m, i) => (
                <li key={`${i}-${m}`} className={i === lines.length - 1 && phase === "running" ? "font-semibold" : "text-muted-foreground"}>
                  {m}
                </li>
              ))}
            </ul>
          </div>
        )}

        {phase === "done" && result && (
          <div className="space-y-2 text-sm">
            <div className="flex items-center gap-2 text-green-700 dark:text-green-400">
              <CheckCircle2 className="w-4 h-4" />
              <span>
                Limpeza concluída; Prometheus religado{result.ready ? " e Ready" : ", mas ainda não está Ready"}.
              </span>
            </div>
            {result.cleaned.map((c) => (
              <div key={c.pvc} className="rounded-md bg-muted/50 p-2 text-xs">
                <span className="font-mono">{c.pvc}</span>: WAL de {formatBytes(c.wal_bytes)} apagado, {c.blocks_preserved} bloco(s) preservado(s)
              </div>
            ))}
            {result.warnings?.map((w) => (
              <p key={w} className="text-xs text-amber-700 dark:text-amber-400">{w}</p>
            ))}
          </div>
        )}

        {phase === "failed" && (
          <Alert variant="destructive">
            <XCircle className="h-4 w-4" />
            <AlertDescription className="space-y-1">
              <p>{error}</p>
              <p className="text-xs">
                {result?.restored
                  ? "O Prometheus foi religado."
                  : "Se o Prometheus ficou desligado, rode a análise de novo: ela oferece o botão de restauração."}
              </p>
            </AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          {phase === "confirm" && (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>Cancelar</Button>
              <Button variant="destructive" onClick={start} disabled={!canStart}>
                {starting ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-1 animate-spin" />
                    Iniciando...
                  </>
                ) : (
                  "Apagar WAL"
                )}
              </Button>
            </>
          )}
          {phase === "running" && (
            <Button variant="outline" onClick={cancel} disabled={cancelling}>
              {cancelling ? "Cancelando..." : "Cancelar (religa o Prometheus)"}
            </Button>
          )}
          {(phase === "done" || phase === "failed") && <Button onClick={finish}>Fechar</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
