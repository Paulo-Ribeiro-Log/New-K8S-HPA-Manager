import { Fragment, useMemo, useState } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ChevronDown, ChevronRight, Search } from "lucide-react";
import { fmtBRL, fmtUSD, KubectlBlock } from "@/lib/finopsFormat";
import type { OrphanResource, OrphanSummary, ScopedResourceGroup } from "./types";

// Recursos Azure sem uso (fora discos) nos RGs das jornadas: NIC sem VM, IP público sem
// associação, Private Endpoint desconectado, VM desalocada/parada, NSG/LB/NAT/route table sem
// vínculo... Só leitura — cada item traz o comando de exclusão para copiar.

const ORPHAN_TYPE_LABEL: Record<string, string> = {
  "microsoft.compute/virtualmachines": "VM",
  "microsoft.network/networkinterfaces": "NIC",
  "microsoft.network/publicipaddresses": "IP público",
  "microsoft.network/privateendpoints": "Private Endpoint",
  "microsoft.network/networksecuritygroups": "NSG",
  "microsoft.network/loadbalancers": "Load Balancer",
  "microsoft.network/applicationgateways": "Application Gateway",
  "microsoft.network/natgateways": "NAT Gateway",
  "microsoft.network/routetables": "Route table",
  "microsoft.network/privatednszones": "Private DNS zone",
  "microsoft.web/serverfarms": "App Service Plan",
  "microsoft.compute/availabilitysets": "Availability Set",
};
const typeLabel = (t: string) => ORPHAN_TYPE_LABEL[t] ?? t;

const ageLabel = (o: OrphanResource) =>
  o.since_basis === "unknown" ? "—" : o.since_basis === "no_change_14d" ? `≥ ${o.age_days} d` : `${o.age_days} d`;
const ageTitle = (o: OrphanResource) =>
  o.since_basis === "unknown"
    ? "Histórico de alterações indisponível"
    : o.since_basis === "no_change_14d"
      ? "Nenhuma alteração nos últimos 14 dias (limite do histórico do Azure)"
      : `Última alteração em ${new Date(o.last_change!).toLocaleString("pt-BR")} — o Azure não registra "órfão desde" para este tipo`;

