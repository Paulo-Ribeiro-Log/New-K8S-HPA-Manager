import { Fragment, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AlertTriangle, ChevronDown, ChevronRight, Database, Info, Lightbulb, Loader2, RefreshCw } from "lucide-react";
import { fmtBRL } from "@/lib/finopsFormat";

// ─── Painel de Recursos de Dados (RG de dados, fora do cluster K8s) ───────────────────────────
// Recursos do Resource Group "rg-<nome>-data-<env>" (VMs de banco self-hosted, discos managed,
// PostgreSQL/MySQL Flexible Server, Storage/Redis/Cosmos/Service Bus...). A coleta é via ARM REST
// (internal/finops/data_resources.go): cada disco traz estado (Attached/Unattached/Reserved), VM
// dona e tier cobrado; VM desalocada não cobra compute. Ofertas de resizing:
//   - de INVENTÁRIO (sempre): disco desatachado, disco de VM desalocada, Premium SSD em HLG;
//   - de USO REAL (botão "Analisar uso real"): CPU/memória de VM e CPU/memória/storage de
//     Flexible Server via Azure Monitor (internal/finops/data_resources_resizing.go).

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

type Verdict = "recommended" | "consider" | "info";

interface DataRecommendation {
  kind: string;
  verdict: Verdict;
  title: string;
  reason: string;
  target_sku?: string;
  monthly_savings_brl?: number;
}

interface DataUtilization {
  days: number;
  points: number;
  cpu_avg_pct: number;
  cpu_p95_pct: number;
  cpu_max_pct: number;
  has_memory: boolean;
  mem_avg_pct?: number;
  mem_p95_pct?: number;
  mem_max_pct?: number;
  storage_pct?: number;
}

interface AzureDataResource {
  name: string;
  type: string;
  kind?: string;
  sku_name?: string;
  sku_tier?: string;
  size_gb?: number;
  location?: string;
  monthly_cost_usd?: number;
  monthly_cost_brl?: number;
  pricing_note?: string;
  price_source?: string;
  disk_tier?: string;
  disk_state?: string;
  attached_to?: string;
  power_state?: string;
  provisioned_iops?: number;
  provisioned_mbps?: number;
  unattached_since?: string;
  // Flexible Server: storage provisionado e a decomposição do custo (compute + storage)
  storage_gb?: number;
  storage_tier?: string;
  ha_mode?: string;
  compute_cost_brl?: number;
  storage_cost_brl?: number;
  utilization?: DataUtilization;
  recommendations?: DataRecommendation[];
}

interface DataResourcesResponse {
  available: boolean;
  reason?: string;
  data_resource_group?: string;
  resources?: AzureDataResource[];
  resource_count?: number;
  priced_count?: number;
  total_monthly_cost_usd?: number;
  total_monthly_cost_brl?: number;
  recommendation_count?: number;
  potential_savings_brl?: number;
  analyzed?: boolean;
  analysis_days?: number;
  analyzed_at?: string;
  // Onde o RG de dados foi achado (pode ser outra subscription que a do cluster) e o que foi tentado.
  subscription?: string;
  subscription_id?: string;
  in_cluster_subscription?: boolean;
  other_subscriptions?: string[];
  tried_resource_groups?: string[];
}

const ANALYSIS_DAYS = 14;
const COLLAPSED_ROWS = 12;

// Rótulo amigável a partir do tipo ARM completo — evita mostrar "Microsoft.Sql/servers/databases"
// cru pro usuário.
const TYPE_LABELS: Record<string, string> = {
  "microsoft.compute/virtualmachines": "Servidor (VM)",
  "microsoft.compute/disks": "Disco Gerenciado",
  "microsoft.sql/servers": "SQL Server",
  "microsoft.sql/servers/databases": "SQL Database",
  "microsoft.dbforpostgresql/flexibleservers": "PostgreSQL (Flexible Server)",
  "microsoft.dbforpostgresql/servers": "PostgreSQL (Single Server)",
  "microsoft.dbformysql/flexibleservers": "MySQL (Flexible Server)",
  "microsoft.dbformysql/servers": "MySQL (Single Server)",
  "microsoft.dbformariadb/servers": "MariaDB",
  "microsoft.storage/storageaccounts": "Storage Account",
  "microsoft.cache/redis": "Azure Cache for Redis",
  "microsoft.documentdb/databaseaccounts": "Cosmos DB",
  "microsoft.servicebus/namespaces": "Service Bus",
  "microsoft.eventhub/namespaces": "Event Hub",
};

