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
import { PrometheusHelmHistory } from "@/components/PrometheusHelmHistory";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { copyText } from "@/lib/clipboard";
import type { PrometheusHelmResult } from "@/lib/api/types";

interface Toleration {
  key: string;
  operator: "Equal" | "Exists";
  value: string;
  effect: "" | "NoSchedule" | "PreferNoSchedule" | "NoExecute";
}

// Cada linha da lista é editável; `preset` liga a linha ao checkbox (marcar cria a linha, desmarcar remove).
interface Row extends Toleration {
  id: number;
  preset?: string;
}

interface SelRow { id: number; key: string; value: string }

/** Requests/limits de um componente; campo vazio = não mexe (o --reuse-values mantém o que o release já tem). */
export interface Res { cpuReq: string; cpuLim: string; memReq: string; memLim: string }
const emptyRes = (): Res => ({ cpuReq: "", cpuLim: "", memReq: "", memLim: "" });
const RES_FIELDS: { key: keyof Res; label: string; cpu: boolean }[] = [
  { key: "cpuReq", label: "CPU request", cpu: true },
  { key: "cpuLim", label: "CPU limit", cpu: true },
  { key: "memReq", label: "Mem request", cpu: false },
  { key: "memLim", label: "Mem limit", cpu: false },
];
// Quantidade do Kubernetes: 500m, 2, 1.5, 512Mi, 4Gi, 4G…
const reQuantity = /^[0-9]+(\.[0-9]+)?(m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$/;
const validQty = (v: string) => v.trim() === "" || reQuantity.test(v.trim());
const resFilled = (r: Res | undefined) => !!r && Object.values(r).some((v) => v.trim() !== "");

/** Ponto de partida vindo de uma correção de agendamento do relatório do WAL. */
export interface HelmInitial {
  tolerations: { key?: string; operator?: string; value?: string; effect?: string }[];
  nodeSelector: Record<string, string>;
  /** nodeSelector vazio na correção = remover o seletor atual. */
  removeSelector: boolean;
  /** Requests/limits já preenchidos por componente (ex.: o memory limit sugerido pelo diagnóstico). */
  resources?: Record<string, Partial<Res>>;
  /** Componentes marcados ao abrir (padrão: só o Prometheus, no ponto de partida). */
  components?: string[];
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

let rowSeq = 0;
const newRow = (t: Partial<Toleration> = {}, preset?: string): Row => ({
  id: ++rowSeq, preset, key: "", operator: "Equal", value: "", effect: "NoSchedule", ...t,
});

// Linha sem key só vale com operator Exists (tolera tudo); do contrário está incompleta e fica fora do YAML.
const isComplete = (t: Toleration) => t.key.trim() !== "" || t.operator === "Exists";

const sameTol = (a: Toleration, b: Toleration) =>
  a.key === b.key && a.operator === b.operator && a.effect === b.effect && (a.operator === "Exists" || a.value === b.value);

const asToleration = (t: HelmInitial["tolerations"][number]): Toleration => ({
  key: t.key ?? "",
  operator: t.operator === "Exists" ? "Exists" : "Equal",
  value: t.value ?? "",
  effect: (t.effect ?? "") as Toleration["effect"],
});

// Monta o override de values: strings sempre entre aspas (JSON é YAML válido), sem value quando o operador é Exists.
// O nodeSelector vai para todos os componentes marcados (no chart ele fica ao lado das tolerations); `null` remove o
// seletor que o release já tem.
function buildOverrideYaml(tols: Toleration[], components: string[], selector: Record<string, string>, removeSelector: boolean, resources: Record<string, Res>): string {
  const items = tols.map((t) => {
    const lines = [`- key: ${JSON.stringify(t.key)}`, `  operator: ${t.operator}`];
    if (t.operator === "Equal") lines.push(`  value: ${JSON.stringify(t.value)}`);
    if (t.effect) lines.push(`  effect: ${t.effect}`);
    return lines;
  });
  const out: string[] = [];
  for (const c of COMPONENTS.filter((c) => components.includes(c.id))) {
    const selKeys = Object.keys(selector);
    const nullSel = removeSelector && selKeys.length === 0;
    const res = resources[c.id];
    const hasRes = resFilled(res);
    if (items.length === 0 && selKeys.length === 0 && !nullSel && !hasRes) continue;
    c.path.forEach((seg, i) => out.push(`${"  ".repeat(i)}${seg}:`));
    const base = "  ".repeat(c.path.length);
    if (items.length > 0) {
      out.push(`${base}tolerations:`);
      items.forEach((lines) => lines.forEach((l) => out.push(`${base}  ${l}`)));
    }
    if (selKeys.length > 0) {
      out.push(`${base}nodeSelector:`);
      selKeys.forEach((k) => out.push(`${base}  ${JSON.stringify(k)}: ${JSON.stringify(selector[k])}`));
    } else if (nullSel) {
      out.push(`${base}nodeSelector: null`);
    }
    if (hasRes) {
      const block = (name: string, cpu: string, mem: string) => {
        if (!cpu.trim() && !mem.trim()) return;
        out.push(`${base}  ${name}:`);
        if (cpu.trim()) out.push(`${base}    cpu: ${JSON.stringify(cpu.trim())}`);
        if (mem.trim()) out.push(`${base}    memory: ${JSON.stringify(mem.trim())}`);
      };
      out.push(`${base}resources:`);
      block("requests", res.cpuReq, res.memReq);
      block("limits", res.cpuLim, res.memLim);
    }
    out.push("");
  }
  return out.join("\n");
}

// Corrige as tolerations do kube-prometheus-stack com o mesmo `helm upgrade --reuse-values -f override` que o SRE
// roda à mão: primeiro a simulação (--dry-run), depois a aplicação. A tela só mostra o que o chart vai aplicar.
interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  /** Componente do recurso selecionado na aba e seus valores atuais (viram dica nos campos de recursos). */
  current?: { component: string; res: Res } | null;
  /** Preenche a lista com a correção de agendamento escolhida no relatório (valores editáveis). */
  initial?: HelmInitial | null;
  /** Chamado depois de aplicar, para recarregar a lista de recursos. */
  onApplied?: () => void;
}

export const PrometheusHelmTolerationsDialog = ({ open, onOpenChange, cluster, namespace, initial, current, onApplied }: Props) => {
  const [loading, setLoading] = useState(true);
  const [detectError, setDetectError] = useState<string | null>(null);
  const [release, setRelease] = useState("");
  const [chart, setChart] = useState("kube-prometheus-stack");
  const [repo, setRepo] = useState("");
  const [version, setVersion] = useState("");
  const [rows, setRows] = useState<Row[]>([]);
  const [components, setComponents] = useState<string[]>(COMPONENTS.map((c) => c.id));
  const [selRows, setSelRows] = useState<SelRow[]>([]);
  const [removeSelector, setRemoveSelector] = useState(false);
  const [resources, setResources] = useState<Record<string, Res>>({});
  const [tab, setTab] = useState<"editar" | "historico">("editar");
  const [busy, setBusy] = useState<"dry" | "apply" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PrometheusHelmResult | null>(null);
  const [confirm, setConfirm] = useState(false);
  // O que a última simulação usou: qualquer edição depois dela invalida a aplicação.
  const [simulatedWith, setSimulatedWith] = useState<string | null>(null);

  const complete = rows.filter(isComplete);
  const incomplete = rows.length - complete.length;
  const selector = useMemo(() => {
    const m: Record<string, string> = {};
    selRows.forEach((r) => { if (r.key.trim()) m[r.key.trim()] = r.value.trim(); });
    return m;
  }, [selRows]);
  const valuesYaml = useMemo(() => {
    const tols: Toleration[] = [];
    complete.forEach((r) => {
      const t = { key: r.key.trim(), operator: r.operator, value: r.value.trim(), effect: r.effect };
      if (!tols.some((x) => sameTol(x, t))) tols.push(t); // sem duplicata
    });
    return buildOverrideYaml(tols, components, selector, removeSelector, resources);
  }, [complete, components, selector, removeSelector, resources]);
  const badRes = components.some((id) => resources[id] && Object.values(resources[id]).some((v) => !validQty(v)));
  const setRes = (id: string, key: keyof Res, v: string) =>
    setResources((m) => ({ ...m, [id]: { ...(m[id] ?? emptyRes()), [key]: v } }));

  const toggle = (list: string[], id: string) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  const updateRow = (id: number, patch: Partial<Toleration>) =>
    setRows((l) => l.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  const togglePreset = (id: string) =>
    setRows((l) => {
      if (l.some((r) => r.preset === id)) return l.filter((r) => r.preset !== id);
      const p = PRESETS.find((x) => x.id === id)!;
      return [...l, newRow(p.tol, id)];
    });

  const signature = JSON.stringify([release, chart, repo, version, valuesYaml]);
  const simulated = result?.dry_run === true && simulatedWith === signature;

  useEffect(() => {
    if (!open) return;
    let alive = true;
    setLoading(true);
    setError(null);
    setResult(null);
    setSimulatedWith(null);
    setTab("editar");
    setResources(Object.fromEntries(Object.entries(initial?.resources ?? {}).map(([id, r]) => [id, { ...emptyRes(), ...r }])));
    if (initial) {
      // Correção de agendamento: as tolerations atuais + as da opção, e o nodeSelector resultante; só o Prometheus.
      setRows(initial.tolerations.map((t) => {
        const tol = asToleration(t);
        const preset = PRESETS.find((p) => sameTol(p.tol, tol))?.id;
        return newRow(tol, preset);
      }));
      setSelRows(Object.entries(initial.nodeSelector).map(([key, value], i) => ({ id: i + 1, key, value })));
      setRemoveSelector(initial.removeSelector);
      setComponents(initial.components ?? ["prometheus"]); // a correção de agendamento é do Prometheus; os outros ficam desmarcados
    } else {
      setRows(PRESETS.map((p) => newRow(p.tol, p.id)));
      setSelRows([]);
      setRemoveSelector(false);
      setComponents(COMPONENTS.map((c) => c.id));
    }
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
  }, [open, cluster, namespace, initial]);

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

  const canRun = !loading && !busy && release.trim() !== "" && valuesYaml.trim() !== "" && !badRes;

  return (
    <>
      <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Prometheus via Helm</DialogTitle>
            <DialogDescription>
              Roda <span className="font-mono">helm upgrade --reuse-values -f override</span> no release do kube-prometheus-stack
              de <span className="font-mono">{namespace}</span> ({cluster}): os values atuais são mantidos (recursos e nodeSelector
              são mesclados), mas a lista de tolerations de cada componente marcado é <b>substituída</b> pela que está abaixo (o helm
              não soma listas). Simule antes de aplicar; cada aplicação vira uma revisão do release.
            </DialogDescription>
          </DialogHeader>

          <div className="flex gap-1 border-b flex-shrink-0">
            {([["editar", "Editar values"], ["historico", "Revisões / rollback"]] as const).map(([id, label]) => (
              <button key={id} type="button" onClick={() => setTab(id)}
                className={`px-3 py-1.5 text-xs whitespace-nowrap border-b-2 -mb-px ${tab === id ? "border-indigo-500 font-medium" : "border-transparent text-muted-foreground hover:text-foreground"}`}>
                {label}
              </button>
            ))}
          </div>

          {loading ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground py-6 justify-center">
              <Loader2 className="w-4 h-4 animate-spin" /> Procurando o release no namespace…
            </div>
          ) : tab === "historico" ? (
            release.trim() ? (
              <PrometheusHelmHistory cluster={cluster} namespace={namespace} release={release.trim()} onRolledBack={onApplied} />
            ) : (
              <p className="text-xs text-muted-foreground py-4">Informe o nome do release na aba "Editar values" para ver as revisões.</p>
            )
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
                <Label className="text-xs">Tolerations (a lista do componente passa a ser exatamente esta)</Label>
                <div className="flex flex-wrap gap-x-4 gap-y-1">
                  {PRESETS.map((p) => (
                    <label key={p.id} className="flex items-center gap-1.5 text-xs cursor-pointer">
                      <Checkbox checked={rows.some((r) => r.preset === p.id)} onCheckedChange={() => togglePreset(p.id)} />
                      <span className="font-medium">{p.label}</span>
                    </label>
                  ))}
                </div>

                {rows.map((t) => (
                  <div key={t.id} className="grid grid-cols-[1.6fr_0.9fr_1fr_1.2fr_auto] gap-2 items-center">
                    <Input value={t.key} onChange={(e) => updateRow(t.id, { key: e.target.value })}
                      placeholder="key" className={`h-8 text-xs font-mono ${isComplete(t) ? "" : "border-amber-500"}`} />
                    <Select value={t.operator} onValueChange={(v) => updateRow(t.id, { operator: v as Toleration["operator"] })}>
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="Equal">Equal</SelectItem>
                        <SelectItem value="Exists">Exists</SelectItem>
                      </SelectContent>
                    </Select>
                    <Input value={t.value} onChange={(e) => updateRow(t.id, { value: e.target.value })}
                      placeholder="value" disabled={t.operator === "Exists"} className="h-8 text-xs font-mono" />
                    <Select value={t.effect || "all"} onValueChange={(v) => updateRow(t.id, { effect: v === "all" ? "" : (v as Toleration["effect"]) })}>
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="NoSchedule">NoSchedule</SelectItem>
                        <SelectItem value="PreferNoSchedule">PreferNoSchedule</SelectItem>
                        <SelectItem value="NoExecute">NoExecute</SelectItem>
                        <SelectItem value="all">(todos os effects)</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button type="button" size="icon" variant="ghost" className="h-8 w-8"
                      onClick={() => setRows((l) => l.filter((r) => r.id !== t.id))} title="Remover">
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                ))}
                <Button type="button" size="sm" variant="outline" className="h-7 text-xs"
                  onClick={() => setRows((l) => [...l, newRow()])}>
                  <Plus className="w-3.5 h-3.5 mr-1" /> Custom
                </Button>
                {incomplete > 0 && (
                  <p className="text-[11px] text-amber-600">
                    {incomplete} linha(s) sem key ficam fora do YAML (preencha a key ou use o operador Exists).
                  </p>
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

              <div className="space-y-2">
                <Label className="text-xs">Recursos (requests/limits) — campo vazio mantém o valor atual do release</Label>
                {COMPONENTS.filter((c) => components.includes(c.id)).map((c) => {
                  const r = resources[c.id] ?? emptyRes();
                  const hint = current?.component === c.id ? current.res : null;
                  return (
                    <div key={c.id} className="grid grid-cols-[1.2fr_repeat(4,1fr)] gap-2 items-center">
                      <span className="text-xs font-medium">{c.label}</span>
                      {RES_FIELDS.map((f) => (
                        <Input key={f.key} value={r[f.key]} onChange={(e) => setRes(c.id, f.key, e.target.value)}
                          placeholder={hint?.[f.key] ? `${f.label} (atual ${hint[f.key]})` : f.label}
                          title={f.cpu ? "ex.: 500m, 2" : "ex.: 512Mi, 4Gi"}
                          className={`h-8 text-xs font-mono ${validQty(r[f.key]) ? "" : "border-red-500"}`} />
                      ))}
                    </div>
                  );
                })}
                {badRes && <p className="text-[11px] text-red-600">Quantidade inválida (use formatos como 500m, 2, 512Mi, 4Gi).</p>}
              </div>

              {(
                <div className="space-y-2">
                  <Label className="text-xs">nodeSelector (opcional; vale para todos os componentes marcados)</Label>
                  {selRows.map((r) => (
                    <div key={r.id} className="grid grid-cols-[1.6fr_1.6fr_auto] gap-2 items-center">
                      <Input value={r.key} onChange={(e) => setSelRows((l) => l.map((x) => (x.id === r.id ? { ...x, key: e.target.value } : x)))}
                        placeholder="label" className="h-8 text-xs font-mono" />
                      <Input value={r.value} onChange={(e) => setSelRows((l) => l.map((x) => (x.id === r.id ? { ...x, value: e.target.value } : x)))}
                        placeholder="valor" className="h-8 text-xs font-mono" />
                      <Button type="button" size="icon" variant="ghost" className="h-8 w-8"
                        onClick={() => setSelRows((l) => l.filter((x) => x.id !== r.id))} title="Remover">
                        <Trash2 className="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  ))}
                  <div className="flex items-center gap-3">
                    <Button type="button" size="sm" variant="outline" className="h-7 text-xs"
                      onClick={() => setSelRows((l) => [...l, { id: Date.now(), key: "", value: "" }])}>
                      <Plus className="w-3.5 h-3.5 mr-1" /> nodeSelector
                    </Button>
                    {selRows.length === 0 && (
                      <label className="flex items-center gap-1.5 text-xs cursor-pointer">
                        <Checkbox checked={removeSelector} onCheckedChange={(v) => setRemoveSelector(v === true)} />
                        remover o nodeSelector atual dos componentes marcados (<span className="font-mono">nodeSelector: null</span>)
                      </label>
                    )}
                  </div>
                </div>
              )}

              <div className="space-y-1">
                <Label className="text-xs">Override gerado (tolerations-override.yaml) — reflete as linhas acima</Label>
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
                            {w.tolerations?.map((t) => (
                              <span key={t} className="font-mono rounded bg-muted px-1.5 py-0.5">{t}</span>
                            ))}
                            {w.resources?.map((r) => (
                              <span key={r} className="font-mono rounded bg-emerald-500/15 px-1.5 py-0.5">{r}</span>
                            ))}
                            {w.node_selector?.map((n) => (
                              <span key={n} className="font-mono rounded bg-indigo-500/15 px-1.5 py-0.5">nodeSelector {n}</span>
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
            {tab === "editar" && (<>
            <Button variant="outline" onClick={() => run(true)} disabled={!canRun}>
              {busy === "dry" ? <><Loader2 className="w-4 h-4 mr-1 animate-spin" />Simulando…</> : "Simular (dry-run)"}
            </Button>
            <Button onClick={() => setConfirm(true)} disabled={!canRun || !simulated}
              title={simulated ? undefined : "Simule antes: a aplicação só libera depois de uma simulação com estes mesmos valores"}>
              {busy === "apply" ? <><Loader2 className="w-4 h-4 mr-1 animate-spin" />Aplicando…</> : "Aplicar"}
            </Button>
            </>)}
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
