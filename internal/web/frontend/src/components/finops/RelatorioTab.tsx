// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, ResponsiveContainer, Cell, PieChart, Pie } from "recharts";
import { AlertTriangle, CheckCircle2, Loader2, Info, Download } from "lucide-react";
import { toast } from "sonner";
import { fmtBRL, fmtUSD, VerdictBadge } from "@/lib/finopsFormat";
import type { FinOpsReport } from "./types";

interface Finding {
  priority: "critical" | "high" | "medium";
  category: "compute" | "storage" | "hpa" | "nodepool";
  title: string;
  detail: string;
  savingBRL: number;
  action: string;
}

const CATEGORY_LABEL: Record<string, string> = {
  compute: "Compute",
  storage: "Storage",
  hpa: "HPA",
  nodepool: "Node Pool",
};

const CATEGORY_COLOR: Record<string, string> = {
  compute: "#6366f1",
  storage: "#8b5cf6",
  hpa: "#f59e0b",
  nodepool: "#0ea5e9",
};

export function RelatorioTab({ report, windowDays: _windowDays, cluster }: { report: FinOpsReport; windowDays: number; cluster: string }) {
  const { summary } = report;
  const workloads  = report.workloads  ?? [];
  const node_pools = report.node_pools ?? [];
  const storage = report.storage;
  const pvcs = report.pvcs ?? [];
  const contentRef = useRef<HTMLDivElement>(null);
  const pieChartRef = useRef<HTMLDivElement>(null);
  const [exporting, setExporting] = useState(false);

  // ── Breakdown de custo ────────────────────────────────────────────────────
  // storage.total_monthly_cost_brl = somente PVCs (OS disk fica em os_disk_cost_brl)
  const computeCost  = summary.total_monthly_cost_brl;
  const pvcCost      = storage?.total_monthly_cost_brl ?? 0;   // PVCs apenas
  const osDiskCost   = storage?.os_disk_cost_brl ?? 0;          // Disco OS separado
  const storageCost  = pvcCost + osDiskCost;                    // storage real = PVC + OS disk
  const totalCost    = summary.total_with_storage_brl ?? (computeCost + storageCost);
  const orphanedCost = storage?.orphaned_cost_brl ?? 0;
  const pvcActiveCost = Math.max(0, pvcCost - orphanedCost);   // PVCs montados (sem orfãos)

  // Desperdício identificado no compute
  const prometheusWaste = workloads.reduce((s, w) => s + (w.waste_brl ?? 0), 0);
  const hpaExcessCost   = workloads
    .filter(w => w.verdict === "superprovisioned" || w.verdict === "hpa_removable")
    .reduce((s, w) => s + Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl), 0);
  const computeWaste        = prometheusWaste > 0 ? prometheusWaste : hpaExcessCost;
  const totalIdentifiedWaste = computeWaste + orphanedCost;
  const savingPct = totalCost > 0 ? Math.round(totalIdentifiedWaste / totalCost * 100) : 0;

  // ── Pie chart ─────────────────────────────────────────────────────────────
  const prodCompute = Math.max(0, computeCost - computeWaste);

  // Cores para tipos de storage (até 6 tipos individuais antes de agrupar em "Outros")
  const STORAGE_COLORS = ["#0ea5e9", "#8b5cf6", "#06b6d4", "#a78bfa", "#38bdf8", "#7c3aed"];

  // Quebrar storage por tipo real (by_storage_class) excluindo custo de orfãos
  // proporcionalmente — o desperdício já entra no slice "Desperdício identificado".
  // Nota: osDiskCost NÃO faz parte de by_storage_class — é adicionado separadamente.
  const byClass = (storage?.by_storage_class ?? [])
    .filter(sc => sc.monthly_cost_brl > 0)
    .sort((a, b) => b.monthly_cost_brl - a.monthly_cost_brl);

  // orphanedCost é de PVCs — ratio contra pvcCost (não storageCost que inclui OS disk)
  const orphanRatio = pvcCost > 0 ? orphanedCost / pvcCost : 0;
  const storageSlices: { name: string; value: number; fill: string }[] = [];

  // 1. Disco OS (sempre separado — não é PVC, não sofre orphan ratio)
  if (osDiskCost > 0) {
    storageSlices.push({ name: "Disco OS", value: Math.round(osDiskCost), fill: STORAGE_COLORS[0] });
  }

  // 2. PVCs: breakdown por tipo se disponível, senão bucket genérico
  if (byClass.length > 0) {
    const TOP = 4; // 4 tipos + "Outros" para não ultrapassar 6 cores
    const topItems = byClass.slice(0, TOP);
    const restCost = byClass.slice(TOP).reduce((s, sc) => s + sc.monthly_cost_brl, 0);
    let pvcSliceTotal = 0;
    topItems.forEach((sc, i) => {
      const prodCost = Math.round(sc.monthly_cost_brl * (1 - orphanRatio));
      if (prodCost > 0) {
        storageSlices.push({ name: sc.azure_type || sc.storage_class, value: prodCost, fill: STORAGE_COLORS[i + 1] ?? "#64748b" });
        pvcSliceTotal += prodCost;
      }
    });
    if (restCost > 0) {
      const prodRest = Math.round(restCost * (1 - orphanRatio));
      if (prodRest > 0) {
        storageSlices.push({ name: "Outros PVCs", value: prodRest, fill: "#64748b" });
        pvcSliceTotal += prodRest;
      }
    }
    // Fallback: se todos ficaram zerados pelo orphanRatio mas há PVCs ativos, mostra agregado
    if (pvcSliceTotal === 0 && pvcActiveCost > 0) {
      storageSlices.push({ name: "PVCs ativos", value: Math.round(pvcActiveCost), fill: STORAGE_COLORS[1] });
    }
  } else if (pvcActiveCost > 0) {
    // Sem breakdown por tipo — bucket genérico
    storageSlices.push({ name: "PVCs ativos", value: Math.round(pvcActiveCost), fill: STORAGE_COLORS[1] });
  }

  const pieData = [
    ...(prodCompute > 0          ? [{ name: "Compute produtivo",       value: Math.round(prodCompute),       fill: "#6366f1" }] : []),
    ...storageSlices,
    ...(totalIdentifiedWaste > 0 ? [{ name: "Desperdício identificado", value: Math.round(totalIdentifiedWaste), fill: "#ef4444" }] : []),
  ];
  // ── Achados ───────────────────────────────────────────────────────────────
  const findings: Finding[] = [];

  // 1. PVCs orfãos
  if (orphanedCost > 0 && (storage?.orphaned_pvc_count ?? 0) > 0) {
    const retainCount = pvcs.filter(p => p.is_orphaned && p.reclaim_policy === "Retain").length;
    findings.push({
      priority: "critical",
      category: "storage",
      title: `${storage!.orphaned_pvc_count} PVC(s) orfão(s) sem workload`,
      detail: `Discos provisionados sem nenhum pod montando${retainCount > 0 ? ` · ${retainCount} com reclaimPolicy=Retain (disco persiste após delete do PVC)` : ""}`,
      savingBRL: orphanedCost,
      action: "Aba Armazenamento → seção PVCs Orfãos",
    });
  }

  // 2. Desperdício Prometheus
  if (prometheusWaste > 50) {
    const wasteful = workloads
      .filter(w => (w.waste_brl ?? 0) > 50)
      .sort((a, b) => (b.waste_brl ?? 0) - (a.waste_brl ?? 0));
    findings.push({
      priority: prometheusWaste > 500 ? "critical" : "high",
      category: "compute",
      title: `${wasteful.length} workload(s) com CPU/Mem acima do uso real (Prometheus)`,
      detail: wasteful.slice(0, 3).map(w =>
        `${w.workload}: ${Math.round(w.cpu_request_millis)}m req vs ${Math.round(w.cpu_avg_millis ?? 0)}m uso (desperd. ${fmtBRL(w.waste_brl!)})`
      ).join(" · "),
      savingBRL: prometheusWaste,
      action: "Aba Oportunidades → reduzir CPU/Mem requests",
    });
  }

  // 3. Superprovisionados (HPA, sem Prometheus)
  const superWkl = workloads.filter(w => w.verdict === "superprovisioned");
  if (superWkl.length > 0 && prometheusWaste <= 50) {
    const saving = superWkl.reduce((s, w) => s + Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl), 0);
    findings.push({
      priority: saving > 500 ? "critical" : "high",
      category: "hpa",
      title: `${superWkl.length} workload(s) superprovisionados (HPA ≤ 35% do máximo)`,
      detail: superWkl.slice(0, 3).map(w =>
        `${w.workload}: ${w.hpa_current}/${w.hpa_max} réplicas (min ${w.hpa_min})`
      ).join(" · "),
      savingBRL: saving,
      action: "Aba Oportunidades → reduzir minReplicas",
    });
  }

  // 4. HPA removível
  const hpaRem = workloads.filter(w => w.verdict === "hpa_removable");
  if (hpaRem.length > 0) {
    const saving = hpaRem.reduce((s, w) => s + Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl), 0);
    findings.push({
      priority: "medium",
      category: "hpa",
      title: `${hpaRem.length} workload(s) com HPA que nunca escalou`,
      detail: hpaRem.slice(0, 3).map(w =>
        `${w.workload}: HPA ${w.hpa_min}–${w.hpa_max} rep, sempre em ${w.hpa_min}`
      ).join(" · "),
      savingBRL: saving,
      action: "Aba HPA Histórico → considerar remover HPA",
    });
  }

  // 5. Scale-down de Node Pool
  const totalCPU    = node_pools.reduce((s, p) => s + p.total_cpu_millicores, 0);
  const totalCPUReq = workloads.reduce((s, w) => s + w.cpu_request_millis, 0);
  const totalNodes  = node_pools.reduce((s, p) => s + p.node_count, 0);
  const allocPct    = totalCPU > 0 ? Math.round(totalCPUReq / totalCPU * 100) : null;
  if (allocPct !== null && allocPct < 45 && totalNodes > 3) {
    const userPools = node_pools.filter(p => p.mode !== "System" && p.node_count > 2);
    const potSaving = userPools.reduce((s, p) => {
      const removable = Math.max(0, Math.floor(p.node_count * (1 - (allocPct / 60))));
      return s + removable * (p.monthly_cost_brl / p.node_count);
    }, 0);
    if (potSaving > 0) {
      findings.push({
        priority: allocPct < 30 ? "high" : "medium",
        category: "nodepool",
        title: `Cluster com ${allocPct}% de CPU alocada — candidato a scale-down`,
        detail: `${totalNodes} nodes · ${(totalCPUReq / 1000).toFixed(0)} vCPUs requested de ${(totalCPU / 1000).toFixed(0)} disponíveis`,
        savingBRL: Math.round(potSaving),
        action: "Aba Node Pools → verificar recomendações de scale-down",
      });
    }
  }

  // 6. Sem requests definidos
  const noReq = workloads.filter(w => w.verdict === "no_request");
  if (noReq.length > 0) {
    findings.push({
      priority: "medium",
      category: "compute",
      title: `${noReq.length} workload(s) sem CPU/Mem requests — custo não mensurável`,
      detail: `${noReq.slice(0, 3).map(w => w.workload).join(", ")}${noReq.length > 3 ? ` +${noReq.length - 3}` : ""}`,
      savingBRL: 0,
      action: "Definir resources.requests em todos os Deployments",
    });
  }

  findings.sort((a, b) => b.savingBRL - a.savingBRL);
  const totalSaving = findings.reduce((s, f) => s + f.savingBRL, 0);

  // ── Top workloads por custo total ─────────────────────────────────────────
  const topWorkloads = workloads
    .map(w => ({ ...w, totalCostBRL: w.cost_share_brl + (w.storage_cost_brl ?? 0) }))
    .sort((a, b) => b.totalCostBRL - a.totalCostBRL)
    .slice(0, 10);

  const PRIORITY_COLOR = { critical: "#ef4444", high: "#f59e0b", medium: "#6366f1" };
  const PRIORITY_LABEL = { critical: "Crítico", high: "Alto", medium: "Médio" };

  // ── Rótulos externos do pie com anti-colisão (stacking lateral) ─────────────
  // Algoritmo: pré-computa midAngles a partir dos dados, separa em grupos
  // esquerdo/direito, resolve sobreposições empurrando labels verticalmente,
  // e desenha connector em cotovelo (polyline) do segmento até o label ajustado.
  const RADIAN = Math.PI / 180;

  // Armazena os midAngles REAIS que o Recharts passa ao renderPieLabel.
  // O Recharts aplica minAngle={3} e paddingAngle={2}, então os ângulos reais diferem
  // dos calculados por proporção bruta. Usamos os reais para isLeft e origY precisos.
  const _realMidAngles = useRef<Map<number, number>>(new Map());

  const _labelCache = useRef<{ key: string; positions: Map<number, {
    lx: number; ly: number; origY: number; isLeft: boolean; pct: number;
  }> }>({ key: "", positions: new Map() });

  const _computeLabelPositions = (
    cx: number, cy: number, outerRadius: number,
    angles: Map<number, number>, // index → midAngle real do Recharts
  ) => {
    const total = pieData.reduce((s, d) => s + d.value, 0);
    if (total === 0) return new Map<number, { lx: number; ly: number; origY: number; isLeft: boolean; pct: number }>();

    const R = outerRadius + 34;
    const MIN_GAP = 28; // bloco de texto = ~20px; 28px garante 8px de espaço entre labels
    const Y_MIN = cy - outerRadius - 30;
    const Y_MAX = cy + outerRadius + 50;

    // 1. Posição bruta usando midAngles REAIS do Recharts (ou fallback por proporção se ainda não disponíveis)
    const raw = pieData.map((d, i) => {
      const realAngle = angles.get(i);
      let cosA: number, sinA: number;
      if (realAngle !== undefined) {
        cosA = Math.cos(-realAngle * RADIAN);
        sinA = Math.sin(-realAngle * RADIAN);
      } else {
        // fallback: proporcional (só no primeiro render antes de _realMidAngles estar preenchido)
        const pct = d.value / total;
        const cumPct = pieData.slice(0, i).reduce((s, dd) => s + dd.value / total, 0);
        const mid = (cumPct + pct / 2) * 360;
        cosA = Math.cos(-mid * RADIAN);
        sinA = Math.sin(-mid * RADIAN);
      }
      return {
        i, cosA, sinA,
        origY: cy + R * sinA, adjY: cy + R * sinA,
        pct: d.value / total,
        isLeft: cosA < 0,
      };
    });

    // 2. Separar em grupos L/R, ordenar por Y
    const left  = raw.filter(x => x.isLeft).sort((a, b) => a.adjY - b.adjY);
    const right = raw.filter(x => !x.isLeft).sort((a, b) => a.adjY - b.adjY);

    // 3. Resolver colisões: push-down → push-up → shift de grupo para dentro dos limites
    const resolve = (grp: typeof left) => {
      if (grp.length <= 1) return;
      for (let i = 1; i < grp.length; i++)
        if (grp[i].adjY - grp[i - 1].adjY < MIN_GAP)
          grp[i] = { ...grp[i], adjY: grp[i - 1].adjY + MIN_GAP };
      for (let i = grp.length - 2; i >= 0; i--)
        if (grp[i + 1].adjY - grp[i].adjY < MIN_GAP)
          grp[i] = { ...grp[i], adjY: grp[i + 1].adjY - MIN_GAP };
      const overflow = grp[grp.length - 1].adjY - Y_MAX;
      if (overflow > 0)
        for (let i = 0; i < grp.length; i++) grp[i] = { ...grp[i], adjY: grp[i].adjY - overflow };
      const underflow = Y_MIN - grp[0].adjY;
      if (underflow > 0)
        for (let i = 0; i < grp.length; i++) grp[i] = { ...grp[i], adjY: grp[i].adjY + underflow };
    };
    resolve(left);
    resolve(right);

    // 4. Coluna x fixa por lado
    const COL_L = cx - R - 10;
    const COL_R = cx + R + 10;

    const positions = new Map<number, { lx: number; ly: number; origY: number; isLeft: boolean; pct: number }>();
    for (const it of [...left, ...right])
      positions.set(it.i, { lx: it.isLeft ? COL_L : COL_R, ly: it.adjY, origY: it.origY, isLeft: it.isLeft, pct: it.pct });
    return positions;
  };

  const renderPieLabel = ({ cx, cy, midAngle, outerRadius, index, name, value }: {
    cx: number; cy: number; midAngle: number; outerRadius: number;
    index: number; name: string; value: number;
  }) => {
    // Registrar o midAngle real do Recharts (inclui minAngle e paddingAngle)
    _realMidAngles.current.set(index, midAngle);

    // Recalcular posições apenas quando o conjunto de ângulos mudar
    const angleKey = Array.from(_realMidAngles.current.entries())
      .sort(([a], [b]) => a - b).map(([i, v]) => `${i}:${v.toFixed(1)}`).join(",");
    const cacheKey = `${cx.toFixed(0)},${cy.toFixed(0)},${outerRadius},${angleKey}`;

    if (_labelCache.current.key !== cacheKey) {
      _labelCache.current = {
        key: cacheKey,
        positions: _computeLabelPositions(cx, cy, outerRadius, _realMidAngles.current),
      };
    }

    const pos = _labelCache.current.positions.get(index);
    if (!pos) return null;

    const cosA = Math.cos(-midAngle * RADIAN);
    const sinA = Math.sin(-midAngle * RADIAN);
    const sx = cx + (outerRadius + 5) * cosA;
    const sy = cy + (outerRadius + 5) * sinA;
    const kneeX = pos.lx + (pos.isLeft ? 12 : -12);
    const anchor = pos.isLeft ? "end" : "start";
    const tx = pos.lx + (pos.isLeft ? -3 : 3);
    const shortName = name.length > 18 ? name.slice(0, 17) + "…" : name;

    return (
      <g>
        <polyline
          points={`${sx},${sy} ${kneeX},${pos.origY} ${pos.lx},${pos.ly}`}
          fill="none" stroke="#64748b" strokeWidth={0.8} opacity={0.7}
        />
        <circle cx={pos.lx} cy={pos.ly} r={1.5} fill="#64748b" opacity={0.7} />
        <text x={tx} y={pos.ly - 5} textAnchor={anchor} fontSize={9} fontWeight={600} fill="#e2e8f0">
          {shortName}
        </text>
        <text x={tx} y={pos.ly + 6} textAnchor={anchor} fontSize={9} fill="#94a3b8">
          {fmtBRL(value)} ({Math.round(pos.pct * 100)}%)
        </text>
      </g>
    );
  };

  // ── Exportar PDF ──────────────────────────────────────────────────────────
  const exportPDF = async () => {
    setExporting(true);
    try {
      const { default: jsPDF } = await import("jspdf");
      const { default: autoTable } = await import("jspdf-autotable");

      const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
      const pageW = 210;
      const margin = 14;
      const contentW = pageW - margin * 2;
      let y = margin;

      // ── Helpers de desenho ───────────────────────────────────────────────
      const drawDonutSlice = (
        cx: number, cy: number, rIn: number, rOut: number,
        startA: number, endA: number, col: [number, number, number],
      ) => {
        const span = endA - startA;
        if (Math.abs(span) < 0.002) return; // skip slices too thin to draw
        const steps = Math.max(24, Math.ceil(Math.abs(span) * 18));
        const pts: [number, number][] = [];
        for (let s = 0; s <= steps; s++) {
          const a = startA + (s / steps) * span;
          pts.push([cx + rOut * Math.cos(a), cy + rOut * Math.sin(a)]);
        }
        for (let s = steps; s >= 0; s--) {
          const a = startA + (s / steps) * span;
          pts.push([cx + rIn * Math.cos(a), cy + rIn * Math.sin(a)]);
        }
        doc.setFillColor(...col);
        doc.setDrawColor(255, 255, 255);
        doc.setLineWidth(0.4);
        const rel = pts.slice(1).map((p, i) => [p[0] - pts[i][0], p[1] - pts[i][1]] as [number, number]);
        doc.lines(rel, pts[0][0], pts[0][1], [1, 1], "FD", true);
      };

      // ── Cabeçalho ───────────────────────────────────────────────────────
      doc.setFillColor(15, 23, 42);
      doc.rect(0, 0, pageW, 20, "F");
      doc.setTextColor(255, 255, 255);
      doc.setFontSize(13); doc.setFont("helvetica", "bold");
      doc.text("FinOps — Relatório Executivo", margin, 12);
      doc.setFontSize(8.5); doc.setFont("helvetica", "normal");
      doc.text(`Cluster: ${cluster}   ·   ${new Date().toLocaleDateString("pt-BR", { dateStyle: "long" })}`, margin, 17.5);
      y = 28;

      // ── KPI cards ───────────────────────────────────────────────────────
      const kpis = [
        { label: "Custo Total/mês",         value: fmtBRL(totalCost),      sub: storageCost > 0 ? "Compute + Storage" : fmtUSD(summary.total_monthly_cost_usd) },
        { label: "Desperdício Identificado", value: totalIdentifiedWaste > 0 ? fmtBRL(totalIdentifiedWaste) : "Nenhum", sub: savingPct > 0 ? `${savingPct}% do orçamento` : "" },
        { label: "Achados",                 value: `${findings.length}`,   sub: `${findings.filter(f => f.priority === "critical").length} críticos · ${findings.filter(f => f.priority === "high").length} altos` },
        { label: "Saving Potencial/mês",    value: totalSaving > 0 ? fmtBRL(totalSaving) : "—", sub: totalCost > 0 && totalSaving > 0 ? `${Math.round(totalSaving / totalCost * 100)}% de redução` : "" },
      ];
      const kpiW = (contentW - 6) / 4;
      kpis.forEach((kpi, i) => {
        const kx = margin + i * (kpiW + 2);
        doc.setFillColor(241, 245, 249); doc.roundedRect(kx, y, kpiW, 18, 1.5, 1.5, "F");
        doc.setFontSize(6.5); doc.setTextColor(100, 116, 139); doc.setFont("helvetica", "normal");
        doc.text(kpi.label, kx + 3, y + 5.5);
        doc.setFontSize(10); doc.setTextColor(30, 41, 59); doc.setFont("helvetica", "bold");
        doc.text(kpi.value, kx + 3, y + 12);
        if (kpi.sub) {
          doc.setFontSize(6); doc.setTextColor(100, 116, 139); doc.setFont("helvetica", "normal");
          doc.text(kpi.sub, kx + 3, y + 17);
        }
      });
      y += 24;

      // ── Gráfico donut + legenda (vetorial) ──────────────────────────────
      doc.setFontSize(9); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 41, 59);
      doc.text("Composição do Custo", margin, y);
      y += 5;

      const hexToRGB = (hex: string): [number, number, number] => {
        const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
        return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : [150, 150, 150];
      };

      // Filtrar itens com valor > 0 para não quebrar o desenho
      const pieDataValid = pieData.filter(seg => seg.value > 0);
      const totalPie = pieDataValid.reduce((s, d) => s + d.value, 0);
      const donutCX = margin + 36;
      const donutCY = y + 30;
      const rOut = 26;
      const rIn  = 13;

      if (totalPie > 0) {
        let startA = -Math.PI / 2;
        pieDataValid.forEach(seg => {
          const angle = (seg.value / totalPie) * Math.PI * 2;
          drawDonutSlice(donutCX, donutCY, rIn, rOut, startA, startA + angle, hexToRGB(seg.fill));
          startA += angle;
        });
        doc.setFillColor(255, 255, 255);
        doc.setDrawColor(255, 255, 255);
        doc.circle(donutCX, donutCY, rIn - 0.3, "F");
        doc.setFontSize(7); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 41, 59);
        doc.text("Total", donutCX - 4, donutCY - 1.5);
        doc.setFontSize(6.5); doc.setFont("helvetica", "normal");
        doc.text(fmtBRL(totalPie), donutCX - 6, donutCY + 3.5);
      }

      // Legenda ao lado — apenas itens com valor > 0
      const lx = margin + 72;
      let ly = y + 4;
      const barMaxW = 38;
      pieDataValid.forEach(seg => {
        const col = hexToRGB(seg.fill);
        const pct = totalPie > 0 ? Math.round(seg.value / totalPie * 100) : 0;
        // barra de fundo sempre visível; barra preenchida só se pct > 0
        doc.setFillColor(230, 232, 240); doc.rect(lx, ly + 4.5, barMaxW, 2.5, "F");
        const barW = barMaxW * pct / 100;
        if (barW > 0.1) { doc.setFillColor(...col); doc.rect(lx, ly + 4.5, barW, 2.5, "F"); }
        doc.setFillColor(...col); doc.rect(lx, ly, 3.5, 3.5, "F");
        doc.setFontSize(8.5); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 41, 59);
        doc.text(seg.name, lx + 5.5, ly + 3.2);
        doc.setFontSize(7.5); doc.setFont("helvetica", "normal"); doc.setTextColor(100, 116, 139);
        doc.text(`${fmtBRL(seg.value)}  (${pct}%)`, lx, ly + 10.5);
        ly += 17;
      });
      // Avança y pela altura real da seção
      y += Math.max(68, pieDataValid.length * 17 + 8);

      // ── Divisor ─────────────────────────────────────────────────────────
      doc.setDrawColor(226, 232, 240); doc.setLineWidth(0.3);
      doc.line(margin, y, margin + contentW, y);
      y += 6;

      // ── Achados priorizados ──────────────────────────────────────────────
      if (findings.length > 0) {
        doc.setFontSize(9); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 41, 59);
        doc.text(`Achados Priorizados  —  Saving potencial: ${fmtBRL(totalSaving)}/mês`, margin, y);
        y += 2;
        autoTable(doc, {
          startY: y,
          head: [["Prioridade", "Categoria", "Título", "Evidência", "Saving/mês"]],
          body: findings.map(f => [
            PRIORITY_LABEL[f.priority],
            CATEGORY_LABEL[f.category],
            f.title,
            f.detail.length > 70 ? f.detail.slice(0, 70) + "…" : f.detail,
            f.savingBRL > 0 ? fmtBRL(f.savingBRL) : "—",
          ]),
          theme: "grid",
          styles: { fontSize: 7, cellPadding: 2.2, overflow: "linebreak" },
          headStyles: { fillColor: [30, 41, 59], textColor: [255, 255, 255], fontStyle: "bold", fontSize: 7.5 },
          columnStyles: {
            0: { cellWidth: 18, fontStyle: "bold" },
            1: { cellWidth: 20 },
            2: { cellWidth: 52 },
            3: { cellWidth: 64 },
            4: { cellWidth: 24, halign: "right" },
          },
          didParseCell: (data: any) => {
            if (data.section === "body" && data.column.index === 0) {
              const p = findings[data.row.index]?.priority;
              if (p === "critical") data.cell.styles.textColor = [220, 38, 38];
              else if (p === "high") data.cell.styles.textColor = [217, 119, 6];
              else data.cell.styles.textColor = [99, 102, 241];
            }
          },
          margin: { left: margin, right: margin },
        });
        y = (doc as any).lastAutoTable.finalY + 8;
      }

      // ── Top 10 Workloads ─────────────────────────────────────────────────
      if (y > 240) { doc.addPage(); y = margin; }
      const hasStorageCols = topWorkloads.some(w => (w.storage_cost_brl ?? 0) > 0);
      doc.setFontSize(9); doc.setFont("helvetica", "bold"); doc.setTextColor(30, 41, 59);
      doc.text("Top 10 Workloads — Custo Total (Compute + Storage)", margin, y);
      y += 2;
      autoTable(doc, {
        startY: y,
        head: [hasStorageCols
          ? ["#", "Namespace", "Workload", "Compute", "Storage", "Total/mês", "%", "Veredicto"]
          : ["#", "Namespace", "Workload", "Compute/mês", "Total/mês", "%", "Veredicto"]
        ],
        body: topWorkloads.map((w, i) => hasStorageCols
          ? [`${i + 1}`, w.namespace, w.workload, fmtBRL(w.cost_share_brl), fmtBRL(w.storage_cost_brl ?? 0), fmtBRL(w.totalCostBRL), totalCost > 0 ? `${Math.round(w.totalCostBRL / totalCost * 100)}%` : "—", w.verdict]
          : [`${i + 1}`, w.namespace, w.workload, fmtBRL(w.cost_share_brl), fmtBRL(w.totalCostBRL), totalCost > 0 ? `${Math.round(w.totalCostBRL / totalCost * 100)}%` : "—", w.verdict]
        ),
        theme: "striped",
        styles: { fontSize: 7, cellPadding: 2 },
        headStyles: { fillColor: [30, 41, 59], textColor: [255, 255, 255], fontStyle: "bold", fontSize: 7.5 },
        margin: { left: margin, right: margin },
      });

      // ── Rodapé ───────────────────────────────────────────────────────────
      const finalY = (doc as any).lastAutoTable.finalY + 6;
      doc.setFontSize(7); doc.setFont("helvetica", "italic"); doc.setTextColor(148, 163, 184);
      doc.text(`Gerado em ${new Date().toLocaleString("pt-BR")} · K8s HPA Manager · FinOps`, margin, finalY);

      doc.save(`finops-relatorio-${cluster}-${new Date().toISOString().slice(0, 10)}.pdf`);
      toast.success("PDF exportado com sucesso");
    } catch (err) {
      toast.error("Erro ao exportar PDF: " + (err as Error).message);
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* ── Botão exportar ─────────────────────────────────────────────────── */}
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={exportPDF} disabled={exporting} className="gap-1.5">
          {exporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
          {exporting ? "Gerando PDF…" : "Exportar PDF"}
        </Button>
      </div>

      <div ref={contentRef} className="space-y-4">

      {/* ── KPI cards ──────────────────────────────────────────────────────── */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo Total Real/mês</p>
            <p className="text-lg font-bold text-foreground leading-tight">{fmtBRL(totalCost)}</p>
            <p className="text-[10px] text-muted-foreground">
              {storageCost > 0
                ? `Compute ${fmtBRL(computeCost)} · Storage ${fmtBRL(storageCost)}`
                : fmtUSD(summary.total_monthly_cost_usd)}
            </p>
          </CardContent>
        </Card>
        <Card className={totalIdentifiedWaste > 0 ? "border-red-200 dark:border-red-900/40" : ""}>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Desperdício Identificado</p>
            <p className={`text-lg font-bold leading-tight ${totalIdentifiedWaste > 0 ? "text-red-500" : "text-green-500"}`}>
              {totalIdentifiedWaste > 0 ? fmtBRL(totalIdentifiedWaste) : "Nenhum"}
            </p>
            <p className="text-[10px] text-muted-foreground">
              {savingPct > 0 ? `${savingPct}% do orçamento total` : "nenhum desperdício confirmado"}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Achados</p>
            <p className="text-lg font-bold leading-tight">{findings.length}</p>
            <p className="text-[10px] text-muted-foreground">
              {findings.filter(f => f.priority === "critical").length} críticos ·{" "}
              {findings.filter(f => f.priority === "high").length} altos
            </p>
          </CardContent>
        </Card>
        <Card className={totalSaving > 0 ? "border-green-200 dark:border-green-900/40" : ""}>
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Saving Potencial/mês</p>
            <p className={`text-lg font-bold leading-tight ${totalSaving > 0 ? "text-green-500" : "text-muted-foreground"}`}>
              {totalSaving > 0 ? fmtBRL(totalSaving) : "—"}
            </p>
            <p className="text-[10px] text-muted-foreground">
              {totalCost > 0 && totalSaving > 0
                ? `${Math.round(totalSaving / totalCost * 100)}% de redução possível`
                : "sem ações identificadas"}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* ── Composição do custo + achados ─────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">

        {/* Pie chart */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Composição do Custo</CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3" ref={pieChartRef}>
            {/* [&_svg]:bg-transparent → remove o fundo branco que o Recharts injeta no SVG */}
            <div className="[&_svg]:bg-transparent [&_svg]:!fill-none">
            <ResponsiveContainer width="100%" height={260}>
              <PieChart margin={{ top: 20, right: 10, bottom: 20, left: 10 }}
                {...({ overflow: "visible" } as any)}>
                <Pie
                  data={pieData}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  innerRadius={50}
                  outerRadius={72}
                  paddingAngle={2}
                  minAngle={3}
                  label={renderPieLabel}
                  labelLine={false}
                >
                  {pieData.map((e, i) => <Cell key={i} fill={e.fill} />)}
                </Pie>
                <Tooltip
                  cursor={false}
                  content={({ active, payload }) => {
                    if (!active || !payload?.length) return null;
                    const d = payload[0];
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                        <p className="font-semibold mb-1" style={{ color: d.payload.fill }}>{d.name}</p>
                        <p className="text-foreground">{fmtBRL(d.value as number)}</p>
                      </div>
                    );
                  }}
                />
              </PieChart>
            </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        {/* Resumo por dimensão */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm">Dimensões de Custo</CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-3 space-y-2">
            {pieData.map((slice, i) => {
              // Subtítulo contextual por tipo de slice
              const nodeCount = node_pools.reduce((s, p) => s + p.node_count, 0);
              const sub =
                slice.name === "Compute produtivo" ? `${nodeCount} nodes · ${node_pools.length} pools` :
                slice.name === "Disco OS"           ? `${nodeCount} discos OS` :
                slice.name === "Desperdício identificado" ? `${savingPct}% do custo total` :
                slice.name === "PVCs ativos" || slice.name.startsWith("Outros") ?
                  `${storage?.pvc_count ?? 0} PVCs · ${Math.round(storage?.total_capacity_gb ?? 0)} GB` :
                  // tipos de storage class individuais (Premium SSD, Standard HDD, etc.)
                  (() => {
                    const sc = storage?.by_storage_class?.find(s => s.azure_type === slice.name || s.storage_class === slice.name);
                    return sc ? `${sc.pvc_count} PVC${sc.pvc_count !== 1 ? "s" : ""} · ${Math.round(sc.total_gb)} GB` : "";
                  })();
              return (
                <div key={i} className="flex items-center gap-2">
                  <div className="w-2.5 h-2.5 rounded-sm shrink-0" style={{ background: slice.fill }} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline justify-between gap-1">
                      <span className="text-xs font-medium truncate">{slice.name}</span>
                      <span className="text-xs font-bold shrink-0">{fmtBRL(slice.value)}</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-muted mt-0.5 overflow-hidden">
                      <div className="h-full rounded-full"
                        style={{ width: `${Math.min(100, totalCost > 0 ? slice.value / totalCost * 100 : 0)}%`, background: slice.fill }} />
                    </div>
                    {sub && <p className="text-[10px] text-muted-foreground mt-0.5">{sub}</p>}
                  </div>
                </div>
              );
            })}
          </CardContent>
        </Card>
      </div>

      {/* ── Achados prioritizados ─────────────────────────────────────────── */}
      {findings.length > 0 && (
        <Card>
          <CardHeader className="pb-2 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-orange-500" />
              Achados ({findings.length}) — saving potencial total: <span className="text-green-500 font-bold">{fmtBRL(totalSaving)}/mês</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-4 pb-4 space-y-2">
            {findings.map((f, i) => (
              <div key={i} className="flex items-start gap-3 p-3 rounded-lg border border-border bg-muted/20 hover:bg-muted/40 transition-colors">
                {/* Priority indicator */}
                <div className="w-1 self-stretch rounded-full shrink-0" style={{ background: PRIORITY_COLOR[f.priority] }} />
                <div className="flex-1 min-w-0 space-y-1">
                  <div className="flex items-start justify-between gap-2 flex-wrap">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <Badge className="text-[9px] px-1.5 py-0" style={{ background: PRIORITY_COLOR[f.priority] + "22", color: PRIORITY_COLOR[f.priority], border: `1px solid ${PRIORITY_COLOR[f.priority]}40` }}>
                        {PRIORITY_LABEL[f.priority]}
                      </Badge>
                      <Badge variant="outline" className="text-[9px] px-1.5 py-0" style={{ color: CATEGORY_COLOR[f.category] }}>
                        {CATEGORY_LABEL[f.category]}
                      </Badge>
                      <span className="text-xs font-semibold">{f.title}</span>
                    </div>
                    {f.savingBRL > 0 && (
                      <span className="text-xs font-bold text-green-500 shrink-0">−{fmtBRL(f.savingBRL)}/mês</span>
                    )}
                  </div>
                  <p className="text-[11px] text-muted-foreground leading-snug">{f.detail}</p>
                  <p className="text-[10px] text-blue-400 flex items-center gap-1">
                    <Info className="h-3 w-3 inline" /> {f.action}
                  </p>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {findings.length === 0 && (
        <Card className="border-green-200 dark:border-green-900/40">
          <CardContent className="p-4 flex items-center gap-2 text-green-600">
            <CheckCircle2 className="h-4 w-4" />
            <span className="text-sm font-medium">Nenhum problema identificado — cluster otimizado.</span>
          </CardContent>
        </Card>
      )}

      {/* ── Top 10 workloads por custo total ──────────────────────────────── */}
      <Card>
        <CardHeader className="pb-2 pt-3 px-4">
          <CardTitle className="text-sm">Top 10 Workloads — Custo Total (Compute + Storage)</CardTitle>
        </CardHeader>
        <Table>
          <TableHeader>
            <TableRow className="text-[11px]">
              <TableHead>Namespace / Workload</TableHead>
              <TableHead className="text-right">Compute</TableHead>
              {topWorkloads.some(w => (w.storage_cost_brl ?? 0) > 0) && (
                <TableHead className="text-right text-purple-400">Storage</TableHead>
              )}
              <TableHead className="text-right font-bold">Total</TableHead>
              <TableHead className="text-right">% do total</TableHead>
              <TableHead>Veredicto</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {topWorkloads.map((w, i) => (
              <TableRow key={i} className="text-xs">
                <TableCell className="max-w-[180px]">
                  <p className="text-muted-foreground text-[10px] truncate">{w.namespace}</p>
                  <p className="font-medium truncate">{w.workload}</p>
                </TableCell>
                <TableCell className="text-right font-mono">{fmtBRL(w.cost_share_brl)}</TableCell>
                {topWorkloads.some(x => (x.storage_cost_brl ?? 0) > 0) && (
                  <TableCell className="text-right font-mono text-purple-400">
                    {(w.storage_cost_brl ?? 0) > 0 ? fmtBRL(w.storage_cost_brl!) : <span className="text-muted-foreground">—</span>}
                  </TableCell>
                )}
                <TableCell className="text-right font-bold">{fmtBRL(w.totalCostBRL)}</TableCell>
                <TableCell className="text-right">
                  <span className="text-muted-foreground text-[10px]">
                    {totalCost > 0 ? `${Math.round(w.totalCostBRL / totalCost * 100)}%` : "—"}
                  </span>
                </TableCell>
                <TableCell><VerdictBadge verdict={w.verdict} /></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      </div>{/* fim ref={contentRef} */}
    </div>
  );
}
