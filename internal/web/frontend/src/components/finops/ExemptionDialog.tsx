// Isenção por workload (FINOPS-EXEMPTIONS-PLAN.md): o workload sai do desperdício e das
// oportunidades enquanto as réplicas (atuais e o pico do histórico) ficarem dentro do limite. O
// backend aplica a isenção na leitura do relatório, então salvar só relê o último relatório.
import { useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, ShieldCheck, ShieldOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { apiClient } from "@/lib/api/client";
import { fmtBRL, verdictConfig } from "@/lib/finopsFormat";
import type { FinOpsReport, FinOpsWorkload } from "./types";

const REASONS = ["Ambiente HLG", "Controle de custos", "Definição de projeto", "Política de capacidade"];

// Relê o último relatório (o backend reaplica as isenções) e troca o que está na tela.
async function reloadReport(queryClient: QueryClient, cluster: string) {
  const data = await apiClient.get<FinOpsReport & { scanned?: boolean }>(`/finops/report/last?cluster=${encodeURIComponent(cluster)}`);
  if (data?.scanned !== false) queryClient.setQueryData(["finops-report", cluster], data);
}

/** Etiqueta de isenção ao lado do veredito: ativa (cinza) ou sem efeito (âmbar). */
export function ExemptBadge({ w }: { w: FinOpsWorkload }) {
  if (!w.is_exempt) return null;
  if (w.exempt_active) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-slate-500/15 px-1.5 py-0.5 text-[10px] font-medium text-slate-700 dark:text-slate-300"
        title={`Isento: ${w.exempt_reason || "sem motivo"} · até ${w.exempt_threshold} réplica(s)`}>
        <ShieldCheck className="h-3 w-3" /> Isento: {w.exempt_reason || "sem motivo"}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:text-amber-300"
      title={`A isenção (${w.exempt_reason || "sem motivo"}) vale até ${w.exempt_threshold} réplica(s); o workload chegou a ${w.exempt_replicas}`}>
      <ShieldOff className="h-3 w-3" /> Isenção sem efeito: {w.exempt_replicas} réplicas &gt; limite {w.exempt_threshold}
    </span>
  );
}

export function ExemptionDialog({ cluster, workload: w, open, onOpenChange }: {
  cluster: string;
  workload: FinOpsWorkload | null;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  if (!w) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {/* key: reabrir para outro workload recomeça o formulário */}
        <ExemptionForm key={`${w.namespace}/${w.workload}`} cluster={cluster} w={w} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

function ExemptionForm({ cluster, w, onDone }: { cluster: string; w: FinOpsWorkload; onDone: () => void }) {
  const queryClient = useQueryClient();
  const initialReason = w.exempt_reason ?? REASONS[0];
  const [threshold, setThreshold] = useState(w.exempt_threshold ?? Math.max(1, w.hpa_max || w.pods || 1));
  const [preset, setPreset] = useState(REASONS.includes(initialReason) ? initialReason : "outro");
  const [custom, setCustom] = useState(REASONS.includes(initialReason) ? "" : initialReason);
  const [busy, setBusy] = useState<"save" | "delete" | null>(null);
  const reason = (preset === "outro" ? custom : preset).trim();
  const verdict = w.exempt_original_verdict ?? w.verdict;
  const waste = w.exempt_original_waste_brl ?? w.waste_brl ?? 0;

  const run = async (kind: "save" | "delete") => {
    setBusy(kind);
    try {
      if (kind === "save") {
        await apiClient.upsertFinOpsExemption({ cluster, namespace: w.namespace, workload: w.workload, max_replicas_threshold: threshold, reason });
      } else {
        await apiClient.deleteFinOpsExemption(cluster, w.namespace, w.workload);
      }
      await reloadReport(queryClient, cluster);
      toast.success(kind === "save" ? `Isenção salva para ${w.workload}` : `Isenção removida de ${w.workload}`);
      onDone();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-primary" /> {w.is_exempt ? "Editar isenção" : "Isentar workload"}</DialogTitle>
        <DialogDescription>
          <span className="font-medium text-foreground">{w.namespace}/{w.workload}</span>
          {" · "}análise: {verdictConfig[verdict]?.label ?? verdict}{waste > 0 && <>, {fmtBRL(waste)}/mês</>}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 text-sm">
        <label className="block space-y-1">
          <span className="block text-xs font-medium">Vale até quantas réplicas</span>
          <input type="number" min={1} value={threshold} onChange={e => setThreshold(Math.max(1, Number(e.target.value) || 1))}
            className="h-8 w-28 rounded border bg-background px-2 text-sm" />
          <span className="block text-[11px] text-muted-foreground">
            Hoje: {w.hpa_max > 0 ? `HPA ${w.hpa_min}/${w.hpa_current}/${w.hpa_max}` : `${w.pods} pod(s)`}
            {(w.hpa_max_observed ?? 0) > 0 && ` · pico no histórico: ${w.hpa_max_observed}`}.
            Se passar do limite, a isenção deixa de valer sozinha.
          </span>
        </label>
        <label className="block space-y-1">
          <span className="block text-xs font-medium">Motivo</span>
          <select value={preset} onChange={e => setPreset(e.target.value)} className="h-8 w-full rounded border bg-background px-2 text-sm">
            {REASONS.map(r => <option key={r} value={r}>{r}</option>)}
            <option value="outro">Outro…</option>
          </select>
          {preset === "outro" && (
            <input value={custom} onChange={e => setCustom(e.target.value.slice(0, 200))} placeholder="Descreva o motivo"
              className="h-8 w-full rounded border bg-background px-2 text-sm" autoFocus />
          )}
        </label>
        <p className="text-[11px] text-muted-foreground">
          O workload sai do desperdício, das oportunidades e da economia do relatório. Vale para todo mundo que abrir este cluster e fica no Change History.
        </p>
      </div>

      <DialogFooter className="gap-2 sm:justify-between">
        {w.is_exempt ? (
          <Button variant="outline" size="sm" onClick={() => run("delete")} disabled={!!busy} className="text-rose-600">
            {busy === "delete" ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <ShieldOff className="h-3.5 w-3.5 mr-1.5" />}
            Remover isenção
          </Button>
        ) : <span />}
        <Button size="sm" onClick={() => run("save")} disabled={!!busy || !reason}>
          {busy === "save" && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
          Salvar
        </Button>
      </DialogFooter>
    </>
  );
}
