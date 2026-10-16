// Aba Relatório do FinOps: visão executiva do cluster — para onde vai o dinheiro, eficiência,
// tendência, maiores custos, plano de ação com economia, riscos e qualidade dos dados — e o mesmo
// conteúdo em PDF. Os números vêm de capacity.ts (mesma conta do Node Pools e do rateio do backend).
import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  PieChart, Pie, Cell, Tooltip, ResponsiveContainer, Treemap, BarChart, Bar, XAxis, YAxis,
  CartesianGrid, AreaChart, Area, LineChart, Line, ReferenceArea,
} from "recharts";
import {
  Download, Loader2, CircleDollarSign, CalendarRange, PiggyBank, Gauge, Wallet, Activity,
  LayoutGrid, BarChart3, ListChecks, ShieldAlert, Info, Server, CheckCircle2,
} from "lucide-react";
import { toast } from "sonner";
import { fmtBRL } from "@/lib/finopsFormat";
import { apiClient } from "@/lib/api/client";
import type { FinOpsReport, FinOpsWorkload } from "./types";
import { buildActionPlan, computeCostBreakdown, computeEfficiency, computePoolUsage, poolCostFactors } from "./capacity";
import { AXIS_TICK, COST_COLORS, RingGauge, SectionTitle, TooltipBox, TooltipRow } from "./charts";
import { BilledCostPanel, billedToBRL, useBilledCost } from "./BilledCostPanel";

interface Risk {
  count: number;
  title: string;
  detail: string;
  tone: "bad" | "warn" | "info";
}

const PRIORITY = {
  critical: { label: "Crítico", cls: "bg-rose-500/10 text-rose-700 dark:text-rose-300", bar: "#f43f5e", rgb: [225, 29, 72] },
  high:     { label: "Alto",    cls: "bg-amber-500/10 text-amber-700 dark:text-amber-300", bar: "#f59e0b", rgb: [217, 119, 6] },
  medium:   { label: "Médio",   cls: "bg-sky-500/10 text-sky-700 dark:text-sky-300", bar: "#0ea5e9", rgb: [2, 132, 199] },
} as const;

const RISK_TONE = {
  bad: "border-rose-500/30 bg-rose-500/5",
  warn: "border-amber-500/30 bg-amber-500/5",
  info: "border-border bg-muted/30",
};

const shortBRL = (v: number) => (v >= 1000 ? `R$ ${(v / 1000).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} mil` : fmtBRL(v));
const pct = (a: number, b: number) => (b > 0 ? Math.round((a / b) * 100) : 0);
const trunc = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

// Cor do namespace pela fração de desperdício.
function wasteColor(ratio: number): string {
  if (ratio >= 0.35) return COST_COLORS.idle;
  if (ratio >= 0.15) return COST_COLORS.waste;
  return COST_COLORS.productive;
}

// Célula do treemap de namespaces.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function NamespaceCell(props: any) {
  const { x, y, width, height, name, value, ratio, depth } = props;
  // O Treemap também desenha o nó raiz (depth 0), sem nome.
  if (depth !== 1 || width <= 0 || height <= 0) return null;
  const fill = wasteColor(ratio ?? 0);
  return (
    <g>
      <rect x={x} y={y} width={width} height={height} rx={8} fill={fill} fillOpacity={0.88} stroke="hsl(var(--card))" strokeWidth={3} />
      {width > 70 && height > 34 && (
        <>
          <text x={x + 9} y={y + 20} fontSize={11} fontWeight={600} fill="#fff">{trunc(String(name), Math.floor(width / 7))}</text>
          <text x={x + 9} y={y + 35} fontSize={10} fill="#fff" fillOpacity={0.85}>{shortBRL(value)}</text>
        </>
      )}
    </g>
  );
}

