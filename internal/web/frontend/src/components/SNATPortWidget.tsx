import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { AlertTriangle, CheckCircle2, XCircle, RefreshCw, Network } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

interface SNATNodePoolInfo {
  name: string;
  node_count: number;
  required_ports: number;
}

interface SNATProfile {
  cluster: string;
  allocated_outbound_ports: number;
  outbound_ip_count: number;
  max_ports_per_ip: number;
  total_node_count: number;
  total_available_ports: number;
  total_required_ports: number;
  port_deficit: number;
  usage_percent: number;
  max_nodes_allowed: number;
  nodes_until_limit: number;
  ips_needed_for_current_nodes: number;
  status: "ok" | "warning" | "critical";
  node_pools: SNATNodePoolInfo[];
  fetched_at: string;
  error?: string;
}

function fmt(n: number) {
  return n.toLocaleString("pt-BR");
}

const statusColors = {
  ok:       { bar: "bg-emerald-500", text: "text-emerald-400", icon: CheckCircle2, label: "OK" },
  warning:  { bar: "bg-amber-500",   text: "text-amber-400",   icon: AlertTriangle, label: "Atenção" },
  critical: { bar: "bg-red-500",     text: "text-red-400",     icon: XCircle,       label: "Crítico" },
};

interface Props {
  cluster: string;
}

