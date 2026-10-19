// Ambientes não produtivos fora do horário (Fase 3): quanto se economiza parando o AKS ou
// escalando os pools User a 0 à noite e no fim de semana. Com Prometheus, a conta usa as horas-node
// medidas nos últimos 7 dias (GET /finops/node-hours) — se o pool já desce à noite, sobra menos.
// A automação sugerida é um CronJob que escala os deployments a 0 e de volta (o autoscaler tira
// os nodes vazios). Conta em capacity.ts (offHoursEstimate).
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Moon, Info, CalendarClock, Copy, Activity } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { apiClient } from "@/lib/api/client";
import { fmtBRL, KubectlBlock } from "@/lib/finopsFormat";
import type { FinOpsPool } from "./types";
import { type OffHoursSchedule, offHoursEstimate } from "./capacity";
import { SectionTitle } from "./charts";

const HOURS = Array.from({ length: 24 }, (_, h) => h);
const SYSTEM_NS = /^(kube-|gatekeeper|calico|tigera|azure-|monitoring$|logging$|ingress|cert-manager|kyverno|velero|dynatrace|falcon|istio)/;

/** CronJobs (America/Sao_Paulo) que escalam os deployments dos namespaces a 0 e restauram as réplicas. */
function cronJobYAML(s: OffHoursSchedule, namespaces: string[]): string {
  const days = s.weekends ? "1-5" : "*";
  const down = `0 ${s.offStart} * * ${s.weekends ? "1-5" : "*"}`;
  const up = `0 ${s.offEnd} * * ${days}`;
  const nsList = namespaces.join(" ");
  const script = (dir: "down" | "up") => dir === "down"
    ? `for ns in ${nsList}; do
          for d in $(kubectl -n $ns get deploy -o name); do
            r=$(kubectl -n $ns get $d -o jsonpath='{.spec.replicas}')
            [ "$r" = "0" ] && continue
            kubectl -n $ns annotate $d finops.offhours/replicas=$r --overwrite
            kubectl -n $ns scale $d --replicas=0
          done
        done`
    : `for ns in ${nsList}; do
          for d in $(kubectl -n $ns get deploy -o name); do
            r=$(kubectl -n $ns get $d -o jsonpath='{.metadata.annotations.finops\\.offhours/replicas}')
            [ -z "$r" ] && continue
            kubectl -n $ns scale $d --replicas=$r
            kubectl -n $ns annotate $d finops.offhours/replicas-
          done
        done`;
  const job = (name: string, schedule: string, dir: "down" | "up") => `---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ${name}
  namespace: finops-offhours
spec:
  schedule: "${schedule}"
  timeZone: America/Sao_Paulo
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      backoffLimit: 1
      template:
        spec:
          serviceAccountName: offhours
          restartPolicy: Never
          containers:
            - name: kubectl
              image: bitnami/kubectl:1.30
              command: ["/bin/sh", "-c"]
              args:
                - |
                  ${script(dir)}`;
  return `# Gerado pelo FinOps (K8s HPA Manager) — revise antes de aplicar.
# Desliga às ${s.offStart}h${s.weekends ? " (seg–sex) e fica desligado no fim de semana" : ""}; religa às ${s.offEnd}h${s.weekends ? " (seg–sex)" : ""}.
# Os deployments vão a 0 (a réplica anterior fica na anotação finops.offhours/replicas) e o
# autoscaler tira os nodes vazios — o pool precisa de autoscaler com min 0 (comando ao lado).
# HPA com 0 réplicas fica inativo e volta a agir quando o deployment é restaurado.
# GitOps (Argo/Flux) reverte o scale: pause a sincronização desses apps no horário ou use KEDA cron.
apiVersion: v1
kind: Namespace
metadata:
  name: finops-offhours
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: offhours
  namespace: finops-offhours
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: finops-offhours
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "deployments/scale"]
    verbs: ["get", "list", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: finops-offhours
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: finops-offhours
subjects:
  - kind: ServiceAccount
    name: offhours
    namespace: finops-offhours
${job("offhours-down", down, "down")}
${job("offhours-up", up, "up")}
`;
}

