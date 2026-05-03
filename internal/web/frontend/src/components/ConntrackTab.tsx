import { useMemo, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { Input } from '@/components/ui/input';
import {
  Loader2,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Activity,
  ChevronDown,
  ChevronUp,
  TrendingUp,
  Search,
} from 'lucide-react';
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  ReferenceLine,
  Cell,
} from 'recharts';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart';
import { apiClient } from '@/lib/api/client';
import type { ConntrackNodeStats, ConntrackNodeHistoryResponse, ConntrackHistoryPoint } from '@/lib/api/types';

interface ConntrackTabProps {
  cluster: string;
  nodepool: string;
}

// ─── Tipos e helpers ───────────────────────────────────────────────────────────

type HistStats = { avg: number; p95: number; max: number };

type CapRec = { level: 'ok' | 'warning' | 'critical' | 'spike'; label: string };

function computeHistStats(points: ConntrackHistoryPoint[]): HistStats | null {
  if (!points.length) return null;
  const pcts = points.map((p) => p.usage_pct);
  const sorted = [...pcts].sort((a, b) => a - b);
  return {
    avg: pcts.reduce((s, v) => s + v, 0) / pcts.length,
    p95: sorted[Math.floor(sorted.length * 0.95)] ?? sorted[sorted.length - 1],
    max: sorted[sorted.length - 1],
  };
}

function getCapacityRec(current: number, hist: HistStats | null): CapRec {
  if (!hist) return { level: 'ok', label: 'Sem histórico' };
  if (hist.p95 >= 80) return { level: 'critical', label: 'Aumentar limite' };
  if (hist.p95 >= 65) return { level: 'warning', label: 'Monitorar tendência' };
  if (current >= hist.avg * 1.5 && current >= 30) return { level: 'spike', label: 'Spike ativo' };
  return { level: 'ok', label: 'Capacidade OK' };
}

function barFill(pct: number): string {
  if (pct >= 90) return '#ef4444';
  if (pct >= 70) return '#eab308';
  return '#22c55e';
}

