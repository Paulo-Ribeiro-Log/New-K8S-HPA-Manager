// F5.1 (FINOPS-IMPROVEMENTS-PLAN.md) — extraído de FinOpsTab.tsx (mesmo padrão já usado pra
// RightsizingTab.tsx/DataResourcesPanel.tsx), sem mudança de comportamento.

import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, LabelList } from "recharts";
import { TrendingDown, CheckCircle2, ChevronDown, ChevronUp } from "lucide-react";
import { fmtBRL, fmtMillis, fmtMi, KubectlBlock, VerdictBadge } from "@/lib/finopsFormat";
import type { FinOpsSummary, FinOpsWorkload, Recommendation } from "./types";
import { buildRecommendation } from "./helpers";

const ACTION_TYPE_LABELS: Record<string, string> = {
  hpa_min:           "Reduzir HPA min",
  hpa_max:           "Reduzir HPA max",
  hpa_and_resources: "HPA + Resources",
  remove_hpa:        "Remover HPA",
  add_hpa:           "Adicionar HPA",
  resources:         "Ajustar Resources",
  oom_risk:          "Risco OOM",
  no_request:        "Definir Requests",
  other:             "Outros",
};
const GROUP_ORDER = ["hpa_min", "hpa_and_resources", "hpa_max", "remove_hpa", "add_hpa", "resources", "oom_risk", "no_request", "other"];

function deriveActionType(w: FinOpsWorkload, rec: Recommendation): string {
  if (w.verdict === "hpa_removable")   return "remove_hpa";
  if (w.verdict === "no_request")      return "no_request";
  if (w.verdict === "oom_risk")        return "oom_risk";
  if (w.verdict === "fixed_high_cost") return "add_hpa";
  const hasHpaMin = rec.kubectlList.some(c => c.includes("minReplicas"));
  const hasHpaMax = rec.kubectlList.some(c => c.includes("maxReplicas"));
  const hasRes    = rec.kubectlList.some(c => c.includes("set resources"));
  if (hasHpaMin && hasRes) return "hpa_and_resources";
  if (hasHpaMin)           return "hpa_min";
  if (hasHpaMax)           return "hpa_max";
  if (hasRes)              return "resources";
  return "other";
}

