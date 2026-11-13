import { useState } from "react";
import { ChevronDown, ChevronRight, ListFilter } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { allKeys, clusterState, describeScope, scopeKey, toggleCluster, toggleNamespace, type ScopeCluster } from "@/lib/metricsExportScope";

/**
 * Escolhe o que vai para o Markdown, o CSV e o PDF: tudo o que foi coletado, só um cluster ou só alguns namespaces.
 * Não muda a tabela da tela nem o que foi coletado, só o documento exportado.
 */
export function MetricsExportScopePicker({ tree, selected, onChange }: { tree: ScopeCluster[]; selected: Set<string>; onChange: (next: Set<string>) => void }) {
  const [open, setOpen] = useState<Set<string>>(new Set()); // clusters com os namespaces à mostra
  const sum = describeScope(tree, selected);
  const label = sum.partial ? `${sum.clusters} de ${sum.totalClusters} cluster(s) · ${sum.apps} de ${sum.totalApps} aplicações` : `tudo (${sum.totalClusters} cluster(s), ${sum.totalApps} aplicações)`;
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant={sum.partial ? "default" : "outline"} className="h-7 text-xs gap-1" title="Escolher o que vai para o Markdown, o CSV e o PDF: tudo, um cluster ou só alguns namespaces">
          <ListFilter className="h-3.5 w-3.5" />Escopo: {label}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[26rem] space-y-2 p-3 text-xs">
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" className="h-6 text-[11px]" onClick={() => onChange(allKeys(tree))}>Marcar tudo</Button>
          <Button size="sm" variant="outline" className="h-6 text-[11px]" onClick={() => onChange(new Set())}>Limpar</Button>
          <span className="text-[10px] text-muted-foreground">vale só para a exportação</span>
        </div>
        <ul className="max-h-72 space-y-1 overflow-y-auto">
          {tree.map((c) => {
            const state = clusterState(tree, selected, c.cluster);
            const expanded = open.has(c.cluster);
            return (
              <li key={c.cluster}>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    title={expanded ? "Esconder os namespaces" : "Escolher os namespaces"}
                    onClick={() => setOpen((p) => { const n = new Set(p); if (!n.delete(c.cluster)) n.add(c.cluster); return n; })}
                  >
                    {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                  </button>
                  <Checkbox checked={state === "all" ? true : state === "some" ? "indeterminate" : false} onCheckedChange={(v) => onChange(toggleCluster(tree, selected, c.cluster, v === true))} aria-label={`Cluster ${c.cluster}`} />
                  <span className="min-w-0 flex-1 truncate font-medium" title={c.cluster}>{c.cluster}</span>
                  <span className="text-muted-foreground">{c.count} aplicação(ões)</span>
                </div>
                {expanded && (
                  <ul className="ml-7 mt-1 space-y-0.5">
                    {c.namespaces.map((n) => (
                      <li key={n.namespace} className="flex items-center gap-1.5">
                        <Checkbox checked={selected.has(scopeKey(c.cluster, n.namespace))} onCheckedChange={(v) => onChange(toggleNamespace(selected, c.cluster, n.namespace, v === true))} aria-label={`Namespace ${n.namespace}`} />
                        <span className="min-w-0 flex-1 truncate" title={n.namespace}>{n.namespace}</span>
                        <span className="text-muted-foreground">{n.count}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
        {sum.apps === 0 && <div className="text-amber-400">Marque ao menos um cluster ou namespace para exportar.</div>}
      </PopoverContent>
    </Popover>
  );
}
