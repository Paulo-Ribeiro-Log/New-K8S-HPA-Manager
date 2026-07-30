import { useState, useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { AlertTriangle, CheckCircle2, XCircle, RefreshCw, Network, Banknote, TrendingUp, TrendingDown, Minus, Server, KeyRound, Copy, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { toast } from "sonner";
import { CloudAccountHintField } from "@/components/CloudAccountHintField";
import {
  LineChart, Line, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, ReferenceLine,
} from "recharts";

interface SNATNodePoolInfo {
  name: string;
  node_count: number;
  required_ports: number;
}

interface SNATProfile {
  cluster: string;
  cloud_provider: "aks" | "eks" | "gke";
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
  requires_gcp_auth?: boolean;
}

interface SNATNodeStat {
  name: string;
  pool: string;
  internal_ip: string;
  conntrack_entries: number;
  conntrack_max: number;
  allocated_ports: number;
  snat_usage_pct: number;
  conntrack_usage_pct: number;
  status: "ok" | "warning" | "critical" | "unknown";
}

interface SNATNodesResponse {
  cluster: string;
  cloud_provider: "aks" | "eks" | "gke";
  allocated_outbound_ports: number;
  nodes: SNATNodeStat[];
  prometheus_available: boolean;
  fetched_at: string;
}

// Reusa ConntrackHistoryPoint do endpoint existente /nodepools/conntrack/history
interface ConntrackPoint {
  ts: number;
  count: number;
  max: number;
  usage_pct: number;
}

interface ConntrackHistoryResponse {
  node_name: string;
  points: ConntrackPoint[];
  prometheus_available: boolean;
  error?: string;
}

interface SNATHistoryRecord {
  cluster: string;
  total_node_count: number;
  usage_percent: number;
  nodes_until_limit: number;
  allocated_outbound_ports: number;
  outbound_ip_count: number;
  recorded_at: string;
}

interface SNATProjection {
  records: SNATHistoryRecord[];
  growth_per_day: number;
  days_until_limit: number;   // -1 = indeterminado
  estimated_date: string | null;
  confidence: "high" | "medium" | "low" | "none";
  data_points: number;
}

interface SNATCostInfo {
  cluster: string;
  cloud_provider: string;
  ip_price_monthly: number;  // AKS/GKE: custo por IP/mês
  gw_hourly_price: number;   // GKE/EKS: custo por hora do NAT gateway
  data_price_per_gb: number; // GKE/EKS: custo por GB processado
  currency: string;           // "BRL" ou "USD"
  pricing_region: string;
  source: string;             // "azure-retail-api", "gcp-billing-api", "aws-pricing-api", "reference"
  fetched_at: string;
  error?: string;
}

interface GCPAuthStatus {
  authenticated: boolean;
  account?: string;
  has_gcloud: boolean;
  has_adc: boolean;
}

interface GCPLoginSession {
  session_id: string;
  user_code: string;
  verify_url: string;
  expires_at: string;
  interval_sec: number;
}

// Fallbacks documentais por provider (usados quando a API de pricing falha)
const FALLBACK_COSTS: Record<string, Partial<SNATCostInfo>> = {
  aks: { ip_price_monthly: 20,   currency: "BRL", pricing_region: "brazilsouth" },
  gke: { ip_price_monthly: 2.92, gw_hourly_price: 0.044, data_price_per_gb: 0.045, currency: "USD", pricing_region: "southamerica-east1" },
  eks: { ip_price_monthly: 0,    gw_hourly_price: 0.044, data_price_per_gb: 0.044, currency: "USD", pricing_region: "us-east-1" },
};

const providerIPLabel: Record<string, string> = {
  aks: "IPs públicos no LB",
  gke: "IPs no Cloud NAT",
  eks: "EIPs nos NAT Gateways",
};

const providerPortsLabel: Record<string, string> = {
  aks: "Portas por nó (alocadas)",
  gke: "Portas por VM (minPortsPerVm)",
  eks: "Conexões máx. por EIP/destino",
};

function fmt(n: number) {
  return n.toLocaleString("pt-BR");
}


const statusColors = {
  ok:       { bar: "bg-emerald-500", text: "text-emerald-400", icon: CheckCircle2, label: "OK" },
  warning:  { bar: "bg-amber-500",   text: "text-amber-400",   icon: AlertTriangle, label: "Atenção" },
  critical: { bar: "bg-red-500",     text: "text-red-400",     icon: XCircle,       label: "Crítico" },
};

const confidenceLabel: Record<string, { label: string; cls: string }> = {
  high:   { label: "Alta confiança",  cls: "bg-emerald-500/20 text-emerald-400" },
  medium: { label: "Média confiança", cls: "bg-amber-500/20 text-amber-400" },
  low:    { label: "Baixa confiança", cls: "bg-muted text-muted-foreground" },
  none:   { label: "Sem dados",       cls: "bg-muted text-muted-foreground" },
};

type Tab = "diagnostico" | "financeiro" | "formula" | "projecao" | "nos";

interface Props {
  cluster: string;
}

export function SNATPortWidget({ cluster }: Props) {
  const [open, setOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<Tab>("diagnostico");
  const [selectedNode, setSelectedNode] = useState<string | null>(null);

  // GCP auth state (usado quando cluster é GKE e gcloud não está autenticado)
  const [gcpStatus, setGcpStatus] = useState<GCPAuthStatus | null>(null);
  const [gcpLoginSession, setGcpLoginSession] = useState<GCPLoginSession | null>(null);
  const [gcpPolling, setGcpPolling] = useState(false);
  const gcpPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const { data, isLoading, error, refetch, isFetching } = useQuery<SNATProfile>({
    queryKey: ["snat-profile", cluster],
    queryFn: () => apiClient.get<SNATProfile>(`/nodepools/snat?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster,
    staleTime: 2 * 60 * 1000,
    retry: 1,
  });

  const { data: proj, isLoading: projLoading, refetch: refetchProj } = useQuery<SNATProjection>({
    queryKey: ["snat-projection", cluster],
    queryFn: () => apiClient.get<SNATProjection>(`/nodepools/snat/projection?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster && open && activeTab === "projecao",
    staleTime: 5 * 60 * 1000,
    retry: 1,
  });

  const { data: nodesData, isLoading: nodesLoading, refetch: refetchNodes } = useQuery<SNATNodesResponse>({
    queryKey: ["snat-nodes", cluster],
    queryFn: () => apiClient.get<SNATNodesResponse>(`/nodepools/snat/nodes?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster && open && activeTab === "nos",
    staleTime: 2 * 60 * 1000,
    retry: 1,
  });

  const { data: nodeHistory, isLoading: histLoading } = useQuery<ConntrackHistoryResponse>({
    queryKey: ["snat-node-history", cluster, selectedNode],
    queryFn: () => apiClient.get<ConntrackHistoryResponse>(
      `/nodepools/conntrack/history?cluster=${encodeURIComponent(cluster)}&node=${encodeURIComponent(selectedNode!)}&hours=6`
    ),
    enabled: !!cluster && !!selectedNode && activeTab === "nos",
    staleTime: 3 * 60 * 1000,
    retry: 1,
  });

  // Preços nativos por cloud provider via API de pricing de cada provider
  const { data: costsData } = useQuery<SNATCostInfo>({
    queryKey: ["snat-costs", cluster],
    queryFn: () => apiClient.get<SNATCostInfo>(`/nodepools/snat/costs?cluster=${encodeURIComponent(cluster)}`),
    enabled: !!cluster && open && activeTab === "financeiro",
    staleTime: 60 * 60 * 1000, // preços mudam pouco: cache 1h
    retry: 1,
  });

  // Verificar auth GCP quando modal abre (apenas para GKE)
  useEffect(() => {
    if (open && data?.requires_gcp_auth) {
      checkGCPAuth();
    }
    if (!open) {
      stopGCPPoll();
    }
  }, [open, data?.requires_gcp_auth]);

  useEffect(() => () => stopGCPPoll(), []);

  const stopGCPPoll = () => {
    if (gcpPollRef.current) {
      clearInterval(gcpPollRef.current);
      gcpPollRef.current = null;
    }
  };

  const checkGCPAuth = async () => {
    try {
      const resp = await fetch("/api/v1/gcp/auth/status", {
        headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
      });
      if (resp.ok) {
        const s: GCPAuthStatus = await resp.json();
        setGcpStatus(s);
      }
    } catch {
      // opcional
    }
  };

  const startGCPLogin = async () => {
    try {
      const resp = await fetch("/api/v1/gcp/auth/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const session: GCPLoginSession = await resp.json();
      setGcpLoginSession(session);
      setGcpPolling(true);
      const interval = Math.max((session.interval_sec || 5), 5) * 1000;
      gcpPollRef.current = setInterval(() => pollGCPLogin(session.session_id), interval);
    } catch (err) {
      toast.error("Erro ao iniciar autenticação GCP", {
        description: err instanceof Error ? err.message : String(err),
      });
    }
  };

  const pollGCPLogin = async (sessionID: string) => {
    try {
      const resp = await fetch(`/api/v1/gcp/auth/poll?session_id=${sessionID}`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` },
      });
      if (!resp.ok) return;
      const result = await resp.json();
      if (result.done) {
        stopGCPPoll();
        setGcpPolling(false);
        setGcpLoginSession(null);
        if (result.success) {
          toast.success("GCP autenticado com sucesso!");
          setGcpStatus(null);
          refetch();
        } else {
          toast.error("Autenticação GCP falhou ou expirou");
        }
      }
    } catch {
      // ignorar erros de polling
    }
  };

  if (!cluster) return null;

  const gcpNeedsAuth = data?.requires_gcp_auth && data?.cloud_provider === "gke";

  // Preços efetivos: API de pricing nativa ou fallback documental
  const provider = data?.cloud_provider ?? "aks";
  const fallback = FALLBACK_COSTS[provider] ?? FALLBACK_COSTS.aks;
  const costs: SNATCostInfo = costsData ?? {
    cluster,
    cloud_provider: provider,
    ip_price_monthly:  fallback.ip_price_monthly  ?? 0,
    gw_hourly_price:   fallback.gw_hourly_price   ?? 0,
    data_price_per_gb: fallback.data_price_per_gb ?? 0,
    currency:          fallback.currency           ?? "USD",
    pricing_region:    fallback.pricing_region     ?? "",
    source:            "reference",
    fetched_at:        "",
  };

  // Formatadores monetários por moeda
  const fmtCost = (n: number) =>
    costs.currency === "BRL"
      ? n.toLocaleString("pt-BR", { style: "currency", currency: "BRL", maximumFractionDigits: 2 })
      : `$${n.toFixed(3)}`;

  const colors = data ? statusColors[data.status] : statusColors.ok;
  const StatusIcon = colors.icon;

  const ipsToAdd = data ? Math.max(0, data.ips_needed_for_current_nodes - data.outbound_ip_count) : 0;

  // Prepara dados para o gráfico de projeção
  const chartData = (proj?.records ?? []).map(r => ({
    date: new Date(r.recorded_at).toLocaleDateString("pt-BR", { month: "2-digit", day: "2-digit" }),
    nós: r.total_node_count,
  }));

  const growthPerWeek = proj ? proj.growth_per_day * 7 : 0;
  const GrowthIcon = growthPerWeek > 0.1 ? TrendingUp : growthPerWeek < -0.1 ? TrendingDown : Minus;
  const growthColor = growthPerWeek > 0.1 ? "text-amber-400" : growthPerWeek < -0.1 ? "text-emerald-400" : "text-muted-foreground";

  return (
    <>
      {/* Header compacto — 1 linha só, sem "ml-auto" empurrando os valores pra beira oposta do
          widget (problema visível quando ele passa a dividir a largura com o
          ConntrackAlertWidget). Conteúdo secundário fica num bloco truncável, não mais esticado. */}
      <button
        className={`w-fit flex items-center gap-2 px-3 py-2 rounded-lg border text-xs hover:bg-muted/20 transition-colors text-left whitespace-nowrap ${
          data?.status === "critical" ? "border-red-500/40 bg-red-500/5" :
          data?.status === "warning"  ? "border-amber-500/40 bg-amber-500/5" :
          "border-border/50 bg-muted/10"
        }`}
        onClick={() => setOpen(true)}
      >
        <Network className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
        <span className="font-medium text-foreground flex-shrink-0">SNAT</span>

        {isLoading || isFetching ? (
          <RefreshCw className="w-3 h-3 animate-spin text-muted-foreground flex-shrink-0" />
        ) : gcpNeedsAuth ? (
          <>
            <KeyRound className="w-3.5 h-3.5 text-amber-400 flex-shrink-0" />
            <span className="font-semibold text-amber-400">Login GCP necessário</span>
          </>
        ) : data ? (
          <>
            <StatusIcon className={`w-3.5 h-3.5 ${colors.text} flex-shrink-0`} />
            <span className={`font-semibold ${colors.text} flex-shrink-0`}>{colors.label}</span>
            <span className="text-muted-foreground">
              {data.allocated_outbound_ports > 0 ? (
                <>
                  {fmt(data.total_required_ports)}/{fmt(data.total_available_ports)}
                  {" · "}
                  <span className={
                    data.usage_percent >= 100 ? "text-red-400 font-bold" :
                    data.usage_percent >= 85  ? "text-amber-400 font-bold" :
                    "text-foreground"
                  }>
                    {data.usage_percent.toFixed(1)}%
                  </span>
                  {data.nodes_until_limit <= 10 && (
                    <span className={data.nodes_until_limit <= 0 ? "text-red-400" : "text-amber-400"}>
                      {" · "}{data.nodes_until_limit <= 0 ? "0 nós" : `+${fmt(data.nodes_until_limit)} nós`}
                    </span>
                  )}
                </>
              ) : (
                <>{fmt(data.outbound_ip_count)} EIP{data.outbound_ip_count !== 1 ? "s" : ""} · {fmt(data.total_node_count)} nós</>
              )}
            </span>
          </>
        ) : error ? (
          <span className="text-red-400">Erro ao carregar</span>
        ) : null}

        <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/50 flex-shrink-0" />
      </button>

      {/* Modal */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-2xl max-h-[78vh] flex flex-col gap-0 p-0">

          {/* Cabeçalho */}
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
                <Button
                  size="sm" variant="ghost" className="h-6 text-[11px]"
                  onClick={() => {
                    refetch();
                    if (activeTab === "projecao") refetchProj();
                    if (activeTab === "nos") refetchNodes();
                  }}
                  disabled={isFetching || projLoading || nodesLoading}
                >
                  <RefreshCw className={`w-3 h-3 mr-1 ${(isFetching || projLoading || nodesLoading) ? "animate-spin" : ""}`} />
                  Atualizar
                </Button>
              </div>
            </DialogTitle>
          </DialogHeader>

          {/* GCP Auth necessária */}
          {gcpNeedsAuth && (
            <div className="flex-1 flex flex-col items-center justify-center gap-4 p-8">
              <KeyRound className="w-10 h-10 text-amber-400" />
              <div className="text-center">
                <p className="font-semibold text-foreground">Autenticação GCP necessária</p>
                <p className="text-xs text-muted-foreground mt-1">
                  O gcloud não está autenticado. Faça login para obter dados do Cloud NAT.
                </p>
              </div>

              {!gcpLoginSession && !gcpPolling && (
                <Button size="sm" onClick={startGCPLogin}>
                  <KeyRound className="w-3.5 h-3.5 mr-1.5" />
                  Autenticar com Google
                </Button>
              )}

              {gcpLoginSession && (
                <div className="w-full max-w-sm space-y-3 p-4 rounded-lg border border-amber-500/30 bg-amber-500/5">
                  <p className="text-xs text-muted-foreground text-center">
                    Acesse o link e insira o código:
                  </p>
                  <a
                    href={gcpLoginSession.verify_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block text-xs text-blue-400 underline text-center break-all"
                  >
                    {gcpLoginSession.verify_url}
                  </a>
                  <div className="flex items-center justify-center gap-2">
                    <span className="font-mono text-lg font-bold tracking-widest text-foreground">
                      {gcpLoginSession.user_code}
                    </span>
                    <button
                      className="text-muted-foreground hover:text-foreground"
                      onClick={() => {
                        navigator.clipboard.writeText(gcpLoginSession.user_code);
                        toast.success("Código copiado!");
                      }}
                    >
                      <Copy className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  {gcpPolling && (
                    <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
                      <RefreshCw className="w-3 h-3 animate-spin" />
                      Aguardando confirmação...
                    </div>
                  )}
                  <div className="flex justify-center">
                    <CloudAccountHintField provider="gcp" />
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Abas */}
          {data && !gcpNeedsAuth && (
            <div className="flex border-b border-border/50 px-5 gap-1 flex-shrink-0 bg-background">
              {(["diagnostico", "financeiro", "formula", "projecao", "nos"] as const).map(tab => (
                <button
                  key={tab}
                  onClick={() => setActiveTab(tab)}
                  className={`px-3 py-2 text-xs font-medium transition-colors border-b-2 -mb-px ${
                    activeTab === tab
                      ? "border-primary text-foreground"
                      : "border-transparent text-muted-foreground hover:text-foreground"
                  }`}
                >
                  {tab === "diagnostico" ? "Diagnóstico" :
                   tab === "financeiro"  ? "Financeiro" :
                   tab === "formula"     ? "Fórmula" :
                   tab === "projecao"    ? "Projeção" :
                   "Nós"}
                </button>
              ))}
            </div>
          )}

          {/* Conteúdo com scroll */}
          {data && !gcpNeedsAuth && (
            <div className="overflow-y-auto flex-1 px-5 py-4 text-xs">
              {data.error && <p className="text-amber-400 italic mb-3">{data.error}</p>}

              {/* ── ABA: DIAGNÓSTICO ── */}
              {activeTab === "diagnostico" && (
                <div className="space-y-4">

                  {/* EKS: modelo NAT Gateway (sem alocação de portas por nó) */}
                  {data.cloud_provider === "eks" ? (
                    <>
                      <div className="rounded border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-[11px] text-blue-300">
                        <strong>Modelo EKS (NAT Gateway):</strong> diferente do Azure LB, o NAT Gateway AWS não aloca portas por nó.
                        O limite é de 55.000 conexões simultâneas por EIP por destino único (IP+porta).
                        O número de nós não limita diretamente o SNAT — o gargalo é a saturação de conexões por destino.
                      </div>
                      <div className="grid grid-cols-3 gap-2">
                        <MetricCard label="NAT Gateways / EIPs"   value={fmt(data.outbound_ip_count)} sub="EIPs ativos na VPC" />
                        <MetricCard label="Cap. por EIP/destino"  value={fmt(data.max_ports_per_ip)} sub="conn. simultâneas (AWS)" />
                        <MetricCard label="Total de nós"          value={fmt(data.total_node_count)} />
                        <MetricCard label="Cap. total (estimada)" value={fmt(data.total_available_ports)} sub="por destino único" />
                      </div>
                      {data.node_pools && data.node_pools.length > 0 && (
                        <div className="space-y-1.5">
                          <p className="font-medium text-foreground">Node Groups</p>
                          <div className="rounded border border-border/50 overflow-hidden">
                            <table className="w-full text-[11px]">
                              <thead>
                                <tr className="bg-muted/30 text-muted-foreground">
                                  <th className="text-left px-3 py-1.5">Group</th>
                                  <th className="text-right px-3 py-1.5">Nós</th>
                                </tr>
                              </thead>
                              <tbody>
                                {data.node_pools.map((p, i) => (
                                  <tr key={p.name} className={i % 2 === 0 ? "bg-background/30" : ""}>
                                    <td className="px-3 py-1.5 font-mono">{p.name}</td>
                                    <td className="px-3 py-1.5 text-right">{fmt(p.node_count)}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        </div>
                      )}
                    </>
                  ) : (
                    /* AKS + GKE: fórmula de portas por nó */
                    <>
                      {/* Barra de uso */}
                      <div className="space-y-1.5">
                        <div className="flex justify-between text-muted-foreground">
                          <span>Uso de portas {data.cloud_provider === "gke" ? "Cloud NAT" : "SNAT"}</span>
                          <span className={`${colors.text} font-semibold`}>{data.usage_percent.toFixed(1)}%</span>
                        </div>
                        <div className="h-2.5 rounded-full bg-muted overflow-hidden">
                          <div
                            className={`h-full rounded-full transition-all ${colors.bar}`}
                            style={{ width: `${Math.min(data.usage_percent, 100)}%` }}
                          />
                        </div>
                        <div className="flex justify-between text-muted-foreground pt-0.5">
                          <span>
                            Restam{" "}
                            <span className="text-foreground font-medium">
                              {fmt(data.total_available_ports - data.total_required_ports)}
                            </span>{" "}
                            portas livres
                          </span>
                          {data.allocated_outbound_ports > 0 && (
                            <span>
                              Cabem mais{" "}
                              <span className={`font-semibold ${
                                data.nodes_until_limit <= 0  ? "text-red-400" :
                                data.nodes_until_limit <= 10 ? "text-amber-400" :
                                "text-emerald-400"
                              }`}>
                                {data.nodes_until_limit <= 0 ? "0" : `+${fmt(data.nodes_until_limit)}`} nós
                              </span>
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Cards de métricas */}
                      <div className="grid grid-cols-3 gap-2">
                        <MetricCard label={providerPortsLabel[data.cloud_provider] ?? "Portas por nó"} value={fmt(data.allocated_outbound_ports)} />
                        <MetricCard label={providerIPLabel[data.cloud_provider] ?? "IPs públicos"}     value={fmt(data.outbound_ip_count)} sub={`${fmt(data.outbound_ip_count * data.max_ports_per_ip)} portas totais`} />
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
                    </>
                  )}
                </div>
              )}

              {/* ── ABA: FINANCEIRO ── */}
              {activeTab === "financeiro" && (
                <div className="space-y-4">
                  {/* Badge de fonte dos preços */}
                  <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <Banknote className="w-3 h-3" />
                    {costs.source === "azure-retail-api"  && <span>Preços via <strong className="text-foreground">Azure Retail Prices API</strong> ({costs.pricing_region})</span>}
                    {costs.source === "gcp-billing-api"   && <span>Preços via <strong className="text-foreground">GCP Cloud Billing Catalog API</strong> ({costs.pricing_region})</span>}
                    {costs.source === "aws-pricing-api"   && <span>Preços via <strong className="text-foreground">AWS Pricing API</strong> ({costs.pricing_region})</span>}
                    {costs.source === "reference"         && <span>Preços de referência documental ({costs.pricing_region}){costs.error ? ` — ${costs.error}` : ""}</span>}
                    <span className="ml-auto">{costs.currency}</span>
                  </div>

                  {data.cloud_provider === "eks" ? (
                    /* ── EKS: modelo por hora de gateway + GB processado ── */
                    <div className="rounded border border-border/40 bg-muted/10 p-4 space-y-2 text-xs">
                      <p className="font-medium text-foreground">Modelo de custo EKS (NAT Gateway)</p>
                      <p className="text-muted-foreground">
                        O custo de saída no EKS é cobrado por hora de NAT Gateway + por GB processado,
                        não por IP alocado fixo. O número de EIPs impacta a disponibilidade, não o custo direto de SNAT.
                      </p>
                      <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5 mt-2">
                        <Row label="NAT Gateways / EIPs ativos"       value={`${fmt(data.outbound_ip_count)} EIPs`} />
                        <Row label="Custo NAT Gateway (fixo/hora)"     value={`~${fmtCost(costs.gw_hourly_price)}/h por NAT GW`} />
                        <Row label="Custo mensal por NAT GW (730h)"    value={`~${fmtCost(costs.gw_hourly_price * 730)}/mês`} />
                        <Row label="Custo de dados processados"        value={`~${fmtCost(costs.data_price_per_gb)}/GB (saída VPC → Internet)`} />
                        <Row label="Custo total NAT GWs (mensal est.)" value={`~${fmtCost(costs.gw_hourly_price * 730 * data.outbound_ip_count)}/mês`} />
                      </div>
                      <p className="text-muted-foreground text-[10px]">
                        * Preços AWS sujeitos a variação por região. Consulte a calculadora AWS para valores exatos.
                      </p>
                    </div>

                  ) : data.cloud_provider === "gke" ? (
                    /* ── GKE: Cloud NAT por gateway + IP externo ── */
                    <>
                      {data.outbound_ip_count === 0 ? (
                        /* Sem Cloud NAT configurado nesta região */
                        <div className="rounded border border-amber-500/30 bg-amber-500/5 p-4 space-y-2">
                          <p className="font-medium text-amber-400">Nenhum Cloud NAT encontrado</p>
                          <p className="text-muted-foreground text-[11px]">
                            Este cluster não tem Cloud NAT configurado na região detectada.
                            O tráfego de saída pode estar usando rota alternativa (VPN, Shared VPC, NAT customizado)
                            ou os IPs são gerenciados externamente.
                          </p>
                          <p className="text-muted-foreground text-[11px] pt-1">
                            Preços de referência GCP Cloud NAT:
                            <span className="text-foreground ml-1">{`~${fmtCost(costs.gw_hourly_price)}/h por NAT GW`}</span>
                            {" · "}
                            <span className="text-foreground">{`~${fmtCost(costs.data_price_per_gb)}/GB processado`}</span>
                          </p>
                        </div>
                      ) : (
                        <>
                          <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5">
                            <p className="font-medium text-foreground">Custo Cloud NAT</p>
                            <Row label={providerIPLabel["gke"]} value={`${fmt(data.outbound_ip_count)} IPs/NATs`} />
                            <Row label="Custo por NAT Gateway/hora"  value={`~${fmtCost(costs.gw_hourly_price)}/h`} />
                            <Row label="Custo gateways/mês (730h)"   value={fmtCost(costs.gw_hourly_price * 730 * data.outbound_ip_count)} />
                            <Row label="IP externo estático/mês"     value={`~${fmtCost(costs.ip_price_monthly)}/IP`} />
                            <Row label="Custo IPs/mês"               value={fmtCost(data.outbound_ip_count * costs.ip_price_monthly)} />
                            <Row label="Custo dados processados/GB"  value={`~${fmtCost(costs.data_price_per_gb)}/GB`} />
                          </div>

                          {ipsToAdd > 0 ? (
                            <div className="rounded border border-red-500/30 bg-red-500/5 p-3 space-y-1.5">
                              <p className="font-medium text-red-400">Ajuste necessário (déficit atual)</p>
                              <Row label="IPs adicionais necessários" value={`+${fmt(ipsToAdd)} IPs`} valueClass="text-red-400 font-semibold" />
                              <Row label="Custo adicional IPs/mês"   value={`+${fmtCost(ipsToAdd * costs.ip_price_monthly)}`} valueClass="text-red-400 font-semibold" />
                              <div className="border-t border-border/30 pt-1.5 mt-1.5">
                                <Row label="Total IPs após ajuste (mensal)" value={fmtCost(data.ips_needed_for_current_nodes * costs.ip_price_monthly)} valueClass="text-foreground font-semibold" />
                              </div>
                            </div>
                          ) : (
                            <div className="rounded border border-emerald-500/30 bg-emerald-500/5 p-3">
                              <p className="text-emerald-400 font-medium">Configuração atual cobre os nós presentes</p>
                              <p className="text-muted-foreground mt-1">Nenhum IP adicional necessário para a carga atual.</p>
                            </div>
                          )}
                        </>
                      )}

                      {data.allocated_outbound_ports > 0 && data.max_ports_per_ip > 0 && (
                        <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5">
                          <p className="font-medium text-foreground">Planejamento de capacidade</p>
                          <Row label="Nós por IP (config atual)"  value={`${fmt(Math.floor(data.max_ports_per_ip / data.allocated_outbound_ports))} nós/IP`} />
                          <Row label="Custo por nó adicional"     value={`~${fmtCost(costs.ip_price_monthly / Math.floor(data.max_ports_per_ip / data.allocated_outbound_ports))}/mês`} />
                        </div>
                      )}
                      <p className="text-muted-foreground text-[10px]">
                        * Preços aproximados. Consulte a calculadora Google Cloud para valores exatos por região.
                      </p>
                    </>

                  ) : (
                    /* ── AKS: IP público Standard no Load Balancer ── */
                    <>
                      <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5">
                        <p className="font-medium text-foreground">Custo atual</p>
                        <Row label={providerIPLabel["aks"]} value={`${fmt(data.outbound_ip_count)} IPs`} />
                        <Row label="Custo mensal" value={fmtCost(data.outbound_ip_count * costs.ip_price_monthly)} />
                        <Row label="Custo anual"  value={fmtCost(data.outbound_ip_count * costs.ip_price_monthly * 12)} />
                      </div>

                      {ipsToAdd > 0 ? (
                        <div className="rounded border border-red-500/30 bg-red-500/5 p-3 space-y-1.5">
                          <p className="font-medium text-red-400">Ajuste necessário (déficit atual)</p>
                          <Row label="IPs adicionais necessários" value={`+${fmt(ipsToAdd)} IPs`} valueClass="text-red-400 font-semibold" />
                          <Row label="Custo adicional/mês" value={`+${fmtCost(ipsToAdd * costs.ip_price_monthly)}`} valueClass="text-red-400 font-semibold" />
                          <div className="border-t border-border/30 pt-1.5 mt-1.5">
                            <Row label="Total após ajuste (mensal)" value={fmtCost(data.ips_needed_for_current_nodes * costs.ip_price_monthly)} valueClass="text-foreground font-semibold" />
                            <Row label="Total após ajuste (anual)"  value={fmtCost(data.ips_needed_for_current_nodes * costs.ip_price_monthly * 12)} />
                          </div>
                        </div>
                      ) : (
                        <div className="rounded border border-emerald-500/30 bg-emerald-500/5 p-3">
                          <p className="text-emerald-400 font-medium">Configuração atual cobre os nós presentes</p>
                          <p className="text-muted-foreground mt-1">Nenhum IP adicional necessário para a carga atual.</p>
                        </div>
                      )}

                      {data.allocated_outbound_ports > 0 && data.max_ports_per_ip > 0 && (
                        <div className="rounded border border-border/50 bg-muted/20 p-3 space-y-1.5">
                          <p className="font-medium text-foreground">Planejamento de capacidade</p>
                          <Row label="Nós por IP (config atual)" value={`${fmt(Math.floor(data.max_ports_per_ip / data.allocated_outbound_ports))} nós/IP`} />
                          <Row label="Custo por nó adicional"    value={`~${fmtCost(costs.ip_price_monthly / Math.floor(data.max_ports_per_ip / data.allocated_outbound_ports))}/mês`} />
                          <Row label="1 IP suporta até"          value={`${fmt(Math.floor(data.max_ports_per_ip / data.allocated_outbound_ports))} nós`} />
                        </div>
                      )}

                      <p className="text-muted-foreground text-[10px]">
                        * Preços aproximados. Consulte a calculadora Azure para valores exatos por região e tipo de IP.
                      </p>
                    </>
                  )}
                </div>
              )}

              {/* ── ABA: FÓRMULA ── */}
              {activeTab === "formula" && (
                <div className="space-y-3">
                  {data.cloud_provider === "eks" ? (
                    /* EKS: modelo diferente */
                    <>
                      <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1.5">
                        <p className="font-medium text-foreground">Modelo NAT Gateway (EKS / AWS)</p>
                        <p>O NAT Gateway <strong className="text-foreground">não aloca portas por nó</strong>. O limite é de conexões simultâneas por par <em>(EIP, IP destino + porta destino)</em>.</p>
                        <p className="pt-1">
                          <span className="text-foreground font-medium">Cap. total (por destino)</span>{" "}
                          = EIPs × 55.000
                        </p>
                        <p className="pl-3 font-mono">
                          = {fmt(data.outbound_ip_count)} × 55.000 = <span className="text-foreground">{fmt(data.total_available_ports)}</span>
                        </p>
                        <p className="pt-1 text-[11px]">
                          Se dois pods diferentes no mesmo cluster se conectarem ao mesmo destino (IP+porta),
                          eles compartilham o budget de 55k do mesmo EIP — a saturação por destino é o risco real no EKS.
                        </p>
                      </div>
                      <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1">
                        <p className="font-medium text-foreground">Thresholds</p>
                        <p className="text-[11px]">No EKS o widget usa <strong className="text-foreground">conntrack %</strong> (via Prometheus) como proxy de uso — o status na aba Nós reflete saturação de conntrack, não de portas SNAT diretamente.</p>
                      </div>
                    </>
                  ) : (
                    /* AKS + GKE: fórmula ports-per-node */
                    <>
                      <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1.5">
                        <p className="font-medium text-foreground">
                          Como o orçamento é calculado ({data.cloud_provider === "gke" ? "Cloud NAT" : "Azure LB"})
                        </p>
                        <p>
                          <span className="text-foreground font-medium">Portas disponíveis</span>{" "}
                          = {data.cloud_provider === "gke" ? "IPs no Cloud NAT" : "IPs no LB"} × portas por IP
                        </p>
                        <p className="pl-3 font-mono">
                          = {fmt(data.outbound_ip_count)} × {fmt(data.max_ports_per_ip)}{" "}
                          = <span className="text-foreground">{fmt(data.total_available_ports)}</span>
                        </p>

                        {data.allocated_outbound_ports > 0 && (
                          <>
                            <p className="pt-1">
                              <span className="text-foreground font-medium">Portas necessárias</span>{" "}
                              = total de nós × {data.cloud_provider === "gke" ? "minPortsPerVm" : "portas alocadas por nó"}
                            </p>
                            <p className="pl-3 font-mono">
                              = {fmt(data.total_node_count)} × {fmt(data.allocated_outbound_ports)}{" "}
                              = <span className={data.port_deficit > 0 ? "text-red-400 font-bold" : "text-foreground"}>
                                {fmt(data.total_required_ports)}
                              </span>
                            </p>
                          </>
                        )}

                        <p className="pt-1">
                          <span className="text-foreground font-medium">Uso</span>{" "}
                          = necessárias / disponíveis × 100
                        </p>
                        <p className="pl-3 font-mono">
                          = {fmt(data.total_required_ports)} / {fmt(data.total_available_ports)} × 100{" "}
                          = <span className={colors.text + " font-bold"}>{data.usage_percent.toFixed(2)}%</span>
                        </p>
                      </div>

                      <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1.5">
                        <p className="font-medium text-foreground">Capacidade máxima</p>
                        <p>
                          <span className="text-foreground font-medium">Máx. nós suportados</span>{" "}
                          = portas disponíveis / portas por nó
                        </p>
                        {data.allocated_outbound_ports > 0 && (
                          <p className="pl-3 font-mono">
                            = {fmt(data.total_available_ports)} / {fmt(data.allocated_outbound_ports)}{" "}
                            = <span className="text-foreground">{fmt(data.max_nodes_allowed)}</span>
                          </p>
                        )}
                      </div>

                      {data.port_deficit > 0 && (
                        <div className="rounded border border-red-500/30 bg-red-500/5 p-3 space-y-1">
                          <p className="text-red-400 font-semibold">
                            Déficit: {fmt(data.port_deficit)} portas faltando
                          </p>
                          <p className="text-muted-foreground">
                            {data.cloud_provider === "gke"
                              ? "O Cloud NAT pode começar a descartar conexões de saída silenciosamente quando as portas se esgotam."
                              : "O Azure LB bloqueia a escala de novos nós quando não há portas SNAT suficientes. Conexões de saída falham com erro de esgotamento SNAT."
                            }
                          </p>
                          <p className="text-amber-400 pt-0.5">
                            Solução: adicionar {fmt(ipsToAdd)} IP(s) {data.cloud_provider === "gke" ? "ao Cloud NAT" : "ao LB"}
                            {data.allocated_outbound_ports > 0 &&
                              ` ou reduzir ${data.cloud_provider === "gke" ? "minPortsPerVm" : "allocatedOutboundPorts"} para ≤ ${fmt(Math.floor(data.total_available_ports / data.total_node_count))}`}
                          </p>
                        </div>
                      )}

                      <div className="rounded border border-border/40 bg-muted/10 p-3 text-muted-foreground space-y-1">
                        <p className="font-medium text-foreground">Thresholds de status</p>
                        <Row label="OK"      value="< 80% de uso" />
                        <Row label="Atenção" value="80% – 94%" valueClass="text-amber-400" />
                        <Row label="Crítico" value="≥ 95%"     valueClass="text-red-400" />
                      </div>
                    </>
                  )}
                </div>
              )}

              {/* ── ABA: PROJEÇÃO ── */}
              {activeTab === "projecao" && (
                <div className="space-y-4">
                  {projLoading ? (
                    <div className="flex items-center justify-center gap-2 py-10 text-muted-foreground">
                      <RefreshCw className="w-4 h-4 animate-spin" />
                      Carregando histórico...
                    </div>
                  ) : !proj || proj.data_points < 2 ? (
                    <div className="rounded border border-border/40 bg-muted/10 p-4 text-center space-y-2">
                      <TrendingUp className="w-8 h-8 text-muted-foreground mx-auto" />
                      <p className="text-muted-foreground">
                        Dados insuficientes para projeção.
                      </p>
                      <p className="text-muted-foreground text-[11px]">
                        A projeção é calculada após pelo menos 2 snapshots históricos.
                        Os dados são coletados automaticamente toda vez que este painel é aberto (1 snapshot/hora por cluster).
                        Pontos coletados até agora: <span className="text-foreground">{proj?.data_points ?? 0}</span>.
                      </p>
                    </div>
                  ) : (
                    <>
                      {/* Resumo da projeção */}
                      <div className="grid grid-cols-2 gap-2">
                        <div className="rounded border border-border/40 bg-muted/10 p-3">
                          <p className="text-muted-foreground">Taxa de crescimento</p>
                          <div className="flex items-center gap-1.5 mt-0.5">
                            <GrowthIcon className={`w-4 h-4 ${growthColor}`} />
                            <span className={`font-semibold text-sm ${growthColor}`}>
                              {growthPerWeek >= 0 ? "+" : ""}{growthPerWeek.toFixed(1)} nós/semana
                            </span>
                          </div>
                          <p className="text-muted-foreground text-[10px] mt-0.5">
                            ({proj.growth_per_day >= 0 ? "+" : ""}{proj.growth_per_day.toFixed(2)} nós/dia — regressão linear)
                          </p>
                        </div>

                        <div className="rounded border border-border/40 bg-muted/10 p-3">
                          <p className="text-muted-foreground">Confiança da estimativa</p>
                          <div className="mt-0.5">
                            <span className={`inline-block px-2 py-0.5 rounded text-[11px] font-medium ${confidenceLabel[proj.confidence]?.cls ?? ""}`}>
                              {confidenceLabel[proj.confidence]?.label ?? proj.confidence}
                            </span>
                          </div>
                          <p className="text-muted-foreground text-[10px] mt-0.5">
                            {proj.data_points} pontos · span {
                              proj.records.length >= 2
                                ? Math.round((new Date(proj.records[proj.records.length - 1].recorded_at).getTime() - new Date(proj.records[0].recorded_at).getTime()) / 86400000)
                                : 0
                            } dias
                          </p>
                        </div>

                        {proj.days_until_limit > 0 ? (
                          <>
                            <div className="rounded border border-amber-500/30 bg-amber-500/5 p-3">
                              <p className="text-muted-foreground">Estimativa de limite</p>
                              <p className="font-semibold text-sm text-amber-400 mt-0.5">
                                em ~{fmt(proj.days_until_limit)} dias
                              </p>
                              <p className="text-muted-foreground text-[10px] mt-0.5">
                                com o ritmo de crescimento atual
                              </p>
                            </div>
                            <div className="rounded border border-amber-500/30 bg-amber-500/5 p-3">
                              <p className="text-muted-foreground">Data estimada</p>
                              <p className="font-semibold text-sm text-foreground mt-0.5">
                                {proj.estimated_date
                                  ? new Date(proj.estimated_date).toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric" })
                                  : "—"}
                              </p>
                              <p className="text-muted-foreground text-[10px] mt-0.5">
                                data em que o limite SNAT será atingido
                              </p>
                            </div>
                          </>
                        ) : (
                          <div className="col-span-2 rounded border border-emerald-500/30 bg-emerald-500/5 p-3">
                            <p className="text-emerald-400 font-medium">
                              {proj.growth_per_day <= 0
                                ? "Frota estável ou em redução — sem projeção de limite"
                                : "Ritmo de crescimento dentro da margem disponível"}
                            </p>
                          </div>
                        )}
                      </div>

                      {/* Gráfico histórico */}
                      {chartData.length >= 2 && (
                        <div className="space-y-1.5">
                          <p className="font-medium text-foreground">Histórico de nós (últimos 30 dias)</p>
                          <div className="h-44 w-full">
                            <ResponsiveContainer width="100%" height="100%">
                              <LineChart data={chartData} margin={{ top: 4, right: 8, left: -16, bottom: 0 }}>
                                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" strokeOpacity={0.4} />
                                <XAxis
                                  dataKey="date"
                                  tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                                  interval="preserveStartEnd"
                                />
                                <YAxis
                                  tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                                  allowDecimals={false}
                                />
                                <Tooltip
                                  contentStyle={{
                                    background: "hsl(var(--background))",
                                    border: "1px solid hsl(var(--border))",
                                    borderRadius: 6,
                                    fontSize: 11,
                                  }}
                                  formatter={(v: number) => [`${v} nós`, "Total de nós"]}
                                />
                                {data.max_nodes_allowed > 0 && (
                                  <ReferenceLine
                                    y={data.max_nodes_allowed}
                                    stroke="hsl(var(--destructive))"
                                    strokeDasharray="4 2"
                                    label={{ value: `Limite (${data.max_nodes_allowed})`, fill: "hsl(var(--destructive))", fontSize: 10, position: "insideTopRight" }}
                                  />
                                )}
                                <Line
                                  type="monotone"
                                  dataKey="nós"
                                  stroke="hsl(var(--primary))"
                                  strokeWidth={2}
                                  dot={{ r: 3, fill: "hsl(var(--primary))" }}
                                  activeDot={{ r: 5 }}
                                />
                              </LineChart>
                            </ResponsiveContainer>
                          </div>
                          <p className="text-muted-foreground text-[10px]">
                            Linha vermelha tracejada = limite máximo de nós com a configuração SNAT atual.
                            Dados coletados automaticamente (1 snapshot/hora).
                          </p>
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}
              {/* ── ABA: NÓS ── */}
              {activeTab === "nos" && (
                <div className="space-y-3">
                  {nodesLoading ? (
                    <div className="flex items-center justify-center gap-2 py-10 text-muted-foreground">
                      <RefreshCw className="w-4 h-4 animate-spin" />
                      Carregando dados dos nós...
                    </div>
                  ) : !nodesData ? null : (
                    <>
                      {!nodesData.prometheus_available && (
                        <div className="rounded border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-amber-400 text-[11px]">
                          Prometheus indisponível — exibindo lista de nós sem dados de conntrack.
                        </div>
                      )}

                      {/* Tabela de nós */}
                      <div className="space-y-1.5">
                        <div className="flex items-center gap-1.5">
                          <Server className="w-3.5 h-3.5 text-muted-foreground" />
                          <p className="font-medium text-foreground">
                            {nodesData.nodes?.length ?? 0} nós
                            {nodesData.allocated_outbound_ports > 0
                              ? ` · portas alocadas por nó: ${fmt(nodesData.allocated_outbound_ports)}`
                              : nodesData.cloud_provider === "eks"
                              ? " · NAT Gateway (sem alocação por nó)"
                              : ""}
                          </p>
                        </div>
                        <div className="rounded border border-border/50 overflow-hidden">
                          <table className="w-full text-[11px]">
                            <thead>
                              <tr className="bg-muted/30 text-muted-foreground">
                                <th className="text-left px-3 py-1.5">Nó</th>
                                <th className="text-left px-3 py-1.5">Pool</th>
                                <th className="text-right px-3 py-1.5">Conntrack</th>
                                <th className="text-left px-3 py-1.5 w-28">{nodesData.allocated_outbound_ports > 0 ? "Uso SNAT est." : "Conntrack %"}</th>
                              </tr>
                            </thead>
                            <tbody>
                              {(nodesData.nodes ?? []).map((n, i) => {
                                const isSelected = selectedNode === n.name;
                                const barColor =
                                  n.status === "critical" ? "bg-red-500" :
                                  n.status === "warning"  ? "bg-amber-500" :
                                  n.status === "unknown"  ? "bg-muted" :
                                  "bg-emerald-500";
                                return (
                                  <tr
                                    key={n.name}
                                    className={`cursor-pointer transition-colors ${
                                      isSelected ? "bg-primary/10 border-l-2 border-primary" :
                                      i % 2 === 0 ? "bg-background/30 hover:bg-muted/20" : "hover:bg-muted/20"
                                    }`}
                                    onClick={() => setSelectedNode(isSelected ? null : n.name)}
                                  >
                                    <td className="px-3 py-1.5 font-mono max-w-[140px]">
                                      <span className="truncate block" title={n.name}>
                                        {n.name.length > 26 ? `…${n.name.slice(-24)}` : n.name}
                                      </span>
                                    </td>
                                    <td className="px-3 py-1.5 text-muted-foreground">{n.pool || "—"}</td>
                                    <td className="px-3 py-1.5 text-right">
                                      {n.conntrack_entries > 0 ? fmt(n.conntrack_entries) : "—"}
                                    </td>
                                    <td className="px-3 py-1.5">
                                      {n.status === "unknown" || (n.allocated_ports === 0 && n.conntrack_usage_pct === 0) ? (
                                        <span className="text-muted-foreground">N/D</span>
                                      ) : (
                                        <div className="flex items-center gap-1.5">
                                          <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
                                            <div
                                              className={`h-full rounded-full ${barColor}`}
                                              style={{ width: `${Math.min(n.allocated_ports > 0 ? n.snat_usage_pct : n.conntrack_usage_pct, 100)}%` }}
                                            />
                                          </div>
                                          <span className={`w-8 text-right ${
                                            n.status === "critical" ? "text-red-400 font-semibold" :
                                            n.status === "warning"  ? "text-amber-400" : "text-foreground"
                                          }`}>
                                            {(n.allocated_ports > 0 ? n.snat_usage_pct : n.conntrack_usage_pct).toFixed(0)}%
                                          </span>
                                        </div>
                                      )}
                                    </td>
                                  </tr>
                                );
                              })}
                            </tbody>
                          </table>
                        </div>
                        <p className="text-muted-foreground text-[10px]">
                          Clique em um nó para ver o histórico de 6h ·{" "}
                          {nodesData.allocated_outbound_ports > 0
                            ? "Estimativa SNAT = conntrack ÷ portas alocadas (conntrack inclui tráfego de entrada e local — valor é limite superior)"
                            : "Uso = conntrack ÷ limite do kernel — SNAT por nó não aplicável neste provider (EKS)"}
                        </p>
                      </div>

                      {/* Gráfico histórico do nó selecionado */}
                      {selectedNode && (
                        <div className="rounded border border-border/40 bg-muted/10 p-3 space-y-2">
                          <div className="flex items-center justify-between">
                            <p className="font-medium text-foreground text-[11px] font-mono truncate max-w-[300px]" title={selectedNode}>
                              {selectedNode.length > 32 ? `…${selectedNode.slice(-30)}` : selectedNode} · últimas 6h
                            </p>
                            {histLoading && <RefreshCw className="w-3 h-3 animate-spin text-muted-foreground flex-shrink-0" />}
                          </div>

                          {nodeHistory && !nodeHistory.prometheus_available ? (
                            <p className="text-amber-400 text-[11px]">Prometheus indisponível para histórico.</p>
                          ) : nodeHistory?.error ? (
                            <p className="text-muted-foreground text-[11px]">{nodeHistory.error}</p>
                          ) : nodeHistory && nodeHistory.points?.length > 0 ? (
                            <div className="h-32">
                              <ResponsiveContainer width="100%" height="100%">
                                <AreaChart
                                  data={nodeHistory.points.map(p => ({
                                    time: new Date(p.ts * 1000).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" }),
                                    conntrack: p.count,
                                  }))}
                                  margin={{ top: 2, right: 8, left: -20, bottom: 0 }}
                                >
                                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" strokeOpacity={0.3} />
                                  <XAxis dataKey="time" tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }} interval="preserveStartEnd" />
                                  <YAxis tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }} allowDecimals={false} />
                                  <Tooltip
                                    contentStyle={{ background: "hsl(var(--background))", border: "1px solid hsl(var(--border))", borderRadius: 6, fontSize: 10 }}
                                    formatter={(v: number) => [`${fmt(v)}`, "Conntrack"]}
                                  />
                                  {nodesData.allocated_outbound_ports > 0 && (
                                    <ReferenceLine
                                      y={nodesData.allocated_outbound_ports}
                                      stroke="hsl(var(--destructive))"
                                      strokeDasharray="3 2"
                                      label={{ value: `Limite SNAT (${fmt(nodesData.allocated_outbound_ports)})`, fill: "hsl(var(--destructive))", fontSize: 9, position: "insideTopRight" }}
                                    />
                                  )}
                                  <Area type="monotone" dataKey="conntrack" stroke="hsl(var(--primary))" fill="hsl(var(--primary))" fillOpacity={0.15} strokeWidth={1.5} dot={false} />
                                </AreaChart>
                              </ResponsiveContainer>
                            </div>
                          ) : !histLoading ? (
                            <p className="text-muted-foreground text-[11px]">Sem dados históricos para este nó.</p>
                          ) : null}
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}
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
