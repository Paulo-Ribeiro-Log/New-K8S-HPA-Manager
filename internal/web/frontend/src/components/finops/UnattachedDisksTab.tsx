import { Fragment, useMemo, useState } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AlertTriangle, ChevronDown, ChevronRight, HardDrive, Info, Loader2, RefreshCw, Search } from "lucide-react";
import { fmtBRL, fmtUSD, KubectlBlock } from "@/lib/finopsFormat";
import { useUnattachedDisks } from "@/hooks/useUnattachedDisks";
import type { UnattachedDiskItem, UnattachedDiskVerdict } from "./types";

// Discos (Azure Managed Disk / GCP Persistent Disk / AWS EBS) que existem na conta do cloud mas não
// estão atachados a nenhuma VM. Só leitura — a app nunca exclui nada; cada disco traz o comando de
// exclusão pra copiar. O veredito vem do backend (cruza com os PVs do cluster selecionado).

const VERDICT_META: Record<UnattachedDiskVerdict, { label: string; badge: string; help: string }> = {
  candidate: {
    label: "Candidato a exclusão",
    badge: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
    help: "Criado por este cluster e sem PV que o use (ou PV Released/Available).",
  },
  review: {
    label: "Revisar",
    badge: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400",
    help: "Sem evidência suficiente: disco fora do K8s, de outro cluster ou desatachado há pouco tempo.",
  },
  in_use_by_pv: {
    label: "Em uso por PV",
    badge: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
    help: "Um PV Bound deste cluster o referencia — está desatachado só porque nenhum pod o monta agora.",
  },
};

type VerdictFilter = "all" | UnattachedDiskVerdict;
type ScopeFilter = "all" | "this" | "other" | "external";
type SortKey = "cost" | "size" | "age";

// "Este cluster" = PV daqui aponta pro disco ou a pista de cluster bate; "Outros" = criado pelo K8s
// mas sem vínculo com este cluster; "Sem vínculo K8s" = disco criado fora do Kubernetes.
const scopeOf = (d: UnattachedDiskItem): Exclude<ScopeFilter, "all"> =>
  d.cluster_match ? "this" : d.origin === "external" ? "external" : "other";

const locationOf = (d: UnattachedDiskItem) =>
  [d.resource_group, d.zone || d.location].filter(Boolean).join(" · ") || "—";

const ageLabel = (d: UnattachedDiskItem) => {
  if (!d.age_basis) return "—";
  return `${d.age_days} d`;
};
const ageTitle = (d: UnattachedDiskItem) =>
  d.age_basis === "unattached"
    ? "Dias desde que o disco foi desatachado"
    : d.age_basis === "created"
      ? "Dias desde a criação do disco (o cloud não informa desde quando está desatachado)"
      : "";

const priceNote = (d: UnattachedDiskItem) => {
  switch (d.price_source) {
    case "unsupported": return "Sem preço estimado para este tipo de disco";
    case "unpriced": return "Sem preço disponível";
    case "fallback": return "Preço de referência (API de preços indisponível)";
    case "table": return "Preço de tabela (on-demand, região de referência) — inclui IOPS/throughput provisionados";
    default: return "";
  }
};

