import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { AlertTriangle, CheckCircle2, Copy, Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { copyText } from "@/lib/clipboard";
import type { PrometheusHelmResult } from "@/lib/api/types";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  /** Chamado depois de aplicar, para recarregar a lista de recursos. */
  onApplied?: () => void;
}

interface Toleration {
  key: string;
  operator: "Equal" | "Exists";
  value: string;
  effect: "" | "NoSchedule" | "PreferNoSchedule" | "NoExecute";
}

// Opções prontas: o taint do pool dedicado do Prometheus e o do pool spot do AKS.
const PRESETS: { id: string; label: string; tol: Toleration }[] = [
  { id: "prometheus-only", label: "prometheus-only", tol: { key: "prometheus-only", operator: "Equal", value: "true", effect: "NoSchedule" } },
  { id: "spot", label: "spot", tol: { key: "kubernetes.azure.com/scalesetpriority", operator: "Equal", value: "spot", effect: "NoSchedule" } },
];

// Onde o chart lê as tolerations de cada componente.
const COMPONENTS: { id: string; label: string; path: string[] }[] = [
  { id: "prometheus", label: "Prometheus", path: ["prometheus", "prometheusSpec"] },
  { id: "alertmanager", label: "Alertmanager", path: ["alertmanager", "alertmanagerSpec"] },
  { id: "kube-state-metrics", label: "kube-state-metrics", path: ["kube-state-metrics"] },
  { id: "prometheusOperator", label: "Prometheus Operator", path: ["prometheusOperator"] },
];

const emptyCustom = (): Toleration => ({ key: "", operator: "Equal", value: "", effect: "NoSchedule" });

// Monta o override de values: strings sempre entre aspas (JSON é YAML válido), sem value quando o operador é Exists.
function buildOverrideYaml(tols: Toleration[], components: string[]): string {
  const items = tols.map((t) => {
    const lines = [`- key: ${JSON.stringify(t.key)}`, `  operator: ${t.operator}`];
    if (t.operator === "Equal") lines.push(`  value: ${JSON.stringify(t.value)}`);
    if (t.effect) lines.push(`  effect: ${t.effect}`);
    return lines;
  });
  const out: string[] = [];
  for (const c of COMPONENTS.filter((c) => components.includes(c.id))) {
    c.path.forEach((seg, i) => out.push(`${"  ".repeat(i)}${seg}:`));
    const base = "  ".repeat(c.path.length);
    out.push(`${base}tolerations:`);
    items.forEach((lines) => lines.forEach((l) => out.push(`${base}  ${l}`)));
    out.push("");
  }
  return out.join("\n");
}

