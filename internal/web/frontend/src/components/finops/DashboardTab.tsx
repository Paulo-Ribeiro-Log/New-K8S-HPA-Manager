// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx). Fase 2 do FINOPS-REVISAO-PLAN.md: sem o card de
// oportunidades (Workloads → Oportunidades) e sem o gráfico de réplicas (aba HPA).

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  ComposedChart, Line, Area, ReferenceLine,
} from "recharts";
import {
  Loader2, Server, Layers, CircleDollarSign, Activity,
} from "lucide-react";
import { fmtBRL, fmtUSD } from "@/lib/finopsFormat";
import { DataResourcesPanel } from "@/components/DataResourcesPanel";
import type { FinOpsReport, TimelineReport } from "./types";
import { buildActionPlan, toEffectiveReport } from "./capacity";
import { apiClient } from "@/lib/api/client";


export function DashboardTab({ cluster, report, provider }: { cluster: string; report: FinOpsReport; provider?: string }) {
  // Valores em reais no custo efetivo dos pools com cobertura de reserva/Savings Plan.
  const view = toEffectiveReport(report);
  const effective = view !== report;
  const { summary } = view;
  const node_pools = view.node_pools ?? [];
  const workloads  = view.workloads  ?? [];
  const namespaces = view.namespaces ?? [];
  const [days, setDays] = useState(30);

  // ── Fetch timeline ──────────────────────────────────────────────────────────
  const { data: tl, isLoading: tlLoading } = useQuery<TimelineReport>({
    queryKey: ["finops-timeline-dashboard", cluster, days],
    queryFn: () => apiClient.getFinOpsTimeline(cluster, days),
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

  // Mesma economia do Relatório (capacity.ts), sem dupla contagem.
  const plan = buildActionPlan(report);
  const totalSavings = plan.totalSaving;

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
    // Soma dias à última data completa da série (YYYY-MM-DD) e devolve "MM-DD".
    const lastISO = [...(tl?.nodes ?? []), ...(tl?.cpu ?? []), ...(tl?.mem ?? [])].map(p => p.date).sort().pop() ?? "";
    const addDays = (iso: string, d: number) => {
      const dt = new Date(`${iso}T00:00:00`);
      dt.setDate(dt.getDate() + d);
      return `${String(dt.getMonth() + 1).padStart(2, "0")}-${String(dt.getDate()).padStart(2, "0")}`;
    };
    // Ponto de ponte: último ponto real também recebe projecao
    const extended: MainPointExt[] = mainTimeline.map(p => ({ ...p }));
    extended[extended.length - 1].projecao = Math.max(0, Math.round(intercept + slope * (n - 1)));
    // 30 dias de projeção
    for (let i = 1; i <= 30; i++) {
      extended.push({
        date: addDays(lastISO, i),
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
              {(summary.total_with_storage_brl ?? 0) > 0 ? "Compute/mês" : "Custo/mês"}{effective ? " (efetivo)" : ""}
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
            {effective && (
              <p className="text-[10px] text-muted-foreground" title="Pools com cobertura do Cost Management pelo custo efetivo (reserva/Savings Plan); demais pools e discos em preço de tabela">
                tabela: {fmtBRL(report.summary.total_monthly_cost_brl)}
              </p>
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
        <Card className="col-span-1 border-green-200 dark:border-green-900/40">
          <CardContent className="p-3">
            <p className="text-[10px] text-muted-foreground">Economia certa/mês</p>
            <p className="text-lg font-bold text-green-600 leading-tight">{fmtBRL(totalSavings)}</p>
            <p className="text-[10px] text-muted-foreground">
              {totalSavings === 0 && plan.actions.length > 0
                ? "as ações dependem de reaproveitar reservas"
                : `${(summary.total_with_storage_brl ?? summary.total_monthly_cost_brl) > 0 ? Math.round(totalSavings / (summary.total_with_storage_brl ?? summary.total_monthly_cost_brl) * 100) : 0}% do custo total`}
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
      {provider === "aks" && (
        <Card>
          <CardContent className="p-3">
            <DataResourcesPanel cluster={cluster} />
          </CardContent>
        </Card>
      )}

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
            {!hasEfficiency && <span className="text-yellow-600 ml-2">⚠ Sem eficiência real nesta série — rode Analisar com "Análise histórica" marcada</span>}
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
              {hasEfficiency ? "barras = custo estimado R$/dia (nodes × custo médio por node) · linhas = eficiência % · tracejado = projeção 30d" : "barras = custo estimado R$/dia (nodes × custo médio por node) · tracejado = projeção 30d (regressão linear)"}
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

      {/* ── 4. Nodes Ready ─────────────────────────────────────────────────────── */}
      <div className="grid grid-cols-1 gap-4">
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

    </div>
  );
}
