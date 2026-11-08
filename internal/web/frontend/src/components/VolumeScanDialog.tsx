import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Bell, Loader2, RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { apiClient, type ScanResult, type VolumeScanState } from "@/lib/api/client";

// Varredura periódica de volumes: o servidor lê os clusters marcados a cada 30 min (enquanto estiver
// ligado) e guarda o que merece atenção. Aqui se escolhe os clusters e se vê o último resultado.

const QUERY_KEY = ["volume-scan"];

/** Estado da varredura, atualizado a cada minuto; falha em silêncio (o selo só some). */
export function useVolumeScan() {
  return useQuery<VolumeScanState>({
    queryKey: QUERY_KEY,
    queryFn: () => apiClient.getVolumeScan(),
    refetchInterval: (q) => (q.state.data?.scanning ? 4_000 : 60_000),
    retry: false,
    refetchOnWindowFocus: false,
  });
}

/** Soma o que pede atenção: volumes enchendo e ociosos antigos (sem snapshot é só informativo). */
export function scanAttention(state?: VolumeScanState): { total: number; fresh: number } {
  let total = 0, fresh = 0;
  for (const r of state?.results ?? []) {
    total += r.counts.critical + r.counts.idle_old;
    fresh += r.new_count;
  }
  return { total, fresh };
}

const KIND_LABEL: Record<string, { text: string; cls: string }> = {
  critical: { text: "Enchendo", cls: "text-red-400" },
  idle_old: { text: "Ocioso antigo", cls: "text-amber-300" },
  no_snapshot: { text: "Sem snapshot", cls: "text-muted-foreground" },
};

function ago(iso: string): string {
  const m = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  return m < 1 ? "agora" : m < 60 ? `há ${m} min` : m < 1440 ? `há ${Math.round(m / 60)} h` : `há ${Math.round(m / 1440)} d`;
}

interface Props {
  clusters: { context: string; name: string }[];
  onClose: () => void;
  onOpenCluster: (context: string) => void;
}

