import { Fragment, memo, type ReactNode } from "react";
import { ArrowDown, ArrowUp, BarChart3, ChevronDown, ChevronRight, X } from "lucide-react";
import { LEVEL_LABEL, errorLevel, fmtRestartReasons, latencyLevel, restartsLevel, usageLevel, worstLevel, type LiveSeries } from "@/lib/metricsLive";
import { fmtInt, fmtLatency, fmtRps, type ReportItem } from "@/lib/metricsReport";
import type { LiveRowModel, SortKey, SortState } from "@/lib/metricsLiveView";
import { LEVEL_CLASS, LevelIcon, Spark } from "@/components/metricsLiveShared";

const NUM = "px-2 py-1.5 text-right whitespace-nowrap tabular-nums";
const dash = <span className="text-muted-foreground">—</span>;
const pct = (v: number | null) => (v == null ? null : `${v.toLocaleString("pt-BR", { maximumFractionDigits: 0 })}%`);

interface Col {
  key: SortKey | null;
  label: string;
  title?: string;
  align?: "left" | "right";
}

/**
 * Tabela densa do Tempo real: uma linha por aplicação (situação, RPS com mini-barras, response time médio,
 * latência, erros, réplicas, CPU, memória e restarts), agrupada por cluster. Pensada para varrer muitas
 * aplicações de uma vez; clicar na linha abre o cartão completo logo abaixo.
 */
