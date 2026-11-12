import { useMemo } from "react";
import { AlertTriangle, CheckCircle2, CircleHelp, XCircle } from "lucide-react";
import { LEVEL_LABEL, sparkHeights, type Level } from "@/lib/metricsLive";
import { REPORT_TZ, fmtRps } from "@/lib/metricsReport";

// Peças compartilhadas pelas visões do Tempo real (cartões, tabela e faixa de resumo).

export const COLORS = { rps: "#38bdf8", p50: "#34d399", p95: "#fbbf24", p99: "#f87171", e4: "#fbbf24", e5: "#f87171", proj: "#a78bfa", avg: "#2dd4bf", grid: "#374151", axis: "#9ca3af", spark: "#d946ef" };
export const LEVEL_CLASS: Record<Level, string> = { ok: "text-emerald-400", warn: "text-amber-400", red: "text-red-400", info: "text-sky-400" };
export const BADGE_CLASS: Record<Level, string> = {
  red: "border-red-500/50 bg-red-500/10 text-red-400",
  warn: "border-amber-500/50 bg-amber-500/10 text-amber-400",
  ok: "border-emerald-500/50 bg-emerald-500/10 text-emerald-400",
  info: "border-sky-500/50 bg-sky-500/10 text-sky-400",
};

export const fmtClock = (ms: number, withDate: boolean) =>
  new Date(ms).toLocaleString("pt-BR", { timeZone: REPORT_TZ, ...(withDate ? { day: "2-digit", month: "2-digit" } : {}), hour: "2-digit", minute: "2-digit" });

const ICON: Record<Level, typeof CheckCircle2> = { ok: CheckCircle2, warn: AlertTriangle, red: XCircle, info: CircleHelp };

/** Situação como ícone + texto de apoio: a cor nunca é o único sinal (daltonismo, impressão). */
export function LevelIcon({ level, className = "h-4 w-4" }: { level: Level; className?: string }) {
  const Icon = ICON[level];
  return (
    <span className="inline-flex" title={LEVEL_LABEL[level]}>
      <Icon className={`${className} ${LEVEL_CLASS[level]}`} aria-hidden="true" />
      <span className="sr-only">{LEVEL_LABEL[level]}</span>
    </span>
  );
}

// Barras do RPS no período (SVG leve; lacuna = sem barra). Cada barra leva a hora e o valor na dica.
// `compact` é a versão da tabela: menor e sem o rótulo do máximo.
export function Spark({ times, values, compact = false }: { times: number[]; values: (number | null)[]; compact?: boolean }) {
  const { heights, max } = useMemo(() => sparkHeights(values), [values]);
  const n = heights.length;
  if (n === 0) return <span className="text-muted-foreground">{compact ? "—" : "sem dados"}</span>;
  const H = 16;
  return (
    <span className="inline-flex items-center gap-2">
      <svg viewBox={`0 0 ${n * 3} ${H}`} preserveAspectRatio="none" className={compact ? "h-4 w-20" : "h-4 w-48 max-w-full"} role="img" aria-label="RPS no período">
        {heights.map((h, i) =>
          h == null ? null : (
            <rect key={i} x={i * 3} y={H - Math.max(1, h * H)} width={2} height={Math.max(1, h * H)} fill={compact ? COLORS.rps : COLORS.spark}>
              <title>{`${fmtClock(times[i], true)}: ${fmtRps(values[i])} req/s`}</title>
            </rect>
          ),
        )}
      </svg>
      {!compact && <span className="text-fuchsia-400">(Máx: {fmtRps(max)})</span>}
    </span>
  );
}
