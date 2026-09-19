// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell } from "recharts";
import { AlertTriangle, ChevronDown, ChevronUp, Copy, Check } from "lucide-react";
import { fmtBRL, fmtUSD } from "@/lib/finopsFormat";
import type { PVCCostItem, StorageSummary } from "./types";

export function StorageTab({ cluster, pvcs: pvcsRaw, storage }: { cluster: string; pvcs: PVCCostItem[]; storage: StorageSummary }) {
  const pvcs = pvcsRaw ?? [];
  const [filterNs, setFilterNs] = useState("all");
  const [filterType, setFilterType] = useState("all");
  const [sortBy, setSortBy] = useState<"cost" | "gb" | "name">("cost");
  const [orphanOpen, setOrphanOpen] = useState(true);
  const [copiedCmd, setCopiedCmd] = useState<string | null>(null);

  const namespaces = [...new Set(pvcs.map(p => p.namespace))].sort();
  const azureTypes = [...new Set(pvcs.map(p => p.azure_disk_type))].sort();

  const filtered = pvcs
    .filter(p => filterNs === "all" || p.namespace === filterNs)
    .filter(p => filterType === "all" || p.azure_disk_type === filterType)
    .sort((a, b) =>
      sortBy === "cost" ? b.monthly_cost_brl - a.monthly_cost_brl
      : sortBy === "gb" ? b.capacity_gb - a.capacity_gb
      : a.name.localeCompare(b.name)
    );

  const orphans = pvcs.filter(p => p.is_orphaned);

  const copyCmd = (cmd: string) => {
    navigator.clipboard.writeText(cmd).then(() => {
      setCopiedCmd(cmd);
      setTimeout(() => setCopiedCmd(null), 2000);
    });
  };

  const diskTypeColor: Record<string, string> = {
    "Premium SSD": "#6366f1",
    "Standard SSD": "#22c55e",
    "Standard HDD": "#f59e0b",
    "Azure Files Standard": "#0ea5e9",
    "Azure Files Premium": "#8b5cf6",
    "Azure Blob": "#ec4899",
    // GKE (Persistent Disk) — mesma paleta de "peso" do Azure: standard=verde, balanced=índigo,
    // ssd=violeta, extreme=rosa (por ordem crescente de performance/custo).
    "pd-standard": "#22c55e",
    "pd-balanced": "#6366f1",
    "pd-ssd": "#8b5cf6",
    "pd-extreme": "#ec4899",
  };

  const chartData = (storage.by_storage_class ?? []).map(sc => ({
    name: sc.azure_type.length > 18 ? sc.azure_type.slice(0, 16) + "…" : sc.azure_type,
    fullName: sc.azure_type,
    custo: Math.round(sc.monthly_cost_brl),
    gb: Math.round(sc.total_gb),
    pvcs: sc.pvc_count,
    color: diskTypeColor[sc.azure_type] ?? "#94a3b8",
  }));

  return (
    <div className="space-y-4">
      {/* Recursos de Dados (RG separado, fora do cluster K8s) agora é a sub-aba própria "Recursos de
          Dados" do FinOps — disponível sem precisar de "Analisar". */}
      {/* ── 4 KPI Cards (storage DENTRO do cluster — PVCs + disco OS) ──────────────────────── */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Storage Total/mês</p>
            <p className="text-lg font-bold text-purple-600 leading-tight">{fmtBRL(storage.total_monthly_cost_brl)}</p>
            <p className="text-[10px] text-muted-foreground">{fmtUSD(storage.total_monthly_cost_usd)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">PVCs</p>
            <p className="text-lg font-bold leading-tight">{storage.pvc_count}</p>
            <p className="text-[10px] text-muted-foreground">{storage.bound_pvc_count} montados · {Math.round(storage.total_capacity_gb)} GB</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Disco OS (node pools)</p>
            <p className="text-lg font-bold text-blue-600 leading-tight">{fmtBRL(storage.os_disk_cost_brl)}</p>
            <p className="text-[10px] text-muted-foreground">{fmtUSD(storage.os_disk_cost_usd)}</p>
          </CardContent>
        </Card>
        <Card className={storage.orphaned_pvc_count > 0 ? "border-red-200 dark:border-red-900/40" : ""}>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">PVCs Orfãos</p>
            <p className={`text-lg font-bold leading-tight ${storage.orphaned_pvc_count > 0 ? "text-red-500" : "text-green-500"}`}>
              {storage.orphaned_pvc_count}
            </p>
            <p className="text-[10px] text-muted-foreground">
              {storage.orphaned_cost_brl > 0 ? `${fmtBRL(storage.orphaned_cost_brl)}/mês desperdiçado` : "nenhum custo desperdiçado"}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* ── BarChart por tipo ─────────────────────────────────────────────────── */}
      {chartData.length > 0 && (
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Custo por Tipo de Storage (R$/mês)</CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <ResponsiveContainer width="100%" height={160}>
              <BarChart data={chartData} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border) / 0.4)" />
                <XAxis dataKey="name" tick={{ fontSize: 10 }} />
                <YAxis tick={{ fontSize: 10 }} tickFormatter={v => `R$${(v/1000).toFixed(0)}k`} />
                <Tooltip
                  cursor={{ fill: 'transparent' }}
                  content={({ active, payload }) => {
                    if (!active || !payload?.length) return null;
                    const d = payload[0].payload;
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12 }}>
                        <p className="font-semibold mb-1">{d.fullName}</p>
                        <p>Custo: <strong>{fmtBRL(d.custo)}</strong></p>
                        <p>Capacidade: <strong>{d.gb} GB</strong></p>
                        <p>PVCs: <strong>{d.pvcs}</strong></p>
                      </div>
                    );
                  }}
                />
                <Bar dataKey="custo" radius={[3, 3, 0, 0]} maxBarSize={40}>
                  {chartData.map((e, i) => <Cell key={i} fill={e.color} />)}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      {/* ── Alerta orfãos ─────────────────────────────────────────────────────── */}
      {storage.orphaned_cost_brl > 0 && (
        <Alert className="border-red-200 dark:border-red-900/40 bg-red-50/50 dark:bg-red-950/10">
          <AlertTriangle className="h-4 w-4 text-red-500" />
          <AlertDescription className="text-sm">
            <strong>{storage.orphaned_pvc_count} PVC(s) orfão(s)</strong> sem pod montando —{" "}
            <strong className="text-red-500">{fmtBRL(storage.orphaned_cost_brl)}/mês</strong> desperdiçado.
            {orphans.some(p => p.reclaim_policy === "Retain") && (
              <span className="ml-1 text-orange-500">Atenção: alguns têm <code className="text-xs">reclaimPolicy: Retain</code> — o disco persiste após deletar o PVC.</span>
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* ── Tabela de PVCs ───────────────────────────────────────────────────── */}
      <Card>
        <CardHeader className="pb-2 pt-3 px-4">
          <div className="flex items-center justify-between flex-wrap gap-2">
            <CardTitle className="text-sm">PVCs ({filtered.length})</CardTitle>
            <div className="flex items-center gap-2 flex-wrap">
              <Select value={filterNs} onValueChange={setFilterNs}>
                <SelectTrigger className="h-7 text-xs w-36"><SelectValue placeholder="Namespace" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos namespaces</SelectItem>
                  {namespaces.map(ns => <SelectItem key={ns} value={ns}>{ns}</SelectItem>)}
                </SelectContent>
              </Select>
              <Select value={filterType} onValueChange={setFilterType}>
                <SelectTrigger className="h-7 text-xs w-36"><SelectValue placeholder="Tipo" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Todos os tipos</SelectItem>
                  {azureTypes.map(t => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                </SelectContent>
              </Select>
              <Select value={sortBy} onValueChange={v => setSortBy(v as "cost" | "gb" | "name")}>
                <SelectTrigger className="h-7 text-xs w-28"><SelectValue placeholder="Ordenar" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="cost">Por custo</SelectItem>
                  <SelectItem value="gb">Por tamanho</SelectItem>
                  <SelectItem value="name">Por nome</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardHeader>
        <ScrollArea className="h-72">
          <Table>
            <TableHeader>
              <TableRow className="text-[11px]">
                <TableHead>Namespace / Nome</TableHead>
                <TableHead>Storage Class</TableHead>
                <TableHead className="text-center">Tipo de Disco</TableHead>
                <TableHead className="text-center">Tier</TableHead>
                <TableHead className="text-right">GB</TableHead>
                <TableHead className="text-right">R$/mês</TableHead>
                <TableHead>Workload</TableHead>
                <TableHead className="text-center">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((p, i) => (
                <TableRow key={i} className="text-xs">
                  <TableCell className="max-w-[160px]">
                    <p className="text-muted-foreground text-[10px] truncate">{p.namespace}</p>
                    <p className="font-medium truncate">{p.name}</p>
                  </TableCell>
                  <TableCell className="font-mono text-[10px] max-w-[100px] truncate">{p.storage_class || "—"}</TableCell>
                  <TableCell className="text-center text-[10px]">
                    <span style={{ color: diskTypeColor[p.azure_disk_type] ?? undefined }}>{p.azure_disk_type || "—"}</span>
                  </TableCell>
                  <TableCell className="text-center font-mono text-[10px]">{p.azure_disk_tier || "—"}</TableCell>
                  <TableCell className="text-right font-mono">{p.capacity_gb.toFixed(0)}</TableCell>
                  <TableCell className="text-right font-semibold">{fmtBRL(p.monthly_cost_brl)}</TableCell>
                  <TableCell className="text-xs max-w-[120px] truncate text-muted-foreground">{p.workload_ref || "—"}</TableCell>
                  <TableCell className="text-center">
                    {p.is_orphaned ? (
                      <Badge variant="destructive" className="text-[9px]">orfão</Badge>
                    ) : (
                      <Badge variant="outline" className="text-[9px] text-green-600 border-green-300">{p.phase}</Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
      </Card>

      {/* ── PVCs Orfãos ─────────────────────────────────────────────────────── */}
      {orphans.length > 0 && (
        <Card className="border-red-200 dark:border-red-900/40">
          <CardHeader className="pb-1 pt-3 px-4">
            <div className="flex items-center justify-between cursor-pointer" onClick={() => setOrphanOpen(o => !o)}>
              <CardTitle className="text-sm flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 text-red-500" />
                PVCs Orfãos ({orphans.length})
                <span className="text-[10px] font-normal text-muted-foreground">sem pod montando</span>
              </CardTitle>
              {orphanOpen ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />}
            </div>
          </CardHeader>
          {orphanOpen && (
            <CardContent className="px-4 pb-4 space-y-2">
              {orphans.map((p, i) => {
                const cmd = `kubectl delete pvc ${p.name} -n ${p.namespace}`;
                const isCopied = copiedCmd === cmd;
                return (
                  <div key={i} className="flex items-start justify-between gap-2 p-2 rounded border border-red-100 dark:border-red-900/30 bg-red-50/30 dark:bg-red-950/10">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-medium text-xs">{p.namespace}/{p.name}</span>
                        <span className="text-[10px] text-muted-foreground">{p.azure_disk_type} · {p.capacity_gb.toFixed(0)} GB</span>
                        <span className="font-semibold text-xs text-red-500">{fmtBRL(p.monthly_cost_brl)}/mês</span>
                        {p.reclaim_policy === "Retain" && (
                          <Badge variant="outline" className="text-[9px] text-orange-500 border-orange-300">Retain — disco persiste</Badge>
                        )}
                      </div>
                      <p className="text-[10px] font-mono text-muted-foreground mt-1">{cmd}</p>
                    </div>
                    <Button variant="ghost" size="sm" className="h-6 w-6 p-0 shrink-0" onClick={() => copyCmd(cmd)}>
                      {isCopied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
                    </Button>
                  </div>
                );
              })}
            </CardContent>
          )}
        </Card>
      )}
    </div>
  );
}
