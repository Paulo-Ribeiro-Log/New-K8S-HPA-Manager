import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Loader2, RefreshCw, AlertTriangle, CheckCircle2, XCircle, Activity } from 'lucide-react';
import { apiClient } from '@/lib/api/client';
import type { ConntrackNodeStats } from '@/lib/api/types';

interface ConntrackTabProps {
  cluster: string;
  nodepool: string;
}

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

  const statusBadge = (status: ConntrackNodeStats['status']) => {
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
  };

  const usageBarColor = (pct: number) => {
    if (pct >= 90) return 'bg-red-500';
    if (pct >= 70) return 'bg-yellow-500';
    return 'bg-green-500';
  };

  const fmt = (n: number) =>
    n >= 1_000_000
      ? `${(n / 1_000_000).toFixed(1)}M`
      : n >= 1_000
      ? `${(n / 1_000).toFixed(1)}K`
      : String(n);

  return (
    <div className="space-y-4 mt-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-muted-foreground">
            Conexões ativas rastreadas pelo kernel Linux em cada nó do pool <strong>{nodepool}</strong>.
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
            <Card key={node.node_name} className="overflow-hidden">
              <CardHeader className="py-3 px-4">
                <CardTitle className="text-sm font-mono flex items-center justify-between">
                  <span className="truncate mr-2">{node.node_name}</span>
                  {statusBadge(node.status)}
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
                          className={`h-full rounded-full transition-all ${usageBarColor(node.usage_pct)}`}
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

                    {/* Método de coleta */}
                    <p className="text-xs text-muted-foreground truncate">via {node.probe_method}</p>
                  </>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