export function OrphanResourcesPanel({
  orphans,
  summary,
  minAgeDays,
}: {
  orphans: OrphanResource[];
  summary: OrphanSummary;
  minAgeDays: number;
}) {
  const [onlyAged, setOnlyAged] = useState(true);
  const [type, setType] = useState("all");
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const types = useMemo(() => Object.entries(summary.by_type).sort((a, b) => b[1] - a[1]), [summary]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return orphans
      .filter(o => !onlyAged || !o.recent)
      .filter(o => type === "all" || o.type === type)
      .filter(o => !q || [o.name, o.resource_group, o.journey, o.reason, o.sku].some(v => v?.toLowerCase().includes(q)));
  }, [orphans, onlyAged, type, search]);
  const filteredCost = filtered.reduce((sum, o) => sum + o.monthly_cost_brl, 0);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Recursos órfãos</p>
            <p className="text-lg font-bold leading-tight">{summary.total_count}</p>
            <p className="text-[10px] text-muted-foreground">{types.length} tipo(s)</p>
          </CardContent>
        </Card>
        <Card className={summary.aged_count > 0 ? "border-red-200 dark:border-red-900/40" : ""}>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Sem alteração há {minAgeDays}+ dias</p>
            <p className={`text-lg font-bold leading-tight ${summary.aged_count > 0 ? "text-red-500" : "text-green-500"}`}>{summary.aged_count}</p>
            <p className="text-[10px] text-muted-foreground">{fmtBRL(summary.aged_cost_brl)}/mês estimado</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Alterados há menos de {minAgeDays} dias</p>
            <p className="text-lg font-bold leading-tight text-amber-600">{summary.recent_count}</p>
            <p className="text-[10px] text-muted-foreground">podem estar em troca — revisar depois</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo estimado/mês</p>
            <p className="text-lg font-bold text-purple-600 leading-tight">{fmtBRL(summary.total_cost_brl)}</p>
            <p className="text-[10px] text-muted-foreground">preço de lista; tipos sem custo fixo = R$ 0</p>
          </CardContent>
        </Card>
      </div>

      {summary.total_count === 0 ? (
        <div className="text-center py-12 text-muted-foreground text-sm">Nenhum recurso órfão encontrado nos resource groups deste escopo.</div>
      ) : (
        <>
          <div className="flex items-center gap-2 flex-wrap">
            <div className="relative">
              <Search className="h-3.5 w-3.5 absolute left-2 top-2 text-muted-foreground" />
              <Input value={search} onChange={e => setSearch(e.target.value)} placeholder="Buscar recurso, RG, jornada, motivo..." className="h-8 w-64 pl-7 text-xs" />
            </div>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger className="h-8 w-52 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos os tipos ({summary.total_count})</SelectItem>
                {types.map(([t, n]) => (
                  <SelectItem key={t} value={t}>{typeLabel(t)} ({n})</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <label className="flex items-center gap-2 text-xs cursor-pointer">
              <Switch checked={onlyAged} onCheckedChange={setOnlyAged} />
              Só sem alteração há {minAgeDays}+ dias
            </label>
            <span className="text-xs text-muted-foreground ml-auto">
              {filtered.length} de {summary.total_count} · {fmtBRL(filteredCost)}/mês
            </span>
          </div>

          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-6" />
                    <TableHead>Recurso</TableHead>
                    <TableHead>Motivo</TableHead>
                    <TableHead>Resource group</TableHead>
                    <TableHead className="text-right">Idade</TableHead>
                    <TableHead className="text-right">Custo/mês</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={6} className="text-center text-xs text-muted-foreground py-8">Nenhum recurso corresponde aos filtros.</TableCell>
                    </TableRow>
                  )}
                  {filtered.map(o => {
                    const open = expanded === o.id;
                    return (
                      <Fragment key={o.id}>
                        <TableRow className="cursor-pointer text-xs" onClick={() => setExpanded(open ? null : o.id)}>
                          <TableCell className="px-2">{open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}</TableCell>
                          <TableCell className="max-w-[260px]">
                            <div className="font-mono truncate" title={o.name}>{o.name}</div>
                            <div className="text-[10px] text-muted-foreground">{typeLabel(o.type)}{o.sku ? ` · ${o.sku}` : ""}</div>
                          </TableCell>
                          <TableCell className="max-w-[280px]">
                            <span className="line-clamp-2">{o.reason}</span>
                          </TableCell>
                          <TableCell className="max-w-[220px]">
                            <div className="truncate" title={o.resource_group}>{o.resource_group}</div>
                            {o.journey && <div className="text-[10px] text-muted-foreground">{o.journey}</div>}
                          </TableCell>
                          <TableCell className="text-right whitespace-nowrap" title={ageTitle(o)}>
                            {ageLabel(o)}
                            {o.recent && <Badge variant="outline" className="ml-1 text-[9px] border-amber-500/40 text-amber-600">recente</Badge>}
                          </TableCell>
                          <TableCell className="text-right whitespace-nowrap" title={o.price_note}>
                            {o.monthly_cost_brl > 0 ? fmtBRL(o.monthly_cost_brl) : "—"}
                          </TableCell>
                        </TableRow>
                        {open && (
                          <TableRow className="bg-muted/30 hover:bg-muted/30">
                            <TableCell />
                            <TableCell colSpan={5} className="space-y-2 py-3">
                              <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-0.5 text-[11px] text-muted-foreground">
                                <span>Tipo ARM: <strong className="text-foreground">{o.type}</strong></span>
                                {o.location && <span>Região: <strong className="text-foreground">{o.location}</strong></span>}
                                <span>Subscription: <strong className="text-foreground font-mono">{o.subscription_id}</strong></span>
                                <span>{ageTitle(o)}</span>
                                {o.price_note && <span>Custo: {o.monthly_cost_usd > 0 ? `${fmtUSD(o.monthly_cost_usd)}/mês — ` : ""}{o.price_note}</span>}
                                <span className="break-all">ID: {o.id}</span>
                              </div>
                              {o.delete_command && (
                                <div className="space-y-1">
                                  <p className="text-[11px] text-muted-foreground">
                                    Comando de exclusão (a app não exclui nada). Confirme com o dono do recurso antes:
                                  </p>
                                  <KubectlBlock cmd={o.delete_command} />
                                </div>
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

const SOURCE_LABEL: Record<ScopedResourceGroup["source"], string> = {
  tag: "Tag jornada",
  cluster: "RG do cluster",
  node: "Node RG (MC_)",
  data: "RG de dados",
};

// Transparência do escopo: quais RGs foram varridos e por quê — e quais da jornada ficaram de
// fora (outro ambiente ou ambiente não identificável).
export function ScopedResourceGroupsPanel({ rgs, excluded }: { rgs: ScopedResourceGroup[]; excluded: ScopedResourceGroup[] }) {
  return (
    <div className="space-y-4">
      {rgs.length === 0 ? (
        <div className="text-center py-12 text-muted-foreground text-sm">Nenhum resource group no escopo.</div>
      ) : (
        <ResourceGroupsTable rgs={rgs} />
      )}
      {excluded.length > 0 && (
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            {excluded.length} resource group(s) da jornada ignorado(s) — outro ambiente ou ambiente não identificável (sem tag de ambiente nem
            marcador prd/hlg/... no nome):
          </p>
          <ResourceGroupsTable rgs={excluded} showReason />
        </div>
      )}
    </div>
  );
}

function ResourceGroupsTable({ rgs, showReason = false }: { rgs: ScopedResourceGroup[]; showReason?: boolean }) {
  return (
    <Card className={showReason ? "opacity-70" : ""}>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Resource group</TableHead>
              <TableHead>Jornada</TableHead>
              <TableHead>Origem</TableHead>
              <TableHead>Ambiente</TableHead>
              <TableHead>Cluster</TableHead>
              <TableHead>Subscription</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rgs.map(rg => (
              <TableRow key={`${rg.subscription_id}/${rg.resource_group}`} className="text-xs">
                <TableCell className="font-mono">{rg.resource_group}</TableCell>
                <TableCell>{rg.journey || "—"}</TableCell>
                <TableCell><Badge variant="secondary" className="text-[10px]">{SOURCE_LABEL[rg.source]}</Badge></TableCell>
                <TableCell title={rg.excluded_reason}>
                  {rg.environment || "—"}
                  {showReason && rg.excluded_reason && <div className="text-[10px] text-muted-foreground">{rg.excluded_reason}</div>}
                </TableCell>
                <TableCell className="text-muted-foreground">{rg.cluster || "—"}</TableCell>
                <TableCell className="font-mono text-muted-foreground">{rg.subscription_id}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
