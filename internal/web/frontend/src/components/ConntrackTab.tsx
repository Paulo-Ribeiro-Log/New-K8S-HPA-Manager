import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Loader2,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Activity,
  History,
  ChevronDown,
  ChevronUp,
} from 'lucide-react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts';
import { apiClient } from '@/lib/api/client';
import type { ConntrackNodeStats, ConntrackNodeHistoryResponse } from '@/lib/api/types';

interface ConntrackTabProps {
  cluster: string;
  nodepool: string;
}

// ─── helpers ──────────────────────────────────────────────────────────────────

const fmt = (n: number) =>
  n >= 1_000_000
    ? `${(n / 1_000_000).toFixed(1)}M`
    : n >= 1_000
    ? `${(n / 1_000).toFixed(1)}K`
    : String(Math.round(n));

const barColor = (pct: number) => {
  if (pct >= 90) return 'bg-red-500';
  if (pct >= 70) return 'bg-yellow-500';
  return 'bg-green-500';
};

const lineColor = (pct: number) => {
  if (pct >= 90) return '#ef4444';
  if (pct >= 70) return '#eab308';
  return '#22c55e';
};

// ─── StatusBadge ──────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: ConntrackNodeStats['status'] }) {
  switch (status) {
    case 'ok':
      return (
        <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100 gap-1">
          <CheckCircle2 className="h-3 w-3" /> OK
        </Badge>
      );
    case 'warning':
      return (
        <Badge className="bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-100 gap-1">
          <AlertTriangle className="h-3 w-3" /> Warning
        </Badge>
      );
    case 'critical':
      return (
        <Badge className="bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100 gap-1">
          <XCircle className="h-3 w-3" /> Critical
        </Badge>
      );
    default:
      return (
        <Badge variant="destructive" className="gap-1">
          <XCircle className="h-3 w-3" /> Error
        </Badge>
      );
  }
}

// ─── HoursSelector ────────────────────────────────────────────────────────────

const HOUR_OPTIONS = [1, 3, 6, 12, 24] as const;
type Hours = (typeof HOUR_OPTIONS)[number];

function HoursSelector({ value, onChange }: { value: Hours; onChange: (h: Hours) => void }) {
  return (
    <div className="flex gap-1">
      {HOUR_OPTIONS.map((h) => (
        <button
          key={h}
          onClick={() => onChange(h)}
          className={`text-xs px-2 py-0.5 rounded-full border transition-colors ${
            value === h
              ? 'bg-primary text-primary-foreground border-primary'
              : 'border-border text-muted-foreground hover:bg-muted'
          }`}
        >
          {h}h
        </button>
      ))}
    </div>
  );
}

// ─── NodeHistoryChart ─────────────────────────────────────────────────────────

interface NodeHistoryChartProps {
  cluster: string;
  nodeName: string;
  currentUsagePct: number;
}

