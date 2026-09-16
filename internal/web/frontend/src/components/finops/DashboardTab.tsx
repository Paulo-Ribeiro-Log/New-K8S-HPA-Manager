// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  Legend, ComposedChart, Line, Area, ReferenceLine,
} from "recharts";
import {
  TrendingDown, CheckCircle2, Loader2, Server, Layers, CircleDollarSign, Activity,
} from "lucide-react";
import { fmtBRL, fmtUSD, VerdictBadge } from "@/lib/finopsFormat";
import { DataResourcesPanel } from "@/components/DataResourcesPanel";
import type { FinOpsReport, TimelineReport } from "./types";
import { buildRecommendation } from "./helpers";

const LINE_COLORS = ["#6366f1","#10b981","#f59e0b","#ef4444","#8b5cf6","#06b6d4","#ec4899","#84cc16","#f97316","#14b8a6"];

export function DashboardTab({ cluster, report }: { cluster: string; report: FinOpsReport }) {
  const { summary } = report;
  const node_pools = report.node_pools ?? [];
  const workloads  = report.workloads  ?? [];
  const namespaces = report.namespaces ?? [];
  const [days, setDays] = useState(30);

  // ── Fetch timeline ──────────────────────────────────────────────────────────
  const { data: tl, isLoading: tlLoading } = useQuery<TimelineReport>({
    queryKey: ["finops-timeline-dashboard", cluster, days],
    queryFn: async () => {
      const r = await fetch(
        `/api/v1/finops/timeline?cluster=${encodeURIComponent(cluster)}&days=${days}`,
        { headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` } }
      );
      if (!r.ok) throw new Error(`Timeline: ${r.status}`);
      return r.json();
    },
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  // ── Cost per node per day ────────────────────────────────────────────────────
  const totalNodes = node_pools.reduce((s, p) => s + p.node_count, 0);
  const costPerNodePerDay = totalNodes > 0 ? summary.total_monthly_cost_brl / 30 / totalNodes : 0;

  // ── Global efficiency metrics ────────────────────────────────────────────────
  const promWorkloads = workloads.filter(w => w.cpu_avg_millis && (w.cpu_request_millis ?? 0) > 0);
  const sumCpuUsed = promWorkloads.reduce((s, w) => s + (w.cpu_avg_millis ?? 0) * w.pods, 0);
  const sumCpuReq  = promWorkloads.reduce((s, w) => s + w.cpu_request_millis * w.pods, 0);
  const cpuEffGlobal = sumCpuReq > 0 ? Math.round(sumCpuUsed / sumCpuReq * 100) : null;

  const promMemWorkloads = workloads.filter(w => w.mem_avg_mi && (w.mem_request_mi ?? 0) > 0);
  const sumMemUsed = promMemWorkloads.reduce((s, w) => s + (w.mem_avg_mi ?? 0) * w.pods, 0);
  const sumMemReq  = promMemWorkloads.reduce((s, w) => s + w.mem_request_mi * w.pods, 0);
  const memEffGlobal = sumMemReq > 0 ? Math.round(sumMemUsed / sumMemReq * 100) : null;

  const hpaWkl = workloads.filter(w => w.hpa_max > 0);
  const hpaEff = hpaWkl.length > 0
    ? Math.round(hpaWkl.reduce((s, w) => s + w.hpa_current, 0) / hpaWkl.reduce((s, w) => s + w.hpa_max, 0) * 100)
    : null;

  const totalSavings = summary.potential_savings_brl > 0
    ? summary.potential_savings_brl
    : summary.hpa_savings_if_min_brl;

  // ── Main timeline: custo diário + eficiência % ───────────────────────────────
  type MainPoint = { date: string; custo: number | null; cpuEff: number | null; memEff: number | null; nodes: number | null };
  const mainByDate = new Map<string, MainPoint>();

  (tl?.nodes ?? []).forEach(n => {
    const date = n.date.slice(5);
    mainByDate.set(date, {
      date,
      custo: Math.round(n.node_count * costPerNodePerDay),
      nodes: n.node_count,
      cpuEff: null,
      memEff: null,
    });
  });
  (tl?.cpu ?? []).forEach(p => {
    const date = p.date.slice(5);
    const existing = mainByDate.get(date) ?? { date, custo: null, nodes: null, cpuEff: null, memEff: null };
    existing.cpuEff = p.req_millis > 0 ? Math.round(p.used_millis / p.req_millis * 100) : null;
    mainByDate.set(date, existing);
  });
  (tl?.mem ?? []).forEach(p => {
    const date = p.date.slice(5);
    const existing = mainByDate.get(date) ?? { date, custo: null, nodes: null, cpuEff: null, memEff: null };
    existing.memEff = p.req_mi > 0 ? Math.round(p.used_mi / p.req_mi * 100) : null;
    mainByDate.set(date, existing);
  });
  const mainTimeline = [...mainByDate.values()].sort((a, b) => a.date.localeCompare(b.date));
  const hasEfficiency = mainTimeline.some(p => p.cpuEff !== null);

  // ── Projeção de custo 30d (regressão linear) ─────────────────────────────────
  type MainPointExt = MainPoint & { projecao?: number };
  const mainTimelineExt: MainPointExt[] = (() => {
    const pts = mainTimeline.filter(p => p.custo !== null);
    if (pts.length < 5) return mainTimeline;
    const indexed = pts.map((p, i) => ({ x: i, y: p.custo as number }));
    const n = indexed.length;
    const sumX  = indexed.reduce((s, p) => s + p.x, 0);
    const sumY  = indexed.reduce((s, p) => s + p.y, 0);
    const sumXY = indexed.reduce((s, p) => s + p.x * p.y, 0);
    const sumX2 = indexed.reduce((s, p) => s + p.x * p.x, 0);
    const denom = n * sumX2 - sumX * sumX;
    const slope = denom !== 0 ? (n * sumXY - sumX * sumY) / denom : 0;
    const intercept = (sumY - slope * sumX) / n;
    // Função auxiliar para somar dias à string "MM-DD"
    const addDays = (mmdd: string, d: number) => {
      const [mm, day] = mmdd.split("-").map(Number);
      const dt = new Date(2026, mm - 1, day + d);
      return `${String(dt.getMonth() + 1).padStart(2, "0")}-${String(dt.getDate()).padStart(2, "0")}`;
    };
    const lastDate = mainTimeline[mainTimeline.length - 1]?.date ?? "";
    // Ponto de ponte: último ponto real também recebe projecao
    const extended: MainPointExt[] = mainTimeline.map(p => ({ ...p }));
    extended[extended.length - 1].projecao = Math.max(0, Math.round(intercept + slope * (n - 1)));
    // 30 dias de projeção
    for (let i = 1; i <= 30; i++) {
      extended.push({
        date: addDays(lastDate, i),
        custo: null,
        cpuEff: null,
        memEff: null,
        nodes: null,
        projecao: Math.max(0, Math.round(intercept + slope * (n - 1 + i))),
      });
    }
    return extended;
  })();

  // ── Namespace breakdown (stacked by verdict) ─────────────────────────────────
  // IMPORTANTE: total e wastePct ficam fora do objeto de dados do Recharts para não
  // expandir o domínio do eixo X além da soma das barras empilhadas (causaria barra branca vazia)
  const nsBreakdownRaw = namespaces.slice(0, 9).map(ns => {
    const nswl = workloads.filter(w => w.namespace === ns.namespace);
    let eficiente = 0, desperdicio = 0, risco = 0, sem_req = 0;
    nswl.forEach(w => {
      if      (w.verdict === "ok" || w.verdict === "hpa_removable") eficiente  += w.cost_share_brl;
      else if (w.verdict === "superprovisioned")                    desperdicio += w.cost_share_brl;
      else if (w.verdict === "oom_risk")                            risco       += w.cost_share_brl;
      else                                                          sem_req     += w.cost_share_brl;
    });
    const total = ns.monthly_cost_brl;
    const wastePct = total > 0 ? Math.round((desperdicio + risco) / total * 100) : 0;
    return { ns: ns.namespace, eficiente, desperdicio, risco, sem_req, total, wastePct };
  }).sort((a, b) => (b.desperdicio + b.risco) - (a.desperdicio + a.risco));

  // Dados para o Recharts — sem campos numéricos extras
  const nsBreakdown = nsBreakdownRaw.map(({ eficiente, desperdicio, risco, sem_req, ns }) => ({
    name: ns.length > 22 ? ns.slice(0, 20) + "…" : ns,
    eficiente, desperdicio, risco, sem_req,
  }));
  // Mapa auxiliar para o tooltip (total e wastePct)
  const nsExtraMap = new Map(nsBreakdownRaw.map(r => [
    r.ns.length > 22 ? r.ns.slice(0, 20) + "…" : r.ns,
    { total: r.total, wastePct: r.wastePct },
  ]));

  // ── Opportunities table ───────────────────────────────────────────────────────
  const windowDays = report.window_days || 30;
  const opportunities = workloads
    .map(w => {
      const rec = buildRecommendation(w, windowDays);
      // Prioridade: waste_brl (Prometheus) > estimativa HPA > fallback
      const saving = rec.savingBRL > 0
        ? rec.savingBRL
        : w.verdict === "superprovisioned" ? w.hpa_cost_current_brl - w.hpa_cost_min_brl
        : w.verdict === "hpa_removable"    ? w.hpa_cost_current_brl - w.hpa_cost_min_brl
        : 0;
      return { ...w, saving, rec };
    })
    .filter(w => w.saving > 5 || (w.verdict !== "ok" && w.cost_share_brl > 50))
    .sort((a, b) => b.saving - a.saving)
    .slice(0, 12);

  const opTotalSaving = opportunities.reduce((s, o) => s + o.saving, 0);

  // ── HPA top-8 for chart ───────────────────────────────────────────────────────
  const top8HPAs = (tl?.hpas ?? [])
    .map(h => ({ ...h, maxObs: Math.max(...h.series.map(p => p.max_replicas), 0) }))
    .sort((a, b) => b.maxObs - a.maxObs).slice(0, 8);

  const hpaAllDates = [...new Set((tl?.hpas ?? []).flatMap(h => h.series.map(p => p.date)))].sort();
  const hpaChartData = hpaAllDates.map(date => {
    const entry: Record<string, number | string> = { date: date.slice(5) };
    top8HPAs.forEach((h, i) => {
      const pt = h.series.find(p => p.date === date);
      entry[`h${i}`] = pt ? Math.round(pt.avg_replicas) : 0;
    });
    return entry;
  });

  // ── Nodes ─────────────────────────────────────────────────────────────────────
  const nodesData = (tl?.nodes ?? []).map(n => ({
    date: n.date.slice(5),
    Nodes: n.node_count,
    custo: Math.round(n.node_count * costPerNodePerDay),
  }));

  return (
    <div className="space-y-4">
      {/* ── 1. KPI cards (6) ──────────────────────────────────────────────── */}
      <div className="grid grid-cols-3 md:grid-cols-6 gap-2">
        {/* Custo/mês */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">
              {(summary.total_with_storage_brl ?? 0) > 0 ? "Compute/mês" : "Custo/mês"}
            </p>
            <p className="text-lg font-bold text-blue-600 leading-tight">{fmtBRL(summary.total_monthly_cost_brl)}</p>
            {(summary.total_with_storage_brl ?? 0) > 0 ? (
              <>
                {/* Bug real corrigido — crash "Cannot read properties of undefined (reading
                    'toLocaleString')": total_with_storage_brl = compute + storage_monthly_cost_brl
                    (PVC) + os_disk_cost_brl (disco OS dos nodes, quase sempre > 0). Um cluster sem
                    NENHUM PVC (storage_monthly_cost_brl genuinamente 0 → omitido do JSON por
                    `omitempty`) ainda tem total_with_storage_brl > 0 só pelo disco OS — a condição
                    acima então entrava neste branch com storage_monthly_cost_brl undefined, e o
                    `!` do TypeScript não protege nada em runtime. Corrigido guardando esta linha
                    pelo PRÓPRIO campo, não pelo de total — some quando não há custo de PVC real,
                    "Total" abaixo continua mostrando compute+disco OS normalmente. */}
                {(summary.storage_monthly_cost_brl ?? 0) > 0 && (
                  <p className="text-[10px] text-purple-500 font-medium">+{fmtBRL(summary.storage_monthly_cost_brl ?? 0)} storage</p>
                )}
                <p className="text-[10px] text-muted-foreground">Total: <strong>{fmtBRL(summary.total_with_storage_brl!)}</strong></p>
              </>
            ) : (
              <p className="text-[10px] text-muted-foreground">{fmtUSD(summary.total_monthly_cost_usd)}</p>
            )}
          </CardContent>
        </Card>
        {/* Custo/dia */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Custo/dia (médio)</p>
            <p className="text-lg font-bold text-blue-400 leading-tight">{fmtBRL(summary.total_monthly_cost_brl / 30)}</p>
            <p className="text-[10px] text-muted-foreground">{totalNodes} nodes · {node_pools.length} pools</p>
          </CardContent>
        </Card>
        {/* Eficiência CPU */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Eficiência CPU</p>
            {cpuEffGlobal !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${cpuEffGlobal < 35 ? "text-red-500" : cpuEffGlobal < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {cpuEffGlobal}%
                </p>
                <p className="text-[10px] text-muted-foreground">uso real / request</p>
              </>
            ) : hpaEff !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${hpaEff < 35 ? "text-red-500" : hpaEff < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {hpaEff}%
                </p>
                <p className="text-[10px] text-muted-foreground">HPA cur/max (proxy)</p>
              </>
            ) : (
              <p className="text-lg font-bold leading-tight text-muted-foreground">—</p>
            )}
          </CardContent>
        </Card>
        {/* Eficiência Mem */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Eficiência Mem</p>
            {memEffGlobal !== null ? (
              <>
                <p className={`text-lg font-bold leading-tight ${memEffGlobal < 35 ? "text-red-500" : memEffGlobal < 65 ? "text-yellow-500" : "text-green-500"}`}>
                  {memEffGlobal}%
                </p>
                <p className="text-[10px] text-muted-foreground">uso real / request</p>
              </>
            ) : (
              <p className="text-lg font-bold leading-tight text-muted-foreground">—</p>
            )}
          </CardContent>
        </Card>
        {/* Desperdício */}
        <Card className="col-span-1 border-red-200 dark:border-red-900/40">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Desperdício/mês</p>
            <p className="text-lg font-bold text-red-500 leading-tight">{fmtBRL(totalSavings)}</p>
            <p className="text-[10px] text-muted-foreground">
              {summary.total_monthly_cost_brl > 0 ? Math.round(totalSavings / summary.total_monthly_cost_brl * 100) : 0}% do custo total
            </p>
          </CardContent>
        </Card>
        {/* Câmbio */}
        <Card className="col-span-1">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">USD/BRL</p>
            <p className="text-lg font-bold text-orange-500 leading-tight">R$ {report.exchange_rate.toFixed(2)}</p>
            <p className="text-[10px] text-muted-foreground">{report.exchange_date}</p>
          </CardContent>
        </Card>
      </div>

      {/* ── 1b. Recursos de Dados (RG separado, rg-<nome>-data-<env>) ──────────
          Antes só vivia dentro da aba "Armazenamento" (7ª de 8) — pedido explícito do usuário
          relatado 2x nesta sessão ("não existe nada relacionado a rg-<cluster>-data<env> sendo
          exibido nas tabs"): o backend/endpoint sempre funcionou (confirmado ao vivo contra 4
          clusters reais), o problema era só descoberta — enterrado numa sub-aba raramente
          aberta. Duplicado aqui na Dashboard (1ª aba, sempre vista primeiro) só como resumo
          compacto; a versão completa (lista expansível por recurso) continua em Armazenamento,
          sem mudança. O próprio componente já é silencioso quando o cluster não tem RG de dados
          (nota discreta, nunca alarme) — reaproveitado tal como está, sem duplicar lógica. */}
      <Card>
        <CardContent className="p-3">
          <DataResourcesPanel cluster={cluster} />
        </CardContent>
      </Card>

      {/* ── 2. Window selector ──────────────────────────────────────────────── */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">Série temporal:</span>
        {([7, 15, 30] as const).map(d => (
          <Button key={d} variant={days === d ? "default" : "outline"} size="sm"
            className="h-7 text-xs px-3" onClick={() => setDays(d)}>
            {d}d
          </Button>
        ))}
        {tlLoading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground ml-1" />}
        {tl && !tlLoading && (
          <span className="text-[10px] text-muted-foreground ml-1">
            {tl.start_date} → {tl.end_date} · {tl.hpas.length} HPAs · {tl.nodes.length} dias
            {!hasEfficiency && <span className="text-yellow-600 ml-2">⚠ Ative Prometheus para eficiência real</span>}
          </span>
        )}
      </div>

      {/* ── 3. Custo Diário × Eficiência | Custo por Namespace ───────────────── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card>
        <CardHeader className="pb-1 pt-3 px-4">
          <CardTitle className="text-sm flex items-center gap-2">
            <CircleDollarSign className="h-4 w-4 text-amber-500" />
            Custo Diário × Eficiência de Uso
            <span className="text-[10px] font-normal text-muted-foreground">
              {hasEfficiency ? "barras = custo R$/dia · linhas = eficiência % · tracejado = projeção 30d" : "barras = custo estimado R$/dia · tracejado = projeção 30d (regressão linear)"}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent className="px-2 pb-3">
          {mainTimeline.length === 0 ? (
            <div className="flex items-center justify-center h-[210px] text-xs text-muted-foreground gap-2">
              {tlLoading ? <><Loader2 className="h-4 w-4 animate-spin" />Consultando Prometheus…</> : <><Activity className="h-4 w-4 opacity-30" />Sem dados de timeline (Prometheus inacessível)</>}
            </div>
          ) : (
            <>
            {(hasEfficiency || mainTimelineExt.some(p => p.projecao !== undefined)) && (
              <div className="flex items-center gap-4 px-3 mb-1 flex-wrap">
                {hasEfficiency && <>
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 h-0.5 bg-indigo-500 rounded" />CPU utilizado % (real/request)
                  </span>
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 h-0.5 bg-emerald-500 rounded" />Mem utilizado % (real/request)
                  </span>
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 border-t-2 border-dashed border-red-400" />35% — alerta desperdício
                  </span>
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 border-t-2 border-dashed border-green-500 opacity-60" />70% — uso saudável
                  </span>
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 border-t-2 border-amber-400" />100% — limite do request (acima = risco OOM)
                  </span>
                </>}
                {mainTimelineExt.some(p => p.projecao !== undefined) && (
                  <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                    <span className="inline-block w-5 border-t-2 border-dashed border-amber-400 opacity-60" />Projeção 30d (tendência)
                  </span>
                )}
              </div>
            )}
            <ResponsiveContainer width="100%" height={hasEfficiency ? 190 : 170}>
              <ComposedChart data={mainTimelineExt} margin={{ left: 8, right: hasEfficiency ? 44 : 8, top: 4, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                <YAxis yAxisId="cost" tickFormatter={v => `R$${v >= 1000 ? (v/1000).toFixed(1)+"k" : v}`}
                  tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={60} />
                {hasEfficiency && (
                  <YAxis yAxisId="eff" orientation="right" unit="%" allowDataOverflow={false}
                    tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} width={40} />
                )}
                <Tooltip content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  const custo  = payload.find(p => p.name === "custo");
                  const proj   = payload.find(p => p.name === "projecao");
                  const cpuE   = payload.find(p => p.name === "cpuEff");
                  const memE   = payload.find(p => p.name === "memEff");
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 4 }}>{label}</p>
                      {custo && <p style={{ color: "#f59e0b" }}>Custo estimado: {fmtBRL(custo.value as number)}</p>}
                      {proj && !custo && <p style={{ color: "#f59e0b", opacity: 0.7 }}>Projeção: {fmtBRL(proj.value as number)}</p>}
                      {cpuE  && <p style={{ color: "#6366f1" }}>CPU utilizado: {cpuE.value}% do request{(cpuE.value as number) > 100 ? " ⚠ over-request!" : ""}</p>}
                      {memE  && <p style={{ color: "#10b981" }}>Mem utilizada: {memE.value}% do request{(memE.value as number) > 100 ? " ⚠ risco OOM!" : ""}</p>}
                      {cpuE && (cpuE.value as number) < 35 && <p style={{ color: "#f87171", fontWeight: 600 }}>⚠ CPU abaixo de 35% — alto desperdício</p>}
                    </div>
                  );
                }} />
                <Bar yAxisId="cost" dataKey="custo" name="custo" fill="#f59e0b" fillOpacity={0.65} radius={[2,2,0,0]} maxBarSize={16} />
                {/* Linha de projeção de custo (tracejada) */}
                {mainTimelineExt.some(p => p.projecao !== undefined) && (
                  <Line yAxisId="cost" dataKey="projecao" name="projecao" type="monotone"
                    stroke="#f59e0b" strokeWidth={1.5} strokeDasharray="6 3" dot={false}
                    strokeOpacity={0.6} connectNulls legendType="none" />
                )}
                {hasEfficiency && <>
                  <Line yAxisId="eff" dataKey="cpuEff" name="cpuEff" type="monotone"
                    stroke="#6366f1" strokeWidth={2} dot={false} connectNulls legendType="none" />
                  <Line yAxisId="eff" dataKey="memEff" name="memEff" type="monotone"
                    stroke="#10b981" strokeWidth={2} dot={false} connectNulls legendType="none" />
                  <ReferenceLine yAxisId="eff" y={35} stroke="#ef4444" strokeDasharray="5 3" strokeOpacity={0.6}
                    label={{ value: "35%", position: "insideTopLeft", fontSize: 9, fill: "#ef4444" }} />
                  <ReferenceLine yAxisId="eff" y={70} stroke="#22c55e" strokeDasharray="5 3" strokeOpacity={0.5}
                    label={{ value: "70%", position: "insideTopLeft", fontSize: 9, fill: "#22c55e" }} />
                  <ReferenceLine yAxisId="eff" y={100} stroke="#f59e0b" strokeOpacity={0.7}
                    label={{ value: "100%", position: "insideTopLeft", fontSize: 9, fill: "#f59e0b" }} />
                </>}
              </ComposedChart>
            </ResponsiveContainer>
            </>
          )}
        </CardContent>
      </Card>

        {/* Namespace: custo com breakdown de veredicto */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Layers className="h-4 w-4 text-purple-500" />
              Custo por Namespace — distribuição de saúde
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            <ResponsiveContainer width="100%" height={Math.max(160, nsBreakdown.length * 28 + 20)}>
              <BarChart data={nsBreakdown} layout="vertical" margin={{ left: 6, right: 70, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.3} />
                <XAxis type="number"
                  tickFormatter={v => v === 0 ? "R$0" : v >= 1000 ? `R$${(v/1000).toFixed(0)}k` : `R$${Math.round(v)}`}
                  tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} />
                <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={110} axisLine={false} tickLine={false} />
                <Tooltip cursor={{ fill: "rgba(100,100,100,0.1)" }} content={({ active, payload, label }) => {
                  if (!active || !payload?.length) return null;
                  const extra = nsExtraMap.get(label as string);
                  return (
                    <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                      <p style={{ fontWeight: 600, marginBottom: 4 }}>{label}</p>
                      <p style={{ color: "var(--foreground)" }}>Total: {extra ? fmtBRL(extra.total) : "—"}/mês</p>
                      {payload.map((p, i) => (p.value as number) > 0 && (
                        <p key={i} style={{ color: p.color }}>{p.name}: {fmtBRL(p.value as number)}</p>
                      ))}
                      {extra && extra.wastePct > 0 && (
                        <p style={{ color: "#f87171", fontWeight: 600, marginTop: 4 }}>{extra.wastePct}% potencialmente desperdiçado</p>
                      )}
                    </div>
                  );
                }} />
                <Bar dataKey="eficiente"   name="Eficiente"   stackId="a" fill="#10b981" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="desperdicio" name="Desperdício"  stackId="a" fill="#ef4444" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="risco"       name="Risco OOM"   stackId="a" fill="#f59e0b" fillOpacity={0.8} maxBarSize={20} />
                <Bar dataKey="sem_req"     name="Sem Request"  stackId="a" fill="#9ca3af" fillOpacity={0.8} maxBarSize={20} radius={[0,3,3,0]} />
              </BarChart>
            </ResponsiveContainer>
            <div className="flex gap-3 mt-1 px-2 flex-wrap">
              {[["#10b981","Eficiente"],["#ef4444","Desperdício"],["#f59e0b","Risco OOM"],["#9ca3af","Sem Request"]].map(([c, l]) => (
                <span key={l} className="flex items-center gap-1 text-[10px] text-muted-foreground">
                  <span className="w-2 h-2 rounded-sm inline-block" style={{background: c}} />{l}
                </span>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* ── 4. HPA Réplicas | Nodes Ready ────────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* HPA Réplicas */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Activity className="h-4 w-4 text-violet-500" />
              HPA Réplicas — top {top8HPAs.length}
              <span className="text-[10px] font-normal text-muted-foreground">(avg diário)</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {hpaChartData.length === 0 ? (
              <div className="flex items-center justify-center h-[170px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem HPAs com histórico (Prometheus)"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={170}>
                <ComposedChart data={hpaChartData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                        <p className="font-medium mb-1">{label}</p>
                        {payload.filter(p => (p.value as number) > 0).map((p, i) => {
                          const idx = parseInt((p.dataKey as string).replace("h",""));
                          return <p key={i} style={{ color: p.color }}>{top8HPAs[idx]?.workload}: {p.value} répl.</p>;
                        })}
                      </div>
                    );
                  }} />
                  <Legend iconType="plainline" iconSize={12} wrapperStyle={{ fontSize: 9, paddingTop: 4 }}
                    formatter={(_v: unknown, entry: { dataKey?: unknown }) => {
                      const idx = parseInt(((entry.dataKey as string) ?? "").replace("h",""));
                      return top8HPAs[idx]?.workload ?? String(entry.dataKey ?? "");
                    }} />
                  {top8HPAs.map((_, i) => (
                    <Line key={i} type="monotone" dataKey={`h${i}`}
                      stroke={LINE_COLORS[i % LINE_COLORS.length]}
                      strokeWidth={1.5} dot={false} connectNulls />
                  ))}
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        {/* Nodes Ready */}
        <Card>
          <CardHeader className="pb-1 pt-3 px-4">
            <CardTitle className="text-sm flex items-center gap-2">
              <Server className="h-4 w-4 text-cyan-500" />
              Nodes Ready
              <span className="text-[10px] font-normal text-muted-foreground">custo estimado no tooltip</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="px-2 pb-3">
            {nodesData.length === 0 ? (
              <div className="flex items-center justify-center h-[170px] text-xs text-muted-foreground gap-2">
                {tlLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Server className="h-4 w-4 opacity-30" />}
                {tlLoading ? "Carregando…" : "Sem dados Prometheus"}
              </div>
            ) : (
              <ResponsiveContainer width="100%" height={170}>
                <ComposedChart data={nodesData} margin={{ left: 4, right: 8, top: 4, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: "#9ca3af" }} axisLine={false} tickLine={false} interval="preserveStartEnd" />
                  <YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false} width={24} allowDecimals={false} />
                  <Tooltip content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    const nodes = payload[0]?.value as number;
                    return (
                      <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                        <p className="font-medium">{label}</p>
                        <p className="text-cyan-500">{nodes} nodes</p>
                        <p className="text-amber-500">≈ {fmtBRL(nodes * costPerNodePerDay)}/dia</p>
                        <p className="text-muted-foreground">≈ {fmtBRL(nodes * costPerNodePerDay * 30)}/mês projetado</p>
                      </div>
                    );
                  }} />
                  <Area type="stepAfter" dataKey="Nodes" fill="#06b6d4" stroke="#06b6d4"
                    fillOpacity={0.15} strokeWidth={2} dot={false} />
                  <ReferenceLine y={totalNodes} stroke="#f59e0b" strokeDasharray="4 2" strokeOpacity={0.7}
                    label={{ value: `atual: ${totalNodes}`, position: "right", fontSize: 9, fill: "#f59e0b" }} />
                </ComposedChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      {/* ── 5. Oportunidades de Economia (último painel) ──────────────────────── */}
      <Card className="border-amber-200 dark:border-amber-900/40">
        <CardHeader className="pb-1 pt-3 px-4">
          <CardTitle className="text-sm flex items-center justify-between">
            <span className="flex items-center gap-2">
              <TrendingDown className="h-4 w-4 text-red-500" />
              Oportunidades de Economia
              {windowDays > 0 && (
                <span className="text-[10px] font-normal text-muted-foreground">({windowDays}d de histórico Prometheus)</span>
              )}
            </span>
            {opTotalSaving > 0 && (
              <span className="text-[10px] font-semibold text-green-600 dark:text-green-400">
                Potencial total: {fmtBRL(opTotalSaving)}/mês
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {opportunities.length === 0 ? (
            <div className="flex items-center justify-center py-8 text-xs text-muted-foreground gap-2">
              <CheckCircle2 className="h-4 w-4 text-green-500" />
              Nenhuma oportunidade significativa identificada
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="text-[10px] text-muted-foreground">
                  <TableHead className="pl-4">Workload</TableHead>
                  <TableHead>Recomendação concreta</TableHead>
                  <TableHead className="text-right">Custo atual</TableHead>
                  <TableHead className="text-right text-green-600">Economia/mês</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {opportunities.map((w, i) => (
                  <TableRow key={i} className={`text-xs align-top ${
                    w.verdict === "superprovisioned" ? "bg-red-50/40 dark:bg-red-950/10" :
                    w.verdict === "oom_risk"         ? "bg-yellow-50/40 dark:bg-yellow-950/10" :
                    w.verdict === "hpa_removable"    ? "bg-purple-50/40 dark:bg-purple-950/10" : ""
                  }`}>
                    <TableCell className="pl-4 py-2 min-w-[130px]">
                      <p className="font-medium truncate max-w-[160px]">{w.workload}</p>
                      <p className="text-[10px] text-muted-foreground truncate max-w-[160px]">{w.namespace}</p>
                      <div className="mt-1">
                        <VerdictBadge verdict={w.verdict} />
                      </div>
                      {w.hpa_max > 0 && (
                        <p className="text-[10px] text-muted-foreground mt-1 font-mono">
                          HPA: {w.hpa_min}↔{w.hpa_current}↔{w.hpa_max}
                          {w.hpa_avg_replicas ? ` · avg ${w.hpa_avg_replicas.toFixed(1)}` : ""}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="py-2 max-w-[340px]">
                      <div className="space-y-0.5">
                        {w.rec.lines.map((line, li) => (
                          <p key={li} className={`text-[11px] leading-snug ${
                            line.highlight
                              ? "font-semibold text-foreground"
                              : "text-muted-foreground"
                          }`}>
                            {line.highlight && (
                              <span className="inline-block w-1.5 h-1.5 rounded-full bg-green-500 mr-1.5 mb-px" />
                            )}
                            {line.text}
                          </p>
                        ))}
                        {(w.rec.kubectlList ?? []).length > 0 && (
                          <p className="text-[10px] font-mono bg-muted/50 rounded px-1.5 py-0.5 mt-1 truncate text-muted-foreground" title={w.rec.kubectlList[0]}>
                            $ {w.rec.kubectlList[0]}
                          </p>
                        )}
                        {w.rec.safeMin !== undefined && w.hpa_min > 0 && (
                          <p className="text-[10px] text-green-600 dark:text-green-400 font-medium mt-0.5">
                            Economia estimada: {fmtBRL((w.hpa_min - w.rec.safeMin) * (w.pods > 0 ? w.cost_share_brl / w.pods : 0))}/mês por réplica removida
                          </p>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-mono text-[11px] py-2 whitespace-nowrap">
                      {fmtBRL(w.cost_share_brl)}
                      {w.hpa_cost_min_brl > 0 && w.hpa_cost_min_brl < w.cost_share_brl && (
                        <p className="text-[10px] text-muted-foreground">
                          mín: {fmtBRL(w.hpa_cost_min_brl)}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="text-right py-2 whitespace-nowrap">
                      {w.saving > 0
                        ? (
                          <div>
                            <span className="text-green-600 font-bold text-sm">-{fmtBRL(w.saving)}</span>
                            <p className="text-[10px] text-muted-foreground">/ano: -{fmtBRL(w.saving * 12)}</p>
                          </div>
                        )
                        : <span className="text-muted-foreground font-mono text-[11px]">—</span>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {opTotalSaving > 0 && (
            <div className="px-4 py-2 border-t bg-green-50/50 dark:bg-green-950/10 flex justify-between items-center">
              <span className="text-[10px] text-muted-foreground">Aplicando todas as recomendações acima:</span>
              <span className="text-xs font-bold text-green-600">
                -{fmtBRL(opTotalSaving)}/mês · -{fmtBRL(opTotalSaving * 12)}/ano
              </span>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
