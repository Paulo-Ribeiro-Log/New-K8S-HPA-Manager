import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, ArrowDown, ArrowUp, Database, Download, Loader2, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ProtectedAction } from "@/components/rbac";
import { useK8sPermissions } from "@/hooks/useK8sPermissions";
import { apiClient, type VolumeAppCost, type VolumeItem, type VolumesResponse } from "@/lib/api/client";

// Volumes do cluster (PV/PVC) ligados à aplicação, node e node pool. Somente leitura.

interface Props {
  cluster: string;
  resourceLabel: string; // "Node Pools" ou "Node Groups"
  onClose: () => void;
}

type SortKey = "namespace" | "workload" | "pvc" | "storage_class" | "source" | "capacity" | "cost" | "usage" | "age" | "pool" | "issues";

const OWNER_SOURCE_LABEL: Record<string, string> = { pod: "pod", spec: "spec do workload", statefulset: "volumeClaimTemplate", label: "label" };
const OWNER_SOURCE_HELP: Record<string, string> = {
  pod: "Um pod em execução monta este volume",
  spec: "O pod template do workload referencia este volume (pode estar com 0 réplicas ou entre execuções)",
  statefulset: "O nome do PVC segue o volumeClaimTemplate de um StatefulSet",
  label: "Deduzido pelas labels app/app.kubernetes.io/name do PVC",
};
const GIB = 1024 ** 3;
const fmtBRL = (v: number) => v.toLocaleString("pt-BR", { style: "currency", currency: "BRL" });
const USAGE_CLASS_LABEL: Record<string, string> = { critical: "Crítico", warning: "Atenção", oversized: "Superdimensionado" };
const USAGE_BAR: Record<string, string> = { critical: "bg-red-500", warning: "bg-amber-400", oversized: "bg-sky-400", ok: "bg-emerald-500" };
const COST_KIND_LABEL: Record<string, string> = { effective: "efetivo", estimate: "estimado", none: "sem preço" };
const fmtGiB = (b: number) => (b ? `${(b / GIB).toFixed(b >= 10 * GIB ? 0 : 1)} GiB` : "—");

// Mesma frase que o servidor exige (deleteConfirmPhrase): copiada na tela e colada no campo.
const deletePhrase = (i: VolumeItem) => (i.kind === "pv" ? `apagar pv ${i.pv ?? ""}` : `apagar pvc ${i.namespace}/${i.pvc ?? ""}`);

function sortValue(it: VolumeItem, k: SortKey): string | number {
  switch (k) {
    case "namespace": return it.namespace;
    case "workload": return it.workloads[0] ?? it.app_hint ?? "\uffff";
    case "pvc": return it.pvc || it.pv || "";
    case "storage_class": return it.storage_class;
    case "source": return `${it.source ?? ""} ${it.source_detail ?? ""}`;
    case "capacity": return it.capacity_bytes;
    case "cost": return it.cost_brl_month ?? 0;
    case "usage": return it.usage_percentage ?? -1;
    case "age": return it.age_days ?? -1;
    case "pool": return it.node_pools.join(",");
    case "issues": return it.issues.length;
  }
}

