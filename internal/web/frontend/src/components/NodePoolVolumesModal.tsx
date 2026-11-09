import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, ArrowDown, ArrowUp, Camera, Database, Download, Info, Loader2, MoreVertical, Search, Trash2, Maximize2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useK8sPermissions } from "@/hooks/useK8sPermissions";
import { apiClient, type AzStorageDetails, type SnapshotCreated, type SnapshotInfo, type VolumeAppCost, type VolumeItem, type VolumesResponse } from "@/lib/api/client";

// Volumes do cluster (PV/PVC) ligados à aplicação, node e node pool. Somente leitura.

interface Props {
  cluster: string;
  resourceLabel: string; // "Node Pools" ou "Node Groups"
  initialQuery?: string; // abre já filtrado (ex.: vindo da aba Dependencies)
  onClose: () => void;
}

type VolumeActionType = "expand" | "delete" | "snapshot" | "details";
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
  const head = ["tipo", "namespace", "aplicações", "origem_do_vinculo", "pvc", "status_pvc", "pv", "fase_pv", "storage_class", "origem", "origem_detalhe", "sku", "redundancia", "tier_acesso", "protocolo", "conta_storage", "zona", "criado_em", "idade_dias", "ocioso_visto_desde", "ocioso_dias_observados", "gerenciado_por", "tamanho_template_sts", "expansivel", "snapshots", "ultimo_snapshot", "capacidade_gib", "custo_brl_mes", "custo_tipo", "access_modes", "reclaim", "pods", "nodes", "node_pools", "uso_gib", "uso_%", "crescimento_gib_dia", "dias_para_encher", "classe_uso", "problemas"];
  const rows = items.map((i) => [
    i.kind, i.namespace, i.workloads.length ? i.workloads.join(" ") : (i.app_hint ? `(label) ${i.app_hint}` : ""), i.owner_source ?? "", i.pvc ?? "", i.pvc_status ?? "", i.pv ?? "", i.pv_phase ?? "",
    i.storage_class, i.source ?? "", i.source_detail ?? "", i.storage?.sku ?? "", i.storage?.redundancy ?? "", i.storage?.tier ?? "", i.storage?.protocol ?? "", i.storage?.account ?? "", i.zone ?? "", i.created_at ?? "", i.age_days ?? "", i.idle_since ?? "", i.idle_days ?? "", i.managed_by ?? "", i.template_size ?? "", i.can_expand === undefined ? "" : i.can_expand ? "sim" : "não", i.snapshots ?? 0, i.last_snapshot ?? "", (i.capacity_bytes / GIB).toFixed(2), i.cost_brl_month ? i.cost_brl_month.toFixed(2) : "", i.cost_kind ?? "", i.access_modes.join(" "), i.reclaim_policy ?? "", i.pods.join(" "), i.nodes.join(" "),
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

// Menu de ações (⋮) de uma linha, só com o que o RBAC do cluster permite no namespace do volume
// (SelfSubjectRulesReview, em cache por namespace). O servidor revalida de qualquer forma.
function VolumeRowActions({ cluster, item, snapshotApi, onAction }: { cluster: string; item: VolumeItem; snapshotApi?: string; onAction: (t: VolumeActionType, i: VolumeItem) => void }) {
  const bound = item.kind === "pvc" && item.pvc_status === "Bound";
  const canExpand = bound && !!item.can_expand;
  const canDelete = (item.kind === "pvc" || item.kind === "pv") && item.idle;
  const canSnap = bound && !!snapshotApi;
  const { permissions } = useK8sPermissions(cluster, canExpand || canDelete || canSnap ? item.namespace || "default" : "");
  const mayExpand = canExpand && permissions.canPatchPVC;
  const mayDelete = canDelete && (item.kind === "pv" ? permissions.canDeletePV : permissions.canDeletePVC);
  const maySnap = canSnap && permissions.canSnapshot;
  if (!mayExpand && !mayDelete && !maySnap && !item.storage) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="h-6 w-6 p-0" aria-label="Ações do volume"><MoreVertical className="w-4 h-4" /></Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="text-xs">
        {item.storage && <DropdownMenuItem className="text-xs gap-2" onSelect={() => onAction("details", item)}><Info className="w-3.5 h-3.5" />Detalhes do storage</DropdownMenuItem>}
        {item.storage && (maySnap || mayExpand || mayDelete) && <DropdownMenuSeparator />}
        {maySnap && <DropdownMenuItem className="text-xs gap-2" onSelect={() => onAction("snapshot", item)}><Camera className="w-3.5 h-3.5" />Tirar snapshot</DropdownMenuItem>}
        {mayExpand && <DropdownMenuItem className="text-xs gap-2" onSelect={() => onAction("expand", item)}><Maximize2 className="w-3.5 h-3.5" />Expandir</DropdownMenuItem>}
        {mayDelete && (maySnap || mayExpand) && <DropdownMenuSeparator />}
        {mayDelete && <DropdownMenuItem className="text-xs gap-2 text-red-400 focus:text-red-400" onSelect={() => onAction("delete", item)}><Trash2 className="w-3.5 h-3.5" />Apagar</DropdownMenuItem>}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const STORAGE_LABEL: Record<string, string> = {
  driver: "Driver CSI", sku: "SKU", performance: "Desempenho", redundancy: "Redundância", tier: "Tier de acesso",
  protocol: "Protocolo", account: "Storage account", resource_group: "Resource group", share: "Share / container",
};

// Detalhes do storage: o que o cluster declara (StorageClass + PV). Kind da conta (StorageV2…) e tier real
// só existem no Azure: o botão "Consultar na Azure" faz a leitura (GET) a pedido; o comando az fica como alternativa.
function StorageDetailDialog({ cluster, item, onClose }: { cluster: string; item: VolumeItem; onClose: () => void }) {
  const st = item.storage!;
  const [az, setAz] = useState<AzStorageDetails | null>(null);
  const [azBusy, setAzBusy] = useState(false);
  const [azError, setAzError] = useState("");
  const isAzure = /azure/.test(st.driver ?? "");

  async function queryAzure() {
    setAzBusy(true);
    setAzError("");
    try {
      setAz(await apiClient.getVolumeAzureDetails(cluster, { namespace: item.namespace, pvc: item.kind === "pvc" ? item.pvc : undefined, pv: item.pv }));
    } catch (e) {
      setAzError(e instanceof Error ? e.message : "Falha ao consultar a Azure");
    } finally {
      setAzBusy(false);
    }
  }
  const azCmd = st.account && /azure/.test(st.driver ?? "")
    ? `az storage account show -n ${st.account}${st.resource_group ? ` -g ${st.resource_group}` : ""} --query "{kind:kind,sku:sku.name,accessTier:accessTier,hns:isHnsEnabled,tls:minimumTlsVersion,publicBlob:allowBlobPublicAccess}" -o table`
    : "";
  const rows = Object.keys(STORAGE_LABEL).filter((k) => st[k as keyof typeof st]);
  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-2xl max-h-[88vh] overflow-auto">
        <DialogHeader><DialogTitle className="text-sm">Detalhes do storage</DialogTitle></DialogHeader>
        <div className="space-y-2 text-xs">
          <p className="font-mono">{item.namespace ? `${item.namespace}/` : ""}{item.pvc || item.pv}</p>
          <table className="w-full">
            <tbody>
              {rows.map((k) => (
                <tr key={k} className="border-b border-border/20 align-top">
                  <td className="py-1 pr-3 text-muted-foreground whitespace-nowrap">{STORAGE_LABEL[k]}</td>
                  <td className="py-1 font-mono [overflow-wrap:anywhere]">{String(st[k as keyof typeof st])}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {st.redundancy_note && <p className="text-muted-foreground">{st.redundancy}: {st.redundancy_note}.</p>}
          {st.params && (
            <details>
              <summary className="cursor-pointer text-muted-foreground">Parâmetros da StorageClass e do PV ({Object.keys(st.params).length})</summary>
              <table className="w-full mt-1">
                <tbody>
                  {Object.entries(st.params).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => (
                    <tr key={k} className="border-b border-border/10 align-top">
                      <td className="py-0.5 pr-3 font-mono text-muted-foreground">{k}</td>
                      <td className="py-0.5 font-mono [overflow-wrap:anywhere]">{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </details>
          )}
          <p className="text-muted-foreground">Só o que a StorageClass e o PV declaram. Se um campo não aparece, o cluster não informa — ele pode existir na conta.</p>
          {isAzure && (
            <div className="space-y-2 rounded border border-border/40 p-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground">Kind da conta, tier real, replicação, rede, proteção de dados e (disco) IOPS/estado só existem na Azure.</span>
                <Button size="sm" className="h-7 px-2 text-[11px] flex-shrink-0" onClick={queryAzure} disabled={azBusy}>
                  {azBusy && <Loader2 className="w-3 h-3 animate-spin mr-1" />}{az ? "Consultar de novo" : "Consultar na Azure"}
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground">Somente leitura (GET no Azure Resource Manager), com o <code>az login</code> do servidor. Nada é alterado.</p>
              {azError && <div className="text-red-400 whitespace-pre-wrap [overflow-wrap:anywhere]">{azError}</div>}
              {az && (
                <div className="space-y-2">
                  {az.warnings.map((w) => (
                    <div key={w} className="flex items-start gap-1.5 text-amber-300"><AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" /><span className="[overflow-wrap:anywhere]">{w}</span></div>
                  ))}
                  {az.notes.length > 0 && (
                    <div className="rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-amber-300 space-y-0.5">
                      <div className="font-medium">Pontos de atenção</div>
                      {az.notes.map((n) => <div key={n}>• {n}</div>)}
                    </div>
                  )}
                  {az.sections.map((sec) => (
                    <div key={sec.title}>
                      <div className="font-medium mb-0.5">{sec.title}</div>
                      <table className="w-full">
                        <tbody>
                          {sec.fields.map((f) => (
                            <tr key={f.label} className="border-b border-border/10 align-top">
                              <td className="py-0.5 pr-3 text-muted-foreground whitespace-nowrap">{f.label}</td>
                              <td className={`py-0.5 font-mono [overflow-wrap:anywhere] ${f.warn ? "text-amber-300" : ""}`}>{f.value}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ))}
                  <div className="text-[10px] text-muted-foreground">Consultado em {new Date(az.queried_at).toLocaleString("pt-BR")}{az.subscription ? ` · subscription ${az.subscription}` : ""}</div>
                </div>
              )}
              {azCmd && (
                <details>
                  <summary className="cursor-pointer text-muted-foreground">Ou rode você mesmo no terminal</summary>
                  <code className="block mt-1 rounded border border-border/40 bg-muted/40 px-2 py-1 font-mono [overflow-wrap:anywhere] select-all">{azCmd}</code>
                  <Button variant="outline" size="sm" className="h-6 px-2 text-[11px] mt-1" onClick={() => { void navigator.clipboard?.writeText(azCmd); }}>Copiar comando</Button>
                </details>
              )}
            </div>
          )}
        </div>
        <div className="flex justify-end"><Button variant="outline" size="sm" onClick={onClose}>Fechar</Button></div>
      </DialogContent>
    </Dialog>
  );
}

// Tirar snapshot: confere a versão da API de snapshot e as classes compatíveis com o driver do volume.
function SnapshotDialog({ cluster, item, onClose, onDone }: { cluster: string; item: VolumeItem; onClose: () => void; onDone: () => void }) {
  const [info, setInfo] = useState<SnapshotInfo | null>(null);
  const [klass, setKlass] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [created, setCreated] = useState<SnapshotCreated | null>(null);

  useEffect(() => {
    let cancelled = false;
    apiClient
      .getSnapshotInfo(cluster, item.namespace, item.pvc ?? "")
      .then((r) => { if (!cancelled) { setInfo(r); setKlass(r.suggested); } })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao consultar o snapshot"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [cluster, item.namespace, item.pvc]);

  async function create() {
    setBusy(true);
    setError("");
    try {
      setCreated(await apiClient.createSnapshot(cluster, item.namespace, item.pvc ?? "", klass));
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao criar o snapshot");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !busy) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader><DialogTitle className="text-sm">Tirar snapshot</DialogTitle></DialogHeader>
        <div className="space-y-2 text-xs">
          <p><span className="font-mono">{item.namespace}/{item.pvc}</span> — {fmtGiB(item.capacity_bytes)}</p>
          {loading && <div className="flex items-center gap-2 text-muted-foreground"><Loader2 className="w-3.5 h-3.5 animate-spin" />Consultando a API de snapshot do cluster…</div>}
          {info && !created && (
            <>
              <p className="text-muted-foreground">API <span className="font-mono">snapshot.storage.k8s.io/{info.api_version}</span> · driver <span className="font-mono">{info.driver}</span></p>
              <select className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs" value={klass} onChange={(e) => setKlass(e.target.value)} aria-label="VolumeSnapshotClass">
                {info.suggested === "" && <option value="">Escolha a VolumeSnapshotClass…</option>}
                {info.classes.map((c) => <option key={c.name} value={c.name}>{c.name}{c.default ? " (padrão)" : ""}{c.deletion_policy ? ` · ${c.deletion_policy}` : ""}</option>)}
              </select>
              <p className="text-muted-foreground">Cria um VolumeSnapshot no namespace do PVC. Snapshot cobra armazenamento e continua existindo até alguém apagá-lo. Com a classe em <b>Delete</b>, apagar o snapshot apaga a cópia.</p>
            </>
          )}
          {created && (
            <p className="text-emerald-400">Snapshot <span className="font-mono">{created.snapshot}</span> criado (classe {created.class}). Ele leva alguns instantes até ficar pronto; recarregue a lista para ver a contagem.</p>
          )}
          {error && <div className="text-red-400 whitespace-pre-wrap [overflow-wrap:anywhere]">{error}</div>}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose} disabled={busy}>{created ? "Fechar" : "Cancelar"}</Button>
          {!created && (
            <Button size="sm" onClick={create} disabled={busy || loading || !info || !klass}>
              {busy && <Loader2 className="w-3 h-3 animate-spin mr-1" />}Criar snapshot
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function NodePoolVolumesModal({ cluster, resourceLabel, initialQuery, onClose }: Props) {
  const [data, setData] = useState<VolumesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [includeUsage, setIncludeUsage] = useState(false);
  const [includeCost, setIncludeCost] = useState(false);
  const [view, setView] = useState<"volumes" | "apps" | "nfs">("volumes");
  const [query, setQuery] = useState(initialQuery ?? "");
  const [namespace, setNamespace] = useState("");
  const [pool, setPool] = useState("");
  const [source, setSource] = useState("");
  const [usageFilter, setUsageFilter] = useState("");
  const [zone, setZone] = useState("");
  const [redundancy, setRedundancy] = useState("");
  const [onlyIssues, setOnlyIssues] = useState(false);
  const [noSnapshot, setNoSnapshot] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [action, setAction] = useState<{ type: VolumeActionType; item: VolumeItem } | null>(null);
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
  const redundancies = useMemo(() => [...new Set(items.map((i) => i.storage?.redundancy).filter((v): v is string => !!v))].sort(), [items]);
  const pools = useMemo(() => [...new Set(items.flatMap((i) => i.node_pools).filter(Boolean))].sort(), [items]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = items.filter((i) => {
      if (namespace && i.namespace !== namespace) return false;
      if (pool && !i.node_pools.includes(pool)) return false;
      if (source && i.source !== source) return false;
      if (usageFilter && i.usage_class !== usageFilter) return false;
      if (zone && i.zone !== zone) return false;
      if (redundancy && i.storage?.redundancy !== redundancy) return false;
      if (onlyIssues && i.issues.length === 0) return false;
      if (noSnapshot && (i.kind !== "pvc" || i.snapshots)) return false;
      if (!q) return true;
      return [i.namespace, ...i.workloads, i.app_hint, i.pvc, i.pv, i.storage_class, i.source, i.source_detail, i.storage?.sku, i.storage?.account, ...i.pods, ...i.nodes].some((v) => v?.toLowerCase().includes(q));
    });
    const dir = sort.desc ? -1 : 1;
    return [...filtered].sort((a, b) => {
      const va = sortValue(a, sort.key), vb = sortValue(b, sort.key);
      return (typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb))) * dir;
    });
  }, [items, query, namespace, pool, source, usageFilter, zone, redundancy, onlyIssues, noSnapshot, sort]);

  function openAction(type: VolumeActionType, item: VolumeItem) {
    setAction({ type, item });
    setActionInput("");
    setActionError("");
  }

  async function runAction() {
    if (!action) return;
    const { type, item } = action;
    if (type === "snapshot" || type === "details") return;
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
            {s.snapshots_known && <span title={`PVCs com VolumeSnapshot pronto · API snapshot.storage.k8s.io/${s.snapshot_api ?? "?"}`}><b>{s.with_snapshot}</b> com snapshot</span>}
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
          {redundancies.length > 0 && (
            <select className={selectCls} value={redundancy} onChange={(e) => setRedundancy(e.target.value)} aria-label="Redundância do storage">
              <option value="">Toda redundância</option>
              {redundancies.map((n) => <option key={n} value={n}>{n}</option>)}
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
                      {i.managed_by && <div className="text-[10px] text-sky-300 font-sans" title="Controlador que gerencia este volume ou o workload dono: mudanças manuais podem ser revertidas">⎈ {i.managed_by}</div>}
                    </td>
                    <td className="px-2 py-1.5">{i.storage_class || "—"}</td>
                    <td className="px-2 py-1.5">
                      {i.source ? <span>{i.source}</span> : <span className="text-muted-foreground">—</span>}
                      {i.source_detail && <div className="font-mono text-[10px] text-muted-foreground [overflow-wrap:anywhere]" title={i.source_detail}>{i.source_detail}</div>}
                      {i.storage?.sku && (
                        <div className="text-[10px] text-muted-foreground" title={i.storage.redundancy_note || undefined}>
                          {i.storage.sku}{i.storage.tier ? ` · ${i.storage.tier}` : ""}{i.storage.protocol ? ` · ${i.storage.protocol}` : ""}
                        </div>
                      )}
                      {i.storage?.account && <div className="font-mono text-[10px] text-muted-foreground [overflow-wrap:anywhere]" title="Storage account">{i.storage.account}</div>}
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
                      {i.idle_days !== undefined && (
                        <div className="text-[10px] text-amber-300" title={`Visto ocioso desde ${i.idle_since ? new Date(i.idle_since).toLocaleString("pt-BR") : "?"}. O registro começa na primeira vez que este servidor listou o volume; antes disso só vale a idade.`}>
                          {i.idle_days >= 1 ? `ocioso há ${i.idle_days} d*` : "ocioso desde hoje*"}
                        </div>
                      )}
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
                      <VolumeRowActions cluster={cluster} item={i} snapshotApi={s?.snapshot_api} onAction={openAction} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {action?.type === "snapshot" && (
          <SnapshotDialog cluster={cluster} item={action.item} onClose={() => setAction(null)} onDone={() => setReloadKey((k) => k + 1)} />
        )}
        {action?.type === "details" && action.item.storage && <StorageDetailDialog cluster={cluster} item={action.item} onClose={() => setAction(null)} />}
        {action && action.type !== "snapshot" && action.type !== "details" && (
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
                    {action.item.managed_by && (
                      <div className="rounded border border-sky-500/40 bg-sky-500/10 px-2.5 py-1.5 text-sky-300">
                        Gerenciado por <b>{action.item.managed_by}</b>. Se o manifesto no Git declara outro tamanho, o controlador tentará voltar a ele; como o Kubernetes recusa reduzir, o reconcile pode passar a falhar até o manifesto ser atualizado também.
                      </div>
                    )}
                    {action.item.template_size && (
                      <div className="rounded border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-amber-300">
                        O <b>volumeClaimTemplate</b> do StatefulSet <span className="font-mono">{action.item.template_owner}</span> declara <b>{action.item.template_size}</b> e é imutável: réplicas novas continuarão nascendo com {action.item.template_size}. Para igualar é preciso recriar o StatefulSet (<span className="font-mono">kubectl delete sts {action.item.template_owner} --cascade=orphan</span>) com o novo tamanho no manifesto.
                      </div>
                    )}
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
                    {action.item.managed_by && (
                      <div className="rounded border border-sky-500/40 bg-sky-500/10 px-2.5 py-1.5 text-sky-300">
                        Gerenciado por <b>{action.item.managed_by}</b>: se o manifesto ainda declara este volume, o controlador pode recriá-lo vazio no próximo reconcile. Remova-o do Git também.
                      </div>
                    )}
                    {action.item.template_owner && (
                      <div className="rounded border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-amber-300">
                        PVC de StatefulSet (<span className="font-mono">{action.item.template_owner}</span>): ao subir a réplica, o StatefulSet cria um PVC novo, vazio.
                      </div>
                    )}
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
              {s?.snapshot_api && action.item.kind === "pvc" && !action.item.snapshots && (
                <div className="flex items-center justify-between gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-300">
                  <span>Este PVC não tem snapshot pronto.</span>
                  <Button variant="outline" size="sm" className="h-6 px-2 text-[11px] flex-shrink-0" onClick={() => openAction("snapshot", action.item)}>Tirar snapshot antes</Button>
                </div>
              )}
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
