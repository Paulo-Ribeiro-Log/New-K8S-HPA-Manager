import { type Level, type StatusSummary } from "@/lib/metricsLive";
import { fmtInt, fmtLatency, fmtRps } from "@/lib/metricsReport";
import type { Kpis } from "@/lib/metricsLiveView";
import { BADGE_CLASS, LevelIcon } from "@/components/metricsLiveShared";

const LEVELS: Level[] = ["red", "warn", "ok", "info"];
const NAME: Record<Level, string> = { red: "ERRO", warn: "ALERTA", ok: "OK", info: "SEM DADO" };

const tip = (status: StatusSummary | null, lv: Level) =>
  (status?.[lv] ?? []).map((e) => `${e.name}${e.reasons.length ? ` — ${e.reasons.join("; ")}` : ""}`).join("\n");

/**
 * Faixa de resumo do topo: quantas aplicações em cada situação (clicar filtra), o RPS total, a pior latência
 * (com o nome da aplicação) e o total de 5xx. O número é grande e em tinta primária; a cor fica no ícone e na
 * borda, que também carregam o texto da situação.
 */
export function MetricsLiveSummary({
  kpis, status, level, onLevel,
}: {
  kpis: Kpis;
  status: StatusSummary | null;
  level: Level | null;
  onLevel: (l: Level | null) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 xl:grid-cols-8" aria-label="Resumo do tempo real">
      {LEVELS.map((lv) => {
        const n = kpis.counts[lv];
        if (lv === "info" && n === 0) return null;
        const active = level === lv;
        return (
          <button
            key={lv}
            type="button"
            disabled={n === 0}
            aria-pressed={active}
            onClick={() => onLevel(active ? null : lv)}
            title={n === 0 ? `Nenhuma aplicação em ${NAME[lv]}` : `${tip(status, lv)}\n\nClique para ${active ? "tirar o filtro" : "filtrar só estas"}`}
            className={`rounded-md border px-3 py-2 text-left transition ${n > 0 ? BADGE_CLASS[lv] : "border-border text-muted-foreground opacity-60"} ${active ? "ring-2 ring-offset-1 ring-offset-background ring-current" : ""} ${n > 0 ? "cursor-pointer hover:brightness-125" : "cursor-default"}`}
          >
            <div className="flex items-center gap-1.5 text-[11px] font-medium"><LevelIcon level={lv} className="h-3.5 w-3.5" />{NAME[lv]}</div>
            <div className="text-2xl font-semibold leading-tight tabular-nums text-foreground">{n}</div>
            <div className="text-[10px] text-muted-foreground">de {kpis.total} aplicação(ões)</div>
          </button>
        );
      })}
      <Tile label="RPS total" value={kpis.rps == null ? "—" : fmtRps(kpis.rps)} sub="req/s, soma das aplicações" />
      <Tile
        label={kpis.worst ? `Pior P${kpis.worst.percentile}` : "Pior latência"}
        value={kpis.worst ? fmtLatency(kpis.worst.ms) : "—"}
        sub={kpis.worst ? kpis.worst.name : "sem dado"}
        subTitle={kpis.worst?.name}
      />
      <Tile
        label="Erros 5xx"
        value={kpis.e5Total == null ? "—" : fmtInt(kpis.e5Total)}
        sub={kpis.e5Rate == null ? "sem dado" : `no período · ${fmtRps(kpis.e5Rate)}/s agora`}
        alert={(kpis.e5Total ?? 0) > 0}
      />
    </div>
  );
}

function Tile({ label, value, sub, subTitle, alert = false }: { label: string; value: string; sub: string; subTitle?: string; alert?: boolean }) {
  return (
    <div className={`rounded-md border px-3 py-2 ${alert ? "border-red-500/50" : "border-border"}`}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="text-2xl font-semibold leading-tight tabular-nums text-foreground">{value}</div>
      <div className="truncate text-[10px] text-muted-foreground" title={subTitle ?? sub}>{sub}</div>
    </div>
  );
}