function csvCell(v: string | number): string {
  const s = String(v);
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function toCSV(items: VolumeItem[]): string {
  const head = ["tipo", "namespace", "aplicações", "origem_do_vinculo", "pvc", "status_pvc", "pv", "fase_pv", "storage_class", "origem", "origem_detalhe", "zona", "criado_em", "idade_dias", "expansivel", "snapshots", "ultimo_snapshot", "capacidade_gib", "custo_brl_mes", "custo_tipo", "access_modes", "reclaim", "pods", "nodes", "node_pools", "uso_gib", "uso_%", "crescimento_gib_dia", "dias_para_encher", "classe_uso", "problemas"];
  const rows = items.map((i) => [
    i.kind, i.namespace, i.workloads.length ? i.workloads.join(" ") : (i.app_hint ? `(label) ${i.app_hint}` : ""), i.owner_source ?? "", i.pvc ?? "", i.pvc_status ?? "", i.pv ?? "", i.pv_phase ?? "",
    i.storage_class, i.source ?? "", i.source_detail ?? "", i.zone ?? "", i.created_at ?? "", i.age_days ?? "", i.can_expand === undefined ? "" : i.can_expand ? "sim" : "não", i.snapshots ?? 0, i.last_snapshot ?? "", (i.capacity_bytes / GIB).toFixed(2), i.cost_brl_month ? i.cost_brl_month.toFixed(2) : "", i.cost_kind ?? "", i.access_modes.join(" "), i.reclaim_policy ?? "", i.pods.join(" "), i.nodes.join(" "),
    i.node_pools.join(" "), i.used_bytes ? (i.used_bytes / GIB).toFixed(2) : "", i.usage_percentage ? i.usage_percentage.toFixed(1) : "",
    i.growth_bytes_per_day ? (i.growth_bytes_per_day / GIB).toFixed(3) : "", i.days_to_full ?? "", i.usage_class ?? "", i.issues.join(" | "),
  ]);
  return [head, ...rows].map((r) => r.map(csvCell).join(";")).join("\n");
}

function toAppsCSV(apps: VolumeAppCost[]): string {
  const head = ["namespace", "aplicação", "volumes", "compartilhados", "capacidade_gib", "custo_brl_mes", "volumes_sem_preco"];
  const rows = apps.map((a) => [a.namespace, a.app, a.volumes, a.shared_volumes, (a.capacity_bytes / GIB).toFixed(2), a.cost_brl_month.toFixed(2), a.unpriced]);
  return [head, ...rows].map((r) => r.map(csvCell).join(";")).join("\n");
}

// Servidor NFS e seus exports: quantos volumes, aplicações e namespaces dependem de cada servidor.
interface NFSServer {
  server: string;
  volumes: number;
  capacity: number;
  idle: number;
  exports: string[];
  apps: string[];
  namespaces: string[];
  kinds: string[];
}

function groupNFS(items: VolumeItem[]): NFSServer[] {
  const by = new Map<string, NFSServer & { e: Set<string>; a: Set<string>; n: Set<string>; k: Set<string> }>();
  for (const i of items) {
    if (!/^(NFS|EFS)/.test(i.source ?? "")) continue;
    const detail = i.source_detail || "(sem servidor)";
    const idx = detail.indexOf(":");
    const server = idx > 0 ? detail.slice(0, idx) : detail;
    const path = idx > 0 ? detail.slice(idx + 1) : "";
    let g = by.get(server);
    if (!g) {
      g = { server, volumes: 0, capacity: 0, idle: 0, exports: [], apps: [], namespaces: [], kinds: [], e: new Set(), a: new Set(), n: new Set(), k: new Set() };
      by.set(server, g);
    }
    g.volumes++;
    g.capacity += i.capacity_bytes;
    if (i.idle) g.idle++;
    if (path) g.e.add(path);
    if (i.namespace) g.n.add(i.namespace);
    g.k.add(i.source ?? "");
    for (const w of i.workloads) g.a.add(`${i.namespace}/${w}`);
    if (!i.workloads.length && i.app_hint) g.a.add(`${i.namespace}/${i.app_hint}`);
  }
  return [...by.values()]
    .map((g) => ({ server: g.server, volumes: g.volumes, capacity: g.capacity, idle: g.idle, exports: [...g.e].sort(), apps: [...g.a].sort(), namespaces: [...g.n].sort(), kinds: [...g.k].sort() }))
    .sort((a, b) => b.apps.length - a.apps.length || b.volumes - a.volumes);
}

function toNFSCSV(servers: NFSServer[]): string {
  const head = ["servidor", "tipo", "volumes", "exports", "aplicações", "namespaces", "ociosos", "capacidade_declarada_gib"];
  const rows = servers.map((v) => [v.server, v.kinds.join(" "), v.volumes, v.exports.join(" "), v.apps.join(" "), v.namespaces.join(" "), v.idle, (v.capacity / GIB).toFixed(2)]);
  return [head, ...rows].map((r) => r.map(csvCell).join(";")).join("\n");
}

// Botões de escrita de uma linha, só com o que o RBAC do cluster permite no namespace do volume
// (SelfSubjectRulesReview, em cache por namespace). O servidor revalida de qualquer forma.
function VolumeRowActions({ cluster, item, onAction }: { cluster: string; item: VolumeItem; onAction: (t: "expand" | "delete", i: VolumeItem) => void }) {
  const canExpand = item.kind === "pvc" && !!item.can_expand && item.pvc_status === "Bound";
  const canDelete = (item.kind === "pvc" || item.kind === "pv") && item.idle;
  const { permissions } = useK8sPermissions(cluster, canExpand || canDelete ? item.namespace || "default" : "");
  const mayExpand = canExpand && permissions.canPatchPVC;
  const mayDelete = canDelete && (item.kind === "pv" ? permissions.canDeletePV : permissions.canDeletePVC);
  if (!mayExpand && !mayDelete) return null;
  return (
    <div className="flex flex-col items-start gap-1">
      {mayExpand && (
        <ProtectedAction allowed={permissions.canPatchPVC} showWarning={false}>
          <Button variant="outline" size="sm" className="h-6 px-2 text-[11px]" onClick={() => onAction("expand", item)}>Expandir</Button>
        </ProtectedAction>
      )}
      {mayDelete && (
        <ProtectedAction allowed={item.kind === "pv" ? permissions.canDeletePV : permissions.canDeletePVC} showWarning={false}>
          <Button variant="outline" size="sm" className="h-6 px-2 text-[11px] text-red-400 hover:text-red-300" onClick={() => onAction("delete", item)}>Apagar</Button>
        </ProtectedAction>
      )}
    </div>
  );
}

export function NodePoolVolumesModal({ cluster, resourceLabel, onClose }: Props) {
  const [data, setData] = useState<VolumesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [includeUsage, setIncludeUsage] = useState(false);
  const [includeCost, setIncludeCost] = useState(false);
  const [view, setView] = useState<"volumes" | "apps" | "nfs">("volumes");
  const [query, setQuery] = useState("");
  const [namespace, setNamespace] = useState("");
  const [pool, setPool] = useState("");
  const [source, setSource] = useState("");
  const [usageFilter, setUsageFilter] = useState("");
  const [zone, setZone] = useState("");
  const [onlyIssues, setOnlyIssues] = useState(false);
  const [noSnapshot, setNoSnapshot] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [action, setAction] = useState<{ type: "expand" | "delete"; item: VolumeItem } | null>(null);
  const [actionInput, setActionInput] = useState("");
  const [actionBusy, setActionBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "namespace", desc: false });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    apiClient
      .getNodePoolVolumes(cluster, includeUsage, includeCost)
      .then((r) => { if (!cancelled) setData(r); })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao listar os volumes"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [cluster, includeUsage, includeCost, reloadKey]);

  const items = data?.items ?? [];
  const namespaces = useMemo(() => [...new Set(items.map((i) => i.namespace).filter(Boolean))].sort(), [items]);
  const sources = useMemo(() => [...new Set(items.map((i) => i.source).filter((v): v is string => !!v))].sort(), [items]);
  const zones = useMemo(() => [...new Set(items.map((i) => i.zone).filter((v): v is string => !!v))].sort(), [items]);
  const pools = useMemo(() => [...new Set(items.flatMap((i) => i.node_pools).filter(Boolean))].sort(), [items]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = items.filter((i) => {
      if (namespace && i.namespace !== namespace) return false;
      if (pool && !i.node_pools.includes(pool)) return false;
      if (source && i.source !== source) return false;
      if (usageFilter && i.usage_class !== usageFilter) return false;
      if (zone && i.zone !== zone) return false;
      if (onlyIssues && i.issues.length === 0) return false;
      if (noSnapshot && (i.kind !== "pvc" || i.snapshots)) return false;
      if (!q) return true;
      return [i.namespace, ...i.workloads, i.app_hint, i.pvc, i.pv, i.storage_class, i.source, i.source_detail, ...i.pods, ...i.nodes].some((v) => v?.toLowerCase().includes(q));
    });
    const dir = sort.desc ? -1 : 1;
    return [...filtered].sort((a, b) => {
      const va = sortValue(a, sort.key), vb = sortValue(b, sort.key);
      return (typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb))) * dir;
    });
  }, [items, query, namespace, pool, source, usageFilter, zone, onlyIssues, noSnapshot, sort]);

  function openAction(type: "expand" | "delete", item: VolumeItem) {
    setAction({ type, item });
    setActionInput("");
    setActionError("");
  }

  async function runAction() {
    if (!action) return;
    const { type, item } = action;
    setActionBusy(true);
    setActionError("");
    try {
      if (type === "expand") await apiClient.expandVolume(cluster, item.namespace, item.pvc ?? "", Number(actionInput));
      else await apiClient.deleteVolume(cluster, item.kind === "pv" ? "pv" : "pvc", item.namespace, item.kind === "pv" ? (item.pv ?? "") : (item.pvc ?? ""), actionInput);
      setAction(null);
      setReloadKey((k) => k + 1);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : "Falha ao executar a ação");
    } finally {
      setActionBusy(false);
    }
  }

  function toggleSort(key: SortKey) {
    setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: false }));
  }

  const apps = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (data?.by_app ?? []).filter((a) => (!namespace || a.namespace === namespace) && (!q || `${a.namespace} ${a.app}`.toLowerCase().includes(q)));
  }, [data, query, namespace]);
  const nfsServers = useMemo(() => groupNFS(items.filter((i) => (!namespace || i.namespace === namespace) && (!pool || i.node_pools.includes(pool)))), [items, namespace, pool]);
  const nfsView = view === "nfs";
  const costOn = includeCost && !!data && !data.cost_warning;
  const usageOn = includeUsage && !!data && !data.usage_warning;
  const appsView = view === "apps" && costOn;

  function exportCSV() {
    const blob = new Blob(["﻿" + (nfsView ? toNFSCSV(nfsServers) : appsView ? toAppsCSV(apps) : toCSV(rows))], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${nfsView ? "volumes-servidores-nfs" : appsView ? "volumes-por-aplicacao" : "volumes"}-${cluster.replace(/[^\w.-]+/g, "_")}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  const Th = ({ k, children, className = "" }: { k: SortKey; children: React.ReactNode; className?: string }) => (
    <th className={`text-left font-medium px-2 py-1.5 whitespace-nowrap ${className}`}>
      <button className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => toggleSort(k)}>
        {children}
        {sort.key === k && (sort.desc ? <ArrowDown className="w-3 h-3" /> : <ArrowUp className="w-3 h-3" />)}
      </button>
    </th>
  );

  const s = data?.summary;
  const selectCls = "h-7 rounded-md border border-input bg-background px-2 text-xs max-w-[11rem]";

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-[min(96vw,90rem)] h-[88vh] flex flex-col gap-2 p-4">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm pr-6">
            <Database className="w-4 h-4" />Volumes (PV/PVC) do cluster
            <span className="font-mono text-xs text-muted-foreground truncate">{cluster}</span>
          </DialogTitle>
        </DialogHeader>

        {s && (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs flex-shrink-0">
            <span><b>{s.pvcs}</b> PVCs</span>
            <span><b>{s.pvs}</b> PVs</span>
            <span><b>{fmtGiB(s.capacity_bytes)}</b> provisionados</span>
            <span><b>{s.nfs}</b> NFS</span>
            <span className={s.idle_old ? "text-amber-300" : "text-muted-foreground"} title="Volumes ociosos criados há mais de 30 dias"><b>{s.idle_old}</b> ociosos antigos</span>
            {s.resize_pending > 0 && <span className="text-amber-300" title="PVC com expansão em andamento ou aguardando o pod reiniciar"><b>{s.resize_pending}</b> expandindo</span>}
            {usageOn && s.expand_blocked > 0 && <span className="text-red-400" title="Volume cheio ou enchendo em StorageClass sem allowVolumeExpansion"><b>{s.expand_blocked}</b> cheios sem como expandir</span>}
            {s.snapshots_known && <span title="PVCs com VolumeSnapshot pronto"><b>{s.with_snapshot}</b> com snapshot</span>}
            {zones.length > 0 && (
              <>
                <span className={s.zone_no_nodes ? "text-red-400" : "text-muted-foreground"} title="Disco zonal em zona sem nenhum node no cluster"><b>{s.zone_no_nodes}</b> em zona sem node</span>
                <span className={s.zone_single_node ? "text-amber-300" : "text-muted-foreground"} title="Volume em uso cuja zona tem um único node"><b>{s.zone_single_node}</b> em zona com 1 node</span>
              </>
            )}
            {usageOn && (
              <>
                <span className={s.usage_critical ? "text-red-400" : "text-muted-foreground"} title="Uso a partir de 90% ou previsão de encher em até 7 dias"><b>{s.usage_critical}</b> enchendo (crítico)</span>
                <span className={s.usage_warning ? "text-amber-300" : "text-muted-foreground"} title="Uso a partir de 80% ou previsão de encher em até 30 dias"><b>{s.usage_warning}</b> em atenção</span>
                <span className={s.usage_oversized ? "text-sky-300" : "text-muted-foreground"} title="Volume de 10 GiB ou mais usando menos de 20%"><b>{s.usage_oversized}</b> superdimensionados</span>
              </>
            )}
            {costOn && (
              <>
                <span title="Soma dos volumes com preço (efetivo ou estimado)"><b>{fmtBRL(s.cost_brl_month)}</b>/mês</span>
                <span className={s.cost_idle_brl_month ? "text-amber-300" : "text-muted-foreground"} title="Volumes sem pod usando ou PV sem PVC">
                  <b>{fmtBRL(s.cost_idle_brl_month)}</b>/mês em volumes ociosos
                </span>
              </>
            )}
            <span className={s.with_issues ? "text-amber-300" : "text-muted-foreground"}><b>{s.with_issues}</b> com problema</span>
            <span className={s.unused_pvcs ? "text-amber-300" : "text-muted-foreground"}><b>{s.unused_pvcs}</b> PVCs sem pod</span>
            <span className={s.no_owner ? "text-amber-300" : "text-muted-foreground"}><b>{s.no_owner}</b> sem aplicação identificada</span>
            <span className={s.orphan_pvs ? "text-amber-300" : "text-muted-foreground"}><b>{s.orphan_pvs}</b> PVs órfãos</span>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2 flex-shrink-0">
          <div className="relative">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground pointer-events-none" />
            <Input className="h-7 pl-7 text-xs w-64" placeholder="Buscar app, PVC, PV, pod, node, servidor NFS…" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <select className={selectCls} value={namespace} onChange={(e) => setNamespace(e.target.value)} aria-label="Namespace">
            <option value="">Todos os namespaces</option>
            {namespaces.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
          <select className={selectCls} value={pool} onChange={(e) => setPool(e.target.value)} aria-label={resourceLabel}>
            <option value="">Todos os {resourceLabel}</option>
            {pools.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
          {usageOn && (
            <select className={selectCls} value={usageFilter} onChange={(e) => setUsageFilter(e.target.value)} aria-label="Situação de uso">
              <option value="">Todo o uso</option>
              <option value="critical">Crítico (enche)</option>
              <option value="warning">Atenção</option>
              <option value="oversized">Superdimensionado</option>
            </select>
          )}
          {zones.length > 0 && (
            <select className={selectCls} value={zone} onChange={(e) => setZone(e.target.value)} aria-label="Zona do disco">
              <option value="">Todas as zonas</option>
              {zones.map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          )}
          <select className={selectCls} value={source} onChange={(e) => setSource(e.target.value)} aria-label="Tipo de volume">
            <option value="">Todos os tipos</option>
            {sources.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
          <label className="flex items-center gap-1.5 text-xs cursor-pointer">
            <Checkbox checked={onlyIssues} onCheckedChange={(v) => setOnlyIssues(v === true)} />Só com problema
          </label>
          {s?.snapshots_known && (
            <label className="flex items-center gap-1.5 text-xs cursor-pointer" title="PVCs sem nenhum VolumeSnapshot pronto">
              <Checkbox checked={noSnapshot} onCheckedChange={(v) => setNoSnapshot(v === true)} />Sem snapshot
            </label>
          )}
          <label className="flex items-center gap-1.5 text-xs cursor-pointer" title="Custo mensal por volume e por aplicação: efetivo do Cost Management quando existe (Azure) e, no resto, estimativa de tabela on-demand do FinOps">
            <Checkbox checked={includeCost} onCheckedChange={(v) => { setIncludeCost(v === true); if (v !== true) setView((cur) => (cur === "apps" ? "volumes" : cur)); }} />Incluir custo (FinOps)
          </label>
          <label className="flex items-center gap-1.5 text-xs cursor-pointer" title="Consulta o Prometheus do cluster (kubelet_volume_stats); pode demorar alguns segundos">
            <Checkbox checked={includeUsage} onCheckedChange={(v) => setIncludeUsage(v === true)} />Incluir uso (Prometheus)
          </label>
          <div className="flex-1" />
          {(costOn || (s?.nfs ?? 0) > 0) && (
            <div className="flex rounded-md border border-input overflow-hidden text-xs">
              {(["volumes", ...(costOn ? ["apps"] : []), ...((s?.nfs ?? 0) > 0 ? ["nfs"] : [])] as const).map((v) => (
                <button key={v} onClick={() => setView(v as "volumes" | "apps" | "nfs")} className={`px-2 h-7 ${view === v ? "bg-primary text-primary-foreground" : "hover:bg-muted"}`}>
                  {v === "volumes" ? "Volumes" : v === "apps" ? "Por aplicação" : "Servidores NFS"}
                </button>
              ))}
            </div>
          )}
          <span className="text-xs text-muted-foreground">{nfsView ? `${nfsServers.length} servidores` : appsView ? `${apps.length} aplicações` : `${rows.length} de ${items.length}`}</span>
          <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={exportCSV} disabled={nfsView ? nfsServers.length === 0 : appsView ? apps.length === 0 : rows.length === 0}>
            <Download className="w-3 h-3" />CSV
          </Button>
        </div>

        {includeUsage && data?.usage_warning && (
          <div className="flex items-start gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-300 flex-shrink-0">
            <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" /><span className="[overflow-wrap:anywhere]">Uso indisponível: {data.usage_warning}</span>
          </div>
        )}
        {usageOn && (
          <p className="text-[11px] text-muted-foreground flex-shrink-0">
            Uso do kubelet (Prometheus) e crescimento pela regressão linear dos últimos 7 dias. Só volumes montados por um pod têm métrica; a previsão de “enche em N dias”
            some quando há pouco histórico ou o volume não cresce. Disco não reduz no lugar: superdimensionado exige recriar o volume.
          </p>
        )}
        {includeCost && data?.cost_warning && (
          <div className="flex items-start gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-300 flex-shrink-0">
            <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" /><span className="[overflow-wrap:anywhere]">{data.cost_warning}</span>
          </div>
        )}
        {costOn && (
          <p className="text-[11px] text-muted-foreground flex-shrink-0">
            {s?.cost_effective ?? 0} volume(s) com custo efetivo (Cost Management), {s?.cost_estimate ?? 0} estimado(s) por preço de tabela on-demand (sem reserva) e {s?.cost_none ?? 0} sem preço
            (NFS/Files/EFS: o custo é do servidor). Volume compartilhado entra dividido entre as aplicações.
          </p>
        )}

        <div className="flex-1 min-h-0 overflow-auto border border-border/40 rounded">
          {nfsView && !loading && !error ? (
            nfsServers.length === 0 ? (
              <div className="p-4 text-sm text-muted-foreground">Nenhum volume NFS com esses filtros.</div>
            ) : (
              <table className="w-full text-xs">
                <thead className="sticky top-0 bg-card border-b border-border/40 text-muted-foreground z-10">
                  <tr>
                    <th className="text-left font-medium px-2 py-1.5">Servidor</th>
                    <th className="text-right font-medium px-2 py-1.5">Volumes</th>
                    <th className="text-right font-medium px-2 py-1.5">Aplicações</th>
                    <th className="text-left font-medium px-2 py-1.5">Exports</th>
                    <th className="text-left font-medium px-2 py-1.5">Aplicações que dependem</th>
                    <th className="text-left font-medium px-2 py-1.5">Namespaces</th>
                  </tr>
                </thead>
                <tbody>
                  {nfsServers.map((v) => (
                    <tr key={v.server} className="border-b border-border/20 hover:bg-muted/30 align-top">
                      <td className="px-2 py-1.5">
                        <div className="font-mono">{v.server}</div>
                        <div className="text-[10px] text-muted-foreground">{v.kinds.join(", ")}</div>
                      </td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap">
                        {v.volumes}
                        {v.idle > 0 && <span className="ml-1 text-[10px] text-amber-300" title="Volumes sem pod ou PV sem PVC">({v.idle} ocioso(s))</span>}
                      </td>
                      <td className={`px-2 py-1.5 text-right font-semibold ${v.apps.length >= 5 ? "text-amber-300" : ""}`} title="Quantas aplicações param se este servidor cair">{v.apps.length}</td>
                      <td className="px-2 py-1.5 font-mono [overflow-wrap:anywhere]">{v.exports.map((e) => <div key={e}>{e}</div>)}</td>
                      <td className="px-2 py-1.5 font-mono [overflow-wrap:anywhere]">{v.apps.length ? v.apps.map((a) => <div key={a}>{a}</div>) : <span className="text-amber-300">nenhuma identificada</span>}</td>
                      <td className="px-2 py-1.5 font-mono [overflow-wrap:anywhere]">{v.namespaces.join(", ")}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          ) : appsView && !loading && !error ? (
            apps.length === 0 ? (
              <div className="p-4 text-sm text-muted-foreground">Nenhuma aplicação com esses filtros.</div>
            ) : (
              <table className="w-full text-xs">
                <thead className="sticky top-0 bg-card border-b border-border/40 text-muted-foreground z-10">
                  <tr>
                    <th className="text-left font-medium px-2 py-1.5">Namespace</th>
                    <th className="text-left font-medium px-2 py-1.5">Aplicação</th>
                    <th className="text-right font-medium px-2 py-1.5">Volumes</th>
                    <th className="text-right font-medium px-2 py-1.5">Capacidade</th>
                    <th className="text-right font-medium px-2 py-1.5">Custo/mês</th>
                    <th className="text-right font-medium px-2 py-1.5">% do total</th>
                  </tr>
                </thead>
                <tbody>
                  {apps.map((a) => (
                    <tr key={`${a.namespace}|${a.app}`} className="border-b border-border/20 hover:bg-muted/30">
                      <td className="px-2 py-1.5 font-mono">{a.namespace || "—"}</td>
                      <td className="px-2 py-1.5">
                        {a.app === "(sem aplicação identificada)" ? <span className="text-amber-300">{a.app}</span> : <span className="font-mono">{a.app}</span>}
                      </td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap">
                        {a.volumes}
                        {a.shared_volumes > 0 && <span className="ml-1 text-[10px] text-muted-foreground" title="Volumes compartilhados com outras aplicações (custo dividido)">({a.shared_volumes} comp.)</span>}
                        {a.unpriced > 0 && <span className="ml-1 text-[10px] text-amber-300" title="Volumes sem preço: o custo real pode ser maior">+{a.unpriced} sem preço</span>}
                      </td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap">{fmtGiB(a.capacity_bytes)}</td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap font-semibold">{fmtBRL(a.cost_brl_month)}</td>
                      <td className="px-2 py-1.5 text-right text-muted-foreground">{s && s.cost_brl_month > 0 ? `${((a.cost_brl_month / s.cost_brl_month) * 100).toFixed(1)}%` : "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          ) : loading ? (
            <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground"><Loader2 className="w-4 h-4 animate-spin" />Listando PVs, PVCs e pods do cluster…</div>
          ) : error ? (
            <div className="p-4 text-sm text-red-400 whitespace-pre-wrap">{error}</div>
          ) : rows.length === 0 ? (
            <div className="p-4 text-sm text-muted-foreground">{items.length === 0 ? "Este cluster não tem PVs nem PVCs." : "Nenhum volume com esses filtros."}</div>
          ) : (
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-card border-b border-border/40 text-muted-foreground z-10">
                <tr>
                  <Th k="namespace">Namespace</Th>
                  <Th k="workload">Aplicação</Th>
                  <Th k="pvc">PVC / PV</Th>
                  <Th k="storage_class">Storage class</Th>
                  <Th k="source">Origem</Th>
                  <Th k="capacity">Capacidade</Th>
                  <Th k="age">Idade</Th>
                  {costOn && <Th k="cost">Custo/mês</Th>}
                  {includeUsage && <Th k="usage">Uso</Th>}
                  <Th k="pool">{resourceLabel.replace(/s$/, "")}</Th>
                  <Th k="issues">Situação</Th>
                  <th className="text-left font-medium px-2 py-1.5">Ações</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((i) => (
                  <tr key={`${i.kind}/${i.namespace}/${i.pvc}/${i.pv}`} className="border-b border-border/20 hover:bg-muted/30 align-top">
                    <td className="px-2 py-1.5 font-mono">{i.namespace || "—"}</td>
                    <td className="px-2 py-1.5">
                      {i.workloads.length > 0 ? (
                        <>
                          {i.workloads.map((w) => {
                            const [kind, ...rest] = w.split("/");
                            return (
                              <div key={w}>
                                <span className="font-mono">{rest.join("/")}</span>
                                <span className="ml-1 text-[10px] text-muted-foreground">{kind}</span>
                              </div>
                            );
                          })}
                          <div className="text-[10px] text-muted-foreground" title={OWNER_SOURCE_HELP[i.owner_source ?? "pod"]}>
                            {i.pods.length > 0 ? `${i.pods.length} pod(s) rodando` : "nenhum pod rodando"} · via {OWNER_SOURCE_LABEL[i.owner_source ?? "pod"]}
                          </div>
                        </>
                      ) : i.app_hint ? (
                        <>
                          <span className="font-mono">{i.app_hint}</span>
                          <div className="text-[10px] text-amber-300" title={OWNER_SOURCE_HELP.label}>só pela label do PVC — nenhum workload o referencia</div>
                        </>
                      ) : (
                        <span className="text-amber-300 text-[11px]">sem aplicação identificada</span>
                      )}
                    </td>
                    <td className="px-2 py-1.5 font-mono">
                      <div title={i.pvc}>{i.pvc || <span className="text-muted-foreground">{i.kind === "nfs" ? "(direto no pod, sem PVC)" : "(sem PVC)"}</span>}</div>
                      {i.pv && <div className="text-[10px] text-muted-foreground" title={i.pv}>{i.pv}</div>}
                    </td>
                    <td className="px-2 py-1.5">{i.storage_class || "—"}</td>
                    <td className="px-2 py-1.5">
                      {i.source ? <span>{i.source}</span> : <span className="text-muted-foreground">—</span>}
                      {i.source_detail && <div className="font-mono text-[10px] text-muted-foreground [overflow-wrap:anywhere]" title={i.source_detail}>{i.source_detail}</div>}
                      {i.zone && <div className="text-[10px] text-muted-foreground" title="Zona de disponibilidade do disco">zona {i.zone}</div>}
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap">
                      {fmtGiB(i.capacity_bytes)}
                      <div className="text-[10px] text-muted-foreground">{i.access_modes.join(", ")}</div>
                      {i.can_expand !== undefined && (
                        <div className={`text-[10px] ${i.can_expand ? "text-muted-foreground" : "text-amber-300"}`} title="allowVolumeExpansion da StorageClass">{i.can_expand ? "expansível" : "não expansível"}</div>
                      )}
                      {!!i.snapshots && (
                        <div className="text-[10px] text-muted-foreground" title={i.last_snapshot ? `Último: ${new Date(i.last_snapshot).toLocaleString("pt-BR")}` : undefined}>{i.snapshots} snapshot(s)</div>
                      )}
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap text-muted-foreground" title={i.created_at ? `Criado em ${new Date(i.created_at).toLocaleString("pt-BR")}` : undefined}>
                      {i.age_days === undefined ? "—" : i.age_days >= 365 ? `${(i.age_days / 365).toFixed(1)} anos` : `${i.age_days} d`}
                    </td>
                    {costOn && (
                      <td className="px-2 py-1.5 whitespace-nowrap" title={i.cost_note}>
                        {i.cost_kind && i.cost_kind !== "none" ? (
                          <>
                            <span className={i.idle ? "text-amber-300 font-semibold" : "font-semibold"}>{fmtBRL(i.cost_brl_month ?? 0)}</span>
                            <div className="text-[10px] text-muted-foreground">{COST_KIND_LABEL[i.cost_kind]}</div>
                          </>
                        ) : <span className="text-muted-foreground text-[11px]">sem preço</span>}
                      </td>
                    )}
                    {includeUsage && (
                      <td className="px-2 py-1.5 min-w-[9rem]">
                        {i.usage_class ? (
                          <>
                            <div className="flex items-center gap-2">
                              <div className="h-1.5 w-16 rounded bg-muted overflow-hidden flex-shrink-0">
                                <div className={`h-full ${USAGE_BAR[i.usage_class]}`} style={{ width: `${Math.min(100, i.usage_percentage ?? 0)}%` }} />
                              </div>
                              <span className="whitespace-nowrap">{fmtGiB(i.used_bytes ?? 0)} ({(i.usage_percentage ?? 0).toFixed(0)}%)</span>
                            </div>
                            {i.days_to_full != null ? (
                              <div className={`text-[10px] ${i.days_to_full <= 7 ? "text-red-400" : i.days_to_full <= 30 ? "text-amber-300" : "text-muted-foreground"}`}>
                                enche em ~{i.days_to_full} dia(s) · +{((i.growth_bytes_per_day ?? 0) / GIB).toFixed(2)} GiB/dia
                              </div>
                            ) : i.growth_bytes_per_day ? (
                              <div className="text-[10px] text-muted-foreground">{i.growth_bytes_per_day > 0 ? "estável" : "diminuindo"}</div>
                            ) : null}
                            {USAGE_CLASS_LABEL[i.usage_class] && (
                              <div className={`text-[10px] ${i.usage_class === "critical" ? "text-red-400" : i.usage_class === "warning" ? "text-amber-300" : "text-sky-300"}`}>{USAGE_CLASS_LABEL[i.usage_class]}</div>
                            )}
                          </>
                        ) : <span className="text-muted-foreground">—</span>}
                      </td>
                    )}
                    <td className="px-2 py-1.5 font-mono" title={i.nodes.join(", ")}>{i.node_pools.length ? i.node_pools.join(", ") : <span className="text-muted-foreground">—</span>}</td>
                    <td className="px-2 py-1.5">
                      <div className="flex flex-wrap items-center gap-1">
                        {i.pvc_status && <Badge variant={i.pvc_status === "Bound" ? "default" : "secondary"} className="text-[10px]">{i.pvc_status}</Badge>}
                        {i.kind === "pv" && <Badge variant="secondary" className="text-[10px]">PV {i.pv_phase}</Badge>}
                      </div>
                      {i.issues.map((m) => (
                        <div key={m} className="flex items-start gap-1 text-amber-300 text-[11px] mt-0.5">
                          <AlertTriangle className="w-3 h-3 flex-shrink-0 mt-0.5" /><span className="[overflow-wrap:anywhere]">{m}</span>
                        </div>
                      ))}
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap">
                      <VolumeRowActions cluster={cluster} item={i} onAction={openAction} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {action && (
          <Dialog open onOpenChange={(v) => { if (!v && !actionBusy) setAction(null); }}>
            <DialogContent className="max-w-md">
              <DialogHeader>
                <DialogTitle className="text-sm">
                  {action.type === "expand" ? "Expandir volume" : action.item.kind === "pv" ? "Apagar PV" : "Apagar PVC"}
                </DialogTitle>
              </DialogHeader>
              {(() => {
                const name = action.item.kind === "pv" ? (action.item.pv ?? "") : (action.item.pvc ?? "");
                return action.type === "expand" ? (
                  <div className="space-y-2 text-xs">
                    <p><span className="font-mono">{action.item.namespace}/{name}</span> — atual: <b>{fmtGiB(action.item.capacity_bytes)}</b>. Disco só cresce; reduzir exige recriar o volume.</p>
                    <Input type="number" min={1} step={1} className="h-8 text-xs" placeholder="Novo tamanho em GiB" value={actionInput} onChange={(e) => setActionInput(e.target.value)} autoFocus />
                  </div>
                ) : (
                  <div className="space-y-2 text-xs">
                    <p>
                      Vai apagar <span className="font-mono">{action.item.kind === "pvc" ? `${action.item.namespace}/` : ""}{name}</span>.
                      {action.item.reclaim_policy === "Delete"
                        ? <b className="text-red-400"> O reclaim é Delete: o disco e os dados serão apagados junto e não há como desfazer.</b>
                        : " O reclaim é " + (action.item.reclaim_policy || "desconhecido") + ": o disco pode continuar existindo (e cobrando) fora do cluster."}
                    </p>
                    <p className="text-muted-foreground">O servidor confere de novo que ninguém está usando o volume. Copie a frase abaixo e cole no campo (não vale digitar):</p>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 rounded border border-border/40 bg-muted/40 px-2 py-1 font-mono [overflow-wrap:anywhere] select-all">{deletePhrase(action.item)}</code>
                      <Button type="button" variant="outline" size="sm" className="h-7 text-[11px]" onClick={() => { void navigator.clipboard?.writeText(deletePhrase(action.item)); }}>Copiar</Button>
                    </div>
                    <Input
                      className="h-8 text-xs font-mono"
                      placeholder="Cole a frase aqui"
                      value={actionInput}
                      onChange={(e) => {
                        // só aceita colagem; digitar não altera o campo e apagar limpa tudo
                        const t = (e.nativeEvent as InputEvent).inputType ?? "";
                        if (t === "insertFromPaste") setActionInput(e.target.value.trim());
                        else if (t.startsWith("delete") || e.target.value === "") setActionInput("");
                      }}
                      autoFocus
                    />
                  </div>
                );
              })()}
              {actionError && <div className="text-xs text-red-400 whitespace-pre-wrap [overflow-wrap:anywhere]">{actionError}</div>}
              <div className="flex justify-end gap-2">
                <Button variant="outline" size="sm" onClick={() => setAction(null)} disabled={actionBusy}>Cancelar</Button>
                <Button
                  size="sm"
                  variant={action.type === "delete" ? "destructive" : "default"}
                  onClick={runAction}
                  disabled={actionBusy || !actionInput || (action.type === "delete" && actionInput !== deletePhrase(action.item))}
                >
                  {actionBusy && <Loader2 className="w-3 h-3 animate-spin mr-1" />}{action.type === "expand" ? "Expandir" : "Apagar"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
      </DialogContent>
    </Dialog>
  );
}
