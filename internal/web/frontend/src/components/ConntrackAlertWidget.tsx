import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { AlertTriangle, CheckCircle2, XCircle, RefreshCw, Activity } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type { ConntrackNodeStats } from "@/lib/api/types";

const statusColors = {
  ok:       { text: "text-emerald-400", icon: CheckCircle2, label: "OK" },
  warning:  { text: "text-amber-400",   icon: AlertTriangle, label: "Atenção" },
  critical: { text: "text-red-400",     icon: XCircle,       label: "Crítico" },
};

// Mesmo limiar de "atenção" já usado internamente pelo ConntrackTab (getCapacityRec, p95 >= 65)
// e o limiar de "crítico" já usado pelo backend (probeConntrack, >= 90).
function widgetStatus(highestPct: number): "ok" | "warning" | "critical" {
  if (highestPct >= 90) return "critical";
  if (highestPct >= 65) return "warning";
  return "ok";
}

interface Props {
  cluster: string;
  nodepool?: string | null;
}

export function ConntrackAlertWidget({ cluster, nodepool }: Props) {
  const [open, setOpen] = useState(false);

  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: ["conntrack-alert", cluster, nodepool],
    queryFn: () => apiClient.getConntrackStats(cluster, nodepool as string),
    enabled: !!cluster && !!nodepool,
    staleTime: 60 * 1000,
    retry: 1,
  });

  if (!nodepool) return null;

  const validNodes: ConntrackNodeStats[] = (data?.nodes ?? []).filter((n) => !n.error && n.max > 0);
  const highest = validNodes.reduce<ConntrackNodeStats | null>(
    (max, n) => (!max || n.usage_pct > max.usage_pct ? n : max),
    null
  );
  const highestPct = highest?.usage_pct ?? 0;
  const status = data ? widgetStatus(highestPct) : "ok";
  const colors = statusColors[status];
  const StatusIcon = colors.icon;

  return (
    <>
      <button
        className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg border text-xs hover:bg-muted/20 transition-colors ${
          status === "critical" ? "border-red-500/40 bg-red-500/5" :
          status === "warning"  ? "border-amber-500/40 bg-amber-500/5" :
          "border-border/50 bg-muted/10"
        }`}
        onClick={() => setOpen(true)}
      >
        <Activity className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
        <span className="font-medium text-foreground">Conntrack — {nodepool}</span>

        {isLoading || isFetching ? (
          <RefreshCw className="w-3 h-3 animate-spin text-muted-foreground ml-1" />
        ) : error ? (
          <span className="text-red-400 ml-1">Erro ao carregar</span>
        ) : data ? (
          <>
            <StatusIcon className={`w-3.5 h-3.5 ${colors.text} ml-1`} />
            <span className={`font-semibold ${colors.text}`}>{colors.label}</span>
            <span className="text-muted-foreground ml-auto">
              {highest ? (
                <>
                  nó mais saturado: <strong className="text-foreground">{highest.node_name}</strong>
                  {" · "}
                  <span className={colors.text}>{highestPct.toFixed(1)}%</span>
                </>
              ) : (
                "sem dados válidos"
              )}
            </span>
          </>
        ) : null}

        <span className="text-muted-foreground text-[11px] flex-shrink-0 ml-2">Ver detalhes →</span>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Conntrack — {nodepool}</DialogTitle>
          </DialogHeader>
          <div className="space-y-1.5 max-h-[60vh] overflow-y-auto">
            {validNodes.length === 0 && (
              <p className="text-sm text-muted-foreground py-4 text-center">Nenhum dado disponível.</p>
            )}
            {[...validNodes]
              .sort((a, b) => b.usage_pct - a.usage_pct)
              .map((n) => {
                const nStatus = widgetStatus(n.usage_pct);
                const nColors = statusColors[nStatus];
                return (
                  <div
                    key={n.node_name}
                    className="flex items-center justify-between gap-2 px-3 py-2 rounded border border-border/50 text-xs"
                  >
                    <span className="font-mono truncate">{n.node_name}</span>
                    <span className={`font-semibold ${nColors.text}`}>
                      {n.usage_pct.toFixed(1)}% ({n.count.toLocaleString("pt-BR")}/{n.max.toLocaleString("pt-BR")})
                    </span>
                  </div>
                );
              })}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
