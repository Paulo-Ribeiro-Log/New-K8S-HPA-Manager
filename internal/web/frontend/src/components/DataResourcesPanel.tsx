import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertTriangle, ChevronDown, ChevronUp, Database, Info, Loader2, RefreshCw } from "lucide-react";
import { fmtBRL } from "@/lib/finopsFormat";

// ─── Painel de Recursos de Dados (RG de dados, fora do cluster K8s) ───────────────────────────
// Pedido explícito do usuário: "o resource group de dados... também impacta custos de cloud...
// estamos analisando sempre apenas os resource groups de app". Escopo (ver internal/finops/
// data_resources.go/data_resources_pricing.go), validado ao vivo contra RGs de dados de produção
// reais desta empresa: VMs self-hosted de banco (mongo/redis/etc, o achado mais significativo —
// um RG real chegou a somar 82 recursos e R$44,6 mil/mês, número antes 100% invisível), discos
// managed dessas VMs, Azure Database for PostgreSQL/MySQL (Flexible/Single Server), e os PaaS
// originalmente cobertos (SQL, Storage, Redis, Cosmos DB, Service Bus/Event Hub). Preço automático
// confiável pra VM/disco/Redis/ServiceBus-EventHub Premium/Postgres-MySQL (compute) — os demais
// aparecem listados com o motivo de não terem estimativa automática (nunca um número inventado).

const authHeaders = () => ({ Authorization: `Bearer ${localStorage.getItem("auth_token")}` });

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
}

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

function friendlyType(type: string): string {
  return TYPE_LABELS[type.toLowerCase()] ?? type;
}

export function DataResourcesPanel({ cluster }: { cluster: string }) {
  // Colapsado por padrão — achado ao vivo durante o desenvolvimento: um único RG de dados real
  // chegou a ter 82 recursos (majoritariamente VMs self-hosted + seus discos). Uma lista aberta
  // por padrão dominaria a aba Armazenamento inteira; o total já resume o que importa de cara.
  const [expanded, setExpanded] = useState(false);

  const { data, isLoading, isError, error, refetch } = useQuery<DataResourcesResponse>({
    queryKey: ["finops-data-resources", cluster],
    queryFn: async () => {
      const r = await fetch(`/api/v1/finops/data-resources?cluster=${encodeURIComponent(cluster)}`, {
        headers: authHeaders(),
      });
      if (!r.ok) throw new Error(`Erro ${r.status}`);
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const sortedResources = useMemo(() => {
    const list = data?.resources ?? [];
    // Maior custo primeiro; entre os sem estimativa, mantém a ordem original (já agrupada por
    // tipo pelo backend — VMs juntas, discos juntos, etc.).
    return [...list].sort((a, b) => (b.monthly_cost_brl ?? 0) - (a.monthly_cost_brl ?? 0));
  }, [data?.resources]);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground py-3">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Consultando Resource Group de dados…
      </div>
    );
  }

  // F2.2 (FINOPS-IMPROVEMENTS-PLAN.md) — antes, uma falha real de rede/Azure (ex: VPN fora do
  // ar, token expirado) caía no MESMO texto neutro de "não disponível" que o caso legítimo
  // "este cluster não tem RG de dados" (retry:false + só {data,isLoading} desestruturado, sem
  // error/isError nenhum) — indistinguível pro usuário, sem botão de tentar de novo. Corrigido
  // com um branch PRÓPRIO, sempre checado antes do de "não aplicável" — nunca afirma "sem RG de
  // dados" quando a causa real é uma falha transiente.
  if (isError) {
    return (
      <div className="flex items-start justify-between gap-2 text-xs py-2 px-1 flex-wrap">
        <div className="flex items-start gap-2 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
          <span>
            Falha ao consultar Recursos de Dados{error instanceof Error ? `: ${error.message}` : ""} — pode ser algo
            transitório (VPN/Azure), não necessariamente "sem RG de dados pra este cluster".
          </span>
        </div>
        <Button size="sm" variant="outline" className="h-6 text-[10px] shrink-0" onClick={() => refetch()}>
          <RefreshCw className="h-3 w-3 mr-1" /> Tentar novamente
        </Button>
      </div>
    );
  }

  // Feature opt-in por convenção de nome — a maioria dos clusters GKE/EKS, ou AKS fora da
  // convenção "-app-"/"-data-", simplesmente não tem esse RG. Nota discreta, nunca um alerta —
  // ausência aqui é o caso comum, não um erro.
  if (!data?.available) {
    return (
      <div className="flex items-start gap-2 text-xs text-muted-foreground py-2 px-1">
        <Info className="h-3.5 w-3.5 mt-0.5 shrink-0" />
        <span>{data?.reason ?? "Recursos de dados (RG separado) não disponíveis para este cluster."}</span>
      </div>
    );
  }

  const hasUnpriced = sortedResources.some((r) => !r.monthly_cost_brl && r.pricing_note);

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div className="flex items-center gap-1.5">
          <Database className="h-4 w-4 text-cyan-500" />
          <p className="text-sm font-medium">Recursos de Dados</p>
          <span className="text-[10px] text-muted-foreground font-mono">({data.data_resource_group})</span>
        </div>
        <div className="flex items-center gap-2">
          {(data.total_monthly_cost_brl ?? 0) > 0 && (
            <p className="text-sm font-bold text-cyan-600">
              {fmtBRL(data.total_monthly_cost_brl ?? 0)}/mês
              <span className="text-[10px] font-normal text-muted-foreground ml-1">
                ({data.priced_count} de {data.resource_count} com estimativa)
              </span>
            </p>
          )}
          {sortedResources.length > 0 && (
            <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px] gap-1" onClick={() => setExpanded((v) => !v)}>
              {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
              {expanded ? "Ocultar" : "Ver"} {sortedResources.length} recurso(s)
            </Button>
          )}
        </div>
      </div>

      {sortedResources.length === 0 ? (
        <p className="text-xs text-muted-foreground">Nenhum recurso de dados reconhecido encontrado neste Resource Group.</p>
      ) : expanded ? (
        <div className="grid gap-2">
          {sortedResources.map((r) => (
            <Card key={`${r.type}/${r.name}`}>
              <CardContent className="p-3 flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-sm font-medium truncate">{r.name}</p>
                  <p className="text-[11px] text-muted-foreground">
                    {friendlyType(r.type)}
                    {r.sku_tier && <> · {r.sku_tier}</>}
                    {r.sku_name && <> ({r.sku_name})</>}
                    {r.size_gb ? <> · {r.size_gb} GB</> : null}
                  </p>
                </div>
                <div className="text-right shrink-0">
                  {r.monthly_cost_brl ? (
                    <p className="text-sm font-semibold text-cyan-600">{fmtBRL(r.monthly_cost_brl)}/mês</p>
                  ) : (
                    <p className="text-[10px] text-muted-foreground max-w-[220px]" title={r.pricing_note}>
                      {r.pricing_note ?? "Sem estimativa"}
                    </p>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : null}

      {expanded && hasUnpriced && (
        <p className="text-[10px] text-muted-foreground">
          Preços via Azure Retail Prices API (tabela pública, sem desconto/reserved instance) — alguns tipos de recurso têm modelo de
          cobrança baseado em consumo/volume e não têm estimativa automática nesta versão (motivo explicado em cada item).
        </p>
      )}
    </div>
  );
}