const friendlyType = (type: string) => TYPE_LABELS[type.toLowerCase()] ?? type;
const isDisk = (r: AzureDataResource) => r.type.toLowerCase() === "microsoft.compute/disks";
const isVM = (r: AzureDataResource) => r.type.toLowerCase() === "microsoft.compute/virtualmachines";

type Group = "all" | "vm" | "disk" | "db" | "other";
const groupOf = (r: AzureDataResource): Exclude<Group, "all"> => {
  const t = r.type.toLowerCase();
  if (t === "microsoft.compute/virtualmachines") return "vm";
  if (t === "microsoft.compute/disks") return "disk";
  if (t.includes("dbfor") || t.startsWith("microsoft.sql") || t.includes("documentdb") || t.includes("cache/redis")) return "db";
  return "other";
};

const VERDICT_BADGE: Record<Verdict, { label: string; cls: string }> = {
  recommended: { label: "Recomendado", cls: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400" },
  consider: { label: "Considerar", cls: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400" },
  info: { label: "Info", cls: "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-400" },
};

// Estado do recurso em texto curto + cor (disco: anexo; VM: power state).
function stateOf(r: AzureDataResource): { text: string; cls: string; title?: string } | null {
  if (isDisk(r)) {
    switch (r.disk_state) {
      case "Attached":
        return { text: r.attached_to ? `Anexado a ${r.attached_to}` : "Anexado", cls: "text-muted-foreground" };
      case "Unattached":
        return { text: "Desatachado", cls: "text-red-600 font-medium", title: "Nenhuma VM usa este disco" };
      case "Reserved":
        return { text: `VM desalocada${r.attached_to ? ` (${r.attached_to})` : ""}`, cls: "text-amber-600", title: "Disco de uma VM desalocada — continua cobrando" };
      default:
        return r.disk_state ? { text: r.disk_state, cls: "text-muted-foreground" } : null;
    }
  }
  if (isVM(r) && r.power_state === "deallocated") {
    return { text: "Desalocada", cls: "text-amber-600", title: "VM desalocada: sem custo de compute" };
  }
  return null;
}

const bestSavings = (r: AzureDataResource) =>
  Math.max(0, ...(r.recommendations ?? []).filter((x) => x.verdict !== "info").map((x) => x.monthly_savings_brl ?? 0));

async function fetchDataResources(cluster: string, analyze: boolean): Promise<DataResourcesResponse> {
  const url = `/api/v1/finops/data-resources?cluster=${encodeURIComponent(cluster)}${analyze ? `&analyze=true&days=${ANALYSIS_DAYS}` : ""}`;
  const r = await fetch(url, { headers: authHeaders() });
  if (!r.ok) throw new Error(`Erro ${r.status}`);
  return r.json();
}

export function DataResourcesPanel({ cluster }: { cluster: string }) {
  const [showAll, setShowAll] = useState(false);
  const [group, setGroup] = useState<Group>("all");
  const [onlyOffers, setOnlyOffers] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [analyzeOn, setAnalyzeOn] = useState(false);

  const base = useQuery<DataResourcesResponse>({
    queryKey: ["finops-data-resources", cluster],
    queryFn: () => fetchDataResources(cluster, false),
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  // Análise de uso real: só dispara quando o usuário pede (~1 chamada ao Azure Monitor por VM).
  const analysis = useQuery<DataResourcesResponse>({
    queryKey: ["finops-data-resources", cluster, "analyze", ANALYSIS_DAYS],
    queryFn: () => fetchDataResources(cluster, true),
    enabled: !!cluster && analyzeOn,
    staleTime: 30 * 60 * 1000,
    retry: false,
  });

  // A resposta com análise é um superset da base (mesmos recursos + uso + ofertas de uso).
  const data = analysis.data ?? base.data;
  const isLoading = base.isLoading;

  const resources = useMemo(() => data?.resources ?? [], [data?.resources]);

  const counts = useMemo(() => {
    const c = { vm: 0, disk: 0, db: 0, other: 0 };
    for (const r of resources) c[groupOf(r)]++;
    return c;
  }, [resources]);

  const filtered = useMemo(() => {
    return resources
      .filter((r) => group === "all" || groupOf(r) === group)
      .filter((r) => !onlyOffers || (r.recommendations ?? []).length > 0)
      // Maior custo primeiro; empate: quem tem oferta primeiro.
      .sort((a, b) => (b.monthly_cost_brl ?? 0) - (a.monthly_cost_brl ?? 0) || bestSavings(b) - bestSavings(a));
  }, [resources, group, onlyOffers]);

  const visible = showAll ? filtered : filtered.slice(0, COLLAPSED_ROWS);

  const attention = useMemo(() => {
    let unattached = 0, reservedDisks = 0, deallocatedVMs = 0;
    for (const r of resources) {
      if (isDisk(r) && r.disk_state === "Unattached") unattached++;
      if (isDisk(r) && r.disk_state === "Reserved") reservedDisks++;
      if (isVM(r) && r.power_state === "deallocated") deallocatedVMs++;
    }
    return { unattached, reservedDisks, deallocatedVMs };
  }, [resources]);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground py-3">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Consultando Resource Group de dados…
      </div>
    );
  }

  // Uma falha real de rede/Azure NUNCA pode aparecer como "este cluster não tem RG de dados".
  if (base.isError) {
    return (
      <div className="flex items-start justify-between gap-2 text-xs py-2 px-1 flex-wrap">
        <div className="flex items-start gap-2 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
          <span>
            Falha ao consultar Recursos de Dados{base.error instanceof Error ? `: ${base.error.message}` : ""} — pode ser algo
            transitório (VPN/Azure), não necessariamente "sem RG de dados pra este cluster".
          </span>
        </div>
        <Button size="sm" variant="outline" className="h-6 text-[10px] shrink-0" onClick={() => base.refetch()}>
          <RefreshCw className="h-3 w-3 mr-1" /> Tentar novamente
        </Button>
      </div>
    );
  }

  // Feature opt-in por convenção de nome ("rg-<nome>-app-<env>" → "-data-") — GKE/EKS ou AKS fora
  // da convenção simplesmente não têm esse RG. O motivo real vem do backend (RG inexistente, falha
  // de autenticação...).
  if (!data?.available) {
    return (
      <div className="flex items-start gap-2 text-xs text-muted-foreground py-2 px-1">
        <Info className="h-3.5 w-3.5 mt-0.5 shrink-0" />
        <div className="space-y-1">
          <span>{data?.reason ?? "Recursos de dados (RG separado) não disponíveis para este cluster."}</span>
          {!!data?.tried_resource_groups?.length && (
            <p className="text-[10px] font-mono">Nomes tentados: {data.tried_resource_groups.join(", ")}</p>
          )}
        </div>
      </div>
    );
  }

  const analyzed = !!data.analyzed;
  const analyzing = analyzeOn && analysis.isFetching;

  return (
    <div className="space-y-3">
      {/* Cabeçalho: RG, total, economia potencial e ação de análise */}
      <div className="flex items-start justify-between gap-2 flex-wrap">
        <div>
          <div className="flex items-center gap-1.5">
            <Database className="h-4 w-4 text-cyan-500" />
            <p className="text-sm font-medium">Recursos de Dados</p>
            <span className="text-[10px] text-muted-foreground font-mono">({data.data_resource_group})</span>
          </div>
          {data.subscription && (
            <p className={`text-[11px] mt-0.5 ${data.in_cluster_subscription === false ? "text-amber-600" : "text-muted-foreground"}`}
               title={data.in_cluster_subscription === false ? "O RG de dados fica numa subscription diferente da do cluster" : undefined}>
              Subscription: {data.subscription}
              {data.in_cluster_subscription === false && " — diferente da subscription do cluster"}
            </p>
          )}
          {!!data.other_subscriptions?.length && (
            <p className="text-[11px] mt-0.5 text-amber-600"
               title="Existe um Resource Group com o MESMO nome em outra subscription, com conteúdo diferente. A app mostra só o da subscription do cluster e não soma os dois.">
              ⚠ RG homônimo também em: {data.other_subscriptions.join(", ")} — conteúdo diferente, não somado
            </p>
          )}
          <p className="text-[11px] text-muted-foreground mt-0.5">
            {resources.length} recurso(s): {counts.vm} VM · {counts.disk} disco(s) · {counts.db} banco(s)
            {attention.deallocatedVMs > 0 && <> · <span className="text-amber-600">{attention.deallocatedVMs} VM(s) desalocada(s)</span></>}
            {attention.unattached > 0 && <> · <span className="text-red-600">{attention.unattached} disco(s) desatachado(s)</span></>}
            {attention.reservedDisks > 0 && <> · {attention.reservedDisks} disco(s) de VM desalocada</>}
          </p>
        </div>
        <div className="flex items-center gap-3 flex-wrap justify-end">
          {(data.total_monthly_cost_brl ?? 0) > 0 && (
            <p className="text-sm font-bold text-cyan-600 text-right">
              {fmtBRL(data.total_monthly_cost_brl ?? 0)}/mês
              <span className="block text-[10px] font-normal text-muted-foreground">
                {data.priced_count} de {data.resource_count} com estimativa
              </span>
            </p>
          )}
          {(data.potential_savings_brl ?? 0) > 0 && (
            <p className="text-sm font-bold text-emerald-600 text-right" title="Soma das economias das ofertas (recomendado + considerar). Ofertas do mesmo recurso podem ser alternativas entre si — trate como teto.">
              até {fmtBRL(data.potential_savings_brl ?? 0)}/mês
              <span className="block text-[10px] font-normal text-muted-foreground">{data.recommendation_count} oferta(s) de resizing</span>
            </p>
          )}
          <Button
            size="sm"
            variant={analyzed ? "outline" : "default"}
            className="h-8 gap-1 text-xs"
            disabled={analyzing}
            onClick={() => (analyzeOn ? analysis.refetch() : setAnalyzeOn(true))}
            title="Consulta CPU/memória de cada VM e CPU/memória/storage dos Flexible Servers no Azure Monitor e oferece resizing com base no uso real."
          >
            {analyzing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Lightbulb className="h-3.5 w-3.5" />}
            {analyzing ? "Analisando uso…" : analyzed ? `Reanalisar uso (${data.analysis_days}d)` : `Analisar uso real (${ANALYSIS_DAYS}d)`}
          </Button>
        </div>
      </div>

      {analysis.isError && (
        <p className="text-[11px] text-amber-600 flex items-center gap-1">
          <AlertTriangle className="h-3 w-3" /> Falha na análise de uso: {analysis.error instanceof Error ? analysis.error.message : "erro"} — exibindo só o inventário.
        </p>
      )}
      {!analyzed && !analyzing && resources.some((r) => isVM(r) && r.power_state !== "deallocated") && (
        <p className="text-[11px] text-muted-foreground flex items-start gap-1">
          <Info className="h-3 w-3 mt-0.5 shrink-0" />
          Ofertas de redução de VM/banco dependem de uso real — clique em <strong>Analisar uso real</strong>. Sem isso só aparecem as
          ofertas de inventário (disco desatachado, VM desalocada, Premium SSD em HLG).
        </p>
      )}

      {resources.length === 0 ? (
        <p className="text-xs text-muted-foreground">Nenhum recurso de dados reconhecido encontrado neste Resource Group.</p>
      ) : (
        <>
          {/* Filtros */}
          <div className="flex items-center gap-1.5 flex-wrap">
            {([["all", "Todos", resources.length], ["vm", "VMs", counts.vm], ["disk", "Discos", counts.disk], ["db", "Bancos", counts.db], ["other", "Outros", counts.other]] as const)
              .filter(([g, , n]) => g === "all" || n > 0)
              .map(([g, label, n]) => (
                <Button key={g} size="sm" variant={group === g ? "default" : "outline"} className="h-6 px-2 text-[11px]" onClick={() => { setGroup(g); setShowAll(false); }}>
                  {label} ({n})
                </Button>
              ))}
            <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground cursor-pointer ml-2 select-none">
              <input type="checkbox" className="h-3 w-3" checked={onlyOffers} onChange={(e) => { setOnlyOffers(e.target.checked); setShowAll(false); }} />
              só com oferta
            </label>
          </div>

          <div className="border rounded-md overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-6 h-9" />
                  <TableHead className="h-9">Recurso</TableHead>
                  <TableHead className="h-9">SKU / Tier</TableHead>
                  <TableHead className="text-right h-9">GB</TableHead>
                  <TableHead className="h-9">Estado</TableHead>
                  <TableHead className="text-right h-9">Custo/mês</TableHead>
                  <TableHead className="h-9">Oferta</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center text-xs text-muted-foreground py-6">Nenhum recurso corresponde aos filtros.</TableCell>
                  </TableRow>
                )}
                {visible.map((r) => {
                  const key = `${r.type}/${r.name}`;
                  const open = expanded === key;
                  const state = stateOf(r);
                  const recs = r.recommendations ?? [];
                  const top = [...recs].sort((a, b) => (b.monthly_savings_brl ?? 0) - (a.monthly_savings_brl ?? 0))[0];
                  const tier = r.disk_tier || r.sku_tier;
                  const detailable = recs.length > 0 || !!r.utilization || !!r.pricing_note || !!r.storage_gb;
                  return (
                    <Fragment key={key}>
                      <TableRow className={`text-xs [&>td]:py-1.5 ${detailable ? "cursor-pointer" : ""}`} onClick={() => detailable && setExpanded(open ? null : key)}>
                        <TableCell className="px-2">{detailable ? (open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />) : null}</TableCell>
                        <TableCell className="max-w-[240px]">
                          <div className="font-medium truncate" title={r.name}>{r.name}</div>
                          <div className="text-[10px] text-muted-foreground">{friendlyType(r.type)}</div>
                        </TableCell>
                        <TableCell className="whitespace-nowrap">
                          {r.sku_name ?? "—"}
                          {tier && <Badge variant="outline" className="ml-1.5 text-[10px] px-1.5 py-0">{tier}</Badge>}
                        </TableCell>
                        <TableCell className="text-right" title={r.storage_gb ? "Storage provisionado do servidor" : undefined}>
                          {r.size_gb ? Math.round(r.size_gb) : r.storage_gb ? Math.round(r.storage_gb) : ""}
                        </TableCell>
                        <TableCell className={`max-w-[200px] truncate ${state?.cls ?? ""}`} title={state?.title ?? state?.text}>{state?.text ?? ""}</TableCell>
                        <TableCell className="text-right whitespace-nowrap">
                          {r.monthly_cost_brl ? (
                            <span title={r.price_source === "estimated" ? r.pricing_note : r.price_source === "table" ? "Preço de tabela (Premium SSD v2/Ultra): capacidade + IOPS/throughput provisionados" : undefined}>
                              {r.price_source === "estimated" ? "≈ " : ""}{fmtBRL(r.monthly_cost_brl)}
                              {r.price_source === "table" && <span className="ml-1 text-[9px] text-muted-foreground align-top">tabela</span>}
                            </span>
                          ) : (
                            <span className="text-[10px] text-muted-foreground" title={r.pricing_note}>{r.pricing_note ? "sem custo/estimativa" : "—"}</span>
                          )}
                        </TableCell>
                        <TableCell className="max-w-[260px]">
                          {top ? (
                            <div className="flex items-center gap-1.5 min-w-0">
                              <Badge className={`text-[10px] shrink-0 ${VERDICT_BADGE[top.verdict].cls}`}>{VERDICT_BADGE[top.verdict].label}</Badge>
                              <span className="truncate text-[11px]" title={top.title}>{top.title}</span>
                              {(top.monthly_savings_brl ?? 0) > 0 && <span className="text-[11px] text-emerald-600 font-medium whitespace-nowrap">−{fmtBRL(top.monthly_savings_brl ?? 0)}</span>}
                            </div>
                          ) : null}
                        </TableCell>
                      </TableRow>
                      {open && (
                        <TableRow className="bg-muted/30 hover:bg-muted/30 [&>td]:py-2">
                          <TableCell />
                          <TableCell colSpan={6} className="space-y-2 py-3">
                            {r.utilization && (
                              <p className="text-[11px] text-muted-foreground">
                                Uso real ({r.utilization.days}d, {r.utilization.points} pontos): CPU média {r.utilization.cpu_avg_pct}% · P95 {r.utilization.cpu_p95_pct}% · pico {r.utilization.cpu_max_pct}%
                                {r.utilization.has_memory && <> · memória média {r.utilization.mem_avg_pct}% · P95 {r.utilization.mem_p95_pct}% · pico {r.utilization.mem_max_pct}%</>}
                                {!!r.utilization.storage_pct && <> · storage {r.utilization.storage_pct}%</>}
                              </p>
                            )}
                            {r.storage_gb ? (
                              <p className="text-[11px]">
                                <span className="text-muted-foreground">Composição do custo: </span>
                                compute <strong>{fmtBRL(r.compute_cost_brl ?? 0)}</strong>
                                {" + "}storage {Math.round(r.storage_gb)} GB{r.storage_tier ? ` (${r.storage_tier})` : ""} <strong>{fmtBRL(r.storage_cost_brl ?? 0)}</strong>
                                {r.ha_mode && r.ha_mode !== "Disabled" ? <span className="text-amber-600"> · alta disponibilidade {r.ha_mode} (em dobro)</span> : null}
                              </p>
                            ) : null}
                            {(r.provisioned_iops || r.provisioned_mbps) ? (
                              <p className="text-[11px] text-muted-foreground">
                                Performance provisionada: {r.provisioned_iops ? `${Math.round(r.provisioned_iops)} IOPS` : ""}{r.provisioned_iops && r.provisioned_mbps ? " · " : ""}{r.provisioned_mbps ? `${Math.round(r.provisioned_mbps)} MB/s` : ""}
                              </p>
                            ) : null}
                            {recs.map((x, i) => (
                              <div key={i} className="text-xs space-y-0.5">
                                <div className="flex items-center gap-1.5 flex-wrap">
                                  <Badge className={`text-[10px] ${VERDICT_BADGE[x.verdict].cls}`}>{VERDICT_BADGE[x.verdict].label}</Badge>
                                  <span className="font-medium">{x.title}</span>
                                  {(x.monthly_savings_brl ?? 0) > 0 && <span className="text-emerald-600 font-medium">economia de até {fmtBRL(x.monthly_savings_brl ?? 0)}/mês</span>}
                                </div>
                                <p className="text-[11px] text-muted-foreground">{x.reason}</p>
                              </div>
                            ))}
                            {r.pricing_note && <p className="text-[10px] text-muted-foreground">{r.pricing_note}</p>}
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  );
                })}
              </TableBody>
            </Table>
          </div>

          {filtered.length > COLLAPSED_ROWS && (
            <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]" onClick={() => setShowAll((v) => !v)}>
              {showAll ? "Mostrar menos" : `Ver todos os ${filtered.length} recursos`}
            </Button>
          )}
          <p className="text-[10px] text-muted-foreground">
            Preços via Azure Retail Prices API (tabela pública, sem desconto/reserved instance); Premium SSD v2/Ultra por tabela. Discos não podem ser
            reduzidos no Azure (só aumentados) — ofertas de disco são exclusão, snapshot ou troca de SKU. Ofertas são sugestões: nada é aplicado pela app.
          </p>
        </>
      )}
    </div>
  );
}