// Corrige as tolerations do kube-prometheus-stack com o mesmo `helm upgrade --reuse-values -f override` que o SRE
// roda à mão: primeiro a simulação (--dry-run), depois a aplicação. A tela só mostra as tolerations por recurso.
export const PrometheusHelmTolerationsDialog = ({ open, onOpenChange, cluster, namespace, onApplied }: Props) => {
  const [loading, setLoading] = useState(true);
  const [detectError, setDetectError] = useState<string | null>(null);
  const [release, setRelease] = useState("");
  const [chart, setChart] = useState("kube-prometheus-stack");
  const [repo, setRepo] = useState("");
  const [version, setVersion] = useState("");
  const [presets, setPresets] = useState<string[]>(PRESETS.map((p) => p.id));
  const [custom, setCustom] = useState<Toleration[]>([]);
  const [components, setComponents] = useState<string[]>(COMPONENTS.map((c) => c.id));
  const [busy, setBusy] = useState<"dry" | "apply" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PrometheusHelmResult | null>(null);
  const [confirm, setConfirm] = useState(false);
  // O que a última simulação usou: qualquer edição depois dela invalida a aplicação.
  const [simulatedWith, setSimulatedWith] = useState<string | null>(null);

  // Custom sem key só vale com operator Exists (tolera tudo); do contrário a linha ainda está incompleta.
  const customValid = custom.every((t) => t.key.trim() !== "" || t.operator === "Exists");
  const valuesYaml = useMemo(() => {
    const tols = [
      ...PRESETS.filter((p) => presets.includes(p.id)).map((p) => p.tol),
      ...custom.map((t) => ({ ...t, key: t.key.trim(), value: t.value.trim() })),
    ];
    return tols.length === 0 || components.length === 0 ? "" : buildOverrideYaml(tols, components);
  }, [presets, custom, components]);
  const toggle = (list: string[], id: string) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  const updateCustom = (i: number, patch: Partial<Toleration>) =>
    setCustom((rows) => rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));

  const signature = JSON.stringify([release, chart, repo, version, valuesYaml]);
  const simulated = result?.dry_run === true && simulatedWith === signature;

  useEffect(() => {
    if (!open) return;
    let alive = true;
    setLoading(true);
    setError(null);
    setResult(null);
    setSimulatedWith(null);
    apiClient.getPrometheusHelmInfo(cluster, namespace)
      .then((info) => {
        if (!alive) return;
        setRelease(info.release ?? "");
        setChart(info.chart);
        setRepo(info.repo);
        setVersion(info.chart_version ?? "");
        setDetectError(info.release ? null : (info.detect_error ?? "Release do kube-prometheus-stack não encontrado neste namespace: informe o nome."));
      })
      .catch((e) => alive && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, [open, cluster, namespace]);

  const run = async (dryRun: boolean) => {
    setBusy(dryRun ? "dry" : "apply");
    setError(null);
    try {
      const res = await apiClient.applyPrometheusHelmTolerations({ cluster, namespace, release, chart, repo, version, valuesYaml, dryRun });
      setResult(res);
      if (dryRun) {
        setSimulatedWith(signature);
      } else {
        setSimulatedWith(null);
        toast.success(`Helm upgrade aplicado (revisão ${res.revision})`);
        onApplied?.();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const canRun = !loading && !busy && release.trim() !== "" && valuesYaml.trim() !== "" && customValid;

  return (
    <>
      <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Tolerations do Prometheus (Helm)</DialogTitle>
            <DialogDescription>
              Roda <span className="font-mono">helm upgrade --reuse-values -f override</span> no release do kube-prometheus-stack
              de <span className="font-mono">{namespace}</span> ({cluster}): os values atuais são mantidos, mas a lista de tolerations
              de cada componente marcado é <b>substituída</b> pela que está abaixo (o helm não soma listas). Simule antes de aplicar.
            </DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground py-6 justify-center">
              <Loader2 className="w-4 h-4 animate-spin" /> Procurando o release no namespace…
            </div>
          ) : (
            <div className="space-y-3 text-sm">
              {detectError && (
                <Alert>
                  <AlertTriangle className="h-4 w-4" />
                  <AlertDescription className="text-xs">{detectError}</AlertDescription>
                </Alert>
              )}
              <div className="grid grid-cols-2 gap-2">
                <div className="space-y-1">
                  <Label className="text-xs">Release</Label>
                  <Input value={release} onChange={(e) => setRelease(e.target.value)} className="h-8 text-xs font-mono" />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">Versão do chart (vazio = a mais recente)</Label>
                  <Input value={version} onChange={(e) => setVersion(e.target.value)} className="h-8 text-xs font-mono" />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">Chart</Label>
                  <Input value={chart} onChange={(e) => setChart(e.target.value)} className="h-8 text-xs font-mono" />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">Repositório</Label>
                  <Input value={repo} onChange={(e) => setRepo(e.target.value)} className="h-8 text-xs font-mono" />
                </div>
              </div>
              <div className="space-y-2">
                <Label className="text-xs">Tolerations a somar</Label>
                <div className="flex flex-wrap gap-x-4 gap-y-1">
                  {PRESETS.map((p) => (
                    <label key={p.id} className="flex items-center gap-1.5 text-xs cursor-pointer">
                      <Checkbox checked={presets.includes(p.id)} onCheckedChange={() => setPresets((l) => toggle(l, p.id))} />
                      <span className="font-medium">{p.label}</span>
                      <span className="font-mono text-muted-foreground">
                        {p.tol.key}={p.tol.value}:{p.tol.effect}
                      </span>
                    </label>
                  ))}
                </div>

                {custom.map((t, i) => (
                  <div key={i} className="grid grid-cols-[1.6fr_0.9fr_1fr_1.2fr_auto] gap-2 items-center">
                    <Input value={t.key} onChange={(e) => updateCustom(i, { key: e.target.value })}
                      placeholder="key" className="h-8 text-xs font-mono" />
                    <Select value={t.operator} onValueChange={(v) => updateCustom(i, { operator: v as Toleration["operator"] })}>
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="Equal">Equal</SelectItem>
                        <SelectItem value="Exists">Exists</SelectItem>
                      </SelectContent>
                    </Select>
                    <Input value={t.value} onChange={(e) => updateCustom(i, { value: e.target.value })}
                      placeholder="value" disabled={t.operator === "Exists"} className="h-8 text-xs font-mono" />
                    <Select value={t.effect || "all"} onValueChange={(v) => updateCustom(i, { effect: v === "all" ? "" : (v as Toleration["effect"]) })}>
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="NoSchedule">NoSchedule</SelectItem>
                        <SelectItem value="PreferNoSchedule">PreferNoSchedule</SelectItem>
                        <SelectItem value="NoExecute">NoExecute</SelectItem>
                        <SelectItem value="all">(todos os effects)</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button type="button" size="icon" variant="ghost" className="h-8 w-8"
                      onClick={() => setCustom((rows) => rows.filter((_, idx) => idx !== i))} title="Remover">
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                ))}
                <Button type="button" size="sm" variant="outline" className="h-7 text-xs"
                  onClick={() => setCustom((rows) => [...rows, emptyCustom()])}>
                  <Plus className="w-3.5 h-3.5 mr-1" /> Custom
                </Button>
                {!customValid && (
                  <p className="text-[11px] text-amber-600">Toda toleration custom precisa de key (ou do operador Exists).</p>
                )}
              </div>

              <div className="space-y-1">
                <Label className="text-xs">Aplicar em</Label>
                <div className="flex flex-wrap gap-x-4 gap-y-1">
                  {COMPONENTS.map((c) => (
                    <label key={c.id} className="flex items-center gap-1.5 text-xs cursor-pointer">
                      <Checkbox checked={components.includes(c.id)} onCheckedChange={() => setComponents((l) => toggle(l, c.id))} />
                      {c.label}
                    </label>
                  ))}
                </div>
              </div>

              <div className="space-y-1">
                <Label className="text-xs">Override gerado (tolerations-override.yaml)</Label>
                <Textarea value={valuesYaml || "# selecione ao menos uma toleration e um componente"} readOnly
                  spellCheck={false} className="font-mono text-xs h-48 bg-muted/40" />
              </div>

              {error && (
                <Alert variant="destructive">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertDescription className="text-xs whitespace-pre-wrap break-words">{error}</AlertDescription>
                </Alert>
              )}

              {result && (
                <div className="rounded-md border p-3 space-y-2">
                  <div className="flex items-center gap-2 text-xs">
                    <CheckCircle2 className="w-4 h-4 text-green-600" />
                    <span className="font-medium">{result.dry_run ? "Simulação (dry-run) concluída" : "Aplicado"}</span>
                    <span className="text-muted-foreground">
                      {result.release} · chart {result.chart_version} · revisão {result.revision} · {result.status}
                    </span>
                  </div>
                  {result.workloads.length === 0 ? (
                    <p className="text-xs text-muted-foreground">Nenhum recurso do manifesto leva tolerations.</p>
                  ) : (
                    <ul className="space-y-1.5">
                      {result.workloads.map((w) => (
                        <li key={`${w.kind}/${w.name}`} className="text-xs">
                          <span className="font-medium">{w.kind}</span> <span className="font-mono">{w.name}</span>
                          <div className="flex flex-wrap gap-1 mt-0.5">
                            {w.tolerations.map((t) => (
                              <span key={t} className="font-mono rounded bg-muted px-1.5 py-0.5">{t}</span>
                            ))}
                          </div>
                        </li>
                      ))}
                    </ul>
                  )}
                  <button
                    type="button"
                    className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
                    onClick={() => copyText(result.command, "comando")}
                    title="Comando equivalente (o override vai no arquivo tolerations-override.yaml)"
                  >
                    <Copy className="w-3 h-3" /> Copiar o comando helm equivalente
                  </button>
                </div>
              )}
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={!!busy}>Fechar</Button>
            <Button variant="outline" onClick={() => run(true)} disabled={!canRun}>
              {busy === "dry" ? <><Loader2 className="w-4 h-4 mr-1 animate-spin" />Simulando…</> : "Simular (dry-run)"}
            </Button>
            <Button onClick={() => setConfirm(true)} disabled={!canRun || !simulated}
              title={simulated ? undefined : "Simule antes: a aplicação só libera depois de uma simulação com estes mesmos valores"}>
              {busy === "apply" ? <><Loader2 className="w-4 h-4 mr-1 animate-spin" />Aplicando…</> : "Aplicar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Aplicar o helm upgrade?</AlertDialogTitle>
            <AlertDialogDescription>
              O release <span className="font-mono">{namespace}/{release}</span> ({cluster}) será atualizado: a lista de tolerations dos componentes marcados passa a ser a acima.
              O Prometheus, o Alertmanager e os demais componentes podem ser recriados pelo operator.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => { setConfirm(false); void run(false); }}>Aplicar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};
