import { useState, useEffect } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Activity,
  RefreshCcw,
  Loader2,
  Server,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  Clock,
  TrendingUp,
  BarChart3,
} from "lucide-react";
import { toast } from "sonner";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  BarChart,
  Bar,
  Cell,
  PieChart,
  Pie,
} from "recharts";
import { apiClient } from "@/lib/api/client";

// Tipos para as métricas do dashboard
interface ClusterMetrics {
  cluster: string;
  last_status: string;
  last_run_at: string;
  total_runs: number;
  healthy_runs: number;
  warning_runs: number;
  critical_runs: number;
  avg_duration_ms: number;
  total_healthy: number;
  total_warnings: number;
  total_critical: number;
}

interface DurationDataPoint {
  timestamp: string;
  cluster: string;
  duration_ms: number;
  status: string;
}

interface SeverityCount {
  severity: string;
  count: number;
}

interface DashboardMetrics {
  cluster_metrics: ClusterMetrics[];
  duration_history: DurationDataPoint[];
  severity_breakdown: SeverityCount[];
  summary: {
    total_clusters: number;
    total_runs: number;
    health_rate: number;
    avg_duration_ms: number;
    last_run_at: string | null;
  };
}

// Cores para status
const STATUS_COLORS = {
  healthy: "#22c55e",
  warning: "#f59e0b",
  critical: "#ef4444",
};

const SEVERITY_COLORS = {
  healthy: "#22c55e",
  warning: "#f59e0b",
  critical: "#ef4444",
};

