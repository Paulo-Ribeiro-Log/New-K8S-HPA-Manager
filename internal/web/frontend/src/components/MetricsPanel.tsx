// MetricsPanel - Painel de visualização profissional de métricas

import { useState, useMemo, useEffect } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
  Edit,
  Bell,
  Maximize2,
  X,
  ZoomIn,
  ZoomOut,
  Minimize2,
} from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useHPASpecificAlertsWithDuration } from "@/hooks/useAlerts";
import { AlertsDialog } from "@/components/AlertsDialog";
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  ComposedChart,
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
  onEditHPA?: () => void;
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

interface LatencySeriesStats {
  current: number | null;
  previous: number | null;
  timestamp: string | null;
}

interface LatencyStats {
  p95: LatencySeriesStats;
  p99: LatencySeriesStats;
}

interface ChartPoint {
  timestamp: number;
  time: string;
  cpuCurrent: number;
  cpuTarget?: number;
  cpuRequest: number | null;
  cpuLimit: number | null;
  cpuYesterday: number | null;
  memoryCurrent: number;
  memoryTarget?: number;
  memoryRequest?: string | null;
  memoryLimit?: string | null;
  memoryYesterday: number | null;
  replicasCurrent: number;
  replicasDesired: number;
  replicasMin: number;
  replicasMax: number;
  replicasYesterday: number | null;
  latencyP95: number | null;
  latencyP99: number | null;
  requestRate: number | null;
  errorRate: number | null;
}

