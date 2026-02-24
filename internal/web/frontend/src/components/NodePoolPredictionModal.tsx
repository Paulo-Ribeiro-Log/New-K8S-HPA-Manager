import { useState, useMemo } from "react";
import {
  LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceLine,
} from "recharts";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Separator } from "@/components/ui/separator";
import {
  TrendingUp, Loader2, X, Download, History,
  CheckCircle2, TriangleAlert, Activity, Server,
  DollarSign, Zap, ArrowUp, ArrowDown, Minus,
  Network, BarChart3, CalendarClock, Scale,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { generateNodePoolPredictionPDF } from "@/lib/nodePoolPdfGenerator";

interface NodePoolPredictionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  result: any;
  nodepoolName: string;
  onShowHistory?: () => void;
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function healthColor(score: number) {
  if (score >= 75) return "text-green-500";
  if (score >= 50) return "text-yellow-500";
  return "text-red-500";
}

function healthCategory(score: number) {
  if (score >= 85) return "Excelente";
  if (score >= 70) return "Bom";
  if (score >= 50) return "Regular";
  if (score >= 30) return "Crítico";
  return "Emergência";
}

function riskBadge(level: string) {
  const variants: Record<string, string> = {
    low: "bg-green-500/20 text-green-500 border-green-500/30",
    medium: "bg-yellow-500/20 text-yellow-500 border-yellow-500/30",
    high: "bg-orange-500/20 text-orange-500 border-orange-500/30",
    critical: "bg-red-500/20 text-red-500 border-red-500/30",
  };
  return variants[level] ?? variants.medium;
}

function conntrackColor(pct: number) {
  if (pct >= 85) return "text-red-500";
  if (pct >= 70) return "text-yellow-500";
  return "text-green-500";
}

function conntrackBg(pct: number) {
  if (pct >= 85) return "bg-red-500";
  if (pct >= 70) return "bg-yellow-500";
  return "bg-green-500";
}

function trendIcon(dir: string) {
  if (dir === "up") return <ArrowUp className="w-3 h-3 text-red-400" />;
  if (dir === "down") return <ArrowDown className="w-3 h-3 text-green-400" />;
  return <Minus className="w-3 h-3 text-muted-foreground" />;
}

function trendLabel(dir: string) {
  if (dir === "up") return "Crescendo";
  if (dir === "down") return "Decrescendo";
  return "Estável";
}

function severityBadgeVariant(sev: string): "destructive" | "default" | "secondary" | "outline" {
  if (sev === "critical" || sev === "high") return "destructive";
  if (sev === "medium") return "default";
  return "secondary";
}

function fmtPct(v: number | undefined) {
  if (v === undefined || v === null) return "—";
  return `${v.toFixed(1)}%`;
}

function fmtUSD(v: number | undefined) {
  if (v === undefined || v === null) return "—";
  return `$${v.toFixed(2)}`;
}

function fmtBRL(v: number | undefined) {
  if (v === undefined || v === null) return "—";
  return `R$ ${v.toFixed(2)}`;
}

// ── Chart helpers ──────────────────────────────────────────────────────────────

/** Converte array de TrendSnapshot (Go) em dados para Recharts, do mais antigo ao mais recente */
function buildTrendChartData(snapshots: any[] | undefined) {
  if (!snapshots?.length) return null;
  return [...snapshots]
    .sort((a: any, b: any) => b.days_ago - a.days_ago) // D-14 primeiro, D-0 por último
    .map((s: any) => ({
      label: s.days_ago === 0 ? "Hoje" : `D-${s.days_ago}`,
      value: Math.round((s.value_per_node ?? 0) * 10) / 10,
    }));
}

function trendLineColor(dir: string) {
  if (dir === "up" || dir === "increasing") return "#f97316"; // laranja
  if (dir === "down" || dir === "decreasing") return "#22c55e"; // verde
  return "#6366f1"; // índigo (estável)
}

interface TrendMiniChartProps {
  data: { label: string; value: number }[];
  color: string;
  unit?: string;
  showThreshold?: boolean;
}

function TrendMiniChart({ data, color, unit = "%", showThreshold = false }: TrendMiniChartProps) {
  const maxVal = Math.max(...data.map((d) => d.value), showThreshold ? 90 : 0);
  const domain: [number, number] = [0, Math.ceil(Math.max(maxVal * 1.1, 10) / 10) * 10];
  return (
    <ResponsiveContainer width="100%" height={90}>
      <LineChart data={data} margin={{ top: 4, right: 6, left: -22, bottom: 0 }}>
        <XAxis
          dataKey="label"
          tick={{ fontSize: 10, fill: "#9ca3af" }}
          axisLine={false}
          tickLine={false}
        />
        <YAxis
          domain={domain}
          tick={{ fontSize: 10, fill: "#9ca3af" }}
          axisLine={false}
          tickLine={false}
          tickFormatter={(v) => `${v}${unit === "%" ? "%" : ""}`}
        />
        <Tooltip
          contentStyle={{
            fontSize: 11,
            background: "hsl(var(--background))",
            border: "1px solid hsl(var(--border))",
            borderRadius: 6,
            padding: "4px 8px",
          }}
          formatter={(v: any) => [`${v}${unit}`, ""]}
          labelStyle={{ color: "#9ca3af" }}
        />
        {showThreshold && (
          <ReferenceLine
            y={85}
            stroke="#ef4444"
            strokeDasharray="3 3"
            strokeWidth={1}
            label={{ value: "85%", position: "right", fontSize: 9, fill: "#ef4444" }}
          />
        )}
        <Line
          type="monotone"
          dataKey="value"
          stroke={color}
          strokeWidth={2}
          dot={{ r: 3, fill: color, strokeWidth: 0 }}
          activeDot={{ r: 4 }}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}

// ── Componente principal ───────────────────────────────────────────────────────

export function NodePoolPredictionModal({
  open,
  onOpenChange,
  loading,
  result,
  nodepoolName,
  onShowHistory,
}: NodePoolPredictionModalProps) {

  const [exportingMD, setExportingMD] = useState(false);
  const [exportingPDF, setExportingPDF] = useState(false);

  // Dados dos gráficos de tendência (memoizados para evitar recálculo em renders)
  const cpuChartData = useMemo(() => buildTrendChartData(result?.raw_metrics?.cpu_trend_per_node), [result]);
  const memChartData = useMemo(() => buildTrendChartData(result?.raw_metrics?.mem_trend_per_node), [result]);
  const podsChartData = useMemo(() => buildTrendChartData(result?.raw_metrics?.pods_trend_per_node), [result]);

  const handleClose = () => {
    if (loading) return;
    onOpenChange(false);
  };

  const handleExportPDF = async () => {
    if (!result) {
      toast.error("Nenhum resultado disponível para exportar");
      return;
    }
    setExportingPDF(true);
    try {
      await generateNodePoolPredictionPDF(result);
      toast.success("Relatório PDF exportado com sucesso");
    } catch (err) {
      console.error("Erro ao exportar PDF:", err);
      toast.error("Erro ao gerar relatório PDF");
    } finally {
      setExportingPDF(false);
    }
  };

  const handleExportMD = async () => {
    if (!result?.request_id) {
      toast.error("ID da análise não disponível");
      return;
    }
    setExportingMD(true);
    try {
      const md = await apiClient.getNodePoolPredictionReport(result.request_id);
      const blob = new Blob([md], { type: "text/markdown" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      const ts = new Date(result.analyzed_at).toISOString().replace(/[:.]/g, "-").slice(0, 16);
      a.href = url;
      a.download = `nodepool_${nodepoolName}_${ts}.md`;
      a.click();
      URL.revokeObjectURL(url);
      toast.success("Relatório Markdown exportado");
    } catch (err) {
      toast.error("Erro ao exportar relatório");
    } finally {
      setExportingMD(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent
        className="max-w-5xl h-[90vh] flex flex-col p-0"
        onInteractOutside={(e) => e.preventDefault()}
        onEscapeKeyDown={(e) => { if (loading) e.preventDefault(); }}
      >
        {/* Header */}
        <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b">
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <TrendingUp className="w-5 h-5 text-blue-500" />
                Análise Preditiva — {nodepoolName}
              </DialogTitle>
              <DialogDescription>
                Análise baseada em métricas históricas, conntrack, autoscaler e IA
              </DialogDescription>
            </div>
            <div className="flex gap-2">
              {onShowHistory && (
                <Button variant="outline" size="sm" onClick={onShowHistory}>
                  <History className="w-4 h-4 mr-2" />
                  Histórico
                </Button>
              )}
              {result && !result.error && (
                <>
                  <Button variant="outline" size="sm" onClick={handleExportPDF} disabled={exportingPDF}>
                    {exportingPDF ? <Loader2 className="w-4 h-4 animate-spin" /> : <Download className="w-4 h-4 mr-2" />}
                    Exportar PDF
                  </Button>
                  <Button variant="outline" size="sm" onClick={handleExportMD} disabled={exportingMD}>
                    {exportingMD ? <Loader2 className="w-4 h-4 animate-spin" /> : <Download className="w-4 h-4 mr-2" />}
                    Exportar MD
                  </Button>
                </>
              )}
              <Button
                variant="ghost"
                size="sm"
                disabled={loading}
                onClick={handleClose}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="w-4 h-4 mr-1" />
                Fechar
              </Button>
            </div>
          </div>
        </DialogHeader>

        {/* Body */}
        <ScrollArea className="flex-1 px-6" style={{ height: "calc(90vh - 140px)" }}>
          <div className="py-4">
            {/* Loading state */}
            {loading && (
              <div className="flex flex-col items-center justify-center py-24 gap-4">
                <Loader2 className="w-10 h-10 animate-spin text-primary" />
                <p className="text-muted-foreground text-sm">
                  Coletando métricas do node pool e executando análise IA...
                </p>
              </div>
            )}

            {/* Error state */}
            {!loading && result?.error && (
              <div className="bg-destructive/10 border border-destructive/50 rounded-lg p-4">
                <p className="text-destructive font-semibold">Erro na análise:</p>
                <p className="text-sm text-muted-foreground mt-2">{result.error}</p>
              </div>
            )}

            {/* Success state */}
            {!loading && result && !result.error && (
              <div className="space-y-6">

                {/* ── ACTION SUMMARY ────────────────────────────────────── */}
                {result.action_summary && (
                  <div className={`rounded-lg p-4 border-l-4 ${
                    result.action_summary.status === "critical"
                      ? "bg-red-500/10 border-red-500"
                      : result.action_summary.status === "attention"
                      ? "bg-yellow-500/10 border-yellow-500"
                      : "bg-green-500/10 border-green-500"
                  }`}>
                    <div className="flex items-start gap-3 mb-3">
                      {result.action_summary.status === "critical" ? (
                        <TriangleAlert className="w-6 h-6 text-red-500 flex-shrink-0 mt-0.5" />
                      ) : result.action_summary.status === "attention" ? (
                        <TriangleAlert className="w-6 h-6 text-yellow-500 flex-shrink-0 mt-0.5" />
                      ) : (
                        <CheckCircle2 className="w-6 h-6 text-green-500 flex-shrink-0 mt-0.5" />
                      )}
                      <div className="flex-1">
                        <h3 className={`font-bold text-lg leading-tight ${
                          result.action_summary.status === "critical" ? "text-red-500" :
                          result.action_summary.status === "attention" ? "text-yellow-500" :
                          "text-green-500"
                        }`}>
                          {result.action_summary.status_message}
                        </h3>
                        {result.action_summary.critical_reason && (
                          <p className="text-sm text-muted-foreground mt-1">
                            {result.action_summary.critical_reason}
                          </p>
                        )}
                      </div>
                    </div>

                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                      {result.action_summary.hours_to_critical != null && (
                        <div className="bg-background/50 rounded p-2 text-center">
                          <div className="text-2xl font-bold text-red-500">
                            {result.action_summary.hours_to_critical}h
                          </div>
                          <div className="text-xs text-muted-foreground">até crítico</div>
                        </div>
                      )}
                      {result.saturation_timeline?.most_critical?.estimated_date && (
                        <div className="bg-background/50 rounded p-2 text-center">
                          <div className={`text-base font-bold leading-tight ${
                            result.saturation_timeline.most_critical.urgency_badge === "CRITICO" ? "text-red-400" :
                            result.saturation_timeline.most_critical.urgency_badge === "ATENCAO" ? "text-yellow-400" :
                            "text-green-400"
                          }`}>
                            {new Date(result.saturation_timeline.most_critical.estimated_date).toLocaleDateString("pt-BR")}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            satura ({result.saturation_timeline.most_critical.metric})
                          </div>
                        </div>
                      )}
                      <div className="bg-background/50 rounded p-2 text-center">
                        <div className="text-2xl font-bold">{result.action_summary.total_actions}</div>
                        <div className="text-xs text-muted-foreground">
                          ações ({result.action_summary.urgent_actions} urgentes)
                        </div>
                      </div>
                      <div className="bg-background/50 rounded p-2 text-center">
                        <div className="text-2xl font-bold">
                          {result.action_summary.next_review_days === 0
                            ? "Hoje"
                            : `${result.action_summary.next_review_days}d`}
                        </div>
                        <div className="text-xs text-muted-foreground">próx. revisão</div>
                      </div>
                      <div className="bg-background/50 rounded p-2 text-center">
                        <div className={`text-2xl font-bold ${
                          (result.action_summary.overall_confidence ?? 0) >= 70 ? "text-green-500" :
                          (result.action_summary.overall_confidence ?? 0) >= 50 ? "text-yellow-500" :
                          "text-red-500"
                        }`}>
                          {((result.action_summary.overall_confidence ?? 0)).toFixed(0)}%
                        </div>
                        <div className="text-xs text-muted-foreground">confiança</div>
                      </div>
                    </div>

                    {result.action_summary.top_action && (
                      <div className="mt-3 p-2 bg-background/50 rounded">
                        <div className="text-xs text-muted-foreground mb-1">Ação Principal:</div>
                        <div className="font-medium text-sm">{result.action_summary.top_action}</div>
                        {result.action_summary.top_action_command && (
                          <code className="block mt-1 text-xs bg-secondary/50 p-1 rounded font-mono text-primary">
                            {result.action_summary.top_action_command}
                          </code>
                        )}
                      </div>
                    )}
                  </div>
                )}

                {/* ── DELTA (comparação com análise anterior) ─────────── */}
                {result.delta && (
                  <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                    <h3 className="font-semibold mb-2 flex items-center gap-2">
                      <History className="w-4 h-4 text-primary" />
                      Comparação com análise anterior
                      <span className="text-xs text-muted-foreground font-normal">
                        — há {result.delta.days_since < 1
                          ? `${Math.round(result.delta.days_since * 24)}h`
                          : `${Math.round(result.delta.days_since)}d`}
                      </span>
                    </h3>
                    <p className="text-xs text-muted-foreground italic mb-3">{result.delta.summary}</p>
                    <div className="flex flex-wrap gap-2">
                      {/* Health Score */}
                      {result.delta.health_score_delta !== 0 && (
                        <Badge variant="outline" className={
                          result.delta.health_score_delta > 0
                            ? "border-green-500 text-green-400"
                            : "border-red-500 text-red-400"
                        }>
                          Health {result.delta.health_score_delta > 0 ? "+" : ""}{result.delta.health_score_delta}
                        </Badge>
                      )}
                      {/* CPU */}
                      {Math.abs(result.delta.cpu_delta_pp ?? 0) >= 1 && (
                        <Badge variant="outline" className={
                          result.delta.cpu_delta_pp < 0
                            ? "border-green-500 text-green-400"
                            : "border-red-500 text-red-400"
                        }>
                          CPU {result.delta.cpu_delta_pp > 0 ? "+" : ""}{(result.delta.cpu_delta_pp).toFixed(1)}pp
                        </Badge>
                      )}
                      {/* Memória */}
                      {Math.abs(result.delta.mem_delta_pp ?? 0) >= 1 && (
                        <Badge variant="outline" className={
                          result.delta.mem_delta_pp < 0
                            ? "border-green-500 text-green-400"
                            : "border-red-500 text-red-400"
                        }>
                          Mem {result.delta.mem_delta_pp > 0 ? "+" : ""}{(result.delta.mem_delta_pp).toFixed(1)}pp
                        </Badge>
                      )}
                      {/* Pods */}
                      {Math.abs(result.delta.pods_delta ?? 0) >= 0.5 && (
                        <Badge variant="outline" className={
                          result.delta.pods_delta < 0
                            ? "border-green-500 text-green-400"
                            : "border-yellow-500 text-yellow-400"
                        }>
                          Pods {result.delta.pods_delta > 0 ? "+" : ""}{(result.delta.pods_delta).toFixed(1)}/node
                        </Badge>
                      )}
                      {/* conntrack */}
                      {Math.abs(result.delta.conntrack_delta_pp ?? 0) >= 0.5 && (
                        <Badge variant="outline" className={
                          result.delta.conntrack_delta_pp < 0
                            ? "border-green-500 text-green-400"
                            : "border-red-500 text-red-400"
                        }>
                          conntrack {result.delta.conntrack_delta_pp > 0 ? "+" : ""}{(result.delta.conntrack_delta_pp).toFixed(1)}pp
                        </Badge>
                      )}
                      {/* Fragmentação */}
                      {Math.abs(result.delta.bin_packing_delta_pp ?? 0) >= 2 && (
                        <Badge variant="outline" className={
                          result.delta.bin_packing_delta_pp > 0
                            ? "border-green-500 text-green-400"
                            : "border-yellow-500 text-yellow-400"
                        }>
                          Frag. {result.delta.bin_packing_delta_pp > 0 ? "+" : ""}{(result.delta.bin_packing_delta_pp).toFixed(1)}pp
                        </Badge>
                      )}
                      {/* Nenhuma variação significativa */}
                      {(result.delta.improving?.length ?? 0) === 0 &&
                       (result.delta.degrading?.length ?? 0) === 0 && (
                        <span className="text-xs text-muted-foreground">
                          Sem variação significativa nas métricas
                        </span>
                      )}
                    </div>
                    {((result.delta.improving?.length ?? 0) > 0 || (result.delta.degrading?.length ?? 0) > 0) && (
                      <div className="mt-2 flex flex-wrap gap-4 text-xs">
                        {(result.delta.improving?.length ?? 0) > 0 && (
                          <span className="text-green-400">
                            Melhorando: {result.delta.improving.join(", ")}
                          </span>
                        )}
                        {(result.delta.degrading?.length ?? 0) > 0 && (
                          <span className="text-red-400">
                            Degradando: {result.delta.degrading.join(", ")}
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                )}

                {/* ── HEALTH SCORE ──────────────────────────────────────── */}
                <div className="bg-gradient-card border border-border/50 rounded-lg p-4">
                  <h3 className="font-semibold mb-3 flex items-center gap-2">
                    <Activity className="w-4 h-4 text-primary" />
                    Health Score
                  </h3>
                  <div className="flex items-center gap-6">
                    <div className={`text-5xl font-bold ${healthColor(result.health_score?.overall ?? 0)}`}>
                      {result.health_score?.overall ?? 0}
                      <span className="text-xl text-muted-foreground">/100</span>
                    </div>
                    <div className="flex-1 space-y-2">
                      <div className="flex items-center justify-between text-sm">
                        <span className="text-muted-foreground">Categoria</span>
                        <Badge className={riskBadge(result.health_score?.risk_level ?? "medium")}>
                          {healthCategory(result.health_score?.overall ?? 0)}
                        </Badge>
                      </div>
                      <Progress
                        value={result.health_score?.overall ?? 0}
                        className="h-3"
                      />
                    </div>
                  </div>
                  {/* Breakdown */}
                  {result.health_score?.breakdown && (
                    <div className="mt-4 grid grid-cols-2 md:grid-cols-5 gap-2 text-center">
                      {[
                        { label: "Disponibilidade", value: result.health_score.breakdown.node_availability, weight: "25%" },
                        { label: "Headroom", value: result.health_score.breakdown.resource_headroom, weight: "30%" },
                        { label: "Densidade Pods", value: result.health_score.breakdown.pod_density, weight: "20%" },
                        { label: "conntrack", value: result.health_score.breakdown.conntrack_safety, weight: "15%" },
                        { label: "Autoscaler", value: result.health_score.breakdown.autoscaler_health, weight: "10%" },
                      ].map((item) => (
                        <div key={item.label} className="bg-background/50 rounded p-2">
                          <div className={`text-lg font-bold ${healthColor(item.value ?? 0)}`}>
                            {item.value ?? 0}
                          </div>
                          <div className="text-xs text-muted-foreground leading-tight">{item.label}</div>
                          <div className="text-xs text-muted-foreground/60">{item.weight}</div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <Accordion type="multiple" className="space-y-3">

                  {/* ── ESTADO ATUAL DOS NODES ────────────────────────── */}
                  {result.raw_metrics?.nodes_snapshot?.length > 0 && (
                    <AccordionItem value="nodes" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <Server className="w-4 h-4 text-primary" />
                          Estado dos Nodes
                          <Badge variant="outline" className="ml-2">
                            {result.raw_metrics.nodes_snapshot.length} nodes
                          </Badge>
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-2 mb-2">
                          <div className="grid grid-cols-3 gap-3 text-center">
                            <div className="bg-background/50 rounded p-2">
                              <div className="text-2xl font-bold">{result.raw_metrics.current_nodes}</div>
                              <div className="text-xs text-muted-foreground">Nodes atuais</div>
                            </div>
                            <div className="bg-background/50 rounded p-2">
                              <div className="text-2xl font-bold text-muted-foreground">
                                {result.raw_metrics.data_sources?.azure_api_available
                                  ? `${result.raw_metrics.min_nodes}–${result.raw_metrics.max_nodes}`
                                  : "N/A"}
                              </div>
                              <div className="text-xs text-muted-foreground">min–max</div>
                            </div>
                            <div className="bg-background/50 rounded p-2">
                              <div className={`text-2xl font-bold ${!result.raw_metrics.data_sources?.azure_api_available ? "text-muted-foreground" : result.raw_metrics.autoscaler_enabled ? "text-green-500" : "text-muted-foreground"}`}>
                                {!result.raw_metrics.data_sources?.azure_api_available ? "N/A" : result.raw_metrics.autoscaler_enabled ? "ON" : "OFF"}
                              </div>
                              <div className="text-xs text-muted-foreground">Autoscaler</div>
                            </div>
                          </div>
                        </div>
                        <div className="overflow-x-auto">
                          <table className="w-full text-sm">
                            <thead>
                              <tr className="border-b border-border/50 text-muted-foreground text-xs">
                                <th className="text-left py-1 pr-3">Node</th>
                                <th className="text-right py-1 px-1" title="CPU usado (Metrics Server)">CPU uso</th>
                                <th className="text-right py-1 px-1" title="CPU solicitado pelos pods (requests)">CPU req</th>
                                <th className="text-right py-1 px-1" title="Mem usada (Metrics Server)">Mem uso</th>
                                <th className="text-right py-1 px-1" title="Mem solicitada pelos pods (requests)">Mem req</th>
                                <th className="text-right py-1 px-1">Pods</th>
                                <th className="text-right py-1 pl-2">Status</th>
                              </tr>
                            </thead>
                            <tbody>
                              {result.raw_metrics.nodes_snapshot.slice(0, 10).map((n: any, i: number) => (
                                <tr key={i} className="border-b border-border/20 hover:bg-background/30">
                                  <td className="py-1.5 pr-3 font-mono text-xs truncate max-w-[140px]" title={n.node_name}>
                                    {n.node_name}
                                    {n.is_unschedulable && <Badge variant="destructive" className="ml-1 text-xs py-0 px-1">cordoned</Badge>}
                                  </td>
                                  <td className={`text-right py-1.5 px-1 font-medium text-xs ${n.cpu_usage_percent >= 80 ? "text-red-400" : n.cpu_usage_percent >= 60 ? "text-yellow-400" : "text-green-400"}`}>
                                    {fmtPct(n.cpu_usage_percent)}
                                  </td>
                                  <td className={`text-right py-1.5 px-1 text-xs ${n.cpu_requested_percent >= 90 ? "text-red-400" : n.cpu_requested_percent >= 70 ? "text-yellow-400" : "text-muted-foreground"}`}
                                      title={`${(n.cpu_requested_cores ?? 0).toFixed(2)} cores solicitados`}>
                                    {n.cpu_requested_percent > 0 ? fmtPct(n.cpu_requested_percent) : "—"}
                                  </td>
                                  <td className={`text-right py-1.5 px-1 font-medium text-xs ${n.mem_usage_percent >= 80 ? "text-red-400" : n.mem_usage_percent >= 60 ? "text-yellow-400" : "text-green-400"}`}>
                                    {fmtPct(n.mem_usage_percent)}
                                  </td>
                                  <td className={`text-right py-1.5 px-1 text-xs ${n.mem_requested_percent >= 90 ? "text-red-400" : n.mem_requested_percent >= 70 ? "text-yellow-400" : "text-muted-foreground"}`}
                                      title={`${(n.mem_requested_gb ?? 0).toFixed(2)} GB solicitados`}>
                                    {n.mem_requested_percent > 0 ? fmtPct(n.mem_requested_percent) : "—"}
                                  </td>
                                  <td className="text-right py-1.5 px-1 text-xs">{n.pod_count}</td>
                                  <td className="text-right py-1.5 pl-2">
                                    <Badge variant={n.status === "Ready" ? "outline" : "destructive"} className="text-xs">
                                      {n.status || "Unknown"}
                                    </Badge>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── CONNTRACK (destaque especial) ─────────────────── */}
                  {result.raw_metrics?.conntrack_pool?.has_sufficient_data && (
                    <AccordionItem value="conntrack" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <Network className="w-4 h-4 text-orange-400" />
                          Análise conntrack
                          {result.raw_metrics.conntrack_pool.max_usage >= 85 && (
                            <Badge variant="destructive" className="ml-2 text-xs">CRÍTICO</Badge>
                          )}
                          {result.raw_metrics.conntrack_pool.max_usage >= 70 &&
                           result.raw_metrics.conntrack_pool.max_usage < 85 && (
                            <Badge className="ml-2 text-xs bg-yellow-500/20 text-yellow-500 border-yellow-500/30">ATENÇÃO</Badge>
                          )}
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        {/* Resumo cluster-wide */}
                        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${conntrackColor(result.raw_metrics.conntrack_pool.avg_usage)}`}>
                              {fmtPct(result.raw_metrics.conntrack_pool.avg_usage)}
                            </div>
                            <div className="text-xs text-muted-foreground">Uso médio pool</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${conntrackColor(result.raw_metrics.conntrack_pool.max_usage)}`}>
                              {fmtPct(result.raw_metrics.conntrack_pool.max_usage)}
                            </div>
                            <div className="text-xs text-muted-foreground">Pico (max node)</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${result.raw_metrics.conntrack_pool.nodes_warning > 0 ? "text-yellow-500" : "text-green-500"}`}>
                              {result.raw_metrics.conntrack_pool.nodes_warning}
                            </div>
                            <div className="text-xs text-muted-foreground">Nodes warning (&gt;70%)</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${result.raw_metrics.conntrack_pool.nodes_critical > 0 ? "text-red-500" : "text-green-500"}`}>
                              {result.raw_metrics.conntrack_pool.nodes_critical}
                            </div>
                            <div className="text-xs text-muted-foreground">Nodes críticos (&gt;85%)</div>
                          </div>
                        </div>
                        {result.raw_metrics.conntrack_pool.avg_growth_rate_per_h > 0 && (
                          <p className="text-sm text-muted-foreground mb-3">
                            Taxa de crescimento: <span className="font-medium text-foreground">
                              {result.raw_metrics.conntrack_pool.avg_growth_rate_per_h.toFixed(0)} entries/hora
                            </span>
                          </p>
                        )}
                        {/* Tabela por node */}
                        {result.raw_metrics.conntrack_pool.nodes?.length > 0 && (
                          <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                              <thead>
                                <tr className="border-b border-border/50 text-muted-foreground text-xs">
                                  <th className="text-left py-1 pr-3">Node</th>
                                  <th className="text-right py-1 px-2">Entries</th>
                                  <th className="text-right py-1 px-2">Limit</th>
                                  <th className="text-right py-1 px-2">Uso%</th>
                                  <th className="py-1 pl-2 text-left">Saturação</th>
                                </tr>
                              </thead>
                              <tbody>
                                {result.raw_metrics.conntrack_pool.nodes.map((cn: any, i: number) => (
                                  <tr key={i} className="border-b border-border/20 hover:bg-background/30">
                                    <td className="py-1.5 pr-3 font-mono text-xs truncate max-w-[160px]">{cn.node_name}</td>
                                    <td className="text-right py-1.5 px-2">{cn.current_entries?.toLocaleString()}</td>
                                    <td className="text-right py-1.5 px-2 text-muted-foreground">{cn.max_entries?.toLocaleString()}</td>
                                    <td className={`text-right py-1.5 px-2 font-bold ${conntrackColor(cn.usage_percent)}`}>
                                      {fmtPct(cn.usage_percent)}
                                    </td>
                                    <td className="py-1.5 pl-2 w-32">
                                      <div className="w-full bg-background/50 rounded-full h-2 relative">
                                        <div
                                          className={`h-2 rounded-full ${conntrackBg(cn.usage_percent)}`}
                                          style={{ width: `${Math.max(Math.min(cn.usage_percent ?? 0, 100), 2)}%` }}
                                        />
                                      </div>
                                      <div className="text-xs text-muted-foreground/60 mt-0.5">
                                        {(cn.usage_percent ?? 0).toFixed(2)}% de {cn.max_entries?.toLocaleString() ?? "131.072"}
                                      </div>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        )}
                        {/* Cluster-wide aggregate */}
                        {result.raw_metrics.conntrack_cluster && (
                          <div className="mt-3 p-2 bg-background/30 rounded text-xs text-muted-foreground">
                            <span className="font-medium text-foreground">Cluster-wide:</span>{" "}
                            {result.raw_metrics.conntrack_cluster.total_entries?.toLocaleString()} entries /{" "}
                            {result.raw_metrics.conntrack_cluster.total_limit?.toLocaleString()} limit
                            ({fmtPct(result.raw_metrics.conntrack_cluster.avg_usage)} médio,{" "}
                            {fmtPct(result.raw_metrics.conntrack_cluster.max_usage)} pico)
                          </div>
                        )}
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── TENDÊNCIAS ────────────────────────────────────── */}
                  {result.trends && (
                    <AccordionItem value="trends" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <BarChart3 className="w-4 h-4 text-primary" />
                          Tendências (por node, normalizadas)
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-3">

                          {/* CPU */}
                          <div className="bg-background/50 rounded p-3">
                            <div className="flex items-center justify-between mb-1">
                              <span className="font-medium text-sm">CPU por node</span>
                              <div className="flex items-center gap-1">
                                {trendIcon(result.trends.cpu_trend)}
                                <span className="text-xs text-muted-foreground">{trendLabel(result.trends.cpu_trend)}</span>
                              </div>
                            </div>
                            {cpuChartData ? (
                              <TrendMiniChart
                                data={cpuChartData}
                                color={trendLineColor(result.trends.cpu_trend)}
                                unit="%"
                                showThreshold={true}
                              />
                            ) : null}
                            <div className="flex gap-4 justify-end text-xs mt-1">
                              {[
                                ["D-3", result.trends.cpu_change_3d_percent],
                                ["D-7", result.trends.cpu_change_7d_percent],
                                ["D-14", result.trends.cpu_change_14d_percent],
                              ].map(([lbl, val]) =>
                                val != null && val !== undefined ? (
                                  <span key={lbl as string}>
                                    <span className="text-muted-foreground">{lbl}: </span>
                                    <span className={`font-bold ${(val as number) > 0 ? "text-red-400" : "text-green-400"}`}>
                                      {(val as number) > 0 ? "+" : ""}{(val as number).toFixed(1)}%
                                    </span>
                                  </span>
                                ) : null
                              )}
                            </div>
                          </div>

                          {/* Memória */}
                          <div className="bg-background/50 rounded p-3">
                            <div className="flex items-center justify-between mb-1">
                              <span className="font-medium text-sm">Memória por node</span>
                              <div className="flex items-center gap-1">
                                {trendIcon(result.trends.mem_trend)}
                                <span className="text-xs text-muted-foreground">{trendLabel(result.trends.mem_trend)}</span>
                              </div>
                            </div>
                            {memChartData ? (
                              <TrendMiniChart
                                data={memChartData}
                                color={trendLineColor(result.trends.mem_trend)}
                                unit="%"
                                showThreshold={true}
                              />
                            ) : null}
                            <div className="flex gap-4 justify-end text-xs mt-1">
                              {[
                                ["D-7", result.trends.mem_change_7d_percent],
                                ["D-14", result.trends.mem_change_14d_percent],
                              ].map(([lbl, val]) =>
                                val != null && val !== undefined ? (
                                  <span key={lbl as string}>
                                    <span className="text-muted-foreground">{lbl}: </span>
                                    <span className={`font-bold ${(val as number) > 0 ? "text-red-400" : "text-green-400"}`}>
                                      {(val as number) > 0 ? "+" : ""}{(val as number).toFixed(1)}%
                                    </span>
                                  </span>
                                ) : null
                              )}
                            </div>
                          </div>

                          {/* Pods/node */}
                          <div className="bg-background/50 rounded p-3">
                            <div className="flex items-center justify-between mb-1">
                              <span className="font-medium text-sm">Pods por node</span>
                              <div className="flex items-center gap-1">
                                {trendIcon(result.trends.pods_trend)}
                                <span className="text-xs text-muted-foreground">{trendLabel(result.trends.pods_trend)}</span>
                              </div>
                            </div>
                            {podsChartData ? (
                              <TrendMiniChart
                                data={podsChartData}
                                color={trendLineColor(result.trends.pods_trend)}
                                unit=" pods"
                                showThreshold={false}
                              />
                            ) : null}
                            <div className="flex gap-4 justify-end text-xs mt-1">
                              {result.trends.pods_change_7d_percent != null && (
                                <span>
                                  <span className="text-muted-foreground">D-7: </span>
                                  <span className={`font-bold ${result.trends.pods_change_7d_percent > 0 ? "text-red-400" : "text-green-400"}`}>
                                    {result.trends.pods_change_7d_percent > 0 ? "+" : ""}{result.trends.pods_change_7d_percent.toFixed(1)}%
                                  </span>
                                </span>
                              )}
                            </div>
                          </div>

                          {/* conntrack — texto apenas (sem histórico de snapshots) */}
                          {result.trends.conntrack_trend && (
                            <div className="bg-background/50 rounded p-3">
                              <div className="flex items-center justify-between">
                                <span className="font-medium text-sm">conntrack</span>
                                <div className="flex items-center gap-1">
                                  {trendIcon(result.trends.conntrack_trend)}
                                  <span className="text-xs text-muted-foreground">{trendLabel(result.trends.conntrack_trend)}</span>
                                </div>
                              </div>
                              {result.trends.conntrack_change_7d_percent !== 0 &&
                               result.trends.conntrack_change_7d_percent != null && (
                                <div className="flex gap-4 justify-end text-xs mt-1">
                                  <span>
                                    <span className="text-muted-foreground">D-7 est.: </span>
                                    <span className={`font-bold ${result.trends.conntrack_change_7d_percent > 0 ? "text-red-400" : "text-green-400"}`}>
                                      {result.trends.conntrack_change_7d_percent > 0 ? "+" : ""}{result.trends.conntrack_change_7d_percent.toFixed(1)}%
                                    </span>
                                  </span>
                                </div>
                              )}
                              <p className="text-xs text-muted-foreground mt-1">
                                Tendência estimada via taxa de crescimento (sem histórico D-14)
                              </p>
                            </div>
                          )}

                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── TIMELINE DE SATURAÇÃO ─────────────────────────── */}
                  {result.saturation_timeline?.forecasts?.length > 0 && (
                    <AccordionItem value="saturation" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <CalendarClock className="w-4 h-4 text-red-400" />
                          Timeline de Saturação
                          {result.saturation_timeline.most_critical && (
                            <Badge variant="outline" className={`ml-2 text-xs ${
                              result.saturation_timeline.most_critical.urgency_badge === "CRITICO"
                                ? "border-red-500 text-red-400"
                                : result.saturation_timeline.most_critical.urgency_badge === "ATENCAO"
                                ? "border-yellow-500 text-yellow-400"
                                : "border-green-500 text-green-400"
                            }`}>
                              {result.saturation_timeline.most_critical.urgency_badge}
                            </Badge>
                          )}
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        {result.saturation_timeline.summary && (
                          <p className="text-sm text-muted-foreground mb-3 italic">
                            {result.saturation_timeline.summary}
                          </p>
                        )}
                        <div className="space-y-2">
                          {result.saturation_timeline.forecasts.map((f: any, idx: number) => {
                            const urgencyColor =
                              f.urgency_badge === "CRITICO"
                                ? "border-l-red-500 bg-red-500/5"
                                : f.urgency_badge === "ATENCAO"
                                ? "border-l-yellow-500 bg-yellow-500/5"
                                : "border-l-green-500 bg-green-500/5";
                            const badgeVariant =
                              f.urgency_badge === "CRITICO"
                                ? "text-red-400 border-red-500"
                                : f.urgency_badge === "ATENCAO"
                                ? "text-yellow-400 border-yellow-500"
                                : "text-green-400 border-green-500";

                            return (
                              <div key={idx} className={`border-l-4 rounded-r p-3 ${urgencyColor}`}>
                                <div className="flex items-center justify-between mb-1">
                                  <span className="font-semibold text-sm capitalize">
                                    {f.metric}
                                    {f.affected_node && (
                                      <span className="text-muted-foreground font-normal ml-1 text-xs">
                                        ({f.affected_node})
                                      </span>
                                    )}
                                  </span>
                                  <div className="flex items-center gap-2">
                                    <Badge variant="outline" className={`text-xs ${badgeVariant}`}>
                                      {f.urgency_badge}
                                    </Badge>
                                    <span className="text-xs text-muted-foreground">
                                      conf. {f.confidence}
                                    </span>
                                  </div>
                                </div>
                                <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-xs mt-2">
                                  <div>
                                    <span className="text-muted-foreground block">Atual</span>
                                    <span className="font-medium">{f.current_value?.toFixed(1)}%</span>
                                  </div>
                                  <div>
                                    <span className="text-muted-foreground block">Crescimento/dia</span>
                                    <span className={`font-medium ${f.daily_growth_rate > 0 ? "text-red-400" : "text-green-400"}`}>
                                      {f.daily_growth_rate > 0 ? "+" : ""}{f.daily_growth_rate?.toFixed(2)}%
                                    </span>
                                  </div>
                                  <div>
                                    <span className="text-muted-foreground block">Dias restantes</span>
                                    <span className="font-medium">
                                      {f.days_until_saturation != null
                                        ? `${Math.round(f.days_until_saturation)}d`
                                        : "—"}
                                    </span>
                                  </div>
                                  <div>
                                    <span className="text-muted-foreground block">Data estimada</span>
                                    <span className="font-medium">
                                      {f.estimated_date
                                        ? new Date(f.estimated_date).toLocaleDateString("pt-BR")
                                        : "—"}
                                    </span>
                                  </div>
                                </div>
                                {/* Barra de progresso visual */}
                                <div className="mt-2">
                                  <Progress
                                    value={Math.min(100, f.current_value ?? 0)}
                                    className="h-1.5"
                                  />
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── AUTOSCALER ────────────────────────────────────── */}
                  {result.raw_metrics?.autoscaler_events?.length > 0 && (
                    <AccordionItem value="autoscaler" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <Zap className="w-4 h-4 text-yellow-400" />
                          Histórico Autoscaler
                          <Badge variant="outline" className="ml-2 text-xs">
                            {result.raw_metrics.autoscaler_events.length} eventos
                          </Badge>
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-2 max-h-56 overflow-y-auto">
                          {result.raw_metrics.autoscaler_events.map((ev: any, i: number) => (
                            <div key={i} className="text-xs bg-background/50 rounded p-2 flex items-start gap-2">
                              <Badge
                                variant={ev.event_type === "ScaleUp" ? "destructive" : "default"}
                                className="text-xs flex-shrink-0"
                              >
                                {ev.event_type}
                              </Badge>
                              <div>
                                <p className="font-medium">{ev.reason}</p>
                                {ev.message && <p className="text-muted-foreground">{ev.message}</p>}
                                <p className="text-muted-foreground/60 mt-0.5">
                                  {ev.timestamp ? new Date(ev.timestamp).toLocaleString("pt-BR") : ""}
                                </p>
                              </div>
                            </div>
                          ))}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── BIN PACKING ───────────────────────────────────── */}
                  {result.raw_metrics?.bin_packing && (
                    <AccordionItem value="binpacking" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <Activity className="w-4 h-4 text-primary" />
                          Análise de Bin Packing
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${healthColor(result.raw_metrics.bin_packing.cpu_efficiency ?? 0)}`}>
                              {fmtPct(result.raw_metrics.bin_packing.cpu_efficiency)}
                            </div>
                            <div className="text-xs text-muted-foreground">Eficiência CPU</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${healthColor(result.raw_metrics.bin_packing.mem_efficiency ?? 0)}`}>
                              {fmtPct(result.raw_metrics.bin_packing.mem_efficiency)}
                            </div>
                            <div className="text-xs text-muted-foreground">Eficiência Mem</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className={`text-xl font-bold ${(result.raw_metrics.bin_packing.scale_in_candidates ?? 0) > 0 ? "text-yellow-500" : "text-muted-foreground"}`}>
                              {result.raw_metrics.bin_packing.scale_in_candidates ?? 0}
                            </div>
                            <div className="text-xs text-muted-foreground">Candidatos scale-in</div>
                          </div>
                          <div className="bg-background/50 rounded p-2 text-center">
                            <div className="text-xl font-bold text-muted-foreground">
                              {result.raw_metrics.bin_packing.fragmentation_level ?? "—"}
                            </div>
                            <div className="text-xs text-muted-foreground">Fragmentação</div>
                          </div>
                        </div>
                        {result.raw_metrics.bin_packing.wasted_resources && (
                          <p className="text-xs text-muted-foreground mt-2">
                            Recursos desperdiçados: {result.raw_metrics.bin_packing.wasted_resources}
                          </p>
                        )}
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── HPAs DO POOL ─────────────────────────────────── */}
                  {(result.raw_metrics?.hpa_correlation?.length ?? 0) > 0 && (
                    <AccordionItem value="hpa" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <Scale className="w-4 h-4 text-blue-400" />
                          HPAs neste Pool
                          {result.raw_metrics.hpa_correlation.some((h: any) => h.at_max) && (
                            <Badge className="ml-2 text-xs bg-red-500/20 text-red-400 border-red-500/30">
                              {result.raw_metrics.hpa_correlation.filter((h: any) => h.at_max).length} em limite
                            </Badge>
                          )}
                          {!result.raw_metrics.hpa_correlation.some((h: any) => h.at_max) && (
                            <Badge className="ml-2 text-xs bg-green-500/20 text-green-500 border-green-500/30">
                              {result.raw_metrics.hpa_correlation.length} HPAs
                            </Badge>
                          )}
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-2">
                          {result.raw_metrics.hpa_correlation.map((hpa: any, i: number) => (
                            <div
                              key={i}
                              className={`flex items-center justify-between rounded p-2 text-xs ${
                                hpa.at_max
                                  ? "bg-red-500/10 border border-red-500/30"
                                  : "bg-background/50 border border-border/30"
                              }`}
                            >
                              <div className="flex flex-col gap-0.5 min-w-0">
                                <span className="font-medium truncate">{hpa.hpa_name}</span>
                                <span className="text-muted-foreground">{hpa.namespace} · {hpa.target_kind}/{hpa.target_name}</span>
                              </div>
                              <div className="flex items-center gap-3 shrink-0 ml-3">
                                {/* Réplicas */}
                                <div className="text-center">
                                  <div className={`font-bold ${hpa.at_max ? "text-red-400" : "text-foreground"}`}>
                                    {hpa.current_replicas}/{hpa.max_replicas}
                                  </div>
                                  <div className="text-muted-foreground">replicas</div>
                                </div>
                                {/* Pods no pool */}
                                <div className="text-center">
                                  <div className="font-medium">{hpa.pods_on_pool}/{hpa.total_pods}</div>
                                  <div className="text-muted-foreground">no pool</div>
                                </div>
                                {/* Target CPU */}
                                {hpa.target_cpu_percent > 0 && (
                                  <div className="text-center">
                                    <div className="font-medium">{hpa.target_cpu_percent}%</div>
                                    <div className="text-muted-foreground">target CPU</div>
                                  </div>
                                )}
                                {/* Badge at_max */}
                                {hpa.at_max ? (
                                  <Badge className="text-xs bg-red-500/20 text-red-400 border-red-500/30">
                                    LIMITE
                                  </Badge>
                                ) : (
                                  <Badge className="text-xs bg-green-500/20 text-green-500 border-green-500/30">
                                    OK
                                  </Badge>
                                )}
                              </div>
                            </div>
                          ))}
                        </div>
                        {result.raw_metrics.hpa_correlation.some((h: any) => h.at_max) && (
                          <p className="text-xs text-red-400/80 mt-3">
                            HPAs em limite maximo nao conseguem escalar mais. Se a carga aumentar, as replicas existentes absorvem toda a pressao sem possibilidade de escalonamento horizontal.
                          </p>
                        )}
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── CUSTO ────────────────────────────────────────── */}
                  {result.cost_analysis && (
                    <AccordionItem value="cost" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <DollarSign className="w-4 h-4 text-green-400" />
                          Análise de Custo
                          {result.cost_analysis.monthly_savings_usd > 0 && (
                            <Badge className="ml-2 text-xs bg-green-500/20 text-green-500 border-green-500/30">
                              Economia potencial: {fmtUSD(result.cost_analysis.monthly_savings_usd)}/mês
                            </Badge>
                          )}
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="grid grid-cols-2 md:grid-cols-3 gap-3 mb-4">
                          <div className="bg-background/50 rounded p-3">
                            <div className="text-xs text-muted-foreground mb-1">VM Size</div>
                            <div className="font-semibold text-sm">{result.cost_analysis.vm_size}</div>
                            <div className="text-xs text-muted-foreground">
                              {result.cost_analysis.vm_cpu_cores} vCPUs · {result.cost_analysis.vm_memory_gb} GB
                            </div>
                          </div>
                          <div className="bg-background/50 rounded p-3">
                            <div className="text-xs text-muted-foreground mb-1">Custo Atual/mês</div>
                            <div className="font-bold text-green-500">{fmtUSD(result.cost_analysis.current_monthly_cost_usd)}</div>
                            <div className="text-xs text-muted-foreground">{fmtBRL(result.cost_analysis.current_monthly_cost_brl)}</div>
                          </div>
                          <div className="bg-background/50 rounded p-3">
                            <div className="text-xs text-muted-foreground mb-1">Custo Máximo/mês</div>
                            <div className="font-bold text-orange-500">{fmtUSD(result.cost_analysis.max_monthly_cost_usd)}</div>
                            <div className="text-xs text-muted-foreground">{result.raw_metrics?.max_nodes} nodes</div>
                          </div>
                        </div>
                        {result.cost_analysis.idle_waste_percent > 0 && (
                          <div className="bg-yellow-500/10 border border-yellow-500/30 rounded p-2 mb-3 text-sm">
                            <span className="text-yellow-500 font-medium">Desperdício idle: </span>
                            {fmtPct(result.cost_analysis.idle_waste_percent)} — {fmtUSD(result.cost_analysis.waste_monthly_cost_usd)}/mês
                          </div>
                        )}
                        {result.cost_analysis.monthly_savings_usd > 0 && (
                          <div className="bg-green-500/10 border border-green-500/30 rounded p-2 mb-3">
                            <div className="text-sm font-medium text-green-500">
                              Economia potencial com {result.cost_analysis.recommended_nodes} nodes (P95):
                            </div>
                            <div className="text-lg font-bold text-green-500">
                              {fmtUSD(result.cost_analysis.monthly_savings_usd)}/mês ({fmtBRL(result.cost_analysis.monthly_savings_brl)})
                            </div>
                            <div className="text-xs text-muted-foreground">
                              Anual: {fmtUSD(result.cost_analysis.annual_savings_usd)} / {fmtBRL(result.cost_analysis.annual_savings_brl)}
                            </div>
                          </div>
                        )}
                        {result.cost_analysis.recommendations?.length > 0 && (
                          <div className="space-y-2">
                            <div className="text-sm font-medium text-muted-foreground">Recomendações de custo:</div>
                            {result.cost_analysis.recommendations.map((rec: any, i: number) => (
                              <div key={i} className="bg-background/50 rounded p-2 text-sm">
                                <div className="font-medium">{rec.title}</div>
                                <div className="text-xs text-muted-foreground">{rec.description}</div>
                                {rec.savings_usd > 0 && (
                                  <div className="text-xs text-green-500 mt-1">
                                    Economia: {fmtUSD(rec.savings_usd)}/mês · Impacto: {rec.impact}
                                  </div>
                                )}
                              </div>
                            ))}
                          </div>
                        )}
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── PREVISÕES IA ──────────────────────────────────── */}
                  {(result.predictions?.short_term?.length > 0 ||
                    result.predictions?.medium_term?.length > 0 ||
                    result.predictions?.long_term?.length > 0) && (
                    <AccordionItem value="predictions" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <TrendingUp className="w-4 h-4 text-blue-400" />
                          Previsões IA
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-4">
                          {[
                            { label: "Curto Prazo", items: result.predictions.short_term },
                            { label: "Médio Prazo", items: result.predictions.medium_term },
                            { label: "Longo Prazo", items: result.predictions.long_term },
                          ].map(({ label, items }) =>
                            items?.length > 0 ? (
                              <div key={label}>
                                <h4 className="text-sm font-medium text-muted-foreground mb-2">{label}</h4>
                                <div className="space-y-2">
                                  {items.map((p: any, i: number) => (
                                    <div key={i} className="bg-background/50 rounded p-3">
                                      <div className="flex items-start justify-between gap-2 mb-1">
                                        <p className="text-sm flex-1">{p.event}</p>
                                        <Badge variant={severityBadgeVariant(p.severity)} className="text-xs flex-shrink-0">
                                          {p.severity}
                                        </Badge>
                                      </div>
                                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                                        <span>Confiança: {p.confidence_percent?.toFixed(0) ?? "?"}%</span>
                                        <span>Prob: {((p.probability ?? 0) * 100).toFixed(0)}%</span>
                                        <span>Timeframe: {p.timeframe}</span>
                                      </div>
                                      {p.impact && (
                                        <p className="text-xs text-muted-foreground mt-1">{p.impact}</p>
                                      )}
                                    </div>
                                  ))}
                                </div>
                              </div>
                            ) : null
                          )}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── RECOMENDAÇÕES ─────────────────────────────────── */}
                  {result.recommendations?.length > 0 && (
                    <AccordionItem value="recommendations" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <CheckCircle2 className="w-4 h-4 text-primary" />
                          Recomendações
                          <Badge variant="outline" className="ml-2 text-xs">
                            {result.recommendations.length}
                          </Badge>
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-2">
                          {result.recommendations.map((rec: any, i: number) => (
                            <div key={i} className="bg-background/50 rounded p-3">
                              <div className="flex items-start justify-between gap-2">
                                <div className="flex items-start gap-2 flex-1">
                                  <span className="text-muted-foreground text-sm font-bold flex-shrink-0">{i + 1}.</span>
                                  <div>
                                    <div className="text-sm font-medium">{rec.title}</div>
                                    <div className="text-xs text-muted-foreground">{rec.description}</div>
                                    {rec.action_command && (
                                      <code className="block mt-1 text-xs bg-secondary/50 p-1 rounded font-mono text-primary">
                                        {rec.action_command}
                                      </code>
                                    )}
                                  </div>
                                </div>
                                <div className="flex flex-col gap-1 text-right flex-shrink-0">
                                  <Badge variant={
                                    rec.priority === 1 ? "destructive" :
                                    rec.priority === 2 ? "default" : "secondary"
                                  } className="text-xs">
                                    P{rec.priority}
                                  </Badge>
                                  {rec.category && (
                                    <Badge variant="outline" className="text-xs">{rec.category}</Badge>
                                  )}
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                  {/* ── SUMÁRIO EXECUTIVO ────────────────────────────── */}
                  {result.executive_summary?.current_state && (
                    <AccordionItem value="executive" className="bg-gradient-card border border-border/50 rounded-lg px-4">
                      <AccordionTrigger className="hover:no-underline">
                        <span className="flex items-center gap-2 font-semibold">
                          <BarChart3 className="w-4 h-4 text-primary" />
                          Sumário Executivo
                        </span>
                      </AccordionTrigger>
                      <AccordionContent>
                        <div className="space-y-3">
                          <p className="text-sm">{result.executive_summary.current_state}</p>
                          <Separator />
                          <div className="flex gap-4">
                            <div>
                              <div className="text-xs text-muted-foreground">Nível de Risco</div>
                              <Badge className={`mt-1 ${riskBadge(result.executive_summary.risk_level ?? "medium")}`}>
                                {result.executive_summary.risk_level ?? "—"}
                              </Badge>
                            </div>
                            {result.executive_summary.action_required && (
                              <div>
                                <div className="text-xs text-muted-foreground">Ação Necessária</div>
                                <div className="text-sm font-medium mt-1">{result.executive_summary.action_required}</div>
                              </div>
                            )}
                          </div>
                          {result.executive_summary.key_findings?.length > 0 && (
                            <div>
                              <div className="text-xs font-medium text-muted-foreground mb-1">Principais Constatações:</div>
                              <ul className="space-y-1">
                                {result.executive_summary.key_findings.map((f: string, i: number) => (
                                  <li key={i} className="text-xs text-muted-foreground flex items-start gap-1">
                                    <span className="text-primary mt-0.5">•</span>
                                    <span>{f}</span>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                          {result.executive_summary.next_steps?.length > 0 && (
                            <div>
                              <div className="text-xs font-medium text-muted-foreground mb-1">Próximos Passos:</div>
                              <ul className="space-y-1">
                                {result.executive_summary.next_steps.map((s: string, i: number) => (
                                  <li key={i} className="text-xs text-muted-foreground flex items-start gap-1">
                                    <span className="text-blue-400 mt-0.5">{i + 1}.</span>
                                    <span>{s}</span>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                        </div>
                      </AccordionContent>
                    </AccordionItem>
                  )}

                </Accordion>
              </div>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

export default NodePoolPredictionModal;