export const HealthCheckMetricsDashboard = () => {
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [days, setDays] = useState("7");

  const fetchMetrics = async () => {
    try {
      setLoading(true);
      const response = await apiClient.get(`/healthcheck/dashboard?days=${days}`);
      if (response.success && response.data) {
        setMetrics(response.data);
      }
    } catch (error) {
      console.error("Erro ao buscar métricas:", error);
      toast.error("Falha ao carregar métricas do dashboard");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchMetrics();
  }, [days]);

  const formatDuration = (ms: number) => {
    if (ms < 1000) return `${Math.round(ms)}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  };

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "-";
    const date = new Date(dateStr);
    return date.toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "healthy":
        return <CheckCircle2 className="h-4 w-4 text-green-500" />;
      case "warning":
        return <AlertTriangle className="h-4 w-4 text-yellow-500" />;
      case "critical":
        return <XCircle className="h-4 w-4 text-red-500" />;
      default:
        return <Activity className="h-4 w-4 text-gray-400" />;
    }
  };

  const getStatusBadge = (status: string) => {
    const variants: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
      healthy: "default",
      warning: "secondary",
      critical: "destructive",
    };
    const colors: Record<string, string> = {
      healthy: "bg-green-500/20 text-green-600 border-green-500/30",
      warning: "bg-yellow-500/20 text-yellow-600 border-yellow-500/30",
      critical: "bg-red-500/20 text-red-600 border-red-500/30",
    };

    return (
      <Badge variant="outline" className={colors[status] || ""}>
        {status === "healthy" ? "Saudável" : status === "warning" ? "Aviso" : "Crítico"}
      </Badge>
    );
  };

  // Preparar dados para gráfico de linha (duração)
  const durationChartData = metrics?.duration_history?.map((dp) => ({
    time: formatDate(dp.timestamp),
    cluster: dp.cluster,
    duration: dp.duration_ms / 1000, // converter para segundos
    status: dp.status,
  })) || [];

  // Preparar dados para gráfico de pizza (severidade)
  const severityPieData = metrics?.severity_breakdown?.map((s) => ({
    name: s.severity === "healthy" ? "Saudável" : s.severity === "warning" ? "Aviso" : "Crítico",
    value: s.count,
    fill: SEVERITY_COLORS[s.severity as keyof typeof SEVERITY_COLORS] || "#gray",
  })) || [];

  // Preparar dados para gráfico de barras (por cluster)
  const clusterBarData = metrics?.cluster_metrics?.map((cm) => ({
    cluster: cm.cluster.replace(/-admin$/, "").split("-").slice(-2).join("-"), // encurtar nome
    healthy: cm.healthy_runs,
    warning: cm.warning_runs,
    critical: cm.critical_runs,
    healthRate: cm.total_runs > 0 ? ((cm.healthy_runs / cm.total_runs) * 100).toFixed(0) : 0,
  })) || [];

  if (loading && !metrics) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="flex flex-col items-center gap-4">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-sm text-muted-foreground">Carregando métricas...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4 p-4">
      {/* Header com controles */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">Dashboard de Métricas</h2>
        </div>
        <div className="flex items-center gap-2">
          <Select value={days} onValueChange={setDays}>
            <SelectTrigger className="w-[140px]">
              <SelectValue placeholder="Período" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">Último dia</SelectItem>
              <SelectItem value="7">Últimos 7 dias</SelectItem>
              <SelectItem value="14">Últimos 14 dias</SelectItem>
              <SelectItem value="30">Últimos 30 dias</SelectItem>
              <SelectItem value="90">Últimos 90 dias</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={fetchMetrics} disabled={loading}>
            {loading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCcw className="h-4 w-4" />
            )}
          </Button>
        </div>
      </div>

      {/* Cards de resumo */}
      <div className="grid grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Clusters Monitorados</CardDescription>
            <CardTitle className="text-2xl">{metrics?.summary.total_clusters || 0}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center text-xs text-muted-foreground">
              <Server className="h-3 w-3 mr-1" />
              Total de clusters
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Total de Execuções</CardDescription>
            <CardTitle className="text-2xl">{metrics?.summary.total_runs || 0}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center text-xs text-muted-foreground">
              <Activity className="h-3 w-3 mr-1" />
              Health checks executados
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Taxa de Saúde</CardDescription>
            <CardTitle className="text-2xl text-green-600">
              {(metrics?.summary.health_rate || 0).toFixed(1)}%
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Progress
              value={metrics?.summary.health_rate || 0}
              className="h-2"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Duração Média</CardDescription>
            <CardTitle className="text-2xl">
              {formatDuration(metrics?.summary.avg_duration_ms || 0)}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center text-xs text-muted-foreground">
              <Clock className="h-3 w-3 mr-1" />
              Por execução
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Gráficos */}
      <div className="grid grid-cols-2 gap-4">
        {/* Gráfico de Duração */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <TrendingUp className="h-4 w-4" />
              Histórico de Duração
            </CardTitle>
            <CardDescription>Tempo de execução dos health checks (segundos)</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="h-[250px]">
              {durationChartData.length > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={durationChartData}>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 10 }}
                      tickFormatter={(v) => v.split(" ")[0]}
                    />
                    <YAxis tick={{ fontSize: 10 }} />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: "hsl(var(--background))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: "6px",
                      }}
                      formatter={(value: number) => [`${value.toFixed(1)}s`, "Duração"]}
                    />
                    <Line
                      type="monotone"
                      dataKey="duration"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      dot={{ fill: "#3b82f6", r: 3 }}
                    />
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-muted-foreground">
                  Sem dados de histórico
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Gráfico de Severidade */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <BarChart3 className="h-4 w-4" />
              Distribuição de Issues
            </CardTitle>
            <CardDescription>Total de issues por severidade</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="h-[250px]">
              {severityPieData.length > 0 && severityPieData.some(d => d.value > 0) ? (
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={severityPieData}
                      cx="50%"
                      cy="50%"
                      innerRadius={60}
                      outerRadius={90}
                      paddingAngle={2}
                      dataKey="value"
                      label={({ name, value }) => `${name}: ${value}`}
                      labelLine={{ stroke: "hsl(var(--muted-foreground))", strokeWidth: 1 }}
                    >
                      {severityPieData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.fill} />
                      ))}
                    </Pie>
                    <Tooltip
                      contentStyle={{
                        backgroundColor: "hsl(var(--background))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: "6px",
                      }}
                    />
                  </PieChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-muted-foreground">
                  Sem dados de severidade
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Gráfico de Barras por Cluster */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Server className="h-4 w-4" />
            Status por Cluster
          </CardTitle>
          <CardDescription>Execuções de health check agrupadas por cluster</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="h-[200px]">
            {clusterBarData.length > 0 ? (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={clusterBarData} layout="vertical">
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" horizontal={false} />
                  <XAxis type="number" tick={{ fontSize: 10 }} />
                  <YAxis dataKey="cluster" type="category" tick={{ fontSize: 10 }} width={100} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--background))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "6px",
                    }}
                  />
                  <Legend />
                  <Bar dataKey="healthy" name="Saudável" stackId="a" fill={STATUS_COLORS.healthy} />
                  <Bar dataKey="warning" name="Aviso" stackId="a" fill={STATUS_COLORS.warning} />
                  <Bar dataKey="critical" name="Crítico" stackId="a" fill={STATUS_COLORS.critical} />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="h-full flex items-center justify-center text-muted-foreground">
                Sem dados de clusters
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Tabela de Clusters */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Status Atual dos Clusters</CardTitle>
          <CardDescription>Último status e estatísticas por cluster</CardDescription>
        </CardHeader>
        <CardContent>
          <ScrollArea className="h-[300px]">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Cluster</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Última Execução</TableHead>
                  <TableHead className="text-right">Execuções</TableHead>
                  <TableHead className="text-right">Taxa Saúde</TableHead>
                  <TableHead className="text-right">Duração Média</TableHead>
                  <TableHead className="text-right">Issues</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {metrics?.cluster_metrics?.map((cm) => {
                  const healthRate = cm.total_runs > 0
                    ? ((cm.healthy_runs / cm.total_runs) * 100).toFixed(0)
                    : 0;
                  const totalIssues = cm.total_warnings + cm.total_critical;

                  return (
                    <TableRow key={cm.cluster}>
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-2">
                          {getStatusIcon(cm.last_status)}
                          <span className="truncate max-w-[200px]" title={cm.cluster}>
                            {cm.cluster}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>{getStatusBadge(cm.last_status)}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatDate(cm.last_run_at)}
                      </TableCell>
                      <TableCell className="text-right">{cm.total_runs}</TableCell>
                      <TableCell className="text-right">
                        <span className={Number(healthRate) >= 80 ? "text-green-600" : Number(healthRate) >= 50 ? "text-yellow-600" : "text-red-600"}>
                          {healthRate}%
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        {formatDuration(cm.avg_duration_ms)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          {cm.total_warnings > 0 && (
                            <Badge variant="outline" className="bg-yellow-500/20 text-yellow-600 text-xs">
                              {cm.total_warnings}
                            </Badge>
                          )}
                          {cm.total_critical > 0 && (
                            <Badge variant="outline" className="bg-red-500/20 text-red-600 text-xs">
                              {cm.total_critical}
                            </Badge>
                          )}
                          {totalIssues === 0 && (
                            <span className="text-muted-foreground text-xs">-</span>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
                {(!metrics?.cluster_metrics || metrics.cluster_metrics.length === 0) && (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center text-muted-foreground py-8">
                      Nenhum health check executado no período selecionado
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </ScrollArea>
        </CardContent>
      </Card>
    </div>
  );
};
