// Aplicar uma recomendação do FinOps (Fase 3): manda para o Staging (HPA real + requests do
// deployment) ou gera o patch de values (Helm), e grava o envio para acompanhamento
// (POST /finops/actions — a foto do workload é tirada no servidor).
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Send, FileCode2, Loader2, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useStaging } from "@/contexts/StagingContext";
import { apiClient } from "@/lib/api/client";
import type { HPA } from "@/lib/api/types";
import type { FinOpsWorkload } from "./types";
import { type ActionTargets, cpuQty, memQty, parseCpuMillis, parseMemMi, valuesPatch } from "./helpers";

async function recordAction(cluster: string, w: FinOpsWorkload, t: ActionTargets, channel: "staging" | "values", expected: number) {
  await apiClient.recordFinOpsAction({
    cluster, namespace: w.namespace, workload: w.workload, channel, expected_saving_brl: expected,
    target_hpa_min: t.minReplicas, target_hpa_max: t.maxReplicas, target_cpu_millis: t.cpuMillis, target_mem_mi: t.memMi,
  });
}

const near = (a: number, b: number) => Number.isFinite(a) && b > 0 && Math.abs(a - b) <= b * 0.02;

export function ApplyRecommendation({ cluster, w, targets, expectedSaving }: {
  cluster: string;
  w: FinOpsWorkload;
  targets: ActionTargets;
  expectedSaving: number;
}) {
  const staging = useStaging();
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [valuesOpen, setValuesOpen] = useState(false);
  // O Staging aplica em HPA (min/max) e nos requests do deployment alvo do HPA.
  const canStage = !!w.hpa_name && (w.kind ?? "Deployment") === "Deployment";
  const patch = valuesPatch(w, targets);

  const sendToStaging = async () => {
    setBusy(true);
    try {
      const hpa = await apiClient.getHPA(cluster, w.namespace, w.hpa_name!, true);
      const updates: Partial<HPA> = {};
      const skipped: string[] = [];
      if (targets.minReplicas !== undefined) updates.min_replicas = targets.minReplicas;
      if (targets.maxReplicas !== undefined) updates.max_replicas = targets.maxReplicas;
      // O Staging grava os requests no PRIMEIRO container; a recomendação é do pod inteiro. Só leva
      // os requests quando o primeiro container já carrega o request do pod (sem sidecar com request).
      const ov = hpa.original_values ?? {};
      if (targets.cpuMillis !== undefined) {
        if (near(parseCpuMillis(ov.cpu_request), w.cpu_request_millis)) {
          updates.target_cpu_request = cpuQty(targets.cpuMillis);
          const lim = parseCpuMillis(ov.cpu_limit);
          if (Number.isFinite(lim) && targets.cpuMillis > lim) updates.target_cpu_limit = cpuQty(Math.max(targets.cpuMillis, w.cpu_limit_recommended_millis ?? 0));
        } else skipped.push("CPU");
      }
      if (targets.memMi !== undefined) {
        if (near(parseMemMi(ov.memory_request), w.mem_request_mi)) {
          updates.target_memory_request = memQty(targets.memMi);
          const lim = parseMemMi(ov.memory_limit);
          if (Number.isFinite(lim) && targets.memMi > lim) updates.target_memory_limit = memQty(Math.max(targets.memMi, w.mem_limit_recommended_mi ?? 0));
        } else skipped.push("memória");
      }
      if (Object.keys(updates).length === 0) {
        toast.warning(`Nada a enviar: o request de ${skipped.join(" e ")} do pod está dividido entre containers. Use o patch de values.`);
        return;
      }
      staging.addHPAToStaging(hpa);
      staging.updateHPAInStaging(hpa.cluster, hpa.namespace, hpa.name, updates);
      const sent: ActionTargets = {
        minReplicas: updates.min_replicas ?? undefined,
        maxReplicas: updates.max_replicas,
        cpuMillis: updates.target_cpu_request ? targets.cpuMillis : undefined,
        memMi: updates.target_memory_request ? targets.memMi : undefined,
      };
      await recordAction(cluster, w, sent, "staging", expectedSaving);
      await queryClient.invalidateQueries({ queryKey: ["finops-actions", cluster] });
      toast.success(`${w.workload} no Staging — revise e aplique na aba Staging`, {
        description: skipped.length ? `Request de ${skipped.join(" e ")} ficou de fora: o pod tem mais de um container com request.` : "Acompanhamento em Workloads → Acompanhamento.",
      });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const copyValues = async () => {
    try {
      await navigator.clipboard.writeText(patch);
      await recordAction(cluster, w, targets, "values", expectedSaving);
      await queryClient.invalidateQueries({ queryKey: ["finops-actions", cluster] });
      toast.success("Patch copiado e em acompanhamento");
      setValuesOpen(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {canStage && (
        <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={sendToStaging} disabled={busy}
          title="Busca o HPA no cluster e põe o alvo no Staging (min/max e requests do deployment). Nada é aplicado até você confirmar na aba Staging.">
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />} Enviar ao Staging
        </Button>
      )}
      <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={() => setValuesOpen(true)}
        title="Bloco YAML para o values do chart (Helm/Argo/Flux) — o caminho certo quando o workload é gerenciado por GitOps">
        <FileCode2 className="h-3.5 w-3.5" /> Patch de values
      </Button>
      <Dialog open={valuesOpen} onOpenChange={setValuesOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Patch de values — {w.workload}</DialogTitle>
            <DialogDescription>
              Requests por pod. Se o pod tem sidecar com request, desconte a parte dele. Ao copiar, a recomendação entra no acompanhamento.
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-3 text-xs font-mono">{patch}</pre>
          <div className="flex justify-end">
            <Button size="sm" className="gap-1.5" onClick={copyValues}><Copy className="h-3.5 w-3.5" /> Copiar e acompanhar</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
