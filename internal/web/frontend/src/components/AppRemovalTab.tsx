import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Check, CheckCircle2, ChevronsUpDown, FlaskConical, Loader2, PackageX, RefreshCw, ScanSearch, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
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
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { apiClient } from "@/lib/api/client";
import { useClusters } from "@/hooks/useAPI";
import { cn } from "@/lib/utils";
import type {
  AppRemovalCandidate,
  AppRemovalItem,
  AppRemovalOrigin,
  AppRemovalOutcome,
  AppRemovalScanResult,
  AppRemovalSSEEvent,
  AppRemovalStatus,
} from "@/lib/api/types";

const ORIGIN_LABEL: Record<AppRemovalOrigin, string> = {
  helm: "Helm",
  kustomize: "Kustomize",
  "kustomize-provavel": "Kustomize (provável)",
  manual: "Manual",
};

function SearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  emptyMessage,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  placeholder: string;
  searchPlaceholder: string;
  emptyMessage: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const selected = options.find((o) => o.value === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">{selected?.label || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt.value}
                  value={opt.label}
                  onSelect={() => {
                    onChange(opt.value === value ? "" : opt.value);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === opt.value ? "opacity-100" : "opacity-0")} />
                  {opt.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function candidateKey(c: AppRemovalCandidate) {
  return `${c.kind}/${c.name}`;
}

const itemKey = (i: { kind: string; name: string }) => `${i.kind}/${i.name}`;

const STATUS_LABEL: Record<AppRemovalStatus, { text: string; className: string }> = {
  deleted: { text: "Removido", className: "text-green-600 dark:text-green-400" },
  simulated: { text: "Simulado", className: "text-blue-600 dark:text-blue-400" },
  not_found: { text: "Já não existia", className: "text-muted-foreground" },
  skipped: { text: "Não executado", className: "text-amber-600 dark:text-amber-400" },
  failed: { text: "Falhou", className: "text-red-600 dark:text-red-400" },
};

interface RunState {
  operationId: string;
  dryRun: boolean;
  total: number;
  progress: number;
  outcomes: Record<string, AppRemovalOutcome>;
  finished: boolean;
  failed: boolean;
  error?: string;
}

interface VerificationSummary {
  removed: number;
  total: number;
  appGone: boolean;
  leftovers: number;
}

function ItemRow({
  item,
  checked,
  onToggle,
  locked,
  outcome,
}: {
  item: AppRemovalItem;
  checked: boolean;
  onToggle: () => void;
  locked: boolean;
  outcome?: AppRemovalOutcome;
}) {
  const coveredByHelm = item.removed_by_helm;
  return (
    <tr className="border-t align-top">
      <td className="px-3 py-2 w-8">
        <Checkbox
          checked={coveredByHelm ? false : checked}
          disabled={coveredByHelm || locked}
          onCheckedChange={onToggle}
          aria-label={`Selecionar ${item.kind} ${item.name}`}
        />
      </td>
      <td className="px-3 py-2 font-mono text-xs">
        <span className="text-muted-foreground">{item.kind}/</span>
        {item.name}
      </td>
      <td className="px-3 py-2">
        <Badge variant="outline" className="text-[10px]">
          {ORIGIN_LABEL[item.origin]}
        </Badge>
      </td>
      <td className="px-3 py-2 text-xs text-muted-foreground">
        {item.reason}
        {item.used_by && item.used_by.length > 0 && (
          <div className="mt-1 text-amber-600 dark:text-amber-400">
            Também usado por: {item.used_by.join(", ")}
          </div>
        )}
        {item.finalizers && item.finalizers.length > 0 && (
          <div className="mt-1 text-amber-600 dark:text-amber-400">Finalizers: {item.finalizers.join(", ")}</div>
        )}
        {outcome?.message && <div className="mt-1 text-red-600 dark:text-red-400">{outcome.message}</div>}
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap gap-1">
          {item.shared && <Badge variant="outline" className="text-[10px] border-amber-500 text-amber-600">Compartilhado</Badge>}
          {item.data_risk && <Badge variant="outline" className="text-[10px] border-red-500 text-red-600">Dados</Badge>}
          {coveredByHelm && <Badge variant="outline" className="text-[10px]">Removido pelo Helm</Badge>}
          {item.state === "terminating" && <Badge variant="outline" className="text-[10px] border-amber-500 text-amber-600">Terminating</Badge>}
          {item.state === "recreated" && <Badge variant="outline" className="text-[10px] border-red-500 text-red-600">Recriado</Badge>}
          {item.state === "new" && <Badge variant="outline" className="text-[10px] border-blue-500 text-blue-600">Novo</Badge>}
        </div>
      </td>
      <td className="px-3 py-2 text-xs whitespace-nowrap">
        {outcome && <span className={STATUS_LABEL[outcome.status].className}>{STATUS_LABEL[outcome.status].text}</span>}
      </td>
    </tr>
  );
}

/**
 * AppRemovalTab — "Remoção de Aplicação" (Tools menu). Descobre tudo o que pertence a uma
 * aplicação (workloads, Services, Ingress, config, dados, RBAC), indica o gerenciador (Helm /
 * Kustomize provável / manual) e a seleção sugerida. O usuário marca o que remover, pode simular
 * (dry-run) e confirma digitando o nome da aplicação; o progresso chega por SSE.
 */
export default function AppRemovalTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [target, setTarget] = useState("");
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState<string | null>(null);
  const [result, setResult] = useState<AppRemovalScanResult | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [run, setRun] = useState<RunState | null>(null);
  const [verification, setVerification] = useState<VerificationSummary | null>(null);
  const [verifying, setVerifying] = useState(false);
  const verifiedRef = useRef<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmName, setConfirmName] = useState("");
  const esRef = useRef<EventSource | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["app-removal-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const { data: candidatesResp, isLoading: loadingCandidates, isError: candidatesError } = useQuery({
    queryKey: ["app-removal-candidates", cluster, namespace],
    queryFn: () => apiClient.listAppRemovalCandidates(cluster, namespace),
    enabled: !!cluster && !!namespace,
  });
  const candidates = candidatesResp?.candidates ?? [];
  const targetCandidate = candidates.find((c) => candidateKey(c) === target);

  const resetResult = () => {
    setResult(null);
    setScanError(null);
    setRun(null);
    setVerification(null);
    setSelected(new Set());
  };

  // Confere o namespace contra o scan guardado; as sobras viram a nova lista de seleção.
  const verify = async (scanId: string) => {
    setVerifying(true);
    try {
      const v = await apiClient.verifyAppRemoval(scanId);
      setVerification({
        removed: v.items.filter((i) => i.state === "removed").length,
        total: v.items.length,
        appGone: v.app_gone,
        leftovers: v.leftovers?.items.length ?? 0,
      });
      if (v.app_gone || !v.leftovers) {
        setResult(null);
        setSelected(new Set());
      } else {
        setResult(v.leftovers);
        setSelected(new Set(v.leftovers.items.filter((i) => i.default_selected).map(itemKey)));
      }
    } catch (e) {
      toast.error("Falha ao verificar o namespace", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setVerifying(false);
    }
  };

  // Ao terminar uma remoção real, verifica sozinho (uma vez por operação).
  useEffect(() => {
    if (!run?.finished || run.dryRun || !result?.scan_id) return;
    if (verifiedRef.current === run.operationId) return;
    verifiedRef.current = run.operationId;
    void verify(result.scan_id);
  }, [run?.finished, run?.operationId, run?.dryRun, result?.scan_id]);

  const handleScan = async () => {
    if (!targetCandidate) return;
    setScanning(true);
    resetResult();
    try {
      const res = await apiClient.scanAppRemoval(cluster, namespace, targetCandidate.name);
      setResult(res);
      setSelected(new Set(res.items.filter((i) => i.default_selected).map(itemKey)));
    } catch (e) {
      setScanError(e instanceof Error ? e.message : "Erro desconhecido");
    } finally {
      setScanning(false);
    }
  };

  const running = !!run && !run.finished;
  const selectedItems = result ? result.items.filter((i) => selected.has(itemKey(i)) && !i.removed_by_helm) : [];
  const helmSelected = selectedItems.some((i) => i.kind === "HelmRelease");
  const coveredByHelm = helmSelected && result ? result.items.filter((i) => i.removed_by_helm) : [];

  const toggle = (item: AppRemovalItem) => {
    setSelected((prev) => {
      const next = new Set(prev);
      const k = itemKey(item);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  };

  // SSE de progresso: um EventSource por operação, fechado ao terminar ou desmontar.
  useEffect(() => {
    const operationId = run?.operationId;
    if (!operationId || run?.finished) return;
    const es = new EventSource(apiClient.getAppRemovalStreamURL(operationId));
    esRef.current = es;
    es.onmessage = (e) => {
      try {
        const event: AppRemovalSSEEvent = JSON.parse(e.data);
        setRun((prev) => {
          if (!prev || prev.operationId !== operationId) return prev;
          if (event.type === "item" && event.result && "status" in event.result) {
            const o = event.result as AppRemovalOutcome;
            return { ...prev, progress: event.progress, outcomes: { ...prev.outcomes, [itemKey(o)]: o } };
          }
          if (event.type === "complete") {
            const r = event.result as { failed: boolean; outcomes: AppRemovalOutcome[] } | undefined;
            const outcomes = { ...prev.outcomes };
            r?.outcomes.forEach((o) => (outcomes[itemKey(o)] = o));
            return { ...prev, progress: 1, outcomes, finished: true, failed: !!r?.failed };
          }
          if (event.type === "error") {
            return { ...prev, finished: true, failed: true, error: event.error || event.message };
          }
          return prev;
        });
      } catch {
        /* ignora evento malformado */
      }
    };
    es.onerror = () => {
      es.close();
      setRun((prev) =>
        prev && prev.operationId === operationId && !prev.finished
          ? { ...prev, finished: true, failed: true, error: "Conexão de progresso perdida. Escaneie novamente para ver o estado real." }
          : prev
      );
    };
    return () => {
      es.close();
      esRef.current = null;
    };
  }, [run?.operationId, run?.finished]);

  const startExecution = async (dryRun: boolean) => {
    if (!result || selectedItems.length === 0) return;
    setStarting(true);
    try {
      const res = await apiClient.executeAppRemoval({
        cluster,
        namespace,
        name: result.name,
        items: selectedItems.map((i) => ({ kind: i.kind, name: i.name, uid: i.uid })),
        dry_run: dryRun,
        confirm_name: dryRun ? "" : confirmName,
        scan_id: result.scan_id,
      });
      setRun({
        operationId: res.operation_id,
        dryRun,
        total: res.total,
        progress: 0,
        outcomes: {},
        finished: false,
        failed: false,
      });
      setConfirmOpen(false);
      setConfirmName("");
    } catch (e) {
      toast.error(dryRun ? "Falha ao iniciar a simulação" : "Falha ao iniciar a remoção", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setStarting(false);
    }
  };

  // Grupos = etapas do plano de remoção, na ordem em que serão executadas.
  const groups = result
    ? result.items.reduce<{ step: string; items: AppRemovalItem[] }[]>((acc, item) => {
        const last = acc[acc.length - 1];
        if (last && last.step === item.step) last.items.push(item);
        else acc.push({ step: item.step, items: [item] });
        return acc;
      }, [])
    : [];

  // Controllers que ficam de pé podem recriar ou continuar usando o que for removido.
  const controllerKinds = ["Deployment", "StatefulSet", "DaemonSet", "CronJob"];
  const keptControllers =
    result && !helmSelected
      ? result.items.filter((i) => controllerKinds.includes(i.kind) && !selected.has(itemKey(i)))
      : [];
  const dependsOnControllers = ["Service", "ExternalSecret", "ConfigMap", "Secret", "PersistentVolumeClaim"];
  const removesWhatControllersUse = selectedItems.some((i) => dependsOnControllers.includes(i.kind));

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-4">
      <div className="flex items-center gap-2">
        <PackageX className="h-5 w-5 text-primary" />
        <h1 className="text-lg font-semibold">Remoção de Aplicação</h1>
      </div>
      <p className="text-sm text-muted-foreground">
        Descobre tudo o que pertence a uma aplicação no namespace (workloads, Services, Ingress,
        config, dados e RBAC) e se ela é gerenciada por Helm ou Kustomize. Marque o que remover,
        simule se quiser e confirme digitando o nome da aplicação. Depois, escaneie de novo para
        conferir o que restou.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div>
          <Label className="text-xs">Cluster</Label>
          <div className="mt-1">
            <SearchableSelect
              value={cluster}
              onChange={(v) => {
                setCluster(v);
                setNamespace("");
                setTarget("");
                resetResult();
              }}
              options={clusters.map((c) => ({ value: c.context, label: c.context }))}
              placeholder="Selecione..."
              searchPlaceholder="Buscar cluster..."
              emptyMessage="Nenhum cluster encontrado."
            />
          </div>
        </div>
        <div>
          <Label className="text-xs">Namespace</Label>
          <div className="mt-1">
            <SearchableSelect
              value={namespace}
              onChange={(v) => {
                setNamespace(v);
                setTarget("");
                resetResult();
              }}
              options={namespaces.map((n) => ({ value: n.name, label: n.name }))}
              placeholder="Selecione..."
              searchPlaceholder="Buscar namespace..."
              emptyMessage="Nenhum namespace encontrado."
              disabled={!cluster}
            />
          </div>
        </div>
        <div>
          <Label className="text-xs">Aplicação</Label>
          <div className="mt-1">
            <SearchableSelect
              value={target}
              onChange={(v) => {
                setTarget(v);
                resetResult();
              }}
              options={candidates.map((c) => ({
                value: candidateKey(c),
                label:
                  c.kind === "HelmRelease"
                    ? `${c.name} (release Helm${c.chart ? ` · ${c.chart}` : ""}${c.status && c.status !== "deployed" ? ` · ${c.status}` : ""})`
                    : `${c.name} (${c.kind})`,
              }))}
              placeholder={loadingCandidates ? "Carregando..." : "Selecione..."}
              searchPlaceholder="Buscar aplicação..."
              emptyMessage="Nenhum workload encontrado."
              disabled={!namespace || loadingCandidates}
            />
          </div>
        </div>
      </div>

      {candidatesError && (
        <Alert variant="destructive">
          <AlertTitle>Falha ao listar aplicações</AlertTitle>
          <AlertDescription>Verifique o acesso ao cluster e tente novamente.</AlertDescription>
        </Alert>
      )}

      <Button onClick={handleScan} disabled={!targetCandidate || scanning}>
        {scanning ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <ScanSearch className="h-4 w-4 mr-2" />}
        Escanear dependências
      </Button>

      {scanError && (
        <Alert variant="destructive">
          <AlertTitle>Falha no scan</AlertTitle>
          <AlertDescription>{scanError}</AlertDescription>
        </Alert>
      )}

      {verifying && (
        <Alert>
          <Loader2 className="h-4 w-4 animate-spin" />
          <AlertDescription>Verificando o namespace (itens em Terminating podem levar alguns segundos)...</AlertDescription>
        </Alert>
      )}

      {verification && !verifying && (
        <Alert className={verification.appGone ? "border-green-500" : "border-amber-500"}>
          {verification.appGone ? <CheckCircle2 className="h-4 w-4 text-green-600" /> : <AlertTriangle className="h-4 w-4" />}
          <AlertTitle>
            {verification.appGone ? "Aplicação totalmente removida" : `Sobrou ${verification.leftovers} item(ns)`}
          </AlertTitle>
          <AlertDescription className="flex flex-wrap items-center gap-2">
            <span>
              {verification.removed} de {verification.total} itens removidos
              {verification.appGone
                ? ". Nada restou no namespace."
                : ". Os que restam estão abaixo: marque o que ainda deve sair ou deixe como está."}
            </span>
            {!verification.appGone && result?.scan_id && (
              <Button size="sm" variant="outline" disabled={running} onClick={() => verify(result.scan_id!)}>
                <RefreshCw className="h-3 w-3 mr-1" />
                Verificar novamente
              </Button>
            )}
          </AlertDescription>
        </Alert>
      )}

      {result && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium">{result.name}</span>
            <Badge variant="outline">{ORIGIN_LABEL[result.origin]}</Badge>
            {result.helm && (
              <span className="text-muted-foreground">
                release <code>{result.helm.release}</code>
                {result.helm.chart && <> · chart <code>{result.helm.chart}</code></>}
                {result.helm.status && result.helm.status !== "deployed" && <> · {result.helm.status}</>} ·{" "}
                {result.helm.revisions} revisões
              </span>
            )}
            <span className="text-muted-foreground">
              · {result.items.length} itens · {selectedItems.length} selecionado(s)
            </span>
          </div>

          {result.warnings.map((w) => (
            <Alert key={w}>
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>{w}</AlertDescription>
            </Alert>
          ))}

          {run && (
            <div className="rounded-md border p-3 space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium">
                  {run.dryRun ? "Simulação" : "Remoção"}
                  {run.finished ? (run.failed ? " — terminou com falha" : " — concluída") : " em andamento..."}
                </span>
                <span className="text-xs text-muted-foreground">
                  {Object.keys(run.outcomes).length}/{run.total}
                </span>
              </div>
              <Progress value={run.progress * 100} />
              {run.error && <p className="text-xs text-red-600 dark:text-red-400">{run.error}</p>}
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            A remoção segue a ordem das etapas abaixo: primeiro o que recria (release Helm, autoscaling, CronJobs,
            controllers com espera pela saída dos pods), depois o que é recriado (ExternalSecrets, Secrets, PVCs).
          </p>

          <div className="flex flex-wrap gap-2">
            <ProtectedAction>
              <Button
                variant="outline"
                disabled={selectedItems.length === 0 || running || starting}
                onClick={() => startExecution(true)}
              >
                <FlaskConical className="h-4 w-4 mr-2" />
                Simular (dry-run)
              </Button>
              <Button
                variant="destructive"
                className="ml-2"
                disabled={selectedItems.length === 0 || running || starting}
                onClick={() => setConfirmOpen(true)}
              >
                <Trash2 className="h-4 w-4 mr-2" />
                Remover selecionados ({selectedItems.length})
              </Button>
            </ProtectedAction>
          </div>

          {groups.map((g, gi) => (
            <div key={g.step} className="rounded-md border overflow-hidden">
              <div className="bg-muted px-3 py-1.5 text-xs font-semibold">
                Etapa {gi + 1} · {g.step}{" "}
                <span className="text-muted-foreground font-normal">({g.items.length})</span>
              </div>
              <table className="w-full text-left">
                <thead className="sr-only">
                  <tr>
                    <th>Selecionar</th>
                    <th>Nome</th>
                    <th>Origem</th>
                    <th>Motivo</th>
                    <th>Avisos</th>
                    <th>Resultado</th>
                  </tr>
                </thead>
                <tbody>
                  {g.items.map((item) => (
                    <ItemRow
                      key={itemKey(item)}
                      item={item}
                      checked={selected.has(itemKey(item))}
                      onToggle={() => toggle(item)}
                      locked={running}
                      outcome={run?.outcomes[itemKey(item)]}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      )}

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(open) => {
          if (!starting) {
            setConfirmOpen(open);
            if (!open) setConfirmName("");
          }
        }}
      >
        <AlertDialogContent className="max-w-2xl">
          <AlertDialogHeader>
            <AlertDialogTitle>Remover {selectedItems.length} item(ns) de {result?.name}?</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 text-sm">
                <p>
                  Cluster <code>{cluster}</code>, namespace <code>{namespace}</code>. A remoção não pode ser
                  desfeita e para na primeira falha.
                </p>
                <ul className="max-h-56 overflow-y-auto rounded border p-2 space-y-1 text-xs font-mono">
                  {selectedItems.map((i) => (
                    <li key={itemKey(i)} className="flex flex-wrap items-center gap-2">
                      <span>{i.kind}/{i.name}</span>
                      {i.data_risk && <Badge variant="outline" className="text-[10px] border-red-500 text-red-600">Dados</Badge>}
                      {i.shared && <Badge variant="outline" className="text-[10px] border-amber-500 text-amber-600">Compartilhado</Badge>}
                    </li>
                  ))}
                </ul>
                {helmSelected && (
                  <p className="text-amber-600 dark:text-amber-400">
                    O <code>helm uninstall</code> também remove {coveredByHelm.length} recurso(s) do release
                    {coveredByHelm.some((i) => i.data_risk) && ", incluindo Secrets/PVCs"}.
                  </p>
                )}
                {keptControllers.length > 0 && removesWhatControllersUse && (
                  <p className="text-amber-600 dark:text-amber-400">
                    Os controllers {keptControllers.map((i) => `${i.kind}/${i.name}`).join(", ")} não estão
                    selecionados e podem recriar ou continuar usando o que será removido.
                  </p>
                )}
                {selectedItems.some((i) => i.data_risk) && (
                  <p className="text-red-600 dark:text-red-400">
                    A seleção inclui Secrets ou PVCs: os dados serão apagados.
                  </p>
                )}
                {selectedItems.some((i) => i.shared) && (
                  <p className="text-red-600 dark:text-red-400">
                    A seleção inclui itens também usados por outra aplicação.
                  </p>
                )}
                <div>
                  <Label className="text-xs">
                    Digite <code>{result?.name}</code> para confirmar
                  </Label>
                  <Input
                    className="mt-1"
                    value={confirmName}
                    onChange={(e) => setConfirmName(e.target.value)}
                    autoComplete="off"
                  />
                </div>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={starting}>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={confirmName !== result?.name || starting}
              onClick={(e) => {
                e.preventDefault();
                startExecution(false);
              }}
            >
              {starting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Remover
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
