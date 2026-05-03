import { useMemo, useState, useEffect, useRef } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { Input } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
  Loader2, RefreshCw, AlertTriangle, CheckCircle2, XCircle, Activity,
  ChevronDown, ChevronUp, TrendingUp, TrendingDown, Minus, Search,
  LayoutList, LayoutGrid, ArrowUpDown,
} from 'lucide-react';
import {
  BarChart, Bar, XAxis, YAxis, ReferenceLine, Cell,
} from 'recharts';
import {
  ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig,
} from '@/components/ui/chart';
import { apiClient } from '@/lib/api/client';
import type { ConntrackNodeStats, ConntrackNodeHistoryResponse, ConntrackHistoryPoint } from '@/lib/api/types';

interface ConntrackTabProps {
  cluster: string;
  nodepool: string;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

type HistStats = { avg: number; p95: number; max: number };
type CapRec = { level: 'ok' | 'warning' | 'critical' | 'spike'; label: string };
type Trend = 'up' | 'down' | 'stable';
type SortKey = 'usage' | 'name' | 'p95';
type ViewMode = 'table' | 'cards';

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

function getTrend(current: number, hist: HistStats | null): Trend {
  if (!hist || hist.avg === 0) return 'stable';
  if (current >= hist.avg * 1.4) return 'up';
  if (current <= hist.avg * 0.6) return 'down';
  return 'stable';
}

function barFill(pct: number): string {
  if (pct >= 90) return '#ef4444';
  if (pct >= 70) return '#eab308';
  return '#22c55e';
}

const fmt = (n: number) =>
  n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${(n / 1_000).toFixed(1)}K` : String(Math.round(n));

const chartConfig = { pct: { label: 'Uso %' } } satisfies ChartConfig;

// ─── Sub-componentes pequenos ──────────────────────────────────────────────────

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

function CapacityBadge({ rec }: { rec: CapRec }) {
  const cls: Record<CapRec['level'], string> = {
    ok:       'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950 dark:text-emerald-300',
    warning:  'bg-yellow-50 text-yellow-700 border-yellow-200 dark:bg-yellow-950 dark:text-yellow-300',
    critical: 'bg-red-50 text-red-700 border-red-200 dark:bg-red-950 dark:text-red-300',
    spike:    'bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-950 dark:text-orange-300',
  };
  return (
    <Badge variant="outline" className={`gap-1 text-xs font-medium ${cls[rec.level]}`}>
      <TrendingUp className="h-3 w-3" />{rec.label}
    </Badge>
  );
}

function TrendIcon({ trend }: { trend: Trend }) {
  if (trend === 'up') return <TrendingUp className="h-3.5 w-3.5 text-red-500" />;
  if (trend === 'down') return <TrendingDown className="h-3.5 w-3.5 text-green-500" />;
  return <Minus className="h-3.5 w-3.5 text-muted-foreground" />;
}

function MiniBar({ pct }: { pct: number }) {
  return (
    <div className="flex items-center gap-1.5">
      <div className="h-1.5 w-20 rounded-full bg-muted overflow-hidden flex-shrink-0">
        <div className="h-full rounded-full" style={{ width: `${Math.min(pct, 100)}%`, backgroundColor: barFill(pct) }} />
      </div>
      <span className="text-xs tabular-nums w-9 text-right" style={{ color: barFill(pct) }}>
        {pct.toFixed(1)}%
      </span>
    </div>
  );
}

function HistoryChart({
  node, history, histLoading,
}: {
  node: ConntrackNodeStats;
  history?: ConntrackNodeHistoryResponse;
  histLoading: boolean;
}) {
  const chartData = useMemo(() => {
    if (!history?.points?.length) return [];
    const pts = history.points;
    const step = Math.ceil(pts.length / 48);
    return pts.filter((_, i) => i % step === 0).map((p) => ({
      time: new Date(p.ts * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' }),
      pct: parseFloat(p.usage_pct.toFixed(1)),
      fill: barFill(p.usage_pct),
    }));
  }, [history]);

  const xInterval = Math.max(0, Math.floor(chartData.length / 6) - 1);

  if (histLoading && !history) return <Skeleton className="h-[140px] w-full rounded-md" />;

  if (!history?.prometheus_available) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground py-3">
        <AlertTriangle className="h-4 w-4 text-yellow-500 flex-shrink-0" />
        Prometheus indisponível — histórico requer node_exporter.
      </div>
    );
  }

  if (!chartData.length) return null;

  return (
    <div className="space-y-1">
      <p className="text-[10px] text-muted-foreground uppercase tracking-wide">Uso conntrack — últimas 24h</p>
      <ChartContainer config={chartConfig} className="h-[140px] w-full">
        <BarChart data={chartData} margin={{ top: 8, right: 8, left: -18, bottom: 0 }} barCategoryGap="15%">
          <XAxis dataKey="time" tick={{ fontSize: 9 }} tickLine={false} axisLine={false} interval={xInterval} />
          <YAxis tick={{ fontSize: 9 }} tickLine={false} axisLine={false} domain={[0, 100]} unit="%" />
          <ChartTooltip
            content={<ChartTooltipContent formatter={(v) => [`${v}%`, 'Uso']} labelFormatter={(l) => `Horário: ${l}`} />}
          />
          <ReferenceLine y={90} stroke="#ef4444" strokeDasharray="4 3" strokeWidth={1} />
          <ReferenceLine y={70} stroke="#eab308" strokeDasharray="4 3" strokeWidth={1} />
          <ReferenceLine y={node.usage_pct} stroke="#3b82f6" strokeWidth={1.5}
            label={{ value: `Atual ${node.usage_pct.toFixed(1)}%`, position: 'insideTopRight', fontSize: 9, fill: '#3b82f6' }}
          />
          <Bar dataKey="pct" radius={[3, 3, 0, 0]}>
            {chartData.map((e, i) => <Cell key={i} fill={e.fill} fillOpacity={0.85} />)}
          </Bar>
        </BarChart>
      </ChartContainer>
      <div className="flex items-center gap-3 text-[10px] text-muted-foreground justify-end">
        <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-yellow-400" />70%</span>
        <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-red-500" />90%</span>
        <span className="flex items-center gap-1"><span className="inline-block w-3 h-0.5 bg-blue-500" />Atual</span>
      </div>
    </div>
  );
}

// ─── NodeCard (view cards) ────────────────────────────────────────────────────

function NodeCard({
  node, history, histLoading, histStats, trend, capacityRec,
}: {
  node: ConntrackNodeStats;
  history?: ConntrackNodeHistoryResponse;
  histLoading: boolean;
  histStats: HistStats | null;
  trend: Trend;
  capacityRec: CapRec;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <Card className="overflow-hidden">
      <CardHeader className="py-3 px-4">
        <CardTitle className="text-sm font-mono flex flex-wrap items-center gap-2 justify-between">
          <div className="flex items-center gap-2 min-w-0">
            <TrendIcon trend={trend} />
            <span className="truncate">{node.node_name}</span>
          </div>
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
            <HistoryChart node={node} history={history} histLoading={histLoading} />

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

            {/* Barra de uso atual */}
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>Conexões ativas: {fmt(node.count)}</span>
                <span>Limite: {fmt(node.max)}</span>
              </div>
              <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
                <div className="h-full rounded-full transition-all"
                  style={{ width: `${Math.min(node.usage_pct, 100)}%`, backgroundColor: barFill(node.usage_pct) }} />
              </div>
            </div>

            {/* Metadados */}
            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
              <div className="flex gap-4">
                <span>via {node.probe_method}</span>
                {node.buckets > 0 && <span>buckets: {fmt(node.buckets)}</span>}
              </div>
              <Button variant="ghost" size="sm" className="h-6 text-[10px] px-2 gap-1 text-muted-foreground hover:text-foreground"
                onClick={() => setExpanded((v) => !v)}>
                {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                {expanded ? 'Ocultar' : 'Dados brutos'}
              </Button>
            </div>

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

// ─── SummaryStrip ─────────────────────────────────────────────────────────────

function SummaryStrip({
  nodes, histMap,
}: {
  nodes: ConntrackNodeStats[];
  histMap: Record<string, ConntrackNodeHistoryResponse>;
}) {
  const atRisk = nodes.filter((n) => n.status === 'warning' || n.status === 'critical').length;
  const worst = nodes.reduce((a, b) => (a.usage_pct > b.usage_pct ? a : b), nodes[0]);
  const avgPct = nodes.reduce((s, n) => s + n.usage_pct, 0) / nodes.length;
  const promOk = Object.values(histMap).some((h) => h.prometheus_available);

  const tiles = [
    { label: 'Nodes monitorados', value: String(nodes.length), sub: promOk ? 'histórico disponível' : 'sem histórico Prometheus' },
    {
      label: 'Em alerta',
      value: String(atRisk),
      sub: atRisk === 0 ? 'todos saudáveis' : `${atRisk} node${atRisk > 1 ? 's' : ''} acima de 70%`,
      color: atRisk > 0 ? (nodes.some((n) => n.status === 'critical') ? 'text-red-500' : 'text-amber-500') : 'text-green-500',
    },
    {
      label: 'Maior uso',
      value: `${worst.usage_pct.toFixed(1)}%`,
      sub: worst.node_name.split('-').slice(-1)[0],
      color: worst.usage_pct >= 90 ? 'text-red-500' : worst.usage_pct >= 70 ? 'text-amber-500' : 'text-green-500',
    },
    { label: 'Uso médio', value: `${avgPct.toFixed(1)}%`, sub: 'todos os nodes', color: barFill(avgPct) },
  ];

  return (
    <div className="grid grid-cols-4 gap-3">
      {tiles.map((t) => (
        <div key={t.label} className="rounded-md border border-border/60 px-3 py-2 bg-muted/20">
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground">{t.label}</p>
          <p className={`text-lg font-bold tabular-nums leading-tight ${t.color ?? ''}`}>{t.value}</p>
          <p className="text-[10px] text-muted-foreground truncate">{t.sub}</p>
        </div>
      ))}
    </div>
  );
}

// ─── TableRow expansível ──────────────────────────────────────────────────────

function TableNodeRow({
  node, history, histLoading, histStats, trend, capacityRec,
}: {
  node: ConntrackNodeStats;
  history?: ConntrackNodeHistoryResponse;
  histLoading: boolean;
  histStats: HistStats | null;
  trend: Trend;
  capacityRec: CapRec;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-muted/40 transition-colors"
        onClick={() => setExpanded((v) => !v)}
      >
        <TableCell className="py-2">
          <div className="flex items-center gap-1.5">
            {expanded ? <ChevronUp className="h-3 w-3 text-muted-foreground flex-shrink-0" /> : <ChevronDown className="h-3 w-3 text-muted-foreground flex-shrink-0" />}
            <TrendIcon trend={trend} />
            <span className="text-xs font-mono truncate max-w-[160px]" title={node.node_name}>{node.node_name}</span>
          </div>
        </TableCell>
        <TableCell className="py-2">
          <div className="text-xs text-muted-foreground tabular-nums">
            {fmt(node.count)} <span className="text-muted-foreground/60">/ {fmt(node.max)}</span>
          </div>
        </TableCell>
        <TableCell className="py-2">
          <MiniBar pct={node.usage_pct} />
        </TableCell>
        <TableCell className="py-2 text-xs tabular-nums text-center">
          {histStats ? (
            <span style={{ color: barFill(histStats.p95) }}>{histStats.p95.toFixed(1)}%</span>
          ) : histLoading ? (
            <Loader2 className="h-3 w-3 animate-spin mx-auto text-muted-foreground" />
          ) : '—'}
        </TableCell>
        <TableCell className="py-2 text-xs tabular-nums text-center text-muted-foreground">
          {node.buckets > 0 ? fmt(node.buckets) : '—'}
        </TableCell>
        <TableCell className="py-2">
          <StatusBadge status={node.status} />
        </TableCell>
        <TableCell className="py-2">
          <CapacityBadge rec={capacityRec} />
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="bg-muted/20 hover:bg-muted/20">
          <TableCell colSpan={7} className="py-3 px-6">
            <div className="space-y-3">
              <HistoryChart node={node} history={history} histLoading={histLoading} />
              {histStats && (
                <div className="grid grid-cols-4 gap-2 text-xs rounded-md border border-border/60 px-3 py-2 bg-background">
                  {[
                    { label: 'Atual', value: node.usage_pct },
                    { label: 'Média 24h', value: histStats.avg },
                    { label: 'P95', value: histStats.p95 },
                    { label: 'Pico', value: histStats.max },
                  ].map(({ label, value }) => (
                    <div key={label} className="text-center">
                      <p className="text-muted-foreground text-[10px]">{label}</p>
                      <p className="font-semibold tabular-nums" style={{ color: barFill(value) }}>{value.toFixed(1)}%</p>
                    </div>
                  ))}
                </div>
              )}
              <p className="text-[10px] text-muted-foreground">
                via {node.probe_method}{node.buckets > 0 ? ` · buckets: ${fmt(node.buckets)}` : ''}
              </p>
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

// ─── ResizeHDivider ───────────────────────────────────────────────────────────

function ResizeHDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastY = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientY - lastY.current);
      lastY.current = e.clientY;
    };
    const onUp = () => {
      dragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => { window.removeEventListener('mousemove', onMove); window.removeEventListener('mouseup', onUp); };
  }, [onDrag]);

  return (
    <div
      className="h-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-row-resize transition-colors rounded-full"
      onMouseDown={(e) => {
        dragging.current = true;
        lastY.current = e.clientY;
        document.body.style.cursor = 'row-resize';
        document.body.style.userSelect = 'none';
        e.preventDefault();
      }}
    />
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
  const [nodeSearch, setNodeSearch] = useState('');
  const [viewMode, setViewMode] = useState<ViewMode>('table');
  const [sortKey, setSortKey] = useState<SortKey>('usage');
  const [tableHeight, setTableHeight] = useState(320);

  const fetchHistory = async (ns: ConntrackNodeStats[]) => {
    if (!ns.length) return;
    setHistLoading(true);
    const results = await Promise.allSettled(
      ns.map((n) => apiClient.getConntrackNodeHistory(cluster, n.node_name, 24, 30)),
    );
    const map: Record<string, ConntrackNodeHistoryResponse> = {};
    results.forEach((r, i) => { if (r.status === 'fulfilled') map[ns[i].node_name] = r.value; });
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

  // Pré-calcula histStats, trend e capacityRec para cada node
  const nodesMeta = useMemo(() => {
    return nodes.map((n) => {
      const hist = historyMap[n.node_name]?.points ? computeHistStats(historyMap[n.node_name].points) : null;
      return {
        node: n,
        histStats: hist,
        trend: getTrend(n.usage_pct, hist),
        capacityRec: getCapacityRec(n.usage_pct, hist),
      };
    });
  }, [nodes, historyMap]);

  const filteredSorted = useMemo(() => {
    const q = nodeSearch.trim().toLowerCase();
    let result = q ? nodesMeta.filter((m) => m.node.node_name.toLowerCase().includes(q)) : nodesMeta;
    if (sortKey === 'usage') result = [...result].sort((a, b) => b.node.usage_pct - a.node.usage_pct);
    if (sortKey === 'p95') result = [...result].sort((a, b) => (b.histStats?.p95 ?? 0) - (a.histStats?.p95 ?? 0));
    if (sortKey === 'name') result = [...result].sort((a, b) => a.node.node_name.localeCompare(b.node.node_name));
    return result;
  }, [nodesMeta, nodeSearch, sortKey]);

  const cycleSortKey = () => {
    setSortKey((k) => k === 'usage' ? 'p95' : k === 'p95' ? 'name' : 'usage');
  };
  const sortLabel: Record<SortKey, string> = { usage: 'Uso atual', p95: 'P95 24h', name: 'Nome' };

  return (
    <div className="space-y-4 mt-4">
      {/* Cabeçalho */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
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
        <div className="flex items-center gap-2 flex-shrink-0 flex-wrap">
          {nodes.length > 0 && (
            <>
              {/* Ordenação */}
              <Button variant="outline" size="sm" className="h-8 text-xs gap-1.5" onClick={cycleSortKey}>
                <ArrowUpDown className="h-3.5 w-3.5" />
                {sortLabel[sortKey]}
              </Button>

              {/* Toggle view */}
              <div className="flex rounded-md border border-input overflow-hidden">
                <button onClick={() => setViewMode('table')}
                  className={`p-1.5 transition-colors ${viewMode === 'table' ? 'bg-primary text-primary-foreground' : 'bg-background text-muted-foreground hover:bg-muted'}`}
                  title="Tabela">
                  <LayoutList className="h-3.5 w-3.5" />
                </button>
                <button onClick={() => setViewMode('cards')}
                  className={`p-1.5 transition-colors ${viewMode === 'cards' ? 'bg-primary text-primary-foreground' : 'bg-background text-muted-foreground hover:bg-muted'}`}
                  title="Cards">
                  <LayoutGrid className="h-3.5 w-3.5" />
                </button>
              </div>

              {/* Busca */}
              <div className="relative">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
                <Input value={nodeSearch} onChange={(e) => setNodeSearch(e.target.value)}
                  placeholder="Filtrar por nome..." className="pl-8 h-8 text-xs w-44" />
              </div>
            </>
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

      {nodes.length > 0 && (
        <>
          {/* Summary */}
          <SummaryStrip nodes={nodes} histMap={historyMap} />

          {filteredSorted.length === 0 && (
            <div className="text-center py-8 text-muted-foreground text-sm">
              Nenhum node encontrado para "<strong>{nodeSearch}</strong>"
            </div>
          )}

          {/* View: tabela */}
          {viewMode === 'table' && filteredSorted.length > 0 && (
            <div className="space-y-1">
              <div className="rounded-md border border-border overflow-hidden">
                <div style={{ height: tableHeight, overflowY: 'auto' }}>
                  <Table>
                    <TableHeader className="sticky top-0 z-10 bg-background">
                      <TableRow>
                        <TableHead className="text-xs">Node</TableHead>
                        <TableHead className="text-xs">Conexões / Limite</TableHead>
                        <TableHead className="text-xs">Uso atual</TableHead>
                        <TableHead className="text-xs text-center">P95 24h</TableHead>
                        <TableHead className="text-xs text-center">Buckets</TableHead>
                        <TableHead className="text-xs">Status</TableHead>
                        <TableHead className="text-xs">Recomendação</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredSorted.map(({ node, histStats, trend, capacityRec }) => (
                        <TableNodeRow
                          key={node.node_name}
                          node={node}
                          history={historyMap[node.node_name]}
                          histLoading={histLoading}
                          histStats={histStats}
                          trend={trend}
                          capacityRec={capacityRec}
                        />
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </div>
              <ResizeHDivider onDrag={(d) => setTableHeight((h) => Math.max(160, h + d))} />
            </div>
          )}

          {/* View: cards */}
          {viewMode === 'cards' && filteredSorted.length > 0 && (
            <div className="space-y-3">
              {filteredSorted.map(({ node, histStats, trend, capacityRec }) => (
                <NodeCard
                  key={node.node_name}
                  node={node}
                  history={historyMap[node.node_name]}
                  histLoading={histLoading}
                  histStats={histStats}
                  trend={trend}
                  capacityRec={capacityRec}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