export function MetricsPanel({
  cluster,
  namespace,
  hpaName,
  className = "",
  onEditHPA,
}: MetricsPanelProps) {
  const [duration, setDuration] = useState<string>("1h");
  const [autoRefreshInterval, setAutoRefreshInterval] = useState<number>(1); // 1 minuto
  const [latencyView, setLatencyView] = useState<"p95" | "p99">("p95");
  const [alertsDialogOpen, setAlertsDialogOpen] = useState(false);
  const [expandedChart, setExpandedChart] = useState<"cpu" | "memory" | "replicas" | null>(null);
  const [comparisonDays, setComparisonDays] = useState<number>(1); // D-1, D-2 ou D-3
  const [zoomDomain, setZoomDomain] = useState<{ start: number; end: number } | null>(null);
  const [selectedArea, setSelectedArea] = useState<{ start: number; end: number } | null>(null);
  const [zoomLevel, setZoomLevel] = useState<number>(1); // 1 = sem zoom, maior = mais zoom
  const [refAreaLeft, setRefAreaLeft] = useState<number | null>(null);
  const [refAreaRight, setRefAreaRight] = useState<number | null>(null);

  // Buscar alertas específicos deste HPA filtrados pelo período selecionado
  const { data: alerts } = useHPASpecificAlertsWithDuration(cluster, namespace, hpaName, duration);
  const alertsCount = alerts?.length || 0;
  const criticalAlertsCount = alerts?.filter(a => a.severity === "critical").length || 0;
  const hasAlerts = alertsCount > 0;
  const { metrics, loading, error, refetch } = useHPAMetrics(
    cluster,
    namespace,
    hpaName,
    duration,
    comparisonDays
  );

  // Auto-refresh com intervalo configurável
  useEffect(() => {
    if (autoRefreshInterval === 0) return; // Auto-refresh desabilitado

    const intervalId = setInterval(() => {
      console.log(`[MetricsPanel] Auto-refresh (${autoRefreshInterval}min)`);
      refetch();
    }, autoRefreshInterval * 60 * 1000); // Converter minutos para ms

    return () => clearInterval(intervalId);
  }, [autoRefreshInterval, refetch]);

  // Helper para converter CPU (millicores)
  const parseCpuValue = (value: string): number | null => {
    if (!value) return null;
    const trimmed = value.trim();

    if (trimmed.endsWith('m')) {
      return parseInt(trimmed.slice(0, -1), 10);
    }

    const numeric = parseFloat(trimmed);
    if (Number.isNaN(numeric)) return null;

    // Valores sem sufixo são expressos em cores → converter para millicores
    return numeric * 1000;
  };

  // Helper para converter valores de memória (ex: "2Gi") para MiB
  const parseResourceValue = (value: string): number | null => {
    if (!value) return null;

    if (value.endsWith('m')) {
      return parseInt(value.slice(0, -1), 10);
    }

    if (value.endsWith('Mi')) {
      return parseInt(value.slice(0, -2), 10);
    }
    if (value.endsWith('Gi')) {
      return parseInt(value.slice(0, -2), 10) * 1024;
    }

    // Fallback: tentar parsear como número puro
    return parseFloat(value) || null;
  };

  // Encontrar snapshot com Request/Limit (para usar nas ReferenceLine)
  const snapshotWithResources = useMemo(() => {
    if (!metrics?.snapshots || metrics.snapshots.length === 0) {
      console.log('[MetricsPanel] Nenhum snapshot disponível');
      return null;
    }

    // Encontrar snapshot que tenha TODOS os 4 campos não-vazios (Request E Limit para CPU e Memory)
    const found = metrics.snapshots.find(s =>
      (s.cpu_request && s.cpu_request.trim() !== '') &&
      (s.cpu_limit && s.cpu_limit.trim() !== '') &&
      (s.memory_request && s.memory_request.trim() !== '') &&
      (s.memory_limit && s.memory_limit.trim() !== '')
    ) || metrics.snapshots[0];

    console.log('[MetricsPanel] Snapshot com recursos:', {
      cpu_request: found?.cpu_request,
      cpu_limit: found?.cpu_limit,
      memory_request: found?.memory_request,
      memory_limit: found?.memory_limit
    });

    return found;
  }, [metrics]);

  // Preparar dados para os gráficos
  const chartData = useMemo<ChartPoint[]>(() => {
    if (!metrics || !metrics.snapshots || metrics.snapshots.length === 0) {
      return [];
    }

    console.log('🚀 [D-1] NOVA LÓGICA - Iniciando mapeamento por índice direto', {
      snapshotsCount: metrics.snapshots.length,
      snapshotsYesterdayCount: metrics.snapshots_yesterday?.length || 0,
      willMapYesterday: !!metrics.snapshots_yesterday
    });

    // Mapeamento D-1: ambos arrays têm mesmo tamanho e cadência, mapear por índice direto
    const result = metrics.snapshots.map((snapshot: HPASnapshot, index: number) => {
      // Dados D-1 no mesmo índice (mesma posição relativa na janela de tempo)
      const yesterdaySnapshot = metrics.snapshots_yesterday?.[index];

      // Extrair valores de Request/Limit
      const cpuRequest = snapshot.cpu_request ? parseCpuValue(snapshot.cpu_request) : null;
      const cpuLimit = snapshot.cpu_limit ? parseCpuValue(snapshot.cpu_limit) : null;

      return {
        timestamp: new Date(snapshot.timestamp).getTime(),
        time: format(new Date(snapshot.timestamp), "HH:mm:ss", { locale: ptBR }),
        cpuCurrent: snapshot.cpu_current,
        cpuTarget: snapshot.cpu_target,
        cpuRequest: cpuRequest,
        cpuLimit: cpuLimit,
        cpuYesterday: yesterdaySnapshot?.cpu_current ?? null,
        memoryCurrent: snapshot.memory_current,
        memoryTarget: snapshot.memory_target,
        memoryRequest: snapshot.memory_request,
        memoryLimit: snapshot.memory_limit,
        memoryYesterday: yesterdaySnapshot?.memory_current ?? null,
        replicasCurrent: snapshot.replicas_current,
        replicasDesired: snapshot.replicas_desired,
        replicasMin: snapshot.replicas_min,
        replicasMax: snapshot.replicas_max,
        replicasYesterday: yesterdaySnapshot?.replicas_current ?? null,
        latencyP95: snapshot.p95_latency ?? null,
        latencyP99: snapshot.p99_latency ?? null,
        requestRate: snapshot.request_rate ?? null,
        errorRate: snapshot.error_rate ?? null,
      };
    });

    // Log de debug para verificar mapeamento D-1
    const hasYesterdayData = result.some(d => d.cpuYesterday !== null);
    if (metrics.snapshots_yesterday && metrics.snapshots_yesterday.length > 0) {
      console.log('[D-1] Mapeamento concluído:', {
        totalPoints: result.length,
        yesterdayPointsWithData: result.filter(d => d.cpuYesterday !== null).length,
        firstPoint: {
          today: result[0]?.timestamp ? new Date(result[0].timestamp).toISOString() : 'N/A',
          todayCPU: result[0]?.cpuCurrent,
          yesterdayCPU: result[0]?.cpuYesterday
        },
        lastPoint: {
          today: result[result.length-1]?.timestamp ? new Date(result[result.length-1].timestamp).toISOString() : 'N/A',
          todayCPU: result[result.length-1]?.cpuCurrent,
          yesterdayCPU: result[result.length-1]?.cpuYesterday
        }
      });
    }

    return result;
  }, [metrics]);

  // Descobrir limites de réplicas usando o ponto mais recente válido (ignora snapshots antigos zerados)
  const replicaBounds = useMemo(() => {
    if (chartData.length === 0) {
      return { min: 0, max: 0 };
    }

    const resolveBound = (key: "replicasMin" | "replicasMax") => {
      // Procura do fim para o começo por um valor positivo (HPA não aceita <= 0)
      for (let i = chartData.length - 1; i >= 0; i--) {
        const value = chartData[i][key];
        if (typeof value === "number" && value > 0) {
          return value;
        }
      }

      // Fallback: usa último snapshot mesmo que seja 0, para não quebrar comportamento anterior
      const lastValue = chartData[chartData.length - 1]?.[key];
      return typeof lastValue === "number" ? lastValue : 0;
    };

    return {
      min: resolveBound("replicasMin"),
      max: resolveBound("replicasMax"),
    };
  }, [chartData]);

  // Calcular estatísticas
  const cpuStats = useMemo((): MetricStats => {
    if (chartData.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const values = chartData.map(d => d.cpuCurrent).filter(v => typeof v === 'number' && !isNaN(v));
    if (values.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const current = values[values.length - 1] || 0;
    const average = values.reduce((a, b) => a + b, 0) / values.length;
    const peak = Math.max(...values);
    const min = Math.min(...values);
    const sorted = [...values].sort((a, b) => a - b);
    const p95 = sorted[Math.floor(sorted.length * 0.95)] || 0;

    // Calcular tendência (últimos 5 vs primeiros 5 pontos)
    const recentAvg = values.slice(-5).reduce((a, b) => a + b, 0) / Math.max(values.slice(-5).length, 1);
    const oldAvg = values.slice(0, 5).reduce((a, b) => a + b, 0) / Math.max(values.slice(0, 5).length, 1);
    const trendPercent = oldAvg !== 0 ? ((recentAvg - oldAvg) / oldAvg) * 100 : 0;
    const trend = Math.abs(trendPercent) < 5 ? "stable" : trendPercent > 0 ? "up" : "down";

    return { current, average, peak, min, p95, trend, trendPercent };
  }, [chartData]);

  const memoryStats = useMemo((): MetricStats => {
    if (chartData.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const values = chartData.map(d => d.memoryCurrent).filter(v => typeof v === 'number' && !isNaN(v));
    if (values.length === 0) {
      return { current: 0, average: 0, peak: 0, min: 0, p95: 0, trend: "stable", trendPercent: 0 };
    }

    const current = values[values.length - 1] || 0;
    const average = values.reduce((a, b) => a + b, 0) / values.length;
    const peak = Math.max(...values);
    const min = Math.min(...values);
    const sorted = [...values].sort((a, b) => a - b);
    const p95 = sorted[Math.floor(sorted.length * 0.95)] || 0;

    const recentAvg = values.slice(-5).reduce((a, b) => a + b, 0) / Math.max(values.slice(-5).length, 1);
    const oldAvg = values.slice(0, 5).reduce((a, b) => a + b, 0) / Math.max(values.slice(0, 5).length, 1);
    const trendPercent = oldAvg !== 0 ? ((recentAvg - oldAvg) / oldAvg) * 100 : 0;
    const trend = Math.abs(trendPercent) < 5 ? "stable" : trendPercent > 0 ? "up" : "down";

    return { current, average, peak, min, p95, trend, trendPercent };
  }, [chartData]);

  const latencyStats = useMemo<LatencyStats>(() => {
    const empty: LatencySeriesStats = { current: null, previous: null, timestamp: null };
    if (chartData.length === 0) {
      return { p95: empty, p99: empty };
    }

    const buildStats = (key: "latencyP95" | "latencyP99"): LatencySeriesStats => {
      const entries = chartData
        .filter(point => typeof point[key] === "number" && point[key] !== null)
        .map(point => ({ value: point[key] as number, time: point.time }));

      if (entries.length === 0) {
        return { current: null, previous: null, timestamp: null };
      }

      const current = entries[entries.length - 1];
      const previous = entries.length > 1 ? entries[entries.length - 2] : null;

      return {
        current: current.value,
        previous: previous ? previous.value : null,
        timestamp: current.time,
      };
    };

    return {
      p95: buildStats("latencyP95"),
      p99: buildStats("latencyP99"),
    };
  }, [chartData]);

  // Extrair targets para usar nas ReferenceLine (valores fixos, não mudam ao longo do tempo)
  // Busca em QUALQUER snapshot até encontrar valor válido (não apenas primeiro)
  const cpuTarget = useMemo(() => {
    if (!metrics?.snapshots || metrics.snapshots.length === 0) return 0;

    // Buscar primeiro snapshot com cpu_target válido (não-zero)
    for (const snap of metrics.snapshots) {
      if (snap.cpu_target && snap.cpu_target > 0) {
        return snap.cpu_target;
      }
    }
    return 0;
  }, [metrics]);

  const memoryTarget = useMemo(() => {
    if (!metrics?.snapshots || metrics.snapshots.length === 0) return 0;

    // Buscar primeiro snapshot com memory_target válido (não-zero)
    for (const snap of metrics.snapshots) {
      if (snap.memory_target && snap.memory_target > 0) {
        return snap.memory_target;
      }
    }
    return 0;
  }, [metrics]);

  const formatCpuUsage = (millicores: number): string => {
    if (!Number.isFinite(millicores)) {
      return "--";
    }

    const absValue = Math.abs(millicores);

    // >= 1000 millicores = mostrar em vCores com 2 casas decimais
    if (absValue >= 1000) {
      const vCores = millicores / 1000;
      return `${vCores.toFixed(2)} vCores`;
    }

    // >= 1 millicore = mostrar em millicores com 2 casas decimais
    if (absValue >= 1) {
      return `${millicores.toFixed(2)}m`;
    }

    // < 1 millicore = mostrar em microcores (µ) com 2 casas decimais
    const microcores = millicores * 1000;
    return `${microcores.toFixed(2)}µ`;
  };

  // Helpers de exibição dos cards
  const getCpuDisplayValues = (percentValue: number, limitValue?: string) => {
    const sanitizedPercent = Number.isFinite(percentValue) ? percentValue : 0;

    if (!limitValue) {
      return {
        primary: `${sanitizedPercent.toFixed(2)}%`,
        percentLabel: `${sanitizedPercent.toFixed(2)}%`,
        hasLimit: false,
      };
    }

    const parsedLimit = parseCpuValue(limitValue);
    if (parsedLimit === null || parsedLimit === 0) {
      return {
        primary: `${sanitizedPercent.toFixed(2)}%`,
        percentLabel: `${sanitizedPercent.toFixed(2)}%`,
        hasLimit: false,
      };
    }

    const usageMillicores = (sanitizedPercent / 100) * parsedLimit;

    return {
      primary: formatCpuUsage(usageMillicores),
      percentLabel: `${sanitizedPercent.toFixed(2)}%`,
      hasLimit: true,
    };
  };

  // Componente de Estatística - COMPACTO
  const StatCard = ({
    icon: Icon,
    label,
    percentValue,
    limitValue,
  }: {
    icon: any;
    label: string;
    percentValue: number;
    limitValue?: string;
  }) => {
    const { primary, percentLabel, hasLimit } = getCpuDisplayValues(percentValue, limitValue);

    return (
      <div className="px-2.5 py-2 rounded-lg border bg-blue-950/30 border-blue-800/40">
        <div className="flex items-center gap-1.5 mb-1">
          <Icon className="h-3 w-3 flex-shrink-0" />
          <span className="text-[11px] font-semibold leading-none">{label}</span>
        </div>
        <div className="text-base font-bold leading-none mb-0.5">{primary}</div>
        <div className="text-[10px] text-muted-foreground leading-none">
          {percentLabel} {hasLimit && limitValue ? `de ${limitValue}` : ''}
        </div>
      </div>
    );
  };

  const formatLatencyValue = (value: number | null) => {
    if (value === null || Number.isNaN(value)) {
      return "--";
    }

    // >= 1000ms = mostrar em segundos com 2 casas decimais
    if (value >= 1000) {
      return `${(value / 1000).toFixed(2)} s`;
    }

    // >= 1ms = mostrar em milissegundos com 2 casas decimais
    if (value >= 1) {
      return `${value.toFixed(2)} ms`;
    }

    // < 1ms = mostrar em microssegundos (µs) com 2 casas decimais
    return `${Math.max(value * 1000, 0).toFixed(2)} µs`;
  };

  const LatencyCard = ({
    icon: Icon,
    stats,
    percentile,
    onPercentileChange,
  }: {
    icon: any;
    stats: LatencyStats;
    percentile: "p95" | "p99";
    onPercentileChange: (value: "p95" | "p99") => void;
  }) => {
    const current =
      percentile === "p95" ? stats.p95.current : stats.p99.current;
    const previous =
      percentile === "p95" ? stats.p95.previous : stats.p99.previous;
    const timestamp =
      percentile === "p95" ? stats.p95.timestamp : stats.p99.timestamp;

    const percentChange =
      previous && previous !== 0 && current !== null
        ? ((current - previous) / previous) * 100
        : null;

    const percentChangeLabel =
      percentChange === null
        ? "--"
        : `${percentChange > 0 ? "+" : ""}${percentChange.toFixed(1)}%`;

    const percentChangeColor =
      percentChange === null
        ? "text-muted-foreground"
        : percentChange > 0
        ? "text-red-600"
        : "text-green-600";

    return (
      <div className="px-2.5 py-2 rounded-lg border bg-blue-950/30 border-blue-800/40">
        <div className="flex items-center justify-between mb-1">
          <div className="flex items-center gap-1.5">
            <Icon className="h-3 w-3 flex-shrink-0" />
            <span className="text-[11px] font-semibold leading-none">
              Latência {percentile.toUpperCase()}
            </span>
          </div>
          <div className="inline-flex rounded-full border px-1 py-0.5 bg-background/60">
            {(["p95", "p99"] as const).map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => onPercentileChange(option)}
                className={`px-1.5 py-0.5 text-[10px] font-semibold rounded-full transition-colors ${
                  percentile === option
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground"
                }`}
              >
                {option.toUpperCase()}
              </button>
            ))}
          </div>
        </div>
        <div className="text-base font-bold leading-none mb-0.5">
          {formatLatencyValue(current)}
        </div>
        <div className="flex items-center justify-between text-[10px] leading-none">
          <span className="text-muted-foreground">
            {timestamp ? timestamp : "Sem dados"}
          </span>
          <span className={`font-semibold ${percentChangeColor}`}>
            {percentChangeLabel}
          </span>
        </div>
      </div>
    );
  };

  // Helper para converter percentual em valor absoluto baseado no limit
  const percentToAbsolute = (percent: number, limit: string, isCpu: boolean): string => {
    if (!limit) return '';

    const limitValue = isCpu ? parseCpuValue(limit) : parseResourceValue(limit);
    if (limitValue === null) return '';

    const absolute = (percent / 100) * limitValue;

    if (isCpu) {
      return formatCpuUsage(absolute);
    } else {
      // Memory: retorna em MiB ou GiB
      if (absolute >= 1024) {
        return `${(absolute / 1024).toFixed(2)}Gi`;
      }
      return `${absolute.toFixed(0)}Mi`;
    }
  };

  // Helper para converter valor absoluto em percentual do limit (para linhas de referência)
  const absoluteToPercent = (absoluteValue: string, limit: string, isCpu: boolean): number | null => {
    if (!absoluteValue || !limit) return null;

    const absVal = isCpu ? parseCpuValue(absoluteValue) : parseResourceValue(absoluteValue);
    const limitVal = isCpu ? parseCpuValue(limit) : parseResourceValue(limit);

    if (absVal === null || limitVal === null || limitVal === 0) return null;

    const percent = (absVal / limitVal) * 100;
    console.log(`[absoluteToPercent] ${absoluteValue} / ${limit} = ${absVal} / ${limitVal} = ${percent}%`);
    return percent;
  };

  // Custom Tooltip com valores absolutos completos
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (!active || !payload || !payload.length) return null;

    // Determinar se é gráfico de CPU ou Memory pelo primeiro payload
    const firstDataKey = payload[0]?.dataKey || '';
    const isCpuChart = firstDataKey.includes('cpu');

    return (
      <div className="bg-background border rounded-lg shadow-lg p-3 space-y-1.5">
        <p className="text-sm font-semibold mb-2">{label}</p>
        {payload.map((entry: any, index: number) => {
          const dataKey = entry.dataKey || '';
          const name = entry.name || '';

          // Calcular valor absoluto
          let limit = '';
          let absoluteValue = '';

          if (isCpuChart && snapshotWithResources?.cpu_limit) {
            limit = snapshotWithResources.cpu_limit;
            absoluteValue = percentToAbsolute(entry.value, limit, true);
          } else if (!isCpuChart && snapshotWithResources?.memory_limit) {
            limit = snapshotWithResources.memory_limit;
            absoluteValue = percentToAbsolute(entry.value, limit, false);
          }

          return (
            <div key={index} className="flex items-center justify-between gap-3 text-xs">
              <div className="flex items-center gap-2 min-w-0">
                <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: entry.color }} />
                <span className="text-muted-foreground truncate">{entry.name}</span>
              </div>
              <span className="font-semibold whitespace-nowrap">
                {absoluteValue && limit
                  ? `${absoluteValue} (${entry.value.toFixed(1)}% de ${limit})`
                  : `${entry.value.toFixed(1)}%`
                }
              </span>
            </div>
          );
        })}

        {/* Adicionar Request, Limit, Target e D-1 no tooltip */}
        {snapshotWithResources && (
          <>
            <div className="border-t pt-2 mt-2 space-y-1">
              {/* CPU Target */}
              {isCpuChart && snapshotWithResources.cpu_target && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#10b981' }} />
                    <span className="text-muted-foreground">CPU Target:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.cpu_target}%</span>
                </div>
              )}
              {/* CPU Request */}
              {isCpuChart && snapshotWithResources.cpu_request && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#f97316' }} />
                    <span className="text-muted-foreground">CPU Request:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.cpu_request}</span>
                </div>
              )}
              {/* CPU Limit */}
              {isCpuChart && snapshotWithResources.cpu_limit && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#ef4444' }} />
                    <span className="text-muted-foreground">CPU Limit:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.cpu_limit}</span>
                </div>
              )}
              {/* Memory Target */}
              {!isCpuChart && snapshotWithResources.memory_target && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#10b981' }} />
                    <span className="text-muted-foreground">Memory Target:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.memory_target}%</span>
                </div>
              )}
              {/* Memory Request */}
              {!isCpuChart && snapshotWithResources.memory_request && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#f97316' }} />
                    <span className="text-muted-foreground">Memory Request:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.memory_request}</span>
                </div>
              )}
              {/* Memory Limit */}
              {!isCpuChart && snapshotWithResources.memory_limit && (
                <div className="flex items-center justify-between gap-3 text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: '#ef4444' }} />
                    <span className="text-muted-foreground">Memory Limit:</span>
                  </div>
                  <span className="font-semibold">{snapshotWithResources.memory_limit}</span>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    );
  };

  // Funções de zoom para o gráfico expandido
  const getZoomedData = () => {
    if (!zoomDomain || chartData.length === 0) return chartData;
    
    return chartData.filter(d => 
      d.timestamp >= zoomDomain.start && d.timestamp <= zoomDomain.end
    );
  };

  // Aplicar zoom baseado na área selecionada e no nível de zoom
  const applyZoom = (level: number, area: { start: number; end: number } | null = null) => {
    if (chartData.length === 0) return;

    // Se temos uma área específica (seleção do usuário), usa ela
    if (area) {
      const center = (area.start + area.end) / 2;
      const baseRange = area.end - area.start;
      const zoomedRange = baseRange / level;

      setZoomDomain({
        start: center - zoomedRange / 2,
        end: center + zoomedRange / 2
      });
      return;
    }

    // Senão, aplica zoom na região atual ou no gráfico completo
    const currentDomain = zoomDomain || {
      start: chartData[0].timestamp,
      end: chartData[chartData.length - 1].timestamp
    };

    const center = (currentDomain.start + currentDomain.end) / 2;
    const baseRange = currentDomain.end - currentDomain.start;
    const zoomedRange = baseRange / level;

    setZoomDomain({
      start: center - zoomedRange / 2,
      end: center + zoomedRange / 2
    });
  };

  const handleZoomIn = () => {
    if (chartData.length === 0) return;

    const currentDomain = zoomDomain || {
      start: chartData[0].timestamp,
      end: chartData[chartData.length - 1].timestamp
    };

    const center = (currentDomain.start + currentDomain.end) / 2;
    const currentRange = currentDomain.end - currentDomain.start;
    const newRange = currentRange * 0.75; // Reduz 25% (zoom in)

    setZoomDomain({
      start: center - newRange / 2,
      end: center + newRange / 2
    });
  };

  const handleZoomOut = () => {
    if (chartData.length === 0) return;

    if (!zoomDomain) {
      // Já está no zoom completo
      return;
    }

    const center = (zoomDomain.start + zoomDomain.end) / 2;
    const currentRange = zoomDomain.end - zoomDomain.start;
    const newRange = currentRange * 1.33; // Aumenta 33% (zoom out)

    // Verificar se o novo range ultrapassa os limites dos dados
    const fullStart = chartData[0].timestamp;
    const fullEnd = chartData[chartData.length - 1].timestamp;
    const fullRange = fullEnd - fullStart;

    if (newRange >= fullRange) {
      // Reset para visualização completa
      setZoomDomain(null);
      setSelectedArea(null);
      setZoomLevel(1);
    } else {
      setZoomDomain({
        start: Math.max(fullStart, center - newRange / 2),
        end: Math.min(fullEnd, center + newRange / 2)
      });
    }
  };

  const handleZoomReset = () => {
    setZoomDomain(null);
    setSelectedArea(null);
    setZoomLevel(1);
    setRefAreaLeft(null);
    setRefAreaRight(null);
  };

  // Funções para seleção de área (como no Grafana)
  const handleMouseDown = (e: any) => {
    if (e && e.activeTooltipIndex !== undefined) {
      setRefAreaLeft(e.activeTooltipIndex);
      setRefAreaRight(e.activeTooltipIndex);
    }
  };

  const handleMouseMove = (e: any) => {
    if (refAreaLeft !== null && e && e.activeTooltipIndex !== undefined) {
      setRefAreaRight(e.activeTooltipIndex);
    }
  };

  const handleMouseUp = () => {
    if (refAreaLeft !== null && refAreaRight !== null && refAreaLeft !== refAreaRight) {
      // Usar índices diretos dos dados sendo exibidos
      const displayedData = zoomDomain ? zoomedData : chartData;

      if (refAreaLeft >= 0 && refAreaRight >= 0 &&
          refAreaLeft < displayedData.length && refAreaRight < displayedData.length) {

        const startIndex = Math.min(refAreaLeft, refAreaRight);
        const endIndex = Math.max(refAreaLeft, refAreaRight);

        const start = displayedData[startIndex].timestamp;
        const end = displayedData[endIndex].timestamp;

        // Define a área selecionada e aplica zoom
        setSelectedArea({ start, end });
        setZoomDomain({ start, end });
        setZoomLevel(1);
      }

      // Limpar seleção visual
      setRefAreaLeft(null);
      setRefAreaRight(null);
    } else {
      setRefAreaLeft(null);
      setRefAreaRight(null);
    }
  };

  // Reset zoom quando trocar de gráfico ou fechar modal
  useEffect(() => {
    if (expandedChart === null) {
      setZoomDomain(null);
      setSelectedArea(null);
      setZoomLevel(1);
      setRefAreaLeft(null);
      setRefAreaRight(null);
    }
  }, [expandedChart]);

  const zoomedData = getZoomedData();

  // Helper para converter índices em valores de time para o ReferenceArea
  const getRefAreaValues = () => {
    if (refAreaLeft === null || refAreaRight === null) return null;

    const displayedData = zoomDomain ? zoomedData : chartData;

    if (refAreaLeft >= 0 && refAreaRight >= 0 &&
        refAreaLeft < displayedData.length && refAreaRight < displayedData.length) {
      return {
        x1: displayedData[refAreaLeft].time,
        x2: displayedData[refAreaRight].time
      };
    }

    return null;
  };

  const refAreaValues = getRefAreaValues();

  return (
    <Card className={className}>
      <CardHeader className="pb-4">
        <div className="flex items-start justify-between gap-4">
          {/* Lado esquerdo: Título compacto com breadcrumb inline */}
          <div className="min-w-0 flex-shrink">
            <div className="flex items-center gap-2 mb-1">
              <BarChart3 className="h-4 w-4 flex-shrink-0 text-primary" />
              <h3 className="text-base font-semibold leading-none">Análise de Métricas</h3>
            </div>
            <p className="text-xs text-muted-foreground truncate">
              {cluster} / {namespace} / {hpaName}
            </p>
          </div>
          
          {/* Lado direito: Controles compactos em linha */}
          <div className="flex items-center gap-1.5 flex-shrink-0">
            <Select value={duration} onValueChange={setDuration}>
              <SelectTrigger className="h-8 w-24 text-xs">
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
            <Select value={autoRefreshInterval.toString()} onValueChange={(v) => setAutoRefreshInterval(parseInt(v))}>
              <SelectTrigger className="h-8 w-28 text-xs">
                <SelectValue placeholder="Auto-refresh" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">Off</SelectItem>
                <SelectItem value="1">1 min</SelectItem>
                <SelectItem value="5">5 min</SelectItem>
                <SelectItem value="10">10 min</SelectItem>
                <SelectItem value="15">15 min</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="icon"
              onClick={() => refetch(duration)}
              disabled={loading}
              title="Atualizar métricas"
              className="h-8 w-8"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            </Button>
            {onEditHPA && (
              <Button
                variant="default"
                size="sm"
                onClick={onEditHPA}
                className="gap-1.5 h-8 text-xs"
                title="Editar configurações deste HPA"
              >
                <Edit className="h-3.5 w-3.5" />
                <span className="hidden xl:inline">Editar HPA</span>
                <span className="xl:hidden">Editar</span>
              </Button>
            )}
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

        {loading && !error && chartData.length === 0 && (
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

        {!error && chartData.length > 0 && (
          <div className="space-y-6">
            {/* Estatísticas - Cards de métricas */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-3">
                <StatCard
                  icon={Activity}
                  label="CPU Atual"
                  percentValue={cpuStats.current}
                  limitValue={snapshotWithResources?.cpu_limit}
                />
                <StatCard
                  icon={TrendingUp}
                  label="Média"
                  percentValue={cpuStats.average}
                  limitValue={snapshotWithResources?.cpu_limit}
                />
                <StatCard
                  icon={AlertCircle}
                  label="Pico"
                  percentValue={cpuStats.peak}
                  limitValue={snapshotWithResources?.cpu_limit}
                />
                <StatCard
                  icon={TrendingDown}
                  label="Mínimo"
                  percentValue={cpuStats.min}
                  limitValue={snapshotWithResources?.cpu_limit}
                />
                {/* Card de Latência P95/P99 oculto - não é útil sem dados de latência */}
                {/* <LatencyCard
                  icon={BarChart3}
                  stats={latencyStats}
                  percentile={latencyView}
                  onPercentileChange={setLatencyView}
                /> */}
                {/* Botão de Alertas */}
                <div className="px-2.5 py-2 rounded-lg border bg-blue-950/30 border-blue-800/40">
                  <div className="flex items-center gap-1.5 mb-1">
                    <Bell className="h-3 w-3 flex-shrink-0" />
                    <span className="text-[11px] font-semibold leading-none">Alertas</span>
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Button
                      variant={hasAlerts ? "destructive" : "outline"}
                      size="sm"
                      className={`w-full h-7 text-xs gap-1.5 ${
                        hasAlerts
                          ? criticalAlertsCount > 0
                            ? "bg-red-600 hover:bg-red-700"
                            : "bg-yellow-600 hover:bg-yellow-700"
                          : ""
                      }`}
                      onClick={(e) => {
                        e.stopPropagation();
                        setAlertsDialogOpen(true);
                      }}
                    >
                      {hasAlerts ? (
                        <>
                          <AlertCircle className="h-3 w-3" />
                          <span>{alertsCount} Alerta{alertsCount > 1 ? "s" : ""}</span>
                        </>
                      ) : (
                        <>
                          <Bell className="h-3 w-3" />
                          <span>Sem Alertas</span>
                        </>
                      )}
                    </Button>
                    <div className="text-[10px] text-muted-foreground text-center leading-tight">
                      {hasAlerts ? (
                        criticalAlertsCount > 0 ? (
                          <span className="text-red-400 font-semibold">
                            {criticalAlertsCount} crítico{criticalAlertsCount > 1 ? "s" : ""}
                          </span>
                        ) : (
                          <span className="text-yellow-400">Clique para detalhes</span>
                        )
                      ) : (
                        <span>Tudo OK ✓</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>

            {/* Gráficos - Seção separada */}
            <div className="space-y-6">
              {/* Gráfico de CPU */}
              <div className="border rounded-lg p-4 bg-card group relative">
                <Button
                  variant="ghost"
                  size="icon"
                  className="absolute top-2 right-2 h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity z-10"
                  onClick={() => setExpandedChart("cpu")}
                  title="Expandir gráfico"
                >
                  <Maximize2 className="h-4 w-4" />
                </Button>
                <div className="mb-4 flex items-center justify-between">
                  <h3 className="text-sm font-semibold flex items-center gap-2">
                    <Cpu className="h-4 w-4" />
                    Uso de CPU ao longo do tempo
                  </h3>
                  <div className="flex items-center gap-4 text-xs">
                    {snapshotWithResources?.cpu_request && (
                      <span className="text-orange-600">
                        CPU Request: <strong>{snapshotWithResources.cpu_request}</strong>
                      </span>
                    )}
                    {snapshotWithResources?.cpu_limit && (
                      <span className="text-red-600">
                        CPU Limit: <strong>{snapshotWithResources.cpu_limit}</strong>
                      </span>
                    )}
                  </div>
                </div>
                <ResponsiveContainer width="100%" height={300}>
                  <ComposedChart data={chartData}>
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
                      domain={[0, 150]}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    {/* Linhas tracejadas de Request e Limit (como no Grafana) */}
                    {snapshotWithResources?.cpu_request && (
                    <ReferenceLine
                      y={absoluteToPercent(snapshotWithResources.cpu_request, snapshotWithResources.cpu_limit || snapshotWithResources.cpu_request, true) || 0}
                        stroke="#f97316"
                        strokeDasharray="3 3"
                        label={{ value: `Request: ${snapshotWithResources.cpu_request}`, position: "right", fontSize: 11, fill: "#f97316" }}
                      />
                    )}
                    {snapshotWithResources?.cpu_limit && (
                      <ReferenceLine
                        y={100}
                        stroke="#ef4444"
                        strokeDasharray="3 3"
                        label={{ value: `Limit: ${snapshotWithResources.cpu_limit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
                      />
                    )}
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
                    {/* Linha verde - CPU Target (Reference Line) - PRIMEIRO */}
                    <ReferenceLine
                      y={cpuTarget}
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Target: ${cpuTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
                    />
                    <Area
                      type="monotone"
                      dataKey="cpuCurrent"
                      name="CPU Atual"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      fill="url(#cpuGradient)"
                      unit="%"
                      isAnimationActive={false}
                    />
                    {/* Linha D-N - lembrança sutil no gráfico */}
                    <Line
                      type="monotone"
                      dataKey="cpuYesterday"
                      name={`CPU D-${comparisonDays} (${comparisonDays * 24}h atrás)`}
                      stroke="#c084fc"
                      strokeWidth={1.5}
                      strokeOpacity={0.2}
                      dot={false}
                      unit="%"
                      isAnimationActive={false}
                    />
                  </ComposedChart>
                </ResponsiveContainer>
              </div>

              {/* Memory e Replicas lado a lado - Grid 2 colunas */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Memory Analysis */}
                <div className="border rounded-lg p-4 bg-card group relative">
                <Button
                  variant="ghost"
                  size="icon"
                  className="absolute top-2 right-2 h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity z-10"
                  onClick={() => setExpandedChart("memory")}
                  title="Expandir gráfico"
                >
                  <Maximize2 className="h-4 w-4" />
                </Button>
                <div className="mb-4 flex items-center justify-between">
                  <h3 className="text-sm font-semibold flex items-center gap-2">
                    <MemoryStick className="h-4 w-4" />
                    Uso de Memória ao longo do tempo
                  </h3>
                  <div className="flex items-center gap-4 text-xs">
                    {snapshotWithResources?.memory_request && (
                      <span className="text-orange-600">
                        Memory Request: <strong>{snapshotWithResources.memory_request}</strong>
                      </span>
                    )}
                    {snapshotWithResources?.memory_limit && (
                      <span className="text-red-600">
                        Memory Limit: <strong>{snapshotWithResources.memory_limit}</strong>
                      </span>
                    )}
                  </div>
                </div>
                <ResponsiveContainer width="100%" height={300}>
                  <ComposedChart data={chartData}>
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
                      domain={[0, 150]}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    {/* Linhas tracejadas de Request e Limit (como no Grafana) */}
                    {snapshotWithResources?.memory_request && (
                    <ReferenceLine
                      y={absoluteToPercent(snapshotWithResources.memory_request, snapshotWithResources.memory_limit || snapshotWithResources.memory_request, false) || 0}
                        stroke="#f97316"
                        strokeDasharray="3 3"
                        label={{ value: `Request: ${snapshotWithResources.memory_request}`, position: "right", fontSize: 11, fill: "#f97316" }}
                      />
                    )}
                    {snapshotWithResources?.memory_limit && (
                      <ReferenceLine
                        y={100}
                        stroke="#ef4444"
                        strokeDasharray="3 3"
                        label={{ value: `Limit: ${snapshotWithResources.memory_limit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
                      />
                    )}
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
                    {/* Linha verde - Memory Target (Reference Line) - PRIMEIRO */}
                    <ReferenceLine
                      y={memoryTarget}
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Target: ${memoryTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
                    />
                    <Area
                      type="monotone"
                      dataKey="memoryCurrent"
                      name="Memória Atual"
                      stroke="#8b5cf6"
                      strokeWidth={2}
                      fill="url(#memoryGradient)"
                      unit="%"
                      isAnimationActive={false}
                    />
                    {/* Linha D-N - lembrança sutil no gráfico */}
                    <Line
                      type="monotone"
                      dataKey="memoryYesterday"
                      name={`Memória D-${comparisonDays} (${comparisonDays * 24}h atrás)`}
                      stroke="#c084fc"
                      strokeWidth={1.5}
                      strokeOpacity={0.3}
                      dot={false}
                      unit="%"
                      isAnimationActive={false}
                    />
                  </ComposedChart>
                </ResponsiveContainer>

                {/* Labels de estatísticas abaixo do gráfico */}
                <div className="mt-4 pt-3 border-t flex items-center justify-around text-xs">
                  <div className="text-center">
                    <div className="text-muted-foreground">Atual</div>
                    <div className="font-semibold text-sm">{memoryStats.current.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Média</div>
                    <div className="font-semibold text-sm">{memoryStats.average.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Pico</div>
                    <div className="font-semibold text-sm text-orange-600">{memoryStats.peak.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Mínimo</div>
                    <div className="font-semibold text-sm">{memoryStats.min.toFixed(1)}%</div>
                  </div>
                  {/* Card P95 oculto - não é útil para análise de memória */}
                  {/* <div className="text-center">
                    <div className="text-muted-foreground">P95</div>
                    <div className="font-semibold text-sm">{memoryStats.p95.toFixed(1)}%</div>
                  </div> */}
                </div>
              </div>

                {/* Replicas Analysis */}
                <div className="border rounded-lg p-4 bg-card group relative">
                <Button
                  variant="ghost"
                  size="icon"
                  className="absolute top-2 right-2 h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity z-10"
                  onClick={() => setExpandedChart("replicas")}
                  title="Expandir gráfico"
                >
                  <Maximize2 className="h-4 w-4" />
                </Button>
                <h3 className="text-sm font-semibold mb-4 flex items-center gap-2">
                  <Users className="h-4 w-4" />
                  Réplicas ao longo do tempo
                </h3>
                <ResponsiveContainer width="100%" height={300}>
                  <LineChart data={chartData} isAnimationActive={false}>
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
                      domain={[0, (dataMax: number) => {
                        const maxReplicas = replicaBounds.max || dataMax;
                        return Math.max(dataMax, maxReplicas * 1.5); // 50% margem acima (replicas_max + 50%)
                      }]}
                    />
                    <Tooltip content={(props) => {
                      if (!props.active || !props.payload || !props.payload.length) return null;
                      return (
                        <div className="bg-background border rounded-lg shadow-lg p-3 space-y-1.5">
                          <p className="text-sm font-semibold mb-2">{props.label}</p>
                          {props.payload.map((entry: any, index: number) => (
                            <div key={index} className="flex items-center justify-between gap-3 text-xs">
                              <div className="flex items-center gap-2 min-w-0">
                                <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: entry.color }} />
                                <span className="text-muted-foreground truncate">{entry.name}</span>
                              </div>
                              <span className="font-semibold whitespace-nowrap">
                                {entry.value}
                              </span>
                            </div>
                          ))}
                        </div>
                      );
                    }} />
                    <Legend />
                    <ReferenceLine
                      y={replicaBounds.min}
                      stroke="#f59e0b"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Min: ${replicaBounds.min}`, position: "left", fill: "#f59e0b", fontSize: 12 }}
                    />
                    <ReferenceLine
                      y={replicaBounds.max}
                      stroke="#ef4444"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Max: ${replicaBounds.max}`, position: "right", fill: "#ef4444", fontSize: 12 }}
                    />
                    <Line
                      type="stepAfter"
                      dataKey="replicasCurrent"
                      name="Réplicas Atuais"
                      stroke="#3b82f6"
                      strokeWidth={2}
                      dot={false}
                      isAnimationActive={false}
                    />
                    {/* Linha D-N - lembrança sutil no gráfico */}
                    <Line
                      type="stepAfter"
                      dataKey="replicasYesterday"
                      name={`Réplicas D-${comparisonDays} (${comparisonDays * 24}h atrás)`}
                      stroke="#c084fc"
                      strokeWidth={1.5}
                      strokeOpacity={0.3}
                      dot={false}
                      unit=""
                      connectNulls
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>

                {/* Labels de informações do HPA */}
                <div className="mt-4 pt-3 border-t flex items-center justify-around text-xs">
                    <div className="text-center">
                      <div className="text-muted-foreground">Min (HPA)</div>
                      <div className="font-semibold text-sm text-amber-600">{replicaBounds.min}</div>
                    </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Atual</div>
                    <div className="font-semibold text-sm text-blue-600">{chartData[chartData.length - 1]?.replicasCurrent || 0}</div>
                  </div>
                    <div className="text-center">
                      <div className="text-muted-foreground">Max (HPA)</div>
                      <div className="font-semibold text-sm text-red-600">{replicaBounds.max}</div>
                    </div>
                </div>
              </div>
              </div>
            </div>
          </div>
        )}
      </CardContent>

      {/* Dialog de Alertas - Renderizado fora do Card para evitar problemas de layout */}
      <AlertsDialog
        open={alertsDialogOpen}
        onOpenChange={setAlertsDialogOpen}
        cluster={cluster}
        namespace={namespace}
        hpaName={hpaName}
      />

      {/* Dialog de Gráfico Expandido */}
      <Dialog open={expandedChart !== null} onOpenChange={(open) => !open && setExpandedChart(null)}>
        <DialogContent className="max-w-[95vw] max-h-[95vh] p-6">
          <DialogHeader className="flex flex-row items-center justify-between gap-4">
            <DialogTitle className="flex items-center gap-2">
                {expandedChart === "cpu" && (
                  <>
                    <Cpu className="h-5 w-5" />
                    Uso de CPU ao longo do tempo
                  </>
                )}
                {expandedChart === "memory" && (
                  <>
                    <MemoryStick className="h-5 w-5" />
                    Uso de Memória ao longo do tempo
                  </>
                )}
                {expandedChart === "replicas" && (
                  <>
                    <Users className="h-5 w-5" />
                    Réplicas ao longo do tempo
                  </>
                )}
              </DialogTitle>
              <div className="flex items-end gap-4 flex-shrink-0">
                {/* Seletor de Comparação (D-1, D-2, D-3) */}
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground font-medium">Comparação</label>
                  <Select value={comparisonDays.toString()} onValueChange={(v) => setComparisonDays(parseInt(v))}>
                    <SelectTrigger className="h-8 w-20 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="1">D-1</SelectItem>
                      <SelectItem value="2">D-2</SelectItem>
                      <SelectItem value="3">D-3</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {/* Seletor de Duração */}
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground font-medium">Período</label>
                  <Select value={duration} onValueChange={setDuration}>
                    <SelectTrigger className="h-8 w-24 text-xs">
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
                </div>
                {/* Seletor de Auto-refresh */}
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground font-medium">Atualização</label>
                  <Select value={autoRefreshInterval.toString()} onValueChange={(v) => setAutoRefreshInterval(parseInt(v))}>
                    <SelectTrigger className="h-8 w-28 text-xs">
                      <SelectValue placeholder="Auto-refresh" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">Off</SelectItem>
                      <SelectItem value="1">1 min</SelectItem>
                      <SelectItem value="5">5 min</SelectItem>
                      <SelectItem value="10">10 min</SelectItem>
                      <SelectItem value="15">15 min</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => refetch(duration)}
                  disabled={loading}
                  title="Atualizar métricas"
                  className="h-8 w-8"
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
                </Button>
                {/* Controles de Zoom */}
                <div className="flex items-center gap-1 border rounded-md px-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleZoomIn}
                    className="h-7 w-7"
                    title="Zoom In"
                  >
                    <ZoomIn className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleZoomOut}
                    className="h-7 w-7"
                    title="Zoom Out"
                  >
                    <ZoomOut className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={handleZoomReset}
                    className="h-7 w-7"
                    title="Reset Zoom"
                    disabled={zoomDomain === null}
                  >
                    <Minimize2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </DialogHeader>
            <div className="mt-4">
            {/* Gráfico de CPU Expandido */}
            {expandedChart === "cpu" && (
              <div>
                <div className="mb-4 flex items-center justify-between text-sm">
                  {snapshotWithResources?.cpu_request && (
                    <span className="text-orange-600">
                      CPU Request: <strong>{snapshotWithResources.cpu_request}</strong>
                    </span>
                  )}
                  {snapshotWithResources?.cpu_limit && (
                    <span className="text-red-600">
                      CPU Limit: <strong>{snapshotWithResources.cpu_limit}</strong>
                    </span>
                  )}
                </div>
                <div onMouseDown={(e) => e.preventDefault()}>
                  <ResponsiveContainer width="100%" height={600}>
                    <ComposedChart 
                      data={zoomedData}
                      onMouseDown={handleMouseDown}
                      onMouseMove={handleMouseMove}
                      onMouseUp={handleMouseUp}
                    >
                    <defs>
                      <linearGradient id="cpuGradientExpanded" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="time" tick={{ fontSize: 12 }} className="text-xs" />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "CPU (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
                      domain={[0, 150]}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    {snapshotWithResources?.cpu_request && (
                      <ReferenceLine
                        y={absoluteToPercent(snapshotWithResources.cpu_request, snapshotWithResources.cpu_limit || snapshotWithResources.cpu_request, true) || 0}
                        stroke="#f97316"
                        strokeDasharray="3 3"
                        label={{ value: `Request: ${snapshotWithResources.cpu_request}`, position: "right", fontSize: 11, fill: "#f97316" }}
                      />
                    )}
                    {snapshotWithResources?.cpu_limit && (
                      <ReferenceLine
                        y={100}
                        stroke="#ef4444"
                        strokeDasharray="3 3"
                        label={{ value: `Limit: ${snapshotWithResources.cpu_limit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
                      />
                    )}
                    <ReferenceArea y1={90} y2={100} fill="#ef4444" fillOpacity={0.1} label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }} />
                    <ReferenceArea y1={80} y2={90} fill="#f59e0b" fillOpacity={0.1} label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }} />
                    <ReferenceLine
                      y={cpuTarget}
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Target: ${cpuTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
                    />
                    <Area type="monotone" dataKey="cpuCurrent" name="CPU Atual" stroke="#3b82f6" strokeWidth={2} fill="url(#cpuGradientExpanded)" unit="%" isAnimationActive={false} />
                    {/* Linha D-N - lembrança sutil */}
                    <Line type="monotone" dataKey="cpuYesterday" name={`CPU D-${comparisonDays} (${comparisonDays * 24}h atrás)`} stroke="#c084fc" strokeWidth={1.5} strokeOpacity={0.3} dot={false} unit="%" isAnimationActive={false} />
                    {refAreaValues && (
                      <ReferenceArea
                        x1={refAreaValues.x1}
                        x2={refAreaValues.x2}
                        strokeOpacity={0.3}
                        fill="#3b82f6"
                        fillOpacity={0.3}
                      />
                    )}
                  </ComposedChart>
                </ResponsiveContainer>
                </div>
              </div>
            )}

            {/* Gráfico de Memory Expandido */}
            {expandedChart === "memory" && (
              <div>
                <div className="mb-4 flex items-center justify-between text-sm">
                  {snapshotWithResources?.memory_request && (
                    <span className="text-orange-600">
                      Memory Request: <strong>{snapshotWithResources.memory_request}</strong>
                    </span>
                  )}
                  {snapshotWithResources?.memory_limit && (
                    <span className="text-red-600">
                      Memory Limit: <strong>{snapshotWithResources.memory_limit}</strong>
                    </span>
                  )}
                </div>
                <div onMouseDown={(e) => e.preventDefault()}>
                  <ResponsiveContainer width="100%" height={600}>
                  <ComposedChart 
                    data={zoomedData}
                    onMouseDown={handleMouseDown}
                    onMouseMove={handleMouseMove}
                    onMouseUp={handleMouseUp}
                  >
                    <defs>
                      <linearGradient id="memoryGradientExpanded" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#8b5cf6" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="time" tick={{ fontSize: 12 }} className="text-xs" />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "Memória (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
                      domain={[0, 150]}
                    />
                    <Tooltip content={<CustomTooltip />} />
                    <Legend />
                    {snapshotWithResources?.memory_request && (
                      <ReferenceLine
                        y={absoluteToPercent(snapshotWithResources.memory_request, snapshotWithResources.memory_limit || snapshotWithResources.memory_request, false) || 0}
                        stroke="#f97316"
                        strokeDasharray="3 3"
                        label={{ value: `Request: ${snapshotWithResources.memory_request}`, position: "right", fontSize: 11, fill: "#f97316" }}
                      />
                    )}
                    {snapshotWithResources?.memory_limit && (
                      <ReferenceLine
                        y={100}
                        stroke="#ef4444"
                        strokeDasharray="3 3"
                        label={{ value: `Limit: ${snapshotWithResources.memory_limit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
                      />
                    )}
                    <ReferenceArea y1={90} y2={100} fill="#ef4444" fillOpacity={0.1} label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }} />
                    <ReferenceArea y1={80} y2={90} fill="#f59e0b" fillOpacity={0.1} label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }} />
                    <ReferenceLine
                      y={memoryTarget}
                      stroke="#10b981"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Target: ${memoryTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
                    />
                    <Area type="monotone" dataKey="memoryCurrent" name="Memória Atual" stroke="#8b5cf6" strokeWidth={2} fill="url(#memoryGradientExpanded)" unit="%" isAnimationActive={false} />
                    {/* Linha D-N - lembrança sutil */}
                    <Line type="monotone" dataKey="memoryYesterday" name={`Memória D-${comparisonDays} (${comparisonDays * 24}h atrás)`} stroke="#c084fc" strokeWidth={1.5} strokeOpacity={0.3} dot={false} unit="%" isAnimationActive={false} />
                    {refAreaValues && (
                      <ReferenceArea
                        x1={refAreaValues.x1}
                        x2={refAreaValues.x2}
                        strokeOpacity={0.3}
                        fill="#8b5cf6"
                        fillOpacity={0.3}
                      />
                    )}
                  </ComposedChart>
                  </ResponsiveContainer>
                </div>
                <div className="mt-4 pt-3 border-t flex items-center justify-around text-sm">
                  <div className="text-center">
                    <div className="text-muted-foreground">Atual</div>
                    <div className="font-semibold">{memoryStats.current.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Média</div>
                    <div className="font-semibold">{memoryStats.average.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Pico</div>
                    <div className="font-semibold text-orange-600">{memoryStats.peak.toFixed(1)}%</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Mínimo</div>
                    <div className="font-semibold">{memoryStats.min.toFixed(1)}%</div>
                  </div>
                </div>
              </div>
            )}

            {/* Gráfico de Replicas Expandido */}
            {expandedChart === "replicas" && (
              <div>
                <div onMouseDown={(e) => e.preventDefault()}>
                <ResponsiveContainer width="100%" height={600}>
                  <LineChart 
                    data={zoomedData} 
                    isAnimationActive={false}
                    onMouseDown={handleMouseDown}
                    onMouseMove={handleMouseMove}
                    onMouseUp={handleMouseUp}
                  >
                    <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                    <XAxis dataKey="time" tick={{ fontSize: 12 }} className="text-xs" />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      label={{ value: "Réplicas", angle: -90, position: "insideLeft", fontSize: 12 }}
                      allowDecimals={false}
                      domain={[0, (dataMax: number) => {
                        const maxReplicas = replicaBounds.max || dataMax;
                        return Math.max(dataMax, maxReplicas * 1.5);
                      }]}
                    />
                    <Tooltip content={(props) => {
                      if (!props.active || !props.payload || !props.payload.length) return null;
                      return (
                        <div className="bg-background border rounded-lg shadow-lg p-3 space-y-1.5">
                          <p className="text-sm font-semibold mb-2">{props.label}</p>
                          {props.payload.map((entry: any, index: number) => (
                            <div key={index} className="flex items-center justify-between gap-3 text-xs">
                              <div className="flex items-center gap-2 min-w-0">
                                <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: entry.color }} />
                                <span className="text-muted-foreground truncate">{entry.name}</span>
                              </div>
                              <span className="font-semibold whitespace-nowrap">{entry.value}</span>
                            </div>
                          ))}
                        </div>
                      );
                    }} />
                    <Legend />
                    <ReferenceLine
                      y={replicaBounds.min}
                      stroke="#f59e0b"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Min: ${replicaBounds.min}`, position: "left", fill: "#f59e0b", fontSize: 12 }}
                    />
                    <ReferenceLine
                      y={replicaBounds.max}
                      stroke="#ef4444"
                      strokeWidth={2}
                      strokeDasharray="5 5"
                      label={{ value: `Max: ${replicaBounds.max}`, position: "right", fill: "#ef4444", fontSize: 12 }}
                    />
                    <Line type="stepAfter" dataKey="replicasCurrent" name="Réplicas Atuais" stroke="#3b82f6" strokeWidth={2} dot={false} unit="" connectNulls isAnimationActive={false} />
                    {/* Linha D-N - lembrança sutil */}
                    <Line type="stepAfter" dataKey="replicasYesterday" name={`Réplicas D-${comparisonDays} (${comparisonDays * 24}h atrás)`} stroke="#c084fc" strokeWidth={1.5} strokeOpacity={0.3} dot={false} unit="" connectNulls isAnimationActive={false} />
                    {refAreaValues && (
                      <ReferenceArea
                        x1={refAreaValues.x1}
                        x2={refAreaValues.x2}
                        strokeOpacity={0.3}
                        fill="#3b82f6"
                        fillOpacity={0.3}
                      />
                    )}
                  </LineChart>
                </ResponsiveContainer>
                </div>
                <div className="mt-4 pt-3 border-t flex items-center justify-around text-sm">
                  <div className="text-center">
                    <div className="text-muted-foreground">Min (HPA)</div>
                    <div className="font-semibold text-amber-600">{replicaBounds.min}</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Atual</div>
                    <div className="font-semibold text-blue-600">{zoomedData[zoomedData.length - 1]?.replicasCurrent || 0}</div>
                  </div>
                  <div className="text-center">
                    <div className="text-muted-foreground">Max (HPA)</div>
                    <div className="font-semibold text-red-600">{replicaBounds.max}</div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