export function MetricsLiveTable({
  groups, sort, onSort, collapsed, onToggleCluster, expanded, onToggleRow, renderExpanded, onDetail, onRemove, percentiles, showResources,
}: {
  groups: { cluster: string; rows: LiveRowModel[] }[];
  sort: SortState;
  onSort: (key: SortKey) => void;
  collapsed: Set<string>;
  onToggleCluster: (cluster: string) => void;
  expanded: Set<string>;
  onToggleRow: (key: string) => void;
  renderExpanded: (s: LiveSeries) => ReactNode;
  onDetail: (s: LiveSeries) => void;
  onRemove: (item: ReportItem) => void;
  /** Percentis coletados: uma coluna para cada. */
  percentiles: number[];
  showResources: boolean;
}) {
  const pcts = [...percentiles].sort((a, b) => a - b).filter((p): p is 50 | 95 | 99 => p === 50 || p === 95 || p === 99);
  const cols: Col[] = [
    { key: "gravidade", label: "Situação", title: "Pior situação da aplicação (latência, erros, réplicas, HPA, restarts, throttling, CPU e memória)" },
    { key: "nome", label: "Aplicação" },
    { key: "rps", label: "RPS", align: "right", title: "Requisições por segundo agora, com as barras do período" },
    { key: "avg", label: "Response time", align: "right", title: "Response time médio (ponderado pelas requisições)" },
    // a cor segue a regra da situação, que usa só o MAIOR percentil marcado: pintar também o P50 e o P95 com limites
    // pensados para o P99 faria um P50 de 600 ms parecer problema sem que a aplicação esteja em alerta
    ...pcts.map((p): Col => ({
      key: p === 99 ? "p99" : p === 95 ? "p95" : "p50",
      label: `P${p}`,
      align: "right",
      title: p === pcts[pcts.length - 1] ? `Latência P${p} agora. Cor: acima de 0,5 s é alerta e de 1 s é erro (é o percentil que define a situação)` : `Latência P${p} agora. Sem cor: os limites de 0,5 s e 1 s valem para o P${pcts[pcts.length - 1]}, o maior percentil marcado`,
    })),
    { key: "err5", label: "5xx", align: "right", title: "Erros 5xx no período (e por segundo agora)" },
    { key: "err4", label: "4xx", align: "right", title: "Erros 4xx no período (e por segundo agora)" },
    ...(showResources
      ? ([
          { key: "replicas", label: "Réplicas", align: "right", title: "Disponíveis / desejadas; teto = desejadas no máximo do HPA" },
          { key: "cpu", label: "CPU", align: "right", title: "% do limit" },
          { key: "mem", label: "Mem", align: "right", title: "% do limit" },
          { key: null, label: "Restarts", align: "right", title: "Restarts nos últimos 15 min" },
        ] as Col[])
      : []),
    { key: null, label: "" },
  ];
  const total = cols.length;

  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table className="w-full text-xs">
        <thead className="sticky top-0 z-10 bg-muted/80 text-left backdrop-blur">
          <tr>
            {cols.map((c, i) => {
              const active = c.key != null && sort.key === c.key;
              return (
                <th key={i} className={`px-2 py-1.5 font-medium whitespace-nowrap ${c.align === "right" ? "text-right" : ""}`} aria-sort={active ? (sort.desc ? "descending" : "ascending") : undefined}>
                  {c.key ? (
                    <button type="button" title={c.title ?? `Ordenar por ${c.label}`} className={`inline-flex items-center gap-1 hover:text-foreground ${active ? "text-foreground" : "text-muted-foreground"}`} onClick={() => onSort(c.key as SortKey)}>
                      {c.label}
                      {active && (sort.desc ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />)}
                    </button>
                  ) : (
                    <span className="text-muted-foreground" title={c.title}>{c.label}</span>
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => {
            const closed = collapsed.has(g.cluster);
            const worst = worstLevel(g.rows.map((r) => r.level));
            return (
              <Fragment key={g.cluster}>
                <tr className="border-t border-border bg-muted/30">
                  <td colSpan={total} className="px-2 py-1">
                    <button type="button" className="flex items-center gap-1.5 text-xs font-medium text-amber-500 hover:text-amber-400" title={closed ? "Expandir o cluster" : "Minimizar o cluster"} onClick={() => onToggleCluster(g.cluster)}>
                      {closed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                      {g.cluster}
                      <span className="text-[11px] font-normal text-muted-foreground">({g.rows.length} aplicação(ões))</span>
                      {closed && <span className={`inline-flex items-center gap-1 text-[11px] font-medium ${LEVEL_CLASS[worst]}`}><LevelIcon level={worst} className="h-3 w-3" />{LEVEL_LABEL[worst]}</span>}
                    </button>
                  </td>
                </tr>
                {!closed &&
                  g.rows.map((r) => (
                    <TableRow key={r.key} row={r} open={expanded.has(r.key)} showResources={showResources} pcts={pcts} total={total} onToggle={onToggleRow} renderExpanded={renderExpanded} onDetail={onDetail} onRemove={onRemove} />
                  ))}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

const TableRow = memo(function TableRow({
  row: r, open, showResources, pcts, total, onToggle, renderExpanded, onDetail, onRemove,
}: {
  row: LiveRowModel;
  open: boolean;
  showResources: boolean;
  pcts: (50 | 95 | 99)[];
  total: number;
  onToggle: (key: string) => void;
  renderExpanded: (s: LiveSeries) => ReactNode;
  onDetail: (s: LiveSeries) => void;
  onRemove: (item: ReportItem) => void;
}) {
  const s = r.s;
  const failed = !!s.error && !s.resources;
  const why = r.reasons.length ? r.reasons.join("\n") : LEVEL_LABEL[r.level];
  return (
    <>
      <tr className="cursor-pointer border-t border-border/60 hover:bg-muted/30" onClick={() => onToggle(r.key)} aria-expanded={open}>
        <td className="px-2 py-1.5" title={why}>
          <span className="inline-flex items-center gap-1.5">
            {open ? <ChevronDown className="h-3 w-3 text-muted-foreground" /> : <ChevronRight className="h-3 w-3 text-muted-foreground" />}
            <LevelIcon level={r.level} />
          </span>
        </td>
        <td className="px-2 py-1.5">
          <div className="font-medium text-foreground">{s.workload}</div>
          <div className="max-w-[18rem] truncate text-[10px] text-muted-foreground" title={failed ? s.error : `ns: ${s.namespace}${r.reasons.length ? ` · ${r.reasons.join("; ")}` : ""}`}>
            {failed ? <span className="text-red-400">{s.error}</span> : <>ns: {s.namespace}{r.reasons.length > 0 && <span className={LEVEL_CLASS[r.level]}> · {r.reasons[0]}{r.reasons.length > 1 ? ` (+${r.reasons.length - 1})` : ""}</span>}</>}
          </div>
        </td>
        <td className={NUM}>
          {failed ? dash : (
            <span className="inline-flex items-center justify-end gap-2">
              <Spark times={s.times} values={s.rps} compact />
              <b className="min-w-[3.5rem] text-right text-foreground">{fmtRps(r.rps)}</b>
            </span>
          )}
        </td>
        <td className={NUM}>{r.avg == null ? dash : fmtLatency(r.avg)}</td>
        {pcts.map((p) => {
          const v = p === 99 ? r.p99 : p === 95 ? r.p95 : r.p50;
          const primary = p === pcts[pcts.length - 1];
          return <td key={p} className={`${NUM} ${v == null || !primary ? "" : LEVEL_CLASS[latencyLevel(v)]}`}>{v == null ? dash : <b>{fmtLatency(v)}</b>}</td>;
        })}
        <td className={`${NUM} ${errorLevel(r.e5Rate, "5xx") === "red" ? LEVEL_CLASS.red : ""}`}>
          {r.e5Total == null ? dash : <><b>{fmtInt(r.e5Total)}</b><div className="text-[10px] font-normal text-muted-foreground">{fmtRps(r.e5Rate)}/s</div></>}
        </td>
        <td className={`${NUM} ${errorLevel(r.e4Rate, "4xx") === "warn" ? LEVEL_CLASS.warn : ""}`}>
          {r.e4Total == null ? dash : <><b>{fmtInt(r.e4Total)}</b><div className="text-[10px] font-normal text-muted-foreground">{fmtRps(r.e4Rate)}/s</div></>}
        </td>
        {showResources && (
          <>
            <td className={`${NUM} ${r.repAvail != null && r.repDesired != null && r.repAvail < r.repDesired ? LEVEL_CLASS.red : ""}`}>
              {r.repAvail == null ? dash : (
                <>
                  <b>{r.repAvail}</b>/{r.repDesired ?? "?"}
                  {r.atCeiling && <span className="ml-1 rounded border border-amber-500/50 px-1 text-[10px] text-amber-400" title={`Réplicas desejadas no máximo do HPA (${r.hpaMax}): não escala mais`}>teto</span>}
                </>
              )}
            </td>
            <td className={`${NUM} ${r.cpuLim != null ? LEVEL_CLASS[usageLevel(r.cpuLim)] : ""}`}>{pct(r.cpuLim) ?? dash}</td>
            <td className={`${NUM} ${r.memLim != null ? LEVEL_CLASS[usageLevel(r.memLim)] : ""}`}>{pct(r.memLim) ?? dash}</td>
            <td className={`${NUM} ${r.restarts != null ? LEVEL_CLASS[restartsLevel(r.restarts)] : ""}`} title={fmtRestartReasons(s.resources?.restart_reasons) || undefined}>
              {r.restarts == null ? dash : r.restarts}
            </td>
          </>
        )}
        <td className="px-2 py-1.5 text-right whitespace-nowrap" onClick={(e) => e.stopPropagation()}>
          <button type="button" className="mr-1 text-muted-foreground hover:text-foreground" title="Abrir os gráficos completos" onClick={() => onDetail(s)}><BarChart3 className="h-3.5 w-3.5" /></button>
          <button type="button" className="text-muted-foreground hover:text-red-400" title="Remover do acompanhamento (também desmarca da seleção do painel)" onClick={() => onRemove({ cluster: s.cluster, namespace: s.namespace, workload: s.workload })}><X className="h-3.5 w-3.5" /></button>
        </td>
      </tr>
      {open && (
        <tr className="bg-muted/20">
          <td colSpan={total} className="p-2">{renderExpanded(s)}</td>
        </tr>
      )}
    </>
  );
});