export function OpportunitiesTab({ workloads, windowDays, historicalRan = true }: {
  workloads: FinOpsWorkload[];
  summary?: FinOpsSummary;
  windowDays: number;
  // A "Análise histórica" rodou neste relatório (report.window_days > 0) — muda o motivo mostrado
  // quando falta histórico de HPA (ver buildRecommendation).
  historicalRan?: boolean;
}) {
  const hasPrometheus = workloads.some(w => (w.waste_brl ?? 0) > 0 || (w.hpa_avg_replicas ?? 0) > 0);

  // ── Filtros (2a) ─────────────────────────────────────────────────────────────
  const [filterNs, setFilterNs]             = useState("all");
  const [filterType, setFilterType]         = useState("all");
  const [minSaving, setMinSaving]           = useState(0);
  const [onlyWithKubectl, setOnlyWithKubectl] = useState(false);
  // ── Seleção (2b) ─────────────────────────────────────────────────────────────
  const [selectedKeys, setSelectedKeys]     = useState<Set<string>>(new Set());
  // ── Grupos (2c) ──────────────────────────────────────────────────────────────
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set());

  type OpItem = FinOpsWorkload & { rec: Recommendation; saving: number; actionType: string };

  // ── Build lista completa de oportunidades ─────────────────────────────────────
  const allOpportunities: OpItem[] = workloads
    .filter(w => w.verdict === "superprovisioned" || w.verdict === "hpa_removable" ||
                 w.verdict === "no_request" || w.verdict === "oom_risk" ||
                 w.verdict === "fixed_high_cost" || (w.waste_brl ?? 0) > 0)
    .map(w => {
      const rec = buildRecommendation(w, windowDays, historicalRan);
      const saving = rec.savingBRL > 0
        ? rec.savingBRL
        : Math.max(0, w.hpa_cost_current_brl - w.hpa_cost_min_brl);
      return { ...w, rec, saving, actionType: deriveActionType(w, rec) };
    })
    .sort((a, b) => b.saving !== a.saving ? b.saving - a.saving : b.rec.exposureBRL - a.rec.exposureBRL);

  const totalSaving          = allOpportunities.reduce((s, o) => s + o.saving, 0);
  const totalExposure        = allOpportunities.reduce((s, o) => s + o.rec.exposureBRL, 0);
  const totalWaste           = workloads.reduce((s, w) => s + (w.waste_brl ?? 0), 0);
  const needsPrometheusCount = allOpportunities.filter(o => o.rec.needsPrometheus).length;

  // ── Namespaces únicos para filtro ─────────────────────────────────────────────
  const namespaces = [...new Set(allOpportunities.map(o => o.namespace))].sort();

  // ── Aplicar filtros ───────────────────────────────────────────────────────────
  const itemKey = (o: OpItem) => `${o.namespace}/${o.workload}`;

  const opportunities = allOpportunities.filter(o => {
    if (filterNs !== "all" && o.namespace !== filterNs) return false;
    if (filterType !== "all" && o.actionType !== filterType) return false;
    if (minSaving > 0 && o.saving < minSaving) return false;
    if (onlyWithKubectl && o.rec.kubectlList.length === 0) return false;
    return true;
  });

  // ── Seleção helpers ───────────────────────────────────────────────────────────
  const selectedSaving      = opportunities.filter(o => selectedKeys.has(itemKey(o))).reduce((s, o) => s + o.saving, 0);
  const allFilteredSelected = opportunities.length > 0 && opportunities.every(o => selectedKeys.has(itemKey(o)));

  const toggleItem = (key: string) =>
    setSelectedKeys(prev => { const n = new Set(prev); n.has(key) ? n.delete(key) : n.add(key); return n; });

  const toggleAll = () => {
    const keys = opportunities.map(itemKey);
    if (allFilteredSelected)
      setSelectedKeys(prev => { const n = new Set(prev); keys.forEach(k => n.delete(k)); return n; });
    else
      setSelectedKeys(prev => new Set([...prev, ...keys]));
  };

  // ── Agrupamento (2c) ──────────────────────────────────────────────────────────
  const groups = GROUP_ORDER
    .map(type => ({ type, label: ACTION_TYPE_LABELS[type] ?? type, items: opportunities.filter(o => o.actionType === type) }))
    .filter(g => g.items.length > 0);

  const toggleGroup = (type: string) =>
    setCollapsedGroups(prev => { const n = new Set(prev); n.has(type) ? n.delete(type) : n.add(type); return n; });

  // ── Render card ───────────────────────────────────────────────────────────────
  const renderCard = (w: OpItem) => {
    const key = itemKey(w);
    const isSelected = selectedKeys.has(key);
    const borderColor =
      w.verdict === "oom_risk"          ? "#f59e0b"
      : w.verdict === "superprovisioned" ? "#ef4444"
      : w.verdict === "hpa_removable"    ? "#8b5cf6"
      : "#9ca3af";
    return (
      <Card key={key} className={`border-l-4 transition-shadow ${isSelected ? "ring-2 ring-primary/40" : ""}`}
        style={{ borderLeftColor: borderColor }}>
        <CardContent className="p-3">
          {/* Cabeçalho: checkbox + workload + saving */}
          <div className="flex items-start gap-2 mb-2">
            <input type="checkbox" checked={isSelected} onChange={() => toggleItem(key)}
              className="mt-1 h-3.5 w-3.5 shrink-0 cursor-pointer rounded" />
            <div className="flex-1 flex items-start justify-between gap-3 min-w-0">
              <div className="flex items-center gap-2 flex-wrap min-w-0">
                <span className="font-semibold text-sm">{w.workload}</span>
                <span className="text-xs text-muted-foreground">{w.namespace}</span>
                <VerdictBadge verdict={w.verdict} />
              </div>
              <div className="text-right flex-shrink-0 space-y-0.5">
                <p className="text-[10px] text-muted-foreground">custo atual</p>
                <p className="text-sm font-semibold">{fmtBRL(w.cost_share_brl)}/mês</p>
                {(w.avg_replicas_cost_brl ?? 0) > 0 && w.avg_replicas_cost_brl !== w.cost_share_brl && (
                  <p className="text-[10px] text-muted-foreground">
                    custo real (avg répl.): <span className="text-foreground font-medium">{fmtBRL(w.avg_replicas_cost_brl!)}</span>
                  </p>
                )}
                {w.saving > 0 ? (
                  <>
                    <p className="text-[10px] text-muted-foreground mt-1">economia estimada</p>
                    <p className="text-base font-bold text-green-600">-{fmtBRL(w.saving)}/mês</p>
                    <p className="text-[10px] text-muted-foreground">/ano: -{fmtBRL(w.saving * 12)}</p>
                  </>
                ) : w.rec.exposureBRL > 0 ? (
                  <>
                    <p className="text-[10px] text-amber-600 dark:text-amber-400 mt-1">exposição máxima</p>
                    <p className="text-sm font-bold text-amber-600">+{fmtBRL(w.rec.exposureBRL)}/mês</p>
                    <p className="text-[10px] text-muted-foreground">(se escalar ao max={w.hpa_max})</p>
                  </>
                ) : null}
              </div>
            </div>
          </div>

          {/* Estado atual */}
          <div className="text-xs text-muted-foreground mb-2 space-y-0.5">
            {w.hpa_max > 0 && (
              <p>
                HPA configurado: <span className="font-mono">{w.hpa_min} (min) / {w.hpa_current} (atual) / {w.hpa_max} (max)</span>
                <span className="ml-1">— atual em <strong>{Math.round((w.hpa_current / w.hpa_max) * 100)}% do máximo</strong></span>
                {w.hpa_avg_replicas != null && <span className="ml-1">· média {windowDays}d: <strong>{w.hpa_avg_replicas.toFixed(1)}</strong> répl.</span>}
                {w.hpa_max_observed != null && w.hpa_max_observed > 0 && <span className="ml-1">· pico observado: <strong>{w.hpa_max_observed}</strong></span>}
              </p>
            )}
            {(w.cpu_p95_millis ?? 0) > 0 && (
              <p className="flex items-center gap-1 flex-wrap">
                <span>CPU — request: <span className="font-mono">{fmtMillis(w.cpu_request_millis)}</span>
                  {" · "}P95: <span className="font-mono">{fmtMillis(w.cpu_p95_millis!)}</span>
                  {w.cpu_recommended_millis && <span> · recomendado: <span className="font-mono text-amber-600">{fmtMillis(w.cpu_recommended_millis)}</span></span>}
                </span>
                {w.metrics_source === "dynatrace" && <span className="text-[9px] font-medium px-1 py-0.5 rounded bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">DT</span>}
                {w.metrics_source === "prometheus" && <span className="text-[9px] font-medium px-1 py-0.5 rounded bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300">Prom</span>}
              </p>
            )}
            {(w.mem_p95_mi ?? 0) > 0 && (
              <p>Mem — request: <span className="font-mono">{fmtMi(w.mem_request_mi)}</span>
                {" · "}P95: <span className="font-mono">{fmtMi(w.mem_p95_mi!)}</span>
                {w.mem_recommended_mi && <span> · recomendado: <span className="font-mono text-amber-600">{fmtMi(w.mem_recommended_mi)}</span></span>}
              </p>
            )}
            {w.verdict === "no_request" && <p>Sem resource requests definidos — custo alocado por heurística</p>}
          </div>

          {/* Recomendação concreta */}
          <div className="border rounded-md bg-muted/30 px-3 py-2 space-y-1">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground mb-1">Ação recomendada</p>
            {w.rec.lines.map((line, li) => (
              <p key={li} className={`text-xs leading-snug flex items-start gap-1.5 ${line.highlight ? "font-semibold text-foreground" : "text-muted-foreground"}`}>
                <span className={`mt-1 shrink-0 w-2 h-2 rounded-full inline-block ${line.highlight ? "bg-green-500" : "bg-muted-foreground/30"}`} />
                {line.text}
              </p>
            ))}
            {w.rec.kubectlList.length > 0 && (
              <div className="mt-1.5 space-y-1.5">
                {w.rec.kubectlList.map((cmd, ci) => <KubectlBlock key={ci} cmd={cmd} />)}
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    );
  };

  // ── Estado vazio ──────────────────────────────────────────────────────────────
  if (allOpportunities.length === 0) {
    return (
      <div className="space-y-3">
        <Card>
          <CardContent className="p-8 text-center text-muted-foreground">
            <CheckCircle2 className="h-10 w-10 mx-auto mb-2 text-green-500" />
            <p className="font-medium">Nenhuma oportunidade de saving identificada</p>
            <p className="text-sm mt-1">Todos os workloads estão classificados como eficientes.</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* ── Banner de resumo ─────────────────────────────────────────────────── */}
      <Alert className={totalWaste > 0 ? "border-red-200 bg-red-50 dark:bg-red-950/20" : totalSaving > 0 ? "border-green-200 bg-green-50 dark:bg-green-950/20" : "border-amber-200 bg-amber-50 dark:bg-amber-950/20"}>
        <TrendingDown className={`h-4 w-4 ${totalWaste > 0 ? "text-red-600" : totalSaving > 0 ? "text-green-600" : "text-amber-600"}`} />
        <AlertDescription className="text-sm space-y-1">
          <span>
            <strong>{allOpportunities.length} oportunidades</strong> identificadas
            {needsPrometheusCount > 0 && (
              <span className="text-muted-foreground text-[11px]">
                {" "}({needsPrometheusCount} {historicalRan ? "sem histórico de réplicas do HPA para quantificar" : 'precisam da "Análise histórica" para quantificar'})
              </span>
            )}
            {"."}
          </span>
          {totalWaste > 0 && <span className="block">Desperdício real (P95): <strong className="text-red-600">{fmtBRL(totalWaste)}/mês = {fmtBRL(totalWaste * 12)}/ano</strong></span>}
          {totalSaving > 0 && <span className="block">Economia imediata estimada: <strong className="text-green-600">{fmtBRL(totalSaving)}/mês = {fmtBRL(totalSaving * 12)}/ano</strong></span>}
          {!hasPrometheus && totalExposure > 0 && (
            <span className="block text-amber-700 dark:text-amber-400">
              Exposição máxima: <strong>{fmtBRL(totalExposure)}/mês</strong> — ative Prometheus para recomendar valores seguros
            </span>
          )}
        </AlertDescription>
      </Alert>

      {/* ── Chart de economia (top 10) ───────────────────────────────────────── */}
      {allOpportunities.length > 1 && (() => {
        const chartData = allOpportunities.slice(0, 10).map(w => ({
          name: w.workload.length > 16 ? w.workload.slice(0, 14) + "…" : w.workload,
          atual: w.cost_share_brl,
          saving: w.saving,
        })).filter(d => d.saving > 0);
        if (!chartData.length) return null;
        return (
          <Card>
            <CardHeader className="pb-1 pt-3 px-4">
              <CardTitle className="text-sm">Potencial de Economia por Workload (top {chartData.length})</CardTitle>
            </CardHeader>
            <CardContent className="px-2 pb-3">
              <ResponsiveContainer width="100%" height={Math.max(160, chartData.length * 26)}>
                <BarChart data={chartData} layout="vertical" margin={{ left: 8, right: 70, top: 4, bottom: 4 }}>
                  <CartesianGrid strokeDasharray="3 3" horizontal={false} opacity={0.4} />
                  <XAxis type="number"
                    tickFormatter={v => v === 0 ? "R$0" : v >= 1000 ? `R$${(v/1000).toFixed(0)}k` : `R$${Math.round(v)}`}
                    tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
                  <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={110} axisLine={false} tickLine={false} />
                  <Tooltip cursor={{ fill: "rgba(100,100,100,0.1)" }}
                    content={({ active, payload, label }) => {
                      if (!active || !payload?.length) return null;
                      return (
                        <div style={{ background: "hsl(var(--card) / 0.97)", backdropFilter: "blur(8px)", border: "1px solid var(--border)", borderRadius: 8, padding: "8px 12px", fontSize: 12, boxShadow: "0 4px 16px rgba(0,0,0,0.4)" }}>
                          <p className="font-medium">{label}</p>
                          {payload.map((p, i) => (
                            <p key={i} style={{ color: p.color }}>
                              {p.name === "atual" ? `Custo atual: ${fmtBRL(p.value as number)}` : `Economia estimada: ${fmtBRL(p.value as number)}`}
                            </p>
                          ))}
                        </div>
                      );
                    }}
                  />
                  <Bar dataKey="atual" name="atual" stackId="a" fill="#6366f1" fillOpacity={0.25} maxBarSize={18} />
                  <Bar dataKey="saving" name="saving" stackId="a" fill="#ef4444" fillOpacity={0.85} radius={[0,4,4,0]} maxBarSize={18}>
                    <LabelList dataKey="saving" position="right"
                      formatter={(v: number) => v > 0 ? `-${fmtBRL(v)}` : ""}
                      style={{ fontSize: 9, fill: "#16a34a" }} />
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        );
      })()}

      {/* ── Barra de filtros (2a) ────────────────────────────────────────────── */}
      <div className="flex items-center gap-2 flex-wrap p-2.5 rounded-lg border bg-muted/30">
        {/* Selecionar todos (visíveis) */}
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none border-r pr-2.5">
          <input type="checkbox" checked={allFilteredSelected} onChange={toggleAll}
            className="h-3.5 w-3.5 cursor-pointer rounded" />
          Todos
        </label>

        {/* Namespace */}
        <select value={filterNs} onChange={e => setFilterNs(e.target.value)}
          className="h-7 text-xs px-2 rounded border bg-background">
          <option value="all">Todos namespaces</option>
          {namespaces.map(ns => <option key={ns} value={ns}>{ns}</option>)}
        </select>

        {/* Tipo de ação */}
        <select value={filterType} onChange={e => setFilterType(e.target.value)}
          className="h-7 text-xs px-2 rounded border bg-background">
          <option value="all">Todos os tipos</option>
          {Object.entries(ACTION_TYPE_LABELS).map(([k, v]) => (
            <option key={k} value={k}>{v}</option>
          ))}
        </select>

        {/* Saving mínimo */}
        <select value={String(minSaving)} onChange={e => setMinSaving(Number(e.target.value))}
          className="h-7 text-xs px-2 rounded border bg-background">
          <option value="0">Qualquer saving</option>
          <option value="50">≥ R$50/mês</option>
          <option value="100">≥ R$100/mês</option>
          <option value="250">≥ R$250/mês</option>
          <option value="500">≥ R$500/mês</option>
        </select>

        {/* Só com kubectl */}
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
          <input type="checkbox" checked={onlyWithKubectl} onChange={e => setOnlyWithKubectl(e.target.checked)}
            className="h-3.5 w-3.5 cursor-pointer rounded" />
          Só com kubectl
        </label>

        {/* Contador */}
        <span className="ml-auto text-[10px] text-muted-foreground">
          {opportunities.length === allOpportunities.length
            ? `${allOpportunities.length} oportunidades`
            : `${opportunities.length} de ${allOpportunities.length}`}
        </span>
      </div>

      {/* ── Sem resultados após filtro ────────────────────────────────────────── */}
      {opportunities.length === 0 ? (
        <Card>
          <CardContent className="py-8 text-center text-muted-foreground text-sm">
            Nenhuma oportunidade com os filtros selecionados
          </CardContent>
        </Card>
      ) : (
        /* ── Grupos colapsáveis (2c) ─────────────────────────────────────────── */
        <div className="space-y-3">
          {groups.map(group => {
            const isCollapsed = collapsedGroups.has(group.type);
            const groupSaving = group.items.reduce((s, o) => s + o.saving, 0);
            return (
              <div key={group.type}>
                <button
                  className="w-full flex items-center justify-between gap-2 px-3 py-1.5 rounded-t-md bg-muted/60 border border-b-0 text-left hover:bg-muted/80 transition-colors"
                  onClick={() => toggleGroup(group.type)}
                >
                  <div className="flex items-center gap-2">
                    {isCollapsed ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronUp className="h-3.5 w-3.5" />}
                    <span className="text-xs font-semibold">{group.label}</span>
                    <span className="text-[10px] text-muted-foreground">{group.items.length} item{group.items.length !== 1 ? "s" : ""}</span>
                  </div>
                  {groupSaving > 0 && (
                    <span className="text-xs font-semibold text-green-600">-{fmtBRL(groupSaving)}/mês</span>
                  )}
                </button>
                {!isCollapsed && (
                  <div className="space-y-2 border border-t-0 rounded-b-md p-2 bg-card">
                    {group.items.map(renderCard)}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* ── Barra sticky de seleção (2b) ─────────────────────────────────────── */}
      {selectedKeys.size > 0 && (
        <div className="sticky bottom-2 z-10 px-4 py-2.5 rounded-lg border bg-card shadow-lg flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <CheckCircle2 className="h-4 w-4 text-primary" />
            <span className="text-sm font-medium">
              {selectedKeys.size} item{selectedKeys.size !== 1 ? "s" : ""} selecionado{selectedKeys.size !== 1 ? "s" : ""}
            </span>
            <button onClick={() => setSelectedKeys(new Set())}
              className="text-[10px] text-muted-foreground hover:text-foreground underline">
              limpar
            </button>
          </div>
          <div className="flex items-center gap-2">
            {selectedSaving > 0 ? (
              <>
                <span className="text-sm font-bold text-green-600">-{fmtBRL(selectedSaving)}/mês</span>
                <span className="text-xs text-muted-foreground">· -{fmtBRL(selectedSaving * 12)}/ano</span>
              </>
            ) : (
              <span className="text-xs text-muted-foreground">{historicalRan ? "Sem histórico de HPA para quantificar" : 'Marque "Análise histórica" para quantificar'}</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