function Panel({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <Card className={`shadow-sm ${className}`}><CardContent className="p-5">{children}</CardContent></Card>;
}

export function RelatorioTab({ report, cluster, provider }: { report: FinOpsReport; windowDays: number; cluster: string; provider?: string }) {
  const { summary } = report;
  const workloads = report.workloads ?? [];
  const pools = report.node_pools ?? [];
  const [exporting, setExporting] = useState(false);
  const [activeSlice, setActiveSlice] = useState<string | null>(null);

  const cost = computeCostBreakdown(report);
  const eff = computeEfficiency(pools, workloads);
  const { pools: poolUsage } = computePoolUsage(pools, workloads);
  // Pools com cobertura do Cost Management: custo efetivo (com reserva/Savings Plan) × tabela.
  const covered = poolUsage.filter(u => u.effectiveBRL !== null);
  const coveredTable = covered.reduce((s, u) => s + u.costBRL, 0);
  const coveredEffective = covered.reduce((s, u) => s + (u.effectiveBRL ?? 0), 0);
  const reservedPools = pools.filter(p => (p.coverage?.reservation ?? 0) >= 0.2 || (p.coverage?.savings_plan ?? 0) >= 0.2);
  const hasMetrics = workloads.some(w => (w.cpu_p95_millis ?? 0) > 0 || (w.mem_p95_mi ?? 0) > 0);

  // ── Para onde vai o dinheiro ──────────────────────────────────────────────
  const slices = [
    { key: "productive", name: hasMetrics ? "Em uso pelos workloads" : "Reservado pelos workloads", value: cost.productiveBRL, color: COST_COLORS.productive,
      hint: hasMetrics ? "request compatível com o uso real" : "sem métrica de uso: não dá para separar o desperdício" },
    { key: "waste", name: "Request acima do uso", value: cost.wasteBRL, color: COST_COLORS.waste, hint: "reservado e não usado (P95 + 20%)" },
    { key: "idle", name: "Capacidade ociosa", value: cost.idleBRL, color: COST_COLORS.idle, hint: "nodes pagos que nem request nem uso ocupam (pelo recurso mais cheio de cada pool)" },
    { key: "os", name: "Disco do SO", value: cost.osDiskBRL, color: COST_COLORS.osDisk, hint: "discos dos nodes" },
    { key: "pvc", name: "Volumes (PVC)", value: cost.pvcBRL, color: COST_COLORS.pvc, hint: "PVCs montados" },
    { key: "orphan", name: "Volumes órfãos", value: cost.orphanBRL, color: COST_COLORS.orphan, hint: "PVCs sem pod" },
  ].filter(s => s.value >= 1);
  const sliceTotal = slices.reduce((s, x) => s + x.value, 0);
  const hovered = slices.find(s => s.key === activeSlice) ?? null;

  // ── Plano de ação (economia) ──────────────────────────────────────────────
  const { actions, totalSaving } = buildActionPlan(report);
  const savingPct = pct(totalSaving, cost.totalBRL);

  // ── Riscos e pontos de atenção ────────────────────────────────────────────
  const count = (v: FinOpsWorkload["verdict"]) => workloads.filter(w => w.verdict === v).length;
  const fallbackPools = pools.filter(p => p.price_source !== "api").length;
  const risks: Risk[] = [
    { count: count("oom_risk"), title: "Risco de OOM ou throttling", detail: "uso P95 encostado no request — subir o request antes de reduzir qualquer coisa", tone: "bad" as const },
    { count: count("no_request"), title: "Sem requests definidos", detail: "o scheduler não reserva recurso e o custo é estimado", tone: "warn" as const },
    { count: count("fixed_high_cost"), title: "Custo alto com réplicas fixas", detail: "candidatos a HPA", tone: "warn" as const },
    { count: count("hpa_removable"), title: "HPA que nunca escalou", detail: "min e max sem efeito prático no período", tone: "info" as const },
    { count: summary.no_data_count ?? count("sem_dados"), title: "Sem métrica de uso", detail: "desperdício não calculado para estes workloads", tone: "info" as const },
    { count: fallbackPools, title: "Pools com preço estimado", detail: "a API de preços não respondeu para o SKU", tone: "info" as const },
  ].filter(r => r.count > 0);

  // ── Namespaces e workloads ────────────────────────────────────────────────
  // Custo e desperdício dos workloads no custo efetivo do pool deles (quando há cobertura).
  const factorOf = poolCostFactors(report);
  const nsWaste = new Map<string, number>();
  const nsCost = new Map<string, number>();
  workloads.forEach(w => {
    const f = factorOf(w.node_pool);
    nsWaste.set(w.namespace, (nsWaste.get(w.namespace) ?? 0) + (w.waste_brl ?? 0) * f);
    nsCost.set(w.namespace, (nsCost.get(w.namespace) ?? 0) + w.cost_share_brl * f);
  });
  const nsData = [...(report.namespaces ?? [])]
    .map(n => ({ ...n, monthly_cost_brl: nsCost.get(n.namespace) ?? n.monthly_cost_brl }))
    .filter(n => n.monthly_cost_brl > 0)
    .sort((a, b) => b.monthly_cost_brl - a.monthly_cost_brl)
    .slice(0, 18)
    .map(n => ({ name: n.namespace, size: Math.round(n.monthly_cost_brl), waste: nsWaste.get(n.namespace) ?? 0, ratio: (nsWaste.get(n.namespace) ?? 0) / n.monthly_cost_brl, workloads: n.workload_count }));

  const topWorkloads = workloads
    .map(w => {
      const f = factorOf(w.node_pool);
      const compute = w.cost_share_brl * f;
      const waste = Math.min((w.waste_brl ?? 0) * f, compute);
      return { name: w.workload, ns: w.namespace, needed: Math.round(compute - waste), waste: Math.round(waste), storage: Math.round(w.storage_cost_brl ?? 0), total: compute + (w.storage_cost_brl ?? 0) };
    })
    .sort((a, b) => b.total - a.total)
    .slice(0, 10);

  // ── Tendência (mesma query do Dashboard/Node Pools) ───────────────────────
  const { data: tl } = useQuery({
    queryKey: ["finops-timeline-dashboard", cluster, 30],
    queryFn: () => apiClient.getFinOpsTimeline(cluster, 30),
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  const nodeTrend = (tl?.nodes ?? []).map(n => ({ date: n.date.slice(5), nodes: n.node_count }));
  const effByDate = new Map<string, { date: string; cpu?: number; mem?: number }>();
  (tl?.cpu ?? []).forEach(p => { if (p.req_millis > 0) effByDate.set(p.date, { ...(effByDate.get(p.date) ?? { date: p.date.slice(5) }), cpu: pct(p.used_millis, p.req_millis) }); });
  (tl?.mem ?? []).forEach(p => { if (p.req_mi > 0) effByDate.set(p.date, { ...(effByDate.get(p.date) ?? { date: p.date.slice(5) }), mem: pct(p.used_mi, p.req_mi) }); });
  const effTrend = [...effByDate.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([, v]) => v);

  // ── Fatura (Cost Management) — a mesma consulta do painel, para o PDF ────
  const { data: billed } = useBilledCost(cluster, provider === "aks");
  const billedFactor = billedToBRL(billed, report.exchange_rate);

  // ── Qualidade dos dados ───────────────────────────────────────────────────
  const enriched = summary.metrics_workloads_enriched ?? workloads.filter(w => (w.cpu_p95_millis ?? 0) > 0).length;
  const coverage = pct(enriched, summary.workloads_analyzed || workloads.length);
  const dtCount = workloads.filter(w => w.metrics_source === "dynatrace").length;
  const promCount = workloads.filter(w => w.metrics_source === "prometheus").length;
  const generated = new Date(report.generated_at);
  const exempt = workloads.filter(w => w.exempt_active);
  const exemptWaste = exempt.reduce((s, w) => s + (w.exempt_original_waste_brl ?? 0) * factorOf(w.node_pool), 0);

  // ── PDF ───────────────────────────────────────────────────────────────────
  const exportPDF = async () => {
    setExporting(true);
    try {
      const { default: jsPDF } = await import("jspdf");
      const { default: autoTable } = await import("jspdf-autotable");
      const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
      const W = 210, M = 14, CW = W - M * 2;
      const money = (v: number) => fmtBRL(v).replace(/\u00a0/g, " ");
      const rgb = (hex: string): [number, number, number] => {
        const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
        return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : [150, 150, 150];
      };
      const title = (text: string, y: number) => {
        doc.setFontSize(10); doc.setFont("helvetica", "bold"); doc.setTextColor(15, 23, 42);
        doc.text(text, M, y);
        doc.setDrawColor(99, 102, 241); doc.setLineWidth(0.6); doc.line(M, y + 1.5, M + 12, y + 1.5);
      };
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const lastY = () => (doc as any).lastAutoTable.finalY as number;
      const ensure = (y: number, need: number) => (y + need > 282 ? (doc.addPage(), M) : y);

      // Faixa de título
      doc.setFillColor(30, 27, 75); doc.rect(0, 0, W, 30, "F");
      doc.setFillColor(79, 70, 229); doc.rect(0, 28, W, 2, "F");
      doc.setTextColor(255, 255, 255);
      doc.setFontSize(16); doc.setFont("helvetica", "bold"); doc.text("Relatório FinOps", M, 13);
      doc.setFontSize(9); doc.setFont("helvetica", "normal");
      doc.text(`${cluster.replace(/-admin$/, "")}  ·  ${generated.toLocaleString("pt-BR")}  ·  câmbio USD/BRL ${report.exchange_rate.toFixed(4)}`, M, 21);
      let y = 38;

      // Indicadores
      const kpis: [string, string, string][] = [
        [cost.coveredPools > 0 ? "Custo mensal (efetivo)" : "Custo mensal (tabela)", money(cost.totalBRL), cost.coveredPools > 0 ? `compute de tabela ${money(cost.tableComputeBRL)}` : `compute ${money(cost.computeBRL)}`],
        ["Projeção anual", money(cost.totalBRL * 12), "no ritmo atual"],
        ["Economia possível", totalSaving > 0 ? `${money(totalSaving)}/mês` : "-", totalSaving > 0 ? `${savingPct}% do custo · ${money(totalSaving * 12)}/ano` : actions.length > 0 ? "nada certo (reservas)" : "nenhuma ação"],
        ["Eficiência de CPU", eff.cpuUse !== null ? `${eff.cpuUse}%` : "-", "uso médio / request"],
      ];
      const kw = (CW - 9) / 4;
      kpis.forEach(([label, value, sub], i) => {
        const x = M + i * (kw + 3);
        doc.setFillColor(248, 250, 252); doc.setDrawColor(226, 232, 240); doc.roundedRect(x, y, kw, 22, 2, 2, "FD");
        doc.setFontSize(7); doc.setTextColor(100, 116, 139); doc.setFont("helvetica", "normal"); doc.text(label.toUpperCase(), x + 3, y + 6);
        doc.setFontSize(12); doc.setTextColor(15, 23, 42); doc.setFont("helvetica", "bold"); doc.text(value, x + 3, y + 13.5);
        doc.setFontSize(6.5); doc.setTextColor(100, 116, 139); doc.setFont("helvetica", "normal"); doc.text(doc.splitTextToSize(sub, kw - 6)[0], x + 3, y + 19);
      });
      y += 31;

      // Para onde vai o dinheiro: barra empilhada + legenda
      title("Para onde vai o dinheiro", y); y += 7;
      let bx = M;
      slices.forEach(s => {
        const w = (s.value / sliceTotal) * CW;
        doc.setFillColor(...rgb(s.color)); doc.rect(bx, y, w, 7, "F");
        bx += w;
      });
      y += 12;
      slices.forEach((s, i) => {
        const col = i % 2, row = Math.floor(i / 2);
        const x = M + col * (CW / 2), yy = y + row * 9;
        doc.setFillColor(...rgb(s.color)); doc.roundedRect(x, yy - 3, 3, 3, 0.5, 0.5, "F");
        doc.setFontSize(8.5); doc.setTextColor(15, 23, 42); doc.setFont("helvetica", "bold"); doc.text(s.name, x + 5, yy);
        doc.setFont("helvetica", "normal"); doc.setTextColor(100, 116, 139);
        doc.text(`${money(s.value)}  ·  ${pct(s.value, sliceTotal)}%`, x + CW / 2 - 4, yy, { align: "right" });
      });
      y += Math.ceil(slices.length / 2) * 9 + 4;

      // Eficiência
      doc.setFontSize(8); doc.setTextColor(71, 85, 105);
      const e = (v: number | null) => (v === null ? "-" : `${v}%`);
      doc.text(`Eficiência (uso / request): CPU ${e(eff.cpuUse)} · memória ${e(eff.memUse)}     Alocação (request / capacidade): CPU ${e(eff.cpuAlloc)} · memória ${e(eff.memAlloc)}`, M, y);
      y += 9;

      // Plano de ação
      y = ensure(y, 30);
      title(`Plano de ação  ·  ${totalSaving > 0 ? `${money(totalSaving)}/mês` : "sem economia identificada"}`, y); y += 4;
      if (actions.length > 0) {
        autoTable(doc, {
          startY: y,
          head: [["Prioridade", "Ação", "Onde", "Economia/mês"]],
          body: actions.map(a => [PRIORITY[a.priority].label, `${a.title}\n${a.detail}`, a.where, a.savingBRL > 0 ? money(a.savingBRL) : "sem economia certa"]),
          theme: "plain",
          styles: { fontSize: 7.5, cellPadding: 2.4, textColor: [30, 41, 59], lineColor: [226, 232, 240], lineWidth: { bottom: 0.2 } },
          headStyles: { fillColor: [238, 242, 255], textColor: [55, 48, 163], fontStyle: "bold" },
          columnStyles: { 0: { cellWidth: 20, fontStyle: "bold" }, 1: { cellWidth: 100 }, 2: { cellWidth: 34 }, 3: { cellWidth: 28, halign: "right", fontStyle: "bold" } },
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          didParseCell: (d: any) => {
            if (d.section === "body" && d.column.index === 0) d.cell.styles.textColor = PRIORITY[actions[d.row.index].priority].rgb;
          },
          margin: { left: M, right: M },
        });
        y = lastY() + 9;
      } else {
        y += 6;
      }

      // Riscos
      if (risks.length > 0) {
        y = ensure(y, 30);
        title("Riscos e pontos de atenção", y); y += 4;
        autoTable(doc, {
          startY: y,
          head: [["Qtd.", "Ponto", "Por que importa"]],
          body: risks.map(r => [String(r.count), r.title, r.detail]),
          theme: "plain",
          styles: { fontSize: 7.5, cellPadding: 2.2, textColor: [30, 41, 59], lineColor: [226, 232, 240], lineWidth: { bottom: 0.2 } },
          headStyles: { fillColor: [254, 242, 242], textColor: [153, 27, 27], fontStyle: "bold" },
          columnStyles: { 0: { cellWidth: 14, halign: "center", fontStyle: "bold" }, 1: { cellWidth: 56 } },
          margin: { left: M, right: M },
        });
        y = lastY() + 9;
      }

      // Pools
      y = ensure(y, 30);
      title("Node pools", y); y += 4;
      autoTable(doc, {
        startY: y,
        head: [["Pool", "VM", "Nodes", "Custo/mês", "CPU req.", "Mem req.", "Ociosa", "Sugestão"]],
        body: poolUsage.map(u => [
          u.pool.name, u.pool.vm_size, String(u.pool.node_count), money(u.costBRL),
          `${pct(u.reqCPU, u.capCPU)}%`, `${pct(u.reqMem, u.capMem)}%`,
          u.workloads > 0 ? money(u.idleCostBRL) : "-",
          u.removable > 0 ? `${u.pool.node_count} para ${u.safeNodes} (-${money(u.savingBRL)})` : "-",
        ]),
        theme: "striped",
        styles: { fontSize: 7, cellPadding: 2 },
        headStyles: { fillColor: [30, 27, 75], textColor: [255, 255, 255], fontStyle: "bold" },
        margin: { left: M, right: M },
      });
      y = lastY() + 9;

      // Top workloads
      y = ensure(y, 30);
      title("Maiores custos por workload", y); y += 4;
      autoTable(doc, {
        startY: y,
        head: [["#", "Namespace", "Workload", "Compute", "Desperdício", "Storage", "Total/mês"]],
        body: topWorkloads.map((w, i) => [String(i + 1), w.ns, w.name, money(w.needed + w.waste), w.waste > 0 ? money(w.waste) : "-", w.storage > 0 ? money(w.storage) : "-", money(w.total)]),
        theme: "striped",
        styles: { fontSize: 7, cellPadding: 2 },
        headStyles: { fillColor: [30, 27, 75], textColor: [255, 255, 255], fontStyle: "bold" },
        columnStyles: { 3: { halign: "right" }, 4: { halign: "right" }, 5: { halign: "right" }, 6: { halign: "right", fontStyle: "bold" } },
        margin: { left: M, right: M },
      });
      y = lastY() + 8;

      // Notas
      y = ensure(y, 16);
      doc.setFontSize(7); doc.setFont("helvetica", "italic"); doc.setTextColor(100, 116, 139);
      const notes = [
        `Métricas de uso: ${coverage}% dos workloads (${enriched}/${summary.workloads_analyzed}), janela de ${report.window_days || 0} dias.`,
        "Preços de tabela pay-as-you-go: reservas e Savings Plan não estão descontados. Ociosidade pelo recurso mais ocupado de cada pool, sobre a capacidade alocável.",
        ...(covered.length > 0 ? [`Cost Management em ${covered.length} de ${pools.length} pools: ${money(coveredEffective)} efetivos contra ${money(coveredTable)} de tabela.`] : []),
        ...(billed?.available && billedFactor !== null ? [`Fatura do Azure (amortizada, ${billed.from} a ${billed.to}, RGs do AKS e dos nodes): ${money(billed.total * billedFactor)}.`] : []),
        ...(exempt.length > 0 ? [`Isentos: ${exempt.length} workload(s) fora do desperdício e do plano (${exempt.map(w => w.workload).slice(0, 8).join(", ")}${exempt.length > 8 ? ", …" : ""}).`] : []),
        ...(reservedPools.length > 0 ? [`Sob reserva/Savings Plan (${reservedPools.map(p => p.name).join(", ")}): a economia de tirar nodes conta só a parte sob demanda.`] : []),
        `Gerado por K8s HPA Manager em ${new Date().toLocaleString("pt-BR")}.`,
      ];
      doc.text(notes, M, y);

      doc.save(`finops-relatorio-${cluster.replace(/-admin$/, "")}-${new Date().toISOString().slice(0, 10)}.pdf`);
      toast.success("PDF exportado");
    } catch (err) {
      toast.error("Erro ao exportar PDF: " + (err as Error).message);
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="space-y-4 pb-6">
      {/* ── Capa ─────────────────────────────────────────────────────────── */}
      <div className="relative overflow-hidden rounded-2xl border bg-gradient-to-br from-indigo-600 via-indigo-700 to-violet-800 p-6 text-white shadow-lg">
        <div className="pointer-events-none absolute -right-16 -top-20 h-64 w-64 rounded-full bg-white/10 blur-2xl" />
        <div className="pointer-events-none absolute -bottom-24 left-1/3 h-56 w-56 rounded-full bg-fuchsia-400/20 blur-3xl" />
        <div className="relative flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="text-xs uppercase tracking-[0.2em] text-indigo-200">Relatório FinOps</p>
            <h2 className="mt-1 text-2xl font-semibold">{cluster.replace(/-admin$/, "")}</h2>
            <p className="mt-1 text-sm text-indigo-100/80">
              {generated.toLocaleString("pt-BR", { dateStyle: "long", timeStyle: "short" })}
              {report.window_days > 0 && <> · histórico de {report.window_days} dias</>}
              {" · "}USD/BRL {report.exchange_rate.toFixed(4)}
            </p>
          </div>
          <Button size="sm" onClick={exportPDF} disabled={exporting}
            className="gap-1.5 bg-white/15 text-white hover:bg-white/25 border border-white/20 backdrop-blur">
            {exporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            {exporting ? "Gerando PDF…" : "Exportar PDF"}
          </Button>
        </div>

        <div className="relative mt-6 grid grid-cols-2 lg:grid-cols-4 gap-3">
          {[
            { icon: CircleDollarSign, label: cost.coveredPools > 0 ? "Custo mensal (efetivo)" : "Custo mensal (tabela)", value: fmtBRL(cost.totalBRL), sub: cost.coveredPools > 0
              ? `com reserva/Savings Plan em ${cost.coveredPools} de ${pools.length} pools · compute de tabela ${shortBRL(cost.tableComputeBRL)}`
              : `compute ${shortBRL(cost.computeBRL)}${cost.totalBRL > cost.computeBRL ? ` · storage ${shortBRL(cost.totalBRL - cost.computeBRL)}` : ""}` },
            { icon: CalendarRange, label: "Projeção anual", value: shortBRL(cost.totalBRL * 12), sub: "no ritmo atual" },
            { icon: PiggyBank, label: "Economia possível", value: totalSaving > 0 ? fmtBRL(totalSaving) : "—", sub: totalSaving > 0 ? `${savingPct}% do custo · ${shortBRL(totalSaving * 12)}/ano`
              : actions.length > 0 ? "nada certo: as ações dependem de reaproveitar reservas" : "nenhuma ação identificada" },
            { icon: Gauge, label: "Eficiência de CPU", value: eff.cpuUse !== null ? `${eff.cpuUse}%` : "—", sub: eff.cpuUse !== null ? "uso médio ÷ request" : "sem métrica de uso" },
          ].map(k => (
            <div key={k.label} className="rounded-xl bg-white/10 p-4 ring-1 ring-white/15 backdrop-blur">
              <p className="flex items-center gap-1.5 text-[11px] uppercase tracking-wide text-indigo-100/80"><k.icon className="h-3.5 w-3.5" />{k.label}</p>
              <p className="mt-1.5 text-2xl font-semibold tabular-nums">{k.value}</p>
              <p className="mt-0.5 text-xs text-indigo-100/70">{k.sub}</p>
            </div>
          ))}
        </div>
      </div>

      <BilledCostPanel cluster={cluster} provider={provider} cost={cost} exchangeRate={report.exchange_rate} />

      {/* ── Dinheiro + eficiência ────────────────────────────────────────── */}
      <div className="grid gap-4 xl:grid-cols-5">
        <Panel className="xl:col-span-3">
          <SectionTitle icon={Wallet} title="Para onde vai o dinheiro" sub="Custo mensal dividido entre o que é usado, o que sobra e o que é armazenamento" />
          <div className="grid items-center gap-4 md:grid-cols-[220px_1fr]">
            <div className="relative h-[220px]">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  {/* Sem tooltip: a fatia sob o mouse aparece no centro da rosca (o tooltip caía em cima do total). */}
                  <Pie data={slices} dataKey="value" nameKey="name" innerRadius={72} outerRadius={104} paddingAngle={2} cornerRadius={5} stroke="none" startAngle={90} endAngle={-270}
                    onMouseEnter={(_, i) => setActiveSlice(slices[i]?.key ?? null)} onMouseLeave={() => setActiveSlice(null)}>
                    {slices.map(s => <Cell key={s.key} fill={s.color} fillOpacity={hovered && hovered.key !== s.key ? 0.35 : 1} className="transition-opacity" />)}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center">
                {hovered ? (
                  <>
                    <span className="max-w-[8.5rem] text-[11px] font-medium leading-tight" style={{ color: hovered.color }}>{hovered.name}</span>
                    <span className="text-xl font-semibold tabular-nums">{shortBRL(hovered.value)}</span>
                    <span className="text-[11px] text-muted-foreground">{pct(hovered.value, sliceTotal)}% do total</span>
                  </>
                ) : (
                  <>
                    <span className="text-[11px] uppercase tracking-wide text-muted-foreground">total/mês</span>
                    <span className="text-xl font-semibold tabular-nums">{shortBRL(sliceTotal)}</span>
                  </>
                )}
              </div>
            </div>
            <div className="space-y-2.5">
              {slices.map(s => (
                <div key={s.key} onMouseEnter={() => setActiveSlice(s.key)} onMouseLeave={() => setActiveSlice(null)}
                  className={`rounded-md px-1 -mx-1 transition-opacity ${hovered && hovered.key !== s.key ? "opacity-50" : ""}`}>
                  <div className="flex items-baseline justify-between gap-2 text-sm">
                    <span className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full" style={{ background: s.color }} />{s.name}</span>
                    <span className="tabular-nums font-medium">{fmtBRL(s.value)} <span className="text-xs text-muted-foreground">{pct(s.value, sliceTotal)}%</span></span>
                  </div>
                  <div className="mt-1 h-1.5 rounded-full bg-muted overflow-hidden">
                    <div className="h-full rounded-full" style={{ width: `${pct(s.value, sliceTotal)}%`, background: s.color }} />
                  </div>
                  <p className="mt-0.5 text-[11px] text-muted-foreground">{s.hint}</p>
                </div>
              ))}
            </div>
          </div>
        </Panel>

        <Panel className="xl:col-span-2">
          <SectionTitle icon={Gauge} title="Eficiência" sub="Faixa saudável: 60–85%" />
          <div className="grid grid-cols-2 gap-y-5">
            <RingGauge value={eff.cpuUse} label="CPU usada" hint="uso médio ÷ request" />
            <RingGauge value={eff.memUse} label="Memória usada" hint="uso médio ÷ request" />
            <RingGauge value={eff.cpuAlloc} label="CPU reservada" hint="requests ÷ capacidade dos nodes" goodFrom={65} goodTo={90} />
            <RingGauge value={eff.memAlloc} label="Memória reservada" hint="requests ÷ capacidade dos nodes" goodFrom={65} goodTo={90} />
          </div>
        </Panel>
      </div>

      {/* ── Tendência ────────────────────────────────────────────────────── */}
      {(nodeTrend.length > 3 || effTrend.length > 3) && (
        <div className="grid gap-4 lg:grid-cols-2">
          {nodeTrend.length > 3 && (
            <Panel>
              <SectionTitle icon={Server} title="Nodes — 30 dias" />
              <ResponsiveContainer width="100%" height={170}>
                <AreaChart data={nodeTrend} margin={{ left: -18, right: 6, top: 6, bottom: 0 }}>
                  <defs>
                    <linearGradient id="rel-nodes" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#6366f1" stopOpacity={0.4} />
                      <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} stroke="hsl(var(--border))" strokeDasharray="3 3" />
                  <XAxis dataKey="date" tick={AXIS_TICK} axisLine={false} tickLine={false} minTickGap={24} />
                  <YAxis tick={AXIS_TICK} axisLine={false} tickLine={false} allowDecimals={false} domain={[0, "auto"]} />
                  <Tooltip cursor={{ stroke: "hsl(var(--border))" }} content={({ active, payload, label }) =>
                    active && payload?.length ? <TooltipBox title={label}><TooltipRow color="#6366f1" label="Nodes" value={payload[0].value} /></TooltipBox> : null} />
                  <Area type="monotone" dataKey="nodes" stroke="#6366f1" strokeWidth={2.5} fill="url(#rel-nodes)" dot={false} />
                </AreaChart>
              </ResponsiveContainer>
            </Panel>
          )}
          {effTrend.length > 3 && (
            <Panel>
              <SectionTitle icon={Activity} title="Eficiência — 30 dias" sub="uso ÷ request por dia" />
              <ResponsiveContainer width="100%" height={170}>
                <LineChart data={effTrend} margin={{ left: -18, right: 6, top: 6, bottom: 0 }}>
                  <CartesianGrid vertical={false} stroke="hsl(var(--border))" strokeDasharray="3 3" />
                  <ReferenceArea y1={60} y2={85} fill={COST_COLORS.productive} fillOpacity={0.08} />
                  <XAxis dataKey="date" tick={AXIS_TICK} axisLine={false} tickLine={false} minTickGap={24} />
                  <YAxis tick={AXIS_TICK} axisLine={false} tickLine={false} unit="%" domain={[0, (max: number) => Math.max(100, max)]} />
                  <Tooltip cursor={{ stroke: "hsl(var(--border))" }} content={({ active, payload, label }) =>
                    active && payload?.length ? (
                      <TooltipBox title={label}>
                        {payload.map(p => <TooltipRow key={String(p.dataKey)} color={p.color} label={p.dataKey === "cpu" ? "CPU" : "Memória"} value={`${p.value}%`} />)}
                      </TooltipBox>) : null} />
                  <Line type="monotone" dataKey="cpu" stroke="#6366f1" strokeWidth={2.5} dot={false} connectNulls />
                  <Line type="monotone" dataKey="mem" stroke="#14b8a6" strokeWidth={2.5} dot={false} connectNulls />
                </LineChart>
              </ResponsiveContainer>
              <div className="mt-1 flex gap-4 text-[11px] text-muted-foreground">
                <span className="flex items-center gap-1.5"><span className="h-0.5 w-4 rounded bg-indigo-500" />CPU</span>
                <span className="flex items-center gap-1.5"><span className="h-0.5 w-4 rounded bg-teal-500" />Memória</span>
                <span className="flex items-center gap-1.5"><span className="h-2.5 w-4 rounded-sm bg-emerald-500/20" />faixa saudável</span>
              </div>
            </Panel>
          )}
        </div>
      )}

      {/* ── Namespaces + workloads ───────────────────────────────────────── */}
      <div className="grid gap-4 xl:grid-cols-2">
        {nsData.length > 0 && (
          <Panel>
            <SectionTitle icon={LayoutGrid} title="Custo por namespace"
              sub={<>{cost.coveredPools > 0 && "Custo efetivo · "}Área = custo · cor = desperdício: <span style={{ color: COST_COLORS.productive }}>até 15%</span>, <span style={{ color: COST_COLORS.waste }}>15–35%</span>, <span style={{ color: COST_COLORS.idle }}>acima de 35%</span></>} />
            <ResponsiveContainer width="100%" height={300}>
              <Treemap data={nsData} dataKey="size" nameKey="name" isAnimationActive={false} content={<NamespaceCell />}>
                <Tooltip content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const d = payload[0].payload as typeof nsData[number];
                  return (
                    <TooltipBox title={d.name}>
                      <TooltipRow label="custo/mês" value={fmtBRL(d.size)} />
                      <TooltipRow color={wasteColor(d.ratio)} label="desperdício" value={`${fmtBRL(d.waste)} (${Math.round(d.ratio * 100)}%)`} />
                      <TooltipRow label="workloads" value={d.workloads} />
                    </TooltipBox>
                  );
                }} />
              </Treemap>
            </ResponsiveContainer>
          </Panel>
        )}

        {topWorkloads.length > 0 && (
          <Panel>
            <SectionTitle icon={BarChart3} title="Maiores custos por workload" sub="Compute necessário, desperdício e storage" />
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={topWorkloads} layout="vertical" margin={{ left: 0, right: 12, top: 0, bottom: 0 }} barCategoryGap={6}>
                <CartesianGrid horizontal={false} stroke="hsl(var(--border))" strokeDasharray="3 3" />
                <XAxis type="number" tick={AXIS_TICK} axisLine={false} tickLine={false} tickFormatter={v => shortBRL(v)} />
                <YAxis type="category" dataKey="name" width={130} tick={AXIS_TICK} axisLine={false} tickLine={false} tickFormatter={v => trunc(String(v), 20)} />
                <Tooltip cursor={{ fill: "hsl(var(--muted) / 0.5)" }} content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const d = payload[0].payload as typeof topWorkloads[number];
                  return (
                    <TooltipBox title={<>{d.name} <span className="font-normal text-muted-foreground">· {d.ns}</span></>}>
                      <TooltipRow color={COST_COLORS.productive} label="necessário" value={fmtBRL(d.needed)} />
                      {d.waste > 0 && <TooltipRow color={COST_COLORS.waste} label="desperdício" value={fmtBRL(d.waste)} />}
                      {d.storage > 0 && <TooltipRow color={COST_COLORS.pvc} label="storage" value={fmtBRL(d.storage)} />}
                      <TooltipRow label="total/mês" value={fmtBRL(d.total)} />
                    </TooltipBox>
                  );
                }} />
                <Bar dataKey="needed" stackId="c" fill={COST_COLORS.productive} />
                <Bar dataKey="waste" stackId="c" fill={COST_COLORS.waste} />
                <Bar dataKey="storage" stackId="c" fill={COST_COLORS.pvc} radius={[0, 4, 4, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </Panel>
        )}
      </div>

      {/* ── Plano de ação ────────────────────────────────────────────────── */}
      <Panel>
        <SectionTitle icon={ListChecks} title="Plano de ação"
          sub="Ordenado pela economia mensal; prioridade pelo peso no custo total"
          right={totalSaving > 0 && (
            <span className="rounded-full bg-emerald-500/10 px-3 py-1 text-sm font-semibold text-emerald-700 dark:text-emerald-300 tabular-nums">
              −{fmtBRL(totalSaving)}/mês
            </span>
          )} />
        {actions.length === 0 ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <CheckCircle2 className="h-4 w-4 text-emerald-500" />
            {hasMetrics ? "Nenhuma economia identificada neste cluster." : "Nenhuma economia identificada — rode Analisar com a análise histórica para medir o uso real."}
          </p>
        ) : (
          <div className="space-y-2">
            {actions.map(a => (
              <div key={a.title} className="relative flex items-center gap-4 overflow-hidden rounded-xl border p-4 pl-5">
                <span className="absolute inset-y-0 left-0 w-1" style={{ background: PRIORITY[a.priority].bar }} />
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-2 text-sm font-medium">
                    <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${PRIORITY[a.priority].cls}`}>{PRIORITY[a.priority].label}</span>
                    {a.title}
                  </p>
                  <p className="mt-1 truncate text-xs text-muted-foreground" title={a.detail}>{a.detail}</p>
                  <p className="mt-0.5 text-[11px] text-muted-foreground">Onde: {a.where}</p>
                </div>
                <div className="shrink-0 text-right">
                  {a.savingBRL > 0 ? (
                    <>
                      <p className="text-base font-semibold text-emerald-600 dark:text-emerald-400 tabular-nums">−{fmtBRL(a.savingBRL)}</p>
                      <p className="text-[11px] text-muted-foreground">{pct(a.savingBRL, cost.totalBRL)}% do custo</p>
                    </>
                  ) : (
                    <p className="text-sm font-medium text-amber-600" title="Sob reserva/Savings Plan: só economiza se a reserva liberada for reaproveitada">sem economia certa</p>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </Panel>

      {/* ── Riscos + qualidade dos dados ─────────────────────────────────── */}
      <div className="grid gap-4 xl:grid-cols-3">
        <Panel className="xl:col-span-2">
          <SectionTitle icon={ShieldAlert} title="Riscos e pontos de atenção" sub="O que conferir antes de cortar custo" />
          {risks.length === 0 ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground"><CheckCircle2 className="h-4 w-4 text-emerald-500" />Nenhum ponto de atenção.</p>
          ) : (
            <div className="grid gap-2 sm:grid-cols-2">
              {risks.map(r => (
                <div key={r.title} className={`flex gap-3 rounded-xl border p-3 ${RISK_TONE[r.tone]}`}>
                  <span className="text-2xl font-semibold tabular-nums leading-none">{r.count}</span>
                  <div>
                    <p className="text-sm font-medium">{r.title}</p>
                    <p className="text-xs text-muted-foreground">{r.detail}</p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel>
          <SectionTitle icon={Info} title="Qualidade dos dados" />
          <div className="space-y-3 text-sm">
            <div>
              <div className="flex justify-between text-xs"><span>Workloads com métrica de uso</span><span className="font-medium tabular-nums">{coverage}%</span></div>
              <div className="mt-1 h-1.5 rounded-full bg-muted overflow-hidden">
                <div className="h-full rounded-full" style={{ width: `${coverage}%`, background: coverage >= 80 ? COST_COLORS.productive : coverage >= 40 ? COST_COLORS.waste : COST_COLORS.idle }} />
              </div>
              <p className="mt-1 text-[11px] text-muted-foreground">
                {enriched} de {summary.workloads_analyzed}{dtCount > 0 && ` · Dynatrace ${dtCount}`}{promCount > 0 && ` · Prometheus ${promCount}`}
              </p>
            </div>
            <ul className="space-y-1.5 text-xs text-muted-foreground">
              <li>Janela: {report.window_days > 0 ? `${report.window_days} dias` : "sem análise histórica"}</li>
              <li>{cost.coveredPools > 0
                ? `Compute pelo custo efetivo do Cost Management nos pools com cobertura; demais pools e discos em preço de tabela`
                : "Preços de tabela pay-as-you-go: reservas e Savings Plan não descontados"}</li>
              {covered.length > 0 && (
                <li>Cobertura do Cost Management em {covered.length} de {pools.length} pools: {fmtBRL(coveredEffective)} efetivos contra {fmtBRL(coveredTable)} de tabela</li>
              )}
              {reservedPools.length > 0 && (
                <li className="text-amber-600">
                  {reservedPools.map(p => p.name).join(", ")} sob reserva/Savings Plan: a economia de tirar nodes conta só a parte certa (sob demanda); o valor de tabela depende de reaproveitar a reserva
                </li>
              )}
              <li>Ociosidade pelo recurso mais ocupado de cada pool (maior entre request e uso), sobre a capacidade alocável dos nodes</li>
              {fallbackPools > 0 && <li className="text-amber-600">{fallbackPools} pool(s) com preço estimado</li>}
              {exempt.length > 0 && (
                <li>{exempt.length} workload(s) isento(s) fora do desperdício e do plano{exemptWaste > 0 && ` (${fmtBRL(exemptWaste)}/mês que seriam desperdício)`} — ver Workloads → Oportunidades</li>
              )}
            </ul>
          </div>
        </Panel>
      </div>
    </div>
  );
}
