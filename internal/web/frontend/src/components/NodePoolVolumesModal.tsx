import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, ArrowDown, ArrowUp, Database, Download, Loader2, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { apiClient, type VolumeItem, type VolumesResponse } from "@/lib/api/client";

// Volumes do cluster (PV/PVC) ligados à aplicação, node e node pool. Somente leitura.

interface Props {
  cluster: string;
  resourceLabel: string; // "Node Pools" ou "Node Groups"
  onClose: () => void;
}

type SortKey = "namespace" | "workload" | "pvc" | "storage_class" | "source" | "capacity" | "pool" | "issues";

const OWNER_SOURCE_LABEL: Record<string, string> = { pod: "pod", spec: "spec do workload", statefulset: "volumeClaimTemplate", label: "label" };
const OWNER_SOURCE_HELP: Record<string, string> = {
  pod: "Um pod em execução monta este volume",
  spec: "O pod template do workload referencia este volume (pode estar com 0 réplicas ou entre execuções)",
  statefulset: "O nome do PVC segue o volumeClaimTemplate de um StatefulSet",
  label: "Deduzido pelas labels app/app.kubernetes.io/name do PVC",
};
const GIB = 1024 ** 3;
const fmtGiB = (b: number) => (b ? `${(b / GIB).toFixed(b >= 10 * GIB ? 0 : 1)} GiB` : "—");

function sortValue(it: VolumeItem, k: SortKey): string | number {
  switch (k) {
    case "namespace": return it.namespace;
    case "workload": return it.workloads[0] ?? it.app_hint ?? "\uffff";
    case "pvc": return it.pvc || it.pv || "";
    case "storage_class": return it.storage_class;
    case "source": return `${it.source ?? ""} ${it.source_detail ?? ""}`;
    case "capacity": return it.capacity_bytes;
    case "pool": return it.node_pools.join(",");
    case "issues": return it.issues.length;
  }
}

function csvCell(v: string | number): string {
  const s = String(v);
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function toCSV(items: VolumeItem[]): string {
  const head = ["tipo", "namespace", "aplicações", "origem_do_vinculo", "pvc", "status_pvc", "pv", "fase_pv", "storage_class", "origem", "origem_detalhe", "capacidade_gib", "access_modes", "reclaim", "pods", "nodes", "node_pools", "uso_gib", "uso_%", "problemas"];
  const rows = items.map((i) => [
    i.kind, i.namespace, i.workloads.length ? i.workloads.join(" ") : (i.app_hint ? `(label) ${i.app_hint}` : ""), i.owner_source ?? "", i.pvc ?? "", i.pvc_status ?? "", i.pv ?? "", i.pv_phase ?? "",
    i.storage_class, i.source ?? "", i.source_detail ?? "", (i.capacity_bytes / GIB).toFixed(2), i.access_modes.join(" "), i.reclaim_policy ?? "", i.pods.join(" "), i.nodes.join(" "),
    i.node_pools.join(" "), i.used_bytes ? (i.used_bytes / GIB).toFixed(2) : "", i.usage_percentage ? i.usage_percentage.toFixed(1) : "", i.issues.join(" | "),
  ]);
  return [head, ...rows].map((r) => r.map(csvCell).join(";")).join("\n");
}

export function NodePoolVolumesModal({ cluster, resourceLabel, onClose }: Props) {
  const [data, setData] = useState<VolumesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [includeUsage, setIncludeUsage] = useState(false);
  const [query, setQuery] = useState("");
  const [namespace, setNamespace] = useState("");
  const [pool, setPool] = useState("");
  const [source, setSource] = useState("");
  const [onlyIssues, setOnlyIssues] = useState(false);
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "namespace", desc: false });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    apiClient
      .getNodePoolVolumes(cluster, includeUsage)
      .then((r) => { if (!cancelled) setData(r); })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao listar os volumes"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [cluster, includeUsage]);

  const items = data?.items ?? [];
  const namespaces = useMemo(() => [...new Set(items.map((i) => i.namespace).filter(Boolean))].sort(), [items]);
  const sources = useMemo(() => [...new Set(items.map((i) => i.source).filter((v): v is string => !!v))].sort(), [items]);
  const pools = useMemo(() => [...new Set(items.flatMap((i) => i.node_pools).filter(Boolean))].sort(), [items]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = items.filter((i) => {
      if (namespace && i.namespace !== namespace) return false;
      if (pool && !i.node_pools.includes(pool)) return false;
      if (source && i.source !== source) return false;
      if (onlyIssues && i.issues.length === 0) return false;
      if (!q) return true;
      return [i.namespace, ...i.workloads, i.app_hint, i.pvc, i.pv, i.storage_class, i.source, i.source_detail, ...i.pods, ...i.nodes].some((v) => v?.toLowerCase().includes(q));
    });
    const dir = sort.desc ? -1 : 1;
    return [...filtered].sort((a, b) => {
      const va = sortValue(a, sort.key), vb = sortValue(b, sort.key);
      return (typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb))) * dir;
    });
  }, [items, query, namespace, pool, source, onlyIssues, sort]);

  function toggleSort(key: SortKey) {
    setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: false }));
  }

  function exportCSV() {
    const blob = new Blob(["﻿" + toCSV(rows)], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `volumes-${cluster.replace(/[^\w.-]+/g, "_")}.csv`;
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
          <select className={selectCls} value={source} onChange={(e) => setSource(e.target.value)} aria-label="Tipo de volume">
            <option value="">Todos os tipos</option>
            {sources.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
          <label className="flex items-center gap-1.5 text-xs cursor-pointer">
            <Checkbox checked={onlyIssues} onCheckedChange={(v) => setOnlyIssues(v === true)} />Só com problema
          </label>
          <label className="flex items-center gap-1.5 text-xs cursor-pointer" title="Consulta o Prometheus do cluster (kubelet_volume_stats); pode demorar alguns segundos">
            <Checkbox checked={includeUsage} onCheckedChange={(v) => setIncludeUsage(v === true)} />Incluir uso (Prometheus)
          </label>
          <div className="flex-1" />
          <span className="text-xs text-muted-foreground">{rows.length} de {items.length}</span>
          <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={exportCSV} disabled={rows.length === 0}>
            <Download className="w-3 h-3" />CSV
          </Button>
        </div>

        <div className="flex-1 min-h-0 overflow-auto border border-border/40 rounded">
          {loading ? (
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
                  {includeUsage && <th className="text-left font-medium px-2 py-1.5">Uso</th>}
                  <Th k="pool">{resourceLabel.replace(/s$/, "")}</Th>
                  <Th k="issues">Situação</Th>
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
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap">
                      {fmtGiB(i.capacity_bytes)}
                      <div className="text-[10px] text-muted-foreground">{i.access_modes.join(", ")}</div>
                    </td>
                    {includeUsage && (
                      <td className="px-2 py-1.5 whitespace-nowrap">
                        {i.used_bytes ? `${fmtGiB(i.used_bytes)} (${(i.usage_percentage ?? 0).toFixed(0)}%)` : <span className="text-muted-foreground">—</span>}
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
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
