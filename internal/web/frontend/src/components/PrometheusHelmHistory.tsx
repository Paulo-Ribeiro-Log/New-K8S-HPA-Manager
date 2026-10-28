import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { AlertTriangle, Loader2, RefreshCw, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { PrometheusHelmRevision } from "@/lib/api/types";

interface Props {
  cluster: string;
  namespace: string;
  release: string;
  /** Chamado depois de um rollback aplicado, para recarregar a lista de recursos. */
  onRolledBack?: () => void;
}

const STATUS_CLASS: Record<string, string> = {
  deployed: "bg-green-600 text-white",
  failed: "bg-red-600 text-white",
  superseded: "bg-muted text-muted-foreground",
};

// Revisões do release (cada helm upgrade cria uma) e rollback para uma delas.
export const PrometheusHelmHistory = ({ cluster, namespace, release, onRolledBack }: Props) => {
  const [revs, setRevs] = useState<PrometheusHelmRevision[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [target, setTarget] = useState<PrometheusHelmRevision | null>(null);
  const [rolling, setRolling] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setRevs((await apiClient.getPrometheusHelmHistory(cluster, namespace, release)).slice().reverse()); // mais recente primeiro
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [cluster, namespace, release]);

  useEffect(() => { void load(); }, [load]);

  const rollback = async () => {
    if (!target) return;
    setRolling(true);
    try {
      await apiClient.rollbackPrometheusHelm({ cluster, namespace, release, revision: target.revision });
      toast.success(`Release voltou para a revisão ${target.revision} (nova revisão criada)`);
      setTarget(null);
      onRolledBack?.();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setTarget(null);
    } finally {
      setRolling(false);
    }
  };

  const current = revs?.find((r) => r.status === "deployed")?.revision;

  return (
    <div className="space-y-2 text-sm">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          Revisões de <span className="font-mono">{namespace}/{release}</span>. Cada aplicação cria uma; o rollback volta os values
          e o manifesto de uma delas (e cria uma nova revisão).
        </p>
        <Button size="sm" variant="outline" className="h-7" onClick={() => void load()} disabled={loading}>
          <RefreshCw className={`w-3.5 h-3.5 mr-1 ${loading ? "animate-spin" : ""}`} /> Atualizar
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription className="text-xs whitespace-pre-wrap break-words">{error}</AlertDescription>
        </Alert>
      )}
      {loading && !revs && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground py-4 justify-center">
          <Loader2 className="w-4 h-4 animate-spin" /> Lendo o histórico do release…
        </div>
      )}

      {revs && (
        <div className="rounded-md border divide-y">
          {revs.map((r) => (
            <div key={r.revision} className="flex items-center gap-3 px-3 py-2 text-xs">
              <span className="font-mono font-semibold w-8">#{r.revision}</span>
              <span className={`rounded px-1.5 py-0.5 text-[10px] ${STATUS_CLASS[r.status] ?? "bg-muted"}`}>{r.status}</span>
              <span className="text-muted-foreground w-40 shrink-0">{r.updated ? new Date(r.updated).toLocaleString("pt-BR") : "—"}</span>
              <span className="flex-1 min-w-0 truncate" title={r.description}>
                {r.chart} · {r.description}
              </span>
              {r.revision !== current && (
                <Button size="sm" variant="outline" className="h-7" onClick={() => setTarget(r)} disabled={rolling}>
                  <Undo2 className="w-3.5 h-3.5 mr-1" /> Voltar para esta
                </Button>
              )}
            </div>
          ))}
        </div>
      )}

      <AlertDialog open={!!target} onOpenChange={(o) => !rolling && !o && setTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Voltar para a revisão {target?.revision}?</AlertDialogTitle>
            <AlertDialogDescription>
              O release <span className="font-mono">{namespace}/{release}</span> ({cluster}) volta aos values e ao manifesto da
              revisão {target?.revision}. O Prometheus e os demais componentes podem ser recriados.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rolling}>Cancelar</AlertDialogCancel>
            <AlertDialogAction disabled={rolling} onClick={(e) => { e.preventDefault(); void rollback(); }}>
              {rolling ? "Voltando…" : "Voltar o release"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
};