function NodeHistoryChart({ cluster, nodeName, currentUsagePct }: NodeHistoryChartProps) {
  const [hours, setHours] = useState<Hours>(6);
  const [history, setHistory] = useState<ConntrackNodeHistoryResponse | null>(null);
  const [loading, setLoading] = useState(false);

  const load = async (h: Hours) => {
    setLoading(true);
    try {
      const data = await apiClient.getConntrackNodeHistory(cluster, nodeName, h, 5);
      setHistory(data);
    } finally {
      setLoading(false);
    }
  };

  const handleHoursChange = (h: Hours) => {
    setHours(h);
    load(h);
  };

  // Carregar ao montar
  if (history === null && !loading) {
    load(hours);
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-24 gap-2 text-muted-foreground text-xs">
        <Loader2 className="h-4 w-4 animate-spin" />
        Consultando Prometheus...
      </div>
    );
  }

  if (!history) return null;

  if (!history.prometheus_available) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
        <AlertTriangle className="h-4 w-4 text-yellow-500 flex-shrink-0" />
        Prometheus não disponível neste cluster — histórico requer node_exporter.
      </div>
    );
  }

  if (history.error) {
    return (
      <p className="text-xs text-destructive py-2">{history.error}</p>
    );
  }

  if (!history.points || history.points.length === 0) {
    return (
      <p className="text-xs text-muted-foreground py-2">
        Sem dados no Prometheus para este nó. Verifique se o node_exporter está instalado.
      </p>
    );
  }

  const chartData = history.points.map((p) => ({
    time: new Date(p.ts * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' }),
    count: Math.round(p.count),
    max: Math.round(p.max),
    pct: parseFloat(p.usage_pct.toFixed(1)),
  }));

  const color = lineColor(currentUsagePct);
  const hasMax = history.points.some((p) => p.max > 0);

  return (
    <div className="space-y-2 pt-1">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">Janela:</span>
        <HoursSelector value={hours} onChange={handleHoursChange} />
      </div>

      {/* Gráfico de % de uso */}
      <div>
        <p className="text-xs text-muted-foreground mb-1">Uso % (conntrack/limit)</p>
        <ResponsiveContainer width="100%" height={100}>
          <LineChart data={chartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis dataKey="time" tick={{ fontSize: 9 }} interval="preserveStartEnd" />
            <YAxis tick={{ fontSize: 9 }} domain={[0, 100]} unit="%" />
            <Tooltip
              content={({ active, payload, label }) => {
                if (!active || !payload?.length) return null;
                return (
                  <div className="bg-background border rounded p-2 text-xs space-y-0.5 shadow-lg">
                    <p className="font-semibold">{label}</p>
                    {payload.map((e: any) => (
                      <p key={e.name} style={{ color: e.color }}>
                        {e.name}: {e.value}{e.name === 'pct' ? '%' : ''}
                      </p>
                    ))}
                  </div>
                );
              }}
            />
            <ReferenceLine y={90} stroke="#ef4444" strokeDasharray="4 4" />
            <ReferenceLine y={70} stroke="#eab308" strokeDasharray="4 4" />
            <Line
              type="monotone"
              dataKey="pct"
              stroke={color}
              strokeWidth={1.5}
              dot={false}
              name="pct"
            />
          </LineChart>
        </ResponsiveContainer>
      </div>

      {/* Gráfico de conexões absolutas */}
      <div>
        <p className="text-xs text-muted-foreground mb-1">Conexões ativas</p>
        <ResponsiveContainer width="100%" height={90}>
          <LineChart data={chartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis dataKey="time" tick={{ fontSize: 9 }} interval="preserveStartEnd" />
            <YAxis
              tick={{ fontSize: 9 }}
              tickFormatter={(v) => fmt(v)}
            />
            <Tooltip
              content={({ active, payload, label }) => {
                if (!active || !payload?.length) return null;
                return (
                  <div className="bg-background border rounded p-2 text-xs space-y-0.5 shadow-lg">
                    <p className="font-semibold">{label}</p>
                    {payload.map((e: any) => (
                      <p key={e.name} style={{ color: e.color }}>
                        {e.name}: {fmt(e.value)}
                      </p>
                    ))}
                  </div>
                );
              }}
            />
            <Line
              type="monotone"
              dataKey="count"
              stroke={color}
              strokeWidth={1.5}
              dot={false}
              name="ativas"
            />
            {hasMax && (
              <Line
                type="monotone"
                dataKey="max"
                stroke="#6b7280"
                strokeWidth={1}
                strokeDasharray="4 4"
                dot={false}
                name="limit"
              />
            )}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

// ─── NodeCard ─────────────────────────────────────────────────────────────────

function NodeCard({ node, cluster }: { node: ConntrackNodeStats; cluster: string }) {
  const [historyOpen, setHistoryOpen] = useState(false);

  return (
    <Card className="overflow-hidden">
      <CardHeader className="py-3 px-4">
        <CardTitle className="text-sm font-mono flex items-center justify-between">
          <span className="truncate mr-2">{node.node_name}</span>
          <StatusBadge status={node.status} />
        </CardTitle>
      </CardHeader>

      <CardContent className="px-4 pb-4 space-y-3">
        {node.error ? (
          <p className="text-xs text-destructive">{node.error}</p>
        ) : (
          <>
            {/* Barra de uso */}
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>Uso: {node.usage_pct.toFixed(1)}%</span>
                <span>{fmt(node.count)} / {fmt(node.max)}</span>
              </div>
              <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all ${barColor(node.usage_pct)}`}
                  style={{ width: `${Math.min(node.usage_pct, 100)}%` }}
                />
              </div>
            </div>

            {/* Métricas */}
            <div className="grid grid-cols-3 gap-2 text-xs">
              <div>
                <p className="text-muted-foreground">Ativas</p>
                <p className="font-semibold">{fmt(node.count)}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Máximo</p>
                <p className="font-semibold">{fmt(node.max)}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Buckets</p>
                <p className="font-semibold">{node.buckets > 0 ? fmt(node.buckets) : '—'}</p>
              </div>
            </div>

            {/* Coleta */}
            <p className="text-xs text-muted-foreground truncate">via {node.probe_method}</p>

            {/* Botão histórico */}
            <Button
              variant="outline"
              size="sm"
              className="w-full h-7 text-xs gap-1"
              onClick={() => setHistoryOpen((v) => !v)}
            >
              <History className="h-3.5 w-3.5" />
              {historyOpen ? 'Ocultar histórico' : 'Ver histórico'}
              {historyOpen ? <ChevronUp className="h-3 w-3 ml-auto" /> : <ChevronDown className="h-3 w-3 ml-auto" />}
            </Button>

            {/* Gráficos históricos (expansível) */}
            {historyOpen && (
              <NodeHistoryChart
                cluster={cluster}
                nodeName={node.node_name}
                currentUsagePct={node.usage_pct}
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ─── ConntrackTab (principal) ─────────────────────────────────────────────────

export function ConntrackTab({ cluster, nodepool }: ConntrackTabProps) {
  const [nodes, setNodes] = useState<ConntrackNodeStats[]>([]);
  const [fetchedAt, setFetchedAt] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchStats = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await apiClient.getConntrackStats(cluster, nodepool);
      setNodes(data.nodes);
      setFetchedAt(data.fetched_at);
    } catch (e: any) {
      setError(e?.message ?? 'Erro ao buscar estatísticas de conntrack');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-4 mt-4">
      {/* Cabeçalho */}
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-muted-foreground">
            Conexões ativas rastreadas pelo kernel em cada nó do pool{' '}
            <strong>{nodepool}</strong>.
          </p>
          {fetchedAt && (
            <p className="text-xs text-muted-foreground mt-0.5">
              Atualizado: {new Date(fetchedAt).toLocaleTimeString('pt-BR')}
            </p>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={fetchStats} disabled={loading}>
          {loading ? (
            <Loader2 className="h-4 w-4 animate-spin mr-1" />
          ) : (
            <RefreshCw className="h-4 w-4 mr-1" />
          )}
          {nodes.length === 0 && !loading ? 'Carregar' : 'Atualizar'}
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {loading && nodes.length === 0 && (
        <div className="flex items-center justify-center py-12 text-muted-foreground gap-2">
          <Loader2 className="h-5 w-5 animate-spin" />
          <span>Coletando dados dos nós...</span>
        </div>
      )}

      {!loading && nodes.length === 0 && !error && (
        <div className="flex flex-col items-center justify-center py-12 gap-2 text-muted-foreground">
          <Activity className="h-8 w-8 opacity-40" />
          <p className="text-sm">Clique em "Carregar" para buscar as estatísticas de conntrack.</p>
        </div>
      )}

      {nodes.length > 0 && (
        <div className="space-y-3">
          {nodes.map((node) => (
            <NodeCard key={node.node_name} node={node} cluster={cluster} />
          ))}
        </div>
      )}
    </div>
  );
}