const fmt = (n: number) =>
  n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${(n / 1_000).toFixed(1)}K` : String(Math.round(n));

const chartConfig = {
  pct: { label: 'Uso %' },
} satisfies ChartConfig;

// ─── StatusBadge ──────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: ConntrackNodeStats['status'] }) {
  const map: Record<string, { cls: string; icon: React.ReactNode; label: string }> = {
    ok:       { cls: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100', icon: <CheckCircle2 className="h-3 w-3" />, label: 'OK' },
    warning:  { cls: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-100', icon: <AlertTriangle className="h-3 w-3" />, label: 'Warning' },
    critical: { cls: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100', icon: <XCircle className="h-3 w-3" />, label: 'Critical' },
    error:    { cls: '', icon: <XCircle className="h-3 w-3" />, label: 'Erro' },
  };
  const m = map[status] ?? map.error;
  return <Badge className={`gap-1 ${m.cls}`}>{m.icon}{m.label}</Badge>;
}

// ─── CapacityBadge ────────────────────────────────────────────────────────────

function CapacityBadge({ rec }: { rec: CapRec }) {
  const cls: Record<CapRec['level'], string> = {
    ok:       'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950 dark:text-emerald-300',
    warning:  'bg-yellow-50 text-yellow-700 border-yellow-200 dark:bg-yellow-950 dark:text-yellow-300',
    critical: 'bg-red-50 text-red-700 border-red-200 dark:bg-red-950 dark:text-red-300',
    spike:    'bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-950 dark:text-orange-300',
  };
  return (
    <Badge variant="outline" className={`gap-1 text-xs font-medium ${cls[rec.level]}`}>
      <TrendingUp className="h-3 w-3" />
      {rec.label}
    </Badge>
  );
}

// ─── NodeCard ─────────────────────────────────────────────────────────────────

interface NodeCardProps {
  node: ConntrackNodeStats;
  cluster: string;
  history?: ConntrackNodeHistoryResponse;
  histLoading: boolean;
}

function NodeCard({ node, histLoading, history }: NodeCardProps) {
  const [expanded, setExpanded] = useState(false);

  const histStats = useMemo(
    () => (history?.points ? computeHistStats(history.points) : null),
    [history],
  );

  const capacityRec = useMemo(
    () => getCapacityRec(node.usage_pct, histStats),
    [node.usage_pct, histStats],
  );

  // Dados para o BarChart — step 30min = ~48 barras em 24h
  const chartData = useMemo(() => {
    if (!history?.points?.length) return [];
    // Reduzir para no máximo 48 pontos para não sobrecarregar o chart
    const pts = history.points;
    const step = Math.ceil(pts.length / 48);
    return pts
      .filter((_, i) => i % step === 0)
      .map((p) => ({
        time: new Date(p.ts * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' }),
        pct: parseFloat(p.usage_pct.toFixed(1)),
        fill: barFill(p.usage_pct),
      }));
  }, [history]);

  const xInterval = Math.max(0, Math.floor(chartData.length / 6) - 1);

  return (
    <Card className="overflow-hidden">
      <CardHeader className="py-3 px-4">
        <CardTitle className="text-sm font-mono flex flex-wrap items-center gap-2 justify-between">
          <span className="truncate mr-1">{node.node_name}</span>
          <div className="flex items-center gap-2 flex-shrink-0">
            <StatusBadge status={node.status} />
            <CapacityBadge rec={capacityRec} />
          </div>
        </CardTitle>
      </CardHeader>

      <CardContent className="px-4 pb-4 space-y-3">
        {node.error ? (
          <p className="text-xs text-destructive">{node.error}</p>
        ) : (
          <>
            {/* ── BarChart histórico ── */}
            {histLoading && !history && (
              <Skeleton className="h-[140px] w-full rounded-md" />
            )}

            {!histLoading && history && !history.prometheus_available && (
              <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
                <AlertTriangle className="h-4 w-4 text-yellow-500 flex-shrink-0" />
                Prometheus indisponível — histórico requer node_exporter.
              </div>
            )}

            {chartData.length > 0 && (
              <div className="space-y-1">
                <p className="text-[10px] text-muted-foreground uppercase tracking-wide">
                  Uso conntrack — últimas 24h
                </p>
                <ChartContainer config={chartConfig} className="h-[140px] w-full">
                  <BarChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }} barCategoryGap="15%">
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                      interval={xInterval}
                    />
                    <YAxis
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                      domain={[0, 100]}
                      unit="%"
                    />
                    <ChartTooltip
                      content={
                        <ChartTooltipContent
                          formatter={(value) => [`${value}%`, 'Uso']}
                          labelFormatter={(label) => `Horário: ${label}`}
                        />
                      }
                    />
                    {/* Limiares */}
                    <ReferenceLine y={90} stroke="#ef4444" strokeDasharray="4 3" strokeWidth={1} />
                    <ReferenceLine y={70} stroke="#eab308" strokeDasharray="4 3" strokeWidth={1} />
                    {/* Valor atual */}
                    <ReferenceLine
                      y={node.usage_pct}
                      stroke="#3b82f6"
                      strokeWidth={1.5}
                      label={{
                        value: `Atual ${node.usage_pct.toFixed(1)}%`,
                        position: 'insideTopRight',
                        fontSize: 9,
                        fill: '#3b82f6',
                      }}
                    />
                    <Bar dataKey="pct" radius={[3, 3, 0, 0]}>
                      {chartData.map((entry, i) => (
                        <Cell key={i} fill={entry.fill} fillOpacity={0.85} />
                      ))}
                    </Bar>
                  </BarChart>
                </ChartContainer>

                {/* Legenda de limiares */}
                <div className="flex items-center gap-3 text-[10px] text-muted-foreground justify-end">
                  <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-yellow-400" />70%</span>
                  <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-red-500" />90%</span>
                  <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-blue-500" />Atual</span>
                </div>
              </div>
            )}

            {/* ── Stats de comparação ── */}
            {histStats && (
              <div className="grid grid-cols-4 gap-2 text-xs rounded-md border border-border/60 px-3 py-2 bg-muted/30">
                {[
                  { label: 'Atual', value: node.usage_pct, color: barFill(node.usage_pct) },
                  { label: 'Média 24h', value: histStats.avg, color: barFill(histStats.avg) },
                  { label: 'P95', value: histStats.p95, color: barFill(histStats.p95) },
                  { label: 'Pico', value: histStats.max, color: barFill(histStats.max) },
                ].map(({ label, value, color }) => (
                  <div key={label} className="text-center">
                    <p className="text-muted-foreground text-[10px]">{label}</p>
                    <p className="font-semibold tabular-nums" style={{ color }}>{value.toFixed(1)}%</p>
                  </div>
                ))}
              </div>
            )}

            {/* ── Barra de uso atual ── */}
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>Conexões ativas: {fmt(node.count)}</span>
                <span>Limite: {fmt(node.max)}</span>
              </div>
              <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
                <div
                  className="h-full rounded-full transition-all"
                  style={{
                    width: `${Math.min(node.usage_pct, 100)}%`,
                    backgroundColor: barFill(node.usage_pct),
                  }}
                />
              </div>
            </div>

            {/* ── Coleta + toggle detalhes ── */}
            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
              <span className="truncate">via {node.probe_method}</span>
              {chartData.length > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-[10px] px-2 gap-1 text-muted-foreground hover:text-foreground"
                  onClick={() => setExpanded((v) => !v)}
                >
                  {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                  {expanded ? 'Ocultar detalhes' : 'Dados brutos'}
                </Button>
              )}
            </div>

            {/* ── Detalhes expandidos ── */}
            {expanded && history?.points && (
              <div className="rounded-md border border-border/50 bg-muted/20 p-2 space-y-1 max-h-40 overflow-y-auto">
                <p className="text-[10px] text-muted-foreground font-medium uppercase">Pontos históricos ({history.points.length})</p>
                {history.points.map((p) => (
                  <div key={p.ts} className="grid grid-cols-3 gap-2 text-[10px] font-mono">
                    <span className="text-muted-foreground">
                      {new Date(p.ts * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' })}
                    </span>
                    <span style={{ color: barFill(p.usage_pct) }}>{p.usage_pct.toFixed(1)}%</span>
                    <span className="text-muted-foreground">{fmt(p.count)}</span>
                  </div>
                ))}
              </div>
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
  const [historyMap, setHistoryMap] = useState<Record<string, ConntrackNodeHistoryResponse>>({});
  const [histLoading, setHistLoading] = useState(false);
  const [nodeSearch, setNodeSearch] = useState("");

  const fetchHistory = async (ns: ConntrackNodeStats[]) => {
    if (!ns.length) return;
    setHistLoading(true);
    const results = await Promise.allSettled(
      ns.map((n) => apiClient.getConntrackNodeHistory(cluster, n.node_name, 24, 30)),
    );
    const map: Record<string, ConntrackNodeHistoryResponse> = {};
    results.forEach((r, i) => {
      if (r.status === 'fulfilled') map[ns[i].node_name] = r.value;
    });
    setHistoryMap(map);
    setHistLoading(false);
  };

  const fetchStats = async () => {
    setLoading(true);
    setError(null);
    setHistoryMap({});
    try {
      const data = await apiClient.getConntrackStats(cluster, nodepool);
      setNodes(data.nodes);
      setFetchedAt(data.fetched_at);
      fetchHistory(data.nodes);
    } catch (e: any) {
      setError(e?.message ?? 'Erro ao buscar estatísticas de conntrack');
    } finally {
      setLoading(false);
    }
  };

  const filteredNodes = useMemo(() => {
    const q = nodeSearch.trim().toLowerCase();
    if (!q) return nodes;
    return nodes.filter((n) => n.node_name.toLowerCase().includes(q));
  }, [nodes, nodeSearch]);

  return (
    <div className="space-y-4 mt-4">
      {/* Cabeçalho */}
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm text-muted-foreground">
            Conexões rastreadas pelo kernel — pool <strong>{nodepool}</strong>
          </p>
          {fetchedAt && (
            <p className="text-xs text-muted-foreground mt-0.5">
              Snapshot: {new Date(fetchedAt).toLocaleTimeString('pt-BR')}
              {histLoading && <span className="ml-2 italic">carregando histórico...</span>}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {nodes.length > 0 && (
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
              <Input
                value={nodeSearch}
                onChange={(e) => setNodeSearch(e.target.value)}
                placeholder="Filtrar por nome..."
                className="pl-8 h-8 text-xs w-48"
              />
            </div>
          )}
          <Button size="sm" variant="outline" onClick={fetchStats} disabled={loading}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <RefreshCw className="h-4 w-4 mr-1" />}
            {nodes.length === 0 && !loading ? 'Carregar' : 'Atualizar'}
          </Button>
        </div>
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

      {nodes.length > 0 && filteredNodes.length === 0 && (
        <div className="flex items-center justify-center py-8 text-muted-foreground text-sm">
          Nenhum node encontrado para "<strong>{nodeSearch}</strong>"
        </div>
      )}

      {filteredNodes.length > 0 && (
        <div className="space-y-3">
          {filteredNodes.map((node) => (
            <NodeCard
              key={node.node_name}
              node={node}
              cluster={cluster}
              history={historyMap[node.node_name]}
              histLoading={histLoading}
            />
          ))}
        </div>
      )}
    </div>
  );
}
