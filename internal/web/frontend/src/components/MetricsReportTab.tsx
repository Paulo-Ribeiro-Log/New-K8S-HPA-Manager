import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, ChevronDown, ChevronRight, ChevronsUpDown, Copy, FileDown, FileSpreadsheet, FileText, Loader2, Play, Plus, RefreshCw, Save, Square, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { MetricsLivePanel } from "@/components/MetricsLivePanel";
import { MetricsEventPicker } from "@/components/MetricsEventPicker";
import { MetricsComparePanel } from "@/components/MetricsComparePanel";
import { useReportEvents } from "@/hooks/useReportEvents";
import { ClusterFilterBar } from "@/components/ClusterFilterBar";
import { ClusterJourneyCombobox } from "@/components/ClusterJourneyCombobox";
import { useClusterEnvFilter } from "@/hooks/useClusterEnvFilter";
import { cloudProviderBadge } from "@/hooks/useCloudProvider";
import { isProdClusterName } from "@/lib/clusterSafety";
import { cn } from "@/lib/utils";
import { apiClient } from "@/lib/api/client";
import { copyText } from "@/lib/clipboard";
import { buildCSV, buildMarkdown, downloadText, exportReportPDF, fileStamp, type CsvDelimiter } from "@/lib/metricsReportExport";
import {
  EXTRAS_COLUMNS, EXTRAS_KEY, extrasCells, parseExtras, FLAG_ICON, NODE_CONTEXT_KEY, PERCENTILES_KEY, PERCENTILE_CHOICES, REPORT_TZ, SAVED_SETS_KEY, TARGETS_KEY, mergeSets, removePickedItem, allItems, fmtInt, groupRows, fmtLatency, fmtPctStats, fmtRps, isoToZonedLocal, newTarget,
  fmtNodeUsage, fmtPools, fmtThrottle, parseNodeContext, parsePercentiles, parseSets, parseTargets, percentileValue, pickKey, presetWindow, setTargetCluster, targetItems, targetSummary, togglePercentile,
  setTargetNamespaces, setTargetPicked, upsertSet, zonedLocalToISO, type ReportAlerts, type ReportResult, type ReportRow, type SavedSet, type Target, type WindowPreset,
} from "@/lib/metricsReport";

// Relatório de métricas por aplicação: o operador monta uma lista de ALVOS (cada alvo = um cluster com
// os namespaces e deployments escolhidos), define a janela de tempo e vê o relatório (tráfego, latência
// e erros do Dynatrace; CPU e memória do Prometheus). Independente de outras abas. Somente leitura —
// plano em MONITORING-REPORT-PLAN.md.

const MAX_ITEMS = 200;
const LIST_STALE = 2 * 60 * 1000;
const PRESETS: { id: WindowPreset; label: string }[] = [
  { id: "1h", label: "Última 1 h" },
  { id: "3h", label: "Últimas 3 h" },
  { id: "6h", label: "Últimas 6 h" },
  { id: "12h", label: "Últimas 12 h" },
  { id: "24h", label: "Últimas 24 h" },
  { id: "7d", label: "Últimos 7 dias" },
  { id: "14d", label: "Últimos 14 dias" },
];
// consultas das listas (clusters, namespaces, deployments): o botão de recarregar invalida só estas
const LIST_KEYS = ["metrics-report-clusters", "metrics-report-namespaces", "metrics-report-deployments"];

function load<T>(key: string, parse: (raw: string | null) => T): T {
  try {
    return parse(localStorage.getItem(key));
  } catch {
    return parse(null);
  }
}

function save(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* sem armazenamento: a tela segue funcionando sem lembrar */
  }
}

const errText = (e: unknown) => (e instanceof Error ? e.message : "erro desconhecido");

// ── Seletores do alvo ──────────────────────────────────────────────────────────────────────────────

interface ClusterInfo {
  names: string[];
  providers: Record<string, string>;
  journeys: Record<string, string>;
}

