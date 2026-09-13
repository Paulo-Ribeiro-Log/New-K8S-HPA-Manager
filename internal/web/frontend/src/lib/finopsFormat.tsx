import { useState, useCallback } from "react";
import { Card, CardContent } from "@/components/ui/card";
import {
  DollarSign, TrendingDown, TrendingUp, AlertTriangle, CheckCircle2, Info, Copy, Check,
} from "lucide-react";

// Helpers/componentes compartilhados de FinOps — extraídos de FinOpsTab.tsx (que já passava de
// 4500 linhas) pra permitir que RightsizingTab.tsx (8ª aba, arquivo próprio) reaproveite sem
// duplicar. FinOpsTab.tsx importa daqui em vez de declarar localmente — mesmo comportamento de
// antes, só movido.

export const fmtBRL = (v: number) =>
  v.toLocaleString("pt-BR", { style: "currency", currency: "BRL", maximumFractionDigits: 0 });

export const fmtUSD = (v: number) =>
  v.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

/** Formata millicores: 1500 → "1.5" (cores), 250 → "250m" */
export const fmtMillis = (v: number) =>
  v >= 1000 ? `${(v / 1000).toFixed(v % 1000 === 0 ? 0 : 1)}` : `${Math.round(v)}m`;

/** Formata Mi: 2048 → "2.0Gi", 512 → "512Mi" */
export const fmtMi = (v: number) => (v >= 1024 ? `${(v / 1024).toFixed(1)}Gi` : `${Math.round(v)}Mi`);

export const verdictConfig: Record<string, { label: string; color: string; fill: string; icon: typeof CheckCircle2 }> = {
  superprovisioned: { label: "Desperdício", fill: "#ef4444", color: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400", icon: TrendingDown },
  oom_risk: { label: "Risco OOM", fill: "#f59e0b", color: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400", icon: AlertTriangle },
  ok: { label: "Eficiente", fill: "#10b981", color: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400", icon: CheckCircle2 },
  no_request: { label: "Sem Request", fill: "#9ca3af", color: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400", icon: Info },
  hpa_removable: { label: "Remover HPA", fill: "#8b5cf6", color: "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400", icon: Info },
  fixed_high_cost: { label: "Sem HPA", fill: "#f97316", color: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400", icon: TrendingUp },
};

export const POOL_COLORS = ["#6366f1", "#8b5cf6", "#06b6d4", "#10b981", "#f59e0b", "#ef4444", "#ec4899"];

/** Exibe um comando kubectl com botão de copiar. */
export function KubectlBlock({ cmd }: { cmd: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(cmd).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [cmd]);
  return (
    <div className="flex items-center gap-1.5">
      <code className="flex-1 text-[10px] font-mono bg-background border rounded px-2 py-1 break-all">
        {cmd}
      </code>
      <button
        onClick={handleCopy}
        title={copied ? "Copiado!" : "Copiar"}
        className="shrink-0 p-1 rounded hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
      >
        {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}

export function SummaryCard({ icon: Icon, label, value, sub, color }: {
  icon: typeof DollarSign; label: string; value: string; sub?: string; color: string;
}) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-start justify-between gap-2">
          <div>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className={`text-xl font-bold ${color}`}>{value}</p>
            {sub && <p className="text-xs text-muted-foreground mt-0.5">{sub}</p>}
          </div>
          <Icon className={`h-5 w-5 mt-0.5 ${color}`} />
        </div>
      </CardContent>
    </Card>
  );
}

export function VerdictBadge({ verdict }: { verdict: string }) {
  const cfg = verdictConfig[verdict] ?? verdictConfig.ok;
  const Icon = cfg.icon;
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium ${cfg.color}`}>
      <Icon className="h-3 w-3" />
      {cfg.label}
    </span>
  );
}