export function OffHoursCard({ pools, cluster, provider, namespaces }: { pools: FinOpsPool[]; cluster: string; provider?: string; namespaces: string[] }) {
  const [s, setS] = useState<OffHoursSchedule>({ offStart: 20, offEnd: 8, weekends: true });
  const [autoOpen, setAutoOpen] = useState(false);
  const [nsText, setNsText] = useState(() => namespaces.filter(n => !SYSTEM_NS.test(n)).join(" "));
  const { data: nh } = useQuery({
    queryKey: ["finops-node-hours", cluster],
    queryFn: () => apiClient.get<{ available: boolean; reason?: string; node_hours?: { pools: Record<string, number[]>; hours: number } }>(`/finops/node-hours?cluster=${encodeURIComponent(cluster)}`),
    staleTime: 30 * 60_000,
    retry: false,
  });
  const profile = nh?.available ? nh.node_hours?.pools : undefined;
  const est = offHoursEstimate(pools, s, profile);
  const name = cluster.replace(/-admin$/, "");
  const userPools = est.pools.filter(p => !p.stays);
  const measured = est.pools.filter(p => p.measured);
  const offAvg = measured.reduce((a, p) => a + (p.offNodes ?? 0), 0);
  const onAvg = measured.reduce((a, p) => a + (p.onNodes ?? 0), 0);
  const yaml = cronJobYAML(s, nsText.split(/[\s,]+/).filter(Boolean));

  const option = (title: string, sub: string, v: { certainBRL: number; committedBRL: number }) => (
    <div className="rounded-lg border bg-background/60 p-3">
      <p className="text-xs font-medium">{title}</p>
      <p className="text-[11px] text-muted-foreground">{sub}</p>
      <p className="mt-1.5 text-2xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">
        {v.certainBRL >= 1 ? `${fmtBRL(v.certainBRL)}/mês` : "—"}
      </p>
      <p className="text-[11px] text-muted-foreground">
        {v.certainBRL >= 1 ? `${fmtBRL(v.certainBRL * 12)}/ano` : "nada certo"}
        {v.committedBRL >= 1 && <> · <span className="text-amber-600 dark:text-amber-400">+ até {fmtBRL(v.committedBRL)} em pools sob reserva (só reaproveitando a reserva)</span></>}
      </p>
    </div>
  );

  return (
    <div className="rounded-xl border border-indigo-500/30 bg-gradient-to-br from-indigo-500/5 to-violet-500/5 p-5 shadow-sm">
      <SectionTitle icon={Moon} title="Fora do horário"
        sub={`Ambiente não produtivo: horário desligado = ${Math.round(est.fraction * 100)}% da semana (${Math.round(est.fraction * 168)} h de 168 h)`}
        right={
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1">desliga às
              <select value={s.offStart} onChange={e => setS({ ...s, offStart: Number(e.target.value) })} className="h-7 rounded border bg-background px-1">
                {HOURS.map(h => <option key={h} value={h}>{h}h</option>)}
              </select>
            </label>
            <label className="flex items-center gap-1">liga às
              <select value={s.offEnd} onChange={e => setS({ ...s, offEnd: Number(e.target.value) })} className="h-7 rounded border bg-background px-1">
                {HOURS.map(h => <option key={h} value={h}>{h}h</option>)}
              </select>
            </label>
            <label className="flex items-center gap-1 cursor-pointer">
              <input type="checkbox" checked={s.weekends} onChange={e => setS({ ...s, weekends: e.target.checked })} className="h-3.5 w-3.5" />
              fim de semana inteiro
            </label>
          </div>
        } />

      <p className={`mb-3 flex items-center gap-1.5 text-xs ${measured.length ? "text-foreground" : "text-amber-700 dark:text-amber-300"}`}>
        <Activity className="h-3.5 w-3.5 shrink-0" />
        {measured.length
          ? <>Medido nos últimos 7 dias (Prometheus) em {measured.length} de {est.pools.length} pool(s): fora do horário eles rodam em média <strong>{offAvg.toFixed(1)} node(s)</strong>, contra {onAvg.toFixed(1)} no horário{offAvg >= onAvg * 0.9 ? " — hoje ficam ligados a noite toda" : " — já descem parte à noite"}.{measured.length < est.pools.length ? " Nos demais, a conta supõe ligado 24 h." : ""}</>
          : <>Sem o perfil por hora do Prometheus ({!nh ? "carregando…" : nh.available
              ? "poucas horas com dado nos últimos 7 dias para os pools deste relatório"
              : nh.reason}): a conta supõe o ambiente ligado 24 h.</>}
      </p>

      <div className="grid gap-3 md:grid-cols-2">
        {option("Parar o cluster", "az aks stop: todos os nodes saem (os discos continuam cobrados)", est.stopCluster)}
        {option("Escalar pools User a 0", `o pool System fica ligado; ${userPools.length} pool(s) User`, est.scaleUserPools)}
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        {est.pools.map(p => (
          <span key={p.pool.name} className={`rounded-full border px-2 py-0.5 text-[11px] ${p.committed ? "border-amber-500/40 text-amber-700 dark:text-amber-300" : ""}`}
            title={p.committed ? "Sob reserva/Savings Plan: desligar não reduz a fatura, a menos que a reserva seja usada por outra carga"
              : `custo/mês ${fmtBRL(p.monthlyBRL)} · ${Math.round(p.share * 100)}% das horas-node fora do horário${p.measured ? ` (medido: ${p.offNodes?.toFixed(1)} node(s) à noite, ${p.onNodes?.toFixed(1)} de dia)` : " (estimado)"}`}>
            {p.pool.name}{p.stays ? " (System)" : ""} · {fmtBRL(p.savingBRL)}{p.committed ? " · reserva" : ""}
          </span>
        ))}
      </div>

      {provider === "aks" && (
        <div className="mt-3 space-y-1.5">
          <KubectlBlock cmd={`az aks stop --resource-group <RESOURCE_GROUP> --name ${name}   # às ${s.offStart}h; para religar: az aks start ...`} />
          {userPools.length > 0 && (
            <KubectlBlock cmd={userPools.map(p => `az aks nodepool update --resource-group <RESOURCE_GROUP> --cluster-name ${name} --name ${p.pool.name} --update-cluster-autoscaler --min-count 0 --max-count <MAX>`).join(" && ")} />
          )}
        </div>
      )}
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <p className="flex items-start gap-1 text-[11px] text-muted-foreground">
          <Info className="h-3 w-3 mt-0.5 shrink-0" />
          Custo atual dos pools (efetivo quando há cobertura). Com min 0 no autoscaler, os nodes só saem quando os pods também saem.
        </p>
        <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={() => setAutoOpen(true)}>
          <CalendarClock className="h-3.5 w-3.5" /> Automatizar (CronJob)
        </Button>
      </div>

      <Dialog open={autoOpen} onOpenChange={setAutoOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Automatizar fora do horário — {name}</DialogTitle>
            <DialogDescription>
              CronJobs no próprio cluster escalam os deployments a 0 às {s.offStart}h e restauram às {s.offEnd}h (horário de Brasília). Nada é aplicado daqui: revise e aplique com <code>kubectl apply -f</code> ou pelo repositório GitOps.
            </DialogDescription>
          </DialogHeader>
          <label className="block space-y-1 text-xs">
            <span className="font-medium">Namespaces (separados por espaço)</span>
            <input value={nsText} onChange={e => setNsText(e.target.value)} className="h-8 w-full rounded border bg-background px-2 font-mono text-xs" />
            <span className="text-[11px] text-muted-foreground">Os de sistema (kube-*, monitoring, ingress, logging…) ficam de fora por padrão.</span>
          </label>
          <pre className="max-h-80 overflow-auto whitespace-pre rounded-md border bg-muted/40 p-3 text-[11px] font-mono">{yaml}</pre>
          <div className="flex justify-end">
            <Button size="sm" className="gap-1.5" onClick={async () => {
              try { await navigator.clipboard.writeText(yaml); toast.success("YAML copiado"); } catch (e) { toast.error(String(e)); }
            }}><Copy className="h-3.5 w-3.5" /> Copiar YAML</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
