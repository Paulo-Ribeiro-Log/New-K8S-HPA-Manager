// MetricsPanel - Painel de visualização profissional de métricas

import { useState, useMemo } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Activity,
  TrendingUp,
  TrendingDown,
  AlertCircle,
  RefreshCw,
  BarChart3,
  Cpu,
  MemoryStick,
  Users,
} from "lucide-react";
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ReferenceLine,
  ReferenceArea,
} from "recharts";
import { useHPAMetrics } from "@/hooks/useMonitoring";
import type { HPASnapshot } from "@/lib/api/types";
import { format } from "date-fns";
import { ptBR } from "date-fns/locale";

interface MetricsPanelProps {
  cluster: string;
  namespace: string;
  hpaName: string;
  className?: string;
}

interface MetricStats {
  current: number;
  average: number;
  peak: number;
  min: number;
  p95: number;
  trend: "up" | "down" | "stable";
  trendPercent: number;
}

export function MetricsPanel({
  cluster,
  namespace,
  hpaName,
  className = "",
}: MetricsPanelProps) {
  const [duration, setDuration] = useState<string>("1h");
  const { metrics, loading, error, refetch } = useHPAMetrics(
    cluster,
    namespace,
    hpaName,
    duration
  );

  // Preparar dados para os gráficos
  const chartData = useMemo(() => {
    if (!metrics || !metrics.snapshots || metrics.snapshots.length === 0) {
      return [];
    }

    return metrics.snapshots.map((snapshot: HPASnapshot) => ({
      timestamp: new Date(snapshot.timestamp).getTime(),
      time: format(new Date(snapshot.timestamp), "HH:mm:ss", { locale: ptBR }),
      cpuCurrent: snapshot.cpu_current,
      cpuTarget: snapshot.cpu_target,
      memoryCurrent: snapshot.memory_current,
      memoryTarget: snapshot.memory_target,
      replicasCurrent: snapshot.replicas_current,
      replicasDesired: snapshot.replicas_desired,
      replicasMin: snapshot.replicas_min,
      replicasMax: snapshot.replicas_max,
    }));
  }, [metrics]);

  // Calcular estatísticas
  const cpuStats = useMemo((): MetricStats => {
    if (chartData.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const values = chartData.map(d => d.cpuCurrent);
    const current = values[values.length - 1];
    const average = values.reduce((a, b) => a + b, 0) / values.length;
    const peak = Math.max(...values);
    const min = Math.min(...values);
    const sorted = [...values].sort((a, b) => a - b);
    const p95 = sorted[Math.floor(sorted.length * 0.95)];

    // Calcular tendência (últimos 5 vs primeiros 5 pontos)
    const recentAvg = values.slice(-5).reduce((a, b) => a + b, 0) / 5;
    const oldAvg = values.slice(0, 5).reduce((a, b) => a + b, 0) / 5;
    const trendPercent = ((recentAvg - oldAvg) / oldAvg) * 100;
    const trend = Math.abs(trendPercent) < 5 ? "stable" : trendPercent > 0 ? "up" : "down";

    return { current, average, peak, min, p95, trend, trendPercent };
  }, [chartData]);

  const memoryStats = useMemo((): MetricStats => {
    if (chartData.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const values = chartData.map(d => d.memoryCurrent);
    const current = values[values.length - 1];
    const average = values.reduce((a, b) => a + b, 0) / values.length;
    const peak = Math.max(...values);
    const min = Math.min(...values);
    const sorted = [...values].sort((a, b) => a - b);
    const p95 = sorted[Math.floor(sorted.length * 0.95)];

    const recentAvg = values.slice(-5).reduce((a, b) => a + b, 0) / 5;
    const oldAvg = values.slice(0, 5).reduce((a, b) => a + b, 0) / 5;
    const trendPercent = ((recentAvg - oldAvg) / oldAvg) * 100;
    const trend = Math.abs(trendPercent) < 5 ? "stable" : trendPercent > 0 ? "up" : "down";

    return { current, average, peak, min, p95, trend, trendPercent };
  }, [chartData]);

  // Componente de Estatística
  const StatCard = ({
    icon: Icon,
    label,
    value,
    unit,
    trend,
    trendPercent,
    className: cardClassName = "",
  }: {
    icon: any;
    label: string;
    value: number;
    unit: string;
    trend: "up" | "down" | "stable";
    trendPercent: number;
    className?: string;
  }) => {
    const TrendIcon = trend === "up" ? TrendingUp : trend === "down" ? TrendingDown : Activity;
    const trendColor =
      trend === "up" ? "text-red-600" : trend === "down" ? "text-green-600" : "text-muted-foreground";

    return (
      <div className={`p-4 rounded-lg border bg-card ${cardClassName}`}>
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <Icon className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">{label}</span>
          </div>
          <div className={`flex items-center gap-1 text-xs ${trendColor}`}>
            <TrendIcon className="h-3 w-3" />
            <span>{Math.abs(trendPercent).toFixed(1)}%</span>
          </div>
        </div>
        <div className="text-2xl font-bold">
          {value.toFixed(1)}
          <span className="text-sm font-normal text-muted-foreground ml-1">{unit}</span>
        </div>
      </div>
    );
  };

  // Custom Tooltip
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (!active || !payload || !payload.length) return null;

    return (
      <div className="bg-background border rounded-lg shadow-lg p-3 space-y-2">
        <p className="text-sm font-semibold">{label}</p>
        {payload.map((entry: any, index: number) => (
          <div key={index} className="flex items-center gap-2 text-xs">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: entry.color }} />
            <span className="text-muted-foreground">{entry.name}:</span>
            <span className="font-semibold">{entry.value.toFixed(1)}{entry.unit || "%"}</span>
          </div>
        ))}
      </div>
    );
  };

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <BarChart3 className="h-5 w-5" />
              Análise de Métricas
            </CardTitle>
            <CardDescription>
              {cluster} / {namespace} / {hpaName}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Select value={duration} onValueChange={setDuration}>
              <SelectTrigger className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="5m">5 min</SelectItem>
                <SelectItem value="15m">15 min</SelectItem>
                <SelectItem value="30m">30 min</SelectItem>
                <SelectItem value="1h">1 hora</SelectItem>
                <SelectItem value="3h">3 horas</SelectItem>
                <SelectItem value="6h">6 horas</SelectItem>
                <SelectItem value="12h">12 horas</SelectItem>
                <SelectItem value="24h">24 horas</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="icon"
              onClick={() => refetch(duration)}
              disabled={loading}
            >
              <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {error && (
          <div className="p-4 border border-red-200 bg-red-50 rounded-lg text-sm text-red-600">
            <AlertCircle className="h-4 w-4 inline mr-2" />
            {error}
          </div>
        )}

        {loading && !error && (
          <div className="text-center py-12 text-muted-foreground">
            <RefreshCw className="h-8 w-8 animate-spin mx-auto mb-2" />
            <p>Carregando métricas...</p>
          </div>
        )}

        {!loading && !error && chartData.length === 0 && (
          <div className="text-center py-12">
            <Activity className="h-12 w-12 text-muted-foreground mx-auto mb-2" />
            <p className="text-lg font-semibold">Sem dados disponíveis</p>
            <p className="text-sm text-muted-foreground">
              Inicie o monitoring engine para coletar métricas
            </p>
          </div>
        )}

        {!loading && !error && chartData.length > 0 && (
          <Tabs defaultValue="cpu" className="space-y-6">
            <TabsList className="grid w-full grid-cols-3">
              <TabsTrigger value="cpu" className="gap-2">
                <Cpu className="h-4 w-4" />
                CPU
              </TabsTrigger>
              <TabsTrigger value="memory" className="gap-2">
                <MemoryStick className="h-4 w-4" />
                Memória
              </TabsTrigger>
              <TabsTrigger value="replicas" className="gap-2">
                <Users className="h-4 w-4" />
                Réplicas
              </TabsTrigger>
            </TabsList>

            {/* CPU Analysis */}
            <TabsContent value="cpu" className="space-y-6">
              {/* Estatísticas */}
              <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
                <StatCard
                  icon={Activity}
                  label="Atual"
                  value={cpuStats.current}
                  unit="%"
                  trend={cpuStats.trend}
                  trendPercent={cpuStats.trendPercent}
                />
                <StatCard
                  icon={TrendingUp}
                  label="Média"
                  value={cpuStats.average}
                  unit="%"
                  trend="stable"
                  trendPercent={0}
                />
                <StatCard
                  icon={AlertCircle}
                  label="Pico"
                  value={cpuStats.peak}
                  unit="%"
                  trend="up"
                  trendPercent={0}
                  className="border-orange-200 bg-orange-50"
                />
                <StatCard
                  icon={TrendingDown}
                  label="Mínimo"
                  value={cpuStats.min}
                  unit="%"
                  trend="down"
                  trendPercent={0}
                />
                <StatCard
                  icon={BarChart3}
                  label="P95"
                  value={cpuStats.p95}
                  unit="%"
                  trend="stable"
                  trendPercent={0}
                />
              </div>

              {/* Gráfico de CPU */}
              <div className="border rounded-lg p-4 bg-card">
                <h3 className="text-sm font-semibold mb-4 flex items-center gap-2">
                  <Cpu className="h-4 w-4" />
                  Uso de CPU ao longo do tempo
                </h3>
                <ResponsiveContainer width="100%" height={300}>
                  <AreaChart data={chartData}>
                    <defs>
                      <linearGradient id="cpuGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 12 }}
                      className="text-xs"
                    />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "CPU (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    <ReferenceLine
                      y={chartData[0]?.cpuTarget || 0}
                      stroke="#10b981"
                      strokeDasharray="5 5"
                      label={{ value: "Target", position: "right", fontSize: 12 }}
                    />
                    <ReferenceArea
                      y1={90}
                      y2={100}
                      fill="#ef4444"
                      fillOpacity={0.1}
                      label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }}
                    />
                    <ReferenceArea
                      y1={80}
                      y2={90}
                      fill="#f59e0b"
                      fillOpacity={0.1}
                      label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }}
                    />
                    <Area
                      type="monotone"
                      dataKey="cpuCurrent"
                      name="CPU Atual"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      fill="url(#cpuGradient)"
                      unit="%"
                    />
                    <Line
                      type="monotone"
                      dataKey="cpuTarget"
                      name="CPU Target"
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      dot={false}
                      unit="%"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </TabsContent>

            {/* Memory Analysis */}
            <TabsContent value="memory" className="space-y-6">
              {/* Estatísticas */}
              <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
                <StatCard
                  icon={Activity}
                  label="Atual"
                  value={memoryStats.current}
                  unit="%"
                  trend={memoryStats.trend}
                  trendPercent={memoryStats.trendPercent}
                />
                <StatCard
                  icon={TrendingUp}
                  label="Média"
                  value={memoryStats.average}
                  unit="%"
                  trend="stable"
                  trendPercent={0}
                />
                <StatCard
                  icon={AlertCircle}
                  label="Pico"
                  value={memoryStats.peak}
                  unit="%"
                  trend="up"
                  trendPercent={0}
                  className="border-orange-200 bg-orange-50"
                />
                <StatCard
                  icon={TrendingDown}
                  label="Mínimo"
                  value={memoryStats.min}
                  unit="%"
                  trend="down"
                  trendPercent={0}
                />
                <StatCard
                  icon={BarChart3}
                  label="P95"
                  value={memoryStats.p95}
                  unit="%"
                  trend="stable"
                  trendPercent={0}
                />
              </div>

              {/* Gráfico de Memória */}
              <div className="border rounded-lg p-4 bg-card">
                <h3 className="text-sm font-semibold mb-4 flex items-center gap-2">
                  <MemoryStick className="h-4 w-4" />
                  Uso de Memória ao longo do tempo
                </h3>
                <ResponsiveContainer width="100%" height={300}>
                  <AreaChart data={chartData}>
                    <defs>
                      <linearGradient id="memoryGradient" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#8b5cf6" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 12 }}
                      className="text-xs"
                    />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "Memória (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    <ReferenceLine
                      y={chartData[0]?.memoryTarget || 0}
                      stroke="#10b981"
                      strokeDasharray="5 5"
                      label={{ value: "Target", position: "right", fontSize: 12 }}
                    />
                    <ReferenceArea
                      y1={90}
                      y2={100}
                      fill="#ef4444"
                      fillOpacity={0.1}
                      label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }}
                    />
                    <ReferenceArea
                      y1={80}
                      y2={90}
                      fill="#f59e0b"
                      fillOpacity={0.1}
                      label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }}
                    />
                    <Area
                      type="monotone"
                      dataKey="memoryCurrent"
                      name="Memória Atual"
                      stroke="#8b5cf6"
                      strokeWidth={2}
                      fill="url(#memoryGradient)"
                      unit="%"
                    />
                    <Line
                      type="monotone"
                      dataKey="memoryTarget"
                      name="Memória Target"
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      dot={false}
                      unit="%"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </TabsContent>

            {/* Replicas Timeline */}
            <TabsContent value="replicas" className="space-y-6">
              <div className="border rounded-lg p-4 bg-card">
                <h3 className="text-sm font-semibold mb-4 flex items-center gap-2">
                  <Users className="h-4 w-4" />
                  Réplicas ao longo do tempo
                </h3>
                <ResponsiveContainer width="100%" height={300}>
                  <LineChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 12 }}
                      className="text-xs"
                    />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "Réplicas", angle: -90, position: "insideLeft", fontSize: 12 }}
                      allowDecimals={false}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    <ReferenceLine
                      y={chartData[0]?.replicasMin || 0}
                      stroke="#f59e0b"
                      strokeDasharray="5 5"
                      label={{ value: "Min", position: "right", fontSize: 12 }}
                    />
                    <ReferenceLine
                      y={chartData[0]?.replicasMax || 0}
                      stroke="#ef4444"
                      strokeDasharray="5 5"
                      label={{ value: "Max", position: "right", fontSize: 12 }}
                    />
                    <Line
                      type="stepAfter"
                      dataKey="replicasCurrent"
                      name="Réplicas Atuais"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      dot={{ r: 4 }}
                      unit=""
                    />
                    <Line
                      type="stepAfter"
                      dataKey="replicasDesired"
                      name="Réplicas Desejadas"
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      dot={{ r: 3 }}
                      unit=""
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </TabsContent>
          </Tabs>
        )}
      </CardContent>
    </Card>
  );
}
