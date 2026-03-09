import { useState, useEffect, useRef, useCallback } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Loader2, RefreshCw, Copy, Check } from "lucide-react";
import type { PodSummary, PodMetricsSingle } from "@/lib/api/types";
import { formatAge, formatMillicores, formatBytes, formatPercent } from "@/lib/monitorUtils";
import { apiClient } from "@/lib/api/client";
import { ProtectedAction } from "@/components/rbac";
import { toast } from "sonner";

// Gauge duplo concêntrico: anel externo = MEM, anel interno = CPU
function DualGauge({ cpuPct, memPct, cpuVal, memVal }: {
  cpuPct: number; memPct: number; cpuVal: string; memVal: string;
}) {
  const CX = 52, CY = 52;
  const R_OUT = 42, R_IN = 28, STROKE = 7;
  const clamp = (v: number) => Math.max(0, Math.min(100, isNaN(v) ? 0 : v));
  const cp = clamp(cpuPct), mp = clamp(memPct);

  const arc = (r: number, pct: number) => {
    const circ = 2 * Math.PI * r;
    return { dash: circ, offset: circ * (1 - pct / 100) };
  };
  const memArc = arc(R_OUT, mp);
  const cpuArc = arc(R_IN, cp);

  const cpuColor = cp >= 90 ? "#ef4444" : cp >= 70 ? "#f97316" : "#22c55e";
  const memColor = mp >= 90 ? "#ef4444" : mp >= 70 ? "#f97316" : "#8b5cf6";

  return (
    <div className="flex flex-col items-center flex-shrink-0">
      <svg width="104" height="104" viewBox="0 0 104 104">
        <circle cx={CX} cy={CY} r={R_OUT} fill="none" stroke="rgba(255,255,255,0.07)" strokeWidth={STROKE} />
        <circle cx={CX} cy={CY} r={R_IN} fill="none" stroke="rgba(255,255,255,0.07)" strokeWidth={STROKE} />
        <circle cx={CX} cy={CY} r={R_OUT} fill="none"
          stroke={memColor} strokeWidth={STROKE}
          strokeDasharray={memArc.dash} strokeDashoffset={memArc.offset}
          strokeLinecap="round" transform={`rotate(-90 ${CX} ${CY})`}
          style={{ transition: "stroke-dashoffset 0.6s ease" }}
        />
        <circle cx={CX} cy={CY} r={R_IN} fill="none"
          stroke={cpuColor} strokeWidth={STROKE}
          strokeDasharray={cpuArc.dash} strokeDashoffset={cpuArc.offset}
          strokeLinecap="round" transform={`rotate(-90 ${CX} ${CY})`}
          style={{ transition: "stroke-dashoffset 0.6s ease" }}
        />
        <text x={CX} y={CY - 5} textAnchor="middle" style={{ fontSize: 13, fontWeight: 700, fill: cpuColor }}>
          {cp < 1 && cpuPct > 0 ? "<1%" : `${Math.round(cp)}%`}
        </text>
        <text x={CX} y={CY + 8} textAnchor="middle" style={{ fontSize: 8, fill: "rgba(255,255,255,0.45)" }}>
          CPU
        </text>
      </svg>
      <div className="flex gap-3 text-[9px] font-mono mt-0.5">
        <span style={{ color: cpuColor }}>● CPU {cpuVal}</span>
        <span style={{ color: memColor }}>● MEM {memVal}</span>
      </div>
    </div>
  );
}

function logLineColor(line: string): string {
  const l = line.toUpperCase();
  if (/\b(ERROR|FATAL|EXCEPTION|PANIC)\b/.test(l)) return "text-red-400";
  if (/\b(WARN|WARNING)\b/.test(l)) return "text-yellow-400";
  if (/\b(DEBUG|TRACE)\b/.test(l)) return "text-purple-400";
  if (/\b(INFO)\b/.test(l)) return "text-blue-400";
  if (/\s[45]\d{2}\s/.test(line)) return "text-orange-400";
  if (/\s2\d{2}\s/.test(line)) return "text-green-400";
  return "";
}

type PendingAction = "restart" | "kill" | "delete" | null;

interface Props {
  pod: PodSummary | null;
  cluster: string;
  metrics?: PodMetricsSingle | null;
  onClose: () => void;
  onRefresh?: () => void;
}