export function VolumeScanDialog({ clusters, onClose, onOpenCluster }: Props) {
  const qc = useQueryClient();
  const { data, isLoading, error } = useVolumeScan();
  const [selected, setSelected] = useState<string[]>([]);
  const [dirty, setDirty] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [onlyNew, setOnlyNew] = useState(false);

  useEffect(() => {
    if (data && !dirty) setSelected(data.watched);
  }, [data, dirty]);

  const save = useMutation({
    mutationFn: () => apiClient.setVolumeScanWatch(selected),
    onSuccess: () => { setDirty(false); void qc.invalidateQueries({ queryKey: QUERY_KEY }); },
  });
  const run = useMutation({
    mutationFn: async () => { await apiClient.runVolumeScan(); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: QUERY_KEY }); },
  });

  function toggle(ctx: string, on: boolean) {
    setDirty(true);
    setSelected((cur) => (on ? [...new Set([...cur, ctx])] : cur.filter((c) => c !== ctx)));
  }

  const nameOf = (ctx: string) => clusters.find((c) => c.context === ctx)?.name ?? ctx;
  const results: ScanResult[] = data?.results ?? [];

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-3xl max-h-[88vh] overflow-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm"><Bell className="w-4 h-4" />Varredura de volumes</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 text-xs">
          <p className="text-muted-foreground">
            A cada {data?.interval_minutes ?? 30} min, <b>enquanto o servidor estiver ligado</b> (com o navegador aberto — ele se desliga sozinho sem aba aberta), os clusters marcados são lidos
            com uso do Prometheus e o resultado fica aqui. Avisa: volume <b>enchendo</b> (crítico), <b>ocioso antigo</b> (&gt; 30 dias) e, só como informação, <b>sem snapshot</b>. Somente leitura.
          </p>

          {isLoading && <div className="flex items-center gap-2 text-muted-foreground"><Loader2 className="w-3.5 h-3.5 animate-spin" />Carregando…</div>}
          {error && <div className="text-red-400">{error instanceof Error ? error.message : "Erro ao carregar a varredura"}</div>}
          {data && !data.enabled && <div className="text-amber-300">A varredura não está ligada neste servidor (o banco local não abriu).</div>}

          {data?.enabled && (
            <>
              <div className="rounded border border-border/40 p-2 space-y-1.5">
                <div className="font-medium">Clusters varridos</div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1 max-h-40 overflow-auto">
                  {clusters.map((c) => (
                    <label key={c.context} className="flex items-center gap-1.5 cursor-pointer">
                      <Checkbox checked={selected.includes(c.context)} onCheckedChange={(v) => toggle(c.context, v === true)} />
                      <span className="font-mono truncate" title={c.context}>{c.name}</span>
                    </label>
                  ))}
                </div>
                <div className="flex items-center gap-2 pt-1">
                  <Button size="sm" className="h-7 text-[11px]" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
                    {save.isPending && <Loader2 className="w-3 h-3 animate-spin mr-1" />}Salvar lista
                  </Button>
                  <Button variant="outline" size="sm" className="h-7 text-[11px] gap-1" disabled={dirty || run.isPending || data.scanning || data.watched.length === 0} onClick={() => run.mutate()}
                    title={dirty ? "Salve a lista antes" : "Varre agora, sem esperar o próximo ciclo"}>
                    {data.scanning ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}{data.scanning ? "Varrendo…" : "Varrer agora"}
                  </Button>
                  <label className="flex items-center gap-1.5 cursor-pointer ml-auto">
                    <Checkbox checked={onlyNew} onCheckedChange={(v) => setOnlyNew(v === true)} />Só novos desde a última varredura
                  </label>
                </div>
                {(save.error || run.error) && <div className="text-red-400">{String((save.error ?? run.error) instanceof Error ? (save.error ?? run.error)!.message : "Falha")}</div>}
              </div>

              {data.watched.length === 0 && <div className="text-muted-foreground">Marque ao menos um cluster e salve para começar. A primeira varredura roda cerca de 1 minuto depois.</div>}
              {data.watched.length > 0 && results.length === 0 && <div className="text-muted-foreground">Ainda sem resultado. Use “Varrer agora”.</div>}

              {results.map((r) => {
                const list = r.findings.filter((f) => !onlyNew || f.new);
                const open = expanded === r.cluster;
                return (
                  <div key={r.cluster} className="rounded border border-border/40">
                    <div className="flex flex-wrap items-center gap-2 px-2 py-1.5">
                      <button className="font-mono font-medium hover:underline" onClick={() => setExpanded(open ? null : r.cluster)}>{nameOf(r.cluster)}</button>
                      <span className="text-muted-foreground">{ago(r.scanned_at)}</span>
                      <span className={r.counts.critical ? "text-red-400" : "text-muted-foreground"}><b>{r.counts.critical}</b> enchendo</span>
                      <span className={r.counts.idle_old ? "text-amber-300" : "text-muted-foreground"}><b>{r.counts.idle_old}</b> ociosos antigos</span>
                      <span className="text-muted-foreground"><b>{r.counts.no_snapshot}</b> sem snapshot</span>
                      {r.new_count > 0 && <Badge variant="destructive" className="text-[10px]">{r.new_count} novo(s)</Badge>}
                      <Button variant="outline" size="sm" className="h-6 px-2 text-[11px] ml-auto" onClick={() => onOpenCluster(r.cluster)}>Abrir Volumes</Button>
                    </div>
                    {r.error && (
                      <div className="flex items-start gap-1.5 px-2 pb-1.5 text-amber-300"><AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" /><span className="[overflow-wrap:anywhere]">Última leitura falhou: {r.error}{r.findings.length ? " (mostrando o último estado conhecido)" : ""}</span></div>
                    )}
                    {r.usage_warning && !r.error && <div className="px-2 pb-1.5 text-amber-300">Uso indisponível ({r.usage_warning}): “enchendo” pode estar incompleto.</div>}
                    {open && (
                      <div className="border-t border-border/30 px-2 py-1.5 max-h-60 overflow-auto">
                        {list.length === 0 ? <div className="text-muted-foreground">Nada com esse filtro.</div> : (
                          <table className="w-full">
                            <tbody>
                              {list.map((f) => (
                                <tr key={`${f.kind}|${f.namespace}|${f.name}`} className="border-b border-border/10 align-top">
                                  <td className={`py-0.5 pr-2 whitespace-nowrap ${KIND_LABEL[f.kind]?.cls ?? ""}`}>{KIND_LABEL[f.kind]?.text ?? f.kind}{f.new && " •"}</td>
                                  <td className="py-0.5 pr-2 font-mono [overflow-wrap:anywhere]">{f.namespace ? `${f.namespace}/` : ""}{f.name}</td>
                                  <td className="py-0.5 text-muted-foreground">{f.detail}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                        {r.truncated && <div className="pt-1 text-muted-foreground">Lista cortada nos primeiros 200 achados; abra o modal Volumes para ver tudo.</div>}
                      </div>
                    )}
                  </div>
                );
              })}
            </>
          )}
        </div>
        <div className="flex justify-end"><Button variant="outline" size="sm" onClick={onClose}>Fechar</Button></div>
      </DialogContent>
    </Dialog>
  );
}
