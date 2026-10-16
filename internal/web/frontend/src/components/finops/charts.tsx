// Peças visuais compartilhadas pelas abas Relatório e Node Pools do FinOps.
import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

// Paleta da decomposição de custo (mesma no Relatório, no Node Pools e no PDF).
export const COST_COLORS = {
  productive: "#10b981", // em uso
  waste: "#f59e0b",      // request acima do necessário
  idle: "#f43f5e",       // capacidade ociosa
  osDisk: "#38bdf8",
  pvc: "#818cf8",
  orphan: "#e879f9",
} as const;

export const AXIS_TICK = { fontSize: 10, fill: "hsl(var(--muted-foreground))" };

// Caixa de tooltip dos gráficos Recharts.
export function TooltipBox({ title, children }: { title?: ReactNode; children: ReactNode }) {
  return (
    <div className="rounded-lg border bg-popover/95 backdrop-blur px-3 py-2 text-xs shadow-xl">
      {title && <p className="font-semibold mb-1 text-foreground">{title}</p>}
      <div className="space-y-0.5 text-muted-foreground">{children}</div>
    </div>
  );
}

export function TooltipRow({ color, label, value }: { color?: string; label: ReactNode; value: ReactNode }) {
  return (
    <p className="flex items-center gap-2">
      {color && <span className="h-2 w-2 rounded-full shrink-0" style={{ background: color }} />}
      <span className="flex-1">{label}</span>
      <span className="font-medium text-foreground tabular-nums">{value}</span>
    </p>
  );
}

// Indicador grande (número + rótulo + linha de apoio).
export function StatTile({ icon: Icon, label, value, sub, tone = "default" }: {
  icon: LucideIcon;
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  tone?: "default" | "good" | "warn" | "bad";
}) {
  const toneCls = {
    default: "text-foreground",
    good: "text-emerald-600 dark:text-emerald-400",
    warn: "text-amber-600 dark:text-amber-400",
    bad: "text-rose-600 dark:text-rose-400",
  }[tone];
  return (
    <div className="rounded-xl border bg-card p-4 shadow-sm">
      <p className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        <Icon className="h-3.5 w-3.5" /> {label}
      </p>
      <p className={`mt-1.5 text-2xl font-semibold tabular-nums leading-tight ${toneCls}`}>{value}</p>
      {sub && <p className="mt-1 text-xs text-muted-foreground">{sub}</p>}
    </div>
  );
}

// Barra de capacidade: trilho = capacidade; faixa clara = requisitado; faixa cheia = em uso.
export function CapacityBar({ label, cap, req, use, hasUsage, format, capLabel = "", limiting = false }: {
  label: string;
  cap: number;
  req: number;
  use: number;
  hasUsage: boolean;
  format: (v: number) => string;
  capLabel?: string;   // ex.: "alocável"
  limiting?: boolean;  // é o recurso que limita o pool
}) {
  const reqPct = cap > 0 ? (req / cap) * 100 : 0;
  const usePct = cap > 0 ? (use / cap) * 100 : 0;
  const over = reqPct > 100;
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="font-medium">
          {label}
          {limiting && <span className="ml-1.5 rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">limita o pool</span>}
        </span>
        <span className="text-muted-foreground tabular-nums">
          <span className={over ? "text-rose-600 font-semibold" : "text-indigo-600 dark:text-indigo-400 font-semibold"}>{Math.round(reqPct)}%</span> requisitado
          {hasUsage && <> · <span className="text-emerald-600 dark:text-emerald-400 font-semibold">{Math.round(usePct)}%</span> em uso</>}
        </span>
      </div>
      <div className="relative h-2.5 rounded-full bg-muted overflow-hidden">
        <div className="absolute inset-y-0 left-0 rounded-full bg-indigo-400/35 dark:bg-indigo-400/30" style={{ width: `${Math.min(100, reqPct)}%` }} />
        {hasUsage && (
          <div className="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-emerald-500 to-emerald-400" style={{ width: `${Math.min(100, usePct)}%` }} />
        )}
      </div>
      <p className="text-[11px] text-muted-foreground tabular-nums">
        {format(req)} requisitados{hasUsage && <> · {format(use)} em uso</>} · capacidade {capLabel && `${capLabel} `}{format(cap)}
      </p>
    </div>
  );
}

// Medidor circular (SVG puro): valor em %, faixa ideal pintada em verde.
export function RingGauge({ value, label, hint, goodFrom = 60, goodTo = 85 }: {
  value: number | null;
  label: string;
  hint: string;
  goodFrom?: number;
  goodTo?: number;
}) {
  const r = 34, c = 2 * Math.PI * r;
  const v = value ?? 0;
  const color = value === null ? "hsl(var(--muted-foreground))"
    : v > 100 ? COST_COLORS.idle
    : v >= goodFrom && v <= goodTo ? COST_COLORS.productive
    : v >= goodFrom * 0.6 ? COST_COLORS.waste
    : COST_COLORS.idle;
  return (
    <div className="flex flex-col items-center text-center">
      <div className="relative h-24 w-24">
        <svg viewBox="0 0 84 84" className="h-full w-full -rotate-90">
          <circle cx="42" cy="42" r={r} fill="none" stroke="hsl(var(--muted))" strokeWidth="8" />
          <circle cx="42" cy="42" r={r} fill="none" stroke={color} strokeWidth="8" strokeLinecap="round"
            strokeDasharray={`${(Math.min(100, v) / 100) * c} ${c}`} className="transition-all duration-700" />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-lg font-semibold tabular-nums" style={{ color }}>
          {value === null ? "—" : `${value}%`}
        </span>
      </div>
      <p className="mt-1.5 text-xs font-medium">{label}</p>
      <p className="text-[10px] text-muted-foreground max-w-[9rem]">{hint}</p>
    </div>
  );
}

// Cabeçalho de seção dos cards.
export function SectionTitle({ icon: Icon, title, sub, right }: { icon: LucideIcon; title: string; sub?: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 mb-3">
      <div>
        <h3 className="text-sm font-semibold flex items-center gap-2"><Icon className="h-4 w-4 text-primary" /> {title}</h3>
        {sub && <p className="text-xs text-muted-foreground mt-0.5">{sub}</p>}
      </div>
      {right}
    </div>
  );
}