/** Combobox de cluster com o filtro Todos/HLG/PRD e a jornada — mesmos componentes do seletor das outras abas. A escolha só vale ao clicar em Aplicar. */
function ClusterPicker({
  value, info, loading, hasSelection, onSelect,
}: { value: string; info: ClusterInfo; loading: boolean; hasSelection: boolean; onSelect: (cluster: string) => void }) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value);
  const { envFilter, setEnvFilter, journeyOptions, selectedJourneys, toggleJourney, filteredClusters } = useClusterEnvFilter(info.names, info.journeys);
  const searchRef = useRef<HTMLInputElement>(null);
  const changed = !!draft && draft !== value;
  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        if (o) setDraft(value); // abre com a escolha atual; fechar sem aplicar descarta o rascunho
        setOpen(o);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="outline" role="combobox" aria-expanded={open} disabled={loading} className="h-8 w-full justify-between text-xs">
          <span className={cn("truncate", value && isProdClusterName(value) && "text-amber-600 dark:text-amber-400 font-semibold")}>
            {loading ? "Carregando clusters…" : value || "Escolher cluster…"}
          </span>
          <ChevronsUpDown className="ml-2 h-3.5 w-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[320px] p-0" onOpenAutoFocus={(e) => { e.preventDefault(); searchRef.current?.focus(); }}>
        <ClusterFilterBar envFilter={envFilter} onEnvFilterChange={setEnvFilter} />
        <div className="px-2 pt-1 empty:hidden">
          <ClusterJourneyCombobox variant="light" journeyOptions={journeyOptions} selectedJourneys={selectedJourneys} onToggleJourney={toggleJourney} />
        </div>
        <Command>
          <CommandInput ref={searchRef} placeholder="Buscar cluster…" />
          <CommandList>
            <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
            <CommandGroup>
              {filteredClusters.map((c) => {
                const badge = cloudProviderBadge(info.providers[c]);
                return (
                  <CommandItem key={c} value={c} onSelect={() => setDraft(c)}>
                    <Check className={cn("mr-2 h-4 w-4", draft === c ? "opacity-100" : "opacity-0")} />
                    {badge && <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded mr-1.5 ${badge.className}`}>{badge.label}</span>}
                    <span className={isProdClusterName(c) ? "text-amber-600 dark:text-amber-400 font-medium" : undefined}>{c}</span>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </CommandList>
        </Command>
        <div className="border-t border-border p-2 space-y-1">
          {changed && hasSelection && <div className="text-[10px] text-amber-400">Trocar o cluster limpa os namespaces e deployments deste alvo.</div>}
          <div className="flex items-center justify-end gap-1">
            <Button size="sm" variant="ghost" className="h-6 text-[11px]" onClick={() => setOpen(false)}>Cancelar</Button>
            <Button size="sm" className="h-6 text-[11px]" disabled={!changed} onClick={() => { onSelect(draft); setOpen(false); }}>Aplicar</Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** Seletor múltiplo genérico: botão com o resumo e popover com busca. As marcações ficam em rascunho e só valem ao clicar em Aplicar. */
function MultiPicker({
  label, placeholder, disabled, loading, error, emptyText, groups, selected, onApply, extra,
}: {
  label: string;
  placeholder: string;
  disabled?: boolean;
  loading?: boolean;
  error?: string;
  emptyText: string;
  groups: { heading?: string; options: { key: string; label: string; hint?: string }[] }[];
  selected: Set<string>;
  onApply: (keys: string[]) => void;
  extra?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Set<string>>(new Set());
  const all = groups.flatMap((g) => g.options.map((o) => o.key));
  const changed = draft.size !== selected.size || [...draft].some((k) => !selected.has(k));
  const toggle = (k: string) =>
    setDraft((prev) => {
      const next = new Set(prev);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        if (o) setDraft(new Set(selected)); // abre com o que já está aplicado; fechar sem aplicar descarta
        setOpen(o);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="outline" role="combobox" aria-expanded={open} disabled={disabled} className="h-8 w-full justify-between text-xs">
          <span className="truncate">{loading ? "Carregando…" : selected.size > 0 ? `${selected.size} ${label}` : placeholder}</span>
          {loading ? <Loader2 className="ml-2 h-3.5 w-3.5 animate-spin" /> : <ChevronsUpDown className="ml-2 h-3.5 w-3.5 shrink-0 opacity-50" />}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[320px] p-0" align="start">
        <div className="flex items-center gap-1 px-2 py-1.5 border-b border-border">
          <Button size="sm" variant="outline" className="h-6 text-[11px]" disabled={all.length === 0} onClick={() => setDraft(new Set(all))}>Marcar todos</Button>
          <Button size="sm" variant="outline" className="h-6 text-[11px]" disabled={draft.size === 0} onClick={() => setDraft(new Set())}>Limpar</Button>
          <div className="flex-1" />
          {extra}
        </div>
        {error && <div className="px-3 py-2 text-[11px] text-red-400 break-words">{error}</div>}
        <Command>
          <CommandInput placeholder="Buscar…" />
          <CommandList>
            <CommandEmpty>{emptyText}</CommandEmpty>
            {groups.map((g, i) => (
              <CommandGroup key={g.heading ?? i} heading={g.heading}>
                {g.options.map((o) => (
                  <CommandItem key={o.key} value={o.key} onSelect={() => toggle(o.key)}>
                    <Check className={cn("mr-2 h-4 w-4", draft.has(o.key) ? "opacity-100" : "opacity-0")} />
                    <span className="truncate">{o.label}</span>
                    {o.hint && <span className="ml-auto pl-2 text-[10px] text-muted-foreground">{o.hint}</span>}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
        <div className="flex items-center gap-1 border-t border-border p-2">
          <span className="text-[11px] text-muted-foreground">{draft.size} marcado(s){changed ? " · não aplicado" : ""}</span>
          <div className="flex-1" />
          <Button size="sm" variant="ghost" className="h-6 text-[11px]" onClick={() => setOpen(false)}>Cancelar</Button>
          <Button size="sm" className="h-6 text-[11px]" disabled={!changed} onClick={() => { onApply([...draft]); setOpen(false); }}>Aplicar</Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TargetCard({
  index, target, info, loading, onChange, onRemove, canRemove,
}: {
  index: number;
  target: Target;
  info: ClusterInfo;
  loading: boolean;
  onChange: (t: Target) => void;
  onRemove: () => void;
  canRemove: boolean;
}) {
  const [open, setOpen] = useState(true);
  const [showSystem, setShowSystem] = useState(false);
  const cluster = target.cluster;

  const nsQuery = useQuery({
    queryKey: ["metrics-report-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
    staleTime: LIST_STALE,
    retry: false,
  });
  const depQuery = useQuery({
    queryKey: ["metrics-report-deployments", cluster, target.namespaces.join(",")],
    queryFn: () => apiClient.getDeployments(cluster, target.namespaces, undefined, true),
    enabled: !!cluster && target.namespaces.length > 0,
    staleTime: LIST_STALE,
    retry: false,
  });

  const nsOptions = (nsQuery.data ?? [])
    .filter((n) => showSystem || !n.isSystem || target.namespaces.includes(n.name))
    .map((n) => ({ key: n.name, label: n.name }))
    .sort((a, b) => a.label.localeCompare(b.label, "pt-BR"));
  // só conta o que ainda vale (deployment de um namespace que continua escolhido)
  const pickedKeys = new Set(targetItems(target).map((i) => pickKey(i.namespace, i.workload)));
  const depGroups = target.namespaces.map((ns) => ({
    heading: ns,
    options: (depQuery.data ?? [])
      .filter((d) => d.namespace === ns)
      .map((d) => ({ key: pickKey(ns, d.name), label: d.name, hint: `${d.readyReplicas}/${d.replicas}` }))
      .sort((a, b) => a.label.localeCompare(b.label, "pt-BR")),
  }));

  return (
    <div className="rounded-md border border-border">
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-border/60">
        <button onClick={() => setOpen((o) => !o)} className="text-muted-foreground" title={open ? "Recolher" : "Expandir"}>
          {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        </button>
        <div className="flex-1 min-w-0">
          <div className="text-xs font-medium truncate">Alvo {index + 1}{cluster ? ` · ${cluster}` : ""}</div>
          <div className="text-[10px] text-muted-foreground truncate">{targetSummary(target)}</div>
        </div>
        {canRemove && (
          <Button size="icon" variant="ghost" className="h-6 w-6" title="Remover este alvo" onClick={onRemove}>
            <X className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
      {open && (
        <div className="p-2 space-y-1.5">
          <ClusterPicker value={cluster} info={info} loading={loading} hasSelection={target.namespaces.length > 0} onSelect={(c) => onChange(setTargetCluster(target, c))} />
          <MultiPicker
            label={target.namespaces.length === 1 ? "namespace" : "namespaces"}
            placeholder={cluster ? "Escolher namespaces…" : "Escolha o cluster primeiro"}
            disabled={!cluster}
            loading={nsQuery.isLoading}
            error={nsQuery.isError ? errText(nsQuery.error) : undefined}
            emptyText="Nenhum namespace encontrado."
            groups={[{ options: nsOptions }]}
            selected={new Set(target.namespaces)}
            onApply={(keys) => onChange(setTargetNamespaces(target, keys))}
            extra={
              <label className="flex items-center gap-1 text-[10px] cursor-pointer">
                <Checkbox checked={showSystem} onCheckedChange={(v) => setShowSystem(v === true)} />
                sistema
              </label>
            }
          />
          <MultiPicker
            label={targetItems(target).length === 1 ? "deployment" : "deployments"}
            placeholder={target.namespaces.length > 0 ? "Escolher deployments…" : "Escolha os namespaces primeiro"}
            disabled={target.namespaces.length === 0}
            loading={depQuery.isLoading}
            error={depQuery.isError ? errText(depQuery.error) : undefined}
            emptyText="Nenhum deployment encontrado."
            groups={depGroups}
            selected={pickedKeys}
            onApply={(keys) => onChange(setTargetPicked(target, keys))}
          />
        </div>
      )}
    </div>
  );
}

export function MetricsReportTab() {
  const queryClient = useQueryClient();

  // ── alvos ──────────────────────────────────────────────────────────────────
  const [targets, setTargets] = useState<Target[]>(() => {
    const saved = load(TARGETS_KEY, parseTargets);
    return saved.length > 0 ? saved : [newTarget()];
  });
  useEffect(() => save(TARGETS_KEY, targets), [targets]);
  const [sets, setSets] = useState<SavedSet[]>(() => load(SAVED_SETS_KEY, parseSets));
  const [setName, setSetName] = useState("");
  // o servidor é a fonte dos conjuntos (o navegador é só cache): ao abrir, junta os dois e sobe o que faltar
  useEffect(() => {
    let cancelled = false;
    apiClient
      .metricsReportSetsGet()
      .then((r) => {
        if (cancelled) return;
        const server = parseSets(JSON.stringify(r.sets ?? []));
        setSets((local) => {
          const merged = mergeSets(server, local);
          save(SAVED_SETS_KEY, merged);
          if (merged.length !== server.length) apiClient.metricsReportSetsPut(merged).catch(() => {});
          return merged;
        });
      })
      .catch(() => {
        /* servidor indisponível: segue com o cache do navegador */
      });
    return () => {
      cancelled = true;
    };
  }, []);
  function persistSets(next: SavedSet[]) {
    setSets(next);
    save(SAVED_SETS_KEY, next);
    apiClient.metricsReportSetsPut(next).catch(() => toast.error("Não consegui gravar os conjuntos no servidor (ficaram só neste navegador)"));
  }

  const clusters = useQuery({ queryKey: ["metrics-report-clusters"], queryFn: () => apiClient.getClusters(), staleTime: 5 * 60 * 1000, retry: false });
  const info = useMemo<ClusterInfo>(() => {
    const list = clusters.data ?? [];
    const providers: Record<string, string> = {};
    const journeys: Record<string, string> = {};
    for (const c of list) {
      if (c.cloud_provider) providers[c.name] = c.cloud_provider;
      if (c.journey) journeys[c.name] = c.journey;
    }
    return { names: list.map((c) => c.name).sort((a, b) => a.localeCompare(b, "pt-BR")), providers, journeys };
  }, [clusters.data]);

  const items = useMemo(() => allItems(targets), [targets]);
  const updateTarget = (id: string, t: Target) => setTargets((prev) => prev.map((x) => (x.id === id ? t : x)));

  function refreshLists() {
    queryClient.invalidateQueries({ predicate: (q) => LIST_KEYS.includes(String(q.queryKey[0])) });
  }
  function saveSet() {
    if (!setName.trim() || items.length === 0) return;
    const next = upsertSet(sets, setName, targets);
    persistSets(next);
    toast.success(`Conjunto "${setName.trim()}" salvo (${items.length})`);
    setSetName("");
  }
  function removeSet(name: string) {
    const next = sets.filter((s) => s.name !== name);
    persistSets(next);
  }

  // ── percentis de latência a coletar (P50, P95 e/ou P99; lembrados no navegador) ───
  // sub-abas: o relatório por janela (coleta única) e o acompanhamento em tempo real
  const [view, setView] = useState<"report" | "live">("report");
  const [percentiles, setPercentiles] = useState<number[]>(() => load(PERCENTILES_KEY, parsePercentiles));
  useEffect(() => save(PERCENTILES_KEY, percentiles), [percentiles]);

  // ── contexto de node (pool, uso dos nodes, throttling): opt-in, custa consultas a mais ───
  const [nodeContext, setNodeContext] = useState<boolean>(() => load(NODE_CONTEXT_KEY, parseNodeContext));
  useEffect(() => save(NODE_CONTEXT_KEY, nodeContext), [nodeContext]);
  const reportEvents = useReportEvents();
  const [extras, setExtras] = useState<boolean>(() => load(EXTRAS_KEY, parseExtras));
  useEffect(() => save(EXTRAS_KEY, extras), [extras]);

  // ── janela ─────────────────────────────────────────────────────────────────
  const [mode, setMode] = useState<WindowPreset | "custom">("6h");
  const [presetAt, setPresetAt] = useState(() => Date.now());
  const [startLocal, setStartLocal] = useState(() => isoToZonedLocal(Date.now() - 6 * 3600e3));
  const [endLocal, setEndLocal] = useState(() => isoToZonedLocal(Date.now()));

  const windowIso = useMemo(() => {
    if (mode === "custom") {
      const start = zonedLocalToISO(startLocal);
      const end = zonedLocalToISO(endLocal);
      return start && end ? { start, end } : null;
    }
    return presetWindow(mode, presetAt);
  }, [mode, presetAt, startLocal, endLocal]);

  function pickPreset(p: WindowPreset) {
    setMode(p);
    setPresetAt(Date.now());
  }

  const plan = useQuery({
    queryKey: ["metrics-report-plan", windowIso?.start, windowIso?.end],
    queryFn: () => apiClient.metricsReportPlan(windowIso!.start, windowIso!.end),
    enabled: !!windowIso,
    retry: false,
    staleTime: 30_000,
  });

  // ── coleta ─────────────────────────────────────────────────────────────────
  const [jobId, setJobId] = useState<string | null>(null);
  const start = useMutation({
    mutationFn: () => {
      // atalho termina "agora": recalcula no clique (a janela do plano pode ter alguns minutos)
      const w = mode === "custom" ? windowIso! : presetWindow(mode);
      return apiClient.startMetricsReport({ items, start: w.start, end: w.end, percentiles, node_context: nodeContext, extras });
    },
    onSuccess: (r) => {
      setJobId(r.id);
      setExpanded(new Set());
    },
    onError: (e: Error) => toast.error(e.message || "Falha ao iniciar o relatório"),
  });
  const job = useQuery({
    queryKey: ["metrics-report-job", jobId],
    queryFn: () => apiClient.metricsReportJob(jobId!),
    enabled: !!jobId,
    retry: false,
    refetchInterval: (query) => (query.state.data?.state === "running" ? 2000 : false),
  });
  const running = start.isPending || job.data?.state === "running";
  const cancel = useMutation({
    mutationFn: () => apiClient.cancelMetricsReport(jobId!),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["metrics-report-job", jobId] }),
  });

  const canCollect = !running && items.length > 0 && items.length <= MAX_ITEMS && !!windowIso && !plan.isError && !plan.isLoading;
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const result = job.data?.result;

  return (
    <div className="flex h-full overflow-hidden bg-background">
      {/* ── Alvos: um cartão por cluster; o + adiciona outro ── */}
      <aside className="w-80 flex-shrink-0 border-r border-border flex flex-col min-h-0">
        <div className="px-3 py-2 border-b border-border flex items-center gap-2">
          <div className="flex-1">
            <div className="text-sm font-medium">Alvos da coleta</div>
            <div className="text-xs text-muted-foreground">{items.length > 0 ? `${items.length} aplicação(ões) no total` : "Nenhuma aplicação marcada"}</div>
          </div>
          <Button size="icon" variant="ghost" className="h-7 w-7" title="Recarregar clusters, namespaces e deployments" onClick={refreshLists}>
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
          <Button size="icon" variant="outline" className="h-7 w-7" title="Adicionar um alvo (outro cluster)" onClick={() => setTargets((prev) => [...prev, newTarget()])}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto p-2 space-y-2">
          {clusters.isError && <div className="text-xs text-red-400 break-words">{errText(clusters.error)}</div>}
          {targets.map((t, i) => (
            <TargetCard
              key={t.id}
              index={i}
              target={t}
              info={info}
              loading={clusters.isLoading}
              onChange={(next) => updateTarget(t.id, next)}
              onRemove={() => setTargets((prev) => prev.filter((x) => x.id !== t.id))}
              canRemove={targets.length > 1}
            />
          ))}
          <Button variant="outline" size="sm" className="w-full h-7 text-xs gap-1" onClick={() => setTargets((prev) => [...prev, newTarget()])}>
            <Plus className="h-3.5 w-3.5" />Adicionar alvo
          </Button>
        </div>

        <div className="p-2 space-y-1.5 border-t border-border">
          <select
            className="h-7 w-full rounded border border-border bg-background px-1 text-xs"
            value=""
            onChange={(e) => {
              const s = sets.find((x) => x.name === e.target.value);
              if (s) setTargets(s.targets.map((t) => ({ ...t, id: newTarget().id })));
            }}
            title="Carregar um conjunto salvo (substitui os alvos atuais)"
          >
            <option value="">Conjuntos salvos…</option>
            {sets.map((s) => (
              <option key={s.name} value={s.name}>{s.name} ({allItems(s.targets).length})</option>
            ))}
          </select>
          <div className="flex gap-1">
            <Input className="h-7 text-xs" placeholder="Nome do conjunto" value={setName} onChange={(e) => setSetName(e.target.value)} />
            <Button size="icon" variant="outline" className="h-7 w-7 flex-shrink-0" title="Salvar os alvos atuais como conjunto" disabled={!setName.trim() || items.length === 0} onClick={saveSet}>
              <Save className="h-3.5 w-3.5" />
            </Button>
          </div>
          {sets.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {sets.map((s) => (
                <Badge key={s.name} variant="secondary" className="gap-1 text-[10px] font-normal">
                  {s.name}
                  <button title={`Apagar o conjunto ${s.name}`} onClick={() => removeSet(s.name)}><Trash2 className="h-2.5 w-2.5" /></button>
                </Badge>
              ))}
            </div>
          )}
        </div>
      </aside>

      {/* ── Janela, coleta e relatório ── */}
      <main className="flex-1 min-w-0 overflow-y-auto p-4 space-y-4">
        <div className="flex gap-1 border-b border-border -mt-1 pb-2">
          {([["report", "Relatório"], ["live", "Tempo real"]] as const).map(([id, label]) => (
            <Button key={id} size="sm" variant={view === id ? "default" : "ghost"} className="h-7 text-xs" onClick={() => setView(id)}>
              {label}
            </Button>
          ))}
        </div>
        {view === "live" ? (
          <MetricsLivePanel items={items} percentiles={percentiles} onPercentiles={setPercentiles} events={reportEvents.events} onSaveEvent={reportEvents.save} onRemoveEvent={reportEvents.remove} onRemove={(it) => setTargets((prev) => removePickedItem(prev, it))} />
        ) : (
          <>
        <section className="rounded-md border border-border p-3 space-y-2">
          <div className="text-sm font-medium">Janela de tempo</div>
          <div className="flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <Button key={p.id} size="sm" variant={mode === p.id ? "default" : "outline"} className="h-7 text-xs" onClick={() => pickPreset(p.id)}>
                {p.label}
              </Button>
            ))}
            <Button size="sm" variant={mode === "custom" ? "default" : "outline"} className="h-7 text-xs" onClick={() => setMode("custom")}>
              Personalizado
            </Button>
            <MetricsEventPicker
              events={reportEvents.events}
              current={windowIso}
              disabled={running}
              onApply={(s, e) => {
                setMode("custom");
                setStartLocal(isoToZonedLocal(Date.parse(s)));
                setEndLocal(isoToZonedLocal(Date.parse(e)));
              }}
              onSave={reportEvents.save}
              onRemove={reportEvents.remove}
            />
          </div>
          {mode === "custom" && (
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <label className="flex items-center gap-1.5">
                Início
                <Input type="datetime-local" className="h-7 w-48 text-xs" value={startLocal} onChange={(e) => setStartLocal(e.target.value)} />
              </label>
              <label className="flex items-center gap-1.5">
                Fim
                <Input type="datetime-local" className="h-7 w-48 text-xs" value={endLocal} onChange={(e) => setEndLocal(e.target.value)} />
              </label>
              <span className="text-muted-foreground">fuso {REPORT_TZ}</span>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-3 text-xs">
            <span className="text-muted-foreground">Percentis de latência (pico):</span>
            {PERCENTILE_CHOICES.map((p) => (
              <label key={p} className="flex items-center gap-1.5 cursor-pointer">
                <Checkbox
                  checked={percentiles.includes(p)}
                  disabled={running}
                  onCheckedChange={(v) => setPercentiles((prev) => togglePercentile(prev, p, v === true))}
                />
                P{p}
              </label>
            ))}
            <span className="text-[10px] text-muted-foreground">só os marcados são consultados</span>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1.5 cursor-pointer" title="Mostra em que node pool a aplicação rodou, o uso de CPU e memória dos nodes que a hospedaram e o throttling de CPU dela. É contexto para interpretar a latência, não um veredito.">
              <Checkbox checked={nodeContext} disabled={running} onCheckedChange={(v) => setNodeContext(v === true)} />
              Contexto de node
            </label>
            <span className="text-[10px] text-muted-foreground">pool, CPU e memória dos nodes, throttling · mais consultas ao Prometheus (dado dos últimos ~14 dias)</span>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <label className="flex items-center gap-1.5 cursor-pointer" title="Acrescenta réplicas disponíveis (máx/mín/méd), limites do HPA, restarts na janela e rede (bytes/s recebidos e enviados, descartes e erros). Só Prometheus; é contexto, não veredito.">
              <Checkbox checked={extras} disabled={running} onCheckedChange={(v) => setExtras(v === true)} />
              Réplicas, HPA e rede
            </label>
            <span className="text-[10px] text-muted-foreground">réplicas, teto do HPA, restarts, rede · mais consultas ao Prometheus (dado dos últimos ~14 dias)</span>
          </div>
          <div className="text-xs space-y-1">
            {!windowIso && <div className="text-red-400">Data ou hora inválida.</div>}
            {plan.isLoading && windowIso && <div className="text-muted-foreground">Conferindo a janela…</div>}
            {plan.isError && <div className="text-red-400">{(plan.error as Error).message}</div>}
            {plan.data && (
              <>
                <div className="text-muted-foreground">
                  Resolução do tráfego: <b>{plan.data.plan.dt_resolution}</b> · do P99: <b>{plan.data.plan.p99_resolution}</b>
                  {plan.data.plan.peak_approx && <span className="ml-2 text-amber-400">pico aproximado</span>}
                </div>
                {[...(plan.data.warnings ?? []), ...(plan.data.plan.notes ?? [])].map((w) => (
                  <div key={w} className="text-amber-400">{w}</div>
                ))}
              </>
            )}
          </div>
        </section>

        <section className="flex flex-wrap items-center gap-3">
          <Button disabled={!canCollect} onClick={() => start.mutate()} className="gap-2">
            {running ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
            Coletar {items.length > 0 ? `${items.length} aplicação(ões)` : ""}
          </Button>
          {running && jobId && (
            <Button variant="outline" size="sm" className="gap-1" onClick={() => cancel.mutate()}>
              <Square className="h-3 w-3" />Cancelar
            </Button>
          )}
          {job.data && job.data.state === "running" && (
            <span className="text-xs text-muted-foreground">{job.data.done} de {job.data.total} concluídas…</span>
          )}
          {items.length === 0 && <span className="text-xs text-muted-foreground">Marque ao menos uma aplicação à esquerda.</span>}
          {items.length > MAX_ITEMS && <span className="text-xs text-red-400">Máximo de {MAX_ITEMS} aplicações por relatório.</span>}
        </section>

        {job.isError && <div className="text-xs text-red-400">{(job.error as Error).message}</div>}
        {job.data?.state === "failed" && <div className="text-xs text-red-400">Falhou: {job.data.error}</div>}
        {job.data?.state === "cancelled" && <div className="text-xs text-amber-400">Coleta cancelada.</div>}

        {result && <ReportExportBar result={result} />}

        {result && (
          <ReportTable
            rows={result.rows}
            percentiles={result.percentiles?.length ? result.percentiles : [99]}
            nodeContext={!!result.node_context}
            extras={!!result.extras}
            expanded={expanded}
            onToggle={(k) =>
              setExpanded((prev) => {
                const next = new Set(prev);
                if (next.has(k)) next.delete(k);
                else next.add(k);
                return next;
              })
            }
            window={result.window}
            plan={result.plan}
            warnings={result.warnings ?? []}
          />
        )}
        {result && result.rows.length > 0 && <MetricsComparePanel key={job.data?.id} result={result} events={reportEvents.events} />}
          </>
        )}
      </main>
    </div>
  );
}

// Exporta o relatório: Markdown (com o código à vista para colar em relatórios manuais), CSV e PDF.
// Tudo é gerado no navegador a partir do resultado; nada é enviado a lugar nenhum.
function ReportExportBar({ result }: { result: ReportResult }) {
  const [showMd, setShowMd] = useState(false);
  const [delimiter, setDelimiter] = useState<CsvDelimiter>(";");
  const [pdfBusy, setPdfBusy] = useState(false);
  const md = useMemo(() => buildMarkdown(result), [result]);
  const stamp = fileStamp(result.generated_at);

  async function pdf() {
    setPdfBusy(true);
    try {
      await exportReportPDF(result);
    } catch (e) {
      toast.error(`Falha ao gerar o PDF: ${errText(e)}`);
    } finally {
      setPdfBusy(false);
    }
  }

  return (
    <section className="rounded-md border border-border p-2 space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs font-medium mr-1">Exportar</span>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => copyText(md, "Markdown do relatório")}>
          <Copy className="h-3.5 w-3.5" />Copiar Markdown
        </Button>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => setShowMd(true)}>
          <FileText className="h-3.5 w-3.5" />Ver código Markdown
        </Button>
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => downloadText(md, "text/markdown", `relatorio-metricas_${stamp}.md`)}>
          <FileDown className="h-3.5 w-3.5" />.md
        </Button>
        <span className="mx-1 h-4 w-px bg-border" />
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" onClick={() => downloadText(buildCSV(result, delimiter), "text/csv", `relatorio-metricas_${stamp}.csv`)}>
          <FileSpreadsheet className="h-3.5 w-3.5" />CSV
        </Button>
        <select
          className="h-7 rounded border border-border bg-background px-1 text-xs"
          value={delimiter}
          onChange={(e) => setDelimiter(e.target.value as CsvDelimiter)}
          title="Separador do CSV: ponto e vírgula abre certo no Excel em português; vírgula é o padrão internacional"
        >
          <option value=";">separador ;</option>
          <option value=",">separador ,</option>
        </select>
        <span className="mx-1 h-4 w-px bg-border" />
        <Button size="sm" variant="outline" className="h-7 text-xs gap-1" disabled={pdfBusy} onClick={pdf}>
          {pdfBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileDown className="h-3.5 w-3.5" />}PDF
        </Button>
      </div>
      <Dialog open={showMd} onOpenChange={setShowMd}>
        <DialogContent className="max-w-4xl max-h-[88vh] flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle>Código Markdown do relatório</DialogTitle>
            <DialogDescription>
              Uma tabela por cluster, pronta para colar em um relatório feito à mão. Selecione um trecho ou use os botões.
            </DialogDescription>
          </DialogHeader>
          <pre className="flex-1 min-h-0 overflow-auto rounded border border-border bg-muted/30 p-3 text-[11px] leading-snug whitespace-pre">{md}</pre>
          <div className="flex items-center justify-end gap-2">
            <Button size="sm" variant="outline" className="gap-1" onClick={() => downloadText(md, "text/markdown", `relatorio-metricas_${stamp}.md`)}>
              <FileDown className="h-3.5 w-3.5" />Baixar .md
            </Button>
            <Button size="sm" className="gap-1" onClick={() => copyText(md, "Markdown do relatório")}>
              <Copy className="h-3.5 w-3.5" />Copiar
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function AlertBadges({ a }: { a: ReportAlerts }) {
  return (
    <span className="ml-2 inline-flex items-center gap-2 text-[10px] font-normal text-muted-foreground">
      <span>{a.total} aplicação(ões)</span>
      {a.red > 0 && <span className="text-red-400">{FLAG_ICON.red} {a.red}</span>}
      {a.warn > 0 && <span className="text-amber-400">{FLAG_ICON.warn} {a.warn}</span>}
      {a.failed > 0 && <span className="text-red-400">{a.failed} com erro de coleta</span>}
    </span>
  );
}

// Resultado em árvore: cluster → namespace → deployment. Cada nível recolhe; o resumo de alertas
// (🔴/⚠️/erros) aparece no cabeçalho para achar o problema sem abrir tudo.
function ReportTable({
  rows, percentiles, nodeContext, extras, expanded, onToggle, window, plan, warnings,
}: {
  rows: ReportRow[];
  percentiles: number[];
  nodeContext: boolean;
  extras: boolean;
  expanded: Set<string>;
  onToggle: (key: string) => void;
  window: { start: string; end: string };
  plan: { dt_resolution: string; p99_resolution: string; peak_approx: boolean };
  warnings: string[];
}) {
  const fmtDate = (iso: string) => new Date(iso).toLocaleString("pt-BR", { timeZone: REPORT_TZ });
  const flag = (f?: string) => (f ? ` ${FLAG_ICON[f] ?? ""}` : "");
  const groups = useMemo(() => groupRows(rows), [rows]);
  const cols = 8 + percentiles.length + (nodeContext ? 4 : 0) + (extras ? EXTRAS_COLUMNS.length : 0); // Aplicação, Requisições, Max RPS, [percentis], 4xx, 5xx, CPU, Mem, [node: pool, CPU, Mem, throttling]
  const [closed, setClosed] = useState<Set<string>>(new Set());
  const toggleGroup = (id: string) =>
    setClosed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const allGroupIds = useMemo(() => groups.flatMap((c) => [`c|${c.cluster}`, ...c.namespaces.map((n) => `n|${c.cluster}|${n.namespace}`)]), [groups]);

  const renderRow = (r: ReportRow) => {
    const key = `${r.cluster}|${r.namespace}|${r.workload}`;
    const open = expanded.has(key);
    const t = r.traffic;
    const u = r.usage;
    const notes = [...(t?.notes ?? []), ...(u?.notes ?? []), ...(r.node?.notes ?? []), ...(r.extras?.notes ?? [])];
    return (
      <Fragment key={key}>
        <tr className="border-t border-border/60 hover:bg-muted/30 cursor-pointer align-top" onClick={() => onToggle(key)}>
          <td className="py-1.5 pl-12 pr-2">
            <div className="font-medium">{r.workload}</div>
            {(r.errors?.length ?? 0) > 0 && <div className="text-[10px] text-red-400">{r.errors!.length} erro(s) de coleta</div>}
          </td>
          <td className="px-2 py-1.5 whitespace-nowrap">{fmtInt(t?.requests)}</td>
          <td className="px-2 py-1.5 whitespace-nowrap">{fmtRps(t?.max_rps)}{t?.peak_approx && <span title="pico aproximado"> ≈</span>}</td>
          <td className="px-2 py-1.5 whitespace-nowrap" title="Média do tempo de resposta na janela, ponderada pelas requisições (não é pico)">{fmtLatency(t?.avg_ms)}</td>
          {percentiles.map((p) => (
            <td key={p} className="px-2 py-1.5 whitespace-nowrap">
              {fmtLatency(percentileValue(t, p))}
              {p === 99 ? flag(r.flags.p99) : ""}
            </td>
          ))}
          <td className="px-2 py-1.5 whitespace-nowrap">{fmtInt(t?.err_4xx)}{flag(r.flags.err_4xx)}</td>
          <td className="px-2 py-1.5 whitespace-nowrap">{fmtInt(t?.err_5xx)}{flag(r.flags.err_5xx)}</td>
          <td className="px-2 py-1.5">
            <div>req: {fmtPctStats(u?.cpu_req_pct)}</div>
            <div className="text-muted-foreground">lim: {fmtPctStats(u?.cpu_lim_pct)}</div>
          </td>
          <td className="px-2 py-1.5">
            <div>req: {fmtPctStats(u?.mem_req_pct)}</div>
            <div className="text-muted-foreground">lim: {fmtPctStats(u?.mem_lim_pct)}</div>
          </td>
          {nodeContext && (
            <>
              <td className="px-2 py-1.5">{fmtPools(r.node?.pools)}</td>
              <td className="px-2 py-1.5 whitespace-nowrap">{fmtNodeUsage(r.node?.cpu, r.node?.nodes)}</td>
              <td className="px-2 py-1.5 whitespace-nowrap">{fmtNodeUsage(r.node?.mem, r.node?.nodes)}</td>
              <td className="px-2 py-1.5 whitespace-nowrap">{fmtThrottle(r.node?.throttle_pct)}</td>
            </>
          )}
          {extras && extrasCells(r.extras).map((c, i) => <td key={i} className="px-2 py-1.5 whitespace-nowrap">{c}</td>)}
        </tr>
        {open && (
          <tr className="bg-muted/20">
            <td colSpan={cols} className="py-2 pl-12 pr-3 space-y-1 text-[11px]">
              {t?.services && <div><b>Serviços usados:</b> {t.services.join(", ")}</div>}
              {t?.p99_service && <div><b>P99 de pico vem de:</b> {t.p99_service}</div>}
              {r.node && <div><b>Node:</b> {r.node.nodes} node(s) hospedaram a aplicação{r.node.pool_label ? ` · pool pelo rótulo ${r.node.pool_label}` : ""}</div>}
              <div><b>Fontes:</b> {[t?.source, u?.source, r.node?.source, r.extras?.source].filter(Boolean).filter((x, i, a) => a.indexOf(x) === i).join(" + ") || "nenhuma"}</div>
              {r.errors?.map((e) => (
                <div key={e} className="text-red-400 break-words">{e.length > 400 ? `${e.slice(0, 400)}…` : e}</div>
              ))}
              {notes.map((n) => (
                <div key={n} className="text-muted-foreground break-words">{n}</div>
              ))}
            </td>
          </tr>
        )}
      </Fragment>
    );
  };

  return (
    <section className="space-y-2">
      <div className="flex items-start gap-2">
        <div className="flex-1 text-xs text-muted-foreground">
          Janela: {fmtDate(window.start)} → {fmtDate(window.end)} ({REPORT_TZ}) · resolução {plan.dt_resolution} (P99 {plan.p99_resolution})
          {plan.peak_approx && <span className="text-amber-400"> · pico aproximado</span>}
          {warnings.map((w) => (
            <div key={w} className="text-amber-400">{w}</div>
          ))}
        </div>
        <Button size="sm" variant="outline" className="h-6 text-[11px]" onClick={() => setClosed(new Set())}>Expandir tudo</Button>
        <Button size="sm" variant="outline" className="h-6 text-[11px]" onClick={() => setClosed(new Set(allGroupIds))}>Recolher tudo</Button>
      </div>
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full text-xs">
          <thead className="bg-muted/50 text-left">
            <tr>
              {["Aplicação", "Requisições", "Max RPS", "Resposta média", ...percentiles.map((p) => `P${p} (pico)`), "4xx", "5xx", "CPU % (máx/mín/méd)", "Mem % (máx/mín/méd)", ...(nodeContext ? ["Node pool", "Node CPU % (pior/média)", "Node Mem % (pior/média)", "Throttling CPU % (máx/méd)"] : []), ...(extras ? EXTRAS_COLUMNS : [])].map((h) => (
                <th key={h} className="px-2 py-1.5 font-medium whitespace-nowrap">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {groups.map((c) => {
              const cId = `c|${c.cluster}`;
              const cOpen = !closed.has(cId);
              return (
                <Fragment key={cId}>
                  <tr className="border-t border-border bg-muted/40 cursor-pointer" onClick={() => toggleGroup(cId)}>
                    <td colSpan={cols} className="px-2 py-1.5 font-medium">
                      <span className="inline-flex items-center gap-1">
                        {cOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                        <span className={isProdClusterName(c.cluster) ? "text-amber-600 dark:text-amber-400" : undefined}>{c.cluster}</span>
                        <AlertBadges a={c.alerts} />
                      </span>
                    </td>
                  </tr>
                  {cOpen &&
                    c.namespaces.map((n) => {
                      const nId = `n|${c.cluster}|${n.namespace}`;
                      const nOpen = !closed.has(nId);
                      return (
                        <Fragment key={nId}>
                          <tr className="border-t border-border/60 bg-muted/20 cursor-pointer" onClick={() => toggleGroup(nId)}>
                            <td colSpan={cols} className="py-1 pl-6 pr-2">
                              <span className="inline-flex items-center gap-1">
                                {nOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                                <span className="font-medium">{n.namespace}</span>
                                <AlertBadges a={n.alerts} />
                              </span>
                            </td>
                          </tr>
                          {nOpen && n.rows.map(renderRow)}
                        </Fragment>
                      );
                    })}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="text-[10px] text-muted-foreground">🔴 P99 acima de 1 s (só quando o P99 foi coletado) ou qualquer 5xx · ⚠️ qualquer 4xx · "sem dado" = a fonte não devolveu o valor (não é zero).</div>
    </section>
  );
}