export function UnattachedDisksTab({ cluster }: { cluster: string }) {
  const { data: report, isFetching, error, refresh } = useUnattachedDisks(cluster);

  const [verdict, setVerdict] = useState<VerdictFilter>("all");
  const [scope, setScope] = useState<ScopeFilter>("all");
  const [sortBy, setSortBy] = useState<SortKey>("cost");
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const disks = useMemo(() => report?.disks ?? [], [report]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return disks
      .filter(d => verdict === "all" || d.verdict === verdict)
      .filter(d => scope === "all" || scopeOf(d) === scope)
      .filter(d => {
        if (!q) return true;
        return [d.name, d.pvc, d.k8s_pvc_name, d.k8s_pvc_namespace, d.resource_group, d.k8s_cluster_hint, d.disk_type]
          .some(v => v?.toLowerCase().includes(q));
      })
      .sort((a, b) =>
        sortBy === "cost" ? b.monthly_cost_brl - a.monthly_cost_brl || b.size_gb - a.size_gb
        : sortBy === "size" ? b.size_gb - a.size_gb
        : b.age_days - a.age_days);
  }, [disks, verdict, scope, search, sortBy]);

  const scopeCounts = useMemo(() => {
    const c = { this: 0, other: 0, external: 0 };
    for (const d of disks) c[scopeOf(d)]++;
    return c;
  }, [disks]);

  const filteredCost = filtered.reduce((sum, d) => sum + d.monthly_cost_brl, 0);

  if (isFetching && !report) {
    return (
      <div className="flex items-center justify-center gap-3 py-16 text-muted-foreground">
        <Loader2 className="h-5 w-5 animate-spin" />
        <span>Consultando discos desatachados no cloud e cruzando com os PVs do cluster...</span>
      </div>
    );
  }

  if (error && !report) {
    return (
      <div className="space-y-3">
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>{(error as Error).message}</AlertDescription>
        </Alert>
        <Button size="sm" variant="outline" className="gap-1" onClick={() => void refresh()}>
          <RefreshCw className="h-3.5 w-3.5" /> Tentar novamente
        </Button>
      </div>
    );
  }

  if (!report) return null;
  const s = report.summary;

  return (
    <div className="space-y-4">
      {/* Cabeçalho: escopo varrido + atualizar */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="text-xs text-muted-foreground flex items-center gap-2 flex-wrap">
          <HardDrive className="h-3.5 w-3.5 shrink-0" />
          <span>{report.scope}</span>
          <span>· varrido às {new Date(report.scanned_at).toLocaleTimeString("pt-BR")}{report.from_cache ? " (cache de até 5 min)" : ""}</span>
          <span>· câmbio R$ {report.exchange_rate.toFixed(4)}</span>
        </div>
        <Button size="sm" variant="outline" className="h-8 gap-1" disabled={isFetching} onClick={() => void refresh()}>
          {isFetching ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          Atualizar
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>Falha ao atualizar: {(error as Error).message} (exibindo a última varredura)</AlertDescription>
        </Alert>
      )}

      {!report.pv_cross_ref && (
        <Alert className="border-amber-300 bg-amber-50 dark:bg-amber-950/20">
          <AlertTriangle className="h-4 w-4 text-amber-600" />
          <AlertDescription className="text-sm text-amber-800 dark:text-amber-300">
            Não foi possível cruzar com os PVs deste cluster — nenhum disco é marcado como candidato a exclusão nesta varredura.
          </AlertDescription>
        </Alert>
      )}

      {/* KPIs */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Discos desatachados</p>
            <p className="text-lg font-bold leading-tight">{s.total_count}</p>
            <p className="text-[10px] text-muted-foreground">{Math.round(s.total_size_gb)} GB no total</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo total/mês</p>
            <p className="text-lg font-bold text-purple-600 leading-tight">{fmtBRL(s.total_cost_brl)}</p>
            <p className="text-[10px] text-muted-foreground">
              {fmtUSD(s.total_cost_usd)}{s.unpriced_count > 0 ? ` · ${s.unpriced_count} sem preço` : ""}
            </p>
          </CardContent>
        </Card>
        <Card className={s.candidate_count > 0 ? "border-red-200 dark:border-red-900/40" : ""}>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Candidatos a exclusão</p>
            <p className={`text-lg font-bold leading-tight ${s.candidate_count > 0 ? "text-red-500" : "text-green-500"}`}>{s.candidate_count}</p>
            <p className="text-[10px] text-muted-foreground">
              {s.candidate_cost_brl > 0 ? `${fmtBRL(s.candidate_cost_brl)}/mês desperdiçado` : "nenhum desperdício confirmado"}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Revisar · Em uso por PV</p>
            <p className="text-lg font-bold leading-tight">
              <span className="text-amber-600">{s.review_count}</span>
              <span className="text-muted-foreground"> · </span>
              <span className="text-blue-600">{s.in_use_by_pv_count}</span>
            </p>
            <p className="text-[10px] text-muted-foreground">{fmtBRL(s.review_cost_brl)} · {fmtBRL(s.in_use_by_pv_cost_brl)}/mês</p>
          </CardContent>
        </Card>
      </div>

      {/* Notas/limitações da estimativa */}
      {report.warnings.length > 0 && (
        <div className="space-y-1">
          {report.warnings.map((w, i) => (
            <p key={i} className="text-[11px] text-muted-foreground flex items-start gap-1.5">
              <Info className="h-3 w-3 mt-0.5 shrink-0" />{w}
            </p>
          ))}
        </div>
      )}

      {s.total_count === 0 ? (
        <div className="text-center py-12 text-muted-foreground text-sm">
          Nenhum disco desatachado encontrado neste escopo.
        </div>
      ) : (
        <>
          {/* Filtros */}
          <div className="flex items-center gap-2 flex-wrap">
            <div className="relative">
              <Search className="h-3.5 w-3.5 absolute left-2 top-2 text-muted-foreground" />
              <Input value={search} onChange={e => setSearch(e.target.value)} placeholder="Buscar disco, PVC, resource group..." className="h-8 w-64 pl-7 text-xs" />
            </div>
            <Select value={verdict} onValueChange={v => setVerdict(v as VerdictFilter)}>
              <SelectTrigger className="h-8 w-48 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos os vereditos ({s.total_count})</SelectItem>
                <SelectItem value="candidate">Candidatos ({s.candidate_count})</SelectItem>
                <SelectItem value="review">Revisar ({s.review_count})</SelectItem>
                <SelectItem value="in_use_by_pv">Em uso por PV ({s.in_use_by_pv_count})</SelectItem>
              </SelectContent>
            </Select>
            <Select value={scope} onValueChange={v => setScope(v as ScopeFilter)}>
              <SelectTrigger className="h-8 w-52 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Toda a conta ({s.total_count})</SelectItem>
                <SelectItem value="this">Deste cluster ({scopeCounts.this})</SelectItem>
                <SelectItem value="other">De outros clusters ({scopeCounts.other})</SelectItem>
                <SelectItem value="external">Sem vínculo com K8s ({scopeCounts.external})</SelectItem>
              </SelectContent>
            </Select>
            <Select value={sortBy} onValueChange={v => setSortBy(v as SortKey)}>
              <SelectTrigger className="h-8 w-36 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="cost">Ordenar: custo</SelectItem>
                <SelectItem value="size">Ordenar: tamanho</SelectItem>
                <SelectItem value="age">Ordenar: idade</SelectItem>
              </SelectContent>
            </Select>
            <span className="text-xs text-muted-foreground ml-auto">
              {filtered.length} de {s.total_count} · {fmtBRL(filteredCost)}/mês
            </span>
          </div>

          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-6" />
                    <TableHead>Disco</TableHead>
                    <TableHead>Tipo</TableHead>
                    <TableHead className="text-right">GB</TableHead>
                    <TableHead>Local</TableHead>
                    <TableHead className="text-right">Idade</TableHead>
                    <TableHead className="text-right">Custo/mês</TableHead>
                    <TableHead>Veredito</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={8} className="text-center text-xs text-muted-foreground py-8">
                        Nenhum disco corresponde aos filtros.
                      </TableCell>
                    </TableRow>
                  )}
                  {filtered.map(d => {
                    const open = expanded === d.id;
                    const meta = VERDICT_META[d.verdict];
                    const pvcLabel = d.pvc || (d.k8s_pvc_name ? `${d.k8s_pvc_namespace ? d.k8s_pvc_namespace + "/" : ""}${d.k8s_pvc_name}` : "");
                    return (
                      <Fragment key={d.id}>
                        <TableRow className="cursor-pointer text-xs" onClick={() => setExpanded(open ? null : d.id)}>
                          <TableCell className="px-2">
                            {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                          </TableCell>
                          <TableCell className="max-w-[280px]">
                            <div className="font-mono truncate" title={d.name}>{d.name}</div>
                            {pvcLabel && <div className="text-[10px] text-muted-foreground truncate" title={pvcLabel}>PVC {pvcLabel}</div>}
                          </TableCell>
                          <TableCell className="whitespace-nowrap">{d.disk_type}</TableCell>
                          <TableCell className="text-right">{Math.round(d.size_gb)}</TableCell>
                          <TableCell className="max-w-[220px] truncate text-muted-foreground" title={locationOf(d)}>{locationOf(d)}</TableCell>
                          <TableCell className="text-right whitespace-nowrap" title={ageTitle(d)}>{ageLabel(d)}</TableCell>
                          <TableCell className="text-right whitespace-nowrap" title={priceNote(d)}>
                            {d.price_source === "unsupported" || d.price_source === "unpriced" ? "—" : fmtBRL(d.monthly_cost_brl)}
                            {d.price_source === "table" && <span className="ml-1 text-[9px] text-muted-foreground align-top">tabela</span>}
                          </TableCell>
                          <TableCell>
                            <Badge className={`text-[10px] font-medium ${meta.badge}`} title={meta.help}>{meta.label}</Badge>
                          </TableCell>
                        </TableRow>
                        {open && (
                          <TableRow className="bg-muted/30 hover:bg-muted/30">
                            <TableCell />
                            <TableCell colSpan={7} className="space-y-2 py-3">
                              <p className="text-xs">{d.reason}</p>
                              <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-0.5 text-[11px] text-muted-foreground">
                                {d.pv_name && <span>PV: <strong className="text-foreground">{d.pv_name}</strong> ({d.pv_phase}{d.reclaim_policy ? `, reclaim ${d.reclaim_policy}` : ""})</span>}
                                {d.storage_class && <span>StorageClass: <strong className="text-foreground">{d.storage_class}</strong></span>}
                                {d.k8s_cluster_hint && <span>Pista de cluster: <strong className="text-foreground break-all">{d.k8s_cluster_hint}</strong></span>}
                                {d.created_at && <span>Criado em: {new Date(d.created_at).toLocaleString("pt-BR")}</span>}
                                {d.unattached_since && <span>Desatachado desde: {new Date(d.unattached_since).toLocaleString("pt-BR")}</span>}
                                <span>Custo: {fmtUSD(d.monthly_cost_usd)}/mês{d.price_source === "fallback" ? " (referência)" : d.price_source === "table" ? " (preço de tabela)" : ""}</span>
                                {(d.provisioned_iops || d.provisioned_mbps) ? (
                                  <span>Performance provisionada: {d.provisioned_iops ? `${Math.round(d.provisioned_iops)} IOPS` : ""}{d.provisioned_iops && d.provisioned_mbps ? " · " : ""}{d.provisioned_mbps ? `${Math.round(d.provisioned_mbps)} MB/s` : ""}</span>
                                ) : null}
                                <span className="break-all">ID: {d.id}</span>
                              </div>
                              {d.delete_command ? (
                                <div className="space-y-1">
                                  <p className="text-[11px] text-muted-foreground">
                                    Comando de exclusão (a app não exclui nada). Considere criar um snapshot antes e confirme que não há dado necessário:
                                  </p>
                                  <KubectlBlock cmd={d.delete_command} />
                                </div>
                              ) : (
                                <p className="text-[11px] text-muted-foreground">Sem comando de exclusão sugerido: remova o PVC/PV pelo cluster, não o disco direto.</p>
                              )}
                            </TableCell>
                          </TableRow>
                        )}
                      </Fragment>
                    );
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
