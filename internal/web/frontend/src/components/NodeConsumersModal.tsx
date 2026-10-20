import { Fragment, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ChevronDown, ChevronRight, Loader2, RefreshCcw } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { apiClient } from "@/lib/api/client";
import { formatBytes, formatMillicores } from "@/lib/monitorUtils";
import type { NodeConsumer } from "@/lib/api/types";

interface NodeConsumersModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  nodePool: string;
  nodeName: string | null;
}

type SortBy = "mem" | "cpu";

const TOP_N = 10;

function pct(value: number, total: number): number {
  return total > 0 ? (value / total) * 100 : 0;
}

/** Barra fina: a parte cheia é o uso, o traço é o request. Tudo em % da capacidade alocável do node. */
function UsageBar({ usedPct, requestPct }: { usedPct: number; requestPct: number }) {
  return (
    <div className="relative h-1.5 w-full rounded bg-muted overflow-hidden">
      <div className="absolute inset-y-0 left-0 bg-primary" style={{ width: `${Math.min(usedPct, 100)}%` }} />
      {requestPct > 0 && (
        <div className="absolute inset-y-0 w-px bg-foreground/60" style={{ left: `${Math.min(requestPct, 100)}%` }} />
      )}
    </div>
  );
}

export function NodeConsumersModal({ open, onOpenChange, cluster, nodePool, nodeName }: NodeConsumersModalProps) {
  const [sortBy, setSortBy] = useState<SortBy>("mem");
  const [showAll, setShowAll] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ["node-consumers", cluster, nodePool, nodeName],
    queryFn: () => apiClient.getNodeConsumers(cluster, nodePool, nodeName as string),
    enabled: open && !!nodeName && !!cluster,
    staleTime: 0,
    refetchOnWindowFocus: false,
  });

  const sorted = useMemo(() => {
    const pods = [...(data?.pods ?? [])];
    const key = (p: NodeConsumer) =>
      sortBy === "cpu" ? [p.cpu_usage_m, p.cpu_request_m] : [p.mem_usage_bytes, p.mem_request_bytes];
    pods.sort((a, b) => {
      const [au, ar] = key(a);
      const [bu, br] = key(b);
      return bu - au || br - ar;
    });
    return pods;
  }, [data, sortBy]);

  const visible = showAll ? sorted : sorted.slice(0, TOP_N);
  const cpuAlloc = data?.cpu_allocatable_m ?? 0;
  const memAlloc = data?.mem_allocatable_bytes ?? 0;
  const noMetrics = !!data && !data.metrics_available;

  const handleOpenChange = (o: boolean) => {
    if (!o) {
      setShowAll(false);
      setExpanded(null);
    }
    onOpenChange(o);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-5xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Maiores consumidores — {nodeName}</DialogTitle>
          <DialogDescription>
            Pods do node ordenados pelo consumo atual (Metrics Server). A barra mostra o uso em % da capacidade
            alocável do node; o traço é o request.
          </DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" /> Carregando pods do node...
          </div>
        ) : error ? (
          <div className="py-8 text-center text-destructive">
            <p className="mb-3 text-sm">{error instanceof Error ? error.message : "Erro ao carregar os consumidores"}</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCcw className="w-4 h-4 mr-2" /> Tentar de novo
            </Button>
          </div>
        ) : data ? (
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="rounded border p-3 space-y-1">
                <div className="text-xs text-muted-foreground">CPU do node</div>
                <div className="text-sm tabular-nums">
                  {data.metrics_available ? `${formatMillicores(data.cpu_usage_m)} em uso · ` : ""}
                  {formatMillicores(data.cpu_requested_m)} requisitado · {formatMillicores(cpuAlloc)} alocável
                </div>
                <UsageBar usedPct={pct(data.cpu_usage_m, cpuAlloc)} requestPct={pct(data.cpu_requested_m, cpuAlloc)} />
              </div>
              <div className="rounded border p-3 space-y-1">
                <div className="text-xs text-muted-foreground">Memória do node</div>
                <div className="text-sm tabular-nums">
                  {data.metrics_available ? `${formatBytes(data.mem_usage_bytes)} em uso · ` : ""}
                  {formatBytes(data.mem_requested_bytes)} requisitado · {formatBytes(memAlloc)} alocável
                </div>
                <UsageBar usedPct={pct(data.mem_usage_bytes, memAlloc)} requestPct={pct(data.mem_requested_bytes, memAlloc)} />
              </div>
            </div>

            {noMetrics && (
              <div className="flex items-start gap-2 rounded border border-yellow-500/40 bg-yellow-500/10 p-3 text-xs">
                <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0 text-yellow-600" />
                <span>
                  Metrics Server indisponível neste cluster: sem consumo atual. A lista está ordenada pelo que cada pod
                  reservou (request).
                </span>
              </div>
            )}

            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-1">
                <span className="text-xs text-muted-foreground mr-1">Ordenar por</span>
                <Button size="sm" variant={sortBy === "mem" ? "default" : "outline"} onClick={() => setSortBy("mem")}>
                  Memória
                </Button>
                <Button size="sm" variant={sortBy === "cpu" ? "default" : "outline"} onClick={() => setSortBy("cpu")}>
                  CPU
                </Button>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {visible.length} de {sorted.length} pods
                </span>
                {sorted.length > TOP_N && (
                  <Button size="sm" variant="ghost" onClick={() => setShowAll((v) => !v)}>
                    {showAll ? `Só os ${TOP_N} maiores` : "Ver todos"}
                  </Button>
                )}
                <Button size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching}>
                  <RefreshCcw className={`w-4 h-4 mr-2 ${isFetching ? "animate-spin" : ""}`} /> Atualizar
                </Button>
              </div>
            </div>

            {sorted.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">Nenhum pod neste node.</div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-8 text-xs">#</TableHead>
                    <TableHead className="text-xs">Pod</TableHead>
                    <TableHead className="text-xs min-w-[170px]">CPU</TableHead>
                    <TableHead className="text-xs min-w-[170px]">Memória</TableHead>
                    <TableHead className="text-xs text-center">Restarts</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((p, i) => {
                    const key = `${p.namespace}/${p.name}`;
                    const isOpen = expanded === key;
                    const canExpand = (p.containers?.length ?? 0) > 1;
                    return (
                      <Fragment key={key}>
                        <TableRow
                          className={canExpand ? "cursor-pointer hover:bg-muted/50" : "hover:bg-muted/50"}
                          onClick={canExpand ? () => setExpanded(isOpen ? null : key) : undefined}
                        >
                          <TableCell className="text-xs text-muted-foreground tabular-nums">{i + 1}</TableCell>
                          <TableCell className="max-w-[320px]">
                            <div className="flex items-center gap-1 text-xs font-mono truncate" title={p.name}>
                              {canExpand && (isOpen ? <ChevronDown className="w-3 h-3 shrink-0" /> : <ChevronRight className="w-3 h-3 shrink-0" />)}
                              <span className="truncate">{p.name}</span>
                            </div>
                            <div className="text-[11px] text-muted-foreground truncate">
                              {p.namespace} · {p.workload_kind}/{p.workload}
                              {p.phase !== "Running" && <Badge variant="outline" className="ml-1 text-[10px]">{p.phase}</Badge>}
                            </div>
                          </TableCell>
                          <TableCell className="text-xs space-y-1">
                            <div className="tabular-nums">
                              {p.has_metrics ? <strong>{formatMillicores(p.cpu_usage_m)}</strong> : <span className="text-muted-foreground">sem métrica</span>}
                              <span className="text-muted-foreground">
                                {" "}· req {formatMillicores(p.cpu_request_m)}
                                {p.cpu_limit_m > 0 && ` · lim ${formatMillicores(p.cpu_limit_m)}`}
                              </span>
                            </div>
                            <UsageBar usedPct={pct(p.cpu_usage_m, cpuAlloc)} requestPct={pct(p.cpu_request_m, cpuAlloc)} />
                          </TableCell>
                          <TableCell className="text-xs space-y-1">
                            <div className="tabular-nums">
                              {p.has_metrics ? <strong>{formatBytes(p.mem_usage_bytes)}</strong> : <span className="text-muted-foreground">sem métrica</span>}
                              <span className="text-muted-foreground">
                                {" "}· req {formatBytes(p.mem_request_bytes)}
                                {p.mem_limit_bytes > 0 && ` · lim ${formatBytes(p.mem_limit_bytes)}`}
                              </span>
                            </div>
                            <UsageBar usedPct={pct(p.mem_usage_bytes, memAlloc)} requestPct={pct(p.mem_request_bytes, memAlloc)} />
                          </TableCell>
                          <TableCell className="text-xs text-center tabular-nums">{p.restart_count}</TableCell>
                        </TableRow>
                        {isOpen &&
                          p.containers?.map((c) => (
                            <TableRow key={`${key}/${c.name}`} className="bg-muted/30">
                              <TableCell />
                              <TableCell className="text-[11px] font-mono text-muted-foreground pl-6">↳ {c.name}</TableCell>
                              <TableCell className="text-[11px] tabular-nums">{formatMillicores(c.cpu_usage_m)}</TableCell>
                              <TableCell className="text-[11px] tabular-nums">{formatBytes(c.mem_usage_bytes)}</TableCell>
                              <TableCell />
                            </TableRow>
                          ))}
                      </Fragment>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