export function SNATPortWidget({ cluster }: Props) {
  const [open, setOpen] = useState(false);

  const { data, isLoading, error, refetch, isFetching } = useQuery<SNATProfile>({
    queryKey: ["snat-profile", cluster],
    queryFn: () => apiClient.get<SNATProfile>(`/nodepools/snat?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster,
    staleTime: 2 * 60 * 1000,
    retry: 1,
  });

  if (!cluster) return null;

  const colors = data ? statusColors[data.status] : statusColors.ok;
  const StatusIcon = colors.icon;

  return (
    <>
      {/* Header compacto — sempre visível, abre o modal ao clicar */}
      <button
        className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg border text-xs hover:bg-muted/20 transition-colors ${
          data?.status === "critical" ? "border-red-500/40 bg-red-500/5" :
          data?.status === "warning"  ? "border-amber-500/40 bg-amber-500/5" :
          "border-border/50 bg-muted/10"
        }`}
        onClick={() => setOpen(true)}
      >
        <Network className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
        <span className="font-medium text-foreground">Diagnóstico SNAT</span>

        {isLoading || isFetching ? (
          <RefreshCw className="w-3 h-3 animate-spin text-muted-foreground ml-1" />
        ) : data ? (
          <>
            <StatusIcon className={`w-3.5 h-3.5 ${colors.text} ml-1`} />
            <span className={`font-semibold ${colors.text}`}>{colors.label}</span>
            <span className="text-muted-foreground ml-auto flex items-center gap-2">
              <span>
                {fmt(data.total_required_ports)} / {fmt(data.total_available_ports)} portas
                {" · "}
                <span className={
                  data.usage_percent >= 100 ? "text-red-400 font-bold" :
                  data.usage_percent >= 85  ? "text-amber-400 font-bold" :
                  "text-foreground"
                }>
                  {data.usage_percent.toFixed(1)}%
                </span>
              </span>
              {data.allocated_outbound_ports > 0 && (
                <span className={`font-semibold px-1.5 py-0.5 rounded text-[11px] ${
                  data.nodes_until_limit <= 0
                    ? "bg-red-500/20 text-red-400"
                    : data.nodes_until_limit <= 10
                    ? "bg-amber-500/20 text-amber-400"
                    : "bg-emerald-500/20 text-emerald-400"
                }`}>
                  {data.nodes_until_limit <= 0 ? "0 nós disponíveis" : `+${fmt(data.nodes_until_limit)} nós`}
                </span>
              )}
            </span>
          </>
        ) : error ? (
          <span className="text-red-400 ml-1">Erro ao carregar</span>
        ) : null}

        <span className="text-muted-foreground text-[11px] flex-shrink-0 ml-2">Ver detalhes →</span>
      </button>

      {/* Modal de detalhes */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-2xl max-h-[78vh] flex flex-col gap-0 p-0">
          <DialogHeader className="px-5 py-3 border-b border-border/50 flex-shrink-0">
            <DialogTitle className="flex items-center gap-2 text-sm font-semibold">
              <Network className="w-4 h-4 text-muted-foreground" />
              Diagnóstico SNAT
              {data && (
                <>
                  <StatusIcon className={`w-4 h-4 ${colors.text}`} />
                  <span className={colors.text}>{colors.label}</span>
                  <span className="text-muted-foreground text-xs font-normal">— {cluster}</span>
                </>
              )}
              <div className="ml-auto mr-8">
                <Button size="sm" variant="ghost" className="h-6 text-[11px]" onClick={() => refetch()} disabled={isFetching}>
                  <RefreshCw className={`w-3 h-3 mr-1 ${isFetching ? "animate-spin" : ""}`} />
                  Atualizar
                </Button>
              </div>
            </DialogTitle>
          </DialogHeader>

          {data && (
            <div className="overflow-y-auto flex-1 px-5 py-4 space-y-4 text-xs">
              {data.error && (
                <p className="text-amber-400 italic">{data.error}</p>
              )}

              {/* Barra de uso */}
              <div className="space-y-1.5">
                <div className="flex justify-between text-muted-foreground">
                  <span>Uso de portas SNAT</span>
                  <span className={`${colors.text} font-semibold`}>{data.usage_percent.toFixed(1)}%</span>
                </div>
                <div className="h-2.5 rounded-full bg-muted overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${colors.bar}`}
                    style={{ width: `${Math.min(data.usage_percent, 100)}%` }}
                  />
                </div>
              </div>

              {/* Cards de métricas */}
              <div className="grid grid-cols-3 gap-2">
                <MetricCard label="Portas por nó (alocadas)"  value={fmt(data.allocated_outbound_ports)} />
                <MetricCard label="IPs públicos no LB"        value={fmt(data.outbound_ip_count)} sub={`${fmt(data.outbound_ip_count * data.max_ports_per_ip)} portas totais`} />
                <MetricCard label="Total de nós"              value={fmt(data.total_node_count)} />
                <MetricCard label="Portas necessárias"        value={fmt(data.total_required_ports)} highlight={data.total_required_ports > data.total_available_ports} />
                <MetricCard label="Portas disponíveis"        value={fmt(data.total_available_ports)} />
                <MetricCard
                  label={data.port_deficit > 0 ? "Déficit de portas" : "Margem de portas"}
                  value={data.port_deficit > 0 ? `−${fmt(data.port_deficit)}` : `+${fmt(-data.port_deficit)}`}
                  highlight={data.port_deficit > 0}
                  highlightGreen={data.port_deficit <= 0}
                />
              </div>

              {/* Capacidade */}
              {data.allocated_outbound_ports > 0 && (
                <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5">
                  <p className="font-medium text-foreground">Capacidade</p>
                  <Row label="Máx. nós com config atual" value={fmt(data.max_nodes_allowed)} />
                  <Row
                    label="Nós que ainda cabem"
                    value={data.nodes_until_limit > 0 ? `+${fmt(data.nodes_until_limit)}` : "0 (limite atingido)"}
                    valueClass={
                      data.nodes_until_limit <= 0  ? "text-red-400 font-bold" :
                      data.nodes_until_limit <= 10 ? "text-amber-400 font-bold" :
                      "text-emerald-400 font-semibold"
                    }
                  />
                  <Row
                    label="IPs necessários (nós atuais)"
                    value={`${fmt(data.ips_needed_for_current_nodes)} IPs`}
                    valueClass={data.ips_needed_for_current_nodes > data.outbound_ip_count ? "text-red-400 font-bold" : ""}
                  />
                </div>
              )}

              {/* Breakdown por node pool */}
              {data.node_pools && data.node_pools.length > 0 && (
                <div className="space-y-1.5">
                  <p className="font-medium text-foreground">Por Node Pool</p>
                  <div className="rounded border border-border/50 overflow-hidden">
                    <table className="w-full text-[11px]">
                      <thead>
                        <tr className="bg-muted/30 text-muted-foreground">
                          <th className="text-left px-3 py-1.5">Pool</th>
                          <th className="text-right px-3 py-1.5">Nós</th>
                          <th className="text-right px-3 py-1.5">Portas</th>
                          <th className="text-right px-3 py-1.5">% do total</th>
                        </tr>
                      </thead>
                      <tbody>
                        {data.node_pools.map((p, i) => (
                          <tr key={p.name} className={i % 2 === 0 ? "bg-background/30" : ""}>
                            <td className="px-3 py-1.5 font-mono">{p.name}</td>
                            <td className="px-3 py-1.5 text-right">{fmt(p.node_count)}</td>
                            <td className="px-3 py-1.5 text-right">{fmt(p.required_ports)}</td>
                            <td className="px-3 py-1.5 text-right text-muted-foreground">
                              {data.total_required_ports > 0
                                ? ((p.required_ports / data.total_required_ports) * 100).toFixed(1) + "%"
                                : "—"}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* Fórmula explicada */}
              <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1">
                <p className="font-medium text-foreground">Como calcular</p>
                <p>Disponível = {fmt(data.outbound_ip_count)} IPs × {fmt(data.max_ports_per_ip)} = <span className="text-foreground">{fmt(data.total_available_ports)}</span></p>
                {data.allocated_outbound_ports > 0 && (
                  <p>Necessário = {fmt(data.total_node_count)} nós × {fmt(data.allocated_outbound_ports)} = <span className={data.port_deficit > 0 ? "text-red-400 font-bold" : "text-foreground"}>{fmt(data.total_required_ports)}</span></p>
                )}
                {data.port_deficit > 0 && (
                  <p className="text-red-400 font-semibold">
                    ⚠ Faltam {fmt(data.port_deficit)} portas — escala bloqueada pelo Azure LB
                  </p>
                )}
                {data.port_deficit > 0 && (
                  <p className="text-amber-400">
                    Solução: adicionar {fmt(data.ips_needed_for_current_nodes - data.outbound_ip_count)} IP(s) ao LB
                    {data.allocated_outbound_ports > 0 &&
                      ` ou reduzir allocatedOutboundPorts para ≤ ${Math.floor(data.total_available_ports / data.total_node_count)}`}
                  </p>
                )}
              </div>
            </div>
          )}

          {!data && !isLoading && error && (
            <div className="px-5 py-8 text-center text-red-400 text-sm">
              Erro ao carregar dados SNAT para este cluster.
            </div>
          )}

          {isLoading && (
            <div className="px-5 py-8 text-center text-muted-foreground text-sm flex items-center justify-center gap-2">
              <RefreshCw className="w-4 h-4 animate-spin" />
              Carregando...
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function MetricCard({ label, value, sub, highlight, highlightGreen }: {
  label: string; value: string; sub?: string; highlight?: boolean; highlightGreen?: boolean;
}) {
  return (
    <div className="rounded border border-border/40 bg-background/30 px-3 py-2">
      <p className="text-muted-foreground leading-tight">{label}</p>
      <p className={`font-semibold text-sm mt-0.5 ${highlight ? "text-red-400" : highlightGreen ? "text-emerald-400" : "text-foreground"}`}>
        {value}
      </p>
      {sub && <p className="text-muted-foreground text-[10px] mt-0.5">{sub}</p>}
    </div>
  );
}

function Row({ label, value, valueClass = "" }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span className={valueClass || "text-foreground"}>{value}</span>
    </div>
  );
}
