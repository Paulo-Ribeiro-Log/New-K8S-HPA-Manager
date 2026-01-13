import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { BarChart3, Brain, Clock, Zap, History, RefreshCw } from "lucide-react";
import type { AIStats } from "@/types/ai";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Legend,
} from "recharts";

interface AIStatsCardProps {
  stats: AIStats | null;
  isLoading: boolean;
  onRefresh: () => void;
}

const COLORS = ["#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6"];

export function AIStatsCard({ stats, isLoading, onRefresh }: AIStatsCardProps) {
  // Empty state - mostrar quando não há dados
  if (!stats || (stats.total_analyses ?? 0) === 0) {
    return (
      <Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60">
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="p-3 bg-gradient-to-r from-purple-500 to-indigo-600 rounded-xl shadow-lg">
              <BarChart3 className="w-6 h-6 text-white" />
            </div>
            <div className="flex-1">
              <CardTitle>Estatísticas de Uso</CardTitle>
              <CardDescription>Análises AI realizadas</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <Brain className="h-12 w-12 text-muted-foreground/50 mb-4" />
            <p className="text-sm text-muted-foreground">
              Nenhuma análise AI foi realizada ainda
            </p>
            <p className="text-xs text-muted-foreground mt-2">
              Vá para a aba Pods e clique em "Analisar com AI" para começar
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  // Safe defaults for all stats fields
  const totalAnalyses = stats.total_analyses ?? 0;
  const avgResponseTime = stats.avg_response_time ?? 0;
  const totalTokensUsed = stats.total_tokens_used ?? 0;
  const lastAnalysisAt = stats.last_analysis_at;

  // Format relative time
  const getRelativeTime = (timestamp?: string) => {
    if (!timestamp) return "Nunca";
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) return "Agora";
    if (diffMins < 60) return `${diffMins}min atrás`;
    if (diffHours < 24) return `${diffHours}h atrás`;
    return `${diffDays}d atrás`;
  };

  // Prepare data for charts
  const resourceData = Object.entries(stats.by_resource_type || {}).map(([type, count]) => ({
    type,
    count,
  }));

  const providerData = Object.entries(stats.by_provider || {}).map(([provider, count]) => ({
    provider,
    count,
  }));

  return (
    <Card className="bg-white/80 dark:bg-slate-800/80 backdrop-blur-sm border border-slate-200/60">
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="p-3 bg-gradient-to-r from-purple-500 to-indigo-600 rounded-xl shadow-lg">
            <BarChart3 className="w-6 h-6 text-white" />
          </div>
          <div className="flex-1">
            <CardTitle>Estatísticas de Uso</CardTitle>
            <CardDescription>Análises AI realizadas</CardDescription>
          </div>
          <Button
            variant="outline"
            size="icon"
            onClick={onRefresh}
            disabled={isLoading}
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {/* Grid 2x2 de métricas */}
        <div className="grid grid-cols-2 gap-4 mb-6">
          <MetricBox
            label="Total Análises"
            value={totalAnalyses.toString()}
            icon={Brain}
            color="text-blue-500"
          />
          <MetricBox
            label="Tempo Médio"
            value={`${avgResponseTime.toFixed(1)}s`}
            icon={Clock}
            color="text-green-500"
          />
          <MetricBox
            label="Tokens Usados"
            value={formatNumber(totalTokensUsed)}
            icon={Zap}
            color="text-yellow-500"
          />
          <MetricBox
            label="Último Uso"
            value={getRelativeTime(lastAnalysisAt)}
            icon={History}
            color="text-purple-500"
          />
        </div>

        {/* Gráficos lado a lado */}
        {resourceData.length > 0 || providerData.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            {/* Gráfico de análises por recurso */}
            {resourceData.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold mb-3">Análises por Recurso</h4>
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart data={resourceData}>
                    <XAxis dataKey="type" tick={{ fontSize: 12 }} />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: "hsl(var(--popover))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: "8px",
                        boxShadow: "0 4px 6px -1px rgb(0 0 0 / 0.1)",
                      }}
                      itemStyle={{
                        color: "hsl(var(--popover-foreground))",
                      }}
                      labelStyle={{
                        color: "hsl(var(--popover-foreground))",
                      }}
                      cursor={{ fill: "transparent" }}
                    />
                    <Bar dataKey="count" fill="#3b82f6" radius={[8, 8, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}

            {/* Gráfico de distribuição por provider */}
            {providerData.length > 0 && (
              <div>
                <h4 className="text-sm font-semibold mb-3">Distribuição por Provider</h4>
                <ResponsiveContainer width="100%" height={200}>
                  <PieChart>
                    <Pie
                      data={providerData}
                      dataKey="count"
                      nameKey="provider"
                      cx="50%"
                      cy="50%"
                      outerRadius={60}
                      label={(entry) => `${entry.provider}: ${entry.count}`}
                      labelLine={false}
                      isAnimationActive={false}
                    >
                      {providerData.map((_, index) => (
                        <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip
                      contentStyle={{
                        backgroundColor: "hsl(var(--popover))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: "8px",
                        boxShadow: "0 4px 6px -1px rgb(0 0 0 / 0.1)",
                      }}
                      itemStyle={{
                        color: "hsl(var(--popover-foreground))",
                      }}
                      labelStyle={{
                        color: "hsl(var(--popover-foreground))",
                      }}
                      cursor={false}
                    />
                    <Legend
                      wrapperStyle={{ fontSize: "12px" }}
                      iconType="circle"
                    />
                  </PieChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

// MetricBox component
interface MetricBoxProps {
  label: string;
  value: string;
  icon: React.ComponentType<{ className?: string }>;
  color: string;
}

function MetricBox({ label, value, icon: Icon, color }: MetricBoxProps) {
  return (
    <div className="rounded-lg border bg-card p-3">
      <div className="flex items-center gap-2 mb-1">
        <Icon className={`h-4 w-4 ${color}`} />
        <span className="text-xs text-muted-foreground">{label}</span>
      </div>
      <div className="text-2xl font-bold">{value}</div>
    </div>
  );
}

// Format number helper
function formatNumber(num: number): string {
  if (num >= 1000000) {
    return `${(num / 1000000).toFixed(1)}M`;
  }
  if (num >= 1000) {
    return `${(num / 1000).toFixed(1)}K`;
  }
  return num.toString();
}