export function PodQuickViewModal({ pod, cluster, metrics, onClose, onRefresh }: Props) {
  const [activeTab, setActiveTab] = useState("details");
  const [selectedContainer, setSelectedContainer] = useState("");
  const [tailLines, setTailLines] = useState("500");
  const [logs, setLogs] = useState("");
  const [logsLoading, setLogsLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [copied, setCopied] = useState(false);

  // Action state
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const [actionLoading, setActionLoading] = useState(false);

  const logsEndRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const containerNames = pod?.containers?.map(c => c.name) ?? [];

  useEffect(() => {
    if (!pod) return;
    setActiveTab("details");
    setLogs("");
    setAutoRefresh(true);
    setPendingAction(null);
    setSelectedContainer(pod.containers?.[0]?.name ?? "");
  }, [pod?.namespace, pod?.name]);

  const fetchLogs = useCallback(async () => {
    if (!pod || !cluster) return;
    setLogsLoading(true);
    try {
      const res = await apiClient.getPodLogs(
        cluster, pod.namespace, pod.name,
        selectedContainer || pod.containers?.[0]?.name,
        parseInt(tailLines)
      );
      setLogs(res.logs ?? "");
      setTimeout(() => logsEndRef.current?.scrollIntoView({ behavior: "smooth" }), 50);
    } catch {
      setLogs("Erro ao carregar logs.");
    } finally {
      setLogsLoading(false);
    }
  }, [pod, cluster, selectedContainer, tailLines]);

  useEffect(() => {
    if (activeTab !== "logs") return;
    fetchLogs();
  }, [activeTab, selectedContainer, tailLines, fetchLogs]);

  useEffect(() => {
    if (intervalRef.current) clearInterval(intervalRef.current);
    if (activeTab === "logs" && autoRefresh) {
      intervalRef.current = setInterval(fetchLogs, 5000);
    }
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [activeTab, autoRefresh, fetchLogs]);

  const copyLogs = () => {
    navigator.clipboard.writeText(logs);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const executeAction = async () => {
    if (!pod || !pendingAction) return;
    setActionLoading(true);
    try {
      if (pendingAction === "restart") {
        const res = await apiClient.restartPod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod reiniciado com sucesso");
        onRefresh?.();
        setPendingAction(null);
      } else if (pendingAction === "kill") {
        const res = await apiClient.killPod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod finalizado com sucesso");
        onRefresh?.();
        setPendingAction(null);
      } else if (pendingAction === "delete") {
        const res = await apiClient.deletePod(cluster, pod.namespace, pod.name);
        toast.success(res.message || "Pod deletado com sucesso");
        onClose();
        onRefresh?.();
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Erro ao executar ação";
      toast.error(msg);
      setPendingAction(null);
    } finally {
      setActionLoading(false);
    }
  };

  if (!pod) return null;

  const isHealthy = pod.phase === "Running" && pod.readyContainers === pod.totalContainers;
  const statusVariant = isHealthy ? "default" : (pod.phase === "Pending" ? "secondary" : "destructive");

  const cpuPct = metrics && metrics.cpuPercentRequest >= 0 ? metrics.cpuPercentRequest : 0;
  const memPct = metrics && metrics.memPercentRequest >= 0 ? metrics.memPercentRequest : 0;

  const actionConfig = {
    restart: {
      label: "Reiniciar Pod",
      desc: "O pod será reiniciado (deletado e recriado pelo controller).",
      confirmLabel: "Reiniciar",
      color: "bg-blue-600 hover:bg-blue-700 text-white",
    },
    kill: {
      label: "Kill Pod (Forçar)",
      desc: "O pod será encerrado imediatamente com SIGKILL.",
      confirmLabel: "Forçar Kill",
      color: "bg-orange-600 hover:bg-orange-700 text-white",
    },
    delete: {
      label: "Deletar Pod",
      desc: "O pod será deletado permanentemente do cluster.",
      confirmLabel: "Deletar",
      color: "bg-destructive hover:bg-destructive/90 text-white",
    },
  } as const;

  return (
    <Dialog open={!!pod} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent
        className="max-w-5xl w-[95vw] max-h-[90vh] flex flex-col p-0 gap-0"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader className="px-4 pt-4 pb-3 border-b border-border flex-shrink-0">
          <DialogTitle className="font-mono text-sm leading-snug">
            <span className="text-muted-foreground text-xs">{pod.namespace}/</span>
            <span className="text-foreground">{pod.name}</span>
          </DialogTitle>
          <div className="flex items-center gap-2 mt-1 flex-wrap">
            <Badge variant={statusVariant} className="text-[10px] h-4 px-1.5">
              {pod.statusReason || pod.phase}
            </Badge>
          </div>
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1 flex flex-col min-h-0">
          <TabsList className="mx-4 mt-3 flex-shrink-0 w-fit">
            <TabsTrigger value="details" className="text-xs">Detalhes</TabsTrigger>
            <TabsTrigger value="logs" className="text-xs">Logs</TabsTrigger>
          </TabsList>

          {/* ── DETALHES ── */}
          <TabsContent value="details" className="flex-1 overflow-auto mt-0">
            <div className="p-4 space-y-4">

              {/* Gauge + Info lado a lado */}
              <div className="flex gap-5 items-start">
                {metrics ? (
                  <DualGauge
                    cpuPct={cpuPct}
                    memPct={memPct}
                    cpuVal={formatMillicores(metrics.cpuMillicores)}
                    memVal={formatBytes(metrics.memoryBytes)}
                  />
                ) : (
                  <div className="w-[104px] h-[104px] flex items-center justify-center flex-shrink-0">
                    <span className="text-[10px] text-muted-foreground text-center leading-tight">
                      Métricas<br/>indisponíveis
                    </span>
                  </div>
                )}

                {/* Info grid ao lado do gauge */}
                <div className="flex-1 grid grid-cols-2 gap-x-6 gap-y-1.5 text-xs">
                  {[
                    { label: "Namespace",   value: pod.namespace },
                    { label: "Restarts",    value: String(pod.restarts) },
                    { label: "Ready",       value: `${pod.readyContainers}/${pod.totalContainers}` },
                    { label: "IP",          value: pod.podIP || "-" },
                    { label: "CPU Request", value: pod.cpuRequest || "-" },
                    { label: "CPU Limit",   value: pod.cpuLimit || "-" },
                    { label: "Mem Request", value: pod.memoryRequest || "-" },
                    { label: "Mem Limit",   value: pod.memoryLimit || "-" },
                    { label: "Age",         value: pod.createdAt ? formatAge(pod.createdAt) : "-" },
                  ].map(({ label, value }) => (
                    <div key={label} className="flex gap-2">
                      <span className="text-muted-foreground w-24 flex-shrink-0">{label}:</span>
                      <span className="font-mono font-medium">{value}</span>
                    </div>
                  ))}
                  {/* Node — ocupa coluna inteira para não truncar */}
                  <div className="flex gap-2 col-span-2">
                    <span className="text-muted-foreground w-24 flex-shrink-0">Node:</span>
                    <span className="font-mono font-medium break-all">{pod.nodeName || "-"}</span>
                  </div>
                  {/* Percentuais vs limits (só com métricas) */}
                  {metrics && (
                    <>
                      <div className="flex gap-2 col-span-2 mt-0.5 pt-1.5 border-t border-border/40">
                        <span className="text-muted-foreground w-24 flex-shrink-0">CPU vs Req:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.cpuPercentRequest)}</span>
                        <span className="text-muted-foreground ml-3 w-20">vs Limit:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.cpuPercentLimit)}</span>
                      </div>
                      <div className="flex gap-2 col-span-2">
                        <span className="text-muted-foreground w-24 flex-shrink-0">MEM vs Req:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.memPercentRequest)}</span>
                        <span className="text-muted-foreground ml-3 w-20">vs Limit:</span>
                        <span className="font-mono font-medium">{formatPercent(metrics.memPercentLimit)}</span>
                      </div>
                    </>
                  )}
                </div>
              </div>

              {/* Containers */}
              <div>
                <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">Containers</div>
                <div className="space-y-1.5">
                  {pod.containers.map(c => (
                    <div key={c.name} className="flex items-start gap-2 bg-muted/20 rounded px-3 py-2 text-xs font-mono">
                      <span className={`w-2 h-2 rounded-full mt-0.5 flex-shrink-0 ${c.ready ? "bg-green-500" : "bg-orange-500"}`} />
                      <div className="min-w-0">
                        <div className="font-medium text-foreground">{c.name}</div>
                        <div className="text-muted-foreground text-[10px] truncate">{c.image}</div>
                        <div className="text-[10px] mt-0.5 flex gap-2">
                          <span className={c.ready ? "text-green-400" : "text-orange-400"}>
                            {c.state}{c.stateReason ? ` (${c.stateReason})` : ""}
                          </span>
                          {c.restartCount > 0 && (
                            <span className="text-orange-400">{c.restartCount} restarts</span>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              {/* Labels */}
              {pod.labels && Object.keys(pod.labels).length > 0 && (
                <div>
                  <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">Labels</div>
                  <div className="flex flex-wrap gap-1">
                    {Object.entries(pod.labels).map(([k, v]) => (
                      <Badge key={k} variant="secondary" className="text-[9px] font-mono px-1.5 h-4">
                        {k}={v}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}

              {/* Ações */}
              <div className="pt-2 border-t border-border/50">
                <div className="text-[10px] font-medium text-muted-foreground uppercase mb-2">Ações</div>
                <div className="flex items-center gap-2 flex-wrap">
                  <ProtectedAction showWarning={false}>
                    <Button
                      size="sm" variant="outline"
                      className="h-7 text-xs text-blue-400 border-blue-400/30 hover:bg-blue-400/10"
                      onClick={() => setPendingAction(pendingAction === "restart" ? null : "restart")}
                    >
                      Rollout Restart
                    </Button>
                  </ProtectedAction>
                  <ProtectedAction showWarning={false}>
                    <Button
                      size="sm" variant="outline"
                      className="h-7 text-xs text-orange-400 border-orange-400/30 hover:bg-orange-400/10"
                      onClick={() => setPendingAction(pendingAction === "kill" ? null : "kill")}
                    >
                      Kill (Forçar)
                    </Button>
                  </ProtectedAction>
                  <ProtectedAction showWarning={false}>
                    <Button
                      size="sm" variant="outline"
                      className="h-7 text-xs text-destructive border-destructive/30 hover:bg-destructive/10"
                      onClick={() => setPendingAction(pendingAction === "delete" ? null : "delete")}
                    >
                      Deletar Pod
                    </Button>
                  </ProtectedAction>
                </div>

                {/* Barra de confirmação inline */}
                {pendingAction && (
                  <div className="mt-2 flex items-center gap-2 p-2 rounded border border-border bg-muted/30 text-xs">
                    <span className="flex-1 text-muted-foreground">
                      <strong className="text-foreground">{actionConfig[pendingAction].label}</strong>
                      {" — "}{actionConfig[pendingAction].desc}
                    </span>
                    <Button
                      size="sm"
                      className={`h-6 text-xs ${actionConfig[pendingAction].color}`}
                      onClick={executeAction}
                      disabled={actionLoading}
                    >
                      {actionLoading
                        ? <Loader2 className="w-3 h-3 animate-spin mr-1" />
                        : null}
                      {actionConfig[pendingAction].confirmLabel}
                    </Button>
                    <Button
                      size="sm" variant="ghost"
                      className="h-6 text-xs"
                      onClick={() => setPendingAction(null)}
                      disabled={actionLoading}
                    >
                      Cancelar
                    </Button>
                  </div>
                )}
              </div>
            </div>
          </TabsContent>

          {/* ── LOGS ── */}
          <TabsContent value="logs" className="flex-1 flex flex-col min-h-0 mt-0">
            <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0">
              <Select value={selectedContainer} onValueChange={setSelectedContainer}>
                <SelectTrigger className="h-7 text-xs w-44">
                  <SelectValue placeholder="Container" />
                </SelectTrigger>
                <SelectContent>
                  {containerNames.map(c => (
                    <SelectItem key={c} value={c} className="text-xs">{c}</SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Select value={tailLines} onValueChange={setTailLines}>
                <SelectTrigger className="h-7 text-xs w-24">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["100", "500", "1000", "5000"].map(n => (
                    <SelectItem key={n} value={n} className="text-xs">{n} linhas</SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Button
                size="sm" variant={autoRefresh ? "default" : "outline"}
                className="h-7 text-xs gap-1"
                onClick={() => setAutoRefresh(v => !v)}
              >
                <RefreshCw className={`w-3 h-3 ${autoRefresh ? "animate-spin" : ""}`} />
                Auto
              </Button>

              <Button
                size="sm" variant="outline" className="h-7 text-xs"
                onClick={fetchLogs} disabled={logsLoading}
              >
                {logsLoading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
              </Button>

              <div className="flex-1" />

              <Button
                size="sm" variant="ghost" className="h-7 text-xs gap-1"
                onClick={copyLogs} disabled={!logs}
              >
                {copied ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
                {copied ? "Copiado" : "Copiar"}
              </Button>
            </div>

            {/* Área de scroll de logs */}
            <div className="flex-1 min-h-0 overflow-auto bg-black/50">
              <div className="p-3 font-mono text-xs leading-5">
                {logsLoading && !logs ? (
                  <div className="text-muted-foreground flex items-center gap-2">
                    <Loader2 className="w-3 h-3 animate-spin" /> Carregando logs...
                  </div>
                ) : logs ? (
                  logs.split("\n").map((line, i) => (
                    <div key={i} className={`whitespace-pre-wrap break-all ${logLineColor(line)}`}>
                      {line || " "}
                    </div>
                  ))
                ) : (
                  <span className="text-muted-foreground">Nenhum log disponível.</span>
                )}
                <div ref={logsEndRef} />
              </div>
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
